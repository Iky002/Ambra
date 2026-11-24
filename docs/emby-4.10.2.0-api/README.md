# Emby Server 4.10.2.0 完整 HTTP API 文档

本文档通过反射扫描 `embyserver-netcore_4.10.2.0.zip` 中的全部核心程序集（`system/*.dll`）与内置插件（`system/plugins/*.dll`）自动提取生成，涵盖全部 **[Route]** 定义的 HTTP REST / 流媒体 / UPnP 接口。

## 概览统计

- **目标版本**：Emby Server `4.10.2.0` (`.NET 8.0`)
- **总路由数**：**582** 条（核心程序集 `551` 条 + 内置插件 `31` 条）
- **鉴权分布**：需认证 `521` 条，公开接口 `61` 条
- **隐藏接口**（`IsHidden = true`）：`39` 条
- **一级路径前缀**：`71` 个，共整理为 **7** 个功能目录、**23** 篇子文档

## 通用调用说明

- **基础地址**：默认 HTTP `http://<host>:8096`，HTTPS `https://<host>:8920`。Emby 同时支持带 `/emby` 前缀（推荐，例如 `/emby/System/Info`）或不带前缀（例如 `/System/Info`）访问。
- **认证方式**：
  - Query 参数：`?api_key=<token>`
  - HTTP Header：`X-Emby-Token: <token>`
  - 完整客户端头：`X-Emby-Authorization: MediaBrowser Client="...", Device="...", DeviceId="...", Version="...", Token="<token>"`
- **在线交互文档**：运行中的 Emby 实例本身也提供 `/swagger`、`/swagger.json`、`/openapi` 与 `/openapi.json` 接口。

## 文档目录

### 01. 系统与服务器管理（`01-system-and-server/` — 共 71 条）

系统信息、配置、日志、许可、品牌、环境、计划任务、启动向导、安装包与 Web/OpenAPI 入口。

| 文档 | 包含路由前缀 | 接口数 |
| :--- | :--- | :---: |
| [系统信息、配置、许可与本地化](01-system-and-server/system-info.md) | `/Appstore`, `/Branding`, `/Features`, `/Localization`, `/Registrations`, `/System` | 32 |
| [环境文件系统、计划任务、启动向导与安装包](01-system-and-server/environment-and-tasks.md) | `/Environment`, `/Packages`, `/ScheduledTasks`, `/Startup` | 26 |
| [Web 前端资源、通用 UI 与 OpenAPI/Swagger](01-system-and-server/web-and-openapi.md) | `/favicon.ico`, `/openapi`, `/openapi.json`, `/robots.txt`, `/swagger`, `/swagger.json`, `/UI`, `/web` | 13 |

### 02. 用户、认证、设备与会话（`02-users-and-auth/` — 共 118 条）

用户管理、API Key 认证、Emby Connect、用户设置、播放会话、播放状态与设备管理。

| 文档 | 包含路由前缀 | 接口数 |
| :--- | :--- | :---: |
| [API 密钥认证与 Emby Connect](02-users-and-auth/auth-and-connect.md) | `/Auth`, `/Connect` | 7 |
| [用户管理、用户设置与显示偏好](02-users-and-auth/users.md) | `/DisplayPreferences`, `/Users`, `/UserSettings` | 76 |
| [会话控制、播放状态与共同观看派对](02-users-and-auth/sessions-and-playstate.md) | `/Parties`, `/Playback`, `/Sessions` | 27 |
| [设备管理](02-users-and-auth/devices.md) | `/Devices` | 8 |

### 03. 媒体库、条目与元数据（`03-media-library/` — 共 181 条）

媒体库结构管理、媒体条目查询与刷新、搜索、合集、播放列表、各类媒体分类与筛选维度。

| 文档 | 包含路由前缀 | 接口数 |
| :--- | :--- | :---: |
| [媒体条目（Items）、搜索与刮削提供商](03-media-library/items-and-search.md) | `/Items`, `/Providers`, `/Search` | 74 |
| [媒体库管理与虚拟文件夹](03-media-library/library-management.md) | `/Libraries`, `/Library` | 21 |
| [合集（Collections）与播放列表（Playlists）](03-media-library/collections-and-playlists.md) | `/Collections`, `/Playlists` | 14 |
| [影视、剧集、音乐、有声书、游戏与频道](03-media-library/media-categories.md) | `/Albums`, `/AudioBooks`, `/Channels`, `/Games`, `/Movies`, `/Shows`, `/Songs`, `/Trailers` | 18 |
| [演职员、流派、工作室、标签与媒体筛选维度](03-media-library/metadata-and-filters.md) | `/Artists`, `/AudioCodecs`, `/AudioLayouts`, `/Containers`, `/ExtendedVideoTypes`, `/GameGenres`, `/Genres`, `/ItemTypes`, `/MusicGenres`, `/OfficialRatings`, `/Persons`, `/StreamLanguages`, `/Studios`, `/SubtitleCodecs`, `/Tags`, `/VideoCodecs`, `/Years` | 54 |

### 04. 音视频流媒体、转码与图片（`04-streaming-and-media/` — 共 63 条）

视频/HLS 流媒体、音频流、实时流管理、转码编码配置与全局图片接口。

| 文档 | 包含路由前缀 | 接口数 |
| :--- | :--- | :---: |
| [视频流、HLS 分片、字幕提取与 LiveStreams](04-streaming-and-media/videos-and-hls.md) | `/LiveStreams`, `/Videos` | 32 |
| [音频流与通用音频转码](04-streaming-and-media/audio.md) | `/Audio` | 17 |
| [编码与硬件加速配置](04-streaming-and-media/encoding.md) | `/Encoding` | 13 |
| [图片服务（全局图片入口）](04-streaming-and-media/images.md) | `/Images` | 1 |

### 05. 电视直播与录制（Live TV）（`05-live-tv/` — 共 71 条）

直播频道、电子节目单（EPG）、调谐器主机、录制计划、系列录制与录播管理。

| 文档 | 包含路由前缀 | 接口数 |
| :--- | :--- | :---: |
| [Live TV 直播、节目单与录制管理](05-live-tv/livetv.md) | `/LiveTv` | 71 |

### 06. 同步、通知与插件管理（`06-sync-and-notifications/` — 共 47 条）

离线同步与转码下载、系统/用户通知及已安装插件生命周期管理。

| 文档 | 包含路由前缀 | 接口数 |
| :--- | :--- | :---: |
| [离线同步与下载（Sync）](06-sync-and-notifications/sync.md) | `/Sync` | 25 |
| [通知服务（Notifications）](06-sync-and-notifications/notifications.md) | `/Notifications` | 13 |
| [插件管理（Plugins）](06-sync-and-notifications/plugins.md) | `/Plugins` | 9 |

### 07. 内置插件 API（system/plugins）（`07-builtin-plugins/` — 共 31 条）

来自 Emby 4.10.2.0 安装包内置插件（`Emby.Dlna.dll`、`MBBackup.dll`、`Emby.Webhooks.dll`）的扩展接口。

| 文档 | 包含路由前缀 | 接口数 |
| :--- | :--- | :---: |
| [DLNA 服务与设备配置文件（Emby.Dlna）](07-builtin-plugins/dlna.md) | `/Dlna` | 26 |
| [配置备份与还原（MBBackup）](07-builtin-plugins/backup-restore.md) | `/BackupRestore` | 3 |
| [Webhooks 推送（Emby.Webhooks）](07-builtin-plugins/webhooks.md) | `/Webhooks` | 2 |

## 按程序集分布

| 程序集 | 来源 | 路由数 |
| :--- | :---: | :---: |
| `Emby.Api.dll` | 核心 (`system`) | 355 |
| `Emby.LiveTV.dll` | 核心 (`system`) | 70 |
| `Emby.Server.MediaEncoding.dll` | 核心 (`system`) | 69 |
| `Emby.Dlna.dll` | 内置插件 (`system/plugins`) | 26 |
| `Emby.Server.Sync.dll` | 核心 (`system`) | 25 |
| `Emby.Notifications.dll` | 核心 (`system`) | 13 |
| `Emby.Web.dll` | 核心 (`system`) | 7 |
| `Emby.Server.Connect.dll` | 核心 (`system`) | 5 |
| `Emby.Server.Implementations.dll` | 核心 (`system`) | 4 |
| `MBBackup.dll` | 内置插件 (`system/plugins`) | 3 |
| `Emby.Web.GenericUI.dll` | 核心 (`system`) | 2 |
| `Emby.Webhooks.dll` | 内置插件 (`system/plugins`) | 2 |
| `Emby.ActivityLog.dll` | 核心 (`system`) | 1 |
