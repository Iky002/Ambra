package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/pkg/guid"
)

type UserRecord struct {
	ID           string
	Name         string
	PasswordHash string
	IsAdmin      bool
	IsDisabled   bool
	Policy       dto.UserPolicy
	Config       dto.UserConfiguration
	LastLoginAt  *time.Time
	LastActiveAt *time.Time
	CreatedAt    time.Time
}

type AccessTokenRecord struct {
	Token        string
	UserID       string
	DeviceID     string
	DeviceName   string
	AppName      string
	AppVersion   string
	CreatedAt    time.Time
	LastActiveAt time.Time
}

type DeviceRecord struct {
	ID               string
	Name             string
	AppName          string
	AppVersion       string
	LastUserID       string
	LastUserName     string
	DateLastActivity time.Time
}

type APIKeyRecord struct {
	KeyID     string
	Token     string
	AppName   string
	CreatedAt time.Time
}

type CameraUploadRecord struct {
	UserID    string
	DeviceID  string
	FileID    string
	Name      string
	Album     string
	MimeType  string
	FilePath  string
	FileSize  int64
	ItemID    string
	CreatedAt time.Time
}

// MaskAPIKey returns a safe display mask for an API key token without exposing the full secret.
func MaskAPIKey(token string) string {
	t := strings.TrimSpace(token)
	if len(t) <= 8 {
		return "********"
	}
	return t[:4] + "••••••••" + t[len(t)-4:]
}

type DisplayPreferencesRecord struct {
	ID        string
	UserID    string
	Client    string
	DataJSON  string
	UpdatedAt time.Time
}

type ActivityLogRecord struct {
	ID            int64
	Name          string
	Overview      string
	ShortOverview string
	Type          string
	ItemID        string
	UserID        string
	Severity      string
	CreatedAt     time.Time
}

// DefaultUserPolicy returns a sensible Emby-compatible default UserPolicy.
func DefaultUserPolicy(isAdmin bool) dto.UserPolicy {
	return dto.UserPolicy{
		IsAdministrator:                isAdmin,
		IsHidden:                       false,
		IsDisabled:                     false,
		EnableAllFolders:               true,
		EnabledFolders:                 []string{},
		EnableMediaPlayback:            true,
		EnableAudioPlaybackTranscoding: true,
		EnableVideoPlaybackTranscoding: true,
		EnablePlaybackRemuxing:         true,
		EnableContentDeletion:          isAdmin,
		EnableContentDownloading:       true,
		EnableRemoteAccess:             true,
		EnableLiveTvAccess:             true,
		EnableLiveTvManagement:         isAdmin,
		BlockedTags:                    []string{},
		AuthenticationProviderId:       "Ambra.LocalAuthenticationProvider",
	}
}

// DefaultUserConfig returns a sensible Emby-compatible default UserConfiguration.
func DefaultUserConfig() dto.UserConfiguration {
	return dto.UserConfiguration{
		PlayDefaultAudioTrack:      true,
		SubtitleMode:               "Default",
		EnableLocalPassword:        true,
		OrderedViews:               []string{},
		LatestItemsExcludes:        []string{},
		MyMediaExcludes:            []string{},
		HidePlayedInLatest:         false,
		RememberAudioSelections:    true,
		RememberSubtitleSelections: true,
		EnableNextEpisodeAutoPlay:  true,
	}
}

type UserRepo struct {
	store *Store
}

func NewUserRepo(store *Store) *UserRepo {
	return &UserRepo{store: store}
}

// Store returns the underlying SQLite store.
func (r *UserRepo) Store() *Store {
	if r == nil {
		return nil
	}
	return r.store
}

func (r *UserRepo) CountUsers(ctx context.Context) (int, error) {
	var count int
	err := r.store.Reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count)
	return count, err
}

func (r *UserRepo) CreateUser(ctx context.Context, u *UserRecord) error {
	if u.ID == "" {
		u.ID = guid.New()
	} else {
		u.ID = guid.Normalize(u.ID)
	}
	if u.CreatedAt.IsZero() {
		u.CreatedAt = time.Now().UTC()
	}
	u.Policy.IsAdministrator = u.IsAdmin
	u.Policy.IsDisabled = u.IsDisabled
	if u.Policy.EnabledFolders == nil {
		u.Policy.EnabledFolders = []string{}
	}
	if u.Policy.BlockedTags == nil {
		u.Policy.BlockedTags = []string{}
	}
	if u.Config.OrderedViews == nil {
		u.Config.OrderedViews = []string{}
	}
	if u.Config.LatestItemsExcludes == nil {
		u.Config.LatestItemsExcludes = []string{}
	}
	if u.Config.MyMediaExcludes == nil {
		u.Config.MyMediaExcludes = []string{}
	}

	policyJSON, err := json.Marshal(u.Policy)
	if err != nil {
		return fmt.Errorf("marshal policy: %w", err)
	}
	configJSON, err := json.Marshal(u.Config)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	_, err = r.store.Writer.ExecContext(ctx, `
		INSERT INTO users (id, name, password_hash, is_admin, is_disabled, policy_json, config_json, last_login_at, last_active_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		u.ID, u.Name, u.PasswordHash, boolToInt(u.IsAdmin), boolToInt(u.IsDisabled),
		string(policyJSON), string(configJSON),
		nullableUnix(u.LastLoginAt), nullableUnix(u.LastActiveAt), u.CreatedAt.Unix(),
	)
	if err != nil {
		if isUniqueConstraintErr(err) {
			return ErrUserExists
		}
		return err
	}
	return nil
}

func isUniqueConstraintErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint failed") || strings.Contains(msg, "constraint failed: unique")
}

func (r *UserRepo) GetUserByID(ctx context.Context, id string) (*UserRecord, error) {
	norm := guid.Normalize(id)
	row := r.store.Reader.QueryRowContext(ctx, `
		SELECT id, name, password_hash, is_admin, is_disabled, policy_json, config_json, last_login_at, last_active_at, created_at
		FROM users WHERE id = ? OR id = ? LIMIT 1`, norm, id)
	return scanUser(row)
}

func (r *UserRepo) GetUserByName(ctx context.Context, name string) (*UserRecord, error) {
	row := r.store.Reader.QueryRowContext(ctx, `
		SELECT id, name, password_hash, is_admin, is_disabled, policy_json, config_json, last_login_at, last_active_at, created_at
		FROM users WHERE LOWER(name) = LOWER(?) LIMIT 1`, name)
	return scanUser(row)
}

func (r *UserRepo) ListUsers(ctx context.Context) ([]*UserRecord, error) {
	rows, err := r.store.Reader.QueryContext(ctx, `
		SELECT id, name, password_hash, is_admin, is_disabled, policy_json, config_json, last_login_at, last_active_at, created_at
		FROM users ORDER BY created_at ASC, name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*UserRecord
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (r *UserRepo) ListPublicUsers(ctx context.Context) ([]*UserRecord, error) {
	all, err := r.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	var out []*UserRecord
	for _, u := range all {
		if !u.IsDisabled && !u.Policy.IsHidden {
			out = append(out, u)
		}
	}
	return out, nil
}

func (r *UserRepo) UpdateUserPassword(ctx context.Context, id, passwordHash string) error {
	res, err := r.store.Writer.ExecContext(ctx,
		`UPDATE users SET password_hash = ? WHERE id = ? OR id = ?`,
		passwordHash, guid.Normalize(id), id,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *UserRepo) UpdateUserPolicy(ctx context.Context, id string, policy dto.UserPolicy) error {
	if policy.EnabledFolders == nil {
		policy.EnabledFolders = []string{}
	}
	if policy.BlockedTags == nil {
		policy.BlockedTags = []string{}
	}
	raw, err := json.Marshal(policy)
	if err != nil {
		return fmt.Errorf("marshal policy: %w", err)
	}
	res, err := r.store.Writer.ExecContext(ctx,
		`UPDATE users SET is_admin = ?, is_disabled = ?, policy_json = ? WHERE id = ? OR id = ?`,
		boolToInt(policy.IsAdministrator), boolToInt(policy.IsDisabled), string(raw), guid.Normalize(id), id,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *UserRepo) UpdateUserConfig(ctx context.Context, id string, cfg dto.UserConfiguration) error {
	if cfg.OrderedViews == nil {
		cfg.OrderedViews = []string{}
	}
	if cfg.LatestItemsExcludes == nil {
		cfg.LatestItemsExcludes = []string{}
	}
	if cfg.MyMediaExcludes == nil {
		cfg.MyMediaExcludes = []string{}
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	res, err := r.store.Writer.ExecContext(ctx,
		`UPDATE users SET config_json = ? WHERE id = ? OR id = ?`,
		string(raw), guid.Normalize(id), id,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *UserRepo) UpdateLastLogin(ctx context.Context, id string, at time.Time) error {
	ts := at.UTC().Unix()
	_, err := r.store.Writer.ExecContext(ctx,
		`UPDATE users SET last_login_at = ?, last_active_at = ? WHERE id = ? OR id = ?`,
		ts, ts, guid.Normalize(id), id,
	)
	return err
}

func (r *UserRepo) UpdateLastActive(ctx context.Context, id string, at time.Time) error {
	_, err := r.store.Writer.ExecContext(ctx,
		`UPDATE users SET last_active_at = ? WHERE id = ? OR id = ?`,
		at.UTC().Unix(), guid.Normalize(id), id,
	)
	return err
}

// ErrLastAdmin is returned when an operation would delete, disable, or demote
// the last active administrator account.
var ErrLastAdmin = errors.New("cannot remove or disable the last active administrator")

// ErrUserExists is returned when inserting a user violates the case-insensitive unique username constraint.
var ErrUserExists = errors.New("user already exists")

func purgeUserScopedStateTx(ctx context.Context, tx *sql.Tx, norm, rawID string) {
	_, _ = tx.ExecContext(ctx, `DELETE FROM display_preferences WHERE user_id = ? OR user_id = ?`, norm, rawID)
	_, _ = tx.ExecContext(ctx, `DELETE FROM camera_uploads WHERE user_id = ? OR user_id = ?`, norm, rawID)
	for _, uid := range []string{norm, rawID} {
		if strings.TrimSpace(uid) == "" {
			continue
		}
		exactKeys := []string{
			"user_settings:" + uid,
			"user_home_sections:" + uid,
			"user_searched_items:" + uid,
			"user_connect_link:" + uid,
			"party:last_leave:" + uid,
		}
		for _, k := range exactKeys {
			_, _ = tx.ExecContext(ctx, `DELETE FROM server_config WHERE key = ?`, k)
		}
		likePrefixes := []string{
			"user_typed_settings:" + uid + ":%",
			"user_rating:" + uid + ":%",
			"user_hide_resume:" + uid + ":%",
		}
		for _, p := range likePrefixes {
			_, _ = tx.ExecContext(ctx, `DELETE FROM server_config WHERE key LIKE ?`, p)
		}
	}
}

func (r *UserRepo) DeleteUser(ctx context.Context, id string) error {
	norm := guid.Normalize(id)
	tx, err := r.store.Writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	purgeUserScopedStateTx(ctx, tx, norm, id)
	res, err := tx.ExecContext(ctx, `DELETE FROM users WHERE id = ? OR id = ?`, norm, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

// DeleteUserSafe atomically verifies that deleting id will not leave zero active
// administrators, cleans up user-owned display_preferences, and deletes the user.
func (r *UserRepo) DeleteUserSafe(ctx context.Context, id string) error {
	norm := guid.Normalize(id)
	tx, err := r.store.Writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var targetID string
	var isAdmin, isDisabled int
	err = tx.QueryRowContext(ctx,
		`SELECT id, is_admin, is_disabled FROM users WHERE id = ? OR id = ? LIMIT 1`,
		norm, id,
	).Scan(&targetID, &isAdmin, &isDisabled)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}

	if isAdmin != 0 && isDisabled == 0 {
		var activeAdmins int
		if err := tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM users WHERE is_admin = 1 AND is_disabled = 0`,
		).Scan(&activeAdmins); err != nil {
			return err
		}
		if activeAdmins <= 1 {
			return ErrLastAdmin
		}
	}

	purgeUserScopedStateTx(ctx, tx, norm, targetID)
	res, err := tx.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, targetID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

// UpdateUserPolicySafe atomically updates a user's policy while refusing to
// demote or disable the last active administrator.
func (r *UserRepo) UpdateUserPolicySafe(ctx context.Context, id string, policy dto.UserPolicy) error {
	if policy.EnabledFolders == nil {
		policy.EnabledFolders = []string{}
	}
	if policy.BlockedTags == nil {
		policy.BlockedTags = []string{}
	}
	raw, err := json.Marshal(policy)
	if err != nil {
		return fmt.Errorf("marshal policy: %w", err)
	}

	norm := guid.Normalize(id)
	tx, err := r.store.Writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var targetID string
	var curAdmin, curDisabled int
	err = tx.QueryRowContext(ctx,
		`SELECT id, is_admin, is_disabled FROM users WHERE id = ? OR id = ? LIMIT 1`,
		norm, id,
	).Scan(&targetID, &curAdmin, &curDisabled)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}

	wasActiveAdmin := curAdmin != 0 && curDisabled == 0
	willBeActiveAdmin := policy.IsAdministrator && !policy.IsDisabled
	if wasActiveAdmin && !willBeActiveAdmin {
		var activeAdmins int
		if err := tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM users WHERE is_admin = 1 AND is_disabled = 0`,
		).Scan(&activeAdmins); err != nil {
			return err
		}
		if activeAdmins <= 1 {
			return ErrLastAdmin
		}
	}

	res, err := tx.ExecContext(ctx,
		`UPDATE users SET is_admin = ?, is_disabled = ?, policy_json = ? WHERE id = ?`,
		boolToInt(policy.IsAdministrator), boolToInt(policy.IsDisabled), string(raw), targetID,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

// --- Access Tokens & Devices ---

func (r *UserRepo) CreateAccessToken(ctx context.Context, t *AccessTokenRecord) error {
	now := time.Now().UTC()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	if t.LastActiveAt.IsZero() {
		t.LastActiveAt = now
	}
	_, err := r.store.Writer.ExecContext(ctx, `
		INSERT INTO access_tokens (token, user_id, device_id, device_name, app_name, app_version, created_at, last_active_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		t.Token, guid.Normalize(t.UserID), t.DeviceID, t.DeviceName, t.AppName, t.AppVersion,
		t.CreatedAt.Unix(), t.LastActiveAt.Unix(),
	)
	return err
}

func (r *UserRepo) GetAccessToken(ctx context.Context, token string) (*AccessTokenRecord, error) {
	var rec AccessTokenRecord
	var createdAt, lastActiveAt int64
	err := r.store.Reader.QueryRowContext(ctx, `
		SELECT token, user_id, device_id, device_name, app_name, app_version, created_at, last_active_at
		FROM access_tokens WHERE token = ?`, token,
	).Scan(&rec.Token, &rec.UserID, &rec.DeviceID, &rec.DeviceName, &rec.AppName, &rec.AppVersion, &createdAt, &lastActiveAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rec.CreatedAt = time.Unix(createdAt, 0).UTC()
	rec.LastActiveAt = time.Unix(lastActiveAt, 0).UTC()
	return &rec, nil
}

func (r *UserRepo) TouchAccessToken(ctx context.Context, token string, at time.Time) error {
	_, err := r.store.Writer.ExecContext(ctx,
		`UPDATE access_tokens SET last_active_at = ? WHERE token = ?`,
		at.UTC().Unix(), token,
	)
	return err
}

func (r *UserRepo) DeleteAccessToken(ctx context.Context, token string) error {
	_, err := r.store.Writer.ExecContext(ctx, `DELETE FROM access_tokens WHERE token = ?`, token)
	return err
}

func (r *UserRepo) DeleteTokensByDevice(ctx context.Context, deviceID string) error {
	_, err := r.store.Writer.ExecContext(ctx, `DELETE FROM access_tokens WHERE device_id = ?`, deviceID)
	return err
}

func (r *UserRepo) DeleteTokensByUser(ctx context.Context, userID string) error {
	_, err := r.store.Writer.ExecContext(ctx, `DELETE FROM access_tokens WHERE user_id = ?`, guid.Normalize(userID))
	return err
}

func (r *UserRepo) DeleteTokensByUserExcept(ctx context.Context, userID, keepToken string) error {
	keepToken = strings.TrimSpace(keepToken)
	if keepToken == "" {
		return r.DeleteTokensByUser(ctx, userID)
	}
	_, err := r.store.Writer.ExecContext(ctx, `DELETE FROM access_tokens WHERE user_id = ? AND token != ?`, guid.Normalize(userID), keepToken)
	return err
}

func (r *UserRepo) ListDevices(ctx context.Context) ([]DeviceRecord, error) {
	rows, err := r.store.Reader.QueryContext(ctx, `
		SELECT t.device_id, t.device_name, t.app_name, t.app_version, t.user_id, COALESCE(u.name, ''), t.last_active_at
		FROM access_tokens t
		LEFT JOIN users u ON u.id = t.user_id
		WHERE t.rowid IN (
			SELECT MAX(rowid) FROM access_tokens GROUP BY device_id
		)
		ORDER BY t.last_active_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []DeviceRecord
	for rows.Next() {
		var d DeviceRecord
		var ts int64
		if err := rows.Scan(&d.ID, &d.Name, &d.AppName, &d.AppVersion, &d.LastUserID, &d.LastUserName, &ts); err != nil {
			return nil, err
		}
		d.DateLastActivity = time.Unix(ts, 0).UTC()
		out = append(out, d)
	}
	return out, rows.Err()
}

// --- API Keys ---

func (r *UserRepo) CreateAPIKey(ctx context.Context, k *APIKeyRecord) error {
	if k.CreatedAt.IsZero() {
		k.CreatedAt = time.Now().UTC()
	}
	if strings.TrimSpace(k.KeyID) == "" {
		k.KeyID = guid.New()
	}
	_, err := r.store.Writer.ExecContext(ctx,
		`INSERT INTO api_keys (token, app_name, created_at, key_id) VALUES (?, ?, ?, ?)`,
		k.Token, k.AppName, k.CreatedAt.Unix(), k.KeyID,
	)
	return err
}

func (r *UserRepo) GetAPIKey(ctx context.Context, token string) (*APIKeyRecord, error) {
	var rec APIKeyRecord
	var createdAt int64
	err := r.store.Reader.QueryRowContext(ctx,
		`SELECT COALESCE(key_id, ''), token, app_name, created_at FROM api_keys WHERE token = ?`, token,
	).Scan(&rec.KeyID, &rec.Token, &rec.AppName, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rec.CreatedAt = time.Unix(createdAt, 0).UTC()
	return &rec, nil
}

func (r *UserRepo) ListAPIKeys(ctx context.Context) ([]APIKeyRecord, error) {
	rows, err := r.store.Reader.QueryContext(ctx,
		`SELECT COALESCE(key_id, ''), token, app_name, created_at FROM api_keys ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []APIKeyRecord
	for rows.Next() {
		var rec APIKeyRecord
		var ts int64
		if err := rows.Scan(&rec.KeyID, &rec.Token, &rec.AppName, &ts); err != nil {
			return nil, err
		}
		rec.CreatedAt = time.Unix(ts, 0).UTC()
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (r *UserRepo) DeleteAPIKey(ctx context.Context, token string) error {
	_, err := r.DeleteAPIKeyByIDOrToken(ctx, token)
	return err
}

// DeleteAPIKeyByIDOrToken deletes an API key matching either its KeyID or raw Token,
// returning the deleted raw Token (empty if nothing matched) so callers can evict caches/WebSockets.
func (r *UserRepo) DeleteAPIKeyByIDOrToken(ctx context.Context, idOrToken string) (string, error) {
	key := strings.TrimSpace(idOrToken)
	if key == "" {
		return "", nil
	}
	tx, err := r.store.Writer.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()

	var rawToken string
	err = tx.QueryRowContext(ctx,
		`SELECT token FROM api_keys WHERE token = ? OR key_id = ? LIMIT 1`,
		key, key,
	).Scan(&rawToken)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM api_keys WHERE token = ?`, rawToken); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return rawToken, nil
}

// --- Camera Uploads ---

func (r *UserRepo) UpsertCameraUpload(ctx context.Context, rec *CameraUploadRecord) error {
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = time.Now().UTC()
	}
	_, err := r.store.Writer.ExecContext(ctx, `
		INSERT INTO camera_uploads (user_id, device_id, file_id, name, album, mime_type, file_path, file_size, item_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, device_id, file_id) DO UPDATE SET
			name = excluded.name,
			album = excluded.album,
			mime_type = excluded.mime_type,
			file_path = excluded.file_path,
			file_size = excluded.file_size,
			item_id = excluded.item_id,
			created_at = excluded.created_at`,
		guid.Normalize(rec.UserID), strings.TrimSpace(rec.DeviceID), strings.TrimSpace(rec.FileID),
		rec.Name, rec.Album, rec.MimeType, rec.FilePath, rec.FileSize, rec.ItemID, rec.CreatedAt.Unix(),
	)
	return err
}

func (r *UserRepo) ListCameraUploads(ctx context.Context, userID, deviceID string) ([]CameraUploadRecord, error) {
	rows, err := r.store.Reader.QueryContext(ctx, `
		SELECT user_id, device_id, file_id, name, album, mime_type, file_path, file_size, item_id, created_at
		FROM camera_uploads
		WHERE user_id = ? AND device_id = ?
		ORDER BY created_at DESC`,
		guid.Normalize(userID), strings.TrimSpace(deviceID),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []CameraUploadRecord
	for rows.Next() {
		var rec CameraUploadRecord
		var ts int64
		if err := rows.Scan(
			&rec.UserID, &rec.DeviceID, &rec.FileID, &rec.Name, &rec.Album,
			&rec.MimeType, &rec.FilePath, &rec.FileSize, &rec.ItemID, &ts,
		); err != nil {
			return nil, err
		}
		rec.CreatedAt = time.Unix(ts, 0).UTC()
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (r *UserRepo) DeleteDeviceArtifacts(ctx context.Context, deviceID string) error {
	dev := strings.TrimSpace(deviceID)
	if dev == "" {
		return nil
	}
	tx, err := r.store.Writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	_, _ = tx.ExecContext(ctx, `DELETE FROM camera_uploads WHERE device_id = ?`, dev)
	_, _ = tx.ExecContext(ctx, `DELETE FROM server_config WHERE key = ? OR key = ?`, "device_options:"+dev, "device_camera_uploads:"+dev)
	return tx.Commit()
}

// --- Display Preferences ---

func (r *UserRepo) GetDisplayPreferences(ctx context.Context, id, userID, client string) (*DisplayPreferencesRecord, error) {
	var rec DisplayPreferencesRecord
	var ts int64
	err := r.store.Reader.QueryRowContext(ctx, `
		SELECT id, user_id, client, data_json, updated_at
		FROM display_preferences
		WHERE id = ? AND user_id = ? AND client = ?`,
		id, guid.Normalize(userID), client,
	).Scan(&rec.ID, &rec.UserID, &rec.Client, &rec.DataJSON, &ts)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rec.UpdatedAt = time.Unix(ts, 0).UTC()
	return &rec, nil
}

func (r *UserRepo) UpsertDisplayPreferences(ctx context.Context, rec *DisplayPreferencesRecord) error {
	if rec.UpdatedAt.IsZero() {
		rec.UpdatedAt = time.Now().UTC()
	}
	if rec.DataJSON == "" {
		rec.DataJSON = "{}"
	}
	_, err := r.store.Writer.ExecContext(ctx, `
		INSERT INTO display_preferences (id, user_id, client, data_json, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(id, user_id, client) DO UPDATE SET
			data_json = excluded.data_json,
			updated_at = excluded.updated_at`,
		rec.ID, guid.Normalize(rec.UserID), rec.Client, rec.DataJSON, rec.UpdatedAt.Unix(),
	)
	return err
}

// --- Activity Logs ---

func (r *UserRepo) CreateActivityLog(ctx context.Context, entry *ActivityLogRecord) error {
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	if entry.Severity == "" {
		entry.Severity = "Info"
	}
	res, err := r.store.Writer.ExecContext(ctx, `
		INSERT INTO activity_logs (name, overview, short_overview, type, item_id, user_id, severity, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.Name, nullableString(entry.Overview), nullableString(entry.ShortOverview),
		entry.Type, nullableString(entry.ItemID), nullableString(entry.UserID),
		entry.Severity, entry.CreatedAt.Unix(),
	)
	if err != nil {
		return err
	}
	if id, err := res.LastInsertId(); err == nil {
		entry.ID = id
	}
	return nil
}

func (r *UserRepo) ListActivityLogs(ctx context.Context, startIndex, limit int, minDate *time.Time) ([]ActivityLogRecord, int, error) {
	return r.ListActivityLogsFiltered(ctx, startIndex, limit, minDate, "", "")
}

func (r *UserRepo) ListActivityLogsFiltered(ctx context.Context, startIndex, limit int, minDate *time.Time, severity, searchTerm string) ([]ActivityLogRecord, int, error) {
	if startIndex < 0 {
		startIndex = 0
	}
	if limit <= 0 {
		limit = 50
	}

	var clauses []string
	var args []any
	if minDate != nil {
		clauses = append(clauses, `created_at >= ?`)
		args = append(args, minDate.Unix())
	}
	if sev := strings.TrimSpace(severity); sev != "" && !strings.EqualFold(sev, "all") {
		clauses = append(clauses, `LOWER(severity) = LOWER(?)`)
		args = append(args, sev)
	}
	if q := strings.TrimSpace(searchTerm); q != "" {
		esc := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(strings.ToLower(q))
		pat := "%" + esc + "%"
		clauses = append(clauses, `(LOWER(name) LIKE ? ESCAPE '\' OR LOWER(COALESCE(overview,'')) LIKE ? ESCAPE '\' OR LOWER(COALESCE(short_overview,'')) LIKE ? ESCAPE '\' OR LOWER(type) LIKE ? ESCAPE '\')`)
		args = append(args, pat, pat, pat, pat)
	}

	whereSQL := ""
	if len(clauses) > 0 {
		whereSQL = " WHERE " + strings.Join(clauses, " AND ")
	}

	var total int
	if err := r.store.Reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM activity_logs`+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	queryArgs := append(append([]any(nil), args...), limit, startIndex)
	rows, err := r.store.Reader.QueryContext(ctx, `
		SELECT id, name, COALESCE(overview,''), COALESCE(short_overview,''), type, COALESCE(item_id,''), COALESCE(user_id,''), severity, created_at
		FROM activity_logs`+whereSQL+`
		ORDER BY created_at DESC, id DESC
		LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []ActivityLogRecord
	for rows.Next() {
		var rec ActivityLogRecord
		var ts int64
		if err := rows.Scan(&rec.ID, &rec.Name, &rec.Overview, &rec.ShortOverview, &rec.Type, &rec.ItemID, &rec.UserID, &rec.Severity, &ts); err != nil {
			return nil, 0, err
		}
		rec.CreatedAt = time.Unix(ts, 0).UTC()
		out = append(out, rec)
	}
	return out, total, rows.Err()
}

// --- Server Config Key/Value ---

func (r *UserRepo) GetServerConfig(ctx context.Context, key string) (string, error) {
	var val string
	err := r.store.Reader.QueryRowContext(ctx,
		`SELECT value_json FROM server_config WHERE key = ?`, key,
	).Scan(&val)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return val, err
}

func (r *UserRepo) SetServerConfig(ctx context.Context, key, valueJSON string) error {
	_, err := r.store.Writer.ExecContext(ctx, `
		INSERT INTO server_config (key, value_json, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			value_json = excluded.value_json,
			updated_at = excluded.updated_at`,
		key, valueJSON, time.Now().UTC().Unix(),
	)
	return err
}

// --- Playback Progress (user_item_data) ---

func (r *UserRepo) GetItemRuntimeTicks(ctx context.Context, itemID string) (int64, error) {
	var ticks sql.NullInt64
	err := r.store.Reader.QueryRowContext(ctx,
		`SELECT runtime_ticks FROM items WHERE id = ? OR id = ? LIMIT 1`,
		guid.Normalize(itemID), itemID,
	).Scan(&ticks)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	if !ticks.Valid {
		return 0, nil
	}
	return ticks.Int64, nil
}

func (r *UserRepo) UpsertPlaybackProgress(
	ctx context.Context,
	userID, itemID string,
	positionTicks int64,
	markPlayed bool,
	audioStreamIndex, subtitleStreamIndex *int,
) (*UserItemDataRecord, error) {
	uid := guid.Normalize(userID)
	iid := guid.Normalize(itemID)
	now := time.Now().UTC()
	nowUnix := now.Unix()

	if markPlayed {
		positionTicks = 0
		_, err := r.store.Writer.ExecContext(ctx, `
			INSERT INTO user_item_data (
				user_id, item_id, played, play_count, playback_position_ticks,
				last_played_date, audio_stream_index, subtitle_stream_index
			) VALUES (?, ?, 1, 1, 0, ?, ?, ?)
			ON CONFLICT(user_id, item_id) DO UPDATE SET
				played = 1,
				play_count = user_item_data.play_count + 1,
				playback_position_ticks = 0,
				last_played_date = excluded.last_played_date,
				audio_stream_index = COALESCE(excluded.audio_stream_index, user_item_data.audio_stream_index),
				subtitle_stream_index = COALESCE(excluded.subtitle_stream_index, user_item_data.subtitle_stream_index)`,
			uid, iid, nowUnix, nullableInt(audioStreamIndex), nullableInt(subtitleStreamIndex),
		)
		if err != nil {
			return nil, err
		}
	} else {
		_, err := r.store.Writer.ExecContext(ctx, `
			INSERT INTO user_item_data (
				user_id, item_id, played, play_count, playback_position_ticks,
				last_played_date, audio_stream_index, subtitle_stream_index
			) VALUES (?, ?, 0, 0, ?, ?, ?, ?)
			ON CONFLICT(user_id, item_id) DO UPDATE SET
				playback_position_ticks = excluded.playback_position_ticks,
				last_played_date = excluded.last_played_date,
				audio_stream_index = COALESCE(excluded.audio_stream_index, user_item_data.audio_stream_index),
				subtitle_stream_index = COALESCE(excluded.subtitle_stream_index, user_item_data.subtitle_stream_index)`,
			uid, iid, positionTicks, nowUnix, nullableInt(audioStreamIndex), nullableInt(subtitleStreamIndex),
		)
		if err != nil {
			return nil, err
		}
		if positionTicks > 0 {
			hideKey := fmt.Sprintf("user_hide_resume:%s:%s", uid, iid)
			_, _ = r.store.Writer.ExecContext(ctx, `DELETE FROM server_config WHERE key = ?`, hideKey)
		}
	}

	return r.GetUserItemData(ctx, uid, iid)
}

func (r *UserRepo) SetUserItemLikes(ctx context.Context, userID, itemID string, likes *bool) (*UserItemDataRecord, error) {
	uid := guid.Normalize(userID)
	iid := guid.Normalize(itemID)
	var likesVal any
	if likes != nil {
		if *likes {
			likesVal = 1
		} else {
			likesVal = 0
		}
	}
	_, err := r.store.Writer.ExecContext(ctx, `
		INSERT INTO user_item_data (user_id, item_id, likes)
		VALUES (?, ?, ?)
		ON CONFLICT(user_id, item_id) DO UPDATE SET
			likes = excluded.likes`,
		uid, iid, likesVal,
	)
	if err != nil {
		return nil, err
	}
	return r.GetUserItemData(ctx, uid, iid)
}

func (r *UserRepo) GetUserItemData(ctx context.Context, userID, itemID string) (*UserItemDataRecord, error) {
	var rec UserItemDataRecord
	var played, fav int
	var likes, lastPlayed, audioIdx, subIdx sql.NullInt64
	err := r.store.Reader.QueryRowContext(ctx, `
		SELECT user_id, item_id, played, play_count, playback_position_ticks, is_favorite, likes, last_played_date, audio_stream_index, subtitle_stream_index
		FROM user_item_data
		WHERE user_id = ? AND item_id = ?`,
		guid.Normalize(userID), guid.Normalize(itemID),
	).Scan(&rec.UserID, &rec.ItemID, &played, &rec.PlayCount, &rec.PlaybackPositionTicks, &fav, &likes, &lastPlayed, &audioIdx, &subIdx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rec.Played = played != 0
	rec.IsFavorite = fav != 0
	if likes.Valid {
		v := likes.Int64 != 0
		rec.Likes = &v
	}
	if lastPlayed.Valid {
		ts := lastPlayed.Int64
		rec.LastPlayedDate = &ts
	}
	if audioIdx.Valid {
		v := int(audioIdx.Int64)
		rec.AudioStreamIndex = &v
	}
	if subIdx.Valid {
		v := int(subIdx.Int64)
		rec.SubtitleStreamIndex = &v
	}
	return &rec, nil
}

// --- Helpers ---

type userRowScanner interface {
	Scan(dest ...any) error
}

func scanUser(row userRowScanner) (*UserRecord, error) {
	var u UserRecord
	var isAdmin, isDisabled int
	var policyJSON, configJSON string
	var lastLogin, lastActive sql.NullInt64
	var createdAt int64

	err := row.Scan(
		&u.ID, &u.Name, &u.PasswordHash, &isAdmin, &isDisabled,
		&policyJSON, &configJSON, &lastLogin, &lastActive, &createdAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	u.IsAdmin = isAdmin != 0
	u.IsDisabled = isDisabled != 0
	u.CreatedAt = time.Unix(createdAt, 0).UTC()
	if lastLogin.Valid {
		t := time.Unix(lastLogin.Int64, 0).UTC()
		u.LastLoginAt = &t
	}
	if lastActive.Valid {
		t := time.Unix(lastActive.Int64, 0).UTC()
		u.LastActiveAt = &t
	}

	u.Policy = DefaultUserPolicy(u.IsAdmin)
	if policyJSON != "" && policyJSON != "{}" {
		_ = json.Unmarshal([]byte(policyJSON), &u.Policy)
	}
	u.Policy.IsAdministrator = u.IsAdmin
	u.Policy.IsDisabled = u.IsDisabled
	if u.Policy.EnabledFolders == nil {
		u.Policy.EnabledFolders = []string{}
	}
	if u.Policy.BlockedTags == nil {
		u.Policy.BlockedTags = []string{}
	}

	u.Config = DefaultUserConfig()
	if configJSON != "" && configJSON != "{}" {
		_ = json.Unmarshal([]byte(configJSON), &u.Config)
	}
	if u.Config.OrderedViews == nil {
		u.Config.OrderedViews = []string{}
	}
	if u.Config.LatestItemsExcludes == nil {
		u.Config.LatestItemsExcludes = []string{}
	}
	if u.Config.MyMediaExcludes == nil {
		u.Config.MyMediaExcludes = []string{}
	}

	return &u, nil
}

func nullableUnix(t *time.Time) any {
	if t == nil || t.IsZero() {
		return nil
	}
	return t.UTC().Unix()
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullableInt(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}
