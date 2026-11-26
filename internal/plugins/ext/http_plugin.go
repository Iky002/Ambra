package ext

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ambra-media/ambra/internal/pkg/netutil"
	"github.com/ambra-media/ambra/internal/plugins"
)

const (
	defaultTimeout          = 5 * time.Second
	maxTimeout              = 60 * time.Second
	maxTimeoutMs            = 60_000
	defaultFailureThreshold = 3
	defaultResetTimeout     = 30 * time.Second
	maxResponseBytes        = 16 << 20 // 16 MiB
)

// ErrCircuitOpen is returned when the plugin's circuit breaker is tripped.
var ErrCircuitOpen = errors.New("external plugin circuit breaker open")

// Compile-time interface assertions.
var (
	_ plugins.Plugin           = (*HTTPPlugin)(nil)
	_ plugins.MetadataProvider = (*HTTPPlugin)(nil)
	_ plugins.StreamResolver   = (*HTTPPlugin)(nil)
)

// HTTPPlugin proxies MetadataProvider and StreamResolver calls to an external HTTP service
// with strict timeouts and a consecutive-failure circuit breaker.
type HTTPPlugin struct {
	mu sync.RWMutex

	id          string
	name        string
	version     string
	description string
	author      string
	endpoint    string
	apiKey      string
	enabled     bool
	priority    int
	kinds       []plugins.Kind

	timeout time.Duration
	client  *http.Client

	failureThreshold int
	resetTimeout     time.Duration
	consecutiveFails int
	circuitOpenUntil time.Time
	probeInFlight    bool
	nowFunc          func() time.Time
}

// NewHTTPPlugin validates m and returns a ready-to-use HTTPPlugin.
func NewHTTPPlugin(m ExternalManifest) (*HTTPPlugin, error) {
	id := strings.TrimSpace(m.ID)
	if id == "" {
		return nil, errors.New("manifest Id is required")
	}
	if plugins.IsBuiltInID(id) {
		return nil, fmt.Errorf("manifest Id %q cannot use reserved prefix %q", id, plugins.BuiltInIDPrefix)
	}
	name := strings.TrimSpace(m.Name)
	if name == "" {
		return nil, errors.New("manifest Name is required")
	}
	endpoint, err := validateEndpoint(m.Endpoint)
	if err != nil {
		return nil, err
	}

	timeout := defaultTimeout
	if m.TimeoutMs > 0 {
		ms := m.TimeoutMs
		if ms > maxTimeoutMs {
			ms = maxTimeoutMs
		}
		timeout = time.Duration(ms) * time.Millisecond
	}

	enabled := true
	if m.Enabled != nil {
		enabled = *m.Enabled
	}

	kinds := append([]plugins.Kind(nil), m.Kinds...)
	if len(kinds) == 0 {
		kinds = []plugins.Kind{plugins.KindMetadata, plugins.KindStream}
	}

	return &HTTPPlugin{
		id:               id,
		name:             name,
		version:          m.Version,
		description:      m.Description,
		author:           m.Author,
		endpoint:         endpoint,
		apiKey:           strings.TrimSpace(m.APIKey),
		enabled:          enabled,
		priority:         m.Priority,
		kinds:            kinds,
		timeout:          timeout,
		client:           netutil.NewSafeHTTPClient(timeout, true),
		failureThreshold: defaultFailureThreshold,
		resetTimeout:     defaultResetTimeout,
		nowFunc:          time.Now,
	}, nil
}

// SetHTTPClient overrides the HTTP client used for external plugin requests (useful in tests).
func (p *HTTPPlugin) SetHTTPClient(c *http.Client) {
	if c == nil {
		return
	}
	p.mu.Lock()
	p.client = c
	p.mu.Unlock()
}

// Manifest returns the current plugin manifest.
func (p *HTTPPlugin) Manifest() plugins.Manifest {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return plugins.Manifest{
		ID:          p.id,
		Name:        p.name,
		Version:     p.version,
		Description: p.description,
		Author:      p.author,
		BuiltIn:     false,
		Enabled:     p.enabled,
		Priority:    p.priority,
		Kinds:       append([]plugins.Kind(nil), p.kinds...),
	}
}

func urlHostLower(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(u.Host))
}

// Configure dynamically updates Endpoint, APIKey, Enabled, Priority, and TimeoutMs.
func (p *HTTPPlugin) Configure(cfg map[string]any) error {
	if cfg == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	hasNewAPIKey := false
	if raw, ok := lookupString(cfg, "APIKey", "ApiKey", "api_key"); ok {
		p.apiKey = strings.TrimSpace(raw)
		hasNewAPIKey = true
	}
	if raw, ok := lookupString(cfg, "Endpoint", "endpoint"); ok {
		ep, err := validateEndpoint(raw)
		if err != nil {
			return err
		}
		if urlHostLower(ep) != urlHostLower(p.endpoint) && !hasNewAPIKey {
			p.apiKey = ""
		}
		p.endpoint = ep
	}
	if b, ok := lookupBool(cfg, "Enabled", "enabled"); ok {
		p.enabled = b
	}
	if prio, ok, err := lookupInt(cfg, "Priority", "priority"); err != nil {
		return err
	} else if ok {
		p.priority = prio
	}
	if tms, ok, err := lookupInt(cfg, "TimeoutMs", "timeout_ms"); err != nil {
		return err
	} else if ok {
		if tms <= 0 {
			return errors.New("TimeoutMs must be positive")
		}
		if tms > maxTimeoutMs {
			tms = maxTimeoutMs
		}
		p.timeout = time.Duration(tms) * time.Millisecond
		p.client = netutil.NewSafeHTTPClient(p.timeout, true)
	}
	return nil
}

// FetchMetadata sends a JSON POST to <Endpoint>/metadata and parses MetadataResult.
func (p *HTTPPlugin) FetchMetadata(ctx context.Context, q plugins.MediaQuery) (*plugins.MetadataResult, error) {
	if !p.supports(plugins.KindMetadata) {
		return nil, nil
	}
	var out plugins.MetadataResult
	ok, err := p.postJSON(ctx, "/metadata", q, &out)
	if err != nil || !ok {
		return nil, err
	}
	return &out, nil
}

// ResolveStream sends a JSON POST to <Endpoint>/resolve and parses StreamResolveResult.
func (p *HTTPPlugin) ResolveStream(ctx context.Context, rawPath string) (*plugins.StreamResolveResult, error) {
	if !p.supports(plugins.KindStream) {
		return nil, nil
	}
	reqBody := map[string]string{"path": rawPath}
	var out plugins.StreamResolveResult
	ok, err := p.postJSON(ctx, "/resolve", reqBody, &out)
	if err != nil || !ok {
		return nil, err
	}
	resolvedURL := strings.TrimSpace(out.URL)
	if resolvedURL == "" {
		return nil, nil
	}
	if err := netutil.ValidateRemoteURL(resolvedURL, true); err != nil {
		return nil, fmt.Errorf("plugin %s returned unsafe stream url: %w", p.id, err)
	}
	out.URL = resolvedURL
	return &out, nil
}

// CircuitState reports whether the circuit breaker is currently open and the consecutive failure count.
func (p *HTTPPlugin) CircuitState() (open bool, consecutiveFailures int) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.nowFunc().Before(p.circuitOpenUntil), p.consecutiveFails
}

func (p *HTTPPlugin) supports(kind plugins.Kind) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if !p.enabled {
		return false
	}
	for _, k := range p.kinds {
		if k == kind {
			return true
		}
	}
	return false
}

func (p *HTTPPlugin) postJSON(ctx context.Context, suffix string, payload any, dst any) (bool, error) {
	var isProbe bool
	p.mu.Lock()
	now := p.nowFunc()
	if !p.circuitOpenUntil.IsZero() {
		if now.Before(p.circuitOpenUntil) {
			p.mu.Unlock()
			return false, ErrCircuitOpen
		}
		// Half-open state: allow only one concurrent probe request.
		if p.probeInFlight {
			p.mu.Unlock()
			return false, ErrCircuitOpen
		}
		p.probeInFlight = true
		isProbe = true
	}
	endpoint := p.endpoint
	apiKey := p.apiKey
	timeout := p.timeout
	client := p.client
	p.mu.Unlock()

	if isProbe {
		defer func() {
			p.mu.Lock()
			p.probeInFlight = false
			p.mu.Unlock()
		}()
	}

	rawReq, err := json.Marshal(payload)
	if err != nil {
		return false, fmt.Errorf("marshal plugin request: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint+suffix, bytes.NewReader(rawReq))
	if err != nil {
		return false, fmt.Errorf("build plugin request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("X-Api-Key", apiKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		p.recordFailure()
		return false, fmt.Errorf("plugin %s request failed: %w", p.id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		p.recordSuccess()
		return false, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		p.recordFailure()
		return false, fmt.Errorf("plugin %s returned HTTP %d", p.id, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		p.recordFailure()
		return false, fmt.Errorf("read plugin %s response: %w", p.id, err)
	}
	if len(bytes.TrimSpace(body)) == 0 {
		p.recordSuccess()
		return false, nil
	}
	if err := json.Unmarshal(body, dst); err != nil {
		p.recordFailure()
		return false, fmt.Errorf("decode plugin %s response: %w", p.id, err)
	}

	p.recordSuccess()
	return true, nil
}

func (p *HTTPPlugin) recordFailure() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.probeInFlight = false
	p.consecutiveFails++
	if p.consecutiveFails >= p.failureThreshold {
		p.circuitOpenUntil = p.nowFunc().Add(p.resetTimeout)
	}
}

func (p *HTTPPlugin) recordSuccess() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.probeInFlight = false
	p.consecutiveFails = 0
	p.circuitOpenUntil = time.Time{}
}

func validateEndpoint(raw string) (string, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
	if trimmed == "" {
		return "", errors.New("endpoint is required")
	}
	u, err := url.Parse(trimmed)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("invalid endpoint %q: must be http(s) URL", raw)
	}
	return trimmed, nil
}

func lookupString(m map[string]any, keys ...string) (string, bool) {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok {
				return s, true
			}
		}
	}
	return "", false
}

func lookupBool(m map[string]any, keys ...string) (bool, bool) {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if b, ok := v.(bool); ok {
				return b, true
			}
		}
	}
	return false, false
}

func lookupInt(m map[string]any, keys ...string) (int, bool, error) {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			switch n := v.(type) {
			case int:
				return n, true, nil
			case int64:
				return int(n), true, nil
			case float64:
				return int(n), true, nil
			default:
				return 0, false, fmt.Errorf("field %s must be an integer", k)
			}
		}
	}
	return 0, false, nil
}
