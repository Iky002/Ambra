# 通知服务（Notifications）

> 所属模块：**06. 同步、通知与插件管理**  
> 路由前缀：`/Notifications`  
> 接口总数：**13** 条

涵盖通知类型、通知服务配置、用户通知规则与测试通知发送。

[← 返回总目录](../README.md)

## 接口总览

| # | 方法 | 路径 | 鉴权 | 返回类型 | 摘要 |
| :---: | :--- | :--- | :--- | :--- | :--- |
| 1 | `POST` | `/Notifications/Admin` | 需认证 | `void` | Sends a notification to all admin users |
| 2 | `GET` | `/Notifications/Services` *(隐藏)* | 需认证 | `NotificationServiceInfo[]` | Gets notification types |
| 3 | `DELETE` | `/Notifications/Services/Configured` *(隐藏)* | 需认证 | `void` | - |
| 4 | `GET` | `/Notifications/Services/Configured` *(隐藏)* | 需认证 | `UserNotificationInfo[]` | - |
| 5 | `POST` | `/Notifications/Services/Configured` *(隐藏)* | 需认证 | `void` | - |
| 6 | `GET` | `/Notifications/Services/Defaults` | 需认证 | `UserNotificationInfo` | Gets default notification info |
| 7 | `POST` | `/Notifications/Services/Test` | 需认证 | `void` | Sends a test notification |
| 8 | `GET` | `/Notifications/Tokens` *(隐藏)* | 需认证 | `NotificationCategoryInfo[]` | Gets all available tokens |
| 9 | `GET` | `/Notifications/Types` | 需认证 | `NotificationCategoryInfo[]` | Gets notification types |
| 10 | `GET` | `/Notifications/{UserId}` *(隐藏)* | 需认证 | `object` | Gets notifications |
| 11 | `POST` | `/Notifications/{UserId}/Read` *(隐藏)* | 需认证 | `void` | Marks notifications as read |
| 12 | `GET` | `/Notifications/{UserId}/Summary` *(隐藏)* | 需认证 | `object` | Gets a notification summary for a user |
| 13 | `POST` | `/Notifications/{UserId}/Unread` *(隐藏)* | 需认证 | `void` | Marks notifications as unread |

## 接口详情

### NotificationsService（7 条）

#### `POST` `/Notifications/Admin`

- **摘要**：Sends a notification to all admin users
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Notifications.Api.NotificationsService` (Emby.Notifications.dll)
- **Request DTO**：`Emby.Notifications.Api.AddAdminNotification` (`Emby.Notifications.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Description` | `query` | `string` | 是 | 否 | The notification's description |
| `Name` | `query` | `string` | 是 | 否 | The notification's name |
| `DisplayDateTime` | `query` | `bool` | 否 | 否 | - |
| `ImageUrl` | `query` | `string` | 否 | 否 | The notification's image url |
| `Level` | `query` | `string` | 否 | 否 | The notification level |
| `Url` | `query` | `string` | 否 | 否 | The notification's info url |

#### `GET` `/Notifications/Tokens` `[隐藏接口]`

- **摘要**：Gets all available tokens
- **鉴权**：需要认证
- **返回类型**：`NotificationCategoryInfo[]`
- **处理服务**：`Emby.Notifications.Api.NotificationsService` (Emby.Notifications.dll)
- **Request DTO**：`Emby.Notifications.Api.GetAllTokens` (`Emby.Notifications.dll`)

_无请求参数。_

#### `GET` `/Notifications/Types`

- **摘要**：Gets notification types
- **鉴权**：需要认证
- **返回类型**：`NotificationCategoryInfo[]`
- **处理服务**：`Emby.Notifications.Api.NotificationsService` (Emby.Notifications.dll)
- **Request DTO**：`Emby.Notifications.Api.GetNotificationTypes` (`Emby.Notifications.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `UserId` | `query` | `string` | 否 | 否 | - |

#### `GET` `/Notifications/{UserId}` `[隐藏接口]`

- **摘要**：Gets notifications
- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Notifications.Api.NotificationsService` (Emby.Notifications.dll)
- **Request DTO**：`Emby.Notifications.Api.GetNotifications` (`Emby.Notifications.dll`)

_无请求参数。_

#### `POST` `/Notifications/{UserId}/Read` `[隐藏接口]`

- **摘要**：Marks notifications as read
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Notifications.Api.NotificationsService` (Emby.Notifications.dll)
- **Request DTO**：`Emby.Notifications.Api.MarkRead` (`Emby.Notifications.dll`)

_无请求参数。_

#### `GET` `/Notifications/{UserId}/Summary` `[隐藏接口]`

- **摘要**：Gets a notification summary for a user
- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Notifications.Api.NotificationsService` (Emby.Notifications.dll)
- **Request DTO**：`Emby.Notifications.Api.GetNotificationsSummary` (`Emby.Notifications.dll`)

_无请求参数。_

#### `POST` `/Notifications/{UserId}/Unread` `[隐藏接口]`

- **摘要**：Marks notifications as unread
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Notifications.Api.NotificationsService` (Emby.Notifications.dll)
- **Request DTO**：`Emby.Notifications.Api.MarkUnread` (`Emby.Notifications.dll`)

_无请求参数。_

### UserNotificationsService（6 条）

#### `GET` `/Notifications/Services` `[隐藏接口]`

- **摘要**：Gets notification types
- **鉴权**：需要认证
- **返回类型**：`NotificationServiceInfo[]`
- **处理服务**：`Emby.Notifications.Api.UserNotificationsService` (Emby.Notifications.dll)
- **Request DTO**：`Emby.Notifications.Api.GetUserNotificationServices` (`Emby.Notifications.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `UserId` | `query` | `string` | 否 | 否 | - |

#### `DELETE` `/Notifications/Services/Configured` `[隐藏接口]`

- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Notifications.Api.UserNotificationsService` (Emby.Notifications.dll)
- **Request DTO**：`Emby.Notifications.Api.DeleteConfiguredNotification` (`Emby.Notifications.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `query` | `string` | 否 | 否 | - |
| `UserId` | `query` | `string` | 否 | 否 | - |

#### `GET` `/Notifications/Services/Configured` `[隐藏接口]`

- **鉴权**：需要认证
- **返回类型**：`UserNotificationInfo[]`
- **处理服务**：`Emby.Notifications.Api.UserNotificationsService` (Emby.Notifications.dll)
- **Request DTO**：`Emby.Notifications.Api.GetConfiguredNotifications` (`Emby.Notifications.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `UserId` | `query` | `string` | 否 | 否 | - |

#### `POST` `/Notifications/Services/Configured` `[隐藏接口]`

- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Notifications.Api.UserNotificationsService` (Emby.Notifications.dll)
- **Request DTO**：`Emby.Notifications.Api.SaveConfiguredNotification` (`Emby.Notifications.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `DeviceIds` | `query` | `string[]` | 否 | 否 | - |
| `Enabled` | `query` | `bool` | 否 | 否 | - |
| `EventIds` | `query` | `string[]` | 否 | 否 | - |
| `FriendlyName` | `query` | `string` | 否 | 否 | - |
| `GroupItems` | `query` | `bool` | 否 | 否 | - |
| `Id` | `query` | `string` | 否 | 否 | - |
| `IsSelfNotification` | `query` | `bool` | 否 | 否 | - |
| `LibraryIds` | `query` | `string[]` | 否 | 否 | - |
| `NotifierKey` | `query` | `string` | 否 | 否 | - |
| `Options` | `query` | `Dictionary<string, string>` | 否 | 否 | - |
| `PluginId` | `query` | `string` | 否 | 否 | - |
| `ServiceName` | `query` | `string` | 否 | 否 | - |
| `SetupModuleUrl` | `query` | `string` | 否 | 否 | - |
| `UserId` | `query` | `string` | 否 | 否 | - |
| `UserIds` | `query` | `string[]` | 否 | 否 | - |

#### `GET` `/Notifications/Services/Defaults`

- **摘要**：Gets default notification info
- **鉴权**：需要认证
- **返回类型**：`UserNotificationInfo`
- **处理服务**：`Emby.Notifications.Api.UserNotificationsService` (Emby.Notifications.dll)
- **Request DTO**：`Emby.Notifications.Api.GetDefaultNotificationInfo` (`Emby.Notifications.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `NotifierKey` | `query` | `string` | 否 | 否 | - |
| `UserId` | `query` | `string` | 否 | 否 | - |

#### `POST` `/Notifications/Services/Test`

- **摘要**：Sends a test notification
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Notifications.Api.UserNotificationsService` (Emby.Notifications.dll)
- **Request DTO**：`Emby.Notifications.Api.SendTestNotification` (`Emby.Notifications.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `DeviceIds` | `query` | `string[]` | 否 | 否 | - |
| `Enabled` | `query` | `bool` | 否 | 否 | - |
| `EventIds` | `query` | `string[]` | 否 | 否 | - |
| `FriendlyName` | `query` | `string` | 否 | 否 | - |
| `GroupItems` | `query` | `bool` | 否 | 否 | - |
| `Id` | `query` | `string` | 否 | 否 | - |
| `IsSelfNotification` | `query` | `bool` | 否 | 否 | - |
| `LibraryIds` | `query` | `string[]` | 否 | 否 | - |
| `NotifierKey` | `query` | `string` | 否 | 否 | - |
| `Options` | `query` | `Dictionary<string, string>` | 否 | 否 | - |
| `PluginId` | `query` | `string` | 否 | 否 | - |
| `ServiceName` | `query` | `string` | 否 | 否 | - |
| `SetupModuleUrl` | `query` | `string` | 否 | 否 | - |
| `UserId` | `query` | `string` | 否 | 否 | - |
| `UserIds` | `query` | `string[]` | 否 | 否 | - |
