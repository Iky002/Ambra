package compatext

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/handlers/livetv"
	syncapi "github.com/ambra-media/ambra/internal/api/handlers/sync"
	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/dvr"
	"github.com/ambra-media/ambra/internal/core/transcode"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

const (
	stateConfigKey  = "compatext:state"
	liveTVConfigKey = "livetv:state"
	syncConfigKey   = "sync:state"
)

// AuthFuncs decouples route registration from the concrete middleware package.
type AuthFuncs struct {
	RequireAuth   func(http.Handler) http.Handler
	RequireAdmin  func(http.Handler) http.Handler
	UserIDFromCtx func(context.Context) string
}

// ChannelMapping represents a tuner-to-EPG channel mapping entry.
type ChannelMapping struct {
	Name  string `json:"Name"`
	Value string `json:"Value"`
}

// OfflineAction records an offline playback/user-data action submitted by a sync client.
type OfflineAction struct {
	Date                  string `json:"Date,omitempty"`
	ItemId                string `json:"ItemId,omitempty"`
	ServerId              string `json:"ServerId,omitempty"`
	UserId                string `json:"UserId,omitempty"`
	Type                  string `json:"Type,omitempty"`
	IsFavorite            *bool  `json:"IsFavorite,omitempty"`
	Played                *bool  `json:"Played,omitempty"`
	PositionTicks         int64  `json:"PositionTicks,omitempty"`
	PlaybackPositionTicks int64  `json:"PlaybackPositionTicks,omitempty"`
	RecordedAtUnix        int64  `json:"RecordedAtUnix,omitempty"`
}

// NotificationServiceConfig represents a configured Emby user notification service.
type NotificationServiceConfig struct {
	Id          string            `json:"Id"`
	Name        string            `json:"Name"`
	FriendlyName string           `json:"FriendlyName,omitempty"`
	ServiceType string            `json:"ServiceType,omitempty"`
	UserId      string            `json:"UserId,omitempty"`
	Enabled     bool              `json:"Enabled"`
	Events      []string          `json:"Events,omitempty"`
	Options     map[string]string `json:"Options,omitempty"`
}

type compatExtState struct {
	ChannelDisabled      map[string]bool             `json:"channel_disabled,omitempty"`
	ChannelSortIndex     map[string]int              `json:"channel_sort_index,omitempty"`
	ChannelMappings      map[string][]ChannelMapping `json:"channel_mappings,omitempty"`
	TunerResetAt         map[string]int64            `json:"tuner_reset_at,omitempty"`
	RemovedJobItems      map[string]bool             `json:"removed_job_items,omitempty"`
	OfflineActions       []OfflineAction             `json:"offline_actions,omitempty"`
	NotificationServices []NotificationServiceConfig `json:"notification_services,omitempty"`
	LastTestNotifUnix    int64                       `json:"last_test_notif_unix,omitempty"`
}

// Handler implements extended Emby 4.10.2.0 compatibility routes for LiveTv, Sync, and Notifications.
type Handler struct {
	store        *sqlite.Store
	cfg          *config.Config
	auth         AuthFuncs
	liveTV       *livetv.Handler
	syncH        *syncapi.Handler
	dvrRec       *dvr.Recorder
	transcodeMgr *transcode.Manager
	libRepo      *sqlite.LibraryRepo
	userRepo     *sqlite.UserRepo
	notifTester  func(ctx context.Context, title, description string) error

	mu    sync.RWMutex
	state compatExtState
}

// SetNotificationTester wires a notification dispatch callback invoked by POST /Notifications/Services/Test.
func (h *Handler) SetNotificationTester(fn func(ctx context.Context, title, description string) error) {
	h.mu.Lock()
	h.notifTester = fn
	h.mu.Unlock()
}

// SetTranscodeManager wires the shared transcode Manager for Live TV recording HLS playback and subtitle extraction.
func (h *Handler) SetTranscodeManager(mgr *transcode.Manager) {
	h.mu.Lock()
	h.transcodeMgr = mgr
	h.mu.Unlock()
}

// SetLiveTV wires the primary Live TV handler for real channel stream proxying and unified state.
func (h *Handler) SetLiveTV(lt *livetv.Handler) {
	h.mu.Lock()
	h.liveTV = lt
	h.mu.Unlock()
}

// PruneOrphanedChannelMappings removes channel mappings referencing deleted listing providers
// or tuner channels from in-memory state and persists the updated state to SQLite if modified.
func (h *Handler) PruneOrphanedChannelMappings(ctx context.Context, activeProviders, activeChannels map[string]struct{}) {
	if h == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.state.ChannelMappings) == 0 {
		return
	}
	modified := false
	cleaned := make(map[string][]ChannelMapping, len(h.state.ChannelMappings))
	for provID, list := range h.state.ChannelMappings {
		if _, exists := activeProviders[provID]; !exists {
			modified = true
			continue
		}
		kept := make([]ChannelMapping, 0, len(list))
		for _, m := range list {
			tunerID := strings.TrimSpace(m.Name)
			provCh := strings.TrimSpace(m.Value)
			if tunerID == "" || provCh == "" {
				modified = true
				continue
			}
			if _, exists := activeChannels[tunerID]; !exists {
				modified = true
				continue
			}
			kept = append(kept, m)
		}
		if len(kept) == 0 {
			if len(list) > 0 {
				modified = true
			}
			continue
		}
		cleaned[provID] = kept
	}
	if !modified {
		return
	}
	h.state.ChannelMappings = cleaned
	_ = h.saveStateLocked(ctx)
}

// SetSyncHandler wires the primary Sync handler to unify in-memory and SQLite sync state.
func (h *Handler) SetSyncHandler(s *syncapi.Handler) {
	h.mu.Lock()
	h.syncH = s
	h.mu.Unlock()
}

func (h *Handler) getSyncHandler() *syncapi.Handler {
	h.mu.RLock()
	s := h.syncH
	h.mu.RUnlock()
	return s
}

// SetDVR wires the DVR recorder for recordings directory validation and timer cancellation.
func (h *Handler) SetDVR(r *dvr.Recorder) {
	h.mu.Lock()
	h.dvrRec = r
	h.mu.Unlock()
}

// SetLibraryRepo wires LibraryRepo for applying offline sync actions to user item data.
func (h *Handler) SetLibraryRepo(r *sqlite.LibraryRepo) {
	h.mu.Lock()
	h.libRepo = r
	h.mu.Unlock()
}

// SetUserRepo wires UserRepo for ownership/admin RBAC and playback position persistence.
func (h *Handler) SetUserRepo(r *sqlite.UserRepo) {
	h.mu.Lock()
	h.userRepo = r
	h.mu.Unlock()
}

func (h *Handler) callerPolicy(r *http.Request) (policy dto.UserPolicy, isAdmin bool, hasAuth bool) {
	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil && (ac.User != nil || ac.IsAPIKey) {
		if ac.IsAdmin() || ac.IsAPIKey {
			return sqlite.DefaultUserPolicy(true), true, true
		}
		if ur := h.resolveUserRepo(); ur != nil {
			u, err := ur.GetUserByID(r.Context(), guid.Normalize(ac.UserID()))
			if err != nil || u == nil {
				return dto.UserPolicy{}, false, true
			}
			if u.IsAdmin || u.Policy.IsAdministrator {
				return sqlite.DefaultUserPolicy(true), true, true
			}
			return u.Policy, false, true
		}
		if ac.User != nil {
			if ac.User.IsAdmin || ac.User.Policy.IsAdministrator {
				return sqlite.DefaultUserPolicy(true), true, true
			}
			return ac.User.Policy, false, true
		}
		return dto.UserPolicy{}, false, true
	}
	if h.auth.UserIDFromCtx != nil {
		if uid := guid.Normalize(h.auth.UserIDFromCtx(r.Context())); uid != "" {
			if ur := h.resolveUserRepo(); ur != nil {
				u, err := ur.GetUserByID(r.Context(), uid)
				if err != nil || u == nil {
					return dto.UserPolicy{}, false, true
				}
				if u.IsAdmin || u.Policy.IsAdministrator {
					return sqlite.DefaultUserPolicy(true), true, true
				}
				return u.Policy, false, true
			}
			return sqlite.DefaultUserPolicy(false), false, true
		}
	}
	return sqlite.DefaultUserPolicy(true), false, false
}

func (h *Handler) canPlayMedia(r *http.Request) bool {
	pol, isAdmin, hasAuth := h.callerPolicy(r)
	if !hasAuth || isAdmin {
		return true
	}
	return pol.EnableMediaPlayback
}

func (h *Handler) callerIdentity(r *http.Request) (userID string, isAdmin bool) {
	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil && (ac.User != nil || ac.IsAPIKey) {
		return guid.Normalize(ac.UserID()), ac.IsAdmin()
	}
	if h.auth.UserIDFromCtx != nil {
		uid := guid.Normalize(h.auth.UserIDFromCtx(r.Context()))
		if uid != "" {
			h.mu.RLock()
			ur := h.userRepo
			h.mu.RUnlock()
			if ur != nil {
				if u, err := ur.GetUserByID(r.Context(), uid); err == nil && u != nil {
					return uid, u.IsAdmin || u.Policy.IsAdministrator
				}
			}
			return uid, false
		}
	}
	return "", false
}

func (h *Handler) resolveUserRepo() *sqlite.UserRepo {
	h.mu.RLock()
	ur := h.userRepo
	h.mu.RUnlock()
	return ur
}

func (h *Handler) isNonAdminCaller(r *http.Request) bool {
	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil && (ac.User != nil || ac.IsAPIKey) {
		return !ac.IsAdmin()
	}
	if ur := h.resolveUserRepo(); ur != nil && h.auth.UserIDFromCtx != nil {
		if uid := guid.Normalize(h.auth.UserIDFromCtx(r.Context())); uid != "" {
			if u, err := ur.GetUserByID(r.Context(), uid); err == nil && u != nil {
				return !(u.IsAdmin || u.Policy.IsAdministrator)
			}
			return true
		}
	}
	return false
}

func (h *Handler) hasLiveTvAccess(r *http.Request) bool {
	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil && (ac.User != nil || ac.IsAPIKey) {
		if ac.IsAdmin() || ac.IsAPIKey {
			return true
		}
		if ur := h.resolveUserRepo(); ur != nil {
			u, err := ur.GetUserByID(r.Context(), guid.Normalize(ac.UserID()))
			if err != nil || u == nil {
				return false
			}
			return u.IsAdmin || u.Policy.IsAdministrator || u.Policy.EnableLiveTvAccess
		}
		return ac.User != nil && ac.User.Policy.EnableLiveTvAccess
	}
	if ur := h.resolveUserRepo(); ur != nil && h.auth.UserIDFromCtx != nil {
		if uid := guid.Normalize(h.auth.UserIDFromCtx(r.Context())); uid != "" {
			if u, err := ur.GetUserByID(r.Context(), uid); err == nil && u != nil {
				return u.IsAdmin || u.Policy.IsAdministrator || u.Policy.EnableLiveTvAccess
			}
			return false
		}
	}
	return true
}

func (h *Handler) requireLiveTvAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !h.hasLiveTvAccess(r) {
			if r.Method == http.MethodHead {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			httpx.Error(w, http.StatusForbidden, "Live TV access is disabled for this user")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) canDownloadContent(r *http.Request) bool {
	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil && (ac.User != nil || ac.IsAPIKey) {
		if ac.IsAdmin() || ac.IsAPIKey {
			return true
		}
		if ur := h.resolveUserRepo(); ur != nil {
			u, err := ur.GetUserByID(r.Context(), guid.Normalize(ac.UserID()))
			if err != nil || u == nil {
				return false
			}
			if u.IsAdmin || u.Policy.IsAdministrator {
				return true
			}
			return u.Policy.EnableContentDownloading
		}
		return ac.User != nil && ac.User.Policy.EnableContentDownloading
	}
	if ur := h.resolveUserRepo(); ur != nil && h.auth.UserIDFromCtx != nil {
		if uid := guid.Normalize(h.auth.UserIDFromCtx(r.Context())); uid != "" {
			if u, err := ur.GetUserByID(r.Context(), uid); err == nil && u != nil {
				if u.IsAdmin || u.Policy.IsAdministrator {
					return true
				}
				return u.Policy.EnableContentDownloading
			}
			return false
		}
	}
	return true
}

func (h *Handler) canAccessItemID(r *http.Request, itemID string) bool {
	normID := guid.Normalize(itemID)
	if normID == "" {
		return true
	}
	callerID, isAdmin := h.callerIdentity(r)
	if isAdmin || callerID == "" {
		return true
	}
	h.mu.RLock()
	libRepo := h.libRepo
	h.mu.RUnlock()
	if libRepo == nil {
		return true
	}
	rec, err := libRepo.GetItemRecordByID(r.Context(), normID)
	if errors.Is(err, sqlite.ErrNotFound) {
		return true
	}
	if err != nil || rec == nil {
		return false
	}

	var policy *dto.UserPolicy
	if ur := h.resolveUserRepo(); ur != nil {
		u, err := ur.GetUserByID(r.Context(), callerID)
		if err != nil || u == nil {
			return false
		}
		if u.IsAdmin || u.Policy.IsAdministrator {
			return true
		}
		p := u.Policy
		policy = &p
	} else if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil && ac.User != nil {
		p := ac.User.Policy
		policy = &p
	}
	if policy == nil {
		return false
	}

	if strings.EqualFold(rec.Type, "Playlist") {
		ownerID := guid.Normalize(rec.ParentID)
		if ownerID != "" && ownerID != callerID {
			var vis struct {
				IsPublic bool `json:"IsPublic"`
			}
			if !h.readConfigJSON(r.Context(), "item_visibility_"+guid.Normalize(rec.ID), &vis) || !vis.IsPublic {
				return false
			}
		}
	} else if rec.LibraryID != "" {
		if !libRepo.CanUserAccessLibrary(r.Context(), *policy, rec.LibraryID) {
			return false
		}
	}
	if blocked, bErr := libRepo.IsItemBlockedByPolicy(r.Context(), rec, *policy); bErr != nil || blocked {
		return false
	}
	return true
}

func (h *Handler) hasLiveTvManagement(r *http.Request) bool {
	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil && (ac.User != nil || ac.IsAPIKey) {
		h.mu.RLock()
		lt := h.liveTV
		h.mu.RUnlock()
		if lt != nil {
			return lt.HasLiveTvManagement(r.Context(), r)
		}
		return ac.IsAdmin() || (ac.User != nil && ac.User.Policy.EnableLiveTvManagement)
	}
	if h.auth.UserIDFromCtx != nil {
		if uid := guid.Normalize(h.auth.UserIDFromCtx(r.Context())); uid != "" {
			h.mu.RLock()
			ur := h.userRepo
			h.mu.RUnlock()
			if ur != nil {
				if u, err := ur.GetUserByID(r.Context(), uid); err == nil && u != nil {
					return u.IsAdmin || u.Policy.IsAdministrator || u.Policy.EnableLiveTvManagement
				}
			}
			return false
		}
	}
	return true
}

// New constructs a compatext Handler and loads persisted state from SQLite when store is non-nil.
func New(store *sqlite.Store, cfg *config.Config) *Handler {
	h := &Handler{
		store: store,
		cfg:   cfg,
		state: newEmptyState(),
	}
	h.loadState(context.Background())
	return h
}

func newEmptyState() compatExtState {
	return compatExtState{
		ChannelDisabled:  make(map[string]bool),
		ChannelSortIndex: make(map[string]int),
		ChannelMappings:  make(map[string][]ChannelMapping),
		TunerResetAt:     make(map[string]int64),
		RemovedJobItems:  make(map[string]bool),
	}
}

func (h *Handler) ensureMapsLocked() {
	if h.state.ChannelDisabled == nil {
		h.state.ChannelDisabled = make(map[string]bool)
	}
	if h.state.ChannelSortIndex == nil {
		h.state.ChannelSortIndex = make(map[string]int)
	}
	if h.state.ChannelMappings == nil {
		h.state.ChannelMappings = make(map[string][]ChannelMapping)
	}
	if h.state.TunerResetAt == nil {
		h.state.TunerResetAt = make(map[string]int64)
	}
	if h.state.RemovedJobItems == nil {
		h.state.RemovedJobItems = make(map[string]bool)
	}
}

func (h *Handler) serverID() string {
	if h.cfg != nil {
		return h.cfg.ServerID
	}
	return ""
}

// BuildRouter builds a chi.Router containing all 59 extended routes for /LiveTv, /Sync, and /Notifications.
func (h *Handler) BuildRouter(auth AuthFuncs) http.Handler {
	h.auth = auth
	wrapAuth := auth.RequireAuth
	if wrapAuth == nil {
		wrapAuth = passthrough
	}
	wrapAdmin := auth.RequireAdmin
	if wrapAdmin == nil {
		wrapAdmin = wrapAuth
	}

	r := chi.NewRouter()
	h.registerLiveTVRoutes(r, wrapAuth, wrapAdmin)
	h.registerSyncRoutes(r, wrapAuth)
	h.registerNotificationRoutes(r, wrapAuth)
	return r
}

func (h *Handler) loadState(ctx context.Context) {
	if h.store == nil {
		return
	}
	var raw string
	err := h.store.Reader.QueryRowContext(ctx,
		`SELECT value_json FROM server_config WHERE key = ?`, stateConfigKey,
	).Scan(&raw)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return
		}
		return
	}
	var st compatExtState
	if err := json.Unmarshal([]byte(raw), &st); err == nil {
		h.mu.Lock()
		h.state = st
		h.ensureMapsLocked()
		h.mu.Unlock()
	}
}

func cloneCompatExtState(st compatExtState) compatExtState {
	out := compatExtState{
		ChannelDisabled:   make(map[string]bool, len(st.ChannelDisabled)),
		ChannelSortIndex:  make(map[string]int, len(st.ChannelSortIndex)),
		ChannelMappings:   make(map[string][]ChannelMapping, len(st.ChannelMappings)),
		TunerResetAt:      make(map[string]int64, len(st.TunerResetAt)),
		RemovedJobItems:   make(map[string]bool, len(st.RemovedJobItems)),
		LastTestNotifUnix: st.LastTestNotifUnix,
	}
	for k, v := range st.ChannelDisabled {
		out.ChannelDisabled[k] = v
	}
	for k, v := range st.ChannelSortIndex {
		out.ChannelSortIndex[k] = v
	}
	for k, v := range st.ChannelMappings {
		out.ChannelMappings[k] = append([]ChannelMapping(nil), v...)
	}
	for k, v := range st.TunerResetAt {
		out.TunerResetAt[k] = v
	}
	for k, v := range st.RemovedJobItems {
		out.RemovedJobItems[k] = v
	}
	if st.OfflineActions != nil {
		out.OfflineActions = append([]OfflineAction(nil), st.OfflineActions...)
	}
	if st.NotificationServices != nil {
		out.NotificationServices = make([]NotificationServiceConfig, len(st.NotificationServices))
		for i, svc := range st.NotificationServices {
			cp := svc
			if svc.Events != nil {
				cp.Events = append([]string(nil), svc.Events...)
			}
			if svc.Options != nil {
				cp.Options = make(map[string]string, len(svc.Options))
				for k, v := range svc.Options {
					cp.Options[k] = v
				}
			}
			out.NotificationServices[i] = cp
		}
	}
	return out
}

func (h *Handler) saveStateLocked(ctx context.Context) error {
	if h.store == nil {
		return nil
	}
	raw, err := json.Marshal(h.state)
	if err != nil {
		return err
	}
	_, err = h.store.Writer.ExecContext(ctx, `
		INSERT INTO server_config (key, value_json, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			value_json = excluded.value_json,
			updated_at = excluded.updated_at`,
		stateConfigKey, string(raw), time.Now().UTC().Unix(),
	)
	return err
}

func (h *Handler) readConfigJSON(ctx context.Context, key string, dst any) bool {
	if h.store == nil {
		return false
	}
	var raw string
	err := h.store.Reader.QueryRowContext(ctx,
		`SELECT value_json FROM server_config WHERE key = ?`, key,
	).Scan(&raw)
	if err != nil {
		return false
	}
	return json.Unmarshal([]byte(raw), dst) == nil
}

func (h *Handler) writeConfigJSON(ctx context.Context, key string, src any) error {
	if h.store == nil {
		return nil
	}
	raw, err := json.Marshal(src)
	if err != nil {
		return err
	}
	_, err = h.store.Writer.ExecContext(ctx, `
		INSERT INTO server_config (key, value_json, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			value_json = excluded.value_json,
			updated_at = excluded.updated_at`,
		key, string(raw), time.Now().UTC().Unix(),
	)
	return err
}

func paginate[T any](items []T, startIndex, limit int) ([]T, int) {
	total := len(items)
	if startIndex < 0 {
		startIndex = 0
	}
	if startIndex >= total {
		return []T{}, total
	}
	end := total
	if limit > 0 && startIndex+limit < end {
		end = startIndex + limit
	}
	out := make([]T, end-startIndex)
	copy(out, items[startIndex:end])
	return out, total
}

func passthrough(next http.Handler) http.Handler { return next }
