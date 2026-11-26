export interface QueryResult<T> {
  Items: T[]
  TotalRecordCount: number
}

export interface PublicSystemInfo {
  LocalAddress?: string
  ServerName: string
  Version: string
  AmbraVersion?: string
  ProductName?: string
  Id: string
}

export interface ServerConfiguration {
  ServerName: string
  IsStartupWizardCompleted?: boolean
  TMDBApiKey?: string
  HasTMDBApiKey?: boolean
  TMDBLanguage?: string
  EnableSTRM302?: boolean
  EnableDLNA?: boolean
  DLNADefaultUserID?: string
  EnableQuickConnect?: boolean
  EnableLoginUserList?: boolean
  EnableRemoteAccess?: boolean
  AllowedOrigins?: string[]
  TrustedProxies?: string[]
  LocalNetworkSubnets?: string[]
  SimultaneousStreamLimit?: number
  RemoteClientBitrateLimit?: number
  FFmpegPath?: string
  FFprobePath?: string
  PlayedThreshold?: number
  PreferredMetadataLanguage?: string
  MetadataCountryCode?: string
  UICulture?: string
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
  ChapterCachePath?: string
  BifCachePath?: string
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
  BlockUnratedItems?: string[]
  BlockedTags?: string[]
  AllowedTags?: string[]
  AuthenticationProviderId?: string
}

export interface UserConfiguration {
  PlayDefaultAudioTrack?: boolean
  SubtitleLanguagePreference?: string
  AudioLanguagePreference?: string
  UICulture?: string
  DisplayMissingEpisodes?: boolean
  SubtitleMode?: string
  EnableLocalPassword?: boolean
  OrderedViews?: string[]
  LatestItemsExcludes?: string[]
  MyMediaExcludes?: string[]
  HidePlayedInLatest?: boolean
  RememberAudioSelections?: boolean
  RememberSubtitleSelections?: boolean
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
  Likes?: boolean
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

export interface ChapterInfo {
  StartPositionTicks: number
  Name?: string
  ImageTag?: string
}

export interface LiveTvTunerHostInfo {
  Id?: string
  Url: string
  Type?: string
  FriendlyName?: string
  UserAgent?: string
  ImportFavoritesOnly?: boolean
  TunerCount?: number
}

export interface LiveTvListingProviderInfo {
  Id?: string
  Type?: string
  Path: string
  UserAgent?: string
}

export interface LiveTvChannelInfo {
  Id: string
  Name: string
  Number?: string
  ChannelType?: string
  Type?: string
  ImageUrl?: string
  StreamUrl?: string
  IsHls?: boolean
  StreamFormat?: string
}

export interface LiveTvInfo {
  Services?: Array<{ Name?: string; Status?: string }>
  IsEnabled?: boolean
  EnabledUsers?: string[]
}

export interface TaskTriggerInfo {
  Type: 'DailyTrigger' | 'IntervalTrigger' | 'StartupTrigger' | 'WeeklyTrigger' | string
  TimeOfDayTicks?: number
  IntervalTicks?: number
  DayOfWeek?: string
  MaxRuntimeTicks?: number
}

export interface BackupFileInfo {
  Name: string
  Path?: string
  Size: number
  DateCreated: string
}

export interface BackupInfoResponse {
  BackupDirectory: string
  Backups: BackupFileInfo[]
}

export interface BackupRestoreResult {
  Restored: boolean
  BackupName: string
  StagedPath?: string
  RequiresRestart?: boolean
}

export interface LiveTvTimerInfo {
  Id: string
  ChannelId: string
  ChannelName?: string
  ProgramId?: string
  Name: string
  Overview?: string
  StartDate: string
  EndDate: string
  Status?: string
  PrePaddingSeconds?: number
  PostPaddingSeconds?: number
}

export interface LiveTvRecordingInfo {
  Id: string
  ChannelId?: string
  ChannelName?: string
  Name: string
  Overview?: string
  StartDate?: string
  EndDate?: string
  Status?: string
  Path?: string
}

export interface WebhookInfo {
  Id: string
  Name: string
  Url: string
  Secret?: string
  HasSecret?: boolean
  Events: string[]
  Enabled: boolean
  CreatedAt?: number
}

export interface WebhookTestResult {
  Success: boolean
  StatusCode?: number
  DurationMs?: number
  Error?: string
}

export interface ParentalRatingOption {
  Name: string
  Value: number
}

export interface NotificationItem {
  Id: string
  UserId?: string
  Date: string
  IsRead: boolean
  Name: string
  Description?: string
  Url?: string
  Level: 'Normal' | 'Warning' | 'Error' | string
}

export interface NotificationResult {
  Notifications: NotificationItem[]
  TotalRecordCount: number
}

export interface NotificationsSummary {
  UnreadCount: number
  MaxUnreadNotificationLevel?: 'Normal' | 'Warning' | 'Error' | string
}

export interface QuickConnectResult {
  Authenticated: boolean
  Secret: string
  Code: string
  DeviceId?: string
  DeviceName?: string
  AppName?: string
  AppVersion?: string
  DateAdded?: string
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
  Album?: string
  AlbumId?: string
  AlbumArtist?: string
  AlbumPrimaryImageTag?: string
  Artists?: string[]
  ArtistItems?: NameIdPair[]
  Studios?: NameIdPair[]
  People?: BaseItemPerson[]
  UserData?: UserItemDataDto
  ChildCount?: number
  RecursiveItemCount?: number
  MediaSources?: MediaSourceInfo[]
  MediaStreams?: MediaStream[]
  Chapters?: ChapterInfo[]
  SeriesPrimaryImageTag?: string
  ParentBackdropItemId?: string
  ParentBackdropImageTags?: string[]
  ImageTags?: Record<string, string>
  BackdropImageTags?: string[]
  PrimaryImageAspectRatio?: number
  LocationType?: string
  PlaylistItemId?: string
  LockedFields?: string[]
  LockData?: boolean
}

export interface MediaAttachmentInfo {
  Index: number
  FileName: string
  MimeType: string
  Url: string
}

export interface LyricLine {
  Start?: number
  Text: string
}

export interface AudioLyricsResponse {
  Metadata?: {
    Artist?: string
    Album?: string
    Title?: string
    IsSynced?: boolean
  }
  Lyrics?: LyricLine[]
  RawLrc?: string
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

export interface VirtualFolderLibraryOptions {
  SaveLocalMetadata?: boolean
  [key: string]: unknown
}

export interface VirtualFolderInfo {
  Name: string
  Locations?: string[]
  CollectionType?: string
  ItemId: string
  LibraryOptions?: VirtualFolderLibraryOptions
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
  Id?: string
  AccessToken: string
  MaskedToken?: string
  AppName: string
  DateCreated?: string
  IsActive?: boolean
}

export type RemoteSearchItemType =
  | 'Movie'
  | 'Series'
  | 'BoxSet'
  | 'MusicAlbum'
  | 'MusicArtist'
  | 'MusicVideo'
  | 'Person'
  | 'Book'
  | 'Game'
  | 'Trailer'

export interface RemoteSubtitleInfo {
  Id: string
  Name: string
  ProviderName?: string
  Language?: string
  ThreeLetterISOLanguageName?: string
  Format?: string
  Comment?: string
  Author?: string
  CommunityRating?: number
  DownloadCount?: number
  IsHashMatch?: boolean
}

export interface EncodingFfmpegOptions {
  HardwareAccelerationMode?: number
  EnableThrottling?: boolean
  ThrottleBufferSize?: number
  ThrottleHysteresis?: number
  H264Crf?: number
  H264Preset?: string
  EnableHardwareEncoding?: boolean
  AllowHevcEncoding?: boolean
  EnableSubtitleExtraction?: boolean
  TranscodingTempPath?: string
  EncoderAppPathDisplay?: string
}

export interface ToneMapOptions {
  EnableToneMapping?: boolean
  ToneMappingAlgorithm?: string
  ToneMappingMode?: string
  ToneMappingDesat?: number
  ToneMappingParam?: number
  ToneMappingPeak?: number
}

export interface LiveTvSeriesTimerInfo {
  Id?: string
  ChannelId?: string
  ProgramId?: string
  SeriesId?: string
  Name: string
  Overview?: string
  RecordAnyTime?: boolean
  RecordAnyChannel?: boolean
  RecordNewOnly?: boolean
  Days?: string[]
  PrePaddingSeconds?: number
  PostPaddingSeconds?: number
}

export interface LiveTvChannelMapping {
  ProviderId?: string
  TunerChannelId: string
  ProviderChannelId: string
  TunerChannelName?: string
  ProviderChannelName?: string
}

export interface ItemFilters2Response {
  Genres?: string[]
  Tags?: string[]
  OfficialRatings?: string[]
  Years?: number[]
  VideoTypes?: string[]
  Codecs?: string[]
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
  Triggers?: TaskTriggerInfo[]
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

export type RankingChartStatus = RankingsChartSnapshot

export interface RankingsSummary {
  douban_top250: RankingsChartSnapshot
  imdb_top250: RankingsChartSnapshot
  tmdb_trending?: RankingsChartSnapshot
  douban_trending_movie?: RankingChartStatus
  douban_trending_tv?: RankingChartStatus
}

export interface RankingsRefreshResult {
  douban_count: number
  douban_error?: string
  imdb_count: number
  imdb_error?: string
  tmdb_trending_count?: number
  tmdb_trending_error?: string
  douban_trending_movie_count?: number
  douban_trending_movie_error?: string
  douban_trending_tv_count?: number
  douban_trending_tv_error?: string
  matched_items: number
  tags_added: number
  synced_at: string
}

export interface LibraryScanProgress {
  library_id: string
  library_name: string
  phase: 'discovering' | 'enriching' | 'completed' | string
  total: number
  processed: number
  added: number
  updated: number
  skipped: number
  failed: number
  current_item: string
  current_action: string
  percentage: number
  started_at: string
  updated_at: string
}

export interface ScanProgressSnapshot {
  running: boolean
  paused?: boolean
  phase: 'idle' | 'discovering' | 'enriching' | string
  library_id?: string
  library_name?: string
  total: number
  processed: number
  added: number
  updated: number
  skipped: number
  failed: number
  current_item?: string
  current_action?: string
  percentage: number
  started_at?: string
  updated_at: string
  libraries?: LibraryScanProgress[]
}

export interface SetupStatus {
  completed: boolean
  server_name: string
  default_language: string
  has_users: boolean
  has_tmdb_key: boolean
}

export interface SetupAdminRequest {
  server_name?: string
  language?: string
  country?: string
  username: string
  password: string
}

export interface ThumbnailSetItem {
  PositionTicks: number
  ImageTag: string
}

export interface ThumbnailSetResponse {
  AspectRatio: number
  Thumbnails: ThumbnailSetItem[]
}

export interface SetupCompleteRequest {
  server_name?: string
  tmdb_api_key?: string
  tmdb_language?: string
  enable_strm_302?: boolean
  trigger_scan?: boolean
  sync_rankings?: boolean
}

export interface UserTraktStatus {
  Enabled: boolean
  Configured: boolean
  ClientId: string
  AccessTokenConfigured: boolean
  ScrobblePlayback: boolean
  SyncWatched: boolean
}

export interface UserTraktUpdate {
  ClientId?: string
  AccessToken?: string
  ScrobblePlayback?: boolean
  SyncWatched?: boolean
}
