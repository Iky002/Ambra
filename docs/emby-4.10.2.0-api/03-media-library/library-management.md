# 媒体库管理与虚拟文件夹

> 所属模块：**03. 媒体库、条目与元数据**  
> 路由前缀：`/Libraries`, `/Library`  
> 接口总数：**21** 条

涵盖媒体库扫描、虚拟文件夹增删改、媒体更新通知与物理媒体文件管理。

[← 返回总目录](../README.md)

## 接口总览

| # | 方法 | 路径 | 鉴权 | 返回类型 | 摘要 |
| :---: | :--- | :--- | :--- | :--- | :--- |
| 1 | `GET` | `/Libraries/AvailableOptions` | 需认证 | `LibraryOptionsResult` | - |
| 2 | `POST` | `/Library/Media/Updated` | 需认证 | `void` | Reports that new movies have been added by an external source |
| 3 | `GET` | `/Library/MediaFolders` | 需认证 | `QueryResult<BaseItemDto>` | Gets all user media folders. |
| 4 | `POST` | `/Library/Movies/Added` | 需认证 | `void` | Deprecated. Use /Library/Media/Updated |
| 5 | `POST` | `/Library/Movies/Updated` | 需认证 | `void` | Deprecated. Use /Library/Media/Updated |
| 6 | `GET` | `/Library/PhysicalPaths` | 认证 (`Admin`) | `List<string>` | Gets a list of physical paths from virtual folders |
| 7 | `POST` | `/Library/Refresh` | 认证 (`Admin`) | `void` | Starts a library scan |
| 8 | `GET` | `/Library/SelectableMediaFolders` | 需认证 | `MediaFolder[]` | Gets all user media folders. |
| 9 | `POST` | `/Library/Series/Added` | 需认证 | `void` | Deprecated. Use /Library/Media/Updated |
| 10 | `POST` | `/Library/Series/Updated` | 需认证 | `void` | Deprecated. Use /Library/Media/Updated |
| 11 | `DELETE` | `/Library/VirtualFolders` | 认证 (`Admin`) | `void` | - |
| 12 | `GET` | `/Library/VirtualFolders` *(隐藏)* | 需认证 | `List<VirtualFolderInfo>` | - |
| 13 | `POST` | `/Library/VirtualFolders` | 认证 (`Admin`) | `void` | - |
| 14 | `POST` | `/Library/VirtualFolders/Delete` | 认证 (`Admin`) | `void` | - |
| 15 | `POST` | `/Library/VirtualFolders/LibraryOptions` | 认证 (`Admin`) | `void` | - |
| 16 | `POST` | `/Library/VirtualFolders/Name` | 认证 (`Admin`) | `void` | - |
| 17 | `DELETE` | `/Library/VirtualFolders/Paths` | 认证 (`Admin`) | `void` | - |
| 18 | `POST` | `/Library/VirtualFolders/Paths` | 认证 (`Admin`) | `void` | - |
| 19 | `POST` | `/Library/VirtualFolders/Paths/Delete` | 认证 (`Admin`) | `void` | - |
| 20 | `POST` | `/Library/VirtualFolders/Paths/Update` | 认证 (`Admin`) | `void` | - |
| 21 | `GET` | `/Library/VirtualFolders/Query` | 需认证 | `QueryResult<VirtualFolderInfo>` | - |

## 接口详情

### LibraryService（10 条）

#### `GET` `/Libraries/AvailableOptions`

- **鉴权**：需要认证；允许在初始化向导完成前调用
- **返回类型**：`LibraryOptionsResult`
- **处理服务**：`Emby.Api.Library.LibraryService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Library.GetLibraryOptionsInfo` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `IsNewLibrary` | `query` | `bool` | 否 | 否 | - |
| `LibraryContentType` | `query` | `string` | 否 | 否 | - |

#### `POST` `/Library/Media/Updated`

- **摘要**：Reports that new movies have been added by an external source
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Library.LibraryService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Library.PostUpdatedMedia` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Updates` | `body` | `List<MediaUpdateInfo>` | 否 | 否 | A list of updated media paths |

#### `GET` `/Library/MediaFolders`

- **摘要**：Gets all user media folders.
- **鉴权**：需要认证
- **返回类型**：`QueryResult<BaseItemDto>`
- **处理服务**：`Emby.Api.Library.LibraryService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Library.GetMediaFolders` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `IsHidden` | `query` | `bool?` | 否 | 否 | Optional. Filter by folders that are marked hidden, or not. |

#### `POST` `/Library/Movies/Added`

- **摘要**：Deprecated. Use /Library/Media/Updated
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Library.LibraryService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Library.PostUpdatedMovies` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `ImdbId` | `path` | `string` | 否 | 否 | Imdb Id |
| `TmdbId` | `path` | `string` | 否 | 否 | Tmdb Id |

#### `POST` `/Library/Movies/Updated`

- **摘要**：Deprecated. Use /Library/Media/Updated
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Library.LibraryService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Library.PostUpdatedMovies` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `ImdbId` | `path` | `string` | 否 | 否 | Imdb Id |
| `TmdbId` | `path` | `string` | 否 | 否 | Tmdb Id |

#### `GET` `/Library/PhysicalPaths`

- **摘要**：Gets a list of physical paths from virtual folders
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`List<string>`
- **处理服务**：`Emby.Api.Library.LibraryService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Library.GetPhyscialPaths` (`Emby.Api.dll`)

_无请求参数。_

#### `POST` `/Library/Refresh`

- **摘要**：Starts a library scan
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Library.LibraryService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Library.RefreshLibrary` (`Emby.Api.dll`)

_无请求参数。_

#### `GET` `/Library/SelectableMediaFolders`

- **摘要**：Gets all user media folders.
- **鉴权**：需要认证
- **返回类型**：`MediaFolder[]`
- **处理服务**：`Emby.Api.Library.LibraryService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Library.GetSelectableMediaFolders` (`Emby.Api.dll`)

_无请求参数。_

#### `POST` `/Library/Series/Added`

- **摘要**：Deprecated. Use /Library/Media/Updated
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Library.LibraryService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Library.PostUpdatedSeries` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `TvdbId` | `path` | `string` | 否 | 否 | Tvdb Id |

#### `POST` `/Library/Series/Updated`

- **摘要**：Deprecated. Use /Library/Media/Updated
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Library.LibraryService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Library.PostUpdatedSeries` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `TvdbId` | `path` | `string` | 否 | 否 | Tvdb Id |

### LibraryStructureService（11 条）

#### `DELETE` `/Library/VirtualFolders`

- **鉴权**：需要认证；角色要求: `Admin`；允许在初始化向导完成前调用
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Library.LibraryStructureService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Library.RemoveVirtualFolder` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `query` | `string` | 否 | 否 | - |

#### `GET` `/Library/VirtualFolders` `[隐藏接口]`

- **鉴权**：需要认证；允许在初始化向导完成前调用
- **返回类型**：`List<VirtualFolderInfo>`
- **处理服务**：`Emby.Api.Library.LibraryStructureService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Library.GetVirtualFolders` (`Emby.Api.dll`)

_无请求参数。_

#### `POST` `/Library/VirtualFolders`

- **鉴权**：需要认证；角色要求: `Admin`；允许在初始化向导完成前调用
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Library.LibraryStructureService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Library.AddVirtualFolder` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `CollectionType` | `query` | `string` | 否 | 否 | - |
| `LibraryOptions` | `query` | `LibraryOptions` | 否 | 否 | - |
| `Name` | `query` | `string` | 否 | 否 | - |
| `Paths` | `query` | `string[]` | 否 | 否 | - |
| `RefreshLibrary` | `query` | `bool` | 否 | 否 | - |

#### `POST` `/Library/VirtualFolders/Delete`

- **鉴权**：需要认证；角色要求: `Admin`；允许在初始化向导完成前调用
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Library.LibraryStructureService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Library.RemoveVirtualFolder` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `query` | `string` | 否 | 否 | - |

#### `POST` `/Library/VirtualFolders/LibraryOptions`

- **鉴权**：需要认证；角色要求: `Admin`；允许在初始化向导完成前调用
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Library.LibraryStructureService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Library.UpdateLibraryOptions` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `query` | `string` | 否 | 否 | - |
| `LibraryOptions` | `query` | `LibraryOptions` | 否 | 否 | - |

#### `POST` `/Library/VirtualFolders/Name`

- **鉴权**：需要认证；角色要求: `Admin`；允许在初始化向导完成前调用
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Library.LibraryStructureService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Library.RenameVirtualFolder` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `query` | `string` | 否 | 否 | - |
| `NewName` | `query` | `string` | 否 | 否 | - |

#### `DELETE` `/Library/VirtualFolders/Paths`

- **鉴权**：需要认证；角色要求: `Admin`；允许在初始化向导完成前调用
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Library.LibraryStructureService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Library.RemoveMediaPath` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `query` | `string` | 否 | 否 | - |
| `Path` | `query` | `string` | 否 | 否 | - |
| `RefreshLibrary` | `query` | `bool` | 否 | 否 | - |

#### `POST` `/Library/VirtualFolders/Paths`

- **鉴权**：需要认证；角色要求: `Admin`；允许在初始化向导完成前调用
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Library.LibraryStructureService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Library.AddMediaPath` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `query` | `string` | 否 | 否 | - |
| `Path` | `query` | `string` | 否 | 否 | - |
| `PathInfo` | `query` | `MediaPathInfo` | 否 | 否 | - |
| `RefreshLibrary` | `query` | `bool` | 否 | 否 | - |

#### `POST` `/Library/VirtualFolders/Paths/Delete`

- **鉴权**：需要认证；角色要求: `Admin`；允许在初始化向导完成前调用
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Library.LibraryStructureService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Library.RemoveMediaPath` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `query` | `string` | 否 | 否 | - |
| `Path` | `query` | `string` | 否 | 否 | - |
| `RefreshLibrary` | `query` | `bool` | 否 | 否 | - |

#### `POST` `/Library/VirtualFolders/Paths/Update`

- **鉴权**：需要认证；角色要求: `Admin`；允许在初始化向导完成前调用
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Library.LibraryStructureService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Library.UpdateMediaPath` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `query` | `string` | 否 | 否 | - |
| `PathInfo` | `query` | `MediaPathInfo` | 否 | 否 | - |

#### `GET` `/Library/VirtualFolders/Query`

- **鉴权**：需要认证；允许在初始化向导完成前调用
- **返回类型**：`QueryResult<VirtualFolderInfo>`
- **处理服务**：`Emby.Api.Library.LibraryStructureService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Library.GetVirtualFoldersResult` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Ids` | `query` | `string` | 否 | 否 | - |
| `Limit` | `query` | `int?` | 否 | 否 | Optional. The maximum number of records to return |
| `StartIndex` | `query` | `int` | 否 | 否 | Optional. The record index to start at. All items with a lower index will be dropped from the results. |
| `UserId` | `query` | `string` | 否 | 否 | - |
