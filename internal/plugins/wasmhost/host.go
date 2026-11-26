package wasmhost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ambra-media/ambra/internal/pkg/netutil"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/tetratelabs/wazero"
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
	defaultMaxConcurrentCalls = 8
)

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
	WasmFile  string `json:"WasmFile"`
	TimeoutMs int    `json:"TimeoutMs,omitempty"`
}

// Request is the JSON envelope written to a WASM plugin's WASI stdin.
type Request struct {
	Action  string              `json:"action"`
	Config  map[string]any      `json:"config,omitempty"`
	Query   *plugins.MediaQuery `json:"query,omitempty"`
	RawPath string              `json:"raw_path,omitempty"`
}

// Host owns a pure-Go wazero runtime for executing WASI-based Ambra plugins.
type Host struct {
	runtime        wazero.Runtime
	defaultTimeout time.Duration
	sem            chan struct{}

	mu     sync.Mutex
	closed bool
}

// NewHost initializes a pure-Go wazero runtime with WASI preview1 support,
// memory page limit (64 MiB), concurrency limiting, and context-cancellation enforcement.
func NewHost(ctx context.Context) (*Host, error) {
	rtCfg := wazero.NewRuntimeConfig().
		WithCloseOnContextDone(true).
		WithMemoryLimitPages(maxWasmMemoryPages)
	rt := wazero.NewRuntimeWithConfig(ctx, rtCfg)
	if _, err := wasi_snapshot_preview1.Instantiate(ctx, rt); err != nil {
		_ = rt.Close(ctx)
		return nil, fmt.Errorf("wasmhost: instantiate wasi: %w", err)
	}
	return &Host{
		runtime:        rt,
		defaultTimeout: DefaultTimeout,
		sem:            make(chan struct{}, defaultMaxConcurrentCalls),
	}, nil
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
	return &WasmPlugin{
		host:     h,
		manifest: manifest,
		compiled: compiled,
		timeout:  timeout,
		enabled:  manifest.Enabled,
		config:   make(map[string]any),
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

		wp, err := h.LoadModule(ctx, pm.Manifest, wasmBytes, timeout)
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

// WasmPlugin wraps a compiled WASM module implementing Plugin, MetadataProvider, and StreamResolver.
type WasmPlugin struct {
	host     *Host
	manifest plugins.Manifest
	compiled wazero.CompiledModule
	timeout  time.Duration

	mu      sync.RWMutex
	enabled bool
	config  map[string]any
}

var (
	_ plugins.Plugin           = (*WasmPlugin)(nil)
	_ plugins.MetadataProvider = (*WasmPlugin)(nil)
	_ plugins.StreamResolver   = (*WasmPlugin)(nil)
)

// Manifest returns the plugin's static metadata with current enabled status.
func (p *WasmPlugin) Manifest() plugins.Manifest {
	p.mu.RLock()
	enabled := p.enabled
	p.mu.RUnlock()
	m := p.manifest
	m.Enabled = enabled
	return m
}

// Configure replaces the runtime configuration passed to the WASM module on each invocation.
func (p *WasmPlugin) Configure(cfg map[string]any) error {
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

	callCtx, cancel := context.WithTimeout(ctx, p.timeout)
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
