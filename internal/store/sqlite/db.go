package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"runtime"
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
	_, err = s.Writer.ExecContext(ctx,
		`INSERT OR IGNORE INTO schema_migrations (version, applied_at) VALUES (1, ?)`,
		time.Now().Unix(),
	)
	return err
}
