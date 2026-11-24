# 会话控制、播放状态与共同观看派对

> 所属模块：**02. 用户、认证、设备与会话**  
> 路由前缀：`/Parties`, `/Playback`, `/Sessions`  
> 接口总数：**27** 条

涵盖活动会话查询、远程播放指令、播放进度上报、转码信息与 Parties（派对）。

[← 返回总目录](../README.md)

## 接口总览

| # | 方法 | 路径 | 鉴权 | 返回类型 | 摘要 |
| :---: | :--- | :--- | :--- | :--- | :--- |
| 1 | `GET` | `/Parties` | 需认证 | `object` | Gets a list of active parties |
| 2 | `POST` | `/Parties` | 需认证 | `PartyInfoResult` | Creates a party |
| 3 | `GET` | `/Parties/Info` | 需认证 | `PartyInfoResult` | Gets info about the session's current party |
| 4 | `POST` | `/Parties/Leave` | 需认证 | `void` | Leaves a party |
| 5 | `GET` | `/Parties/Messages` | 需认证 | `QueryResult<PartyMessage>` | Gets party messages |
| 6 | `POST` | `/Parties/Messages` | 需认证 | `void` | Posts a message to the party |
| 7 | `POST` | `/Parties/{Id}/Join` | 需认证 | `PartyInfoResult` | Joins a party |
| 8 | `GET` | `/Playback/BitrateTest` | 需认证 | `object` | - |
| 9 | `GET` | `/Sessions` | 需认证 | `SessionInfo[]` | Gets a list of sessions |
| 10 | `POST` | `/Sessions/Capabilities` | 需认证 | `void` | Updates capabilities for a device |
| 11 | `POST` | `/Sessions/Capabilities/Full` | 需认证 | `void` | Updates capabilities for a device |
| 12 | `POST` | `/Sessions/Logout` | 需认证 | `void` | Reports that a session has ended |
| 13 | `POST` | `/Sessions/Playing` | 需认证 | `void` | Reports playback has started within a session |
| 14 | `POST` | `/Sessions/Playing/Ping` | 需认证 | `void` | Pings a playback session |
| 15 | `POST` | `/Sessions/Playing/Progress` | 需认证 | `void` | Reports playback progress within a session |
| 16 | `POST` | `/Sessions/Playing/Stopped` | 需认证 | `void` | Reports playback has stopped within a session |
| 17 | `GET` | `/Sessions/PlayQueue` | 需认证 | `QueryResult<BaseItemDto>` | Gets a the current play queue from a session |
| 18 | `POST` | `/Sessions/{Id}/Command` | 需认证 | `void` | Issues a system command to a client |
| 19 | `POST` | `/Sessions/{Id}/Command/{Command}` | 需认证 | `void` | Issues a system command to a client |
| 20 | `POST` | `/Sessions/{Id}/Message` | 需认证 | `void` | Issues a command to a client to display a message to the user |
| 21 | `POST` | `/Sessions/{Id}/Playing` | 需认证 | `void` | Instructs a session to play an item |
| 22 | `POST` | `/Sessions/{Id}/Playing/{Command}` | 需认证 | `void` | Issues a playstate command to a client |
| 23 | `POST` | `/Sessions/{Id}/System/{Command}` | 需认证 | `void` | Issues a system command to a client |
| 24 | `DELETE` | `/Sessions/{Id}/Users/{UserId}` | 需认证 | `void` | Removes an additional user from a session |
| 25 | `POST` | `/Sessions/{Id}/Users/{UserId}` | 需认证 | `void` | Adds an additional user to a session |
| 26 | `POST` | `/Sessions/{Id}/Users/{UserId}/Delete` | 需认证 | `void` | Removes an additional user from a session |
| 27 | `POST` | `/Sessions/{Id}/Viewing` | 需认证 | `void` | Instructs a session to browse to an item or view |

## 接口详情

### MediaInfoService（1 条）

#### `GET` `/Playback/BitrateTest`

- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Server.MediaEncoding.Api.MediaInfoService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.GetBitrateTestBytes` (`Emby.Server.MediaEncoding.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Size` | `query` | `long` | 是 | 否 | Size |

### PartyService（7 条）

#### `GET` `/Parties`

- **摘要**：Gets a list of active parties
- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Api.Session.PartyService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Session.GetParties` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Items` | `query` | `PartyInfoDto[]` | 否 | 否 | - |
| `TotalRecordCount` | `query` | `int` | 否 | 否 | - |

#### `POST` `/Parties`

- **摘要**：Creates a party
- **鉴权**：需要认证
- **返回类型**：`PartyInfoResult`
- **处理服务**：`Emby.Api.Session.PartyService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Session.CreateParty` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Name` | `path` | `string` | 是 | 否 | Name |

#### `GET` `/Parties/Info`

- **摘要**：Gets info about the session's current party
- **鉴权**：需要认证
- **返回类型**：`PartyInfoResult`
- **处理服务**：`Emby.Api.Session.PartyService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Session.GetPartyInfo` (`Emby.Api.dll`)

_无请求参数。_

#### `POST` `/Parties/Leave`

- **摘要**：Leaves a party
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Session.PartyService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Session.LeaveParty` (`Emby.Api.dll`)

_无请求参数。_

#### `GET` `/Parties/Messages`

- **摘要**：Gets party messages
- **鉴权**：需要认证
- **返回类型**：`QueryResult<PartyMessage>`
- **处理服务**：`Emby.Api.Session.PartyService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Session.GetPartyMessages` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Limit` | `query` | `int?` | 否 | 否 | - |
| `StartIndex` | `query` | `int` | 否 | 否 | - |

#### `POST` `/Parties/Messages`

- **摘要**：Posts a message to the party
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Session.PartyService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Session.PostMessage` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `DateTime` | `query` | `DateTimeOffset` | 否 | 否 | - |
| `Message` | `query` | `string` | 否 | 否 | - |
| `UserId` | `query` | `long` | 否 | 否 | - |

#### `POST` `/Parties/{Id}/Join`

- **摘要**：Joins a party
- **鉴权**：需要认证
- **返回类型**：`PartyInfoResult`
- **处理服务**：`Emby.Api.Session.PartyService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Session.JoinParty` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Name |

### PlaystateService（4 条）

#### `POST` `/Sessions/Playing`

- **摘要**：Reports playback has started within a session
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Server.MediaEncoding.Api.PlaystateService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.ReportPlaybackStart` (`Emby.Server.MediaEncoding.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `AspectRatio` | `query` | `string` | 否 | 否 | - |
| `AudioStreamIndex` | `query` | `int?` | 否 | 否 | - |
| `Brightness` | `query` | `int?` | 否 | 否 | - |
| `CanSeek` | `query` | `bool` | 否 | 否 | - |
| `EventName` | `query` | `ProgressEvent` | 否 | 否 | - |
| `IsMuted` | `query` | `bool` | 否 | 否 | - |
| `IsPaused` | `query` | `bool` | 否 | 否 | - |
| `Item` | `query` | `BaseItemDto` | 否 | 否 | - |
| `ItemId` | `query` | `string` | 否 | 否 | - |
| `LiveStreamId` | `query` | `string` | 否 | 否 | - |
| `MediaSourceId` | `query` | `string` | 否 | 否 | - |
| `NowPlayingQueue` | `query` | `QueueItem[]` | 否 | 否 | - |
| `PlaybackRate` | `query` | `double` | 否 | 否 | - |
| `PlaybackStartTimeTicks` | `query` | `long?` | 否 | 否 | - |
| `PlaylistIndex` | `query` | `int` | 否 | 否 | - |
| `PlaylistItemId` | `query` | `string` | 否 | 否 | - |
| `PlaylistItemIds` | `query` | `string[]` | 否 | 否 | - |
| `PlaylistLength` | `query` | `int` | 否 | 否 | - |
| `PlayMethod` | `query` | `PlayMethod` | 否 | 否 | - |
| `PlaySessionId` | `query` | `string` | 否 | 否 | - |
| `PositionTicks` | `query` | `long?` | 否 | 否 | - |
| `RepeatMode` | `query` | `RepeatMode` | 否 | 否 | - |
| `RunTimeTicks` | `query` | `long?` | 否 | 否 | - |
| `SessionId` | `query` | `string` | 否 | 否 | - |
| `Shuffle` | `query` | `bool` | 否 | 否 | - |
| `SleepTimerEndTime` | `query` | `DateTimeOffset?` | 否 | 否 | - |
| `SleepTimerMode` | `query` | `SleepTimerMode` | 否 | 否 | - |
| `SubtitleOffset` | `query` | `int` | 否 | 否 | - |
| `SubtitleStreamIndex` | `query` | `int?` | 否 | 否 | - |
| `VolumeLevel` | `query` | `int?` | 否 | 否 | - |

#### `POST` `/Sessions/Playing/Ping`

- **摘要**：Pings a playback session
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Server.MediaEncoding.Api.PlaystateService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.PingPlaybackSession` (`Emby.Server.MediaEncoding.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `PlaySessionId` | `query` | `string` | 否 | 否 | - |

#### `POST` `/Sessions/Playing/Progress`

- **摘要**：Reports playback progress within a session
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Server.MediaEncoding.Api.PlaystateService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.ReportPlaybackProgress` (`Emby.Server.MediaEncoding.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `AspectRatio` | `query` | `string` | 否 | 否 | - |
| `AudioStreamIndex` | `query` | `int?` | 否 | 否 | - |
| `Brightness` | `query` | `int?` | 否 | 否 | - |
| `CanSeek` | `query` | `bool` | 否 | 否 | - |
| `EventName` | `query` | `ProgressEvent` | 否 | 否 | - |
| `IsMuted` | `query` | `bool` | 否 | 否 | - |
| `IsPaused` | `query` | `bool` | 否 | 否 | - |
| `Item` | `query` | `BaseItemDto` | 否 | 否 | - |
| `ItemId` | `query` | `string` | 否 | 否 | - |
| `LiveStreamId` | `query` | `string` | 否 | 否 | - |
| `MediaSourceId` | `query` | `string` | 否 | 否 | - |
| `NowPlayingQueue` | `query` | `QueueItem[]` | 否 | 否 | - |
| `PlaybackRate` | `query` | `double` | 否 | 否 | - |
| `PlaybackStartTimeTicks` | `query` | `long?` | 否 | 否 | - |
| `PlaylistIndex` | `query` | `int` | 否 | 否 | - |
| `PlaylistItemId` | `query` | `string` | 否 | 否 | - |
| `PlaylistItemIds` | `query` | `string[]` | 否 | 否 | - |
| `PlaylistLength` | `query` | `int` | 否 | 否 | - |
| `PlayMethod` | `query` | `PlayMethod` | 否 | 否 | - |
| `PlaySessionId` | `query` | `string` | 否 | 否 | - |
| `PositionTicks` | `query` | `long?` | 否 | 否 | - |
| `RepeatMode` | `query` | `RepeatMode` | 否 | 否 | - |
| `RunTimeTicks` | `query` | `long?` | 否 | 否 | - |
| `SessionId` | `query` | `string` | 否 | 否 | - |
| `Shuffle` | `query` | `bool` | 否 | 否 | - |
| `SleepTimerEndTime` | `query` | `DateTimeOffset?` | 否 | 否 | - |
| `SleepTimerMode` | `query` | `SleepTimerMode` | 否 | 否 | - |
| `SubtitleOffset` | `query` | `int` | 否 | 否 | - |
| `SubtitleStreamIndex` | `query` | `int?` | 否 | 否 | - |
| `VolumeLevel` | `query` | `int?` | 否 | 否 | - |

#### `POST` `/Sessions/Playing/Stopped`

- **摘要**：Reports playback has stopped within a session
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Server.MediaEncoding.Api.PlaystateService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.ReportPlaybackStopped` (`Emby.Server.MediaEncoding.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Failed` | `query` | `bool` | 否 | 否 | - |
| `IsAutomated` | `query` | `bool` | 否 | 否 | - |
| `Item` | `query` | `BaseItemDto` | 否 | 否 | - |
| `ItemId` | `query` | `string` | 否 | 否 | - |
| `LiveStreamId` | `query` | `string` | 否 | 否 | - |
| `MediaSourceId` | `query` | `string` | 否 | 否 | - |
| `NextMediaType` | `query` | `string` | 否 | 否 | - |
| `NowPlayingQueue` | `query` | `QueueItem[]` | 否 | 否 | - |
| `PlaylistIndex` | `query` | `int` | 否 | 否 | - |
| `PlaylistItemId` | `query` | `string` | 否 | 否 | - |
| `PlaylistLength` | `query` | `int` | 否 | 否 | - |
| `PlaySessionId` | `query` | `string` | 否 | 否 | - |
| `PositionTicks` | `query` | `long?` | 否 | 否 | - |
| `SessionId` | `query` | `string` | 否 | 否 | - |

### SessionsService（15 条）

#### `GET` `/Sessions`

- **摘要**：Gets a list of sessions
- **鉴权**：需要认证
- **返回类型**：`SessionInfo[]`
- **处理服务**：`Emby.Api.Session.SessionsService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Session.GetSessions` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `ActiveWithinSeconds` | `query` | `int?` | 否 | 否 | - |
| `ControllableByUserId` | `query` | `string` | 否 | 否 | Optional. Filter by sessions that a given user is allowed to remote control. |
| `DeviceId` | `query` | `string` | 否 | 否 | Optional. Filter by device id. |
| `Id` | `query` | `string` | 否 | 否 | Optional. Filter by session id. |
| `IncludeAllSessionsIfAdmin` | `query` | `bool` | 否 | 否 | - |
| `IsPlaying` | `query` | `bool?` | 否 | 否 | - |

#### `POST` `/Sessions/Capabilities`

- **摘要**：Updates capabilities for a device
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Session.SessionsService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Session.PostCapabilities` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `query` | `string` | 是 | 否 | Session Id |
| `PlayableMediaTypes` | `query` | `string` | 否 | 否 | A list of playable media types, comma delimited. Audio, Video, Book, Game, Photo. |
| `SupportedCommands` | `query` | `string` | 否 | 否 | A list of supported remote control commands, comma delimited |
| `SupportsMediaControl` | `query` | `bool` | 否 | 否 | Determines whether media can be played remotely. |
| `SupportsSync` | `query` | `bool` | 否 | 否 | Determines whether sync is supported. |

#### `POST` `/Sessions/Capabilities/Full`

- **摘要**：Updates capabilities for a device
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Session.SessionsService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Session.PostFullCapabilities` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `query` | `string` | 是 | 否 | Session Id |
| `AppId` | `query` | `string` | 否 | 否 | - |
| `DeviceProfile` | `query` | `DeviceProfile` | 否 | 否 | - |
| `IconUrl` | `query` | `string` | 否 | 否 | - |
| `PlayableMediaTypes` | `query` | `string[]` | 否 | 否 | - |
| `PushToken` | `query` | `string` | 否 | 否 | - |
| `PushTokenType` | `query` | `string` | 否 | 否 | - |
| `SupportedCommands` | `query` | `string[]` | 否 | 否 | - |
| `SupportsMediaControl` | `query` | `bool` | 否 | 否 | - |
| `SupportsSync` | `query` | `bool` | 否 | 否 | - |

#### `POST` `/Sessions/Logout`

- **摘要**：Reports that a session has ended
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Session.SessionsService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Session.ReportSessionEnded` (`Emby.Api.dll`)

_无请求参数。_

#### `GET` `/Sessions/PlayQueue`

- **摘要**：Gets a the current play queue from a session
- **鉴权**：需要认证
- **返回类型**：`QueryResult<BaseItemDto>`
- **处理服务**：`Emby.Api.Session.SessionsService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Session.GetSessionPlayQueue` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `DeviceId` | `query` | `string` | 否 | 否 | Optional. Filter by device id. |
| `Id` | `query` | `string` | 否 | 否 | Optional. Filter by session id. |

#### `POST` `/Sessions/{Id}/Command`

- **摘要**：Issues a system command to a client
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Session.SessionsService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Session.SendFullGeneralCommand` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Session Id |
| `Arguments` | `query` | `Dictionary<string, string>` | 否 | 否 | - |
| `ControllingUserId` | `query` | `string` | 否 | 否 | - |
| `Name` | `query` | `string` | 否 | 否 | - |

#### `POST` `/Sessions/{Id}/Command/{Command}`

- **摘要**：Issues a system command to a client
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Session.SessionsService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Session.SendGeneralCommand` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Command` | `path` | `string` | 是 | 否 | The command to send. |
| `Id` | `path` | `string` | 是 | 否 | Session Id |

#### `POST` `/Sessions/{Id}/Message`

- **摘要**：Issues a command to a client to display a message to the user
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Session.SessionsService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Session.SendMessageCommand` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Session Id |
| `Header` | `query` | `string` | 是 | 否 | The message header. |
| `Text` | `query` | `string` | 是 | 否 | The message text. |
| `TimeoutMs` | `query` | `long?` | 否 | 否 | The message timeout. If omitted the user will have to confirm viewing the message. |

#### `POST` `/Sessions/{Id}/Playing`

- **摘要**：Instructs a session to play an item
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Session.SessionsService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Session.Play` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Session Id |
| `ItemIds` | `query` | `long[]` | 是 | 是 | The ids of the items to play, comma delimited |
| `PlayCommand` | `query` | `PlayCommand` | 是 | 否 | The type of play command to issue (PlayNow, PlayNext, PlayLast). Clients who have not yet implemented play next and play last may play now. |
| `AudioStreamIndex` | `query` | `int?` | 否 | 否 | - |
| `ControllingUserId` | `query` | `string` | 否 | 否 | - |
| `MediaSourceId` | `query` | `string` | 否 | 否 | - |
| `StartIndex` | `query` | `int?` | 否 | 否 | - |
| `StartPositionTicks` | `query` | `long?` | 否 | 否 | The starting position of the first item. |
| `SubtitleStreamIndex` | `query` | `int?` | 否 | 否 | - |

#### `POST` `/Sessions/{Id}/Playing/{Command}`

- **摘要**：Issues a playstate command to a client
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Session.SessionsService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Session.SendPlaystateCommand` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Session Id |
| `Command` | `path` | `PlaystateCommand` | 否 | 否 | - |
| `ControllingUserId` | `query` | `string` | 否 | 否 | - |
| `SeekPositionTicks` | `query` | `long?` | 否 | 否 | - |

#### `POST` `/Sessions/{Id}/System/{Command}`

- **摘要**：Issues a system command to a client
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Session.SessionsService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Session.SendSystemCommand` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Command` | `path` | `string` | 是 | 否 | The command to send. |
| `Id` | `path` | `string` | 是 | 否 | Session Id |

#### `DELETE` `/Sessions/{Id}/Users/{UserId}`

- **摘要**：Removes an additional user from a session
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Session.SessionsService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Session.RemoveUserFromSession` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Session Id |
| `UserId` | `path` | `string` | 是 | 否 | UserId Id |

#### `POST` `/Sessions/{Id}/Users/{UserId}`

- **摘要**：Adds an additional user to a session
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Session.SessionsService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Session.AddUserToSession` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Session Id |
| `UserId` | `path` | `string` | 是 | 否 | UserId Id |

#### `POST` `/Sessions/{Id}/Users/{UserId}/Delete`

- **摘要**：Removes an additional user from a session
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Session.SessionsService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Session.RemoveUserFromSession` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Session Id |
| `UserId` | `path` | `string` | 是 | 否 | UserId Id |

#### `POST` `/Sessions/{Id}/Viewing`

- **摘要**：Instructs a session to browse to an item or view
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Session.SessionsService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Session.DisplayContent` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Session Id |
| `ItemId` | `query` | `string` | 是 | 否 | The Id of the item. |
| `ItemName` | `query` | `string` | 是 | 否 | The name of the item. |
| `ItemType` | `query` | `string` | 是 | 否 | The type of item to browse to. |
