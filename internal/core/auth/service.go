package auth

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrUserDisabled       = errors.New("user is disabled")
	ErrUnauthorized       = errors.New("unauthorized")
	ErrUserExists         = errors.New("user already exists")
	ErrUserNotFound       = errors.New("user not found")
	ErrInvalidUsername    = errors.New("invalid username")
)

// ContainsControlChar reports whether s contains any Unicode control character.
func ContainsControlChar(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

const (
	tokenCacheTTL       = 5 * time.Minute
	activityFlushWindow = 30 * time.Second
	maxTokenCacheSize   = 4096
)

// ClientInfo holds parsed Emby client metadata extracted from headers or query parameters.
type ClientInfo struct {
	Client     string
	Device     string
	DeviceID   string
	Version    string
	Token      string
	RemoteAddr string
}

// TokenIdentity represents a validated access token or API key.
type TokenIdentity struct {
	Token      string
	User       *sqlite.UserRecord
	IsAPIKey   bool
	APIKeyName string
	DeviceID   string
	DeviceName string
	AppName    string
	AppVersion string
}

type cachedToken struct {
	identity  *TokenIdentity
	expiresAt time.Time
	lastFlush time.Time
}

type Service struct {
	repo *sqlite.UserRepo
	cfg  *config.Config

	mu          sync.RWMutex
	cache       map[string]*cachedToken
	revokeEpoch uint64
}

func NewService(repo *sqlite.UserRepo, cfg *config.Config) *Service {
	return &Service{
		repo:  repo,
		cfg:   cfg,
		cache: make(map[string]*cachedToken),
	}
}

// Config returns the underlying server configuration pointer (may be nil in isolated tests).
func (s *Service) Config() *config.Config {
	if s == nil {
		return nil
	}
	return s.cfg
}

// EnsureDefaultAdmin creates an initial administrator user when the users table is empty
// ONLY if AMBRA_ADMIN_PASS is explicitly set (and AMBRA_SKIP_DEFAULT_ADMIN is not enabled).
// Otherwise it skips creation so fresh instances boot directly into the /setup wizard.
func (s *Service) EnsureDefaultAdmin(ctx context.Context) error {
	if skip := strings.TrimSpace(os.Getenv("AMBRA_SKIP_DEFAULT_ADMIN")); strings.EqualFold(skip, "true") || skip == "1" {
		return nil
	}

	count, err := s.repo.CountUsers(ctx)
	if err != nil {
		return fmt.Errorf("count users: %w", err)
	}
	if count > 0 {
		return nil
	}

	password, hasPassEnv := os.LookupEnv("AMBRA_ADMIN_PASS")
	if !hasPassEnv {
		slog.Info("no users found; complete initial setup via Web UI (/setup) or set AMBRA_ADMIN_PASS for headless bootstrap")
		return nil
	}

	username := strings.TrimSpace(os.Getenv("AMBRA_ADMIN_USER"))
	if username == "" {
		username = "admin"
	}

	_, err = s.CreateUser(ctx, username, password, true)
	return err
}

func (s *Service) CreateUser(ctx context.Context, name, password string, isAdmin bool) (*sqlite.UserRecord, error) {
	if ContainsControlChar(name) {
		return nil, ErrInvalidUsername
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrInvalidUsername
	}
	if existing, err := s.repo.GetUserByName(ctx, name); err == nil && existing != nil {
		return nil, ErrUserExists
	}

	hash, err := HashPassword(password)
	if err != nil {
		return nil, err
	}

	u := &sqlite.UserRecord{
		ID:           guid.New(),
		Name:         name,
		PasswordHash: hash,
		IsAdmin:      isAdmin,
		IsDisabled:   false,
		Policy:       sqlite.DefaultUserPolicy(isAdmin),
		Config:       sqlite.DefaultUserConfig(),
		CreatedAt:    time.Now().UTC(),
	}
	if err := s.repo.CreateUser(ctx, u); err != nil {
		if errors.Is(err, sqlite.ErrUserExists) {
			return nil, ErrUserExists
		}
		return nil, err
	}
	return u, nil
}

// AuthenticateByName validates username/password and issues a new access token.
func (s *Service) AuthenticateByName(ctx context.Context, username, password, legacySHA1 string, client ClientInfo) (*dto.AuthenticationResult, error) {
	u, err := s.repo.GetUserByName(ctx, strings.TrimSpace(username))
	if err != nil {
		if errors.Is(err, sqlite.ErrNotFound) {
			dummyPasswordWork(password)
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}

	if u.IsDisabled || u.Policy.IsDisabled {
		dummyPasswordWork(password)
		return nil, ErrUserDisabled
	}

	ok := false
	if u.PasswordHash == "" {
		// Empty password user: accept empty pw or SHA1("") from legacy clients
		emptySHA1 := sha1Hex("")
		ok = password == "" && (legacySHA1 == "" || strings.EqualFold(legacySHA1, emptySHA1))
		if !ok {
			dummyPasswordWork(password)
		}
	} else {
		ok, err = VerifyPassword(password, u.PasswordHash)
		if err != nil {
			dummyPasswordWork(password)
			return nil, ErrInvalidCredentials
		}
	}
	if !ok {
		return nil, ErrInvalidCredentials
	}

	return s.IssueSessionForUser(ctx, u, client)
}

// IssueSessionForUser issues a new access token and session for an already-authenticated user
// (used by password login and QuickConnect).
func (s *Service) IssueSessionForUser(ctx context.Context, u *sqlite.UserRecord, client ClientInfo) (*dto.AuthenticationResult, error) {
	if u == nil {
		return nil, ErrUserNotFound
	}
	if u.IsDisabled || u.Policy.IsDisabled {
		return nil, ErrUserDisabled
	}

	token, err := GenerateToken()
	if err != nil {
		return nil, fmt.Errorf("generate token: %w", err)
	}

	now := time.Now().UTC()
	deviceID := client.DeviceID
	if deviceID == "" {
		deviceID = guid.New()
	}
	deviceName := client.Device
	if deviceName == "" {
		deviceName = "Unknown Device"
	}
	appName := client.Client
	if appName == "" {
		appName = "Ambra Client"
	}
	appVersion := client.Version
	if appVersion == "" {
		appVersion = "1.0.0"
	}

	tokRec := &sqlite.AccessTokenRecord{
		Token:        token,
		UserID:       u.ID,
		DeviceID:     deviceID,
		DeviceName:   deviceName,
		AppName:      appName,
		AppVersion:   appVersion,
		CreatedAt:    now,
		LastActiveAt: now,
	}
	if err := s.repo.CreateAccessToken(ctx, tokRec); err != nil {
		return nil, err
	}
	_ = s.repo.UpdateLastLogin(ctx, u.ID, now)
	u.LastLoginAt = &now
	u.LastActiveAt = &now

	identity := &TokenIdentity{
		Token:      token,
		User:       u,
		IsAPIKey:   false,
		DeviceID:   deviceID,
		DeviceName: deviceName,
		AppName:    appName,
		AppVersion: appVersion,
	}
	s.putCache(token, identity, now)

	var serverID string
	if s.cfg != nil {
		serverID = s.cfg.Snapshot().ServerID
	}

	sessInfo := dto.SessionInfo{
		Id:                 SessionID(u.ID, deviceID),
		ServerId:           serverID,
		UserId:             u.ID,
		UserName:           u.Name,
		Client:             appName,
		LastActivityDate:   now,
		DeviceName:         deviceName,
		DeviceId:           deviceID,
		ApplicationVersion: appVersion,
		PlayState: dto.PlayerStateInfo{
			RepeatMode: "RepeatNone",
		},
		PlayableMediaTypes: []string{},
		SupportedCommands:  []string{},
	}

	return &dto.AuthenticationResult{
		User:        s.ToUserDto(u),
		SessionInfo: sessInfo,
		AccessToken: token,
		ServerId:    serverID,
	}, nil
}

// ValidateToken resolves a token against the in-memory cache and SQLite (access_tokens & api_keys).
func (s *Service) ValidateToken(ctx context.Context, token string) (*TokenIdentity, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, ErrUnauthorized
	}

	now := time.Now().UTC()

	s.mu.RLock()
	epoch := s.revokeEpoch
	entry, ok := s.cache[token]
	var (
		identity   *TokenIdentity
		expiresAt  time.Time
		lastFlush  time.Time
		isDisabled bool
	)
	if ok {
		identity = entry.identity
		expiresAt = entry.expiresAt
		lastFlush = entry.lastFlush
		isDisabled = !identity.IsAPIKey && identity.User != nil && (identity.User.IsDisabled || identity.User.Policy.IsDisabled)
	}
	s.mu.RUnlock()

	if ok && now.Before(expiresAt) {
		if isDisabled {
			s.InvalidateTokenCache(token)
			return nil, ErrUserDisabled
		}
		// Throttle DB last_active_at updates to once every 30s
		if !identity.IsAPIKey && now.Sub(lastFlush) >= activityFlushWindow {
			s.mu.Lock()
			if cur, exists := s.cache[token]; exists && now.Sub(cur.lastFlush) >= activityFlushWindow {
				cur.lastFlush = now
				uid := cur.identity.User.ID
				go func(tok, userID string, ts time.Time) {
					bgCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
					defer cancel()
					_ = s.repo.TouchAccessToken(bgCtx, tok, ts)
					_ = s.repo.UpdateLastActive(bgCtx, userID, ts)
				}(token, uid, now)
			}
			s.mu.Unlock()
		}
		return identity, nil
	} else if ok {
		s.mu.Lock()
		epoch = s.revokeEpoch
		if cur, exists := s.cache[token]; exists && !now.Before(cur.expiresAt) {
			delete(s.cache, token)
		}
		s.mu.Unlock()
	}

	// Cache miss: check access_tokens first
	if tokRec, err := s.repo.GetAccessToken(ctx, token); err == nil {
		user, err := s.repo.GetUserByID(ctx, tokRec.UserID)
		if err != nil {
			return nil, ErrUnauthorized
		}
		if user.IsDisabled || user.Policy.IsDisabled {
			return nil, ErrUserDisabled
		}
		id := &TokenIdentity{
			Token:      token,
			User:       user,
			IsAPIKey:   false,
			DeviceID:   tokRec.DeviceID,
			DeviceName: tokRec.DeviceName,
			AppName:    tokRec.AppName,
			AppVersion: tokRec.AppVersion,
		}
		s.putCacheWithEpoch(token, id, now, epoch)
		return id, nil
	}

	// Check api_keys
	if apiKey, err := s.repo.GetAPIKey(ctx, token); err == nil {
		id := &TokenIdentity{
			Token:      token,
			IsAPIKey:   true,
			APIKeyName: apiKey.AppName,
			AppName:    apiKey.AppName,
		}
		s.putCacheWithEpoch(token, id, now, epoch)
		return id, nil
	}

	return nil, ErrUnauthorized
}

func (s *Service) CreateAPIKey(ctx context.Context, appName string) (*sqlite.APIKeyRecord, error) {
	appName = strings.TrimSpace(appName)
	if appName == "" {
		appName = "API Client"
	}
	token, err := GenerateToken()
	if err != nil {
		return nil, err
	}
	rec := &sqlite.APIKeyRecord{
		Token:     token,
		AppName:   appName,
		CreatedAt: time.Now().UTC(),
	}
	if err := s.repo.CreateAPIKey(ctx, rec); err != nil {
		return nil, err
	}
	return rec, nil
}

func (s *Service) ListAPIKeys(ctx context.Context) ([]sqlite.APIKeyRecord, error) {
	return s.repo.ListAPIKeys(ctx)
}

func (s *Service) RevokeAPIKey(ctx context.Context, idOrToken string) error {
	deletedToken, err := s.repo.DeleteAPIKeyByIDOrToken(ctx, idOrToken)
	if err != nil {
		return err
	}
	if deletedToken != "" {
		s.InvalidateAPIKey(deletedToken)
	} else {
		s.InvalidateAPIKey(idOrToken)
	}
	return nil
}

// InvalidateAPIKey evicts a revoked API key token from the in-memory auth cache.
func (s *Service) InvalidateAPIKey(token string) {
	if s == nil {
		return
	}
	s.InvalidateTokenCache(token)
}

func (s *Service) RevokeToken(ctx context.Context, token string) error {
	if err := s.repo.DeleteAccessToken(ctx, token); err != nil {
		return err
	}
	s.InvalidateTokenCache(token)
	return nil
}

func (s *Service) RevokeDevice(ctx context.Context, deviceID string) error {
	if err := s.repo.DeleteTokensByDevice(ctx, deviceID); err != nil {
		return err
	}
	s.mu.Lock()
	s.revokeEpoch++
	for tok, entry := range s.cache {
		if entry.identity.DeviceID == deviceID {
			delete(s.cache, tok)
		}
	}
	s.mu.Unlock()
	return nil
}

// RevokeUserTokens revokes all active access tokens for userID in both SQLite and memory cache.
func (s *Service) RevokeUserTokens(ctx context.Context, userID string) error {
	if err := s.repo.DeleteTokensByUser(ctx, userID); err != nil {
		return err
	}
	s.InvalidateUserCache(userID)
	return nil
}

func (s *Service) ChangePassword(ctx context.Context, userID, currentPw, newPw string, isAdminAction bool) error {
	return s.ChangePasswordKeepToken(ctx, userID, currentPw, newPw, isAdminAction, "")
}

// ChangePasswordKeepToken updates a user's password and revokes all existing tokens except keepToken (if non-empty).
func (s *Service) ChangePasswordKeepToken(ctx context.Context, userID, currentPw, newPw string, isAdminAction bool, keepToken string) error {
	u, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		if errors.Is(err, sqlite.ErrNotFound) {
			return ErrUserNotFound
		}
		return err
	}
	if !isAdminAction {
		if u.PasswordHash == "" {
			if currentPw != "" {
				return ErrInvalidCredentials
			}
		} else {
			ok, err := VerifyPassword(currentPw, u.PasswordHash)
			if err != nil || !ok {
				return ErrInvalidCredentials
			}
		}
	}
	hash, err := HashPassword(newPw)
	if err != nil {
		return err
	}
	if err := s.repo.UpdateUserPassword(ctx, u.ID, hash); err != nil {
		return err
	}
	keepToken = strings.TrimSpace(keepToken)
	if keepToken != "" {
		if err := s.repo.DeleteTokensByUserExcept(ctx, u.ID, keepToken); err != nil {
			return err
		}
		s.invalidateUserCacheExcept(u.ID, keepToken)
		return nil
	}
	return s.RevokeUserTokens(ctx, u.ID)
}

func (s *Service) invalidateUserCacheExcept(userID, keepToken string) {
	norm := guid.Normalize(userID)
	s.mu.Lock()
	s.revokeEpoch++
	for tok, entry := range s.cache {
		if tok == keepToken {
			continue
		}
		if entry.identity.User != nil && guid.Normalize(entry.identity.User.ID) == norm {
			delete(s.cache, tok)
		}
	}
	s.mu.Unlock()
}

func (s *Service) InvalidateTokenCache(token string) {
	token = strings.TrimSpace(token)
	s.mu.Lock()
	s.revokeEpoch++
	delete(s.cache, token)
	s.mu.Unlock()
}

func (s *Service) InvalidateUserCache(userID string) {
	norm := guid.Normalize(userID)
	s.mu.Lock()
	s.revokeEpoch++
	for tok, entry := range s.cache {
		if entry.identity.User != nil && guid.Normalize(entry.identity.User.ID) == norm {
			delete(s.cache, tok)
		}
	}
	s.mu.Unlock()
}

func (s *Service) ToUserDto(u *sqlite.UserRecord) dto.UserDto {
	hasPw := u.PasswordHash != ""
	var dataDir, serverID string
	if s.cfg != nil {
		snap := s.cfg.Snapshot()
		dataDir = snap.DataDir
		serverID = snap.ServerID
	}
	return dto.UserDto{
		Name:                      u.Name,
		ServerId:                  serverID,
		Id:                        u.ID,
		PrimaryImageTag:           UserPrimaryImageTag(dataDir, u.ID),
		HasPassword:               hasPw,
		HasConfiguredPassword:     hasPw,
		HasConfiguredEasyPassword: false,
		LastLoginDate:             u.LastLoginAt,
		LastActivityDate:          u.LastActiveAt,
		Configuration:             u.Config,
		Policy:                    u.Policy,
	}
}

// UserPrimaryImageTag returns a deterministic cache tag if the user has a saved primary avatar image.
func UserPrimaryImageTag(dataDir, userID string) string {
	if dataDir == "" || userID == "" {
		return ""
	}
	safeUID := sanitizeUserDir(guid.Normalize(userID))
	candidates := []string{
		filepath.Join(dataDir, "metadata", "users", safeUID, "primary_0.img"),
		filepath.Join(dataDir, "users", safeUID, "primary.jpg"),
		filepath.Join(dataDir, "users", safeUID, "primary.jpeg"),
		filepath.Join(dataDir, "users", safeUID, "primary.png"),
		filepath.Join(dataDir, "users", safeUID, "primary.webp"),
	}
	for _, p := range candidates {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return sha1Hex(fmt.Sprintf("%s:%d:%d", p, info.Size(), info.ModTime().UnixNano()))[:16]
		}
	}
	return ""
}

func sanitizeUserDir(s string) string {
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

func (s *Service) putCache(token string, id *TokenIdentity, now time.Time) {
	s.mu.Lock()
	s.putCacheLocked(token, id, now)
	s.mu.Unlock()
}

func (s *Service) putCacheWithEpoch(token string, id *TokenIdentity, now time.Time, epoch uint64) {
	s.mu.Lock()
	if s.revokeEpoch == epoch {
		s.putCacheLocked(token, id, now)
	}
	s.mu.Unlock()
}

func (s *Service) putCacheLocked(token string, id *TokenIdentity, now time.Time) {
	if _, exists := s.cache[token]; !exists && len(s.cache) >= maxTokenCacheSize {
		// First purge all expired entries.
		for tok, e := range s.cache {
			if !now.Before(e.expiresAt) {
				delete(s.cache, tok)
			}
		}
		// If still at capacity, evict the entry with the earliest expiry.
		if len(s.cache) >= maxTokenCacheSize {
			var oldestTok string
			var oldestExp time.Time
			for tok, e := range s.cache {
				if oldestTok == "" || e.expiresAt.Before(oldestExp) {
					oldestTok = tok
					oldestExp = e.expiresAt
				}
			}
			if oldestTok != "" {
				delete(s.cache, oldestTok)
			}
		}
	}
	s.cache[token] = &cachedToken{
		identity:  id,
		expiresAt: now.Add(tokenCacheTTL),
		lastFlush: now,
	}
}

// SessionID produces a deterministic session identifier for a (userID, deviceID) pair.
func SessionID(userID, deviceID string) string {
	sum := sha1.Sum([]byte(guid.Normalize(userID) + ":" + deviceID))
	return hex.EncodeToString(sum[:16])
}

func sha1Hex(s string) string {
	sum := sha1.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}
