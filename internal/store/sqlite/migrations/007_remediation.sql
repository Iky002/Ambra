CREATE TABLE IF NOT EXISTS camera_uploads (
    user_id        TEXT NOT NULL,
    device_id      TEXT NOT NULL,
    file_id        TEXT NOT NULL,
    name           TEXT NOT NULL,
    album          TEXT NOT NULL DEFAULT '',
    mime_type      TEXT NOT NULL DEFAULT '',
    file_path      TEXT NOT NULL DEFAULT '',
    file_size      INTEGER NOT NULL DEFAULT 0,
    item_id        TEXT NOT NULL DEFAULT '',
    created_at     INTEGER NOT NULL,
    PRIMARY KEY (user_id, device_id, file_id)
);
CREATE INDEX IF NOT EXISTS idx_camera_uploads_device ON camera_uploads(user_id, device_id, created_at DESC);

CREATE TABLE IF NOT EXISTS webhooks (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    url         TEXT NOT NULL,
    secret      TEXT NOT NULL DEFAULT '',
    events_json TEXT NOT NULL DEFAULT '[]',
    enabled     INTEGER NOT NULL DEFAULT 1,
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS dlna_profiles (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    profile_json TEXT NOT NULL DEFAULT '{}',
    is_system   INTEGER NOT NULL DEFAULT 0,
    updated_at  INTEGER NOT NULL
);
