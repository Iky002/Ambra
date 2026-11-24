# DLNA 服务与设备配置文件（Emby.Dlna）

> 所属模块：**07. 内置插件 API（system/plugins）**  
> 路由前缀：`/Dlna`  
> 接口总数：**26** 条

涵盖 DLNA 服务器描述、内容目录/连接管理控制与 UPnP 事件订阅及设备配置文件管理。

[← 返回总目录](../README.md)

## 接口总览

| # | 方法 | 路径 | 鉴权 | 返回类型 | 摘要 |
| :---: | :--- | :--- | :--- | :--- | :--- |
| 1 | `GET` | `/Dlna/icons/{Filename}` | 公开 | `object` | Gets a server icon |
| 2 | `GET` | `/Dlna/ProfileInfos` | 认证 (`Admin`) | `DlnaProfile[]` | Gets a list of profiles |
| 3 | `POST` | `/Dlna/Profiles` | 认证 (`Admin`) | `void` | Creates a profile |
| 4 | `GET` | `/Dlna/Profiles/Default` | 认证 (`Admin`) | `DlnaProfile` | Gets the default profile |
| 5 | `DELETE` | `/Dlna/Profiles/{Id}` | 认证 (`Admin`) | `void` | Deletes a profile |
| 6 | `GET` | `/Dlna/Profiles/{Id}` | 认证 (`Admin`) | `DlnaProfile` | Gets a single profile |
| 7 | `POST` | `/Dlna/Profiles/{Id}` | 认证 (`Admin`) | `void` | Updates a profile |
| 8 | `GET` | `/Dlna/{UuId}/connectionmanager/connectionmanager` | 公开 | `object` | Gets dlna connection manager xml |
| 9 | `HEAD` | `/Dlna/{UuId}/connectionmanager/connectionmanager` | 公开 | `object` | Gets dlna connection manager xml |
| 10 | `GET` | `/Dlna/{UuId}/connectionmanager/connectionmanager.xml` | 公开 | `object` | Gets dlna connection manager xml |
| 11 | `HEAD` | `/Dlna/{UuId}/connectionmanager/connectionmanager.xml` | 公开 | `object` | Gets dlna connection manager xml |
| 12 | `POST` | `/Dlna/{UuId}/connectionmanager/control` | 公开 | `object` | Processes a control request |
| 13 | `SUBSCRIBE` | `/Dlna/{UuId}/connectionmanager/events` | 公开 | `-` | Processes an event subscription request |
| 14 | `UNSUBSCRIBE` | `/Dlna/{UuId}/connectionmanager/events` | 公开 | `-` | Processes an event subscription request |
| 15 | `GET` | `/Dlna/{UuId}/contentdirectory/contentdirectory` | 公开 | `object` | Gets dlna content directory xml |
| 16 | `HEAD` | `/Dlna/{UuId}/contentdirectory/contentdirectory` | 公开 | `object` | Gets dlna content directory xml |
| 17 | `GET` | `/Dlna/{UuId}/contentdirectory/contentdirectory.xml` | 公开 | `object` | Gets dlna content directory xml |
| 18 | `HEAD` | `/Dlna/{UuId}/contentdirectory/contentdirectory.xml` | 公开 | `object` | Gets dlna content directory xml |
| 19 | `POST` | `/Dlna/{UuId}/contentdirectory/control` | 公开 | `object` | Processes a control request |
| 20 | `SUBSCRIBE` | `/Dlna/{UuId}/contentdirectory/events` | 公开 | `-` | Processes an event subscription request |
| 21 | `UNSUBSCRIBE` | `/Dlna/{UuId}/contentdirectory/events` | 公开 | `-` | Processes an event subscription request |
| 22 | `GET` | `/Dlna/{UuId}/description` | 公开 | `object` | Gets dlna server info |
| 23 | `HEAD` | `/Dlna/{UuId}/description` | 公开 | `object` | Gets dlna server info |
| 24 | `GET` | `/Dlna/{UuId}/description.xml` | 公开 | `object` | Gets dlna server info |
| 25 | `HEAD` | `/Dlna/{UuId}/description.xml` | 公开 | `object` | Gets dlna server info |
| 26 | `GET` | `/Dlna/{UuId}/icons/{Filename}` | 公开 | `object` | Gets a server icon |

## 接口详情

### DlnaServerService（16 条）

#### `GET` `/Dlna/icons/{Filename}`

- **摘要**：Gets a server icon
- **鉴权**：无需认证（公开接口）
- **返回类型**：`object`
- **处理服务**：`Emby.Dlna.Api.DlnaServerService` (Emby.Dlna.dll)
- **Request DTO**：`Emby.Dlna.Api.GetIcon` (`Emby.Dlna.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Filename` | `path` | `string` | 是 | 否 | The icon filename |
| `UuId` | `query` | `string` | 否 | 否 | Server UuId |

#### `GET` `/Dlna/{UuId}/connectionmanager/connectionmanager`

- **摘要**：Gets dlna connection manager xml
- **鉴权**：无需认证（公开接口）
- **返回类型**：`object`
- **处理服务**：`Emby.Dlna.Api.DlnaServerService` (Emby.Dlna.dll)
- **Request DTO**：`Emby.Dlna.Api.GetConnnectionManager` (`Emby.Dlna.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `UuId` | `path` | `string` | 否 | 否 | Server UuId |

#### `HEAD` `/Dlna/{UuId}/connectionmanager/connectionmanager`

- **摘要**：Gets dlna connection manager xml
- **鉴权**：无需认证（公开接口）
- **返回类型**：`object`
- **处理服务**：`Emby.Dlna.Api.DlnaServerService` (Emby.Dlna.dll)
- **Request DTO**：`Emby.Dlna.Api.GetConnnectionManager` (`Emby.Dlna.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `UuId` | `path` | `string` | 否 | 否 | Server UuId |

#### `GET` `/Dlna/{UuId}/connectionmanager/connectionmanager.xml`

- **摘要**：Gets dlna connection manager xml
- **鉴权**：无需认证（公开接口）
- **返回类型**：`object`
- **处理服务**：`Emby.Dlna.Api.DlnaServerService` (Emby.Dlna.dll)
- **Request DTO**：`Emby.Dlna.Api.GetConnnectionManager` (`Emby.Dlna.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `UuId` | `path` | `string` | 否 | 否 | Server UuId |

#### `HEAD` `/Dlna/{UuId}/connectionmanager/connectionmanager.xml`

- **摘要**：Gets dlna connection manager xml
- **鉴权**：无需认证（公开接口）
- **返回类型**：`object`
- **处理服务**：`Emby.Dlna.Api.DlnaServerService` (Emby.Dlna.dll)
- **Request DTO**：`Emby.Dlna.Api.GetConnnectionManager` (`Emby.Dlna.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `UuId` | `path` | `string` | 否 | 否 | Server UuId |

#### `POST` `/Dlna/{UuId}/connectionmanager/control`

- **摘要**：Processes a control request
- **鉴权**：无需认证（公开接口）
- **返回类型**：`object`
- **处理服务**：`Emby.Dlna.Api.DlnaServerService` (Emby.Dlna.dll)
- **Request DTO**：`Emby.Dlna.Api.ProcessConnectionManagerControlRequest` (`Emby.Dlna.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `UuId` | `path` | `string` | 否 | 否 | Server UuId |
| `RequestStream` | `query` | `Stream` | 否 | 否 | - |

#### `GET` `/Dlna/{UuId}/contentdirectory/contentdirectory`

- **摘要**：Gets dlna content directory xml
- **鉴权**：无需认证（公开接口）
- **返回类型**：`object`
- **处理服务**：`Emby.Dlna.Api.DlnaServerService` (Emby.Dlna.dll)
- **Request DTO**：`Emby.Dlna.Api.GetContentDirectory` (`Emby.Dlna.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `UuId` | `path` | `string` | 否 | 否 | Server UuId |

#### `HEAD` `/Dlna/{UuId}/contentdirectory/contentdirectory`

- **摘要**：Gets dlna content directory xml
- **鉴权**：无需认证（公开接口）
- **返回类型**：`object`
- **处理服务**：`Emby.Dlna.Api.DlnaServerService` (Emby.Dlna.dll)
- **Request DTO**：`Emby.Dlna.Api.GetContentDirectory` (`Emby.Dlna.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `UuId` | `path` | `string` | 否 | 否 | Server UuId |

#### `GET` `/Dlna/{UuId}/contentdirectory/contentdirectory.xml`

- **摘要**：Gets dlna content directory xml
- **鉴权**：无需认证（公开接口）
- **返回类型**：`object`
- **处理服务**：`Emby.Dlna.Api.DlnaServerService` (Emby.Dlna.dll)
- **Request DTO**：`Emby.Dlna.Api.GetContentDirectory` (`Emby.Dlna.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `UuId` | `path` | `string` | 否 | 否 | Server UuId |

#### `HEAD` `/Dlna/{UuId}/contentdirectory/contentdirectory.xml`

- **摘要**：Gets dlna content directory xml
- **鉴权**：无需认证（公开接口）
- **返回类型**：`object`
- **处理服务**：`Emby.Dlna.Api.DlnaServerService` (Emby.Dlna.dll)
- **Request DTO**：`Emby.Dlna.Api.GetContentDirectory` (`Emby.Dlna.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `UuId` | `path` | `string` | 否 | 否 | Server UuId |

#### `POST` `/Dlna/{UuId}/contentdirectory/control`

- **摘要**：Processes a control request
- **鉴权**：无需认证（公开接口）
- **返回类型**：`object`
- **处理服务**：`Emby.Dlna.Api.DlnaServerService` (Emby.Dlna.dll)
- **Request DTO**：`Emby.Dlna.Api.ProcessContentDirectoryControlRequest` (`Emby.Dlna.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `UuId` | `path` | `string` | 否 | 否 | Server UuId |
| `RequestStream` | `query` | `Stream` | 否 | 否 | - |

#### `GET` `/Dlna/{UuId}/description`

- **摘要**：Gets dlna server info
- **鉴权**：无需认证（公开接口）
- **返回类型**：`object`
- **处理服务**：`Emby.Dlna.Api.DlnaServerService` (Emby.Dlna.dll)
- **Request DTO**：`Emby.Dlna.Api.GetDescriptionXml` (`Emby.Dlna.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `UuId` | `path` | `string` | 否 | 否 | Server UuId |

#### `HEAD` `/Dlna/{UuId}/description`

- **摘要**：Gets dlna server info
- **鉴权**：无需认证（公开接口）
- **返回类型**：`object`
- **处理服务**：`Emby.Dlna.Api.DlnaServerService` (Emby.Dlna.dll)
- **Request DTO**：`Emby.Dlna.Api.GetDescriptionXml` (`Emby.Dlna.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `UuId` | `path` | `string` | 否 | 否 | Server UuId |

#### `GET` `/Dlna/{UuId}/description.xml`

- **摘要**：Gets dlna server info
- **鉴权**：无需认证（公开接口）
- **返回类型**：`object`
- **处理服务**：`Emby.Dlna.Api.DlnaServerService` (Emby.Dlna.dll)
- **Request DTO**：`Emby.Dlna.Api.GetDescriptionXml` (`Emby.Dlna.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `UuId` | `path` | `string` | 否 | 否 | Server UuId |

#### `HEAD` `/Dlna/{UuId}/description.xml`

- **摘要**：Gets dlna server info
- **鉴权**：无需认证（公开接口）
- **返回类型**：`object`
- **处理服务**：`Emby.Dlna.Api.DlnaServerService` (Emby.Dlna.dll)
- **Request DTO**：`Emby.Dlna.Api.GetDescriptionXml` (`Emby.Dlna.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `UuId` | `path` | `string` | 否 | 否 | Server UuId |

#### `GET` `/Dlna/{UuId}/icons/{Filename}`

- **摘要**：Gets a server icon
- **鉴权**：无需认证（公开接口）
- **返回类型**：`object`
- **处理服务**：`Emby.Dlna.Api.DlnaServerService` (Emby.Dlna.dll)
- **Request DTO**：`Emby.Dlna.Api.GetIcon` (`Emby.Dlna.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Filename` | `path` | `string` | 是 | 否 | The icon filename |
| `UuId` | `query` | `string` | 否 | 否 | Server UuId |

### DlnaService（6 条）

#### `GET` `/Dlna/ProfileInfos`

- **摘要**：Gets a list of profiles
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`DlnaProfile[]`
- **处理服务**：`Emby.Dlna.Api.DlnaService` (Emby.Dlna.dll)
- **Request DTO**：`Emby.Dlna.Api.GetProfileInfos` (`Emby.Dlna.dll`)

_无请求参数。_

#### `POST` `/Dlna/Profiles`

- **摘要**：Creates a profile
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`void`
- **处理服务**：`Emby.Dlna.Api.DlnaService` (Emby.Dlna.dll)
- **Request DTO**：`Emby.Dlna.Api.CreateProfile` (`Emby.Dlna.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `AlbumArtPn` | `query` | `string` | 否 | 否 | - |
| `CodecProfiles` | `query` | `CodecProfile[]` | 否 | 否 | - |
| `ContainerProfiles` | `query` | `ContainerProfile[]` | 否 | 否 | - |
| `DeclaredFeatures` | `query` | `string[]` | 否 | 否 | - |
| `DirectPlayProfiles` | `query` | `DirectPlayProfile[]` | 否 | 否 | - |
| `EnableAlbumArtInDidl` | `query` | `bool` | 否 | 否 | - |
| `EnableSingleAlbumArtLimit` | `query` | `bool` | 否 | 否 | - |
| `EnableSingleSubtitleLimit` | `query` | `bool` | 否 | 否 | - |
| `FriendlyName` | `query` | `string` | 否 | 否 | - |
| `Id` | `query` | `string` | 否 | 否 | - |
| `Identification` | `query` | `DeviceIdentification` | 否 | 否 | - |
| `IgnoreTranscodeByteRangeRequests` | `query` | `bool` | 否 | 否 | - |
| `Manufacturer` | `query` | `string` | 否 | 否 | - |
| `ManufacturerUrl` | `query` | `string` | 否 | 否 | - |
| `MaxAlbumArtHeight` | `query` | `int` | 否 | 否 | - |
| `MaxAlbumArtWidth` | `query` | `int` | 否 | 否 | - |
| `MaxIconHeight` | `query` | `int?` | 否 | 否 | - |
| `MaxIconWidth` | `query` | `int?` | 否 | 否 | - |
| `MaxStaticMusicBitrate` | `query` | `int?` | 否 | 否 | - |
| `MaxStreamingBitrate` | `query` | `long?` | 否 | 否 | - |
| `ModelDescription` | `query` | `string` | 否 | 否 | - |
| `ModelName` | `query` | `string` | 否 | 否 | - |
| `ModelNumber` | `query` | `string` | 否 | 否 | - |
| `ModelUrl` | `query` | `string` | 否 | 否 | - |
| `MusicStreamingTranscodingBitrate` | `query` | `int?` | 否 | 否 | - |
| `Name` | `query` | `string` | 否 | 否 | - |
| `ProtocolInfo` | `query` | `string` | 否 | 否 | - |
| `ProtocolInfoDetection` | `query` | `ProtocolInfoDetection` | 否 | 否 | - |
| `RequiresPlainFolders` | `query` | `bool` | 否 | 否 | - |
| `RequiresPlainVideoItems` | `query` | `bool` | 否 | 否 | - |
| `ResponseProfiles` | `query` | `ResponseProfile[]` | 否 | 否 | - |
| `SerialNumber` | `query` | `string` | 否 | 否 | - |
| `SubtitleProfiles` | `query` | `SubtitleProfile[]` | 否 | 否 | - |
| `SupportedMediaTypes` | `query` | `string` | 否 | 否 | - |
| `SupportsSamsungBookmark` | `query` | `bool` | 否 | 否 | - |
| `TimelineOffsetSeconds` | `query` | `int` | 否 | 否 | - |
| `TranscodingProfiles` | `query` | `TranscodingProfile[]` | 否 | 否 | - |
| `Type` | `query` | `DeviceProfileType` | 否 | 否 | - |
| `UserId` | `query` | `string` | 否 | 否 | - |

#### `GET` `/Dlna/Profiles/Default`

- **摘要**：Gets the default profile
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`DlnaProfile`
- **处理服务**：`Emby.Dlna.Api.DlnaService` (Emby.Dlna.dll)
- **Request DTO**：`Emby.Dlna.Api.GetDefaultProfile` (`Emby.Dlna.dll`)

_无请求参数。_

#### `DELETE` `/Dlna/Profiles/{Id}`

- **摘要**：Deletes a profile
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`void`
- **处理服务**：`Emby.Dlna.Api.DlnaService` (Emby.Dlna.dll)
- **Request DTO**：`Emby.Dlna.Api.DeleteProfile` (`Emby.Dlna.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Profile Id |

#### `GET` `/Dlna/Profiles/{Id}`

- **摘要**：Gets a single profile
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`DlnaProfile`
- **处理服务**：`Emby.Dlna.Api.DlnaService` (Emby.Dlna.dll)
- **Request DTO**：`Emby.Dlna.Api.GetProfile` (`Emby.Dlna.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Profile Id |

#### `POST` `/Dlna/Profiles/{Id}`

- **摘要**：Updates a profile
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`void`
- **处理服务**：`Emby.Dlna.Api.DlnaService` (Emby.Dlna.dll)
- **Request DTO**：`Emby.Dlna.Api.UpdateProfile` (`Emby.Dlna.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 否 | 否 | - |
| `AlbumArtPn` | `query` | `string` | 否 | 否 | - |
| `CodecProfiles` | `query` | `CodecProfile[]` | 否 | 否 | - |
| `ContainerProfiles` | `query` | `ContainerProfile[]` | 否 | 否 | - |
| `DeclaredFeatures` | `query` | `string[]` | 否 | 否 | - |
| `DirectPlayProfiles` | `query` | `DirectPlayProfile[]` | 否 | 否 | - |
| `EnableAlbumArtInDidl` | `query` | `bool` | 否 | 否 | - |
| `EnableSingleAlbumArtLimit` | `query` | `bool` | 否 | 否 | - |
| `EnableSingleSubtitleLimit` | `query` | `bool` | 否 | 否 | - |
| `FriendlyName` | `query` | `string` | 否 | 否 | - |
| `Identification` | `query` | `DeviceIdentification` | 否 | 否 | - |
| `IgnoreTranscodeByteRangeRequests` | `query` | `bool` | 否 | 否 | - |
| `Manufacturer` | `query` | `string` | 否 | 否 | - |
| `ManufacturerUrl` | `query` | `string` | 否 | 否 | - |
| `MaxAlbumArtHeight` | `query` | `int` | 否 | 否 | - |
| `MaxAlbumArtWidth` | `query` | `int` | 否 | 否 | - |
| `MaxIconHeight` | `query` | `int?` | 否 | 否 | - |
| `MaxIconWidth` | `query` | `int?` | 否 | 否 | - |
| `MaxStaticMusicBitrate` | `query` | `int?` | 否 | 否 | - |
| `MaxStreamingBitrate` | `query` | `long?` | 否 | 否 | - |
| `ModelDescription` | `query` | `string` | 否 | 否 | - |
| `ModelName` | `query` | `string` | 否 | 否 | - |
| `ModelNumber` | `query` | `string` | 否 | 否 | - |
| `ModelUrl` | `query` | `string` | 否 | 否 | - |
| `MusicStreamingTranscodingBitrate` | `query` | `int?` | 否 | 否 | - |
| `Name` | `query` | `string` | 否 | 否 | - |
| `ProtocolInfo` | `query` | `string` | 否 | 否 | - |
| `ProtocolInfoDetection` | `query` | `ProtocolInfoDetection` | 否 | 否 | - |
| `RequiresPlainFolders` | `query` | `bool` | 否 | 否 | - |
| `RequiresPlainVideoItems` | `query` | `bool` | 否 | 否 | - |
| `ResponseProfiles` | `query` | `ResponseProfile[]` | 否 | 否 | - |
| `SerialNumber` | `query` | `string` | 否 | 否 | - |
| `SubtitleProfiles` | `query` | `SubtitleProfile[]` | 否 | 否 | - |
| `SupportedMediaTypes` | `query` | `string` | 否 | 否 | - |
| `SupportsSamsungBookmark` | `query` | `bool` | 否 | 否 | - |
| `TimelineOffsetSeconds` | `query` | `int` | 否 | 否 | - |
| `TranscodingProfiles` | `query` | `TranscodingProfile[]` | 否 | 否 | - |
| `Type` | `query` | `DeviceProfileType` | 否 | 否 | - |
| `UserId` | `query` | `string` | 否 | 否 | - |

### Emby.Dlna (无直接 Service 绑定)（4 条）

#### `SUBSCRIBE` `/Dlna/{UuId}/connectionmanager/events`

- **摘要**：Processes an event subscription request
- **鉴权**：无需认证（公开接口）
- **返回类型**：`void / Stream`
- **处理服务**：未匹配直接方法
- **Request DTO**：`Emby.Dlna.Api.ProcessConnectionManagerEventRequest` (`Emby.Dlna.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `UuId` | `path` | `string` | 否 | 否 | Server UuId |

#### `UNSUBSCRIBE` `/Dlna/{UuId}/connectionmanager/events`

- **摘要**：Processes an event subscription request
- **鉴权**：无需认证（公开接口）
- **返回类型**：`void / Stream`
- **处理服务**：未匹配直接方法
- **Request DTO**：`Emby.Dlna.Api.ProcessConnectionManagerEventRequest` (`Emby.Dlna.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `UuId` | `path` | `string` | 否 | 否 | Server UuId |

#### `SUBSCRIBE` `/Dlna/{UuId}/contentdirectory/events`

- **摘要**：Processes an event subscription request
- **鉴权**：无需认证（公开接口）
- **返回类型**：`void / Stream`
- **处理服务**：未匹配直接方法
- **Request DTO**：`Emby.Dlna.Api.ProcessContentDirectoryEventRequest` (`Emby.Dlna.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `UuId` | `path` | `string` | 否 | 否 | Server UuId |

#### `UNSUBSCRIBE` `/Dlna/{UuId}/contentdirectory/events`

- **摘要**：Processes an event subscription request
- **鉴权**：无需认证（公开接口）
- **返回类型**：`void / Stream`
- **处理服务**：未匹配直接方法
- **Request DTO**：`Emby.Dlna.Api.ProcessContentDirectoryEventRequest` (`Emby.Dlna.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `UuId` | `path` | `string` | 否 | 否 | Server UuId |
