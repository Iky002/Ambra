# 01. 数据库详细 Schema、动态查询与中文检索规格

[← 返回文档中心](../README.md)

本文档给出 Ambra 的 SQLite 核心表结构（DDL 草案）、树形层级加速方案、`/Items` 动态查询构造器思路以及中文模糊检索算法。

---

## 1. 核心 SQLite 表结构（DDL 草案）

### 1.1 媒体库与核心条目表
```sql
-- 虚拟媒体库
CREATE TABLE libraries (
    id              TEXT PRIMARY KEY,          -- UUID (32位无横杠小写)
    name            TEXT NOT NULL,
    collection_type TEXT NOT NULL,             -- movies / tvshows / music / mixed / books / homevideos / boxsets / playlists
    paths_json      TEXT NOT NULL DEFAULT '[]',
    options_json    TEXT NOT NULL DEFAULT '{}',
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL
);

-- 媒体条目主表
CREATE TABLE items (
    id                   TEXT PRIMARY KEY,     -- UUID (32位无横杠)
    internal_id          INTEGER UNIQUE,       -- 兼容数字 ID 查询
    library_id           TEXT NOT NULL,
    parent_id            TEXT,                 -- 直接父节点 (Season / Series / Folder / Library)
    series_id            TEXT,                 -- 剧集冗余外键，加速 Episode/Season 查询
    season_id            TEXT,                 -- 季冗余外键
    type                 TEXT NOT NULL,        -- Movie / Series / Season / Episode / Audio / MusicAlbum / BoxSet / Playlist / Photo / Video / Folder / LiveTvChannel ...
    media_type           TEXT,                 -- Video / Audio / Photo / Book
    is_folder            INTEGER NOT NULL DEFAULT 0,
    is_strm              INTEGER NOT NULL DEFAULT 0,
    name                 TEXT NOT NULL,
    original_title       TEXT,
    sort_name            TEXT NOT NULL,
    index_number         INTEGER,              -- 集号 / 音轨号
    parent_index_number  INTEGER,              -- 季号 / 碟号
    production_year      INTEGER,
    premiere_date        INTEGER,              -- Unix timestamp (UTC)
    end_date             INTEGER,
    community_rating     REAL,
    critic_rating        REAL,
    official_rating      TEXT,                 -- PG-13 / TV-MA 等（用于家长控制过滤）
    runtime_ticks        INTEGER,
    container            TEXT,
    path                 TEXT,
    file_size            INTEGER,
    file_mtime           INTEGER,
    etag                 TEXT NOT NULL,
    extra_json           TEXT NOT NULL DEFAULT '{}', -- Overview, ProviderIds, Chapters, Taglines, ProductionLocations 等
    date_created         INTEGER NOT NULL,
    date_modified        INTEGER NOT NULL
);

CREATE INDEX idx_items_library_type ON items(library_id, type);
CREATE INDEX idx_items_parent ON items(parent_id, type, index_number);
CREATE INDEX idx_items_series ON items(series_id, type, parent_index_number, index_number);
CREATE INDEX idx_items_sort_name ON items(type, sort_name);
CREATE INDEX idx_items_date_created ON items(library_id, type, date_created DESC);
CREATE INDEX idx_items_year ON items(type, production_year);
CREATE INDEX idx_items_path ON items(path);

-- 树形祖先闭包表：解决 Recursive=true + ParentId=xxx 时的极速查询，彻底避免递归 CTE 性能抖动
CREATE TABLE item_ancestors (
    ancestor_id   TEXT NOT NULL,
    descendant_id TEXT NOT NULL,
    depth         INTEGER NOT NULL,
    PRIMARY KEY (ancestor_id, descendant_id)
);
CREATE INDEX idx_ancestors_desc ON item_ancestors(descendant_id);
```

### 1.2 媒体源、音视频/字幕流与图片表
```sql
-- 多版本媒体源（同电影 1080p + 4K 多文件，或 STRM 源）
CREATE TABLE item_media_sources (
    id                TEXT PRIMARY KEY,
    item_id           TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    name              TEXT NOT NULL,
    path              TEXT NOT NULL,
    protocol          TEXT NOT NULL DEFAULT 'File', -- File / Http / Rtsp
    container         TEXT,
    size              INTEGER,
    bitrate           INTEGER,
    runtime_ticks     INTEGER,
    is_remote         INTEGER NOT NULL DEFAULT 0,
    supports_direct_play   INTEGER NOT NULL DEFAULT 1,
    supports_direct_stream INTEGER NOT NULL DEFAULT 1,
    supports_transcoding   INTEGER NOT NULL DEFAULT 1,
    extra_json        TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX idx_media_sources_item ON item_media_sources(item_id);

-- 音视频与字幕轨道（支持编解码器筛选 + 简繁中文字幕区分）
CREATE TABLE item_media_streams (
    media_source_id   TEXT NOT NULL REFERENCES item_media_sources(id) ON DELETE CASCADE,
    item_id           TEXT NOT NULL,
    stream_index      INTEGER NOT NULL,
    stream_type       TEXT NOT NULL,           -- Video / Audio / Subtitle / EmbeddedImage
    codec             TEXT,
    language          TEXT,                    -- ISO 639-2 (chi/zho/eng/jpn)
    sub_variant       TEXT,                    -- 扩展字段: chs / cht / bilingual，解决 4.10+ 简繁不分问题
    display_title     TEXT,
    title             TEXT,
    width             INTEGER,
    height            INTEGER,
    video_range       TEXT,                    -- SDR / HDR10 / DolbyVision / HLG
    audio_layout      TEXT,                    -- 5.1 / 7.1 / stereo / atmos
    channels          INTEGER,
    sample_rate       INTEGER,
    bitrate           INTEGER,
    is_default        INTEGER NOT NULL DEFAULT 0,
    is_forced         INTEGER NOT NULL DEFAULT 0,
    is_external       INTEGER NOT NULL DEFAULT 0,
    path              TEXT,                    -- 外挂字幕文件路径
    PRIMARY KEY (media_source_id, stream_index)
);
CREATE INDEX idx_streams_item_type ON item_media_streams(item_id, stream_type);
CREATE INDEX idx_streams_filter ON item_media_streams(stream_type, codec, language, video_range);

-- 图片元数据表
CREATE TABLE item_images (
    item_id     TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    image_type  TEXT NOT NULL,                 -- Primary / Backdrop / Logo / Thumb / Banner / Art / Disc / Chapter
    image_index INTEGER NOT NULL DEFAULT 0,
    path        TEXT NOT NULL,
    tag         TEXT NOT NULL,                 -- 内容 hash，用作 ETag 与 URL ?tag=
    width       INTEGER,
    height      INTEGER,
    blurhash    TEXT,
    PRIMARY KEY (item_id, image_type, image_index)
);
```

### 1.3 元数据关联表与用户观影数据表
```sql
-- 通用分类维度表（Genre / MusicGenre / GameGenre / Studio / Tag / Person）
CREATE TABLE people (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE,
    sort_name  TEXT NOT NULL,
    extra_json TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE item_people (
    item_id    TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    person_id  TEXT NOT NULL REFERENCES people(id) ON DELETE CASCADE,
    role       TEXT,
    person_type TEXT NOT NULL,                 -- Actor / Director / Writer / Producer / GuestStar
    sort_order INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (item_id, person_id, person_type)
);
CREATE INDEX idx_item_people_person ON item_people(person_id);

-- 统一标签/流派/工作室关联表（type = 'genre' | 'music_genre' | 'game_genre' | 'studio' | 'tag'）
CREATE TABLE taxonomies (
    id        TEXT PRIMARY KEY,
    kind      TEXT NOT NULL,
    name      TEXT NOT NULL,
    sort_name TEXT NOT NULL,
    UNIQUE(kind, name)
);

CREATE TABLE item_taxonomies (
    item_id     TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    taxonomy_id TEXT NOT NULL REFERENCES taxonomies(id) ON DELETE CASCADE,
    PRIMARY KEY (item_id, taxonomy_id)
);
CREATE INDEX idx_item_tax_tax ON item_taxonomies(taxonomy_id);

-- 合集与播放列表有序成员
CREATE TABLE item_collections (
    container_id TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE, -- BoxSet / Playlist ID
    item_id      TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    position     INTEGER NOT NULL,
    entry_id     TEXT NOT NULL,               -- 兼容 PlaylistItemId 删除语义
    PRIMARY KEY (container_id, entry_id)
);

-- 用户与观影进度
CREATE TABLE users (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,              -- Argon2id
    is_admin      INTEGER NOT NULL DEFAULT 0,
    is_disabled   INTEGER NOT NULL DEFAULT 0,
    policy_json   TEXT NOT NULL DEFAULT '{}', -- 允许的 library_ids、最高分级、禁止标签、转码权限
    config_json   TEXT NOT NULL DEFAULT '{}',
    last_login_at INTEGER,
    last_active_at INTEGER,
    created_at    INTEGER NOT NULL
);

CREATE TABLE user_item_data (
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
CREATE INDEX idx_userdata_resume ON user_item_data(user_id, playback_position_ticks) WHERE playback_position_ticks > 0;
CREATE INDEX idx_userdata_fav ON user_item_data(user_id, is_favorite) WHERE is_favorite = 1;
```

---

## 2. 中文增强全文检索（FTS5 实现思路）

原版 Emby 在 SQLite 中使用 `LIKE '%keyword%'` 导致大库极慢且不支持中文乱序。Ambra 使用独立 FTS5 虚拟表：

```sql
CREATE VIRTUAL TABLE item_search_fts USING fts5(
    item_id UNINDEXED,
    title_raw,          -- 原始标题 + 英文原名 + 别名
    title_tokens,       -- gse 分词结果 + 中文 2-gram (Bigram) 切分
    pinyin_full,        -- 全拼（如 "liu lang di qiu"）
    pinyin_initials,    -- 首字母（如 "lldq"）
    people_names,       -- 演员/导演名
    tokenize = 'unicode61 remove_diacritics 2'
);
```

### 查询解析流程（`/Search/Hints` 与 `/Items?SearchTerm=...`）
1. **输入归一化**：去除多余标点，繁转简（OpenCC 映射表），按空白拆分为多个子词 `[t1, t2, ...]`。
2. **多词乱序构造**：对每个子词 `ti` 生成前缀匹配表达式（如 `ti*`），多个子词之间用 `AND` 连接：
   - 搜 `"地球 流浪"` → `MATCH '地球* AND 流浪*'` → 命中《流浪地球》《流浪地球2》。
   - 搜 `"lldq"` → 命中 `pinyin_initials` 列。
   - 搜 `"诺兰 盗梦"` → 同时跨 `people_names` 与 `title_tokens` 命中《盗梦空间》。
3. **权重排序**：利用 FTS5 内置 `bm25(item_search_fts, 0, 10.0, 5.0, 3.0, 2.0, 1.0)` 将标题精确命中排在最前。

---

## 3. `/Items` 动态查询构造器设计

`/Users/{UserId}/Items` 与 `/Items` 是全系统调用频次最高、参数最复杂的接口（含 60+ 查询参数）。在 `internal/store/sqlite/item_query.go` 中采用**可组合的 SQL Builder**：

1. **强制安全边界**：若带 `userId`，先注入用户的 `policy.EnabledFolders`（媒体库白名单）与 `policy.MaxParentalRating` / `BlockedTags`（家长控制）。
2. **层级过滤**：
   - `Recursive=false`：`WHERE i.parent_id = ?`
   - `Recursive=true` + `ParentId != ""`：`INNER JOIN item_ancestors a ON a.descendant_id = i.id AND a.ancestor_id = ?`
3. **动态条件拼接**：
   - `IncludeItemTypes` / `ExcludeItemTypes` → `i.type IN (...)`
   - `IsPlayed` / `IsUnplayed` / `IsFavorite` / `IsResumable` → `LEFT JOIN user_item_data u ON u.item_id = i.id AND u.user_id = ?`
   - `Genres` / `Studios` / `Tags` / `PersonIds` → `EXISTS (SELECT 1 FROM item_taxonomies ...)` 避免 `JOIN` 导致分页重复行。
   - `VideoCodecs` / `AudioCodecs` / `SubtitleCodecs` / `Is4K` / `IsHD` → `EXISTS (SELECT 1 FROM item_media_streams ...)`。
4. **字段按需水合（`Fields` 参数）**：
   - 列表页（如首页海报墙）默认只查 `items` + `user_item_data` + `Primary/Backdrop` 图片 Tag，**不查 `MediaSources` / `People` / `Overview`**；
   - 仅当请求 `Fields` 包含 `MediaSources`、`People`、`Genres`、`Overview`、`Path` 等，或请求详情页 `/Items/{Id}` 时才批量 `IN (...)` 水合，彻底杜绝 N+1 查询。
5. **总数优化**：当 `EnableTotalRecordCount=false`（很多客户端分页加载会传）时直接跳过 `COUNT(*)`。

---

## 4. 核心 DTO 兼容要点

- **`BaseItemDto`**：核心万能载体，需注意：
  - `Id` 输出 32 位小写无横杠字符串；`ServerId` 必须全局稳定（首次启动生成并持久化到 `server_config`）。
  - 剧集（`Series`）的 `UserData.UnplayedItemCount` 需准确汇总未看集数，客户端角标依赖此字段。
  - `ImageTags` 为 `map[string]string`（如 `{"Primary": "abc123"}`），`BackdropImageTags` 为 `[]string`。
- **`MediaSourceInfo` & `MediaStream`**：确保 `SupportsDirectPlay`、`SupportsDirectStream`、`SupportsTranscoding` 与 `DirectStreamUrl` / `TranscodingUrl` 逻辑自洽。
