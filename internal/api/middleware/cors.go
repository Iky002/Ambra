package middleware

import (
	"net/http"
	"strings"
)

// CORS enables cross-origin requests for Emby Web, Infuse, SenPlayer, VidHub, and third-party clients.
// Without an explicit origin allowlist, it reflects Origin WITHOUT setting Access-Control-Allow-Credentials.
func CORS(next http.Handler) http.Handler {
	return CORSWithOrigins(nil)(next)
}

// CORSWithOrigins returns a CORS middleware with an optional explicit origin allowlist.
// Access-Control-Allow-Credentials: true is only sent for origins matching an explicit allowlist entry.
func CORSWithOrigins(allowed []string) func(http.Handler) http.Handler {
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
	hasExplicitList := len(allowedSet) > 0 || allowWildcard

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := strings.TrimSpace(r.Header.Get("Origin"))
			if hasExplicitList {
				if origin != "" {
					normOrigin := strings.ToLower(strings.TrimRight(origin, "/"))
					w.Header().Add("Vary", "Origin")
					if _, ok := allowedSet[normOrigin]; ok {
						w.Header().Set("Access-Control-Allow-Origin", origin)
						w.Header().Set("Access-Control-Allow-Credentials", "true")
					} else if allowWildcard {
						w.Header().Set("Access-Control-Allow-Origin", origin)
					}
				} else if allowWildcard {
					w.Header().Set("Access-Control-Allow-Origin", "*")
				}
			} else {
				if origin != "" {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Vary", "Origin")
				} else {
					w.Header().Set("Access-Control-Allow-Origin", "*")
				}
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
