CREATE TABLE IF NOT EXISTS schema_migrations (
    version    INTEGER PRIMARY KEY,
    applied_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS server_config (
    key        TEXT PRIMARY KEY,
    value_json TEXT NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS libraries (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    collection_type TEXT NOT NULL DEFAULT 'movies',
    paths_json      TEXT NOT NULL DEFAULT '[]',
    options_json    TEXT NOT NULL DEFAULT '{}',
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS items (
    id                   TEXT PRIMARY KEY,
    internal_id          INTEGER UNIQUE,
    library_id           TEXT NOT NULL,
    parent_id            TEXT,
    series_id            TEXT,
    season_id            TEXT,
    type                 TEXT NOT NULL,
    media_type           TEXT,
    is_folder            INTEGER NOT NULL DEFAULT 0,
    is_strm              INTEGER NOT NULL DEFAULT 0,
    name                 TEXT NOT NULL,
    original_title       TEXT,
    sort_name            TEXT NOT NULL,
    index_number         INTEGER,
    parent_index_number  INTEGER,
    production_year      INTEGER,
    premiere_date        INTEGER,
    end_date             INTEGER,
    community_rating     REAL,
    critic_rating        REAL,
    official_rating      TEXT,
    runtime_ticks        INTEGER,
    container            TEXT,
    path                 TEXT,
    file_size            INTEGER,
    file_mtime           INTEGER,
    etag                 TEXT NOT NULL,
    extra_json           TEXT NOT NULL DEFAULT '{}',
    date_created         INTEGER NOT NULL,
    date_modified        INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_items_library_type ON items(library_id, type);
CREATE INDEX IF NOT EXISTS idx_items_parent ON items(parent_id, type, index_number);
CREATE INDEX IF NOT EXISTS idx_items_series ON items(series_id, type, parent_index_number, index_number);
CREATE INDEX IF NOT EXISTS idx_items_sort_name ON items(type, sort_name);
CREATE INDEX IF NOT EXISTS idx_items_date_created ON items(library_id, type, date_created DESC);
CREATE INDEX IF NOT EXISTS idx_items_year ON items(type, production_year);
CREATE INDEX IF NOT EXISTS idx_items_path ON items(path);

CREATE TABLE IF NOT EXISTS item_ancestors (
    ancestor_id   TEXT NOT NULL,
    descendant_id TEXT NOT NULL,
    depth         INTEGER NOT NULL,
    PRIMARY KEY (ancestor_id, descendant_id)
);
CREATE INDEX IF NOT EXISTS idx_ancestors_desc ON item_ancestors(descendant_id);

CREATE TABLE IF NOT EXISTS item_media_sources (
    id                     TEXT PRIMARY KEY,
    item_id                TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    name                   TEXT NOT NULL,
    path                   TEXT NOT NULL,
    protocol               TEXT NOT NULL DEFAULT 'File',
    container              TEXT,
    size                   INTEGER,
    bitrate                INTEGER,
    runtime_ticks          INTEGER,
    is_remote              INTEGER NOT NULL DEFAULT 0,
    supports_direct_play   INTEGER NOT NULL DEFAULT 1,
    supports_direct_stream INTEGER NOT NULL DEFAULT 1,
    supports_transcoding   INTEGER NOT NULL DEFAULT 1,
    extra_json             TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS idx_media_sources_item ON item_media_sources(item_id);

CREATE TABLE IF NOT EXISTS item_media_streams (
    media_source_id TEXT NOT NULL REFERENCES item_media_sources(id) ON DELETE CASCADE,
    item_id         TEXT NOT NULL,
    stream_index    INTEGER NOT NULL,
    stream_type     TEXT NOT NULL,
    codec           TEXT,
    language        TEXT,
    sub_variant     TEXT,
    display_title   TEXT,
    title           TEXT,
    width           INTEGER,
    height          INTEGER,
    video_range     TEXT,
    audio_layout    TEXT,
    channels        INTEGER,
    sample_rate     INTEGER,
    bitrate         INTEGER,
    is_default      INTEGER NOT NULL DEFAULT 0,
    is_forced       INTEGER NOT NULL DEFAULT 0,
    is_external     INTEGER NOT NULL DEFAULT 0,
    path            TEXT,
    PRIMARY KEY (media_source_id, stream_index)
);
CREATE INDEX IF NOT EXISTS idx_streams_item_type ON item_media_streams(item_id, stream_type);

CREATE TABLE IF NOT EXISTS item_images (
    item_id     TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    image_type  TEXT NOT NULL,
    image_index INTEGER NOT NULL DEFAULT 0,
    path        TEXT NOT NULL,
    tag         TEXT NOT NULL,
    width       INTEGER,
    height      INTEGER,
    blurhash    TEXT,
    PRIMARY KEY (item_id, image_type, image_index)
);

CREATE TABLE IF NOT EXISTS people (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE,
    sort_name  TEXT NOT NULL,
    extra_json TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE IF NOT EXISTS item_people (
    item_id     TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    person_id   TEXT NOT NULL REFERENCES people(id) ON DELETE CASCADE,
    role        TEXT,
    person_type TEXT NOT NULL,
    sort_order  INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (item_id, person_id, person_type)
);
CREATE INDEX IF NOT EXISTS idx_item_people_person ON item_people(person_id);

CREATE TABLE IF NOT EXISTS taxonomies (
    id        TEXT PRIMARY KEY,
    kind      TEXT NOT NULL,
    name      TEXT NOT NULL,
    sort_name TEXT NOT NULL,
    UNIQUE(kind, name)
);

CREATE TABLE IF NOT EXISTS item_taxonomies (
    item_id     TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    taxonomy_id TEXT NOT NULL REFERENCES taxonomies(id) ON DELETE CASCADE,
    PRIMARY KEY (item_id, taxonomy_id)
);
CREATE INDEX IF NOT EXISTS idx_item_tax_tax ON item_taxonomies(taxonomy_id);

CREATE TABLE IF NOT EXISTS item_collections (
    container_id TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    item_id      TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    position     INTEGER NOT NULL,
    entry_id     TEXT NOT NULL,
    PRIMARY KEY (container_id, entry_id)
);

CREATE TABLE IF NOT EXISTS users (
    id             TEXT PRIMARY KEY,
    name           TEXT NOT NULL UNIQUE,
    password_hash  TEXT NOT NULL,
    is_admin       INTEGER NOT NULL DEFAULT 0,
    is_disabled    INTEGER NOT NULL DEFAULT 0,
    policy_json    TEXT NOT NULL DEFAULT '{}',
    config_json    TEXT NOT NULL DEFAULT '{}',
    last_login_at  INTEGER,
    last_active_at INTEGER,
    created_at     INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS access_tokens (
    token          TEXT PRIMARY KEY,
    user_id        TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_id      TEXT NOT NULL,
    device_name    TEXT NOT NULL DEFAULT '',
    app_name       TEXT NOT NULL DEFAULT '',
    app_version    TEXT NOT NULL DEFAULT '',
    created_at     INTEGER NOT NULL,
    last_active_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_tokens_user ON access_tokens(user_id);
CREATE INDEX IF NOT EXISTS idx_tokens_device ON access_tokens(device_id);

CREATE TABLE IF NOT EXISTS api_keys (
    token      TEXT PRIMARY KEY,
    app_name   TEXT NOT NULL,
    created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS user_item_data (
    user_id                 TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    item_id                 TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    played                  INTEGER NOT NULL DEFAULT 0,
    play_count              INTEGER NOT NULL DEFAULT 0,
    playback_position_ticks INTEGER NOT NULL DEFAULT 0,
    is_favorite             INTEGER NOT NULL DEFAULT 0,
    last_played_date        INTEGER,
    audio_stream_index      INTEGER,
    subtitle_stream_index   INTEGER,
    PRIMARY KEY (user_id, item_id)
);
CREATE INDEX IF NOT EXISTS idx_userdata_resume ON user_item_data(user_id, playback_position_ticks) WHERE playback_position_ticks > 0;
CREATE INDEX IF NOT EXISTS idx_userdata_fav ON user_item_data(user_id, is_favorite) WHERE is_favorite = 1;

CREATE TABLE IF NOT EXISTS display_preferences (
    id          TEXT NOT NULL,
    user_id     TEXT NOT NULL,
    client      TEXT NOT NULL,
    data_json   TEXT NOT NULL DEFAULT '{}',
    updated_at  INTEGER NOT NULL,
    PRIMARY KEY (id, user_id, client)
);

CREATE TABLE IF NOT EXISTS activity_logs (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT NOT NULL,
    overview    TEXT,
    short_overview TEXT,
    type        TEXT NOT NULL,
    item_id     TEXT,
    user_id     TEXT,
    severity    TEXT NOT NULL DEFAULT 'Info',
    created_at  INTEGER NOT NULL
);

CREATE VIRTUAL TABLE IF NOT EXISTS item_search_fts USING fts5(
    item_id UNINDEXED,
    title_raw,
    title_tokens,
    pinyin_full,
    pinyin_initials,
    people_names,
    tokenize = 'unicode61 remove_diacritics 2'
);
