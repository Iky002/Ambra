package middleware

import (
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"

	"github.com/ambra-media/ambra/internal/pkg/netutil"
)

func isRequestHTTPS(r *http.Request) bool {
	if r == nil {
		return false
	}
	if r.TLS != nil {
		return true
	}
	remoteHost := strings.TrimSpace(r.RemoteAddr)
	if h, _, err := net.SplitHostPort(remoteHost); err == nil {
		remoteHost = h
	}
	remoteHost = strings.Trim(remoteHost, "[]")
	if remoteIP, err := netip.ParseAddr(remoteHost); err == nil {
		if netutil.IsTrustedProxyIP(remoteIP.WithZone("").Unmap()) {
			proto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))
			if idx := strings.IndexByte(proto, ','); idx >= 0 {
				proto = strings.TrimSpace(proto[:idx])
			}
			if strings.EqualFold(proto, "https") {
				return true
			}
		}
	}
	return false
}

func normalizeOriginHost(hostPort, scheme string) string {
	s := strings.ToLower(strings.TrimSpace(hostPort))
	if s == "" {
		return ""
	}
	if h, p, err := net.SplitHostPort(s); err == nil {
		h = strings.Trim(h, "[]")
		if (scheme == "http" && p == "80") || (scheme == "https" && p == "443") {
			if strings.Contains(h, ":") {
				return "[" + h + "]"
			}
			return h
		}
		if strings.Contains(h, ":") {
			return "[" + h + "]:" + p
		}
		return h + ":" + p
	}
	return s
}

// IsSameOrigin reports whether the Origin header represents the same host(:port) as r.Host
// (and prevents http:// downgrade when r arrived over TLS or via a TLS-terminating trusted proxy).
func IsSameOrigin(origin string, r *http.Request) bool {
	if r == nil {
		return false
	}
	raw := strings.TrimSpace(origin)
	if raw == "" || strings.TrimSpace(r.Host) == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	if u.Path != "" && u.Path != "/" {
		return false
	}
	scheme := strings.ToLower(strings.TrimSpace(u.Scheme))
	if scheme != "http" && scheme != "https" {
		return false
	}
	if isRequestHTTPS(r) && scheme != "https" {
		return false
	}
	originHost := normalizeOriginHost(u.Host, scheme)
	reqHost := normalizeOriginHost(r.Host, scheme)
	return originHost != "" && originHost == reqHost
}

func parseAllowedOrigins(allowed []string) (map[string]struct{}, bool) {
	allowedSet := make(map[string]struct{}, len(allowed))
	allowWildcard := false
	for _, raw := range allowed {
		norm := strings.ToLower(strings.TrimRight(strings.TrimSpace(raw), "/"))
		if norm == "" {
			continue
		}
		if norm == "*" {
			allowWildcard = true
			continue
		}
		allowedSet[norm] = struct{}{}
	}
	return allowedSet, allowWildcard
}

// IsOriginAllowed reports whether origin is empty, same-origin with r.Host, or permitted by allowed.
// Used by setup/startup mutation endpoints to block cross-origin browser takeover.
func IsOriginAllowed(origin string, r *http.Request, allowed []string) bool {
	origin = strings.TrimSpace(origin)
	if origin == "" {
		return true
	}
	if IsSameOrigin(origin, r) {
		return true
	}
	allowedSet, allowWildcard := parseAllowedOrigins(allowed)
	if allowWildcard {
		return true
	}
	normOrigin := strings.ToLower(strings.TrimRight(origin, "/"))
	_, ok := allowedSet[normOrigin]
	return ok
}

// CORS enables same-origin requests by default without reflecting arbitrary cross-origin headers.
func CORS(next http.Handler) http.Handler {
	return CORSWithOrigins(nil)(next)
}

// CORSWithOrigins returns a CORS middleware with an optional explicit origin allowlist.
// Access-Control-Allow-Credentials: true is only sent for origins matching an explicit allowlist entry.
func CORSWithOrigins(allowed []string) func(http.Handler) http.Handler {
	copied := append([]string(nil), allowed...)
	return CORSWithDynamicOrigins(func() []string {
		return copied
	})
}

// CORSWithDynamicOrigins returns a CORS middleware that evaluates getOrigins on each request
// so AllowedOrigins updates take effect immediately via hot-reload.
func CORSWithDynamicOrigins(getOrigins func() []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var currentAllowed []string
			if getOrigins != nil {
				currentAllowed = getOrigins()
			}
			allowedSet, allowWildcard := parseAllowedOrigins(currentAllowed)

			origin := strings.TrimSpace(r.Header.Get("Origin"))
			if origin != "" {
				normOrigin := strings.ToLower(strings.TrimRight(origin, "/"))
				w.Header().Add("Vary", "Origin")
				if _, ok := allowedSet[normOrigin]; ok {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Access-Control-Allow-Credentials", "true")
				} else if IsSameOrigin(origin, r) {
					w.Header().Set("Access-Control-Allow-Origin", origin)
				} else if allowWildcard {
					w.Header().Set("Access-Control-Allow-Origin", "*")
				}
			} else if allowWildcard {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			}

			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, PATCH, OPTIONS, HEAD")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept, Origin, Range, If-None-Match, X-Emby-Token, X-MediaBrowser-Token, X-Emby-Authorization, X-Emby-Client, X-Emby-Client-Version, X-Emby-Device-Id, X-Emby-Device-Name, X-Application")
			w.Header().Set("Access-Control-Expose-Headers", "Content-Length, Content-Range, Accept-Ranges, ETag, Content-Disposition")
			w.Header().Set("Access-Control-Max-Age", "86400")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
