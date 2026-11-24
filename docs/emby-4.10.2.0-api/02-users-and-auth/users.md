# 用户管理、用户设置与显示偏好

> 所属模块：**02. 用户、认证、设备与会话**  
> 路由前缀：`/DisplayPreferences`, `/Users`, `/UserSettings`  
> 接口总数：**76** 条

涵盖用户增删改查、登录认证、收藏/已看标记、用户视图与显示偏好。

[← 返回总目录](../README.md)

## 接口总览

| # | 方法 | 路径 | 鉴权 | 返回类型 | 摘要 |
| :---: | :--- | :--- | :--- | :--- | :--- |
| 1 | `POST` | `/DisplayPreferences/{DisplayPreferencesId}` | 需认证 | `void` | Updates a user's display preferences for an item |
| 2 | `GET` | `/DisplayPreferences/{Id}` | 需认证 | `DisplayPreferences` | Gets a user's display preferences for an item |
| 3 | `GET` | `/Users` *(隐藏)* | 认证 (`admin`) | `UserDto[]` | Gets a list of users |
| 4 | `POST` | `/Users/AuthenticateByName` | 公开 | `AuthenticationResult` | Authenticates a user |
| 5 | `GET` | `/Users/CopyDataOptions` | 认证 (`Admin`) | `FullUserCopyDataOptions` | Gets copy data options |
| 6 | `POST` | `/Users/ForgotPassword` | 公开 | `ForgotPasswordResult` | Initiates the forgot password process for a local user |
| 7 | `POST` | `/Users/ForgotPassword/Pin` | 公开 | `PinRedeemResult` | Redeems a forgot password pin |
| 8 | `GET` | `/Users/ItemAccess` | 需认证 | `QueryResult<UserDto>` | Gets a list of users |
| 9 | `POST` | `/Users/New` | 认证 (`Admin`) | `UserDto` | Creates a user |
| 10 | `GET` | `/Users/Prefixes` | 认证 (`admin`) | `NameIdPair[]` | Gets a list of users |
| 11 | `GET` | `/Users/Public` | 公开 | `UserDto[]` | Gets a list of publicly visible users for display on a login screen. |
| 12 | `GET` | `/Users/Query` | 认证 (`admin`) | `QueryResult<UserDto>` | Gets a list of users |
| 13 | `DELETE` | `/Users/{Id}` | 认证 (`Admin`) | `void` | Deletes a user |
| 14 | `GET` | `/Users/{Id}` | 需认证 | `UserDto` | Gets a user by Id |
| 15 | `POST` | `/Users/{Id}` | 需认证 | `void` | Updates a user |
| 16 | `POST` | `/Users/{Id}/Authenticate` | 公开 | `AuthenticationResult` | Authenticates a user |
| 17 | `POST` | `/Users/{Id}/Configuration` | 需认证 | `void` | Updates a user configuration |
| 18 | `POST` | `/Users/{Id}/Configuration/Partial` | 需认证 | `void` | Updates a user configuration |
| 19 | `DELETE` | `/Users/{Id}/Connect/Link` | 认证 (`Admin`) | `void` | Removes a Connect link for a user |
| 20 | `POST` | `/Users/{Id}/Connect/Link` | 认证 (`Admin`) | `UserLinkResult` | Creates a Connect link for a user |
| 21 | `POST` | `/Users/{Id}/Connect/Link/Delete` | 认证 (`Admin`) | `void` | Removes a Connect link for a user |
| 22 | `POST` | `/Users/{Id}/Delete` | 认证 (`Admin`) | `void` | Deletes a user |
| 23 | `DELETE` | `/Users/{Id}/Images/{Type}` | 需认证 | `void` | - |
| 24 | `GET` | `/Users/{Id}/Images/{Type}` | 需认证 | `object` | - |
| 25 | `HEAD` | `/Users/{Id}/Images/{Type}` | 需认证 | `object` | - |
| 26 | `POST` | `/Users/{Id}/Images/{Type}` | 需认证 | `void` | Uploads an image for an item, must be base64 encoded. |
| 27 | `POST` | `/Users/{Id}/Images/{Type}/Delete` | 需认证 | `void` | - |
| 28 | `DELETE` | `/Users/{Id}/Images/{Type}/{Index}` | 需认证 | `void` | - |
| 29 | `GET` | `/Users/{Id}/Images/{Type}/{Index}` | 需认证 | `object` | - |
| 30 | `HEAD` | `/Users/{Id}/Images/{Type}/{Index}` | 需认证 | `object` | - |
| 31 | `POST` | `/Users/{Id}/Images/{Type}/{Index}` | 需认证 | `void` | Uploads an image for an item, must be base64 encoded. |
| 32 | `POST` | `/Users/{Id}/Images/{Type}/{Index}/Delete` | 需认证 | `void` | - |
| 33 | `POST` | `/Users/{Id}/Password` | 需认证 | `void` | Updates a user's password |
| 34 | `POST` | `/Users/{Id}/Policy` | 认证 (`admin`) | `void` | Updates a user policy |
| 35 | `DELETE` | `/Users/{Id}/RecentlySearched` | 需认证 | `void` | Clears recently searched |
| 36 | `POST` | `/Users/{Id}/RecentlySearched/Delete` | 需认证 | `void` | Clears recently searched |
| 37 | `DELETE` | `/Users/{Id}/TrackSelections/{TrackType}` | 需认证 | `void` | Clears audio or subtitle track selections for a user |
| 38 | `POST` | `/Users/{Id}/TrackSelections/{TrackType}/Delete` | 需认证 | `void` | Clears audio or subtitle track selections for a user |
| 39 | `POST` | `/Users/{UserId}/CopyData` | 认证 (`Admin`) | `void` | Copies data from one user to another |
| 40 | `DELETE` | `/Users/{UserId}/FavoriteItems/{Id}` | 需认证 | `UserItemDataDto` | Unmarks an item as a favorite |
| 41 | `POST` | `/Users/{UserId}/FavoriteItems/{Id}` | 需认证 | `UserItemDataDto` | Marks an item as a favorite |
| 42 | `POST` | `/Users/{UserId}/FavoriteItems/{Id}/Delete` | 需认证 | `UserItemDataDto` | Unmarks an item as a favorite |
| 43 | `GET` | `/Users/{UserId}/GroupingOptions` *(隐藏)* | 需认证 | `BaseItemDto[]` | - |
| 44 | `GET` | `/Users/{UserId}/HomeSections` | 需认证 | `List<ContentSection>` | - |
| 45 | `POST` | `/Users/{UserId}/HomeSections` | 需认证 | `void` | - |
| 46 | `POST` | `/Users/{UserId}/HomeSections/Delete` | 需认证 | `void` | - |
| 47 | `POST` | `/Users/{UserId}/HomeSections/Move` | 需认证 | `void` | - |
| 48 | `GET` | `/Users/{UserId}/Items` | 需认证 | `QueryResult<BaseItemDto>` | Gets items based on a query. |
| 49 | `GET` | `/Users/{UserId}/Items/Latest` | 需认证 | `BaseItemDto[]` | Gets latest media |
| 50 | `GET` | `/Users/{UserId}/Items/Resume` | 需认证 | `QueryResult<BaseItemDto>` | Gets items based on a query. |
| 51 | `GET` | `/Users/{UserId}/Items/Root` | 需认证 | `BaseItemDto` | Gets the root folder from a user's library |
| 52 | `GET` | `/Users/{UserId}/Items/{Id}` | 需认证 | `BaseItemDto` | Gets an item from a user's library |
| 53 | `POST` | `/Users/{UserId}/Items/{Id}/HideFromResume` | 需认证 | `UserItemDataDto` | Updates a user's hide from resume for an item |
| 54 | `GET` | `/Users/{UserId}/Items/{Id}/Intros` | 需认证 | `QueryResult<BaseItemDto>` | Gets intros to play before the main media item plays |
| 55 | `GET` | `/Users/{UserId}/Items/{Id}/LocalTrailers` | 需认证 | `BaseItemDto[]` | Gets local trailers for an item |
| 56 | `DELETE` | `/Users/{UserId}/Items/{Id}/Rating` | 需认证 | `UserItemDataDto` | Deletes a user's saved personal rating for an item |
| 57 | `POST` | `/Users/{UserId}/Items/{Id}/Rating` | 需认证 | `UserItemDataDto` | Updates a user's rating for an item |
| 58 | `POST` | `/Users/{UserId}/Items/{Id}/Rating/Delete` | 需认证 | `UserItemDataDto` | Deletes a user's saved personal rating for an item |
| 59 | `GET` | `/Users/{UserId}/Items/{Id}/SpecialFeatures` | 需认证 | `BaseItemDto[]` | Gets special features for an item |
| 60 | `POST` | `/Users/{UserId}/Items/{ItemId}/UserData` | 需认证 | `void` | Updates userdata for an item |
| 61 | `DELETE` | `/Users/{UserId}/PlayedItems/{Id}` | 需认证 | `UserItemDataDto` | Marks an item as unplayed |
| 62 | `POST` | `/Users/{UserId}/PlayedItems/{Id}` | 需认证 | `UserItemDataDto` | Marks an item as played |
| 63 | `POST` | `/Users/{UserId}/PlayedItems/{Id}/Delete` | 需认证 | `UserItemDataDto` | Marks an item as unplayed |
| 64 | `DELETE` | `/Users/{UserId}/PlayingItems/{Id}` | 需认证 | `void` | Reports that a user has stopped playing an item |
| 65 | `POST` | `/Users/{UserId}/PlayingItems/{Id}` | 需认证 | `void` | Reports that a user has begun playing an item |
| 66 | `POST` | `/Users/{UserId}/PlayingItems/{Id}/Delete` | 需认证 | `void` | Reports that a user has stopped playing an item |
| 67 | `POST` | `/Users/{UserId}/PlayingItems/{Id}/Progress` | 需认证 | `void` | Reports a user's playback progress |
| 68 | `POST` | `/Users/{UserId}/SearchedItems/` | 需认证 | `void` | Marks an item as a favorite |
| 69 | `GET` | `/Users/{UserId}/Sections/{SectionId}/Items` | 需认证 | `QueryResult<BaseItemDto>` | - |
| 70 | `GET` | `/Users/{UserId}/Suggestions` | 需认证 | `QueryResult<BaseItemDto>` | Gets items based on a query. |
| 71 | `GET` | `/Users/{UserId}/TypedSettings/{Key}` | 需认证 | `object` | Gets a typed user setting |
| 72 | `POST` | `/Users/{UserId}/TypedSettings/{Key}` | 需认证 | `void` | Updates a typed user setting |
| 73 | `GET` | `/Users/{UserId}/Views` | 需认证 | `QueryResult<BaseItemDto>` | - |
| 74 | `GET` | `/UserSettings/{UserId}` | 需认证 | `Dictionary<string, string>` | Gets user settings |
| 75 | `POST` | `/UserSettings/{UserId}` | 需认证 | `void` | Updates a user's display preferences for an item |
| 76 | `POST` | `/UserSettings/{UserId}/Partial` | 需认证 | `void` | Updates a user's display preferences for an item |

## 接口详情

### ConnectService（3 条）

#### `DELETE` `/Users/{Id}/Connect/Link`

- **摘要**：Removes a Connect link for a user
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`void`
- **处理服务**：`Emby.Server.Connect.Api.ConnectService` (Emby.Server.Connect.dll)
- **Request DTO**：`Emby.Server.Connect.Api.DeleteConnectLink` (`Emby.Server.Connect.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `query` | `Guid` | 是 | 否 | User Id |

#### `POST` `/Users/{Id}/Connect/Link`

- **摘要**：Creates a Connect link for a user
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`UserLinkResult`
- **处理服务**：`Emby.Server.Connect.Api.ConnectService` (Emby.Server.Connect.dll)
- **Request DTO**：`Emby.Server.Connect.Api.CreateConnectLink` (`Emby.Server.Connect.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `ConnectUsername` | `query` | `string` | 是 | 否 | Connect username |
| `Id` | `query` | `Guid` | 是 | 否 | User Id |

#### `POST` `/Users/{Id}/Connect/Link/Delete`

- **摘要**：Removes a Connect link for a user
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`void`
- **处理服务**：`Emby.Server.Connect.Api.ConnectService` (Emby.Server.Connect.dll)
- **Request DTO**：`Emby.Server.Connect.Api.DeleteConnectLink` (`Emby.Server.Connect.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `query` | `Guid` | 是 | 否 | User Id |

### ContentService（5 条）

#### `GET` `/Users/{UserId}/HomeSections`

- **鉴权**：需要认证
- **返回类型**：`List<ContentSection>`
- **处理服务**：`Emby.Api.UserLibrary.ContentService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.GetHomeSections` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `UserId` | `path` | `string` | 是 | 否 | User Id |
| `DisplayMode` | `query` | `string` | 否 | 否 | - |
| `IsEditMode` | `query` | `bool` | 否 | 否 | - |

#### `POST` `/Users/{UserId}/HomeSections`

- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.UserLibrary.ContentService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.SaveHomeSection` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `UserId` | `path` | `string` | 是 | 否 | User Id |
| `CardSizeOffset` | `query` | `int` | 否 | 否 | - |
| `CollectionType` | `query` | `string` | 否 | 否 | - |
| `CustomName` | `query` | `string` | 否 | 否 | - |
| `DisplayMode` | `query` | `string` | 否 | 否 | - |
| `ExcludedFolders` | `query` | `string[]` | 否 | 否 | - |
| `Id` | `query` | `string` | 否 | 否 | - |
| `ImageType` | `query` | `string` | 否 | 否 | - |
| `IncludeNextUpInResume` | `query` | `bool` | 否 | 否 | - |
| `ItemTypes` | `query` | `string[]` | 否 | 否 | - |
| `Monitor` | `query` | `string[]` | 否 | 否 | - |
| `Name` | `query` | `string` | 否 | 否 | - |
| `ParentId` | `query` | `string` | 否 | 否 | - |
| `ParentItem` | `query` | `BaseItemDto` | 否 | 否 | - |
| `PremiumFeature` | `query` | `string` | 否 | 否 | - |
| `PremiumMessage` | `query` | `string` | 否 | 否 | - |
| `Query` | `query` | `ItemsQuery` | 否 | 否 | - |
| `RefreshInterval` | `query` | `int?` | 否 | 否 | - |
| `ScrollDirection` | `query` | `ScrollDirection?` | 否 | 否 | - |
| `SectionType` | `query` | `string` | 否 | 否 | - |
| `SortBy` | `query` | `string` | 否 | 否 | - |
| `SortOrder` | `query` | `string` | 否 | 否 | - |
| `Subtitle` | `query` | `string` | 否 | 否 | - |
| `TextInfo` | `query` | `TextSectionInfo` | 否 | 否 | - |
| `ViewType` | `query` | `string` | 否 | 否 | - |

#### `POST` `/Users/{UserId}/HomeSections/Delete`

- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.UserLibrary.ContentService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.DeleteHomeSections` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `UserId` | `path` | `string` | 是 | 否 | User Id |
| `Ids` | `query` | `string[]` | 否 | 否 | - |

#### `POST` `/Users/{UserId}/HomeSections/Move`

- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.UserLibrary.ContentService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.MoveHomeSections` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `UserId` | `path` | `string` | 是 | 否 | User Id |
| `Ids` | `query` | `string[]` | 否 | 否 | - |
| `NewIndex` | `query` | `int` | 否 | 否 | - |

#### `GET` `/Users/{UserId}/Sections/{SectionId}/Items`

- **鉴权**：需要认证
- **返回类型**：`QueryResult<BaseItemDto>`
- **处理服务**：`Emby.Api.UserLibrary.ContentService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.GetSectionItems` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `SectionId` | `path` | `string` | 否 | 否 | - |
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
| `EnableFavoriteSorting` | `query` | `bool` | 否 | 否 | - |
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

### DisplayPreferencesService（5 条）

#### `POST` `/DisplayPreferences/{DisplayPreferencesId}`

- **摘要**：Updates a user's display preferences for an item
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.DisplayPreferencesService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UpdateDisplayPreferences` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `DisplayPreferencesId` | `path` | `string` | 是 | 否 | DisplayPreferences Id |
| `UserId` | `query` | `string` | 是 | 否 | User Id |
| `Client` | `query` | `string` | 否 | 否 | - |
| `CustomPrefs` | `query` | `Dictionary<string, string>` | 否 | 否 | - |
| `Id` | `query` | `string` | 否 | 否 | - |
| `SortBy` | `query` | `string` | 否 | 否 | - |
| `SortOrder` | `query` | `SortOrder` | 否 | 否 | - |

#### `GET` `/DisplayPreferences/{Id}`

- **摘要**：Gets a user's display preferences for an item
- **鉴权**：需要认证
- **返回类型**：`DisplayPreferences`
- **处理服务**：`Emby.Api.DisplayPreferencesService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetDisplayPreferences` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `Client` | `query` | `string` | 是 | 否 | Client |
| `UserId` | `query` | `string` | 是 | 否 | User Id |

#### `GET` `/UserSettings/{UserId}`

- **摘要**：Gets user settings
- **鉴权**：需要认证
- **返回类型**：`Dictionary<string, string>`
- **处理服务**：`Emby.Api.DisplayPreferencesService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetUserSettings` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `UserId` | `query` | `string` | 是 | 否 | User Id |

#### `POST` `/UserSettings/{UserId}`

- **摘要**：Updates a user's display preferences for an item
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.DisplayPreferencesService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UpdateUserSettings` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `UserId` | `query` | `string` | 是 | 否 | User Id |
| `Capacity` | `query` | `int` | 否 | 否 | - |
| `Comparer` | `query` | `IEqualityComparer<string>` | 否 | 否 | - |
| `Count` | `query` | `int` | 否 | 否 | - |
| `Item` | `query` | `string` | 否 | 否 | - |
| `Keys` | `query` | `KeyCollection<string, string>` | 否 | 否 | - |
| `Values` | `query` | `ValueCollection<string, string>` | 否 | 否 | - |

#### `POST` `/UserSettings/{UserId}/Partial`

- **摘要**：Updates a user's display preferences for an item
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.DisplayPreferencesService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UpdatePartialUserSettings` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `UserId` | `query` | `string` | 是 | 否 | User Id |
| `RequestStream` | `query` | `Stream` | 否 | 否 | - |

### ImageService（10 条）

#### `DELETE` `/Users/{Id}/Images/{Type}`

- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Images.ImageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Images.DeleteUserImage` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | User Id |
| `Type` | `path` | `ImageType` | 是 | 否 | Image Type |
| `Index` | `query` | `int` | 否 | 否 | Image Index |

#### `GET` `/Users/{Id}/Images/{Type}`

- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Api.Images.ImageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Images.GetUserImage` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | User Id |
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
| `PositionTicks` | `query` | `long` | 否 | 否 | - |
| `Quality` | `query` | `int` | 否 | 否 | Optional quality setting, from 0-100. Defaults to 90 and should suffice in most cases. |
| `Tag` | `query` | `string` | 否 | 否 | Optional. Supply the cache tag from the item object to receive strong caching headers. |
| `Width` | `query` | `int` | 否 | 否 | The fixed image width to return. |

#### `HEAD` `/Users/{Id}/Images/{Type}`

- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Api.Images.ImageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Images.GetUserImage` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | User Id |
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
| `PositionTicks` | `query` | `long` | 否 | 否 | - |
| `Quality` | `query` | `int` | 否 | 否 | Optional quality setting, from 0-100. Defaults to 90 and should suffice in most cases. |
| `Tag` | `query` | `string` | 否 | 否 | Optional. Supply the cache tag from the item object to receive strong caching headers. |
| `Width` | `query` | `int` | 否 | 否 | The fixed image width to return. |

#### `POST` `/Users/{Id}/Images/{Type}`

- **摘要**：Uploads an image for an item, must be base64 encoded.
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Images.ImageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Images.PostUserImage` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | User Id |
| `Type` | `path` | `ImageType` | 是 | 否 | Image Type |
| `RequestStream` | `query` | `Stream` | 否 | 否 | - |

#### `POST` `/Users/{Id}/Images/{Type}/Delete`

- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Images.ImageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Images.DeleteUserImage` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | User Id |
| `Type` | `path` | `ImageType` | 是 | 否 | Image Type |
| `Index` | `query` | `int` | 否 | 否 | Image Index |

#### `DELETE` `/Users/{Id}/Images/{Type}/{Index}`

- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Images.ImageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Images.DeleteUserImage` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | User Id |
| `Type` | `path` | `ImageType` | 是 | 否 | Image Type |
| `Index` | `query` | `int` | 否 | 否 | Image Index |

#### `GET` `/Users/{Id}/Images/{Type}/{Index}`

- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Api.Images.ImageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Images.GetUserImage` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | User Id |
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
| `PositionTicks` | `query` | `long` | 否 | 否 | - |
| `Quality` | `query` | `int` | 否 | 否 | Optional quality setting, from 0-100. Defaults to 90 and should suffice in most cases. |
| `Tag` | `query` | `string` | 否 | 否 | Optional. Supply the cache tag from the item object to receive strong caching headers. |
| `Width` | `query` | `int` | 否 | 否 | The fixed image width to return. |

#### `HEAD` `/Users/{Id}/Images/{Type}/{Index}`

- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Api.Images.ImageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Images.GetUserImage` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | User Id |
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
| `PositionTicks` | `query` | `long` | 否 | 否 | - |
| `Quality` | `query` | `int` | 否 | 否 | Optional quality setting, from 0-100. Defaults to 90 and should suffice in most cases. |
| `Tag` | `query` | `string` | 否 | 否 | Optional. Supply the cache tag from the item object to receive strong caching headers. |
| `Width` | `query` | `int` | 否 | 否 | The fixed image width to return. |

#### `POST` `/Users/{Id}/Images/{Type}/{Index}`

- **摘要**：Uploads an image for an item, must be base64 encoded.
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Images.ImageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Images.PostUserImage` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | User Id |
| `Type` | `path` | `ImageType` | 是 | 否 | Image Type |
| `RequestStream` | `query` | `Stream` | 否 | 否 | - |

#### `POST` `/Users/{Id}/Images/{Type}/{Index}/Delete`

- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.Images.ImageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Images.DeleteUserImage` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | User Id |
| `Type` | `path` | `ImageType` | 是 | 否 | Image Type |
| `Index` | `query` | `int` | 否 | 否 | Image Index |

### ItemsService（2 条）

#### `GET` `/Users/{UserId}/Items`

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

#### `GET` `/Users/{UserId}/Items/Resume`

- **摘要**：Gets items based on a query.
- **鉴权**：需要认证
- **返回类型**：`QueryResult<BaseItemDto>`
- **处理服务**：`Emby.Api.UserLibrary.ItemsService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.GetResumeItems` (`Emby.Api.dll`)

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
| `IncludeNextUp` | `query` | `bool?` | 否 | 否 | - |
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

### PlaystateService（8 条）

#### `POST` `/Users/{UserId}/Items/{ItemId}/UserData`

- **摘要**：Updates userdata for an item
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Server.MediaEncoding.Api.PlaystateService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.UpdateUserData` (`Emby.Server.MediaEncoding.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `UserId` | `path` | `string` | 是 | 否 | User Id |
| `ItemId` | `path` | `string` | 否 | 否 | - |
| `IsFavorite` | `query` | `bool` | 否 | 否 | - |
| `Key` | `query` | `string` | 否 | 否 | - |
| `LastPlayedDate` | `query` | `DateTimeOffset?` | 否 | 否 | - |
| `PlaybackPositionTicks` | `query` | `long` | 否 | 否 | - |
| `PlayCount` | `query` | `int?` | 否 | 否 | - |
| `Played` | `query` | `bool` | 否 | 否 | - |
| `PlayedPercentage` | `query` | `double?` | 否 | 否 | - |
| `Rating` | `query` | `double?` | 否 | 否 | - |
| `ServerId` | `query` | `string` | 否 | 否 | - |
| `UnplayedItemCount` | `query` | `int?` | 否 | 否 | - |

#### `DELETE` `/Users/{UserId}/PlayedItems/{Id}`

- **摘要**：Marks an item as unplayed
- **鉴权**：需要认证
- **返回类型**：`UserItemDataDto`
- **处理服务**：`Emby.Server.MediaEncoding.Api.PlaystateService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.MarkUnplayedItem` (`Emby.Server.MediaEncoding.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `UserId` | `path` | `string` | 是 | 否 | User Id |

#### `POST` `/Users/{UserId}/PlayedItems/{Id}`

- **摘要**：Marks an item as played
- **鉴权**：需要认证
- **返回类型**：`UserItemDataDto`
- **处理服务**：`Emby.Server.MediaEncoding.Api.PlaystateService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.MarkPlayedItem` (`Emby.Server.MediaEncoding.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `UserId` | `path` | `string` | 是 | 否 | User Id |
| `DatePlayed` | `query` | `string` | 否 | 否 | The date the item was played (if any). Format = yyyyMMddHHmmss |

#### `POST` `/Users/{UserId}/PlayedItems/{Id}/Delete`

- **摘要**：Marks an item as unplayed
- **鉴权**：需要认证
- **返回类型**：`UserItemDataDto`
- **处理服务**：`Emby.Server.MediaEncoding.Api.PlaystateService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.MarkUnplayedItem` (`Emby.Server.MediaEncoding.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `UserId` | `path` | `string` | 是 | 否 | User Id |

#### `DELETE` `/Users/{UserId}/PlayingItems/{Id}`

- **摘要**：Reports that a user has stopped playing an item
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Server.MediaEncoding.Api.PlaystateService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.OnPlaybackStopped` (`Emby.Server.MediaEncoding.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `UserId` | `path` | `string` | 是 | 否 | User Id |
| `MediaSourceId` | `query` | `string` | 是 | 否 | The id of the MediaSource |
| `NextMediaType` | `query` | `string` | 是 | 否 | The next media type that will play |
| `LiveStreamId` | `query` | `string` | 否 | 否 | - |
| `PlaySessionId` | `query` | `string` | 否 | 否 | - |
| `PositionTicks` | `query` | `long?` | 否 | 否 | Optional. The position, in ticks, where playback stopped. 1ms = 10000 ticks. |

#### `POST` `/Users/{UserId}/PlayingItems/{Id}`

- **摘要**：Reports that a user has begun playing an item
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Server.MediaEncoding.Api.PlaystateService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.OnPlaybackStart` (`Emby.Server.MediaEncoding.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `UserId` | `path` | `string` | 是 | 否 | User Id |
| `MediaSourceId` | `query` | `string` | 是 | 否 | The id of the MediaSource |
| `AudioStreamIndex` | `query` | `int?` | 否 | 否 | - |
| `CanSeek` | `query` | `bool` | 否 | 否 | Indicates if the client can seek |
| `LiveStreamId` | `query` | `string` | 否 | 否 | - |
| `PlayMethod` | `query` | `PlayMethod` | 否 | 否 | - |
| `PlaySessionId` | `query` | `string` | 否 | 否 | - |
| `SubtitleStreamIndex` | `query` | `int?` | 否 | 否 | - |

#### `POST` `/Users/{UserId}/PlayingItems/{Id}/Delete`

- **摘要**：Reports that a user has stopped playing an item
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Server.MediaEncoding.Api.PlaystateService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.OnPlaybackStopped` (`Emby.Server.MediaEncoding.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `UserId` | `path` | `string` | 是 | 否 | User Id |
| `MediaSourceId` | `query` | `string` | 是 | 否 | The id of the MediaSource |
| `NextMediaType` | `query` | `string` | 是 | 否 | The next media type that will play |
| `LiveStreamId` | `query` | `string` | 否 | 否 | - |
| `PlaySessionId` | `query` | `string` | 否 | 否 | - |
| `PositionTicks` | `query` | `long?` | 否 | 否 | Optional. The position, in ticks, where playback stopped. 1ms = 10000 ticks. |

#### `POST` `/Users/{UserId}/PlayingItems/{Id}/Progress`

- **摘要**：Reports a user's playback progress
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Server.MediaEncoding.Api.PlaystateService` (Emby.Server.MediaEncoding.dll)
- **Request DTO**：`Emby.Server.MediaEncoding.Api.OnPlaybackProgress` (`Emby.Server.MediaEncoding.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `UserId` | `path` | `string` | 是 | 否 | User Id |
| `MediaSourceId` | `query` | `string` | 是 | 否 | The id of the MediaSource |
| `AudioStreamIndex` | `query` | `int?` | 否 | 否 | - |
| `EventName` | `query` | `ProgressEvent` | 否 | 否 | - |
| `IsMuted` | `query` | `bool` | 否 | 否 | Indicates if the player is muted. |
| `IsPaused` | `query` | `bool` | 否 | 否 | Indicates if the player is paused. |
| `LiveStreamId` | `query` | `string` | 否 | 否 | - |
| `PlaybackRate` | `query` | `double` | 否 | 否 | - |
| `PlaylistIndex` | `query` | `int` | 否 | 否 | - |
| `PlaylistLength` | `query` | `int` | 否 | 否 | - |
| `PlayMethod` | `query` | `PlayMethod` | 否 | 否 | - |
| `PlaySessionId` | `query` | `string` | 否 | 否 | - |
| `PositionTicks` | `query` | `long?` | 否 | 否 | Optional. The current position, in ticks. 1ms = 10000 ticks. |
| `RepeatMode` | `query` | `RepeatMode` | 否 | 否 | - |
| `Shuffle` | `query` | `bool` | 否 | 否 | - |
| `SleepTimerEndTime` | `query` | `DateTimeOffset?` | 否 | 否 | - |
| `SleepTimerMode` | `query` | `SleepTimerMode` | 否 | 否 | - |
| `SubtitleOffset` | `query` | `int` | 否 | 否 | - |
| `SubtitleStreamIndex` | `query` | `int?` | 否 | 否 | - |
| `VolumeLevel` | `query` | `int?` | 否 | 否 | Scale of 0-100 |

### SuggestionsService（1 条）

#### `GET` `/Users/{UserId}/Suggestions`

- **摘要**：Gets items based on a query.
- **鉴权**：需要认证
- **返回类型**：`QueryResult<BaseItemDto>`
- **处理服务**：`Emby.Api.SuggestionsService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetSuggestedItems` (`Emby.Api.dll`)

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

### UserLibraryService（14 条）

#### `DELETE` `/Users/{UserId}/FavoriteItems/{Id}`

- **摘要**：Unmarks an item as a favorite
- **鉴权**：需要认证
- **返回类型**：`UserItemDataDto`
- **处理服务**：`Emby.Api.UserLibrary.UserLibraryService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.UnmarkFavoriteItem` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `UserId` | `path` | `string` | 是 | 否 | User Id |

#### `POST` `/Users/{UserId}/FavoriteItems/{Id}`

- **摘要**：Marks an item as a favorite
- **鉴权**：需要认证
- **返回类型**：`UserItemDataDto`
- **处理服务**：`Emby.Api.UserLibrary.UserLibraryService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.MarkFavoriteItem` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `UserId` | `path` | `string` | 是 | 否 | User Id |

#### `POST` `/Users/{UserId}/FavoriteItems/{Id}/Delete`

- **摘要**：Unmarks an item as a favorite
- **鉴权**：需要认证
- **返回类型**：`UserItemDataDto`
- **处理服务**：`Emby.Api.UserLibrary.UserLibraryService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.UnmarkFavoriteItem` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `UserId` | `path` | `string` | 是 | 否 | User Id |

#### `GET` `/Users/{UserId}/Items/Latest`

- **摘要**：Gets latest media
- **鉴权**：需要认证
- **返回类型**：`BaseItemDto[]`
- **处理服务**：`Emby.Api.UserLibrary.UserLibraryService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.GetLatestMedia` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `UserId` | `path` | `string` | 是 | 否 | User Id |
| `EnableImages` | `query` | `bool?` | 否 | 否 | Optional, include image information in output |
| `EnableImageTypes` | `query` | `string` | 否 | 否 | Optional. The image types to include in the output. |
| `EnableUserData` | `query` | `bool?` | 否 | 否 | Optional, include user data |
| `Fields` | `query` | `string` | 否 | 是 | Optional. Specify additional fields of information to return in the output. This allows multiple, comma delimeted. Options: Chapters, DateCreated, Genres, HomePageUrl, IndexOptions, MediaStreams, Overview, ParentId, Path, People, ProviderIds, PrimaryImageAspectRatio, SortName, Studios, Taglines |
| `GroupItems` | `query` | `bool` | 否 | 否 | Whether or not to group items into a parent container. |
| `ImageTypeLimit` | `query` | `int?` | 否 | 否 | Optional, the max number of images to return, per image type |
| `IncludeItemTypes` | `query` | `string` | 否 | 是 | Optional. If specified, results will be filtered based on item type. This allows multiple, comma delimeted. |
| `IsFolder` | `query` | `bool?` | 否 | 否 | Filter by items that are folders, or not. |
| `IsPlayed` | `query` | `bool?` | 否 | 否 | Filter by items that are played, or not. |
| `ItemPersonTypes` | `query` | `PersonType[]` | 否 | 否 | - |
| `Limit` | `query` | `int` | 否 | 否 | Limit |
| `MediaTypes` | `query` | `string` | 否 | 是 | Optional filter by MediaType. Allows multiple, comma delimited. |
| `ParentId` | `query` | `string` | 否 | 否 | Specify this to localize the search to a specific item or folder. Omit to use the root |
| `StartIndex` | `query` | `int?` | 否 | 否 | - |
| `StartItemId` | `query` | `string` | 否 | 否 | - |

#### `GET` `/Users/{UserId}/Items/Root`

- **摘要**：Gets the root folder from a user's library
- **鉴权**：需要认证
- **返回类型**：`BaseItemDto`
- **处理服务**：`Emby.Api.UserLibrary.UserLibraryService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.GetRootFolder` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `UserId` | `path` | `Guid` | 是 | 否 | User Id |

#### `GET` `/Users/{UserId}/Items/{Id}`

- **摘要**：Gets an item from a user's library
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

#### `POST` `/Users/{UserId}/Items/{Id}/HideFromResume`

- **摘要**：Updates a user's hide from resume for an item
- **鉴权**：需要认证
- **返回类型**：`UserItemDataDto`
- **处理服务**：`Emby.Api.UserLibrary.UserLibraryService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.UpdateUserItemHideFromResume` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `UserId` | `path` | `string` | 是 | 否 | User Id |
| `Hide` | `query` | `bool` | 是 | 否 | Whether the item should be hidden from reusme or not. true/false |

#### `GET` `/Users/{UserId}/Items/{Id}/Intros`

- **摘要**：Gets intros to play before the main media item plays
- **鉴权**：需要认证
- **返回类型**：`QueryResult<BaseItemDto>`
- **处理服务**：`Emby.Api.UserLibrary.UserLibraryService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.GetIntros` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `UserId` | `path` | `Guid` | 是 | 否 | User Id |
| `EnableImages` | `query` | `bool?` | 否 | 否 | Optional, include image information in output |
| `EnableImageTypes` | `query` | `string` | 否 | 否 | Optional. The image types to include in the output. |
| `EnableUserData` | `query` | `bool?` | 否 | 否 | Optional, include user data |
| `ExcludeSources` | `query` | `string[]` | 否 | 否 | - |
| `Fields` | `query` | `string` | 否 | 是 | Optional. Specify additional fields of information to return in the output. This allows multiple, comma delimeted. Options: Budget, Chapters, DateCreated, Genres, HomePageUrl, IndexOptions, MediaStreams, Overview, ParentId, Path, People, ProviderIds, PrimaryImageAspectRatio, Revenue, SortName, Studios, Taglines, TrailerUrls |
| `ImageTypeLimit` | `query` | `int?` | 否 | 否 | Optional, the max number of images to return, per image type |
| `ItemPersonTypes` | `query` | `PersonType[]` | 否 | 否 | - |

#### `GET` `/Users/{UserId}/Items/{Id}/LocalTrailers`

- **摘要**：Gets local trailers for an item
- **鉴权**：需要认证
- **返回类型**：`BaseItemDto[]`
- **处理服务**：`Emby.Api.UserLibrary.UserLibraryService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.GetLocalTrailers` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `UserId` | `path` | `Guid` | 是 | 否 | User Id |
| `EnableImages` | `query` | `bool?` | 否 | 否 | Optional, include image information in output |
| `EnableImageTypes` | `query` | `string` | 否 | 否 | Optional. The image types to include in the output. |
| `EnableUserData` | `query` | `bool?` | 否 | 否 | Optional, include user data |
| `Fields` | `query` | `string` | 否 | 是 | Optional. Specify additional fields of information to return in the output. This allows multiple, comma delimeted. Options: Budget, Chapters, DateCreated, Genres, HomePageUrl, IndexOptions, MediaStreams, Overview, ParentId, Path, People, ProviderIds, PrimaryImageAspectRatio, Revenue, SortName, Studios, Taglines, TrailerUrls |
| `ImageTypeLimit` | `query` | `int?` | 否 | 否 | Optional, the max number of images to return, per image type |
| `ItemPersonTypes` | `query` | `PersonType[]` | 否 | 否 | - |

#### `DELETE` `/Users/{UserId}/Items/{Id}/Rating`

- **摘要**：Deletes a user's saved personal rating for an item
- **鉴权**：需要认证
- **返回类型**：`UserItemDataDto`
- **处理服务**：`Emby.Api.UserLibrary.UserLibraryService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.DeleteUserItemRating` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `UserId` | `path` | `string` | 是 | 否 | User Id |

#### `POST` `/Users/{UserId}/Items/{Id}/Rating`

- **摘要**：Updates a user's rating for an item
- **鉴权**：需要认证
- **返回类型**：`UserItemDataDto`
- **处理服务**：`Emby.Api.UserLibrary.UserLibraryService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.UpdateUserItemRating` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `UserId` | `path` | `string` | 是 | 否 | User Id |
| `Likes` | `query` | `bool` | 是 | 否 | Whether the user likes the item or not. true/false |

#### `POST` `/Users/{UserId}/Items/{Id}/Rating/Delete`

- **摘要**：Deletes a user's saved personal rating for an item
- **鉴权**：需要认证
- **返回类型**：`UserItemDataDto`
- **处理服务**：`Emby.Api.UserLibrary.UserLibraryService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.DeleteUserItemRating` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Item Id |
| `UserId` | `path` | `string` | 是 | 否 | User Id |

#### `GET` `/Users/{UserId}/Items/{Id}/SpecialFeatures`

- **摘要**：Gets special features for an item
- **鉴权**：需要认证
- **返回类型**：`BaseItemDto[]`
- **处理服务**：`Emby.Api.UserLibrary.UserLibraryService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.GetSpecialFeatures` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 是 | 否 | Movie Id |
| `UserId` | `path` | `Guid` | 是 | 否 | User Id |
| `EnableImages` | `query` | `bool?` | 否 | 否 | Optional, include image information in output |
| `EnableImageTypes` | `query` | `string` | 否 | 否 | Optional. The image types to include in the output. |
| `EnableUserData` | `query` | `bool?` | 否 | 否 | Optional, include user data |
| `Fields` | `query` | `string` | 否 | 是 | Optional. Specify additional fields of information to return in the output. This allows multiple, comma delimeted. Options: Budget, Chapters, DateCreated, Genres, HomePageUrl, IndexOptions, MediaStreams, Overview, ParentId, Path, People, ProviderIds, PrimaryImageAspectRatio, Revenue, SortName, Studios, Taglines, TrailerUrls |
| `ImageTypeLimit` | `query` | `int?` | 否 | 否 | Optional, the max number of images to return, per image type |
| `ItemPersonTypes` | `query` | `PersonType[]` | 否 | 否 | - |

#### `POST` `/Users/{UserId}/SearchedItems/`

- **摘要**：Marks an item as a favorite
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.UserLibrary.UserLibraryService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.ReportItemsSearched` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Ids` | `path` | `string[]` | 是 | 否 | Item Id |
| `UserId` | `path` | `string` | 是 | 否 | User Id |
| `WasSearched` | `query` | `bool` | 否 | 否 | - |

### UserService（26 条）

#### `GET` `/Users` `[隐藏接口]`

- **摘要**：Gets a list of users
- **鉴权**：需要认证；角色要求: `admin`
- **返回类型**：`UserDto[]`
- **处理服务**：`Emby.Api.UserService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetUsers` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `IsDisabled` | `query` | `bool?` | 否 | 否 | Optional filter by IsDisabled=true or false |
| `IsHidden` | `query` | `bool?` | 否 | 否 | Optional filter by IsHidden=true or false |

#### `POST` `/Users/AuthenticateByName`

- **摘要**：Authenticates a user
- **说明**：Authenticate a user by nane and password. A 200 status code indicates success, while anything in the 400 or 500 range indicates failure
- **鉴权**：无需认证（公开接口）
- **返回类型**：`AuthenticationResult`
- **处理服务**：`Emby.Api.UserService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.AuthenticateUserByName` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Pw` | `body` | `string` | 是 | 否 | Password in plain text |
| `Username` | `body` | `string` | 是 | 否 | - |
| `X-Emby-Authorization` | `header` | `string` | 是 | 否 | The authorization header can be either named 'Authorization' or 'X-Emby-Authorization'.    It must be of the following schema:     Emby UserId="(guid)", Client="(string)", Device="(string)", DeviceId="(string)", Version="string", Token="(string)"     Please consult the documentation for further details. |

#### `GET` `/Users/CopyDataOptions`

- **摘要**：Gets copy data options
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`FullUserCopyDataOptions`
- **处理服务**：`Emby.Api.UserService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetUserCopyDataOptions` (`Emby.Api.dll`)

_无请求参数。_

#### `POST` `/Users/ForgotPassword`

- **摘要**：Initiates the forgot password process for a local user
- **鉴权**：无需认证（公开接口）
- **返回类型**：`ForgotPasswordResult`
- **处理服务**：`Emby.Api.UserService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.ForgotPassword` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `EnteredUsername` | `body` | `string` | 否 | 否 | - |

#### `POST` `/Users/ForgotPassword/Pin`

- **摘要**：Redeems a forgot password pin
- **鉴权**：无需认证（公开接口）
- **返回类型**：`PinRedeemResult`
- **处理服务**：`Emby.Api.UserService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.ForgotPasswordPin` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Pin` | `body` | `string` | 否 | 否 | - |

#### `GET` `/Users/ItemAccess`

- **摘要**：Gets a list of users
- **鉴权**：需要认证
- **返回类型**：`QueryResult<UserDto>`
- **处理服务**：`Emby.Api.UserService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetUsersForItemAccess` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `ExcludeUserIds` | `query` | `string` | 否 | 否 | - |
| `Ids` | `query` | `string` | 否 | 否 | - |
| `IsDisabled` | `query` | `bool?` | 否 | 否 | Optional filter by IsDisabled=true or false |
| `IsHidden` | `query` | `bool?` | 否 | 否 | Optional filter by IsHidden=true or false |
| `ItemId` | `query` | `string` | 否 | 否 | - |
| `Limit` | `query` | `int?` | 否 | 否 | Optional. The maximum number of records to return |
| `NameStartsWithOrGreater` | `query` | `string` | 否 | 否 | Optional filter by items whose name is sorted equally or greater than a given input string. |
| `SortBy` | `query` | `string` | 否 | 否 | - |
| `SortOrder` | `query` | `string` | 否 | 否 | Sort Order - Ascending,Descending |
| `StartIndex` | `query` | `int` | 否 | 否 | Optional. The record index to start at. All items with a lower index will be dropped from the results. |

#### `POST` `/Users/New`

- **摘要**：Creates a user
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`UserDto`
- **处理服务**：`Emby.Api.UserService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.CreateUserByName` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `UserCopyOptions` | `query` | `string[]` | 否 | 否 | - |
| `Name` | `body` | `string` | 是 | 否 | - |
| `CopyFromUserId` | `body` | `string` | 否 | 否 | - |

#### `GET` `/Users/Prefixes`

- **摘要**：Gets a list of users
- **鉴权**：需要认证；角色要求: `admin`
- **返回类型**：`NameIdPair[]`
- **处理服务**：`Emby.Api.UserService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetUsersPrefixesResult` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `ExcludeUserIds` | `query` | `string` | 否 | 否 | - |
| `Ids` | `query` | `string` | 否 | 否 | - |
| `IsDisabled` | `query` | `bool?` | 否 | 否 | Optional filter by IsDisabled=true or false |
| `IsHidden` | `query` | `bool?` | 否 | 否 | Optional filter by IsHidden=true or false |
| `Limit` | `query` | `int?` | 否 | 否 | Optional. The maximum number of records to return |
| `NameStartsWithOrGreater` | `query` | `string` | 否 | 否 | Optional filter by items whose name is sorted equally or greater than a given input string. |
| `SortBy` | `query` | `string` | 否 | 否 | - |
| `SortOrder` | `query` | `string` | 否 | 否 | Sort Order - Ascending,Descending |
| `StartIndex` | `query` | `int` | 否 | 否 | Optional. The record index to start at. All items with a lower index will be dropped from the results. |

#### `GET` `/Users/Public`

- **摘要**：Gets a list of publicly visible users for display on a login screen.
- **鉴权**：无需认证（公开接口）
- **返回类型**：`UserDto[]`
- **处理服务**：`Emby.Api.UserService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetPublicUsers` (`Emby.Api.dll`)

_无请求参数。_

#### `GET` `/Users/Query`

- **摘要**：Gets a list of users
- **鉴权**：需要认证；角色要求: `admin`
- **返回类型**：`QueryResult<UserDto>`
- **处理服务**：`Emby.Api.UserService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetUsersResult` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `ExcludeUserIds` | `query` | `string` | 否 | 否 | - |
| `Ids` | `query` | `string` | 否 | 否 | - |
| `IsDisabled` | `query` | `bool?` | 否 | 否 | Optional filter by IsDisabled=true or false |
| `IsHidden` | `query` | `bool?` | 否 | 否 | Optional filter by IsHidden=true or false |
| `Limit` | `query` | `int?` | 否 | 否 | Optional. The maximum number of records to return |
| `NameStartsWithOrGreater` | `query` | `string` | 否 | 否 | Optional filter by items whose name is sorted equally or greater than a given input string. |
| `SortBy` | `query` | `string` | 否 | 否 | - |
| `SortOrder` | `query` | `string` | 否 | 否 | Sort Order - Ascending,Descending |
| `StartIndex` | `query` | `int` | 否 | 否 | Optional. The record index to start at. All items with a lower index will be dropped from the results. |

#### `DELETE` `/Users/{Id}`

- **摘要**：Deletes a user
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`void`
- **处理服务**：`Emby.Api.UserService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.DeleteUser` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `User Id` | `path` | `string` | 是 | 否 | - |

#### `GET` `/Users/{Id}`

- **摘要**：Gets a user by Id
- **鉴权**：需要认证
- **返回类型**：`UserDto`
- **处理服务**：`Emby.Api.UserService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetUser` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `User Id` | `path` | `string` | 是 | 否 | - |

#### `POST` `/Users/{Id}`

- **摘要**：Updates a user
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.UserService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UpdateUser` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 否 | 否 | - |
| `Configuration` | `query` | `UserConfiguration` | 否 | 否 | - |
| `ConnectLinkType` | `query` | `UserLinkType?` | 否 | 否 | - |
| `ConnectUserName` | `query` | `string` | 否 | 否 | - |
| `DateCreated` | `query` | `DateTimeOffset?` | 否 | 否 | - |
| `EnableAutoLogin` | `query` | `bool?` | 否 | 否 | - |
| `HasConfiguredPassword` | `query` | `bool?` | 否 | 否 | - |
| `HasPassword` | `query` | `bool?` | 否 | 否 | - |
| `LastActivityDate` | `query` | `DateTimeOffset?` | 否 | 否 | - |
| `LastLoginDate` | `query` | `DateTimeOffset?` | 否 | 否 | - |
| `Name` | `query` | `string` | 否 | 否 | - |
| `Policy` | `query` | `UserPolicy` | 否 | 否 | - |
| `Prefix` | `query` | `string` | 否 | 否 | - |
| `PrimaryImageAspectRatio` | `query` | `double?` | 否 | 否 | - |
| `PrimaryImageTag` | `query` | `string` | 否 | 否 | - |
| `ServerId` | `query` | `string` | 否 | 否 | - |
| `ServerName` | `query` | `string` | 否 | 否 | - |
| `UserItemShareLevel` | `query` | `UserItemShareLevel?` | 否 | 否 | - |

#### `POST` `/Users/{Id}/Authenticate`

- **摘要**：Authenticates a user
- **鉴权**：无需认证（公开接口）
- **返回类型**：`AuthenticationResult`
- **处理服务**：`Emby.Api.UserService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.AuthenticateUser` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `User Id` | `path` | `string` | 是 | 否 | - |
| `Pw` | `body` | `string` | 是 | 否 | - |

#### `POST` `/Users/{Id}/Configuration`

- **摘要**：Updates a user configuration
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.UserService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UpdateUserConfiguration` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `User Id` | `path` | `string` | 是 | 否 | - |
| `AudioLanguagePreference` | `query` | `string` | 否 | 否 | - |
| `DisplayMissingEpisodes` | `query` | `bool` | 否 | 否 | - |
| `EnableLocalPassword` | `query` | `bool` | 否 | 否 | - |
| `EnableNextEpisodeAutoPlay` | `query` | `bool` | 否 | 否 | - |
| `HidePlayedInLatest` | `query` | `bool` | 否 | 否 | - |
| `HidePlayedInMoreLikeThis` | `query` | `bool` | 否 | 否 | - |
| `HidePlayedInSuggestions` | `query` | `bool` | 否 | 否 | - |
| `IntroSkipMode` | `query` | `SegmentSkipMode` | 否 | 否 | - |
| `LatestItemsExcludes` | `query` | `string[]` | 否 | 否 | - |
| `MyMediaExcludes` | `query` | `string[]` | 否 | 否 | - |
| `OrderedViews` | `query` | `string[]` | 否 | 否 | - |
| `PlayDefaultAudioTrack` | `query` | `bool` | 否 | 否 | - |
| `ProfilePin` | `query` | `string` | 否 | 否 | - |
| `RememberAudioSelections` | `query` | `bool` | 否 | 否 | - |
| `RememberSubtitleSelections` | `query` | `bool` | 否 | 否 | - |
| `ResumeRewindSeconds` | `query` | `int` | 否 | 否 | - |
| `SubtitleLanguagePreference` | `query` | `string` | 否 | 否 | - |
| `SubtitleMode` | `query` | `SubtitlePlaybackMode` | 否 | 否 | - |

#### `POST` `/Users/{Id}/Configuration/Partial`

- **摘要**：Updates a user configuration
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.UserService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UpdateUserPartialConfiguration` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `User Id` | `path` | `string` | 是 | 否 | - |
| `RequestStream` | `query` | `Stream` | 否 | 否 | - |

#### `POST` `/Users/{Id}/Delete`

- **摘要**：Deletes a user
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`void`
- **处理服务**：`Emby.Api.UserService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.DeleteUser` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `User Id` | `path` | `string` | 是 | 否 | - |

#### `POST` `/Users/{Id}/Password`

- **摘要**：Updates a user's password
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.UserService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UpdateUserPassword` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `path` | `string` | 否 | 否 | - |
| `NewPw` | `query` | `string` | 否 | 否 | - |
| `ResetPassword` | `query` | `bool` | 否 | 否 | - |

#### `POST` `/Users/{Id}/Policy`

- **摘要**：Updates a user policy
- **鉴权**：需要认证；角色要求: `admin`
- **返回类型**：`void`
- **处理服务**：`Emby.Api.UserService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UpdateUserPolicy` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `User Id` | `path` | `string` | 是 | 否 | - |
| `AccessSchedules` | `query` | `AccessSchedule[]` | 否 | 否 | - |
| `AllowCameraUpload` | `query` | `bool` | 否 | 否 | - |
| `AllowSharingPersonalItems` | `query` | `bool` | 否 | 否 | - |
| `AllowTagOrRating` | `query` | `bool` | 否 | 否 | - |
| `AuthenticationProviderId` | `query` | `string` | 否 | 否 | - |
| `AutoRemoteQuality` | `query` | `int` | 否 | 否 | - |
| `BlockedTags` | `query` | `string[]` | 否 | 否 | - |
| `BlockUnratedItems` | `query` | `UnratedItem[]` | 否 | 否 | - |
| `EnableAllChannels` | `query` | `bool` | 否 | 否 | - |
| `EnableAllDevices` | `query` | `bool` | 否 | 否 | - |
| `EnableAllFolders` | `query` | `bool` | 否 | 否 | - |
| `EnableAudioPlaybackTranscoding` | `query` | `bool` | 否 | 否 | - |
| `EnableContentDeletion` | `query` | `bool` | 否 | 否 | - |
| `EnableContentDeletionFromFolders` | `query` | `string[]` | 否 | 否 | - |
| `EnableContentDownloading` | `query` | `bool` | 否 | 否 | - |
| `EnabledChannels` | `query` | `string[]` | 否 | 否 | - |
| `EnabledDevices` | `query` | `string[]` | 否 | 否 | - |
| `EnabledFolders` | `query` | `string[]` | 否 | 否 | - |
| `EnableLiveTvAccess` | `query` | `bool` | 否 | 否 | - |
| `EnableLiveTvManagement` | `query` | `bool` | 否 | 否 | - |
| `EnableMediaConversion` | `query` | `bool` | 否 | 否 | - |
| `EnableMediaPlayback` | `query` | `bool` | 否 | 否 | - |
| `EnablePlaybackRemuxing` | `query` | `bool` | 否 | 否 | - |
| `EnablePublicSharing` | `query` | `bool` | 否 | 否 | - |
| `EnableRemoteAccess` | `query` | `bool` | 否 | 否 | - |
| `EnableRemoteControlOfOtherUsers` | `query` | `bool` | 否 | 否 | - |
| `EnableSharedDeviceControl` | `query` | `bool` | 否 | 否 | - |
| `EnableSubtitleDownloading` | `query` | `bool` | 否 | 否 | - |
| `EnableSubtitleManagement` | `query` | `bool` | 否 | 否 | - |
| `EnableSyncTranscoding` | `query` | `bool` | 否 | 否 | - |
| `EnableTranscodingQuality` | `query` | `bool` | 否 | 否 | - |
| `EnableUserPreferenceAccess` | `query` | `bool` | 否 | 否 | - |
| `EnableVideoPlaybackTranscoding` | `query` | `bool` | 否 | 否 | - |
| `ExcludedSubFolders` | `query` | `string[]` | 否 | 否 | - |
| `IncludeTags` | `query` | `string[]` | 否 | 否 | - |
| `InvalidLoginAttemptCount` | `query` | `int` | 否 | 否 | - |
| `IsAdministrator` | `query` | `bool` | 否 | 否 | - |
| `IsDisabled` | `query` | `bool` | 否 | 否 | - |
| `IsHidden` | `query` | `bool` | 否 | 否 | - |
| `IsHiddenFromUnusedDevices` | `query` | `bool` | 否 | 否 | - |
| `IsHiddenRemotely` | `query` | `bool` | 否 | 否 | - |
| `IsTagBlockingModeInclusive` | `query` | `bool` | 否 | 否 | - |
| `LockedOutDate` | `query` | `long` | 否 | 否 | - |
| `MaxParentalRating` | `query` | `int?` | 否 | 否 | - |
| `RemoteClientBitrateLimit` | `query` | `int` | 否 | 否 | - |
| `RestrictedFeatures` | `query` | `string[]` | 否 | 否 | - |
| `SimultaneousStreamLimit` | `query` | `int` | 否 | 否 | - |

#### `DELETE` `/Users/{Id}/RecentlySearched`

- **摘要**：Clears recently searched
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.UserService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.ClearRecentlySearched` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `User Id` | `path` | `string` | 是 | 否 | - |

#### `POST` `/Users/{Id}/RecentlySearched/Delete`

- **摘要**：Clears recently searched
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.UserService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.ClearRecentlySearched` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `User Id` | `path` | `string` | 是 | 否 | - |

#### `DELETE` `/Users/{Id}/TrackSelections/{TrackType}`

- **摘要**：Clears audio or subtitle track selections for a user
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.UserService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.ClearUserTrackSelections` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `TrackType` | `path` | `string` | 是 | 否 | - |
| `User Id` | `path` | `string` | 是 | 否 | - |

#### `POST` `/Users/{Id}/TrackSelections/{TrackType}/Delete`

- **摘要**：Clears audio or subtitle track selections for a user
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.UserService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.ClearUserTrackSelections` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `TrackType` | `path` | `string` | 是 | 否 | - |
| `User Id` | `path` | `string` | 是 | 否 | - |

#### `POST` `/Users/{UserId}/CopyData`

- **摘要**：Copies data from one user to another
- **鉴权**：需要认证；角色要求: `Admin`
- **返回类型**：`void`
- **处理服务**：`Emby.Api.UserService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.CopyData` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `UserId` | `path` | `string` | 否 | 否 | - |
| `CopyOptions` | `query` | `string[]` | 否 | 否 | - |
| `ToUserIds` | `query` | `string[]` | 否 | 否 | - |

#### `GET` `/Users/{UserId}/TypedSettings/{Key}`

- **摘要**：Gets a typed user setting
- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Api.UserService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetTypedUserSetting` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Key` | `path` | `string` | 是 | 否 | Key |
| `UserId` | `path` | `string` | 否 | 否 | - |

#### `POST` `/Users/{UserId}/TypedSettings/{Key}`

- **摘要**：Updates a typed user setting
- **鉴权**：需要认证
- **返回类型**：`void`
- **处理服务**：`Emby.Api.UserService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UpdateTypedUserSetting` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Key` | `path` | `string` | 是 | 否 | Key |
| `UserId` | `path` | `string` | 否 | 否 | - |
| `RequestStream` | `query` | `Stream` | 否 | 否 | - |

### UserViewsService（2 条）

#### `GET` `/Users/{UserId}/GroupingOptions` `[隐藏接口]`

- **鉴权**：需要认证
- **返回类型**：`BaseItemDto[]`
- **处理服务**：`Emby.Api.UserLibrary.UserViewsService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.GetGroupingOptions` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `UserId` | `path` | `Guid` | 是 | 否 | User Id |

#### `GET` `/Users/{UserId}/Views`

- **鉴权**：需要认证
- **返回类型**：`QueryResult<BaseItemDto>`
- **处理服务**：`Emby.Api.UserLibrary.UserViewsService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.GetUserViews` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `UserId` | `path` | `string` | 是 | 否 | User Id |
| `IncludeExternalContent` | `query` | `bool?` | 是 | 否 | Whether or not to include external views such as channels or live tv |
| `AllowDynamicChildren` | `query` | `bool` | 否 | 否 | - |
| `IncludeHidden` | `query` | `bool` | 否 | 否 | - |
