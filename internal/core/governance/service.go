package governance

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/budget"
	"github.com/ambra-media/ambra/internal/core/scheduler"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

// Options configures the governance service dependencies.
type Options struct {
	Store      *sqlite.Store
	Config     *config.Config
	Scheduler  *scheduler.Manager
	BudgetPool *budget.Pool
}

// Service provides media governance, scrape audit queue, NFO atomic writeback,
// library health auditing, and real-time system health diagnostics.
type Service struct {
	store      *sqlite.Store
	cfg        *config.Config
	scheduler  *scheduler.Manager
	budgetPool *budget.Pool
}

// NewService creates a governance Service and ensures the scrape_audit_queue table exists.
func NewService(opts Options) (*Service, error) {
	if opts.Store == nil {
		return nil, fmt.Errorf("%w: sqlite store is required", ErrInvalidArgument)
	}
	pool := opts.BudgetPool
	if pool == nil {
		pool = budget.Global()
	}
	s := &Service{
		store:      opts.Store,
		cfg:        opts.Config,
		scheduler:  opts.Scheduler,
		budgetPool: pool,
	}
	if err := s.initSchema(context.Background()); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Service) initSchema(ctx context.Context) error {
	_, err := s.store.Writer.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS scrape_audit_queue (
			id              TEXT PRIMARY KEY,
			item_id         TEXT NOT NULL,
			item_name       TEXT NOT NULL,
			item_type       TEXT NOT NULL,
			path            TEXT NOT NULL DEFAULT '',
			confidence      REAL NOT NULL DEFAULT 0,
			candidates_json TEXT NOT NULL DEFAULT '[]',
			status          TEXT NOT NULL DEFAULT 'pending',
			created_at      INTEGER NOT NULL,
			updated_at      INTEGER NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_scrape_audit_status ON scrape_audit_queue(status, created_at DESC);
		CREATE INDEX IF NOT EXISTS idx_scrape_audit_item ON scrape_audit_queue(item_id, status);
	`)
	return err
}

// --- Low-Confidence Scrape Audit Queue ---

// EnqueueAudit inserts or updates a pending low-confidence scrape item in the audit queue.
func (s *Service) EnqueueAudit(ctx context.Context, in EnqueueAuditInput) (*ScrapeAuditItem, error) {
	itemID := guid.Normalize(in.ItemID)
	if itemID == "" || strings.TrimSpace(in.ItemName) == "" {
		return nil, fmt.Errorf("%w: item id and item name are required", ErrInvalidArgument)
	}
	itemType := strings.TrimSpace(in.ItemType)
	if itemType == "" {
		itemType = "Movie"
	}
	candidates := in.Candidates
	if candidates == nil {
		candidates = []ScrapeCandidate{}
	}
	candJSON, err := json.Marshal(candidates)
	if err != nil {
		return nil, err
	}

	now := time.Now().Unix()

	tx, err := s.store.Writer.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Idempotent check-and-upsert inside a single writer transaction to avoid duplicate pending rows.
	var existingID string
	var createdAt int64
	err = tx.QueryRowContext(ctx,
		`SELECT id, created_at FROM scrape_audit_queue WHERE item_id = ? AND status = ? LIMIT 1`,
		itemID, AuditStatusPending,
	).Scan(&existingID, &createdAt)
	if err == nil && existingID != "" {
		if _, err := tx.ExecContext(ctx, `
			UPDATE scrape_audit_queue
			SET item_name = ?, item_type = ?, path = ?, confidence = ?, candidates_json = ?, updated_at = ?
			WHERE id = ?`,
			in.ItemName, itemType, in.Path, in.Confidence, string(candJSON), now, existingID,
		); err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return &ScrapeAuditItem{
			ID:         existingID,
			ItemID:     itemID,
			ItemName:   in.ItemName,
			ItemType:   itemType,
			Path:       in.Path,
			Confidence: in.Confidence,
			Candidates: candidates,
			Status:     AuditStatusPending,
			CreatedAt:  createdAt,
			UpdatedAt:  now,
		}, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	rec := &ScrapeAuditItem{
		ID:         guid.New(),
		ItemID:     itemID,
		ItemName:   in.ItemName,
		ItemType:   itemType,
		Path:       in.Path,
		Confidence: in.Confidence,
		Candidates: candidates,
		Status:     AuditStatusPending,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO scrape_audit_queue (
			id, item_id, item_name, item_type, path, confidence, candidates_json, status, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rec.ID, rec.ItemID, rec.ItemName, rec.ItemType, rec.Path, rec.Confidence,
		string(candJSON), rec.Status, rec.CreatedAt, rec.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return rec, nil
}

// ListAudits lists scrape audit items filtered by status ("" or "all" returns all statuses).
func (s *Service) ListAudits(ctx context.Context, status string) ([]ScrapeAuditItem, error) {
	status = strings.ToLower(strings.TrimSpace(status))
	var (
		rows *sql.Rows
		err  error
	)
	if status == "" || status == "all" {
		rows, err = s.store.Reader.QueryContext(ctx, `
			SELECT id, item_id, item_name, item_type, path, confidence, candidates_json, status, created_at, updated_at
			FROM scrape_audit_queue
			ORDER BY created_at DESC, id ASC`)
	} else {
		rows, err = s.store.Reader.QueryContext(ctx, `
			SELECT id, item_id, item_name, item_type, path, confidence, candidates_json, status, created_at, updated_at
			FROM scrape_audit_queue
			WHERE status = ?
			ORDER BY created_at DESC, id ASC`, status)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ScrapeAuditItem{}
	for rows.Next() {
		var rec ScrapeAuditItem
		var candRaw string
		if err := rows.Scan(
			&rec.ID, &rec.ItemID, &rec.ItemName, &rec.ItemType, &rec.Path,
			&rec.Confidence, &candRaw, &rec.Status, &rec.CreatedAt, &rec.UpdatedAt,
		); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(candRaw), &rec.Candidates)
		if rec.Candidates == nil {
			rec.Candidates = []ScrapeCandidate{}
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// ApproveAudit applies the selected metadata candidate to the item, optionally locks
// the specified fields in items.extra_json, syncs taxonomies and FTS, and marks the audit item as approved.
func (s *Service) ApproveAudit(ctx context.Context, auditID string, candidate ScrapeCandidate, lockFields []string) (*ScrapeAuditItem, error) {
	auditID = strings.TrimSpace(auditID)
	if auditID == "" {
		return nil, fmt.Errorf("%w: audit id is required", ErrInvalidArgument)
	}
	if isCandidateEmpty(candidate) {
		return nil, fmt.Errorf("%w: non-empty candidate metadata is required", ErrInvalidArgument)
	}

	tx, err := s.store.Writer.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var rec ScrapeAuditItem
	var candRaw string
	err = tx.QueryRowContext(ctx, `
		SELECT id, item_id, item_name, item_type, path, confidence, candidates_json, status, created_at, updated_at
		FROM scrape_audit_queue WHERE id = ?`, auditID,
	).Scan(
		&rec.ID, &rec.ItemID, &rec.ItemName, &rec.ItemType, &rec.Path,
		&rec.Confidence, &candRaw, &rec.Status, &rec.CreatedAt, &rec.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if rec.Status != AuditStatusPending {
		return nil, fmt.Errorf("%w: audit item %s is not pending (status=%s)", ErrInvalidArgument, rec.ID, rec.Status)
	}
	_ = json.Unmarshal([]byte(candRaw), &rec.Candidates)
	if rec.Candidates == nil {
		rec.Candidates = []ScrapeCandidate{}
	}

	// Load existing item inside the same writer transaction to merge metadata updates atomically.
	var (
		curName          string
		curOriginalTitle sql.NullString
		curYear          sql.NullInt64
		curRating        sql.NullFloat64
		extraRaw         string
	)
	err = tx.QueryRowContext(ctx, `
		SELECT name, original_title, production_year, community_rating, extra_json
		FROM items WHERE id = ?`, rec.ItemID,
	).Scan(&curName, &curOriginalTitle, &curYear, &curRating, &extraRaw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	newName := curName
	if strings.TrimSpace(candidate.Title) != "" {
		newName = strings.TrimSpace(candidate.Title)
	}
	newOriginalTitle := curOriginalTitle
	if strings.TrimSpace(candidate.OriginalTitle) != "" {
		newOriginalTitle = sql.NullString{String: strings.TrimSpace(candidate.OriginalTitle), Valid: true}
	}
	newYear := curYear
	if candidate.Year != nil {
		newYear = sql.NullInt64{Int64: int64(*candidate.Year), Valid: true}
	}
	newRating := curRating
	if candidate.Rating != nil {
		newRating = sql.NullFloat64{Float64: *candidate.Rating, Valid: true}
	}

	extra := decodeExtraMap(extraRaw)
	if strings.TrimSpace(candidate.Overview) != "" {
		extra["overview"] = strings.TrimSpace(candidate.Overview)
	}
	if strings.TrimSpace(candidate.ProviderID) != "" {
		providers := extractProviderIDs(extra)
		provName := strings.TrimSpace(candidate.ProviderName)
		if provName == "" {
			provName = "Tmdb"
		}
		for existingKey := range providers {
			if strings.EqualFold(existingKey, provName) {
				delete(providers, existingKey)
			}
		}
		providers[provName] = strings.TrimSpace(candidate.ProviderID)
		extra["provider_ids"] = providers
	}
	if len(lockFields) > 0 {
		merged := append(extractLockedFields(extra), lockFields...)
		extra["LockedFields"] = normalizeLockedFields(merged)
	}

	encodedExtra, err := json.Marshal(extra)
	if err != nil {
		return nil, err
	}

	now := time.Now().Unix()
	sortKey := sqlite.PinyinSortKey(newName)
	if _, err := tx.ExecContext(ctx, `
		UPDATE items
		SET name = ?, original_title = ?, sort_name = ?, sort_key = ?, production_year = ?,
		    community_rating = ?, extra_json = ?, date_modified = ?
		WHERE id = ?`,
		newName, newOriginalTitle, strings.ToLower(newName), sortKey, newYear,
		newRating, string(encodedExtra), now, rec.ItemID,
	); err != nil {
		return nil, err
	}

	if len(candidate.Genres) > 0 {
		if err := replaceItemTaxonomiesTx(ctx, tx, rec.ItemID, "genre", candidate.Genres); err != nil {
			return nil, err
		}
	}
	if len(candidate.Tags) > 0 {
		if err := replaceItemTaxonomiesTx(ctx, tx, rec.ItemID, "tag", candidate.Tags); err != nil {
			return nil, err
		}
	}
	if err := syncItemSearchFTSTx(ctx, tx, rec.ItemID, newName, newOriginalTitle.String); err != nil {
		return nil, err
	}

	res, err := tx.ExecContext(ctx, `
		UPDATE scrape_audit_queue SET status = ?, updated_at = ? WHERE id = ? AND status = ?`,
		AuditStatusApproved, now, rec.ID, AuditStatusPending,
	)
	if err != nil {
		return nil, err
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return nil, fmt.Errorf("%w: audit item %s is no longer pending", ErrInvalidArgument, rec.ID)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	rec.Status = AuditStatusApproved
	rec.UpdatedAt = now
	return &rec, nil
}

func isCandidateEmpty(c ScrapeCandidate) bool {
	return strings.TrimSpace(c.Title) == "" &&
		strings.TrimSpace(c.OriginalTitle) == "" &&
		strings.TrimSpace(c.ProviderID) == "" &&
		strings.TrimSpace(c.Overview) == "" &&
		c.Year == nil &&
		c.Rating == nil &&
		len(c.Genres) == 0 &&
		len(c.Tags) == 0
}

func replaceItemTaxonomiesTx(ctx context.Context, tx *sql.Tx, itemID, kind string, names []string) error {
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM item_taxonomies
		WHERE item_id = ? AND taxonomy_id IN (SELECT id FROM taxonomies WHERE kind = ?)`,
		itemID, kind,
	); err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(names))
	for _, rawName := range names {
		name := strings.TrimSpace(rawName)
		if name == "" {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}

		var taxID string
		err := tx.QueryRowContext(ctx, `SELECT id FROM taxonomies WHERE kind = ? AND name = ?`, kind, name).Scan(&taxID)
		if errors.Is(err, sql.ErrNoRows) {
			taxID = guid.New()
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO taxonomies (id, kind, name, sort_name) VALUES (?, ?, ?, ?)`,
				taxID, kind, name, strings.ToLower(name),
			); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO item_taxonomies (item_id, taxonomy_id) VALUES (?, ?)`,
			itemID, taxID,
		); err != nil {
			return err
		}
	}
	return nil
}

func isCJKRune(r rune) bool {
	return unicode.Is(unicode.Han, r) ||
		(r >= 0x3040 && r <= 0x30FF) ||
		(r >= 0xAC00 && r <= 0xD7AF)
}

func tokenizeForFTS(text string) string {
	var tokens []string
	seen := make(map[string]struct{})
	add := func(t string) {
		t = strings.TrimSpace(strings.ToLower(t))
		if t == "" {
			return
		}
		if _, ok := seen[t]; ok {
			return
		}
		seen[t] = struct{}{}
		tokens = append(tokens, t)
	}

	runes := []rune(text)
	i := 0
	for i < len(runes) {
		r := runes[i]
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			i++
			continue
		}
		if isCJKRune(r) {
			j := i
			for j < len(runes) && isCJKRune(runes[j]) {
				j++
			}
			cjk := runes[i:j]
			add(string(cjk))
			for k := 0; k < len(cjk); k++ {
				add(string(cjk[k]))
				if k+1 < len(cjk) {
					add(string(cjk[k : k+2]))
				}
			}
			i = j
		} else {
			j := i
			for j < len(runes) && !unicode.IsSpace(runes[j]) && !unicode.IsPunct(runes[j]) && !unicode.IsSymbol(runes[j]) && !isCJKRune(runes[j]) {
				j++
			}
			add(string(runes[i:j]))
			i = j
		}
	}
	return strings.Join(tokens, " ")
}

func syncItemSearchFTSTx(ctx context.Context, tx *sql.Tx, itemID, name, originalTitle string) error {
	var peopleNames string
	_ = tx.QueryRowContext(ctx,
		`SELECT COALESCE(people_names, '') FROM item_search_fts WHERE item_id = ? LIMIT 1`,
		itemID,
	).Scan(&peopleNames)

	titleRaw := strings.TrimSpace(name)
	if orig := strings.TrimSpace(originalTitle); orig != "" && !strings.EqualFold(orig, titleRaw) {
		titleRaw = strings.TrimSpace(titleRaw + " " + orig)
	}
	titleRaw = sqlite.TradToSimp(titleRaw)
	peopleRaw := sqlite.TradToSimp(peopleNames)
	titleTokens := tokenizeForFTS(titleRaw)
	pinyinFull, pinyinInitials := sqlite.ToPinyin(strings.TrimSpace(titleRaw + " " + peopleRaw))
	if strings.TrimSpace(peopleRaw) != "" {
		peopleNames = tokenizeForFTS(peopleRaw)
	} else {
		peopleNames = ""
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM item_search_fts WHERE item_id = ?`, itemID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx,
		`INSERT INTO item_search_fts (item_id, title_raw, title_tokens, pinyin_full, pinyin_initials, people_names) VALUES (?, ?, ?, ?, ?, ?)`,
		itemID, titleRaw, titleTokens, pinyinFull, pinyinInitials, peopleNames,
	)
	return err
}

// DismissAudit marks a scrape audit queue item as dismissed without modifying the item.
func (s *Service) DismissAudit(ctx context.Context, auditID string) (*ScrapeAuditItem, error) {
	auditID = strings.TrimSpace(auditID)
	if auditID == "" {
		return nil, fmt.Errorf("%w: audit id is required", ErrInvalidArgument)
	}

	var rec ScrapeAuditItem
	var candRaw string
	err := s.store.Reader.QueryRowContext(ctx, `
		SELECT id, item_id, item_name, item_type, path, confidence, candidates_json, status, created_at, updated_at
		FROM scrape_audit_queue WHERE id = ?`, auditID,
	).Scan(
		&rec.ID, &rec.ItemID, &rec.ItemName, &rec.ItemType, &rec.Path,
		&rec.Confidence, &candRaw, &rec.Status, &rec.CreatedAt, &rec.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(candRaw), &rec.Candidates)
	if rec.Candidates == nil {
		rec.Candidates = []ScrapeCandidate{}
	}

	now := time.Now().Unix()
	if _, err := s.store.Writer.ExecContext(ctx,
		`UPDATE scrape_audit_queue SET status = ?, updated_at = ? WHERE id = ?`,
		AuditStatusDismissed, now, rec.ID,
	); err != nil {
		return nil, err
	}
	rec.Status = AuditStatusDismissed
	rec.UpdatedAt = now
	return &rec, nil
}

// --- Library Audit & Orphan Cleanup ---

// RunLibraryAudit scans the entire library for missing posters, missing TMDB provider IDs,
// and non-STRM items whose physical disk paths no longer exist.
func (s *Service) RunLibraryAudit(ctx context.Context) (*LibraryAuditReport, error) {
	report := &LibraryAuditReport{
		ScannedAt:      time.Now().Unix(),
		MissingPosters: []AuditIssueItem{},
		MissingTMDB:    []AuditIssueItem{},
		OrphanItems:    []AuditIssueItem{},
	}

	// Build a set of item IDs that have a Primary image.
	hasPrimary := make(map[string]struct{})
	imgRows, err := s.store.Reader.QueryContext(ctx,
		`SELECT DISTINCT item_id FROM item_images WHERE lower(image_type) = 'primary'`)
	if err != nil {
		return nil, err
	}
	for imgRows.Next() {
		var id string
		if err := imgRows.Scan(&id); err != nil {
			_ = imgRows.Close()
			return nil, err
		}
		hasPrimary[id] = struct{}{}
	}
	if err := imgRows.Err(); err != nil {
		_ = imgRows.Close()
		return nil, err
	}
	_ = imgRows.Close()

	// Load all media_sources per item so multi-version items are only flagged as orphans
	// when ALL associated physical paths are missing.
	type mediaSourcePathInfo struct {
		path     string
		isRemote bool
	}
	sourcesByItem := make(map[string][]mediaSourcePathInfo)
	msRows, err := s.store.Reader.QueryContext(ctx, `SELECT item_id, path, is_remote FROM item_media_sources`)
	if err != nil {
		return nil, err
	}
	for msRows.Next() {
		var itemID, msPath string
		var isRemoteInt int
		if err := msRows.Scan(&itemID, &msPath, &isRemoteInt); err != nil {
			_ = msRows.Close()
			return nil, err
		}
		sourcesByItem[itemID] = append(sourcesByItem[itemID], mediaSourcePathInfo{
			path:     strings.TrimSpace(msPath),
			isRemote: isRemoteInt != 0,
		})
	}
	if err := msRows.Err(); err != nil {
		_ = msRows.Close()
		return nil, err
	}
	_ = msRows.Close()

	rows, err := s.store.Reader.QueryContext(ctx, `
		SELECT id, name, type, library_id, path, production_year, is_strm, is_folder, extra_json
		FROM items
		ORDER BY type ASC, sort_name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			id          string
			name        string
			itemType    string
			libraryID   string
			pathNull    sql.NullString
			yearNull    sql.NullInt64
			isStrmInt   int
			isFolderInt int
			extraRaw    string
		)
		if err := rows.Scan(&id, &name, &itemType, &libraryID, &pathNull, &yearNull, &isStrmInt, &isFolderInt, &extraRaw); err != nil {
			return nil, err
		}

		report.TotalChecked++
		issue := AuditIssueItem{
			ID:        id,
			Name:      name,
			Type:      itemType,
			LibraryID: libraryID,
			Path:      strings.TrimSpace(pathNull.String),
			IsStrm:    isStrmInt != 0,
		}
		if yearNull.Valid {
			y := int(yearNull.Int64)
			issue.Year = &y
		}

		isMovieOrSeries := strings.EqualFold(itemType, "Movie") || strings.EqualFold(itemType, "Series")
		if isMovieOrSeries {
			if _, ok := hasPrimary[id]; !ok {
				report.MissingPosters = append(report.MissingPosters, issue)
			}
			extra := decodeExtraMap(extraRaw)
			providers := extractProviderIDs(extra)
			if lookupProviderID(providers, "Tmdb") == "" {
				report.MissingTMDB = append(report.MissingTMDB, issue)
			}
		}

		if isEligibleOrphanCandidate(issue.IsStrm, isFolderInt != 0, issue.Path) {
			msList := sourcesByItem[id]
			msPaths := make([]mediaSourceRef, 0, len(msList))
			for _, ms := range msList {
				msPaths = append(msPaths, mediaSourceRef{path: ms.path, isRemote: ms.isRemote})
			}
			if allPhysicalPathsMissing(issue.Path, msPaths) {
				report.OrphanItems = append(report.OrphanItems, issue)
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return report, nil
}

func isVirtualPath(p string) bool {
	p = strings.TrimSpace(p)
	if p == "" {
		return true
	}
	lower := strings.ToLower(p)
	if strings.HasPrefix(lower, "artist://") || strings.HasPrefix(lower, "album://") || strings.Contains(p, "://") {
		return true
	}
	if strings.Contains(p, "#Season") {
		return true
	}
	return false
}

type mediaSourceRef struct {
	path     string
	isRemote bool
}

func allPhysicalPathsMissing(itemPath string, sources []mediaSourceRef) bool {
	if _, statErr := os.Stat(itemPath); statErr == nil || !os.IsNotExist(statErr) {
		return false
	}
	for _, ms := range sources {
		if ms.isRemote {
			return false
		}
		p := strings.TrimSpace(ms.path)
		if p == "" || isVirtualPath(p) {
			continue
		}
		if _, statErr := os.Stat(p); statErr == nil || !os.IsNotExist(statErr) {
			return false
		}
	}
	return true
}

func isEligibleOrphanCandidate(isStrm, isFolder bool, path string) bool {
	if isStrm || isFolder {
		return false
	}
	return !isVirtualPath(path)
}

// CleanupOrphans deletes the specified orphaned item IDs from items, item_ancestors,
// item_search_fts, and scrape_audit_queue within a single transaction after re-verifying
// each item is a non-STRM, non-folder physical file whose disk path no longer exists.
func (s *Service) CleanupOrphans(ctx context.Context, itemIDs []string) (*OrphanCleanupResult, error) {
	seen := make(map[string]struct{}, len(itemIDs))
	unique := make([]string, 0, len(itemIDs))
	for _, raw := range itemIDs {
		id := guid.Normalize(raw)
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}

	res := &OrphanCleanupResult{
		Requested:  len(unique),
		DeletedIDs: []string{},
	}
	if len(unique) == 0 {
		return res, nil
	}

	tx, err := s.store.Writer.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	for _, id := range unique {
		var (
			isStrmInt   int
			isFolderInt int
			pathNull    sql.NullString
		)
		err := tx.QueryRowContext(ctx, `SELECT is_strm, is_folder, path FROM items WHERE id = ?`, id).Scan(&isStrmInt, &isFolderInt, &pathNull)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		itemPath := strings.TrimSpace(pathNull.String)
		if !isEligibleOrphanCandidate(isStrmInt != 0, isFolderInt != 0, itemPath) {
			continue
		}
		msRows, msErr := tx.QueryContext(ctx, `SELECT path, is_remote FROM item_media_sources WHERE item_id = ?`, id)
		if msErr != nil {
			return nil, msErr
		}
		var sources []mediaSourceRef
		for msRows.Next() {
			var msPath string
			var isRemoteInt int
			if scanErr := msRows.Scan(&msPath, &isRemoteInt); scanErr != nil {
				_ = msRows.Close()
				return nil, scanErr
			}
			sources = append(sources, mediaSourceRef{
				path:     strings.TrimSpace(msPath),
				isRemote: isRemoteInt != 0,
			})
		}
		if err := msRows.Err(); err != nil {
			_ = msRows.Close()
			return nil, err
		}
		_ = msRows.Close()
		if !allPhysicalPathsMissing(itemPath, sources) {
			continue
		}

		if _, err := tx.ExecContext(ctx, `DELETE FROM item_search_fts WHERE item_id = ?`, id); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM item_ancestors WHERE ancestor_id = ? OR descendant_id = ?`, id, id); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM scrape_audit_queue WHERE item_id = ?`, id); err != nil {
			return nil, err
		}
		execRes, err := tx.ExecContext(ctx, `DELETE FROM items WHERE id = ?`, id)
		if err != nil {
			return nil, err
		}
		if affected, _ := execRes.RowsAffected(); affected > 0 {
			res.Deleted++
			res.DeletedIDs = append(res.DeletedIDs, id)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return res, nil
}

// --- Real-Time System Health Diagnostics ---

// CollectHealth gathers runtime memory/goroutines, SQLite DB+WAL+SHM sizes,
// cache directory sizes, data volume disk space, library/user counts, and scheduler/budget metrics.
func (s *Service) CollectHealth(ctx context.Context) (*SystemHealthReport, error) {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	report := &SystemHealthReport{
		CollectedAt:  time.Now().Unix(),
		Status:       "healthy",
		GoVersion:    runtime.Version(),
		NumCPU:       runtime.NumCPU(),
		NumGoroutine: runtime.NumGoroutine(),
		Memory: MemoryHealth{
			AllocBytes:      ms.Alloc,
			TotalAllocBytes: ms.TotalAlloc,
			SysBytes:        ms.Sys,
			HeapInuseBytes:  ms.HeapInuse,
			NumGC:           ms.NumGC,
		},
	}

	if s.cfg != nil {
		dbPath := s.cfg.DBPath()
		dbSize := fileSizeOrZero(dbPath)
		walSize := fileSizeOrZero(dbPath + "-wal")
		shmSize := fileSizeOrZero(dbPath + "-shm")
		report.Database = DatabaseHealth{
			DBPath:     dbPath,
			DBBytes:    dbSize,
			WALBytes:   walSize,
			SHMBytes:   shmSize,
			TotalBytes: dbSize + walSize + shmSize,
		}

		imgSize := dirSizeBytes(s.cfg.ImageCacheDir())
		chapSize := dirSizeBytes(s.cfg.ChapterCacheDir())
		bifSize := dirSizeBytes(s.cfg.BifCacheDir())
		subSize := dirSizeBytes(s.cfg.SubtitleCacheDir())
		transSize := dirSizeBytes(s.cfg.TranscodeTempDir())
		report.Cache = CacheHealth{
			ImagesBytes:    imgSize,
			ChaptersBytes:  chapSize,
			BifBytes:       bifSize,
			SubtitlesBytes: subSize,
			TranscodeBytes: transSize,
			TotalBytes:     imgSize + chapSize + bifSize + subSize + transSize,
		}

		report.Disk = collectDiskHealth(s.cfg.DataDir)
	}

	if err := s.collectLibraryStats(ctx, &report.Library); err != nil {
		return nil, err
	}

	if s.scheduler != nil {
		tasks := s.scheduler.List(true)
		active := 0
		for _, t := range tasks {
			if t.State == scheduler.StateRunning || t.State == scheduler.StateCancelling {
				active++
			}
		}
		report.Scheduler.TotalTasks = len(tasks)
		report.Scheduler.ActiveTasks = active
	}
	if s.budgetPool != nil {
		inUse := s.budgetPool.InUse()
		capVal := s.budgetPool.Capacity()
		report.Scheduler.BudgetInUse = inUse
		report.Scheduler.BudgetCapacity = capVal
		if capVal > 0 {
			pct := float64(inUse) / float64(capVal) * 100
			report.Scheduler.BudgetUsagePct = math.Round(pct*10) / 10
		}
	}

	if report.Disk.UsedPercent >= 95 {
		report.Status = "critical"
	} else if report.Disk.UsedPercent >= 85 {
		report.Status = "warning"
	}

	return report, nil
}

func (s *Service) collectLibraryStats(ctx context.Context, out *LibraryStatsHealth) error {
	rows, err := s.store.Reader.QueryContext(ctx, `SELECT type, COUNT(*) FROM items GROUP BY type`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var t string
		var cnt int
		if err := rows.Scan(&t, &cnt); err != nil {
			return err
		}
		out.TotalItems += cnt
		switch strings.ToLower(t) {
		case "movie":
			out.MovieCount += cnt
		case "series":
			out.SeriesCount += cnt
		case "episode":
			out.EpisodeCount += cnt
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	if err := s.store.Reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&out.UserCount); err != nil {
		return err
	}
	if err := s.store.Reader.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM scrape_audit_queue WHERE status = ?`, AuditStatusPending,
	).Scan(&out.PendingAudits); err != nil {
		return err
	}
	return nil
}

func fileSizeOrZero(path string) int64 {
	if path == "" {
		return 0
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return 0
	}
	return info.Size()
}

func dirSizeBytes(root string) int64 {
	if root == "" {
		return 0
	}
	var total int64
	_ = filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if d.Name() == config.ChapterCacheReadmeName {
			return nil
		}
		if info, statErr := d.Info(); statErr == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}
