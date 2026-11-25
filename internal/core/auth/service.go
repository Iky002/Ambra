package auth

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

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
)

const (
	tokenCacheTTL       = 5 * time.Minute
	activityFlushWindow = 30 * time.Second
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
	identity   *TokenIdentity
	expiresAt  time.Time
	lastFlush  time.Time
}

type Service struct {
	repo *sqlite.UserRepo
	cfg  *config.Config

	mu    sync.RWMutex
	cache map[string]*cachedToken
}

func NewService(repo *sqlite.UserRepo, cfg *config.Config) *Service {
	return &Service{
		repo:  repo,
		cfg:   cfg,
		cache: make(map[string]*cachedToken),
	}
}

// EnsureDefaultAdmin creates a default administrator user if the users table is empty.
func (s *Service) EnsureDefaultAdmin(ctx context.Context) error {
	count, err := s.repo.CountUsers(ctx)
	if err != nil {
		return fmt.Errorf("count users: %w", err)
	}
	if count > 0 {
		return nil
	}

	username := strings.TrimSpace(os.Getenv("AMBRA_ADMIN_USER"))
	if username == "" {
		username = "admin"
	}
	password, hasPassEnv := os.LookupEnv("AMBRA_ADMIN_PASS")
	if !hasPassEnv {
		password = "admin"
	}

	_, err = s.CreateUser(ctx, username, password, true)
	return err
}

func (s *Service) CreateUser(ctx context.Context, name, password string, isAdmin bool) (*sqlite.UserRecord, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("username cannot be empty")
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
		return nil, err
	}
	return u, nil
}

// AuthenticateByName validates username/password and issues a new access token.
func (s *Service) AuthenticateByName(ctx context.Context, username, password, legacySHA1 string, client ClientInfo) (*dto.AuthenticationResult, error) {
	u, err := s.repo.GetUserByName(ctx, strings.TrimSpace(username))
	if err != nil {
		if errors.Is(err, sqlite.ErrNotFound) {
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}
	if u.IsDisabled || u.Policy.IsDisabled {
		return nil, ErrUserDisabled
	}

	ok := false
	if u.PasswordHash == "" {
		// Empty password user: accept empty pw or SHA1("") from legacy clients
		emptySHA1 := sha1Hex("")
		ok = password == "" && (legacySHA1 == "" || strings.EqualFold(legacySHA1, emptySHA1))
	} else {
		ok, err = VerifyPassword(password, u.PasswordHash)
		if err != nil {
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

	sessInfo := dto.SessionInfo{
		Id:                 SessionID(u.ID, deviceID),
		ServerId:           s.cfg.ServerID,
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
		ServerId:    s.cfg.ServerID,
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
	entry, ok := s.cache[token]
	s.mu.RUnlock()

	if ok && now.Before(entry.expiresAt) {
		if !entry.identity.IsAPIKey && entry.identity.User != nil && (entry.identity.User.IsDisabled || entry.identity.User.Policy.IsDisabled) {
			s.InvalidateTokenCache(token)
			return nil, ErrUserDisabled
		}
		// Throttle DB last_active_at updates to once every 30s
		if !entry.identity.IsAPIKey && now.Sub(entry.lastFlush) >= activityFlushWindow {
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
		return entry.identity, nil
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
		s.putCache(token, id, now)
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
		s.putCache(token, id, now)
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

func (s *Service) RevokeAPIKey(ctx context.Context, token string) error {
	s.InvalidateTokenCache(token)
	return s.repo.DeleteAPIKey(ctx, token)
}

func (s *Service) RevokeToken(ctx context.Context, token string) error {
	s.InvalidateTokenCache(token)
	return s.repo.DeleteAccessToken(ctx, token)
}

func (s *Service) RevokeDevice(ctx context.Context, deviceID string) error {
	s.mu.Lock()
	for tok, entry := range s.cache {
		if entry.identity.DeviceID == deviceID {
			delete(s.cache, tok)
		}
	}
	s.mu.Unlock()
	return s.repo.DeleteTokensByDevice(ctx, deviceID)
}

func (s *Service) ChangePassword(ctx context.Context, userID, currentPw, newPw string, isAdminAction bool) error {
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
	s.InvalidateUserCache(u.ID)
	return nil
}

func (s *Service) InvalidateTokenCache(token string) {
	s.mu.Lock()
	delete(s.cache, token)
	s.mu.Unlock()
}

func (s *Service) InvalidateUserCache(userID string) {
	norm := guid.Normalize(userID)
	s.mu.Lock()
	for tok, entry := range s.cache {
		if entry.identity.User != nil && guid.Normalize(entry.identity.User.ID) == norm {
			delete(s.cache, tok)
		}
	}
	s.mu.Unlock()
}

func (s *Service) ToUserDto(u *sqlite.UserRecord) dto.UserDto {
	hasPw := u.PasswordHash != ""
	var dataDir string
	if s.cfg != nil {
		dataDir = s.cfg.DataDir
	}
	return dto.UserDto{
		Name:                      u.Name,
		ServerId:                  s.cfg.ServerID,
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
	s.cache[token] = &cachedToken{
		identity:  id,
		expiresAt: now.Add(tokenCacheTTL),
		lastFlush: now,
	}
	s.mu.Unlock()
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
