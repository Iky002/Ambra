# 02. Emby API 兼容层设计

[← 返回文档中心](../README.md)

要让官方客户端与第三方客户端（Infuse、VidHub、SenPlayer、Kodi 等）无感知地把 Ambra 当作 Emby Server `4.10.2.0` 使用，必须在协议兼容层处理好 Emby 多年演进留下的各种隐性约定。

> **核心兼容原则：对外语义与功能一致，对内实现完全自主**  
> Ambra 保证每条 API 的路由、参数语义、鉴权行为、响应结构与业务效果对客户端完全透明等价；底层存储结构、索引算法、缓存策略、播放/转码调度与插件机制均采用更现代简洁的独立实现，不照搬 Emby 内部实现细节。

---

## 1. 路由挂载与双前缀兼容

Emby 的同一接口同时支持带 `/emby` 前缀与不带前缀两种访问方式，且部分老客户端会混用大小写路径：

- **双前缀挂载**：在根路由器上同时将全部 582 条路由挂载到 `/` 和 `/emby/` 下（或通过前置路径剥离中间件将 `/emby/...` 重写为 `/...`，注意保留 `/web` 与 `/emby/web` 的静态资源语义）。
- **路径大小写容错**：Emby 基于 ServiceStack，路由匹配默认**大小写不敏感**（如 `/Users/AuthenticateByName` 与 `/users/authenticatebyname` 等价）。在 `chi` 路由层需封装大小写不敏感的路由归一化中间件（仅小写化静态路径段，保留 `{Id}`、`{Name}` 等动态参数原值）。
- **特殊动词支持**：除标准 `GET / POST / PUT / DELETE / HEAD` 外，DLNA 模块（`Emby.Dlna`）还包含 `SUBSCRIBE` 与 `UNSUBSCRIBE` 两个 UPnP 事件动词，路由层需显式注册支持。

---

## 2. 认证与鉴权中间件（`[Authenticated]` 语义）

根据 4.10.2.0 提取结果，582 条路由中 **521 条需认证，61 条公开**。鉴权中间件需完整兼容 Emby 的 4 种 Token 传递方式：

1. HTTP Header：`X-Emby-Token: <token>`
2. HTTP Header：`X-MediaBrowser-Token: <token>`
3. HTTP Header：`X-Emby-Authorization` 或 `Authorization`（格式：`MediaBrowser Client="...", Device="...", DeviceId="...", Version="...", Token="<token>"`）
4. Query 参数：`?api_key=<token>` 或 `?X-Emby-Token=<token>`

### 鉴权策略分级
对应 Emby `[Authenticated]` 特性属性，每条路由注册时声明其策略：
- **Public**：公开访问（如 `/System/Info/Public`、`/Users/Public`、`/System/Ping`、`/Branding/*`）。
- **Authenticated**：有效用户 Token 或 API Key 均可访问。
- **Admin (`Roles = "Admin"`)**：仅管理员用户或具备管理权限的 API Key 可访问（如 `/System/Configuration` POST、`/System/Restart`、`/Library/VirtualFolders`）。
- **AllowBeforeStartupWizard**：初始化向导完成前仍允许调用的接口（如 `/Startup/*`）。
- **AllowLocalOnly**：仅允许环回/局域网地址调用的接口。

同时，中间件自动从 `X-Emby-Authorization` 提取 `Client / Device / DeviceId / Version` 注入 `context`，供 Session 管理与设备追踪使用。

---

## 3. Query 参数与反序列化兼容

Emby 客户端在传递查询参数时存在大量非标准习惯，兼容层需提供统一的 `QueryBinder`：

- **Key 大小写不敏感**：如 `userId`、`UserId`、`userid` 必须都能绑定到同一字段。
- **逗号分隔多值**：`IncludeItemTypes=Movie,Series`、`Fields=Overview,Genres,Path`、`SortBy=SortName,ProductionYear` 等均需支持逗号拆分，同时也兼容重复 Key（`?Ids=1&Ids=2`）。
- **布尔值宽容解析**：同时接受 `true/false`、`True/False`、`1/0`。
- **GUID 归一化**：Emby 客户端对同一资源有时传 32 位无横杠 hex（`a1b2c3...`），有时传 36 位带横杠 UUID（`a1b2c3d4-...`），部分接口还会直接传数字内部 ID（`InternalId`）。兼容层统一做 ID 规范化与兼容查找。
- **枚举字符串解析**：如 `ImageType`（`Primary` / `Backdrop` / `Thumb`）、`SortOrder`（`Ascending` / `Descending`）均做大小写不敏感映射。

---

## 4. JSON 序列化与响应格式规范

- **字段命名**：Emby JSON 默认使用 **PascalCase**（如 `ServerId`, `BaseItemDto`, `TotalRecordCount`, `MediaSources`）。所有对外 DTO 必须严格使用 PascalCase JSON Tag，与 4.10.2.0 抓包及反射模型保持一致。
- **空集合 vs `null`**：部分严格客户端（如 Infuse、官方 iOS App）在遇到 `Items: null` 或 `MediaSources: null` 时会解析报错；切片字段在序列化前必须确保初始化为空切片 `[]` 而非 `nil`。
- **时间与时长单位**：
  - 时间戳：ISO 8601 UTC（如 `2026-01-01T00:00:00.0000000Z`）。
  - 媒体时长/播放进度：**Ticks**（`1 秒 = 10,000,000 Ticks`，100 纳秒单位），内部提供 `pkg/ticks` 统一换算。
- **常见响应包装**：
  - 分页列表统一返回 `QueryResult<T>`：`{ "Items": [...], "TotalRecordCount": N }`。
  - 无内容操作返回 `204 No Content`。
  - 兼容 `ETag` 与 `If-None-Match`（图片与静态资源返回 `304 Not Modified`，显著提升客户端滑动流畅度）。

---

## 5. WebSocket 实时通道

Emby 客户端连接 `/embywebsocket?api_key=<token>&deviceId=<id>`（及 `/socket`）建立长连接，用于：
- **下行推送**：`LibraryChanged`（扫库更新）、`UserDataChanged`（已看/收藏同步）、`Sessions`（活动会话变更）、`Play` / `Playstate` / `GeneralCommand`（远程遥控播放）、`ScheduledTasksInfo`（后台任务进度）、`KeepAlive`。
- **上行心跳与订阅**：`KeepAlive`、`SessionsStart` / `SessionsStop`、`ScheduledTasksInfoStart` / `ScheduledTasksInfoStop`。

在 Go 中通过 `ws.Hub` 按 `SessionId / UserId / DeviceId` 维护连接池，并订阅内部 `EventBus` 将领域事件实时扇出。

---

## 6. 39 条隐藏接口与兼容性兜底

4.10.2.0 中包含 39 条 `IsHidden = true` 接口（如 `/Registrations/{Name}`、`/Appstore/Register`、`/System/Logs` 旧版等）：
- **Premiere / 许可相关接口**（`/Registrations/{Name}`、`/System/Info` 中的许可状态等）：Ambra 作为完全开源自研服务，需按客户端期望返回合法的 `RegistrationInfo`（`IsRegistered: true` 等字段）以保证客户端不弹付费拦截提示，同时不涉及任何第三方专有校验逻辑。
- **已废弃但老客户端仍在调的别名路由**（如 `/Users/{UserId}/Items` 与 `/Items`、旧版 `/System/Logs`）：通过路由别名直接复用新版 Handler，零重复代码即可实现 100% 路由覆盖。
