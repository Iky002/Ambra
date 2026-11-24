package wasmhost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"github.com/tetratelabs/wazero/sys"
)

const DefaultTimeout = 5 * time.Second

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

	mu     sync.Mutex
	closed bool
}

// NewHost initializes a pure-Go wazero runtime with WASI preview1 support and context-cancellation enforcement.
func NewHost(ctx context.Context) (*Host, error) {
	rtCfg := wazero.NewRuntimeConfig().WithCloseOnContextDone(true)
	rt := wazero.NewRuntimeWithConfig(ctx, rtCfg)
	if _, err := wasi_snapshot_preview1.Instantiate(ctx, rt); err != nil {
		_ = rt.Close(ctx)
		return nil, fmt.Errorf("wasmhost: instantiate wasi: %w", err)
	}
	return &Host{
		runtime:        rt,
		defaultTimeout: DefaultTimeout,
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
	if len(wasmBytes) == 0 {
		return nil, errors.New("wasmhost: empty wasm binary")
	}
	compiled, err := h.runtime.CompileModule(ctx, wasmBytes)
	if err != nil {
		return nil, fmt.Errorf("wasmhost: compile module %q: %w", manifest.ID, err)
	}
	if timeout <= 0 {
		timeout = h.defaultTimeout
	}
	return &WasmPlugin{
		host:     h,
		manifest: manifest,
		compiled: compiled,
		timeout:  timeout,
		config:   make(map[string]any),
	}, nil
}

// LoadFromDir scans <dir>/*/manifest.json and registers every plugin defining a "WasmFile" into reg.
func (h *Host) LoadFromDir(ctx context.Context, dir string, reg *plugins.Registry) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("wasmhost: read plugin dir %q: %w", dir, err)
	}

	loaded := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pluginDir := filepath.Join(dir, entry.Name())
		manifestPath := filepath.Join(pluginDir, "manifest.json")
		raw, err := os.ReadFile(manifestPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return loaded, fmt.Errorf("wasmhost: read manifest %q: %w", manifestPath, err)
		}

		var pm PluginManifest
		if err := json.Unmarshal(raw, &pm); err != nil {
			return loaded, fmt.Errorf("wasmhost: parse manifest %q: %w", manifestPath, err)
		}
		if strings.TrimSpace(pm.WasmFile) == "" {
			continue
		}

		wasmPath := filepath.Clean(filepath.Join(pluginDir, pm.WasmFile))
		rel, err := filepath.Rel(pluginDir, wasmPath)
		if err != nil || strings.HasPrefix(rel, "..") {
			return loaded, fmt.Errorf("wasmhost: invalid wasm path %q in %q", pm.WasmFile, manifestPath)
		}

		wasmBytes, err := os.ReadFile(wasmPath)
		if err != nil {
			return loaded, fmt.Errorf("wasmhost: read wasm file %q: %w", wasmPath, err)
		}

		if pm.ID == "" {
			pm.ID = entry.Name()
		}
		if pm.Name == "" {
			pm.Name = pm.ID
		}
		var timeout time.Duration
		if pm.TimeoutMs > 0 {
			timeout = time.Duration(pm.TimeoutMs) * time.Millisecond
		}

		wp, err := h.LoadModule(ctx, pm.Manifest, wasmBytes, timeout)
		if err != nil {
			return loaded, err
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

	mu     sync.RWMutex
	config map[string]any
}

var (
	_ plugins.Plugin           = (*WasmPlugin)(nil)
	_ plugins.MetadataProvider = (*WasmPlugin)(nil)
	_ plugins.StreamResolver   = (*WasmPlugin)(nil)
)

// Manifest returns the plugin's static metadata.
func (p *WasmPlugin) Manifest() plugins.Manifest {
	return p.manifest
}

// Configure replaces the runtime configuration passed to the WASM module on each invocation.
func (p *WasmPlugin) Configure(cfg map[string]any) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	copied := make(map[string]any, len(cfg))
	for k, v := range cfg {
		copied[k] = v
	}
	p.config = copied
	return nil
}

// FetchMetadata invokes the WASM plugin using the WASI stdin/stdout JSON protocol for metadata lookup.
func (p *WasmPlugin) FetchMetadata(ctx context.Context, q plugins.MediaQuery) (*plugins.MetadataResult, error) {
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

	callCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	var stdout, stderr bytes.Buffer
	modCfg := wazero.NewModuleConfig().
		WithName("").
		WithStdin(bytes.NewReader(payload)).
		WithStdout(&stdout).
		WithStderr(&stderr)

	mod, err := p.host.runtime.InstantiateModule(callCtx, p.compiled, modCfg)
	if mod != nil {
		_ = mod.Close(context.Background())
	}
	if err != nil {
		var exitErr *sys.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 0 {
			return stdout.Bytes(), nil
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
