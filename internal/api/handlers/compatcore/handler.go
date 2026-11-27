package compatcore

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/api/ws"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/core/session"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

// AuthFuncs decouples route registration from the concrete middleware package.
type AuthFuncs struct {
	RequireAuth   func(http.Handler) http.Handler
	RequireAdmin  func(http.Handler) http.Handler
	UserIDFromCtx func(context.Context) string
}

const (
	forgotPinMaxAttempts        = 5
	forgotPasswordRateLimit     = 5
	forgotPasswordRateWindow    = time.Minute
	forgotPasswordMaxPinFiles   = 10
	forgotPinsMaxEntries        = 256
	authByIDRateLimit           = 30
	authByIDRateWindow          = time.Minute
	authByIDLoopbackRateLimit   = 60
	authByIDLoopbackRateWindow  = 5 * time.Minute
	maxRateLimitBuckets         = 4096
	maxCameraUploadsEntries     = 500
	maxCameraUploadBytes        = 512 << 20 // 512 MiB
	maxHomeSectionsEntries      = 500
	maxSearchedItemsEntries     = 50
	maxPartyMessageBytes        = 64 << 10
	maxKVJSONBodyBytes          = 64 << 10
	maxSessionCommandBodyBytes  = 64 << 10
	maxTypedSettingKeyLen       = 128
	maxUserTypedSettingsKeys    = 100
	maxDirectoryContentsEntries = 2000
)

type forgotPinEntry struct {
	UserID    string
	UserName  string
	Pin       string
	PinFile   string
	ExpiresAt time.Time
}

// WSSender pushes Emby WebSocket messages to connected user devices.
type WSSender interface {
	SendToUserDevice(userID, deviceID string, msg ws.Message)
}

// Handler implements the Emby 4.10.2.0 compatibility routes for users, sessions,
// devices, auth, system, plugins, and operational endpoints.
type Handler struct {
	cfg      *config.Config
	store    *sqlite.Store
	userRepo *sqlite.UserRepo
	libRepo  *sqlite.LibraryRepo
	sessions *session.Manager
	registry *plugins.Registry
	authSvc  *auth.Service
	wsHub    WSSender
	auth     AuthFuncs

	mu                    sync.RWMutex
	memKV                 map[string]string
	forgotPins            map[string]forgotPinEntry
	forgotRates           map[string][]time.Time
	forgotPinRates        map[string][]time.Time
	authByIDRates         map[string][]time.Time
	loopbackAuthByIDRates map[string][]time.Time
	restartRequested      bool
	shutdownRequested     bool
	onRestart             func()
	onShutdown            func()
	taskStopper           func(id string) bool
	packageCanceller      func(id string) bool
	pluginUninstaller     func(ctx context.Context, id string) error
	onUserDeleted         func(ctx context.Context, userID string)
	restoreHandler        http.HandlerFunc
	partyLeaveHandler     http.HandlerFunc
	partyMsgHandler       http.HandlerFunc
}

// New constructs a compatcore Handler.
func New(
	cfg *config.Config,
	store *sqlite.Store,
	userRepo *sqlite.UserRepo,
	libRepo *sqlite.LibraryRepo,
	sessions *session.Manager,
	registry *plugins.Registry,
) *Handler {
	return &Handler{
		cfg:                   cfg,
		store:                 store,
		userRepo:              userRepo,
		libRepo:               libRepo,
		sessions:              sessions,
		registry:              registry,
		memKV:                 make(map[string]string),
		forgotPins:            make(map[string]forgotPinEntry),
		forgotRates:           make(map[string][]time.Time),
		forgotPinRates:        make(map[string][]time.Time),
		authByIDRates:         make(map[string][]time.Time),
		loopbackAuthByIDRates: make(map[string][]time.Time),
	}
}

// SetWSHub attaches a WebSocket hub for forwarding remote control commands to target devices.
func (h *Handler) SetWSHub(hub WSSender) {
	h.mu.Lock()
	h.wsHub = hub
	h.mu.Unlock()
}

func (h *Handler) getWSHub() WSSender {
	h.mu.RLock()
	hub := h.wsHub
	h.mu.RUnlock()
	return hub
}

// SetAuthService attaches the authentication service for password reset flows.
func (h *Handler) SetAuthService(a *auth.Service) {
	h.mu.Lock()
	h.authSvc = a
	h.mu.Unlock()
}

// SetLifecycleHooks wires callbacks invoked asynchronously on /System/Restart and /System/Shutdown.
func (h *Handler) SetLifecycleHooks(onRestart, onShutdown func()) {
	h.mu.Lock()
	h.onRestart = onRestart
	h.onShutdown = onShutdown
	h.mu.Unlock()
}

// SetTaskStopper wires a callback to stop a running scheduled task by ID.
func (h *Handler) SetTaskStopper(stopFn func(id string) bool) {
	h.mu.Lock()
	h.taskStopper = stopFn
	h.mu.Unlock()
}

// SetPackageCanceller wires a callback to cancel an active/queued package installation by ID.
func (h *Handler) SetPackageCanceller(cancelFn func(id string) bool) {
	h.mu.Lock()
	h.packageCanceller = cancelFn
	h.mu.Unlock()
}

// SetPluginUninstaller wires a callback to uninstall/disable a plugin by ID on POST /Plugins/{Id}/Delete and /Uninstall.
func (h *Handler) SetPluginUninstaller(uninstallFn func(ctx context.Context, id string) error) {
	h.mu.Lock()
	h.pluginUninstaller = uninstallFn
	h.mu.Unlock()
}

// SetOnUserDeleted registers a cleanup callback invoked when a user account is deleted.
func (h *Handler) SetOnUserDeleted(fn func(ctx context.Context, userID string)) {
	h.mu.Lock()
	h.onUserDeleted = fn
	h.mu.Unlock()
}

// SetRestoreHandler delegates POST /BackupRestore/RestoreData to the real ops backup restorer.
func (h *Handler) SetRestoreHandler(fn http.HandlerFunc) {
	h.mu.Lock()
	h.restoreHandler = fn
	h.mu.Unlock()
}

// SetPartyHandlers delegates /Parties/Leave and /Parties/Messages to the canonical ops party handlers.
func (h *Handler) SetPartyHandlers(leaveFn, messagesFn http.HandlerFunc) {
	h.mu.Lock()
	h.partyLeaveHandler = leaveFn
	h.partyMsgHandler = messagesFn
	h.mu.Unlock()
}

// BuildRouter builds a chi.Router containing all 16 compatibility prefixes.
func (h *Handler) BuildRouter(auth AuthFuncs) http.Handler {
	h.auth = auth

	wrapAuth := auth.RequireAuth
	if wrapAuth == nil {
		wrapAuth = passthroughMiddleware
	}
	wrapAdmin := auth.RequireAdmin
	if wrapAdmin == nil {
		wrapAdmin = wrapAuth
	}

	r := chi.NewRouter()

	// Public / unauthenticated endpoints
	r.Post("/Users/ForgotPassword", h.forgotPassword)
	r.Post("/Users/ForgotPassword/Pin", h.forgotPasswordPin)
	r.Post("/Users/{Id}/Authenticate", h.authenticateUserByID)
	r.Get("/Plugins/{Id}/Thumb", h.getPluginThumb)
	r.Get("/Users/{Id}/Images/{Type}", h.getUserImage)
	r.Get("/Users/{Id}/Images/{Type}/{Index}", h.getUserImage)
	r.Head("/Users/{Id}/Images/{Type}", h.headUserImage)
	r.Head("/Users/{Id}/Images/{Type}/{Index}", h.headUserImage)

	// Authenticated endpoints
	r.Group(func(pr chi.Router) {
		pr.Use(wrapAuth)

		// /Connect
		pr.Get("/Connect/Exchange", h.connectExchange)

		// /Devices
		pr.Get("/Devices/Info", h.getDeviceInfo)
		pr.Get("/Devices/Options", h.getDeviceOptions)
		pr.Post("/Devices/Options", h.postDeviceOptions)
		pr.Get("/Devices/CameraUploads", h.getDeviceCameraUploads)
		pr.Post("/Devices/CameraUploads", h.postDeviceCameraUploads)

		// /Parties (delegated to ops when wired, fallback when standalone)
		pr.Post("/Parties/Leave", h.postPartiesLeave)
		pr.Post("/Parties/Messages", h.postPartiesMessages)

		// /Playback
		pr.Get("/Playback/BitrateTest", h.getPlaybackBitrateTest)

		// /Plugins
		pr.Get("/Plugins/RegistrationRecords/{Name}", h.getPluginRegistrationRecord)

		// /Sessions
		pr.Get("/Sessions/PlayQueue", h.getSessionPlayQueue)
		pr.Post("/Sessions/Playing/Ping", h.postSessionPlayingPing)
		pr.Post("/Sessions/{Id}/Viewing", h.postSessionViewing)
		pr.Post("/Sessions/{Id}/Playing", h.postSessionPlaying)
		pr.Post("/Sessions/{Id}/Playing/{Command}", h.postSessionPlayingCommand)
		pr.Post("/Sessions/{Id}/System/{Command}", h.postSessionSystemCommand)
		pr.Post("/Sessions/{Id}/Command/{Command}", h.postSessionCommand)
		pr.Post("/Sessions/{Id}/Command", h.postSessionCommand)
		pr.Post("/Sessions/{Id}/Message", h.postSessionMessage)
		pr.Post("/Sessions/{Id}/Users/{UserId}", h.postSessionAddUser)
		pr.Delete("/Sessions/{Id}/Users/{UserId}", h.deleteSessionUser)
		pr.Post("/Sessions/{Id}/Users/{UserId}/Delete", h.deleteSessionUser)

		// /System
		pr.Get("/System/Endpoint", h.getSystemEndpoint)
		pr.Get("/System/ReleaseNotes/Versions", h.getSystemReleaseNotesVersions)
		pr.Get("/System/ReleaseNotes", h.getSystemReleaseNotes)

		// /UserSettings
		pr.Get("/UserSettings/{UserId}", h.getUserSettings)
		pr.Post("/UserSettings/{UserId}", h.postUserSettings)
		pr.Post("/UserSettings/{UserId}/Partial", h.postUserSettingsPartial)

		// /Users
		pr.Get("/Users/Query", h.queryUsers)
		pr.Get("/Users/Prefix", h.getUserPrefixes)
		pr.Post("/Users/{Id}", h.updateUser)
		pr.Post("/Users/{Id}/Configuration/Partial", h.updateUserConfigurationPartial)
		pr.Delete("/Users/{Id}/TrackSelections/{TrackType}", h.deleteUserTrackSelections)
		pr.Post("/Users/{Id}/TrackSelections/{TrackType}/Delete", h.deleteUserTrackSelections)
		pr.Delete("/Users/{Id}/RecentlySearched", h.deleteUserRecentlySearched)
		pr.Post("/Users/{Id}/RecentlySearched/Delete", h.deleteUserRecentlySearched)
		pr.Get("/Users/{UserId}/TypedSettings/{Key}", h.getUserTypedSettings)
		pr.Post("/Users/{UserId}/TypedSettings/{Key}", h.postUserTypedSettings)
		pr.Get("/Users/{UserId}/HomeSections", h.getUserHomeSections)
		pr.Post("/Users/{UserId}/HomeSections", h.postUserHomeSections)
		pr.Post("/Users/{UserId}/HomeSections/Delete", h.deleteUserHomeSections)
		pr.Post("/Users/{UserId}/HomeSections/Move", h.moveUserHomeSection)
		pr.Get("/Users/{UserId}/Sections/{SectionId}/Items", h.getUserSectionItems)
		pr.Get("/Users/{UserId}/Suggestions", h.getUserSuggestions)
		pr.Get("/Users/{UserId}/GroupingOptions", h.getUserGroupingOptions)
		pr.Get("/Users/{UserId}/Items/Root", h.getUserRootItem)
		pr.Get("/Users/{UserId}/Items/{Id}/Intros", h.getUserItemIntros)
		pr.Get("/Users/{UserId}/Items/{Id}/LocalTrailers", h.getUserItemLocalTrailers)
		pr.Get("/Users/{UserId}/Items/{Id}/SpecialFeatures", h.getUserItemSpecialFeatures)
		pr.Post("/Users/{UserId}/SearchedItems/", h.postUserSearchedItems)
		pr.Post("/Users/{UserId}/SearchedItems", h.postUserSearchedItems)
		pr.Post("/Users/{UserId}/FavoriteItems/{Id}/Delete", h.deleteUserFavoriteItem)
		pr.Post("/Users/{UserId}/Items/{Id}/Rating", h.postUserItemRating)
		pr.Delete("/Users/{UserId}/Items/{Id}/Rating", h.deleteUserItemRating)
		pr.Post("/Users/{UserId}/Items/{Id}/Rating/Delete", h.deleteUserItemRating)
		pr.Post("/Users/{UserId}/Items/{Id}/HideFromResume", h.postUserItemHideFromResume)
		pr.Post("/Users/{UserId}/Items/{ItemId}/UserData", h.postUserItemUserData)
		pr.Post("/Users/{UserId}/PlayedItems/{Id}/Delete", h.deleteUserPlayedItem)
		pr.Post("/Users/{UserId}/PlayingItems/{Id}", h.postUserPlayingItemStart)
		pr.Post("/Users/{UserId}/PlayingItems/{Id}/Progress", h.postUserPlayingItemProgress)
		pr.Delete("/Users/{UserId}/PlayingItems/{Id}", h.deleteUserPlayingItem)
		pr.Post("/Users/{UserId}/PlayingItems/{Id}/Delete", h.deleteUserPlayingItem)
		pr.Post("/Users/{Id}/Images/{Type}", h.postUserImage)
		pr.Post("/Users/{Id}/Images/{Type}/{Index}", h.postUserImage)
		pr.Delete("/Users/{Id}/Images/{Type}", h.deleteUserImage)
		pr.Delete("/Users/{Id}/Images/{Type}/{Index}", h.deleteUserImage)
		pr.Post("/Users/{Id}/Images/{Type}/Delete", h.deleteUserImage)
		pr.Post("/Users/{Id}/Images/{Type}/{Index}/Delete", h.deleteUserImage)
	})

	// Admin endpoints
	r.Group(func(ar chi.Router) {
		ar.Use(wrapAdmin)

		// /Appstore
		ar.Post("/Appstore/Register", h.postAppstoreRegister)

		// /Auth
		ar.Get("/Auth/Providers", h.getAuthProviders)
		ar.Post("/Auth/Keys/{Key}/Delete", h.deleteAuthKey)

		// /BackupRestore
		ar.Post("/BackupRestore/RestoreData", h.postBackupRestoreData)

		// /Devices
		ar.Post("/Devices/Delete", h.postDevicesDelete)

		// /Environment
		ar.Post("/Environment/DirectoryContents", h.postEnvironmentDirectoryContents)
		ar.Get("/Environment/NetworkDevices", h.getEnvironmentNetworkDevices)

		// /Packages
		ar.Post("/Packages/Installing/{Id}/Delete", h.deleteInstallingPackage)

		// /Plugins
		ar.Post("/Plugins/{Id}/Delete", h.deletePlugin)
		ar.Post("/Plugins/{Id}/Uninstall", h.deletePlugin)
		ar.Get("/Plugins/SecurityInfo", h.getPluginSecurityInfo)
		ar.Post("/Plugins/SecurityInfo", h.postPluginSecurityInfo)

		// /ScheduledTasks
		ar.Post("/ScheduledTasks/Running/{Id}/Delete", h.deleteRunningScheduledTask)

		// /System
		ar.Get("/System/Info/Storage", h.getSystemStorageInfo)
		ar.Get("/System/WakeOnLanInfo", h.getSystemWakeOnLanInfo)
		ar.Post("/System/Restart", h.postSystemRestart)
		ar.Post("/System/Shutdown", h.postSystemShutdown)

		// /Users
		ar.Post("/Users/{Id}/Delete", h.deleteUser)
		ar.Post("/Users/{UserId}/CopyData", h.copyUserData)
		ar.Post("/Users/{Id}/Connect/Link", h.postUserConnectLink)
		ar.Delete("/Users/{Id}/Connect/Link", h.deleteUserConnectLink)
		ar.Post("/Users/{Id}/Connect/Link/Delete", h.deleteUserConnectLink)

		// /Webhooks
		ar.Get("/Webhooks/Sample", h.getWebhooksSample)
	})

	return r
}

func passthroughMiddleware(next http.Handler) http.Handler {
	return next
}

func (h *Handler) serverID() string {
	if h.cfg != nil {
		if sid := h.cfg.Snapshot().ServerID; sid != "" {
			return sid
		}
	}
	return "ambra-server"
}

func (h *Handler) resolveUserID(r *http.Request, param string) string {
	if param != "" {
		if v := strings.TrimSpace(chi.URLParam(r, param)); v != "" {
			return guid.Normalize(v)
		}
	}
	if h.auth.UserIDFromCtx != nil {
		if v := strings.TrimSpace(h.auth.UserIDFromCtx(r.Context())); v != "" {
			return guid.Normalize(v)
		}
	}
	return ""
}

func decodeOptionalJSONLimit(r *http.Request, dst any, maxBytes int64) error {
	if r.Body == nil {
		return nil
	}
	err := httpx.DecodeJSONLimit(r, dst, maxBytes)
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

func decodeOptionalJSON(r *http.Request, dst any) error {
	return decodeOptionalJSONLimit(r, dst, httpx.DefaultMaxJSONBodyBytes)
}

func (h *Handler) loadKV(ctx context.Context, key string, dst any) (bool, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.loadKVLocked(ctx, key, dst)
}

func (h *Handler) loadKVLocked(ctx context.Context, key string, dst any) (bool, error) {
	if h.userRepo != nil {
		raw, err := h.userRepo.GetServerConfig(ctx, key)
		if err == nil {
			if err := json.Unmarshal([]byte(raw), dst); err != nil {
				return false, err
			}
			return true, nil
		}
		if !errors.Is(err, sqlite.ErrNotFound) {
			return false, err
		}
		return false, nil
	}
	if h.memKV == nil {
		return false, nil
	}
	raw, ok := h.memKV[key]
	if !ok {
		return false, nil
	}
	if err := json.Unmarshal([]byte(raw), dst); err != nil {
		return false, err
	}
	return true, nil
}

func (h *Handler) saveKV(ctx context.Context, key string, val any) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.saveKVLocked(ctx, key, val)
}

func (h *Handler) saveKVLocked(ctx context.Context, key string, val any) error {
	raw, err := json.Marshal(val)
	if err != nil {
		return err
	}
	if h.userRepo != nil {
		return h.userRepo.SetServerConfig(ctx, key, string(raw))
	}
	if h.memKV == nil {
		h.memKV = make(map[string]string)
	}
	h.memKV[key] = string(raw)
	return nil
}

func (h *Handler) kvStore() *sqlite.Store {
	if h.store != nil {
		return h.store
	}
	if h.userRepo != nil {
		return h.userRepo.Store()
	}
	return nil
}

func (h *Handler) deleteKV(ctx context.Context, key string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.deleteKVLocked(ctx, key)
}

func (h *Handler) deleteKVLocked(ctx context.Context, key string) error {
	if st := h.kvStore(); st != nil && st.Writer != nil {
		_, err := st.Writer.ExecContext(ctx, `DELETE FROM server_config WHERE key = ?`, key)
		return err
	}
	delete(h.memKV, key)
	return nil
}

// writeJSONError is a thin wrapper ensuring JSON Content-Type even for HEAD responses.
func writeJSONError(w http.ResponseWriter, r *http.Request, status int, msg string) {
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		return
	}
	httpx.Error(w, status, msg)
}
