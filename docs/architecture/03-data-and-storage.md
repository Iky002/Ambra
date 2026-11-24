# 03. 数据架构与 SQLite 存储设计

[← 返回文档中心](../README.md)

## 1. SQLite 引擎架构（彻底解决原版卡顿与锁库）

Ambra 默认采用单文件嵌入式 **SQLite**（纯 Go `modernc.org/sqlite`，零 CGO 交叉编译），通过以下机制确保 50 万条目级媒体库依然毫秒级响应：

### 1.1 读写连接池分离
- **写连接池（Writer Pool）**：固定 `SetMaxOpenConns(1)`，所有写操作串行排队，从根源消灭 `SQLITE_BUSY` 锁冲突；扫库与批量元数据入库走每 200~500 条一批的事务提交。
- **读连接池（Reader Pool）**：开启 `SetMaxOpenConns(runtime.NumCPU() * 2)`，在 WAL 模式下完全无阻塞并发读。

### 1.2 必配 PRAGMA 参数
```sql
PRAGMA journal_mode = WAL;
PRAGMA synchronous = NORMAL;
PRAGMA busy_timeout = 5000;
PRAGMA foreign_keys = ON;
PRAGMA temp_store = MEMORY;
PRAGMA cache_size = -64000;        -- 64MB 页缓存
PRAGMA mmap_size = 268435456;      -- 256MB 内存映射（按机器内存可配）
```

### 1.3 可扩展抽象
所有数据访问封装在 `internal/store` 的 `Repository` 接口后，先深耕 SQLite 调优；未来如需支持超大规模多节点部署，可无缝新增 `internal/store/postgres` 实现。

---

## 2. 核心表结构规划

围绕 Emby 582 条 API 的核心实体，数据库拆分为 7 组表（避免原版单宽表膨胀）：

### 2.1 媒体库与条目核心（`03-media-library`）
- **`libraries`**：虚拟媒体库（电影/剧集/音乐/混合/有声书/照片）、集合类型、路径列表、刮削器与扫描策略配置。
- **`items`**：所有媒体实体主表（Movie / Series / Season / Episode / BoxSet / Playlist / MusicAlbum / Audio / Folder / Channel 等）。
  - 高频过滤与排序列独立建索引：`id`（UUID）、`internal_id`（自增整型，兼容数字 ID）、`library_id`、`parent_id`、`series_id`、`season_id`、`type`、`name`、`original_title`、`sort_name`、`index_number`（集数）、`parent_index_number`（季数）、`production_year`、`premiere_date`、`community_rating`、`official_rating`、`runtime_ticks`、`path`、`is_folder`、`is_strm`、`date_created`、`date_modified`。
  - 低频扩展元数据（Overview、ProviderIds、ExternalUrls、Chapters、RemoteTrailers 等）存放在 `extra_json` 列，减少主表行宽。
- **`item_media_sources` & `item_media_streams`**：
  - 记录每个条目的物理文件/STRM 源、容器格式、码率、大小、文件修改时间。
  - 细粒度记录每条视频/音频/字幕轨道（`codec`, `width`, `height`, `video_range`如 HDR/DV, `language`, `display_title`, `sub_variant`如 CHS/CHT 简繁标识, `is_external`, `path`）。
- **`item_images`**：记录本地/远端图片路径、类型（Primary/Backdrop/Logo/Thumb/Banner）、宽高、Tag（用于 `ETag` 与缓存失效）。

### 2.2 多对多元数据维度（`metadata-and-filters`）
- **`people`** + **`item_people`**（演职员、角色名、类型 Actor/Director/Writer）
- **`genres`** + **`item_genres`**（电影/音乐/游戏流派）
- **`studios`** + **`item_studios`**（制片厂/发行商）
- **`tags`** + **`item_tags`**（自定义标签）
- **`collection_items`** & **`playlist_items`**（合集与播放列表有序关联）

> 通过规范化关联表 + 覆盖索引，`/Genres`、`/Studios`、`/Persons`、`/Years`、`/VideoCodecs` 等 54 条筛选维度接口全部可走索引聚合。

### 2.3 中文增强全文检索（`FTS5`）
- **`item_search_fts`**（SQLite FTS5 虚拟表）：
  - 存储字段：`item_id`、`name_tokens`（原文 + `gse` 分词 + Bigram）、`original_title_tokens`、`pinyin_full`（全拼）、`pinyin_initials`（首字母，如 `llq` 匹配 `流浪地球`）、`people_names`。
  - 支持中英文混排与多关键字任意乱序查询（如搜 `"地球 流浪 4k"` 或 `"lldq"` 均可毫秒命中），彻底替代慢查询 `LIKE '%...%'`。

### 2.4 用户、鉴权与观影数据（`02-users-and-auth`）
- **`users`**：账号、Argon2id 密码哈希、角色（Admin/User）、策略 JSON（家长控制、可访问媒体库白名单、转码权限）、配置 JSON。
- **`api_keys` & `access_tokens`**：API Key 与设备登录 Token（绑定 `user_id`, `device_id`, `app_name`, `device_name`, `last_activity`）。
- **`devices`**：设备注册信息、自定义名称、能力描述（`DeviceProfile`）。
- **`user_item_data`**：主键 `(user_id, item_id)`，记录 `played`、`play_count`、`playback_position_ticks`、`is_favorite`、`last_played_date`、`audio_stream_index`、`subtitle_stream_index`。
- **`display_preferences`**：用户界面显示偏好。

### 2.5 系统、日志与计划任务（`01-system-and-server`）
- **`server_config`**：KV 配置表（对应 `/System/Configuration` 与 `/System/Configuration/{Key}`）。
- **`activity_logs`**：系统与用户活动日志（对应 `/System/ActivityLog/Entries`）。
- **`scheduled_tasks`**：计划任务触发器配置与最近执行历史。

### 2.6 Live TV、Sync、通知与内置插件（`05` / `06` / `07`）
- **`livetv_tuners` / `livetv_channels` / `livetv_programs` / `livetv_timers` / `livetv_series_timers`**
- **`sync_jobs` / `sync_job_items`**
- **`notifications` / `webhooks`**
- **`dlna_profiles`**

---

## 3. 文件与缓存目录布局

```text
data/
├── ambra.db                 # SQLite 主库（含 .db-wal / .db-shm）
├── config/                  # 导出的 JSON/XML 配置与备份
├── cache/
│   ├── images/              # 按 hash 分桶的缩放/转码图片缓存
│   ├── subtitles/           # 提取的内封字幕与下载的外挂字幕缓存
│   └── transcoding-temp/    # FFmpeg HLS 临时切片（会话结束自动清理）
├── metadata/                # 刮削落盘的演职员头像、合集封面等
├── logs/                    # 滚动日志文件（供 /System/Logs 接口读取）
└── plugins/                 # Ambra WASM / 脚本扩展包目录
```
