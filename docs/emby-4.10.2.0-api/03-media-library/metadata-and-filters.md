# 演职员、流派、工作室、标签与媒体筛选维度

> 所属模块：**03. 媒体库、条目与元数据**  
> 路由前缀：`/Artists`, `/AudioCodecs`, `/AudioLayouts`, `/Containers`, `/ExtendedVideoTypes`, `/GameGenres`, `/Genres`, `/ItemTypes`, `/MusicGenres`, `/OfficialRatings`, `/Persons`, `/StreamLanguages`, `/Studios`, `/SubtitleCodecs`, `/Tags`, `/VideoCodecs`, `/Years`  
> 接口总数：**54** 条

涵盖艺术家、演职员、类型/流派、工作室、标签、年份、分级及音视频编解码器/容器/语言等筛选器。

[← 返回总目录](../README.md)

## 接口总览

| # | 方法 | 路径 | 鉴权 | 返回类型 | 摘要 |
| :---: | :--- | :--- | :--- | :--- | :--- |
| 1 | `GET` | `/Artists` | 需认证 | `QueryResult<BaseItemDto>` | Gets all artists from a given item, folder, or the entire library |
| 2 | `GET` | `/Artists/AlbumArtists` | 需认证 | `QueryResult<BaseItemDto>` | Gets all album artists from a given item, folder, or the entire library |
| 3 | `GET` | `/Artists/InstantMix` | 需认证 | `QueryResult<BaseItemDto>` | Creates an instant playlist based on a given artist |
| 4 | `GET` | `/Artists/Prefixes` | 需认证 | `NameValuePair[]` | Gets items based on a query. |
| 5 | `GET` | `/Artists/{Id}/Similar` | 需认证 | `QueryResult<BaseItemDto>` | Finds albums similar to a given album. |
| 6 | `GET` | `/Artists/{Name}` | 需认证 | `BaseItemDto` | Gets an artist, by name |
| 7 | `GET` | `/Artists/{Name}/Images/{Type}` | 需认证 | `object` | - |
| 8 | `HEAD` | `/Artists/{Name}/Images/{Type}` | 需认证 | `object` | - |
| 9 | `GET` | `/Artists/{Name}/Images/{Type}/{Index}` | 需认证 | `object` | - |
| 10 | `HEAD` | `/Artists/{Name}/Images/{Type}/{Index}` | 需认证 | `object` | - |
| 11 | `GET` | `/AudioCodecs` | 需认证 | `QueryResult<TagItem>` | Gets items based on a query. |
| 12 | `GET` | `/AudioLayouts` | 需认证 | `QueryResult<TagItem>` | Gets items based on a query. |
| 13 | `GET` | `/Containers` | 需认证 | `QueryResult<TagItem>` | Gets items based on a query. |
| 14 | `GET` | `/ExtendedVideoTypes` | 需认证 | `QueryResult<TagItem>` | Gets items based on a query. |
| 15 | `GET` | `/GameGenres` | 需认证 | `QueryResult<BaseItemDto>` | Gets all Game genres from a given item, folder, or the entire library |
| 16 | `GET` | `/GameGenres/{Name}` | 需认证 | `BaseItemDto` | Gets a Game genre, by name |
| 17 | `GET` | `/GameGenres/{Name}/Images/{Type}` | 需认证 | `object` | - |
| 18 | `HEAD` | `/GameGenres/{Name}/Images/{Type}` | 需认证 | `object` | - |
| 19 | `GET` | `/GameGenres/{Name}/Images/{Type}/{Index}` | 需认证 | `object` | - |
| 20 | `HEAD` | `/GameGenres/{Name}/Images/{Type}/{Index}` | 需认证 | `object` | - |
| 21 | `GET` | `/Genres` | 需认证 | `QueryResult<BaseItemDto>` | Gets all genres from a given item, folder, or the entire library |
| 22 | `GET` | `/Genres/{Name}` | 需认证 | `BaseItemDto` | Gets a genre, by name |
| 23 | `GET` | `/Genres/{Name}/Images/{Type}` | 需认证 | `object` | - |
| 24 | `HEAD` | `/Genres/{Name}/Images/{Type}` | 需认证 | `object` | - |
| 25 | `GET` | `/Genres/{Name}/Images/{Type}/{Index}` | 需认证 | `object` | - |
| 26 | `HEAD` | `/Genres/{Name}/Images/{Type}/{Index}` | 需认证 | `object` | - |
| 27 | `GET` | `/ItemTypes` | 需认证 | `QueryResult<TagItem>` | Gets items based on a query. |
| 28 | `GET` | `/MusicGenres` | 需认证 | `QueryResult<BaseItemDto>` | Gets all music genres from a given item, folder, or the entire library |
| 29 | `GET` | `/MusicGenres/InstantMix` | 需认证 | `QueryResult<BaseItemDto>` | Creates an instant playlist based on a music genre |
| 30 | `GET` | `/MusicGenres/{Name}` | 需认证 | `BaseItemDto` | Gets a music genre, by name |
| 31 | `GET` | `/MusicGenres/{Name}/Images/{Type}` | 需认证 | `object` | - |
| 32 | `HEAD` | `/MusicGenres/{Name}/Images/{Type}` | 需认证 | `object` | - |
| 33 | `GET` | `/MusicGenres/{Name}/Images/{Type}/{Index}` | 需认证 | `object` | - |
| 34 | `HEAD` | `/MusicGenres/{Name}/Images/{Type}/{Index}` | 需认证 | `object` | - |
| 35 | `GET` | `/MusicGenres/{Name}/InstantMix` | 需认证 | `QueryResult<BaseItemDto>` | Creates an instant playlist based on a music genre |
| 36 | `GET` | `/OfficialRatings` | 需认证 | `QueryResult<OfficialRatingItem>` | Gets items based on a query. |
| 37 | `GET` | `/Persons` | 需认证 | `QueryResult<BaseItemDto>` | Gets all persons from a given item, folder, or the entire library |
| 38 | `GET` | `/Persons/{Id}/Credits` | 需认证 | `CreditsList[]` | Gets credits for a person |
| 39 | `GET` | `/Persons/{Name}` | 需认证 | `BaseItemDto` | Gets a person, by name |
| 40 | `GET` | `/Persons/{Name}/Images/{Type}` | 需认证 | `object` | - |
| 41 | `HEAD` | `/Persons/{Name}/Images/{Type}` | 需认证 | `object` | - |
| 42 | `GET` | `/Persons/{Name}/Images/{Type}/{Index}` | 需认证 | `object` | - |
| 43 | `HEAD` | `/Persons/{Name}/Images/{Type}/{Index}` | 需认证 | `object` | - |
| 44 | `GET` | `/StreamLanguages` | 需认证 | `QueryResult<TagItem>` | Gets items based on a query. |
| 45 | `GET` | `/Studios` | 需认证 | `QueryResult<BaseItemDto>` | Gets all studios from a given item, folder, or the entire library |
| 46 | `GET` | `/Studios/{Name}` | 需认证 | `BaseItemDto` | Gets a studio, by name |
| 47 | `GET` | `/Studios/{Name}/Images/{Type}` | 需认证 | `object` | - |
| 48 | `HEAD` | `/Studios/{Name}/Images/{Type}` | 需认证 | `object` | - |
| 49 | `GET` | `/Studios/{Name}/Images/{Type}/{Index}` | 需认证 | `object` | - |
| 50 | `HEAD` | `/Studios/{Name}/Images/{Type}/{Index}` | 需认证 | `object` | - |
| 51 | `GET` | `/SubtitleCodecs` | 需认证 | `QueryResult<TagItem>` | Gets items based on a query. |
| 52 | `GET` | `/Tags` | 需认证 | `QueryResult<TagItem>` | Gets items based on a query. |
| 53 | `GET` | `/VideoCodecs` | 需认证 | `QueryResult<TagItem>` | Gets items based on a query. |
| 54 | `GET` | `/Years` | 需认证 | `QueryResult<TagItem>` | Gets items based on a query. |

## 接口详情

### ArtistsService（3 条）

#### `GET` `/Artists`

- **摘要**：Gets all artists from a given item, folder, or the entire library
- **鉴权**：需要认证
- **返回类型**：`QueryResult<BaseItemDto>`
- **处理服务**：`Emby.Api.UserLibrary.ArtistsService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.GetArtists` (`Emby.Api.dll`)

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

#### `GET` `/Artists/AlbumArtists`

- **摘要**：Gets all album artists from a given item, folder, or the entire library
- **鉴权**：需要认证
- **返回类型**：`QueryResult<BaseItemDto>`
- **处理服务**：`Emby.Api.UserLibrary.ArtistsService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.GetAlbumArtists` (`Emby.Api.dll`)

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

#### `GET` `/Artists/{Name}`

- **摘要**：Gets an artist, by name
- **鉴权**：需要认证
- **返回类型**：`BaseItemDto`
- **处理服务**：`Emby.Api.UserLibrary.ArtistsService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.GetArtist` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Name` | `path` | `string` | 是 | 否 | The artist name |
| `UserId` | `query` | `Guid` | 否 | 否 | Optional. Filter by user id, and attach user data |

### GameGenresService（2 条）

#### `GET` `/GameGenres`

- **摘要**：Gets all Game genres from a given item, folder, or the entire library
- **鉴权**：需要认证
- **返回类型**：`QueryResult<BaseItemDto>`
- **处理服务**：`Emby.Api.UserLibrary.GameGenresService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.GetGameGenres` (`Emby.Api.dll`)

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

#### `GET` `/GameGenres/{Name}`

- **摘要**：Gets a Game genre, by name
- **鉴权**：需要认证
- **返回类型**：`BaseItemDto`
- **处理服务**：`Emby.Api.UserLibrary.GameGenresService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.GetGameGenre` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Name` | `path` | `string` | 是 | 否 | The genre name |
| `UserId` | `query` | `Guid` | 否 | 否 | Optional. Filter by user id, and attach user data |

### GenresService（2 条）

#### `GET` `/Genres`

- **摘要**：Gets all genres from a given item, folder, or the entire library
- **鉴权**：需要认证
- **返回类型**：`QueryResult<BaseItemDto>`
- **处理服务**：`Emby.Api.UserLibrary.GenresService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.GetGenres` (`Emby.Api.dll`)

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

#### `GET` `/Genres/{Name}`

- **摘要**：Gets a genre, by name
- **鉴权**：需要认证
- **返回类型**：`BaseItemDto`
- **处理服务**：`Emby.Api.UserLibrary.GenresService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.GetGenre` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Name` | `path` | `string` | 是 | 否 | The genre name |
| `UserId` | `query` | `Guid` | 否 | 否 | Optional. Filter by user id, and attach user data |

### ImageService（24 条）

#### `GET` `/Artists/{Name}/Images/{Type}`

- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Api.Images.ImageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Images.GetItemByNameImage` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Name` | `path` | `string` | 是 | 否 | Item name |
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

#### `HEAD` `/Artists/{Name}/Images/{Type}`

- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Api.Images.ImageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Images.GetItemByNameImage` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Name` | `path` | `string` | 是 | 否 | Item name |
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

#### `GET` `/Artists/{Name}/Images/{Type}/{Index}`

- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Api.Images.ImageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Images.GetItemByNameImage` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Name` | `path` | `string` | 是 | 否 | Item name |
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

#### `HEAD` `/Artists/{Name}/Images/{Type}/{Index}`

- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Api.Images.ImageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Images.GetItemByNameImage` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Name` | `path` | `string` | 是 | 否 | Item name |
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

#### `GET` `/GameGenres/{Name}/Images/{Type}`

- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Api.Images.ImageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Images.GetItemByNameImage` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Name` | `path` | `string` | 是 | 否 | Item name |
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

#### `HEAD` `/GameGenres/{Name}/Images/{Type}`

- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Api.Images.ImageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Images.GetItemByNameImage` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Name` | `path` | `string` | 是 | 否 | Item name |
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

#### `GET` `/GameGenres/{Name}/Images/{Type}/{Index}`

- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Api.Images.ImageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Images.GetItemByNameImage` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Name` | `path` | `string` | 是 | 否 | Item name |
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

#### `HEAD` `/GameGenres/{Name}/Images/{Type}/{Index}`

- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Api.Images.ImageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Images.GetItemByNameImage` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Name` | `path` | `string` | 是 | 否 | Item name |
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

#### `GET` `/Genres/{Name}/Images/{Type}`

- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Api.Images.ImageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Images.GetItemByNameImage` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Name` | `path` | `string` | 是 | 否 | Item name |
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

#### `HEAD` `/Genres/{Name}/Images/{Type}`

- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Api.Images.ImageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Images.GetItemByNameImage` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Name` | `path` | `string` | 是 | 否 | Item name |
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

#### `GET` `/Genres/{Name}/Images/{Type}/{Index}`

- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Api.Images.ImageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Images.GetItemByNameImage` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Name` | `path` | `string` | 是 | 否 | Item name |
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

#### `HEAD` `/Genres/{Name}/Images/{Type}/{Index}`

- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Api.Images.ImageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Images.GetItemByNameImage` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Name` | `path` | `string` | 是 | 否 | Item name |
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

#### `GET` `/MusicGenres/{Name}/Images/{Type}`

- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Api.Images.ImageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Images.GetItemByNameImage` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Name` | `path` | `string` | 是 | 否 | Item name |
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

#### `HEAD` `/MusicGenres/{Name}/Images/{Type}`

- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Api.Images.ImageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Images.GetItemByNameImage` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Name` | `path` | `string` | 是 | 否 | Item name |
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

#### `GET` `/MusicGenres/{Name}/Images/{Type}/{Index}`

- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Api.Images.ImageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Images.GetItemByNameImage` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Name` | `path` | `string` | 是 | 否 | Item name |
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

#### `HEAD` `/MusicGenres/{Name}/Images/{Type}/{Index}`

- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Api.Images.ImageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Images.GetItemByNameImage` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Name` | `path` | `string` | 是 | 否 | Item name |
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

#### `GET` `/Persons/{Name}/Images/{Type}`

- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Api.Images.ImageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Images.GetItemByNameImage` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Name` | `path` | `string` | 是 | 否 | Item name |
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

#### `HEAD` `/Persons/{Name}/Images/{Type}`

- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Api.Images.ImageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Images.GetItemByNameImage` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Name` | `path` | `string` | 是 | 否 | Item name |
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

#### `GET` `/Persons/{Name}/Images/{Type}/{Index}`

- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Api.Images.ImageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Images.GetItemByNameImage` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Name` | `path` | `string` | 是 | 否 | Item name |
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

#### `HEAD` `/Persons/{Name}/Images/{Type}/{Index}`

- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Api.Images.ImageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Images.GetItemByNameImage` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Name` | `path` | `string` | 是 | 否 | Item name |
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

#### `GET` `/Studios/{Name}/Images/{Type}`

- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Api.Images.ImageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Images.GetItemByNameImage` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Name` | `path` | `string` | 是 | 否 | Item name |
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

#### `HEAD` `/Studios/{Name}/Images/{Type}`

- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Api.Images.ImageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Images.GetItemByNameImage` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Name` | `path` | `string` | 是 | 否 | Item name |
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

#### `GET` `/Studios/{Name}/Images/{Type}/{Index}`

- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Api.Images.ImageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Images.GetItemByNameImage` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Name` | `path` | `string` | 是 | 否 | Item name |
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

#### `HEAD` `/Studios/{Name}/Images/{Type}/{Index}`

- **鉴权**：需要认证
- **返回类型**：`object`
- **处理服务**：`Emby.Api.Images.ImageService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Images.GetItemByNameImage` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Name` | `path` | `string` | 是 | 否 | Item name |
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

### InstantMixService（3 条）

#### `GET` `/Artists/InstantMix`

- **摘要**：Creates an instant playlist based on a given artist
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

#### `GET` `/MusicGenres/InstantMix`

- **摘要**：Creates an instant playlist based on a music genre
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

#### `GET` `/MusicGenres/{Name}/InstantMix`

- **摘要**：Creates an instant playlist based on a music genre
- **鉴权**：需要认证
- **返回类型**：`QueryResult<BaseItemDto>`
- **处理服务**：`Emby.Api.Music.InstantMixService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.Music.GetInstantMixFromMusicGenre` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Name` | `path` | `string` | 否 | 否 | - |
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

### LibraryService（1 条）

#### `GET` `/Artists/{Id}/Similar`

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

### MusicGenresService（2 条）

#### `GET` `/MusicGenres`

- **摘要**：Gets all music genres from a given item, folder, or the entire library
- **鉴权**：需要认证
- **返回类型**：`QueryResult<BaseItemDto>`
- **处理服务**：`Emby.Api.UserLibrary.MusicGenresService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.GetMusicGenres` (`Emby.Api.dll`)

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

#### `GET` `/MusicGenres/{Name}`

- **摘要**：Gets a music genre, by name
- **鉴权**：需要认证
- **返回类型**：`BaseItemDto`
- **处理服务**：`Emby.Api.UserLibrary.MusicGenresService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.GetMusicGenre` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Name` | `path` | `string` | 是 | 否 | The genre name |
| `UserId` | `query` | `Guid` | 否 | 否 | Optional. Filter by user id, and attach user data |

### OfficialRatingService（1 条）

#### `GET` `/OfficialRatings`

- **摘要**：Gets items based on a query.
- **鉴权**：需要认证
- **返回类型**：`QueryResult<OfficialRatingItem>`
- **处理服务**：`Emby.Api.UserLibrary.OfficialRatingService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.GetOfficialRatings` (`Emby.Api.dll`)

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

### PersonsService（3 条）

#### `GET` `/Persons`

- **摘要**：Gets all persons from a given item, folder, or the entire library
- **鉴权**：需要认证
- **返回类型**：`QueryResult<BaseItemDto>`
- **处理服务**：`Emby.Api.UserLibrary.PersonsService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.GetPersons` (`Emby.Api.dll`)

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

#### `GET` `/Persons/{Id}/Credits`

- **摘要**：Gets credits for a person
- **鉴权**：需要认证
- **返回类型**：`CreditsList[]`
- **处理服务**：`Emby.Api.UserLibrary.PersonsService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.GetPersonCredits` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Id` | `query` | `string` | 是 | 否 | The person id |

#### `GET` `/Persons/{Name}`

- **摘要**：Gets a person, by name
- **鉴权**：需要认证
- **返回类型**：`BaseItemDto`
- **处理服务**：`Emby.Api.UserLibrary.PersonsService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.GetPerson` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Name` | `path` | `string` | 是 | 否 | The person name |
| `UserId` | `query` | `Guid` | 否 | 否 | Optional. Filter by user id, and attach user data |

### StudiosService（2 条）

#### `GET` `/Studios`

- **摘要**：Gets all studios from a given item, folder, or the entire library
- **鉴权**：需要认证
- **返回类型**：`QueryResult<BaseItemDto>`
- **处理服务**：`Emby.Api.UserLibrary.StudiosService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.GetStudios` (`Emby.Api.dll`)

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

#### `GET` `/Studios/{Name}`

- **摘要**：Gets a studio, by name
- **鉴权**：需要认证
- **返回类型**：`BaseItemDto`
- **处理服务**：`Emby.Api.UserLibrary.StudiosService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.GetStudio` (`Emby.Api.dll`)

| 参数名 | 位置 | 类型 | 必填 | 多值 | 说明 |
| :--- | :---: | :--- | :---: | :---: | :--- |
| `Name` | `path` | `string` | 是 | 否 | The studio name |
| `UserId` | `query` | `Guid` | 否 | 否 | Optional. Filter by user id, and attach user data |

### TagService（11 条）

#### `GET` `/Artists/Prefixes`

- **摘要**：Gets items based on a query.
- **鉴权**：需要认证
- **返回类型**：`NameValuePair[]`
- **处理服务**：`Emby.Api.UserLibrary.TagService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.GetArtistPrefixes` (`Emby.Api.dll`)

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

#### `GET` `/AudioCodecs`

- **摘要**：Gets items based on a query.
- **鉴权**：需要认证
- **返回类型**：`QueryResult<TagItem>`
- **处理服务**：`Emby.Api.UserLibrary.TagService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.GetAudioCodecs` (`Emby.Api.dll`)

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

#### `GET` `/AudioLayouts`

- **摘要**：Gets items based on a query.
- **鉴权**：需要认证
- **返回类型**：`QueryResult<TagItem>`
- **处理服务**：`Emby.Api.UserLibrary.TagService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.GetAudioLayouts` (`Emby.Api.dll`)

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

#### `GET` `/Containers`

- **摘要**：Gets items based on a query.
- **鉴权**：需要认证
- **返回类型**：`QueryResult<TagItem>`
- **处理服务**：`Emby.Api.UserLibrary.TagService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.GetContainers` (`Emby.Api.dll`)

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

#### `GET` `/ExtendedVideoTypes`

- **摘要**：Gets items based on a query.
- **鉴权**：需要认证
- **返回类型**：`QueryResult<TagItem>`
- **处理服务**：`Emby.Api.UserLibrary.TagService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.GetExtendedVideoTypes` (`Emby.Api.dll`)

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

#### `GET` `/ItemTypes`

- **摘要**：Gets items based on a query.
- **鉴权**：需要认证
- **返回类型**：`QueryResult<TagItem>`
- **处理服务**：`Emby.Api.UserLibrary.TagService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.GetItemTypes` (`Emby.Api.dll`)

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

#### `GET` `/StreamLanguages`

- **摘要**：Gets items based on a query.
- **鉴权**：需要认证
- **返回类型**：`QueryResult<TagItem>`
- **处理服务**：`Emby.Api.UserLibrary.TagService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.GetStreamLanguages` (`Emby.Api.dll`)

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
| `StreamType` | `query` | `MediaStreamType` | 否 | 否 | - |
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

#### `GET` `/SubtitleCodecs`

- **摘要**：Gets items based on a query.
- **鉴权**：需要认证
- **返回类型**：`QueryResult<TagItem>`
- **处理服务**：`Emby.Api.UserLibrary.TagService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.GetSubtitleCodecs` (`Emby.Api.dll`)

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

#### `GET` `/Tags`

- **摘要**：Gets items based on a query.
- **鉴权**：需要认证
- **返回类型**：`QueryResult<TagItem>`
- **处理服务**：`Emby.Api.UserLibrary.TagService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.GetTags` (`Emby.Api.dll`)

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

#### `GET` `/VideoCodecs`

- **摘要**：Gets items based on a query.
- **鉴权**：需要认证
- **返回类型**：`QueryResult<TagItem>`
- **处理服务**：`Emby.Api.UserLibrary.TagService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.GetVideoCodecs` (`Emby.Api.dll`)

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

#### `GET` `/Years`

- **摘要**：Gets items based on a query.
- **鉴权**：需要认证
- **返回类型**：`QueryResult<TagItem>`
- **处理服务**：`Emby.Api.UserLibrary.TagService` (Emby.Api.dll)
- **Request DTO**：`Emby.Api.UserLibrary.GetYears` (`Emby.Api.dll`)

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
