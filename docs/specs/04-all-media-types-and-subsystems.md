# 04. 全媒体类型、Live TV、Sync、DLNA 与 Web UI 规格

[← 返回文档中心](../README.md)

除了电影和剧集，Emby 还涵盖音乐、有声书、照片、电子书、游戏、Live TV 录播、离线同步、DLNA 与 Web/GenericUI 管理界面。本文档补充这些子系统的核心实现思路。

---

## 1. 影视之外的全媒体类型支持

### 1.1 音乐与有声书（`/Albums` · `/Songs` · `/Artists` · `/Audio` · `/AudioBooks`）
- **标签解析**：使用纯 Go 音频标签库（如 `dhowden/tag`）读取 MP3/FLAC/M4A/OGG/Opus 的 `Title`, `Artist`, `AlbumArtist`, `Album`, `Track`, `Disc`, `Genre`, `Year` 及内嵌封面图片，无需频繁启动 `ffprobe`。
- **层级关系**：`MusicArtist` / `AlbumArtist` → `MusicAlbum` → `Audio`；有声书额外支持章节（Chapters）与断点续听（`UserData.PlaybackPositionTicks`）。
- **通用音频转码（`/Audio/{Id}/universal`）**：根据客户端支持的编码（`mp3`/`aac`/`flac`/`opus`）与 `MaxStreamingBitrate` 决定直连还是 FFmpeg 实时流式转码。
- **即时混音（InstantMix）**：`/Items/{Id}/InstantMix`、`/Artists/{Name}/InstantMix`、`/Albums/{Id}/InstantMix` 等 8 条路由，基于同流派（`MusicGenres`）、同艺术家与用户收藏权重随机抽样返回播放队列。
- **歌词支持**：优先读取同名 `.lrc` 文件或内嵌 `LYRICS`/`USLT` 标签。

### 1.2 照片与家庭视频（`Photo` · `PhotoAlbum` · `Video`）
- 扫描目录自动映射为 `PhotoAlbum` → `Photo`；读取 EXIF 信息（拍摄时间、相机型号、GPS、Orientation 方向自动纠正旋转）。
- 照片缩略图按需生成并走 `ImageCache` 落盘，支持 `/Items/{Id}/Images/Primary?maxWidth=...` 流畅滑动浏览。

### 1.3 合集、播放列表、预告片、游戏与电子书
- **合集（`/Collections`）**：支持基于 TMDB `CollectionId` 自动归组（如《指环王》系列）及手动创建/增删成员。
- **播放列表（`/Playlists`）**：维护有序 `entry_id`（支持同一曲目重复出现、支持 `/Playlists/{Id}/Items/{ItemId}/Move/{NewIndex}` 拖拽排序）。
- **预告片（`/Trailers`）**：支持识别本地 `*-trailer.*` 与 `trailers/` 子目录，关联至主电影的 `LocalTrailerCount`。
- **电子书/漫画与游戏（`/Games` · `/GameGenres` · `/Items` Book）**：按目录与元数据归类入库，提供下载接口 `/Items/{Id}/Download` 与封面图提取。

---

## 2. 电视直播与 DVR 录播（`05-live-tv`，71 条路由）

### 2.1 信号源与 EPG 管线
- **调谐器主机（TunerHost）**：原生支持 **M3U/M3U8 列表** 与 **HDHomeRun** 协议；解析 `#EXTINF` 中的 `tvg-id`, `tvg-name`, `tvg-logo`, `group-title` 入库 `livetv_channels`。
- **电子节目单（EPG）**：支持定时拉取 **XMLTV**（`.xml` / `.xml.gz`），按频道映射批量写入 `livetv_programs`，自动清理过期节目单。

### 2.2 直播流多路复用（Stream Sharing）
- 当多个客户端同时观看同一频道时，Ambra 内部的 `LiveStreamHub` **只向源站拉取一条上游流**，通过环形缓冲区（Ring Buffer）分发给所有订阅客户端，大幅降低带宽与调谐器占用；当最后一个观众离开后延迟 10 秒关闭上游连接。

### 2.3 定时录播（Timers & SeriesTimers）
- **单次录制（Timer）**：按 `StartDate - PrePaddingSeconds` 定时触发录制 Worker，将直播流通过 FFmpeg `-c copy` 写入目标录播目录（`.ts` / `.mkv`），录制结束后自动触发增量扫库入库为录播条目。
- **系列录制（SeriesTimer）**：每次 EPG 刷新后自动匹配规则（节目名、是否仅录新集、频道范围、最大保留集数），生成未来的单次录制 Timer。

---

## 3. 离线同步与通知（`06-sync-and-notifications`）

- **离线同步（`/Sync/*`，25 条）**：
  - 客户端创建 `SyncJob`（指定目标条目/剧集未看集数/画质 Profile），后台异步 Worker 调用转码引擎生成离线文件存入 `data/sync/`。
  - 客户端轮询 `/Sync/Items/Ready` 拉取已就绪文件，下载完成后调用 `/Sync/JobItems/{Id}/Transferred` 清理服务端暂存文件。
- **通知服务（`/Notifications/*`，13 条）**：
  - 订阅内部 `EventBus`（新片入库、播放开始/结束、登录失败、任务异常、服务器更新），按用户配置的通知规则分发到站内通知、Webhook、邮件或扩展插件。

---

## 4. DLNA 服务（`07-builtin-plugins/dlna.md`，26 条）

- **SSDP 发现**：启动 UDP `239.255.255.250:1900` 多播监听，响应 `M-SEARCH` 并定时发送 `NOTIFY ssdp:alive`，宣告 MediaServer 描述文档地址 `/Dlna/{UuId}/description.xml`。
- **UPnP 控制与事件**：
  - 解析 `ContentDirectory`（Browse / Search）与 `ConnectionManager` 的 SOAP XML 请求，复用到内部 `LibraryService` 查询媒体树。
  - 支持 `SUBSCRIBE` / `UNSUBSCRIBE` 特殊 HTTP 动词，维护局域网设备的事件回调 URL（`CALLBACK` Header）并在媒体库更新时推送 `SystemUpdateID`。
- **设备 Profile 匹配**：根据请求 `User-Agent` / `X-AV-Client-Info` 匹配 `dlna_profiles` 决定直连流还是转码流。

---

## 5. Web 前端、GenericUI 与系统运维（`01-system-and-server`）

### 5.1 `/web/*` 与 `/UI/*`（GenericUI）策略
- **管理后台与 Web 播放器（`/web/*`）**：
  - 方案 A（推荐长期）：独立开发现代轻量 Web UI（Vue 3 / Svelte + Vite，打包后通过 Go `embed.FS` 内嵌进二进制），直接调用 Ambra 自身的 Emby 兼容 API，彻底摆脱老旧前端包袱。
  - 方案 B（兼容托管）：允许用户挂载自定义的 `web/` 静态目录覆盖默认前端。
- **GenericUI（`/UI/View` & `/UI/Command`）**：
  - 实现轻量化的 `GenericEdit` 表单描述协议（将配置结构体转为 Emby GenericUI JSON 描述），确保官方客户端进入插件/系统设置页时也能正常渲染与提交表单。

### 5.2 初始化向导、计划任务与运维
- **初始化向导（`/Startup/*`）**：首次启动时 `server_config.is_startup_wizard_completed = false`，引导完成管理员账号、默认语言、首个媒体库与远程访问配置；完成后锁定向导接口。
- **计划任务引擎（`/ScheduledTasks/*`）**：内置核心任务（扫描媒体库、刷新元数据、刷新 EPG、清理转码缓存、清理旧日志、自动备份），实时汇报 `CurrentProgressPercentage` 并通过 WebSocket 推送给管理端。
- **日志与热备份**：日志按天轮转落盘供 `/System/Logs/{Name}/Lines` 分页读取；`/BackupRestore/*` 基于 SQLite Online Backup API 打包数据库与配置，支持不中断服务的热备份与一键还原。
