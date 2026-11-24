# 影视、剧集、音乐、有声书、游戏与频道

> 所属模块：**03. 媒体库、条目与元数据**  
> 路由前缀：`/Albums`, `/AudioBooks`, `/Channels`, `/Games`, `/Movies`, `/Shows`, `/Songs`, `/Trailers`  
> 接口总数：**18** 条

涵盖电影推荐、剧集/季/下一集、预告片、专辑、歌曲、有声书、游戏与频道接口。

[← 返回总目录](../README.md)

## 接口总览

| # | 方法 | 路径 | 鉴权 | 返回类型 | 摘要 |
| :---: | :--- | :--- | :--- | :--- | :--- |
| 1 | `GET` | `/Albums/{Id}/InstantMix` | 需认证 | `QueryResult<BaseItemDto>` | Creates an instant playlist based on a given album |
| 2 | `GET` | `/Albums/{Id}/Similar` | 需认证 | `QueryResult<BaseItemDto>` | Finds albums similar to a given album. |
| 3 | `GET` | `/AudioBooks/NextUp` | 需认证 | `QueryResult<BaseItemDto>` | Gets a list of next up episodes |
| 4 | `GET` | `/Channels` | 需认证 | `QueryResult<BaseItemDto>` | Gets available channels |
| 5 | `GET` | `/Channels/Items/Latest` *(隐藏)* | 需认证 | `object` | Gets channel items |
| 6 | `GET` | `/Channels/{Id}/Items` *(隐藏)* | 需认证 | `object` | Gets channel items |
| 7 | `GET` | `/Games/{Id}/Similar` | 需认证 | `QueryResult<BaseItemDto>` | Finds games similar to a given game. |
| 8 | `GET` | `/Movies/Recommendations` | 需认证 | `RecommendationDto[]` | Gets movie recommendations |
| 9 | `GET` | `/Movies/{Id}/Similar` | 需认证 | `QueryResult<BaseItemDto>` | Finds movies and trailers similar to a given movie. |
| 10 | `GET` | `/Shows/Missing` | 需认证 | `object` | Gets a list of missing episodes |
| 11 | `GET` | `/Shows/NextUp` | 需认证 | `QueryResult<BaseItemDto>` | Gets a list of next up episodes |
| 12 | `GET` | `/Shows/Upcoming` | 需认证 | `object` | Gets a list of upcoming episodes |
| 13 | `GET` | `/Shows/{Id}/Episodes` | 需认证 | `object` | Gets episodes for a tv season |
| 14 | `GET` | `/Shows/{Id}/Seasons` | 需认证 | `QueryResult<BaseItemDto>` | Gets seasons for a tv series |
| 15 | `GET` | `/Shows/{Id}/Similar` | 需认证 | `QueryResult<BaseItemDto>` | Finds tv shows similar to a given one. |
| 16 | `GET` | `/Songs/{Id}/InstantMix` | 需认证 | `QueryResult<BaseItemDto>` | Creates an instant playlist based on a given song |
| 17 | `GET` | `/Trailers` | 需认证 | `QueryResult<BaseItemDto>` | Finds movies and trailers similar to a given trailer. |
| 18 | `GET` | `/Trailers/{Id}/Similar` | 需认证 | `QueryResult<BaseItemDto>` | Finds movies and trailers similar to a given trailer. |

## 接口详情

### ChannelService（3 条）

#### `GET` `/Channels`

- **摘要**：Gets available channels
- **鉴权**：需要认证
- **返回类型**：`QueryResult<BaseItemDto>`
- **处理服务**：`Emby.Api.ChannelService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetChannels` (`Emby.Api.dll`)

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

#### `GET` `/Channels/Items/Latest` `[隐藏接口]`

- **摘要**：Gets channel items
- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Api.ChannelService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetLatestChannelItems` (`Emby.Api.dll`)

_无请求参数。_

#### `GET` `/Channels/{Id}/Items` `[隐藏接口]`

- **摘要**：Gets channel items
- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Api.ChannelService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetChannelItems` (`Emby.Api.dll`)

_无请求参数。_

### InstantMixService（3 条）

#### `GET` `/Albums/{Id}/InstantMix`

- **摘要**：Creates an instant playlist based on a given album
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

#### `GET` `/AudioBooks/NextUp`

- **摘要**：Gets a list of next up episodes
- **鉴权**：需要认证
- **返回类型**：`QueryResult<BaseItemDto>`
- **处理服务**：`Emby.Api.Music.InstantMixService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Music.GetNextUpAudioBookItems` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `UserId` | `query` | `string` | 是 | 否 | User Id |
| `AlbumId` | `query` | `string` | 否 | 否 | Optional. Filter by series id |
| `EnableImages` | `query` | `bool?` | 否 | 否 | Optional, include image information in output |
| `EnableImageTypes` | `query` | `string` | 否 | 否 | Optional. The image types to include in the output. |
| `EnableTotalRecordCount` | `query` | `bool` | 否 | 否 | - |
| `EnableUserData` | `query` | `bool?` | 否 | 否 | Optional, include user data |
| `Fields` | `query` | `string` | 否 | 是 | Optional. Specify additional fields of information to return in the output. This allows multiple, comma delimeted. Options: Budget, Chapters, DateCreated, Genres, HomePageUrl, IndexOptions, MediaStreams, Overview, ParentId, Path, People, ProviderIds, PrimaryImageAspectRatio, Revenue, SortName, Studios, Taglines, TrailerUrls |
| `ImageTypeLimit` | `query` | `int?` | 否 | 否 | Optional, the max number of images to return, per image type |
| `ItemPersonTypes` | `query` | `PersonType[]` | 否 | 否 | - |
| `Limit` | `query` | `int?` | 否 | 否 | Optional. The maximum number of records to return |
| `ParentId` | `query` | `string` | 否 | 否 | Specify this to localize the search to a specific item or folder. Omit to use the root |
| `StartIndex` | `query` | `int?` | 否 | 否 | Optional. The record index to start at. All items with a lower index will be dropped from the results. |

#### `GET` `/Songs/{Id}/InstantMix`

- **摘要**：Creates an instant playlist based on a given song
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

### LibraryService（5 条）

#### `GET` `/Albums/{Id}/Similar`

- **摘要**：Finds albums similar to a given album.
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

#### `GET` `/Games/{Id}/Similar`

- **摘要**：Finds games similar to a given game.
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

#### `GET` `/Movies/{Id}/Similar`

- **摘要**：Finds movies and trailers similar to a given movie.
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

#### `GET` `/Shows/{Id}/Similar`

- **摘要**：Finds tv shows similar to a given one.
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

#### `GET` `/Trailers/{Id}/Similar`

- **摘要**：Finds movies and trailers similar to a given trailer.
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

### MoviesService（1 条）

#### `GET` `/Movies/Recommendations`

- **摘要**：Gets movie recommendations
- **鉴权**：需要认证
- **返回类型**：`RecommendationDto[]`
- **处理服务**：`Emby.Api.Movies.MoviesService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Movies.GetMovieRecommendations` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `CategoryLimit` | `query` | `int` | 否 | 否 | The max number of categories to return |
| `EnableImages` | `query` | `bool?` | 否 | 否 | Optional, include image information in output |
| `EnableImageTypes` | `query` | `string` | 否 | 否 | Optional. The image types to include in the output. |
| `EnableUserData` | `query` | `bool?` | 否 | 否 | Optional, include user data |
| `Fields` | `query` | `string` | 否 | 否 | - |
| `GroupProgramsBySeries` | `query` | `bool` | 否 | 否 | - |
| `ImageTypeLimit` | `query` | `int?` | 否 | 否 | Optional, the max number of images to return, per image type |
| `ItemLimit` | `query` | `int` | 否 | 否 | The max number of items to return per category |
| `ItemPersonTypes` | `query` | `PersonType[]` | 否 | 否 | - |
| `ParentId` | `query` | `string` | 否 | 否 | Specify this to localize the search to a specific item or folder. Omit to use the root |
| `UserId` | `query` | `string` | 否 | 否 | Optional. Filter by user id, and attach user data |

### TrailersService（1 条）

#### `GET` `/Trailers`

- **摘要**：Finds movies and trailers similar to a given trailer.
- **鉴权**：需要认证
- **返回类型**：`QueryResult<BaseItemDto>`
- **处理服务**：`Emby.Api.Movies.TrailersService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Movies.Getrailers` (`Emby.Api.dll`)

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

### TvShowsService（5 条）

#### `GET` `/Shows/Missing`

- **摘要**：Gets a list of missing episodes
- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Api.TvShowsService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetMissingEpisodes` (`Emby.Api.dll`)

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
| `IncludeSpecials` | `query` | `bool` | 否 | 否 | - |
| `IncludeUnaired` | `query` | `bool` | 否 | 否 | - |
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

#### `GET` `/Shows/NextUp`

- **摘要**：Gets a list of next up episodes
- **鉴权**：需要认证
- **返回类型**：`QueryResult<BaseItemDto>`
- **处理服务**：`Emby.Api.TvShowsService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetNextUpEpisodes` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `UserId` | `query` | `string` | 是 | 否 | User Id |
| `EnableImages` | `query` | `bool?` | 否 | 否 | Optional, include image information in output |
| `EnableImageTypes` | `query` | `string` | 否 | 否 | Optional. The image types to include in the output. |
| `EnableTotalRecordCount` | `query` | `bool` | 否 | 否 | - |
| `EnableUserData` | `query` | `bool?` | 否 | 否 | Optional, include user data |
| `Fields` | `query` | `string` | 否 | 是 | Optional. Specify additional fields of information to return in the output. This allows multiple, comma delimeted. Options: Budget, Chapters, DateCreated, Genres, HomePageUrl, IndexOptions, MediaStreams, Overview, ParentId, Path, People, ProviderIds, PrimaryImageAspectRatio, Revenue, SortName, Studios, Taglines, TrailerUrls |
| `ImageTypeLimit` | `query` | `int?` | 否 | 否 | Optional, the max number of images to return, per image type |
| `ItemPersonTypes` | `query` | `PersonType[]` | 否 | 否 | - |
| `LegacyNextUp` | `query` | `bool` | 否 | 否 | - |
| `Limit` | `query` | `int?` | 否 | 否 | Optional. The maximum number of records to return |
| `ParentId` | `query` | `string` | 否 | 否 | Specify this to localize the search to a specific item or folder. Omit to use the root |
| `SeriesId` | `query` | `string` | 否 | 否 | Optional. Filter by series id |
| `StartIndex` | `query` | `int?` | 否 | 否 | Optional. The record index to start at. All items with a lower index will be dropped from the results. |

#### `GET` `/Shows/Upcoming`

- **摘要**：Gets a list of upcoming episodes
- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Api.TvShowsService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetUpcomingEpisodes` (`Emby.Api.dll`)

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

#### `GET` `/Shows/{Id}/Episodes`

- **摘要**：Gets episodes for a tv season
- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Api.TvShowsService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetEpisodes` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `query` | `string` | 是 | 否 | The series id |
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
| `Season` | `query` | `int?` | 否 | 否 | Optional filter by season number. |
| `SeasonId` | `query` | `string` | 否 | 否 | Optional. Filter by season id |
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

#### `GET` `/Shows/{Id}/Seasons`

- **摘要**：Gets seasons for a tv series
- **鉴权**：需要认证
- **返回类型**：`QueryResult<BaseItemDto>`
- **处理服务**：`Emby.Api.TvShowsService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.GetSeasons` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `query` | `string` | 是 | 否 | The series id |
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
