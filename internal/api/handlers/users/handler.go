package users

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/api/ws"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/core/session"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/pkg/netutil"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

const (
	authByNameRateLimit           = 30
	authByNameUserRateLimit       = 30
	authByNameIPRateLimit         = 60
	authByNameRateWindow          = time.Minute
	authByNameLoopbackRateLimit   = 60
	authByNameLoopbackIPRateLimit = 180
	authByNameLoopbackRateWindow  = 5 * time.Minute
	authRateMaxBuckets            = 4096
	maxSubBucketsPerIP            = 8
)

type Handler struct {
	authSvc    *auth.Service
	userRepo   *sqlite.UserRepo
	libRepo    *sqlite.LibraryRepo
	sessionMgr *session.Manager
	wsHub      *ws.Hub
	cfg        *config.Config

	mu                  sync.Mutex
	authRates           map[string][]time.Time
	authIPRates         map[string][]time.Time
	loopbackAuthRates   map[string][]time.Time
	loopbackAuthIPRates map[string][]time.Time
	onUserDeleted       func(ctx context.Context, userID string)
}

func NewHandler(
	authSvc *auth.Service,
	userRepo *sqlite.UserRepo,
	sessionMgr *session.Manager,
	wsHub *ws.Hub,
	cfg *config.Config,
) *Handler {
	return &Handler{
		authSvc:             authSvc,
		userRepo:            userRepo,
		sessionMgr:          sessionMgr,
		wsHub:               wsHub,
		cfg:                 cfg,
		authRates:           make(map[string][]time.Time),
		authIPRates:         make(map[string][]time.Time),
		loopbackAuthRates:   make(map[string][]time.Time),
		loopbackAuthIPRates: make(map[string][]time.Time),
	}
}

// SetLibraryRepo attaches a LibraryRepo to enforce library RBAC and parental controls on playback reports.
func (h *Handler) SetLibraryRepo(libRepo *sqlite.LibraryRepo) {
	h.mu.Lock()
	h.libRepo = libRepo
	h.mu.Unlock()
}

// SetOnUserDeleted registers a cleanup callback invoked when a user account is deleted
// (used to purge in-memory/DB notifications and Watch-Together party rooms).
func (h *Handler) SetOnUserDeleted(fn func(ctx context.Context, userID string)) {
	h.mu.Lock()
	h.onUserDeleted = fn
	h.mu.Unlock()
}

func (h *Handler) getLibraryRepo() *sqlite.LibraryRepo {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.libRepo
}

func bucketLastAttempt(ts []time.Time) time.Time {
	var latest time.Time
	for _, t := range ts {
		if t.After(latest) {
			latest = t
		}
	}
	return latest
}

func pruneRateBucketsLocked(buckets map[string][]time.Time, cutoff time.Time) {
	for k, ts := range buckets {
		kept := ts[:0]
		for _, t := range ts {
			if t.After(cutoff) {
				kept = append(kept, t)
			}
		}
		if len(kept) == 0 {
			delete(buckets, k)
		} else {
			buckets[k] = kept
		}
	}
	for len(buckets) > authRateMaxBuckets {
		var oldestKey string
		var oldestTime time.Time
		first := true
		for k, ts := range buckets {
			last := bucketLastAttempt(ts)
			if first || last.Before(oldestTime) || (last.Equal(oldestTime) && k < oldestKey) {
				oldestKey = k
				oldestTime = last
				first = false
			}
		}
		if first {
			break
		}
		delete(buckets, oldestKey)
	}
}

func evictOldestRateBucketKeyLocked(buckets map[string][]time.Time, lockLimit int) bool {
	if len(buckets) == 0 {
		return false
	}
	var oldestKey string
	var oldestTime time.Time
	foundUnlocked := false
	if lockLimit > 0 {
		for k, ts := range buckets {
			if len(ts) >= lockLimit {
				continue
			}
			last := bucketLastAttempt(ts)
			if !foundUnlocked || last.Before(oldestTime) || (last.Equal(oldestTime) && k < oldestKey) {
				oldestKey = k
				oldestTime = last
				foundUnlocked = true
			}
		}
	}
	if !foundUnlocked {
		first := true
		for k, ts := range buckets {
			last := bucketLastAttempt(ts)
			if first || last.Before(oldestTime) || (last.Equal(oldestTime) && k < oldestKey) {
				oldestKey = k
				oldestTime = last
				first = false
			}
		}
		if first {
			return false
		}
	}
	delete(buckets, oldestKey)
	return true
}

func countIPSubBucketsLocked(buckets map[string][]time.Time, ipKey string) int {
	prefix := ipKey + "|"
	count := 0
	for k := range buckets {
		if strings.HasPrefix(k, prefix) {
			count++
		}
	}
	return count
}

func appendRateBucketWithEvictLocked(buckets map[string][]time.Time, key string, now time.Time, lockLimit int) {
	if _, exists := buckets[key]; !exists {
		for len(buckets) >= authRateMaxBuckets {
			if !evictOldestRateBucketKeyLocked(buckets, lockLimit) {
				break
			}
		}
	}
	buckets[key] = append(buckets[key], now)
}

func appendBoundedRateBucketLocked(buckets map[string][]time.Time, key string, now time.Time) {
	appendRateBucketWithEvictLocked(buckets, key, now, 0)
}

func recordAuthFailureBucketsLocked(compMap, ipMap map[string][]time.Time, compKey, ipKey string, now time.Time, compLimit, ipLimit int) {
	appendRateBucketWithEvictLocked(ipMap, ipKey, now, ipLimit)
	if compKey == ipKey {
		appendRateBucketWithEvictLocked(compMap, compKey, now, compLimit)
		return
	}
	if _, exists := compMap[compKey]; exists {
		compMap[compKey] = append(compMap[compKey], now)
		return
	}
	if countIPSubBucketsLocked(compMap, ipKey) >= maxSubBucketsPerIP {
		return
	}
	appendRateBucketWithEvictLocked(compMap, compKey, now, compLimit)
}

func isLoopbackClientIP(raw string) bool {
	s := strings.TrimSpace(raw)
	if s == "" {
		return false
	}
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return false
	}
	return addr.WithZone("").Unmap().IsLoopback()
}

func normalizeLoginRateKey(ip, username string) string {
	normIP := strings.TrimSpace(ip)
	if addr, err := netip.ParseAddr(normIP); err == nil {
		addr = addr.WithZone("").Unmap()
		if addr.Is6() {
			normIP = netip.PrefixFrom(addr, 64).Masked().String()
		} else {
			normIP = addr.String()
		}
	}
	normUser := strings.ToLower(strings.TrimSpace(username))
	if normUser == "" {
		return normIP
	}
	return normIP + "|" + normUser
}

func (h *Handler) checkAuthRateLimit(ip string, now time.Time) bool {
	if h.isLoginRateLimited(ip, "", now) {
		return false
	}
	h.recordLoginFailure(ip, "", now)
	return true
}

func (h *Handler) isLoginRateLimited(ip, username string, now time.Time) bool {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return false
	}
	loopback := isLoopbackClientIP(ip)
	key := normalizeLoginRateKey(ip, username)
	ipKey := normalizeLoginRateKey(ip, "")

	h.mu.Lock()
	defer h.mu.Unlock()
	if loopback {
		cutoff := now.Add(-authByNameLoopbackRateWindow)
		if h.loopbackAuthRates != nil {
			pruneRateBucketsLocked(h.loopbackAuthRates, cutoff)
			if len(h.loopbackAuthRates[key]) >= authByNameLoopbackRateLimit {
				return true
			}
			if username != "" && len(h.loopbackAuthRates[ipKey]) >= authByNameLoopbackRateLimit {
				return true
			}
		}
		if h.loopbackAuthIPRates != nil {
			pruneRateBucketsLocked(h.loopbackAuthIPRates, cutoff)
			if len(h.loopbackAuthIPRates[ipKey]) >= authByNameLoopbackIPRateLimit {
				return true
			}
		}
		return false
	}
	cutoff := now.Add(-authByNameRateWindow)
	if h.authRates != nil {
		pruneRateBucketsLocked(h.authRates, cutoff)
		if len(h.authRates[key]) >= authByNameRateLimit {
			return true
		}
		if username != "" && len(h.authRates[ipKey]) >= authByNameRateLimit {
			return true
		}
	}
	if h.authIPRates != nil {
		pruneRateBucketsLocked(h.authIPRates, cutoff)
		if len(h.authIPRates[ipKey]) >= authByNameIPRateLimit {
			return true
		}
	}
	return false
}

func (h *Handler) recordLoginFailure(ip, username string, now time.Time) {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return
	}
	loopback := isLoopbackClientIP(ip)
	key := normalizeLoginRateKey(ip, username)
	ipKey := normalizeLoginRateKey(ip, "")

	h.mu.Lock()
	defer h.mu.Unlock()
	if loopback {
		if h.loopbackAuthRates == nil {
			h.loopbackAuthRates = make(map[string][]time.Time)
		}
		if h.loopbackAuthIPRates == nil {
			h.loopbackAuthIPRates = make(map[string][]time.Time)
		}
		cutoff := now.Add(-authByNameLoopbackRateWindow)
		pruneRateBucketsLocked(h.loopbackAuthRates, cutoff)
		pruneRateBucketsLocked(h.loopbackAuthIPRates, cutoff)
		recordAuthFailureBucketsLocked(h.loopbackAuthRates, h.loopbackAuthIPRates, key, ipKey, now, authByNameLoopbackRateLimit, authByNameLoopbackIPRateLimit)
		return
	}
	if h.authRates == nil {
		h.authRates = make(map[string][]time.Time)
	}
	if h.authIPRates == nil {
		h.authIPRates = make(map[string][]time.Time)
	}
	cutoff := now.Add(-authByNameRateWindow)
	pruneRateBucketsLocked(h.authRates, cutoff)
	pruneRateBucketsLocked(h.authIPRates, cutoff)
	recordAuthFailureBucketsLocked(h.authRates, h.authIPRates, key, ipKey, now, authByNameRateLimit, authByNameIPRateLimit)
}

func (h *Handler) canAccessItem(ctx context.Context, ac *middleware.AuthContext, itemID string) (bool, error) {
	libRepo := h.getLibraryRepo()
	if libRepo == nil {
		return true, nil
	}
	if ac == nil {
		return false, nil
	}
	if ac.IsAdmin() {
		return true, nil
	}
	normID := guid.Normalize(itemID)
	if normID == "" {
		return true, nil
	}
	rec, err := libRepo.GetItemRecordByID(ctx, normID)
	if errors.Is(err, sqlite.ErrNotFound) {
		return false, nil
	}
	if err != nil || rec == nil {
		return false, err
	}
	if ac.User == nil {
		return false, nil
	}
	if !session.UserCanAccessLibrary(ctx, libRepo, ac.User, rec.LibraryID) {
		return false, nil
	}
	blocked, err := libRepo.IsItemBlockedByPolicy(ctx, rec, ac.User.Policy)
	if err != nil {
		return false, err
	}
	return !blocked, nil
}

func (h *Handler) disconnectUserSessions(_ context.Context, userID string) {
	norm := guid.Normalize(userID)
	if norm == "" {
		return
	}
	if h.sessionMgr != nil {
		h.sessionMgr.RemoveSessionsByUser(norm)
	}
	if h.wsHub != nil {
		h.wsHub.DisconnectUser(norm)
	}
}

func (h *Handler) disconnectUserSessionsExcept(userID, keepDeviceID, keepToken string) {
	norm := guid.Normalize(userID)
	if norm == "" {
		return
	}
	if h.sessionMgr != nil {
		for _, s := range h.sessionMgr.ListSessions() {
			if guid.Normalize(s.UserId) == norm && (keepDeviceID == "" || s.DeviceId != keepDeviceID) {
				h.sessionMgr.RemoveSession(s.UserId, s.DeviceId)
			}
		}
	}
	if h.wsHub != nil {
		h.wsHub.DisconnectUserExceptToken(norm, keepToken)
	}
}

func (h *Handler) countOtherActiveAdmins(ctx context.Context, excludeUserID string) (int, error) {
	if h.userRepo == nil {
		return 0, nil
	}
	all, err := h.userRepo.ListUsers(ctx)
	if err != nil {
		return 0, err
	}
	excludeNorm := guid.Normalize(excludeUserID)
	count := 0
	for _, u := range all {
		if guid.Normalize(u.ID) == excludeNorm {
			continue
		}
		if (u.IsAdmin || u.Policy.IsAdministrator) && !u.IsDisabled && !u.Policy.IsDisabled {
			count++
		}
	}
	return count, nil
}

func RegisterRoutes(r chi.Router, authMW *middleware.Auth, h *Handler) {
	// Public authentication routes
	r.Get("/Users/Public", h.ListPublicUsers)
	r.Post("/Users/AuthenticateByName", h.AuthenticateByName)

	// Authenticated user, session, device & display preference routes
	r.Group(func(pr chi.Router) {
		pr.Use(authMW.RequireAuth)

		pr.Get("/Users", h.ListUsers)
		pr.Get("/Users/{Id}", h.GetUser)
		pr.Post("/Users/{Id}/Password", h.UpdateUserPassword)
		pr.Post("/Users/{Id}/Configuration", h.UpdateUserConfiguration)

		pr.Get("/Sessions", h.ListSessions)
		pr.Post("/Sessions/Logout", h.Logout)
		pr.Post("/Sessions/Capabilities", h.UpdateCapabilities)
		pr.Post("/Sessions/Capabilities/Full", h.UpdateCapabilities)
		pr.Post("/Sessions/Playing", h.ReportPlaybackStart)
		pr.Post("/Sessions/Playing/Progress", h.ReportPlaybackProgress)
		pr.Post("/Sessions/Playing/Stopped", h.ReportPlaybackStopped)

		pr.Get("/Devices", h.ListDevices)

		pr.Get("/DisplayPreferences/{Id}", h.GetDisplayPreferences)
		pr.Post("/DisplayPreferences/{Id}", h.UpdateDisplayPreferences)
	})

	// Admin-only routes
	r.Group(func(ar chi.Router) {
		ar.Use(authMW.RequireAdmin)

		ar.Post("/Users/New", h.CreateUser)
		ar.Delete("/Users/{Id}", h.DeleteUser)
		ar.Post("/Users/{Id}/Policy", h.UpdateUserPolicy)

		ar.Get("/Auth/Keys", h.ListAPIKeys)
		ar.Post("/Auth/Keys", h.CreateAPIKey)
		ar.Delete("/Auth/Keys/{Key}", h.DeleteAPIKey)

		ar.Delete("/Devices", h.DeleteDevice)
	})
}

// --- Users & Auth ---

func (h *Handler) ListPublicUsers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	if !h.isLoginUserListEnabled() {
		httpx.JSON(w, http.StatusOK, []dto.UserDto{})
		return
	}
	users, err := h.userRepo.ListPublicUsers(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to list public users")
		return
	}
	out := make([]dto.UserDto, 0, len(users))
	for _, u := range users {
		d := h.authSvc.ToUserDto(u)
		// Strip sensitive policy and configuration details from unauthenticated public responses.
		d.Policy = dto.UserPolicy{
			EnabledFolders: []string{},
			BlockedTags:    []string{},
		}
		d.Configuration = dto.UserConfiguration{}
		out = append(out, d)
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) isLoginUserListEnabled() bool {
	cfg := h.cfg
	if cfg == nil && h.authSvc != nil {
		cfg = h.authSvc.Config()
	}
	if cfg == nil {
		return false
	}
	return cfg.Snapshot().EnableLoginUserList
}

type authenticateByNameRequest struct {
	Username string `json:"Username"`
	Pw       string `json:"Pw"`
	Password string `json:"Password"` // Legacy SHA1 field
}

const emptyPasswordSHA1 = "da39a3ee5e6b4b0d3255bfef95601890afd80709"

func verifyUserPassword(u *sqlite.UserRecord, pw, legacySHA1 string) bool {
	if u == nil {
		return false
	}
	if u.PasswordHash == "" {
		return pw == "" && (legacySHA1 == "" || strings.EqualFold(legacySHA1, emptyPasswordSHA1))
	}
	ok, err := auth.VerifyPassword(pw, u.PasswordHash)
	return err == nil && ok
}

func (h *Handler) isGlobalRemoteAccessEnabled() bool {
	cfg := h.cfg
	if cfg == nil && h.authSvc != nil {
		cfg = h.authSvc.Config()
	}
	if cfg == nil {
		return true
	}
	return cfg.Snapshot().IsGlobalRemoteAccessEnabled()
}

func (h *Handler) AuthenticateByName(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	isLocal := netutil.IsLocalOrLANRequest(r)
	if !isLocal && !h.isGlobalRemoteAccessEnabled() {
		httpx.Error(w, http.StatusForbidden, "Remote access is disabled on this server")
		return
	}
	now := time.Now().UTC()
	ip := netutil.ClientIP(r)
	if h.isLoginRateLimited(ip, "", now) {
		httpx.Error(w, http.StatusTooManyRequests, "Too many authentication attempts")
		return
	}
	var req authenticateByNameRequest
	if err := httpx.DecodeJSONLimit(r, &req, 64<<10); err != nil {
		h.recordLoginFailure(ip, "", now)
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if h.isLoginRateLimited(ip, req.Username, now) {
		httpx.Error(w, http.StatusTooManyRequests, "Too many authentication attempts")
		return
	}
	if h.userRepo != nil && !isLocal {
		u, lookupErr := h.userRepo.GetUserByName(r.Context(), strings.TrimSpace(req.Username))
		if lookupErr == nil && u != nil && !u.Policy.EnableRemoteAccess {
			if u.PasswordHash == "" {
				auth.DummyPasswordWork(req.Pw)
			} else {
				_, _ = auth.VerifyPassword(req.Pw, u.PasswordHash)
			}
			h.recordLoginFailure(ip, req.Username, now)
			_ = h.userRepo.CreateActivityLog(r.Context(), &sqlite.ActivityLogRecord{
				Name:     u.Name + " failed to log in (remote access disabled)",
				Type:     "AuthenticationFailed",
				UserID:   u.ID,
				Severity: "Warning",
			})
			httpx.Error(w, http.StatusUnauthorized, "Invalid username or password")
			return
		}
	}
	client := middleware.ExtractClientInfo(r)
	res, err := h.authSvc.AuthenticateByName(r.Context(), req.Username, req.Pw, req.Password, client)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidCredentials):
			h.recordLoginFailure(ip, req.Username, now)
			if h.userRepo != nil {
				uname := strings.TrimSpace(req.Username)
				if uname == "" {
					uname = "unknown"
				}
				_ = h.userRepo.CreateActivityLog(r.Context(), &sqlite.ActivityLogRecord{
					Name:     uname + " failed to log in",
					Type:     "AuthenticationFailed",
					Severity: "Warning",
				})
			}
			httpx.Error(w, http.StatusUnauthorized, "Invalid username or password")
		case errors.Is(err, auth.ErrUserDisabled):
			if h.userRepo != nil {
				uname := strings.TrimSpace(req.Username)
				if uname == "" {
					uname = "unknown"
				}
				_ = h.userRepo.CreateActivityLog(r.Context(), &sqlite.ActivityLogRecord{
					Name:     uname + " failed to log in (account disabled)",
					Type:     "AuthenticationFailed",
					Severity: "Warning",
				})
			}
			httpx.Error(w, http.StatusForbidden, "User account is disabled")
		default:
			httpx.Error(w, http.StatusInternalServerError, "Authentication failed")
		}
		return
	}

	if h.sessionMgr != nil {
		if u, err := h.userRepo.GetUserByID(r.Context(), res.User.Id); err == nil {
			client.DeviceID = res.SessionInfo.DeviceId
			client.Device = res.SessionInfo.DeviceName
			client.Client = res.SessionInfo.Client
			client.Version = res.SessionInfo.ApplicationVersion
			res.SessionInfo = h.sessionMgr.TouchSession(u, client)
		}
	}

	_ = h.userRepo.CreateActivityLog(r.Context(), &sqlite.ActivityLogRecord{
		Name:     res.User.Name + " has logged in",
		Type:     "AuthenticationSucceeded",
		UserID:   res.User.Id,
		Severity: "Info",
	})

	httpx.JSON(w, http.StatusOK, res)
}

func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
	ac, ok := middleware.AuthFromContext(r.Context())
	if !ok || ac == nil {
		httpx.Error(w, http.StatusUnauthorized, "Authentication required")
		return
	}
	if ac.IsAdmin() {
		users, err := h.userRepo.ListUsers(r.Context())
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "failed to list users")
			return
		}
		out := make([]dto.UserDto, 0, len(users))
		for _, u := range users {
			out = append(out, h.authSvc.ToUserDto(u))
		}
		httpx.JSON(w, http.StatusOK, out)
		return
	}
	if ac.User != nil {
		httpx.JSON(w, http.StatusOK, []dto.UserDto{h.authSvc.ToUserDto(ac.User)})
		return
	}
	httpx.JSON(w, http.StatusOK, []dto.UserDto{})
}

func (h *Handler) GetUser(w http.ResponseWriter, r *http.Request) {
	ac, ok := middleware.AuthFromContext(r.Context())
	if !ok || ac == nil || (!ac.IsAPIKey && ac.User == nil) {
		httpx.Error(w, http.StatusUnauthorized, "Authentication required")
		return
	}
	targetID := guid.Normalize(chi.URLParam(r, "Id"))
	if !ac.IsAdmin() && guid.Normalize(ac.UserID()) != targetID {
		httpx.Error(w, http.StatusForbidden, "Forbidden")
		return
	}
	u, err := h.userRepo.GetUserByID(r.Context(), targetID)
	if err != nil {
		if errors.Is(err, sqlite.ErrNotFound) {
			httpx.Error(w, http.StatusNotFound, "User not found")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "failed to get user")
		return
	}
	httpx.JSON(w, http.StatusOK, h.authSvc.ToUserDto(u))
}

type createUserRequest struct {
	Name     string `json:"Name"`
	Password string `json:"Password"`
	IsAdmin  bool   `json:"IsAdministrator"`
}

func (h *Handler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := httpx.DecodeJSONLimit(r, &req, 0); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if auth.ContainsControlChar(req.Name) || strings.TrimSpace(req.Name) == "" {
		httpx.Error(w, http.StatusBadRequest, "username is invalid or empty")
		return
	}
	u, err := h.authSvc.CreateUser(r.Context(), req.Name, req.Password, req.IsAdmin)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidUsername) {
			httpx.Error(w, http.StatusBadRequest, "username is invalid")
			return
		}
		if errors.Is(err, auth.ErrUserExists) {
			httpx.Error(w, http.StatusConflict, "User already exists")
			return
		}
		slog.Error("failed to create user", "username", req.Name, "error", err)
		httpx.Error(w, http.StatusInternalServerError, "failed to create user")
		return
	}
	if h.userRepo != nil {
		_ = h.userRepo.CreateActivityLog(r.Context(), &sqlite.ActivityLogRecord{
			Name:     u.Name + " was created",
			Type:     "UserCreated",
			UserID:   u.ID,
			Severity: "Info",
		})
	}
	httpx.JSON(w, http.StatusOK, h.authSvc.ToUserDto(u))
}

func (h *Handler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	ac, ok := middleware.AuthFromContext(r.Context())
	if !ok || ac == nil || !ac.IsAdmin() {
		httpx.Error(w, http.StatusForbidden, "Administrator privileges required")
		return
	}
	targetID := guid.Normalize(chi.URLParam(r, "Id"))
	if ac.User != nil && guid.Normalize(ac.User.ID) == targetID {
		httpx.Error(w, http.StatusBadRequest, "Cannot delete currently authenticated user")
		return
	}
	if err := h.userRepo.DeleteUserSafe(r.Context(), targetID); err != nil {
		if errors.Is(err, sqlite.ErrLastAdmin) {
			httpx.Error(w, http.StatusBadRequest, "Cannot delete the last administrator")
			return
		}
		if errors.Is(err, sqlite.ErrNotFound) {
			httpx.Error(w, http.StatusNotFound, "User not found")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "failed to delete user")
		return
	}
	if h.authSvc != nil {
		_ = h.authSvc.RevokeUserTokens(r.Context(), targetID)
		h.authSvc.InvalidateUserCache(targetID)
	} else {
		_ = h.userRepo.DeleteTokensByUser(r.Context(), targetID)
	}
	h.disconnectUserSessions(r.Context(), targetID)
	h.removeUserDiskArtifacts(targetID)
	h.mu.Lock()
	onDeleted := h.onUserDeleted
	h.mu.Unlock()
	if onDeleted != nil {
		onDeleted(r.Context(), targetID)
	}
	_ = h.userRepo.CreateActivityLog(r.Context(), &sqlite.ActivityLogRecord{
		Name:     "User " + targetID + " was deleted",
		Type:     "UserDeleted",
		UserID:   targetID,
		Severity: "Info",
	})
	httpx.NoContent(w)
}

func safeSingleSegmentUserDir(userID string) string {
	norm := guid.Normalize(userID)
	var b strings.Builder
	for _, r := range norm {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (h *Handler) removeUserDiskArtifacts(userID string) {
	cfg := h.cfg
	if cfg == nil && h.authSvc != nil {
		cfg = h.authSvc.Config()
	}
	if cfg == nil {
		return
	}
	dataDir := strings.TrimSpace(cfg.Snapshot().DataDir)
	safeUID := safeSingleSegmentUserDir(userID)
	if dataDir == "" || safeUID == "" {
		return
	}
	for _, rel := range []string{
		filepath.Join("metadata", "users", safeUID),
		filepath.Join("users", safeUID),
		filepath.Join("camera-uploads", safeUID),
	} {
		_ = os.RemoveAll(filepath.Join(dataDir, rel))
	}
}

type updatePasswordRequest struct {
	CurrentPw     string `json:"CurrentPw"`
	NewPw         string `json:"NewPw"`
	ResetPassword bool   `json:"ResetPassword"`
}

func (h *Handler) UpdateUserPassword(w http.ResponseWriter, r *http.Request) {
	ac, ok := middleware.AuthFromContext(r.Context())
	if !ok || ac == nil || (!ac.IsAPIKey && ac.User == nil) {
		httpx.Error(w, http.StatusUnauthorized, "Authentication required")
		return
	}
	targetID := guid.Normalize(chi.URLParam(r, "Id"))
	isSelf := !ac.IsAPIKey && ac.User != nil && guid.Normalize(ac.User.ID) == targetID
	if !ac.IsAdmin() && !isSelf {
		httpx.Error(w, http.StatusForbidden, "Forbidden")
		return
	}

	var req updatePasswordRequest
	if err := httpx.DecodeJSONLimit(r, &req, 0); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	newPw := req.NewPw
	if req.ResetPassword {
		newPw = ""
	}
	keepToken := ""
	if isSelf && strings.TrimSpace(ac.Token) != "" {
		keepToken = strings.TrimSpace(ac.Token)
	}
	err := h.authSvc.ChangePasswordKeepToken(r.Context(), targetID, req.CurrentPw, newPw, ac.IsAdmin(), keepToken)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrUserNotFound):
			httpx.Error(w, http.StatusNotFound, "User not found")
		case errors.Is(err, auth.ErrInvalidCredentials):
			httpx.Error(w, http.StatusUnauthorized, "Current password is incorrect")
		default:
			httpx.Error(w, http.StatusInternalServerError, "failed to update password")
		}
		return
	}
	if keepToken != "" {
		h.disconnectUserSessionsExcept(targetID, ac.Client.DeviceID, keepToken)
	} else {
		h.disconnectUserSessions(r.Context(), targetID)
	}
	if h.userRepo != nil {
		_ = h.userRepo.CreateActivityLog(r.Context(), &sqlite.ActivityLogRecord{
			Name:     "Password changed",
			Type:     "UserPasswordChanged",
			UserID:   targetID,
			Severity: "Info",
		})
	}
	httpx.NoContent(w)
}

func hasRemovedEnabledFolder(oldFolders, newFolders []string) bool {
	if len(oldFolders) == 0 {
		return false
	}
	newSet := make(map[string]struct{}, len(newFolders))
	for _, f := range newFolders {
		if norm := guid.Normalize(f); norm != "" {
			newSet[norm] = struct{}{}
		}
	}
	for _, f := range oldFolders {
		norm := guid.Normalize(f)
		if norm == "" {
			continue
		}
		if _, ok := newSet[norm]; !ok {
			return true
		}
	}
	return false
}

func policyRequiresWSDisconnect(oldPol dto.UserPolicy, wasAdmin bool, newPol dto.UserPolicy) bool {
	if newPol.IsDisabled {
		return true
	}
	if wasAdmin && !newPol.IsAdministrator {
		return true
	}
	if oldPol.EnableRemoteAccess && !newPol.EnableRemoteAccess {
		return true
	}
	if oldPol.EnableLiveTvAccess && !newPol.EnableLiveTvAccess {
		return true
	}
	if oldPol.EnableAllFolders && !newPol.EnableAllFolders {
		return true
	}
	if !oldPol.EnableAllFolders && !newPol.EnableAllFolders && hasRemovedEnabledFolder(oldPol.EnabledFolders, newPol.EnabledFolders) {
		return true
	}
	return false
}

func (h *Handler) UpdateUserPolicy(w http.ResponseWriter, r *http.Request) {
	targetID := guid.Normalize(chi.URLParam(r, "Id"))
	var policy dto.UserPolicy
	if err := httpx.DecodeJSONLimit(r, &policy, 0); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid policy body")
		return
	}
	existing, err := h.userRepo.GetUserByID(r.Context(), targetID)
	if err != nil {
		if errors.Is(err, sqlite.ErrNotFound) {
			httpx.Error(w, http.StatusNotFound, "User not found")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "failed to load user")
		return
	}
	wasAdmin := existing.IsAdmin || existing.Policy.IsAdministrator
	if err := h.userRepo.UpdateUserPolicySafe(r.Context(), targetID, policy); err != nil {
		if errors.Is(err, sqlite.ErrLastAdmin) {
			httpx.Error(w, http.StatusBadRequest, "Cannot revoke or disable the last administrator")
			return
		}
		if errors.Is(err, sqlite.ErrNotFound) {
			httpx.Error(w, http.StatusNotFound, "User not found")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "failed to update user policy")
		return
	}
	demoted := wasAdmin && !policy.IsAdministrator
	if policy.IsDisabled {
		if h.authSvc != nil {
			_ = h.authSvc.RevokeUserTokens(r.Context(), targetID)
		} else {
			_ = h.userRepo.DeleteTokensByUser(r.Context(), targetID)
		}
		h.disconnectUserSessions(r.Context(), targetID)
	} else if demoted {
		h.disconnectUserSessions(r.Context(), targetID)
	} else if policyRequiresWSDisconnect(existing.Policy, wasAdmin, policy) {
		if h.wsHub != nil {
			h.wsHub.DisconnectUser(targetID)
		}
	}
	if h.authSvc != nil {
		h.authSvc.InvalidateUserCache(targetID)
	}
	httpx.NoContent(w)
}

func (h *Handler) UpdateUserConfiguration(w http.ResponseWriter, r *http.Request) {
	ac, ok := middleware.AuthFromContext(r.Context())
	if !ok || ac == nil || (!ac.IsAPIKey && ac.User == nil) {
		httpx.Error(w, http.StatusUnauthorized, "Authentication required")
		return
	}
	targetID := guid.Normalize(chi.URLParam(r, "Id"))
	if !ac.IsAdmin() && guid.Normalize(ac.UserID()) != targetID {
		httpx.Error(w, http.StatusForbidden, "Forbidden")
		return
	}
	var cfg dto.UserConfiguration
	if err := httpx.DecodeJSONLimit(r, &cfg, 0); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid configuration body")
		return
	}
	if err := h.userRepo.UpdateUserConfig(r.Context(), targetID, cfg); err != nil {
		if errors.Is(err, sqlite.ErrNotFound) {
			httpx.Error(w, http.StatusNotFound, "User not found")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "failed to update user configuration")
		return
	}
	h.authSvc.InvalidateUserCache(targetID)
	httpx.NoContent(w)
}

// --- API Keys ---

type APIKeyDto struct {
	Id          string    `json:"Id,omitempty"`
	AccessToken string    `json:"AccessToken"`
	MaskedToken string    `json:"MaskedToken,omitempty"`
	AppName     string    `json:"AppName"`
	DateCreated time.Time `json:"DateCreated"`
	IsActive    bool      `json:"IsActive"`
}

func (h *Handler) ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	keys, err := h.authSvc.ListAPIKeys(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to list api keys")
		return
	}
	items := make([]APIKeyDto, 0, len(keys))
	for _, k := range keys {
		masked := sqlite.MaskAPIKey(k.Token)
		items = append(items, APIKeyDto{
			Id:          k.KeyID,
			AccessToken: masked,
			MaskedToken: masked,
			AppName:     k.AppName,
			DateCreated: k.CreatedAt,
			IsActive:    true,
		})
	}
	httpx.JSON(w, http.StatusOK, dto.QueryResult[APIKeyDto]{
		Items:            items,
		TotalRecordCount: len(items),
	})
}

func (h *Handler) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	q := httpx.ParseQuery(r)
	appName := q.Get("App")
	if appName == "" && r.Body != nil {
		var body struct {
			App     string `json:"App"`
			AppName string `json:"AppName"`
		}
		if err := httpx.DecodeJSONLimit(r, &body, 0); err == nil {
			if body.App != "" {
				appName = body.App
			} else if body.AppName != "" {
				appName = body.AppName
			}
		}
	}
	rec, err := h.authSvc.CreateAPIKey(r.Context(), appName)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to create api key")
		return
	}
	httpx.JSON(w, http.StatusOK, APIKeyDto{
		Id:          rec.KeyID,
		AccessToken: rec.Token,
		MaskedToken: sqlite.MaskAPIKey(rec.Token),
		AppName:     rec.AppName,
		DateCreated: rec.CreatedAt,
		IsActive:    true,
	})
}

func (h *Handler) DeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(chi.URLParam(r, "Key"))
	var deletedToken string
	if h.userRepo != nil {
		var err error
		deletedToken, err = h.userRepo.DeleteAPIKeyByIDOrToken(r.Context(), key)
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "failed to delete api key")
			return
		}
	} else if h.authSvc != nil {
		if err := h.authSvc.RevokeAPIKey(r.Context(), key); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "failed to delete api key")
			return
		}
		deletedToken = key
	}
	evictTok := deletedToken
	if evictTok == "" {
		evictTok = key
	}
	if h.authSvc != nil {
		h.authSvc.InvalidateAPIKey(evictTok)
	}
	if h.wsHub != nil {
		h.wsHub.DisconnectToken(evictTok)
	}
	httpx.NoContent(w)
}

// --- Sessions & Playback ---

func (h *Handler) ListSessions(w http.ResponseWriter, r *http.Request) {
	if h.sessionMgr == nil {
		httpx.JSON(w, http.StatusOK, []dto.SessionInfo{})
		return
	}
	ac, ok := middleware.AuthFromContext(r.Context())
	if !ok || ac == nil || (!ac.IsAPIKey && ac.User == nil) {
		httpx.Error(w, http.StatusUnauthorized, "Authentication required")
		return
	}
	isAdmin := ac.IsAdmin()
	callerID := guid.Normalize(ac.UserID())

	q := httpx.ParseQuery(r)
	deviceID := q.Get("DeviceId")
	userID := guid.Normalize(q.Get("ControllableByUserId"))
	activeWithin := q.Int("ActiveWithinSeconds", 0)
	var activeCutoff time.Time
	if activeWithin > 0 {
		activeCutoff = time.Now().UTC().Add(-time.Duration(activeWithin) * time.Second)
	}

	all := h.sessionMgr.ListSessions()
	out := make([]dto.SessionInfo, 0, len(all))
	for _, s := range all {
		if !isAdmin && guid.Normalize(s.UserId) != callerID {
			continue
		}
		if deviceID != "" && s.DeviceId != deviceID {
			continue
		}
		if userID != "" && guid.Normalize(s.UserId) != userID {
			continue
		}
		if !activeCutoff.IsZero() && s.LastActivityDate.Before(activeCutoff) {
			continue
		}
		if !isAdmin && s.NowPlayingItem != nil {
			cp := *s.NowPlayingItem
			dto.SanitizeItemPaths(&cp)
			s.NowPlayingItem = &cp
		}
		out = append(out, s)
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	ac, ok := middleware.AuthFromContext(r.Context())
	if !ok || ac == nil {
		httpx.Error(w, http.StatusUnauthorized, "Authentication required")
		return
	}
	if ac.Token != "" && !ac.IsAPIKey {
		_ = h.authSvc.RevokeToken(r.Context(), ac.Token)
		if h.sessionMgr != nil && ac.User != nil {
			h.sessionMgr.RemoveSession(ac.User.ID, ac.Client.DeviceID)
		}
		if h.wsHub != nil {
			h.wsHub.DisconnectToken(ac.Token)
		}
	}
	httpx.NoContent(w)
}

func (h *Handler) UpdateCapabilities(w http.ResponseWriter, r *http.Request) {
	ac, ok := middleware.AuthFromContext(r.Context())
	if !ok || ac == nil || (!ac.IsAPIKey && ac.User == nil) {
		httpx.Error(w, http.StatusUnauthorized, "Authentication required")
		return
	}
	var body struct {
		PlayableMediaTypes    []string `json:"PlayableMediaTypes"`
		SupportedCommands     []string `json:"SupportedCommands"`
		SupportsRemoteControl *bool    `json:"SupportsRemoteControl"`
		SupportsMediaControl  *bool    `json:"SupportsMediaControl"`
		SupportsSync          *bool    `json:"SupportsSync"`
	}
	if r.Body != nil {
		_ = httpx.DecodeJSONLimit(r, &body, 0)
	}
	q := httpx.ParseQuery(r)
	req := session.CapabilitiesRequest{
		PlayableMediaTypes: body.PlayableMediaTypes,
		SupportedCommands:  body.SupportedCommands,
	}
	if len(req.PlayableMediaTypes) == 0 {
		if types := q.List("PlayableMediaTypes"); len(types) > 0 {
			req.PlayableMediaTypes = types
		}
	}
	if len(req.SupportedCommands) == 0 {
		if cmds := q.List("SupportedCommands"); len(cmds) > 0 {
			req.SupportedCommands = cmds
		}
	}
	switch {
	case body.SupportsRemoteControl != nil:
		req.SupportsRemoteControl = *body.SupportsRemoteControl
	case body.SupportsMediaControl != nil:
		req.SupportsRemoteControl = *body.SupportsMediaControl
	default:
		if b := q.Bool("SupportsMediaControl"); b != nil {
			req.SupportsRemoteControl = *b
		} else if b := q.Bool("SupportsRemoteControl"); b != nil {
			req.SupportsRemoteControl = *b
		}
	}
	if body.SupportsSync == nil {
		_ = q.Bool("SupportsSync")
	}
	if h.sessionMgr != nil && ac != nil && ac.User != nil {
		h.sessionMgr.UpdateCapabilities(ac.User, ac.Client, req)
	}
	httpx.NoContent(w)
}

func (h *Handler) ReportPlaybackStart(w http.ResponseWriter, r *http.Request) {
	ac, ok := middleware.AuthFromContext(r.Context())
	if !ok || ac == nil || (!ac.IsAPIKey && ac.User == nil) {
		httpx.Error(w, http.StatusUnauthorized, "Authentication required")
		return
	}
	var req session.PlaybackStartRequest
	if err := httpx.DecodeJSONLimit(r, &req, 0); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid playback start payload")
		return
	}
	itemID := req.ItemId
	if itemID == "" && req.Item != nil {
		itemID = req.Item.Id
	}
	allowed, err := h.canAccessItem(r.Context(), ac, itemID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to verify item access")
		return
	}
	if !allowed {
		httpx.Error(w, http.StatusForbidden, "Access denied")
		return
	}
	if h.sessionMgr != nil && ac.User != nil {
		_ = h.sessionMgr.ReportPlaybackStart(r.Context(), ac.User, ac.Client, req)
	}
	httpx.NoContent(w)
}

func (h *Handler) ReportPlaybackProgress(w http.ResponseWriter, r *http.Request) {
	ac, ok := middleware.AuthFromContext(r.Context())
	if !ok || ac == nil || (!ac.IsAPIKey && ac.User == nil) {
		httpx.Error(w, http.StatusUnauthorized, "Authentication required")
		return
	}
	var req session.PlaybackProgressRequest
	if err := httpx.DecodeJSONLimit(r, &req, 0); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid playback progress payload")
		return
	}
	allowed, err := h.canAccessItem(r.Context(), ac, req.ItemId)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to verify item access")
		return
	}
	if !allowed {
		httpx.Error(w, http.StatusForbidden, "Access denied")
		return
	}
	if h.sessionMgr != nil && ac.User != nil {
		_ = h.sessionMgr.ReportPlaybackProgress(r.Context(), ac.User, ac.Client, req)
	}
	httpx.NoContent(w)
}

func (h *Handler) ReportPlaybackStopped(w http.ResponseWriter, r *http.Request) {
	ac, ok := middleware.AuthFromContext(r.Context())
	if !ok || ac == nil || (!ac.IsAPIKey && ac.User == nil) {
		httpx.Error(w, http.StatusUnauthorized, "Authentication required")
		return
	}
	var req session.PlaybackStoppedRequest
	if err := httpx.DecodeJSONLimit(r, &req, 0); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid playback stopped payload")
		return
	}
	allowed, err := h.canAccessItem(r.Context(), ac, req.ItemId)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to verify item access")
		return
	}
	if !allowed {
		httpx.Error(w, http.StatusForbidden, "Access denied")
		return
	}
	if h.sessionMgr != nil && ac.User != nil {
		_ = h.sessionMgr.ReportPlaybackStopped(r.Context(), ac.User, ac.Client, req)
	}
	httpx.NoContent(w)
}

// --- Devices ---

type DeviceInfoDto struct {
	Name             string    `json:"Name"`
	Id               string    `json:"Id"`
	LastUserName     string    `json:"LastUserName"`
	AppName          string    `json:"AppName"`
	AppVersion       string    `json:"AppVersion"`
	LastUserId       string    `json:"LastUserId"`
	DateLastActivity time.Time `json:"DateLastActivity"`
}

func (h *Handler) ListDevices(w http.ResponseWriter, r *http.Request) {
	ac, ok := middleware.AuthFromContext(r.Context())
	if !ok || ac == nil || (!ac.IsAPIKey && ac.User == nil) {
		httpx.Error(w, http.StatusUnauthorized, "Authentication required")
		return
	}
	isAdmin := ac.IsAdmin()
	callerID := guid.Normalize(ac.UserID())

	q := httpx.ParseQuery(r)
	filterUserID := guid.Normalize(q.Get("UserId"))
	_ = q.Bool("SupportsSync")

	devices, err := h.userRepo.ListDevices(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to list devices")
		return
	}
	items := make([]DeviceInfoDto, 0, len(devices))
	for _, d := range devices {
		if !isAdmin && guid.Normalize(d.LastUserID) != callerID {
			continue
		}
		if filterUserID != "" && guid.Normalize(d.LastUserID) != filterUserID {
			continue
		}
		items = append(items, DeviceInfoDto{
			Name:             d.Name,
			Id:               d.ID,
			LastUserName:     d.LastUserName,
			AppName:          d.AppName,
			AppVersion:       d.AppVersion,
			LastUserId:       d.LastUserID,
			DateLastActivity: d.DateLastActivity,
		})
	}
	httpx.JSON(w, http.StatusOK, dto.QueryResult[DeviceInfoDto]{
		Items:            items,
		TotalRecordCount: len(items),
	})
}

func (h *Handler) DeleteDevice(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	deviceID := strings.TrimSpace(q.Get("Id"))
	if deviceID == "" {
		httpx.Error(w, http.StatusBadRequest, "missing Id query parameter")
		return
	}
	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil && !ac.IsAdmin() {
		callerID := guid.Normalize(ac.UserID())
		if callerID == "" {
			httpx.Error(w, http.StatusForbidden, "Forbidden")
			return
		}
		if h.userRepo != nil && h.userRepo.Store() != nil && h.userRepo.Store().Writer != nil {
			_, _ = h.userRepo.Store().Writer.ExecContext(r.Context(),
				`DELETE FROM access_tokens WHERE user_id = ? AND device_id = ?`, callerID, deviceID)
		}
		if h.authSvc != nil {
			h.authSvc.InvalidateUserCache(callerID)
		}
		if h.sessionMgr != nil {
			h.sessionMgr.RemoveSession(callerID, deviceID)
		}
		if h.wsHub != nil {
			h.wsHub.DisconnectUserDevice(callerID, deviceID)
		}
		httpx.NoContent(w)
		return
	}
	if err := h.authSvc.RevokeDevice(r.Context(), deviceID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to revoke device")
		return
	}
	if h.userRepo != nil {
		_ = h.userRepo.DeleteDeviceArtifacts(r.Context(), deviceID)
	}
	if h.sessionMgr != nil {
		h.sessionMgr.RemoveSessionByDevice(deviceID)
	}
	if h.wsHub != nil {
		h.wsHub.DisconnectDevice(deviceID)
	}
	httpx.NoContent(w)
}

// --- Display Preferences ---

type DisplayPreferencesDto struct {
	Id                 string            `json:"Id"`
	SortBy             string            `json:"SortBy"`
	SortOrder          string            `json:"SortOrder"`
	Client             string            `json:"Client"`
	CustomPrefs        map[string]string `json:"CustomPrefs"`
	PrimaryImageHeight int               `json:"PrimaryImageHeight"`
	PrimaryImageWidth  int               `json:"PrimaryImageWidth"`
	ScrollDirection    string            `json:"ScrollDirection"`
	ShowBackdrop       bool              `json:"ShowBackdrop"`
	RememberIndexing   bool              `json:"RememberIndexing"`
	RememberSorting    bool              `json:"RememberSorting"`
	ShowSidebar        bool              `json:"ShowSidebar"`
}

func (h *Handler) GetDisplayPreferences(w http.ResponseWriter, r *http.Request) {
	ac, ok := middleware.AuthFromContext(r.Context())
	if !ok || ac == nil || (!ac.IsAPIKey && ac.User == nil) {
		httpx.Error(w, http.StatusUnauthorized, "Authentication required")
		return
	}
	id := chi.URLParam(r, "Id")
	q := httpx.ParseQuery(r)
	userID := guid.Normalize(q.Get("userId"))
	callerID := guid.Normalize(ac.UserID())
	if userID == "" {
		userID = callerID
	}
	if !ac.IsAdmin() && userID != "" && userID != callerID {
		httpx.Error(w, http.StatusForbidden, "Forbidden")
		return
	}
	client := q.Get("client")
	if client == "" {
		client = ac.Client.Client
	}
	if client == "" {
		client = "emby"
	}

	rec, err := h.userRepo.GetDisplayPreferences(r.Context(), id, userID, client)
	if err == nil && rec != nil && rec.DataJSON != "" && rec.DataJSON != "{}" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(rec.DataJSON))
		return
	}
	if err != nil && !errors.Is(err, sqlite.ErrNotFound) {
		httpx.Error(w, http.StatusInternalServerError, "failed to get display preferences")
		return
	}

	httpx.JSON(w, http.StatusOK, DisplayPreferencesDto{
		Id:                 id,
		SortBy:             "SortName",
		SortOrder:          "Ascending",
		Client:             client,
		CustomPrefs:        map[string]string{},
		PrimaryImageHeight: 250,
		PrimaryImageWidth:  250,
		ScrollDirection:    "Horizontal",
		ShowBackdrop:       true,
	})
}

func (h *Handler) UpdateDisplayPreferences(w http.ResponseWriter, r *http.Request) {
	ac, ok := middleware.AuthFromContext(r.Context())
	if !ok || ac == nil || (!ac.IsAPIKey && ac.User == nil) {
		httpx.Error(w, http.StatusUnauthorized, "Authentication required")
		return
	}
	id := chi.URLParam(r, "Id")
	q := httpx.ParseQuery(r)
	userID := guid.Normalize(q.Get("userId"))
	if userID == "" {
		userID = guid.Normalize(ac.UserID())
	}
	if !ac.IsAdmin() && userID != guid.Normalize(ac.UserID()) {
		httpx.Error(w, http.StatusForbidden, "Forbidden")
		return
	}
	client := q.Get("client")
	if client == "" {
		client = ac.Client.Client
	}
	if client == "" {
		client = "emby"
	}

	raw, err := io.ReadAll(io.LimitReader(r.Body, 512<<10))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "failed to read body")
		return
	}
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	if !json.Valid(raw) {
		httpx.Error(w, http.StatusBadRequest, "invalid json body")
		return
	}

	err = h.userRepo.UpsertDisplayPreferences(r.Context(), &sqlite.DisplayPreferencesRecord{
		ID:       id,
		UserID:   userID,
		Client:   client,
		DataJSON: string(raw),
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to save display preferences")
		return
	}
	httpx.NoContent(w)
}
