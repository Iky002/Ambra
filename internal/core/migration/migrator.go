package migration

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

const (
	StatusIdle      = "idle"
	StatusRunning   = "running"
	StatusCompleted = "completed"
	StatusCancelled = "cancelled"
	StatusFailed    = "failed"

	defaultBatchSize = 500
	defaultPageSize  = 200
	checkpointPrefix = "migration:checkpoint:"
)

// ErrMigrationRunning is returned when starting a migration while another is already active.
var ErrMigrationRunning = errors.New("a migration task is already running")

// MigrationRequest configures a dry-run or live migration from a legacy Emby server.
type MigrationRequest struct {
	SourceURL        string   `json:"source_url"`
	APIKey           string   `json:"api_key"`
	UserIDs          []string `json:"user_ids,omitempty"`
	MigrateUsers     bool     `json:"migrate_users"`
	MigratePlayed    bool     `json:"migrate_played"`
	MigrateFavorites bool     `json:"migrate_favorites"`
	MigrateResume    bool     `json:"migrate_resume"`
	DryRun           bool     `json:"dry_run"`
	BatchSize        int      `json:"batch_size,omitempty"`
	ResetCheckpoint  bool     `json:"reset_checkpoint,omitempty"`
}

// ConflictEntry describes a matching or user conflict detected during migration.
type ConflictEntry struct {
	UserName     string `json:"user_name,omitempty"`
	SourceItemID string `json:"source_item_id,omitempty"`
	ItemName     string `json:"item_name,omitempty"`
	ItemType     string `json:"item_type,omitempty"`
	Reason       string `json:"reason"`
}

// MigrationReport summarizes progress and final statistics of a migration run.
type MigrationReport struct {
	Status                string          `json:"status"`
	DryRun                bool            `json:"dry_run"`
	SourceURL             string          `json:"source_url,omitempty"`
	SourceServerName      string          `json:"source_server_name,omitempty"`
	StartedAt             *time.Time      `json:"started_at,omitempty"`
	CompletedAt           *time.Time      `json:"completed_at,omitempty"`
	TotalUsers            int             `json:"total_users"`
	ProcessedUsers        int             `json:"processed_users"`
	CurrentUser           string          `json:"current_user,omitempty"`
	UsersMatched          int             `json:"users_matched"`
	UsersCreated          int             `json:"users_created"`
	ItemsScanned          int             `json:"items_scanned"`
	ItemsMatched          int             `json:"items_matched"`
	ItemsSkipped          int             `json:"items_skipped"`
	PlayedMigrated        int             `json:"played_migrated"`
	FavoritesMigrated     int             `json:"favorites_migrated"`
	ResumeMigrated        int             `json:"resume_migrated"`
	Conflicts             []ConflictEntry `json:"conflicts"`
	Errors                []string        `json:"errors"`
	Notices               []string        `json:"notices,omitempty"`
	ResumedFromCheckpoint bool            `json:"resumed_from_checkpoint"`
	CheckpointKey         string          `json:"checkpoint_key,omitempty"`
}

// SourceTestResult bundles server info, users, and virtual folders from a source Emby server.
type SourceTestResult struct {
	Server         *SourceServerInfo     `json:"server"`
	Users          []SourceUser          `json:"users"`
	VirtualFolders []SourceVirtualFolder `json:"virtual_folders"`
}

type checkpointState struct {
	CompletedUserIDs []string `json:"completed_user_ids"`
	CurrentUserID    string   `json:"current_user_id,omitempty"`
	NextStartIndex   int      `json:"next_start_index"`
	UpdatedAt        int64    `json:"updated_at"`
}

type pendingUserItemWrite struct {
	UserID         string
	ItemID         string
	Played         bool
	PlayCount      int
	PositionTicks  int64
	IsFavorite     bool
	LastPlayedUnix *int64
	ApplyPlayed    bool
	ApplyFavorite  bool
	ApplyResume    bool
}

// ClientFactory creates a SourceClient for a given source URL and API key.
type ClientFactory func(baseURL, apiKey string) (*SourceClient, error)

// MigratorOption customizes Migrator construction.
type MigratorOption func(*Migrator)

// WithClientFactory overrides SourceClient creation (useful for testing).
func WithClientFactory(fn ClientFactory) MigratorOption {
	return func(m *Migrator) {
		if fn != nil {
			m.clientFactory = fn
		}
	}
}

// WithPageSize sets the number of items requested per page from the source server.
func WithPageSize(size int) MigratorOption {
	return func(m *Migrator) {
		if size > 0 {
			m.pageSize = size
		}
	}
}

// Migrator orchestrates one-click migration of users, watched status, favorites,
// and resume positions from a legacy Emby server into Ambra.
type Migrator struct {
	store         *sqlite.Store
	userRepo      *sqlite.UserRepo
	clientFactory ClientFactory
	pageSize      int

	mu       sync.RWMutex
	running  bool
	cancelFn context.CancelFunc
	status   MigrationReport
}

// NewMigrator creates a new Migrator backed by the given SQLite store and UserRepo.
func NewMigrator(store *sqlite.Store, userRepo *sqlite.UserRepo, opts ...MigratorOption) *Migrator {
	if userRepo == nil && store != nil {
		userRepo = sqlite.NewUserRepo(store)
	}
	m := &Migrator{
		store:    store,
		userRepo: userRepo,
		clientFactory: func(baseURL, apiKey string) (*SourceClient, error) {
			return NewSourceClient(baseURL, apiKey)
		},
		pageSize: defaultPageSize,
		status: MigrationReport{
			Status:    StatusIdle,
			Conflicts: []ConflictEntry{},
			Errors:    []string{},
		},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(m)
		}
	}
	return m
}

// TestSource connects to the legacy Emby server and retrieves its server info, users, and libraries.
func (m *Migrator) TestSource(ctx context.Context, sourceURL, apiKey string) (*SourceTestResult, error) {
	client, err := m.clientFactory(sourceURL, apiKey)
	if err != nil {
		return nil, err
	}
	serverInfo, err := client.TestConnection(ctx)
	if err != nil {
		return nil, err
	}
	users, err := client.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	folders, err := client.ListVirtualFolders(ctx)
	if err != nil {
		return nil, err
	}
	return &SourceTestResult{
		Server:         serverInfo,
		Users:          users,
		VirtualFolders: folders,
	}, nil
}

// Status returns a snapshot of the current or most recent migration report.
func (m *Migrator) Status() MigrationReport {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneReport(m.status)
}

// Cancel requests cancellation of an active migration task.
// Returns true if a running migration was signalled to cancel.
func (m *Migrator) Cancel() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.running || m.cancelFn == nil {
		return false
	}
	m.cancelFn()
	return true
}

// StartAsync launches a live migration in the background and returns the initial running report.
func (m *Migrator) StartAsync(req MigrationRequest) (MigrationReport, error) {
	req.DryRun = false
	if m.store == nil {
		return MigrationReport{}, errors.New("sqlite store is required")
	}
	client, err := m.clientFactory(req.SourceURL, req.APIKey)
	if err != nil {
		return MigrationReport{}, err
	}

	runCtx, err := m.beginRun(context.Background(), req, client.BaseURL())
	if err != nil {
		return MigrationReport{}, err
	}

	initial := m.Status()
	go func() {
		_, _ = m.execute(runCtx, client, req)
	}()
	return initial, nil
}

// Run executes a dry-run preview or live migration synchronously.
func (m *Migrator) Run(ctx context.Context, req MigrationRequest) (*MigrationReport, error) {
	if m.store == nil {
		return nil, errors.New("sqlite store is required")
	}
	client, err := m.clientFactory(req.SourceURL, req.APIKey)
	if err != nil {
		return nil, err
	}

	runCtx, err := m.beginRun(ctx, req, client.BaseURL())
	if err != nil {
		return nil, err
	}
	return m.execute(runCtx, client, req)
}

func (m *Migrator) beginRun(parent context.Context, req MigrationRequest, normalizedURL string) (context.Context, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.running {
		return nil, ErrMigrationRunning
	}

	runCtx, cancel := context.WithCancel(parent)
	now := time.Now().UTC()
	m.running = true
	m.cancelFn = cancel
	m.status = MigrationReport{
		Status:        StatusRunning,
		DryRun:        req.DryRun,
		SourceURL:     normalizedURL,
		StartedAt:     &now,
		Conflicts:     []ConflictEntry{},
		Errors:        []string{},
		CheckpointKey: checkpointKeyFor(normalizedURL, req.UserIDs),
	}
	return runCtx, nil
}

func (m *Migrator) execute(ctx context.Context, client *SourceClient, req MigrationRequest) (*MigrationReport, error) {
	defer func() {
		m.mu.Lock()
		if m.cancelFn != nil {
			m.cancelFn()
		}
		m.running = false
		m.cancelFn = nil
		m.mu.Unlock()
	}()

	batchSize := req.BatchSize
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}

	report := m.Status()
	finish := func(status string, runErr error) (*MigrationReport, error) {
		now := time.Now().UTC()
		report.Status = status
		report.CompletedAt = &now
		report.CurrentUser = ""
		if runErr != nil && status == StatusFailed {
			report.Errors = append(report.Errors, runErr.Error())
		}
		m.setStatus(report)
		out := cloneReport(report)
		return &out, runErr
	}

	serverInfo, err := client.TestConnection(ctx)
	if err != nil {
		if errors.Is(err, context.Canceled) || ctx.Err() != nil {
			return finish(StatusCancelled, context.Canceled)
		}
		return finish(StatusFailed, fmt.Errorf("connect source server: %w", err))
	}
	report.SourceServerName = serverInfo.ServerName
	m.setStatus(report)

	sourceUsers, err := client.ListUsers(ctx)
	if err != nil {
		if errors.Is(err, context.Canceled) || ctx.Err() != nil {
			return finish(StatusCancelled, context.Canceled)
		}
		return finish(StatusFailed, fmt.Errorf("list source users: %w", err))
	}

	selectedUsers := filterSourceUsers(sourceUsers, req.UserIDs)
	report.TotalUsers = len(selectedUsers)
	m.setStatus(report)

	idx, err := m.loadLocalIndex(ctx)
	if err != nil {
		return finish(StatusFailed, fmt.Errorf("load local media index: %w", err))
	}

	cp := checkpointState{}
	completedSet := make(map[string]struct{})
	if !req.DryRun {
		if req.ResetCheckpoint {
			_ = m.clearCheckpoint(ctx, report.CheckpointKey)
		} else if loaded, ok := m.loadCheckpoint(ctx, report.CheckpointKey); ok {
			cp = loaded
			for _, uid := range cp.CompletedUserIDs {
				completedSet[uid] = struct{}{}
			}
			if len(cp.CompletedUserIDs) > 0 || cp.CurrentUserID != "" || cp.NextStartIndex > 0 {
				report.ResumedFromCheckpoint = true
			}
		}
	}

	needItemData := req.MigratePlayed || req.MigrateFavorites || req.MigrateResume

	for _, srcUser := range selectedUsers {
		if err := ctx.Err(); err != nil {
			return finish(StatusCancelled, err)
		}

		if !req.DryRun {
			if _, done := completedSet[srcUser.ID]; done {
				report.ProcessedUsers++
				m.setStatus(report)
				continue
			}
		}

		report.CurrentUser = srcUser.Name
		m.setStatus(report)

		targetUserID, matched, created, conflict, notice, err := m.resolveOrCreateUser(ctx, srcUser, req.MigrateUsers, req.DryRun)
		if err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("user %s: %v", srcUser.Name, err))
			report.ProcessedUsers++
			m.setStatus(report)
			continue
		}
		if notice != "" {
			report.Notices = append(report.Notices, notice)
		}
		if matched {
			report.UsersMatched++
		}
		if created {
			report.UsersCreated++
		}
		if conflict != nil {
			report.Conflicts = append(report.Conflicts, *conflict)
			report.ProcessedUsers++
			if !req.DryRun {
				cp.CompletedUserIDs = appendUnique(cp.CompletedUserIDs, srcUser.ID)
				cp.CurrentUserID = ""
				cp.NextStartIndex = 0
				_ = m.saveCheckpoint(ctx, report.CheckpointKey, cp)
			}
			m.setStatus(report)
			continue
		}

		if needItemData {
			startIndex := 0
			if !req.DryRun && cp.CurrentUserID == srcUser.ID && cp.NextStartIndex > 0 {
				startIndex = cp.NextStartIndex
			}

			var batch []pendingUserItemWrite
			flushBatch := func(nextCursor int) error {
				if len(batch) > 0 && !req.DryRun {
					if err := m.writeUserItemBatch(ctx, batch); err != nil {
						return err
					}
				}
				batch = batch[:0]
				if !req.DryRun {
					cp.CurrentUserID = srcUser.ID
					cp.NextStartIndex = nextCursor
					if err := m.saveCheckpoint(ctx, report.CheckpointKey, cp); err != nil {
						return err
					}
				}
				return nil
			}

			for {
				if err := ctx.Err(); err != nil {
					return finish(StatusCancelled, err)
				}

				page, err := client.FetchUserItemsPage(ctx, srcUser.ID, startIndex, m.pageSize)
				if err != nil {
					if errors.Is(err, context.Canceled) || ctx.Err() != nil {
						return finish(StatusCancelled, context.Canceled)
					}
					return finish(StatusFailed, fmt.Errorf("fetch items for user %s at %d: %w", srcUser.Name, startIndex, err))
				}

				if len(page.Items) == 0 {
					break
				}

				for itemIdx, item := range page.Items {
					if err := ctx.Err(); err != nil {
						return finish(StatusCancelled, err)
					}
					report.ItemsScanned++

					ud := item.UserData
					hasPlayed := req.MigratePlayed && ud != nil && (ud.Played || ud.PlayCount > 0)
					hasFav := req.MigrateFavorites && ud != nil && ud.IsFavorite
					hasResume := req.MigrateResume && ud != nil && ud.PlaybackPositionTicks > 0
					if !hasPlayed && !hasFav && !hasResume {
						report.ItemsSkipped++
						continue
					}

					localID, conflictReason := idx.match(item)
					if conflictReason != "" {
						report.ItemsSkipped++
						report.Conflicts = append(report.Conflicts, ConflictEntry{
							UserName:     srcUser.Name,
							SourceItemID: item.ID,
							ItemName:     item.Name,
							ItemType:     item.Type,
							Reason:       conflictReason,
						})
						continue
					}
					if localID == "" {
						report.ItemsSkipped++
						continue
					}

					report.ItemsMatched++
					if hasPlayed {
						report.PlayedMigrated++
					}
					if hasFav {
						report.FavoritesMigrated++
					}
					if hasResume {
						report.ResumeMigrated++
					}

					var lastPlayedUnix *int64
					if ud.LastPlayedDate != nil && !ud.LastPlayedDate.IsZero() {
						ts := ud.LastPlayedDate.UTC().Unix()
						lastPlayedUnix = &ts
					}
					playCount := ud.PlayCount
					if ud.Played && playCount == 0 {
						playCount = 1
					}

					batch = append(batch, pendingUserItemWrite{
						UserID:         targetUserID,
						ItemID:         localID,
						Played:         ud.Played,
						PlayCount:      playCount,
						PositionTicks:  ud.PlaybackPositionTicks,
						IsFavorite:     ud.IsFavorite,
						LastPlayedUnix: lastPlayedUnix,
						ApplyPlayed:    hasPlayed,
						ApplyFavorite:  hasFav,
						ApplyResume:    hasResume,
					})

					if len(batch) >= batchSize {
						if err := flushBatch(startIndex + itemIdx + 1); err != nil {
							return finish(StatusFailed, fmt.Errorf("flush batch for user %s: %w", srcUser.Name, err))
						}
					}
				}

				nextStart := startIndex + len(page.Items)
				if err := flushBatch(nextStart); err != nil {
					return finish(StatusFailed, fmt.Errorf("flush page for user %s: %w", srcUser.Name, err))
				}
				m.setStatus(report)

				if page.TotalRecordCount > 0 && nextStart >= page.TotalRecordCount {
					break
				}
				if len(page.Items) < m.pageSize && page.TotalRecordCount == 0 {
					break
				}
				startIndex = nextStart
			}
		}

		report.ProcessedUsers++
		if !req.DryRun {
			cp.CompletedUserIDs = appendUnique(cp.CompletedUserIDs, srcUser.ID)
			cp.CurrentUserID = ""
			cp.NextStartIndex = 0
			if err := m.saveCheckpoint(ctx, report.CheckpointKey, cp); err != nil {
				return finish(StatusFailed, fmt.Errorf("save checkpoint: %w", err))
			}
		}
		m.setStatus(report)
	}

	if !req.DryRun {
		_ = m.clearCheckpoint(ctx, report.CheckpointKey)
	}
	return finish(StatusCompleted, nil)
}

func (m *Migrator) resolveOrCreateUser(
	ctx context.Context,
	src SourceUser,
	migrateUsers bool,
	dryRun bool,
) (targetUserID string, matched bool, created bool, conflict *ConflictEntry, notice string, err error) {
	name := strings.TrimSpace(src.Name)
	if name == "" {
		return "", false, false, &ConflictEntry{
			SourceItemID: src.ID,
			Reason:       "source user has empty name",
		}, "", nil
	}

	existing, getErr := m.userRepo.GetUserByName(ctx, name)
	if getErr == nil && existing != nil {
		if migrateUsers && !dryRun {
			policy := src.Policy
			if policy.AuthenticationProviderId == "" {
				policy.AuthenticationProviderId = existing.Policy.AuthenticationProviderId
			}
			// Never auto-promote a non-admin local user to administrator during migration merge.
			if !existing.IsAdmin && policy.IsAdministrator {
				policy.IsAdministrator = false
				notice = fmt.Sprintf("user %q kept as non-administrator during merge (admin elevation skipped)", name)
			}
			// Protect existing local administrators from accidental demotion or lockout by source policy.
			if existing.IsAdmin {
				var protected []string
				if !policy.IsAdministrator {
					policy.IsAdministrator = true
					protected = append(protected, "IsAdmin=true preserved")
				}
				if policy.IsDisabled {
					policy.IsDisabled = false
					protected = append(protected, "IsDisabled=false preserved")
				}
				if len(protected) > 0 {
					notice = fmt.Sprintf("local admin %q protected during merge (%s)", name, strings.Join(protected, ", "))
				}
			}
			if err := m.userRepo.UpdateUserPolicy(ctx, existing.ID, policy); err != nil {
				return "", false, false, nil, "", err
			}
			if err := m.userRepo.UpdateUserConfig(ctx, existing.ID, src.Configuration); err != nil {
				return "", false, false, nil, "", err
			}
		}
		return existing.ID, true, false, nil, notice, nil
	}
	if getErr != nil && !errors.Is(getErr, sqlite.ErrNotFound) {
		return "", false, false, nil, "", getErr
	}

	if !migrateUsers {
		return "", false, false, &ConflictEntry{
			UserName: name,
			Reason:   "local user not found and migrate_users is disabled",
		}, "", nil
	}

	if dryRun {
		return "dryrun-" + guid.Normalize(src.ID), false, true, nil, "", nil
	}

	policy := src.Policy
	if policy.AuthenticationProviderId == "" {
		policy.AuthenticationProviderId = "Ambra.LocalAuthenticationProvider"
	}
	cfg := src.Configuration
	if cfg.SubtitleMode == "" {
		cfg = sqlite.DefaultUserConfig()
	}

	passwordHash := ""
	if src.HasPassword {
		var secret [32]byte
		if _, err := rand.Read(secret[:]); err != nil {
			return "", false, false, nil, "", fmt.Errorf("generate random password for migrated user %q: %w", name, err)
		}
		hashed, err := auth.HashPassword(hex.EncodeToString(secret[:]))
		if err != nil {
			return "", false, false, nil, "", fmt.Errorf("hash random password for migrated user %q: %w", name, err)
		}
		passwordHash = hashed
		policy.IsDisabled = true
		notice = fmt.Sprintf("user %q had a password on source server; created disabled with high-entropy random password and requires admin password reset", name)
	} else if policy.IsAdministrator {
		policy.IsDisabled = true
		notice = fmt.Sprintf("administrator %q had no password on source server; created disabled for security and requires setting a password before enabling", name)
	}

	rec := &sqlite.UserRecord{
		ID:           guid.New(),
		Name:         name,
		PasswordHash: passwordHash,
		IsAdmin:      policy.IsAdministrator,
		IsDisabled:   policy.IsDisabled,
		Policy:       policy,
		Config:       cfg,
		LastLoginAt:  src.LastLoginDate,
		LastActiveAt: src.LastActivityDate,
		CreatedAt:    time.Now().UTC(),
	}
	if err := m.userRepo.CreateUser(ctx, rec); err != nil {
		return "", false, false, nil, "", err
	}
	return rec.ID, false, true, nil, notice, nil
}

func (m *Migrator) writeUserItemBatch(ctx context.Context, batch []pendingUserItemWrite) error {
	if len(batch) == 0 {
		return nil
	}

	tx, err := m.store.Writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO user_item_data (
			user_id, item_id, played, play_count, playback_position_ticks, is_favorite, last_played_date
		) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, item_id) DO UPDATE SET
			played = CASE WHEN ? = 1 THEN MAX(user_item_data.played, excluded.played) ELSE user_item_data.played END,
			play_count = CASE WHEN ? = 1 THEN MAX(user_item_data.play_count, excluded.play_count) ELSE user_item_data.play_count END,
			playback_position_ticks = CASE WHEN ? = 1 AND excluded.playback_position_ticks > 0 THEN excluded.playback_position_ticks ELSE user_item_data.playback_position_ticks END,
			is_favorite = CASE WHEN ? = 1 THEN MAX(user_item_data.is_favorite, excluded.is_favorite) ELSE user_item_data.is_favorite END,
			last_played_date = CASE
				WHEN excluded.last_played_date IS NOT NULL AND (user_item_data.last_played_date IS NULL OR excluded.last_played_date > user_item_data.last_played_date)
				THEN excluded.last_played_date
				ELSE user_item_data.last_played_date
			END
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, w := range batch {
		uid := guid.Normalize(w.UserID)
		iid := guid.Normalize(w.ItemID)
		playedVal := 0
		playCountVal := 0
		if w.ApplyPlayed {
			if w.Played {
				playedVal = 1
			}
			playCountVal = w.PlayCount
		}
		posVal := int64(0)
		if w.ApplyResume {
			posVal = w.PositionTicks
		}
		favVal := 0
		if w.ApplyFavorite && w.IsFavorite {
			favVal = 1
		}
		var lastPlayed any
		if w.LastPlayedUnix != nil {
			lastPlayed = *w.LastPlayedUnix
		}

		if _, err := stmt.ExecContext(
			ctx,
			uid, iid, playedVal, playCountVal, posVal, favVal, lastPlayed,
			boolToInt(w.ApplyPlayed),
			boolToInt(w.ApplyPlayed),
			boolToInt(w.ApplyResume),
			boolToInt(w.ApplyFavorite),
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (m *Migrator) loadCheckpoint(ctx context.Context, key string) (checkpointState, bool) {
	raw, err := m.userRepo.GetServerConfig(ctx, key)
	if err != nil {
		return checkpointState{}, false
	}
	var cp checkpointState
	if err := json.Unmarshal([]byte(raw), &cp); err != nil {
		return checkpointState{}, false
	}
	return cp, true
}

func (m *Migrator) saveCheckpoint(ctx context.Context, key string, cp checkpointState) error {
	cp.UpdatedAt = time.Now().UTC().Unix()
	raw, err := json.Marshal(cp)
	if err != nil {
		return err
	}
	return m.userRepo.SetServerConfig(ctx, key, string(raw))
}

func (m *Migrator) clearCheckpoint(ctx context.Context, key string) error {
	_, err := m.store.Writer.ExecContext(ctx, `DELETE FROM server_config WHERE key = ?`, key)
	return err
}

func (m *Migrator) setStatus(r MigrationReport) {
	m.mu.Lock()
	m.status = cloneReport(r)
	m.mu.Unlock()
}

// --- Local Media Matching Index ---

type localItemRow struct {
	ID                string
	Type              string
	Name              string
	OriginalTitle     string
	SeriesID          string
	ParentIndexNumber *int
	IndexNumber       *int
	ProductionYear    *int
	Extra             sqlite.ItemExtra
}

type localItemIndex struct {
	byProvider  map[string][]string
	byTitleYear map[string][]string
	byTitleOnly map[string][]string
	byEpisode   map[string][]string
}

func (m *Migrator) loadLocalIndex(ctx context.Context) (*localItemIndex, error) {
	rows, err := m.store.Reader.QueryContext(ctx, `
		SELECT id, type, name, COALESCE(original_title, ''), COALESCE(series_id, ''),
		       parent_index_number, index_number, production_year, COALESCE(extra_json, '{}')
		FROM items
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []localItemRow
	seriesNames := make(map[string][]string)

	for rows.Next() {
		var r localItemRow
		var parentIdx, idxNum, prodYear sql.NullInt64
		var extraRaw string
		if err := rows.Scan(
			&r.ID, &r.Type, &r.Name, &r.OriginalTitle, &r.SeriesID,
			&parentIdx, &idxNum, &prodYear, &extraRaw,
		); err != nil {
			return nil, err
		}
		if parentIdx.Valid {
			v := int(parentIdx.Int64)
			r.ParentIndexNumber = &v
		}
		if idxNum.Valid {
			v := int(idxNum.Int64)
			r.IndexNumber = &v
		}
		if prodYear.Valid {
			v := int(prodYear.Int64)
			r.ProductionYear = &v
		}
		if extraRaw != "" && extraRaw != "{}" {
			_ = json.Unmarshal([]byte(extraRaw), &r.Extra)
		}
		items = append(items, r)
		if strings.EqualFold(r.Type, "Series") {
			if r.Name != "" {
				seriesNames[r.ID] = appendUnique(seriesNames[r.ID], r.Name)
			}
			if r.OriginalTitle != "" {
				seriesNames[r.ID] = appendUnique(seriesNames[r.ID], r.OriginalTitle)
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	idx := &localItemIndex{
		byProvider:  make(map[string][]string),
		byTitleYear: make(map[string][]string),
		byTitleOnly: make(map[string][]string),
		byEpisode:   make(map[string][]string),
	}

	for _, it := range items {
		normType := strings.ToLower(strings.TrimSpace(it.Type))
		for k, v := range it.Extra.ProviderIDs {
			pk := strings.ToLower(strings.TrimSpace(k))
			pv := strings.ToLower(strings.TrimSpace(v))
			if (pk == "tmdb" || pk == "imdb") && pv != "" {
				key := normType + "|" + pk + ":" + pv
				idx.byProvider[key] = appendUnique(idx.byProvider[key], it.ID)
			}
		}

		titles := []string{it.Name}
		if it.OriginalTitle != "" && it.OriginalTitle != it.Name {
			titles = append(titles, it.OriginalTitle)
		}
		for _, t := range titles {
			nt := normalizeTitle(t)
			if nt == "" {
				continue
			}
			idx.byTitleOnly[normType+"|"+nt] = appendUnique(idx.byTitleOnly[normType+"|"+nt], it.ID)
			if it.ProductionYear != nil && *it.ProductionYear > 0 {
				key := normType + "|" + nt + "|" + strconv.Itoa(*it.ProductionYear)
				idx.byTitleYear[key] = appendUnique(idx.byTitleYear[key], it.ID)
			}
		}

		if strings.EqualFold(it.Type, "Episode") && it.ParentIndexNumber != nil && it.IndexNumber != nil {
			var sNames []string
			if it.Extra.SeriesName != "" {
				sNames = appendUnique(sNames, it.Extra.SeriesName)
			}
			if it.SeriesID != "" {
				for _, sn := range seriesNames[it.SeriesID] {
					sNames = appendUnique(sNames, sn)
				}
			}
			for _, sn := range sNames {
				ns := normalizeTitle(sn)
				if ns == "" {
					continue
				}
				key := episodeKey(ns, *it.ParentIndexNumber, *it.IndexNumber)
				idx.byEpisode[key] = appendUnique(idx.byEpisode[key], it.ID)
			}
		}
	}

	return idx, nil
}

func (idx *localItemIndex) match(src SourceItem) (string, string) {
	normType := strings.ToLower(strings.TrimSpace(src.Type))

	// 1. Priority: ProviderIds (Tmdb first, then Imdb)
	if len(src.ProviderIDs) > 0 {
		lowerProviders := make(map[string]string, len(src.ProviderIDs))
		for k, v := range src.ProviderIDs {
			lk := strings.ToLower(strings.TrimSpace(k))
			lv := strings.ToLower(strings.TrimSpace(v))
			if lk != "" && lv != "" {
				lowerProviders[lk] = lv
			}
		}
		for _, provider := range []string{"tmdb", "imdb"} {
			if val, ok := lowerProviders[provider]; ok {
				if ids := idx.byProvider[normType+"|"+provider+":"+val]; len(ids) > 0 {
					if len(ids) > 1 {
						return "", fmt.Sprintf("ambiguous provider match (%s:%s matched %d local items)", provider, val, len(ids))
					}
					return ids[0], ""
				}
			}
		}
	}

	// 2. Episode fallback: SeriesName + Season + Episode
	if strings.EqualFold(src.Type, "Episode") && src.SeriesName != "" && src.ParentIndexNumber != nil && src.IndexNumber != nil {
		key := episodeKey(normalizeTitle(src.SeriesName), *src.ParentIndexNumber, *src.IndexNumber)
		if ids := idx.byEpisode[key]; len(ids) > 0 {
			if len(ids) > 1 {
				return "", fmt.Sprintf("ambiguous episode match (%s S%02dE%02d matched %d local items)", src.SeriesName, *src.ParentIndexNumber, *src.IndexNumber, len(ids))
			}
			return ids[0], ""
		}
	}

	// 3. Fallback: Type + normalized title + year (or title only if year absent)
	candidates := []string{src.Name}
	if src.OriginalTitle != "" && src.OriginalTitle != src.Name {
		candidates = append(candidates, src.OriginalTitle)
	}
	for _, title := range candidates {
		nt := normalizeTitle(title)
		if nt == "" {
			continue
		}
		if src.ProductionYear != nil && *src.ProductionYear > 0 {
			key := normType + "|" + nt + "|" + strconv.Itoa(*src.ProductionYear)
			if ids := idx.byTitleYear[key]; len(ids) > 0 {
				if len(ids) > 1 {
					return "", fmt.Sprintf("ambiguous title+year match (%s %d matched %d local items)", title, *src.ProductionYear, len(ids))
				}
				return ids[0], ""
			}
		} else {
			key := normType + "|" + nt
			if ids := idx.byTitleOnly[key]; len(ids) > 0 {
				if len(ids) > 1 {
					return "", fmt.Sprintf("ambiguous title match (%s matched %d local items)", title, len(ids))
				}
				return ids[0], ""
			}
		}
	}

	return "", ""
}

// --- Helpers ---

func filterSourceUsers(users []SourceUser, wantedIDs []string) []SourceUser {
	if len(wantedIDs) == 0 {
		return users
	}
	wanted := make(map[string]struct{}, len(wantedIDs))
	for _, id := range wantedIDs {
		n := strings.ToLower(strings.TrimSpace(id))
		if n != "" {
			wanted[n] = struct{}{}
			wanted[guid.Normalize(n)] = struct{}{}
		}
	}
	var out []SourceUser
	for _, u := range users {
		lu := strings.ToLower(strings.TrimSpace(u.ID))
		if _, ok := wanted[lu]; ok {
			out = append(out, u)
			continue
		}
		if _, ok := wanted[guid.Normalize(lu)]; ok {
			out = append(out, u)
		}
	}
	return out
}

func checkpointKeyFor(normalizedURL string, userIDs []string) string {
	sorted := make([]string, 0, len(userIDs))
	for _, id := range userIDs {
		if s := strings.TrimSpace(id); s != "" {
			sorted = append(sorted, strings.ToLower(s))
		}
	}
	sort.Strings(sorted)
	raw := strings.ToLower(strings.TrimRight(normalizedURL, "/")) + "|" + strings.Join(sorted, ",")
	sum := sha256.Sum256([]byte(raw))
	return checkpointPrefix + hex.EncodeToString(sum[:8])
}

func normalizeTitle(s string) string {
	s = strings.TrimSpace(sqlite.TradToSimp(s))
	if s == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func episodeKey(normSeries string, season, episode int) string {
	return normSeries + "|s" + strconv.Itoa(season) + "|e" + strconv.Itoa(episode)
}

func appendUnique(slice []string, val string) []string {
	for _, s := range slice {
		if s == val {
			return slice
		}
	}
	return append(slice, val)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func cloneReport(r MigrationReport) MigrationReport {
	out := r
	if r.StartedAt != nil {
		t := *r.StartedAt
		out.StartedAt = &t
	}
	if r.CompletedAt != nil {
		t := *r.CompletedAt
		out.CompletedAt = &t
	}
	out.Conflicts = make([]ConflictEntry, len(r.Conflicts))
	copy(out.Conflicts, r.Conflicts)
	out.Errors = make([]string, len(r.Errors))
	copy(out.Errors, r.Errors)
	if len(r.Notices) > 0 {
		out.Notices = make([]string, len(r.Notices))
		copy(out.Notices, r.Notices)
	}
	return out
}
