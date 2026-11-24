# Live TV 直播、节目单与录制管理

> 所属模块：**05. 电视直播与录制（Live TV）**  
> 路由前缀：`/LiveTv`  
> 接口总数：**71** 条

涵盖 `/LiveTv` 下的全部直播频道、EPG、调谐器、录制任务与系列定时器接口。

[← 返回总目录](../README.md)

## 接口总览

| # | 方法 | 路径 | 鉴权 | 返回类型 | 摘要 |
| :---: | :--- | :--- | :--- | :--- | :--- |
| 1 | `GET` | `/LiveTv/AvailableRecordingOptions` | 认证 (`managelivetv`) | `AvailableRecordingOptions` | Gets available recording options |
| 2 | `GET` | `/LiveTv/ChannelMappingOptions` | 认证 (`Admin`) | `object` | - |
| 3 | `POST` | `/LiveTv/ChannelMappings` | 认证 (`Admin`) | `object` | - |
| 4 | `GET` | `/LiveTv/Channels` | 需认证 | `QueryResult<BaseItemDto>` | Gets available live tv channels. |
| 5 | `GET` | `/LiveTv/Channels/{Id}` | 需认证 | `BaseItemDto` | Gets a live tv channel |
| 6 | `GET` | `/LiveTv/ChannelTags` | 需认证 | `QueryResult<BaseItemDto>` | Gets live tv channel tags |
| 7 | `GET` | `/LiveTv/ChannelTags/Prefixes` | 需认证 | `TagItem[]` | Gets live tv channel tag prefixes |
| 8 | `GET` | `/LiveTv/EPG` | 需认证 | `QueryResult<EpgRow>` | Gets the epg. |
| 9 | `GET` | `/LiveTv/Folder` | 需认证 | `BaseItemDto` | Gets the top level live tv folder |
| 10 | `GET` | `/LiveTv/GuideInfo` | 需认证 | `GuideInfo` | Gets guide info |
| 11 | `GET` | `/LiveTv/Info` | 需认证 | `LiveTvInfo` | Gets available live tv services. |
| 12 | `DELETE` | `/LiveTv/ListingProviders` | 认证 (`Admin`) | `void` | Deletes a listing provider |
| 13 | `GET` | `/LiveTv/ListingProviders` | 认证 (`Admin`) | `ListingsProviderInfo[]` | Gets current listing providers |
| 14 | `POST` | `/LiveTv/ListingProviders` | 认证 (`Admin`) | `ListingsProviderInfo` | Adds a listing provider |
| 15 | `GET` | `/LiveTv/ListingProviders/Available` | 认证 (`Admin`) | `ListingProviderTypeInfo[]` | Gets listing provider |
| 16 | `GET` | `/LiveTv/ListingProviders/Default` | 需认证 | `ListingsProviderInfo` | - |
| 17 | `POST` | `/LiveTv/ListingProviders/Delete` | 认证 (`Admin`) | `void` | Deletes a listing provider |
| 18 | `GET` | `/LiveTv/ListingProviders/Lineups` | 认证 (`Admin`) | `List<NameIdPair>` | Gets available lineups |
| 19 | `GET` | `/LiveTv/LiveRecordings/{Id}/hls/live.m3u8` | 需认证 | `object` | Gets a live recording |
| 20 | `HEAD` | `/LiveTv/LiveRecordings/{Id}/hls/live.m3u8` | 需认证 | `object` | Gets a live recording |
| 21 | `GET` | `/LiveTv/LiveRecordings/{Id}/hls/master.m3u8` | 需认证 | `object` | Gets a live recording |
| 22 | `HEAD` | `/LiveTv/LiveRecordings/{Id}/hls/master.m3u8` | 需认证 | `object` | Gets a live recording |
| 23 | `GET` | `/LiveTv/LiveRecordings/{Id}/hls/{Segment}` | 需认证 | `object` | Gets a live recording |
| 24 | `HEAD` | `/LiveTv/LiveRecordings/{Id}/hls/{Segment}` | 需认证 | `object` | Gets a live recording |
| 25 | `GET` | `/LiveTv/LiveRecordings/{Id}/stream` | 需认证 | `object` | Gets a live tv channel |
| 26 | `GET` | `/LiveTv/LiveStreamFiles/{Id}/hls/live.m3u8` | 需认证 | `object` | Gets a live tv channel |
| 27 | `HEAD` | `/LiveTv/LiveStreamFiles/{Id}/hls/live.m3u8` | 需认证 | `object` | Gets a live tv channel |
| 28 | `GET` | `/LiveTv/LiveStreamFiles/{Id}/hls/live_local.m3u8` *(隐藏)* | 需认证 | `object` | Gets a live tv channel |
| 29 | `HEAD` | `/LiveTv/LiveStreamFiles/{Id}/hls/live_local.m3u8` *(隐藏)* | 需认证 | `object` | Gets a live tv channel |
| 30 | `GET` | `/LiveTv/LiveStreamFiles/{Id}/hls/master.m3u8` | 需认证 | `object` | Gets a live tv channel |
| 31 | `HEAD` | `/LiveTv/LiveStreamFiles/{Id}/hls/master.m3u8` | 需认证 | `object` | Gets a live tv channel |
| 32 | `GET` | `/LiveTv/LiveStreamFiles/{Id}/hls/master_local.m3u8` *(隐藏)* | 需认证 | `object` | Gets a live tv channel |
| 33 | `HEAD` | `/LiveTv/LiveStreamFiles/{Id}/hls/master_local.m3u8` *(隐藏)* | 需认证 | `object` | Gets a live tv channel |
| 34 | `GET` | `/LiveTv/LiveStreamFiles/{Id}/hls/{Segment}` | 需认证 | `object` | Gets a live tv channel |
| 35 | `HEAD` | `/LiveTv/LiveStreamFiles/{Id}/hls/{Segment}` | 需认证 | `object` | Gets a live tv channel |
| 36 | `GET` | `/LiveTv/LiveStreamFiles/{Id}/stream.{Container}` | 需认证 | `object` | Gets a live tv channel |
| 37 | `GET` | `/LiveTv/Manage/Channels` | 认证 (`Admin`) | `QueryResult<BaseItemDto>` | Gets the channel management list |
| 38 | `POST` | `/LiveTv/Manage/Channels/{Id}/Disabled` | 认证 (`Admin`) | `QueryResult<ChannelManagementInfo>` | Sets a channel disabled or not |
| 39 | `POST` | `/LiveTv/Manage/Channels/{Id}/SortIndex` | 认证 (`Admin`) | `QueryResult<ChannelManagementInfo>` | Sets a channel sort index |
| 40 | `GET,POST` | `/LiveTv/Programs` | 需认证 | `object` | Gets available live tv epgs.. |
| 41 | `GET` | `/LiveTv/Programs/Recommended` | 需认证 | `QueryResult<BaseItemDto>` | Gets available live tv epgs.. |
| 42 | `GET` | `/LiveTv/Programs/{Id}` | 需认证 | `BaseItemDto` | Gets a live tv program |
| 43 | `GET` | `/LiveTv/Recordings` | 需认证 | `object` | Gets live tv recordings |
| 44 | `GET` | `/LiveTv/Recordings/Folders` | 需认证 | `BaseItemDto[]` | Gets recording folders |
| 45 | `GET` | `/LiveTv/Recordings/Groups` | 需认证 | `QueryResult<BaseItemDto>` | Gets live tv recording groups |
| 46 | `GET` | `/LiveTv/Recordings/Series` | 需认证 | `QueryResult<BaseItemDto>` | Gets live tv recordings |
| 47 | `DELETE` | `/LiveTv/Recordings/{Id}` | 认证 (`managelivetv`) | `void` | Deletes a live tv recording |
| 48 | `GET` | `/LiveTv/Recordings/{Id}` | 需认证 | `BaseItemDto` | Gets a live tv recording |
| 49 | `POST` | `/LiveTv/Recordings/{Id}/Delete` | 认证 (`managelivetv`) | `void` | Deletes a live tv recording |
| 50 | `GET` | `/LiveTv/SeriesTimers` | 需认证 | `QueryResult<SeriesTimerInfoDto>` | Gets live tv series timers |
| 51 | `POST` | `/LiveTv/SeriesTimers` | 认证 (`managelivetv`) | `SeriesTimerInfoDto` | Creates a live tv series timer |
| 52 | `DELETE` | `/LiveTv/SeriesTimers/{Id}` | 认证 (`managelivetv`) | `void` | Cancels a live tv series timer |
| 53 | `GET` | `/LiveTv/SeriesTimers/{Id}` | 需认证 | `TimerInfoDto` | Gets a live tv series timer |
| 54 | `POST` | `/LiveTv/SeriesTimers/{Id}` | 认证 (`managelivetv`) | `void` | Updates a live tv series timer |
| 55 | `POST` | `/LiveTv/SeriesTimers/{Id}/Delete` | 认证 (`managelivetv`) | `void` | Cancels a live tv series timer |
| 56 | `GET` | `/LiveTv/Timers` | 需认证 | `QueryResult<TimerInfoDto>` | Gets live tv timers |
| 57 | `POST` | `/LiveTv/Timers` | 认证 (`managelivetv`) | `void` | Creates a live tv timer |
| 58 | `GET` | `/LiveTv/Timers/Defaults` | 需认证 | `SeriesTimerInfoDto` | Gets default values for a new timer |
| 59 | `DELETE` | `/LiveTv/Timers/{Id}` | 认证 (`managelivetv`) | `void` | Cancels a live tv timer |
| 60 | `GET` | `/LiveTv/Timers/{Id}` | 需认证 | `TimerInfoDto` | Gets a live tv timer |
| 61 | `POST` | `/LiveTv/Timers/{Id}` | 认证 (`managelivetv`) | `void` | Updates a live tv timer |
| 62 | `POST` | `/LiveTv/Timers/{Id}/Delete` | 认证 (`managelivetv`) | `void` | Cancels a live tv timer |
| 63 | `DELETE` | `/LiveTv/TunerHosts` | 认证 (`Admin`) | `void` | Deletes a tuner host |
| 64 | `GET` | `/LiveTv/TunerHosts` | 认证 (`Admin`) | `TunerHostInfo[]` | Gets tuner hosts |
| 65 | `POST` | `/LiveTv/TunerHosts` | 认证 (`Admin`) | `TunerHostInfo` | Adds a tuner host |
| 66 | `GET` | `/LiveTv/TunerHosts/Default/{Type}` | 认证 (`Admin`) | `TunerHostInfo` | Gets tuner hosts |
| 67 | `POST` | `/LiveTv/TunerHosts/Delete` | 认证 (`Admin`) | `void` | Deletes a tuner host |
| 68 | `GET` | `/LiveTv/TunerHosts/Types` | 需认证 | `List<NameIdPair>` | - |
| 69 | `GET` | `/LiveTv/Tuners/Discover` | 需认证 | `List<TunerHostInfo>` | - |
| 70 | `GET` | `/LiveTv/Tuners/Discvover` | 需认证 | `List<TunerHostInfo>` | - |
| 71 | `POST` | `/LiveTv/Tuners/{Id}/Reset` | 认证 (`Admin`) | `void` | Resets a tv tuner |

## 接口详情

### LiveStreamService（18 条）

#### `GET` `/LiveTv/LiveRecordings/{Id}/hls/live.m3u8`

- **摘要**：Gets a live recording
- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.LiveTV.Api.LiveStreamService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.GetLiveRecordingHlsVariant` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 否 | 否 | - |

#### `HEAD` `/LiveTv/LiveRecordings/{Id}/hls/live.m3u8`

- **摘要**：Gets a live recording
- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.LiveTV.Api.LiveStreamService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.GetLiveRecordingHlsVariant` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 否 | 否 | - |

#### `GET` `/LiveTv/LiveRecordings/{Id}/hls/master.m3u8`

- **摘要**：Gets a live recording
- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.LiveTV.Api.LiveStreamService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.GetLiveRecordingHlsMaster` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 否 | 否 | - |

#### `HEAD` `/LiveTv/LiveRecordings/{Id}/hls/master.m3u8`

- **摘要**：Gets a live recording
- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.LiveTV.Api.LiveStreamService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.GetLiveRecordingHlsMaster` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 否 | 否 | - |

#### `GET` `/LiveTv/LiveRecordings/{Id}/hls/{Segment}`

- **摘要**：Gets a live recording
- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.LiveTV.Api.LiveStreamService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.GetLiveRecordingSegment` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 否 | 否 | - |
| `Segment` | `path` | `string` | 否 | 否 | - |

#### `HEAD` `/LiveTv/LiveRecordings/{Id}/hls/{Segment}`

- **摘要**：Gets a live recording
- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.LiveTV.Api.LiveStreamService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.GetLiveRecordingSegment` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 否 | 否 | - |
| `Segment` | `path` | `string` | 否 | 否 | - |

#### `GET` `/LiveTv/LiveRecordings/{Id}/stream`

- **摘要**：Gets a live tv channel
- **鉴权**：需要认证；仅限本地调用
- **返回类型**：`object`
- **处理服务**：`Emby.LiveTV.Api.LiveStreamService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.GetLiveRecordingFile` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 否 | 否 | - |

#### `GET` `/LiveTv/LiveStreamFiles/{Id}/hls/live.m3u8`

- **摘要**：Gets a live tv channel
- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.LiveTV.Api.LiveStreamService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.GetLiveStreamHlsVariant` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 否 | 否 | - |

#### `HEAD` `/LiveTv/LiveStreamFiles/{Id}/hls/live.m3u8`

- **摘要**：Gets a live tv channel
- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.LiveTV.Api.LiveStreamService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.GetLiveStreamHlsVariant` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 否 | 否 | - |

#### `GET` `/LiveTv/LiveStreamFiles/{Id}/hls/live_local.m3u8` `[隐藏接口]`

- **摘要**：Gets a live tv channel
- **鉴权**：需要认证；仅限本地调用
- **返回类型**：`object`
- **处理服务**：`Emby.LiveTV.Api.LiveStreamService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.GetLocalLiveStreamHlsVariant` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 否 | 否 | - |

#### `HEAD` `/LiveTv/LiveStreamFiles/{Id}/hls/live_local.m3u8` `[隐藏接口]`

- **摘要**：Gets a live tv channel
- **鉴权**：需要认证；仅限本地调用
- **返回类型**：`object`
- **处理服务**：`Emby.LiveTV.Api.LiveStreamService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.GetLocalLiveStreamHlsVariant` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 否 | 否 | - |

#### `GET` `/LiveTv/LiveStreamFiles/{Id}/hls/master.m3u8`

- **摘要**：Gets a live tv channel
- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.LiveTV.Api.LiveStreamService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.GetLiveStreamHlsMaster` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 否 | 否 | - |

#### `HEAD` `/LiveTv/LiveStreamFiles/{Id}/hls/master.m3u8`

- **摘要**：Gets a live tv channel
- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.LiveTV.Api.LiveStreamService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.GetLiveStreamHlsMaster` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 否 | 否 | - |

#### `GET` `/LiveTv/LiveStreamFiles/{Id}/hls/master_local.m3u8` `[隐藏接口]`

- **摘要**：Gets a live tv channel
- **鉴权**：需要认证；仅限本地调用
- **返回类型**：`object`
- **处理服务**：`Emby.LiveTV.Api.LiveStreamService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.GetLocalLiveStreamHlsMaster` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 否 | 否 | - |

#### `HEAD` `/LiveTv/LiveStreamFiles/{Id}/hls/master_local.m3u8` `[隐藏接口]`

- **摘要**：Gets a live tv channel
- **鉴权**：需要认证；仅限本地调用
- **返回类型**：`object`
- **处理服务**：`Emby.LiveTV.Api.LiveStreamService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.GetLocalLiveStreamHlsMaster` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 否 | 否 | - |

#### `GET` `/LiveTv/LiveStreamFiles/{Id}/hls/{Segment}`

- **摘要**：Gets a live tv channel
- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.LiveTV.Api.LiveStreamService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.GetLiveStreamSegment` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 否 | 否 | - |
| `Segment` | `path` | `string` | 否 | 否 | - |

#### `HEAD` `/LiveTv/LiveStreamFiles/{Id}/hls/{Segment}`

- **摘要**：Gets a live tv channel
- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.LiveTV.Api.LiveStreamService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.GetLiveStreamSegment` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 否 | 否 | - |
| `Segment` | `path` | `string` | 否 | 否 | - |

#### `GET` `/LiveTv/LiveStreamFiles/{Id}/stream.{Container}`

- **摘要**：Gets a live tv channel
- **鉴权**：需要认证；仅限本地调用
- **返回类型**：`object`
- **处理服务**：`Emby.LiveTV.Api.LiveStreamService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.GetLiveStreamFile` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Container` | `path` | `string` | 否 | 否 | - |
| `Id` | `path` | `string` | 否 | 否 | - |

### LiveTvService（52 条）

#### `GET` `/LiveTv/AvailableRecordingOptions`

- **摘要**：Gets available recording options
- **鉴权**：需要认证；角色要求: `managelivetv`
- **返回类型**：`AvailableRecordingOptions`
- **处理服务**：`Emby.LiveTV.Api.LiveTvService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.GetAvailableRecordingOptions` (`Emby.LiveTV.dll`)

_无请求参数。_

#### `GET` `/LiveTv/ChannelMappingOptions`

- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`object`
- **处理服务**：`Emby.LiveTV.Api.LiveTvService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.GetChannelMappingOptions` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `query` | `string` | 是 | 否 | Provider id |

#### `POST` `/LiveTv/ChannelMappings`

- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`object`
- **处理服务**：`Emby.LiveTV.Api.LiveTvService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.SetChannelMapping` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `query` | `string` | 是 | 否 | Provider id |
| `ProviderChannelId` | `query` | `string` | 否 | 否 | - |
| `TunerChannelId` | `query` | `string` | 否 | 否 | - |

#### `GET` `/LiveTv/Channels`

- **摘要**：Gets available live tv channels.
- **鉴权**：需要认证
- **返回类型**：`QueryResult<BaseItemDto>`
- **处理服务**：`Emby.LiveTV.Api.LiveTvService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.GetChannels` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `AddCurrentProgram` | `query` | `bool` | 否 | 否 | Optional. Adds current program info to each channel |
| `AdjacentTo` | `query` | `string` | 否 | 否 | Optional. Return items that are siblings of a supplied item. |
| `AirDays` | `query` | `DayOfWeek[]` | 否 | 否 | - |
| `AiredDuringSeason` | `query` | `int?` | 否 | 否 | Gets all episodes that aired during a season, including specials. |
| `AlbumArtistIds` | `query` | `string` | 否 | 否 | - |
| `AlbumArtistStartsWithOrGreater` | `query` | `string` | 否 | 否 | Optional filter by items whose name is sorted equally or greater than a given input string. |
| `AlbumIds` | `query` | `string` | 否 | 否 | - |
| `Albums` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on album. This allows multiple, pipe delimeted. |
| `AnyProviderIdEquals` | `query` | `string` | 否 | 是 | Optional. If specified, result will be filtered to contain only items which match at least one of the specified IDs. Each provider ID must be in the form 'prov.id', e.g. 'imdb.tt123456'. This allows multiple, comma delimeted value pairs. |
| `ArtistIds` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on artist. This allows multiple, pipe delimeted. |
| `Artists` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on artist. This allows multiple, pipe delimeted. |
| `ArtistStartsWithOrGreater` | `query` | `string` | 否 | 否 | Optional filter by items whose name is sorted equally or greater than a given input string. |
| `ArtistType` | `query` | `string` | 否 | 否 | Artist or AlbumArtist |
| `AudioCodecs` | `query` | `string` | 否 | 是 | Optional filter by AudioCodec. Allows multiple, comma delimeted. |
| `AudioLanguages` | `query` | `string` | 否 | 否 | - |
| `AudioLayouts` | `query` | `string` | 否 | 是 | Optional filter by AudioLayout. Allows multiple, comma delimeted. |
| `CanEditItems` | `query` | `bool?` | 否 | 否 | - |
| `CollectionIds` | `query` | `string` | 否 | 否 | - |
| `CollectionTypes` | `query` | `string` | 否 | 否 | - |
| `ComposerArtistIds` | `query` | `string` | 否 | 否 | - |
| `Containers` | `query` | `string` | 否 | 是 | Optional filter by Container. Allows multiple, comma delimeted. |
| `ContributingArtistIds` | `query` | `string` | 否 | 否 | - |
| `EnableFavoriteSorting` | `query` | `bool` | 否 | 否 | Incorporate favorite and like status into channel sorting. |
| `EnableImages` | `query` | `bool?` | 否 | 否 | Optional, include image information in output |
| `EnableImageTypes` | `query` | `string` | 否 | 否 | Optional. The image types to include in the output. |
| `EnableTotalRecordCount` | `query` | `bool` | 否 | 否 | - |
| `EnableUserData` | `query` | `bool?` | 否 | 否 | Optional, include user data |
| `ExcludeArtistIds` | `query` | `string` | 否 | 否 | - |
| `ExcludeItemIds` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered by exxcluding item ids. This allows multiple, comma delimeted. |
| `ExcludeItemTypes` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on item type. This allows multiple, comma delimeted. |
| `ExcludeSources` | `query` | `string[]` | 否 | 否 | - |
| `ExcludeTagIds` | `query` | `string` | 否 | 否 | - |
| `ExcludeTags` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on tag. This allows multiple, pipe delimeted. |
| `ExtendedVideoTypes` | `query` | `string` | 否 | 是 | Optional filter by ExtendedVideoType. Allows multiple, comma delimeted. |
| `Fields` | `query` | `string` | 否 | 是 | Optional. Specify additional fields of information to return in the output. This allows multiple, comma delimeted. Options: Budget, Chapters, DateCreated, Genres, HomePageUrl, IndexOptions, MediaStreams, Overview, ParentId, Path, People, ProviderIds, PrimaryImageAspectRatio, Revenue, SortName, Studios, Taglines |
| `Filters` | `query` | `string` | 否 | 是 | Optional. Specify additional filters to apply. This allows multiple, comma delimeted. Options: IsFolder, IsNotFolder, IsUnplayed, IsPlayed, IsFavorite, IsResumable, Likes, Dislikes |
| `GenreIds` | `query` | `string` | 否 | 否 | - |
| `Genres` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on genre. This allows multiple, pipe delimeted. |
| `GroupByPresentationUniqueKey` | `query` | `bool?` | 否 | 否 | - |
| `GroupItemsInto` | `query` | `ItemLinkType?` | 否 | 否 | - |
| `GroupItemsIntoCollections` | `query` | `bool` | 否 | 否 | Whether or not to hide items behind their boxsets. |
| `GroupProgramsBySeries` | `query` | `bool` | 否 | 否 | - |
| `HasAired` | `query` | `bool?` | 否 | 否 | - |
| `HasImdbId` | `query` | `bool?` | 否 | 否 | Optional filter by items that have an imdb id or not. |
| `HasOfficialRating` | `query` | `bool?` | 否 | 否 | Optional filter by items that have official ratings |
| `HasOverview` | `query` | `bool?` | 否 | 否 | Optional filter by items that have an overview or not. |
| `HasParentalRating` | `query` | `bool?` | 否 | 否 | Optional filter by items that have or do not have a parental rating |
| `HasSpecialFeature` | `query` | `bool?` | 否 | 否 | Optional filter by items with special features. |
| `HasSubtitles` | `query` | `bool?` | 否 | 否 | Optional filter by items with subtitles. |
| `HasThemeSong` | `query` | `bool?` | 否 | 否 | Optional filter by items with theme songs. |
| `HasThemeVideo` | `query` | `bool?` | 否 | 否 | Optional filter by items with theme videos. |
| `HasTmdbId` | `query` | `bool?` | 否 | 否 | Optional filter by items that have a tmdb id or not. |
| `HasTrailer` | `query` | `bool?` | 否 | 否 | Optional filter by items with trailers. |
| `HasTvdbId` | `query` | `bool?` | 否 | 否 | Optional filter by items that have a tvdb id or not. |
| `Ids` | `query` | `string` | 否 | 是 | Optional. If specific items are needed, specify a list of item id's to retrieve. This allows multiple, comma delimited. |
| `ImageTypeLimit` | `query` | `int?` | 否 | 否 | Optional, the max number of images to return, per image type |
| `ImageTypes` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on those containing image types. This allows multiple, comma delimited. |
| `IncludeItemTypes` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on item type. This allows multiple, comma delimeted. |
| `Is3D` | `query` | `bool?` | 否 | 否 | Optional filter by items that are 3D, or not. |
| `Is4K` | `query` | `bool?` | 否 | 否 | - |
| `IsAiring` | `query` | `bool?` | 否 | 否 | - |
| `IsDisliked` | `query` | `bool?` | 否 | 否 | Filter by channels that are disliked, or not. |
| `IsDuplicate` | `query` | `bool?` | 否 | 否 | - |
| `IsFavorite` | `query` | `bool?` | 否 | 否 | Optional filter by items that are marked as favorite, or not. |
| `IsFolder` | `query` | `bool?` | 否 | 否 | Optional filter for folders. |
| `IsHD` | `query` | `bool?` | 否 | 否 | Optional filter by items that are HD or not. |
| `IsKids` | `query` | `bool?` | 否 | 否 | Optional filter for kids. |
| `IsLiked` | `query` | `bool?` | 否 | 否 | Filter by channels that are liked, or not. |
| `IsLocked` | `query` | `bool?` | 否 | 否 | Optional filter by items that are locked. |
| `IsMovie` | `query` | `bool?` | 否 | 否 | Optional filter for movies. |
| `IsNew` | `query` | `bool?` | 否 | 否 | Optional filter for IsNew. |
| `IsNewOrPremiere` | `query` | `bool?` | 否 | 否 | Optional filter for IsNewOrPremiere. |
| `IsNews` | `query` | `bool?` | 否 | 否 | Optional filter for news. |
| `IsPlaceHolder` | `query` | `bool?` | 否 | 否 | Optional filter by items that are placeholders |
| `IsPlayed` | `query` | `bool?` | 否 | 否 | Optional filter by items that are played, or not. |
| `IsPremiere` | `query` | `bool?` | 否 | 否 | Optional filter for IsPremiere. |
| `IsRepeat` | `query` | `bool?` | 否 | 否 | Optional filter for IsRepeat. |
| `IsSeries` | `query` | `bool?` | 否 | 否 | Optional filter for series. |
| `IsSpecialEpisode` | `query` | `bool?` | 否 | 否 | - |
| `IsSpecialSeason` | `query` | `bool?` | 否 | 否 | Optional. Filter by special season. |
| `IsSports` | `query` | `bool?` | 否 | 否 | Optional filter for sports. |
| `IsStandaloneSpecial` | `query` | `bool?` | 否 | 否 | - |
| `IsUnaired` | `query` | `bool?` | 否 | 否 | Optional filter by items that are unaired episodes or not. |
| `ItemPersonTypes` | `query` | `PersonType[]` | 否 | 否 | - |
| `Limit` | `query` | `int?` | 否 | 否 | Optional. The maximum number of records to return |
| `ListItemIds` | `query` | `string` | 否 | 否 | - |
| `MatchAnyWord` | `query` | `bool` | 否 | 否 | - |
| `MaxDateCreated` | `query` | `DateTimeOffset?` | 否 | 否 | - |
| `MaxEndDate` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The maximum premiere date. Format = ISO |
| `MaxHeight` | `query` | `int?` | 否 | 否 | - |
| `MaxOfficialRating` | `query` | `string` | 否 | 否 | Optional filter by maximum official rating (PG, PG-13, TV-MA, etc). |
| `MaxPlayers` | `query` | `int?` | 否 | 否 | Optional filter by maximum number of game players. |
| `MaxPremiereDate` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The maximum premiere date. Format = ISO |
| `MaxStartDate` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The maximum premiere date. Format = ISO |
| `MaxWidth` | `query` | `int?` | 否 | 否 | - |
| `MediaTypes` | `query` | `string` | 否 | 是 | Optional filter by MediaType. Allows multiple, comma delimited. |
| `MinCommunityRating` | `query` | `double?` | 否 | 否 | Optional filter by minimum community rating. |
| `MinCriticRating` | `query` | `double?` | 否 | 否 | Optional filter by minimum critic rating. |
| `MinDateCreated` | `query` | `DateTimeOffset?` | 否 | 否 | - |
| `MinDateLastSaved` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The minimum premiere date. Format = ISO |
| `MinDateLastSavedForUser` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The minimum premiere date. Format = ISO |
| `MinEndDate` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The minimum premiere date. Format = ISO |
| `MinHeight` | `query` | `int?` | 否 | 否 | - |
| `MinIndexNumber` | `query` | `int?` | 否 | 否 | Optional filter by minimum index number. |
| `MinOfficialRating` | `query` | `string` | 否 | 否 | Optional filter by minimum official rating (PG, PG-13, TV-MA, etc). |
| `MinPlayers` | `query` | `int?` | 否 | 否 | Optional filter by minimum number of game players. |
| `MinPremiereDate` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The minimum premiere date. Format = ISO |
| `MinStartDate` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The minimum premiere date. Format = ISO |
| `MinWidth` | `query` | `int?` | 否 | 否 | - |
| `Name` | `query` | `string` | 否 | 否 | - |
| `NameLessThan` | `query` | `string` | 否 | 否 | Optional filter by items whose name is equally or lesser than a given input string. |
| `NameStartsWith` | `query` | `string` | 否 | 否 | Optional filter by items whose name is sorted equally than a given input string. |
| `NameStartsWithOrGreater` | `query` | `string` | 否 | 否 | Optional filter by items whose name is sorted equally or greater than a given input string. |
| `OfficialRatings` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on OfficialRating. This allows multiple, pipe delimeted. |
| `OuterIds` | `query` | `string` | 否 | 否 | - |
| `ParentId` | `query` | `string` | 否 | 否 | Specify this to localize the search to a specific item or folder. Omit to use the root |
| `ParentIndexNumber` | `query` | `int?` | 否 | 否 | Optional filter by parent index number. |
| `Path` | `query` | `string` | 否 | 是 | Optional filter by Path. |
| `Person` | `query` | `string` | 否 | 否 | Optional. If specified, results will be filtered to include only those containing the specified person. |
| `PersonIds` | `query` | `string` | 否 | 否 | Optional. If specified, results will be filtered to include only those containing the specified person. |
| `PersonTypes` | `query` | `string` | 否 | 否 | Optional. If specified, along with Person, results will be filtered to include only those containing the specified person and PersonType. Allows multiple, comma-delimited |
| `ProjectToMedia` | `query` | `bool` | 否 | 否 | ProjectToMedia |
| `RandomSeed` | `query` | `int` | 否 | 否 | - |
| `RecordingKeyword` | `query` | `string` | 否 | 否 | - |
| `RecordingKeywordType` | `query` | `KeywordType?` | 否 | 否 | - |
| `Recursive` | `query` | `bool` | 否 | 否 | When searching within folders, this determines whether or not the search will be recursive. true/false |
| `SearchTerm` | `query` | `string` | 否 | 否 | Enter a search term to perform a search request |
| `SeriesStatus` | `query` | `string` | 否 | 是 | Optional filter by Series Status. Allows multiple, comma delimeted. |
| `SortBy` | `query` | `string` | 否 | 是 | Optional. Specify one or more sort orders, comma delimeted. Options: Album, AlbumArtist, Artist, Budget, CommunityRating, CriticRating, DateCreated, DatePlayed, PlayCount, PremiereDate, ProductionYear, SortName, Random, Revenue, Runtime |
| `SortOrder` | `query` | `string` | 否 | 否 | Sort Order - Ascending,Descending |
| `StartIndex` | `query` | `int?` | 否 | 否 | Optional. The record index to start at. All items with a lower index will be dropped from the results. |
| `StartItemId` | `query` | `string` | 否 | 否 | Optional. Skip through the list until a given item is found. |
| `StudioIds` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on studio. This allows multiple, pipe delimeted. |
| `Studios` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on studio. This allows multiple, pipe delimeted. |
| `SubtitleCodecs` | `query` | `string` | 否 | 是 | Optional filter by SubtitleCodec. Allows multiple, comma delimeted. |
| `SubtitleLanguages` | `query` | `string` | 否 | 否 | - |
| `TagIds` | `query` | `string` | 否 | 否 | - |
| `Tags` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on tag. This allows multiple, pipe delimeted. |
| `Type` | `query` | `ChannelType?` | 否 | 否 | Optional filter by channel type. |
| `UserId` | `query` | `Guid` | 否 | 否 | User Id |
| `VideoCodecs` | `query` | `string` | 否 | 是 | Optional filter by VideoCodec. Allows multiple, comma delimeted. |
| `VideoTypes` | `query` | `string` | 否 | 是 | Optional filter by VideoType (videofile, dvd, bluray, iso). Allows multiple, comma delimeted. |
| `WasSearched` | `query` | `bool?` | 否 | 否 | - |
| `Years` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on production year. This allows multiple, comma delimeted. |

#### `GET` `/LiveTv/Channels/{Id}`

- **摘要**：Gets a live tv channel
- **鉴权**：需要认证
- **返回类型**：`BaseItemDto`
- **处理服务**：`Emby.LiveTV.Api.LiveTvService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.GetChannel` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Channel Id |
| `UserId` | `query` | `Guid` | 否 | 否 | Optional attach user data. |

#### `GET` `/LiveTv/ChannelTags`

- **摘要**：Gets live tv channel tags
- **鉴权**：需要认证
- **返回类型**：`QueryResult<BaseItemDto>`
- **处理服务**：`Emby.LiveTV.Api.LiveTvService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.GetLiveTvChannelTags` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `AdjacentTo` | `query` | `string` | 否 | 否 | Optional. Return items that are siblings of a supplied item. |
| `AirDays` | `query` | `DayOfWeek[]` | 否 | 否 | - |
| `AiredDuringSeason` | `query` | `int?` | 否 | 否 | Gets all episodes that aired during a season, including specials. |
| `AlbumArtistIds` | `query` | `string` | 否 | 否 | - |
| `AlbumArtistStartsWithOrGreater` | `query` | `string` | 否 | 否 | Optional filter by items whose name is sorted equally or greater than a given input string. |
| `AlbumIds` | `query` | `string` | 否 | 否 | - |
| `Albums` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on album. This allows multiple, pipe delimeted. |
| `AnyProviderIdEquals` | `query` | `string` | 否 | 是 | Optional. If specified, result will be filtered to contain only items which match at least one of the specified IDs. Each provider ID must be in the form 'prov.id', e.g. 'imdb.tt123456'. This allows multiple, comma delimeted value pairs. |
| `ArtistIds` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on artist. This allows multiple, pipe delimeted. |
| `Artists` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on artist. This allows multiple, pipe delimeted. |
| `ArtistStartsWithOrGreater` | `query` | `string` | 否 | 否 | Optional filter by items whose name is sorted equally or greater than a given input string. |
| `ArtistType` | `query` | `string` | 否 | 否 | Artist or AlbumArtist |
| `AudioCodecs` | `query` | `string` | 否 | 是 | Optional filter by AudioCodec. Allows multiple, comma delimeted. |
| `AudioLanguages` | `query` | `string` | 否 | 否 | - |
| `AudioLayouts` | `query` | `string` | 否 | 是 | Optional filter by AudioLayout. Allows multiple, comma delimeted. |
| `CanEditItems` | `query` | `bool?` | 否 | 否 | - |
| `CollectionIds` | `query` | `string` | 否 | 否 | - |
| `CollectionTypes` | `query` | `string` | 否 | 否 | - |
| `ComposerArtistIds` | `query` | `string` | 否 | 否 | - |
| `Containers` | `query` | `string` | 否 | 是 | Optional filter by Container. Allows multiple, comma delimeted. |
| `ContributingArtistIds` | `query` | `string` | 否 | 否 | - |
| `EnableImages` | `query` | `bool?` | 否 | 否 | Optional, include image information in output |
| `EnableImageTypes` | `query` | `string` | 否 | 否 | Optional. The image types to include in the output. |
| `EnableTotalRecordCount` | `query` | `bool` | 否 | 否 | - |
| `EnableUserData` | `query` | `bool?` | 否 | 否 | Optional, include user data |
| `ExcludeArtistIds` | `query` | `string` | 否 | 否 | - |
| `ExcludeItemIds` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered by exxcluding item ids. This allows multiple, comma delimeted. |
| `ExcludeItemTypes` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on item type. This allows multiple, comma delimeted. |
| `ExcludeSources` | `query` | `string[]` | 否 | 否 | - |
| `ExcludeTagIds` | `query` | `string` | 否 | 否 | - |
| `ExcludeTags` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on tag. This allows multiple, pipe delimeted. |
| `ExtendedVideoTypes` | `query` | `string` | 否 | 是 | Optional filter by ExtendedVideoType. Allows multiple, comma delimeted. |
| `Fields` | `query` | `string` | 否 | 是 | Optional. Specify additional fields of information to return in the output. This allows multiple, comma delimeted. Options: Budget, Chapters, DateCreated, Genres, HomePageUrl, IndexOptions, MediaStreams, Overview, ParentId, Path, People, ProviderIds, PrimaryImageAspectRatio, Revenue, SortName, Studios, Taglines |
| `Filters` | `query` | `string` | 否 | 是 | Optional. Specify additional filters to apply. This allows multiple, comma delimeted. Options: IsFolder, IsNotFolder, IsUnplayed, IsPlayed, IsFavorite, IsResumable, Likes, Dislikes |
| `GenreIds` | `query` | `string` | 否 | 否 | - |
| `Genres` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on genre. This allows multiple, pipe delimeted. |
| `GroupByPresentationUniqueKey` | `query` | `bool?` | 否 | 否 | - |
| `GroupItemsInto` | `query` | `ItemLinkType?` | 否 | 否 | - |
| `GroupItemsIntoCollections` | `query` | `bool` | 否 | 否 | Whether or not to hide items behind their boxsets. |
| `GroupProgramsBySeries` | `query` | `bool` | 否 | 否 | - |
| `HasAired` | `query` | `bool?` | 否 | 否 | - |
| `HasImdbId` | `query` | `bool?` | 否 | 否 | Optional filter by items that have an imdb id or not. |
| `HasOfficialRating` | `query` | `bool?` | 否 | 否 | Optional filter by items that have official ratings |
| `HasOverview` | `query` | `bool?` | 否 | 否 | Optional filter by items that have an overview or not. |
| `HasParentalRating` | `query` | `bool?` | 否 | 否 | Optional filter by items that have or do not have a parental rating |
| `HasSpecialFeature` | `query` | `bool?` | 否 | 否 | Optional filter by items with special features. |
| `HasSubtitles` | `query` | `bool?` | 否 | 否 | Optional filter by items with subtitles. |
| `HasThemeSong` | `query` | `bool?` | 否 | 否 | Optional filter by items with theme songs. |
| `HasThemeVideo` | `query` | `bool?` | 否 | 否 | Optional filter by items with theme videos. |
| `HasTmdbId` | `query` | `bool?` | 否 | 否 | Optional filter by items that have a tmdb id or not. |
| `HasTrailer` | `query` | `bool?` | 否 | 否 | Optional filter by items with trailers. |
| `HasTvdbId` | `query` | `bool?` | 否 | 否 | Optional filter by items that have a tvdb id or not. |
| `Ids` | `query` | `string` | 否 | 是 | Optional. If specific items are needed, specify a list of item id's to retrieve. This allows multiple, comma delimited. |
| `ImageTypeLimit` | `query` | `int?` | 否 | 否 | Optional, the max number of images to return, per image type |
| `ImageTypes` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on those containing image types. This allows multiple, comma delimited. |
| `IncludeItemTypes` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on item type. This allows multiple, comma delimeted. |
| `Is3D` | `query` | `bool?` | 否 | 否 | Optional filter by items that are 3D, or not. |
| `Is4K` | `query` | `bool?` | 否 | 否 | - |
| `IsAiring` | `query` | `bool?` | 否 | 否 | - |
| `IsDuplicate` | `query` | `bool?` | 否 | 否 | - |
| `IsFavorite` | `query` | `bool?` | 否 | 否 | Optional filter by items that are marked as favorite, or not. |
| `IsFolder` | `query` | `bool?` | 否 | 否 | Optional filter for folders. |
| `IsHD` | `query` | `bool?` | 否 | 否 | Optional filter by items that are HD or not. |
| `IsKids` | `query` | `bool?` | 否 | 否 | Optional filter for kids. |
| `IsLocked` | `query` | `bool?` | 否 | 否 | Optional filter by items that are locked. |
| `IsMovie` | `query` | `bool?` | 否 | 否 | Optional filter for movies. |
| `IsNew` | `query` | `bool?` | 否 | 否 | Optional filter for IsNew. |
| `IsNewOrPremiere` | `query` | `bool?` | 否 | 否 | Optional filter for IsNewOrPremiere. |
| `IsNews` | `query` | `bool?` | 否 | 否 | Optional filter for news. |
| `IsPlaceHolder` | `query` | `bool?` | 否 | 否 | Optional filter by items that are placeholders |
| `IsPlayed` | `query` | `bool?` | 否 | 否 | Optional filter by items that are played, or not. |
| `IsPremiere` | `query` | `bool?` | 否 | 否 | Optional filter for IsPremiere. |
| `IsRepeat` | `query` | `bool?` | 否 | 否 | Optional filter for IsRepeat. |
| `IsSeries` | `query` | `bool?` | 否 | 否 | Optional filter for series. |
| `IsSpecialEpisode` | `query` | `bool?` | 否 | 否 | - |
| `IsSpecialSeason` | `query` | `bool?` | 否 | 否 | Optional. Filter by special season. |
| `IsSports` | `query` | `bool?` | 否 | 否 | Optional filter for sports. |
| `IsStandaloneSpecial` | `query` | `bool?` | 否 | 否 | - |
| `IsUnaired` | `query` | `bool?` | 否 | 否 | Optional filter by items that are unaired episodes or not. |
| `ItemPersonTypes` | `query` | `PersonType[]` | 否 | 否 | - |
| `Limit` | `query` | `int?` | 否 | 否 | Optional. The maximum number of records to return |
| `ListItemIds` | `query` | `string` | 否 | 否 | - |
| `MatchAnyWord` | `query` | `bool` | 否 | 否 | - |
| `MaxDateCreated` | `query` | `DateTimeOffset?` | 否 | 否 | - |
| `MaxEndDate` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The maximum premiere date. Format = ISO |
| `MaxHeight` | `query` | `int?` | 否 | 否 | - |
| `MaxOfficialRating` | `query` | `string` | 否 | 否 | Optional filter by maximum official rating (PG, PG-13, TV-MA, etc). |
| `MaxPlayers` | `query` | `int?` | 否 | 否 | Optional filter by maximum number of game players. |
| `MaxPremiereDate` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The maximum premiere date. Format = ISO |
| `MaxStartDate` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The maximum premiere date. Format = ISO |
| `MaxWidth` | `query` | `int?` | 否 | 否 | - |
| `MediaTypes` | `query` | `string` | 否 | 是 | Optional filter by MediaType. Allows multiple, comma delimited. |
| `MinCommunityRating` | `query` | `double?` | 否 | 否 | Optional filter by minimum community rating. |
| `MinCriticRating` | `query` | `double?` | 否 | 否 | Optional filter by minimum critic rating. |
| `MinDateCreated` | `query` | `DateTimeOffset?` | 否 | 否 | - |
| `MinDateLastSaved` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The minimum premiere date. Format = ISO |
| `MinDateLastSavedForUser` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The minimum premiere date. Format = ISO |
| `MinEndDate` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The minimum premiere date. Format = ISO |
| `MinHeight` | `query` | `int?` | 否 | 否 | - |
| `MinIndexNumber` | `query` | `int?` | 否 | 否 | Optional filter by minimum index number. |
| `MinOfficialRating` | `query` | `string` | 否 | 否 | Optional filter by minimum official rating (PG, PG-13, TV-MA, etc). |
| `MinPlayers` | `query` | `int?` | 否 | 否 | Optional filter by minimum number of game players. |
| `MinPremiereDate` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The minimum premiere date. Format = ISO |
| `MinStartDate` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The minimum premiere date. Format = ISO |
| `MinWidth` | `query` | `int?` | 否 | 否 | - |
| `Name` | `query` | `string` | 否 | 否 | - |
| `NameLessThan` | `query` | `string` | 否 | 否 | Optional filter by items whose name is equally or lesser than a given input string. |
| `NameStartsWith` | `query` | `string` | 否 | 否 | Optional filter by items whose name is sorted equally than a given input string. |
| `NameStartsWithOrGreater` | `query` | `string` | 否 | 否 | Optional filter by items whose name is sorted equally or greater than a given input string. |
| `OfficialRatings` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on OfficialRating. This allows multiple, pipe delimeted. |
| `OuterIds` | `query` | `string` | 否 | 否 | - |
| `ParentId` | `query` | `string` | 否 | 否 | Specify this to localize the search to a specific item or folder. Omit to use the root |
| `ParentIndexNumber` | `query` | `int?` | 否 | 否 | Optional filter by parent index number. |
| `Path` | `query` | `string` | 否 | 是 | Optional filter by Path. |
| `Person` | `query` | `string` | 否 | 否 | Optional. If specified, results will be filtered to include only those containing the specified person. |
| `PersonIds` | `query` | `string` | 否 | 否 | Optional. If specified, results will be filtered to include only those containing the specified person. |
| `PersonTypes` | `query` | `string` | 否 | 否 | Optional. If specified, along with Person, results will be filtered to include only those containing the specified person and PersonType. Allows multiple, comma-delimited |
| `ProjectToMedia` | `query` | `bool` | 否 | 否 | ProjectToMedia |
| `RandomSeed` | `query` | `int` | 否 | 否 | - |
| `RecordingKeyword` | `query` | `string` | 否 | 否 | - |
| `RecordingKeywordType` | `query` | `KeywordType?` | 否 | 否 | - |
| `Recursive` | `query` | `bool` | 否 | 否 | When searching within folders, this determines whether or not the search will be recursive. true/false |
| `SearchTerm` | `query` | `string` | 否 | 否 | Enter a search term to perform a search request |
| `SeriesStatus` | `query` | `string` | 否 | 是 | Optional filter by Series Status. Allows multiple, comma delimeted. |
| `SortBy` | `query` | `string` | 否 | 是 | Optional. Specify one or more sort orders, comma delimeted. Options: Album, AlbumArtist, Artist, Budget, CommunityRating, CriticRating, DateCreated, DatePlayed, PlayCount, PremiereDate, ProductionYear, SortName, Random, Revenue, Runtime |
| `SortOrder` | `query` | `string` | 否 | 否 | Sort Order - Ascending,Descending |
| `StartIndex` | `query` | `int?` | 否 | 否 | Optional. The record index to start at. All items with a lower index will be dropped from the results. |
| `StartItemId` | `query` | `string` | 否 | 否 | Optional. Skip through the list until a given item is found. |
| `StudioIds` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on studio. This allows multiple, pipe delimeted. |
| `Studios` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on studio. This allows multiple, pipe delimeted. |
| `SubtitleCodecs` | `query` | `string` | 否 | 是 | Optional filter by SubtitleCodec. Allows multiple, comma delimeted. |
| `SubtitleLanguages` | `query` | `string` | 否 | 否 | - |
| `TagIds` | `query` | `string` | 否 | 否 | - |
| `Tags` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on tag. This allows multiple, pipe delimeted. |
| `UserId` | `query` | `Guid` | 否 | 否 | User Id |
| `VideoCodecs` | `query` | `string` | 否 | 是 | Optional filter by VideoCodec. Allows multiple, comma delimeted. |
| `VideoTypes` | `query` | `string` | 否 | 是 | Optional filter by VideoType (videofile, dvd, bluray, iso). Allows multiple, comma delimeted. |
| `WasSearched` | `query` | `bool?` | 否 | 否 | - |
| `Years` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on production year. This allows multiple, comma delimeted. |

#### `GET` `/LiveTv/ChannelTags/Prefixes`

- **摘要**：Gets live tv channel tag prefixes
- **鉴权**：需要认证
- **返回类型**：`TagItem[]`
- **处理服务**：`Emby.LiveTV.Api.LiveTvService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.GetLiveTvChannelTagPrefixes` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `AdjacentTo` | `query` | `string` | 否 | 否 | Optional. Return items that are siblings of a supplied item. |
| `AirDays` | `query` | `DayOfWeek[]` | 否 | 否 | - |
| `AiredDuringSeason` | `query` | `int?` | 否 | 否 | Gets all episodes that aired during a season, including specials. |
| `AlbumArtistIds` | `query` | `string` | 否 | 否 | - |
| `AlbumArtistStartsWithOrGreater` | `query` | `string` | 否 | 否 | Optional filter by items whose name is sorted equally or greater than a given input string. |
| `AlbumIds` | `query` | `string` | 否 | 否 | - |
| `Albums` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on album. This allows multiple, pipe delimeted. |
| `AnyProviderIdEquals` | `query` | `string` | 否 | 是 | Optional. If specified, result will be filtered to contain only items which match at least one of the specified IDs. Each provider ID must be in the form 'prov.id', e.g. 'imdb.tt123456'. This allows multiple, comma delimeted value pairs. |
| `ArtistIds` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on artist. This allows multiple, pipe delimeted. |
| `Artists` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on artist. This allows multiple, pipe delimeted. |
| `ArtistStartsWithOrGreater` | `query` | `string` | 否 | 否 | Optional filter by items whose name is sorted equally or greater than a given input string. |
| `ArtistType` | `query` | `string` | 否 | 否 | Artist or AlbumArtist |
| `AudioCodecs` | `query` | `string` | 否 | 是 | Optional filter by AudioCodec. Allows multiple, comma delimeted. |
| `AudioLanguages` | `query` | `string` | 否 | 否 | - |
| `AudioLayouts` | `query` | `string` | 否 | 是 | Optional filter by AudioLayout. Allows multiple, comma delimeted. |
| `CanEditItems` | `query` | `bool?` | 否 | 否 | - |
| `CollectionIds` | `query` | `string` | 否 | 否 | - |
| `CollectionTypes` | `query` | `string` | 否 | 否 | - |
| `ComposerArtistIds` | `query` | `string` | 否 | 否 | - |
| `Containers` | `query` | `string` | 否 | 是 | Optional filter by Container. Allows multiple, comma delimeted. |
| `ContributingArtistIds` | `query` | `string` | 否 | 否 | - |
| `EnableImages` | `query` | `bool?` | 否 | 否 | Optional, include image information in output |
| `EnableImageTypes` | `query` | `string` | 否 | 否 | Optional. The image types to include in the output. |
| `EnableTotalRecordCount` | `query` | `bool` | 否 | 否 | - |
| `EnableUserData` | `query` | `bool?` | 否 | 否 | Optional, include user data |
| `ExcludeArtistIds` | `query` | `string` | 否 | 否 | - |
| `ExcludeItemIds` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered by exxcluding item ids. This allows multiple, comma delimeted. |
| `ExcludeItemTypes` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on item type. This allows multiple, comma delimeted. |
| `ExcludeSources` | `query` | `string[]` | 否 | 否 | - |
| `ExcludeTagIds` | `query` | `string` | 否 | 否 | - |
| `ExcludeTags` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on tag. This allows multiple, pipe delimeted. |
| `ExtendedVideoTypes` | `query` | `string` | 否 | 是 | Optional filter by ExtendedVideoType. Allows multiple, comma delimeted. |
| `Fields` | `query` | `string` | 否 | 是 | Optional. Specify additional fields of information to return in the output. This allows multiple, comma delimeted. Options: Budget, Chapters, DateCreated, Genres, HomePageUrl, IndexOptions, MediaStreams, Overview, ParentId, Path, People, ProviderIds, PrimaryImageAspectRatio, Revenue, SortName, Studios, Taglines |
| `Filters` | `query` | `string` | 否 | 是 | Optional. Specify additional filters to apply. This allows multiple, comma delimeted. Options: IsFolder, IsNotFolder, IsUnplayed, IsPlayed, IsFavorite, IsResumable, Likes, Dislikes |
| `GenreIds` | `query` | `string` | 否 | 否 | - |
| `Genres` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on genre. This allows multiple, pipe delimeted. |
| `GroupByPresentationUniqueKey` | `query` | `bool?` | 否 | 否 | - |
| `GroupItemsInto` | `query` | `ItemLinkType?` | 否 | 否 | - |
| `GroupItemsIntoCollections` | `query` | `bool` | 否 | 否 | Whether or not to hide items behind their boxsets. |
| `GroupProgramsBySeries` | `query` | `bool` | 否 | 否 | - |
| `HasAired` | `query` | `bool?` | 否 | 否 | - |
| `HasImdbId` | `query` | `bool?` | 否 | 否 | Optional filter by items that have an imdb id or not. |
| `HasOfficialRating` | `query` | `bool?` | 否 | 否 | Optional filter by items that have official ratings |
| `HasOverview` | `query` | `bool?` | 否 | 否 | Optional filter by items that have an overview or not. |
| `HasParentalRating` | `query` | `bool?` | 否 | 否 | Optional filter by items that have or do not have a parental rating |
| `HasSpecialFeature` | `query` | `bool?` | 否 | 否 | Optional filter by items with special features. |
| `HasSubtitles` | `query` | `bool?` | 否 | 否 | Optional filter by items with subtitles. |
| `HasThemeSong` | `query` | `bool?` | 否 | 否 | Optional filter by items with theme songs. |
| `HasThemeVideo` | `query` | `bool?` | 否 | 否 | Optional filter by items with theme videos. |
| `HasTmdbId` | `query` | `bool?` | 否 | 否 | Optional filter by items that have a tmdb id or not. |
| `HasTrailer` | `query` | `bool?` | 否 | 否 | Optional filter by items with trailers. |
| `HasTvdbId` | `query` | `bool?` | 否 | 否 | Optional filter by items that have a tvdb id or not. |
| `Ids` | `query` | `string` | 否 | 是 | Optional. If specific items are needed, specify a list of item id's to retrieve. This allows multiple, comma delimited. |
| `ImageTypeLimit` | `query` | `int?` | 否 | 否 | Optional, the max number of images to return, per image type |
| `ImageTypes` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on those containing image types. This allows multiple, comma delimited. |
| `IncludeItemTypes` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on item type. This allows multiple, comma delimeted. |
| `Is3D` | `query` | `bool?` | 否 | 否 | Optional filter by items that are 3D, or not. |
| `Is4K` | `query` | `bool?` | 否 | 否 | - |
| `IsAiring` | `query` | `bool?` | 否 | 否 | - |
| `IsDuplicate` | `query` | `bool?` | 否 | 否 | - |
| `IsFavorite` | `query` | `bool?` | 否 | 否 | Optional filter by items that are marked as favorite, or not. |
| `IsFolder` | `query` | `bool?` | 否 | 否 | Optional filter for folders. |
| `IsHD` | `query` | `bool?` | 否 | 否 | Optional filter by items that are HD or not. |
| `IsKids` | `query` | `bool?` | 否 | 否 | Optional filter for kids. |
| `IsLocked` | `query` | `bool?` | 否 | 否 | Optional filter by items that are locked. |
| `IsMovie` | `query` | `bool?` | 否 | 否 | Optional filter for movies. |
| `IsNew` | `query` | `bool?` | 否 | 否 | Optional filter for IsNew. |
| `IsNewOrPremiere` | `query` | `bool?` | 否 | 否 | Optional filter for IsNewOrPremiere. |
| `IsNews` | `query` | `bool?` | 否 | 否 | Optional filter for news. |
| `IsPlaceHolder` | `query` | `bool?` | 否 | 否 | Optional filter by items that are placeholders |
| `IsPlayed` | `query` | `bool?` | 否 | 否 | Optional filter by items that are played, or not. |
| `IsPremiere` | `query` | `bool?` | 否 | 否 | Optional filter for IsPremiere. |
| `IsRepeat` | `query` | `bool?` | 否 | 否 | Optional filter for IsRepeat. |
| `IsSeries` | `query` | `bool?` | 否 | 否 | Optional filter for series. |
| `IsSpecialEpisode` | `query` | `bool?` | 否 | 否 | - |
| `IsSpecialSeason` | `query` | `bool?` | 否 | 否 | Optional. Filter by special season. |
| `IsSports` | `query` | `bool?` | 否 | 否 | Optional filter for sports. |
| `IsStandaloneSpecial` | `query` | `bool?` | 否 | 否 | - |
| `IsUnaired` | `query` | `bool?` | 否 | 否 | Optional filter by items that are unaired episodes or not. |
| `ItemPersonTypes` | `query` | `PersonType[]` | 否 | 否 | - |
| `Limit` | `query` | `int?` | 否 | 否 | Optional. The maximum number of records to return |
| `ListItemIds` | `query` | `string` | 否 | 否 | - |
| `MatchAnyWord` | `query` | `bool` | 否 | 否 | - |
| `MaxDateCreated` | `query` | `DateTimeOffset?` | 否 | 否 | - |
| `MaxEndDate` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The maximum premiere date. Format = ISO |
| `MaxHeight` | `query` | `int?` | 否 | 否 | - |
| `MaxOfficialRating` | `query` | `string` | 否 | 否 | Optional filter by maximum official rating (PG, PG-13, TV-MA, etc). |
| `MaxPlayers` | `query` | `int?` | 否 | 否 | Optional filter by maximum number of game players. |
| `MaxPremiereDate` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The maximum premiere date. Format = ISO |
| `MaxStartDate` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The maximum premiere date. Format = ISO |
| `MaxWidth` | `query` | `int?` | 否 | 否 | - |
| `MediaTypes` | `query` | `string` | 否 | 是 | Optional filter by MediaType. Allows multiple, comma delimited. |
| `MinCommunityRating` | `query` | `double?` | 否 | 否 | Optional filter by minimum community rating. |
| `MinCriticRating` | `query` | `double?` | 否 | 否 | Optional filter by minimum critic rating. |
| `MinDateCreated` | `query` | `DateTimeOffset?` | 否 | 否 | - |
| `MinDateLastSaved` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The minimum premiere date. Format = ISO |
| `MinDateLastSavedForUser` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The minimum premiere date. Format = ISO |
| `MinEndDate` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The minimum premiere date. Format = ISO |
| `MinHeight` | `query` | `int?` | 否 | 否 | - |
| `MinIndexNumber` | `query` | `int?` | 否 | 否 | Optional filter by minimum index number. |
| `MinOfficialRating` | `query` | `string` | 否 | 否 | Optional filter by minimum official rating (PG, PG-13, TV-MA, etc). |
| `MinPlayers` | `query` | `int?` | 否 | 否 | Optional filter by minimum number of game players. |
| `MinPremiereDate` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The minimum premiere date. Format = ISO |
| `MinStartDate` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The minimum premiere date. Format = ISO |
| `MinWidth` | `query` | `int?` | 否 | 否 | - |
| `Name` | `query` | `string` | 否 | 否 | - |
| `NameLessThan` | `query` | `string` | 否 | 否 | Optional filter by items whose name is equally or lesser than a given input string. |
| `NameStartsWith` | `query` | `string` | 否 | 否 | Optional filter by items whose name is sorted equally than a given input string. |
| `NameStartsWithOrGreater` | `query` | `string` | 否 | 否 | Optional filter by items whose name is sorted equally or greater than a given input string. |
| `OfficialRatings` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on OfficialRating. This allows multiple, pipe delimeted. |
| `OuterIds` | `query` | `string` | 否 | 否 | - |
| `ParentId` | `query` | `string` | 否 | 否 | Specify this to localize the search to a specific item or folder. Omit to use the root |
| `ParentIndexNumber` | `query` | `int?` | 否 | 否 | Optional filter by parent index number. |
| `Path` | `query` | `string` | 否 | 是 | Optional filter by Path. |
| `Person` | `query` | `string` | 否 | 否 | Optional. If specified, results will be filtered to include only those containing the specified person. |
| `PersonIds` | `query` | `string` | 否 | 否 | Optional. If specified, results will be filtered to include only those containing the specified person. |
| `PersonTypes` | `query` | `string` | 否 | 否 | Optional. If specified, along with Person, results will be filtered to include only those containing the specified person and PersonType. Allows multiple, comma-delimited |
| `ProjectToMedia` | `query` | `bool` | 否 | 否 | ProjectToMedia |
| `RandomSeed` | `query` | `int` | 否 | 否 | - |
| `RecordingKeyword` | `query` | `string` | 否 | 否 | - |
| `RecordingKeywordType` | `query` | `KeywordType?` | 否 | 否 | - |
| `Recursive` | `query` | `bool` | 否 | 否 | When searching within folders, this determines whether or not the search will be recursive. true/false |
| `SearchTerm` | `query` | `string` | 否 | 否 | Enter a search term to perform a search request |
| `SeriesStatus` | `query` | `string` | 否 | 是 | Optional filter by Series Status. Allows multiple, comma delimeted. |
| `SortBy` | `query` | `string` | 否 | 是 | Optional. Specify one or more sort orders, comma delimeted. Options: Album, AlbumArtist, Artist, Budget, CommunityRating, CriticRating, DateCreated, DatePlayed, PlayCount, PremiereDate, ProductionYear, SortName, Random, Revenue, Runtime |
| `SortOrder` | `query` | `string` | 否 | 否 | Sort Order - Ascending,Descending |
| `StartIndex` | `query` | `int?` | 否 | 否 | Optional. The record index to start at. All items with a lower index will be dropped from the results. |
| `StartItemId` | `query` | `string` | 否 | 否 | Optional. Skip through the list until a given item is found. |
| `StudioIds` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on studio. This allows multiple, pipe delimeted. |
| `Studios` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on studio. This allows multiple, pipe delimeted. |
| `SubtitleCodecs` | `query` | `string` | 否 | 是 | Optional filter by SubtitleCodec. Allows multiple, comma delimeted. |
| `SubtitleLanguages` | `query` | `string` | 否 | 否 | - |
| `TagIds` | `query` | `string` | 否 | 否 | - |
| `Tags` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on tag. This allows multiple, pipe delimeted. |
| `UserId` | `query` | `Guid` | 否 | 否 | User Id |
| `VideoCodecs` | `query` | `string` | 否 | 是 | Optional filter by VideoCodec. Allows multiple, comma delimeted. |
| `VideoTypes` | `query` | `string` | 否 | 是 | Optional filter by VideoType (videofile, dvd, bluray, iso). Allows multiple, comma delimeted. |
| `WasSearched` | `query` | `bool?` | 否 | 否 | - |
| `Years` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on production year. This allows multiple, comma delimeted. |

#### `GET` `/LiveTv/EPG`

- **摘要**：Gets the epg.
- **鉴权**：需要认证
- **返回类型**：`QueryResult<EpgRow>`
- **处理服务**：`Emby.LiveTV.Api.LiveTvService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.GetEpg` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `AddCurrentProgram` | `query` | `bool` | 否 | 否 | Optional. Adds current program info to each channel |
| `AdjacentTo` | `query` | `string` | 否 | 否 | Optional. Return items that are siblings of a supplied item. |
| `AirDays` | `query` | `DayOfWeek[]` | 否 | 否 | - |
| `AiredDuringSeason` | `query` | `int?` | 否 | 否 | Gets all episodes that aired during a season, including specials. |
| `AlbumArtistIds` | `query` | `string` | 否 | 否 | - |
| `AlbumArtistStartsWithOrGreater` | `query` | `string` | 否 | 否 | Optional filter by items whose name is sorted equally or greater than a given input string. |
| `AlbumIds` | `query` | `string` | 否 | 否 | - |
| `Albums` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on album. This allows multiple, pipe delimeted. |
| `AnyProviderIdEquals` | `query` | `string` | 否 | 是 | Optional. If specified, result will be filtered to contain only items which match at least one of the specified IDs. Each provider ID must be in the form 'prov.id', e.g. 'imdb.tt123456'. This allows multiple, comma delimeted value pairs. |
| `ArtistIds` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on artist. This allows multiple, pipe delimeted. |
| `Artists` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on artist. This allows multiple, pipe delimeted. |
| `ArtistStartsWithOrGreater` | `query` | `string` | 否 | 否 | Optional filter by items whose name is sorted equally or greater than a given input string. |
| `ArtistType` | `query` | `string` | 否 | 否 | Artist or AlbumArtist |
| `AudioCodecs` | `query` | `string` | 否 | 是 | Optional filter by AudioCodec. Allows multiple, comma delimeted. |
| `AudioLanguages` | `query` | `string` | 否 | 否 | - |
| `AudioLayouts` | `query` | `string` | 否 | 是 | Optional filter by AudioLayout. Allows multiple, comma delimeted. |
| `CanEditItems` | `query` | `bool?` | 否 | 否 | - |
| `ChannelIds` | `query` | `string` | 否 | 否 | The channels to return guide information for. |
| `CollectionIds` | `query` | `string` | 否 | 否 | - |
| `CollectionTypes` | `query` | `string` | 否 | 否 | - |
| `ComposerArtistIds` | `query` | `string` | 否 | 否 | - |
| `Containers` | `query` | `string` | 否 | 是 | Optional filter by Container. Allows multiple, comma delimeted. |
| `ContributingArtistIds` | `query` | `string` | 否 | 否 | - |
| `EnableFavoriteSorting` | `query` | `bool` | 否 | 否 | Incorporate favorite and like status into channel sorting. |
| `EnableImages` | `query` | `bool?` | 否 | 否 | Optional, include image information in output |
| `EnableImageTypes` | `query` | `string` | 否 | 否 | Optional. The image types to include in the output. |
| `EnableTotalRecordCount` | `query` | `bool` | 否 | 否 | - |
| `EnableUserData` | `query` | `bool?` | 否 | 否 | Optional, include user data |
| `ExcludeArtistIds` | `query` | `string` | 否 | 否 | - |
| `ExcludeItemIds` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered by exxcluding item ids. This allows multiple, comma delimeted. |
| `ExcludeItemTypes` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on item type. This allows multiple, comma delimeted. |
| `ExcludeSources` | `query` | `string[]` | 否 | 否 | - |
| `ExcludeTagIds` | `query` | `string` | 否 | 否 | - |
| `ExcludeTags` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on tag. This allows multiple, pipe delimeted. |
| `ExtendedVideoTypes` | `query` | `string` | 否 | 是 | Optional filter by ExtendedVideoType. Allows multiple, comma delimeted. |
| `Fields` | `query` | `string` | 否 | 是 | Optional. Specify additional fields of information to return in the output. This allows multiple, comma delimeted. Options: Budget, Chapters, DateCreated, Genres, HomePageUrl, IndexOptions, MediaStreams, Overview, ParentId, Path, People, ProviderIds, PrimaryImageAspectRatio, Revenue, SortName, Studios, Taglines |
| `Filters` | `query` | `string` | 否 | 是 | Optional. Specify additional filters to apply. This allows multiple, comma delimeted. Options: IsFolder, IsNotFolder, IsUnplayed, IsPlayed, IsFavorite, IsResumable, Likes, Dislikes |
| `GenreIds` | `query` | `string` | 否 | 否 | - |
| `Genres` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on genre. This allows multiple, pipe delimeted. |
| `GroupByPresentationUniqueKey` | `query` | `bool?` | 否 | 否 | - |
| `GroupItemsInto` | `query` | `ItemLinkType?` | 否 | 否 | - |
| `GroupItemsIntoCollections` | `query` | `bool` | 否 | 否 | Whether or not to hide items behind their boxsets. |
| `GroupProgramsBySeries` | `query` | `bool` | 否 | 否 | - |
| `HasAired` | `query` | `bool?` | 否 | 否 | - |
| `HasImdbId` | `query` | `bool?` | 否 | 否 | Optional filter by items that have an imdb id or not. |
| `HasOfficialRating` | `query` | `bool?` | 否 | 否 | Optional filter by items that have official ratings |
| `HasOverview` | `query` | `bool?` | 否 | 否 | Optional filter by items that have an overview or not. |
| `HasParentalRating` | `query` | `bool?` | 否 | 否 | Optional filter by items that have or do not have a parental rating |
| `HasSpecialFeature` | `query` | `bool?` | 否 | 否 | Optional filter by items with special features. |
| `HasSubtitles` | `query` | `bool?` | 否 | 否 | Optional filter by items with subtitles. |
| `HasThemeSong` | `query` | `bool?` | 否 | 否 | Optional filter by items with theme songs. |
| `HasThemeVideo` | `query` | `bool?` | 否 | 否 | Optional filter by items with theme videos. |
| `HasTmdbId` | `query` | `bool?` | 否 | 否 | Optional filter by items that have a tmdb id or not. |
| `HasTrailer` | `query` | `bool?` | 否 | 否 | Optional filter by items with trailers. |
| `HasTvdbId` | `query` | `bool?` | 否 | 否 | Optional filter by items that have a tvdb id or not. |
| `Ids` | `query` | `string` | 否 | 是 | Optional. If specific items are needed, specify a list of item id's to retrieve. This allows multiple, comma delimited. |
| `ImageTypeLimit` | `query` | `int?` | 否 | 否 | Optional, the max number of images to return, per image type |
| `ImageTypes` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on those containing image types. This allows multiple, comma delimited. |
| `IncludeItemTypes` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on item type. This allows multiple, comma delimeted. |
| `Is3D` | `query` | `bool?` | 否 | 否 | Optional filter by items that are 3D, or not. |
| `Is4K` | `query` | `bool?` | 否 | 否 | - |
| `IsAiring` | `query` | `bool?` | 否 | 否 | - |
| `IsDisliked` | `query` | `bool?` | 否 | 否 | Filter by channels that are disliked, or not. |
| `IsDuplicate` | `query` | `bool?` | 否 | 否 | - |
| `IsFavorite` | `query` | `bool?` | 否 | 否 | Optional filter by items that are marked as favorite, or not. |
| `IsFolder` | `query` | `bool?` | 否 | 否 | Optional filter for folders. |
| `IsHD` | `query` | `bool?` | 否 | 否 | Optional filter by items that are HD or not. |
| `IsKids` | `query` | `bool?` | 否 | 否 | Optional filter for kids. |
| `IsLiked` | `query` | `bool?` | 否 | 否 | Filter by channels that are liked, or not. |
| `IsLocked` | `query` | `bool?` | 否 | 否 | Optional filter by items that are locked. |
| `IsMovie` | `query` | `bool?` | 否 | 否 | Optional filter for movies. |
| `IsNew` | `query` | `bool?` | 否 | 否 | Optional filter for IsNew. |
| `IsNewOrPremiere` | `query` | `bool?` | 否 | 否 | Optional filter for IsNewOrPremiere. |
| `IsNews` | `query` | `bool?` | 否 | 否 | Optional filter for news. |
| `IsPlaceHolder` | `query` | `bool?` | 否 | 否 | Optional filter by items that are placeholders |
| `IsPlayed` | `query` | `bool?` | 否 | 否 | Optional filter by items that are played, or not. |
| `IsPremiere` | `query` | `bool?` | 否 | 否 | Optional filter for IsPremiere. |
| `IsRepeat` | `query` | `bool?` | 否 | 否 | Optional filter for IsRepeat. |
| `IsSeries` | `query` | `bool?` | 否 | 否 | Optional filter for series. |
| `IsSpecialEpisode` | `query` | `bool?` | 否 | 否 | - |
| `IsSpecialSeason` | `query` | `bool?` | 否 | 否 | Optional. Filter by special season. |
| `IsSports` | `query` | `bool?` | 否 | 否 | Optional filter for sports. |
| `IsStandaloneSpecial` | `query` | `bool?` | 否 | 否 | - |
| `IsUnaired` | `query` | `bool?` | 否 | 否 | Optional filter by items that are unaired episodes or not. |
| `ItemPersonTypes` | `query` | `PersonType[]` | 否 | 否 | - |
| `Limit` | `query` | `int?` | 否 | 否 | Optional. The maximum number of records to return |
| `ListItemIds` | `query` | `string` | 否 | 否 | - |
| `MatchAnyWord` | `query` | `bool` | 否 | 否 | - |
| `MaxDateCreated` | `query` | `DateTimeOffset?` | 否 | 否 | - |
| `MaxEndDate` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The maximum premiere date. Format = ISO |
| `MaxHeight` | `query` | `int?` | 否 | 否 | - |
| `MaxOfficialRating` | `query` | `string` | 否 | 否 | Optional filter by maximum official rating (PG, PG-13, TV-MA, etc). |
| `MaxPlayers` | `query` | `int?` | 否 | 否 | Optional filter by maximum number of game players. |
| `MaxPremiereDate` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The maximum premiere date. Format = ISO |
| `MaxStartDate` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The maximum premiere date. Format = ISO |
| `MaxWidth` | `query` | `int?` | 否 | 否 | - |
| `MediaTypes` | `query` | `string` | 否 | 是 | Optional filter by MediaType. Allows multiple, comma delimited. |
| `MinCommunityRating` | `query` | `double?` | 否 | 否 | Optional filter by minimum community rating. |
| `MinCriticRating` | `query` | `double?` | 否 | 否 | Optional filter by minimum critic rating. |
| `MinDateCreated` | `query` | `DateTimeOffset?` | 否 | 否 | - |
| `MinDateLastSaved` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The minimum premiere date. Format = ISO |
| `MinDateLastSavedForUser` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The minimum premiere date. Format = ISO |
| `MinEndDate` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The minimum premiere date. Format = ISO |
| `MinHeight` | `query` | `int?` | 否 | 否 | - |
| `MinIndexNumber` | `query` | `int?` | 否 | 否 | Optional filter by minimum index number. |
| `MinOfficialRating` | `query` | `string` | 否 | 否 | Optional filter by minimum official rating (PG, PG-13, TV-MA, etc). |
| `MinPlayers` | `query` | `int?` | 否 | 否 | Optional filter by minimum number of game players. |
| `MinPremiereDate` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The minimum premiere date. Format = ISO |
| `MinStartDate` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The minimum premiere date. Format = ISO |
| `MinWidth` | `query` | `int?` | 否 | 否 | - |
| `Name` | `query` | `string` | 否 | 否 | - |
| `NameLessThan` | `query` | `string` | 否 | 否 | Optional filter by items whose name is equally or lesser than a given input string. |
| `NameStartsWith` | `query` | `string` | 否 | 否 | Optional filter by items whose name is sorted equally than a given input string. |
| `NameStartsWithOrGreater` | `query` | `string` | 否 | 否 | Optional filter by items whose name is sorted equally or greater than a given input string. |
| `OfficialRatings` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on OfficialRating. This allows multiple, pipe delimeted. |
| `OuterIds` | `query` | `string` | 否 | 否 | - |
| `ParentId` | `query` | `string` | 否 | 否 | Specify this to localize the search to a specific item or folder. Omit to use the root |
| `ParentIndexNumber` | `query` | `int?` | 否 | 否 | Optional filter by parent index number. |
| `Path` | `query` | `string` | 否 | 是 | Optional filter by Path. |
| `Person` | `query` | `string` | 否 | 否 | Optional. If specified, results will be filtered to include only those containing the specified person. |
| `PersonIds` | `query` | `string` | 否 | 否 | Optional. If specified, results will be filtered to include only those containing the specified person. |
| `PersonTypes` | `query` | `string` | 否 | 否 | Optional. If specified, along with Person, results will be filtered to include only those containing the specified person and PersonType. Allows multiple, comma-delimited |
| `ProgramFields` | `query` | `string` | 否 | 否 | - |
| `ProjectToMedia` | `query` | `bool` | 否 | 否 | ProjectToMedia |
| `RandomSeed` | `query` | `int` | 否 | 否 | - |
| `RecordingKeyword` | `query` | `string` | 否 | 否 | - |
| `RecordingKeywordType` | `query` | `KeywordType?` | 否 | 否 | - |
| `Recursive` | `query` | `bool` | 否 | 否 | When searching within folders, this determines whether or not the search will be recursive. true/false |
| `SearchTerm` | `query` | `string` | 否 | 否 | Enter a search term to perform a search request |
| `SeriesStatus` | `query` | `string` | 否 | 是 | Optional filter by Series Status. Allows multiple, comma delimeted. |
| `SortBy` | `query` | `string` | 否 | 是 | Optional. Specify one or more sort orders, comma delimeted. Options: Album, AlbumArtist, Artist, Budget, CommunityRating, CriticRating, DateCreated, DatePlayed, PlayCount, PremiereDate, ProductionYear, SortName, Random, Revenue, Runtime |
| `SortOrder` | `query` | `string` | 否 | 否 | Sort Order - Ascending,Descending |
| `StartIndex` | `query` | `int?` | 否 | 否 | Optional. The record index to start at. All items with a lower index will be dropped from the results. |
| `StartItemId` | `query` | `string` | 否 | 否 | Optional. Skip through the list until a given item is found. |
| `StudioIds` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on studio. This allows multiple, pipe delimeted. |
| `Studios` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on studio. This allows multiple, pipe delimeted. |
| `SubtitleCodecs` | `query` | `string` | 否 | 是 | Optional filter by SubtitleCodec. Allows multiple, comma delimeted. |
| `SubtitleLanguages` | `query` | `string` | 否 | 否 | - |
| `TagIds` | `query` | `string` | 否 | 否 | - |
| `Tags` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on tag. This allows multiple, pipe delimeted. |
| `Type` | `query` | `ChannelType?` | 否 | 否 | Optional filter by channel type. |
| `UserId` | `query` | `Guid` | 否 | 否 | User Id |
| `VideoCodecs` | `query` | `string` | 否 | 是 | Optional filter by VideoCodec. Allows multiple, comma delimeted. |
| `VideoTypes` | `query` | `string` | 否 | 是 | Optional filter by VideoType (videofile, dvd, bluray, iso). Allows multiple, comma delimeted. |
| `WasSearched` | `query` | `bool?` | 否 | 否 | - |
| `Years` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on production year. This allows multiple, comma delimeted. |

#### `GET` `/LiveTv/Folder`

- **摘要**：Gets the top level live tv folder
- **鉴权**：需要认证
- **返回类型**：`BaseItemDto`
- **处理服务**：`Emby.LiveTV.Api.LiveTvService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.GetLiveTvFolder` (`Emby.LiveTV.dll`)

_无请求参数。_

#### `GET` `/LiveTv/GuideInfo`

- **摘要**：Gets guide info
- **鉴权**：需要认证
- **返回类型**：`GuideInfo`
- **处理服务**：`Emby.LiveTV.Api.LiveTvService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.GetGuideInfo` (`Emby.LiveTV.dll`)

_无请求参数。_

#### `GET` `/LiveTv/Info`

- **摘要**：Gets available live tv services.
- **鉴权**：需要认证
- **返回类型**：`LiveTvInfo`
- **处理服务**：`Emby.LiveTV.Api.LiveTvService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.GetLiveTvInfo` (`Emby.LiveTV.dll`)

_无请求参数。_

#### `DELETE` `/LiveTv/ListingProviders`

- **摘要**：Deletes a listing provider
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`void`
- **处理服务**：`Emby.LiveTV.Api.LiveTvService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.DeleteListingProvider` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `query` | `string` | 否 | 否 | Provider id |

#### `GET` `/LiveTv/ListingProviders`

- **摘要**：Gets current listing providers
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`ListingsProviderInfo[]`
- **处理服务**：`Emby.LiveTV.Api.LiveTvService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.GetListingProviders` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `ChannelId` | `query` | `string` | 是 | 否 | Channel id |

#### `POST` `/LiveTv/ListingProviders`

- **摘要**：Adds a listing provider
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`ListingsProviderInfo`
- **处理服务**：`Emby.LiveTV.Api.LiveTvService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.AddListingProvider` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `ChannelMappings` | `query` | `NameValuePair[]` | 否 | 否 | - |
| `Country` | `query` | `string` | 否 | 否 | - |
| `DataVersion` | `query` | `string` | 否 | 否 | - |
| `EnableAllTuners` | `query` | `bool` | 否 | 否 | - |
| `EnabledTuners` | `query` | `string[]` | 否 | 否 | - |
| `Id` | `query` | `string` | 否 | 否 | - |
| `KidsCategories` | `query` | `string[]` | 否 | 否 | - |
| `ListingsId` | `query` | `string` | 否 | 否 | - |
| `MovieCategories` | `query` | `string[]` | 否 | 否 | - |
| `MoviePrefix` | `query` | `string` | 否 | 否 | - |
| `Name` | `query` | `string` | 否 | 否 | - |
| `NewsCategories` | `query` | `string[]` | 否 | 否 | - |
| `Password` | `query` | `string` | 否 | 否 | - |
| `Path` | `query` | `string` | 否 | 否 | - |
| `PreferredLanguage` | `query` | `string` | 否 | 否 | - |
| `Pw` | `query` | `string` | 否 | 否 | - |
| `SetupUrl` | `query` | `string` | 否 | 否 | - |
| `SportsCategories` | `query` | `string[]` | 否 | 否 | - |
| `TvgShiftTicks` | `query` | `long` | 否 | 否 | - |
| `Type` | `query` | `string` | 否 | 否 | - |
| `UserAgent` | `query` | `string` | 否 | 否 | - |
| `Username` | `query` | `string` | 否 | 否 | - |
| `ValidateListings` | `query` | `bool` | 否 | 否 | - |
| `ValidateLogin` | `query` | `bool` | 否 | 否 | - |
| `ZipCode` | `query` | `string` | 否 | 否 | - |

#### `GET` `/LiveTv/ListingProviders/Available`

- **摘要**：Gets listing provider
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`ListingProviderTypeInfo[]`
- **处理服务**：`Emby.LiveTV.Api.LiveTvService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.GetAvailableListingProviders` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Country` | `query` | `string` | 否 | 否 | - |

#### `GET` `/LiveTv/ListingProviders/Default`

- **鉴权**：需要认证
- **返回类型**：`ListingsProviderInfo`
- **处理服务**：`Emby.LiveTV.Api.LiveTvService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.GetDefaultListingProvider` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `ChannelMappings` | `query` | `NameValuePair[]` | 否 | 否 | - |
| `Country` | `query` | `string` | 否 | 否 | - |
| `DataVersion` | `query` | `string` | 否 | 否 | - |
| `EnableAllTuners` | `query` | `bool` | 否 | 否 | - |
| `EnabledTuners` | `query` | `string[]` | 否 | 否 | - |
| `Id` | `query` | `string` | 否 | 否 | - |
| `KidsCategories` | `query` | `string[]` | 否 | 否 | - |
| `ListingsId` | `query` | `string` | 否 | 否 | - |
| `MovieCategories` | `query` | `string[]` | 否 | 否 | - |
| `MoviePrefix` | `query` | `string` | 否 | 否 | - |
| `Name` | `query` | `string` | 否 | 否 | - |
| `NewsCategories` | `query` | `string[]` | 否 | 否 | - |
| `Password` | `query` | `string` | 否 | 否 | - |
| `Path` | `query` | `string` | 否 | 否 | - |
| `PreferredLanguage` | `query` | `string` | 否 | 否 | - |
| `SetupUrl` | `query` | `string` | 否 | 否 | - |
| `SportsCategories` | `query` | `string[]` | 否 | 否 | - |
| `TvgShiftTicks` | `query` | `long` | 否 | 否 | - |
| `Type` | `query` | `string` | 否 | 否 | - |
| `UserAgent` | `query` | `string` | 否 | 否 | - |
| `Username` | `query` | `string` | 否 | 否 | - |
| `ZipCode` | `query` | `string` | 否 | 否 | - |

#### `POST` `/LiveTv/ListingProviders/Delete`

- **摘要**：Deletes a listing provider
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`void`
- **处理服务**：`Emby.LiveTV.Api.LiveTvService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.DeleteListingProvider` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `query` | `string` | 否 | 否 | Provider id |

#### `GET` `/LiveTv/ListingProviders/Lineups`

- **摘要**：Gets available lineups
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`List<NameIdPair>`
- **处理服务**：`Emby.LiveTV.Api.LiveTvService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.GetLineups` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Country` | `query` | `string` | 否 | 否 | Country |
| `Id` | `query` | `string` | 否 | 否 | Provider id |
| `Location` | `query` | `string` | 否 | 否 | Location |
| `Type` | `query` | `string` | 否 | 否 | Provider Type |

#### `GET` `/LiveTv/Manage/Channels`

- **摘要**：Gets the channel management list
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`QueryResult<BaseItemDto>`
- **处理服务**：`Emby.LiveTV.Api.LiveTvService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.GetChannelManagementList` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `ChannelMappingStatus` | `query` | `ChannelMappingStatus?` | 否 | 否 | - |
| `Limit` | `query` | `int?` | 否 | 否 | Optional. The maximum number of records to return |
| `SortBy` | `query` | `string` | 否 | 是 | Optional. Specify one or more sort orders, comma delimeted. Options: Name, StartDate |
| `SortOrder` | `query` | `string` | 否 | 否 | Sort Order - Ascending,Descending |
| `StartIndex` | `query` | `int?` | 否 | 否 | Optional. The record index to start at. All items with a lower index will be dropped from the results. |

#### `POST` `/LiveTv/Manage/Channels/{Id}/Disabled`

- **摘要**：Sets a channel disabled or not
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`QueryResult<ChannelManagementInfo>`
- **处理服务**：`Emby.LiveTV.Api.LiveTvService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.SetChannelDisabled` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 否 | 否 | - |
| `Disabled` | `query` | `bool` | 否 | 否 | - |
| `ManagementId` | `query` | `string` | 否 | 否 | - |

#### `POST` `/LiveTv/Manage/Channels/{Id}/SortIndex`

- **摘要**：Sets a channel sort index
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`QueryResult<ChannelManagementInfo>`
- **处理服务**：`Emby.LiveTV.Api.LiveTvService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.SetChannelSortIndex` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 否 | 否 | - |
| `ManagementId` | `query` | `string` | 否 | 否 | - |
| `NewIndex` | `query` | `int` | 否 | 否 | - |

#### `GET,POST` `/LiveTv/Programs`

- **摘要**：Gets available live tv epgs..
- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.LiveTV.Api.LiveTvService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.GetPrograms` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `AdjacentTo` | `query` | `string` | 否 | 否 | Optional. Return items that are siblings of a supplied item. |
| `AirDays` | `query` | `DayOfWeek[]` | 否 | 否 | - |
| `AiredDuringSeason` | `query` | `int?` | 否 | 否 | Gets all episodes that aired during a season, including specials. |
| `AlbumArtistIds` | `query` | `string` | 否 | 否 | - |
| `AlbumArtistStartsWithOrGreater` | `query` | `string` | 否 | 否 | Optional filter by items whose name is sorted equally or greater than a given input string. |
| `AlbumIds` | `query` | `string` | 否 | 否 | - |
| `Albums` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on album. This allows multiple, pipe delimeted. |
| `AnyProviderIdEquals` | `query` | `string` | 否 | 是 | Optional. If specified, result will be filtered to contain only items which match at least one of the specified IDs. Each provider ID must be in the form 'prov.id', e.g. 'imdb.tt123456'. This allows multiple, comma delimeted value pairs. |
| `ArtistIds` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on artist. This allows multiple, pipe delimeted. |
| `Artists` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on artist. This allows multiple, pipe delimeted. |
| `ArtistStartsWithOrGreater` | `query` | `string` | 否 | 否 | Optional filter by items whose name is sorted equally or greater than a given input string. |
| `ArtistType` | `query` | `string` | 否 | 否 | Artist or AlbumArtist |
| `AudioCodecs` | `query` | `string` | 否 | 是 | Optional filter by AudioCodec. Allows multiple, comma delimeted. |
| `AudioLanguages` | `query` | `string` | 否 | 否 | - |
| `AudioLayouts` | `query` | `string` | 否 | 是 | Optional filter by AudioLayout. Allows multiple, comma delimeted. |
| `CanEditItems` | `query` | `bool?` | 否 | 否 | - |
| `ChannelIds` | `query` | `string` | 否 | 否 | The channels to return guide information for. |
| `CollectionIds` | `query` | `string` | 否 | 否 | - |
| `CollectionTypes` | `query` | `string` | 否 | 否 | - |
| `ComposerArtistIds` | `query` | `string` | 否 | 否 | - |
| `Containers` | `query` | `string` | 否 | 是 | Optional filter by Container. Allows multiple, comma delimeted. |
| `ContributingArtistIds` | `query` | `string` | 否 | 否 | - |
| `EnableImages` | `query` | `bool?` | 否 | 否 | Optional, include image information in output |
| `EnableImageTypes` | `query` | `string` | 否 | 否 | Optional. The image types to include in the output. |
| `EnableTotalRecordCount` | `query` | `bool` | 否 | 否 | - |
| `EnableUserData` | `query` | `bool?` | 否 | 否 | Optional, include user data |
| `ExcludeArtistIds` | `query` | `string` | 否 | 否 | - |
| `ExcludeItemIds` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered by exxcluding item ids. This allows multiple, comma delimeted. |
| `ExcludeItemTypes` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on item type. This allows multiple, comma delimeted. |
| `ExcludeSources` | `query` | `string[]` | 否 | 否 | - |
| `ExcludeTagIds` | `query` | `string` | 否 | 否 | - |
| `ExcludeTags` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on tag. This allows multiple, pipe delimeted. |
| `ExtendedVideoTypes` | `query` | `string` | 否 | 是 | Optional filter by ExtendedVideoType. Allows multiple, comma delimeted. |
| `Fields` | `query` | `string` | 否 | 是 | Optional. Specify additional fields of information to return in the output. This allows multiple, comma delimeted. Options: Budget, Chapters, DateCreated, Genres, HomePageUrl, IndexOptions, MediaStreams, Overview, ParentId, Path, People, ProviderIds, PrimaryImageAspectRatio, Revenue, SortName, Studios, Taglines |
| `Filters` | `query` | `string` | 否 | 是 | Optional. Specify additional filters to apply. This allows multiple, comma delimeted. Options: IsFolder, IsNotFolder, IsUnplayed, IsPlayed, IsFavorite, IsResumable, Likes, Dislikes |
| `GenreIds` | `query` | `string` | 否 | 否 | - |
| `Genres` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on genre. This allows multiple, pipe delimeted. |
| `GroupByPresentationUniqueKey` | `query` | `bool?` | 否 | 否 | - |
| `GroupItemsInto` | `query` | `ItemLinkType?` | 否 | 否 | - |
| `GroupItemsIntoCollections` | `query` | `bool` | 否 | 否 | Whether or not to hide items behind their boxsets. |
| `GroupProgramsBySeries` | `query` | `bool` | 否 | 否 | - |
| `HasAired` | `query` | `bool?` | 否 | 否 | - |
| `HasImdbId` | `query` | `bool?` | 否 | 否 | Optional filter by items that have an imdb id or not. |
| `HasOfficialRating` | `query` | `bool?` | 否 | 否 | Optional filter by items that have official ratings |
| `HasOverview` | `query` | `bool?` | 否 | 否 | Optional filter by items that have an overview or not. |
| `HasParentalRating` | `query` | `bool?` | 否 | 否 | Optional filter by items that have or do not have a parental rating |
| `HasSpecialFeature` | `query` | `bool?` | 否 | 否 | Optional filter by items with special features. |
| `HasSubtitles` | `query` | `bool?` | 否 | 否 | Optional filter by items with subtitles. |
| `HasThemeSong` | `query` | `bool?` | 否 | 否 | Optional filter by items with theme songs. |
| `HasThemeVideo` | `query` | `bool?` | 否 | 否 | Optional filter by items with theme videos. |
| `HasTmdbId` | `query` | `bool?` | 否 | 否 | Optional filter by items that have a tmdb id or not. |
| `HasTrailer` | `query` | `bool?` | 否 | 否 | Optional filter by items with trailers. |
| `HasTvdbId` | `query` | `bool?` | 否 | 否 | Optional filter by items that have a tvdb id or not. |
| `Ids` | `query` | `string` | 否 | 是 | Optional. If specific items are needed, specify a list of item id's to retrieve. This allows multiple, comma delimited. |
| `ImageTypeLimit` | `query` | `int?` | 否 | 否 | Optional, the max number of images to return, per image type |
| `ImageTypes` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on those containing image types. This allows multiple, comma delimited. |
| `IncludeItemTypes` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on item type. This allows multiple, comma delimeted. |
| `Is3D` | `query` | `bool?` | 否 | 否 | Optional filter by items that are 3D, or not. |
| `Is4K` | `query` | `bool?` | 否 | 否 | - |
| `IsAiring` | `query` | `bool?` | 否 | 否 | - |
| `IsDuplicate` | `query` | `bool?` | 否 | 否 | - |
| `IsFavorite` | `query` | `bool?` | 否 | 否 | Optional filter by items that are marked as favorite, or not. |
| `IsFolder` | `query` | `bool?` | 否 | 否 | Optional filter for folders. |
| `IsHD` | `query` | `bool?` | 否 | 否 | Optional filter by items that are HD or not. |
| `IsKids` | `query` | `bool?` | 否 | 否 | Optional filter for kids. |
| `IsLocked` | `query` | `bool?` | 否 | 否 | Optional filter by items that are locked. |
| `IsMovie` | `query` | `bool?` | 否 | 否 | Optional filter for movies. |
| `IsNew` | `query` | `bool?` | 否 | 否 | Optional filter for IsNew. |
| `IsNewOrPremiere` | `query` | `bool?` | 否 | 否 | Optional filter for IsNewOrPremiere. |
| `IsNews` | `query` | `bool?` | 否 | 否 | Optional filter for news. |
| `IsPlaceHolder` | `query` | `bool?` | 否 | 否 | Optional filter by items that are placeholders |
| `IsPlayed` | `query` | `bool?` | 否 | 否 | Optional filter by items that are played, or not. |
| `IsPremiere` | `query` | `bool?` | 否 | 否 | Optional filter for IsPremiere. |
| `IsRepeat` | `query` | `bool?` | 否 | 否 | Optional filter for IsRepeat. |
| `IsSeries` | `query` | `bool?` | 否 | 否 | Optional filter for series. |
| `IsSpecialEpisode` | `query` | `bool?` | 否 | 否 | - |
| `IsSpecialSeason` | `query` | `bool?` | 否 | 否 | Optional. Filter by special season. |
| `IsSports` | `query` | `bool?` | 否 | 否 | Optional filter for sports. |
| `IsStandaloneSpecial` | `query` | `bool?` | 否 | 否 | - |
| `IsUnaired` | `query` | `bool?` | 否 | 否 | Optional filter by items that are unaired episodes or not. |
| `ItemPersonTypes` | `query` | `PersonType[]` | 否 | 否 | - |
| `LibrarySeriesId` | `query` | `string` | 否 | 否 | - |
| `Limit` | `query` | `int?` | 否 | 否 | Optional. The maximum number of records to return |
| `ListItemIds` | `query` | `string` | 否 | 否 | - |
| `MatchAnyWord` | `query` | `bool` | 否 | 否 | - |
| `MaxDateCreated` | `query` | `DateTimeOffset?` | 否 | 否 | - |
| `MaxEndDate` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The maximum premiere date. Format = ISO |
| `MaxHeight` | `query` | `int?` | 否 | 否 | - |
| `MaxOfficialRating` | `query` | `string` | 否 | 否 | Optional filter by maximum official rating (PG, PG-13, TV-MA, etc). |
| `MaxPlayers` | `query` | `int?` | 否 | 否 | Optional filter by maximum number of game players. |
| `MaxPremiereDate` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The maximum premiere date. Format = ISO |
| `MaxStartDate` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The maximum premiere date. Format = ISO |
| `MaxWidth` | `query` | `int?` | 否 | 否 | - |
| `MediaTypes` | `query` | `string` | 否 | 是 | Optional filter by MediaType. Allows multiple, comma delimited. |
| `MinCommunityRating` | `query` | `double?` | 否 | 否 | Optional filter by minimum community rating. |
| `MinCriticRating` | `query` | `double?` | 否 | 否 | Optional filter by minimum critic rating. |
| `MinDateCreated` | `query` | `DateTimeOffset?` | 否 | 否 | - |
| `MinDateLastSaved` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The minimum premiere date. Format = ISO |
| `MinDateLastSavedForUser` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The minimum premiere date. Format = ISO |
| `MinEndDate` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The minimum premiere date. Format = ISO |
| `MinHeight` | `query` | `int?` | 否 | 否 | - |
| `MinIndexNumber` | `query` | `int?` | 否 | 否 | Optional filter by minimum index number. |
| `MinOfficialRating` | `query` | `string` | 否 | 否 | Optional filter by minimum official rating (PG, PG-13, TV-MA, etc). |
| `MinPlayers` | `query` | `int?` | 否 | 否 | Optional filter by minimum number of game players. |
| `MinPremiereDate` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The minimum premiere date. Format = ISO |
| `MinStartDate` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The minimum premiere date. Format = ISO |
| `MinWidth` | `query` | `int?` | 否 | 否 | - |
| `Name` | `query` | `string` | 否 | 否 | - |
| `NameLessThan` | `query` | `string` | 否 | 否 | Optional filter by items whose name is equally or lesser than a given input string. |
| `NameStartsWith` | `query` | `string` | 否 | 否 | Optional filter by items whose name is sorted equally than a given input string. |
| `NameStartsWithOrGreater` | `query` | `string` | 否 | 否 | Optional filter by items whose name is sorted equally or greater than a given input string. |
| `OfficialRatings` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on OfficialRating. This allows multiple, pipe delimeted. |
| `OuterIds` | `query` | `string` | 否 | 否 | - |
| `ParentId` | `query` | `string` | 否 | 否 | Specify this to localize the search to a specific item or folder. Omit to use the root |
| `ParentIndexNumber` | `query` | `int?` | 否 | 否 | Optional filter by parent index number. |
| `Path` | `query` | `string` | 否 | 是 | Optional filter by Path. |
| `Person` | `query` | `string` | 否 | 否 | Optional. If specified, results will be filtered to include only those containing the specified person. |
| `PersonIds` | `query` | `string` | 否 | 否 | Optional. If specified, results will be filtered to include only those containing the specified person. |
| `PersonTypes` | `query` | `string` | 否 | 否 | Optional. If specified, along with Person, results will be filtered to include only those containing the specified person and PersonType. Allows multiple, comma-delimited |
| `ProjectToMedia` | `query` | `bool` | 否 | 否 | ProjectToMedia |
| `RandomSeed` | `query` | `int` | 否 | 否 | - |
| `RecordingKeyword` | `query` | `string` | 否 | 否 | - |
| `RecordingKeywordType` | `query` | `KeywordType?` | 否 | 否 | - |
| `Recursive` | `query` | `bool` | 否 | 否 | When searching within folders, this determines whether or not the search will be recursive. true/false |
| `SearchTerm` | `query` | `string` | 否 | 否 | Enter a search term to perform a search request |
| `SeriesFromProgramId` | `query` | `string` | 否 | 否 | - |
| `SeriesStatus` | `query` | `string` | 否 | 是 | Optional filter by Series Status. Allows multiple, comma delimeted. |
| `SeriesTimerId` | `query` | `string` | 否 | 否 | - |
| `ShowingsFromProgramId` | `query` | `string` | 否 | 否 | - |
| `SortBy` | `query` | `string` | 否 | 是 | Optional. Specify one or more sort orders, comma delimeted. Options: Album, AlbumArtist, Artist, Budget, CommunityRating, CriticRating, DateCreated, DatePlayed, PlayCount, PremiereDate, ProductionYear, SortName, Random, Revenue, Runtime |
| `SortOrder` | `query` | `string` | 否 | 否 | Sort Order - Ascending,Descending |
| `StartIndex` | `query` | `int?` | 否 | 否 | Optional. The record index to start at. All items with a lower index will be dropped from the results. |
| `StartItemId` | `query` | `string` | 否 | 否 | Optional. Skip through the list until a given item is found. |
| `StudioIds` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on studio. This allows multiple, pipe delimeted. |
| `Studios` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on studio. This allows multiple, pipe delimeted. |
| `SubtitleCodecs` | `query` | `string` | 否 | 是 | Optional filter by SubtitleCodec. Allows multiple, comma delimeted. |
| `SubtitleLanguages` | `query` | `string` | 否 | 否 | - |
| `TagIds` | `query` | `string` | 否 | 否 | - |
| `Tags` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on tag. This allows multiple, pipe delimeted. |
| `UserId` | `query` | `Guid` | 否 | 否 | User Id |
| `VideoCodecs` | `query` | `string` | 否 | 是 | Optional filter by VideoCodec. Allows multiple, comma delimeted. |
| `VideoTypes` | `query` | `string` | 否 | 是 | Optional filter by VideoType (videofile, dvd, bluray, iso). Allows multiple, comma delimeted. |
| `WasSearched` | `query` | `bool?` | 否 | 否 | - |
| `Years` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on production year. This allows multiple, comma delimeted. |

#### `GET` `/LiveTv/Programs/Recommended`

- **摘要**：Gets available live tv epgs..
- **鉴权**：需要认证
- **返回类型**：`QueryResult<BaseItemDto>`
- **处理服务**：`Emby.LiveTV.Api.LiveTvService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.GetRecommendedPrograms` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `AdjacentTo` | `query` | `string` | 否 | 否 | Optional. Return items that are siblings of a supplied item. |
| `AirDays` | `query` | `DayOfWeek[]` | 否 | 否 | - |
| `AiredDuringSeason` | `query` | `int?` | 否 | 否 | Gets all episodes that aired during a season, including specials. |
| `AlbumArtistIds` | `query` | `string` | 否 | 否 | - |
| `AlbumArtistStartsWithOrGreater` | `query` | `string` | 否 | 否 | Optional filter by items whose name is sorted equally or greater than a given input string. |
| `AlbumIds` | `query` | `string` | 否 | 否 | - |
| `Albums` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on album. This allows multiple, pipe delimeted. |
| `AnyProviderIdEquals` | `query` | `string` | 否 | 是 | Optional. If specified, result will be filtered to contain only items which match at least one of the specified IDs. Each provider ID must be in the form 'prov.id', e.g. 'imdb.tt123456'. This allows multiple, comma delimeted value pairs. |
| `ArtistIds` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on artist. This allows multiple, pipe delimeted. |
| `Artists` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on artist. This allows multiple, pipe delimeted. |
| `ArtistStartsWithOrGreater` | `query` | `string` | 否 | 否 | Optional filter by items whose name is sorted equally or greater than a given input string. |
| `ArtistType` | `query` | `string` | 否 | 否 | Artist or AlbumArtist |
| `AudioCodecs` | `query` | `string` | 否 | 是 | Optional filter by AudioCodec. Allows multiple, comma delimeted. |
| `AudioLanguages` | `query` | `string` | 否 | 否 | - |
| `AudioLayouts` | `query` | `string` | 否 | 是 | Optional filter by AudioLayout. Allows multiple, comma delimeted. |
| `CanEditItems` | `query` | `bool?` | 否 | 否 | - |
| `CollectionIds` | `query` | `string` | 否 | 否 | - |
| `CollectionTypes` | `query` | `string` | 否 | 否 | - |
| `ComposerArtistIds` | `query` | `string` | 否 | 否 | - |
| `Containers` | `query` | `string` | 否 | 是 | Optional filter by Container. Allows multiple, comma delimeted. |
| `ContributingArtistIds` | `query` | `string` | 否 | 否 | - |
| `EnableImages` | `query` | `bool?` | 否 | 否 | Optional, include image information in output |
| `EnableImageTypes` | `query` | `string` | 否 | 否 | Optional. The image types to include in the output. |
| `EnableTotalRecordCount` | `query` | `bool` | 否 | 否 | - |
| `EnableUserData` | `query` | `bool?` | 否 | 否 | Optional, include user data |
| `ExcludeArtistIds` | `query` | `string` | 否 | 否 | - |
| `ExcludeItemIds` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered by exxcluding item ids. This allows multiple, comma delimeted. |
| `ExcludeItemTypes` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on item type. This allows multiple, comma delimeted. |
| `ExcludeSources` | `query` | `string[]` | 否 | 否 | - |
| `ExcludeTagIds` | `query` | `string` | 否 | 否 | - |
| `ExcludeTags` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on tag. This allows multiple, pipe delimeted. |
| `ExtendedVideoTypes` | `query` | `string` | 否 | 是 | Optional filter by ExtendedVideoType. Allows multiple, comma delimeted. |
| `Fields` | `query` | `string` | 否 | 是 | Optional. Specify additional fields of information to return in the output. This allows multiple, comma delimeted. Options: Budget, Chapters, DateCreated, Genres, HomePageUrl, IndexOptions, MediaStreams, Overview, ParentId, Path, People, ProviderIds, PrimaryImageAspectRatio, Revenue, SortName, Studios, Taglines |
| `Filters` | `query` | `string` | 否 | 是 | Optional. Specify additional filters to apply. This allows multiple, comma delimeted. Options: IsFolder, IsNotFolder, IsUnplayed, IsPlayed, IsFavorite, IsResumable, Likes, Dislikes |
| `GenreIds` | `query` | `string` | 否 | 否 | - |
| `Genres` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on genre. This allows multiple, pipe delimeted. |
| `GroupByPresentationUniqueKey` | `query` | `bool?` | 否 | 否 | - |
| `GroupItemsInto` | `query` | `ItemLinkType?` | 否 | 否 | - |
| `GroupItemsIntoCollections` | `query` | `bool` | 否 | 否 | Whether or not to hide items behind their boxsets. |
| `GroupProgramsBySeries` | `query` | `bool` | 否 | 否 | - |
| `HasAired` | `query` | `bool?` | 否 | 否 | - |
| `HasImdbId` | `query` | `bool?` | 否 | 否 | Optional filter by items that have an imdb id or not. |
| `HasOfficialRating` | `query` | `bool?` | 否 | 否 | Optional filter by items that have official ratings |
| `HasOverview` | `query` | `bool?` | 否 | 否 | Optional filter by items that have an overview or not. |
| `HasParentalRating` | `query` | `bool?` | 否 | 否 | Optional filter by items that have or do not have a parental rating |
| `HasSpecialFeature` | `query` | `bool?` | 否 | 否 | Optional filter by items with special features. |
| `HasSubtitles` | `query` | `bool?` | 否 | 否 | Optional filter by items with subtitles. |
| `HasThemeSong` | `query` | `bool?` | 否 | 否 | Optional filter by items with theme songs. |
| `HasThemeVideo` | `query` | `bool?` | 否 | 否 | Optional filter by items with theme videos. |
| `HasTmdbId` | `query` | `bool?` | 否 | 否 | Optional filter by items that have a tmdb id or not. |
| `HasTrailer` | `query` | `bool?` | 否 | 否 | Optional filter by items with trailers. |
| `HasTvdbId` | `query` | `bool?` | 否 | 否 | Optional filter by items that have a tvdb id or not. |
| `Ids` | `query` | `string` | 否 | 是 | Optional. If specific items are needed, specify a list of item id's to retrieve. This allows multiple, comma delimited. |
| `ImageTypeLimit` | `query` | `int?` | 否 | 否 | Optional, the max number of images to return, per image type |
| `ImageTypes` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on those containing image types. This allows multiple, comma delimited. |
| `IncludeItemTypes` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on item type. This allows multiple, comma delimeted. |
| `Is3D` | `query` | `bool?` | 否 | 否 | Optional filter by items that are 3D, or not. |
| `Is4K` | `query` | `bool?` | 否 | 否 | - |
| `IsAiring` | `query` | `bool?` | 否 | 否 | - |
| `IsDuplicate` | `query` | `bool?` | 否 | 否 | - |
| `IsFavorite` | `query` | `bool?` | 否 | 否 | Optional filter by items that are marked as favorite, or not. |
| `IsFolder` | `query` | `bool?` | 否 | 否 | Optional filter for folders. |
| `IsHD` | `query` | `bool?` | 否 | 否 | Optional filter by items that are HD or not. |
| `IsKids` | `query` | `bool?` | 否 | 否 | Optional filter for kids. |
| `IsLocked` | `query` | `bool?` | 否 | 否 | Optional filter by items that are locked. |
| `IsMovie` | `query` | `bool?` | 否 | 否 | Optional filter for movies. |
| `IsNew` | `query` | `bool?` | 否 | 否 | Optional filter for IsNew. |
| `IsNewOrPremiere` | `query` | `bool?` | 否 | 否 | Optional filter for IsNewOrPremiere. |
| `IsNews` | `query` | `bool?` | 否 | 否 | Optional filter for news. |
| `IsPlaceHolder` | `query` | `bool?` | 否 | 否 | Optional filter by items that are placeholders |
| `IsPlayed` | `query` | `bool?` | 否 | 否 | Optional filter by items that are played, or not. |
| `IsPremiere` | `query` | `bool?` | 否 | 否 | Optional filter for IsPremiere. |
| `IsRepeat` | `query` | `bool?` | 否 | 否 | Optional filter for IsRepeat. |
| `IsSeries` | `query` | `bool?` | 否 | 否 | Optional filter for series. |
| `IsSpecialEpisode` | `query` | `bool?` | 否 | 否 | - |
| `IsSpecialSeason` | `query` | `bool?` | 否 | 否 | Optional. Filter by special season. |
| `IsSports` | `query` | `bool?` | 否 | 否 | Optional filter for sports. |
| `IsStandaloneSpecial` | `query` | `bool?` | 否 | 否 | - |
| `IsUnaired` | `query` | `bool?` | 否 | 否 | Optional filter by items that are unaired episodes or not. |
| `ItemPersonTypes` | `query` | `PersonType[]` | 否 | 否 | - |
| `Limit` | `query` | `int?` | 否 | 否 | Optional. The maximum number of records to return |
| `ListItemIds` | `query` | `string` | 否 | 否 | - |
| `MatchAnyWord` | `query` | `bool` | 否 | 否 | - |
| `MaxDateCreated` | `query` | `DateTimeOffset?` | 否 | 否 | - |
| `MaxEndDate` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The maximum premiere date. Format = ISO |
| `MaxHeight` | `query` | `int?` | 否 | 否 | - |
| `MaxOfficialRating` | `query` | `string` | 否 | 否 | Optional filter by maximum official rating (PG, PG-13, TV-MA, etc). |
| `MaxPlayers` | `query` | `int?` | 否 | 否 | Optional filter by maximum number of game players. |
| `MaxPremiereDate` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The maximum premiere date. Format = ISO |
| `MaxStartDate` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The maximum premiere date. Format = ISO |
| `MaxWidth` | `query` | `int?` | 否 | 否 | - |
| `MediaTypes` | `query` | `string` | 否 | 是 | Optional filter by MediaType. Allows multiple, comma delimited. |
| `MinCommunityRating` | `query` | `double?` | 否 | 否 | Optional filter by minimum community rating. |
| `MinCriticRating` | `query` | `double?` | 否 | 否 | Optional filter by minimum critic rating. |
| `MinDateCreated` | `query` | `DateTimeOffset?` | 否 | 否 | - |
| `MinDateLastSaved` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The minimum premiere date. Format = ISO |
| `MinDateLastSavedForUser` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The minimum premiere date. Format = ISO |
| `MinEndDate` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The minimum premiere date. Format = ISO |
| `MinHeight` | `query` | `int?` | 否 | 否 | - |
| `MinIndexNumber` | `query` | `int?` | 否 | 否 | Optional filter by minimum index number. |
| `MinOfficialRating` | `query` | `string` | 否 | 否 | Optional filter by minimum official rating (PG, PG-13, TV-MA, etc). |
| `MinPlayers` | `query` | `int?` | 否 | 否 | Optional filter by minimum number of game players. |
| `MinPremiereDate` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The minimum premiere date. Format = ISO |
| `MinStartDate` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The minimum premiere date. Format = ISO |
| `MinWidth` | `query` | `int?` | 否 | 否 | - |
| `Name` | `query` | `string` | 否 | 否 | - |
| `NameLessThan` | `query` | `string` | 否 | 否 | Optional filter by items whose name is equally or lesser than a given input string. |
| `NameStartsWith` | `query` | `string` | 否 | 否 | Optional filter by items whose name is sorted equally than a given input string. |
| `NameStartsWithOrGreater` | `query` | `string` | 否 | 否 | Optional filter by items whose name is sorted equally or greater than a given input string. |
| `OfficialRatings` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on OfficialRating. This allows multiple, pipe delimeted. |
| `OuterIds` | `query` | `string` | 否 | 否 | - |
| `ParentId` | `query` | `string` | 否 | 否 | Specify this to localize the search to a specific item or folder. Omit to use the root |
| `ParentIndexNumber` | `query` | `int?` | 否 | 否 | Optional filter by parent index number. |
| `Path` | `query` | `string` | 否 | 是 | Optional filter by Path. |
| `Person` | `query` | `string` | 否 | 否 | Optional. If specified, results will be filtered to include only those containing the specified person. |
| `PersonIds` | `query` | `string` | 否 | 否 | Optional. If specified, results will be filtered to include only those containing the specified person. |
| `PersonTypes` | `query` | `string` | 否 | 否 | Optional. If specified, along with Person, results will be filtered to include only those containing the specified person and PersonType. Allows multiple, comma-delimited |
| `ProjectToMedia` | `query` | `bool` | 否 | 否 | ProjectToMedia |
| `RandomSeed` | `query` | `int` | 否 | 否 | - |
| `RecordingKeyword` | `query` | `string` | 否 | 否 | - |
| `RecordingKeywordType` | `query` | `KeywordType?` | 否 | 否 | - |
| `Recursive` | `query` | `bool` | 否 | 否 | When searching within folders, this determines whether or not the search will be recursive. true/false |
| `SearchTerm` | `query` | `string` | 否 | 否 | Enter a search term to perform a search request |
| `SeriesStatus` | `query` | `string` | 否 | 是 | Optional filter by Series Status. Allows multiple, comma delimeted. |
| `SortBy` | `query` | `string` | 否 | 是 | Optional. Specify one or more sort orders, comma delimeted. Options: Album, AlbumArtist, Artist, Budget, CommunityRating, CriticRating, DateCreated, DatePlayed, PlayCount, PremiereDate, ProductionYear, SortName, Random, Revenue, Runtime |
| `SortOrder` | `query` | `string` | 否 | 否 | Sort Order - Ascending,Descending |
| `StartIndex` | `query` | `int?` | 否 | 否 | Optional. The record index to start at. All items with a lower index will be dropped from the results. |
| `StartItemId` | `query` | `string` | 否 | 否 | Optional. Skip through the list until a given item is found. |
| `StudioIds` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on studio. This allows multiple, pipe delimeted. |
| `Studios` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on studio. This allows multiple, pipe delimeted. |
| `SubtitleCodecs` | `query` | `string` | 否 | 是 | Optional filter by SubtitleCodec. Allows multiple, comma delimeted. |
| `SubtitleLanguages` | `query` | `string` | 否 | 否 | - |
| `TagIds` | `query` | `string` | 否 | 否 | - |
| `Tags` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on tag. This allows multiple, pipe delimeted. |
| `UserId` | `query` | `Guid` | 否 | 否 | User Id |
| `VideoCodecs` | `query` | `string` | 否 | 是 | Optional filter by VideoCodec. Allows multiple, comma delimeted. |
| `VideoTypes` | `query` | `string` | 否 | 是 | Optional filter by VideoType (videofile, dvd, bluray, iso). Allows multiple, comma delimeted. |
| `WasSearched` | `query` | `bool?` | 否 | 否 | - |
| `Years` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on production year. This allows multiple, comma delimeted. |

#### `GET` `/LiveTv/Recordings`

- **摘要**：Gets live tv recordings
- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.LiveTV.Api.LiveTvService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.GetRecordings` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `AdjacentTo` | `query` | `string` | 否 | 否 | Optional. Return items that are siblings of a supplied item. |
| `AirDays` | `query` | `DayOfWeek[]` | 否 | 否 | - |
| `AiredDuringSeason` | `query` | `int?` | 否 | 否 | Gets all episodes that aired during a season, including specials. |
| `AlbumArtistIds` | `query` | `string` | 否 | 否 | - |
| `AlbumArtistStartsWithOrGreater` | `query` | `string` | 否 | 否 | Optional filter by items whose name is sorted equally or greater than a given input string. |
| `AlbumIds` | `query` | `string` | 否 | 否 | - |
| `Albums` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on album. This allows multiple, pipe delimeted. |
| `AnyProviderIdEquals` | `query` | `string` | 否 | 是 | Optional. If specified, result will be filtered to contain only items which match at least one of the specified IDs. Each provider ID must be in the form 'prov.id', e.g. 'imdb.tt123456'. This allows multiple, comma delimeted value pairs. |
| `ArtistIds` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on artist. This allows multiple, pipe delimeted. |
| `Artists` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on artist. This allows multiple, pipe delimeted. |
| `ArtistStartsWithOrGreater` | `query` | `string` | 否 | 否 | Optional filter by items whose name is sorted equally or greater than a given input string. |
| `ArtistType` | `query` | `string` | 否 | 否 | Artist or AlbumArtist |
| `AudioCodecs` | `query` | `string` | 否 | 是 | Optional filter by AudioCodec. Allows multiple, comma delimeted. |
| `AudioLanguages` | `query` | `string` | 否 | 否 | - |
| `AudioLayouts` | `query` | `string` | 否 | 是 | Optional filter by AudioLayout. Allows multiple, comma delimeted. |
| `CanEditItems` | `query` | `bool?` | 否 | 否 | - |
| `ChannelId` | `query` | `string` | 否 | 否 | Optional filter by channel id. |
| `CollectionIds` | `query` | `string` | 否 | 否 | - |
| `CollectionTypes` | `query` | `string` | 否 | 否 | - |
| `ComposerArtistIds` | `query` | `string` | 否 | 否 | - |
| `Containers` | `query` | `string` | 否 | 是 | Optional filter by Container. Allows multiple, comma delimeted. |
| `ContributingArtistIds` | `query` | `string` | 否 | 否 | - |
| `EnableImages` | `query` | `bool?` | 否 | 否 | Optional, include image information in output |
| `EnableImageTypes` | `query` | `string` | 否 | 否 | Optional. The image types to include in the output. |
| `EnableTotalRecordCount` | `query` | `bool` | 否 | 否 | - |
| `EnableUserData` | `query` | `bool?` | 否 | 否 | Optional, include user data |
| `ExcludeArtistIds` | `query` | `string` | 否 | 否 | - |
| `ExcludeItemIds` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered by exxcluding item ids. This allows multiple, comma delimeted. |
| `ExcludeItemTypes` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on item type. This allows multiple, comma delimeted. |
| `ExcludeSources` | `query` | `string[]` | 否 | 否 | - |
| `ExcludeTagIds` | `query` | `string` | 否 | 否 | - |
| `ExcludeTags` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on tag. This allows multiple, pipe delimeted. |
| `ExtendedVideoTypes` | `query` | `string` | 否 | 是 | Optional filter by ExtendedVideoType. Allows multiple, comma delimeted. |
| `Fields` | `query` | `string` | 否 | 是 | Optional. Specify additional fields of information to return in the output. This allows multiple, comma delimeted. Options: Budget, Chapters, DateCreated, Genres, HomePageUrl, IndexOptions, MediaStreams, Overview, ParentId, Path, People, ProviderIds, PrimaryImageAspectRatio, Revenue, SortName, Studios, Taglines |
| `Filters` | `query` | `string` | 否 | 是 | Optional. Specify additional filters to apply. This allows multiple, comma delimeted. Options: IsFolder, IsNotFolder, IsUnplayed, IsPlayed, IsFavorite, IsResumable, Likes, Dislikes |
| `GenreIds` | `query` | `string` | 否 | 否 | - |
| `Genres` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on genre. This allows multiple, pipe delimeted. |
| `GroupByPresentationUniqueKey` | `query` | `bool?` | 否 | 否 | - |
| `GroupItems` | `query` | `bool` | 否 | 否 | - |
| `GroupItemsInto` | `query` | `ItemLinkType?` | 否 | 否 | - |
| `GroupItemsIntoCollections` | `query` | `bool` | 否 | 否 | Whether or not to hide items behind their boxsets. |
| `GroupProgramsBySeries` | `query` | `bool` | 否 | 否 | - |
| `HasAired` | `query` | `bool?` | 否 | 否 | - |
| `HasImdbId` | `query` | `bool?` | 否 | 否 | Optional filter by items that have an imdb id or not. |
| `HasOfficialRating` | `query` | `bool?` | 否 | 否 | Optional filter by items that have official ratings |
| `HasOverview` | `query` | `bool?` | 否 | 否 | Optional filter by items that have an overview or not. |
| `HasParentalRating` | `query` | `bool?` | 否 | 否 | Optional filter by items that have or do not have a parental rating |
| `HasSpecialFeature` | `query` | `bool?` | 否 | 否 | Optional filter by items with special features. |
| `HasSubtitles` | `query` | `bool?` | 否 | 否 | Optional filter by items with subtitles. |
| `HasThemeSong` | `query` | `bool?` | 否 | 否 | Optional filter by items with theme songs. |
| `HasThemeVideo` | `query` | `bool?` | 否 | 否 | Optional filter by items with theme videos. |
| `HasTmdbId` | `query` | `bool?` | 否 | 否 | Optional filter by items that have a tmdb id or not. |
| `HasTrailer` | `query` | `bool?` | 否 | 否 | Optional filter by items with trailers. |
| `HasTvdbId` | `query` | `bool?` | 否 | 否 | Optional filter by items that have a tvdb id or not. |
| `Ids` | `query` | `string` | 否 | 是 | Optional. If specific items are needed, specify a list of item id's to retrieve. This allows multiple, comma delimited. |
| `ImageTypeLimit` | `query` | `int?` | 否 | 否 | Optional, the max number of images to return, per image type |
| `ImageTypes` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on those containing image types. This allows multiple, comma delimited. |
| `IncludeItemTypes` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on item type. This allows multiple, comma delimeted. |
| `Is3D` | `query` | `bool?` | 否 | 否 | Optional filter by items that are 3D, or not. |
| `Is4K` | `query` | `bool?` | 否 | 否 | - |
| `IsAiring` | `query` | `bool?` | 否 | 否 | - |
| `IsDuplicate` | `query` | `bool?` | 否 | 否 | - |
| `IsFavorite` | `query` | `bool?` | 否 | 否 | Optional filter by items that are marked as favorite, or not. |
| `IsFolder` | `query` | `bool?` | 否 | 否 | Optional filter for folders. |
| `IsHD` | `query` | `bool?` | 否 | 否 | Optional filter by items that are HD or not. |
| `IsKids` | `query` | `bool?` | 否 | 否 | Optional filter for kids. |
| `IsLocked` | `query` | `bool?` | 否 | 否 | Optional filter by items that are locked. |
| `IsMovie` | `query` | `bool?` | 否 | 否 | Optional filter for movies. |
| `IsNew` | `query` | `bool?` | 否 | 否 | Optional filter for IsNew. |
| `IsNewOrPremiere` | `query` | `bool?` | 否 | 否 | Optional filter for IsNewOrPremiere. |
| `IsNews` | `query` | `bool?` | 否 | 否 | Optional filter for news. |
| `IsPlaceHolder` | `query` | `bool?` | 否 | 否 | Optional filter by items that are placeholders |
| `IsPlayed` | `query` | `bool?` | 否 | 否 | Optional filter by items that are played, or not. |
| `IsPremiere` | `query` | `bool?` | 否 | 否 | Optional filter for IsPremiere. |
| `IsRepeat` | `query` | `bool?` | 否 | 否 | Optional filter for IsRepeat. |
| `IsSeries` | `query` | `bool?` | 否 | 否 | Optional filter for series. |
| `IsSpecialEpisode` | `query` | `bool?` | 否 | 否 | - |
| `IsSpecialSeason` | `query` | `bool?` | 否 | 否 | Optional. Filter by special season. |
| `IsSports` | `query` | `bool?` | 否 | 否 | Optional filter for sports. |
| `IsStandaloneSpecial` | `query` | `bool?` | 否 | 否 | - |
| `IsUnaired` | `query` | `bool?` | 否 | 否 | Optional filter by items that are unaired episodes or not. |
| `ItemPersonTypes` | `query` | `PersonType[]` | 否 | 否 | - |
| `Limit` | `query` | `int?` | 否 | 否 | Optional. The maximum number of records to return |
| `ListItemIds` | `query` | `string` | 否 | 否 | - |
| `MatchAnyWord` | `query` | `bool` | 否 | 否 | - |
| `MaxDateCreated` | `query` | `DateTimeOffset?` | 否 | 否 | - |
| `MaxEndDate` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The maximum premiere date. Format = ISO |
| `MaxHeight` | `query` | `int?` | 否 | 否 | - |
| `MaxOfficialRating` | `query` | `string` | 否 | 否 | Optional filter by maximum official rating (PG, PG-13, TV-MA, etc). |
| `MaxPlayers` | `query` | `int?` | 否 | 否 | Optional filter by maximum number of game players. |
| `MaxPremiereDate` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The maximum premiere date. Format = ISO |
| `MaxStartDate` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The maximum premiere date. Format = ISO |
| `MaxWidth` | `query` | `int?` | 否 | 否 | - |
| `MediaTypes` | `query` | `string` | 否 | 是 | Optional filter by MediaType. Allows multiple, comma delimited. |
| `MinCommunityRating` | `query` | `double?` | 否 | 否 | Optional filter by minimum community rating. |
| `MinCriticRating` | `query` | `double?` | 否 | 否 | Optional filter by minimum critic rating. |
| `MinDateCreated` | `query` | `DateTimeOffset?` | 否 | 否 | - |
| `MinDateLastSaved` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The minimum premiere date. Format = ISO |
| `MinDateLastSavedForUser` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The minimum premiere date. Format = ISO |
| `MinEndDate` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The minimum premiere date. Format = ISO |
| `MinHeight` | `query` | `int?` | 否 | 否 | - |
| `MinIndexNumber` | `query` | `int?` | 否 | 否 | Optional filter by minimum index number. |
| `MinOfficialRating` | `query` | `string` | 否 | 否 | Optional filter by minimum official rating (PG, PG-13, TV-MA, etc). |
| `MinPlayers` | `query` | `int?` | 否 | 否 | Optional filter by minimum number of game players. |
| `MinPremiereDate` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The minimum premiere date. Format = ISO |
| `MinStartDate` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The minimum premiere date. Format = ISO |
| `MinWidth` | `query` | `int?` | 否 | 否 | - |
| `Name` | `query` | `string` | 否 | 否 | - |
| `NameLessThan` | `query` | `string` | 否 | 否 | Optional filter by items whose name is equally or lesser than a given input string. |
| `NameStartsWith` | `query` | `string` | 否 | 否 | Optional filter by items whose name is sorted equally than a given input string. |
| `NameStartsWithOrGreater` | `query` | `string` | 否 | 否 | Optional filter by items whose name is sorted equally or greater than a given input string. |
| `OfficialRatings` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on OfficialRating. This allows multiple, pipe delimeted. |
| `OuterIds` | `query` | `string` | 否 | 否 | - |
| `ParentId` | `query` | `string` | 否 | 否 | Specify this to localize the search to a specific item or folder. Omit to use the root |
| `ParentIndexNumber` | `query` | `int?` | 否 | 否 | Optional filter by parent index number. |
| `Path` | `query` | `string` | 否 | 是 | Optional filter by Path. |
| `Person` | `query` | `string` | 否 | 否 | Optional. If specified, results will be filtered to include only those containing the specified person. |
| `PersonIds` | `query` | `string` | 否 | 否 | Optional. If specified, results will be filtered to include only those containing the specified person. |
| `PersonTypes` | `query` | `string` | 否 | 否 | Optional. If specified, along with Person, results will be filtered to include only those containing the specified person and PersonType. Allows multiple, comma-delimited |
| `ProjectToMedia` | `query` | `bool` | 否 | 否 | ProjectToMedia |
| `RandomSeed` | `query` | `int` | 否 | 否 | - |
| `RecordingKeyword` | `query` | `string` | 否 | 否 | - |
| `RecordingKeywordType` | `query` | `KeywordType?` | 否 | 否 | - |
| `Recursive` | `query` | `bool` | 否 | 否 | When searching within folders, this determines whether or not the search will be recursive. true/false |
| `SearchTerm` | `query` | `string` | 否 | 否 | Enter a search term to perform a search request |
| `SeriesStatus` | `query` | `string` | 否 | 是 | Optional filter by Series Status. Allows multiple, comma delimeted. |
| `SeriesTimerId` | `query` | `string` | 否 | 否 | Optional filter by recordings belonging to a series timer |
| `SortBy` | `query` | `string` | 否 | 是 | Optional. Specify one or more sort orders, comma delimeted. Options: Album, AlbumArtist, Artist, Budget, CommunityRating, CriticRating, DateCreated, DatePlayed, PlayCount, PremiereDate, ProductionYear, SortName, Random, Revenue, Runtime |
| `SortOrder` | `query` | `string` | 否 | 否 | Sort Order - Ascending,Descending |
| `StartIndex` | `query` | `int?` | 否 | 否 | Optional. The record index to start at. All items with a lower index will be dropped from the results. |
| `StartItemId` | `query` | `string` | 否 | 否 | Optional. Skip through the list until a given item is found. |
| `Status` | `query` | `RecordingStatus?` | 否 | 否 | Optional filter by recording status. |
| `Status` | `query` | `bool?` | 否 | 否 | Optional filter by recordings that are in progress, or not. |
| `StudioIds` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on studio. This allows multiple, pipe delimeted. |
| `Studios` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on studio. This allows multiple, pipe delimeted. |
| `SubtitleCodecs` | `query` | `string` | 否 | 是 | Optional filter by SubtitleCodec. Allows multiple, comma delimeted. |
| `SubtitleLanguages` | `query` | `string` | 否 | 否 | - |
| `TagIds` | `query` | `string` | 否 | 否 | - |
| `Tags` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on tag. This allows multiple, pipe delimeted. |
| `UserId` | `query` | `Guid` | 否 | 否 | User Id |
| `VideoCodecs` | `query` | `string` | 否 | 是 | Optional filter by VideoCodec. Allows multiple, comma delimeted. |
| `VideoTypes` | `query` | `string` | 否 | 是 | Optional filter by VideoType (videofile, dvd, bluray, iso). Allows multiple, comma delimeted. |
| `WasSearched` | `query` | `bool?` | 否 | 否 | - |
| `Years` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on production year. This allows multiple, comma delimeted. |

#### `GET` `/LiveTv/Recordings/Folders`

- **摘要**：Gets recording folders
- **鉴权**：需要认证
- **返回类型**：`BaseItemDto[]`
- **处理服务**：`Emby.LiveTV.Api.LiveTvService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.GetRecordingFolders` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `AdjacentTo` | `query` | `string` | 否 | 否 | Optional. Return items that are siblings of a supplied item. |
| `AirDays` | `query` | `DayOfWeek[]` | 否 | 否 | - |
| `AiredDuringSeason` | `query` | `int?` | 否 | 否 | Gets all episodes that aired during a season, including specials. |
| `AlbumArtistIds` | `query` | `string` | 否 | 否 | - |
| `AlbumArtistStartsWithOrGreater` | `query` | `string` | 否 | 否 | Optional filter by items whose name is sorted equally or greater than a given input string. |
| `AlbumIds` | `query` | `string` | 否 | 否 | - |
| `Albums` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on album. This allows multiple, pipe delimeted. |
| `AnyProviderIdEquals` | `query` | `string` | 否 | 是 | Optional. If specified, result will be filtered to contain only items which match at least one of the specified IDs. Each provider ID must be in the form 'prov.id', e.g. 'imdb.tt123456'. This allows multiple, comma delimeted value pairs. |
| `ArtistIds` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on artist. This allows multiple, pipe delimeted. |
| `Artists` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on artist. This allows multiple, pipe delimeted. |
| `ArtistStartsWithOrGreater` | `query` | `string` | 否 | 否 | Optional filter by items whose name is sorted equally or greater than a given input string. |
| `ArtistType` | `query` | `string` | 否 | 否 | Artist or AlbumArtist |
| `AudioCodecs` | `query` | `string` | 否 | 是 | Optional filter by AudioCodec. Allows multiple, comma delimeted. |
| `AudioLanguages` | `query` | `string` | 否 | 否 | - |
| `AudioLayouts` | `query` | `string` | 否 | 是 | Optional filter by AudioLayout. Allows multiple, comma delimeted. |
| `CanEditItems` | `query` | `bool?` | 否 | 否 | - |
| `CollectionIds` | `query` | `string` | 否 | 否 | - |
| `CollectionTypes` | `query` | `string` | 否 | 否 | - |
| `ComposerArtistIds` | `query` | `string` | 否 | 否 | - |
| `Containers` | `query` | `string` | 否 | 是 | Optional filter by Container. Allows multiple, comma delimeted. |
| `ContributingArtistIds` | `query` | `string` | 否 | 否 | - |
| `EnableImages` | `query` | `bool?` | 否 | 否 | Optional, include image information in output |
| `EnableImageTypes` | `query` | `string` | 否 | 否 | Optional. The image types to include in the output. |
| `EnableTotalRecordCount` | `query` | `bool` | 否 | 否 | - |
| `EnableUserData` | `query` | `bool?` | 否 | 否 | Optional, include user data |
| `ExcludeArtistIds` | `query` | `string` | 否 | 否 | - |
| `ExcludeItemIds` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered by exxcluding item ids. This allows multiple, comma delimeted. |
| `ExcludeItemTypes` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on item type. This allows multiple, comma delimeted. |
| `ExcludeSources` | `query` | `string[]` | 否 | 否 | - |
| `ExcludeTagIds` | `query` | `string` | 否 | 否 | - |
| `ExcludeTags` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on tag. This allows multiple, pipe delimeted. |
| `ExtendedVideoTypes` | `query` | `string` | 否 | 是 | Optional filter by ExtendedVideoType. Allows multiple, comma delimeted. |
| `Fields` | `query` | `string` | 否 | 是 | Optional. Specify additional fields of information to return in the output. This allows multiple, comma delimeted. Options: Budget, Chapters, DateCreated, Genres, HomePageUrl, IndexOptions, MediaStreams, Overview, ParentId, Path, People, ProviderIds, PrimaryImageAspectRatio, Revenue, SortName, Studios, Taglines |
| `Filters` | `query` | `string` | 否 | 是 | Optional. Specify additional filters to apply. This allows multiple, comma delimeted. Options: IsFolder, IsNotFolder, IsUnplayed, IsPlayed, IsFavorite, IsResumable, Likes, Dislikes |
| `GenreIds` | `query` | `string` | 否 | 否 | - |
| `Genres` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on genre. This allows multiple, pipe delimeted. |
| `GroupByPresentationUniqueKey` | `query` | `bool?` | 否 | 否 | - |
| `GroupItemsInto` | `query` | `ItemLinkType?` | 否 | 否 | - |
| `GroupItemsIntoCollections` | `query` | `bool` | 否 | 否 | Whether or not to hide items behind their boxsets. |
| `GroupProgramsBySeries` | `query` | `bool` | 否 | 否 | - |
| `HasAired` | `query` | `bool?` | 否 | 否 | - |
| `HasImdbId` | `query` | `bool?` | 否 | 否 | Optional filter by items that have an imdb id or not. |
| `HasOfficialRating` | `query` | `bool?` | 否 | 否 | Optional filter by items that have official ratings |
| `HasOverview` | `query` | `bool?` | 否 | 否 | Optional filter by items that have an overview or not. |
| `HasParentalRating` | `query` | `bool?` | 否 | 否 | Optional filter by items that have or do not have a parental rating |
| `HasSpecialFeature` | `query` | `bool?` | 否 | 否 | Optional filter by items with special features. |
| `HasSubtitles` | `query` | `bool?` | 否 | 否 | Optional filter by items with subtitles. |
| `HasThemeSong` | `query` | `bool?` | 否 | 否 | Optional filter by items with theme songs. |
| `HasThemeVideo` | `query` | `bool?` | 否 | 否 | Optional filter by items with theme videos. |
| `HasTmdbId` | `query` | `bool?` | 否 | 否 | Optional filter by items that have a tmdb id or not. |
| `HasTrailer` | `query` | `bool?` | 否 | 否 | Optional filter by items with trailers. |
| `HasTvdbId` | `query` | `bool?` | 否 | 否 | Optional filter by items that have a tvdb id or not. |
| `Ids` | `query` | `string` | 否 | 是 | Optional. If specific items are needed, specify a list of item id's to retrieve. This allows multiple, comma delimited. |
| `ImageTypeLimit` | `query` | `int?` | 否 | 否 | Optional, the max number of images to return, per image type |
| `ImageTypes` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on those containing image types. This allows multiple, comma delimited. |
| `IncludeItemTypes` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on item type. This allows multiple, comma delimeted. |
| `Is3D` | `query` | `bool?` | 否 | 否 | Optional filter by items that are 3D, or not. |
| `Is4K` | `query` | `bool?` | 否 | 否 | - |
| `IsAiring` | `query` | `bool?` | 否 | 否 | - |
| `IsDuplicate` | `query` | `bool?` | 否 | 否 | - |
| `IsFavorite` | `query` | `bool?` | 否 | 否 | Optional filter by items that are marked as favorite, or not. |
| `IsFolder` | `query` | `bool?` | 否 | 否 | Optional filter for folders. |
| `IsHD` | `query` | `bool?` | 否 | 否 | Optional filter by items that are HD or not. |
| `IsKids` | `query` | `bool?` | 否 | 否 | Optional filter for kids. |
| `IsLocked` | `query` | `bool?` | 否 | 否 | Optional filter by items that are locked. |
| `IsMovie` | `query` | `bool?` | 否 | 否 | Optional filter for movies. |
| `IsNew` | `query` | `bool?` | 否 | 否 | Optional filter for IsNew. |
| `IsNewOrPremiere` | `query` | `bool?` | 否 | 否 | Optional filter for IsNewOrPremiere. |
| `IsNews` | `query` | `bool?` | 否 | 否 | Optional filter for news. |
| `IsPlaceHolder` | `query` | `bool?` | 否 | 否 | Optional filter by items that are placeholders |
| `IsPlayed` | `query` | `bool?` | 否 | 否 | Optional filter by items that are played, or not. |
| `IsPremiere` | `query` | `bool?` | 否 | 否 | Optional filter for IsPremiere. |
| `IsRepeat` | `query` | `bool?` | 否 | 否 | Optional filter for IsRepeat. |
| `IsSeries` | `query` | `bool?` | 否 | 否 | Optional filter for series. |
| `IsSpecialEpisode` | `query` | `bool?` | 否 | 否 | - |
| `IsSpecialSeason` | `query` | `bool?` | 否 | 否 | Optional. Filter by special season. |
| `IsSports` | `query` | `bool?` | 否 | 否 | Optional filter for sports. |
| `IsStandaloneSpecial` | `query` | `bool?` | 否 | 否 | - |
| `IsUnaired` | `query` | `bool?` | 否 | 否 | Optional filter by items that are unaired episodes or not. |
| `ItemPersonTypes` | `query` | `PersonType[]` | 否 | 否 | - |
| `Limit` | `query` | `int?` | 否 | 否 | Optional. The maximum number of records to return |
| `ListItemIds` | `query` | `string` | 否 | 否 | - |
| `MatchAnyWord` | `query` | `bool` | 否 | 否 | - |
| `MaxDateCreated` | `query` | `DateTimeOffset?` | 否 | 否 | - |
| `MaxEndDate` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The maximum premiere date. Format = ISO |
| `MaxHeight` | `query` | `int?` | 否 | 否 | - |
| `MaxOfficialRating` | `query` | `string` | 否 | 否 | Optional filter by maximum official rating (PG, PG-13, TV-MA, etc). |
| `MaxPlayers` | `query` | `int?` | 否 | 否 | Optional filter by maximum number of game players. |
| `MaxPremiereDate` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The maximum premiere date. Format = ISO |
| `MaxStartDate` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The maximum premiere date. Format = ISO |
| `MaxWidth` | `query` | `int?` | 否 | 否 | - |
| `MediaTypes` | `query` | `string` | 否 | 是 | Optional filter by MediaType. Allows multiple, comma delimited. |
| `MinCommunityRating` | `query` | `double?` | 否 | 否 | Optional filter by minimum community rating. |
| `MinCriticRating` | `query` | `double?` | 否 | 否 | Optional filter by minimum critic rating. |
| `MinDateCreated` | `query` | `DateTimeOffset?` | 否 | 否 | - |
| `MinDateLastSaved` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The minimum premiere date. Format = ISO |
| `MinDateLastSavedForUser` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The minimum premiere date. Format = ISO |
| `MinEndDate` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The minimum premiere date. Format = ISO |
| `MinHeight` | `query` | `int?` | 否 | 否 | - |
| `MinIndexNumber` | `query` | `int?` | 否 | 否 | Optional filter by minimum index number. |
| `MinOfficialRating` | `query` | `string` | 否 | 否 | Optional filter by minimum official rating (PG, PG-13, TV-MA, etc). |
| `MinPlayers` | `query` | `int?` | 否 | 否 | Optional filter by minimum number of game players. |
| `MinPremiereDate` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The minimum premiere date. Format = ISO |
| `MinStartDate` | `query` | `DateTimeOffset?` | 否 | 否 | Optional. The minimum premiere date. Format = ISO |
| `MinWidth` | `query` | `int?` | 否 | 否 | - |
| `Name` | `query` | `string` | 否 | 否 | - |
| `NameLessThan` | `query` | `string` | 否 | 否 | Optional filter by items whose name is equally or lesser than a given input string. |
| `NameStartsWith` | `query` | `string` | 否 | 否 | Optional filter by items whose name is sorted equally than a given input string. |
| `NameStartsWithOrGreater` | `query` | `string` | 否 | 否 | Optional filter by items whose name is sorted equally or greater than a given input string. |
| `OfficialRatings` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on OfficialRating. This allows multiple, pipe delimeted. |
| `OuterIds` | `query` | `string` | 否 | 否 | - |
| `ParentId` | `query` | `string` | 否 | 否 | Specify this to localize the search to a specific item or folder. Omit to use the root |
| `ParentIndexNumber` | `query` | `int?` | 否 | 否 | Optional filter by parent index number. |
| `Path` | `query` | `string` | 否 | 是 | Optional filter by Path. |
| `Person` | `query` | `string` | 否 | 否 | Optional. If specified, results will be filtered to include only those containing the specified person. |
| `PersonIds` | `query` | `string` | 否 | 否 | Optional. If specified, results will be filtered to include only those containing the specified person. |
| `PersonTypes` | `query` | `string` | 否 | 否 | Optional. If specified, along with Person, results will be filtered to include only those containing the specified person and PersonType. Allows multiple, comma-delimited |
| `ProjectToMedia` | `query` | `bool` | 否 | 否 | ProjectToMedia |
| `RandomSeed` | `query` | `int` | 否 | 否 | - |
| `RecordingKeyword` | `query` | `string` | 否 | 否 | - |
| `RecordingKeywordType` | `query` | `KeywordType?` | 否 | 否 | - |
| `Recursive` | `query` | `bool` | 否 | 否 | When searching within folders, this determines whether or not the search will be recursive. true/false |
| `SearchTerm` | `query` | `string` | 否 | 否 | Enter a search term to perform a search request |
| `SeriesStatus` | `query` | `string` | 否 | 是 | Optional filter by Series Status. Allows multiple, comma delimeted. |
| `SortBy` | `query` | `string` | 否 | 是 | Optional. Specify one or more sort orders, comma delimeted. Options: Album, AlbumArtist, Artist, Budget, CommunityRating, CriticRating, DateCreated, DatePlayed, PlayCount, PremiereDate, ProductionYear, SortName, Random, Revenue, Runtime |
| `SortOrder` | `query` | `string` | 否 | 否 | Sort Order - Ascending,Descending |
| `StartIndex` | `query` | `int?` | 否 | 否 | Optional. The record index to start at. All items with a lower index will be dropped from the results. |
| `StartItemId` | `query` | `string` | 否 | 否 | Optional. Skip through the list until a given item is found. |
| `StudioIds` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on studio. This allows multiple, pipe delimeted. |
| `Studios` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on studio. This allows multiple, pipe delimeted. |
| `SubtitleCodecs` | `query` | `string` | 否 | 是 | Optional filter by SubtitleCodec. Allows multiple, comma delimeted. |
| `SubtitleLanguages` | `query` | `string` | 否 | 否 | - |
| `TagIds` | `query` | `string` | 否 | 否 | - |
| `Tags` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on tag. This allows multiple, pipe delimeted. |
| `UserId` | `query` | `Guid` | 否 | 否 | User Id |
| `VideoCodecs` | `query` | `string` | 否 | 是 | Optional filter by VideoCodec. Allows multiple, comma delimeted. |
| `VideoTypes` | `query` | `string` | 否 | 是 | Optional filter by VideoType (videofile, dvd, bluray, iso). Allows multiple, comma delimeted. |
| `WasSearched` | `query` | `bool?` | 否 | 否 | - |
| `Years` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on production year. This allows multiple, comma delimeted. |

#### `GET` `/LiveTv/Recordings/Groups`

- **摘要**：Gets live tv recording groups
- **鉴权**：需要认证
- **返回类型**：`QueryResult<BaseItemDto>`
- **处理服务**：`Emby.LiveTV.Api.LiveTvService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.GetRecordingGroups` (`Emby.LiveTV.dll`)

_无请求参数。_

#### `GET` `/LiveTv/Recordings/Series`

- **摘要**：Gets live tv recordings
- **鉴权**：需要认证
- **返回类型**：`QueryResult<BaseItemDto>`
- **处理服务**：`Emby.LiveTV.Api.LiveTvService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.GetRecordingSeries` (`Emby.LiveTV.dll`)

_无请求参数。_

#### `DELETE` `/LiveTv/Recordings/{Id}`

- **摘要**：Deletes a live tv recording
- **鉴权**：需要认证；角色要求: `managelivetv`
- **返回类型**：`void`
- **处理服务**：`Emby.LiveTV.Api.LiveTvService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.DeleteRecording` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Recording Id |

#### `GET` `/LiveTv/Recordings/{Id}`

- **摘要**：Gets a live tv recording
- **鉴权**：需要认证
- **返回类型**：`BaseItemDto`
- **处理服务**：`Emby.LiveTV.Api.LiveTvService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.GetRecording` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Recording Id |
| `UserId` | `query` | `Guid` | 否 | 否 | Optional attach user data. |

#### `POST` `/LiveTv/Recordings/{Id}/Delete`

- **摘要**：Deletes a live tv recording
- **鉴权**：需要认证；角色要求: `managelivetv`
- **返回类型**：`void`
- **处理服务**：`Emby.LiveTV.Api.LiveTvService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.DeleteRecording` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Recording Id |

#### `GET` `/LiveTv/SeriesTimers`

- **摘要**：Gets live tv series timers
- **鉴权**：需要认证
- **返回类型**：`QueryResult<SeriesTimerInfoDto>`
- **处理服务**：`Emby.LiveTV.Api.LiveTvService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.GetSeriesTimers` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Limit` | `query` | `int?` | 否 | 否 | Optional. The maximum number of records to return |
| `SortBy` | `query` | `string` | 否 | 否 | Optional. Sort by SortName or Priority |
| `SortOrder` | `query` | `SortOrder` | 否 | 否 | Optional. Sort in Ascending or Descending order |
| `StartIndex` | `query` | `int` | 否 | 否 | Optional. The record index to start at. All items with a lower index will be dropped from the results. |

#### `POST` `/LiveTv/SeriesTimers`

- **摘要**：Creates a live tv series timer
- **鉴权**：需要认证；角色要求: `managelivetv`
- **返回类型**：`SeriesTimerInfoDto`
- **处理服务**：`Emby.LiveTV.Api.LiveTvService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.CreateSeriesTimer` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `ChannelIds` | `query` | `string[]` | 否 | 否 | - |
| `Days` | `query` | `DayOfWeek[]` | 否 | 否 | - |
| `EndDate` | `query` | `DateTimeOffset` | 否 | 否 | - |
| `Id` | `query` | `string` | 否 | 否 | - |
| `IsPostPaddingRequired` | `query` | `bool` | 否 | 否 | - |
| `IsPrePaddingRequired` | `query` | `bool` | 否 | 否 | - |
| `KeepUntil` | `query` | `KeepUntil` | 否 | 否 | - |
| `KeepUpTo` | `query` | `int` | 否 | 否 | - |
| `Keywords` | `query` | `KeywordInfo[]` | 否 | 否 | - |
| `MatchExistingItemsWithAnyLibrary` | `query` | `bool` | 否 | 否 | - |
| `MaxRecordingSeconds` | `query` | `int` | 否 | 否 | - |
| `Name` | `query` | `string` | 否 | 否 | - |
| `Overview` | `query` | `string` | 否 | 否 | - |
| `ParentFolderId` | `query` | `long` | 否 | 否 | - |
| `PostPaddingSeconds` | `query` | `int` | 否 | 否 | - |
| `PrePaddingSeconds` | `query` | `int` | 否 | 否 | - |
| `Priority` | `query` | `int` | 否 | 否 | - |
| `ProgramId` | `query` | `string` | 否 | 否 | - |
| `ProviderIds` | `query` | `ProviderIdDictionary` | 否 | 否 | - |
| `RecordAnyTime` | `query` | `bool` | 否 | 否 | - |
| `RecordNewOnly` | `query` | `bool` | 否 | 否 | - |
| `Recurring` | `query` | `bool` | 否 | 否 | - |
| `SeriesId` | `query` | `string` | 否 | 否 | - |
| `ServiceName` | `query` | `string` | 否 | 否 | - |
| `SkipEpisodesInLibrary` | `query` | `bool` | 否 | 否 | - |
| `StartDate` | `query` | `DateTimeOffset` | 否 | 否 | - |
| `TimerType` | `query` | `TimerType` | 否 | 否 | - |

#### `DELETE` `/LiveTv/SeriesTimers/{Id}`

- **摘要**：Cancels a live tv series timer
- **鉴权**：需要认证；角色要求: `managelivetv`
- **返回类型**：`void`
- **处理服务**：`Emby.LiveTV.Api.LiveTvService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.CancelSeriesTimer` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Timer Id |

#### `GET` `/LiveTv/SeriesTimers/{Id}`

- **摘要**：Gets a live tv series timer
- **鉴权**：需要认证
- **返回类型**：`TimerInfoDto`
- **处理服务**：`Emby.LiveTV.Api.LiveTvService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.GetSeriesTimer` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Timer Id |

#### `POST` `/LiveTv/SeriesTimers/{Id}`

- **摘要**：Updates a live tv series timer
- **鉴权**：需要认证；角色要求: `managelivetv`
- **返回类型**：`void`
- **处理服务**：`Emby.LiveTV.Api.LiveTvService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.UpdateSeriesTimer` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 否 | 否 | - |
| `ChannelIds` | `query` | `string[]` | 否 | 否 | - |
| `Days` | `query` | `DayOfWeek[]` | 否 | 否 | - |
| `EndDate` | `query` | `DateTimeOffset` | 否 | 否 | - |
| `IsPostPaddingRequired` | `query` | `bool` | 否 | 否 | - |
| `IsPrePaddingRequired` | `query` | `bool` | 否 | 否 | - |
| `KeepUntil` | `query` | `KeepUntil` | 否 | 否 | - |
| `KeepUpTo` | `query` | `int` | 否 | 否 | - |
| `Keywords` | `query` | `KeywordInfo[]` | 否 | 否 | - |
| `MatchExistingItemsWithAnyLibrary` | `query` | `bool` | 否 | 否 | - |
| `MaxRecordingSeconds` | `query` | `int` | 否 | 否 | - |
| `Name` | `query` | `string` | 否 | 否 | - |
| `Overview` | `query` | `string` | 否 | 否 | - |
| `ParentFolderId` | `query` | `long` | 否 | 否 | - |
| `PostPaddingSeconds` | `query` | `int` | 否 | 否 | - |
| `PrePaddingSeconds` | `query` | `int` | 否 | 否 | - |
| `Priority` | `query` | `int` | 否 | 否 | - |
| `ProgramId` | `query` | `string` | 否 | 否 | - |
| `ProviderIds` | `query` | `ProviderIdDictionary` | 否 | 否 | - |
| `RecordAnyTime` | `query` | `bool` | 否 | 否 | - |
| `RecordNewOnly` | `query` | `bool` | 否 | 否 | - |
| `SeriesId` | `query` | `string` | 否 | 否 | - |
| `ServiceName` | `query` | `string` | 否 | 否 | - |
| `SkipEpisodesInLibrary` | `query` | `bool` | 否 | 否 | - |
| `StartDate` | `query` | `DateTimeOffset` | 否 | 否 | - |
| `TimerType` | `query` | `TimerType` | 否 | 否 | - |

#### `POST` `/LiveTv/SeriesTimers/{Id}/Delete`

- **摘要**：Cancels a live tv series timer
- **鉴权**：需要认证；角色要求: `managelivetv`
- **返回类型**：`void`
- **处理服务**：`Emby.LiveTV.Api.LiveTvService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.CancelSeriesTimer` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Timer Id |

#### `GET` `/LiveTv/Timers`

- **摘要**：Gets live tv timers
- **鉴权**：需要认证
- **返回类型**：`QueryResult<TimerInfoDto>`
- **处理服务**：`Emby.LiveTV.Api.LiveTvService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.GetTimers` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `ChannelId` | `query` | `string` | 否 | 否 | Optional filter by channel id. |
| `IsActive` | `query` | `bool?` | 否 | 否 | - |
| `IsScheduled` | `query` | `bool?` | 否 | 否 | - |
| `SeriesTimerId` | `query` | `string` | 否 | 否 | Optional filter by timers belonging to a series timer |

#### `POST` `/LiveTv/Timers`

- **摘要**：Creates a live tv timer
- **鉴权**：需要认证；角色要求: `managelivetv`
- **返回类型**：`void`
- **处理服务**：`Emby.LiveTV.Api.LiveTvService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.CreateTimer` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `ChannelId` | `query` | `string` | 否 | 否 | - |
| `ChannelName` | `query` | `string` | 否 | 否 | - |
| `ChannelNumber` | `query` | `string` | 否 | 否 | - |
| `ChannelPrimaryImageTag` | `query` | `string` | 否 | 否 | - |
| `EndDate` | `query` | `DateTimeOffset` | 否 | 否 | - |
| `Id` | `query` | `string` | 否 | 否 | - |
| `IsPostPaddingRequired` | `query` | `bool` | 否 | 否 | - |
| `IsPrePaddingRequired` | `query` | `bool` | 否 | 否 | - |
| `KeepUntil` | `query` | `KeepUntil` | 否 | 否 | - |
| `Name` | `query` | `string` | 否 | 否 | - |
| `Overview` | `query` | `string` | 否 | 否 | - |
| `ParentBackdropImageTags` | `query` | `string[]` | 否 | 否 | - |
| `ParentBackdropItemId` | `query` | `string` | 否 | 否 | - |
| `ParentFolderId` | `query` | `string` | 否 | 否 | - |
| `PostPaddingSeconds` | `query` | `int` | 否 | 否 | - |
| `PrePaddingSeconds` | `query` | `int` | 否 | 否 | - |
| `Priority` | `query` | `int` | 否 | 否 | - |
| `ProgramId` | `query` | `string` | 否 | 否 | - |
| `ProgramInfo` | `query` | `BaseItemDto` | 否 | 否 | - |
| `RunTimeTicks` | `query` | `long?` | 否 | 否 | - |
| `SeriesTimerId` | `query` | `string` | 否 | 否 | - |
| `ServerId` | `query` | `string` | 否 | 否 | - |
| `StartDate` | `query` | `DateTimeOffset` | 否 | 否 | - |
| `Status` | `query` | `RecordingStatus` | 否 | 否 | - |
| `TimerType` | `query` | `TimerType` | 否 | 否 | - |
| `Type` | `query` | `string` | 否 | 否 | - |

#### `GET` `/LiveTv/Timers/Defaults`

- **摘要**：Gets default values for a new timer
- **鉴权**：需要认证
- **返回类型**：`SeriesTimerInfoDto`
- **处理服务**：`Emby.LiveTV.Api.LiveTvService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.GetDefaultTimer` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `ProgramId` | `query` | `string` | 否 | 否 | Optional, to attach default values based on a program. |

#### `DELETE` `/LiveTv/Timers/{Id}`

- **摘要**：Cancels a live tv timer
- **鉴权**：需要认证；角色要求: `managelivetv`
- **返回类型**：`void`
- **处理服务**：`Emby.LiveTV.Api.LiveTvService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.CancelTimer` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Timer Id |

#### `GET` `/LiveTv/Timers/{Id}`

- **摘要**：Gets a live tv timer
- **鉴权**：需要认证
- **返回类型**：`TimerInfoDto`
- **处理服务**：`Emby.LiveTV.Api.LiveTvService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.GetTimer` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Timer Id |

#### `POST` `/LiveTv/Timers/{Id}`

- **摘要**：Updates a live tv timer
- **鉴权**：需要认证；角色要求: `managelivetv`
- **返回类型**：`void`
- **处理服务**：`Emby.LiveTV.Api.LiveTvService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.UpdateTimer` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 否 | 否 | - |
| `ChannelId` | `query` | `string` | 否 | 否 | - |
| `ChannelName` | `query` | `string` | 否 | 否 | - |
| `ChannelNumber` | `query` | `string` | 否 | 否 | - |
| `ChannelPrimaryImageTag` | `query` | `string` | 否 | 否 | - |
| `EndDate` | `query` | `DateTimeOffset` | 否 | 否 | - |
| `IsPostPaddingRequired` | `query` | `bool` | 否 | 否 | - |
| `IsPrePaddingRequired` | `query` | `bool` | 否 | 否 | - |
| `KeepUntil` | `query` | `KeepUntil` | 否 | 否 | - |
| `Name` | `query` | `string` | 否 | 否 | - |
| `Overview` | `query` | `string` | 否 | 否 | - |
| `ParentBackdropImageTags` | `query` | `string[]` | 否 | 否 | - |
| `ParentBackdropItemId` | `query` | `string` | 否 | 否 | - |
| `ParentFolderId` | `query` | `string` | 否 | 否 | - |
| `PostPaddingSeconds` | `query` | `int` | 否 | 否 | - |
| `PrePaddingSeconds` | `query` | `int` | 否 | 否 | - |
| `Priority` | `query` | `int` | 否 | 否 | - |
| `ProgramId` | `query` | `string` | 否 | 否 | - |
| `ProgramInfo` | `query` | `BaseItemDto` | 否 | 否 | - |
| `RunTimeTicks` | `query` | `long?` | 否 | 否 | - |
| `SeriesTimerId` | `query` | `string` | 否 | 否 | - |
| `ServerId` | `query` | `string` | 否 | 否 | - |
| `StartDate` | `query` | `DateTimeOffset` | 否 | 否 | - |
| `Status` | `query` | `RecordingStatus` | 否 | 否 | - |
| `TimerType` | `query` | `TimerType` | 否 | 否 | - |
| `Type` | `query` | `string` | 否 | 否 | - |

#### `POST` `/LiveTv/Timers/{Id}/Delete`

- **摘要**：Cancels a live tv timer
- **鉴权**：需要认证；角色要求: `managelivetv`
- **返回类型**：`void`
- **处理服务**：`Emby.LiveTV.Api.LiveTvService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.CancelTimer` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Timer Id |

#### `DELETE` `/LiveTv/TunerHosts`

- **摘要**：Deletes a tuner host
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`void`
- **处理服务**：`Emby.LiveTV.Api.LiveTvService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.DeleteTunerHost` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `query` | `string` | 否 | 否 | Tuner host id |

#### `GET` `/LiveTv/TunerHosts`

- **摘要**：Gets tuner hosts
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`TunerHostInfo[]`
- **处理服务**：`Emby.LiveTV.Api.LiveTvService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.GetTunerHosts` (`Emby.LiveTV.dll`)

_无请求参数。_

#### `POST` `/LiveTv/TunerHosts`

- **摘要**：Adds a tuner host
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`TunerHostInfo`
- **处理服务**：`Emby.LiveTV.Api.LiveTvService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.AddTunerHost` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `AllowHWTranscoding` | `query` | `bool` | 否 | 否 | - |
| `AllowMappingByNumber` | `query` | `bool` | 否 | 否 | - |
| `DataVersion` | `query` | `int` | 否 | 否 | - |
| `DeviceId` | `query` | `string` | 否 | 否 | - |
| `FriendlyName` | `query` | `string` | 否 | 否 | - |
| `Id` | `query` | `string` | 否 | 否 | - |
| `ImportFavoritesOnly` | `query` | `bool` | 否 | 否 | - |
| `ImportGuideData` | `query` | `bool` | 否 | 否 | - |
| `PreferEpgChannelImages` | `query` | `bool` | 否 | 否 | - |
| `PreferEpgChannelNumbers` | `query` | `bool` | 否 | 否 | - |
| `ProviderOptions` | `query` | `string` | 否 | 否 | - |
| `Referrer` | `query` | `string` | 否 | 否 | - |
| `SetupUrl` | `query` | `string` | 否 | 否 | - |
| `Source` | `query` | `string` | 否 | 否 | - |
| `TunerCount` | `query` | `int` | 否 | 否 | - |
| `Type` | `query` | `string` | 否 | 否 | - |
| `Url` | `query` | `string` | 否 | 否 | - |
| `UserAgent` | `query` | `string` | 否 | 否 | - |

#### `GET` `/LiveTv/TunerHosts/Default/{Type}`

- **摘要**：Gets tuner hosts
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`TunerHostInfo`
- **处理服务**：`Emby.LiveTV.Api.LiveTvService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.GetTunerHostDefaultConfiguration` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Type` | `path` | `string` | 是 | 否 | Type |

#### `POST` `/LiveTv/TunerHosts/Delete`

- **摘要**：Deletes a tuner host
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`void`
- **处理服务**：`Emby.LiveTV.Api.LiveTvService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.DeleteTunerHost` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `query` | `string` | 否 | 否 | Tuner host id |

#### `GET` `/LiveTv/TunerHosts/Types`

- **鉴权**：需要认证
- **返回类型**：`List<NameIdPair>`
- **处理服务**：`Emby.LiveTV.Api.LiveTvService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.GetTunerHostTypes` (`Emby.LiveTV.dll`)

_无请求参数。_

#### `GET` `/LiveTv/Tuners/Discover`

- **鉴权**：需要认证
- **返回类型**：`List<TunerHostInfo>`
- **处理服务**：`Emby.LiveTV.Api.LiveTvService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.DiscoverTuners` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `NewDevicesOnly` | `query` | `bool` | 否 | 否 | - |

#### `GET` `/LiveTv/Tuners/Discvover`

- **鉴权**：需要认证
- **返回类型**：`List<TunerHostInfo>`
- **处理服务**：`Emby.LiveTV.Api.LiveTvService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.DiscoverTuners` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `NewDevicesOnly` | `query` | `bool` | 否 | 否 | - |

#### `POST` `/LiveTv/Tuners/{Id}/Reset`

- **摘要**：Resets a tv tuner
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`void`
- **处理服务**：`Emby.LiveTV.Api.LiveTvService` (Emby.LiveTV.dll)
- **Request DTO**：`Emby.LiveTV.Api.ResetTuner` (`Emby.LiveTV.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Tuner Id |

### UserLibraryService（1 条）

#### `GET` `/LiveTv/Programs/{Id}`

- **摘要**：Gets a live tv program
- **鉴权**：需要认证
- **返回类型**：`BaseItemDto`
- **处理服务**：`Emby.Api.UserLibrary.UserLibraryService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.GetItem` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `UserId` | `path` | `Guid` | 是 | 否 | User Id |
| `AsSeries` | `query` | `bool` | 否 | 否 | - |
| `ExcludeFields` | `query` | `string` | 否 | 否 | - |
| `Fields` | `query` | `string` | 否 | 否 | - |
