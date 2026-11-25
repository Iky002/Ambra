package pluginadmin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

const (
	configKeyPrefix = "plugin:"
	maskedSecret    = "********"
)

// AuthFuncs decouples route registration from the concrete middleware package.
type AuthFuncs struct {
	RequireAuth   func(http.Handler) http.Handler
	RequireAdmin  func(http.Handler) http.Handler
	UserIDFromCtx func(context.Context) string
}

type Handler struct {
	registry *plugins.Registry
	store    *sqlite.Store
	auth     AuthFuncs

	cfgMu   sync.Mutex
	mu      sync.RWMutex
	tracked map[string]plugins.Plugin
	configs map[string]map[string]any
}

// New constructs a plugin administration handler.
// It automatically discovers registered MetadataProviders and StreamResolvers from registry
// so they remain controllable even after being disabled.
func New(registry *plugins.Registry, store *sqlite.Store) *Handler {
	h := &Handler{
		registry: registry,
		store:    store,
		tracked:  make(map[string]plugins.Plugin),
		configs:  make(map[string]map[string]any),
	}
	h.syncFromRegistry()
	return h
}

// Track explicitly registers additional plugin instances (e.g. custom or initially disabled plugins).
func (h *Handler) Track(ps ...plugins.Plugin) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, p := range ps {
		if p != nil {
			h.tracked[p.Manifest().ID] = p
		}
	}
}

func (h *Handler) syncFromRegistry() {
	if h.registry == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, p := range h.registry.MetadataProviders() {
		h.tracked[p.Manifest().ID] = p
	}
	for _, p := range h.registry.StreamResolvers() {
		h.tracked[p.Manifest().ID] = p
	}
}

func (h *Handler) findPlugin(id string) (plugins.Plugin, bool) {
	h.mu.RLock()
	p, ok := h.tracked[id]
	h.mu.RUnlock()
	if ok {
		return p, true
	}
	h.syncFromRegistry()
	h.mu.RLock()
	p, ok = h.tracked[id]
	h.mu.RUnlock()
	return p, ok
}

// ApplySavedConfigs loads all persisted "plugin:<id>" entries from SQLite server_config
// and applies them to the corresponding plugins via Plugin.Configure.
func (h *Handler) ApplySavedConfigs(ctx context.Context) error {
	if h.store == nil {
		return nil
	}
	h.cfgMu.Lock()
	defer h.cfgMu.Unlock()
	h.syncFromRegistry()

	rows, err := h.store.Reader.QueryContext(ctx,
		`SELECT key, value_json FROM server_config WHERE key LIKE ?`,
		configKeyPrefix+"%",
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	type savedEntry struct {
		pluginID string
		cfg      map[string]any
	}
	var entries []savedEntry
	for rows.Next() {
		var key, val string
		if err := rows.Scan(&key, &val); err != nil {
			return err
		}
		pluginID := strings.TrimPrefix(key, configKeyPrefix)
		var cfg map[string]any
		if err := json.Unmarshal([]byte(val), &cfg); err != nil {
			continue
		}
		entries = append(entries, savedEntry{pluginID: pluginID, cfg: cfg})
	}
	if err := rows.Err(); err != nil {
		return err
	}

	var errs []error
	for _, e := range entries {
		h.mu.Lock()
		h.configs[e.pluginID] = cloneMap(e.cfg)
		p := h.tracked[e.pluginID]
		h.mu.Unlock()
		if p != nil {
			if err := p.Configure(cloneMap(e.cfg)); err != nil {
				slog.Error("pluginadmin: failed to apply saved config", "pluginId", e.pluginID, "err", err)
				errs = append(errs, err)
				continue
			}
		}
	}
	return errors.Join(errs...)
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

	handleAdmin := func(pattern string, fn http.HandlerFunc) {
		mux.Handle(pattern, wrapAdmin(fn))
	}

	handleAdmin("GET /Plugins/{Id}/Configuration", h.handleGetConfiguration)
	handleAdmin("POST /Plugins/{Id}/Configuration", h.handleUpdateConfiguration)
	handleAdmin("POST /Plugins/{Id}/Enable", h.handleEnablePlugin)
	handleAdmin("DELETE /Plugins/{Id}", h.handleDeletePlugin)
}

func (h *Handler) handleGetConfiguration(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("Id")
	p, ok := h.findPlugin(id)
	if !ok {
		httpx.Error(w, http.StatusNotFound, "plugin not found")
		return
	}
	manifest := p.Manifest()
	out := map[string]any{
		"id":       manifest.ID,
		"name":     manifest.Name,
		"enabled":  manifest.Enabled,
		"priority": manifest.Priority,
		"Id":       manifest.ID,
		"Name":     manifest.Name,
		"Enabled":  manifest.Enabled,
		"Priority": manifest.Priority,
	}

	saved, err := h.loadSavedConfig(r.Context(), id)
	if err == nil {
		for k, v := range maskSensitiveMap(saved) {
			out[k] = v
		}
	}
	// Always reflect live manifest metadata state
	out["id"] = manifest.ID
	out["name"] = manifest.Name
	out["enabled"] = manifest.Enabled
	out["priority"] = manifest.Priority
	out["Id"] = manifest.ID
	out["Name"] = manifest.Name
	out["Enabled"] = manifest.Enabled
	out["Priority"] = manifest.Priority
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) handleUpdateConfiguration(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("Id")
	p, ok := h.findPlugin(id)
	if !ok {
		httpx.Error(w, http.StatusNotFound, "plugin not found")
		return
	}

	var incoming map[string]any
	if err := httpx.DecodeJSONLimit(r, &incoming, 0); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid JSON configuration")
		return
	}
	if incoming == nil {
		incoming = map[string]any{}
	}

	h.cfgMu.Lock()
	defer h.cfgMu.Unlock()

	prev, err := h.loadSavedConfig(r.Context(), id)
	if err != nil {
		prev = map[string]any{}
	}
	merged := mergePluginConfig(prev, incoming)

	if err := p.Configure(cloneMap(merged)); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.saveConfig(r.Context(), id, merged); err != nil {
		_ = p.Configure(cloneMap(prev))
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, maskSensitiveMap(merged))
}

func (h *Handler) handleEnablePlugin(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("Id")
	p, ok := h.findPlugin(id)
	if !ok {
		httpx.Error(w, http.StatusNotFound, "plugin not found")
		return
	}

	q := httpx.ParseQuery(r)
	enabled := true
	if b := q.Bool("enabled"); b != nil {
		enabled = *b
	} else if r.Body != nil {
		var body struct {
			Enabled *bool `json:"enabled"`
		}
		if err := httpx.DecodeJSONLimit(r, &body, 0); err == nil && body.Enabled != nil {
			enabled = *body.Enabled
		}
	}

	h.cfgMu.Lock()
	defer h.cfgMu.Unlock()

	prev, err := h.loadSavedConfig(r.Context(), id)
	if err != nil {
		prev = map[string]any{}
	}
	merged := cloneMap(prev)
	merged["enabled"] = enabled

	if err := p.Configure(cloneMap(merged)); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.saveConfig(r.Context(), id, merged); err != nil {
		_ = p.Configure(cloneMap(prev))
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"Id":      id,
		"Enabled": p.Manifest().Enabled,
	})
}

func (h *Handler) handleDeletePlugin(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("Id")
	p, ok := h.findPlugin(id)
	if !ok {
		httpx.Error(w, http.StatusNotFound, "plugin not found")
		return
	}

	h.cfgMu.Lock()
	defer h.cfgMu.Unlock()

	prev, err := h.loadSavedConfig(r.Context(), id)
	if err != nil {
		prev = map[string]any{}
	}
	merged := cloneMap(prev)
	merged["enabled"] = false

	if err := p.Configure(cloneMap(merged)); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.saveConfig(r.Context(), id, merged); err != nil {
		_ = p.Configure(cloneMap(prev))
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) loadSavedConfig(ctx context.Context, id string) (map[string]any, error) {
	if h.store != nil {
		var raw string
		err := h.store.Reader.QueryRowContext(ctx,
			`SELECT value_json FROM server_config WHERE key = ?`,
			configKeyPrefix+id,
		).Scan(&raw)
		if err == nil {
			var m map[string]any
			if err := json.Unmarshal([]byte(raw), &m); err == nil {
				return m, nil
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	if m, ok := h.configs[id]; ok {
		return cloneMap(m), nil
	}
	return map[string]any{}, nil
}

func (h *Handler) saveConfig(ctx context.Context, id string, cfg map[string]any) error {
	if h.store != nil {
		raw, err := json.Marshal(cfg)
		if err != nil {
			return err
		}
		_, err = h.store.Writer.ExecContext(ctx, `
			INSERT INTO server_config (key, value_json, updated_at)
			VALUES (?, ?, ?)
			ON CONFLICT(key) DO UPDATE SET
				value_json = excluded.value_json,
				updated_at = excluded.updated_at`,
			configKeyPrefix+id, string(raw), time.Now().UTC().Unix(),
		)
		if err != nil {
			return err
		}
	}
	h.mu.Lock()
	h.configs[id] = cloneMap(cfg)
	h.mu.Unlock()
	return nil
}

func isSensitiveConfigKey(k string) bool {
	lk := strings.ToLower(strings.TrimSpace(k))
	return strings.Contains(lk, "key") ||
		strings.Contains(lk, "secret") ||
		strings.Contains(lk, "token") ||
		strings.Contains(lk, "password")
}

func normalizeConfigKeyLookup(k string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(k), "_", ""))
}

func maskSensitiveMap(src map[string]any) map[string]any {
	out := make(map[string]any, len(src))
	for k, v := range src {
		if isSensitiveConfigKey(k) {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				out[k] = maskedSecret
				continue
			}
		}
		out[k] = v
	}
	return out
}

func mergePluginConfig(prev, incoming map[string]any) map[string]any {
	merged := cloneMap(prev)
	existingByLower := make(map[string]string, len(merged))
	existingByNorm := make(map[string]string, len(merged))
	for k := range merged {
		existingByLower[strings.ToLower(k)] = k
		existingByNorm[normalizeConfigKeyLookup(k)] = k
	}
	for k, v := range incoming {
		targetKey := k
		if ek, ok := existingByLower[strings.ToLower(k)]; ok {
			targetKey = ek
		} else if ek, ok := existingByNorm[normalizeConfigKeyLookup(k)]; ok {
			targetKey = ek
		}
		if isSensitiveConfigKey(targetKey) {
			if s, ok := v.(string); ok {
				trimmed := strings.TrimSpace(s)
				if trimmed == "" || trimmed == maskedSecret {
					if oldStr, okOld := merged[targetKey].(string); okOld && strings.TrimSpace(oldStr) != "" {
						continue
					}
				}
			}
		}
		merged[targetKey] = v
		existingByLower[strings.ToLower(targetKey)] = targetKey
		existingByNorm[normalizeConfigKeyLookup(targetKey)] = targetKey
	}
	return merged
}

func cloneMap(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func passthrough(next http.Handler) http.Handler { return next }
