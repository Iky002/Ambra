package sqlite

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"embed"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	sqlite3 "modernc.org/sqlite"
)

func init() {
	sqlite3.MustRegisterDeterministicScalarFunction(
		"pinyin_sort_key",
		1,
		func(_ *sqlite3.FunctionContext, args []driver.Value) (driver.Value, error) {
			if len(args) == 0 || args[0] == nil {
				return "", nil
			}
			switch v := args[0].(type) {
			case string:
				return PinyinSortKey(v), nil
			case []byte:
				return PinyinSortKey(string(v)), nil
			default:
				return PinyinSortKey(fmt.Sprint(v)), nil
			}
		},
	)
	sqlite3.MustRegisterDeterministicScalarFunction(
		"parental_rating_rank",
		1,
		func(_ *sqlite3.FunctionContext, args []driver.Value) (driver.Value, error) {
			if len(args) == 0 || args[0] == nil {
				return int64(0), nil
			}
			switch v := args[0].(type) {
			case string:
				return int64(ParseParentalRatingValue(v)), nil
			case []byte:
				return int64(ParseParentalRatingValue(string(v))), nil
			default:
				return int64(ParseParentalRatingValue(fmt.Sprint(v))), nil
			}
		},
	)
}

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Store manages separated read/write SQLite pools configured with WAL mode.
type Store struct {
	Writer *sql.DB
	Reader *sql.DB
}

func envIntWithDefault(key string, defVal, minVal int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return defVal
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return defVal
	}
	if v < minVal {
		return minVal
	}
	return v
}

func escapeSQLiteURIPath(p string) string {
	return strings.NewReplacer("%", "%25", "?", "%3F", "#", "%23").Replace(p)
}

func Open(dbPath string) (*Store, error) {
	cacheMB := envIntWithDefault("AMBRA_SQLITE_CACHE_MB", 64, 4)
	mmapMB := envIntWithDefault("AMBRA_SQLITE_MMAP_MB", 256, 0)
	cacheKB := cacheMB * 1024
	mmapBytes := int64(mmapMB) * 1024 * 1024
	escapedPath := escapeSQLiteURIPath(dbPath)

	writerDSN := fmt.Sprintf(
		"file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)&_pragma=temp_store(MEMORY)&_pragma=cache_size(-%d)",
		escapedPath, cacheKB,
	)

	writer, err := sql.Open("sqlite", writerDSN)
	if err != nil {
		return nil, fmt.Errorf("open sqlite writer: %w", err)
	}
	writer.SetMaxOpenConns(1)
	writer.SetMaxIdleConns(1)

	readerDSN := fmt.Sprintf(
		"file:%s?mode=ro&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)&_pragma=temp_store(MEMORY)&_pragma=cache_size(-%d)",
		escapedPath, cacheKB,
	)
	if mmapBytes > 0 {
		readerDSN += fmt.Sprintf("&_pragma=mmap_size(%d)", mmapBytes)
	}
	reader, err := sql.Open("sqlite", readerDSN)
	if err != nil {
		_ = writer.Close()
		return nil, fmt.Errorf("open sqlite reader: %w", err)
	}
	maxReaders := runtime.NumCPU() * 2
	if maxReaders < 4 {
		maxReaders = 4
	}
	reader.SetMaxOpenConns(maxReaders)
	reader.SetMaxIdleConns(maxReaders)

	s := &Store{Writer: writer, Reader: reader}
	if err := s.migrate(context.Background()); err != nil {
		_ = s.Close()
		return nil, err
	}
	_, _ = s.Writer.ExecContext(context.Background(), `PRAGMA optimize`)
	return s, nil
}

func (s *Store) Close() error {
	var firstErr error
	if s.Reader != nil {
		if err := s.Reader.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if s.Writer != nil {
		_, _ = s.Writer.Exec(`PRAGMA optimize`)
		if err := s.Writer.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (s *Store) migrate(ctx context.Context) error {
	sqlBytes, err := migrationsFS.ReadFile("migrations/001_init.sql")
	if err != nil {
		return fmt.Errorf("read migration 001: %w", err)
	}
	if _, err := s.Writer.ExecContext(ctx, string(sqlBytes)); err != nil {
		return fmt.Errorf("apply migration 001: %w", err)
	}
	if _, err := s.Writer.ExecContext(ctx,
		`INSERT OR IGNORE INTO schema_migrations (version, applied_at) VALUES (1, ?)`,
		time.Now().Unix(),
	); err != nil {
		return err
	}

	var v2 int
	if err := s.Writer.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version = 2`).Scan(&v2); err != nil {
		return err
	}
	if v2 == 0 {
		// Rebuild item_images without foreign key on items(id) so people and taxonomies can also store images.
		var ddl string
		_ = s.Writer.QueryRowContext(ctx, `SELECT COALESCE(sql, '') FROM sqlite_master WHERE type = 'table' AND name = 'item_images'`).Scan(&ddl)
		if ddl != "" && strings.Contains(strings.ToUpper(ddl), "REFERENCES") {
			tx, err := s.Writer.BeginTx(ctx, nil)
			if err != nil {
				return err
			}
			defer tx.Rollback()
			stmts := []string{
				`CREATE TABLE item_images_new (
					item_id     TEXT NOT NULL,
					image_type  TEXT NOT NULL,
					image_index INTEGER NOT NULL DEFAULT 0,
					path        TEXT NOT NULL,
					tag         TEXT NOT NULL,
					width       INTEGER,
					height      INTEGER,
					blurhash    TEXT,
					PRIMARY KEY (item_id, image_type, image_index)
				)`,
				`INSERT OR IGNORE INTO item_images_new (item_id, image_type, image_index, path, tag, width, height, blurhash)
				 SELECT item_id, image_type, image_index, path, tag, width, height, blurhash FROM item_images`,
				`DROP TABLE item_images`,
				`ALTER TABLE item_images_new RENAME TO item_images`,
			}
			for _, st := range stmts {
				if _, err := tx.ExecContext(ctx, st); err != nil {
					return fmt.Errorf("apply migration 002: %w", err)
				}
			}
			if err := tx.Commit(); err != nil {
				return err
			}
		}
		if _, err := s.Writer.ExecContext(ctx,
			`INSERT OR IGNORE INTO schema_migrations (version, applied_at) VALUES (2, ?)`,
			time.Now().Unix(),
		); err != nil {
			return err
		}
	}

	var v3 int
	if err := s.Writer.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version = 3`).Scan(&v3); err != nil {
		return err
	}
	if v3 == 0 {
		if err := s.applyMigrationV3(ctx); err != nil {
			return err
		}
	}

	var v4 int
	if err := s.Writer.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version = 4`).Scan(&v4); err != nil {
		return err
	}
	if v4 == 0 {
		if err := s.applyMigrationV4(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) applyMigrationV3(ctx context.Context) error {
	cols := make(map[string]bool)
	rows, err := s.Writer.QueryContext(ctx, `PRAGMA table_info(items)`)
	if err != nil {
		return fmt.Errorf("migration 003 pragma table_info: %w", err)
	}
	for rows.Next() {
		var (
			cid        int
			name, ctyp string
			notnull    int
			dflt       sql.NullString
			pk         int
		)
		if err := rows.Scan(&cid, &name, &ctyp, &notnull, &dflt, &pk); err != nil {
			rows.Close()
			return fmt.Errorf("migration 003 scan table_info: %w", err)
		}
		cols[strings.ToLower(name)] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	tx, err := s.Writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if !cols["sort_key"] {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE items ADD COLUMN sort_key TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("migration 003 add sort_key: %w", err)
		}
	}
	if !cols["extra_type"] {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE items ADD COLUMN extra_type TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("migration 003 add extra_type: %w", err)
		}
	}
	if !cols["parental_rating_value"] {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE items ADD COLUMN parental_rating_value INTEGER NOT NULL DEFAULT 0`); err != nil {
			return fmt.Errorf("migration 003 add parental_rating_value: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE items
		SET sort_key = pinyin_sort_key(COALESCE(NULLIF(sort_name, ''), name)),
		    extra_type = LOWER(COALESCE(json_extract(extra_json, '$.extra_type'), '')),
		    parental_rating_value = parental_rating_rank(official_rating)
	`); err != nil {
		return fmt.Errorf("migration 003 backfill items: %w", err)
	}

	indexStmts := []string{
		`CREATE INDEX IF NOT EXISTS idx_items_lib_type_sortkey ON items(library_id, type, sort_key) WHERE extra_type = ''`,
		`CREATE INDEX IF NOT EXISTS idx_items_type_sortkey ON items(type, sort_key) WHERE extra_type = ''`,
		`CREATE INDEX IF NOT EXISTS idx_items_lib_type_created ON items(library_id, type, date_created DESC) WHERE extra_type = ''`,
		`CREATE INDEX IF NOT EXISTS idx_items_parent_extras ON items(parent_id, extra_type) WHERE extra_type != ''`,
	}
	for _, st := range indexStmts {
		if _, err := tx.ExecContext(ctx, st); err != nil {
			return fmt.Errorf("migration 003 create index: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO schema_migrations (version, applied_at) VALUES (3, ?)`,
		time.Now().Unix(),
	); err != nil {
		return fmt.Errorf("migration 003 record version: %w", err)
	}

	return tx.Commit()
}

func (s *Store) applyMigrationV4(ctx context.Context) error {
	tx, err := s.Writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	indexStmts := []string{
		`CREATE INDEX IF NOT EXISTS idx_users_name_nocase ON users(LOWER(name))`,
		`CREATE INDEX IF NOT EXISTS idx_activity_logs_created_at ON activity_logs(created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_item_collections_item_id ON item_collections(item_id)`,
	}
	for _, st := range indexStmts {
		if _, err := tx.ExecContext(ctx, st); err != nil {
			return fmt.Errorf("migration 004 create index: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO schema_migrations (version, applied_at) VALUES (4, ?)`,
		time.Now().Unix(),
	); err != nil {
		return fmt.Errorf("migration 004 record version: %w", err)
	}

	return tx.Commit()
}
