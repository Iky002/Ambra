# 媒体条目（Items）、搜索与刮削提供商

> 所属模块：**03. 媒体库、条目与元数据**  
> 路由前缀：`/Items`, `/Providers`, `/Search`  
> 接口总数：**74** 条

涵盖媒体条目通用查询、元数据刷新、远端搜索、字幕/图片/相似推荐、全局搜索与提供商信息。

[← 返回总目录](../README.md)

## 接口总览

| # | 方法 | 路径 | 鉴权 | 返回类型 | 摘要 |
| :---: | :--- | :--- | :--- | :--- | :--- |
| 1 | `DELETE` | `/Items` | 需认证 | `void` | Deletes an item from the library and file system |
| 2 | `GET` | `/Items` | 需认证 | `QueryResult<BaseItemDto>` | Gets items based on a query. |
| 3 | `POST` | `/Items/Access` | 需认证 | `void` | Updates user item access |
| 4 | `GET` | `/Items/Counts` | 需认证 | `ItemCounts` | - |
| 5 | `POST` | `/Items/Delete` | 需认证 | `void` | Deletes an item from the library and file system |
| 6 | `GET` | `/Items/File` *(隐藏)* | 需认证 | `object` | Gets the original file of an item |
| 7 | `GET` | `/Items/Intros` | 认证 (`Admin`) | `List<IntroDebugInfo>` | Gets info to debug intros |
| 8 | `POST` | `/Items/Metadata/Reset` | 认证 (`Admin`) | `void` | Resets metadata for one or more items |
| 9 | `GET` | `/Items/Prefixes` | 需认证 | `NameValuePair[]` | Gets items based on a query. |
| 10 | `POST` | `/Items/RemoteSearch/Apply/{Id}` | 认证 (`Admin`) | `void` | Applies search criteria to an item and refreshes metadata |
| 11 | `POST` | `/Items/RemoteSearch/Book` | 需认证 | `List<RemoteSearchResult>` | - |
| 12 | `POST` | `/Items/RemoteSearch/BoxSet` | 需认证 | `List<RemoteSearchResult>` | - |
| 13 | `POST` | `/Items/RemoteSearch/Game` | 需认证 | `List<RemoteSearchResult>` | - |
| 14 | `GET` | `/Items/RemoteSearch/Image` | 认证 (`Admin`) | `object` | Gets a remote image |
| 15 | `POST` | `/Items/RemoteSearch/Movie` | 需认证 | `List<RemoteSearchResult>` | - |
| 16 | `POST` | `/Items/RemoteSearch/MusicAlbum` | 需认证 | `List<RemoteSearchResult>` | - |
| 17 | `POST` | `/Items/RemoteSearch/MusicArtist` | 需认证 | `List<RemoteSearchResult>` | - |
| 18 | `POST` | `/Items/RemoteSearch/MusicVideo` | 需认证 | `List<RemoteSearchResult>` | - |
| 19 | `POST` | `/Items/RemoteSearch/Person` | 认证 (`Admin`) | `List<RemoteSearchResult>` | - |
| 20 | `POST` | `/Items/RemoteSearch/Series` | 需认证 | `List<RemoteSearchResult>` | - |
| 21 | `POST` | `/Items/RemoteSearch/Trailer` | 需认证 | `List<RemoteSearchResult>` | - |
| 22 | `POST` | `/Items/Shared/Leave` | 需认证 | `void` | Leaves a shared item |
| 23 | `DELETE` | `/Items/{Id}` | 需认证 | `void` | Deletes an item from the library and file system |
| 24 | `GET` | `/Items/{Id}/Ancestors` | 需认证 | `BaseItemDto[]` | Gets all parents of an item |
| 25 | `GET` | `/Items/{Id}/CriticReviews` | 需认证 | `QueryResult<BaseItemDto>` | Gets critic reviews for an item |
| 26 | `POST` | `/Items/{Id}/Delete` | 需认证 | `void` | Deletes an item from the library and file system |
| 27 | `GET` | `/Items/{Id}/DeleteInfo` | 需认证 | `DeleteInfo` | Gets delete info for an item |
| 28 | `GET` | `/Items/{Id}/Download` | 认证 (`download`) | `object` | Downloads item media |
| 29 | `GET` | `/Items/{Id}/ExternalIdInfos` | 认证 (`Admin`) | `List<ExternalIdInfo>` | Gets external id infos for an item |
| 30 | `GET` | `/Items/{Id}/File` | 公开 | `object` | Gets the original file of an item |
| 31 | `GET` | `/Items/{Id}/Images` | 需认证 | `List<ImageInfo>` | Gets information about an item's images |
| 32 | `DELETE` | `/Items/{Id}/Images/{Type}` | 认证 (`admin`) | `void` | - |
| 33 | `GET` | `/Items/{Id}/Images/{Type}` | 需认证 | `object` | - |
| 34 | `HEAD` | `/Items/{Id}/Images/{Type}` | 需认证 | `object` | - |
| 35 | `POST` | `/Items/{Id}/Images/{Type}` | 认证 (`admin`) | `void` | Uploads an image for an item, must be base64 encoded. |
| 36 | `POST` | `/Items/{Id}/Images/{Type}/Delete` | 认证 (`admin`) | `void` | - |
| 37 | `DELETE` | `/Items/{Id}/Images/{Type}/{Index}` | 认证 (`admin`) | `void` | - |
| 38 | `GET` | `/Items/{Id}/Images/{Type}/{Index}` | 需认证 | `object` | - |
| 39 | `HEAD` | `/Items/{Id}/Images/{Type}/{Index}` | 需认证 | `object` | - |
| 40 | `POST` | `/Items/{Id}/Images/{Type}/{Index}` | 认证 (`admin`) | `void` | Uploads an image for an item, must be base64 encoded. |
| 41 | `POST` | `/Items/{Id}/Images/{Type}/{Index}/Delete` | 认证 (`admin`) | `void` | - |
| 42 | `POST` | `/Items/{Id}/Images/{Type}/{Index}/Index` | 认证 (`admin`) | `void` | Updates the index for an item image |
| 43 | `POST` | `/Items/{Id}/Images/{Type}/{Index}/Url` | 认证 (`admin`) | `void` | Updates the index for an item image |
| 44 | `GET` | `/Items/{Id}/Images/{Type}/{Index}/{Tag}/{Format}/{MaxWidth}/{MaxHeight}/{PercentPlayed}/{UnPlayedCount}` | 需认证 | `object` | - |
| 45 | `HEAD` | `/Items/{Id}/Images/{Type}/{Index}/{Tag}/{Format}/{MaxWidth}/{MaxHeight}/{PercentPlayed}/{UnPlayedCount}` | 需认证 | `object` | - |
| 46 | `GET` | `/Items/{Id}/InstantMix` | 需认证 | `QueryResult<BaseItemDto>` | Creates an instant playlist based on a given item |
| 47 | `POST` | `/Items/{Id}/MakePrivate` | 需认证 | `void` | Makes an item private |
| 48 | `POST` | `/Items/{Id}/MakePublic` | 需认证 | `void` | Makes an item public to all users |
| 49 | `GET` | `/Items/{Id}/PlaybackInfo` | 需认证 | `PlaybackInfoResponse` | Gets live playback media info for an item |
| 50 | `POST` | `/Items/{Id}/PlaybackInfo` | 需认证 | `PlaybackInfoResponse` | Gets live playback media info for an item |
| 51 | `POST` | `/Items/{Id}/Refresh` | 需认证 | `void` | Refreshes metadata for an item |
| 52 | `GET` | `/Items/{Id}/RemoteImages` | 需认证 | `RemoteImageResult` | Gets available remote images for an item |
| 53 | `POST` | `/Items/{Id}/RemoteImages/Download` | 认证 (`Admin`) | `void` | Downloads a remote image for an item |
| 54 | `GET` | `/Items/{Id}/RemoteImages/Providers` | 需认证 | `List<ImageProviderInfo>` | Gets available remote image providers for an item |
| 55 | `GET` | `/Items/{Id}/RemoteSearch/Subtitles/{Language}` | 需认证 | `RemoteSubtitleInfo[]` | - |
| 56 | `POST` | `/Items/{Id}/RemoteSearch/Subtitles/{SubtitleId}` | 认证 (`DownloadSubtitles`) | `SubtitleDownloadResult` | - |
| 57 | `GET` | `/Items/{Id}/Similar` | 需认证 | `QueryResult<BaseItemDto>` | Gets similar items |
| 58 | `DELETE` | `/Items/{Id}/Subtitles/{Index}` | 认证 (`ManageSubtitles`) | `void` | Deletes an external subtitle file |
| 59 | `POST` | `/Items/{Id}/Subtitles/{Index}/Delete` | 认证 (`ManageSubtitles`) | `void` | Deletes an external subtitle file |
| 60 | `POST` | `/Items/{Id}/Tags/Add` | 需认证 | `void` | Adds new tags to an item |
| 61 | `POST` | `/Items/{Id}/Tags/Delete` | 需认证 | `void` | Removes tags from an item |
| 62 | `GET` | `/Items/{Id}/ThemeMedia` | 需认证 | `AllThemeMediaResult` | Gets theme videos and songs for an item |
| 63 | `GET` | `/Items/{Id}/ThemeSongs` | 需认证 | `ThemeMediaResult` | Gets theme songs for an item |
| 64 | `GET` | `/Items/{Id}/ThemeVideos` | 需认证 | `ThemeMediaResult` | Gets theme videos for an item |
| 65 | `GET` | `/Items/{Id}/thumbnails` *(隐藏)* | 需认证 | `ThumbnailInfo[]` | - |
| 66 | `GET` | `/Items/{Id}/ThumbnailSet` | 需认证 | `ThumbnailSetInfo` | - |
| 67 | `GET` | `/Items/{Id}/{MediaSourceId}/Subtitles/{Index}/Stream.{Format}` | 公开 | `object` | Gets subtitles in a specified format. |
| 68 | `HEAD` | `/Items/{Id}/{MediaSourceId}/Subtitles/{Index}/Stream.{Format}` | 公开 | `object` | Gets subtitles in a specified format. |
| 69 | `GET` | `/Items/{Id}/{MediaSourceId}/Subtitles/{Index}/{StartPositionTicks}/Stream.{Format}` | 公开 | `object` | Gets subtitles in a specified format. |
| 70 | `HEAD` | `/Items/{Id}/{MediaSourceId}/Subtitles/{Index}/{StartPositionTicks}/Stream.{Format}` | 公开 | `object` | Gets subtitles in a specified format. |
| 71 | `POST` | `/Items/{ItemId}` | 需认证 | `void` | Updates an item |
| 72 | `GET` | `/Items/{ItemId}/MetadataEditor` | 需认证 | `MetadataEditorInfo` | Gets metadata editor info for an item |
| 73 | `GET` | `/Providers/Subtitles/Subtitles/{Id}` | 需认证 | `void` | - |
| 74 | `GET` | `/Search/Hints` *(隐藏)* | 需认证 | `void` | Gets search hints based on a search term |

## 接口详情

### BifService（2 条）

#### `GET` `/Items/{Id}/thumbnails` `[隐藏接口]`

- **鉴权**：需要认证
- **返回类型**：`ThumbnailInfo[]`
- **处理服务**：`RokuMetadata.Api.BifService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`RokuMetadata.Api.GetThumbnailLegacys` (`Emby.Server.MediaEncoding.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `MediaSourceId` | `path` | `string` | 是 | 否 | The media version id, if playing an alternate version |
| `Width` | `query` | `int` | 是 | 否 | - |

#### `GET` `/Items/{Id}/ThumbnailSet`

- **鉴权**：需要认证
- **返回类型**：`ThumbnailSetInfo`
- **处理服务**：`RokuMetadata.Api.BifService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`RokuMetadata.Api.GetThumbnails` (`Emby.Server.MediaEncoding.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `MediaSourceId` | `path` | `string` | 是 | 否 | The media version id, if playing an alternate version |
| `Width` | `query` | `int` | 是 | 否 | - |

### ImageService（15 条）

#### `GET` `/Items/{Id}/Images`

- **摘要**：Gets information about an item's images
- **鉴权**：需要认证
- **返回类型**：`List<ImageInfo>`
- **处理服务**：`Emby.Api.Images.ImageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Images.GetItemImageInfos` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |

#### `DELETE` `/Items/{Id}/Images/{Type}`

- **鉴权**：需要认证；角色要求: `admin`
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Images.ImageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Images.DeleteItemImage` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `Type` | `path` | `ImageType` | 是 | 否 | Image Type |
| `Index` | `query` | `int` | 否 | 否 | Image Index |

#### `GET` `/Items/{Id}/Images/{Type}`

- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Api.Images.ImageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Images.GetItemImage` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `Type` | `path` | `ImageType` | 是 | 否 | Image Type |
| `AutoOrient` | `query` | `bool` | 否 | 否 | Set to true to force normalization of orientation in the event the renderer does not support it. |
| `BackgroundColor` | `query` | `string` | 否 | 否 | Optional. Apply a background color for transparent images. |
| `Blur` | `query` | `int` | 否 | 否 | - |
| `CropWhitespace` | `query` | `bool?` | 否 | 否 | Specify if whitespace should be cropped out of the image. True/False. If unspecified, whitespace will be cropped from logos and clear art. |
| `EnableImageEnhancers` | `query` | `bool` | 否 | 否 | Enable or disable image enhancers such as cover art. |
| `ForegroundLayer` | `query` | `string` | 否 | 否 | Optional. Apply a foreground layer on top of the image. |
| `Format` | `query` | `string` | 否 | 否 | Determines the output foramt of the image - original,gif,jpg,png |
| `Height` | `query` | `int` | 否 | 否 | The fixed image height to return. |
| `Index` | `query` | `int` | 否 | 否 | Image Index |
| `KeepAnimation` | `query` | `bool` | 否 | 否 | Set to true to retain image animation (when supported). |
| `MaxHeight` | `query` | `int` | 否 | 否 | The maximum image height to return. |
| `MaxWidth` | `query` | `int` | 否 | 否 | The maximum image width to return. |
| `MediaSourceId` | `query` | `string` | 否 | 否 | - |
| `PercentPlayed` | `query` | `int?` | 否 | 否 | - |
| `PositionTicks` | `query` | `long` | 否 | 否 | - |
| `Quality` | `query` | `int` | 否 | 否 | Optional quality setting, from 0-100. Defaults to 90 and should suffice in most cases. |
| `Tag` | `query` | `string` | 否 | 否 | Optional. Supply the cache tag from the item object to receive strong caching headers. |
| `UnPlayedCount` | `query` | `int?` | 否 | 否 | - |
| `Width` | `query` | `int` | 否 | 否 | The fixed image width to return. |

#### `HEAD` `/Items/{Id}/Images/{Type}`

- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Api.Images.ImageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Images.GetItemImage` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `Type` | `path` | `ImageType` | 是 | 否 | Image Type |
| `AutoOrient` | `query` | `bool` | 否 | 否 | Set to true to force normalization of orientation in the event the renderer does not support it. |
| `BackgroundColor` | `query` | `string` | 否 | 否 | Optional. Apply a background color for transparent images. |
| `Blur` | `query` | `int` | 否 | 否 | - |
| `CropWhitespace` | `query` | `bool?` | 否 | 否 | Specify if whitespace should be cropped out of the image. True/False. If unspecified, whitespace will be cropped from logos and clear art. |
| `EnableImageEnhancers` | `query` | `bool` | 否 | 否 | Enable or disable image enhancers such as cover art. |
| `ForegroundLayer` | `query` | `string` | 否 | 否 | Optional. Apply a foreground layer on top of the image. |
| `Format` | `query` | `string` | 否 | 否 | Determines the output foramt of the image - original,gif,jpg,png |
| `Height` | `query` | `int` | 否 | 否 | The fixed image height to return. |
| `Index` | `query` | `int` | 否 | 否 | Image Index |
| `KeepAnimation` | `query` | `bool` | 否 | 否 | Set to true to retain image animation (when supported). |
| `MaxHeight` | `query` | `int` | 否 | 否 | The maximum image height to return. |
| `MaxWidth` | `query` | `int` | 否 | 否 | The maximum image width to return. |
| `MediaSourceId` | `query` | `string` | 否 | 否 | - |
| `PercentPlayed` | `query` | `int?` | 否 | 否 | - |
| `PositionTicks` | `query` | `long` | 否 | 否 | - |
| `Quality` | `query` | `int` | 否 | 否 | Optional quality setting, from 0-100. Defaults to 90 and should suffice in most cases. |
| `Tag` | `query` | `string` | 否 | 否 | Optional. Supply the cache tag from the item object to receive strong caching headers. |
| `UnPlayedCount` | `query` | `int?` | 否 | 否 | - |
| `Width` | `query` | `int` | 否 | 否 | The fixed image width to return. |

#### `POST` `/Items/{Id}/Images/{Type}`

- **摘要**：Uploads an image for an item, must be base64 encoded.
- **鉴权**：需要认证；角色要求: `admin`
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Images.ImageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Images.PostItemImage` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `Type` | `path` | `ImageType` | 是 | 否 | Image Type |
| `Index` | `query` | `int?` | 否 | 否 | Image Index |
| `RequestStream` | `query` | `Stream` | 否 | 否 | - |

#### `POST` `/Items/{Id}/Images/{Type}/Delete`

- **鉴权**：需要认证；角色要求: `admin`
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Images.ImageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Images.DeleteItemImage` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `Type` | `path` | `ImageType` | 是 | 否 | Image Type |
| `Index` | `query` | `int` | 否 | 否 | Image Index |

#### `DELETE` `/Items/{Id}/Images/{Type}/{Index}`

- **鉴权**：需要认证；角色要求: `admin`
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Images.ImageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Images.DeleteItemImage` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `Type` | `path` | `ImageType` | 是 | 否 | Image Type |
| `Index` | `query` | `int` | 否 | 否 | Image Index |

#### `GET` `/Items/{Id}/Images/{Type}/{Index}`

- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Api.Images.ImageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Images.GetItemImage` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `Type` | `path` | `ImageType` | 是 | 否 | Image Type |
| `AutoOrient` | `query` | `bool` | 否 | 否 | Set to true to force normalization of orientation in the event the renderer does not support it. |
| `BackgroundColor` | `query` | `string` | 否 | 否 | Optional. Apply a background color for transparent images. |
| `Blur` | `query` | `int` | 否 | 否 | - |
| `CropWhitespace` | `query` | `bool?` | 否 | 否 | Specify if whitespace should be cropped out of the image. True/False. If unspecified, whitespace will be cropped from logos and clear art. |
| `EnableImageEnhancers` | `query` | `bool` | 否 | 否 | Enable or disable image enhancers such as cover art. |
| `ForegroundLayer` | `query` | `string` | 否 | 否 | Optional. Apply a foreground layer on top of the image. |
| `Format` | `query` | `string` | 否 | 否 | Determines the output foramt of the image - original,gif,jpg,png |
| `Height` | `query` | `int` | 否 | 否 | The fixed image height to return. |
| `Index` | `query` | `int` | 否 | 否 | Image Index |
| `KeepAnimation` | `query` | `bool` | 否 | 否 | Set to true to retain image animation (when supported). |
| `MaxHeight` | `query` | `int` | 否 | 否 | The maximum image height to return. |
| `MaxWidth` | `query` | `int` | 否 | 否 | The maximum image width to return. |
| `MediaSourceId` | `query` | `string` | 否 | 否 | - |
| `PercentPlayed` | `query` | `int?` | 否 | 否 | - |
| `PositionTicks` | `query` | `long` | 否 | 否 | - |
| `Quality` | `query` | `int` | 否 | 否 | Optional quality setting, from 0-100. Defaults to 90 and should suffice in most cases. |
| `Tag` | `query` | `string` | 否 | 否 | Optional. Supply the cache tag from the item object to receive strong caching headers. |
| `UnPlayedCount` | `query` | `int?` | 否 | 否 | - |
| `Width` | `query` | `int` | 否 | 否 | The fixed image width to return. |

#### `HEAD` `/Items/{Id}/Images/{Type}/{Index}`

- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Api.Images.ImageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Images.GetItemImage` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `Type` | `path` | `ImageType` | 是 | 否 | Image Type |
| `AutoOrient` | `query` | `bool` | 否 | 否 | Set to true to force normalization of orientation in the event the renderer does not support it. |
| `BackgroundColor` | `query` | `string` | 否 | 否 | Optional. Apply a background color for transparent images. |
| `Blur` | `query` | `int` | 否 | 否 | - |
| `CropWhitespace` | `query` | `bool?` | 否 | 否 | Specify if whitespace should be cropped out of the image. True/False. If unspecified, whitespace will be cropped from logos and clear art. |
| `EnableImageEnhancers` | `query` | `bool` | 否 | 否 | Enable or disable image enhancers such as cover art. |
| `ForegroundLayer` | `query` | `string` | 否 | 否 | Optional. Apply a foreground layer on top of the image. |
| `Format` | `query` | `string` | 否 | 否 | Determines the output foramt of the image - original,gif,jpg,png |
| `Height` | `query` | `int` | 否 | 否 | The fixed image height to return. |
| `Index` | `query` | `int` | 否 | 否 | Image Index |
| `KeepAnimation` | `query` | `bool` | 否 | 否 | Set to true to retain image animation (when supported). |
| `MaxHeight` | `query` | `int` | 否 | 否 | The maximum image height to return. |
| `MaxWidth` | `query` | `int` | 否 | 否 | The maximum image width to return. |
| `MediaSourceId` | `query` | `string` | 否 | 否 | - |
| `PercentPlayed` | `query` | `int?` | 否 | 否 | - |
| `PositionTicks` | `query` | `long` | 否 | 否 | - |
| `Quality` | `query` | `int` | 否 | 否 | Optional quality setting, from 0-100. Defaults to 90 and should suffice in most cases. |
| `Tag` | `query` | `string` | 否 | 否 | Optional. Supply the cache tag from the item object to receive strong caching headers. |
| `UnPlayedCount` | `query` | `int?` | 否 | 否 | - |
| `Width` | `query` | `int` | 否 | 否 | The fixed image width to return. |

#### `POST` `/Items/{Id}/Images/{Type}/{Index}`

- **摘要**：Uploads an image for an item, must be base64 encoded.
- **鉴权**：需要认证；角色要求: `admin`
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Images.ImageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Images.PostItemImage` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `Type` | `path` | `ImageType` | 是 | 否 | Image Type |
| `Index` | `query` | `int?` | 否 | 否 | Image Index |
| `RequestStream` | `query` | `Stream` | 否 | 否 | - |

#### `POST` `/Items/{Id}/Images/{Type}/{Index}/Delete`

- **鉴权**：需要认证；角色要求: `admin`
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Images.ImageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Images.DeleteItemImage` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `Type` | `path` | `ImageType` | 是 | 否 | Image Type |
| `Index` | `query` | `int` | 否 | 否 | Image Index |

#### `POST` `/Items/{Id}/Images/{Type}/{Index}/Index`

- **摘要**：Updates the index for an item image
- **鉴权**：需要认证；角色要求: `admin`
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Images.ImageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Images.UpdateItemImageIndex` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `Type` | `path` | `ImageType` | 是 | 否 | Image Type |
| `NewIndex` | `query` | `int` | 是 | 否 | The new image index |
| `Index` | `query` | `int` | 否 | 否 | Image Index |

#### `POST` `/Items/{Id}/Images/{Type}/{Index}/Url`

- **摘要**：Updates the index for an item image
- **鉴权**：需要认证；角色要求: `admin`
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Images.ImageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Images.UpdateItemImageFromUrl` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `Type` | `path` | `ImageType` | 是 | 否 | Image Type |
| `Url` | `query` | `string` | 是 | 否 | The url for the new image |
| `Index` | `query` | `int` | 否 | 否 | Image Index |

#### `GET` `/Items/{Id}/Images/{Type}/{Index}/{Tag}/{Format}/{MaxWidth}/{MaxHeight}/{PercentPlayed}/{UnPlayedCount}`

- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Api.Images.ImageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Images.GetItemImage` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `Type` | `path` | `ImageType` | 是 | 否 | Image Type |
| `PercentPlayed` | `path` | `int?` | 否 | 否 | - |
| `UnPlayedCount` | `path` | `int?` | 否 | 否 | - |
| `AutoOrient` | `query` | `bool` | 否 | 否 | Set to true to force normalization of orientation in the event the renderer does not support it. |
| `BackgroundColor` | `query` | `string` | 否 | 否 | Optional. Apply a background color for transparent images. |
| `Blur` | `query` | `int` | 否 | 否 | - |
| `CropWhitespace` | `query` | `bool?` | 否 | 否 | Specify if whitespace should be cropped out of the image. True/False. If unspecified, whitespace will be cropped from logos and clear art. |
| `EnableImageEnhancers` | `query` | `bool` | 否 | 否 | Enable or disable image enhancers such as cover art. |
| `ForegroundLayer` | `query` | `string` | 否 | 否 | Optional. Apply a foreground layer on top of the image. |
| `Format` | `query` | `string` | 否 | 否 | Determines the output foramt of the image - original,gif,jpg,png |
| `Height` | `query` | `int` | 否 | 否 | The fixed image height to return. |
| `Index` | `query` | `int` | 否 | 否 | Image Index |
| `KeepAnimation` | `query` | `bool` | 否 | 否 | Set to true to retain image animation (when supported). |
| `MaxHeight` | `query` | `int` | 否 | 否 | The maximum image height to return. |
| `MaxWidth` | `query` | `int` | 否 | 否 | The maximum image width to return. |
| `MediaSourceId` | `query` | `string` | 否 | 否 | - |
| `PositionTicks` | `query` | `long` | 否 | 否 | - |
| `Quality` | `query` | `int` | 否 | 否 | Optional quality setting, from 0-100. Defaults to 90 and should suffice in most cases. |
| `Tag` | `query` | `string` | 否 | 否 | Optional. Supply the cache tag from the item object to receive strong caching headers. |
| `Width` | `query` | `int` | 否 | 否 | The fixed image width to return. |

#### `HEAD` `/Items/{Id}/Images/{Type}/{Index}/{Tag}/{Format}/{MaxWidth}/{MaxHeight}/{PercentPlayed}/{UnPlayedCount}`

- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Api.Images.ImageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Images.GetItemImage` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `Type` | `path` | `ImageType` | 是 | 否 | Image Type |
| `PercentPlayed` | `path` | `int?` | 否 | 否 | - |
| `UnPlayedCount` | `path` | `int?` | 否 | 否 | - |
| `AutoOrient` | `query` | `bool` | 否 | 否 | Set to true to force normalization of orientation in the event the renderer does not support it. |
| `BackgroundColor` | `query` | `string` | 否 | 否 | Optional. Apply a background color for transparent images. |
| `Blur` | `query` | `int` | 否 | 否 | - |
| `CropWhitespace` | `query` | `bool?` | 否 | 否 | Specify if whitespace should be cropped out of the image. True/False. If unspecified, whitespace will be cropped from logos and clear art. |
| `EnableImageEnhancers` | `query` | `bool` | 否 | 否 | Enable or disable image enhancers such as cover art. |
| `ForegroundLayer` | `query` | `string` | 否 | 否 | Optional. Apply a foreground layer on top of the image. |
| `Format` | `query` | `string` | 否 | 否 | Determines the output foramt of the image - original,gif,jpg,png |
| `Height` | `query` | `int` | 否 | 否 | The fixed image height to return. |
| `Index` | `query` | `int` | 否 | 否 | Image Index |
| `KeepAnimation` | `query` | `bool` | 否 | 否 | Set to true to retain image animation (when supported). |
| `MaxHeight` | `query` | `int` | 否 | 否 | The maximum image height to return. |
| `MaxWidth` | `query` | `int` | 否 | 否 | The maximum image width to return. |
| `MediaSourceId` | `query` | `string` | 否 | 否 | - |
| `PositionTicks` | `query` | `long` | 否 | 否 | - |
| `Quality` | `query` | `int` | 否 | 否 | Optional quality setting, from 0-100. Defaults to 90 and should suffice in most cases. |
| `Tag` | `query` | `string` | 否 | 否 | Optional. Supply the cache tag from the item object to receive strong caching headers. |
| `Width` | `query` | `int` | 否 | 否 | The fixed image width to return. |

### InstantMixService（1 条）

#### `GET` `/Items/{Id}/InstantMix`

- **摘要**：Creates an instant playlist based on a given item
- **鉴权**：需要认证
- **返回类型**：`QueryResult<BaseItemDto>`
- **处理服务**：`Emby.Api.Music.InstantMixService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Music.GetInstantMixFromItem` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
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

### ItemLookupService（14 条）

#### `POST` `/Items/Metadata/Reset`

- **摘要**：Resets metadata for one or more items
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`void`
- **处理服务**：`Emby.Api.ItemLookupService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.ResetMetadata` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `ItemIds` | `query` | `string` | 是 | 否 | The item ids |

#### `POST` `/Items/RemoteSearch/Apply/{Id}`

- **摘要**：Applies search criteria to an item and refreshes metadata
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`void`
- **处理服务**：`Emby.Api.ItemLookupService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.ApplySearchCriteria` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `query` | `string` | 是 | 否 | The item id |
| `AlbumArtist` | `query` | `RemoteSearchResult` | 否 | 否 | - |
| `Artists` | `query` | `RemoteSearchResult[]` | 否 | 否 | - |
| `DisambiguationComment` | `query` | `string` | 否 | 否 | - |
| `EndDate` | `query` | `DateTimeOffset?` | 否 | 否 | - |
| `GameSystem` | `query` | `string` | 否 | 否 | - |
| `ImageUrl` | `query` | `string` | 否 | 否 | - |
| `IndexNumber` | `query` | `int?` | 否 | 否 | - |
| `IndexNumberEnd` | `query` | `int?` | 否 | 否 | - |
| `Name` | `query` | `string` | 否 | 否 | - |
| `OriginalTitle` | `query` | `string` | 否 | 否 | - |
| `Overview` | `query` | `string` | 否 | 否 | - |
| `ParentIndexNumber` | `query` | `int?` | 否 | 否 | - |
| `PersonType` | `query` | `PersonType?` | 否 | 否 | - |
| `PremiereDate` | `query` | `DateTimeOffset?` | 否 | 否 | - |
| `ProductionYear` | `query` | `int?` | 否 | 否 | - |
| `ProviderIds` | `query` | `ProviderIdDictionary` | 否 | 否 | - |
| `ReplaceAllImages` | `query` | `bool` | 否 | 否 | Whether or not to replace all images |
| `Role` | `query` | `string` | 否 | 否 | - |
| `SearchProviderName` | `query` | `string` | 否 | 否 | - |
| `SortIndexNumber` | `query` | `int?` | 否 | 否 | - |
| `SortParentIndexNumber` | `query` | `int?` | 否 | 否 | - |
| `StartDate` | `query` | `DateTimeOffset?` | 否 | 否 | - |
| `ThumbnailUrl` | `query` | `string` | 否 | 否 | - |
| `Type` | `query` | `string` | 否 | 否 | - |

#### `POST` `/Items/RemoteSearch/Book`

- **鉴权**：需要认证
- **返回类型**：`List<RemoteSearchResult>`
- **处理服务**：`Emby.Api.ItemLookupService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetBookRemoteSearchResults` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `IncludeDisabledProviders` | `query` | `bool` | 否 | 否 | - |
| `ItemId` | `query` | `long` | 否 | 否 | - |
| `Providers` | `query` | `string[]` | 否 | 否 | - |
| `SearchInfo` | `query` | `BookInfo` | 否 | 否 | - |
| `SearchProviderName` | `query` | `string` | 否 | 否 | - |

#### `POST` `/Items/RemoteSearch/BoxSet`

- **鉴权**：需要认证
- **返回类型**：`List<RemoteSearchResult>`
- **处理服务**：`Emby.Api.ItemLookupService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetCollectionRemoteSearchResults` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `IncludeDisabledProviders` | `query` | `bool` | 否 | 否 | - |
| `ItemId` | `query` | `long` | 否 | 否 | - |
| `Providers` | `query` | `string[]` | 否 | 否 | - |
| `SearchInfo` | `query` | `ItemLookupInfo` | 否 | 否 | - |
| `SearchProviderName` | `query` | `string` | 否 | 否 | - |

#### `POST` `/Items/RemoteSearch/Game`

- **鉴权**：需要认证
- **返回类型**：`List<RemoteSearchResult>`
- **处理服务**：`Emby.Api.ItemLookupService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetGameRemoteSearchResults` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `IncludeDisabledProviders` | `query` | `bool` | 否 | 否 | - |
| `ItemId` | `query` | `long` | 否 | 否 | - |
| `Providers` | `query` | `string[]` | 否 | 否 | - |
| `SearchInfo` | `query` | `GameInfo` | 否 | 否 | - |
| `SearchProviderName` | `query` | `string` | 否 | 否 | - |

#### `GET` `/Items/RemoteSearch/Image`

- **摘要**：Gets a remote image
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`object`
- **处理服务**：`Emby.Api.ItemLookupService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetRemoteSearchImage` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `ImageUrl` | `query` | `string` | 是 | 否 | The image url |
| `ProviderName` | `query` | `string` | 是 | 否 | - |

#### `POST` `/Items/RemoteSearch/Movie`

- **鉴权**：需要认证
- **返回类型**：`List<RemoteSearchResult>`
- **处理服务**：`Emby.Api.ItemLookupService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetMovieRemoteSearchResults` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `IncludeDisabledProviders` | `query` | `bool` | 否 | 否 | - |
| `ItemId` | `query` | `long` | 否 | 否 | - |
| `Providers` | `query` | `string[]` | 否 | 否 | - |
| `SearchInfo` | `query` | `MovieInfo` | 否 | 否 | - |
| `SearchProviderName` | `query` | `string` | 否 | 否 | - |

#### `POST` `/Items/RemoteSearch/MusicAlbum`

- **鉴权**：需要认证
- **返回类型**：`List<RemoteSearchResult>`
- **处理服务**：`Emby.Api.ItemLookupService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetMusicAlbumRemoteSearchResults` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `IncludeDisabledProviders` | `query` | `bool` | 否 | 否 | - |
| `ItemId` | `query` | `long` | 否 | 否 | - |
| `Providers` | `query` | `string[]` | 否 | 否 | - |
| `SearchInfo` | `query` | `AlbumInfo` | 否 | 否 | - |
| `SearchProviderName` | `query` | `string` | 否 | 否 | - |

#### `POST` `/Items/RemoteSearch/MusicArtist`

- **鉴权**：需要认证
- **返回类型**：`List<RemoteSearchResult>`
- **处理服务**：`Emby.Api.ItemLookupService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetMusicArtistRemoteSearchResults` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `IncludeDisabledProviders` | `query` | `bool` | 否 | 否 | - |
| `ItemId` | `query` | `long` | 否 | 否 | - |
| `Providers` | `query` | `string[]` | 否 | 否 | - |
| `SearchInfo` | `query` | `ArtistInfo` | 否 | 否 | - |
| `SearchProviderName` | `query` | `string` | 否 | 否 | - |

#### `POST` `/Items/RemoteSearch/MusicVideo`

- **鉴权**：需要认证
- **返回类型**：`List<RemoteSearchResult>`
- **处理服务**：`Emby.Api.ItemLookupService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetMusicVideoRemoteSearchResults` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `IncludeDisabledProviders` | `query` | `bool` | 否 | 否 | - |
| `ItemId` | `query` | `long` | 否 | 否 | - |
| `Providers` | `query` | `string[]` | 否 | 否 | - |
| `SearchInfo` | `query` | `MusicVideoInfo` | 否 | 否 | - |
| `SearchProviderName` | `query` | `string` | 否 | 否 | - |

#### `POST` `/Items/RemoteSearch/Person`

- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`List<RemoteSearchResult>`
- **处理服务**：`Emby.Api.ItemLookupService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetPersonRemoteSearchResults` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `IncludeDisabledProviders` | `query` | `bool` | 否 | 否 | - |
| `ItemId` | `query` | `long` | 否 | 否 | - |
| `Providers` | `query` | `string[]` | 否 | 否 | - |
| `SearchInfo` | `query` | `PersonLookupInfo` | 否 | 否 | - |
| `SearchProviderName` | `query` | `string` | 否 | 否 | - |

#### `POST` `/Items/RemoteSearch/Series`

- **鉴权**：需要认证
- **返回类型**：`List<RemoteSearchResult>`
- **处理服务**：`Emby.Api.ItemLookupService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetSeriesRemoteSearchResults` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `IncludeDisabledProviders` | `query` | `bool` | 否 | 否 | - |
| `ItemId` | `query` | `long` | 否 | 否 | - |
| `Providers` | `query` | `string[]` | 否 | 否 | - |
| `SearchInfo` | `query` | `SeriesInfo` | 否 | 否 | - |
| `SearchProviderName` | `query` | `string` | 否 | 否 | - |

#### `POST` `/Items/RemoteSearch/Trailer`

- **鉴权**：需要认证
- **返回类型**：`List<RemoteSearchResult>`
- **处理服务**：`Emby.Api.ItemLookupService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetTrailerRemoteSearchResults` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `IncludeDisabledProviders` | `query` | `bool` | 否 | 否 | - |
| `ItemId` | `query` | `long` | 否 | 否 | - |
| `Providers` | `query` | `string[]` | 否 | 否 | - |
| `SearchInfo` | `query` | `TrailerInfo` | 否 | 否 | - |
| `SearchProviderName` | `query` | `string` | 否 | 否 | - |

#### `GET` `/Items/{Id}/ExternalIdInfos`

- **摘要**：Gets external id infos for an item
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`List<ExternalIdInfo>`
- **处理服务**：`Emby.Api.ItemLookupService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetExternalIdInfos` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `IsSupportedAsIdentifier` | `query` | `bool?` | 否 | 否 | - |

### ItemRefreshService（1 条）

#### `POST` `/Items/{Id}/Refresh`

- **摘要**：Refreshes metadata for an item
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.ItemRefreshService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.RefreshItem` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `ImageRefreshMode` | `query` | `MetadataRefreshMode` | 否 | 否 | Specifies the image refresh mode |
| `MetadataRefreshMode` | `query` | `MetadataRefreshMode` | 否 | 否 | Specifies the metadata refresh mode |
| `Recursive` | `query` | `bool` | 否 | 否 | Indicates if the refresh should occur recursively. |
| `ReplaceAllImages` | `query` | `bool` | 否 | 否 | Determines if images should be replaced. Only applicable if mode is FullRefresh |
| `ReplaceAllMetadata` | `query` | `bool` | 否 | 否 | Determines if metadata should be replaced. Only applicable if mode is FullRefresh |
| `ReplaceThumbnailImages` | `query` | `bool` | 否 | 否 | - |

### ItemsService（1 条）

#### `GET` `/Items`

- **摘要**：Gets items based on a query.
- **鉴权**：需要认证
- **返回类型**：`QueryResult<BaseItemDto>`
- **处理服务**：`Emby.Api.UserLibrary.ItemsService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.GetItems` (`Emby.Api.dll`)

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

### ItemUpdateService（2 条）

#### `POST` `/Items/{ItemId}`

- **摘要**：Updates an item
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.ItemUpdateService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UpdateItem` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `ItemId` | `path` | `string` | 是 | 否 | The id of the item |
| `AffiliateCallSign` | `query` | `string` | 否 | 否 | - |
| `AirDays` | `query` | `DayOfWeek[]` | 否 | 否 | - |
| `Album` | `query` | `string` | 否 | 否 | - |
| `AlbumArtist` | `query` | `string` | 否 | 否 | - |
| `AlbumArtists` | `query` | `NameIdPair[]` | 否 | 否 | - |
| `AlbumCount` | `query` | `int?` | 否 | 否 | - |
| `AlbumId` | `query` | `string` | 否 | 否 | - |
| `AlbumPrimaryImageTag` | `query` | `string` | 否 | 否 | - |
| `Altitude` | `query` | `double?` | 否 | 否 | - |
| `Aperture` | `query` | `double?` | 否 | 否 | - |
| `ArtistItems` | `query` | `NameIdPair[]` | 否 | 否 | - |
| `Artists` | `query` | `string[]` | 否 | 否 | - |
| `AsSeries` | `query` | `bool?` | 否 | 否 | - |
| `AudioCodec` | `query` | `string` | 否 | 否 | - |
| `AverageFrameRate` | `query` | `float?` | 否 | 否 | - |
| `BackdropImageTags` | `query` | `string[]` | 否 | 否 | - |
| `Bitrate` | `query` | `int?` | 否 | 否 | - |
| `CameraMake` | `query` | `string` | 否 | 否 | - |
| `CameraModel` | `query` | `string` | 否 | 否 | - |
| `CanDelete` | `query` | `bool?` | 否 | 否 | - |
| `CanDownload` | `query` | `bool?` | 否 | 否 | - |
| `CanEditItems` | `query` | `bool?` | 否 | 否 | - |
| `CanLeaveContent` | `query` | `bool?` | 否 | 否 | - |
| `CanMakePublic` | `query` | `bool?` | 否 | 否 | - |
| `CanManageAccess` | `query` | `bool?` | 否 | 否 | - |
| `ChannelId` | `query` | `string` | 否 | 否 | - |
| `ChannelName` | `query` | `string` | 否 | 否 | - |
| `ChannelNumber` | `query` | `string` | 否 | 否 | - |
| `ChannelPrimaryImageTag` | `query` | `string` | 否 | 否 | - |
| `Chapters` | `query` | `List<ChapterInfo>` | 否 | 否 | - |
| `ChildCount` | `query` | `int?` | 否 | 否 | - |
| `Collections` | `query` | `NameLongIdPair[]` | 否 | 否 | - |
| `CollectionType` | `query` | `string` | 否 | 否 | - |
| `CommunityRating` | `query` | `float?` | 否 | 否 | - |
| `CompletionPercentage` | `query` | `double?` | 否 | 否 | - |
| `Composers` | `query` | `NameIdPair[]` | 否 | 否 | - |
| `Container` | `query` | `string` | 否 | 否 | - |
| `CriticRating` | `query` | `float?` | 否 | 否 | - |
| `CurrentProgram` | `query` | `BaseItemDto` | 否 | 否 | - |
| `CustomRating` | `query` | `string` | 否 | 否 | - |
| `DateCreated` | `query` | `DateTimeOffset?` | 否 | 否 | - |
| `DateModified` | `query` | `DateTimeOffset?` | 否 | 否 | - |
| `Disabled` | `query` | `bool?` | 否 | 否 | - |
| `DisplayOrder` | `query` | `string` | 否 | 否 | - |
| `DisplayPreferencesId` | `query` | `string` | 否 | 否 | - |
| `EndDate` | `query` | `DateTimeOffset?` | 否 | 否 | - |
| `EpisodeTitle` | `query` | `string` | 否 | 否 | - |
| `Etag` | `query` | `string` | 否 | 否 | - |
| `ExposureTime` | `query` | `double?` | 否 | 否 | - |
| `ExternalUrls` | `query` | `ExternalUrl[]` | 否 | 否 | - |
| `ExtraType` | `query` | `string` | 否 | 否 | - |
| `FileName` | `query` | `string` | 否 | 否 | - |
| `FocalLength` | `query` | `double?` | 否 | 否 | - |
| `ForcedSortName` | `query` | `string` | 否 | 否 | - |
| `GameSystem` | `query` | `string` | 否 | 否 | - |
| `GameSystemId` | `query` | `long?` | 否 | 否 | - |
| `GenreItems` | `query` | `NameLongIdPair[]` | 否 | 否 | - |
| `Genres` | `query` | `string[]` | 否 | 否 | - |
| `Guid` | `query` | `string` | 否 | 否 | - |
| `Height` | `query` | `int?` | 否 | 否 | - |
| `Id` | `query` | `string` | 否 | 否 | - |
| `ImageOrientation` | `query` | `ImageOrientation?` | 否 | 否 | - |
| `ImageTags` | `query` | `Dictionary<ImageType, string>` | 否 | 否 | - |
| `IndexNumber` | `query` | `int?` | 否 | 否 | - |
| `IndexNumberEnd` | `query` | `int?` | 否 | 否 | - |
| `IsFolder` | `query` | `bool?` | 否 | 否 | - |
| `IsKids` | `query` | `bool?` | 否 | 否 | - |
| `IsLive` | `query` | `bool?` | 否 | 否 | - |
| `IsMovie` | `query` | `bool?` | 否 | 否 | - |
| `IsNew` | `query` | `bool?` | 否 | 否 | - |
| `IsNews` | `query` | `bool?` | 否 | 否 | - |
| `IsoSpeedRating` | `query` | `int?` | 否 | 否 | - |
| `IsPremiere` | `query` | `bool?` | 否 | 否 | - |
| `IsRepeat` | `query` | `bool?` | 否 | 否 | - |
| `IsSeries` | `query` | `bool?` | 否 | 否 | - |
| `IsSports` | `query` | `bool?` | 否 | 否 | - |
| `Latitude` | `query` | `double?` | 否 | 否 | - |
| `ListingsChannelId` | `query` | `string` | 否 | 否 | - |
| `ListingsChannelName` | `query` | `string` | 否 | 否 | - |
| `ListingsChannelNumber` | `query` | `string` | 否 | 否 | - |
| `ListingsId` | `query` | `string` | 否 | 否 | - |
| `ListingsPath` | `query` | `string` | 否 | 否 | - |
| `ListingsProviderId` | `query` | `string` | 否 | 否 | - |
| `LocalTrailerCount` | `query` | `int?` | 否 | 否 | - |
| `LocationType` | `query` | `LocationType?` | 否 | 否 | - |
| `LockData` | `query` | `bool?` | 否 | 否 | - |
| `LockedFields` | `query` | `MetadataFields[]` | 否 | 否 | - |
| `Longitude` | `query` | `double?` | 否 | 否 | - |
| `ManagementId` | `query` | `string` | 否 | 否 | - |
| `MediaSources` | `query` | `MediaSourceInfo[]` | 否 | 否 | - |
| `MediaStreams` | `query` | `MediaStream[]` | 否 | 否 | - |
| `MediaType` | `query` | `string` | 否 | 否 | - |
| `MimeType` | `query` | `string` | 否 | 否 | - |
| `MovieCount` | `query` | `int?` | 否 | 否 | - |
| `MusicVideoCount` | `query` | `int?` | 否 | 否 | - |
| `Name` | `query` | `string` | 否 | 否 | - |
| `Number` | `query` | `string` | 否 | 否 | - |
| `OfficialRating` | `query` | `string` | 否 | 否 | - |
| `OriginalTitle` | `query` | `string` | 否 | 否 | - |
| `Overview` | `query` | `string` | 否 | 否 | - |
| `ParentBackdropImageTags` | `query` | `string[]` | 否 | 否 | - |
| `ParentBackdropItemId` | `query` | `string` | 否 | 否 | - |
| `ParentId` | `query` | `string` | 否 | 否 | - |
| `ParentIndexNumber` | `query` | `int?` | 否 | 否 | - |
| `ParentLogoImageTag` | `query` | `string` | 否 | 否 | - |
| `ParentLogoItemId` | `query` | `string` | 否 | 否 | - |
| `ParentThumbImageTag` | `query` | `string` | 否 | 否 | - |
| `ParentThumbItemId` | `query` | `string` | 否 | 否 | - |
| `PartCount` | `query` | `int?` | 否 | 否 | - |
| `Path` | `query` | `string` | 否 | 否 | - |
| `People` | `query` | `BaseItemPerson[]` | 否 | 否 | - |
| `PlaylistItemId` | `query` | `string` | 否 | 否 | - |
| `PreferredMetadataCountryCode` | `query` | `string` | 否 | 否 | - |
| `PreferredMetadataLanguage` | `query` | `string` | 否 | 否 | - |
| `Prefix` | `query` | `string` | 否 | 否 | - |
| `PremiereDate` | `query` | `DateTimeOffset?` | 否 | 否 | - |
| `PresentationUniqueKey` | `query` | `string` | 否 | 否 | - |
| `PrimaryImageAspectRatio` | `query` | `double?` | 否 | 否 | - |
| `PrimaryImageItemId` | `query` | `string` | 否 | 否 | - |
| `PrimaryImageTag` | `query` | `string` | 否 | 否 | - |
| `ProductionLocations` | `query` | `string[]` | 否 | 否 | - |
| `ProductionYear` | `query` | `int?` | 否 | 否 | - |
| `ProviderIds` | `query` | `ProviderIdDictionary` | 否 | 否 | - |
| `RealFrameRate` | `query` | `float?` | 否 | 否 | - |
| `RecursiveItemCount` | `query` | `int?` | 否 | 否 | - |
| `RemoteTrailers` | `query` | `MediaUrl[]` | 否 | 否 | - |
| `RunTimeTicks` | `query` | `long?` | 否 | 否 | - |
| `SeasonCount` | `query` | `int?` | 否 | 否 | - |
| `SeasonId` | `query` | `string` | 否 | 否 | - |
| `SeasonName` | `query` | `string` | 否 | 否 | - |
| `SeriesCount` | `query` | `int?` | 否 | 否 | - |
| `SeriesId` | `query` | `string` | 否 | 否 | - |
| `SeriesName` | `query` | `string` | 否 | 否 | - |
| `SeriesPrimaryImageTag` | `query` | `string` | 否 | 否 | - |
| `SeriesStudio` | `query` | `string` | 否 | 否 | - |
| `SeriesTimerId` | `query` | `string` | 否 | 否 | - |
| `ServerId` | `query` | `string` | 否 | 否 | - |
| `ShutterSpeed` | `query` | `double?` | 否 | 否 | - |
| `Size` | `query` | `long?` | 否 | 否 | - |
| `Software` | `query` | `string` | 否 | 否 | - |
| `SongCount` | `query` | `int?` | 否 | 否 | - |
| `SortIndexNumber` | `query` | `int?` | 否 | 否 | - |
| `SortName` | `query` | `string` | 否 | 否 | - |
| `SortParentIndexNumber` | `query` | `int?` | 否 | 否 | - |
| `SpecialFeatureCount` | `query` | `int?` | 否 | 否 | - |
| `StartDate` | `query` | `DateTimeOffset?` | 否 | 否 | - |
| `Status` | `query` | `string` | 否 | 否 | - |
| `Studios` | `query` | `NameLongIdPair[]` | 否 | 否 | - |
| `Subviews` | `query` | `string[]` | 否 | 否 | - |
| `SupportsResume` | `query` | `bool?` | 否 | 否 | - |
| `SupportsSync` | `query` | `bool?` | 否 | 否 | - |
| `SyncStatus` | `query` | `SyncJobItemStatus?` | 否 | 否 | - |
| `TagItems` | `query` | `NameLongIdPair[]` | 否 | 否 | - |
| `Taglines` | `query` | `string[]` | 否 | 否 | - |
| `Tags` | `query` | `string[]` | 否 | 否 | - |
| `TimerId` | `query` | `string` | 否 | 否 | - |
| `TimerType` | `query` | `TimerType?` | 否 | 否 | - |
| `TunerName` | `query` | `string` | 否 | 否 | - |
| `Type` | `query` | `string` | 否 | 否 | - |
| `UserData` | `query` | `UserItemDataDto` | 否 | 否 | - |
| `Video3DFormat` | `query` | `Video3DFormat?` | 否 | 否 | - |
| `VideoCodec` | `query` | `string` | 否 | 否 | - |
| `Width` | `query` | `int?` | 否 | 否 | - |

#### `GET` `/Items/{ItemId}/MetadataEditor`

- **摘要**：Gets metadata editor info for an item
- **鉴权**：需要认证
- **返回类型**：`MetadataEditorInfo`
- **处理服务**：`Emby.Api.ItemUpdateService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetMetadataEditorInfo` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `ItemId` | `path` | `string` | 是 | 否 | The id of the item |

### LibraryService（15 条）

#### `DELETE` `/Items`

- **摘要**：Deletes an item from the library and file system
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Library.LibraryService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Library.DeleteItems` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Ids` | `query` | `string` | 是 | 否 | Ids |

#### `GET` `/Items/Counts`

- **鉴权**：需要认证
- **返回类型**：`ItemCounts`
- **处理服务**：`Emby.Api.Library.LibraryService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Library.GetItemCounts` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `IsFavorite` | `query` | `bool?` | 否 | 否 | Optional. Get counts of favorite items |
| `ParentId` | `query` | `string` | 否 | 否 | - |
| `UserId` | `query` | `string` | 否 | 否 | Optional. Get counts from a specific user's library. |

#### `POST` `/Items/Delete`

- **摘要**：Deletes an item from the library and file system
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Library.LibraryService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Library.DeleteItems` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Ids` | `query` | `string` | 是 | 否 | Ids |

#### `GET` `/Items/Intros`

- **摘要**：Gets info to debug intros
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`List<IntroDebugInfo>`
- **处理服务**：`Emby.Api.Library.LibraryService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Library.GetIntrosDebugInfo` (`Emby.Api.dll`)

_无请求参数。_

#### `DELETE` `/Items/{Id}`

- **摘要**：Deletes an item from the library and file system
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Library.LibraryService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Library.DeleteItem` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |

#### `GET` `/Items/{Id}/Ancestors`

- **摘要**：Gets all parents of an item
- **鉴权**：需要认证
- **返回类型**：`BaseItemDto[]`
- **处理服务**：`Emby.Api.Library.LibraryService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Library.GetAncestors` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `UserId` | `query` | `string` | 否 | 否 | Optional. Filter by user id, and attach user data |

#### `GET` `/Items/{Id}/CriticReviews`

- **摘要**：Gets critic reviews for an item
- **鉴权**：需要认证
- **返回类型**：`QueryResult<BaseItemDto>`
- **处理服务**：`Emby.Api.Library.LibraryService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Library.GetCriticReviews` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `Limit` | `query` | `int?` | 否 | 否 | Optional. The maximum number of records to return |
| `StartIndex` | `query` | `int?` | 否 | 否 | Optional. The record index to start at. All items with a lower index will be dropped from the results. |

#### `POST` `/Items/{Id}/Delete`

- **摘要**：Deletes an item from the library and file system
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Library.LibraryService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Library.DeleteItem` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |

#### `GET` `/Items/{Id}/DeleteInfo`

- **摘要**：Gets delete info for an item
- **鉴权**：需要认证
- **返回类型**：`DeleteInfo`
- **处理服务**：`Emby.Api.Library.LibraryService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Library.GetDeleteInfo` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |

#### `GET` `/Items/{Id}/Download`

- **摘要**：Downloads item media
- **鉴权**：需要认证；角色要求: `download`
- **返回类型**：`object`
- **处理服务**：`Emby.Api.Library.LibraryService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Library.GetDownload` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `MediaSourceId` | `path` | `string` | 否 | 否 | MediaSource Id |

#### `GET` `/Items/{Id}/File`

- **摘要**：Gets the original file of an item
- **鉴权**：无需认证（公开接口）
- **返回类型**：`object`
- **处理服务**：`Emby.Api.Library.LibraryService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Library.GetItemOriginalFile` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `MediaSourceId` | `path` | `string` | 否 | 否 | MediaSource Id |

#### `GET` `/Items/{Id}/Similar`

- **摘要**：Gets similar items
- **鉴权**：需要认证
- **返回类型**：`QueryResult<BaseItemDto>`
- **处理服务**：`Emby.Api.Library.LibraryService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Library.GetSimilarItems` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
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

#### `GET` `/Items/{Id}/ThemeMedia`

- **摘要**：Gets theme videos and songs for an item
- **鉴权**：需要认证
- **返回类型**：`AllThemeMediaResult`
- **处理服务**：`Emby.Api.Library.LibraryService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Library.GetThemeMedia` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
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
| `EnableThemeSongs` | `query` | `bool` | 否 | 否 | - |
| `EnableThemeVideos` | `query` | `bool` | 否 | 否 | - |
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
| `InheritFromParent` | `query` | `bool` | 否 | 否 | Determines whether or not parent items should be searched for theme media. |
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

#### `GET` `/Items/{Id}/ThemeSongs`

- **摘要**：Gets theme songs for an item
- **鉴权**：需要认证
- **返回类型**：`ThemeMediaResult`
- **处理服务**：`Emby.Api.Library.LibraryService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Library.GetThemeSongs` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
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
| `InheritFromParent` | `query` | `bool` | 否 | 否 | Determines whether or not parent items should be searched for theme media. |
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

#### `GET` `/Items/{Id}/ThemeVideos`

- **摘要**：Gets theme videos for an item
- **鉴权**：需要认证
- **返回类型**：`ThemeMediaResult`
- **处理服务**：`Emby.Api.Library.LibraryService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Library.GetThemeVideos` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
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
| `InheritFromParent` | `query` | `bool` | 否 | 否 | Determines whether or not parent items should be searched for theme media. |
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

### MediaInfoService（2 条）

#### `GET` `/Items/{Id}/PlaybackInfo`

- **摘要**：Gets live playback media info for an item
- **鉴权**：需要认证
- **返回类型**：`PlaybackInfoResponse`
- **处理服务**：`Emby.Server.MediaEncoding.Api.MediaInfoService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.GetPlaybackInfo` (`Emby.Server.MediaEncoding.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `UserId` | `query` | `string` | 是 | 否 | User Id |

#### `POST` `/Items/{Id}/PlaybackInfo`

- **摘要**：Gets live playback media info for an item
- **鉴权**：需要认证
- **返回类型**：`PlaybackInfoResponse`
- **处理服务**：`Emby.Server.MediaEncoding.Api.MediaInfoService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.GetPostedPlaybackInfo` (`Emby.Server.MediaEncoding.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 否 | 否 | - |
| `AllowAudioStreamCopy` | `query` | `bool` | 否 | 否 | - |
| `AllowInterlacedVideoStreamCopy` | `query` | `bool` | 否 | 否 | - |
| `AllowVideoStreamCopy` | `query` | `bool` | 否 | 否 | - |
| `AudioStreamIndex` | `query` | `int?` | 否 | 否 | - |
| `AutoOpenLiveStream` | `query` | `bool` | 否 | 否 | - |
| `CurrentPlaySessionId` | `query` | `string` | 否 | 否 | - |
| `DeviceProfile` | `query` | `DeviceProfile` | 否 | 否 | - |
| `EnableDirectPlay` | `query` | `bool` | 否 | 否 | - |
| `EnableDirectStream` | `query` | `bool` | 否 | 否 | - |
| `EnableTranscoding` | `query` | `bool` | 否 | 否 | - |
| `IsPlayback` | `query` | `bool` | 否 | 否 | - |
| `LiveStreamId` | `query` | `string` | 否 | 否 | - |
| `MaxAudioChannels` | `query` | `int?` | 否 | 否 | - |
| `MaxStreamingBitrate` | `query` | `long?` | 否 | 否 | - |
| `MediaSourceId` | `query` | `string` | 否 | 否 | - |
| `StartTimeTicks` | `query` | `long?` | 否 | 否 | - |
| `SubtitleStreamIndex` | `query` | `int?` | 否 | 否 | - |
| `UserId` | `query` | `string` | 否 | 否 | - |

### RemoteImageService（3 条）

#### `GET` `/Items/{Id}/RemoteImages`

- **摘要**：Gets available remote images for an item
- **鉴权**：需要认证
- **返回类型**：`RemoteImageResult`
- **处理服务**：`Emby.Api.Images.RemoteImageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Images.GetRemoteImages` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `EnableSeriesImages` | `query` | `bool` | 否 | 否 | Optional. |
| `IncludeAllLanguages` | `query` | `bool` | 否 | 否 | Optional. |
| `Limit` | `query` | `int?` | 否 | 否 | Optional. The maximum number of records to return |
| `ProviderName` | `query` | `string` | 否 | 否 | Optional. The image provider to use |
| `StartIndex` | `query` | `int?` | 否 | 否 | Optional. The record index to start at. All items with a lower index will be dropped from the results. |
| `Type` | `query` | `ImageType?` | 否 | 否 | The image type |

#### `POST` `/Items/{Id}/RemoteImages/Download`

- **摘要**：Downloads a remote image for an item
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Images.RemoteImageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Images.DownloadRemoteImage` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `Type` | `query` | `ImageType` | 是 | 否 | The image type |
| `ImageIndex` | `query` | `int?` | 否 | 否 | - |
| `ImageUrl` | `query` | `string` | 否 | 否 | The image url |
| `ProviderName` | `query` | `string` | 否 | 否 | The image provider |

#### `GET` `/Items/{Id}/RemoteImages/Providers`

- **摘要**：Gets available remote image providers for an item
- **鉴权**：需要认证
- **返回类型**：`List<ImageProviderInfo>`
- **处理服务**：`Emby.Api.Images.RemoteImageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Images.GetRemoteImageProviders` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |

### SearchService（1 条）

#### `GET` `/Search/Hints` `[隐藏接口]`

- **摘要**：Gets search hints based on a search term
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Obsolete.SearchService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Obsolete.GetSearchHints` (`Emby.Api.dll`)

_无请求参数。_

### SubtitleService（9 条）

#### `GET` `/Items/{Id}/RemoteSearch/Subtitles/{Language}`

- **鉴权**：需要认证
- **返回类型**：`RemoteSubtitleInfo[]`
- **处理服务**：`Emby.Api.Subtitles.SubtitleService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Subtitles.SearchRemoteSubtitles` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `Language` | `path` | `string` | 是 | 否 | Language |
| `MediaSourceId` | `query` | `string` | 是 | 否 | MediaSourceId |
| `IsForced` | `query` | `bool?` | 否 | 否 | IsForced |
| `IsHearingImpaired` | `query` | `bool?` | 否 | 否 | IsHearingImpaired |
| `IsPerfectMatch` | `query` | `bool?` | 否 | 否 | IsPerfectMatch |

#### `POST` `/Items/{Id}/RemoteSearch/Subtitles/{SubtitleId}`

- **鉴权**：需要认证；角色要求: `DownloadSubtitles`
- **返回类型**：`SubtitleDownloadResult`
- **处理服务**：`Emby.Api.Subtitles.SubtitleService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Subtitles.DownloadRemoteSubtitles` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `SubtitleId` | `path` | `string` | 是 | 否 | SubtitleId |
| `MediaSourceId` | `query` | `string` | 是 | 否 | MediaSourceId |

#### `DELETE` `/Items/{Id}/Subtitles/{Index}`

- **摘要**：Deletes an external subtitle file
- **鉴权**：需要认证；角色要求: `ManageSubtitles`
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Subtitles.SubtitleService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Subtitles.DeleteSubtitle` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `Index` | `path` | `int` | 是 | 否 | The subtitle stream index |
| `MediaSourceId` | `query` | `string` | 是 | 否 | MediaSourceId |

#### `POST` `/Items/{Id}/Subtitles/{Index}/Delete`

- **摘要**：Deletes an external subtitle file
- **鉴权**：需要认证；角色要求: `ManageSubtitles`
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Subtitles.SubtitleService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Subtitles.DeleteSubtitle` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `Index` | `path` | `int` | 是 | 否 | The subtitle stream index |
| `MediaSourceId` | `query` | `string` | 是 | 否 | MediaSourceId |

#### `GET` `/Items/{Id}/{MediaSourceId}/Subtitles/{Index}/Stream.{Format}`

- **摘要**：Gets subtitles in a specified format.
- **鉴权**：无需认证（公开接口）
- **返回类型**：`object`
- **处理服务**：`Emby.Api.Subtitles.SubtitleService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Subtitles.GetSubtitle` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Format` | `path` | `string` | 是 | 否 | Format |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `Index` | `path` | `int` | 是 | 否 | The subtitle stream index |
| `MediaSourceId` | `path` | `string` | 是 | 否 | MediaSourceId |
| `AddVttTimeMap` | `query` | `bool` | 否 | 否 | - |
| `AllowChunkedResponse` | `query` | `bool` | 否 | 否 | - |
| `CopyTimestamps` | `query` | `bool` | 否 | 否 | CopyTimestamps |
| `EndPositionTicks` | `query` | `long?` | 否 | 否 | EndPositionTicks |
| `SetFilename` | `query` | `bool` | 否 | 否 | - |
| `StartPositionTicks` | `query` | `long` | 否 | 否 | StartPositionTicks |
| `VttTimestampMap` | `query` | `string` | 否 | 否 | - |

#### `HEAD` `/Items/{Id}/{MediaSourceId}/Subtitles/{Index}/Stream.{Format}`

- **摘要**：Gets subtitles in a specified format.
- **鉴权**：无需认证（公开接口）
- **返回类型**：`object`
- **处理服务**：`Emby.Api.Subtitles.SubtitleService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Subtitles.GetSubtitle` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Format` | `path` | `string` | 是 | 否 | Format |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `Index` | `path` | `int` | 是 | 否 | The subtitle stream index |
| `MediaSourceId` | `path` | `string` | 是 | 否 | MediaSourceId |
| `AddVttTimeMap` | `query` | `bool` | 否 | 否 | - |
| `AllowChunkedResponse` | `query` | `bool` | 否 | 否 | - |
| `CopyTimestamps` | `query` | `bool` | 否 | 否 | CopyTimestamps |
| `EndPositionTicks` | `query` | `long?` | 否 | 否 | EndPositionTicks |
| `SetFilename` | `query` | `bool` | 否 | 否 | - |
| `StartPositionTicks` | `query` | `long` | 否 | 否 | StartPositionTicks |
| `VttTimestampMap` | `query` | `string` | 否 | 否 | - |

#### `GET` `/Items/{Id}/{MediaSourceId}/Subtitles/{Index}/{StartPositionTicks}/Stream.{Format}`

- **摘要**：Gets subtitles in a specified format.
- **鉴权**：无需认证（公开接口）
- **返回类型**：`object`
- **处理服务**：`Emby.Api.Subtitles.SubtitleService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Subtitles.GetSubtitle` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Format` | `path` | `string` | 是 | 否 | Format |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `Index` | `path` | `int` | 是 | 否 | The subtitle stream index |
| `MediaSourceId` | `path` | `string` | 是 | 否 | MediaSourceId |
| `AddVttTimeMap` | `query` | `bool` | 否 | 否 | - |
| `AllowChunkedResponse` | `query` | `bool` | 否 | 否 | - |
| `CopyTimestamps` | `query` | `bool` | 否 | 否 | CopyTimestamps |
| `EndPositionTicks` | `query` | `long?` | 否 | 否 | EndPositionTicks |
| `SetFilename` | `query` | `bool` | 否 | 否 | - |
| `StartPositionTicks` | `query` | `long` | 否 | 否 | StartPositionTicks |
| `VttTimestampMap` | `query` | `string` | 否 | 否 | - |

#### `HEAD` `/Items/{Id}/{MediaSourceId}/Subtitles/{Index}/{StartPositionTicks}/Stream.{Format}`

- **摘要**：Gets subtitles in a specified format.
- **鉴权**：无需认证（公开接口）
- **返回类型**：`object`
- **处理服务**：`Emby.Api.Subtitles.SubtitleService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Subtitles.GetSubtitle` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Format` | `path` | `string` | 是 | 否 | Format |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `Index` | `path` | `int` | 是 | 否 | The subtitle stream index |
| `MediaSourceId` | `path` | `string` | 是 | 否 | MediaSourceId |
| `AddVttTimeMap` | `query` | `bool` | 否 | 否 | - |
| `AllowChunkedResponse` | `query` | `bool` | 否 | 否 | - |
| `CopyTimestamps` | `query` | `bool` | 否 | 否 | CopyTimestamps |
| `EndPositionTicks` | `query` | `long?` | 否 | 否 | EndPositionTicks |
| `SetFilename` | `query` | `bool` | 否 | 否 | - |
| `StartPositionTicks` | `query` | `long` | 否 | 否 | StartPositionTicks |
| `VttTimestampMap` | `query` | `string` | 否 | 否 | - |

#### `GET` `/Providers/Subtitles/Subtitles/{Id}`

- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Subtitles.SubtitleService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Subtitles.GetRemoteSubtitles` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |

### TagService（3 条）

#### `GET` `/Items/Prefixes`

- **摘要**：Gets items based on a query.
- **鉴权**：需要认证
- **返回类型**：`NameValuePair[]`
- **处理服务**：`Emby.Api.UserLibrary.TagService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.GetPrefixes` (`Emby.Api.dll`)

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

#### `POST` `/Items/{Id}/Tags/Add`

- **摘要**：Adds new tags to an item
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.UserLibrary.TagService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.AddTags` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `Id` | `body` | `NameIdPair[]` | 是 | 否 | Tags to add |

#### `POST` `/Items/{Id}/Tags/Delete`

- **摘要**：Removes tags from an item
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.UserLibrary.TagService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.RemoveTags` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `Id` | `body` | `NameIdPair[]` | 是 | 否 | Tags to add |

### UserLibraryService（4 条）

#### `POST` `/Items/Access`

- **摘要**：Updates user item access
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.UserLibrary.UserLibraryService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.UpdateUserItemAccess` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `ItemAccess` | `query` | `UserItemShareLevel` | 否 | 否 | - |
| `ItemIds` | `query` | `string[]` | 否 | 否 | - |
| `UserIds` | `query` | `string[]` | 否 | 否 | - |

#### `POST` `/Items/Shared/Leave`

- **摘要**：Leaves a shared item
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.UserLibrary.UserLibraryService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.LeaveSharedItems` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `ItemIds` | `query` | `string[]` | 否 | 否 | - |
| `UserId` | `query` | `string` | 否 | 否 | - |

#### `POST` `/Items/{Id}/MakePrivate`

- **摘要**：Makes an item private
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.UserLibrary.UserLibraryService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.MakeItemPrivate` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |

#### `POST` `/Items/{Id}/MakePublic`

- **摘要**：Makes an item public to all users
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.UserLibrary.UserLibraryService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.MakeItemPublic` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |

### VideoService（1 条）

#### `GET` `/Items/File` `[隐藏接口]`

- **摘要**：Gets the original file of an item
- **鉴权**：需要认证；仅限本地调用
- **返回类型**：`object`
- **处理服务**：`Emby.Server.MediaEncoding.Api.Progressive.VideoService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.Progressive.GetFile` (`Emby.Server.MediaEncoding.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `query` | `string` | 否 | 否 | - |
