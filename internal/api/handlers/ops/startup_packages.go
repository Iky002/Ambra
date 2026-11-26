package ops

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

const maxInstallations = 100

type StartupConfiguration struct {
	ServerName                string `json:"ServerName"`
	UICulture                 string `json:"UICulture"`
	MetadataCountryCode       string `json:"MetadataCountryCode"`
	PreferredMetadataLanguage string `json:"PreferredMetadataLanguage"`
}

type StartupUserState struct {
	Name        string `json:"Name"`
	HasPassword bool   `json:"HasPassword"`
}

type RemoteAccessConfig struct {
	EnableRemoteAccess         bool `json:"EnableRemoteAccess"`
	EnableAutomaticPortMapping bool `json:"EnableAutomaticPortMapping"`
}

type PackageVersionInfo struct {
	VersionStr  string `json:"versionStr"`
	Description string `json:"description"`
	Checksum    string `json:"checksum,omitempty"`
}

type PackageInfo struct {
	Id       string               `json:"id"`
	Name     string               `json:"name"`
	Category string               `json:"category"`
	Overview string               `json:"overview"`
	Owner    string               `json:"owner"`
	Versions []PackageVersionInfo `json:"versions"`
}

type InstallationInfo struct {
	Id          string  `json:"Id"`
	Name        string  `json:"Name"`
	Version     string  `json:"Version"`
	PercentDone float64 `json:"PercentComplete"`
	Status      string  `json:"Status"`
}

var builtinCatalog = []PackageInfo{
	{
		Id:       "ambra-tmdb",
		Name:     "TMDB Metadata",
		Category: "Metadata",
		Overview: "The Movie Database metadata and artwork provider.",
		Owner:    "Ambra",
		Versions: []PackageVersionInfo{{VersionStr: config.ServerVersion, Description: "Built-in TMDB provider"}},
	},
	{
		Id:       "ambra-strm302",
		Name:     "STRM 302 Resolver",
		Category: "Streaming",
		Overview: "Direct 302 redirect resolver for cloud STRM media.",
		Owner:    "Ambra",
		Versions: []PackageVersionInfo{{VersionStr: config.ServerVersion, Description: "Built-in STRM resolver"}},
	},
	{
		Id:       "ambra-webhooks",
		Name:     "Webhooks",
		Category: "Notifications",
		Overview: "Event-driven HTTP JSON webhook dispatcher.",
		Owner:    "Ambra",
		Versions: []PackageVersionInfo{{VersionStr: config.ServerVersion, Description: "Built-in webhook dispatcher"}},
	},
}

// --- Startup Wizard ---

func (h *Handler) loadSystemExtraLocked(ctx context.Context) map[string]any {
	extra := map[string]any{}
	if h.store == nil || h.store.Reader == nil {
		return extra
	}
	var raw string
	err := h.store.Reader.QueryRowContext(ctx,
		`SELECT value_json FROM server_config WHERE key = ?`, systemExtraConfigKey,
	).Scan(&raw)
	if err == nil && raw != "" {
		_ = json.Unmarshal([]byte(raw), &extra)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return extra
	}
	return extra
}

func (h *Handler) saveSystemExtraLocked(ctx context.Context, extra map[string]any) {
	if h.store == nil || h.store.Writer == nil || extra == nil {
		return
	}
	raw, err := json.Marshal(extra)
	if err != nil {
		return
	}
	_, _ = h.store.Writer.ExecContext(ctx, `
		INSERT INTO server_config (key, value_json, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			value_json = excluded.value_json,
			updated_at = excluded.updated_at`,
		systemExtraConfigKey, string(raw), time.Now().UTC().Unix(),
	)
}

func (h *Handler) handleGetStartupConfig(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	stCfg := h.state.Startup
	extra := h.loadSystemExtraLocked(r.Context())
	h.mu.RUnlock()

	if h.cfg != nil {
		snap := h.cfg.Snapshot()
		if snap.ServerName != "" {
			stCfg.ServerName = snap.ServerName
		}
		if snap.TMDBLanguage != "" {
			stCfg.PreferredMetadataLanguage = snap.TMDBLanguage
		}
	}
	if v, ok := extra["UICulture"].(string); ok && v != "" {
		stCfg.UICulture = v
	}
	if v, ok := extra["MetadataCountryCode"].(string); ok && v != "" {
		stCfg.MetadataCountryCode = v
	}
	if v, ok := extra["PreferredMetadataLanguage"].(string); ok && v != "" {
		stCfg.PreferredMetadataLanguage = v
	}
	httpx.JSON(w, http.StatusOK, stCfg)
}

func (h *Handler) handleUpdateStartupConfig(w http.ResponseWriter, r *http.Request) {
	var req StartupConfiguration
	if err := httpx.DecodeJSONLimit(r, &req, 64<<10); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid startup configuration payload")
		return
	}
	h.mu.Lock()
	if req.ServerName != "" {
		h.state.Startup.ServerName = req.ServerName
	}
	if req.UICulture != "" {
		h.state.Startup.UICulture = req.UICulture
	}
	if req.MetadataCountryCode != "" {
		h.state.Startup.MetadataCountryCode = req.MetadataCountryCode
	}
	if req.PreferredMetadataLanguage != "" {
		h.state.Startup.PreferredMetadataLanguage = req.PreferredMetadataLanguage
	}
	if h.cfg != nil {
		h.cfg.Update(func(c *config.Config) {
			if req.ServerName != "" {
				c.ServerName = req.ServerName
			}
			if req.PreferredMetadataLanguage != "" {
				c.TMDBLanguage = req.PreferredMetadataLanguage
			}
		})
	}
	extra := h.loadSystemExtraLocked(r.Context())
	if req.UICulture != "" {
		extra["UICulture"] = req.UICulture
	}
	if req.MetadataCountryCode != "" {
		extra["MetadataCountryCode"] = req.MetadataCountryCode
	}
	if req.PreferredMetadataLanguage != "" {
		extra["PreferredMetadataLanguage"] = req.PreferredMetadataLanguage
	}
	h.saveSystemExtraLocked(r.Context(), extra)
	_ = h.saveStateLocked(r.Context())
	if h.cfg != nil {
		_ = h.cfg.Save()
	}
	h.mu.Unlock()
	httpx.NoContent(w)
}

func (h *Handler) handleGetStartupUser(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	name := h.state.StartupUser.Name
	h.mu.RUnlock()
	if name == "" {
		name = "admin"
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"Name":     name,
		"Password": "",
	})
}

func (h *Handler) handleUpdateStartupUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name     string `json:"Name"`
		Password string `json:"Password"`
	}
	if err := httpx.DecodeJSONLimit(r, &req, 64<<10); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid startup user payload")
		return
	}
	if auth.ContainsControlChar(req.Name) {
		httpx.Error(w, http.StatusBadRequest, "username must not contain control characters")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len([]rune(name)) > 128 {
		httpx.Error(w, http.StatusBadRequest, "username must be between 1 and 128 characters")
		return
	}
	if strings.TrimSpace(req.Password) == "" {
		httpx.Error(w, http.StatusBadRequest, "password is required")
		return
	}
	if len([]rune(req.Password)) < 4 {
		httpx.Error(w, http.StatusBadRequest, "password must be at least 4 characters")
		return
	}

	if h.userRepo != nil {
		h.setupMu.Lock()
		defer h.setupMu.Unlock()

		count, err := h.userRepo.CountUsers(r.Context())
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "failed to check existing users")
			return
		}
		if count > 0 && (h.auth.RequireAdmin != nil || h.auth.RequireAuth != nil) {
			ac, ok := middleware.AuthFromContext(r.Context())
			if !ok || ac == nil || !ac.IsAdmin() {
				httpx.Error(w, http.StatusForbidden, "initial setup already completed")
				return
			}
		}

		hash, err := auth.HashPassword(req.Password)
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "failed to hash password")
			return
		}
		existing, err := h.userRepo.GetUserByName(r.Context(), name)
		if err == nil && existing != nil {
			if err := h.userRepo.UpdateUserPassword(r.Context(), existing.ID, hash); err != nil {
				httpx.Error(w, http.StatusInternalServerError, "failed to update user password")
				return
			}
		} else if errors.Is(err, sqlite.ErrNotFound) {
			u := &sqlite.UserRecord{
				Name:         name,
				PasswordHash: hash,
				IsAdmin:      true,
				Policy:       sqlite.DefaultUserPolicy(true),
				Config:       sqlite.DefaultUserConfig(),
			}
			if err := h.userRepo.CreateUser(r.Context(), u); err != nil {
				httpx.Error(w, http.StatusInternalServerError, "failed to create startup admin user")
				return
			}
		} else if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "failed to lookup user")
			return
		}
	}

	h.mu.Lock()
	h.state.StartupUser = StartupUserState{
		Name:        name,
		HasPassword: true,
	}
	_ = h.saveStateLocked(r.Context())
	h.mu.Unlock()
	httpx.NoContent(w)
}

func (h *Handler) handleUpdateRemoteAccess(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		EnableRemoteAccess         *bool `json:"EnableRemoteAccess"`
		EnableAutomaticPortMapping *bool `json:"EnableAutomaticPortMapping"`
	}
	if err := httpx.DecodeJSONLimit(r, &payload, 64<<10); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid remote access payload")
		return
	}
	h.mu.Lock()
	if payload.EnableRemoteAccess != nil {
		h.state.RemoteAccess.EnableRemoteAccess = *payload.EnableRemoteAccess
	}
	if payload.EnableAutomaticPortMapping != nil {
		h.state.RemoteAccess.EnableAutomaticPortMapping = *payload.EnableAutomaticPortMapping
	}
	if h.cfg != nil && payload.EnableRemoteAccess != nil {
		enableRemote := *payload.EnableRemoteAccess
		h.cfg.Update(func(c *config.Config) {
			b := enableRemote
			c.EnableRemoteAccessGlobal = &b
		})
		_ = h.cfg.Save()
	}
	extra := h.loadSystemExtraLocked(r.Context())
	if payload.EnableAutomaticPortMapping != nil {
		extra["EnableAutomaticPortMapping"] = *payload.EnableAutomaticPortMapping
	}
	h.saveSystemExtraLocked(r.Context(), extra)
	_ = h.saveStateLocked(r.Context())
	h.mu.Unlock()
	httpx.NoContent(w)
}

func (h *Handler) handleStartupComplete(w http.ResponseWriter, r *http.Request) {
	if h.userRepo == nil {
		httpx.Error(w, http.StatusBadRequest, "at least one user must be created before completing startup wizard")
		return
	}
	count, err := h.userRepo.CountUsers(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to check existing users")
		return
	}
	if count <= 0 {
		httpx.Error(w, http.StatusBadRequest, "at least one user must be created before completing startup wizard")
		return
	}
	h.mu.Lock()
	if h.cfg != nil {
		h.cfg.Update(func(c *config.Config) {
			c.IsStartupWizardCompleted = true
		})
		_ = h.cfg.Save()
	}
	_ = h.saveStateLocked(r.Context())
	h.mu.Unlock()
	httpx.NoContent(w)
}

// --- Packages ---

func (h *Handler) handleListPackages(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	category := strings.ToLower(strings.TrimSpace(q.Get("packagetype")))
	out := make([]PackageInfo, 0, len(builtinCatalog))
	for _, p := range builtinCatalog {
		if category != "" && strings.ToLower(p.Category) != category {
			continue
		}
		out = append(out, p)
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) handleGetPackage(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("Name")
	for _, p := range builtinCatalog {
		if strings.EqualFold(p.Name, name) || strings.EqualFold(p.Id, name) {
			httpx.JSON(w, http.StatusOK, p)
			return
		}
	}
	httpx.Error(w, http.StatusNotFound, "package not found")
}

func (h *Handler) handlePackageUpdates(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, []PackageVersionInfo{})
}

func (h *Handler) handleInstallPackage(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("Name")
	q := httpx.ParseQuery(r)
	version := q.Get("version")
	if version == "" {
		version = config.ServerVersion
	}

	var matched *PackageInfo
	for i := range builtinCatalog {
		if strings.EqualFold(builtinCatalog[i].Name, name) || strings.EqualFold(builtinCatalog[i].Id, name) {
			matched = &builtinCatalog[i]
			break
		}
	}
	if matched == nil {
		httpx.Error(w, http.StatusNotFound, "package not found")
		return
	}

	inst := InstallationInfo{
		Id:          guid.New(),
		Name:        matched.Name,
		Version:     version,
		PercentDone: 100,
		Status:      "Completed",
	}

	h.mu.Lock()
	snapshot := cloneOpsState(h.state)
	h.state.Installations = append(h.state.Installations, inst)
	if len(h.state.Installations) > maxInstallations {
		h.state.Installations = append([]InstallationInfo(nil), h.state.Installations[len(h.state.Installations)-maxInstallations:]...)
	}
	err := h.saveStateLocked(r.Context())
	if err != nil {
		h.state = snapshot
	}
	h.mu.Unlock()
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to persist package installation")
		return
	}

	httpx.JSON(w, http.StatusOK, map[string]any{
		"Id":              inst.Id,
		"Name":            inst.Name,
		"Version":         inst.Version,
		"PercentComplete": inst.PercentDone,
		"Status":          inst.Status,
		"DateCreated":     time.Now().UTC(),
	})
}

func (h *Handler) handleCancelInstall(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("Id")
	h.mu.Lock()
	for i, inst := range h.state.Installations {
		if inst.Id == id {
			snapshot := cloneOpsState(h.state)
			h.state.Installations = append(h.state.Installations[:i], h.state.Installations[i+1:]...)
			if err := h.saveStateLocked(r.Context()); err != nil {
				h.state = snapshot
				h.mu.Unlock()
				httpx.Error(w, http.StatusInternalServerError, "failed to persist cancellation")
				return
			}
			h.mu.Unlock()
			httpx.NoContent(w)
			return
		}
	}
	h.mu.Unlock()
	httpx.Error(w, http.StatusNotFound, "installation task not found")
}
