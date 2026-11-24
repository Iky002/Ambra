# 离线同步与下载（Sync）

> 所属模块：**06. 同步、通知与插件管理**  
> 路由前缀：`/Sync`  
> 接口总数：**25** 条

涵盖同步任务创建、同步目标、已就绪数据拉取与同步状态上报。

[← 返回总目录](../README.md)

## 接口总览

| # | 方法 | 路径 | 鉴权 | 返回类型 | 摘要 |
| :---: | :--- | :--- | :--- | :--- | :--- |
| 1 | `POST` | `/Sync/Data` | 需认证 | `SyncDataResponse` | Syncs data between device and server |
| 2 | `POST` | `/Sync/Items/Cancel` | 需认证 | `void` | Cancels items from a sync target |
| 3 | `GET` | `/Sync/Items/Ready` | 需认证 | `List<SyncedItem>` | Gets ready to download sync items. |
| 4 | `GET` | `/Sync/JobItems` | 需认证 | `QueryResult<SyncJobItem>` | Gets sync job items. |
| 5 | `DELETE` | `/Sync/JobItems/{Id}` | 需认证 | `void` | Cancels a sync job item |
| 6 | `GET` | `/Sync/JobItems/{Id}/AdditionalFiles` | 需认证 | `object` | Gets a sync job item file |
| 7 | `POST` | `/Sync/JobItems/{Id}/Delete` | 需认证 | `void` | Cancels a sync job item |
| 8 | `POST` | `/Sync/JobItems/{Id}/Enable` | 需认证 | `void` | Enables a cancelled or queued sync job item |
| 9 | `GET` | `/Sync/JobItems/{Id}/File` | 需认证 | `object` | Gets a sync job item file |
| 10 | `HEAD` | `/Sync/JobItems/{Id}/File` | 需认证 | `object` | Gets a sync job item file |
| 11 | `POST` | `/Sync/JobItems/{Id}/MarkForRemoval` | 需认证 | `void` | Marks a job item for removal |
| 12 | `POST` | `/Sync/JobItems/{Id}/Transferred` | 需认证 | `void` | Reports that a sync job item has successfully been transferred. |
| 13 | `POST` | `/Sync/JobItems/{Id}/UnmarkForRemoval` | 需认证 | `void` | Unmarks a job item for removal |
| 14 | `GET` | `/Sync/Jobs` | 需认证 | `QueryResult<SyncJob>` | Gets sync jobs. |
| 15 | `POST` | `/Sync/Jobs` | 需认证 | `SyncJobCreationResult` | Gets sync jobs. |
| 16 | `DELETE` | `/Sync/Jobs/{Id}` | 需认证 | `void` | Cancels a sync job. |
| 17 | `GET` | `/Sync/Jobs/{Id}` | 需认证 | `SyncJob` | Gets a sync job. |
| 18 | `POST` | `/Sync/Jobs/{Id}` | 需认证 | `void` | Updates a sync job. |
| 19 | `POST` | `/Sync/Jobs/{Id}/Delete` | 需认证 | `void` | Cancels a sync job. |
| 20 | `POST` | `/Sync/OfflineActions` | 需认证 | `void` | Reports an action that occurred while offline. |
| 21 | `GET` | `/Sync/Options` | 需认证 | `SyncDialogOptions` | Gets a list of available sync targets. |
| 22 | `GET` | `/Sync/Targets` | 需认证 | `List<SyncTarget>` | Gets a list of available sync targets. |
| 23 | `POST` | `/Sync/{ItemId}/Status` | 需认证 | `void` | Gets sync status for an item. |
| 24 | `DELETE` | `/Sync/{TargetId}/Items` | 需认证 | `void` | Cancels items from a sync target |
| 25 | `POST` | `/Sync/{TargetId}/Items/Delete` | 需认证 | `void` | Cancels items from a sync target |

## 接口详情

### SyncService（25 条）

#### `POST` `/Sync/Data`

- **摘要**：Syncs data between device and server
- **鉴权**：需要认证
- **返回类型**：`SyncDataResponse`
- **处理服务**：`Emby.Server.Sync.Api.SyncService` (Emby.Server.Sync.dll)
- **Request DTO**：`Emby.Server.Sync.Api.SyncData` (`Emby.Server.Sync.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `TargetId` | `query` | `string` | 是 | 否 | TargetId |
| `InternalTargetIds` | `query` | `long[]` | 否 | 否 | - |
| `LocalItemIds` | `query` | `string[]` | 否 | 否 | - |

#### `POST` `/Sync/Items/Cancel`

- **摘要**：Cancels items from a sync target
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Server.Sync.Api.SyncService` (Emby.Server.Sync.dll)
- **Request DTO**：`Emby.Server.Sync.Api.CancelItems` (`Emby.Server.Sync.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `TargetId` | `path` | `string` | 否 | 否 | TargetId |
| `ItemIds` | `query` | `string` | 否 | 否 | ItemIds |

#### `GET` `/Sync/Items/Ready`

- **摘要**：Gets ready to download sync items.
- **鉴权**：需要认证
- **返回类型**：`List<SyncedItem>`
- **处理服务**：`Emby.Server.Sync.Api.SyncService` (Emby.Server.Sync.dll)
- **Request DTO**：`Emby.Server.Sync.Api.GetReadySyncItems` (`Emby.Server.Sync.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `TargetId` | `query` | `string` | 是 | 否 | TargetId |

#### `GET` `/Sync/JobItems`

- **摘要**：Gets sync job items.
- **鉴权**：需要认证
- **返回类型**：`QueryResult<SyncJobItem>`
- **处理服务**：`Emby.Server.Sync.Api.SyncService` (Emby.Server.Sync.dll)
- **Request DTO**：`Emby.Server.Sync.Api.GetSyncJobItems` (`Emby.Server.Sync.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `TargetId` | `query` | `string` | 是 | 否 | TargetId |
| `AddMetadata` | `query` | `bool` | 否 | 否 | - |
| `EnableTotalRecordCount` | `query` | `bool` | 否 | 否 | - |
| `InternalTargetIds` | `query` | `long[]` | 否 | 否 | - |
| `ItemId` | `query` | `long` | 否 | 否 | - |
| `JobId` | `query` | `long` | 否 | 否 | - |
| `Limit` | `query` | `int?` | 否 | 否 | - |
| `StartIndex` | `query` | `int?` | 否 | 否 | - |
| `Statuses` | `query` | `SyncJobItemStatus[]` | 否 | 否 | - |

#### `DELETE` `/Sync/JobItems/{Id}`

- **摘要**：Cancels a sync job item
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Server.Sync.Api.SyncService` (Emby.Server.Sync.dll)
- **Request DTO**：`Emby.Server.Sync.Api.CancelSyncJobItem` (`Emby.Server.Sync.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Id |

#### `GET` `/Sync/JobItems/{Id}/AdditionalFiles`

- **摘要**：Gets a sync job item file
- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Server.Sync.Api.SyncService` (Emby.Server.Sync.dll)
- **Request DTO**：`Emby.Server.Sync.Api.GetSyncJobItemAdditionalFile` (`Emby.Server.Sync.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Id |
| `Name` | `query` | `string` | 是 | 否 | Name |

#### `POST` `/Sync/JobItems/{Id}/Delete`

- **摘要**：Cancels a sync job item
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Server.Sync.Api.SyncService` (Emby.Server.Sync.dll)
- **Request DTO**：`Emby.Server.Sync.Api.CancelSyncJobItem` (`Emby.Server.Sync.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Id |

#### `POST` `/Sync/JobItems/{Id}/Enable`

- **摘要**：Enables a cancelled or queued sync job item
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Server.Sync.Api.SyncService` (Emby.Server.Sync.dll)
- **Request DTO**：`Emby.Server.Sync.Api.EnableSyncJobItem` (`Emby.Server.Sync.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Id |

#### `GET` `/Sync/JobItems/{Id}/File`

- **摘要**：Gets a sync job item file
- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Server.Sync.Api.SyncService` (Emby.Server.Sync.dll)
- **Request DTO**：`Emby.Server.Sync.Api.GetSyncJobItemFile` (`Emby.Server.Sync.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Id |

#### `HEAD` `/Sync/JobItems/{Id}/File`

- **摘要**：Gets a sync job item file
- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Server.Sync.Api.SyncService` (Emby.Server.Sync.dll)
- **Request DTO**：`Emby.Server.Sync.Api.GetSyncJobItemFile` (`Emby.Server.Sync.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Id |

#### `POST` `/Sync/JobItems/{Id}/MarkForRemoval`

- **摘要**：Marks a job item for removal
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Server.Sync.Api.SyncService` (Emby.Server.Sync.dll)
- **Request DTO**：`Emby.Server.Sync.Api.MarkJobItemForRemoval` (`Emby.Server.Sync.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Id |

#### `POST` `/Sync/JobItems/{Id}/Transferred`

- **摘要**：Reports that a sync job item has successfully been transferred.
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Server.Sync.Api.SyncService` (Emby.Server.Sync.dll)
- **Request DTO**：`Emby.Server.Sync.Api.ReportSyncJobItemTransferred` (`Emby.Server.Sync.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Id |

#### `POST` `/Sync/JobItems/{Id}/UnmarkForRemoval`

- **摘要**：Unmarks a job item for removal
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Server.Sync.Api.SyncService` (Emby.Server.Sync.dll)
- **Request DTO**：`Emby.Server.Sync.Api.UnmarkJobItemForRemoval` (`Emby.Server.Sync.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Id |

#### `GET` `/Sync/Jobs`

- **摘要**：Gets sync jobs.
- **鉴权**：需要认证
- **返回类型**：`QueryResult<SyncJob>`
- **处理服务**：`Emby.Server.Sync.Api.SyncService` (Emby.Server.Sync.dll)
- **Request DTO**：`Emby.Server.Sync.Api.GetSyncJobs` (`Emby.Server.Sync.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `EnableTotalRecordCount` | `query` | `bool` | 否 | 否 | - |
| `ExcludeInternalTargetIds` | `query` | `long[]` | 否 | 否 | - |
| `ExcludeProviders` | `query` | `string` | 否 | 否 | - |
| `ExcludeTargetIds` | `query` | `string` | 否 | 否 | - |
| `IncludeProviders` | `query` | `string` | 否 | 否 | - |
| `InternalTargetIds` | `query` | `long[]` | 否 | 否 | - |
| `ItemId` | `query` | `long` | 否 | 否 | - |
| `Limit` | `query` | `int?` | 否 | 否 | - |
| `StartIndex` | `query` | `int?` | 否 | 否 | - |
| `Statuses` | `query` | `SyncJobStatus[]` | 否 | 否 | - |
| `SyncNewContent` | `query` | `bool?` | 否 | 否 | - |
| `TargetId` | `query` | `string` | 否 | 否 | - |
| `TargetIds` | `query` | `string` | 否 | 否 | - |
| `UserId` | `query` | `string` | 否 | 否 | - |

#### `POST` `/Sync/Jobs`

- **摘要**：Gets sync jobs.
- **鉴权**：需要认证
- **返回类型**：`SyncJobCreationResult`
- **处理服务**：`Emby.Server.Sync.Api.SyncService` (Emby.Server.Sync.dll)
- **Request DTO**：`Emby.Server.Sync.Api.CreateSyncJob` (`Emby.Server.Sync.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `AudioCodec` | `query` | `string` | 否 | 否 | - |
| `Bitrate` | `query` | `int?` | 否 | 否 | - |
| `Category` | `query` | `SyncCategory?` | 否 | 否 | - |
| `Container` | `query` | `string` | 否 | 否 | - |
| `Downloaded` | `query` | `bool` | 否 | 否 | - |
| `ItemIds` | `query` | `string[]` | 否 | 否 | - |
| `ItemLimit` | `query` | `int?` | 否 | 否 | - |
| `Name` | `query` | `string` | 否 | 否 | - |
| `ParentId` | `query` | `string` | 否 | 否 | - |
| `Profile` | `query` | `string` | 否 | 否 | - |
| `Quality` | `query` | `string` | 否 | 否 | - |
| `SyncNewContent` | `query` | `bool` | 否 | 否 | - |
| `TargetId` | `query` | `string` | 否 | 否 | - |
| `UnwatchedOnly` | `query` | `bool` | 否 | 否 | - |
| `UserId` | `query` | `string` | 否 | 否 | - |
| `VideoCodec` | `query` | `string` | 否 | 否 | - |

#### `DELETE` `/Sync/Jobs/{Id}`

- **摘要**：Cancels a sync job.
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Server.Sync.Api.SyncService` (Emby.Server.Sync.dll)
- **Request DTO**：`Emby.Server.Sync.Api.CancelSyncJob` (`Emby.Server.Sync.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Id |

#### `GET` `/Sync/Jobs/{Id}`

- **摘要**：Gets a sync job.
- **鉴权**：需要认证
- **返回类型**：`SyncJob`
- **处理服务**：`Emby.Server.Sync.Api.SyncService` (Emby.Server.Sync.dll)
- **Request DTO**：`Emby.Server.Sync.Api.GetSyncJob` (`Emby.Server.Sync.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Id |

#### `POST` `/Sync/Jobs/{Id}`

- **摘要**：Updates a sync job.
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Server.Sync.Api.SyncService` (Emby.Server.Sync.dll)
- **Request DTO**：`Emby.Server.Sync.Api.UpdateSyncJob` (`Emby.Server.Sync.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `long` | 否 | 否 | - |
| `AudioCodec` | `query` | `string` | 否 | 否 | - |
| `Bitrate` | `query` | `int?` | 否 | 否 | - |
| `Category` | `query` | `SyncCategory?` | 否 | 否 | - |
| `Container` | `query` | `string` | 否 | 否 | - |
| `DateCreated` | `query` | `DateTimeOffset` | 否 | 否 | - |
| `DateLastModified` | `query` | `DateTimeOffset` | 否 | 否 | - |
| `InternalTargetId` | `query` | `long` | 否 | 否 | - |
| `ItemCount` | `query` | `int` | 否 | 否 | - |
| `ItemId` | `query` | `long` | 否 | 否 | - |
| `ItemLimit` | `query` | `int?` | 否 | 否 | - |
| `Name` | `query` | `string` | 否 | 否 | - |
| `ParentId` | `query` | `long` | 否 | 否 | - |
| `ParentName` | `query` | `string` | 否 | 否 | - |
| `PrimaryImageItemId` | `query` | `string` | 否 | 否 | - |
| `PrimaryImageTag` | `query` | `string` | 否 | 否 | - |
| `Profile` | `query` | `string` | 否 | 否 | - |
| `Progress` | `query` | `double` | 否 | 否 | - |
| `Quality` | `query` | `string` | 否 | 否 | - |
| `RequestedItemIds` | `query` | `long[]` | 否 | 否 | - |
| `Status` | `query` | `SyncJobStatus` | 否 | 否 | - |
| `SyncNewContent` | `query` | `bool` | 否 | 否 | - |
| `TargetId` | `query` | `string` | 否 | 否 | - |
| `TargetName` | `query` | `string` | 否 | 否 | - |
| `UnwatchedOnly` | `query` | `bool` | 否 | 否 | - |
| `UserId` | `query` | `long` | 否 | 否 | - |
| `VideoCodec` | `query` | `string` | 否 | 否 | - |

#### `POST` `/Sync/Jobs/{Id}/Delete`

- **摘要**：Cancels a sync job.
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Server.Sync.Api.SyncService` (Emby.Server.Sync.dll)
- **Request DTO**：`Emby.Server.Sync.Api.CancelSyncJob` (`Emby.Server.Sync.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Id |

#### `POST` `/Sync/OfflineActions`

- **摘要**：Reports an action that occurred while offline.
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Server.Sync.Api.SyncService` (Emby.Server.Sync.dll)
- **Request DTO**：`Emby.Server.Sync.Api.ReportOfflineActions` (`Emby.Server.Sync.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Capacity` | `query` | `int` | 否 | 否 | - |
| `Count` | `query` | `int` | 否 | 否 | - |
| `Item` | `query` | `UserAction` | 否 | 否 | - |

#### `GET` `/Sync/Options`

- **摘要**：Gets a list of available sync targets.
- **鉴权**：需要认证
- **返回类型**：`SyncDialogOptions`
- **处理服务**：`Emby.Server.Sync.Api.SyncService` (Emby.Server.Sync.dll)
- **Request DTO**：`Emby.Server.Sync.Api.GetSyncDialogOptions` (`Emby.Server.Sync.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `UserId` | `query` | `string` | 是 | 否 | UserId |
| `Category` | `query` | `SyncCategory?` | 否 | 否 | Category |
| `ExcludeProviders` | `query` | `string` | 否 | 否 | - |
| `ExcludeTargetIds` | `query` | `string` | 否 | 否 | - |
| `IncludeProviders` | `query` | `string` | 否 | 否 | - |
| `ItemIds` | `query` | `string` | 否 | 否 | ItemIds |
| `ParentId` | `query` | `string` | 否 | 否 | ParentId |
| `TargetId` | `query` | `string` | 否 | 否 | TargetId |

#### `GET` `/Sync/Targets`

- **摘要**：Gets a list of available sync targets.
- **鉴权**：需要认证
- **返回类型**：`List<SyncTarget>`
- **处理服务**：`Emby.Server.Sync.Api.SyncService` (Emby.Server.Sync.dll)
- **Request DTO**：`Emby.Server.Sync.Api.GetSyncTargets` (`Emby.Server.Sync.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `UserId` | `query` | `string` | 是 | 否 | UserId |

#### `POST` `/Sync/{ItemId}/Status`

- **摘要**：Gets sync status for an item.
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Server.Sync.Api.SyncService` (Emby.Server.Sync.dll)
- **Request DTO**：`Emby.Server.Sync.Api.GetSyncStatus` (`Emby.Server.Sync.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `ItemId` | `path` | `string` | 否 | 否 | - |
| `Progress` | `query` | `double?` | 否 | 否 | - |
| `Status` | `query` | `SyncJobItemStatus?` | 否 | 否 | - |
| `TargetId` | `query` | `string` | 否 | 否 | - |

#### `DELETE` `/Sync/{TargetId}/Items`

- **摘要**：Cancels items from a sync target
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Server.Sync.Api.SyncService` (Emby.Server.Sync.dll)
- **Request DTO**：`Emby.Server.Sync.Api.CancelItems` (`Emby.Server.Sync.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `TargetId` | `path` | `string` | 否 | 否 | TargetId |
| `ItemIds` | `query` | `string` | 否 | 否 | ItemIds |

#### `POST` `/Sync/{TargetId}/Items/Delete`

- **摘要**：Cancels items from a sync target
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Server.Sync.Api.SyncService` (Emby.Server.Sync.dll)
- **Request DTO**：`Emby.Server.Sync.Api.CancelItems` (`Emby.Server.Sync.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `TargetId` | `path` | `string` | 否 | 否 | TargetId |
| `ItemIds` | `query` | `string` | 否 | 否 | ItemIds |
