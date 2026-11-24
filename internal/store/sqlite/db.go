package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"runtime"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Store manages separated read/write SQLite pools configured with WAL mode.
type Store struct {
	Writer *sql.DB
	Reader *sql.DB
}

func Open(dbPath string) (*Store, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)&_pragma=temp_store(MEMORY)&_pragma=cache_size(-64000)", dbPath)

	writer, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite writer: %w", err)
	}
	writer.SetMaxOpenConns(1)
	writer.SetMaxIdleConns(1)

	reader, err := sql.Open("sqlite", dsn+"&mode=ro")
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
	return nil
}
