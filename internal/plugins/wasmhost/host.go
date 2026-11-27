package wasmhost

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ambra-media/ambra/internal/pkg/netutil"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"github.com/tetratelabs/wazero/sys"
)

const (
	DefaultTimeout            = 5 * time.Second
	maxPluginTimeout          = 30 * time.Second
	maxWasmMemoryPages        = 1024 // 64 MiB (1024 * 64 KiB)
	maxWasmFileBytes          = 32 << 20
	maxWasmStdoutBytes        = 4 << 20
	maxWasmStderrBytes        = 64 << 10
	maxManifestBytes          = 1 << 20
	maxHostLogMsgBytes        = 64 << 10
	maxHostHTTPReqBytes       = 1 << 20
	maxHostHTTPRespBytes      = 2 << 20 // 2 MiB max HTTP response body for ambra.http_request
	defaultMaxConcurrentCalls = 8

	// Host HTTP error status return codes (negative values in -1..-9; buffer-too-small returns -requiredLen <= -12).
	HostHTTPErrMemory     int32 = -1
	HostHTTPErrBadRequest int32 = -2
	HostHTTPErrForbidden  int32 = -3
	HostHTTPErrNetwork    int32 = -4
	HostHTTPErrTooLarge   int32 = -5
)

type wasmCallCtxKey struct{}

type wasmCallContext struct {
	pluginID     string
	allowedHosts []string
}

type limitedBuffer struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	limit     int
	truncated bool
	cancel    context.CancelFunc
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	remaining := l.limit - l.buf.Len()
	if remaining <= 0 {
		l.truncated = true
		if l.cancel != nil {
			l.cancel()
		}
		return len(p), nil
	}
	if len(p) > remaining {
		_, _ = l.buf.Write(p[:remaining])
		l.truncated = true
		if l.cancel != nil {
			l.cancel()
		}
		return len(p), nil
	}
	_, _ = l.buf.Write(p)
	return len(p), nil
}

func (l *limitedBuffer) Bytes() []byte {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]byte(nil), l.buf.Bytes()...)
}

func (l *limitedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

func (l *limitedBuffer) isTruncated() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.truncated
}

// PluginManifest extends plugins.Manifest with WASM runtime fields loaded from manifest.json.
type PluginManifest struct {
	plugins.Manifest
	WasmFile     string   `json:"WasmFile"`
	TimeoutMs    int      `json:"TimeoutMs,omitempty"`
	AllowedHosts []string `json:"AllowedHosts,omitempty"`
}

// Request is the JSON envelope written to a WASM plugin's WASI stdin.
type Request struct {
	Action        string                       `json:"action"`
	Config        map[string]any               `json:"config,omitempty"`
	Query         *plugins.MediaQuery          `json:"query,omitempty"`
	RawPath       string                       `json:"raw_path,omitempty"`
	SubtitleQuery *plugins.SubtitleSearchQuery `json:"subtitle_query,omitempty"`
	SubtitleID    string                       `json:"subtitle_id,omitempty"`
	Notification  *plugins.NotificationPayload `json:"notification,omitempty"`
}

var errRedirectHostForbidden = errors.New("wasmhost: redirect host is not in AllowedHosts")

type hostHTTPRequestPayload struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
}

type hostHTTPResponsePayload struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body"`
}

// Host owns a pure-Go wazero runtime for executing WASI-based Ambra plugins.
type Host struct {
	runtime        wazero.Runtime
	defaultTimeout time.Duration
	sem            chan struct{}

	mu         sync.RWMutex
	httpClient *http.Client
	customHTTP bool
	logHook    func(pluginID string, level uint32, msg string)
	closed     bool
}

func wrapWasmHTTPClient(base *http.Client, customHTTP bool) *http.Client {
	if base == nil {
		base = netutil.NewSafeHTTPClientNoLoopback(10*time.Second, false)
	}
	cloned := *base
	prevCheck := base.CheckRedirect
	cloned.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("wasmhost: stopped after 10 redirects")
		}
		if req == nil || req.URL == nil || (req.URL.Scheme != "http" && req.URL.Scheme != "https") || req.URL.Hostname() == "" {
			return errRedirectHostForbidden
		}
		if cc, ok := req.Context().Value(wasmCallCtxKey{}).(wasmCallContext); ok && len(cc.allowedHosts) > 0 {
			if !matchAllowedHost(req.URL.Hostname(), cc.allowedHosts) {
				return fmt.Errorf("%w: %s", errRedirectHostForbidden, req.URL.Hostname())
			}
		}
		if !customHTTP {
			if err := netutil.ValidateRemoteURLNoLoopback(req.URL.String(), false); err != nil {
				return fmt.Errorf("%w: %v", errRedirectHostForbidden, err)
			}
		} else if ip := net.ParseIP(req.URL.Hostname()); ip != nil {
			if err := netutil.ValidateIP(ip, true); err != nil {
				return fmt.Errorf("%w: %v", errRedirectHostForbidden, err)
			}
		}
		if prevCheck != nil {
			return prevCheck(req, via)
		}
		if len(via) > 0 && !strings.EqualFold(req.URL.Host, via[0].URL.Host) {
			req.Header.Del("Authorization")
			req.Header.Del("Cookie")
			req.Header.Del("Proxy-Authorization")
			req.Header.Del("X-Emby-Token")
			req.Header.Del("X-MediaBrowser-Token")
		}
		return nil
	}
	return &cloned
}

// NewHost initializes a pure-Go wazero runtime with WASI preview1 support,
// the "ambra" host module (log + restricted http_request), memory page limit (64 MiB),
// concurrency limiting, and context-cancellation enforcement.
func NewHost(ctx context.Context) (*Host, error) {
	rtCfg := wazero.NewRuntimeConfig().
		WithCloseOnContextDone(true).
		WithMemoryLimitPages(maxWasmMemoryPages)
	rt := wazero.NewRuntimeWithConfig(ctx, rtCfg)
	if _, err := wasi_snapshot_preview1.Instantiate(ctx, rt); err != nil {
		_ = rt.Close(ctx)
		return nil, fmt.Errorf("wasmhost: instantiate wasi: %w", err)
	}

	h := &Host{
		runtime:        rt,
		defaultTimeout: DefaultTimeout,
		sem:            make(chan struct{}, defaultMaxConcurrentCalls),
		httpClient:     wrapWasmHTTPClient(netutil.NewSafeHTTPClientNoLoopback(10*time.Second, false), false),
	}

	if _, err := rt.NewHostModuleBuilder("ambra").
		NewFunctionBuilder().
		WithFunc(h.hostLog).
		Export("log").
		NewFunctionBuilder().
		WithFunc(h.hostHTTPRequest).
		Export("http_request").
		Instantiate(ctx); err != nil {
		_ = rt.Close(ctx)
		return nil, fmt.Errorf("wasmhost: instantiate ambra host module: %w", err)
	}

	return h, nil
}

// SetHTTPClient overrides the HTTP client used by ambra.http_request (intended for isolated unit tests).
func (h *Host) SetHTTPClient(c *http.Client) {
	if c == nil {
		return
	}
	h.mu.Lock()
	h.httpClient = wrapWasmHTTPClient(c, true)
	h.customHTTP = true
	h.mu.Unlock()
}

func (h *Host) hostLog(ctx context.Context, m api.Module, level uint32, msgPtr uint32, msgLen uint32) {
	mem := m.Memory()
	if mem == nil || msgLen == 0 {
		return
	}
	if msgLen > maxHostLogMsgBytes {
		msgLen = maxHostLogMsgBytes
	}
	raw, ok := mem.Read(msgPtr, msgLen)
	if !ok {
		return
	}
	msg := strings.TrimSpace(string(raw))
	pluginID := "unknown"
	if cc, ok := ctx.Value(wasmCallCtxKey{}).(wasmCallContext); ok && cc.pluginID != "" {
		pluginID = cc.pluginID
	}

	h.mu.RLock()
	hook := h.logHook
	h.mu.RUnlock()
	if hook != nil {
		hook(pluginID, level, msg)
	}

	switch level {
	case 0:
		slog.Debug("wasm plugin log", "plugin_id", pluginID, "msg", msg)
	case 2:
		slog.Warn("wasm plugin log", "plugin_id", pluginID, "msg", msg)
	case 3:
		slog.Error("wasm plugin log", "plugin_id", pluginID, "msg", msg)
	default:
		slog.Info("wasm plugin log", "plugin_id", pluginID, "msg", msg)
	}
}

func matchAllowedHost(targetHost string, allowedHosts []string) bool {
	target := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(targetHost), "."))
	if target == "" {
		return false
	}
	for _, rawPattern := range allowedHosts {
		pat := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(rawPattern), "."))
		if pat == "" {
			continue
		}
		if pat == "*" || pat == target {
			return true
		}
		if strings.HasPrefix(pat, "*.") {
			suffix := pat[1:] // ".example.com"
			if len(suffix) > 1 && strings.HasSuffix(target, suffix) && len(target) > len(suffix) {
				return true
			}
		}
	}
	return false
}

func (h *Host) hostHTTPRequest(ctx context.Context, m api.Module, reqPtr uint32, reqLen uint32, respPtr uint32, respCap uint32) int32 {
	mem := m.Memory()
	if mem == nil || reqLen == 0 || reqLen > maxHostHTTPReqBytes {
		return HostHTTPErrMemory
	}
	reqBytes, ok := mem.Read(reqPtr, reqLen)
	if !ok {
		return HostHTTPErrMemory
	}

	var reqPayload hostHTTPRequestPayload
	if err := json.Unmarshal(reqBytes, &reqPayload); err != nil {
		return HostHTTPErrBadRequest
	}
	rawURL := strings.TrimSpace(reqPayload.URL)
	if rawURL == "" {
		return HostHTTPErrBadRequest
	}
	parsedURL, err := url.Parse(rawURL)
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Hostname() == "" {
		return HostHTTPErrBadRequest
	}

	cc, _ := ctx.Value(wasmCallCtxKey{}).(wasmCallContext)
	if len(cc.allowedHosts) > 0 && !matchAllowedHost(parsedURL.Hostname(), cc.allowedHosts) {
		return HostHTTPErrForbidden
	}

	h.mu.RLock()
	client := h.httpClient
	customHTTP := h.customHTTP
	h.mu.RUnlock()

	if !customHTTP {
		if err := netutil.ValidateRemoteURLNoLoopback(rawURL, false); err != nil {
			return HostHTTPErrForbidden
		}
	} else if ip := net.ParseIP(parsedURL.Hostname()); ip != nil {
		// Even in custom-client test mode, block cloud metadata / link-local / unspecified IP literals.
		if err := netutil.ValidateIP(ip, true); err != nil {
			return HostHTTPErrForbidden
		}
	}

	method := strings.ToUpper(strings.TrimSpace(reqPayload.Method))
	if method == "" {
		method = http.MethodGet
	}

	ctxWithCall := context.WithValue(ctx, wasmCallCtxKey{}, cc)
	reqCtx, cancel := context.WithTimeout(ctxWithCall, 10*time.Second)
	defer cancel()

	var bodyReader io.Reader
	if reqPayload.Body != "" {
		bodyReader = strings.NewReader(reqPayload.Body)
	}
	httpReq, err := http.NewRequestWithContext(reqCtx, method, rawURL, bodyReader)
	if err != nil {
		return HostHTTPErrBadRequest
	}
	for k, v := range reqPayload.Headers {
		if strings.TrimSpace(k) != "" {
			httpReq.Header.Set(k, v)
		}
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		if errors.Is(err, errRedirectHostForbidden) {
			return HostHTTPErrForbidden
		}
		return HostHTTPErrNetwork
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxHostHTTPRespBytes+1))
	if err != nil {
		return HostHTTPErrNetwork
	}
	if len(respBody) > maxHostHTTPRespBytes {
		return HostHTTPErrTooLarge
	}

	flatHeaders := make(map[string]string, len(resp.Header))
	for k, vals := range resp.Header {
		if len(vals) > 0 {
			flatHeaders[k] = strings.Join(vals, ", ")
		}
	}

	outBytes, err := json.Marshal(hostHTTPResponsePayload{
		Status:  resp.StatusCode,
		Headers: flatHeaders,
		Body:    string(respBody),
	})
	if err != nil {
		return HostHTTPErrNetwork
	}

	if uint32(len(outBytes)) > respCap {
		return -int32(len(outBytes))
	}
	if !mem.Write(respPtr, outBytes) {
		return HostHTTPErrMemory
	}
	return int32(len(outBytes))
}

// Close releases all compiled modules and runtime resources.
func (h *Host) Close(ctx context.Context) error {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil
	}
	h.closed = true
	h.mu.Unlock()
	return h.runtime.Close(ctx)
}

// LoadModule compiles a WASM binary in memory and returns an executable WasmPlugin.
func (h *Host) LoadModule(ctx context.Context, manifest plugins.Manifest, wasmBytes []byte, timeout time.Duration) (*WasmPlugin, error) {
	return h.LoadModuleWithAllowedHosts(ctx, manifest, nil, wasmBytes, timeout)
}

// LoadModuleWithAllowedHosts compiles a WASM binary with an optional host allowlist for ambra.http_request.
func (h *Host) LoadModuleWithAllowedHosts(ctx context.Context, manifest plugins.Manifest, allowedHosts []string, wasmBytes []byte, timeout time.Duration) (*WasmPlugin, error) {
	manifest.BuiltIn = false
	if plugins.IsBuiltInID(manifest.ID) {
		return nil, fmt.Errorf("wasmhost: external plugin id %q cannot use reserved prefix %q", manifest.ID, plugins.BuiltInIDPrefix)
	}
	if len(wasmBytes) == 0 {
		return nil, errors.New("wasmhost: empty wasm binary")
	}
	if len(wasmBytes) > maxWasmFileBytes {
		return nil, fmt.Errorf("wasmhost: wasm binary exceeds maximum size of %d bytes", maxWasmFileBytes)
	}
	compiled, err := h.runtime.CompileModule(ctx, wasmBytes)
	if err != nil {
		return nil, fmt.Errorf("wasmhost: compile module %q: %w", manifest.ID, err)
	}
	if timeout <= 0 {
		timeout = h.defaultTimeout
	}
	if timeout > maxPluginTimeout {
		timeout = maxPluginTimeout
	}
	var cleanedHosts []string
	for _, host := range allowedHosts {
		if th := strings.TrimSpace(host); th != "" {
			cleanedHosts = append(cleanedHosts, th)
		}
	}
	return &WasmPlugin{
		host:         h,
		manifest:     manifest,
		allowedHosts: cleanedHosts,
		compiled:     compiled,
		timeout:      timeout,
		enabled:      manifest.Enabled,
		priority:     manifest.Priority,
		config:       make(map[string]any),
	}, nil
}

func readFileWithLimit(path string, maxBytes int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if fi, err := f.Stat(); err == nil && fi.Size() > maxBytes {
		return nil, fmt.Errorf("file %q exceeds limit of %d bytes", filepath.Base(path), maxBytes)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("file %q exceeds limit of %d bytes", filepath.Base(path), maxBytes)
	}
	return data, nil
}

func isWithinRealDir(realBase, realTarget string) bool {
	rel, err := filepath.Rel(realBase, realTarget)
	if err != nil {
		return false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return false
	}
	return true
}

// LoadFromDir scans <dir>/*/manifest.json and registers every plugin defining a "WasmFile" into reg.
// Individual plugin load failures are logged and skipped so one bad plugin cannot block others.
func (h *Host) LoadFromDir(ctx context.Context, dir string, reg *plugins.Registry) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("wasmhost: read plugin dir %q: %w", dir, err)
	}
	realRootDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return 0, fmt.Errorf("wasmhost: resolve plugin root dir %q: %w", dir, err)
	}

	loaded := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pluginDir := filepath.Join(dir, entry.Name())
		realPluginDir, err := filepath.EvalSymlinks(pluginDir)
		if err != nil {
			slog.Warn("wasmhost: resolve plugin dir failed, skipping", "dir", pluginDir, "err", err)
			continue
		}
		if !isWithinRealDir(realRootDir, realPluginDir) {
			slog.Warn("wasmhost: plugin dir symlink escapes plugins root, skipping", "dir", pluginDir)
			continue
		}
		manifestPath := filepath.Join(pluginDir, "manifest.json")
		if _, err := os.Lstat(manifestPath); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			slog.Warn("wasmhost: stat manifest failed, skipping", "manifest", manifestPath, "err", err)
			continue
		}
		realManifestPath, err := filepath.EvalSymlinks(manifestPath)
		if err != nil {
			slog.Warn("wasmhost: resolve manifest path failed, skipping", "manifest", manifestPath, "err", err)
			continue
		}
		if !isWithinRealDir(realPluginDir, realManifestPath) {
			slog.Warn("wasmhost: manifest symlink escapes plugin dir, skipping", "manifest", manifestPath)
			continue
		}
		raw, err := readFileWithLimit(realManifestPath, maxManifestBytes)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			slog.Warn("wasmhost: read manifest failed, skipping", "manifest", manifestPath, "err", err)
			continue
		}

		var pm PluginManifest
		if err := json.Unmarshal(raw, &pm); err != nil {
			slog.Warn("wasmhost: parse manifest failed, skipping", "manifest", manifestPath, "err", err)
			continue
		}
		if strings.TrimSpace(pm.WasmFile) == "" {
			continue
		}

		wasmPath := filepath.Clean(filepath.Join(pluginDir, pm.WasmFile))
		if !isWithinRealDir(pluginDir, wasmPath) {
			slog.Warn("wasmhost: invalid wasm path, skipping", "wasm_file", pm.WasmFile, "manifest", manifestPath)
			continue
		}
		realWasmPath, err := filepath.EvalSymlinks(wasmPath)
		if err != nil {
			slog.Warn("wasmhost: resolve wasm path failed, skipping", "wasm_path", wasmPath, "err", err)
			continue
		}
		if !isWithinRealDir(realPluginDir, realWasmPath) {
			slog.Warn("wasmhost: symlink escapes plugin dir, skipping", "wasm_path", wasmPath)
			continue
		}

		wasmBytes, err := readFileWithLimit(realWasmPath, maxWasmFileBytes)
		if err != nil {
			slog.Warn("wasmhost: read wasm file failed, skipping", "wasm_path", wasmPath, "err", err)
			continue
		}

		if pm.ID == "" {
			pm.ID = entry.Name()
		}
		if pm.Name == "" {
			pm.Name = pm.ID
		}
		pm.BuiltIn = false
		if plugins.IsBuiltInID(pm.ID) {
			slog.Warn("wasmhost: refusing plugin with reserved built-in prefix", "plugin_id", pm.ID, "manifest", manifestPath)
			continue
		}
		if reg != nil {
			if existing, ok := reg.Get(pm.ID); ok && existing.Manifest().BuiltIn {
				slog.Warn("wasmhost: refusing to overwrite built-in plugin", "plugin_id", pm.ID, "manifest", manifestPath)
				continue
			}
		}
		var timeout time.Duration
		if pm.TimeoutMs > 0 {
			ms := pm.TimeoutMs
			if ms > int(maxPluginTimeout/time.Millisecond) {
				ms = int(maxPluginTimeout / time.Millisecond)
			}
			timeout = time.Duration(ms) * time.Millisecond
		}

		wp, err := h.LoadModuleWithAllowedHosts(ctx, pm.Manifest, pm.AllowedHosts, wasmBytes, timeout)
		if err != nil {
			slog.Warn("wasmhost: compile plugin failed, skipping", "plugin_id", pm.ID, "err", err)
			continue
		}
		if reg != nil {
			reg.Register(wp)
		}
		loaded++
	}
	return loaded, nil
}

// WasmPlugin wraps a compiled WASM module implementing Plugin, MetadataProvider, StreamResolver, and SubtitleProvider.
type WasmPlugin struct {
	host         *Host
	manifest     plugins.Manifest
	allowedHosts []string
	compiled     wazero.CompiledModule
	timeout      time.Duration

	mu       sync.RWMutex
	enabled  bool
	priority int
	config   map[string]any
}

var (
	_ plugins.Plugin                = (*WasmPlugin)(nil)
	_ plugins.MetadataProvider      = (*WasmPlugin)(nil)
	_ plugins.StreamResolver        = (*WasmPlugin)(nil)
	_ plugins.SubtitleProvider      = (*WasmPlugin)(nil)
	_ plugins.Notifier              = (*WasmPlugin)(nil)
	_ plugins.DefaultConfigExporter = (*WasmPlugin)(nil)
)

// Manifest returns the plugin's metadata with current enabled status and priority.
func (p *WasmPlugin) Manifest() plugins.Manifest {
	p.mu.RLock()
	enabled := p.enabled
	priority := p.priority
	p.mu.RUnlock()
	m := p.manifest
	m.Enabled = enabled
	m.Priority = priority
	m.Kinds = append([]plugins.Kind(nil), m.Kinds...)
	return m
}

// DefaultConfig exports the active plugin configuration.
func (p *WasmPlugin) DefaultConfig() map[string]any {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make(map[string]any, len(p.config)+2)
	for k, v := range p.config {
		out[k] = v
	}
	out["enabled"] = p.enabled
	out["priority"] = p.priority
	return out
}

// Configure replaces the runtime configuration passed to the WASM module on each invocation.
func (p *WasmPlugin) Configure(cfg map[string]any) error {
	if cfg == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	copied := make(map[string]any, len(cfg))
	for k, v := range cfg {
		copied[k] = v
	}
	if v, ok := cfg["enabled"].(bool); ok {
		p.enabled = v
	} else if v, ok := cfg["Enabled"].(bool); ok {
		p.enabled = v
	}
	for _, k := range []string{"priority", "Priority"} {
		if raw, exists := cfg[k]; exists {
			switch n := raw.(type) {
			case int:
				p.priority = n
			case int64:
				p.priority = int(n)
			case float64:
				p.priority = int(n)
			}
			break
		}
	}
	p.config = copied
	return nil
}

func (p *WasmPlugin) supports(kind plugins.Kind) bool {
	p.mu.RLock()
	enabled := p.enabled
	p.mu.RUnlock()
	if !enabled {
		return false
	}
	if len(p.manifest.Kinds) == 0 {
		return true
	}
	for _, k := range p.manifest.Kinds {
		if k == kind {
			return true
		}
	}
	return false
}

// FetchMetadata invokes the WASM plugin using the WASI stdin/stdout JSON protocol for metadata lookup.
func (p *WasmPlugin) FetchMetadata(ctx context.Context, q plugins.MediaQuery) (*plugins.MetadataResult, error) {
	if !p.supports(plugins.KindMetadata) {
		return nil, nil
	}
	req := Request{
		Action: "fetch_metadata",
		Config: p.snapshotConfig(),
		Query:  &q,
	}
	out, err := p.invoke(ctx, req)
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(out)) == 0 {
		return nil, nil
	}
	var res plugins.MetadataResult
	if err := json.Unmarshal(out, &res); err != nil {
		return nil, fmt.Errorf("wasmhost: decode metadata result from %q: %w", p.manifest.ID, err)
	}
	return &res, nil
}

// ResolveStream invokes the WASM plugin using the WASI stdin/stdout JSON protocol for stream resolution.
func (p *WasmPlugin) ResolveStream(ctx context.Context, rawPath string) (*plugins.StreamResolveResult, error) {
	if !p.supports(plugins.KindStream) {
		return nil, nil
	}
	req := Request{
		Action:  "resolve_stream",
		Config:  p.snapshotConfig(),
		RawPath: rawPath,
	}
	out, err := p.invoke(ctx, req)
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(out)) == 0 {
		return nil, nil
	}
	var res plugins.StreamResolveResult
	if err := json.Unmarshal(out, &res); err != nil {
		return nil, fmt.Errorf("wasmhost: decode stream result from %q: %w", p.manifest.ID, err)
	}
	if u := strings.TrimSpace(res.URL); u != "" {
		if err := netutil.ValidateRemoteURL(u, true); err != nil {
			return nil, fmt.Errorf("wasmhost: plugin %q returned unsafe stream url: %w", p.manifest.ID, err)
		}
		res.URL = u
	}
	return &res, nil
}

// SearchSubtitles invokes the WASM plugin with Action="search_subtitles" when KindSubtitle is enabled.
func (p *WasmPlugin) SearchSubtitles(ctx context.Context, q plugins.SubtitleSearchQuery) ([]plugins.RemoteSubtitleInfo, error) {
	if !p.supports(plugins.KindSubtitle) {
		return nil, nil
	}
	req := Request{
		Action:        "search_subtitles",
		Config:        p.snapshotConfig(),
		SubtitleQuery: &q,
	}
	out, err := p.invoke(ctx, req)
	if err != nil {
		return nil, err
	}
	trimmed := bytes.TrimSpace(out)
	if len(trimmed) == 0 {
		return nil, nil
	}
	if trimmed[0] == '[' {
		var list []plugins.RemoteSubtitleInfo
		if err := json.Unmarshal(trimmed, &list); err != nil {
			return nil, fmt.Errorf("wasmhost: decode subtitle list from %q: %w", p.manifest.ID, err)
		}
		return list, nil
	}
	var wrapped struct {
		Subtitles []plugins.RemoteSubtitleInfo `json:"subtitles"`
		Results   []plugins.RemoteSubtitleInfo `json:"results"`
	}
	if err := json.Unmarshal(trimmed, &wrapped); err != nil {
		return nil, fmt.Errorf("wasmhost: decode subtitle response from %q: %w", p.manifest.ID, err)
	}
	if len(wrapped.Subtitles) > 0 {
		return wrapped.Subtitles, nil
	}
	return wrapped.Results, nil
}

// DownloadSubtitle invokes the WASM plugin with Action="download_subtitle" when KindSubtitle is enabled.
func (p *WasmPlugin) DownloadSubtitle(ctx context.Context, subtitleID string) (*plugins.SubtitleDownloadResult, error) {
	if !p.supports(plugins.KindSubtitle) {
		return nil, nil
	}
	req := Request{
		Action:     "download_subtitle",
		Config:     p.snapshotConfig(),
		SubtitleID: subtitleID,
	}
	out, err := p.invoke(ctx, req)
	if err != nil {
		return nil, err
	}
	trimmed := bytes.TrimSpace(out)
	if len(trimmed) == 0 {
		return nil, nil
	}
	var resp struct {
		Language      string `json:"language"`
		LanguageUpper string `json:"Language"`
		Format        string `json:"format"`
		FormatUpper   string `json:"Format"`
		Content       any    `json:"content"`
		ContentUpper  any    `json:"Content"`
		ContentBase64 string `json:"content_base64"`
	}
	if err := json.Unmarshal(trimmed, &resp); err != nil {
		return nil, fmt.Errorf("wasmhost: decode subtitle download from %q: %w", p.manifest.ID, err)
	}
	lang := strings.TrimSpace(resp.Language)
	if lang == "" {
		lang = strings.TrimSpace(resp.LanguageUpper)
	}
	format := strings.TrimSpace(resp.Format)
	if format == "" {
		format = strings.TrimSpace(resp.FormatUpper)
	}
	var content []byte
	if resp.ContentBase64 != "" {
		decoded, decErr := base64.StdEncoding.DecodeString(resp.ContentBase64)
		if decErr != nil {
			return nil, fmt.Errorf("wasmhost: decode base64 subtitle from %q: %w", p.manifest.ID, decErr)
		}
		content = decoded
	} else {
		rawContent := resp.Content
		if rawContent == nil {
			rawContent = resp.ContentUpper
		}
		if s, isStr := rawContent.(string); isStr {
			if decoded, decErr := base64.StdEncoding.DecodeString(s); decErr == nil && len(decoded) > 0 && !strings.Contains(s, "-->") {
				content = decoded
			} else {
				content = []byte(s)
			}
		}
	}
	return &plugins.SubtitleDownloadResult{
		Language: lang,
		Format:   format,
		Content:  content,
	}, nil
}

// Notify implements plugins.Notifier by invoking the WASM module's exported "ambra_notify"
// function when present (forwarding the JSON notification envelope via WASI stdin and/or linear memory).
// Modules that do not export "ambra_notify" (or disabled plugins) return nil cleanly.
func (p *WasmPlugin) Notify(ctx context.Context, event plugins.NotificationPayload) error {
	p.mu.RLock()
	enabled := p.enabled
	p.mu.RUnlock()
	if !enabled || p.compiled == nil {
		return nil
	}

	exported := p.compiled.ExportedFunctions()
	if _, ok := exported["ambra_notify"]; !ok {
		return nil
	}

	req := Request{
		Action:       "notify",
		Config:       p.snapshotConfig(),
		Notification: &event,
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("wasmhost: marshal notification: %w", err)
	}

	if p.host.sem != nil {
		select {
		case p.host.sem <- struct{}{}:
			defer func() { <-p.host.sem }()
		case <-ctx.Done():
			return fmt.Errorf("wasmhost: plugin %q cancelled waiting for execution slot: %w", p.manifest.ID, ctx.Err())
		}
	}

	ctxWithMeta := context.WithValue(ctx, wasmCallCtxKey{}, wasmCallContext{
		pluginID:     p.manifest.ID,
		allowedHosts: p.allowedHosts,
	})
	callCtx, cancel := context.WithTimeout(ctxWithMeta, p.timeout)
	defer cancel()

	stdout := &limitedBuffer{limit: maxWasmStdoutBytes, cancel: cancel}
	stderr := &limitedBuffer{limit: maxWasmStderrBytes, cancel: cancel}
	modCfg := wazero.NewModuleConfig().
		WithName("").
		WithStartFunctions().
		WithStdin(bytes.NewReader(payload)).
		WithStdout(stdout).
		WithStderr(stderr)

	mod, err := p.host.runtime.InstantiateModule(callCtx, p.compiled, modCfg)
	if err != nil {
		return fmt.Errorf("wasmhost: instantiate module %q for ambra_notify: %w", p.manifest.ID, err)
	}
	defer func() {
		_ = mod.Close(context.Background())
	}()

	fn := mod.ExportedFunction("ambra_notify")
	if fn == nil {
		return nil
	}

	var callRes []uint64
	var callErr error
	paramTypes := fn.Definition().ParamTypes()
	switch len(paramTypes) {
	case 0:
		callRes, callErr = fn.Call(callCtx)
	case 2:
		var ptr uint32
		allocated := false
		for _, allocName := range []string{"ambra_alloc", "malloc", "alloc"} {
			if allocFn := mod.ExportedFunction(allocName); allocFn != nil && len(allocFn.Definition().ParamTypes()) == 1 {
				if aRes, aErr := allocFn.Call(callCtx, uint64(len(payload))); aErr == nil && len(aRes) > 0 {
					ptr = uint32(aRes[0])
					allocated = true
					break
				}
			}
		}
		if mem := mod.Memory(); mem != nil {
			if !allocated && mem.Size() >= uint32(len(payload)) {
				// Use top of initial memory page if no allocator is exported.
				ptr = mem.Size() - uint32(len(payload))
			}
			if !mem.Write(ptr, payload) {
				return fmt.Errorf("wasmhost: write notification payload to %q memory failed", p.manifest.ID)
			}
		}
		callRes, callErr = fn.Call(callCtx, uint64(ptr), uint64(len(payload)))
	default:
		return fmt.Errorf("wasmhost: unsupported ambra_notify signature on plugin %q (%d params)", p.manifest.ID, len(paramTypes))
	}

	if callErr != nil {
		var exitErr *sys.ExitError
		if errors.As(callErr, &exitErr) && exitErr.ExitCode() == 0 {
			return nil
		}
		if callCtx.Err() != nil {
			return fmt.Errorf("wasmhost: plugin %q ambra_notify timed out or cancelled: %w", p.manifest.ID, callCtx.Err())
		}
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg != "" {
			return fmt.Errorf("wasmhost: plugin %q ambra_notify failed (%s): %w", p.manifest.ID, errMsg, callErr)
		}
		return fmt.Errorf("wasmhost: plugin %q ambra_notify failed: %w", p.manifest.ID, callErr)
	}
	if len(callRes) > 0 && int32(callRes[0]) < 0 {
		return fmt.Errorf("wasmhost: plugin %q ambra_notify returned error code %d", p.manifest.ID, int32(callRes[0]))
	}
	return nil
}

func (p *WasmPlugin) snapshotConfig() map[string]any {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if len(p.config) == 0 {
		return nil
	}
	copied := make(map[string]any, len(p.config))
	for k, v := range p.config {
		copied[k] = v
	}
	return copied
}

func (p *WasmPlugin) invoke(ctx context.Context, req Request) ([]byte, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("wasmhost: marshal request: %w", err)
	}

	if p.host.sem != nil {
		select {
		case p.host.sem <- struct{}{}:
			defer func() { <-p.host.sem }()
		case <-ctx.Done():
			return nil, fmt.Errorf("wasmhost: plugin %q cancelled waiting for execution slot: %w", p.manifest.ID, ctx.Err())
		}
	}

	ctxWithMeta := context.WithValue(ctx, wasmCallCtxKey{}, wasmCallContext{
		pluginID:     p.manifest.ID,
		allowedHosts: p.allowedHosts,
	})
	callCtx, cancel := context.WithTimeout(ctxWithMeta, p.timeout)
	defer cancel()

	stdout := &limitedBuffer{limit: maxWasmStdoutBytes, cancel: cancel}
	stderr := &limitedBuffer{limit: maxWasmStderrBytes, cancel: cancel}
	modCfg := wazero.NewModuleConfig().
		WithName("").
		WithStdin(bytes.NewReader(payload)).
		WithStdout(stdout).
		WithStderr(stderr)

	mod, err := p.host.runtime.InstantiateModule(callCtx, p.compiled, modCfg)
	if mod != nil {
		_ = mod.Close(context.Background())
	}
	if stdout.isTruncated() {
		return nil, fmt.Errorf("wasmhost: plugin %q stdout exceeded %d bytes", p.manifest.ID, maxWasmStdoutBytes)
	}
	if err != nil {
		var exitErr *sys.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 0 {
			return stdout.Bytes(), nil
		}
		if stderr.isTruncated() {
			return nil, fmt.Errorf("wasmhost: plugin %q stderr exceeded %d bytes", p.manifest.ID, maxWasmStderrBytes)
		}
		if callCtx.Err() != nil {
			return nil, fmt.Errorf("wasmhost: plugin %q execution timed out or cancelled: %w", p.manifest.ID, callCtx.Err())
		}
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg != "" {
			return nil, fmt.Errorf("wasmhost: plugin %q failed (%s): %w", p.manifest.ID, errMsg, err)
		}
		return nil, fmt.Errorf("wasmhost: plugin %q failed: %w", p.manifest.ID, err)
	}
	return stdout.Bytes(), nil
}
