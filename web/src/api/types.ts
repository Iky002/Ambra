export interface QueryResult<T> {
  Items: T[]
  TotalRecordCount: number
}

export interface PublicSystemInfo {
  LocalAddress?: string
  ServerName: string
  Version: string
  ProductName?: string
  Id: string
}

export interface SystemInfo extends PublicSystemInfo {
  OperatingSystem?: string
  OperatingSystemDisplayName?: string
  HasPendingRestart?: boolean
  IsShuttingDown?: boolean
  SupportsLibraryMonitor?: boolean
  WebSocketPortNumber?: number
  CanSelfRestart?: boolean
  CanSelfUpdate?: boolean
  ProgramDataPath?: string
  CachePath?: string
  LogPath?: string
  InternalMetadataPath?: string
  TranscodingTempPath?: string
  HttpServerPortNumber?: number
  SupportsHttps?: boolean
  HasUpdateAvailable?: boolean
  EncoderLocationType?: string
  SystemArchitecture?: string
  StartupWizardCompleted?: boolean
}

export interface UserPolicy {
  IsAdministrator?: boolean
  IsHidden?: boolean
  IsDisabled?: boolean
  EnableAllFolders?: boolean
  EnabledFolders?: string[]
  EnableMediaPlayback?: boolean
  EnableAudioPlaybackTranscoding?: boolean
  EnableVideoPlaybackTranscoding?: boolean
  EnablePlaybackRemuxing?: boolean
  EnableContentDeletion?: boolean
  EnableContentDownloading?: boolean
  EnableRemoteAccess?: boolean
  EnableLiveTvAccess?: boolean
  EnableLiveTvManagement?: boolean
  MaxParentalRating?: number
  BlockedTags?: string[]
  AuthenticationProviderId?: string
}

export interface UserConfiguration {
  PlayDefaultAudioTrack?: boolean
  SubtitleLanguagePreference?: string
  AudioLanguagePreference?: string
  SubtitleMode?: string
  EnableNextEpisodeAutoPlay?: boolean
}

export interface UserDto {
  Name: string
  ServerId: string
  Id: string
  HasPassword?: boolean
  HasConfiguredPassword?: boolean
  PrimaryImageTag?: string
  LastLoginDate?: string
  LastActivityDate?: string
  Configuration?: UserConfiguration
  Policy?: UserPolicy
}

export interface AuthenticationResult {
  User: UserDto
  AccessToken: string
  ServerId: string
  SessionInfo?: {
    Id: string
    DeviceId: string
  }
}

export interface UserItemDataDto {
  PlaybackPositionTicks: number
  PlayCount: number
  IsFavorite: boolean
  Played: boolean
  UnplayedItemCount?: number
  PlayedPercentage?: number
  LastPlayedDate?: string
  Key?: string
}

export interface MediaStream {
  Codec?: string
  Language?: string
  Title?: string
  DisplayTitle?: string
  DisplayLanguage?: string
  IsInterlaced?: boolean
  BitRate?: number
  Channels?: number
  SampleRate?: number
  Height?: number
  Width?: number
  VideoRange?: string
  IsDefault?: boolean
  IsForced?: boolean
  IsExternal?: boolean
  Type: 'Video' | 'Audio' | 'Subtitle' | string
  Index: number
  Path?: string
  DeliveryMethod?: 'External' | 'Embed' | 'Encode' | 'Hls' | string
  DeliveryUrl?: string
  IsExternalUrl?: boolean
  /** Ambra 扩展字段: chs / cht / bilingual */
  SubVariant?: string
}

export interface MediaSourceInfo {
  Protocol?: string
  Id: string
  Path?: string
  Type?: string
  Container?: string
  Size?: number
  Name: string
  IsRemote?: boolean
  RunTimeTicks?: number
  Bitrate?: number
  SupportsTranscoding?: boolean
  SupportsDirectStream?: boolean
  SupportsDirectPlay?: boolean
  DirectStreamUrl?: string
  TranscodingUrl?: string
  MediaStreams?: MediaStream[]
  DefaultAudioStreamIndex?: number
  DefaultSubtitleStreamIndex?: number
}

export interface BaseItemPerson {
  Name: string
  Id: string
  Role?: string
  Type: string
  PrimaryImageTag?: string
}

export interface NameIdPair {
  Name: string
  Id: string
}

export interface BaseItemDto {
  Name: string
  OriginalTitle?: string
  ServerId?: string
  Id: string
  Etag?: string
  DateCreated?: string
  Container?: string
  SortName?: string
  PremiereDate?: string
  Path?: string
  OfficialRating?: string
  Overview?: string
  Taglines?: string[]
  Tags?: string[]
  TagItems?: NameIdPair[]
  Genres?: string[]
  GenreItems?: NameIdPair[]
  CommunityRating?: number
  RunTimeTicks?: number
  ProductionYear?: number
  IndexNumber?: number
  ParentIndexNumber?: number
  ProviderIds?: Record<string, string>
  IsFolder?: boolean
  ParentId?: string
  Type: string
  MediaType?: string
  CollectionType?: string
  SeriesName?: string
  SeriesId?: string
  SeasonId?: string
  SeasonName?: string
  Studios?: NameIdPair[]
  People?: BaseItemPerson[]
  UserData?: UserItemDataDto
  ChildCount?: number
  RecursiveItemCount?: number
  MediaSources?: MediaSourceInfo[]
  ImageTags?: Record<string, string>
  BackdropImageTags?: string[]
  PrimaryImageAspectRatio?: number
  LocationType?: string
}

export interface MediaAttachmentInfo {
  Index: number
  FileName: string
  MimeType: string
  Url: string
}

export interface PlaybackInfoResponse {
  MediaSources: MediaSourceInfo[]
  PlaySessionId: string
}

export interface SearchHint {
  ItemId: string
  Id: string
  Name: string
  MatchedTerm?: string
  ProductionYear?: number
  PrimaryImageTag?: string
  Type: string
  MediaType?: string
  RunTimeTicks?: number
  Series?: string
  SeriesId?: string
  IndexNumber?: number
  ParentIndexNumber?: number
  BackdropImageTag?: string
  BackdropImageItemId?: string
}

export interface SearchHintResult {
  SearchHints: SearchHint[]
  TotalRecordCount: number
}

export interface PluginInfo {
  Name: string
  Version?: string
  Description?: string
  Id: string
  Author?: string
  BuiltIn?: boolean
  Enabled?: boolean
  Priority?: number
  Kinds?: string[]
  ConfigurationFileName?: string
  Status?: string
}

export interface PlayerStateInfo {
  PositionTicks?: number
  CanSeek?: boolean
  IsPaused?: boolean
  IsMuted?: boolean
  VolumeLevel?: number
  AudioStreamIndex?: number
  SubtitleStreamIndex?: number
  MediaSourceId?: string
  PlayMethod?: string
  RepeatMode?: string
}

export interface SessionInfo {
  Id: string
  ServerId?: string
  UserId?: string
  UserName?: string
  Client?: string
  LastActivityDate?: string
  DeviceName?: string
  DeviceId?: string
  ApplicationVersion?: string
  SupportsRemoteControl?: boolean
  PlayState?: PlayerStateInfo
  NowPlayingItem?: BaseItemDto
  PlayableMediaTypes?: string[]
  SupportedCommands?: string[]
}

export interface ActivityLogEntry {
  Id: number
  Name: string
  Overview?: string
  ShortOverview?: string
  Type: string
  ItemId?: string
  Date: string
  UserId?: string
  Severity: string
}

export interface VirtualFolderInfo {
  Name: string
  Locations?: string[]
  CollectionType?: string
  ItemId: string
}

export interface FileSystemEntry {
  Name: string
  Path: string
  Type?: 'Directory' | 'File' | 'NetworkComputer' | 'NetworkShare' | string
}

export interface DeviceInfo {
  Name: string
  Id: string
  LastUserName?: string
  AppName?: string
  AppVersion?: string
  LastUserId?: string
  DateLastActivity?: string
}

export interface ApiKeyInfo {
  AccessToken: string
  AppName: string
  DateCreated?: string
  IsActive?: boolean
}

export interface ScheduledTaskExecutionResult {
  StartTimeUtc?: string
  EndTimeUtc?: string
  Status?: string
  Name?: string
  Key?: string
  Id?: string
  ErrorMessage?: string
}

export interface ScheduledTaskInfo {
  Name: string
  State: string
  Id: string
  Description?: string
  Category?: string
  IsHidden?: boolean
  Key?: string
  CurrentProgressPercentage?: number
  LastExecutionResult?: ScheduledTaskExecutionResult
}

export interface ServerLogFile {
  Name: string
  DateCreated?: string
  DateModified?: string
  Size?: number
}

export interface AuthSession {
  accessToken: string
  userId: string
  serverId: string
  userName: string
  isAdmin: boolean
  deviceId: string
}

export interface ItemMetadataUpdatePayload {
  Name?: string
  OriginalTitle?: string
  SortName?: string
  ProductionYear?: number
  CommunityRating?: number
  OfficialRating?: string
  Overview?: string
  Genres?: string[]
  Tags?: string[]
  ProviderIds?: Record<string, string>
}

export interface ItemImageInfo {
  ImageType: string
  ImageIndex?: number
  Path?: string
  Filename?: string
  Height?: number
  Width?: number
  Size?: number
  ImageTag?: string
}

export interface RemoteImageInfo {
  ProviderName?: string
  Url: string
  ThumbnailUrl?: string
  Height?: number
  Width?: number
  CommunityRating?: number
  VoteCount?: number
  Language?: string
  Type: 'Primary' | 'Backdrop' | string
  RatingType?: string
}

export interface RemoteImageResult {
  Images: RemoteImageInfo[]
  TotalRecordCount: number
  Providers: string[]
}

export interface RemoteSearchResult {
  Name: string
  ProductionYear?: number
  PremiereDate?: string
  ImageUrl?: string
  BackdropUrl?: string
  SearchProviderName?: string
  Overview?: string
  ProviderIds?: Record<string, string>
}

export interface RankingsChartSnapshot {
  chart: string
  source: string
  updated_at: string
  count: number
}

export interface RankingsSummary {
  douban_top250: RankingsChartSnapshot
  imdb_top250: RankingsChartSnapshot
}

export interface RankingsRefreshResult {
  douban_count: number
  douban_error?: string
  imdb_count: number
  imdb_error?: string
  matched_items: number
  tags_added: number
  synced_at: string
}
