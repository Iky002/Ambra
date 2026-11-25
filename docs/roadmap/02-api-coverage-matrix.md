# Emby 4.10.2.0 全量 API 兼容矩阵

[← 返回文档中心](../README.md)

本表汇总 `emby-4.10.2.0-api/` 中的 **7 个模块目录、23 份子文档** 与当前契约测试覆盖状态（`tests/compat` 校验全部 583 条 Method+Path 组合）。

> 状态图例：`⬜ 待开发` · `🟡 部分实现` · `✅ 已完成`

---

## 全量模块覆盖矩阵（总计 582 条）

| 模块目录 | API 参考子文档 | 路由前缀 | 路由数 | 核心阶段 | 完整收口 | 当前状态 |
| :--- | :--- | :--- | :---: | :---: | :---: | :---: |
| **01. 系统与服务器管理** *(71)* | [system-info.md](../emby-4.10.2.0-api/01-system-and-server/system-info.md) | `/System`, `/Features`, `/Registrations`, `/Appstore`, `/Branding`, `/Localization` | 32 | Phase 1 | Phase 4 | ✅ 已完成 |
| | [environment-and-tasks.md](../emby-4.10.2.0-api/01-system-and-server/environment-and-tasks.md) | `/Environment`, `/ScheduledTasks`, `/Startup`, `/Packages` | 26 | Phase 4 | Phase 4 | ✅ 已完成 |
| | [web-and-openapi.md](../emby-4.10.2.0-api/01-system-and-server/web-and-openapi.md) | `/web`, `/UI`, `/swagger*`, `/openapi*`, `/robots.txt`, `/favicon.ico` | 13 | Phase 4 | Phase 6 | ✅ 已完成 |
| **02. 用户、认证、设备与会话** *(118)* | [auth-and-connect.md](../emby-4.10.2.0-api/02-users-and-auth/auth-and-connect.md) | `/Auth`, `/Connect` | 7 | Phase 1 | Phase 6 | ✅ 已完成 |
| | [users.md](../emby-4.10.2.0-api/02-users-and-auth/users.md) | `/Users`, `/UserSettings`, `/DisplayPreferences` | 76 | Phase 1 | Phase 3 | ✅ 已完成 |
| | [sessions-and-playstate.md](../emby-4.10.2.0-api/02-users-and-auth/sessions-and-playstate.md) | `/Sessions`, `/Playback`, `/Parties` | 27 | Phase 1 | Phase 6 | ✅ 已完成 |
| | [devices.md](../emby-4.10.2.0-api/02-users-and-auth/devices.md) | `/Devices` | 8 | Phase 3 | Phase 3 | ✅ 已完成 |
| **03. 媒体库、条目与元数据** *(181)* | [items-and-search.md](../emby-4.10.2.0-api/03-media-library/items-and-search.md) | `/Items`, `/Search`, `/Providers` | 74 | Phase 1 | Phase 2 | ✅ 已完成 |
| | [library-management.md](../emby-4.10.2.0-api/03-media-library/library-management.md) | `/Library`, `/Libraries` | 21 | Phase 2 | Phase 2 | ✅ 已完成 |
| | [collections-and-playlists.md](../emby-4.10.2.0-api/03-media-library/collections-and-playlists.md) | `/Collections`, `/Playlists` | 14 | Phase 2 | Phase 2 | ✅ 已完成 |
| | [media-categories.md](../emby-4.10.2.0-api/03-media-library/media-categories.md) | `/Movies`, `/Shows`, `/Trailers`, `/Albums`, `/Songs`, `/AudioBooks`, `/Games`, `/Channels` | 18 | Phase 1 | Phase 2 | ✅ 已完成 |
| | [metadata-and-filters.md](../emby-4.10.2.0-api/03-media-library/metadata-and-filters.md) | `/Artists`, `/Persons`, `/Genres`, `/Studios`, `/Tags`, `/Years`, 编解码器筛选器等 17 个前缀 | 54 | Phase 2 | Phase 2 | ✅ 已完成 |
| **04. 音视频流媒体、转码与图片** *(63)* | [videos-and-hls.md](../emby-4.10.2.0-api/04-streaming-and-media/videos-and-hls.md) | `/Videos`, `/LiveStreams` | 32 | Phase 1 | Phase 3 | ✅ 已完成 |
| | [audio.md](../emby-4.10.2.0-api/04-streaming-and-media/audio.md) | `/Audio` | 17 | Phase 3 | Phase 3 | ✅ 已完成 |
| | [encoding.md](../emby-4.10.2.0-api/04-streaming-and-media/encoding.md) | `/Encoding` | 13 | Phase 3 | Phase 3 | ✅ 已完成 |
| | [images.md](../emby-4.10.2.0-api/04-streaming-and-media/images.md) | `/Images`（各资源图片路由归属对应模块） | 1 | Phase 1 | Phase 1 | ✅ 已完成 |
| **05. 电视直播与录制** *(71)* | [livetv.md](../emby-4.10.2.0-api/05-live-tv/livetv.md) | `/LiveTv` | 71 | Phase 5 | Phase 5 | ✅ 已完成 |
| **06. 同步、通知与插件管理** *(47)* | [sync.md](../emby-4.10.2.0-api/06-sync-and-notifications/sync.md) | `/Sync` | 25 | Phase 5 | Phase 5 | ✅ 已完成 |
| | [notifications.md](../emby-4.10.2.0-api/06-sync-and-notifications/notifications.md) | `/Notifications` | 13 | Phase 5 | Phase 5 | ✅ 已完成 |
| | [plugins.md](../emby-4.10.2.0-api/06-sync-and-notifications/plugins.md) | `/Plugins` | 9 | Phase 4 | Phase 5 | ✅ 已完成 |
| **07. 内置插件 API** *(31)* | [dlna.md](../emby-4.10.2.0-api/07-builtin-plugins/dlna.md) | `/Dlna` (`Emby.Dlna`) | 26 | Phase 6 | Phase 6 | ✅ 已完成 |
| | [backup-restore.md](../emby-4.10.2.0-api/07-builtin-plugins/backup-restore.md) | `/BackupRestore` (`MBBackup`) | 3 | Phase 4 | Phase 4 | ✅ 已完成 |
| | [webhooks.md](../emby-4.10.2.0-api/07-builtin-plugins/webhooks.md) | `/Webhooks` (`Emby.Webhooks`) | 2 | Phase 4 | Phase 4 | ✅ 已完成 |
| **总计** | **23 份子文档** | **71 个一级路由前缀** | **582** | **Phase 1** | **Phase 6** | **全部 7 大业务域语义实现 + 583/583 契约测试全通过（CI 覆盖率门禁 ≥90%）** |

---

## 契约验收标准（每条路由达到 ✅ 的条件）

1. **路由匹配**：同时支持 `/` 与 `/emby/` 前缀，静态路径大小写不敏感，HTTP 动词与 4.10.2.0 一致。
2. **鉴权一致**：准确执行 `[Authenticated]` 策略（公开 / 登录用户 / Admin 角色 / 缺失 Token 返回 `401` / 权限不足返回 `403`）。
3. **参数绑定**：支持大小写不敏感 Query、逗号多值、GUID 归一化，并校验必填参数。
4. **语义与功能一致**：返回状态码、PascalCase JSON 字段与最终业务效果与 Emby 4.10.2.0 语义等价（内部实现可采用更优算法与架构），并通过 `tests/compat` 自动化测试。
