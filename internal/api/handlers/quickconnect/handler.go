package quickconnect

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/core/session"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/pkg/netutil"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

const (
	defaultTTL              = 10 * time.Minute
	maxPendingSessions      = 1000
	maxPendingPerIP         = 20
	maxFailedAttempts       = 5
	maxInitiatesPerIPPerMin = 10
	initiateRateWindow      = time.Minute
)

// AuthFuncs decouples route registration from the concrete middleware package.
type AuthFuncs struct {
	RequireAuth   func(http.Handler) http.Handler
	RequireAdmin  func(http.Handler) http.Handler
	UserIDFromCtx func(context.Context) string
}

// QuickConnectResult matches the Emby/Jellyfin QuickConnect API payload.
type QuickConnectResult struct {
	Authenticated bool      `json:"Authenticated"`
	Secret        string    `json:"Secret"`
	Code          string    `json:"Code"`
	DeviceId      string    `json:"DeviceId"`
	DeviceName    string    `json:"DeviceName"`
	AppName       string    `json:"AppName"`
	AppVersion    string    `json:"AppVersion"`
	DateAdded     time.Time `json:"DateAdded"`
}

type pendingSession struct {
	result    QuickConnectResult
	userID    string
	clientIP  string
	expiresAt time.Time
}

type failWindow struct {
	count   int
	resetAt time.Time
}

// Handler implements Emby/Jellyfin QuickConnect authentication endpoints.
type Handler struct {
	authSvc    *auth.Service
	userRepo   *sqlite.UserRepo
	sessionMgr *session.Manager
	cfg        *config.Config
	auth       AuthFuncs

	ttl time.Duration
	now func() time.Time

	mu             sync.Mutex
	bySecret       map[string]*pendingSession
	byCode         map[string]*pendingSession
	failedAttempts map[string]*failWindow
	initiateRates  map[string]*failWindow
}

func New(authSvc *auth.Service, userRepo *sqlite.UserRepo, sessionMgr *session.Manager) *Handler {
	return &Handler{
		authSvc:        authSvc,
		userRepo:       userRepo,
		sessionMgr:     sessionMgr,
		ttl:            defaultTTL,
		now:            func() time.Time { return time.Now().UTC() },
		bySecret:       make(map[string]*pendingSession),
		byCode:         make(map[string]*pendingSession),
		failedAttempts: make(map[string]*failWindow),
		initiateRates:  make(map[string]*failWindow),
	}
}

// SetConfig attaches the server configuration so QuickConnect honors EnableQuickConnect.
func (h *Handler) SetConfig(cfg *config.Config) {
	h.mu.Lock()
	h.cfg = cfg
	h.mu.Unlock()
}

func (h *Handler) isEnabled() bool {
	h.mu.Lock()
	cfg := h.cfg
	h.mu.Unlock()
	if cfg == nil {
		return true
	}
	return cfg.Snapshot().EnableQuickConnect
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux, authFuncs AuthFuncs) {
	h.auth = authFuncs
	wrapAuth := authFuncs.RequireAuth
	if wrapAuth == nil {
		wrapAuth = func(next http.Handler) http.Handler { return next }
	}

	mux.HandleFunc("GET /QuickConnect/Enabled", h.handleEnabled)
	mux.HandleFunc("POST /QuickConnect/Initiate", h.handleInitiate)
	mux.HandleFunc("GET /QuickConnect/Connect", h.handleConnect)
	mux.Handle("POST /QuickConnect/Authorize", wrapAuth(http.HandlerFunc(h.handleAuthorize)))
	mux.HandleFunc("POST /Users/AuthenticateWithQuickConnect", h.handleAuthenticate)
}

func (h *Handler) handleEnabled(w http.ResponseWriter, _ *http.Request) {
	httpx.JSON(w, http.StatusOK, h.isEnabled())
}

func (h *Handler) countPendingForIPLocked(ip string) int {
	if ip == "" {
		return 0
	}
	n := 0
	for _, entry := range h.bySecret {
		if entry.clientIP == ip {
			n++
		}
	}
	return n
}

func (h *Handler) handleInitiate(w http.ResponseWriter, r *http.Request) {
	if !h.isEnabled() {
		httpx.Error(w, http.StatusForbidden, "quickconnect is disabled")
		return
	}
	client := middleware.ExtractClientInfo(r)
	now := h.now()
	ip := clientIP(r)

	h.mu.Lock()
	h.pruneExpiredLocked(now)
	if ip != "" {
		if h.countPendingForIPLocked(ip) >= maxPendingPerIP {
			h.mu.Unlock()
			httpx.Error(w, http.StatusTooManyRequests, "too many pending quickconnect sessions from this ip")
			return
		}
		rw := h.initiateRates[ip]
		if rw != nil && now.Before(rw.resetAt) && rw.count >= maxInitiatesPerIPPerMin {
			h.mu.Unlock()
			httpx.Error(w, http.StatusTooManyRequests, "too many quickconnect initiate requests from this ip")
			return
		}
		if rw == nil || !now.Before(rw.resetAt) {
			rw = &failWindow{count: 0, resetAt: now.Add(initiateRateWindow)}
			h.initiateRates[ip] = rw
		}
		rw.count++
	}
	if len(h.bySecret) >= maxPendingSessions {
		h.mu.Unlock()
		httpx.Error(w, http.StatusTooManyRequests, "too many pending quickconnect sessions")
		return
	}
	h.mu.Unlock()

	secret, err := generateSecret()
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to generate quickconnect secret")
		return
	}

	h.mu.Lock()
	h.pruneExpiredLocked(now)
	if len(h.bySecret) >= maxPendingSessions || (ip != "" && h.countPendingForIPLocked(ip) >= maxPendingPerIP) {
		h.mu.Unlock()
		httpx.Error(w, http.StatusTooManyRequests, "too many pending quickconnect sessions")
		return
	}

	var code string
	for i := 0; i < 10; i++ {
		c, genErr := generateCode()
		if genErr != nil {
			h.mu.Unlock()
			httpx.Error(w, http.StatusInternalServerError, "failed to generate quickconnect code")
			return
		}
		if _, exists := h.byCode[c]; !exists {
			code = c
			break
		}
	}
	if code == "" {
		h.mu.Unlock()
		httpx.Error(w, http.StatusInternalServerError, "failed to allocate unique quickconnect code")
		return
	}

	entry := &pendingSession{
		result: QuickConnectResult{
			Authenticated: false,
			Secret:        secret,
			Code:          code,
			DeviceId:      client.DeviceID,
			DeviceName:    client.Device,
			AppName:       client.Client,
			AppVersion:    client.Version,
			DateAdded:     now,
		},
		clientIP:  ip,
		expiresAt: now.Add(h.ttl),
	}
	h.bySecret[secret] = entry
	h.byCode[code] = entry
	res := entry.result
	h.mu.Unlock()

	httpx.JSON(w, http.StatusOK, res)
}

func (h *Handler) handleConnect(w http.ResponseWriter, r *http.Request) {
	if !h.isEnabled() {
		httpx.Error(w, http.StatusForbidden, "quickconnect is disabled")
		return
	}
	q := httpx.ParseQuery(r)
	secret := strings.TrimSpace(q.Get("secret"))
	if secret == "" {
		httpx.Error(w, http.StatusNotFound, "quickconnect session not found")
		return
	}

	now := h.now()
	h.mu.Lock()
	h.pruneExpiredLocked(now)
	entry, ok := h.bySecret[secret]
	if !ok {
		h.mu.Unlock()
		httpx.Error(w, http.StatusNotFound, "quickconnect session not found or expired")
		return
	}
	res := entry.result
	h.mu.Unlock()

	httpx.JSON(w, http.StatusOK, res)
}

func (h *Handler) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	if !h.isEnabled() {
		httpx.Error(w, http.StatusForbidden, "quickconnect is disabled")
		return
	}
	ac, hasAC := middleware.AuthFromContext(r.Context())
	isAPIKey := hasAC && ac != nil && ac.IsAPIKey
	callerID := h.callerUserID(r)
	if callerID == "" && !isAPIKey {
		httpx.Error(w, http.StatusUnauthorized, "user authentication required")
		return
	}

	q := httpx.ParseQuery(r)
	code := strings.TrimSpace(q.Get("code"))
	requestedUserID := strings.TrimSpace(q.Get("userId"))
	if (code == "" || requestedUserID == "") && r.Body != nil {
		var body struct {
			Code   string `json:"Code"`
			UserId string `json:"UserId"`
		}
		if err := httpx.DecodeJSONLimit(r, &body, 0); err == nil {
			if code == "" {
				code = strings.TrimSpace(body.Code)
			}
			if requestedUserID == "" {
				requestedUserID = strings.TrimSpace(body.UserId)
			}
		}
	}

	targetUserID := callerID
	normReq := guid.Normalize(requestedUserID)
	if isAPIKey && callerID == "" {
		if normReq == "" {
			httpx.Error(w, http.StatusBadRequest, "userId is required for API key authorization")
			return
		}
		if h.userRepo != nil {
			if _, err := h.userRepo.GetUserByID(r.Context(), normReq); err != nil {
				if errors.Is(err, sqlite.ErrNotFound) {
					httpx.Error(w, http.StatusNotFound, "target user not found")
					return
				}
				httpx.Error(w, http.StatusInternalServerError, "failed to load target user")
				return
			}
		}
		targetUserID = normReq
	} else if normReq != "" && normReq != callerID {
		if !hasAC || ac == nil || !ac.IsAdmin() {
			httpx.Error(w, http.StatusForbidden, "admin privileges required to authorize for another user")
			return
		}
		if h.userRepo != nil {
			if _, err := h.userRepo.GetUserByID(r.Context(), normReq); err != nil {
				if errors.Is(err, sqlite.ErrNotFound) {
					httpx.Error(w, http.StatusNotFound, "target user not found")
					return
				}
				httpx.Error(w, http.StatusInternalServerError, "failed to load target user")
				return
			}
		}
		targetUserID = normReq
	}

	rateKey := callerID
	if rateKey == "" && ac != nil {
		rateKey = "apikey:" + ac.APIKeyName
	}

	now := h.now()
	h.mu.Lock()
	h.pruneExpiredLocked(now)

	fw := h.failedAttempts[rateKey]
	if fw != nil && now.Before(fw.resetAt) && fw.count >= maxFailedAttempts {
		h.mu.Unlock()
		httpx.Error(w, http.StatusTooManyRequests, "too many failed quickconnect authorize attempts")
		return
	}

	entry, ok := h.byCode[code]
	if !ok || code == "" {
		if fw == nil || !now.Before(fw.resetAt) {
			fw = &failWindow{count: 0, resetAt: now.Add(h.ttl)}
			h.failedAttempts[rateKey] = fw
		}
		fw.count++
		h.mu.Unlock()
		httpx.Error(w, http.StatusNotFound, "invalid or expired quickconnect code")
		return
	}

	entry.userID = targetUserID
	entry.result.Authenticated = true
	delete(h.byCode, code)
	delete(h.failedAttempts, rateKey)
	h.mu.Unlock()

	httpx.JSON(w, http.StatusOK, true)
}

func (h *Handler) handleAuthenticate(w http.ResponseWriter, r *http.Request) {
	if !h.isEnabled() {
		httpx.Error(w, http.StatusForbidden, "quickconnect is disabled")
		return
	}
	var body struct {
		Secret string `json:"Secret"`
	}
	if err := httpx.DecodeJSONLimit(r, &body, 0); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	secret := strings.TrimSpace(body.Secret)
	if secret == "" {
		httpx.Error(w, http.StatusBadRequest, "secret is required")
		return
	}

	now := h.now()
	h.mu.Lock()
	h.pruneExpiredLocked(now)
	entry, ok := h.bySecret[secret]
	if !ok || !entry.result.Authenticated || entry.userID == "" {
		h.mu.Unlock()
		httpx.Error(w, http.StatusUnauthorized, "quickconnect session not authorized or expired")
		return
	}
	// One-time redemption: delete immediately to prevent replay attacks.
	delete(h.bySecret, secret)
	delete(h.byCode, entry.result.Code)
	userID := entry.userID
	initiated := entry.result
	h.mu.Unlock()

	u, err := h.userRepo.GetUserByID(r.Context(), userID)
	if err != nil {
		if errors.Is(err, sqlite.ErrNotFound) {
			httpx.Error(w, http.StatusUnauthorized, "user not found")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "failed to load user")
		return
	}

	client := middleware.ExtractClientInfo(r)
	if client.DeviceID == "" {
		client.DeviceID = initiated.DeviceId
	}
	if client.Device == "" {
		client.Device = initiated.DeviceName
	}
	if client.Client == "" {
		client.Client = initiated.AppName
	}
	if client.Version == "" {
		client.Version = initiated.AppVersion
	}

	res, err := h.authSvc.IssueSessionForUser(r.Context(), u, client)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrUserDisabled):
			httpx.Error(w, http.StatusForbidden, "User account is disabled")
		default:
			httpx.Error(w, http.StatusInternalServerError, "Authentication failed")
		}
		return
	}

	if h.sessionMgr != nil {
		client.DeviceID = res.SessionInfo.DeviceId
		client.Device = res.SessionInfo.DeviceName
		client.Client = res.SessionInfo.Client
		client.Version = res.SessionInfo.ApplicationVersion
		res.SessionInfo = h.sessionMgr.TouchSession(u, client)
	}

	_ = h.userRepo.CreateActivityLog(r.Context(), &sqlite.ActivityLogRecord{
		Name:     res.User.Name + " has logged in via QuickConnect",
		Type:     "AuthenticationSucceeded",
		UserID:   res.User.Id,
		Severity: "Info",
	})

	httpx.JSON(w, http.StatusOK, res)
}

func (h *Handler) callerUserID(r *http.Request) string {
	if h.auth.UserIDFromCtx != nil {
		if uid := guid.Normalize(h.auth.UserIDFromCtx(r.Context())); uid != "" {
			return uid
		}
	}
	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil {
		return guid.Normalize(ac.UserID())
	}
	return ""
}

func (h *Handler) pruneExpiredLocked(now time.Time) {
	for secret, entry := range h.bySecret {
		if !now.Before(entry.expiresAt) {
			delete(h.bySecret, secret)
			if cur, ok := h.byCode[entry.result.Code]; ok && cur == entry {
				delete(h.byCode, entry.result.Code)
			}
		}
	}
	for uid, fw := range h.failedAttempts {
		if !now.Before(fw.resetAt) {
			delete(h.failedAttempts, uid)
		}
	}
	for ip, rw := range h.initiateRates {
		if !now.Before(rw.resetAt) {
			delete(h.initiateRates, ip)
		}
	}
}

func clientIP(r *http.Request) string {
	return netutil.ClientIP(r)
}

func generateSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func generateCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}
