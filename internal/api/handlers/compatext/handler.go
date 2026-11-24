package compatext

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ambra-media/ambra/internal/api/handlers/livetv"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/dvr"
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
	Date           string `json:"Date,omitempty"`
	ItemId         string `json:"ItemId,omitempty"`
	ServerId       string `json:"ServerId,omitempty"`
	UserId         string `json:"UserId,omitempty"`
	Type           string `json:"Type,omitempty"`
	IsFavorite     *bool  `json:"IsFavorite,omitempty"`
	Played         *bool  `json:"Played,omitempty"`
	PositionTicks  int64  `json:"PositionTicks,omitempty"`
	RecordedAtUnix int64  `json:"RecordedAtUnix,omitempty"`
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
	store  *sqlite.Store
	cfg    *config.Config
	auth   AuthFuncs
	liveTV *livetv.Handler
	dvrRec *dvr.Recorder

	mu    sync.RWMutex
	state compatExtState
}

// SetLiveTV wires the primary Live TV handler for real channel stream proxying.
func (h *Handler) SetLiveTV(lt *livetv.Handler) {
	h.mu.Lock()
	h.liveTV = lt
	h.mu.Unlock()
}

// SetDVR wires the DVR recorder for recordings directory validation and timer cancellation.
func (h *Handler) SetDVR(r *dvr.Recorder) {
	h.mu.Lock()
	h.dvrRec = r
	h.mu.Unlock()
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

func (h *Handler) saveStateLocked(ctx context.Context) {
	if h.store == nil {
		return
	}
	raw, err := json.Marshal(h.state)
	if err != nil {
		return
	}
	_, _ = h.store.Writer.ExecContext(ctx, `
		INSERT INTO server_config (key, value_json, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			value_json = excluded.value_json,
			updated_at = excluded.updated_at`,
		stateConfigKey, string(raw), time.Now().UTC().Unix(),
	)
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

func (h *Handler) writeConfigJSON(ctx context.Context, key string, src any) {
	if h.store == nil {
		return
	}
	raw, err := json.Marshal(src)
	if err != nil {
		return
	}
	_, _ = h.store.Writer.ExecContext(ctx, `
		INSERT INTO server_config (key, value_json, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			value_json = excluded.value_json,
			updated_at = excluded.updated_at`,
		key, string(raw), time.Now().UTC().Unix(),
	)
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
