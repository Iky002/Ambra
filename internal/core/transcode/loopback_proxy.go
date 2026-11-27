package transcode

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/ambra-media/ambra/internal/pkg/netutil"
)

const (
	loopbackDNSLookupTimeout = 5 * time.Second
	maxHLSManifestBytes      = 4 * 1024 * 1024 // 4 MiB cap on HLS manifest bodies
	manifestSniffBytes       = 512
)

var (
	reHLSTagQuotedURI   = regexp.MustCompile(`(?i)\bURI="([^"]*)"`)
	reHLSTagUnquotedURI = regexp.MustCompile(`(?i)\bURI=([^",\s]+)`)
)

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
// eliminating secondary DNS rebinding windows in external ffmpeg/ffprobe child processes and rewriting
// HLS (.m3u8) sub-playlists, segments, #EXT-X-KEY, and #EXT-X-MAP URIs through an HMAC-signed /sub endpoint.
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

	sanitizedHeaders := sanitizeUpstreamHeaders(opts.Headers)

	client := opts.Client
	if client == nil {
		var err error
		client, err = newPinnedSafeStreamClient(targetHost, opts.AllowPrivateLAN, opts.DisallowLoopback, sanitizedHeaders)
		if err != nil {
			return nil, err
		}
	}

	var tokBytes [32]byte
	if _, err := rand.Read(tokBytes[:]); err != nil {
		return nil, fmt.Errorf("loopback proxy: generate token: %w", err)
	}
	token := hex.EncodeToString(tokBytes[:])

	var hmacKey [32]byte
	if _, err := rand.Read(hmacKey[:]); err != nil {
		return nil, fmt.Errorf("loopback proxy: generate hmac key: %w", err)
	}

	suffix := safeURLMediaSuffix(parsedTarget.Path)
	expectedPath := "/stream/" + token + suffix

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("loopback proxy: listen loopback: %w", err)
	}
	proxyOrigin := "http://" + ln.Addr().String()

	baseCtx, cancelCtx := context.WithCancel(context.Background())
	var consumed atomic.Bool

	serveUpstream := func(w http.ResponseWriter, r *http.Request, reqCtx context.Context, upstreamRawURL string, upstreamParsed *url.URL, httpClient *http.Client, forwardCustomHeaders bool) {
		upReq, err := http.NewRequestWithContext(reqCtx, r.Method, upstreamRawURL, nil)
		if err != nil {
			http.Error(w, "bad upstream request", http.StatusBadGateway)
			return
		}

		upReq.Header.Set("User-Agent", defaultHTTPUserAgent)
		isLikelyManifestURL := isHLSManifestPath(upstreamParsed.Path)
		for _, passthroughHdr := range []string{"Range", "If-Range", "Icy-MetaData"} {
			if isLikelyManifestURL && (passthroughHdr == "Range" || passthroughHdr == "If-Range") {
				continue
			}
			if v := strings.TrimSpace(r.Header.Get(passthroughHdr)); v != "" && !hasControlChars(v) {
				upReq.Header.Set(passthroughHdr, v)
			}
		}
		if forwardCustomHeaders {
			for k, v := range sanitizedHeaders {
				upReq.Header.Set(k, v)
			}
		}

		resp, err := httpClient.Do(upReq)
		if err != nil {
			http.Error(w, "upstream stream error", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		effectiveURL := upstreamParsed
		if resp.Request != nil && resp.Request.URL != nil {
			effectiveURL = resp.Request.URL
		}
		upstreamCT := resp.Header.Get("Content-Type")

		if r.Method == http.MethodHead {
			isManifest := isHLSManifestURLOrContentType(effectiveURL, upstreamCT)
			for _, hName := range []string{
				"Content-Type", "Content-Length", "Content-Range",
				"Accept-Ranges", "ETag", "Last-Modified", "Icy-Metaint",
			} {
				if isManifest && (hName == "Content-Length" || hName == "Content-Range" || hName == "Accept-Ranges") {
					continue
				}
				if val := resp.Header.Get(hName); val != "" {
					w.Header().Set(hName, val)
				}
			}
			if isManifest {
				w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			} else if w.Header().Get("Content-Type") == "" {
				w.Header().Set("Content-Type", "application/octet-stream")
			}
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(resp.StatusCode)
			return
		}

		// Peek initial bytes on 2xx responses to detect HLS (#EXTM3U) manifests reliably
		// without buffering non-manifest media files.
		var peekBuf []byte
		var peekErr error
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			peekBuf, peekErr = sniffInitialBytes(resp.Body, manifestSniffBytes)
			if peekErr != nil && !errors.Is(peekErr, io.EOF) && !errors.Is(peekErr, io.ErrUnexpectedEOF) && len(peekBuf) == 0 {
				http.Error(w, "upstream stream read error", http.StatusBadGateway)
				return
			}
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 && isHLSManifestPayload(effectiveURL, upstreamCT, peekBuf) {
			if peekErr != nil && !errors.Is(peekErr, io.EOF) && !errors.Is(peekErr, io.ErrUnexpectedEOF) {
				http.Error(w, "failed to read upstream hls manifest", http.StatusBadGateway)
				return
			}
			var rawManifest []byte
			if errors.Is(peekErr, io.EOF) {
				rawManifest = peekBuf
			} else {
				combined := io.MultiReader(bytes.NewReader(peekBuf), resp.Body)
				var readErr error
				rawManifest, readErr = io.ReadAll(io.LimitReader(combined, maxHLSManifestBytes+1))
				if readErr != nil {
					http.Error(w, "failed to read upstream hls manifest", http.StatusBadGateway)
					return
				}
			}
			if len(rawManifest) > maxHLSManifestBytes {
				http.Error(w, "upstream hls manifest exceeds maximum size", http.StatusBadGateway)
				return
			}
			rewritten, rwErr := rewriteHLSManifest(rawManifest, effectiveURL, proxyOrigin, hmacKey[:])
			if rwErr != nil {
				http.Error(w, "invalid upstream hls manifest", http.StatusBadGateway)
				return
			}
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			w.Header().Set("Content-Length", strconv.Itoa(len(rewritten)))
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(rewritten)
			return
		}

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

		flusher, canFlush := w.(http.Flusher)
		if len(peekBuf) > 0 {
			if _, writeErr := w.Write(peekBuf); writeErr != nil {
				return
			}
			if canFlush {
				flusher.Flush()
			}
		}
		if peekErr != nil {
			return
		}
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
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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

		if subtle.ConstantTimeCompare([]byte(r.URL.Path), []byte(expectedPath)) == 1 {
			if opts.SingleUse && !consumed.CompareAndSwap(false, true) {
				http.Error(w, "token already consumed", http.StatusForbidden)
				return
			}
			serveUpstream(w, r, reqCtx, rawTarget, parsedTarget, client, true)
			return
		}

		if r.URL.Path == "/sub" {
			uParam := strings.TrimSpace(r.URL.Query().Get("u"))
			sigParam := strings.TrimSpace(r.URL.Query().Get("sig"))
			if uParam == "" || sigParam == "" || !verifyLoopbackSubSig(hmacKey[:], uParam, sigParam) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			decodedBytes, decErr := decodeBase64URLParam(uParam)
			if decErr != nil {
				http.Error(w, "invalid target encoding", http.StatusBadRequest)
				return
			}
			subRaw := strings.TrimSpace(string(decodedBytes))
			if subRaw == "" || hasControlChars(subRaw) {
				http.Error(w, "invalid target url", http.StatusBadRequest)
				return
			}
			subParsed, parseErr := url.Parse(subRaw)
			if parseErr != nil {
				http.Error(w, "invalid target url", http.StatusBadRequest)
				return
			}
			subScheme := strings.ToLower(subParsed.Scheme)
			if subScheme != "http" && subScheme != "https" {
				http.Error(w, "forbidden url scheme", http.StatusForbidden)
				return
			}
			subHost := strings.TrimSpace(subParsed.Hostname())
			if subHost == "" {
				http.Error(w, "missing target host", http.StatusBadRequest)
				return
			}

			sameHost := isSameOriginHost(subParsed, parsedTarget)
			// Cross-origin sub-resources (e.g. #EXT-X-KEY pointing to 127.0.0.1:9999) must ALWAYS block loopback,
			// even in unit tests where initial TargetURL is an httptest server on 127.0.0.1:<port>.
			disallowSubLoopback := opts.DisallowLoopback || !sameHost

			subHTTPClient := client
			if opts.Client == nil && !sameHost {
				var subErr error
				subHTTPClient, subErr = newSafeSubResourceClient(reqCtx, subHost, opts.AllowPrivateLAN, disallowSubLoopback, sanitizedHeaders)
				if subErr != nil {
					http.Error(w, "blocked upstream sub-resource", http.StatusForbidden)
					return
				}
				defer subHTTPClient.CloseIdleConnections()
			} else if _, pinErr := resolveAndPinHost(reqCtx, subHost, opts.AllowPrivateLAN, !disallowSubLoopback); pinErr != nil {
				http.Error(w, "blocked upstream sub-resource", http.StatusForbidden)
				return
			}
			serveUpstream(w, r, reqCtx, subRaw, subParsed, subHTTPClient, sameHost)
			return
		}

		http.Error(w, "forbidden", http.StatusForbidden)
	})

	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	p := &LoopbackStreamProxy{
		proxyURL:  proxyOrigin + expectedPath,
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

func computeLoopbackSubSig(hmacKey []byte, encodedURL string) string {
	mac := hmac.New(sha256.New, hmacKey)
	_, _ = mac.Write([]byte(encodedURL))
	return hex.EncodeToString(mac.Sum(nil))
}

func verifyLoopbackSubSig(hmacKey []byte, encodedURL, sigHex string) bool {
	gotBytes, err := hex.DecodeString(sigHex)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, hmacKey)
	_, _ = mac.Write([]byte(encodedURL))
	expected := mac.Sum(nil)
	return hmac.Equal(gotBytes, expected)
}

func decodeBase64URLParam(raw string) ([]byte, error) {
	if b, err := base64.RawURLEncoding.DecodeString(raw); err == nil {
		return b, nil
	}
	return base64.URLEncoding.DecodeString(raw)
}

func buildLoopbackSubURL(proxyOrigin, targetURL string, hmacKey []byte) string {
	enc := base64.RawURLEncoding.EncodeToString([]byte(targetURL))
	sig := computeLoopbackSubSig(hmacKey, enc)
	return proxyOrigin + "/sub?u=" + enc + "&sig=" + sig
}

func resolveHLSReferenceURI(baseURL *url.URL, rawRef string) (string, error) {
	trimmed := strings.TrimSpace(rawRef)
	if trimmed == "" {
		return "", errors.New("empty hls uri")
	}
	if hasControlChars(trimmed) {
		return "", errors.New("control characters in hls uri")
	}
	ref, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid hls uri %q: %w", trimmed, err)
	}
	if ref.Scheme != "" {
		sc := strings.ToLower(ref.Scheme)
		if sc != "http" && sc != "https" {
			return "", fmt.Errorf("forbidden hls uri scheme %q", ref.Scheme)
		}
	}
	resolved := baseURL.ResolveReference(ref)
	sc := strings.ToLower(resolved.Scheme)
	if sc != "http" && sc != "https" {
		return "", fmt.Errorf("forbidden resolved hls uri scheme %q", resolved.Scheme)
	}
	if strings.TrimSpace(resolved.Hostname()) == "" {
		return "", errors.New("resolved hls uri missing host")
	}
	return resolved.String(), nil
}

func rewriteHLSManifest(raw []byte, baseURL *url.URL, proxyOrigin string, hmacKey []byte) ([]byte, error) {
	if baseURL == nil {
		return nil, errors.New("nil manifest base url")
	}
	text := string(raw)
	text = strings.TrimPrefix(text, "\xef\xbb\xbf")
	lines := strings.Split(text, "\n")

	var out strings.Builder
	out.Grow(len(text) + 256)

	for i, rawLine := range lines {
		line := strings.TrimRight(rawLine, "\r")
		trimmed := strings.TrimSpace(line)

		switch {
		case trimmed == "":
			out.WriteString(line)
		case strings.HasPrefix(trimmed, "#"):
			rewrittenTag, err := rewriteHLSTagURIs(line, baseURL, proxyOrigin, hmacKey)
			if err != nil {
				return nil, err
			}
			out.WriteString(rewrittenTag)
		default:
			resolved, err := resolveHLSReferenceURI(baseURL, trimmed)
			if err != nil {
				return nil, err
			}
			out.WriteString(buildLoopbackSubURL(proxyOrigin, resolved, hmacKey))
		}
		if i < len(lines)-1 {
			out.WriteByte('\n')
		}
	}
	return []byte(out.String()), nil
}

func rewriteHLSTagURIs(line string, baseURL *url.URL, proxyOrigin string, hmacKey []byte) (string, error) {
	if !strings.Contains(strings.ToUpper(line), "URI=") {
		return line, nil
	}
	var firstErr error
	// First rewrite quoted URI="..." attributes.
	if strings.Contains(line, `"`) {
		line = reHLSTagQuotedURI.ReplaceAllStringFunc(line, func(match string) string {
			if firstErr != nil {
				return match
			}
			sub := reHLSTagQuotedURI.FindStringSubmatch(match)
			if len(sub) < 2 {
				return match
			}
			resolved, err := resolveHLSReferenceURI(baseURL, sub[1])
			if err != nil {
				firstErr = err
				return match
			}
			return `URI="` + buildLoopbackSubURL(proxyOrigin, resolved, hmacKey) + `"`
		})
		if firstErr != nil {
			return "", firstErr
		}
	}
	// Also handle unquoted URI=value attributes so unquoted schemes/URLs cannot bypass rewriting.
	line = reHLSTagUnquotedURI.ReplaceAllStringFunc(line, func(match string) string {
		if firstErr != nil {
			return match
		}
		sub := reHLSTagUnquotedURI.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		resolved, err := resolveHLSReferenceURI(baseURL, sub[1])
		if err != nil {
			firstErr = err
			return match
		}
		return `URI="` + buildLoopbackSubURL(proxyOrigin, resolved, hmacKey) + `"`
	})
	if firstErr != nil {
		return "", firstErr
	}
	return line, nil
}

func isHLSManifestPath(urlPath string) bool {
	lower := strings.ToLower(strings.TrimSpace(urlPath))
	return strings.HasSuffix(lower, ".m3u8")
}

func isHLSManifestURLOrContentType(u *url.URL, contentType string) bool {
	if u != nil && isHLSManifestPath(u.Path) {
		return true
	}
	lowerCT := strings.ToLower(contentType)
	return strings.Contains(lowerCT, "mpegurl")
}

func isHLSManifestPayload(u *url.URL, contentType string, peek []byte) bool {
	trimmed := bytes.TrimLeft(peek, "\xef\xbb\xbf \t\r\n")
	if bytes.HasPrefix(trimmed, []byte("#EXTM3U")) {
		return true
	}
	if isHLSManifestURLOrContentType(u, contentType) && len(trimmed) > 0 && utf8.Valid(trimmed) && bytes.HasPrefix(trimmed, []byte("#EXT")) {
		return true
	}
	return false
}

func effectiveURLPort(u *url.URL) string {
	if u == nil {
		return ""
	}
	if p := u.Port(); p != "" {
		return p
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return "443"
	case "http":
		return "80"
	default:
		return ""
	}
}

func isSameOriginHost(a, b *url.URL) bool {
	if a == nil || b == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSuffix(a.Hostname(), "."), strings.TrimSuffix(b.Hostname(), ".")) &&
		effectiveURLPort(a) == effectiveURLPort(b)
}

func sniffInitialBytes(r io.Reader, maxBytes int) ([]byte, error) {
	if maxBytes <= 0 {
		return nil, nil
	}
	targetMin := 64
	if maxBytes < targetMin {
		targetMin = maxBytes
	}
	buf := make([]byte, maxBytes)
	total := 0
	emptyReads := 0
	for total < targetMin {
		n, err := r.Read(buf[total:])
		if n > 0 {
			total += n
			emptyReads = 0
		}
		if err != nil {
			return buf[:total], err
		}
		if n == 0 {
			emptyReads++
			if emptyReads >= 10 {
				return buf[:total], io.ErrNoProgress
			}
		}
	}
	return buf[:total], nil
}

func stripCrossHostRedirectHeaders(req *http.Request, sanitizedHeaders map[string]string) {
	req.Header.Del("Referer")
	req.Header.Del("Authorization")
	req.Header.Del("Cookie")
	for k := range sanitizedHeaders {
		if strings.EqualFold(k, "User-Agent") {
			continue
		}
		req.Header.Del(k)
	}
}

func newSafeSubResourceClient(ctx context.Context, subHost string, allowPrivateLAN, disallowLoopback bool, sanitizedHeaders map[string]string) (*http.Client, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	return newPinnedClientWithPublicFallback(ctx, subHost, allowPrivateLAN, disallowLoopback, sanitizedHeaders)
}

func resolveAndPinHost(ctx context.Context, host string, allowPrivateLAN, allowLoopback bool) ([]string, error) {
	if addr, err := netip.ParseAddr(host); err == nil {
		unmapped := addr.WithZone("").Unmap()
		if unmapped.IsLoopback() {
			if !allowLoopback {
				return nil, fmt.Errorf("blocked loopback address %s", unmapped)
			}
			return []string{unmapped.String()}, nil
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
		if unmapped.IsLoopback() {
			if !allowLoopback {
				return nil, fmt.Errorf("host %q: blocked loopback address %s", host, unmapped)
			}
			pinned = append(pinned, unmapped.String())
			continue
		}
		if err := netutil.ValidateIP(net.IP(unmapped.AsSlice()), allowPrivateLAN); err != nil {
			return nil, fmt.Errorf("host %q: %w", host, err)
		}
		pinned = append(pinned, unmapped.String())
	}
	return pinned, nil
}

func newPinnedSafeStreamClient(initialHost string, allowPrivateLAN, disallowLoopback bool, sanitizedHeaders map[string]string) (*http.Client, error) {
	return newPinnedClientWithPublicFallback(context.Background(), initialHost, allowPrivateLAN, disallowLoopback, sanitizedHeaders)
}

func newPinnedClientWithPublicFallback(ctx context.Context, initialHost string, allowPrivateLAN, disallowLoopback bool, sanitizedHeaders map[string]string) (*http.Client, error) {
	dnsCtx, cancel := context.WithTimeout(ctx, loopbackDNSLookupTimeout)
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

	// Cross-host redirects must ALWAYS use a strict public-only DialContext
	// (disallowing both private LAN and loopback regardless of allowPrivateLAN).
	publicOnlyClient := netutil.NewSafeHTTPClientNoLoopback(0, false)
	var fallbackDial func(ctx context.Context, network, addr string) (net.Conn, error)
	if pubTransport, ok := publicOnlyClient.Transport.(*http.Transport); ok && pubTransport != nil {
		fallbackDial = pubTransport.DialContext
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
				stripCrossHostRedirectHeaders(req, sanitizedHeaders)
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
