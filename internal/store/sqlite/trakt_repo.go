package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/ambra-media/ambra/internal/pkg/guid"
)

// TraktConfig is private account data. It must never be included in user DTOs.
type TraktConfig struct {
	ClientID         string
	AccessToken      string
	ScrobblePlayback bool
	SyncWatched      bool
}

func (r *UserRepo) GetUserTraktConfig(ctx context.Context, userID string) (*TraktConfig, error) {
	uid := guid.Normalize(userID)
	if uid == "" {
		return nil, nil
	}
	var c TraktConfig
	err := r.store.Reader.QueryRowContext(ctx, `
		SELECT t.client_id, t.access_token, t.scrobble_playback, t.sync_watched
		FROM user_trakt t JOIN users u ON u.id = t.user_id
		WHERE t.user_id = ? AND u.is_disabled = 0`,
		uid,
	).Scan(&c.ClientID, &c.AccessToken, &c.ScrobblePlayback, &c.SyncWatched)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &c, err
}

func (r *UserRepo) SaveUserTraktConfig(ctx context.Context, userID string, c TraktConfig) error {
	uid := guid.Normalize(userID)
	if uid == "" {
		return errors.New("user id is required")
	}
	_, err := r.store.Writer.ExecContext(ctx, `
		INSERT INTO user_trakt (user_id, client_id, access_token, scrobble_playback, sync_watched, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET client_id = excluded.client_id,
			access_token = excluded.access_token, scrobble_playback = excluded.scrobble_playback,
			sync_watched = excluded.sync_watched, updated_at = excluded.updated_at`,
		uid, c.ClientID, c.AccessToken, c.ScrobblePlayback, c.SyncWatched, time.Now().Unix())
	return err
}

func (r *UserRepo) DeleteUserTraktConfig(ctx context.Context, userID string) error {
	uid := guid.Normalize(userID)
	if uid == "" {
		return nil
	}
	_, err := r.store.Writer.ExecContext(ctx, `DELETE FROM user_trakt WHERE user_id = ?`, uid)
	return err
}
