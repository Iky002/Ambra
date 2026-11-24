# 01. Ambra 分阶段开发计划（Phase 0 ~ Phase 6）

[← 返回文档中心](../README.md)

Ambra 的终极目标是 **100% 兼容 Emby Server 4.10.2.0 的全部 582 条 HTTP API**。为避免陷入“写半年还跑不起来”的泥潭，采用 **“先跑通核心观影闭环 → 逐域补齐 → 全量协议契约验证”** 的 7 阶段演进路线。

---

## 阶段总览

| 阶段 | 代号 | 核心目标 | 累计覆盖路由 | 关键验收里程碑 |
| :--- | :--- | :--- | :---: | :--- |
| **Phase 0** | 底座与契约骨架 | Go 工程骨架、SQLite WAL/连接池、双前缀路由、鉴权与 DTO 基础设施、统一插件接口骨架、自动化测试框架 | 0 | 服务可启停、健康检查通过、SQLite 迁移与插件注册表可跑 |
| **Phase 1** | MVP 观影闭环 + 首个 Web 客户端 | 认证、系统信息、插件化扫库（NFO + TMDB 内置插件）、核心 Items 查询、直连/302 播放、**现代 Web 客户端 v0.1（含 JASSUB ASS 与 PGS 零转码渲染）** | ~95 | **内置 Web 端** 及 **Infuse / VidHub / SenPlayer** 可登录、浏览海报墙、零转码看 ASS/PGS 字幕并记录进度 |
| **Phase 2** | 完整媒体库、中文检索与插件宿主 | FTS5 中文乱序/拼音搜索、多维筛选器、合集/播放列表、Web 端全页面 + 管理后台 + **`ffmpeg.wasm` 轻量解复用**、开放 `wazero` WASM / HTTP 外部插件加载 | ~270 | **Emby 官方 Web / App / Kodi** 与 Ambra Web 端可完整浏览、搜片、拖拽配置插件与刮削优先级 |
| **Phase 3** | 转码、HLS、音频与会话 | FFmpeg 动态 HLS 转码与硬解探测、内封字幕/字体抽轨、音频流、WebSocket 实时推送、远程遥控与设备管理 | ~375 | 任意浏览器无感回退硬解转码、多端进度实时同步、支持远程投屏控制 |
| **Phase 4** | 系统管理与自动化生态 | 计划任务引擎、初始化向导、环境浏览、日志、API Key、Webhooks 插件、备份还原插件、Packages/Plugins API | ~450 | **MoviePilot / Sonarr / Radarr** 全链路打通，管理后台核心配置完整可用 |
| **Phase 5** | Live TV、Sync 与通知 | M3U/XMLTV 直播、EPG、定时/系列录播、离线同步下载、通知中心与多渠道通知插件 | ~556 | 完整支持 IPTV 直播录制与客户端离线缓存 |
| **Phase 6** | DLNA 与 100% 全量契约收口 | DLNA SSDP/UPnP 服务（26 条）、Parties、OpenAPI/Swagger、39 条隐藏/别名路由全覆盖与回归压测 | **582 (100%)** | 全量 582 条路由契约测试通过，发布 v1.0.0 |

---

## 各阶段详细任务拆解

### Phase 0：底座与协议契约基础设施
- 初始化 Go Module（`cmd/ambra`、`internal/*`）、配置系统与优雅启停。
- 实现 SQLite `Store`（WAL 模式、1 写 N 读连接池、嵌入式 SQL 迁移管理）。
- 实现 Emby 协议兼容中间件：
  - `/emby/*` 与 `/*` 双前缀挂载 + 静态路径大小写不敏感匹配。
  - `X-Emby-Token` / `X-Emby-Authorization` / `?api_key=` 统一鉴权与角色校验。
  - 大小写不敏感 `QueryBinder`、逗号多值拆分、GUID 归一化、PascalCase JSON 序列化器。
- 搭建契约测试框架（`tests/compat`）：支持对比 Ambra 与 Emby 4.10.2.0 响应字段结构与状态码。

### Phase 1：MVP 核心观影闭环 + 首个现代 Web 客户端（约 95 条核心接口）
- **系统与认证**：`/System/Info/Public`、`/System/Info`、`/System/Ping`、`/Users/Public`、`/Users/AuthenticateByName`、`/Users/{Id}`、`/Auth/Keys`。
- **插件化扫库与元数据**：落地统一 `Plugin` 注册表与 `MetadataProvider` / `ImageProvider` 接口，将 **NFO** 与 **TMDB** 作为首批官方内置插件运行；完成本地文件夹与 `.strm` 增量扫描、电影/剧集层级入库与多版本合并。
- **核心查询**：`/Users/{UserId}/Views`、`/Users/{UserId}/Items`（基础过滤/排序/分页）、`/Users/{UserId}/Items/Latest`、`/Users/{UserId}/Items/Resume`、`/Shows/{Id}/Seasons` & `Episodes`、`/Shows/NextUp`、`/Items/{Id}`。
- **播放与图片**：`/Items/{Id}/PlaybackInfo`（STRM 302 + DirectPlay + ASS/PGS 外挂直出）、`/Videos/{Id}/stream`（HTTP Range）、`/Items/{Id}/Images/{Type}`（`singleflight` 防击穿 + 动态缩放缓存）、`/Sessions/Playing/*` 与 `/Users/{UserId}/PlayedItems/{Id}`。
- **现代 Web 客户端 v0.1（`embed.FS` 内嵌）**：实现登录页、首页（轮播/我的媒体/继续观看/最新添加）、媒体库海报墙、详情页（多版本/音轨/简繁字幕切换）与沉浸式播放器（集成 **JASSUB ASS/SSA 渲染** 与 **PGS Canvas 渲染**，避免字幕触发转码）。
- **验收标准**：直接打开浏览器访问 Ambra Web 端，以及使用 Infuse、VidHub、SenPlayer 实测登录、浏览海报墙、无转码播放带 ASS/PGS 字幕的影片并记录进度。

### Phase 2：完整媒体库、中文搜索、Web 管理端与外部插件宿主（累计 ~270 条）
- **补齐 `03-media-library` 全部 181 条路由**：
  - 完整动态查询引擎（支持 `Items` 全部几十个查询参数与 `Fields` 按需裁剪）。
  - SQLite **FTS5 中文增强检索**（`gse` 分词 + 拼音全拼/首字母 + 多关键词乱序）覆盖 `/Search/Hints` 与 `/Items?SearchTerm=...`。
  - 全量元数据筛选维度（54 条）、合集（`/Collections`）、播放列表（`/Playlists`）、媒体分类（`/Movies`、`/Shows`、`/Trailers`、`/Albums`、`/Songs`、`/AudioBooks`、`/Games`、`/Channels`）与虚拟媒体库管理（`/Library/*`）。
- **Web 端进阶能力与插件宿主**：
  - 播放器接入可选 **`ffmpeg.wasm` / WebCodecs** 客户端轻量解复用（MKV → fMP4）与自动降级机制。
  - 上线现代 Web 管理后台（媒体库管理、插件列表与刮削器优先级拖拽、基础系统设置）。
  - 上线 **`wazero`（WASM）与 HTTP 外部插件加载器**，支持社区编写豆瓣/Bangumi 刮削器、字幕源与 302 解析器。
- **验收标准**：Emby 官方 Web/App、Kodi 与 Ambra 自研 Web 端均可完整浏览所有分类、筛选器、演职员页、合集与中文模糊搜索，并可通过插件系统热插拔刮削器。

### Phase 3：转码、HLS、音频流与实时会话（累计 ~375 条）
- **补齐 `04-streaming-and-media` 全部 63 条路由**：
  - FFmpeg 动态 HLS 切片引擎（`/Videos/{Id}/master.m3u8`、`main.m3u8`、`live.m3u8`、`hls1/*`）、Seek 重启、空闲超时回收与 `/Videos/ActiveEncodings`。
  - 硬件加速探测与编码配置（`/Encoding/*` 13 条：VAAPI / QSV / NVENC / VideoToolbox）。
  - 内封字幕提取缓存、SRT/ASS 转 VTT、外挂与远端字幕搜索下载（`SubtitleService` 16 条）、简繁中文字幕轨道标签增强。
  - 完整音频流与通用音频转码（`/Audio/*` 17 条）及 `/LiveStreams/*`。
- **补齐 `02-users-and-auth` 剩余路由**：
  - WebSocket 实时中心（`/embywebsocket`），推送 `LibraryChanged`、`UserDataChanged`、`Sessions`、`Playstate` 与远程遥控指令。
  - 完整设备管理（`/Devices/*`）、用户策略/权限/偏好（`/Users/*`、`/UserSettings/*`、`/DisplayPreferences/*`）与 `/Playback/BitrateTest`。

### Phase 4：系统管理、计划任务与自动化生态（累计 ~450 条）
- **补齐 `01-system-and-server` 全部 71 条路由**：
  - 系统配置读写（`/System/Configuration*`）、活动日志、服务器日志文件与按行查询（`/System/Logs*`）、品牌定制（`/Branding/*`）、本地化（`/Localization/*`）、许可兼容接口（`/Registrations/*`、`/Features`）。
  - 文件系统目录浏览（`/Environment/*`）、计划任务引擎（`/ScheduledTasks/*`）、初始化向导（`/Startup/*`）、Web 前端与 GenericUI（`/web/*`、`/UI/*`）。
- **补齐 `06-sync-and-notifications` 部分与 `07-builtin-plugins` 自动化接口**：
  - 插件与包列表映射（`/Plugins/*`、`/Packages/*`）。
  - 内置 Webhooks（`/Webhooks/*`）与热备份还原（`/BackupRestore/*`）。
  - 与 **MoviePilot / Sonarr / Radarr / Emby2Alist** 联调通过。

### Phase 5：Live TV、离线同步、通知与 WASM 扩展（累计 ~556 条）
- **补齐 `05-live-tv` 全部 71 条路由**：
  - M3U 调谐器主机、XMLTV EPG 定时拉取、频道管理、节目单查询、单次/系列定时录播与录播文件归档。
- **补齐 `06-sync-and-notifications` 剩余路由**：
  - 离线同步与预转码队列（`/Sync/*` 25 条）。
  - 通知类型、用户通知规则与测试发送（`/Notifications/*` 13 条）。
- **Ambra 扩展宿主**：上线基于 `wazero` 的 WASM 插件加载器与外部 HTTP Provider 协议。

### Phase 6：DLNA、Parties 与 100% 协议全量收口（582 / 582）
- **补齐 `07-builtin-plugins/dlna.md` 全部 26 条路由**：
  - SSDP 设备发现、UPnP ContentDirectory / ConnectionManager 控制、设备配置文件管理及 `SUBSCRIBE`/`UNSUBSCRIBE` 事件推送。
- **补齐长尾接口**：
  - 共同观看派对（`/Parties/*` 7 条）、Emby Connect 兼容（`/Connect/*` 2 条）、`/swagger*` & `/openapi*` 文档生成。
  - 逐一核对全部 39 条 `IsHidden = true` 隐藏接口与历史别名路由。
- **全量回归与性能基准**：
  - 针对 582 条路由运行自动化契约测试（路由可达性、鉴权状态码、JSON Schema 校验）。
  - 50 万条目 SQLite 压测（首页加载 < 50ms，中文 FTS5 搜索 < 20ms，空载内存 < 60MB）。
  - 输出多平台二进制与 Docker 镜像，发布 **Ambra v1.0.0**。
