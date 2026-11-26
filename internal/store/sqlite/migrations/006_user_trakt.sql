CREATE TABLE IF NOT EXISTS user_trakt (
    user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    client_id TEXT NOT NULL,
    access_token TEXT NOT NULL,
    scrobble_playback INTEGER NOT NULL DEFAULT 1,
    sync_watched INTEGER NOT NULL DEFAULT 1,
    updated_at INTEGER NOT NULL
);
