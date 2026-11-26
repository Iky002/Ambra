package transcode

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ambra-media/ambra/internal/pkg/netutil"
)

const loopbackDNSLookupTimeout = 5 * time.Second

// LoopbackProxyOptions configures a local 127.0.0.1:0 stream proxy for external FFmpeg/FFprobe processes.
type LoopbackProxyOptions struct {
	TargetURL        string
	Headers          map[string]string
	AllowPrivateLAN  bool
	DisallowLoopback bool
	SingleUse        bool
	Client           *http.Client // optional override for unit tests
}

// LoopbackStreamProxy exposes a short-lived, token-guarded http://127.0.0.1:<port>/stream/<token>
// endpoint that fetches TargetURL in Go using pinned-IP SSRF-safe HTTP transport and custom headers,
// eliminating secondary DNS rebinding windows in external ffmpeg/ffprobe child processes.
type LoopbackStreamProxy struct {
	proxyURL  string
	ln        net.Listener
	srv       *http.Server
	baseCtx   context.Context
	cancelCtx context.CancelFunc
	closeOnce sync.Once
}

// URL returns the local loopback HTTP URL to pass to ffmpeg/ffprobe.
func (p *LoopbackStreamProxy) URL() string {
	if p == nil {
		return ""
	}
	return p.proxyURL
}

// Close immediately shuts down the temporary loopback listener and cancels in-flight upstream streams.
func (p *LoopbackStreamProxy) Close() error {
	if p == nil {
		return nil
	}
	var closeErr error
	p.closeOnce.Do(func() {
		if p.cancelCtx != nil {
			p.cancelCtx()
		}
		if p.srv != nil {
			_ = p.srv.Close()
		}
		if p.ln != nil {
			closeErr = p.ln.Close()
		}
	})
	return closeErr
}

// StartLoopbackStreamProxy validates TargetURL, pins its initial resolved IP addresses,
// and starts a temporary 127.0.0.1:0 HTTP proxy protected by a random cryptographic token.
func StartLoopbackStreamProxy(opts LoopbackProxyOptions) (*LoopbackStreamProxy, error) {
	rawTarget := strings.TrimSpace(opts.TargetURL)
	if rawTarget == "" {
		return nil, errors.New("loopback proxy: empty target url")
	}
	if hasControlChars(rawTarget) {
		return nil, errors.New("loopback proxy: forbidden control characters in target url")
	}
	parsedTarget, err := url.Parse(rawTarget)
	if err != nil {
		return nil, fmt.Errorf("loopback proxy: invalid target url: %w", err)
	}
	scheme := strings.ToLower(parsedTarget.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("loopback proxy: unsupported url scheme %q", parsedTarget.Scheme)
	}
	targetHost := strings.TrimSpace(parsedTarget.Hostname())
	if targetHost == "" {
		return nil, errors.New("loopback proxy: target url host is empty")
	}

	client := opts.Client
	if client == nil {
		var err error
		client, err = newPinnedSafeStreamClient(targetHost, opts.AllowPrivateLAN, opts.DisallowLoopback)
		if err != nil {
			return nil, err
		}
	}

	var tokBytes [32]byte
	if _, err := rand.Read(tokBytes[:]); err != nil {
		return nil, fmt.Errorf("loopback proxy: generate token: %w", err)
	}
	token := hex.EncodeToString(tokBytes[:])

	suffix := safeURLMediaSuffix(parsedTarget.Path)
	expectedPath := "/stream/" + token + suffix

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("loopback proxy: listen loopback: %w", err)
	}

	sanitizedHeaders := sanitizeUpstreamHeaders(opts.Headers)
	baseCtx, cancelCtx := context.WithCancel(context.Background())
	var consumed atomic.Bool

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.URL.Path), []byte(expectedPath)) != 1 {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if opts.SingleUse && !consumed.CompareAndSwap(false, true) {
			http.Error(w, "token already consumed", http.StatusForbidden)
			return
		}

		reqCtx, reqCancel := context.WithCancel(r.Context())
		defer reqCancel()
		go func() {
			select {
			case <-baseCtx.Done():
				reqCancel()
			case <-reqCtx.Done():
			}
		}()

		upReq, err := http.NewRequestWithContext(reqCtx, r.Method, rawTarget, nil)
		if err != nil {
			http.Error(w, "bad upstream request", http.StatusBadGateway)
			return
		}

		upReq.Header.Set("User-Agent", defaultHTTPUserAgent)
		for _, passthroughHdr := range []string{"Range", "If-Range", "Icy-MetaData"} {
			if v := strings.TrimSpace(r.Header.Get(passthroughHdr)); v != "" && !hasControlChars(v) {
				upReq.Header.Set(passthroughHdr, v)
			}
		}
		for k, v := range sanitizedHeaders {
			upReq.Header.Set(k, v)
		}

		resp, err := client.Do(upReq)
		if err != nil {
			http.Error(w, "upstream stream error", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		for _, hName := range []string{
			"Content-Type", "Content-Length", "Content-Range",
			"Accept-Ranges", "ETag", "Last-Modified", "Icy-Metaint",
		} {
			if val := resp.Header.Get(hName); val != "" {
				w.Header().Set(hName, val)
			}
		}
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", "application/octet-stream")
		}
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(resp.StatusCode)
		if r.Method == http.MethodHead {
			return
		}

		flusher, canFlush := w.(http.Flusher)
		buf := make([]byte, 32*1024)
		for {
			n, readErr := resp.Body.Read(buf)
			if n > 0 {
				if _, writeErr := w.Write(buf[:n]); writeErr != nil {
					return
				}
				if canFlush {
					flusher.Flush()
				}
			}
			if readErr != nil {
				return
			}
		}
	})

	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	p := &LoopbackStreamProxy{
		proxyURL:  "http://" + ln.Addr().String() + expectedPath,
		ln:        ln,
		srv:       srv,
		baseCtx:   baseCtx,
		cancelCtx: cancelCtx,
	}
	go func() {
		_ = srv.Serve(ln)
	}()
	return p, nil
}

func resolveAndPinHost(ctx context.Context, host string, allowPrivateLAN, allowLoopback bool) ([]string, error) {
	if addr, err := netip.ParseAddr(host); err == nil {
		unmapped := addr.WithZone("").Unmap()
		if !allowLoopback && unmapped.IsLoopback() {
			return nil, fmt.Errorf("blocked loopback address %s", unmapped)
		}
		if err := netutil.ValidateIP(net.IP(unmapped.AsSlice()), allowPrivateLAN); err != nil {
			return nil, err
		}
		return []string{unmapped.String()}, nil
	}

	lowerHost := strings.ToLower(strings.TrimSuffix(host, "."))
	if !allowLoopback && (lowerHost == "localhost" || strings.HasSuffix(lowerHost, ".localhost")) {
		return nil, fmt.Errorf("blocked loopback hostname %q", host)
	}

	ipAddrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolve host %q: %w", host, err)
	}
	if len(ipAddrs) == 0 {
		return nil, fmt.Errorf("host %q resolved to no ip addresses", host)
	}

	pinned := make([]string, 0, len(ipAddrs))
	for _, ia := range ipAddrs {
		addr, ok := netip.AddrFromSlice(ia.IP)
		if !ok {
			return nil, fmt.Errorf("host %q resolved to invalid ip %q", host, ia.IP.String())
		}
		unmapped := addr.WithZone("").Unmap()
		if !allowLoopback && unmapped.IsLoopback() {
			return nil, fmt.Errorf("host %q: blocked loopback address %s", host, unmapped)
		}
		if err := netutil.ValidateIP(net.IP(unmapped.AsSlice()), allowPrivateLAN); err != nil {
			return nil, fmt.Errorf("host %q: %w", host, err)
		}
		pinned = append(pinned, unmapped.String())
	}
	return pinned, nil
}

func newPinnedSafeStreamClient(initialHost string, allowPrivateLAN, disallowLoopback bool) (*http.Client, error) {
	dnsCtx, cancel := context.WithTimeout(context.Background(), loopbackDNSLookupTimeout)
	defer cancel()

	pinnedIPs, err := resolveAndPinHost(dnsCtx, initialHost, allowPrivateLAN, !disallowLoopback)
	if err != nil {
		return nil, err
	}

	var baseClient *http.Client
	if disallowLoopback {
		baseClient = netutil.NewSafeHTTPClientNoLoopback(0, allowPrivateLAN)
	} else {
		baseClient = netutil.NewSafeHTTPClient(0, allowPrivateLAN)
	}

	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	baseTransport, ok := baseClient.Transport.(*http.Transport)
	var transport *http.Transport
	if ok && baseTransport != nil {
		transport = baseTransport.Clone()
	} else {
		transport = &http.Transport{
			TLSHandshakeTimeout:   10 * time.Second,
			IdleConnTimeout:       90 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
		}
	}
	fallbackDial := transport.DialContext

	normInitialHost := strings.ToLower(strings.TrimSuffix(initialHost, "."))
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, splitErr := net.SplitHostPort(addr)
		if splitErr != nil {
			return nil, fmt.Errorf("invalid dial address %q: %w", addr, splitErr)
		}
		if strings.ToLower(strings.TrimSuffix(host, ".")) == normInitialHost {
			var lastErr error
			for _, ip := range pinnedIPs {
				conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(ip, port))
				if dialErr == nil {
					return conn, nil
				}
				lastErr = dialErr
			}
			if lastErr != nil {
				return nil, lastErr
			}
			return nil, fmt.Errorf("unable to dial pinned host %q", addr)
		}
		if fallbackDial != nil {
			return fallbackDial(ctx, network, addr)
		}
		return nil, fmt.Errorf("unexpected unpinned dial host %q", host)
	}
	baseClient.Transport = transport

	baseCheckRedirect := baseClient.CheckRedirect
	baseClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if baseCheckRedirect != nil {
			if err := baseCheckRedirect(req, via); err != nil {
				return err
			}
		}
		if req.URL == nil {
			return errors.New("redirect target url is nil")
		}
		if len(via) > 0 && via[len(via)-1].URL != nil {
			prev := via[len(via)-1].URL
			if !strings.EqualFold(prev.Host, req.URL.Host) {
				req.Header.Del("Referer")
				// Cross-host redirects must NEVER target private LAN, loopback, or cloud metadata.
				if err := netutil.ValidateRemoteURLNoLoopback(req.URL.String(), false); err != nil {
					return fmt.Errorf("blocked redirect to non-public address: %w", err)
				}
			}
		}
		return nil
	}

	return baseClient, nil
}

func sanitizeUpstreamHeaders(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for rawK, rawV := range in {
		k := http.CanonicalHeaderKey(strings.TrimSpace(rawK))
		v := strings.TrimSpace(rawV)
		if k == "" || v == "" || hasControlChars(k) || hasControlChars(v) {
			continue
		}
		switch strings.ToLower(k) {
		case "host", "connection", "keep-alive", "proxy-authenticate", "proxy-authorization",
			"te", "trailer", "transfer-encoding", "upgrade", "content-length":
			continue
		}
		out[k] = v
	}
	return out
}

func safeURLMediaSuffix(urlPath string) string {
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(urlPath), "."))
	switch ext {
	case "mkv", "mp4", "m4v", "mov", "avi", "ts", "m2ts", "webm", "flv",
		"mp3", "flac", "m4a", "aac", "ogg", "opus", "wav", "wma",
		"srt", "ass", "ssa", "vtt", "sup", "m3u8":
		return "/media." + ext
	default:
		return ""
	}
}
