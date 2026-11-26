package pluginadmin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
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

	cfgMu   sync.RWMutex
	mu      sync.RWMutex
	tracked map[string]plugins.Plugin
	configs map[string]map[string]any
}

// New constructs a plugin administration handler.
// It discovers every registered plugin, including notifier-only plugins such as
// Trakt, so they remain configurable even while disabled.
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
	for _, manifest := range h.registry.List() {
		if p, ok := h.registry.Get(manifest.ID); ok {
			h.tracked[manifest.ID] = p
		}
	}
}

func (h *Handler) findPlugin(id string) (plugins.Plugin, bool) {
	if h.registry != nil {
		if p, ok := h.registry.Get(id); ok && p != nil {
			h.mu.Lock()
			h.tracked[id] = p
			h.mu.Unlock()
			return p, true
		}
	}
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
		if p, ok := h.findPlugin(e.pluginID); ok {
			if filter, ok := p.(plugins.ServerConfigFilter); ok {
				e.cfg = filter.FilterServerConfig(e.cfg)
				// Remove legacy server-wide credentials from persisted configuration.
				if err := h.saveConfig(ctx, e.pluginID, e.cfg); err != nil {
					errs = append(errs, err)
					continue
				}
			}
		}
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
	h.cfgMu.RLock()
	manifest := p.Manifest()
	saved, err := h.loadSavedConfig(r.Context(), id)
	h.cfgMu.RUnlock()

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

	if err == nil {
		if filter, ok := p.(plugins.ServerConfigFilter); ok {
			saved = filter.FilterServerConfig(saved)
		}
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
	prevForMerge := prev
	hostChanged := configURLHostChanged(prev, incoming)
	var clearedSensitiveKeys []string
	if hostChanged {
		prevForMerge, clearedSensitiveKeys = stripSensitiveKeys(prev)
	}
	merged := mergePluginConfig(prevForMerge, incoming)
	if filter, ok := p.(plugins.ServerConfigFilter); ok {
		merged = filter.FilterServerConfig(merged)
	}

	cfgForConfigure := cloneMap(merged)
	if hostChanged && len(clearedSensitiveKeys) > 0 {
		for _, sk := range clearedSensitiveKeys {
			if _, exists := cfgForConfigure[sk]; !exists {
				cfgForConfigure[sk] = ""
			}
		}
	}

	if err := p.Configure(cfgForConfigure); err != nil {
		_ = p.Configure(cloneMap(prev))
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
		_ = p.Configure(cloneMap(prev))
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
		_ = p.Configure(cloneMap(prev))
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
	if p, ok := h.findPlugin(id); ok {
		if filter, ok := p.(plugins.ServerConfigFilter); ok {
			cfg = filter.FilterServerConfig(cfg)
		}
	}
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
	if strings.Contains(lk, "key") ||
		strings.Contains(lk, "secret") ||
		strings.Contains(lk, "token") ||
		strings.Contains(lk, "password") ||
		strings.Contains(lk, "credential") ||
		strings.Contains(lk, "authorization") ||
		strings.Contains(lk, "cookie") {
		return true
	}
	if lk == "pin" || strings.HasSuffix(lk, "_pin") || strings.HasSuffix(lk, "-pin") {
		return true
	}
	if strings.HasSuffix(lk, "pin") && (strings.Contains(lk, "tvdb") || strings.Contains(lk, "user") || strings.Contains(lk, "auth") || strings.Contains(lk, "api") || strings.Contains(lk, "access")) {
		return true
	}
	return false
}

func isURLOrHostConfigKey(k string) bool {
	norm := normalizeConfigKeyLookup(k)
	if norm == "" {
		return false
	}
	switch norm {
	case "baseurl", "endpoint", "imagebaseurl", "apiurl", "serverurl", "url", "host", "apihost", "basehost", "endpointurl":
		return true
	}
	return strings.HasSuffix(norm, "url") || strings.HasSuffix(norm, "endpoint") || strings.HasSuffix(norm, "host")
}

func extractURLHostLower(v any) string {
	s, ok := v.(string)
	if !ok {
		return ""
	}
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return ""
	}
	if u, err := url.Parse(trimmed); err == nil && u.Host != "" {
		return strings.ToLower(u.Host)
	}
	if !strings.Contains(trimmed, "://") && !strings.ContainsAny(trimmed, " \t\r\n/") {
		return strings.ToLower(trimmed)
	}
	return ""
}

func configURLHostChanged(prev, incoming map[string]any) bool {
	if len(incoming) == 0 {
		return false
	}
	prevByNorm := make(map[string]any, len(prev))
	for k, v := range prev {
		prevByNorm[normalizeConfigKeyLookup(k)] = v
	}
	for k, v := range incoming {
		norm := normalizeConfigKeyLookup(k)
		prevVal, hasPrev := prevByNorm[norm]
		if subIn, ok := v.(map[string]any); ok {
			var subPrev map[string]any
			if hasPrev {
				subPrev, _ = prevVal.(map[string]any)
			}
			if configURLHostChanged(subPrev, subIn) {
				return true
			}
			continue
		}
		if !isURLOrHostConfigKey(k) {
			continue
		}
		newHost := extractURLHostLower(v)
		oldHost := ""
		if hasPrev {
			oldHost = extractURLHostLower(prevVal)
		}
		if newHost != oldHost {
			return true
		}
	}
	return false
}

func stripSensitiveKeys(src map[string]any) (map[string]any, []string) {
	out := make(map[string]any, len(src))
	var cleared []string
	for k, v := range src {
		if isSensitiveConfigKey(k) {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				cleared = append(cleared, k)
			}
			continue
		}
		if sub, ok := v.(map[string]any); ok {
			cleanedSub, _ := stripSensitiveKeys(sub)
			out[k] = cleanedSub
			continue
		}
		out[k] = cloneValue(v)
	}
	return out, cleared
}

func normalizeConfigKeyLookup(k string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(k), "_", ""))
}

func maskSensitiveMap(src map[string]any) map[string]any {
	return maskSensitiveMapInternal(src, false)
}

func maskSensitiveMapInternal(src map[string]any, forceSensitive bool) map[string]any {
	out := make(map[string]any, len(src))
	for k, v := range src {
		keySensitive := forceSensitive || isSensitiveConfigKey(k)
		out[k] = maskSensitiveValue(v, keySensitive)
	}
	return out
}

func maskSensitiveValue(v any, sensitive bool) any {
	switch val := v.(type) {
	case string:
		if sensitive && strings.TrimSpace(val) != "" {
			return maskedSecret
		}
		return val
	case map[string]any:
		return maskSensitiveMapInternal(val, sensitive)
	case []any:
		out := make([]any, len(val))
		for i, elem := range val {
			out[i] = maskSensitiveValue(elem, sensitive)
		}
		return out
	default:
		return v
	}
}

func mergePluginConfig(prev, incoming map[string]any) map[string]any {
	return mergePluginConfigInternal(prev, incoming, false)
}

func mergePluginConfigInternal(prev, incoming map[string]any, forceSensitive bool) map[string]any {
	merged := cloneMap(prev)
	existingByLower := make(map[string]string, len(merged))
	existingByNorm := make(map[string]string, len(merged))
	for k := range merged {
		existingByLower[strings.ToLower(k)] = k
		existingByNorm[normalizeConfigKeyLookup(k)] = k
	}
	keys := make([]string, 0, len(incoming))
	for k := range incoming {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := incoming[k]
		targetKey := k
		if ek, ok := existingByLower[strings.ToLower(k)]; ok {
			targetKey = ek
		} else if ek, ok := existingByNorm[normalizeConfigKeyLookup(k)]; ok {
			targetKey = ek
		}
		keySensitive := forceSensitive || isSensitiveConfigKey(targetKey)
		switch val := v.(type) {
		case string:
			if keySensitive && strings.TrimSpace(val) == maskedSecret {
				if oldStr, okOld := merged[targetKey].(string); okOld && strings.TrimSpace(oldStr) != "" {
					continue
				}
				// Never persist literal masked placeholder "********" when no prior secret exists.
				continue
			}
			// Explicit empty string "" clears any previously saved secret instead of preserving it.
			merged[targetKey] = val
		case map[string]any:
			if prevMap, okPrev := merged[targetKey].(map[string]any); okPrev {
				merged[targetKey] = mergePluginConfigInternal(prevMap, val, keySensitive)
			} else {
				merged[targetKey] = mergePluginConfigInternal(map[string]any{}, val, keySensitive)
			}
		case []any:
			if prevSlice, okPrev := merged[targetKey].([]any); okPrev {
				merged[targetKey] = mergePluginSlice(prevSlice, val, keySensitive)
			} else {
				merged[targetKey] = mergePluginSlice(nil, val, keySensitive)
			}
		default:
			merged[targetKey] = cloneValue(v)
		}
		existingByLower[strings.ToLower(targetKey)] = targetKey
		existingByNorm[normalizeConfigKeyLookup(targetKey)] = targetKey
	}
	return merged
}

func mergePluginSlice(prev, incoming []any, forceSensitive bool) []any {
	out := make([]any, len(incoming))
	for i, elem := range incoming {
		var hasPrev bool
		var prevElem any
		if i < len(prev) {
			hasPrev = true
			prevElem = prev[i]
		}
		switch val := elem.(type) {
		case string:
			if forceSensitive && strings.TrimSpace(val) == maskedSecret {
				if hasPrev {
					if oldStr, okOld := prevElem.(string); okOld && strings.TrimSpace(oldStr) != "" {
						out[i] = oldStr
						continue
					}
				}
				out[i] = ""
				continue
			}
			out[i] = val
		case map[string]any:
			if hasPrev {
				if prevMap, okPrev := prevElem.(map[string]any); okPrev {
					out[i] = mergePluginConfigInternal(prevMap, val, forceSensitive)
					continue
				}
			}
			out[i] = cloneMap(val)
		case []any:
			if hasPrev {
				if prevSub, okPrev := prevElem.([]any); okPrev {
					out[i] = mergePluginSlice(prevSub, val, forceSensitive)
					continue
				}
			}
			out[i] = cloneSlice(val)
		default:
			out[i] = cloneValue(elem)
		}
	}
	return out
}

func cloneMap(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = cloneValue(v)
	}
	return dst
}

func cloneSlice(src []any) []any {
	dst := make([]any, len(src))
	for i, v := range src {
		dst[i] = cloneValue(v)
	}
	return dst
}

func cloneValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		return cloneMap(val)
	case []any:
		return cloneSlice(val)
	default:
		return v
	}
}

func passthrough(next http.Handler) http.Handler { return next }
