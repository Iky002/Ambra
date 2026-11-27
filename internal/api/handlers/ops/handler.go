package ops

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/netip"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/api/ws"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/core/session"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/pkg/netutil"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

const (
	stateConfigKey           = "ops:state"
	systemExtraConfigKey     = "system:config_extra"
	startupRateLimit         = 30
	startupLoopbackRateLimit = 180
	startupRateWindow        = time.Minute
	startupRateMaxBuckets    = 2048
)

// AuthFuncs decouples route registration from the concrete middleware package.
type AuthFuncs struct {
	RequireAuth   func(http.Handler) http.Handler
	RequireAdmin  func(http.Handler) http.Handler
	UserIDFromCtx func(context.Context) string
}

type opsState struct {
	Startup       StartupConfiguration `json:"startup"`
	StartupUser   StartupUserState     `json:"startup_user"`
	RemoteAccess  RemoteAccessConfig   `json:"remote_access"`
	Installations []InstallationInfo   `json:"installations"`
	Webhooks      []WebhookConfig      `json:"webhooks"`
	Parties       []Party              `json:"parties"`
	ConnectLinks  []ConnectPending     `json:"connect_links"`
}

type ConnectPending struct {
	Id            string    `json:"Id"`
	ConnectUserId string    `json:"ConnectUserId"`
	UserName      string    `json:"UserName"`
	Status        string    `json:"Status"`
	DateCreated   time.Time `json:"DateCreated"`
}

type ConnectExchangeResult struct {
	LocalUserId string `json:"LocalUserId"`
	AccessToken string `json:"AccessToken"`
}

type UIView struct {
	PageId   string           `json:"PageId"`
	ViewType string           `json:"ViewType"`
	Caption  string           `json:"Caption"`
	Controls []map[string]any `json:"Controls"`
}

type Handler struct {
	store             *sqlite.Store
	cfg               *config.Config
	userRepo          *sqlite.UserRepo
	bus               *eventbus.Bus
	wsHub             *ws.Hub
	authSvc           *auth.Service
	sessionMgr        *session.Manager
	itemAccessChecker func(ctx context.Context, user *sqlite.UserRecord, itemID string) bool
	httpClient        *http.Client
	customHTTPClient  bool
	auth              AuthFuncs

	backupMu      sync.Mutex
	setupMu       sync.Mutex
	startupRateMu sync.Mutex
	startupRates  map[string][]time.Time
	webhookMu     sync.Mutex
	webhookClosed bool
	webhookCtx    context.Context
	webhookCancel context.CancelFunc
	webhookWG     sync.WaitGroup
	webhookCh     chan webhookDispatchJob

	mu             sync.RWMutex
	state          opsState
	partyJoinFails map[string]*partyFailWindow
}

func New(store *sqlite.Store, cfg *config.Config, userRepo *sqlite.UserRepo, bus *eventbus.Bus) *Handler {
	h := &Handler{
		store:          store,
		cfg:            cfg,
		userRepo:       userRepo,
		bus:            bus,
		httpClient:     netutil.NewSafeHTTPClientNoLoopback(10*time.Second, true),
		startupRates:   make(map[string][]time.Time),
		partyJoinFails: make(map[string]*partyFailWindow),
		state: opsState{
			Startup: StartupConfiguration{
				ServerName:                "Ambra Media Server",
				UICulture:                 "zh-CN",
				MetadataCountryCode:       "CN",
				PreferredMetadataLanguage: "zh-CN",
			},
			RemoteAccess: RemoteAccessConfig{
				EnableRemoteAccess:         true,
				EnableAutomaticPortMapping: false,
			},
		},
	}
	if cfg != nil {
		snap := cfg.Snapshot()
		if snap.ServerName != "" {
			h.state.Startup.ServerName = snap.ServerName
		}
		if snap.TMDBLanguage != "" {
			h.state.Startup.PreferredMetadataLanguage = snap.TMDBLanguage
		}
		h.state.RemoteAccess.EnableRemoteAccess = snap.IsGlobalRemoteAccessEnabled()
	}
	h.loadState(context.Background())
	if bus != nil {
		h.subscribeWebhooks(bus)
	}
	return h
}

// SetWSHub attaches the WebSocket hub for real-time Watch-Together party playstate & chat pushes.
func (h *Handler) SetWSHub(hub *ws.Hub) {
	h.mu.Lock()
	h.wsHub = hub
	h.mu.Unlock()
}

func (h *Handler) getWSHub() *ws.Hub {
	h.mu.RLock()
	hub := h.wsHub
	h.mu.RUnlock()
	return hub
}

// SetAuthService attaches the AuthService for token revocation and cache invalidation on startup user updates.
func (h *Handler) SetAuthService(authSvc *auth.Service) {
	h.mu.Lock()
	h.authSvc = authSvc
	h.mu.Unlock()
}

func (h *Handler) getAuthService() *auth.Service {
	h.mu.RLock()
	svc := h.authSvc
	h.mu.RUnlock()
	return svc
}

// SetSessionManager attaches the SessionManager for session cleanup on startup user password updates.
func (h *Handler) SetSessionManager(sm *session.Manager) {
	h.mu.Lock()
	h.sessionMgr = sm
	h.mu.Unlock()
}

func (h *Handler) getSessionManager() *session.Manager {
	h.mu.RLock()
	sm := h.sessionMgr
	h.mu.RUnlock()
	return sm
}

// SetAuthDeps Convenience setter attaching both AuthService and SessionManager.
func (h *Handler) SetAuthDeps(authSvc *auth.Service, sm *session.Manager) {
	h.mu.Lock()
	h.authSvc = authSvc
	h.sessionMgr = sm
	h.mu.Unlock()
}

// SetItemAccessChecker configures an optional RBAC and parental-control item access callback for Watch Parties.
func (h *Handler) SetItemAccessChecker(fn func(ctx context.Context, user *sqlite.UserRecord, itemID string) bool) {
	h.mu.Lock()
	h.itemAccessChecker = fn
	h.mu.Unlock()
}

func (h *Handler) getItemAccessChecker() func(ctx context.Context, user *sqlite.UserRecord, itemID string) bool {
	h.mu.RLock()
	fn := h.itemAccessChecker
	h.mu.RUnlock()
	return fn
}

// SetHTTPClient overrides the HTTP client used for webhook delivery (primarily for testing).
func (h *Handler) SetHTTPClient(client *http.Client) {
	if client == nil {
		return
	}
	h.mu.Lock()
	h.httpClient = client
	h.customHTTPClient = true
	h.mu.Unlock()
}

func (h *Handler) isCustomHTTPClient() bool {
	h.mu.RLock()
	custom := h.customHTTPClient
	h.mu.RUnlock()
	return custom
}

func (h *Handler) getHTTPClient() *http.Client {
	h.mu.RLock()
	client := h.httpClient
	h.mu.RUnlock()
	if client == nil {
		return netutil.NewSafeHTTPClientNoLoopback(10*time.Second, true)
	}
	return client
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux, auth AuthFuncs) {
	h.auth = auth
	wrapAuth := auth.RequireAuth
	if wrapAuth == nil {
		wrapAuth = passthrough
	}
	wrapAdmin := auth.RequireAdmin
	if wrapAdmin == nil {
		wrapAdmin = wrapAuth
	}

	handleAuth := func(pattern string, fn http.HandlerFunc) {
		mux.Handle(pattern, wrapAuth(fn))
	}
	handleAdmin := func(pattern string, fn http.HandlerFunc) {
		mux.Handle(pattern, wrapAdmin(fn))
	}
	handleSetupOrAdmin := func(pattern string, fn http.HandlerFunc) {
		mux.Handle(pattern, h.requireSetupOrAdmin(wrapAdmin)(fn))
	}

	// Startup Wizard (accessible unauthenticated during initial setup; requires admin once a user exists)
	handleSetupOrAdmin("GET /Startup/Configuration", h.handleGetStartupConfig)
	handleSetupOrAdmin("POST /Startup/Configuration", h.handleUpdateStartupConfig)
	handleSetupOrAdmin("GET /Startup/User", h.handleGetStartupUser)
	handleSetupOrAdmin("POST /Startup/User", h.handleUpdateStartupUser)
	handleSetupOrAdmin("POST /Startup/RemoteAccess", h.handleUpdateRemoteAccess)
	handleSetupOrAdmin("POST /Startup/Complete", h.handleStartupComplete)

	// Packages
	handleAuth("GET /Packages", h.handleListPackages)
	handleAdmin("GET /Packages/Updates", h.handlePackageUpdates)
	handleAuth("GET /Packages/{Name}", h.handleGetPackage)
	handleAdmin("POST /Packages/Installed/{Name}", h.handleInstallPackage)
	handleAdmin("DELETE /Packages/Installing/{Id}", h.handleCancelInstall)

	// Webhooks
	handleAdmin("GET /Webhooks", h.handleListWebhooks)
	handleAdmin("POST /Webhooks", h.handleCreateWebhook)
	handleAdmin("DELETE /Webhooks/{Id}", h.handleDeleteWebhook)
	handleAdmin("POST /Webhooks/Test", h.handleTestWebhook)

	// BackupRestore
	handleAdmin("GET /BackupRestore/BackupInfo", h.handleBackupInfo)
	handleAdmin("POST /BackupRestore/Backup", h.handleCreateBackup)
	handleAdmin("POST /BackupRestore/Restore", h.handleRestoreBackup)

	// OpenAPI / Swagger / Robots (public)
	mux.HandleFunc("GET /openapi.json", h.handleOpenAPIJSON)
	mux.HandleFunc("GET /swagger.json", h.handleOpenAPIJSON)
	mux.HandleFunc("GET /openapi", h.handleOpenAPIUI)
	mux.HandleFunc("GET /swagger", h.handleOpenAPIUI)
	mux.HandleFunc("GET /robots.txt", h.handleRobotsTxt)

	// GenericUI (admin-only)
	handleAdmin("GET /UI/View", h.handleUIView)
	handleAdmin("POST /UI/Command", h.handleUICommand)

	// Parties (register static sub-routes before /Parties/{Id})
	handleAuth("GET /Parties", h.handleListParties)
	handleAuth("POST /Parties", h.handleCreateParty)
	handleAuth("GET /Parties/Info", h.handleGetCurrentPartyInfo)
	handleAuth("GET /Parties/Messages", h.handleListPartyMessages)
	handleAuth("POST /Parties/Messages", h.handlePostPartyMessage)
	handleAuth("POST /Parties/Leave", h.handleLeaveCurrentParty)
	handleAuth("GET /Parties/{Id}", h.handleGetParty)
	handleAuth("DELETE /Parties/{Id}", h.handleDeleteParty)
	handleAuth("POST /Parties/{Id}/Join", h.handleJoinParty)
	handleAuth("POST /Parties/{Id}/Leave", h.handleLeaveParty)
	handleAuth("POST /Parties/{Id}/State", h.handleUpdatePartyState)

	// Connect
	handleAdmin("GET /Connect/Pending", h.handleConnectPending)
	handleAuth("POST /Connect/Exchange", h.handleConnectExchange)
}

func (h *Handler) checkStartupIPRateLimit(ip string, now time.Time) bool {
	s := strings.TrimSpace(ip)
	if s == "" {
		return true
	}
	limit := startupRateLimit
	if addr, err := netip.ParseAddr(s); err == nil {
		addr = addr.WithZone("").Unmap()
		if addr.IsLoopback() {
			limit = startupLoopbackRateLimit
		}
		if addr.Is6() {
			s = netip.PrefixFrom(addr, 64).Masked().String()
		} else {
			s = addr.String()
		}
	}
	h.startupRateMu.Lock()
	defer h.startupRateMu.Unlock()
	if h.startupRates == nil {
		h.startupRates = make(map[string][]time.Time)
	}
	cutoff := now.Add(-startupRateWindow)
	for k, ts := range h.startupRates {
		kept := ts[:0]
		for _, t := range ts {
			if t.After(cutoff) {
				kept = append(kept, t)
			}
		}
		if len(kept) == 0 {
			delete(h.startupRates, k)
		} else {
			h.startupRates[k] = kept
		}
	}
	if len(h.startupRates[s]) >= limit {
		return false
	}
	if _, ok := h.startupRates[s]; !ok && len(h.startupRates) >= startupRateMaxBuckets {
		for k := range h.startupRates {
			delete(h.startupRates, k)
			break
		}
	}
	h.startupRates[s] = append(h.startupRates[s], now)
	return true
}

func (h *Handler) requireSetupOrAdmin(wrapAdmin func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		adminWrapped := wrapAdmin(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !h.checkStartupIPRateLimit(netutil.ClientIP(r), time.Now().UTC()) {
				httpx.Error(w, http.StatusTooManyRequests, "too many startup wizard requests")
				return
			}
			wizardCompleted := h.cfg != nil && h.cfg.Snapshot().IsStartupWizardCompleted
			if wizardCompleted {
				adminWrapped.ServeHTTP(w, r)
				return
			}
			if h.userRepo == nil {
				next.ServeHTTP(w, r)
				return
			}
			count, err := h.userRepo.CountUsers(r.Context())
			if err == nil && count == 0 {
				next.ServeHTTP(w, r)
				return
			}
			adminWrapped.ServeHTTP(w, r)
		})
	}
}

func (h *Handler) serverID() string {
	if h.cfg != nil {
		return h.cfg.Snapshot().ServerID
	}
	return ""
}

func (h *Handler) dataDir() string {
	if h.cfg != nil {
		if dd := h.cfg.Snapshot().DataDir; dd != "" {
			return dd
		}
	}
	return "./data"
}

func (h *Handler) backupsDir() string {
	return filepath.Join(h.dataDir(), "config", "backups")
}

// --- OpenAPI / Swagger / Robots ---

func (h *Handler) handleOpenAPIJSON(w http.ResponseWriter, r *http.Request) {
	spec := map[string]any{
		"openapi": "3.0.3",
		"info": map[string]any{
			"title":       "Ambra Media Server API",
			"version":     config.ServerVersion,
			"description": "Emby 4.10.2.0 compatible media server REST API",
		},
		"paths": map[string]any{
			"/System/Info/Public":  map[string]any{"get": map[string]any{"summary": "Get public system info"}},
			"/LiveTv/Info":         map[string]any{"get": map[string]any{"summary": "Get Live TV info"}},
			"/Sync/Jobs":           map[string]any{"get": map[string]any{"summary": "List sync jobs"}},
			"/Notifications/Types": map[string]any{"get": map[string]any{"summary": "List notification types"}},
			"/Webhooks":            map[string]any{"get": map[string]any{"summary": "List webhooks"}},
			"/Parties":             map[string]any{"get": map[string]any{"summary": "List watch parties"}},
		},
	}
	httpx.JSON(w, http.StatusOK, spec)
}

func (h *Handler) handleOpenAPIUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`<!doctype html>
<html>
<head><meta charset="utf-8"><title>Ambra OpenAPI</title></head>
<body>
<h1>Ambra Media Server OpenAPI (` + config.ServerVersion + `)</h1>
<p>OpenAPI specification: <a href="/openapi.json">/openapi.json</a> | <a href="/swagger.json">/swagger.json</a></p>
</body>
</html>`))
}

func (h *Handler) handleRobotsTxt(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("User-agent: *\nDisallow: /\n"))
}

// --- GenericUI ---

func (h *Handler) handleUIView(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	pageID := q.Get("PageId")
	if pageID == "" {
		pageID = "default"
	}
	h.mu.RLock()
	serverName := h.state.Startup.ServerName
	h.mu.RUnlock()

	httpx.JSON(w, http.StatusOK, UIView{
		PageId:   pageID,
		ViewType: "RegularPage",
		Caption:  "Ambra Settings",
		Controls: []map[string]any{
			{
				"Id":      "serverName",
				"Type":    "TextInput",
				"Caption": "Server Name",
				"Value":   serverName,
			},
		},
	})
}

func (h *Handler) handleUICommand(w http.ResponseWriter, r *http.Request) {
	var cmd struct {
		PageId    string         `json:"PageId"`
		CommandId string         `json:"CommandId"`
		ItemId    string         `json:"ItemId"`
		Data      map[string]any `json:"Data"`
	}
	if err := httpx.DecodeJSONLimit(r, &cmd, 0); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid UI command payload")
		return
	}
	if strings.TrimSpace(cmd.CommandId) == "" {
		httpx.Error(w, http.StatusBadRequest, "CommandId is required")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"Status":    "Ok",
		"PageId":    cmd.PageId,
		"CommandId": cmd.CommandId,
		"ItemId":    cmd.ItemId,
	})
}

// --- Connect ---

func (h *Handler) handleConnectPending(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	links := append([]ConnectPending(nil), h.state.ConnectLinks...)
	h.mu.RUnlock()
	if links == nil {
		links = []ConnectPending{}
	}
	httpx.JSON(w, http.StatusOK, links)
}

func (h *Handler) handleConnectExchange(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	connectUserID := strings.TrimSpace(q.Get("ConnectUserId"))
	if connectUserID == "" && r.Body != nil {
		var body struct {
			ConnectUserId string `json:"ConnectUserId"`
		}
		if err := httpx.DecodeJSONLimit(r, &body, 0); err == nil {
			connectUserID = strings.TrimSpace(body.ConnectUserId)
		}
	}
	if connectUserID == "" {
		httpx.Error(w, http.StatusBadRequest, "ConnectUserId is required")
		return
	}
	// Ambra does not fabricate unpersisted Connect access tokens; require a valid linked Connect credential.
	httpx.Error(w, http.StatusUnauthorized, "Emby Connect is not linked or credentials are invalid")
}

// --- Persistence ---

func (h *Handler) loadState(ctx context.Context) {
	if h.store == nil || h.store.Reader == nil {
		return
	}
	var raw string
	err := h.store.Reader.QueryRowContext(ctx,
		`SELECT value_json FROM server_config WHERE key = ?`, stateConfigKey,
	).Scan(&raw)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return
	}
	var st opsState
	hasLegacyState := false
	if err == nil {
		if uErr := json.Unmarshal([]byte(raw), &st); uErr == nil {
			hasLegacyState = true
		}
	}

	h.mu.Lock()
	if hasLegacyState {
		h.state = st
	}
	h.migrateAndLoadWebhooksLocked(ctx)
	h.mu.Unlock()
}

func cloneOpsState(st opsState) opsState {
	out := st
	if st.Installations != nil {
		out.Installations = append([]InstallationInfo(nil), st.Installations...)
	}
	if st.Webhooks != nil {
		out.Webhooks = make([]WebhookConfig, len(st.Webhooks))
		copy(out.Webhooks, st.Webhooks)
		for i := range out.Webhooks {
			if st.Webhooks[i].Events != nil {
				out.Webhooks[i].Events = append([]string(nil), st.Webhooks[i].Events...)
			}
		}
	}
	if st.Parties != nil {
		out.Parties = make([]Party, len(st.Parties))
		copy(out.Parties, st.Parties)
		for i := range out.Parties {
			if st.Parties[i].Members != nil {
				out.Parties[i].Members = append([]string(nil), st.Parties[i].Members...)
			}
			if st.Parties[i].Messages != nil {
				out.Parties[i].Messages = append([]PartyMessage(nil), st.Parties[i].Messages...)
			}
		}
	}
	if st.ConnectLinks != nil {
		out.ConnectLinks = append([]ConnectPending(nil), st.ConnectLinks...)
	}
	return out
}

func (h *Handler) saveStateLocked(ctx context.Context) error {
	if h.store == nil {
		return nil
	}
	raw, err := json.Marshal(h.state)
	if err != nil {
		slog.Error("ops: marshal state failed", "err", err)
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
	if err != nil {
		slog.Error("ops: save state failed", "err", err)
	}
	return err
}

func passthrough(next http.Handler) http.Handler { return next }
