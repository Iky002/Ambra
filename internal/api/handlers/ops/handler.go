package ops

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/pkg/netutil"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

const stateConfigKey = "ops:state"

// AuthFuncs decouples route registration from the concrete middleware package.
type AuthFuncs struct {
	RequireAuth   func(http.Handler) http.Handler
	RequireAdmin  func(http.Handler) http.Handler
	UserIDFromCtx func(context.Context) string
}

type opsState struct {
	Startup      StartupConfiguration `json:"startup"`
	StartupUser  StartupUserState     `json:"startup_user"`
	RemoteAccess RemoteAccessConfig   `json:"remote_access"`
	Installations []InstallationInfo  `json:"installations"`
	Webhooks     []WebhookConfig      `json:"webhooks"`
	Parties      []Party              `json:"parties"`
	ConnectLinks []ConnectPending     `json:"connect_links"`
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
	store            *sqlite.Store
	cfg              *config.Config
	userRepo         *sqlite.UserRepo
	bus              *eventbus.Bus
	httpClient       *http.Client
	customHTTPClient bool
	auth             AuthFuncs

	backupMu      sync.Mutex
	setupMu       sync.Mutex
	webhookMu     sync.Mutex
	webhookClosed bool
	webhookWG     sync.WaitGroup
	webhookCh     chan webhookDispatchJob

	mu             sync.RWMutex
	state          opsState
	partyJoinFails map[string]*partyFailWindow
}

func New(store *sqlite.Store, cfg *config.Config, userRepo *sqlite.UserRepo, bus *eventbus.Bus) *Handler {
	h := &Handler{
		store:      store,
		cfg:        cfg,
		userRepo:   userRepo,
		bus:        bus,
		httpClient:     netutil.NewSafeHTTPClientNoLoopback(10*time.Second, true),
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
		if cfg.ServerName != "" {
			h.state.Startup.ServerName = cfg.ServerName
		}
		if cfg.TMDBLanguage != "" {
			h.state.Startup.PreferredMetadataLanguage = cfg.TMDBLanguage
		}
	}
	h.loadState(context.Background())
	if bus != nil {
		h.subscribeWebhooks(bus)
	}
	return h
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
	handleAuth("GET /Packages/Updates", h.handlePackageUpdates)
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

	// GenericUI
	handleAuth("GET /UI/View", h.handleUIView)
	handleAuth("POST /UI/Command", h.handleUICommand)

	// Parties
	handleAuth("GET /Parties", h.handleListParties)
	handleAuth("POST /Parties", h.handleCreateParty)
	handleAuth("GET /Parties/{Id}", h.handleGetParty)
	handleAuth("DELETE /Parties/{Id}", h.handleDeleteParty)
	handleAuth("POST /Parties/{Id}/Join", h.handleJoinParty)
	handleAuth("POST /Parties/{Id}/Leave", h.handleLeaveParty)
	handleAuth("POST /Parties/{Id}/State", h.handleUpdatePartyState)

	// Connect
	handleAdmin("GET /Connect/Pending", h.handleConnectPending)
	handleAuth("POST /Connect/Exchange", h.handleConnectExchange)
}

func (h *Handler) requireSetupOrAdmin(wrapAdmin func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		adminWrapped := wrapAdmin(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		return h.cfg.ServerID
	}
	return ""
}

func (h *Handler) dataDir() string {
	if h.cfg != nil && h.cfg.DataDir != "" {
		return h.cfg.DataDir
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
			"/System/Info/Public": map[string]any{"get": map[string]any{"summary": "Get public system info"}},
			"/LiveTv/Info":        map[string]any{"get": map[string]any{"summary": "Get Live TV info"}},
			"/Sync/Jobs":          map[string]any{"get": map[string]any{"summary": "List sync jobs"}},
			"/Notifications/Types": map[string]any{"get": map[string]any{"summary": "List notification types"}},
			"/Webhooks":           map[string]any{"get": map[string]any{"summary": "List webhooks"}},
			"/Parties":            map[string]any{"get": map[string]any{"summary": "List watch parties"}},
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
	if h.store == nil {
		return
	}
	var raw string
	err := h.store.Reader.QueryRowContext(ctx,
		`SELECT value_json FROM server_config WHERE key = ?`, stateConfigKey,
	).Scan(&raw)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return
		}
		return
	}
	var st opsState
	if err := json.Unmarshal([]byte(raw), &st); err == nil {
		h.mu.Lock()
		h.state = st
		h.mu.Unlock()
	}
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
