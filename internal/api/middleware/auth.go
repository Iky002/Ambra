package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/core/session"
	"github.com/ambra-media/ambra/internal/pkg/netutil"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

type authContextKey struct{}

// AuthContext holds the authenticated user/API key and parsed Emby client metadata for a request.
type AuthContext struct {
	User       *sqlite.UserRecord
	Token      string
	IsAPIKey   bool
	APIKeyName string
	Client     auth.ClientInfo
	resolved   bool
}

func (ac *AuthContext) IsAdmin() bool {
	if ac == nil {
		return false
	}
	if ac.IsAPIKey {
		return true
	}
	return ac.User != nil && (ac.User.IsAdmin || ac.User.Policy.IsAdministrator)
}

func (ac *AuthContext) UserID() string {
	if ac == nil || ac.User == nil {
		return ""
	}
	return ac.User.ID
}

// AuthFromContext extracts AuthContext from a request context.
func AuthFromContext(ctx context.Context) (*AuthContext, bool) {
	ac, ok := ctx.Value(authContextKey{}).(*AuthContext)
	if !ok || ac == nil {
		return nil, false
	}
	return ac, true
}

// WithAuthContext injects an AuthContext into ctx (useful for middleware and unit tests).
func WithAuthContext(ctx context.Context, ac *AuthContext) context.Context {
	return context.WithValue(ctx, authContextKey{}, ac)
}

// Auth provides Emby-compatible token extraction, validation, and RBAC middleware.
type Auth struct {
	svc      *auth.Service
	sessions *session.Manager
}

func NewAuth(svc *auth.Service, sessions *session.Manager) *Auth {
	return &Auth{
		svc:      svc,
		sessions: sessions,
	}
}

// ParseAuth extracts client metadata and validates any present token without blocking anonymous requests.
func (a *Auth) ParseAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ac := a.resolve(r)
		next.ServeHTTP(w, r.WithContext(WithAuthContext(r.Context(), ac)))
	})
}

// RequireAuth enforces that the request carries a valid user token or API key.
func (a *Auth) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ac, ok := AuthFromContext(r.Context())
		if !ok || (!ac.resolved && ac.User == nil && !ac.IsAPIKey) {
			ac = a.resolve(r)
		}
		if ac == nil || (ac.User == nil && !ac.IsAPIKey) {
			httpx.Error(w, http.StatusUnauthorized, "Authentication required")
			return
		}
		if !ac.IsAPIKey && ac.User != nil && !ac.User.Policy.EnableRemoteAccess && !netutil.IsLocalOrLANRequest(r) {
			httpx.Error(w, http.StatusForbidden, "Remote access is disabled for this user")
			return
		}
		next.ServeHTTP(w, r.WithContext(WithAuthContext(r.Context(), ac)))
	})
}

// RequireAdmin enforces that the request is authenticated as an administrator or via an API key.
func (a *Auth) RequireAdmin(next http.Handler) http.Handler {
	return a.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ac, _ := AuthFromContext(r.Context())
		if !ac.IsAdmin() {
			httpx.Error(w, http.StatusForbidden, "Administrator privileges required")
			return
		}
		next.ServeHTTP(w, r)
	}))
}

func (a *Auth) resolve(r *http.Request) *AuthContext {
	client := ExtractClientInfo(r)
	ac := &AuthContext{
		Token:    client.Token,
		Client:   client,
		resolved: true,
	}
	if client.Token == "" || a.svc == nil {
		return ac
	}
	id, err := a.svc.ValidateToken(r.Context(), client.Token)
	if err != nil {
		return ac
	}
	ac.User = id.User
	ac.IsAPIKey = id.IsAPIKey
	ac.APIKeyName = id.APIKeyName
	// For user session tokens, always enforce the token-bound DeviceID when present
	// so callers cannot spoof another user's or device's DeviceId via headers/query params.
	if !id.IsAPIKey && id.DeviceID != "" {
		ac.Client.DeviceID = id.DeviceID
	} else if ac.Client.DeviceID == "" {
		ac.Client.DeviceID = id.DeviceID
	}
	if ac.Client.Device == "" {
		ac.Client.Device = id.DeviceName
	}
	if ac.Client.Client == "" {
		ac.Client.Client = id.AppName
	}
	if ac.Client.Version == "" {
		ac.Client.Version = id.AppVersion
	}

	if a.sessions != nil && ac.User != nil {
		a.sessions.TouchSession(ac.User, ac.Client)
	}
	return ac
}

// ExtractClientInfo parses Emby's 4 token sources and client metadata from an HTTP request.
// Token priority: X-Emby-Token > X-MediaBrowser-Token > (X-Emby-Authorization / Authorization)[Token] > ?api_key=
func ExtractClientInfo(r *http.Request) auth.ClientInfo {
	info := auth.ClientInfo{
		RemoteAddr: r.RemoteAddr,
	}

	// Parse Authorization / X-Emby-Authorization header first for client fields and fallback token.
	authHeader := r.Header.Get("X-Emby-Authorization")
	if authHeader == "" {
		authHeader = r.Header.Get("Authorization")
	}
	var headerAuthToken string
	if authHeader != "" {
		params := ParseAuthorizationHeader(authHeader)
		info.Client = params["client"]
		info.Device = params["device"]
		info.DeviceID = params["deviceid"]
		info.Version = params["version"]
		headerAuthToken = params["token"]
	}

	// Individual X-Emby-* headers override if present.
	if v := strings.TrimSpace(r.Header.Get("X-Emby-Client")); v != "" {
		info.Client = v
	}
	if v := strings.TrimSpace(r.Header.Get("X-Emby-Device-Name")); v != "" {
		info.Device = v
	}
	if v := strings.TrimSpace(r.Header.Get("X-Emby-Device-Id")); v != "" {
		info.DeviceID = v
	}
	if v := strings.TrimSpace(r.Header.Get("X-Emby-Client-Version")); v != "" {
		info.Version = v
	}

	// Resolve token according to strict Emby priority.
	if v := strings.TrimSpace(r.Header.Get("X-Emby-Token")); v != "" {
		info.Token = v
	} else if v := strings.TrimSpace(r.Header.Get("X-MediaBrowser-Token")); v != "" {
		info.Token = v
	} else if headerAuthToken != "" {
		info.Token = headerAuthToken
	} else {
		q := httpx.ParseQuery(r)
		if v := strings.TrimSpace(q.Get("api_key")); v != "" {
			info.Token = v
		}
		if info.DeviceID == "" {
			if v := strings.TrimSpace(q.Get("deviceId")); v != "" {
				info.DeviceID = v
			}
		}
	}

	if info.DeviceID == "" && r.URL != nil && r.URL.RawQuery != "" {
		q := httpx.ParseQuery(r)
		if v := strings.TrimSpace(q.Get("deviceId")); v != "" {
			info.DeviceID = v
		}
	}

	return info
}

// ParseAuthorizationHeader parses Emby/MediaBrowser authorization headers such as:
// MediaBrowser Client="Emby Web", Device="Chrome", DeviceId="abc", Version="4.8.0", Token="xyz"
func ParseAuthorizationHeader(header string) map[string]string {
	out := make(map[string]string)
	s := strings.TrimSpace(header)
	if s == "" {
		return out
	}

	// Strip leading scheme (MediaBrowser / Emby / Bearer)
	if idx := strings.IndexAny(s, " \t"); idx > 0 {
		scheme := strings.ToLower(s[:idx])
		if scheme == "mediabrowser" || scheme == "emby" {
			s = strings.TrimSpace(s[idx+1:])
		} else if scheme == "bearer" {
			out["token"] = strings.TrimSpace(s[idx+1:])
			return out
		}
	}

	// Parse comma-separated key="value" or key=value pairs, respecting quoted commas.
	for len(s) > 0 {
		s = strings.TrimLeft(s, " ,\t")
		if len(s) == 0 {
			break
		}
		eq := strings.IndexByte(s, '=')
		if eq < 0 {
			break
		}
		key := strings.ToLower(strings.TrimSpace(s[:eq]))
		s = strings.TrimLeft(s[eq+1:], " \t")

		var val string
		if len(s) > 0 && s[0] == '"' {
			s = s[1:]
			end := strings.IndexByte(s, '"')
			if end < 0 {
				val = s
				s = ""
			} else {
				val = s[:end]
				s = s[end+1:]
			}
		} else {
			comma := strings.IndexByte(s, ',')
			if comma < 0 {
				val = strings.TrimSpace(s)
				s = ""
			} else {
				val = strings.TrimSpace(s[:comma])
				s = s[comma+1:]
			}
		}
		if key != "" {
			out[key] = val
		}
	}

	return out
}
