package compatcore

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/core/session"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/pkg/netutil"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

// --- User DTO helper ---

func (h *Handler) toUserDto(u *sqlite.UserRecord) dto.UserDto {
	hasPw := u.PasswordHash != ""
	var dataDir string
	if h.cfg != nil {
		dataDir = h.cfg.Snapshot().DataDir
	}
	return dto.UserDto{
		Name:                      u.Name,
		ServerId:                  h.serverID(),
		Id:                        u.ID,
		PrimaryImageTag:           auth.UserPrimaryImageTag(dataDir, u.ID),
		HasPassword:               hasPw,
		HasConfiguredPassword:     hasPw,
		HasConfiguredEasyPassword: false,
		LastLoginDate:             u.LastLoginAt,
		LastActivityDate:          u.LastActiveAt,
		Configuration:             u.Config,
		Policy:                    u.Policy,
	}
}

// --- /Users public & management ---

func forgotClientIP(r *http.Request) string {
	ip := strings.TrimSpace(netutil.ClientIP(r))
	if ip == "" {
		return ""
	}
	if addr, err := netip.ParseAddr(ip); err == nil {
		addr = addr.WithZone("").Unmap()
		if addr.Is6() {
			return netip.PrefixFrom(addr, 64).Masked().String()
		}
		return addr.String()
	}
	return ip
}

func prunePasswordResetFiles(dataDir string, maxKeep int) {
	if dataDir == "" || maxKeep <= 0 {
		return
	}
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return
	}
	type pinFileInfo struct {
		path    string
		modTime time.Time
	}
	var files []pinFileInfo
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, "passwordreset-") && strings.HasSuffix(name, ".txt") {
			info, err := e.Info()
			if err != nil {
				continue
			}
			files = append(files, pinFileInfo{
				path:    filepath.Join(dataDir, name),
				modTime: info.ModTime(),
			})
		}
	}
	if len(files) <= maxKeep {
		return
	}
	sort.Slice(files, func(i, j int) bool {
		if !files[i].modTime.Equal(files[j].modTime) {
			return files[i].modTime.After(files[j].modTime)
		}
		return files[i].path > files[j].path
	})
	for _, f := range files[maxKeep:] {
		_ = os.Remove(f.path)
	}
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

func pruneRateBucketLocked(bucket map[string][]time.Time, cutoff time.Time, maxEntries int) {
	for k, ts := range bucket {
		kept := ts[:0]
		for _, t := range ts {
			if t.After(cutoff) {
				kept = append(kept, t)
			}
		}
		if len(kept) == 0 {
			delete(bucket, k)
		} else {
			bucket[k] = kept
		}
	}
	for maxEntries > 0 && len(bucket) > maxEntries {
		var oldestKey string
		var oldestTime time.Time
		first := true
		for k, ts := range bucket {
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
		delete(bucket, oldestKey)
	}
}

func evictOldestRateKeyLocked(bucket map[string][]time.Time, lockLimit int) bool {
	if len(bucket) == 0 {
		return false
	}
	var oldestKey string
	var oldestTime time.Time
	foundUnlocked := false
	if lockLimit > 0 {
		for k, ts := range bucket {
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
		for k, ts := range bucket {
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
	delete(bucket, oldestKey)
	return true
}

func countIPSubBucketsLocked(bucket map[string][]time.Time, ipKey string) int {
	prefix := ipKey + "|"
	count := 0
	for k := range bucket {
		if strings.HasPrefix(k, prefix) {
			count++
		}
	}
	return count
}

func appendRateBucketWithEvictLocked(bucket map[string][]time.Time, key string, now time.Time, maxEntries, lockLimit int) {
	if _, exists := bucket[key]; !exists && maxEntries > 0 {
		for len(bucket) >= maxEntries {
			if !evictOldestRateKeyLocked(bucket, lockLimit) {
				break
			}
		}
	}
	bucket[key] = append(bucket[key], now)
}

func appendRateBucketLocked(bucket map[string][]time.Time, key string, now time.Time, maxEntries int) {
	appendRateBucketWithEvictLocked(bucket, key, now, maxEntries, 0)
}

func generateSixDigitPin() string {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		var b [4]byte
		_, _ = rand.Read(b[:])
		v := (int(b[0])<<24 | int(b[1])<<16 | int(b[2])<<8 | int(b[3])) & 0x7fffffff
		return fmt.Sprintf("%06d", v%1_000_000)
	}
	return fmt.Sprintf("%06d", n.Int64())
}

func (h *Handler) forgotPassword(w http.ResponseWriter, r *http.Request) {
	if !netutil.IsLocalOrLANRequest(r) {
		writeJSONError(w, r, http.StatusForbidden, "password reset is only available on the local network")
		return
	}
	now := time.Now().UTC()
	ip := forgotClientIP(r)

	h.mu.Lock()
	if h.forgotRates == nil {
		h.forgotRates = make(map[string][]time.Time)
	}
	pruneRateBucketLocked(h.forgotRates, now.Add(-forgotPasswordRateWindow), maxRateLimitBuckets)
	if ip != "" {
		_, exists := h.forgotRates[ip]
		if len(h.forgotRates[ip]) >= forgotPasswordRateLimit || (!exists && len(h.forgotRates) >= maxRateLimitBuckets) {
			h.mu.Unlock()
			httpx.Error(w, http.StatusTooManyRequests, "too many password reset requests")
			return
		}
		appendRateBucketLocked(h.forgotRates, ip, now, maxRateLimitBuckets)
	}
	h.mu.Unlock()

	var body struct {
		EnteredUsername string `json:"EnteredUsername"`
	}
	_ = decodeOptionalJSON(r, &body)

	pin := generateSixDigitPin()
	expiresAt := now.Add(30 * time.Minute)

	uid := ""
	uname := strings.TrimSpace(body.EnteredUsername)
	if h.userRepo != nil && uname != "" {
		if u, err := h.userRepo.GetUserByName(r.Context(), uname); err == nil && u != nil {
			uid = u.ID
			uname = u.Name
		}
	}

	dataDir := ""
	if h.cfg != nil {
		dataDir = h.cfg.Snapshot().DataDir
	}
	pinFile := ""
	if dataDir != "" {
		pinFile = filepath.Join(dataDir, "passwordreset-"+guid.New()[:12]+".txt")
	}

	var expiredFiles []string
	h.mu.Lock()
	for k, existing := range h.forgotPins {
		if !now.Before(existing.ExpiresAt) || (uid != "" && existing.UserID == uid) {
			if existing.PinFile != "" {
				expiredFiles = append(expiredFiles, existing.PinFile)
			}
			delete(h.forgotPins, k)
		}
	}
	if uid != "" {
		for {
			if _, collision := h.forgotPins[pin]; !collision {
				break
			}
			pin = generateSixDigitPin()
		}
		if len(h.forgotPins) >= forgotPinsMaxEntries {
			var oldestKey string
			var oldestExp time.Time
			for k, existing := range h.forgotPins {
				if oldestKey == "" || existing.ExpiresAt.Before(oldestExp) {
					oldestKey = k
					oldestExp = existing.ExpiresAt
				}
			}
			if oldestKey != "" {
				if oldFile := h.forgotPins[oldestKey].PinFile; oldFile != "" {
					expiredFiles = append(expiredFiles, oldFile)
				}
				delete(h.forgotPins, oldestKey)
			}
		}
		h.forgotPins[pin] = forgotPinEntry{
			UserID:    uid,
			UserName:  uname,
			Pin:       pin,
			PinFile:   pinFile,
			ExpiresAt: expiresAt,
		}
	}
	h.mu.Unlock()

	for _, f := range expiredFiles {
		_ = os.Remove(f)
	}

	if uid != "" && pinFile != "" {
		content := fmt.Sprintf("Ambra Password Reset PIN\nUser: %s\nPIN: %s\nExpiresAt: %s\n", uname, pin, expiresAt.Format(time.RFC3339))
		_ = os.WriteFile(pinFile, []byte(content), 0o600)
		prunePasswordResetFiles(dataDir, forgotPasswordMaxPinFiles)
	}

	publicPinFile := ""
	if pinFile != "" {
		publicPinFile = filepath.Base(pinFile)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"Action":            "PinCode",
		"PinFile":           publicPinFile,
		"PinExpirationDate": expiresAt.Format(time.RFC3339),
	})
}

func (h *Handler) checkIPRateLimit(bucket map[string][]time.Time, ip string, now time.Time, window time.Duration, limit int) bool {
	if ip == "" {
		return true
	}
	pruneRateBucketLocked(bucket, now.Add(-window), maxRateLimitBuckets)
	_, exists := bucket[ip]
	if len(bucket[ip]) >= limit || (!exists && len(bucket) >= maxRateLimitBuckets) {
		return false
	}
	appendRateBucketLocked(bucket, ip, now, maxRateLimitBuckets)
	return true
}

func (h *Handler) forgotPasswordPin(w http.ResponseWriter, r *http.Request) {
	if !netutil.IsLocalOrLANRequest(r) {
		writeJSONError(w, r, http.StatusForbidden, "password reset is only available on the local network")
		return
	}
	now := time.Now().UTC()
	ip := forgotClientIP(r)

	h.mu.Lock()
	if h.forgotPinRates == nil {
		h.forgotPinRates = make(map[string][]time.Time)
	}
	if !h.checkIPRateLimit(h.forgotPinRates, ip, now, forgotPasswordRateWindow, forgotPinMaxAttempts) {
		h.mu.Unlock()
		httpx.Error(w, http.StatusTooManyRequests, "too many pin attempts")
		return
	}
	h.mu.Unlock()

	var body struct {
		Pin string `json:"Pin"`
	}
	_ = decodeOptionalJSON(r, &body)
	pin := strings.TrimSpace(body.Pin)

	var (
		matched      *forgotPinEntry
		filesToClean []string
	)

	h.mu.Lock()
	// Prune expired entries first (each PIN entry is isolated to its own expiry window).
	for k, e := range h.forgotPins {
		if !now.Before(e.ExpiresAt) {
			if e.PinFile != "" {
				filesToClean = append(filesToClean, e.PinFile)
			}
			delete(h.forgotPins, k)
		}
	}
	if pin != "" {
		for k, entry := range h.forgotPins {
			if entry.UserID != "" && len(pin) == len(k) && subtle.ConstantTimeCompare([]byte(pin), []byte(k)) == 1 {
				cp := entry
				matched = &cp
				delete(h.forgotPins, k)
				if entry.PinFile != "" {
					filesToClean = append(filesToClean, entry.PinFile)
				}
				break
			}
		}
	}
	if matched == nil && len(h.forgotPins) > 0 {
		for k, entry := range h.forgotPins {
			entry.failedAttempts++
			if entry.failedAttempts >= forgotPinMaxFailedAttempts {
				if entry.PinFile != "" {
					filesToClean = append(filesToClean, entry.PinFile)
				}
				delete(h.forgotPins, k)
			} else {
				h.forgotPins[k] = entry
			}
		}
	}
	h.mu.Unlock()

	for _, f := range filesToClean {
		_ = os.Remove(f)
	}

	resetUsers := []string{}
	success := false
	if matched != nil {
		if h.authSvc != nil {
			if err := h.authSvc.ChangePassword(r.Context(), matched.UserID, "", "", true); err != nil {
				httpx.Error(w, http.StatusInternalServerError, "failed to reset password")
				return
			}
		}
		h.revokeAndDisconnectUser(r.Context(), matched.UserID)
		if h.userRepo != nil {
			_ = h.userRepo.CreateActivityLog(r.Context(), &sqlite.ActivityLogRecord{
				Name:     fmt.Sprintf("Password reset via PIN for %s", matched.UserName),
				Type:     "UserPasswordChanged",
				UserID:   matched.UserID,
				Severity: "Info",
			})
		}
		success = true
		if matched.UserName != "" {
			resetUsers = append(resetUsers, matched.UserName)
		}
	}

	httpx.JSON(w, http.StatusOK, map[string]any{
		"Success":    success,
		"UsersReset": resetUsers,
	})
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

func isLoopbackIPString(raw string) bool {
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

func normalizeAuthByIDRateKey(ip, uid string) string {
	normIP := strings.TrimSpace(ip)
	if addr, err := netip.ParseAddr(normIP); err == nil {
		addr = addr.WithZone("").Unmap()
		if addr.Is6() {
			normIP = netip.PrefixFrom(addr, 64).Masked().String()
		} else {
			normIP = addr.String()
		}
	}
	normUID := strings.ToLower(strings.TrimSpace(uid))
	if normUID == "" {
		return normIP
	}
	return normIP + "|" + normUID
}

func recordAuthByIDFailureBucketsLocked(compMap, ipMap map[string][]time.Time, compKey, ipKey string, now time.Time, compLimit, ipLimit int) {
	appendRateBucketWithEvictLocked(ipMap, ipKey, now, maxRateLimitBuckets, ipLimit)
	if compKey == ipKey {
		appendRateBucketWithEvictLocked(compMap, compKey, now, maxRateLimitBuckets, compLimit)
		return
	}
	if _, exists := compMap[compKey]; exists {
		compMap[compKey] = append(compMap[compKey], now)
		return
	}
	if countIPSubBucketsLocked(compMap, ipKey) >= maxSubBucketsPerIP {
		return
	}
	appendRateBucketWithEvictLocked(compMap, compKey, now, maxRateLimitBuckets, compLimit)
}

func (h *Handler) isAuthByIDRateLimited(ip, uid string, now time.Time) bool {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return false
	}
	loopback := isLoopbackIPString(ip)
	compKey := normalizeAuthByIDRateKey(ip, uid)
	ipKey := normalizeAuthByIDRateKey(ip, "")

	h.mu.Lock()
	defer h.mu.Unlock()
	if loopback {
		cutoff := now.Add(-authByIDLoopbackRateWindow)
		if h.loopbackAuthByIDRates != nil {
			pruneRateBucketLocked(h.loopbackAuthByIDRates, cutoff, maxRateLimitBuckets)
			if len(h.loopbackAuthByIDRates[compKey]) >= authByIDLoopbackRateLimit {
				return true
			}
			if uid != "" && len(h.loopbackAuthByIDRates[ipKey]) >= authByIDLoopbackRateLimit {
				return true
			}
		}
		if h.loopbackAuthByIDIPRates != nil {
			pruneRateBucketLocked(h.loopbackAuthByIDIPRates, cutoff, maxRateLimitBuckets)
			if len(h.loopbackAuthByIDIPRates[ipKey]) >= authByIDLoopbackIPRateLimit {
				return true
			}
		}
		return false
	}
	cutoff := now.Add(-authByIDRateWindow)
	if h.authByIDRates != nil {
		pruneRateBucketLocked(h.authByIDRates, cutoff, maxRateLimitBuckets)
		if len(h.authByIDRates[compKey]) >= authByIDRateLimit {
			return true
		}
		if uid != "" && len(h.authByIDRates[ipKey]) >= authByIDRateLimit {
			return true
		}
	}
	if h.authByIDIPRates != nil {
		pruneRateBucketLocked(h.authByIDIPRates, cutoff, maxRateLimitBuckets)
		if len(h.authByIDIPRates[ipKey]) >= authByIDIPRateLimit {
			return true
		}
	}
	return false
}

func (h *Handler) recordAuthByIDFailure(ip, uid string, now time.Time) {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return
	}
	loopback := isLoopbackIPString(ip)
	compKey := normalizeAuthByIDRateKey(ip, uid)
	ipKey := normalizeAuthByIDRateKey(ip, "")

	h.mu.Lock()
	defer h.mu.Unlock()
	if loopback {
		if h.loopbackAuthByIDRates == nil {
			h.loopbackAuthByIDRates = make(map[string][]time.Time)
		}
		if h.loopbackAuthByIDIPRates == nil {
			h.loopbackAuthByIDIPRates = make(map[string][]time.Time)
		}
		cutoff := now.Add(-authByIDLoopbackRateWindow)
		pruneRateBucketLocked(h.loopbackAuthByIDRates, cutoff, maxRateLimitBuckets)
		pruneRateBucketLocked(h.loopbackAuthByIDIPRates, cutoff, maxRateLimitBuckets)
		recordAuthByIDFailureBucketsLocked(h.loopbackAuthByIDRates, h.loopbackAuthByIDIPRates, compKey, ipKey, now, authByIDLoopbackRateLimit, authByIDLoopbackIPRateLimit)
		return
	}
	if h.authByIDRates == nil {
		h.authByIDRates = make(map[string][]time.Time)
	}
	if h.authByIDIPRates == nil {
		h.authByIDIPRates = make(map[string][]time.Time)
	}
	cutoff := now.Add(-authByIDRateWindow)
	pruneRateBucketLocked(h.authByIDRates, cutoff, maxRateLimitBuckets)
	pruneRateBucketLocked(h.authByIDIPRates, cutoff, maxRateLimitBuckets)
	recordAuthByIDFailureBucketsLocked(h.authByIDRates, h.authByIDIPRates, compKey, ipKey, now, authByIDRateLimit, authByIDIPRateLimit)
}

func (h *Handler) isGlobalRemoteAccessEnabled() bool {
	if h.cfg != nil {
		return h.cfg.Snapshot().IsGlobalRemoteAccessEnabled()
	}
	if h.authSvc != nil && h.authSvc.Config() != nil {
		return h.authSvc.Config().Snapshot().IsGlobalRemoteAccessEnabled()
	}
	return true
}

func (h *Handler) authenticateUserByID(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	isLocal := netutil.IsLocalOrLANRequest(r)
	if !isLocal && !h.isGlobalRemoteAccessEnabled() {
		writeJSONError(w, r, http.StatusForbidden, "Remote access is disabled on this server")
		return
	}
	now := time.Now().UTC()
	ip := netutil.ClientIP(r)
	uid := h.resolveUserID(r, "Id")

	if h.isAuthByIDRateLimited(ip, uid, now) {
		httpx.Error(w, http.StatusTooManyRequests, "too many authentication attempts")
		return
	}

	if h.userRepo == nil || uid == "" {
		h.recordAuthByIDFailure(ip, uid, now)
		writeJSONError(w, r, http.StatusUnauthorized, "invalid username or password")
		return
	}
	var body struct {
		Pw       string `json:"Pw"`
		Password string `json:"Password"`
	}
	if err := decodeOptionalJSONLimit(r, &body, maxKVJSONBodyBytes); err != nil {
		h.recordAuthByIDFailure(ip, uid, now)
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	u, err := h.userRepo.GetUserByID(r.Context(), uid)
	if err != nil {
		auth.DummyPasswordWork(body.Pw)
		h.recordAuthByIDFailure(ip, uid, now)
		writeJSONError(w, r, http.StatusUnauthorized, "invalid username or password")
		return
	}

	pw := body.Pw
	legacySHA1 := body.Password
	if pw == "" && body.Password != "" && u.PasswordHash != "" {
		pw = body.Password
	}

	// Security (M-02): When per-user remote access is disabled on a remote request,
	// perform uniform Argon2id work, record login failure, and return 401 (never 403)
	// so attackers cannot enumerate local users or probe passwords remotely.
	if !isLocal && !u.Policy.EnableRemoteAccess {
		if u.PasswordHash == "" {
			auth.DummyPasswordWork(pw)
		} else {
			_, _ = auth.VerifyPassword(pw, u.PasswordHash)
		}
		h.recordAuthByIDFailure(ip, uid, now)
		_ = h.userRepo.CreateActivityLog(r.Context(), &sqlite.ActivityLogRecord{
			Name:     u.Name + " failed to log in (remote access disabled)",
			Type:     "AuthenticationFailed",
			UserID:   u.ID,
			Severity: "Warning",
		})
		writeJSONError(w, r, http.StatusUnauthorized, "invalid username or password")
		return
	}

	client := middleware.ExtractClientInfo(r)
	if h.authSvc != nil {
		res, authErr := h.authSvc.AuthenticateByName(r.Context(), u.Name, pw, legacySHA1, client)
		if authErr != nil {
			switch {
			case errors.Is(authErr, auth.ErrInvalidCredentials):
				h.recordAuthByIDFailure(ip, uid, now)
				_ = h.userRepo.CreateActivityLog(r.Context(), &sqlite.ActivityLogRecord{
					Name:     u.Name + " failed to log in",
					Type:     "AuthenticationFailed",
					UserID:   u.ID,
					Severity: "Warning",
				})
				writeJSONError(w, r, http.StatusUnauthorized, "invalid username or password")
			case errors.Is(authErr, auth.ErrUserDisabled):
				_ = h.userRepo.CreateActivityLog(r.Context(), &sqlite.ActivityLogRecord{
					Name:     u.Name + " failed to log in (account disabled)",
					Type:     "AuthenticationFailed",
					UserID:   u.ID,
					Severity: "Warning",
				})
				writeJSONError(w, r, http.StatusForbidden, "user is disabled")
			default:
				httpx.Error(w, http.StatusInternalServerError, "authentication failed")
			}
			return
		}
		if h.sessions != nil {
			client.DeviceID = res.SessionInfo.DeviceId
			client.Device = res.SessionInfo.DeviceName
			client.Client = res.SessionInfo.Client
			client.Version = res.SessionInfo.ApplicationVersion
			res.SessionInfo = h.sessions.TouchSession(u, client)
		}
		_ = h.userRepo.CreateActivityLog(r.Context(), &sqlite.ActivityLogRecord{
			Name:     res.User.Name + " has logged in",
			Type:     "AuthenticationSucceeded",
			UserID:   res.User.Id,
			Severity: "Info",
		})
		httpx.JSON(w, http.StatusOK, res)
		return
	}

	if !verifyUserPassword(u, pw, legacySHA1) {
		h.recordAuthByIDFailure(ip, uid, now)
		writeJSONError(w, r, http.StatusUnauthorized, "invalid username or password")
		return
	}
	if u.IsDisabled || u.Policy.IsDisabled {
		writeJSONError(w, r, http.StatusForbidden, "user is disabled")
		return
	}

	if client.DeviceID == "" {
		client.DeviceID = guid.New()
	}
	tokenBytes := make([]byte, 16)
	_, _ = rand.Read(tokenBytes)
	token := hex.EncodeToString(tokenBytes)

	if err := h.userRepo.CreateAccessToken(r.Context(), &sqlite.AccessTokenRecord{
		Token:      token,
		UserID:     u.ID,
		DeviceID:   client.DeviceID,
		DeviceName: client.Device,
		AppName:    client.Client,
		AppVersion: client.Version,
	}); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to create access token")
		return
	}
	_ = h.userRepo.UpdateLastLogin(r.Context(), u.ID, time.Now().UTC())

	var sessInfo dto.SessionInfo
	if h.sessions != nil {
		sessInfo = h.sessions.TouchSession(u, client)
	}

	httpx.JSON(w, http.StatusOK, dto.AuthenticationResult{
		User:        h.toUserDto(u),
		SessionInfo: sessInfo,
		AccessToken: token,
		ServerId:    h.serverID(),
	})
}

func (h *Handler) queryUsers(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	startIndex := q.Int("StartIndex", 0)
	limit := q.Int("Limit", 0)
	nameStartsWithOrGreater := strings.ToLower(strings.TrimSpace(q.Get("NameStartsWithOrGreater")))
	nameStartsWith := strings.ToLower(strings.TrimSpace(q.Get("NameStartsWith")))
	searchTerm := strings.ToLower(strings.TrimSpace(q.Get("SearchTerm")))
	isHidden := q.Bool("IsHidden")
	isDisabled := q.Bool("IsDisabled")
	isAdmin := q.Bool("IsAdministrator")
	sortBy := strings.ToLower(strings.TrimSpace(q.Get("SortBy")))
	sortOrder := strings.ToLower(strings.TrimSpace(q.Get("SortOrder")))

	var all []*sqlite.UserRecord
	if h.userRepo != nil {
		if h.isCallerAdmin(r) {
			list, err := h.userRepo.ListUsers(r.Context())
			if err != nil {
				httpx.Error(w, http.StatusInternalServerError, "failed to query users")
				return
			}
			all = list
		} else if callerID := h.resolveUserID(r, ""); callerID != "" {
			if u, err := h.userRepo.GetUserByID(r.Context(), callerID); err == nil && u != nil {
				all = []*sqlite.UserRecord{u}
			}
		} else {
			writeJSONError(w, r, http.StatusUnauthorized, "authentication required")
			return
		}
	}

	filtered := make([]dto.UserDto, 0, len(all))
	for _, u := range all {
		lowerName := strings.ToLower(u.Name)
		if nameStartsWithOrGreater != "" && lowerName < nameStartsWithOrGreater {
			continue
		}
		if nameStartsWith != "" && !strings.HasPrefix(lowerName, nameStartsWith) {
			continue
		}
		if searchTerm != "" && !strings.Contains(lowerName, searchTerm) {
			continue
		}
		if isHidden != nil && u.Policy.IsHidden != *isHidden {
			continue
		}
		if isDisabled != nil && (u.IsDisabled || u.Policy.IsDisabled) != *isDisabled {
			continue
		}
		if isAdmin != nil && (u.IsAdmin || u.Policy.IsAdministrator) != *isAdmin {
			continue
		}
		filtered = append(filtered, h.toUserDto(u))
	}

	if sortBy != "" || sortOrder != "" {
		desc := sortOrder == "descending" || sortOrder == "desc"
		primarySort := sortBy
		if idx := strings.IndexByte(primarySort, ','); idx >= 0 {
			primarySort = strings.TrimSpace(primarySort[:idx])
		}
		sort.SliceStable(filtered, func(i, j int) bool {
			var cmp int
			switch primarySort {
			case "lastactivitydate":
				cmp = compareTimePtr(filtered[i].LastActivityDate, filtered[j].LastActivityDate)
			case "lastlogindate":
				cmp = compareTimePtr(filtered[i].LastLoginDate, filtered[j].LastLoginDate)
			case "isadministrator":
				ai, aj := 0, 0
				if filtered[i].Policy.IsAdministrator {
					ai = 1
				}
				if filtered[j].Policy.IsAdministrator {
					aj = 1
				}
				if ai < aj {
					cmp = -1
				} else if ai > aj {
					cmp = 1
				}
			default:
				ni, nj := strings.ToLower(filtered[i].Name), strings.ToLower(filtered[j].Name)
				cmp = strings.Compare(ni, nj)
			}
			if cmp == 0 {
				cmp = strings.Compare(strings.ToLower(filtered[i].Name), strings.ToLower(filtered[j].Name))
			}
			if desc {
				return cmp > 0
			}
			return cmp < 0
		})
	}

	total := len(filtered)
	if startIndex < 0 {
		startIndex = 0
	}
	if startIndex > total {
		startIndex = total
	}
	end := total
	if limit > 0 && startIndex+limit < end {
		end = startIndex + limit
	}

	httpx.JSON(w, http.StatusOK, dto.QueryResult[dto.UserDto]{
		Items:            filtered[startIndex:end],
		TotalRecordCount: total,
	})
}

func compareTimePtr(a, b *time.Time) int {
	if a == nil && b == nil {
		return 0
	}
	if a == nil {
		return -1
	}
	if b == nil {
		return 1
	}
	if a.Before(*b) {
		return -1
	}
	if a.After(*b) {
		return 1
	}
	return 0
}

func (h *Handler) getUserPrefixes(w http.ResponseWriter, r *http.Request) {
	seen := map[string]bool{}
	if h.userRepo != nil {
		var list []*sqlite.UserRecord
		if h.isCallerAdmin(r) {
			if all, err := h.userRepo.ListUsers(r.Context()); err == nil {
				list = all
			}
		} else if callerID := h.resolveUserID(r, ""); callerID != "" {
			if u, err := h.userRepo.GetUserByID(r.Context(), callerID); err == nil && u != nil {
				list = []*sqlite.UserRecord{u}
			}
		} else {
			writeJSONError(w, r, http.StatusUnauthorized, "authentication required")
			return
		}
		for _, u := range list {
			runes := []rune(strings.TrimSpace(u.Name))
			if len(runes) == 0 {
				continue
			}
			ch := unicode.ToUpper(runes[0])
			if unicode.IsLetter(ch) {
				seen[string(ch)] = true
			} else {
				seen["#"] = true
			}
		}
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]map[string]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, map[string]string{"Name": k})
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) disconnectUserWS(userID string) {
	norm := guid.Normalize(userID)
	if norm == "" {
		return
	}
	if wsHub := h.getWSHub(); wsHub != nil {
		if disc, ok := wsHub.(interface{ DisconnectUser(string) }); ok {
			disc.DisconnectUser(norm)
		}
	}
}

func (h *Handler) disconnectUserSessions(userID string) {
	norm := guid.Normalize(userID)
	if norm == "" {
		return
	}
	if h.sessions != nil {
		h.sessions.RemoveSessionsByUser(norm)
	}
	h.disconnectUserWS(norm)
}

func hasRemovedFolder(oldFolders, newFolders []string) bool {
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
	if !oldPol.EnableAllFolders && !newPol.EnableAllFolders && hasRemovedFolder(oldPol.EnabledFolders, newPol.EnabledFolders) {
		return true
	}
	return false
}

func (h *Handler) removeUserDiskArtifacts(userID string) {
	if h.cfg == nil {
		return
	}
	dataDir := strings.TrimSpace(h.cfg.Snapshot().DataDir)
	safeUID := sanitizeFilename(guid.Normalize(userID))
	if dataDir == "" || safeUID == "" || safeUID == "default" {
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

func (h *Handler) revokeAndDisconnectUser(ctx context.Context, userID string) {
	norm := guid.Normalize(userID)
	if norm == "" {
		return
	}
	if h.authSvc != nil {
		_ = h.authSvc.RevokeUserTokens(ctx, norm)
		h.authSvc.InvalidateUserCache(norm)
	} else if h.userRepo != nil {
		_ = h.userRepo.DeleteTokensByUser(ctx, norm)
	}
	h.disconnectUserSessions(norm)
}

func (h *Handler) updateUser(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "Id")
	if !h.canModifyUser(r, uid) {
		writeJSONError(w, r, http.StatusForbidden, "forbidden")
		return
	}
	if h.userRepo == nil || uid == "" {
		writeJSONError(w, r, http.StatusNotFound, "user not found")
		return
	}
	u, err := h.userRepo.GetUserByID(r.Context(), uid)
	if err != nil {
		writeJSONError(w, r, http.StatusNotFound, "user not found")
		return
	}

	var body struct {
		Name          string                 `json:"Name"`
		Configuration *dto.UserConfiguration `json:"Configuration"`
		Policy        *dto.UserPolicy        `json:"Policy"`
	}
	if err := decodeOptionalJSON(r, &body); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.Policy != nil && !h.isCallerAdmin(r) {
		writeJSONError(w, r, http.StatusForbidden, "admin privileges required to update user policy")
		return
	}
	if body.Name != "" {
		if auth.ContainsControlChar(body.Name) {
			httpx.Error(w, http.StatusBadRequest, "username must not contain control characters")
			return
		}
		trimmedName := strings.TrimSpace(body.Name)
		if trimmedName == "" || len([]rune(trimmedName)) > 64 {
			httpx.Error(w, http.StatusBadRequest, "invalid username")
			return
		}
		if trimmedName != u.Name {
			if !h.isCallerAdmin(r) {
				writeJSONError(w, r, http.StatusForbidden, "admin privileges required to rename user")
				return
			}
			if h.store != nil && h.store.Writer != nil {
				if _, err := h.store.Writer.ExecContext(r.Context(), `UPDATE users SET name = ? WHERE id = ?`, trimmedName, u.ID); err != nil {
					if strings.Contains(strings.ToLower(err.Error()), "unique") {
						httpx.Error(w, http.StatusBadRequest, "username already exists")
						return
					}
					httpx.Error(w, http.StatusInternalServerError, "failed to update user name")
					return
				}
			}
			if h.sessions != nil {
				h.sessions.UpdateUserName(u.ID, trimmedName)
			}
		}
	}
	if body.Configuration != nil {
		if err := h.userRepo.UpdateUserConfig(r.Context(), u.ID, *body.Configuration); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "failed to update user configuration")
			return
		}
	}
	if body.Policy != nil {
		wasAdmin := u.IsAdmin || u.Policy.IsAdministrator
		if err := h.userRepo.UpdateUserPolicySafe(r.Context(), u.ID, *body.Policy); err != nil {
			if errors.Is(err, sqlite.ErrLastAdmin) {
				httpx.Error(w, http.StatusBadRequest, "cannot revoke or disable the last administrator")
				return
			}
			if errors.Is(err, sqlite.ErrNotFound) {
				writeJSONError(w, r, http.StatusNotFound, "user not found")
				return
			}
			httpx.Error(w, http.StatusInternalServerError, "failed to update user policy")
			return
		}
		if body.Policy.IsDisabled {
			h.revokeAndDisconnectUser(r.Context(), u.ID)
		} else if wasAdmin && !body.Policy.IsAdministrator {
			h.disconnectUserSessions(u.ID)
		} else if policyRequiresWSDisconnect(u.Policy, wasAdmin, *body.Policy) {
			h.disconnectUserWS(u.ID)
		}
	}
	if h.authSvc != nil {
		h.authSvc.InvalidateUserCache(u.ID)
	}
	httpx.NoContent(w)
}

func (h *Handler) deleteUser(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "Id")
	if callerID := h.callerUserID(r); callerID != "" && callerID == uid {
		httpx.Error(w, http.StatusBadRequest, "cannot delete currently authenticated user")
		return
	}
	if h.userRepo == nil || uid == "" {
		writeJSONError(w, r, http.StatusNotFound, "user not found")
		return
	}
	if err := h.userRepo.DeleteUserSafe(r.Context(), uid); err != nil {
		if errors.Is(err, sqlite.ErrLastAdmin) {
			httpx.Error(w, http.StatusBadRequest, "cannot delete the last administrator")
			return
		}
		if errors.Is(err, sqlite.ErrNotFound) {
			writeJSONError(w, r, http.StatusNotFound, "user not found")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "failed to delete user")
		return
	}
	h.revokeAndDisconnectUser(r.Context(), uid)
	h.removeUserDiskArtifacts(uid)
	h.mu.RLock()
	onDeleted := h.onUserDeleted
	h.mu.RUnlock()
	if onDeleted != nil {
		onDeleted(r.Context(), uid)
	}
	httpx.NoContent(w)
}

func (h *Handler) updateUserConfigurationPartial(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "Id")
	if !h.canModifyUser(r, uid) {
		writeJSONError(w, r, http.StatusForbidden, "forbidden")
		return
	}
	if h.userRepo == nil || uid == "" {
		writeJSONError(w, r, http.StatusNotFound, "user not found")
		return
	}
	u, err := h.userRepo.GetUserByID(r.Context(), uid)
	if err != nil {
		writeJSONError(w, r, http.StatusNotFound, "user not found")
		return
	}

	var patch map[string]any
	if err := decodeOptionalJSON(r, &patch); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(patch) > 0 {
		rawCfg, err := json.Marshal(u.Config)
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "failed to encode current configuration")
			return
		}
		merged := map[string]any{}
		if err := json.Unmarshal(rawCfg, &merged); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "failed to decode current configuration")
			return
		}
		for k, v := range patch {
			merged[k] = v
		}
		updatedRaw, err := json.Marshal(merged)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "invalid configuration patch")
			return
		}
		var nextCfg dto.UserConfiguration
		if err := json.Unmarshal(updatedRaw, &nextCfg); err != nil {
			httpx.Error(w, http.StatusBadRequest, "invalid configuration field type")
			return
		}
		if err := h.userRepo.UpdateUserConfig(r.Context(), u.ID, nextCfg); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "failed to update user configuration")
			return
		}
		if h.authSvc != nil {
			h.authSvc.InvalidateUserCache(u.ID)
		}
	}
	httpx.NoContent(w)
}

func (h *Handler) copyUserData(w http.ResponseWriter, r *http.Request) {
	srcUID := h.resolveUserID(r, "UserId")
	var body struct {
		CopyFromUserId string   `json:"CopyFromUserId"`
		ToUserIds      []string `json:"ToUserIds"`
		CopyOptions    []string `json:"CopyOptions"`
	}
	if err := decodeOptionalJSON(r, &body); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.CopyFromUserId != "" {
		srcUID = guid.Normalize(body.CopyFromUserId)
	}
	if h.userRepo != nil && srcUID != "" {
		copyConfig := len(body.CopyOptions) == 0
		copyPolicy := false
		for _, opt := range body.CopyOptions {
			switch strings.ToLower(strings.TrimSpace(opt)) {
			case "userconfiguration", "configuration", "config":
				copyConfig = true
			case "userpolicy", "policy":
				copyPolicy = true
			}
		}
		if src, err := h.userRepo.GetUserByID(r.Context(), srcUID); err == nil && src != nil {
			for _, target := range body.ToUserIds {
				tid := guid.Normalize(target)
				if tid == "" || tid == src.ID {
					continue
				}
				if copyConfig {
					_ = h.userRepo.UpdateUserConfig(r.Context(), tid, src.Config)
				}
				if copyPolicy {
					wasAdmin := false
					var oldPol dto.UserPolicy
					if existing, getErr := h.userRepo.GetUserByID(r.Context(), tid); getErr == nil && existing != nil {
						wasAdmin = existing.IsAdmin || existing.Policy.IsAdministrator
						oldPol = existing.Policy
					}
					if err := h.userRepo.UpdateUserPolicySafe(r.Context(), tid, src.Policy); err != nil {
						if errors.Is(err, sqlite.ErrLastAdmin) {
							httpx.Error(w, http.StatusBadRequest, "cannot revoke or disable the last administrator")
							return
						}
						if !errors.Is(err, sqlite.ErrNotFound) {
							httpx.Error(w, http.StatusInternalServerError, "failed to update target user policy")
							return
						}
					} else {
						if src.Policy.IsDisabled {
							h.revokeAndDisconnectUser(r.Context(), tid)
						} else if wasAdmin && !src.Policy.IsAdministrator {
							h.disconnectUserSessions(tid)
						} else if policyRequiresWSDisconnect(oldPol, wasAdmin, src.Policy) {
							h.disconnectUserWS(tid)
						}
					}
				}
				if h.authSvc != nil {
					h.authSvc.InvalidateUserCache(tid)
				}
			}
		}
	}
	httpx.NoContent(w)
}

func (h *Handler) deleteUserTrackSelections(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "Id")
	if !h.canModifyUser(r, uid) {
		writeJSONError(w, r, http.StatusForbidden, "forbidden")
		return
	}
	trackType := strings.ToLower(strings.TrimSpace(chi.URLParam(r, "TrackType")))
	if h.store != nil && h.store.Writer != nil && uid != "" {
		switch trackType {
		case "audio":
			_, _ = h.store.Writer.ExecContext(r.Context(), `UPDATE user_item_data SET audio_stream_index = NULL WHERE user_id = ?`, uid)
		case "subtitle":
			_, _ = h.store.Writer.ExecContext(r.Context(), `UPDATE user_item_data SET subtitle_stream_index = NULL WHERE user_id = ?`, uid)
		default:
			_, _ = h.store.Writer.ExecContext(r.Context(), `UPDATE user_item_data SET audio_stream_index = NULL, subtitle_stream_index = NULL WHERE user_id = ?`, uid)
		}
	}
	httpx.NoContent(w)
}

func (h *Handler) deleteUserRecentlySearched(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "Id")
	if !h.canModifyUser(r, uid) {
		writeJSONError(w, r, http.StatusForbidden, "forbidden")
		return
	}
	_ = h.deleteKV(r.Context(), "user_searched_items:"+uid)
	httpx.NoContent(w)
}

func (h *Handler) postUserSearchedItems(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "UserId")
	if !h.canModifyUser(r, uid) {
		writeJSONError(w, r, http.StatusForbidden, "forbidden")
		return
	}
	if r.ContentLength > maxKVJSONBodyBytes {
		httpx.Error(w, http.StatusRequestEntityTooLarge, "request body too large")
		return
	}
	var payload map[string]any
	if err := decodeOptionalJSONLimit(r, &payload, maxKVJSONBodyBytes); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid or oversized request body")
		return
	}
	key := "user_searched_items:" + uid
	if payload == nil {
		payload = map[string]any{}
	}
	payload["SearchedAt"] = time.Now().UTC().Format(time.RFC3339)
	h.mu.Lock()
	var items []map[string]any
	_, _ = h.loadKVLocked(r.Context(), key, &items)
	items = append(items, payload)
	if len(items) > maxSearchedItemsEntries {
		items = items[len(items)-maxSearchedItemsEntries:]
	}
	_ = h.saveKVLocked(r.Context(), key, items)
	h.mu.Unlock()
	httpx.NoContent(w)
}

func (h *Handler) getUserTypedSettings(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "UserId")
	if !h.canModifyUser(r, uid) {
		writeJSONError(w, r, http.StatusForbidden, "forbidden")
		return
	}
	settingKey := strings.TrimSpace(chi.URLParam(r, "Key"))
	out := map[string]any{}
	_, _ = h.loadKV(r.Context(), fmt.Sprintf("user_typed_settings:%s:%s", uid, settingKey), &out)
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) postUserTypedSettings(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "UserId")
	if !h.canModifyUser(r, uid) {
		writeJSONError(w, r, http.StatusForbidden, "forbidden")
		return
	}
	settingKey := strings.TrimSpace(chi.URLParam(r, "Key"))
	if settingKey == "" || len([]rune(settingKey)) > maxTypedSettingKeyLen {
		httpx.Error(w, http.StatusBadRequest, "invalid setting key")
		return
	}
	if r.ContentLength > maxKVJSONBodyBytes {
		httpx.Error(w, http.StatusRequestEntityTooLarge, "request body too large")
		return
	}
	body := map[string]any{}
	if err := decodeOptionalJSONLimit(r, &body, maxKVJSONBodyBytes); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	prefix := fmt.Sprintf("user_typed_settings:%s:", uid)
	fullKey := prefix + settingKey

	h.mu.Lock()
	var existing map[string]any
	exists, _ := h.loadKVLocked(r.Context(), fullKey, &existing)
	if !exists {
		keyCount := 0
		if st := h.kvStore(); st != nil && st.Reader != nil {
			_ = st.Reader.QueryRowContext(
				r.Context(),
				`SELECT COUNT(*) FROM server_config WHERE substr(key, 1, ?) = ?`,
				len(prefix), prefix,
			).Scan(&keyCount)
		} else {
			for k := range h.memKV {
				if strings.HasPrefix(k, prefix) {
					keyCount++
				}
			}
		}
		if keyCount >= maxUserTypedSettingsKeys {
			h.mu.Unlock()
			httpx.Error(w, http.StatusBadRequest, "typed settings key limit reached")
			return
		}
	}
	_ = h.saveKVLocked(r.Context(), fullKey, body)
	h.mu.Unlock()
	httpx.NoContent(w)
}

// --- HomeSections ---

type homeSection struct {
	Id          string `json:"Id"`
	Name        string `json:"Name"`
	SectionType string `json:"SectionType"`
	SortOrder   int    `json:"SortOrder"`
}

func defaultHomeSections() []homeSection {
	return []homeSection{
		{Id: "smalllibrarytiles", Name: "My Media", SectionType: "LibraryTiles", SortOrder: 0},
		{Id: "resume", Name: "Continue Watching", SectionType: "Resume", SortOrder: 1},
		{Id: "latestmedia", Name: "Latest Media", SectionType: "LatestMedia", SortOrder: 2},
	}
}

func (h *Handler) getUserHomeSections(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "UserId")
	if !h.canModifyUser(r, uid) {
		writeJSONError(w, r, http.StatusForbidden, "forbidden")
		return
	}
	var sections []homeSection
	found, _ := h.loadKV(r.Context(), "user_home_sections:"+uid, &sections)
	if !found || len(sections) == 0 {
		sections = defaultHomeSections()
	}
	httpx.JSON(w, http.StatusOK, sections)
}

func (h *Handler) postUserHomeSections(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "UserId")
	if !h.canModifyUser(r, uid) {
		writeJSONError(w, r, http.StatusForbidden, "forbidden")
		return
	}
	if r.ContentLength > maxKVJSONBodyBytes {
		httpx.Error(w, http.StatusRequestEntityTooLarge, "request body too large")
		return
	}
	var sec homeSection
	if err := decodeOptionalJSONLimit(r, &sec, maxKVJSONBodyBytes); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	h.mu.Lock()
	var sections []homeSection
	found, _ := h.loadKVLocked(r.Context(), "user_home_sections:"+uid, &sections)
	if !found {
		sections = defaultHomeSections()
	}
	if sec.Id == "" {
		sec.Id = guid.New()
	}
	if sec.Name == "" {
		sec.Name = "Custom Section"
	}
	replaced := false
	for i := range sections {
		if sections[i].Id == sec.Id {
			sections[i] = sec
			replaced = true
			break
		}
	}
	if !replaced {
		if len(sections) >= maxHomeSectionsEntries {
			h.mu.Unlock()
			httpx.Error(w, http.StatusBadRequest, "home sections limit reached")
			return
		}
		sec.SortOrder = len(sections)
		sections = append(sections, sec)
	}
	_ = h.saveKVLocked(r.Context(), "user_home_sections:"+uid, sections)
	h.mu.Unlock()
	httpx.JSON(w, http.StatusOK, sec)
}

func (h *Handler) deleteUserHomeSections(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "UserId")
	if !h.canModifyUser(r, uid) {
		writeJSONError(w, r, http.StatusForbidden, "forbidden")
		return
	}
	q := httpx.ParseQuery(r)
	ids := q.List("Ids")
	var body struct {
		Ids []string `json:"Ids"`
	}
	_ = decodeOptionalJSON(r, &body)
	ids = append(ids, body.Ids...)

	removeSet := make(map[string]bool, len(ids))
	for _, id := range ids {
		removeSet[id] = true
	}

	h.mu.Lock()
	var sections []homeSection
	found, _ := h.loadKVLocked(r.Context(), "user_home_sections:"+uid, &sections)
	if !found {
		sections = defaultHomeSections()
	}
	next := make([]homeSection, 0, len(sections))
	for _, s := range sections {
		if !removeSet[s.Id] {
			next = append(next, s)
		}
	}
	_ = h.saveKVLocked(r.Context(), "user_home_sections:"+uid, next)
	h.mu.Unlock()
	httpx.NoContent(w)
}

func (h *Handler) moveUserHomeSection(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "UserId")
	if !h.canModifyUser(r, uid) {
		writeJSONError(w, r, http.StatusForbidden, "forbidden")
		return
	}
	q := httpx.ParseQuery(r)
	var body struct {
		Id       string `json:"Id"`
		NewIndex int    `json:"NewIndex"`
	}
	_ = decodeOptionalJSON(r, &body)
	if body.Id == "" {
		body.Id = q.Get("Id")
	}
	if q.Has("NewIndex") {
		body.NewIndex = q.Int("NewIndex", body.NewIndex)
	}

	h.mu.Lock()
	var sections []homeSection
	found, _ := h.loadKVLocked(r.Context(), "user_home_sections:"+uid, &sections)
	if !found {
		sections = defaultHomeSections()
	}
	idx := -1
	for i, s := range sections {
		if s.Id == body.Id {
			idx = i
			break
		}
	}
	if idx >= 0 {
		item := sections[idx]
		sections = append(sections[:idx], sections[idx+1:]...)
		newIdx := body.NewIndex
		if newIdx < 0 {
			newIdx = 0
		}
		if newIdx > len(sections) {
			newIdx = len(sections)
		}
		sections = append(sections[:newIdx], append([]homeSection{item}, sections[newIdx:]...)...)
		for i := range sections {
			sections[i].SortOrder = i
		}
		_ = h.saveKVLocked(r.Context(), "user_home_sections:"+uid, sections)
	}
	h.mu.Unlock()
	httpx.NoContent(w)
}

func (h *Handler) fullUserPolicyForUser(r *http.Request, uid string) ([]string, dto.UserPolicy) {
	if h.userRepo == nil {
		return nil, dto.UserPolicy{IsAdministrator: true}
	}
	normUID := guid.Normalize(uid)
	if normUID == "" {
		return []string{}, dto.UserPolicy{}
	}
	u, err := h.userRepo.GetUserByID(r.Context(), normUID)
	if err != nil || u == nil {
		return []string{}, dto.UserPolicy{}
	}
	if u.IsAdmin || u.Policy.IsAdministrator {
		pol := u.Policy
		pol.IsAdministrator = true
		return nil, pol
	}
	pol := u.Policy
	pol.IsAdministrator = false
	var allowedLibs []string
	if h.libRepo != nil {
		allowedLibs = h.libRepo.AllowedLibraryIDsForPolicy(r.Context(), pol)
	} else if !pol.EnableAllFolders {
		allowedLibs = make([]string, 0, len(pol.EnabledFolders))
		for _, fid := range pol.EnabledFolders {
			if norm := guid.Normalize(fid); norm != "" {
				allowedLibs = append(allowedLibs, norm)
			}
		}
	}
	return allowedLibs, pol
}

func (h *Handler) fullUserPolicyForRequest(r *http.Request, targetUID string) ([]string, dto.UserPolicy) {
	if h.userRepo == nil {
		return nil, dto.UserPolicy{IsAdministrator: true}
	}
	uid := guid.Normalize(targetUID)
	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil && (ac.User != nil || ac.IsAPIKey) {
		if ac.IsAdmin() {
			if uid == "" || uid == guid.Normalize(ac.UserID()) {
				return nil, dto.UserPolicy{IsAdministrator: true}
			}
		} else {
			uid = guid.Normalize(ac.UserID())
		}
	}
	return h.fullUserPolicyForUser(r, uid)
}

func (h *Handler) userPolicyForRequest(r *http.Request, targetUID string) (allowedLibs []string, maxRating *int, blockedTags []string) {
	libs, pol := h.fullUserPolicyForRequest(r, targetUID)
	if pol.IsAdministrator {
		return nil, nil, nil
	}
	return libs, pol.MaxParentalRating, pol.BlockedTags
}

func (h *Handler) postFilterItemsByPolicy(ctx context.Context, items []dto.BaseItemDto, pol dto.UserPolicy) ([]dto.BaseItemDto, int) {
	if h.libRepo == nil || pol.IsAdministrator || len(items) == 0 {
		return items, 0
	}
	if len(pol.BlockUnratedItems) == 0 && len(pol.AllowedTags) == 0 {
		return items, 0
	}
	filtered := make([]dto.BaseItemDto, 0, len(items))
	removed := 0
	for _, it := range items {
		rec, err := h.libRepo.GetItemRecordByID(ctx, it.Id)
		if err != nil || rec == nil {
			removed++
			continue
		}
		if blocked, bErr := h.libRepo.IsItemBlockedByPolicy(ctx, rec, pol); bErr != nil || blocked {
			removed++
			continue
		}
		filtered = append(filtered, it)
	}
	return filtered, removed
}

func (h *Handler) preferredGenresForUser(ctx context.Context, uid string, allowedLibs []string, pol dto.UserPolicy) []string {
	if h.libRepo == nil || uid == "" {
		return nil
	}
	trueVal := true
	counts := map[string]int{}
	addGenres := func(items []dto.BaseItemDto, weight int) {
		for _, it := range items {
			for _, g := range it.Genres {
				g = strings.TrimSpace(g)
				if g != "" {
					counts[g] += weight
				}
			}
		}
	}
	if favs, _, err := h.libRepo.QueryItems(ctx, sqlite.ItemFilter{
		ServerID:          h.serverID(),
		UserID:            uid,
		Recursive:         true,
		AllowedLibraryIDs: allowedLibs,
		MaxParentalRating: pol.MaxParentalRating,
		BlockedTags:       pol.BlockedTags,
		BlockUnratedItems: pol.BlockUnratedItems,
		AllowedTags:       pol.AllowedTags,
		IsFavorite:        &trueVal,
		Fields:            []string{"Genres"},
		Limit:             20,
	}); err == nil {
		addGenres(favs, 2)
	}
	if played, _, err := h.libRepo.QueryItems(ctx, sqlite.ItemFilter{
		ServerID:          h.serverID(),
		UserID:            uid,
		Recursive:         true,
		AllowedLibraryIDs: allowedLibs,
		MaxParentalRating: pol.MaxParentalRating,
		BlockedTags:       pol.BlockedTags,
		BlockUnratedItems: pol.BlockUnratedItems,
		AllowedTags:       pol.AllowedTags,
		IsPlayed:          &trueVal,
		SortBy:            []string{"DatePlayed"},
		SortOrder:         "Descending",
		Fields:            []string{"Genres"},
		Limit:             20,
	}); err == nil {
		addGenres(played, 1)
	}
	if len(counts) == 0 {
		return nil
	}
	type kv struct {
		genre string
		score int
	}
	ranked := make([]kv, 0, len(counts))
	for g, s := range counts {
		ranked = append(ranked, kv{genre: g, score: s})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].genre < ranked[j].genre
	})
	limit := len(ranked)
	if limit > 5 {
		limit = 5
	}
	out := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		out = append(out, ranked[i].genre)
	}
	return out
}

func (h *Handler) isPlaylistPublic(ctx context.Context, playlistID string) bool {
	normID := guid.Normalize(playlistID)
	if normID == "" {
		return false
	}
	var vis struct {
		IsPublic bool `json:"IsPublic"`
	}
	ok, err := h.loadKV(ctx, "item_visibility_"+normID, &vis)
	return err == nil && ok && vis.IsPublic
}

func (h *Handler) canViewPlaylist(r *http.Request, playlistID, parentID string) bool {
	if h.isCallerAdmin(r) {
		return true
	}
	ownerID := guid.Normalize(parentID)
	if ownerID == "" || h.isPlaylistPublic(r.Context(), playlistID) {
		return true
	}
	callerID := h.callerUserID(r)
	return callerID != "" && callerID == ownerID
}

func (h *Handler) hiddenPrivatePlaylistIDs(r *http.Request) []string {
	if h.isCallerAdmin(r) {
		return nil
	}
	store := h.store
	if store == nil && h.userRepo != nil {
		store = h.userRepo.Store()
	}
	if store == nil {
		return nil
	}
	rows, err := store.Reader.QueryContext(r.Context(), `SELECT id, COALESCE(parent_id, '') FROM items WHERE LOWER(type) = 'playlist'`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var hidden []string
	for rows.Next() {
		var id, parentID string
		if err := rows.Scan(&id, &parentID); err != nil {
			continue
		}
		if !h.canViewPlaylist(r, id, parentID) {
			hidden = append(hidden, id)
		}
	}
	return hidden
}

func (h *Handler) filterVisiblePlaylists(r *http.Request, items []dto.BaseItemDto) ([]dto.BaseItemDto, int) {
	if h.isCallerAdmin(r) || len(items) == 0 {
		return items, 0
	}
	out := make([]dto.BaseItemDto, 0, len(items))
	removed := 0
	for _, it := range items {
		if strings.EqualFold(it.Type, "Playlist") {
			parentID := it.ParentId
			if parentID == "" && h.libRepo != nil {
				if rec, err := h.libRepo.GetItemRecordByID(r.Context(), it.Id); err == nil && rec != nil {
					parentID = rec.ParentID
				}
			}
			if !h.canViewPlaylist(r, it.Id, parentID) {
				removed++
				continue
			}
		}
		out = append(out, it)
	}
	return out, removed
}

func (h *Handler) getUserSectionItems(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "UserId")
	if !h.canModifyUser(r, uid) {
		writeJSONError(w, r, http.StatusForbidden, "forbidden")
		return
	}
	q := httpx.ParseQuery(r)
	startIndex := q.Int("StartIndex", 0)
	if startIndex < 0 {
		startIndex = 0
	}
	if startIndex > 10000 {
		startIndex = 10000
	}
	limit := q.Int("Limit", 20)
	if limit <= 0 {
		limit = 20
	}
	if limit > 500 {
		limit = 500
	}
	if h.libRepo == nil {
		httpx.JSON(w, http.StatusOK, dto.QueryResult[dto.BaseItemDto]{Items: []dto.BaseItemDto{}, TotalRecordCount: 0})
		return
	}
	allowedLibs, pol := h.fullUserPolicyForRequest(r, uid)
	hiddenPlaylists := h.hiddenPrivatePlaylistIDs(r)
	sectionID := strings.ToLower(strings.TrimSpace(chi.URLParam(r, "SectionId")))
	parentID := guid.Normalize(q.Get("ParentId"))
	includeTypes := q.List("IncludeItemTypes")
	fields := q.List("Fields")

	var (
		items []dto.BaseItemDto
		total int
		err   error
	)
	switch sectionID {
	case "resume":
		items, total, err = h.libRepo.GetResumeItemsWithUserPolicy(
			r.Context(), h.serverID(), uid, parentID, includeTypes, startIndex, limit, fields,
			allowedLibs, pol,
		)
	case "latestmedia", "latest":
		items, total, err = h.libRepo.QueryItems(r.Context(), sqlite.ItemFilter{
			ServerID:          h.serverID(),
			UserID:            uid,
			ParentID:          parentID,
			Recursive:         true,
			IncludeItemTypes:  includeTypes,
			ExcludeItemIDs:    hiddenPlaylists,
			AllowedLibraryIDs: allowedLibs,
			MaxParentalRating: pol.MaxParentalRating,
			BlockedTags:       pol.BlockedTags,
			BlockUnratedItems: pol.BlockUnratedItems,
			AllowedTags:       pol.AllowedTags,
			Fields:            fields,
			SortBy:            []string{"DateCreated"},
			SortOrder:         "Descending",
			StartIndex:        startIndex,
			Limit:             limit,
		})
	case "favorites":
		fav := true
		items, total, err = h.libRepo.QueryItems(r.Context(), sqlite.ItemFilter{
			ServerID:          h.serverID(),
			UserID:            uid,
			ParentID:          parentID,
			Recursive:         true,
			IncludeItemTypes:  includeTypes,
			ExcludeItemIDs:    hiddenPlaylists,
			AllowedLibraryIDs: allowedLibs,
			MaxParentalRating: pol.MaxParentalRating,
			BlockedTags:       pol.BlockedTags,
			BlockUnratedItems: pol.BlockUnratedItems,
			AllowedTags:       pol.AllowedTags,
			IsFavorite:        &fav,
			Fields:            fields,
			StartIndex:        startIndex,
			Limit:             limit,
		})
	case "nextup":
		seriesID := guid.Normalize(q.Get("SeriesId"))
		var allNextUp []dto.BaseItemDto
		allNextUp, total, err = h.libRepo.GetNextUpEpisodesWithUserPolicy(
			r.Context(), h.serverID(), uid, seriesID, startIndex+limit, fields,
			allowedLibs, pol,
		)
		if err == nil {
			var policyRemoved int
			allNextUp, policyRemoved = h.postFilterItemsByPolicy(r.Context(), allNextUp, pol)
			if policyRemoved > 0 {
				total -= policyRemoved
				if total < 0 {
					total = 0
				}
			}
			if startIndex >= len(allNextUp) {
				items = []dto.BaseItemDto{}
			} else {
				end := startIndex + limit
				if end > len(allNextUp) {
					end = len(allNextUp)
				}
				items = allNextUp[startIndex:end]
			}
		}
	default:
		items, total, err = h.libRepo.QueryItems(r.Context(), sqlite.ItemFilter{
			ServerID:          h.serverID(),
			UserID:            uid,
			ParentID:          parentID,
			Recursive:         true,
			IncludeItemTypes:  includeTypes,
			ExcludeItemIDs:    hiddenPlaylists,
			AllowedLibraryIDs: allowedLibs,
			MaxParentalRating: pol.MaxParentalRating,
			BlockedTags:       pol.BlockedTags,
			BlockUnratedItems: pol.BlockUnratedItems,
			AllowedTags:       pol.AllowedTags,
			Fields:            fields,
			StartIndex:        startIndex,
			Limit:             limit,
		})
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to query section items")
		return
	}
	if items == nil {
		items = []dto.BaseItemDto{}
	}
	if sectionID != "nextup" {
		var policyRemoved int
		items, policyRemoved = h.postFilterItemsByPolicy(r.Context(), items, pol)
		if policyRemoved > 0 {
			total -= policyRemoved
			if total < 0 {
				total = 0
			}
		}
	}
	var removed int
	items, removed = h.filterVisiblePlaylists(r, items)
	if removed > 0 {
		total -= removed
		if total < 0 {
			total = 0
		}
	}
	if !h.isCallerAdmin(r) {
		dto.SanitizeItemPathsSlice(items)
	}
	httpx.JSON(w, http.StatusOK, dto.QueryResult[dto.BaseItemDto]{Items: items, TotalRecordCount: total})
}

func (h *Handler) getUserSuggestions(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "UserId")
	if !h.canModifyUser(r, uid) {
		writeJSONError(w, r, http.StatusForbidden, "forbidden")
		return
	}
	q := httpx.ParseQuery(r)
	if h.libRepo == nil {
		httpx.JSON(w, http.StatusOK, dto.QueryResult[dto.BaseItemDto]{Items: []dto.BaseItemDto{}, TotalRecordCount: 0})
		return
	}
	startIndex := q.Int("StartIndex", 0)
	if startIndex < 0 {
		startIndex = 0
	}
	if startIndex > 10000 {
		startIndex = 10000
	}
	limit := q.Int("Limit", 20)
	if limit <= 0 {
		limit = 20
	}
	if limit > 500 {
		limit = 500
	}
	itemTypes := q.List("Type")
	allowedLibs, pol := h.fullUserPolicyForRequest(r, uid)
	hiddenPlaylists := h.hiddenPrivatePlaylistIDs(r)
	unplayed := true

	preferredGenres := h.preferredGenresForUser(r.Context(), uid, allowedLibs, pol)
	if len(preferredGenres) > 0 {
		fetchLimit := startIndex + limit
		matched, _, err := h.libRepo.QueryItems(r.Context(), sqlite.ItemFilter{
			ServerID:          h.serverID(),
			UserID:            uid,
			Recursive:         true,
			IncludeItemTypes:  itemTypes,
			ExcludeItemIDs:    hiddenPlaylists,
			AllowedLibraryIDs: allowedLibs,
			MaxParentalRating: pol.MaxParentalRating,
			BlockedTags:       pol.BlockedTags,
			BlockUnratedItems: pol.BlockUnratedItems,
			AllowedTags:       pol.AllowedTags,
			Genres:            preferredGenres,
			IsUnplayed:        &unplayed,
			SortBy:            []string{"CommunityRating", "DateCreated"},
			SortOrder:         "Descending",
			Limit:             fetchLimit,
		})
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "failed to load suggestions")
			return
		}
		merged := make([]dto.BaseItemDto, 0, len(matched))
		seen := make(map[string]bool, len(matched))
		for _, it := range matched {
			seen[it.Id] = true
			merged = append(merged, it)
		}
		if len(merged) < fetchLimit {
			fallback, _, fbErr := h.libRepo.QueryItems(r.Context(), sqlite.ItemFilter{
				ServerID:          h.serverID(),
				UserID:            uid,
				Recursive:         true,
				IncludeItemTypes:  itemTypes,
				ExcludeItemIDs:    hiddenPlaylists,
				AllowedLibraryIDs: allowedLibs,
				MaxParentalRating: pol.MaxParentalRating,
				BlockedTags:       pol.BlockedTags,
				BlockUnratedItems: pol.BlockUnratedItems,
				AllowedTags:       pol.AllowedTags,
				IsUnplayed:        &unplayed,
				SortBy:            []string{"CommunityRating", "DateCreated"},
				SortOrder:         "Descending",
				Limit:             fetchLimit,
			})
			if fbErr == nil {
				for _, it := range fallback {
					if !seen[it.Id] {
						seen[it.Id] = true
						merged = append(merged, it)
					}
				}
			}
		}
		merged, _ = h.postFilterItemsByPolicy(r.Context(), merged, pol)
		merged, _ = h.filterVisiblePlaylists(r, merged)
		total := len(merged)
		if startIndex > total {
			startIndex = total
		}
		end := startIndex + limit
		if end > total {
			end = total
		}
		page := merged[startIndex:end]
		if !h.isCallerAdmin(r) {
			dto.SanitizeItemPathsSlice(page)
		}
		httpx.JSON(w, http.StatusOK, dto.QueryResult[dto.BaseItemDto]{Items: page, TotalRecordCount: total})
		return
	}

	items, total, err := h.libRepo.QueryItems(r.Context(), sqlite.ItemFilter{
		ServerID:          h.serverID(),
		UserID:            uid,
		Recursive:         true,
		IncludeItemTypes:  itemTypes,
		ExcludeItemIDs:    hiddenPlaylists,
		AllowedLibraryIDs: allowedLibs,
		MaxParentalRating: pol.MaxParentalRating,
		BlockedTags:       pol.BlockedTags,
		BlockUnratedItems: pol.BlockUnratedItems,
		AllowedTags:       pol.AllowedTags,
		IsUnplayed:        &unplayed,
		SortBy:            []string{"CommunityRating", "DateCreated"},
		SortOrder:         "Descending",
		StartIndex:        startIndex,
		Limit:             limit,
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to load suggestions")
		return
	}
	if total == 0 {
		items, total, err = h.libRepo.QueryItems(r.Context(), sqlite.ItemFilter{
			ServerID:          h.serverID(),
			UserID:            uid,
			Recursive:         true,
			IncludeItemTypes:  itemTypes,
			ExcludeItemIDs:    hiddenPlaylists,
			AllowedLibraryIDs: allowedLibs,
			MaxParentalRating: pol.MaxParentalRating,
			BlockedTags:       pol.BlockedTags,
			BlockUnratedItems: pol.BlockUnratedItems,
			AllowedTags:       pol.AllowedTags,
			SortBy:            []string{"CommunityRating", "DateCreated"},
			SortOrder:         "Descending",
			StartIndex:        startIndex,
			Limit:             limit,
		})
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "failed to load suggestions")
			return
		}
	}
	if items == nil {
		items = []dto.BaseItemDto{}
	}
	var policyRemoved int
	items, policyRemoved = h.postFilterItemsByPolicy(r.Context(), items, pol)
	if policyRemoved > 0 {
		total -= policyRemoved
		if total < 0 {
			total = 0
		}
	}
	var removed int
	items, removed = h.filterVisiblePlaylists(r, items)
	if removed > 0 {
		total -= removed
		if total < 0 {
			total = 0
		}
	}
	if !h.isCallerAdmin(r) {
		dto.SanitizeItemPathsSlice(items)
	}
	httpx.JSON(w, http.StatusOK, dto.QueryResult[dto.BaseItemDto]{Items: items, TotalRecordCount: total})
}

func (h *Handler) getUserGroupingOptions(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "UserId")
	if !h.canModifyUser(r, uid) {
		writeJSONError(w, r, http.StatusForbidden, "forbidden")
		return
	}
	out := []dto.NameIdPair{}
	if h.libRepo != nil {
		allowed := h.allowedLibrariesForUser(r.Context(), uid)
		if libs, err := h.libRepo.ListLibraries(r.Context()); err == nil {
			for _, lib := range libs {
				if !isLibraryAllowed(lib.ID, allowed) {
					continue
				}
				out = append(out, dto.NameIdPair{Name: lib.Name, Id: lib.ID})
			}
		}
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) getUserRootItem(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, dto.BaseItemDto{
		Name:              "Media Folders",
		ServerId:          h.serverID(),
		Id:                guid.Normalize("00000000-0000-0000-0000-000000000001"),
		IsFolder:          true,
		Type:              "UserRootFolder",
		ImageTags:         map[string]string{},
		BackdropImageTags: []string{},
	})
}

func (h *Handler) getUserItemIntros(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, dto.QueryResult[dto.BaseItemDto]{
		Items:            []dto.BaseItemDto{},
		TotalRecordCount: 0,
	})
}

var specialFeatureExtraTypes = []string{
	"Clip", "Featurette", "BehindTheScenes", "DeletedScene", "Interview", "Scene", "Short", "Extra",
}

func (h *Handler) allowedLibrariesForRequest(r *http.Request, fallbackUserID string) []string {
	if h.userRepo == nil {
		return nil
	}
	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil && (ac.User != nil || ac.IsAPIKey) {
		if ac.IsAdmin() {
			return nil
		}
		return h.allowedLibrariesForUser(r.Context(), guid.Normalize(ac.UserID()))
	}
	uid := ""
	if h.auth.UserIDFromCtx != nil {
		uid = guid.Normalize(h.auth.UserIDFromCtx(r.Context()))
	}
	if uid == "" {
		uid = guid.Normalize(fallbackUserID)
	}
	return h.allowedLibrariesForUser(r.Context(), uid)
}

func (h *Handler) allowedLibrariesForUser(ctx context.Context, userID string) []string {
	if h.userRepo == nil {
		return nil
	}
	if userID == "" {
		return []string{}
	}
	u, err := h.userRepo.GetUserByID(ctx, userID)
	if err != nil || u == nil {
		return []string{}
	}
	if u.IsAdmin || u.Policy.IsAdministrator {
		return nil
	}
	pol := u.Policy
	pol.IsAdministrator = false
	if h.libRepo != nil {
		return h.libRepo.AllowedLibraryIDsForPolicy(ctx, pol)
	}
	if pol.EnableAllFolders {
		return nil
	}
	allowed := make([]string, 0, len(pol.EnabledFolders))
	for _, fid := range pol.EnabledFolders {
		if norm := guid.Normalize(fid); norm != "" {
			allowed = append(allowed, norm)
		}
	}
	return allowed
}

func isLibraryAllowed(libID string, allowed []string) bool {
	if allowed == nil {
		return true
	}
	norm := guid.Normalize(libID)
	for _, id := range allowed {
		if id == norm {
			return true
		}
	}
	return false
}

func (h *Handler) canAccessItem(r *http.Request, targetUID, itemID string) bool {
	if h.libRepo == nil || itemID == "" {
		return true
	}
	rec, err := h.libRepo.GetItemRecordByID(r.Context(), itemID)
	if errors.Is(err, sqlite.ErrNotFound) {
		return h.isCallerAdmin(r)
	}
	if err != nil || rec == nil {
		return false
	}
	allowedLibs, pol := h.fullUserPolicyForRequest(r, targetUID)
	if pol.IsAdministrator {
		return true
	}
	if strings.EqualFold(rec.Type, "Playlist") {
		if !h.canViewPlaylist(r, rec.ID, rec.ParentID) {
			return false
		}
	} else if !strings.EqualFold(rec.Type, "BoxSet") && !strings.EqualFold(rec.LibraryID, "collections") && !isLibraryAllowed(rec.LibraryID, allowedLibs) {
		return false
	}
	if blocked, bErr := h.libRepo.IsItemBlockedByPolicy(r.Context(), rec, pol); bErr != nil || blocked {
		return false
	}
	return true
}

func (h *Handler) serveUserItemExtras(w http.ResponseWriter, r *http.Request, extraTypes []string) {
	uid := h.resolveUserID(r, "UserId")
	if !h.canModifyUser(r, uid) {
		writeJSONError(w, r, http.StatusForbidden, "forbidden")
		return
	}
	itemID := guid.Normalize(chi.URLParam(r, "Id"))
	if h.libRepo == nil || itemID == "" {
		httpx.JSON(w, http.StatusOK, []dto.BaseItemDto{})
		return
	}
	rec, err := h.libRepo.GetItemRecordByID(r.Context(), itemID)
	if errors.Is(err, sqlite.ErrNotFound) {
		writeJSONError(w, r, http.StatusNotFound, "item not found")
		return
	}
	if err != nil {
		slog.Error("failed to load item for extras", "item_id", itemID, "error", err)
		httpx.Error(w, http.StatusInternalServerError, "failed to load item")
		return
	}
	allowedLibs, pol := h.fullUserPolicyForRequest(r, uid)
	if !pol.IsAdministrator {
		if strings.EqualFold(rec.Type, "Playlist") {
			if !h.canViewPlaylist(r, rec.ID, rec.ParentID) {
				writeJSONError(w, r, http.StatusForbidden, "access denied")
				return
			}
		} else if !strings.EqualFold(rec.Type, "BoxSet") && !strings.EqualFold(rec.LibraryID, "collections") && !isLibraryAllowed(rec.LibraryID, allowedLibs) {
			writeJSONError(w, r, http.StatusForbidden, "access denied")
			return
		}
		if blocked, bErr := h.libRepo.IsItemBlockedByPolicy(r.Context(), rec, pol); bErr != nil || blocked {
			writeJSONError(w, r, http.StatusForbidden, "access denied")
			return
		}
	}
	items, err := h.libRepo.GetItemExtras(r.Context(), h.serverID(), uid, itemID, extraTypes)
	if err != nil {
		slog.Error("failed to load item extras", "item_id", itemID, "error", err)
		httpx.Error(w, http.StatusInternalServerError, "failed to load item extras")
		return
	}
	hasContentPolicy := !pol.IsAdministrator && (pol.MaxParentalRating != nil ||
		len(pol.BlockedTags) > 0 ||
		len(pol.BlockUnratedItems) > 0 ||
		len(pol.AllowedTags) > 0)
	if hasContentPolicy && len(items) > 0 {
		filtered := make([]dto.BaseItemDto, 0, len(items))
		for _, it := range items {
			exRec, rErr := h.libRepo.GetItemRecordByID(r.Context(), it.Id)
			if rErr != nil || exRec == nil {
				continue
			}
			if blocked, bErr := h.libRepo.IsItemBlockedByPolicy(r.Context(), exRec, pol); bErr != nil || blocked {
				continue
			}
			filtered = append(filtered, it)
		}
		items = filtered
	}
	if items == nil {
		items = []dto.BaseItemDto{}
	}
	if !h.isCallerAdmin(r) {
		dto.SanitizeItemPathsSlice(items)
	}
	httpx.JSON(w, http.StatusOK, items)
}

func (h *Handler) getUserItemLocalTrailers(w http.ResponseWriter, r *http.Request) {
	h.serveUserItemExtras(w, r, []string{"Trailer"})
}

func (h *Handler) getUserItemSpecialFeatures(w http.ResponseWriter, r *http.Request) {
	h.serveUserItemExtras(w, r, specialFeatureExtraTypes)
}

// --- User Item Data / Rating / Favorite / Played / PlayingItems ---

func (h *Handler) deleteUserFavoriteItem(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "UserId")
	if !h.canModifyUser(r, uid) {
		writeJSONError(w, r, http.StatusForbidden, "forbidden")
		return
	}
	itemID := guid.Normalize(chi.URLParam(r, "Id"))
	if !h.canAccessItem(r, uid, itemID) {
		writeJSONError(w, r, http.StatusForbidden, "access denied")
		return
	}
	if h.libRepo != nil && uid != "" && itemID != "" {
		if ud, err := h.libRepo.UnmarkFavorite(r.Context(), uid, itemID); err == nil && ud != nil {
			httpx.JSON(w, http.StatusOK, ud)
			return
		}
	}
	httpx.JSON(w, http.StatusOK, dto.UserItemDataDto{IsFavorite: false, Key: itemID})
}

func userItemDataWithLikes(ud dto.UserItemDataDto, likes *bool) map[string]any {
	out := map[string]any{}
	if raw, err := json.Marshal(ud); err == nil {
		_ = json.Unmarshal(raw, &out)
	}
	if likes != nil {
		out["Likes"] = *likes
	}
	return out
}

func userItemDataDtoFromRecord(rec *sqlite.UserItemDataRecord, fallbackKey string) dto.UserItemDataDto {
	if rec == nil {
		return dto.UserItemDataDto{Key: fallbackKey}
	}
	var lastPlayed *time.Time
	if rec.LastPlayedDate != nil {
		t := time.Unix(*rec.LastPlayedDate, 0).UTC()
		lastPlayed = &t
	}
	key := rec.ItemID
	if key == "" {
		key = fallbackKey
	}
	return dto.UserItemDataDto{
		PlaybackPositionTicks: rec.PlaybackPositionTicks,
		PlayCount:             rec.PlayCount,
		IsFavorite:            rec.IsFavorite,
		LastPlayedDate:        lastPlayed,
		Played:                rec.Played,
		Key:                   key,
		Likes:                 rec.Likes,
	}
}

func (h *Handler) postUserItemRating(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "UserId")
	if !h.canModifyUser(r, uid) {
		writeJSONError(w, r, http.StatusForbidden, "forbidden")
		return
	}
	itemID := guid.Normalize(chi.URLParam(r, "Id"))
	if !h.canAccessItem(r, uid, itemID) {
		writeJSONError(w, r, http.StatusForbidden, "access denied")
		return
	}
	q := httpx.ParseQuery(r)
	likes := q.Bool("Likes")
	val := true
	if likes != nil {
		val = *likes
	}

	if h.userRepo != nil && uid != "" && itemID != "" {
		rec, err := h.userRepo.SetUserItemLikes(r.Context(), uid, itemID, &val)
		if errors.Is(err, sqlite.ErrNotFound) {
			writeJSONError(w, r, http.StatusNotFound, "item not found")
			return
		}
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "failed to update item rating")
			return
		}
		httpx.JSON(w, http.StatusOK, userItemDataDtoFromRecord(rec, itemID))
		return
	}

	_ = h.saveKV(r.Context(), fmt.Sprintf("user_rating:%s:%s", uid, itemID), map[string]any{"Likes": val})
	ud := dto.UserItemDataDto{Key: itemID}
	if h.libRepo != nil && uid != "" && itemID != "" {
		if existing, err := h.libRepo.GetUserItemData(r.Context(), uid, itemID); err == nil && existing != nil {
			ud = *existing
		}
	}
	httpx.JSON(w, http.StatusOK, userItemDataWithLikes(ud, &val))
}

func (h *Handler) deleteUserItemRating(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "UserId")
	if !h.canModifyUser(r, uid) {
		writeJSONError(w, r, http.StatusForbidden, "forbidden")
		return
	}
	itemID := guid.Normalize(chi.URLParam(r, "Id"))
	if !h.canAccessItem(r, uid, itemID) {
		writeJSONError(w, r, http.StatusForbidden, "access denied")
		return
	}

	if h.userRepo != nil && uid != "" && itemID != "" {
		rec, err := h.userRepo.SetUserItemLikes(r.Context(), uid, itemID, nil)
		if errors.Is(err, sqlite.ErrNotFound) {
			writeJSONError(w, r, http.StatusNotFound, "item not found")
			return
		}
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "failed to delete item rating")
			return
		}
		_ = h.deleteKV(r.Context(), fmt.Sprintf("user_rating:%s:%s", uid, itemID))
		httpx.JSON(w, http.StatusOK, userItemDataDtoFromRecord(rec, itemID))
		return
	}

	_ = h.deleteKV(r.Context(), fmt.Sprintf("user_rating:%s:%s", uid, itemID))
	ud := dto.UserItemDataDto{Key: itemID}
	if h.libRepo != nil && uid != "" && itemID != "" {
		if existing, err := h.libRepo.GetUserItemData(r.Context(), uid, itemID); err == nil && existing != nil {
			ud = *existing
		}
	}
	httpx.JSON(w, http.StatusOK, userItemDataWithLikes(ud, nil))
}

func (h *Handler) postUserItemHideFromResume(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "UserId")
	if !h.canModifyUser(r, uid) {
		writeJSONError(w, r, http.StatusForbidden, "forbidden")
		return
	}
	itemID := guid.Normalize(chi.URLParam(r, "Id"))
	if !h.canAccessItem(r, uid, itemID) {
		writeJSONError(w, r, http.StatusForbidden, "access denied")
		return
	}
	q := httpx.ParseQuery(r)
	hide := true
	if b := q.Bool("Hide"); b != nil {
		hide = *b
	}
	kvKey := fmt.Sprintf("user_hide_resume:%s:%s", uid, itemID)
	if hide {
		_ = h.saveKV(r.Context(), kvKey, map[string]any{"Hide": true})
	} else {
		_ = h.deleteKV(r.Context(), kvKey)
	}
	ud := dto.UserItemDataDto{Key: itemID}
	if h.libRepo != nil && uid != "" && itemID != "" {
		if existing, err := h.libRepo.GetUserItemData(r.Context(), uid, itemID); err == nil && existing != nil {
			ud = *existing
		}
	}
	httpx.JSON(w, http.StatusOK, ud)
}

func (h *Handler) postUserItemUserData(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "UserId")
	if !h.canModifyUser(r, uid) {
		writeJSONError(w, r, http.StatusForbidden, "forbidden")
		return
	}
	itemID := guid.Normalize(chi.URLParam(r, "ItemId"))
	if !h.canAccessItem(r, uid, itemID) {
		writeJSONError(w, r, http.StatusForbidden, "access denied")
		return
	}
	var body struct {
		Played                *bool  `json:"Played"`
		IsFavorite            *bool  `json:"IsFavorite"`
		PlaybackPositionTicks *int64 `json:"PlaybackPositionTicks"`
	}
	if err := decodeOptionalJSON(r, &body); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	playedHandled := false
	if h.libRepo != nil && uid != "" && itemID != "" {
		if body.Played != nil {
			playedHandled = true
			if *body.Played {
				_, _ = h.libRepo.MarkPlayed(r.Context(), uid, itemID)
			} else {
				_, _ = h.libRepo.MarkUnplayed(r.Context(), uid, itemID)
			}
		}
		if body.IsFavorite != nil {
			if *body.IsFavorite {
				_, _ = h.libRepo.MarkFavorite(r.Context(), uid, itemID)
			} else {
				_, _ = h.libRepo.UnmarkFavorite(r.Context(), uid, itemID)
			}
		}
	}
	if body.PlaybackPositionTicks != nil && h.userRepo != nil && uid != "" && itemID != "" {
		pos := *body.PlaybackPositionTicks
		if pos < 0 {
			pos = 0
		}
		markPlayed := !playedHandled && body.Played != nil && *body.Played
		_, _ = h.userRepo.UpsertPlaybackProgress(r.Context(), uid, itemID, pos, markPlayed, nil, nil)
	}
	httpx.NoContent(w)
}

func (h *Handler) deleteUserPlayedItem(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "UserId")
	if !h.canModifyUser(r, uid) {
		writeJSONError(w, r, http.StatusForbidden, "forbidden")
		return
	}
	itemID := guid.Normalize(chi.URLParam(r, "Id"))
	if !h.canAccessItem(r, uid, itemID) {
		writeJSONError(w, r, http.StatusForbidden, "access denied")
		return
	}
	if h.libRepo != nil && uid != "" && itemID != "" {
		if ud, err := h.libRepo.MarkUnplayed(r.Context(), uid, itemID); err == nil && ud != nil {
			httpx.JSON(w, http.StatusOK, ud)
			return
		}
	}
	httpx.JSON(w, http.StatusOK, dto.UserItemDataDto{Played: false, Key: itemID})
}

func (h *Handler) clientInfoForRequest(r *http.Request) auth.ClientInfo {
	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil && (ac.User != nil || ac.IsAPIKey) {
		if ac.Client.DeviceID != "" || !ac.IsAdmin() {
			return ac.Client
		}
	}
	return middleware.ExtractClientInfo(r)
}

func (h *Handler) postUserPlayingItemStart(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "UserId")
	if !h.canModifyUser(r, uid) {
		writeJSONError(w, r, http.StatusForbidden, "forbidden")
		return
	}
	itemID := guid.Normalize(chi.URLParam(r, "Id"))
	if !h.canAccessItem(r, uid, itemID) {
		writeJSONError(w, r, http.StatusForbidden, "access denied")
		return
	}
	q := httpx.ParseQuery(r)
	if h.sessions != nil && h.userRepo != nil && uid != "" {
		if u, err := h.userRepo.GetUserByID(r.Context(), uid); err == nil && u != nil {
			client := h.clientInfoForRequest(r)
			_ = h.sessions.ReportPlaybackStart(r.Context(), u, client, session.PlaybackStartRequest{
				ItemId:        itemID,
				MediaSourceId: q.Get("MediaSourceId"),
				PlaySessionId: q.Get("PlaySessionId"),
				PlayMethod:    q.Get("PlayMethod"),
				PositionTicks: q.Int64("PositionTicks", 0),
				CanSeek:       q.Bool("CanSeek") != nil && *q.Bool("CanSeek"),
			})
		}
	}
	httpx.NoContent(w)
}

func (h *Handler) postUserPlayingItemProgress(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "UserId")
	if !h.canModifyUser(r, uid) {
		writeJSONError(w, r, http.StatusForbidden, "forbidden")
		return
	}
	itemID := guid.Normalize(chi.URLParam(r, "Id"))
	if !h.canAccessItem(r, uid, itemID) {
		writeJSONError(w, r, http.StatusForbidden, "access denied")
		return
	}
	q := httpx.ParseQuery(r)
	if h.sessions != nil && h.userRepo != nil && uid != "" {
		if u, err := h.userRepo.GetUserByID(r.Context(), uid); err == nil && u != nil {
			client := h.clientInfoForRequest(r)
			isPaused := q.Bool("IsPaused") != nil && *q.Bool("IsPaused")
			isMuted := q.Bool("IsMuted") != nil && *q.Bool("IsMuted")
			_ = h.sessions.ReportPlaybackProgress(r.Context(), u, client, session.PlaybackProgressRequest{
				ItemId:        itemID,
				MediaSourceId: q.Get("MediaSourceId"),
				PlaySessionId: q.Get("PlaySessionId"),
				PlayMethod:    q.Get("PlayMethod"),
				PositionTicks: q.Int64("PositionTicks", 0),
				IsPaused:      isPaused,
				IsMuted:       isMuted,
				VolumeLevel:   q.Int("VolumeLevel", 100),
				EventName:     q.Get("EventName"),
			})
		}
	}
	httpx.NoContent(w)
}

func (h *Handler) deleteUserPlayingItem(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "UserId")
	if !h.canModifyUser(r, uid) {
		writeJSONError(w, r, http.StatusForbidden, "forbidden")
		return
	}
	itemID := guid.Normalize(chi.URLParam(r, "Id"))
	if !h.canAccessItem(r, uid, itemID) {
		writeJSONError(w, r, http.StatusForbidden, "access denied")
		return
	}
	q := httpx.ParseQuery(r)
	if h.sessions != nil && h.userRepo != nil && uid != "" {
		if u, err := h.userRepo.GetUserByID(r.Context(), uid); err == nil && u != nil {
			client := h.clientInfoForRequest(r)
			_ = h.sessions.ReportPlaybackStopped(r.Context(), u, client, session.PlaybackStoppedRequest{
				ItemId:        itemID,
				MediaSourceId: q.Get("MediaSourceId"),
				PlaySessionId: q.Get("PlaySessionId"),
				PositionTicks: q.Int64("PositionTicks", 0),
			})
		}
	}
	httpx.NoContent(w)
}

// --- User Images ---

func (h *Handler) userImagePath(uid, imgType, idx string) string {
	baseDir := os.TempDir()
	if h.cfg != nil {
		if dd := h.cfg.Snapshot().DataDir; dd != "" {
			baseDir = filepath.Join(dd, "metadata", "users")
		}
	}
	if idx == "" {
		idx = "0"
	}
	safeType := sanitizeFilename(strings.ToLower(imgType))
	safeIdx := sanitizeFilename(idx)
	safeUID := sanitizeFilename(uid)
	return filepath.Join(baseDir, safeUID, safeType+"_"+safeIdx+".img")
}

func sanitizeFilename(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "default"
	}
	return b.String()
}

func (h *Handler) getUserImage(w http.ResponseWriter, r *http.Request) {
	h.serveUserImage(w, r, false)
}

func (h *Handler) headUserImage(w http.ResponseWriter, r *http.Request) {
	h.serveUserImage(w, r, true)
}

func isSafeRasterImageType(ct string) bool {
	switch strings.ToLower(strings.TrimSpace(ct)) {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
		return true
	default:
		return false
	}
}

func (h *Handler) isRemoteAccessAllowed(u *sqlite.UserRecord) bool {
	if !h.isGlobalRemoteAccessEnabled() {
		return false
	}
	if u != nil && !u.Policy.EnableRemoteAccess {
		return false
	}
	return true
}

func (h *Handler) resolveUserImageCaller(r *http.Request) (authed bool, remoteDenied bool, callerID string, isAdmin bool) {
	isLocal := netutil.IsLocalOrLANRequest(r)
	checkUser := func(u *sqlite.UserRecord) (bool, bool, string, bool) {
		if u == nil || u.IsDisabled || u.Policy.IsDisabled {
			return false, false, "", false
		}
		if !isLocal && !h.isRemoteAccessAllowed(u) {
			return false, true, "", false
		}
		return true, false, guid.Normalize(u.ID), u.IsAdmin || u.Policy.IsAdministrator
	}

	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil && (ac.User != nil || ac.IsAPIKey) {
		if ac.IsAPIKey {
			return true, false, guid.Normalize(ac.UserID()), true
		}
		return checkUser(ac.User)
	}
	if h.auth.UserIDFromCtx != nil {
		if cid := strings.TrimSpace(h.auth.UserIDFromCtx(r.Context())); cid != "" {
			normCID := guid.Normalize(cid)
			if h.userRepo != nil {
				if u, err := h.userRepo.GetUserByID(r.Context(), normCID); err == nil && u != nil {
					return checkUser(u)
				}
			}
			if !isLocal && !h.isGlobalRemoteAccessEnabled() {
				return false, true, "", false
			}
			return true, false, normCID, false
		}
	}
	if tok := strings.TrimSpace(middleware.ExtractClientInfo(r).Token); tok != "" {
		if h.authSvc != nil {
			if id, err := h.authSvc.ValidateToken(r.Context(), tok); err == nil && id != nil {
				if id.IsAPIKey {
					return true, false, "", true
				}
				return checkUser(id.User)
			}
		} else if h.userRepo != nil {
			if at, err := h.userRepo.GetAccessToken(r.Context(), tok); err == nil && at != nil {
				if u, uErr := h.userRepo.GetUserByID(r.Context(), at.UserID); uErr == nil && u != nil {
					return checkUser(u)
				}
				if !isLocal && !h.isGlobalRemoteAccessEnabled() {
					return false, true, "", false
				}
				return true, false, guid.Normalize(at.UserID), false
			}
			if _, err := h.userRepo.GetAPIKey(r.Context(), tok); err == nil {
				return true, false, "", true
			}
		}
	}
	return false, false, "", false
}

func (h *Handler) isRequestAuthenticated(r *http.Request) bool {
	authed, _, _, _ := h.resolveUserImageCaller(r)
	return authed
}

func (h *Handler) serveUserImage(w http.ResponseWriter, r *http.Request, headOnly bool) {
	uid := h.resolveUserID(r, "Id")
	authed, remoteDenied, callerID, isAdmin := h.resolveUserImageCaller(r)
	if remoteDenied {
		writeJSONError(w, r, http.StatusForbidden, "Remote access is disabled")
		return
	}
	if !authed && h.userRepo != nil {
		if (h.cfg != nil && !h.cfg.Snapshot().EnableLoginUserList) || uid == "" {
			writeJSONError(w, r, http.StatusNotFound, "user image not found")
			return
		}
		u, err := h.userRepo.GetUserByID(r.Context(), uid)
		if err != nil || u == nil || u.IsDisabled || u.Policy.IsDisabled || u.Policy.IsHidden {
			writeJSONError(w, r, http.StatusNotFound, "user image not found")
			return
		}
	} else if authed && !isAdmin && h.userRepo != nil && (uid == "" || guid.Normalize(callerID) != guid.Normalize(uid)) {
		if uid == "" {
			writeJSONError(w, r, http.StatusNotFound, "user image not found")
			return
		}
		u, err := h.userRepo.GetUserByID(r.Context(), uid)
		if err != nil || u == nil || u.IsDisabled || u.Policy.IsDisabled || u.Policy.IsHidden {
			writeJSONError(w, r, http.StatusNotFound, "user image not found")
			return
		}
	}
	imgType := chi.URLParam(r, "Type")
	idx := chi.URLParam(r, "Index")
	p := h.userImagePath(uid, imgType, idx)

	data, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			slog.Error("permission denied reading media file",
				"path", p,
				"uid", os.Geteuid(),
				"gid", os.Getegid(),
				"error", err,
			)
			writeJSONError(w, r, http.StatusForbidden, "permission denied reading media file")
			return
		}
		writeJSONError(w, r, http.StatusNotFound, "user image not found")
		return
	}
	ct := http.DetectContentType(data)
	if !isSafeRasterImageType(ct) {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
	w.WriteHeader(http.StatusOK)
	if !headOnly {
		_, _ = w.Write(data)
	}
}

func (h *Handler) callerUserID(r *http.Request) string {
	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil {
		return guid.Normalize(ac.UserID())
	}
	if h.auth.UserIDFromCtx != nil {
		return guid.Normalize(h.auth.UserIDFromCtx(r.Context()))
	}
	return ""
}

func (h *Handler) isCallerAdmin(r *http.Request) bool {
	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil {
		return ac.IsAdmin()
	}
	if h.auth.UserIDFromCtx != nil {
		callerID := guid.Normalize(h.auth.UserIDFromCtx(r.Context()))
		if callerID != "" && h.userRepo != nil {
			if u, err := h.userRepo.GetUserByID(r.Context(), callerID); err == nil && u != nil {
				return u.IsAdmin || u.Policy.IsAdministrator
			}
		}
	}
	return false
}

func (h *Handler) canModifyUser(r *http.Request, targetUID string) bool {
	targetID := guid.Normalize(targetUID)
	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil {
		return ac.IsAdmin() || (targetID != "" && guid.Normalize(ac.UserID()) == targetID)
	}
	if h.auth.UserIDFromCtx != nil {
		callerID := guid.Normalize(h.auth.UserIDFromCtx(r.Context()))
		if callerID == "" {
			return false
		}
		if callerID == targetID {
			return true
		}
		if h.userRepo != nil {
			if u, err := h.userRepo.GetUserByID(r.Context(), callerID); err == nil && u != nil {
				return u.IsAdmin || u.Policy.IsAdministrator
			}
		}
	}
	return false
}

func (h *Handler) postUserImage(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "Id")
	if !h.canModifyUser(r, uid) {
		writeJSONError(w, r, http.StatusForbidden, "forbidden")
		return
	}
	imgType := strings.TrimSpace(chi.URLParam(r, "Type"))
	idx := strings.TrimSpace(chi.URLParam(r, "Index"))
	if !strings.EqualFold(imgType, "Primary") || (idx != "" && idx != "0") {
		httpx.Error(w, http.StatusBadRequest, "only Primary image at index 0 is supported")
		return
	}
	p := h.userImagePath(uid, "Primary", "0")

	raw, err := io.ReadAll(io.LimitReader(r.Body, 10<<20))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "failed to read image body")
		return
	}
	// Emby clients often POST base64-encoded image payloads.
	if decoded, decErr := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw))); decErr == nil && len(decoded) > 0 {
		raw = decoded
	}
	if len(raw) == 0 || !isSafeRasterImageType(http.DetectContentType(raw)) {
		httpx.Error(w, http.StatusBadRequest, "unsupported image format")
		return
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to prepare user image directory")
		return
	}
	if err := os.WriteFile(p, raw, 0o644); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to save user image")
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) deleteUserImage(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "Id")
	if !h.canModifyUser(r, uid) {
		writeJSONError(w, r, http.StatusForbidden, "forbidden")
		return
	}
	imgType := chi.URLParam(r, "Type")
	idx := chi.URLParam(r, "Index")
	p := h.userImagePath(uid, imgType, idx)
	_ = os.Remove(p)
	httpx.NoContent(w)
}

// --- User Connect Link ---

func (h *Handler) postUserConnectLink(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "Id")
	if !h.canModifyUser(r, uid) {
		writeJSONError(w, r, http.StatusForbidden, "forbidden")
		return
	}
	q := httpx.ParseQuery(r)
	connectUser := q.Get("ConnectUsername")
	var body struct {
		ConnectUsername string `json:"ConnectUsername"`
	}
	_ = decodeOptionalJSON(r, &body)
	if body.ConnectUsername != "" {
		connectUser = body.ConnectUsername
	}
	_ = h.saveKV(r.Context(), "user_connect_link:"+uid, map[string]any{
		"UserId":          uid,
		"ConnectUsername": connectUser,
		"LinkedAt":        time.Now().UTC().Format(time.RFC3339),
	})
	httpx.JSON(w, http.StatusOK, map[string]any{
		"GuestDisplayName":    connectUser,
		"IsPending":           false,
		"IsNewUserInvitation": false,
	})
}

func (h *Handler) deleteUserConnectLink(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "Id")
	if !h.canModifyUser(r, uid) {
		writeJSONError(w, r, http.StatusForbidden, "forbidden")
		return
	}
	_ = h.deleteKV(r.Context(), "user_connect_link:"+uid)
	httpx.NoContent(w)
}

// --- /UserSettings ---

func (h *Handler) getUserSettings(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "UserId")
	if !h.canModifyUser(r, uid) {
		writeJSONError(w, r, http.StatusForbidden, "forbidden")
		return
	}
	out := map[string]any{}
	_, _ = h.loadKV(r.Context(), "user_settings:"+uid, &out)
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) postUserSettings(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "UserId")
	if !h.canModifyUser(r, uid) {
		writeJSONError(w, r, http.StatusForbidden, "forbidden")
		return
	}
	if r.ContentLength > maxKVJSONBodyBytes {
		httpx.Error(w, http.StatusRequestEntityTooLarge, "request body too large")
		return
	}
	body := map[string]any{}
	if err := decodeOptionalJSONLimit(r, &body, maxKVJSONBodyBytes); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	_ = h.saveKV(r.Context(), "user_settings:"+uid, body)
	httpx.NoContent(w)
}

func (h *Handler) postUserSettingsPartial(w http.ResponseWriter, r *http.Request) {
	uid := h.resolveUserID(r, "UserId")
	if !h.canModifyUser(r, uid) {
		writeJSONError(w, r, http.StatusForbidden, "forbidden")
		return
	}
	if r.ContentLength > maxKVJSONBodyBytes {
		httpx.Error(w, http.StatusRequestEntityTooLarge, "request body too large")
		return
	}
	patch := map[string]any{}
	if err := decodeOptionalJSONLimit(r, &patch, maxKVJSONBodyBytes); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	h.mu.Lock()
	current := map[string]any{}
	_, _ = h.loadKVLocked(r.Context(), "user_settings:"+uid, &current)
	for k, v := range patch {
		current[k] = v
	}
	rawMerged, err := json.Marshal(current)
	if err != nil {
		h.mu.Unlock()
		httpx.Error(w, http.StatusInternalServerError, "failed to encode user settings")
		return
	}
	if len(rawMerged) > maxKVJSONBodyBytes {
		h.mu.Unlock()
		httpx.Error(w, http.StatusRequestEntityTooLarge, "user settings payload too large")
		return
	}
	_ = h.saveKVLocked(r.Context(), "user_settings:"+uid, current)
	h.mu.Unlock()
	httpx.NoContent(w)
}
