# 02. 认证、权限、会话与实时通信规格

[← 返回文档中心](../README.md)

本文档定义用户认证、权限与家长控制策略、活动会话状态机（`SessionManager`）、WebSocket 协议帧格式以及 Parties 共同观看的实现思路。

---

## 1. 认证体系与 Token 模型

### 1.1 登录认证流程
- **`POST /Users/AuthenticateByName`**：
  - 接收 `Username` 与 `Pw`（密码明文；兼容老客户端 `Password` SHA1 字段）。
  - 校验 `users.is_disabled`、Argon2id 密码哈希（支持配置允许空密码本地用户，与 Emby 行为一致）。
  - 生成随机 32 字节 hex `AccessToken`，写入 `access_tokens` 表并关联 `DeviceId`、`AppName`、`DeviceName`、`AppVersion`。
  - 返回 `AuthenticationResult`（`UserDto` + `SessionInfo` + `AccessToken` + `ServerId`）。
- **API Keys（`/Auth/Keys`）**：
  - 管理员创建的永久密钥，拥有完整管理权限，用于 MoviePilot / Sonarr / Radarr / 脚本自动化调用。
- **用户注销与踢出**：
  - `POST /Sessions/Logout` 吊销当前 Token；`DELETE /Devices?Id=...` 吊销该设备所有 Token 并断开其 WebSocket 连接。

### 1.2 统一鉴权中间件逻辑
```text
Request
  ├─ 提取 Token (Header X-Emby-Token > X-MediaBrowser-Token > X-Emby-Authorization[Token] > Query api_key)
  ├─ 提取客户端标识 (X-Emby-Authorization 中的 Client/Device/DeviceId/Version)
  ├─ 若路由为 Public → 直接放行（若有 Token 则顺便注入用户信息）
  ├─ 校验 Token（先查内存 LRU 缓存，未命中查 SQLite access_tokens / api_keys）
  │    ├─ 无效/过期 → 返回 401 Unauthorized
  │    └─ 有效 → 刷新 Device/Session 活跃时间（内存节流，每 30 秒异步刷盘一次，避免每次请求写库）
  └─ 若路由要求 Admin → 校验 user.IsAdmin || isApiKey，否则返回 403 Forbidden
```

---

## 2. 权限与家长控制（`UserPolicy`）

Emby 的 `UserPolicy` 控制细粒度权限，Ambra 在 Domain 层统一通过 `PolicyEvaluator` 执行：
- **媒体库访问控制**：`EnableAllFolders=false` 时仅返回 `EnabledFolders` 白名单中的媒体库与条目。
- **分级与标签过滤**：`MaxParentalRating`（按各国分级映射为统一数值分值）与 `BlockedTags` 在查询构造器阶段直接过滤，保证搜索、推荐、演职员关联页全部生效。
- **播放与管理权限**：
  - `EnableVideoPlaybackTranscoding` / `EnableAudioPlaybackTranscoding`：决定 `PlaybackInfo` 是否允许下发转码流。
  - `EnableContentDeletion` / `EnableContentDownloading`：控制 `DELETE /Items/{Id}` 与 `/Items/{Id}/Download`。
  - `EnableLiveTvAccess` / `EnableLiveTvManagement`：控制 Live TV 与录播权限。

---

## 3. 活动会话状态机（`SessionManager`）

`SessionManager` 是纯内存并发安全管理器（`sync.RWMutex`），维护所有在线客户端的 `SessionInfo`（按 `DeviceId` + `UserId` 索引）：

### 3.1 播放进度上报闭环
1. **`POST /Sessions/Playing`**：客户端开始播放
   - 更新 `SessionInfo.NowPlayingItem` 与 `PlayState`（`MediaSourceId`, `PlayMethod`, `AudioStreamIndex`, `SubtitleStreamIndex`, `PositionTicks`）。
   - 发布 `PlaybackStart` 内部事件（触发 Webhooks / Trakt Scrobble / 活动日志）。
2. **`POST /Sessions/Playing/Progress`**：播放中每 10 秒心跳上报
   - 更新内存 `PlayState.PositionTicks` 与 `IsPaused`。
   - 节流写入 `user_item_data.playback_position_ticks`（每 15~30 秒或暂停状态变化时落盘）。
3. **`POST /Sessions/Playing/Stopped`**：播放结束
   - **自动已看判定**：若 `PositionTicks / RuntimeTicks >= 0.90`（可配置阈值，默认 90%，片头跳过阈值默认 5%），则自动将 `user_item_data.played = 1`、`play_count += 1`、`playback_position_ticks = 0`；否则保留断点进度。
   - 若正在转码，自动触发 `TranscodeManager.Stop(PlaySessionId)` 回收 FFmpeg 进程。
   - 广播 `UserDataChanged` 到该用户的其他在线设备，并触发 `PlaybackStop` 事件。

### 3.2 远程遥控（Casting / Remote Control）
- 客户端通过 `POST /Sessions/Capabilities` 上报自己支持的遥控能力（`SupportsRemoteControl`, `PlayableMediaTypes`, `SupportedCommands`）。
- 其他客户端调用 `POST /Sessions/{Id}/Playing`（指令投屏播放）、`POST /Sessions/{Id}/Playing/{Command}`（Pause/Unpause/Seek/Stop/NextTrack）、`POST /Sessions/{Id}/Message`（弹窗消息）时，`SessionManager` 直接通过目标 Session 的 WebSocket 连接下发指令帧。

---

## 4. WebSocket 实时协议规格（`/embywebsocket`）

Emby WebSocket 使用统一 JSON 帧格式：
```json
{
  "MessageType": "UserDataChanged",
  "MessageId": "a1b2c3d4...",
  "Data": { ... }
}
```

### 4.1 需支持的上行消息（Client → Server）
- `KeepAlive`：刷新连接活跃时间。
- `SessionsStart` / `SessionsStop`：订阅/取消订阅全局活动会话变化（按客户端传入的刷新间隔 `Data: "0,1500"` 节流推送）。
- `ScheduledTasksInfoStart` / `ScheduledTasksInfoStop`：订阅后台计划任务进度条。
- `ActivityLogEntryStart` / `ActivityLogEntryStop`：订阅管理后台活动日志流。

### 4.2 需支持的下行消息（Server → Client）
- `ForceKeepAlive`（连接建立后立即下发心跳间隔，如 60 秒）与 `KeepAlive`。
- `LibraryChanged`（包含 `ItemsAdded`、`ItemsUpdated`、`ItemsRemoved`、`FoldersAddedTo`）。
- `UserDataChanged`（`UserId` + `UserDataList`）。
- `Sessions` / `ScheduledTasksInfo` / `ActivityLogEntry`。
- `Play` / `Playstate` / `GeneralCommand`（远程控制指令）。
- `ServerRestarting` / `ServerShuttingDown`。

---

## 5. 共同观看派对（`/Parties/*`）

- 内存维护 `PartyRoom`（`Id`, `HostSessionId`, `MemberSessionIds`, `ItemId`, `PositionTicks`, `IsPaused`）。
- 房主触发播放/暂停/Seek 时，服务端通过 WebSocket 向房间内所有成员广播同步 `Playstate` 指令（附带服务端时间戳以便成员端补偿网络延迟）。
