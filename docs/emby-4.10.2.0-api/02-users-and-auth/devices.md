# 设备管理

> 所属模块：**02. 用户、认证、设备与会话**  
> 路由前缀：`/Devices`  
> 接口总数：**8** 条

涵盖客户端设备列表、设备选项、摄像头上传与设备注销。

[← 返回总目录](../README.md)

## 接口总览

| # | 方法 | 路径 | 鉴权 | 返回类型 | 摘要 |
| :---: | :--- | :--- | :--- | :--- | :--- |
| 1 | `DELETE` | `/Devices` | 认证 (`Admin`) | `void` | Deletes a device |
| 2 | `GET` | `/Devices` | 认证 (`Admin`) | `QueryResult<DeviceInfo>` | Gets all devices |
| 3 | `GET` | `/Devices/CameraUploads` | 需认证 | `ContentUploadHistory` | Gets camera upload history for a device |
| 4 | `POST` | `/Devices/CameraUploads` | 认证 (`cameraupload`) | `void` | Uploads content |
| 5 | `POST` | `/Devices/Delete` | 认证 (`Admin`) | `void` | Deletes a device |
| 6 | `GET` | `/Devices/Info` | 认证 (`Admin`) | `DeviceInfo` | Gets info for a device |
| 7 | `GET` | `/Devices/Options` | 认证 (`Admin`) | `DeviceOptions` | Gets options for a device |
| 8 | `POST` | `/Devices/Options` | 认证 (`Admin`) | `void` | Updates device options |

## 接口详情

### DeviceService（8 条）

#### `DELETE` `/Devices`

- **摘要**：Deletes a device
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Devices.DeviceService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Devices.DeleteDevice` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `query` | `string` | 是 | 否 | Device Id |

#### `GET` `/Devices`

- **摘要**：Gets all devices
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`QueryResult<DeviceInfo>`
- **处理服务**：`Emby.Api.Devices.DeviceService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Devices.GetDevices` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `HasActiveAuthentication` | `query` | `bool?` | 否 | 否 | - |
| `Ids` | `query` | `string[]` | 否 | 否 | - |
| `IncludeIconUrl` | `query` | `bool` | 否 | 否 | - |
| `Limit` | `query` | `int?` | 否 | 否 | - |
| `ReportedDeviceId` | `query` | `string` | 否 | 否 | - |
| `SortBy` | `query` | `string` | 否 | 否 | - |
| `SortOrder` | `query` | `string` | 否 | 否 | Sort Order - Ascending,Descending |
| `StartIndex` | `query` | `int` | 否 | 否 | - |
| `SupportsSync` | `query` | `bool?` | 否 | 否 | - |
| `UserId` | `query` | `long` | 否 | 否 | - |

#### `GET` `/Devices/CameraUploads`

- **摘要**：Gets camera upload history for a device
- **鉴权**：需要认证
- **返回类型**：`ContentUploadHistory`
- **处理服务**：`Emby.Api.Devices.DeviceService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Devices.GetCameraUploads` (`Emby.Api.dll`)

_无请求参数。_

#### `POST` `/Devices/CameraUploads`

- **摘要**：Uploads content
- **鉴权**：需要认证；角色要求: `cameraupload`
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Devices.DeviceService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Devices.PostCameraUpload` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Album` | `query` | `string` | 是 | 否 | Album |
| `Id` | `query` | `string` | 是 | 否 | Id |
| `Name` | `query` | `string` | 是 | 否 | Name |
| `DateCreated` | `query` | `DateTimeOffset?` | 否 | 否 | - |
| `RequestStream` | `query` | `Stream` | 否 | 否 | - |

#### `POST` `/Devices/Delete`

- **摘要**：Deletes a device
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Devices.DeviceService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Devices.DeleteDevice` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `query` | `string` | 是 | 否 | Device Id |

#### `GET` `/Devices/Info`

- **摘要**：Gets info for a device
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`DeviceInfo`
- **处理服务**：`Emby.Api.Devices.DeviceService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Devices.GetDeviceInfo` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `query` | `string` | 是 | 否 | Device Id |

#### `GET` `/Devices/Options`

- **摘要**：Gets options for a device
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`DeviceOptions`
- **处理服务**：`Emby.Api.Devices.DeviceService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Devices.GetDeviceOptions` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `query` | `string` | 是 | 否 | Device Id |

#### `POST` `/Devices/Options`

- **摘要**：Updates device options
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Devices.DeviceService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Devices.PostDeviceOptions` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `query` | `string` | 是 | 否 | Device Id |
| `CustomName` | `query` | `string` | 否 | 否 | - |
