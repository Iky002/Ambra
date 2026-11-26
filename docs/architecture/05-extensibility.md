# 05. 统一插件内核与扩展架构

[← 返回文档中心](../README.md)

## 1. 设计理念：参考 Emby 插件化，但摆脱 `.dll` 历史包袱

Emby 的核心优势之一是把**元数据刮削器、字幕源、图片源、通知渠道、第三方同步**都做成独立插件，用户可按媒体库自由启停与调整优先级。  
Ambra 完整继承这一**高度模块化的插件架构思想**，但因为用 Go 重写，不直接加载强耦合于 .NET CLR 与 Harmony Hook 的 Emby `.dll`，而是设计了一套**现代、跨语言、零 CGO 的统一插件内核**：

- **同一套契约，两种运行形态**：
  1. **官方内置插件（Built-in Go Plugins）**：TMDB、TVDB、NFO、Fanart、OpenSubtitles、Trakt、Webhooks、BackupRestore、DLNA 等官方模块全部实现统一的 `Plugin` 接口并随主程序编译（开箱即用、零 IPC 开销、仍可按媒体库自由开关/排序/配置）。
  2. **第三方外部插件（WASM `wazero` / HTTP 进程外插件）**：社区开发者编写的插件（如豆瓣/Bangumi 刮削、弹幕源、自定义网盘 302 签名器、飞书/Telegram 通知）实现完全相同的能力接口，热插拔放入 `data/plugins/` 即可被调度器识别。

---

## 2. 统一插件能力接口（Extension Points）

所有插件通过 `manifest.json` 声明元数据、配置表单 Schema（供 Web 管理端与 `/UI/View` 自动渲染）及实现的扩展点：

| 扩展点接口 | 职责 | 典型官方内置 / 社区插件 |
| :--- | :--- | :--- |
| `MetadataProvider` | 按名称/年份/外部 ID 搜索并拉取影视/音乐/演职员元数据 | `tmdb`、`tvdb`、`nfo`、`douban`、`bangumi` |
| `ImageProvider` | 拉取海报、背景图、Logo、演职员头像 | `tmdb-images`、`fanart`、`local-images` |
| `SubtitleProvider` | 按文件哈希或标题搜索并下载外挂字幕 | `opensubtitles`、`shooter`、`assrt` |
| `StreamResolver` | 将 STRM/远端路径动态解析为带签名的 302 直链或代理头 | `strm-direct`、各网盘直链解析插件 |
| `EventSubscriber` / `Notifier` | 订阅 `PlaybackStart/Stop`、`LibraryChanged` 等事件 | `trakt`、`webhooks`、`telegram`、`bark` |
| `ScheduledTaskProvider` | 注册自定义后台定时任务到 `/ScheduledTasks` | `backup-restore`、自定义片头检测/清理任务 |

### 优先级流水线（Scraper Chain）
- 每个媒体库可独立配置启用的 `MetadataProvider` / `ImageProvider` 列表及**拖拽优先级顺序**（例如：`NFO` → `TMDB` → `豆瓣`）。
- 刮削引擎按优先级依次执行并做字段级合并（高优先级已有字段不被低优先级覆盖），并内置 `singleflight` 与速率限制防止触发上游 API 封禁。

---

## 3. 外部插件运行机制

1. **WASM 沙箱插件（基于 `wazero`，纯 Go 零 CGO）**
   - 支持 Go (TinyGo) / Rust / AssemblyScript 编写，编译为 `.wasm` 单文件放入 `data/plugins/` 热加载。
   - 宿主仅按需授予受控能力（受限 HTTP 请求、只读配置、日志写入），插件崩溃或死循环（带超时熔断）绝不拖垮 Ambra 主服务。
2. **HTTP / Webhook 进程外插件**
   - 任意语言（Python / Node.js / Go）起一个轻量 HTTP 服务即可按标准 JSON 契约注册为刮削器、字幕源、通知器或 302 解析器，极度适合快速对接各类非官方站点。

---

## 4. 外部自动化生态与 Emby `/Plugins` API 兼容

- **Emby API 映射**：Ambra 的全部内置插件与外部插件统一映射到 `/Plugins`、`/Plugins/{Id}/Configuration`、`/Packages` 与 `/UI/View`，客户端与配置工具可像操作 Emby 一样管理插件。
- **自动化工具全兼容**：完整兼容 `/Library/Media/Updated`、`/Library/Refresh`、`/Users`、`/System/Info` 与标准 `.nfo`/本地海报规范，保证 **MoviePilot、Sonarr、Radarr、TinyMediaManager、Emby2Alist** 零改动无缝衔接。

## 5. Trakt：服务端可用性与个人账号分离

- 管理员通过 `/Plugins/ambra.builtin.trakt/Configuration` 或 `/Enable` 控制全局启用状态；全局配置只保存 `enabled` 和 `priority`。通知类插件与元数据、流解析插件均可被插件管理器发现。
- 普通用户与管理员均通过自己的登录会话调用 `GET /Users/Me/Trakt`、`POST /Users/Me/Trakt`、`DELETE /Users/Me/Trakt`。接口不接受目标用户 ID，服务端 API Key 无法代替个人会话绑定账号。
- POST 支持 `ClientId`、`AccessToken`、`ScrobblePlayback`、`SyncWatched`；省略 Token 会保留现有值，切换 Client ID 时必须提供新 Token。GET / POST 仅返回配置状态，不返回 Token。当前使用用户自行取得的 OAuth Token，过期后由用户更新。
- 凭据单独保存在 `user_trakt` 表，以 Ambra 用户 ID 为主键，删除用户时级联删除；不写入通用 UserConfiguration / User DTO。全局停用后仍允许查看状态与解除绑定。
- 播放与已看事件根据实际事件用户 ID 查询账号，未绑定或被禁用的用户不会上报。后台任务执行时重新读取配置，避免继续使用已解除的绑定。暂停节流与完成去重按用户和媒体共同隔离。
- 升级时清理旧的全局账号字段，不推断归属、不自动绑定。已有用户需在个人中心重新填写自己的凭据。
