import { ApiError, getStoredAuth } from '../api/client'
import type {
  ApiKeyInfo,
  AuthenticationResult,
  BackupFileInfo,
  BackupInfoResponse,
  BackupRestoreResult,
  BaseItemDto,
  EncodingFfmpegOptions,
  ItemImageInfo,
  LibraryScanProgress,
  LiveTvChannelInfo,
  LiveTvChannelMapping,
  LiveTvInfo,
  LiveTvListingProviderInfo,
  LiveTvRecordingInfo,
  LiveTvSeriesTimerInfo,
  LiveTvTimerInfo,
  LiveTvTunerHostInfo,
  NotificationItem,
  NotificationResult,
  NotificationsSummary,
  ParentalRatingOption,
  PlaybackInfoResponse,
  PublicSystemInfo,
  QueryResult,
  QuickConnectResult,
  RankingsRefreshResult,
  RankingsSummary,
  RemoteImageInfo,
  RemoteImageResult,
  RemoteSearchResult,
  RemoteSubtitleInfo,
  ScanProgressSnapshot,
  SearchHint,
  SearchHintResult,
  ServerConfiguration,
  SessionInfo,
  SetupStatus,
  TaskTriggerInfo,
  ToneMapOptions,
  UserDto,
  UserItemDataDto,
  UserPolicy,
  UserTraktStatus,
  VirtualFolderInfo,
  WebhookInfo,
  WebhookTestResult,
} from '../api/types'
import { registerDemoServer } from './demoLoader'
import { DEMO_VIDEO_DATA_URL, makeDemoAudioDataUrl, makeDemoBifBuffer, makeDemoVttDataUrl } from './demoMedia'
import { createInitialDemoState, DEMO_SERVER_ID, type DemoState } from './mockData'
import { makeAvatarDataUrl, makeLibraryThumbDataUrl, makePosterDataUrl } from './svgArt'

const PINYIN_COLLATOR = new Intl.Collator('zh-CN-u-co-pinyin')
const PINYIN_BOUNDARIES: Array<[string, string]> = [
  ['z', '匝'],
  ['y', '压'],
  ['x', '昔'],
  ['w', '挖'],
  ['t', '塌'],
  ['s', '撒'],
  ['r', '然'],
  ['q', '期'],
  ['p', '啪'],
  ['o', '哦'],
  ['n', '拿'],
  ['m', '妈'],
  ['l', '垃'],
  ['k', '咔'],
  ['j', '讥'],
  ['h', '哈'],
  ['g', '旮'],
  ['f', '发'],
  ['e', '妸'],
  ['d', '哒'],
  ['c', '嚓'],
  ['b', '八'],
  ['a', '阿'],
]

function getDemoSortKey(raw: string): string {
  const trimmed = raw.trim().replace(/^[《〈「『【（“‘]+/, '')
  if (!trimmed) return ''
  const first = trimmed[0]
  if (/[\u4e00-\u9fff]/.test(first)) {
    for (const [letter, boundary] of PINYIN_BOUNDARIES) {
      if (PINYIN_COLLATOR.compare(first, boundary) >= 0) {
        return letter + trimmed.slice(1).toLowerCase()
      }
    }
  }
  return trimmed.toLowerCase()
}

const DEMO_MASKED_SECRET = '********'

function maskApiKeyToken(token: string): string {
  if (!token) return ''
  if (token.includes('•') || token.includes('*')) return token
  if (token.length <= 12) {
    return `${token.slice(0, 3)}••••••••${token.slice(-2)}`
  }
  return `${token.slice(0, 8)}••••••••••••••••${token.slice(-4)}`
}

let store: DemoState = createInitialDemoState()
const demoTraktAccounts = new Map<string, UserTraktStatus>()
const demoTokenUserMap = new Map<string, string>()
const demoRawApiKeys = new Map<string, string>()

function seedInitialApiKeys(): void {
  demoRawApiKeys.clear()
  for (const key of store.apiKeys) {
    const id = key.Id || `apikey-${key.AppName.toLowerCase()}`
    key.Id = id
    const raw = key.AccessToken
    demoRawApiKeys.set(id, raw)
    const masked = key.MaskedToken || maskApiKeyToken(raw)
    key.MaskedToken = masked
  }
}
seedInitialApiKeys()

interface DemoRankingsState {
  doubanUpdatedAt: string
  imdbUpdatedAt: string
  tmdbTrendingUpdatedAt: string
  doubanTrendingMovieUpdatedAt: string
  doubanTrendingTvUpdatedAt: string
}

function createInitialRankingsState(): DemoRankingsState {
  const threeHoursAgo = new Date(Date.now() - 3 * 3600_000).toISOString()
  return {
    doubanUpdatedAt: threeHoursAgo,
    imdbUpdatedAt: threeHoursAgo,
    tmdbTrendingUpdatedAt: threeHoursAgo,
    doubanTrendingMovieUpdatedAt: threeHoursAgo,
    doubanTrendingTvUpdatedAt: threeHoursAgo,
  }
}

let rankingsState: DemoRankingsState = createInitialRankingsState()

interface DemoScanState {
  startedAtMs: number
  durationMs: number
  libraryId: string
  libraryName: string
  items: string[]
  paused?: boolean
  pausedAtMs?: number
}

let activeDemoScan: DemoScanState | null = null
let demoSetupCompleted = true
let demoDefaultLanguage = 'zh-CN'
let demoRawTmdbApiKey = 'demo_tmdb_v3_key_xxxxxxxx'

interface DemoOrphanItem {
  Id: string
  Name: string
  Type: string
  LibraryId: string
  Path: string
  Year?: number
  IsStrm: boolean
}

interface DemoScrapeCandidate {
  ProviderId: string
  ProviderName?: string
  Title: string
  OriginalTitle?: string
  Year?: number
  Overview?: string
  Rating?: number
  PosterUrl?: string
  Confidence: number
  Genres?: string[]
  Tags?: string[]
}

interface DemoScrapeAuditItem {
  Id: string
  ItemId: string
  ItemName: string
  ItemType: string
  Path: string
  Confidence: number
  Candidates: DemoScrapeCandidate[]
  Status: 'pending' | 'approved' | 'dismissed'
  CreatedAt: number
  UpdatedAt: number
}

function createInitialDemoOrphans(): DemoOrphanItem[] {
  return [
    {
      Id: 'orphan-demo-01',
      Name: '失落的测试样片 (磁盘已移除)',
      Type: 'Movie',
      LibraryId: 'lib-movies',
      Path: '/media/movies/Archived/Lost.Sample.2021.mkv',
      Year: 2021,
      IsStrm: false,
    },
  ]
}

function createInitialDemoReviews(): DemoScrapeAuditItem[] {
  const nowSec = Math.floor(Date.now() / 1000)
  return [
    {
      Id: 'audit-demo-01',
      ItemId: 'item-dune-2',
      ItemName: 'Dune.Part.Two.2024.2160p.REMUX.mkv',
      ItemType: 'Movie',
      Path: '/media/movies/Dune.Part.Two.2024.2160p.REMUX.mkv',
      Confidence: 0.68,
      Status: 'pending',
      CreatedAt: nowSec - 3600,
      UpdatedAt: nowSec - 3600,
      Candidates: [
        {
          ProviderId: '693134',
          ProviderName: 'TheMovieDb',
          Title: '沙丘 2',
          OriginalTitle: 'Dune: Part Two',
          Year: 2024,
          Overview: '保罗·厄崔迪与契妮和弗雷曼人联手，踏上复仇与宿命之战。',
          Rating: 8.6,
          Confidence: 0.96,
          Genres: ['科幻', '冒险'],
          Tags: ['TMDB热门'],
        },
      ],
    },
  ]
}

let demoOrphans: DemoOrphanItem[] = createInitialDemoOrphans()
let demoReviews: DemoScrapeAuditItem[] = createInitialDemoReviews()

function createInitialServerConfig(): ServerConfiguration {
  return {
    ServerName: store.systemInfo.ServerName,
    IsStartupWizardCompleted: true,
    TMDBApiKey: DEMO_MASKED_SECRET,
    HasTMDBApiKey: true,
    TMDBLanguage: 'zh-CN',
    EnableSTRM302: true,
    EnableDLNA: false,
    DLNADefaultUserID: '',
    EnableQuickConnect: true,
    EnableLoginUserList: false,
    EnableRemoteAccess: true,
    LocalNetworkSubnets: ['192.168.0.0/16', '10.0.0.0/8'],
    TrustedProxies: ['127.0.0.1', '::1'],
    AllowedOrigins: [],
    SimultaneousStreamLimit: 0,
    RemoteClientBitrateLimit: 0,
    PlayedThreshold: 0.9,
    PreferredMetadataLanguage: 'zh',
    MetadataCountryCode: 'CN',
    UICulture: 'zh-cn',
  }
}

function createInitialFfmpegOptions(): EncodingFfmpegOptions {
  return {
    HardwareAccelerationMode: 1,
    EnableThrottling: true,
    ThrottleBufferSize: 120,
    ThrottleHysteresis: 8,
    H264Crf: 23,
    H264Preset: 'veryfast',
    EnableHardwareEncoding: true,
    AllowHevcEncoding: true,
    EnableSubtitleExtraction: true,
    TranscodingTempPath: store.systemInfo.TranscodingTempPath || '/tmp/ambra-transcode',
    EncoderAppPathDisplay: 'ffmpeg',
  }
}

function createInitialToneMapOptions(): ToneMapOptions {
  return {
    EnableToneMapping: true,
    ToneMappingAlgorithm: 'hable',
    ToneMappingMode: 'auto',
    ToneMappingDesat: 0,
    ToneMappingParam: 0,
    ToneMappingPeak: 100,
  }
}

let demoServerConfig: ServerConfiguration = createInitialServerConfig()
let demoFfmpegOptions: EncodingFfmpegOptions = createInitialFfmpegOptions()
let demoToneMapOptions: ToneMapOptions = createInitialToneMapOptions()

interface DemoMigrationConflict {
  user_name?: string
  source_item_id?: string
  item_name?: string
  item_type?: string
  reason: string
}

interface DemoMigrationReport {
  status: 'idle' | 'running' | 'completed' | 'cancelled' | 'failed'
  dry_run: boolean
  source_url?: string
  source_server_name?: string
  started_at?: string
  completed_at?: string
  total_users: number
  processed_users: number
  current_user?: string
  users_matched: number
  users_created: number
  items_scanned: number
  items_matched: number
  items_skipped: number
  played_migrated: number
  favorites_migrated: number
  resume_migrated: number
  conflicts: DemoMigrationConflict[]
  errors: string[]
  resumed_from_checkpoint: boolean
  checkpoint_key?: string
}

interface DemoMigrationState {
  report: DemoMigrationReport
  migrateUsers: boolean
  migratePlayed: boolean
  migrateFavorites: boolean
  migrateResume: boolean
  checkpointByUrl: Map<string, { checkpointKey: string; processedUsers: number }>
}

const DEMO_MIGRATION_SOURCE_USERS = [
  {
    Id: 'demo-u-1',
    Name: 'Admin',
    Policy: { IsAdministrator: true, IsDisabled: false },
    LastActivityDate: new Date(Date.now() - 3600_000).toISOString(),
  },
  {
    Id: 'demo-u-2',
    Name: '家庭观影',
    Policy: { IsAdministrator: false, IsDisabled: false },
    LastActivityDate: new Date(Date.now() - 86400_000).toISOString(),
  },
  {
    Id: 'demo-u-3',
    Name: '访客账号',
    Policy: { IsAdministrator: false, IsDisabled: false },
    LastActivityDate: new Date(Date.now() - 7 * 86400_000).toISOString(),
  },
]

function createInitialMigrationState(): DemoMigrationState {
  return {
    report: {
      status: 'idle',
      dry_run: false,
      total_users: 0,
      processed_users: 0,
      users_matched: 0,
      users_created: 0,
      items_scanned: 0,
      items_matched: 0,
      items_skipped: 0,
      played_migrated: 0,
      favorites_migrated: 0,
      resume_migrated: 0,
      conflicts: [],
      errors: [],
      resumed_from_checkpoint: false,
    },
    migrateUsers: true,
    migratePlayed: true,
    migrateFavorites: true,
    migrateResume: true,
    checkpointByUrl: new Map(),
  }
}

let demoMigrationState: DemoMigrationState = createInitialMigrationState()

function createInitialTunerHosts(): LiveTvTunerHostInfo[] {
  return [
    {
      Id: 'tuner-demo-01',
      Type: 'm3u',
      FriendlyName: 'IPTV 高清演示源',
      Url: 'https://demo.ambra.local/iptv-hd.m3u',
      TunerCount: 4,
    },
  ]
}

function createInitialListingProviders(): LiveTvListingProviderInfo[] {
  return [
    {
      Id: 'epg-demo-01',
      Type: 'xmltv',
      Path: 'https://demo.ambra.local/epg.xml',
    },
  ]
}

function createInitialLiveTvChannels(): LiveTvChannelInfo[] {
  return [
    {
      Id: 'ch-demo-01',
      Name: 'CCTV-1 综合高清',
      Number: '1',
      ChannelType: 'TV',
      Type: 'TvChannel',
      StreamUrl: 'https://demo.ambra.local/live/cctv1.m3u8',
    },
    {
      Id: 'ch-demo-02',
      Name: 'CCTV-6 电影频道',
      Number: '6',
      ChannelType: 'TV',
      Type: 'TvChannel',
      StreamUrl: 'https://demo.ambra.local/live/cctv6.m3u8',
    },
    {
      Id: 'ch-demo-03',
      Name: '4K 超高清测试台',
      Number: '401',
      ChannelType: 'TV',
      Type: 'TvChannel',
      StreamUrl: 'https://demo.ambra.local/live/uhd4k.m3u8',
    },
  ]
}

interface DemoSyncJob {
  Id: string
  Name: string
  Status: string
  Progress: number
  TargetId: string
  TargetName: string
  Quality: string
  Profile: string
  Bitrate: number
  UserId: string
  ItemCount: number
  DateCreated: string
  DateLastModified: string
}

function createInitialDemoSyncJobs(): DemoSyncJob[] {
  const now = Date.now()
  return [
    {
      Id: 'sync-job-demo-01',
      Name: '星际穿越 (4K -> 1080p 离线缓存)',
      Status: 'Converting',
      Progress: 64,
      TargetId: 'device-ipad-pro-demo',
      TargetName: 'iPad Pro 13" (Offline)',
      Quality: 'High',
      Profile: '1080p H.264',
      Bitrate: 8_000_000,
      UserId: 'demo-user-admin',
      ItemCount: 1,
      DateCreated: new Date(now - 25 * 60_000).toISOString(),
      DateLastModified: new Date(now - 2 * 60_000).toISOString(),
    },
    {
      Id: 'sync-job-demo-02',
      Name: '三体 S01 合集 (720p 移动端同步)',
      Status: 'Completed',
      Progress: 100,
      TargetId: 'device-iphone-demo',
      TargetName: 'iPhone 16 Pro',
      Quality: 'Medium',
      Profile: '720p H.264',
      Bitrate: 4_000_000,
      UserId: 'demo-user-admin',
      ItemCount: 8,
      DateCreated: new Date(now - 3 * 3600_000).toISOString(),
      DateLastModified: new Date(now - 45 * 60_000).toISOString(),
    },
  ]
}

let demoTunerHosts: LiveTvTunerHostInfo[] = createInitialTunerHosts()
let demoListingProviders: LiveTvListingProviderInfo[] = createInitialListingProviders()
let demoLiveTvChannels: LiveTvChannelInfo[] = createInitialLiveTvChannels()
let demoSyncJobs: DemoSyncJob[] = createInitialDemoSyncJobs()

function createInitialLiveTvTimers(): LiveTvTimerInfo[] {
  const start = new Date(Date.now() + 3600_000)
  const end = new Date(Date.now() + 7200_000)
  return [
    {
      Id: 'timer-demo-01',
      ChannelId: 'ch-demo-02',
      ChannelName: 'CCTV-6 电影频道',
      ProgramId: 'prog-ch-demo-02-1',
      Name: '佳片有约：经典科幻之夜',
      Overview: '定时录制 CCTV-6 电影频道晚间精选影片',
      StartDate: start.toISOString(),
      EndDate: end.toISOString(),
      Status: 'New',
      PrePaddingSeconds: 120,
      PostPaddingSeconds: 300,
    },
  ]
}

function createInitialLiveTvSeriesTimers(): LiveTvSeriesTimerInfo[] {
  return [
    {
      Id: 'stimer-demo-01',
      ChannelId: 'ch-demo-01',
      Name: '新闻联播每日连录',
      Overview: '自动按 EPG 节目单匹配并录制最新一期节目',
      RecordAnyTime: false,
      RecordAnyChannel: false,
      RecordNewOnly: true,
      Days: ['Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday', 'Sunday'],
      PrePaddingSeconds: 60,
      PostPaddingSeconds: 180,
    },
  ]
}

function createInitialLiveTvChannelMappings(): LiveTvChannelMapping[] {
  return [
    {
      ProviderId: 'epg-demo-01',
      TunerChannelId: 'ch-demo-01',
      TunerChannelName: 'CCTV-1 综合高清',
      ProviderChannelId: 'cctv1.cn',
      ProviderChannelName: 'CCTV-1 HD (XMLTV)',
    },
    {
      ProviderId: 'epg-demo-01',
      TunerChannelId: 'ch-demo-02',
      TunerChannelName: 'CCTV-6 电影频道',
      ProviderChannelId: 'cctv6.cn',
      ProviderChannelName: 'CCTV-6 Movies (XMLTV)',
    },
  ]
}

function createInitialLiveTvRecordings(): LiveTvRecordingInfo[] {
  const start = new Date(Date.now() - 26 * 3600_000)
  const end = new Date(Date.now() - 24 * 3600_000)
  return [
    {
      Id: 'rec-demo-01',
      ChannelId: 'ch-demo-01',
      ChannelName: 'CCTV-1 综合高清',
      Name: '新闻联播（录制回看）',
      Overview: '已完成录制的示范电视节目',
      StartDate: start.toISOString(),
      EndDate: end.toISOString(),
      Status: 'Completed',
      Path: '/data/recordings/cctv1-news-demo.ts',
    },
  ]
}

function sanitizeWebhookForResponse(w: WebhookInfo): WebhookInfo {
  const { Secret: _secret, ...rest } = w
  return {
    ...rest,
    HasSecret: Boolean(w.Secret || w.HasSecret),
    Events: [...w.Events],
  }
}

function validateHttpUrl(rawUrl: string, label: string): string {
  const trimmed = rawUrl.trim()
  if (!trimmed) {
    throw new ApiError(400, `${label}不能为空`)
  }
  let parsed: URL
  try {
    parsed = new URL(trimmed)
  } catch {
    throw new ApiError(400, `${label}格式无效，必须为合法的 http:// 或 https:// 地址`)
  }
  if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') {
    throw new ApiError(400, `${label}必须使用 http:// 或 https:// 协议`)
  }
  return trimmed
}

function isSensitivePluginConfigKey(key: string): boolean {
  const lower = key.trim().toLowerCase()
  return (
    lower === 'api_key' ||
    lower === 'apikey' ||
    lower === 'tmdb_api_key' ||
    lower === 'tvdb_api_key' ||
    lower === 'pin' ||
    lower === 'tvdb_pin' ||
    lower === 'secret' ||
    lower === 'token' ||
    lower === 'access_token' ||
    lower === 'accesstoken' ||
    lower === 'password' ||
    lower === 'bot_token' ||
    lower.endsWith('_secret') ||
    lower.endsWith('_token') ||
    lower.endsWith('_key') ||
    lower.endsWith('secret') ||
    lower.endsWith('token') ||
    lower.endsWith('apikey') ||
    lower.endsWith('password')
  )
}

function maskPluginConfigForResponse(cfg: Record<string, unknown>): Record<string, unknown> {
  const masked: Record<string, unknown> = {}
  for (const [key, val] of Object.entries(cfg)) {
    if (isSensitivePluginConfigKey(key) && typeof val === 'string' && val.trim() !== '') {
      masked[key] = DEMO_MASKED_SECRET
    } else {
      masked[key] = val
    }
  }
  return masked
}

function mergePluginConfigForSave(
  prev: Record<string, unknown>,
  incoming: Record<string, unknown>,
): Record<string, unknown> {
  const prevLowerMap = new Map<string, string>()
  for (const [k, v] of Object.entries(prev)) {
    if (typeof v === 'string' && v.trim() !== '') {
      prevLowerMap.set(k.toLowerCase(), v)
    }
  }
  const merged: Record<string, unknown> = {}
  for (const [key, val] of Object.entries(incoming)) {
    if (isSensitivePluginConfigKey(key) && typeof val === 'string') {
      const trimmed = val.trim()
      if (trimmed === DEMO_MASKED_SECRET) {
        const lower = key.toLowerCase()
        const existing =
          (typeof prev[key] === 'string' && (prev[key] as string).trim() !== ''
            ? (prev[key] as string)
            : prevLowerMap.get(lower)) ||
          (lower.includes('key')
            ? prevLowerMap.get('apikey') || prevLowerMap.get('api_key')
            : undefined) ||
          (lower.includes('pin')
            ? prevLowerMap.get('pin') || prevLowerMap.get('tvdb_pin')
            : undefined)
        merged[key] = existing || ''
      } else if (trimmed === '') {
        merged[key] = ''
      } else {
        merged[key] = trimmed
      }
    } else {
      merged[key] = val
    }
  }
  for (const [key, val] of Object.entries(prev)) {
    if (
      !(key in merged) &&
      isSensitivePluginConfigKey(key) &&
      typeof val === 'string' &&
      val.trim() !== ''
    ) {
      const lower = key.toLowerCase()
      const explicitlyCleared = Object.entries(incoming).some(
        ([ik, iv]) =>
          typeof iv === 'string' &&
          iv.trim() === '' &&
          (ik.toLowerCase() === lower ||
            (lower.includes('key') &&
              (ik.toLowerCase() === 'api_key' || ik.toLowerCase() === 'apikey')) ||
            (lower.includes('pin') &&
              (ik.toLowerCase() === 'pin' || ik.toLowerCase() === 'tvdb_pin'))),
      )
      if (!explicitlyCleared) {
        merged[key] = val
      }
    }
  }
  return merged
}

function resolveCanonicalPluginId(pluginId: string): string {
  const lower = pluginId.toLowerCase()
  if (lower === 'ambra.builtin.tmdb') return 'ambra.metadata.tmdb'
  if (lower === 'ambra.builtin.strm302' || lower === 'ambra.builtin.strm') {
    return 'ambra.stream.strm302'
  }
  if (lower === 'ambra.builtin.chinese-subtitle' || lower === 'ambra.builtin.subtitle') {
    return 'ambra.subtitle.chinese-variant'
  }
  return pluginId
}

function createInitialDemoWebhooks(): WebhookInfo[] {
  return [
    {
      Id: 'wh-demo-01',
      Name: 'Discord 媒体入库通知',
      Url: 'https://discord.com/api/webhooks/demo/ambra-notify',
      Secret: 'demo-hmac-secret',
      HasSecret: true,
      Events: ['LibraryChanged', 'PlaybackStart'],
      Enabled: true,
      CreatedAt: Math.floor(Date.now() / 1000) - 86400,
    },
  ]
}

let demoLiveTvTimers: LiveTvTimerInfo[] = createInitialLiveTvTimers()
let demoLiveTvSeriesTimers: LiveTvSeriesTimerInfo[] = createInitialLiveTvSeriesTimers()
let demoLiveTvChannelMappings: LiveTvChannelMapping[] = createInitialLiveTvChannelMappings()
let demoLiveTvRecordings: LiveTvRecordingInfo[] = createInitialLiveTvRecordings()
let demoWebhooks: WebhookInfo[] = createInitialDemoWebhooks()

const DEMO_PARENTAL_RATINGS: ParentalRatingOption[] = [
  { Name: 'G', Value: 1 },
  { Name: 'PG', Value: 5 },
  { Name: 'PG-13', Value: 7 },
  { Name: 'R', Value: 9 },
  { Name: 'NC-17', Value: 10 },
]

function parseParentalRatingValue(raw?: string): number | null {
  if (!raw) return null
  const upper = raw.trim().toUpperCase()
  if (!upper || upper === 'NR' || upper === 'UNRATED' || upper === 'NOT RATED') {
    return null
  }
  if (upper === 'G' || upper === 'TV-Y' || upper === 'TV-G' || upper === 'ALL' || upper === '0+') {
    return 1
  }
  if (upper === 'PG' || upper === 'TV-Y7' || upper === 'TV-PG' || upper === '7+' || upper === '6+') {
    return 5
  }
  if (upper === 'PG-13' || upper === 'TV-14' || upper === '12' || upper === '12+' || upper === '13+') {
    return 7
  }
  if (upper === 'R' || upper === 'TV-MA' || upper === '15' || upper === '16+' || upper === '18') {
    return 9
  }
  if (upper === 'NC-17' || upper === '18+' || upper === 'X' || upper === 'AO') {
    return 10
  }
  const matched = DEMO_PARENTAL_RATINGS.find((r) => r.Name.toUpperCase() === upper)
  return matched ? matched.Value : null
}

function createInitialDemoBackups(): BackupFileInfo[] {
  return [
    {
      Name: 'ambra-backup-20261124-030000.zip',
      Path: '/data/config/backups/ambra-backup-20261124-030000.zip',
      Size: 2_621_440,
      DateCreated: new Date(Date.now() - 8 * 3600_000).toISOString(),
    },
    {
      Name: 'ambra-backup-20261123-030000.zip',
      Path: '/data/config/backups/ambra-backup-20261123-030000.zip',
      Size: 2_490_368,
      DateCreated: new Date(Date.now() - 32 * 3600_000).toISOString(),
    },
  ]
}

let demoBackups: BackupFileInfo[] = createInitialDemoBackups()

interface DemoNotificationRecord {
  Id: string
  UserId?: string
  Date: string
  Name: string
  Description?: string
  Url?: string
  Level: 'Normal' | 'Warning' | 'Error' | string
  defaultRead?: boolean
  readByUsers: Set<string>
}

interface DemoQuickConnectSession extends QuickConnectResult {
  AuthorizedUserId?: string
}

function createInitialDemoNotifications(): DemoNotificationRecord[] {
  const now = Date.now()
  return [
    {
      Id: 'notif-demo-01',
      Date: new Date(now - 15 * 60_000).toISOString(),
      Name: '欢迎体验 Ambra v0.12.3',
      Description: '已上线应用内通知中心、家长控制分级过滤与 Quick Connect 设备授权码登录。',
      Level: 'Normal',
      readByUsers: new Set(),
    },
    {
      Id: 'notif-demo-02',
      Date: new Date(now - 2 * 3600_000).toISOString(),
      Name: '媒体库扫描完成',
      Description: '「电影」与「剧集」媒体库已完成增量扫描，共识别 20 部影片与剧集。',
      Level: 'Normal',
      readByUsers: new Set(),
    },
    {
      Id: 'notif-demo-03',
      Date: new Date(now - 12 * 3600_000).toISOString(),
      Name: '检测到 1 条待复核刮削记录',
      Description: '可在「管理控制台 → 媒体治理」中查看并确认低置信度识别结果。',
      Url: '/admin',
      Level: 'Warning',
      defaultRead: true,
      readByUsers: new Set(),
    },
  ]
}

let demoNotifications: DemoNotificationRecord[] = createInitialDemoNotifications()
let demoQuickConnectSessions = new Map<string, DemoQuickConnectSession>()

const DEMO_TMDB_TRENDING_TMDB_IDS = new Set([
  '693134', // 沙丘 2
  '872585', // 奥本海默
  '157336', // 星际穿越
  '842675', // 流浪地球 2
  '1046090', // 周处除三害
  '1022789', // 头脑特工队 2
  '204541', // 三体
  '1429', // 进击的巨人
  '126308', // 幕府将军
])

const DEMO_DOUBAN_TRENDING_MOVIE_TMDB_IDS = new Set([
  '1046090', // 周处除三害
  '1022789', // 头脑特工队 2
  '838240', // 机器人之梦
  '693134', // 沙丘 2
  '872585', // 奥本海默
  '842675', // 流浪地球 2
])

const DEMO_DOUBAN_TRENDING_TV_TMDB_IDS = new Set([
  '126308', // 幕府将军
  '126506', // 繁花
  '224937', // 漫长的季节
  '204541', // 三体
  '1429', // 进击的巨人
])

const DEMO_DOUBAN_TOP250_TMDB_IDS = new Set([
  '278', // 肖申克的救赎
  '238', // 教父
  '155', // 蝙蝠侠：黑暗骑士
  '10997', // 霸王别姬
  '13', // 阿甘正传
  '27205', // 盗梦空间
  '157336', // 星际穿越
  '129', // 千与千寻
  '872585', // 奥本海默
  '693134', // 沙丘 2
])

const DEMO_IMDB_TOP250_TMDB_IDS = new Set([
  '278',
  '238',
  '155',
  '13',
  '27205',
  '157336',
  '129',
  '872585',
  '693134',
  '335984', // 银翼杀手 2049
])

const demoRevokedTokens = new Set<string>()
const demoPlaylistOwners = new Map<string, string>()

/**
 * 重置 Demo 内存态数据（供单元测试与演示重置使用）
 */
export function resetDemoStore(): void {
  store = createInitialDemoState()
  demoTraktAccounts.clear()
  demoTokenUserMap.clear()
  demoRevokedTokens.clear()
  demoPlaylistOwners.clear()
  seedInitialApiKeys()
  rankingsState = createInitialRankingsState()
  activeDemoScan = null
  demoSetupCompleted = true
  demoDefaultLanguage = 'zh-CN'
  demoRawTmdbApiKey = 'demo_tmdb_v3_key_xxxxxxxx'
  demoServerConfig = createInitialServerConfig()
  demoFfmpegOptions = createInitialFfmpegOptions()
  demoToneMapOptions = createInitialToneMapOptions()
  demoMigrationState = createInitialMigrationState()
  demoOrphans = createInitialDemoOrphans()
  demoReviews = createInitialDemoReviews()
  demoTunerHosts = createInitialTunerHosts()
  demoListingProviders = createInitialListingProviders()
  demoLiveTvChannels = createInitialLiveTvChannels()
  demoLiveTvTimers = createInitialLiveTvTimers()
  demoLiveTvSeriesTimers = createInitialLiveTvSeriesTimers()
  demoLiveTvChannelMappings = createInitialLiveTvChannelMappings()
  demoLiveTvRecordings = createInitialLiveTvRecordings()
  demoSyncJobs = createInitialDemoSyncJobs()
  demoWebhooks = createInitialDemoWebhooks()
  demoBackups = createInitialDemoBackups()
  demoNotifications = createInitialDemoNotifications()
  demoQuickConnectSessions = new Map<string, DemoQuickConnectSession>()
}

export function getDemoImageUrl(
  itemId: string,
  type: 'Primary' | 'Backdrop' | 'Thumb' | 'Logo' | 'Banner' = 'Primary',
): string {
  const art = store.artById.get(itemId)
  if (art) {
    return type === 'Backdrop' ? art.backdrop : art.poster
  }
  const item = store.items.get(itemId)
  if (item) {
    return makePosterDataUrl({
      title: item.Name,
      subtitle: item.OriginalTitle,
      year: item.ProductionYear,
      palette: ['#1e1b4b', '#312e81', '#f59e0b'],
    })
  }
  return makePosterDataUrl({
    title: 'Ambra',
    subtitle: itemId,
    palette: ['#18181b', '#27272a', '#f59e0b'],
  })
}

export function getDemoFallbackImageUrl(
  itemId: string,
  type: 'Primary' | 'Backdrop' | 'Thumb' | 'Logo' | 'Banner' = 'Primary',
): string {
  const art = store.artById.get(itemId)
  if (art) {
    return type === 'Backdrop'
      ? art.fallbackBackdrop || art.backdrop
      : art.fallbackPoster || art.poster
  }
  return getDemoImageUrl(itemId, type)
}

export function getDemoAudioUrl(_itemId: string): string {
  return makeDemoAudioDataUrl()
}

export function getDemoUserImageUrl(userId: string): string {
  const existing = store.userAvatarById.get(userId)
  if (existing) return existing
  const rec = store.users.find((u) => u.user.Id === userId)
  const avatar = makeAvatarDataUrl(rec?.user.Name || 'U', ['#d97706', '#78350f'])
  store.userAvatarById.set(userId, avatar)
  return avatar
}

/**
 * 中英文及多关键字任意顺序/乱序模糊匹配（覆盖片名 / 原名 / 排序名 / 年份 / 类型 / 演职员 / 所属剧集）
 */
export function matchesFuzzySearch(item: BaseItemDto, rawTerm: string): boolean {
  const trimmed = rawTerm.trim().toLowerCase()
  if (!trimmed) return true

  const haystack = [
    item.Name,
    item.OriginalTitle,
    item.SortName,
    item.SeriesName,
    item.ProductionYear ? String(item.ProductionYear) : '',
    ...(item.Genres || []),
    ...(item.Tags || []),
    ...(item.Studios?.map((s) => s.Name) || []),
    ...(item.People?.map((p) => p.Name) || []),
  ]
    .filter(Boolean)
    .join(' ')
    .toLowerCase()

  const tokens = trimmed.split(/\s+/).filter(Boolean)
  if (tokens.length > 1) {
    return tokens.every((tok) => haystack.includes(tok))
  }

  if (haystack.includes(trimmed)) {
    return true
  }

  const chars = Array.from(trimmed).filter((ch) => !/\s/.test(ch))
  if (chars.length >= 2 && /[\u4e00-\u9fff]/.test(trimmed)) {
    return chars.every((ch) => haystack.includes(ch))
  }

  return false
}

function parseJsonBody(options?: RequestInit): Record<string, unknown> {
  if (!options?.body || typeof options.body !== 'string') return {}
  try {
    const parsed = JSON.parse(options.body)
    return parsed && typeof parsed === 'object' ? (parsed as Record<string, unknown>) : {}
  } catch {
    return {}
  }
}

function pushActivity(
  name: string,
  type: string,
  shortOverview?: string,
  userId?: string,
  severity = 'Info',
): void {
  store.activityLogs.unshift({
    Id: store.nextActivityId++,
    Name: name,
    ShortOverview: shortOverview,
    Type: type,
    Date: new Date().toISOString(),
    UserId: userId,
    Severity: severity,
  })
}

function extractTokenFromAuthHeader(rawHeader: string | null): string | null {
  if (!rawHeader) return null
  const trimmed = rawHeader.trim()
  if (!trimmed) return null
  const tokenParamMatch = trimmed.match(/Token="([^"]+)"/i) || trimmed.match(/Token=([^,\s"]+)/i)
  if (tokenParamMatch?.[1]) {
    return tokenParamMatch[1].trim()
  }
  const bearerMatch = trimmed.match(/^Bearer\s+(.+)$/i)
  if (bearerMatch?.[1]) {
    return bearerMatch[1].trim()
  }
  return null
}

function hasExplicitAuthCredentialHeader(rawHeader: string | null): boolean {
  if (rawHeader === null) return false
  const trimmed = rawHeader.trim()
  if (!trimmed) return false
  if (/Token\s*=/i.test(trimmed)) return true
  if (!/^MediaBrowser\b/i.test(trimmed)) return true
  return false
}

function extractDemoToken(options: RequestInit, qs: URLSearchParams): {
  token: string | null
  fromExplicitHeaderOrQuery: boolean
} {
  const headers = new Headers(options.headers || {})
  const rawAuth = headers.get('Authorization')
  const stdAuthToken = extractTokenFromAuthHeader(rawAuth)
  if (stdAuthToken || hasExplicitAuthCredentialHeader(rawAuth)) {
    return { token: stdAuthToken, fromExplicitHeaderOrQuery: true }
  }
  const rawEmbyAuth = headers.get('X-Emby-Authorization')
  const embyAuthToken = extractTokenFromAuthHeader(rawEmbyAuth)
  if (embyAuthToken || hasExplicitAuthCredentialHeader(rawEmbyAuth)) {
    return { token: embyAuthToken, fromExplicitHeaderOrQuery: true }
  }
  if (headers.has('X-Emby-Token') || headers.has('X-MediaBrowser-Token')) {
    const directHeaderToken =
      headers.get('X-Emby-Token')?.trim() || headers.get('X-MediaBrowser-Token')?.trim() || null
    return { token: directHeaderToken, fromExplicitHeaderOrQuery: true }
  }
  if (qs.has('api_key') || qs.has('ApiKey')) {
    const queryToken = qs.get('api_key')?.trim() || qs.get('ApiKey')?.trim() || null
    return { token: queryToken, fromExplicitHeaderOrQuery: true }
  }
  const stored = getStoredAuth()
  if (stored?.accessToken) {
    return { token: stored.accessToken, fromExplicitHeaderOrQuery: false }
  }
  return { token: null, fromExplicitHeaderOrQuery: false }
}

interface DemoCaller {
  token: string | null
  userId: string
  user?: UserDto
  isAdmin: boolean
  isApiKey?: boolean
}

function resolveDemoCaller(options: RequestInit, qs: URLSearchParams): DemoCaller {
  const unauthenticated: DemoCaller = {
    token: null,
    userId: '',
    user: undefined,
    isAdmin: false,
  }
  const { token, fromExplicitHeaderOrQuery } = extractDemoToken(options, qs)
  if (!token || demoRevokedTokens.has(token)) {
    return unauthenticated
  }

  // 1. 仅精确匹配服务端签发的登录会话 Token
  const mappedUserId = demoTokenUserMap.get(token)
  if (mappedUserId) {
    const rec = store.users.find((u) => u.user.Id === mappedUserId)
    if (rec) {
      return {
        token,
        userId: rec.user.Id,
        user: rec.user,
        isAdmin: Boolean(rec.user.Policy?.IsAdministrator),
      }
    }
    return unauthenticated
  }

  // 2. 仅精确匹配服务端签发且未吊销的管理员 API Key
  for (const apiKey of store.apiKeys) {
    const raw = (apiKey.Id && demoRawApiKeys.get(apiKey.Id)) || apiKey.AccessToken
    if (apiKey.IsActive !== false && raw && raw === token) {
      const adminRec = store.users.find((u) => u.user.Policy?.IsAdministrator) || store.users[0]
      return {
        token,
        userId: adminRec?.user.Id || 'user-demo-admin',
        user: adminRec?.user,
        isAdmin: true,
        isApiKey: true,
      }
    }
  }

  const stored = getStoredAuth()

  // 3. 若显式携带了 Authorization / X-Emby-Token / api_key 凭证但未知或已吊销，
  //    绝不回退到 localStorage 当前登录用户（仅当显式 Token 恰好等于当前有效会话 Token 时允许匹配）
  if (fromExplicitHeaderOrQuery) {
    if (stored && stored.accessToken === token && stored.userId) {
      const rec = store.users.find((u) => u.user.Id === stored.userId)
      if (rec) {
        return {
          token,
          userId: rec.user.Id,
          user: rec.user,
          isAdmin: Boolean(rec.user.Policy?.IsAdministrator),
        }
      }
      return {
        token,
        userId: stored.userId,
        user: undefined,
        isAdmin: Boolean(stored.isAdmin),
      }
    }
    return unauthenticated
  }

  // 4. 未显式传入凭证时使用 localStorage 当前会话，并优先从 store.users 读取权威角色（杜绝篡改 localStorage 越权）
  if (stored?.userId && stored.accessToken === token) {
    const rec = store.users.find((u) => u.user.Id === stored.userId)
    if (rec) {
      return {
        token,
        userId: rec.user.Id,
        user: rec.user,
        isAdmin: Boolean(rec.user.Policy?.IsAdministrator),
      }
    }
    // 兼容单元测试中构造的虚拟 userId（如 user-admin）
    return {
      token,
      userId: stored.userId,
      user: undefined,
      isAdmin: Boolean(stored.isAdmin),
    }
  }

  return unauthenticated
}

function requireDemoAdmin(caller: DemoCaller): void {
  if (!caller.isAdmin) {
    throw new ApiError(403, '需要管理员权限 (403 Forbidden)')
  }
}

function requireSelfOrAdmin(caller: DemoCaller, targetUserId: string): void {
  if (!targetUserId) return
  if (!caller.isAdmin && caller.userId !== targetUserId) {
    throw new ApiError(403, '无权访问或修改其他用户的数据 (403 Forbidden)')
  }
}

function resolveUserDto(userId: string, fallbackCaller: DemoCaller): UserDto | undefined {
  if (userId) {
    const rec = store.users.find((u) => u.user.Id === userId)
    if (rec) return rec.user
  }
  return fallbackCaller.user
}

function getUserDataMap(userId: string): Map<string, UserItemDataDto> {
  const effectiveUserId = userId || 'user-demo-admin'
  let map = store.userDataByUser.get(effectiveUserId)
  if (!map) {
    map = new Map<string, UserItemDataDto>()
    store.userDataByUser.set(effectiveUserId, map)
  }
  return map
}

function ensureUserDataForUser(userId: string, itemId: string): UserItemDataDto {
  const map = getUserDataMap(userId)
  let ud = map.get(itemId)
  if (!ud) {
    ud = {
      PlaybackPositionTicks: 0,
      PlayCount: 0,
      IsFavorite: false,
      Played: false,
    }
    map.set(itemId, ud)
  }
  return ud
}

function getUserDataSnapshot(userId: string, itemId: string): UserItemDataDto {
  const existing = store.userDataByUser.get(userId || 'user-demo-admin')?.get(itemId)
  if (existing) {
    return { ...existing }
  }
  return {
    PlaybackPositionTicks: 0,
    PlayCount: 0,
    IsFavorite: false,
    Played: false,
  }
}

function projectItemForUser(item: BaseItemDto, userId: string): BaseItemDto {
  const uid = userId || 'user-demo-admin'
  const ud = getUserDataSnapshot(uid, item.Id)

  if (item.Type === 'Series') {
    const seasons = store.seasonsBySeries.get(item.Id) || []
    const allEps = seasons.flatMap((s) => store.episodesBySeason.get(s.Id) || [])
    if (allEps.length > 0) {
      const unplayed = allEps.filter((ep) => !getUserDataSnapshot(uid, ep.Id).Played).length
      ud.UnplayedItemCount = unplayed
    }
  } else if (item.Type === 'Season') {
    const eps = store.episodesBySeason.get(item.Id) || []
    if (eps.length > 0) {
      const unplayed = eps.filter((ep) => !getUserDataSnapshot(uid, ep.Id).Played).length
      ud.UnplayedItemCount = unplayed
    }
  }

  return {
    ...item,
    UserData: ud,
  }
}

function getItemLibraryIds(item: BaseItemDto, visited = new Set<string>()): Set<string> {
  const result = new Set<string>()
  if (visited.has(item.Id)) return result
  visited.add(item.Id)

  const knownLibIds = new Set(store.libraries.map((l) => l.ItemId))

  for (const lib of store.libraries) {
    const ids = store.libraryItems.get(lib.ItemId)
    if (ids?.includes(item.Id)) {
      result.add(lib.ItemId)
    }
  }

  const parentCandidates = [item.ParentId, item.SeriesId, item.SeasonId, item.AlbumId].filter(
    (x): x is string => Boolean(x),
  )

  for (const pid of parentCandidates) {
    if (knownLibIds.has(pid)) {
      result.add(pid)
    } else {
      const parentItem = store.items.get(pid)
      if (parentItem) {
        for (const subLib of getItemLibraryIds(parentItem, visited)) {
          result.add(subLib)
        }
      }
    }
  }

  if (result.size === 0 && item.Type === 'MusicArtist' && knownLibIds.has('lib-music')) {
    result.add('lib-music')
  }

  return result
}

function getEffectiveOfficialRating(item: BaseItemDto): string | undefined {
  if (item.OfficialRating && item.OfficialRating.trim()) {
    return item.OfficialRating.trim()
  }
  if (item.SeriesId) {
    const series = store.items.get(item.SeriesId)
    if (series?.OfficialRating && series.OfficialRating.trim()) {
      return series.OfficialRating.trim()
    }
  }
  if (item.ParentId) {
    const parent = store.items.get(item.ParentId)
    if (parent?.OfficialRating && parent.OfficialRating.trim()) {
      return parent.OfficialRating.trim()
    }
  }
  return undefined
}

function getEffectiveItemTags(item: BaseItemDto): Set<string> {
  const set = new Set<string>()
  for (const t of item.Tags || []) {
    if (t && t.trim()) set.add(t.trim().toLowerCase())
  }
  if (item.SeriesId) {
    const series = store.items.get(item.SeriesId)
    for (const t of series?.Tags || []) {
      if (t && t.trim()) set.add(t.trim().toLowerCase())
    }
  }
  if (item.ParentId) {
    const parent = store.items.get(item.ParentId)
    for (const t of parent?.Tags || []) {
      if (t && t.trim()) set.add(t.trim().toLowerCase())
    }
  }
  return set
}

function isUnratedBlockedByPolicy(item: BaseItemDto, blockUnrated: string[]): boolean {
  const blockedTypes = new Set(blockUnrated.map((s) => s.trim().toLowerCase()).filter(Boolean))
  if (blockedTypes.size === 0) return false
  const itemTypeLower = item.Type.toLowerCase()
  if (blockedTypes.has(itemTypeLower)) return true
  if (
    blockedTypes.has('video') &&
    (item.MediaType === 'Video' ||
      ['movie', 'series', 'season', 'episode', 'video', 'trailer', 'extra'].includes(itemTypeLower))
  ) {
    return true
  }
  if (blockedTypes.has('series') && (itemTypeLower === 'season' || itemTypeLower === 'episode')) {
    return true
  }
  return false
}

export function canDemoUserAccessItem(
  user: UserDto | undefined,
  item: BaseItemDto,
  visitedContainer = new Set<string>(),
): boolean {
  const policy = user?.Policy
  if (!policy) return true

  // BoxSet / Playlist 容器按子项可见性及自身标签判定
  if (item.Type === 'BoxSet' || item.Type === 'Playlist') {
    if (visitedContainer.has(item.Id)) return true
    visitedContainer.add(item.Id)
    const childIds = store.libraryItems.get(item.Id) || []
    if (policy.BlockedTags && policy.BlockedTags.length > 0) {
      const ownTags = getEffectiveItemTags(item)
      if (policy.BlockedTags.some((bt) => bt.trim() && ownTags.has(bt.trim().toLowerCase()))) {
        return false
      }
    }
    if (childIds.length === 0) {
      return true
    }
    return childIds.some((cid) => {
      const child = store.items.get(cid)
      return child ? canDemoUserAccessItem(user, child, visitedContainer) : false
    })
  }

  // 1. 媒体库白名单 (EnableAllFolders === false)
  if (policy.EnableAllFolders === false) {
    const allowedFolders = new Set(policy.EnabledFolders || [])
    const itemLibs = getItemLibraryIds(item)
    if (itemLibs.size > 0) {
      let hasAllowedLib = false
      for (const libId of itemLibs) {
        if (allowedFolders.has(libId)) {
          hasAllowedLib = true
          break
        }
      }
      if (!hasAllowedLib) return false
    } else {
      return false
    }
  }

  // 2. 未分级内容拦截 (BlockUnratedItems) & 最高分级 (MaxParentalRating)
  const effectiveRating = getEffectiveOfficialRating(item)
  const ratingVal = parseParentalRatingValue(effectiveRating)
  if (ratingVal === null) {
    if (
      Array.isArray(policy.BlockUnratedItems) &&
      policy.BlockUnratedItems.length > 0 &&
      isUnratedBlockedByPolicy(item, policy.BlockUnratedItems)
    ) {
      return false
    }
  } else if (typeof policy.MaxParentalRating === 'number' && ratingVal > policy.MaxParentalRating) {
    return false
  }

  // 3. 屏蔽标签黑名单 (BlockedTags)
  const effectiveTags = getEffectiveItemTags(item)
  if (Array.isArray(policy.BlockedTags) && policy.BlockedTags.length > 0) {
    for (const bt of policy.BlockedTags) {
      const normalized = bt.trim().toLowerCase()
      if (normalized && effectiveTags.has(normalized)) {
        return false
      }
    }
  }

  // 4. 白名单允许标签 (AllowedTags)
  if (Array.isArray(policy.AllowedTags)) {
    const allowedTags = policy.AllowedTags.map((t) => t.trim().toLowerCase()).filter(Boolean)
    if (allowedTags.length > 0) {
      const hasAllowedTag = allowedTags.some((at) => effectiveTags.has(at))
      if (!hasAllowedTag) return false
    }
  }

  return true
}

function isLibraryAllowedForUser(user: UserDto | undefined, libraryId: string): boolean {
  const policy = user?.Policy
  if (!policy || policy.EnableAllFolders !== false) return true
  return (policy.EnabledFolders || []).includes(libraryId)
}

function getVisibleLibrariesForUser(userId: string): VirtualFolderInfo[] {
  const rec = store.users.find((u) => u.user.Id === userId)
  const policy = rec?.user.Policy
  if (!policy || policy.EnableAllFolders !== false) {
    return store.libraries
  }
  const allowed = new Set(policy.EnabledFolders || [])
  return store.libraries.filter((lib) => allowed.has(lib.ItemId))
}

function libraryToViewDto(lib: VirtualFolderInfo): BaseItemDto {
  return {
    Id: lib.ItemId,
    Name: lib.Name,
    ServerId: DEMO_SERVER_ID,
    Type: 'CollectionFolder',
    CollectionType: lib.CollectionType,
    IsFolder: true,
    ImageTags: { Primary: 'demo-lib' },
  }
}

function sortItems(items: BaseItemDto[], sortBy: string, sortOrder: string): BaseItemDto[] {
  const primary = (sortBy || 'DateCreated').split(',')[0].trim()
  const dir = sortOrder === 'Ascending' ? 1 : -1

  return [...items].sort((a, b) => {
    let cmp = 0
    if (primary === 'SortName' || primary === 'Name') {
      cmp = (a.SortName || a.Name).localeCompare(b.SortName || b.Name, 'zh-Hans-CN')
    } else if (primary === 'ParentIndexNumber' || primary === 'IndexNumber') {
      const discDiff = (a.ParentIndexNumber || 0) - (b.ParentIndexNumber || 0)
      if (discDiff !== 0) {
        cmp = discDiff
      } else {
        const trackDiff = (a.IndexNumber || 0) - (b.IndexNumber || 0)
        cmp =
          trackDiff !== 0
            ? trackDiff
            : (a.SortName || a.Name).localeCompare(b.SortName || b.Name, 'zh-Hans-CN')
      }
    } else if (primary === 'CommunityRating') {
      cmp = (a.CommunityRating || 0) - (b.CommunityRating || 0)
    } else if (primary === 'PremiereDate' || primary === 'ProductionYear') {
      cmp = (a.ProductionYear || 0) - (b.ProductionYear || 0)
    } else if (primary === 'PlayCount') {
      cmp = (a.UserData?.PlayCount || 0) - (b.UserData?.PlayCount || 0)
    } else {
      const ta = a.DateCreated ? Date.parse(a.DateCreated) : 0
      const tb = b.DateCreated ? Date.parse(b.DateCreated) : 0
      cmp = ta - tb
    }
    return cmp * dir
  })
}

function syncTmdbPluginFromServerConfig(): void {
  const prev = store.pluginConfigs.get('ambra.metadata.tmdb') || {}
  store.pluginConfigs.set('ambra.metadata.tmdb', {
    ...prev,
    ApiKey: demoRawTmdbApiKey,
    api_key: demoRawTmdbApiKey,
    Language: demoServerConfig.TMDBLanguage || 'zh-CN',
    language: demoServerConfig.TMDBLanguage || 'zh-CN',
  })
}

function syncStrmPluginFromServerConfig(): void {
  const enabled = Boolean(demoServerConfig.EnableSTRM302)
  const prev = store.pluginConfigs.get('ambra.stream.strm302') || {}
  store.pluginConfigs.set('ambra.stream.strm302', {
    ...prev,
    Enabled: enabled,
    enabled,
    Enable302: enabled,
    enable_302: enabled,
  })
  const plugin = store.plugins.find((p) => p.Id === 'ambra.stream.strm302')
  if (plugin) {
    plugin.Enabled = enabled
    plugin.Status = enabled ? 'Active' : 'Disabled'
  }
}

/**
 * 纯前端内存态 Emby Mock 请求引擎。
 * 由 src/api/client.ts 的 embyFetch 在 isDemoMode() === true 时直接调用。
 */
export async function demoFetch<T>(
  path: string,
  options: RequestInit = {},
  requireAuth = true,
): Promise<T> {
  const method = (options.method || 'GET').toUpperCase()
  const url = new URL(path.startsWith('/') ? path : `/${path}`, 'http://ambra.demo')
  const rawPathname = url.pathname
  const pathname =
    rawPathname.startsWith('/api/') || rawPathname.startsWith('/emby/')
      ? rawPathname
      : `/emby${rawPathname}`
  const qs = url.searchParams

  const caller = resolveDemoCaller(options, qs)
  if (requireAuth && !caller.token) {
    throw new ApiError(401, '未登录或登录已过期')
  }
  if (requireAuth && caller.user?.Policy?.IsDisabled) {
    throw new ApiError(403, `账号「${caller.user.Name}」已被管理员禁用`)
  }

  if (pathname === '/emby/Users/Me/Trakt') {
    if (!caller.userId) throw new ApiError(403, 'A personal user session is required')
    const enabled = store.plugins.find((p) => p.Id === 'ambra.builtin.trakt')?.Enabled === true
    if (method === 'DELETE') {
      demoTraktAccounts.delete(caller.userId)
      return undefined as T
    }
    const status: UserTraktStatus = {
      Configured: false,
      ClientId: '',
      AccessTokenConfigured: false,
      ScrobblePlayback: true,
      SyncWatched: true,
      ...demoTraktAccounts.get(caller.userId),
      Enabled: enabled,
    }
    if (method === 'POST') {
      if (!enabled) throw new ApiError(403, 'Trakt is disabled by the administrator')
      const body = parseJsonBody(options)
      const clientId = String(body.ClientId ?? status.ClientId).trim()
      const token = typeof body.AccessToken === 'string' ? body.AccessToken.trim() : undefined
      if (
        !clientId ||
        (token === undefined && (!status.Configured || clientId !== status.ClientId)) ||
        token === '' ||
        token === DEMO_MASKED_SECRET
      ) {
        throw new ApiError(400, 'A valid Client ID and Access Token are required')
      }
      status.ClientId = clientId
      status.Configured = status.AccessTokenConfigured = true
      if (typeof body.ScrobblePlayback === 'boolean') status.ScrobblePlayback = body.ScrobblePlayback
      if (typeof body.SyncWatched === 'boolean') status.SyncWatched = body.SyncWatched
      demoTraktAccounts.set(caller.userId, { ...status })
    }
    return status as T
  }

  // ==================== 0. 首次运行初始化向导 ====================
  if (pathname === '/api/setup/status' && method === 'GET') {
    const status: SetupStatus = {
      completed: demoSetupCompleted,
      server_name: store.systemInfo.ServerName,
      default_language: demoDefaultLanguage,
      has_users: store.users.length > 0,
      has_tmdb_key: Boolean(demoRawTmdbApiKey),
    }
    return status as T
  }

  if (pathname === '/api/setup/admin' && method === 'POST') {
    if (demoSetupCompleted && !caller.isAdmin) {
      throw new ApiError(403, '初始化向导已完成，仅管理员可修改管理员配置')
    }
    const body = parseJsonBody(options)
    const serverName = String(body.server_name || '').trim()
    const language = String(body.language || '').trim()
    const country = String(body.country || '').trim()
    const username = String(body.username || '').trim()
    const password = String(body.password ?? '')

    if (!username) {
      throw new ApiError(400, '管理员用户名不能为空')
    }
    if (serverName) {
      store.systemInfo.ServerName = serverName
      demoServerConfig.ServerName = serverName
    }
    if (language) {
      demoDefaultLanguage = language
      demoServerConfig.TMDBLanguage = language
      demoServerConfig.PreferredMetadataLanguage = language
    }
    if (country) {
      demoServerConfig.MetadataCountryCode = country
    }

    let rec = store.users.find((u) => u.user.Name.toLowerCase() === username.toLowerCase())
    if (!rec) {
      const id = `user-setup-${Date.now().toString(36)}`
      const newUser: UserDto = {
        Id: id,
        Name: username,
        ServerId: DEMO_SERVER_ID,
        HasPassword: password.length > 0,
        HasConfiguredPassword: password.length > 0,
        PrimaryImageTag: 'demo-user',
        LastActivityDate: new Date().toISOString(),
        Policy: {
          IsAdministrator: true,
          IsDisabled: false,
          EnableAllFolders: true,
          EnabledFolders: [],
          EnableMediaPlayback: true,
          EnableAudioPlaybackTranscoding: true,
          EnableVideoPlaybackTranscoding: true,
          EnablePlaybackRemuxing: true,
          EnableContentDeletion: true,
          EnableContentDownloading: true,
          EnableRemoteAccess: true,
        },
      }
      rec = { user: newUser, password }
      store.users.push(rec)
      store.userAvatarById.set(id, makeAvatarDataUrl(username, ['#d97706', '#78350f']))
    } else {
      rec.password = password
      rec.user.Policy = { ...rec.user.Policy, IsAdministrator: true, IsDisabled: false }
    }

    const token = `demo_tok_setup_${rec.user.Id}_${Date.now().toString(36)}`
    demoTokenUserMap.set(token, rec.user.Id)
    pushActivity(`初始化向导创建管理员「${rec.user.Name}」`, 'UserCreated', '系统初始化', rec.user.Id)
    const result: AuthenticationResult = {
      User: { ...rec.user, Policy: { ...rec.user.Policy } },
      AccessToken: token,
      ServerId: DEMO_SERVER_ID,
      SessionInfo: {
        Id: `sess-${rec.user.Id}`,
        DeviceId: 'dev-demo-web-01',
      },
    }
    return result as T
  }

  if (pathname === '/api/setup/complete' && method === 'POST') {
    if (demoSetupCompleted && !caller.isAdmin) {
      throw new ApiError(403, '需要管理员权限')
    }
    const body = parseJsonBody(options)
    const serverName = String(body.server_name || '').trim()
    const language = String(body.language || '').trim()
    const country = String(body.country || '').trim()
    const tmdbLanguage = String(body.tmdb_language || '').trim()
    if (serverName) {
      store.systemInfo.ServerName = serverName
      demoServerConfig.ServerName = serverName
    }
    if (language) {
      demoDefaultLanguage = language
      demoServerConfig.PreferredMetadataLanguage = language
      if (!tmdbLanguage) {
        demoServerConfig.TMDBLanguage = language
      }
    }
    if (country) {
      demoServerConfig.MetadataCountryCode = country
    }
    if (tmdbLanguage) {
      demoDefaultLanguage = tmdbLanguage
      demoServerConfig.TMDBLanguage = tmdbLanguage
    }
    if (typeof body.tmdb_api_key === 'string' && body.tmdb_api_key.trim()) {
      demoRawTmdbApiKey = body.tmdb_api_key.trim()
      syncTmdbPluginFromServerConfig()
    }
    if (typeof body.enable_strm_302 === 'boolean') {
      demoServerConfig.EnableSTRM302 = body.enable_strm_302
      syncStrmPluginFromServerConfig()
    }
    demoSetupCompleted = true
    store.systemInfo.StartupWizardCompleted = true
    pushActivity('已完成服务器首次运行初始化向导', 'SetupCompleted')
    return { completed: true } as T
  }

  // ==================== 1. 系统配置、转码配置与公开信息 ====================
  if (pathname === '/emby/System/Configuration') {
    requireDemoAdmin(caller)
    if (method === 'GET') {
      const hasKey = demoRawTmdbApiKey.trim().length > 0
      return {
        ...demoServerConfig,
        ServerName: store.systemInfo.ServerName,
        TMDBApiKey: hasKey ? DEMO_MASKED_SECRET : '',
        HasTMDBApiKey: hasKey,
        LocalNetworkSubnets: [...(demoServerConfig.LocalNetworkSubnets || [])],
        TrustedProxies: [...(demoServerConfig.TrustedProxies || [])],
        AllowedOrigins: [...(demoServerConfig.AllowedOrigins || [])],
      } as T
    }
    if (method === 'POST') {
      const body = parseJsonBody(options)
      if (typeof body.ServerName === 'string' && body.ServerName.trim()) {
        const nextName = body.ServerName.trim()
        demoServerConfig.ServerName = nextName
        store.systemInfo.ServerName = nextName
      }
      if (typeof body.TMDBApiKey === 'string') {
        const trimmedKey = body.TMDBApiKey.trim()
        if (trimmedKey !== DEMO_MASKED_SECRET) {
          demoRawTmdbApiKey = trimmedKey
        }
      }
      if (typeof body.TMDBLanguage === 'string' && body.TMDBLanguage.trim()) {
        demoServerConfig.TMDBLanguage = body.TMDBLanguage.trim()
        demoDefaultLanguage = body.TMDBLanguage.trim()
      }
      if (
        typeof body.PreferredMetadataLanguage === 'string' &&
        body.PreferredMetadataLanguage.trim()
      ) {
        demoServerConfig.PreferredMetadataLanguage = body.PreferredMetadataLanguage.trim()
      }
      if (typeof body.MetadataCountryCode === 'string' && body.MetadataCountryCode.trim()) {
        demoServerConfig.MetadataCountryCode = body.MetadataCountryCode.trim()
      }
      if (typeof body.EnableSTRM302 === 'boolean') {
        demoServerConfig.EnableSTRM302 = body.EnableSTRM302
      }
      if (typeof body.PlayedThreshold === 'number') {
        if (body.PlayedThreshold < 0.5 || body.PlayedThreshold > 0.99) {
          throw new ApiError(400, 'PlayedThreshold 必须在 0.50 到 0.99 之间')
        }
        demoServerConfig.PlayedThreshold = body.PlayedThreshold
      }
      if (typeof body.EnableDLNA === 'boolean') {
        demoServerConfig.EnableDLNA = body.EnableDLNA
      }
      if (typeof body.DLNADefaultUserID === 'string') {
        demoServerConfig.DLNADefaultUserID = body.DLNADefaultUserID.trim()
      }
      if (typeof body.EnableQuickConnect === 'boolean') {
        demoServerConfig.EnableQuickConnect = body.EnableQuickConnect
      }
      if (typeof body.EnableLoginUserList === 'boolean') {
        demoServerConfig.EnableLoginUserList = body.EnableLoginUserList
      }
      if (typeof body.EnableRemoteAccess === 'boolean') {
        demoServerConfig.EnableRemoteAccess = body.EnableRemoteAccess
      }
      if (Array.isArray(body.LocalNetworkSubnets)) {
        demoServerConfig.LocalNetworkSubnets = body.LocalNetworkSubnets.map((s) =>
          String(s).trim(),
        ).filter(Boolean)
      }
      if (Array.isArray(body.TrustedProxies)) {
        demoServerConfig.TrustedProxies = body.TrustedProxies.map((s) => String(s).trim()).filter(
          Boolean,
        )
      }
      if (Array.isArray(body.AllowedOrigins)) {
        demoServerConfig.AllowedOrigins = body.AllowedOrigins.map((s) => String(s).trim()).filter(
          Boolean,
        )
      }
      if (typeof body.SimultaneousStreamLimit === 'number') {
        demoServerConfig.SimultaneousStreamLimit = Math.max(
          0,
          Math.floor(body.SimultaneousStreamLimit),
        )
      }
      if (typeof body.RemoteClientBitrateLimit === 'number') {
        demoServerConfig.RemoteClientBitrateLimit = Math.max(
          0,
          Math.floor(body.RemoteClientBitrateLimit),
        )
      }
      syncTmdbPluginFromServerConfig()
      syncStrmPluginFromServerConfig()
      pushActivity('已更新服务器常规与网络设置', 'ServerConfigUpdated', undefined, caller.userId)
      return undefined as T
    }
  }

  if (pathname === '/emby/Encoding/FfmpegOptions') {
    requireDemoAdmin(caller)
    if (method === 'GET') {
      return { ...demoFfmpegOptions } as T
    }
    if (method === 'POST') {
      const body = parseJsonBody(options)
      if (typeof body.HardwareAccelerationMode === 'number') {
        demoFfmpegOptions.HardwareAccelerationMode = body.HardwareAccelerationMode
      }
      if (typeof body.EnableHardwareEncoding === 'boolean') {
        demoFfmpegOptions.EnableHardwareEncoding = body.EnableHardwareEncoding
      }
      if (typeof body.AllowHevcEncoding === 'boolean') {
        demoFfmpegOptions.AllowHevcEncoding = body.AllowHevcEncoding
      }
      if (typeof body.H264Crf === 'number') {
        demoFfmpegOptions.H264Crf = Math.min(51, Math.max(0, Math.round(body.H264Crf)))
      }
      if (typeof body.H264Preset === 'string' && body.H264Preset.trim()) {
        demoFfmpegOptions.H264Preset = body.H264Preset.trim()
      }
      if (typeof body.EnableThrottling === 'boolean') {
        demoFfmpegOptions.EnableThrottling = body.EnableThrottling
      }
      if (typeof body.ThrottleBufferSize === 'number') {
        demoFfmpegOptions.ThrottleBufferSize = Math.max(10, Math.round(body.ThrottleBufferSize))
      }
      if (typeof body.ThrottleHysteresis === 'number') {
        demoFfmpegOptions.ThrottleHysteresis = Math.max(1, Math.round(body.ThrottleHysteresis))
      }
      if (typeof body.EnableSubtitleExtraction === 'boolean') {
        demoFfmpegOptions.EnableSubtitleExtraction = body.EnableSubtitleExtraction
      }
      if (typeof body.TranscodingTempPath === 'string') {
        demoFfmpegOptions.TranscodingTempPath = body.TranscodingTempPath.trim()
        store.systemInfo.TranscodingTempPath =
          demoFfmpegOptions.TranscodingTempPath || '/tmp/ambra-transcode'
      }
      pushActivity('已更新 FFmpeg 转码与硬件加速配置', 'EncodingConfigUpdated', undefined, caller.userId)
      return undefined as T
    }
  }

  if (pathname === '/emby/Encoding/FullToneMapOptions') {
    requireDemoAdmin(caller)
    if (method === 'GET') {
      return { ...demoToneMapOptions } as T
    }
    if (method === 'POST') {
      const body = parseJsonBody(options)
      if (typeof body.EnableToneMapping === 'boolean') {
        demoToneMapOptions.EnableToneMapping = body.EnableToneMapping
      }
      if (typeof body.ToneMappingAlgorithm === 'string' && body.ToneMappingAlgorithm.trim()) {
        demoToneMapOptions.ToneMappingAlgorithm = body.ToneMappingAlgorithm.trim()
      }
      if (typeof body.ToneMappingMode === 'string' && body.ToneMappingMode.trim()) {
        demoToneMapOptions.ToneMappingMode = body.ToneMappingMode.trim()
      }
      if (typeof body.ToneMappingDesat === 'number') {
        demoToneMapOptions.ToneMappingDesat = body.ToneMappingDesat
      }
      if (typeof body.ToneMappingParam === 'number') {
        demoToneMapOptions.ToneMappingParam = body.ToneMappingParam
      }
      if (typeof body.ToneMappingPeak === 'number') {
        demoToneMapOptions.ToneMappingPeak = body.ToneMappingPeak
      }
      pushActivity('已更新 HDR 色调映射配置', 'ToneMapConfigUpdated', undefined, caller.userId)
      return undefined as T
    }
  }

  if (pathname === '/emby/System/Info/Public' && method === 'GET') {
    const pub: PublicSystemInfo = {
      Id: store.systemInfo.Id,
      ServerName: store.systemInfo.ServerName,
      Version: store.systemInfo.Version,
      ProductName: store.systemInfo.ProductName,
      LocalAddress: store.systemInfo.LocalAddress,
    }
    return pub as T
  }

  if (pathname === '/emby/System/Info' && method === 'GET') {
    requireDemoAdmin(caller)
    return { ...store.systemInfo } as T
  }

  if (pathname === '/emby/System/Restart' && method === 'POST') {
    requireDemoAdmin(caller)
    store.systemInfo.HasPendingRestart = true
    pushActivity('管理员触发了服务器重启（Demo 模拟）', 'ServerRestarting', undefined, caller.userId)
    return undefined as T
  }

  if (pathname === '/emby/System/Shutdown' && method === 'POST') {
    requireDemoAdmin(caller)
    store.systemInfo.IsShuttingDown = true
    pushActivity('管理员触发了服务器关闭（Demo 模拟）', 'ServerShuttingDown', undefined, caller.userId)
    return undefined as T
  }

  if (pathname === '/emby/Users/Public' && method === 'GET') {
    if (!demoServerConfig.EnableLoginUserList) {
      return [] as unknown as T
    }
    return store.users
      .filter((u) => !u.user.Policy?.IsDisabled && !u.user.Policy?.IsHidden)
      .map((u) => ({
        ...u.user,
        Configuration: {},
        Policy: { EnabledFolders: [], BlockedTags: [] },
      })) as T
  }

  // ==================== 2. 认证登录 ====================
  if (pathname === '/emby/Users/AuthenticateByName' && method === 'POST') {
    const body = parseJsonBody(options)
    const username = String(body.Username || '').trim()
    const pw = String(body.Pw ?? '')
    const rec = store.users.find(
      (u) => u.user.Name.toLowerCase() === username.toLowerCase(),
    )
    if (!rec) {
      throw new ApiError(401, `用户「${username}」不存在（Demo 内置账号：admin / guest）`)
    }
    if (rec.user.Policy?.IsDisabled) {
      throw new ApiError(403, `账号「${rec.user.Name}」已被管理员禁用`)
    }
    if (rec.password && rec.password !== pw) {
      throw new ApiError(401, '密码错误，请重试')
    }

    const now = new Date().toISOString()
    rec.user.LastLoginDate = now
    rec.user.LastActivityDate = now
    const token = `demo_tok_${rec.user.Id}_${Date.now().toString(36)}`
    demoTokenUserMap.set(token, rec.user.Id)
    pushActivity(
      `${rec.user.Name} 登录了 Ambra Serverless Demo`,
      'AuthenticationSucceeded',
      rec.user.Policy?.IsAdministrator ? '角色：管理员' : '角色：普通用户',
      rec.user.Id,
    )

    const result: AuthenticationResult = {
      User: { ...rec.user, Policy: { ...rec.user.Policy } },
      AccessToken: token,
      ServerId: DEMO_SERVER_ID,
      SessionInfo: {
        Id: `sess-${rec.user.Id}`,
        DeviceId: 'dev-demo-web-01',
      },
    }
    return result as T
  }

  if (pathname === '/emby/Sessions/Logout' && method === 'POST') {
    if (caller.token) {
      demoTokenUserMap.delete(caller.token)
      demoRevokedTokens.add(caller.token)
    }
    return undefined as T
  }

  // 忘记密码 & PIN 重置（支持 6 位十六进制 PIN /^[0-9A-Fa-f]{6}$/）
  if (pathname === '/emby/Users/ForgotPassword' && method === 'POST') {
    const body = parseJsonBody(options)
    const entered = String(body.EnteredUsername || '').trim()
    pushActivity(
      `收到账号「${entered || 'admin'}」的密码重置请求`,
      'ForgotPasswordRequested',
    )
    return {
      Action: 'PinCode',
      PinFile: '/demo-data/passwordreset-demo01.txt',
      PinExpirationDate: new Date(Date.now() + 30 * 60_000).toISOString(),
    } as T
  }

  if (pathname === '/emby/Users/ForgotPassword/Pin' && method === 'POST') {
    const body = parseJsonBody(options)
    const pin = String(body.Pin || '').trim()
    if (!/^[0-9A-Fa-f]{6}$/.test(pin)) {
      throw new ApiError(400, 'PIN 验证码无效，请输入 6 位十六进制字符（Demo 可使用 123456 或 A1B2C3）')
    }
    pushActivity('已通过 PIN 码完成密码重置（Demo 模式）', 'ForgotPasswordReset')
    return {
      Success: true,
      UsersReset: ['admin'],
    } as T
  }

  // ==================== 3. 用户与权限管理 ====================
  if (pathname === '/emby/Users' && method === 'GET') {
    requireDemoAdmin(caller)
    return store.users.map((u) => ({ ...u.user, Policy: { ...u.user.Policy } })) as T
  }

  if (pathname === '/emby/Users/New' && method === 'POST') {
    requireDemoAdmin(caller)
    const body = parseJsonBody(options)
    const name = String(body.Name || '').trim()
    if (!name) throw new ApiError(400, '用户名不能为空')
    if (store.users.some((u) => u.user.Name.toLowerCase() === name.toLowerCase())) {
      throw new ApiError(409, `用户名「${name}」已存在`)
    }
    const password = String(body.Password ?? '')
    const isAdmin = Boolean(body.IsAdministrator)
    const id = `user-demo-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 6)}`
    const newUser: UserDto = {
      Id: id,
      Name: name,
      ServerId: DEMO_SERVER_ID,
      HasPassword: password.length > 0,
      HasConfiguredPassword: password.length > 0,
      PrimaryImageTag: 'demo-user',
      LastActivityDate: new Date().toISOString(),
      Policy: {
        IsAdministrator: isAdmin,
        IsDisabled: false,
        EnableAllFolders: true,
        EnabledFolders: [],
        EnableMediaPlayback: true,
        EnableAudioPlaybackTranscoding: true,
        EnableVideoPlaybackTranscoding: true,
        EnablePlaybackRemuxing: true,
        EnableContentDeletion: isAdmin,
        EnableContentDownloading: isAdmin,
        EnableRemoteAccess: true,
      },
    }
    store.users.push({ user: newUser, password })
    store.userDataByUser.set(id, new Map())
    store.userAvatarById.set(id, makeAvatarDataUrl(name, ['#7c3aed', '#2e1065']))
    pushActivity(
      `管理员创建了新用户「${name}」`,
      'UserCreated',
      isAdmin ? '管理员权限' : '普通用户',
      caller.userId,
    )
    return { ...newUser } as T
  }

  const userConfigMatch = pathname.match(/^\/emby\/Users\/([^/]+)\/Configuration$/)
  if (userConfigMatch && method === 'POST') {
    const userId = decodeURIComponent(userConfigMatch[1])
    requireSelfOrAdmin(caller, userId)
    const rec = store.users.find((u) => u.user.Id === userId)
    if (!rec) throw new ApiError(404, '用户不存在')
    const patch = parseJsonBody(options) as Record<string, unknown>
    rec.user.Configuration = { ...rec.user.Configuration, ...patch }
    return undefined as T
  }

  const userPolicyMatch = pathname.match(/^\/emby\/Users\/([^/]+)\/Policy$/)
  if (userPolicyMatch && method === 'POST') {
    requireDemoAdmin(caller)
    const userId = decodeURIComponent(userPolicyMatch[1])
    const rec = store.users.find((u) => u.user.Id === userId)
    if (!rec) throw new ApiError(404, '用户不存在')
    const rawBody = parseJsonBody(options)
    const patch = rawBody as UserPolicy
    const nextPolicy: UserPolicy = { ...rec.user.Policy, ...patch }

    // 显式传 null/空串，或完整表单提交（含 EnableAllFolders）但省略了 MaxParentalRating 时清除分级限制；
    // 仅局部更新 BlockedTags / AllowedTags 等单字段时，绝不误删已有的 MaxParentalRating！
    if (
      ('MaxParentalRating' in rawBody &&
        (rawBody.MaxParentalRating === null ||
          rawBody.MaxParentalRating === undefined ||
          rawBody.MaxParentalRating === '')) ||
      (!('MaxParentalRating' in rawBody) && 'EnableAllFolders' in rawBody)
    ) {
      delete nextPolicy.MaxParentalRating
    }
    rec.user.Policy = nextPolicy
    pushActivity(`已更新用户「${rec.user.Name}」的权限策略`, 'UserPolicyUpdated', undefined, caller.userId)
    return undefined as T
  }

  const userPasswordMatch = pathname.match(/^\/emby\/Users\/([^/]+)\/Password$/)
  if (userPasswordMatch && method === 'POST') {
    const userId = decodeURIComponent(userPasswordMatch[1])
    requireSelfOrAdmin(caller, userId)
    const rec = store.users.find((u) => u.user.Id === userId)
    if (!rec) throw new ApiError(404, '用户不存在')
    const body = parseJsonBody(options)
    if (!caller.isAdmin && rec.password) {
      const currentPw = String(body.CurrentPw ?? '')
      if (currentPw !== rec.password) {
        throw new ApiError(401, '当前密码不正确')
      }
    }
    if (body.ResetPassword) {
      rec.password = ''
      rec.user.HasPassword = false
      rec.user.HasConfiguredPassword = false
    } else {
      const nextPw = String(body.NewPw ?? '')
      rec.password = nextPw
      rec.user.HasPassword = nextPw.length > 0
      rec.user.HasConfiguredPassword = nextPw.length > 0
    }
    pushActivity(`已更新用户「${rec.user.Name}」的登录密码`, 'UserPasswordChanged', undefined, caller.userId)
    return undefined as T
  }

  const userImageMatch = pathname.match(/^\/emby\/Users\/([^/]+)\/Images\/Primary(?:\/0)?$/)
  if (userImageMatch) {
    const userId = decodeURIComponent(userImageMatch[1])
    requireSelfOrAdmin(caller, userId)
    const rec = store.users.find((u) => u.user.Id === userId)
    if (!rec) throw new ApiError(404, '用户不存在')
    if (method === 'POST') {
      const headers = new Headers(options?.headers || {})
      const contentType = headers.get('Content-Type') || 'image/jpeg'
      const rawBody = typeof options?.body === 'string' ? options.body : ''
      const dataUrl = rawBody.startsWith('data:')
        ? rawBody
        : `data:${contentType};base64,${rawBody}`
      store.userAvatarById.set(userId, dataUrl)
      rec.user.PrimaryImageTag = `avatar-${Date.now().toString(36)}`
      pushActivity(`用户「${rec.user.Name}」更新了个人头像`, 'UserImageUpdated', undefined, caller.userId)
      return undefined as T
    }
    if (method === 'DELETE') {
      store.userAvatarById.delete(userId)
      delete rec.user.PrimaryImageTag
      pushActivity(`用户「${rec.user.Name}」移除了个人头像`, 'UserImageDeleted', undefined, caller.userId)
      return undefined as T
    }
  }

  // ==================== 4. 媒体视图 / 首页 / 列表 / 详情 ====================
  const userViewsMatch = pathname.match(/^\/emby\/Users\/([^/]+)\/Views$/)
  if (userViewsMatch && method === 'GET') {
    const userId = decodeURIComponent(userViewsMatch[1])
    requireSelfOrAdmin(caller, userId)
    const visible = getVisibleLibrariesForUser(userId).map(libraryToViewDto)
    const res: QueryResult<BaseItemDto> = {
      Items: visible,
      TotalRecordCount: visible.length,
    }
    return res as T
  }

  const userResumeMatch = pathname.match(/^\/emby\/Users\/([^/]+)\/Items\/Resume$/)
  if (userResumeMatch && method === 'GET') {
    const userId = decodeURIComponent(userResumeMatch[1])
    requireSelfOrAdmin(caller, userId)
    const targetUser = resolveUserDto(userId, caller)
    const limit = Number(qs.get('Limit') || 12)
    const resumable = Array.from(store.items.values())
      .filter((it) => it.Type === 'Movie' || it.Type === 'Episode')
      .filter((it) => canDemoUserAccessItem(targetUser, it))
      .map((it) => projectItemForUser(it, userId))
      .filter(
        (it) =>
          (it.UserData?.PlaybackPositionTicks || 0) > 0 &&
          !it.UserData?.Played,
      )
      .sort((a, b) => {
        const ta = a.UserData?.LastPlayedDate ? Date.parse(a.UserData.LastPlayedDate) : 0
        const tb = b.UserData?.LastPlayedDate ? Date.parse(b.UserData.LastPlayedDate) : 0
        return tb - ta
      })
      .slice(0, limit)
    return { Items: resumable, TotalRecordCount: resumable.length } as T
  }

  const userLatestMatch = pathname.match(/^\/emby\/Users\/([^/]+)\/Items\/Latest$/)
  if (userLatestMatch && method === 'GET') {
    const userId = decodeURIComponent(userLatestMatch[1])
    requireSelfOrAdmin(caller, userId)
    const targetUser = resolveUserDto(userId, caller)
    const parentId = qs.get('ParentId') || ''
    const limit = Number(qs.get('Limit') || 16)
    if (
      parentId &&
      store.libraries.some((l) => l.ItemId === parentId) &&
      !isLibraryAllowedForUser(targetUser, parentId)
    ) {
      return [] as unknown as T
    }
    let list: BaseItemDto[] = []
    if (parentId && store.libraryItems.has(parentId)) {
      list = (store.libraryItems.get(parentId) || [])
        .map((id) => store.items.get(id))
        .filter((it): it is BaseItemDto => Boolean(it))
    } else {
      list = Array.from(store.items.values()).filter(
        (it) => it.Type === 'Movie' || it.Type === 'Series',
      )
    }
    const filtered = list
      .filter((it) => canDemoUserAccessItem(targetUser, it))
      .map((it) => projectItemForUser(it, userId))
    return sortItems(filtered, 'DateCreated', 'Descending').slice(0, limit) as T
  }

  if (pathname === '/emby/Tags' && method === 'GET') {
    const qUserId = qs.get('UserId') || caller.userId
    if (qs.get('UserId')) requireSelfOrAdmin(caller, qUserId)
    const targetUser = resolveUserDto(qUserId, caller)
    const parentId = qs.get('ParentId') || ''
    if (
      parentId &&
      store.libraries.some((l) => l.ItemId === parentId) &&
      !isLibraryAllowedForUser(targetUser, parentId)
    ) {
      return { Items: [], TotalRecordCount: 0 } as T
    }
    const pool = (
      parentId && store.libraryItems.has(parentId)
        ? (store.libraryItems.get(parentId) || [])
            .map((id) => store.items.get(id))
            .filter((it): it is BaseItemDto => Boolean(it))
        : Array.from(store.items.values())
    ).filter((it) => canDemoUserAccessItem(targetUser, it))

    const tagSet = new Set<string>()
    for (const it of pool) {
      for (const t of it.Tags || []) {
        if (t) tagSet.add(t)
      }
    }
    const items = Array.from(tagSet)
      .sort((a, b) => a.localeCompare(b, 'zh-Hans-CN'))
      .map((name) => ({ Name: name, Id: `tag-${name}` }))
    return { Items: items, TotalRecordCount: items.length } as T
  }

  if (pathname === '/emby/Genres' && method === 'GET') {
    const qUserId = qs.get('UserId') || caller.userId
    if (qs.get('UserId')) requireSelfOrAdmin(caller, qUserId)
    const targetUser = resolveUserDto(qUserId, caller)
    const parentId = qs.get('ParentId') || ''
    if (
      parentId &&
      store.libraries.some((l) => l.ItemId === parentId) &&
      !isLibraryAllowedForUser(targetUser, parentId)
    ) {
      return { Items: [], TotalRecordCount: 0 } as T
    }
    const pool = (
      parentId && store.libraryItems.has(parentId)
        ? (store.libraryItems.get(parentId) || [])
            .map((id) => store.items.get(id))
            .filter((it): it is BaseItemDto => Boolean(it))
        : Array.from(store.items.values())
    ).filter((it) => canDemoUserAccessItem(targetUser, it))

    const genreSet = new Set<string>()
    for (const it of pool) {
      for (const g of it.Genres || []) {
        if (g) genreSet.add(g)
      }
    }
    const items = Array.from(genreSet)
      .sort((a, b) => a.localeCompare(b, 'zh-Hans-CN'))
      .map((name) => ({ Name: name, Id: `genre-${name}` }))
    return { Items: items, TotalRecordCount: items.length } as T
  }

  if (pathname === '/emby/Years' && method === 'GET') {
    const qUserId = qs.get('UserId') || caller.userId
    if (qs.get('UserId')) requireSelfOrAdmin(caller, qUserId)
    const targetUser = resolveUserDto(qUserId, caller)
    const parentId = qs.get('ParentId') || ''
    if (
      parentId &&
      store.libraries.some((l) => l.ItemId === parentId) &&
      !isLibraryAllowedForUser(targetUser, parentId)
    ) {
      return { Items: [], TotalRecordCount: 0 } as T
    }
    const pool = (
      parentId && store.libraryItems.has(parentId)
        ? (store.libraryItems.get(parentId) || [])
            .map((id) => store.items.get(id))
            .filter((it): it is BaseItemDto => Boolean(it))
        : Array.from(store.items.values())
    ).filter((it) => canDemoUserAccessItem(targetUser, it))

    const yearSet = new Set<number>()
    for (const it of pool) {
      if (it.ProductionYear) yearSet.add(it.ProductionYear)
    }
    const items = Array.from(yearSet)
      .sort((a, b) => b - a)
      .map((y) => ({ Name: String(y), Id: `year-${y}` }))
    return { Items: items, TotalRecordCount: items.length } as T
  }

  if (
    (pathname === '/emby/Items/Filters' || pathname === '/emby/Items/Filters2') &&
    method === 'GET'
  ) {
    const isV2 = pathname.endsWith('/Filters2')
    const qUserId = qs.get('UserId') || caller.userId
    if (qs.get('UserId')) requireSelfOrAdmin(caller, qUserId)
    const targetUser = resolveUserDto(qUserId, caller)
    const parentId = qs.get('ParentId') || ''
    const includeTypesRaw = qs.get('IncludeItemTypes') || ''
    const includeTypes = includeTypesRaw
      ? new Set(includeTypesRaw.split(',').map((s) => s.trim()))
      : null

    let pool: BaseItemDto[] =
      parentId &&
      store.libraries.some((l) => l.ItemId === parentId) &&
      !isLibraryAllowedForUser(targetUser, parentId)
        ? []
        : parentId && store.libraryItems.has(parentId)
          ? (store.libraryItems.get(parentId) || [])
              .map((id) => store.items.get(id))
              .filter((it): it is BaseItemDto => Boolean(it))
          : Array.from(store.items.values())

    pool = pool.filter((it) => canDemoUserAccessItem(targetUser, it))
    if (includeTypes) {
      pool = pool.filter((it) => includeTypes.has(it.Type))
    }

    const genreSet = new Set<string>()
    const tagSet = new Set<string>()
    const yearSet = new Set<number>()
    const studioSet = new Set<string>()
    const ratingSet = new Set<string>()

    for (const it of pool) {
      for (const g of it.Genres || []) {
        if (g) genreSet.add(g)
      }
      for (const t of it.Tags || []) {
        if (t) tagSet.add(t)
      }
      if (it.ProductionYear) yearSet.add(it.ProductionYear)
      for (const st of it.Studios || []) {
        if (st?.Name) studioSet.add(st.Name)
      }
      if (it.OfficialRating) ratingSet.add(it.OfficialRating)
    }

    const genres = Array.from(genreSet).sort((a, b) => a.localeCompare(b, 'zh-Hans-CN'))
    const tags = Array.from(tagSet).sort((a, b) => a.localeCompare(b, 'zh-Hans-CN'))
    const years = Array.from(yearSet).sort((a, b) => b - a)
    const studios = Array.from(studioSet).sort((a, b) => a.localeCompare(b, 'zh-Hans-CN'))
    const officialRatings = Array.from(ratingSet).sort((a, b) => a.localeCompare(b, 'en'))

    if (isV2) {
      return {
        Genres: genres.map((name) => ({ Name: name, Id: `genre-${name}` })),
        Tags: tags,
        Years: years,
        Studios: studios.map((name) => ({ Name: name, Id: `studio-${name}` })),
        OfficialRatings: officialRatings,
      } as T
    }

    return {
      Genres: genres,
      Tags: tags,
      Years: years,
      Studios: studios,
      OfficialRatings: officialRatings,
    } as T
  }

  const userItemsMatch = pathname.match(/^\/emby\/Users\/([^/]+)\/Items$/)
  const isGlobalItemsQuery = pathname === '/emby/Items'
  if ((userItemsMatch || isGlobalItemsQuery) && method === 'GET') {
    const targetUserId = userItemsMatch
      ? decodeURIComponent(userItemsMatch[1])
      : qs.get('UserId') || caller.userId
    if (userItemsMatch || qs.get('UserId')) {
      requireSelfOrAdmin(caller, targetUserId)
    }
    const targetUser = resolveUserDto(targetUserId, caller)
    const parentId = qs.get('ParentId') || ''
    const includeTypesRaw = qs.get('IncludeItemTypes') || ''
    const includeTypes = includeTypesRaw
      ? new Set(includeTypesRaw.split(',').map((s) => s.trim()))
      : null
    const filters = (qs.get('Filters') || '').split(',').filter(Boolean)
    const genresParam = (qs.get('Genres') || '')
      .split(/[|,]/)
      .map((s) => s.trim().toLowerCase())
      .filter(Boolean)
    const yearsParam = (qs.get('Years') || '')
      .split(/[|,]/)
      .map((s) => s.trim())
      .filter(Boolean)
    const tagsParam = (qs.get('Tags') || '')
      .split(/[|,]/)
      .map((s) => s.trim().toLowerCase())
      .filter(Boolean)
    const studiosParam = (qs.get('Studios') || '')
      .split(/[|,]/)
      .map((s) => s.trim().toLowerCase())
      .filter(Boolean)
    const personIdsParam = (qs.get('PersonIds') || '')
      .split(/[|,]/)
      .map((s) => s.trim().toLowerCase())
      .filter(Boolean)
    const personParam = (qs.get('Person') || '').trim().toLowerCase()
    const searchTerm = qs.get('SearchTerm') || ''
    const nameStartsWith = (qs.get('NameStartsWith') || '').trim().toLowerCase()
    const nameStartsWithOrGreater = (qs.get('NameStartsWithOrGreater') || '').trim().toLowerCase()
    const nameLessThan = (qs.get('NameLessThan') || '').trim().toLowerCase()
    const sortBy = qs.get('SortBy') || 'DateCreated'
    const sortOrder = qs.get('SortOrder') || 'Descending'
    const startIndex = Number(qs.get('StartIndex') || 0)
    const limit = Number(qs.get('Limit') || 50)

    if (
      parentId &&
      store.libraries.some((l) => l.ItemId === parentId) &&
      !isLibraryAllowedForUser(targetUser, parentId)
    ) {
      return { Items: [], TotalRecordCount: 0 } as T
    }

    let pool: BaseItemDto[]
    if (parentId && store.libraryItems.has(parentId)) {
      const directItems = (store.libraryItems.get(parentId) || [])
        .map((id) => store.items.get(id))
        .filter((it): it is BaseItemDto => Boolean(it))
      const isMusicLibrary =
        parentId === 'lib-music' ||
        store.libraries.find((l) => l.ItemId === parentId)?.CollectionType?.toLowerCase() === 'music'

      if (isMusicLibrary && (includeTypes?.has('Audio') || includeTypes?.has('MusicArtist'))) {
        const seenIds = new Set<string>()
        const expanded: BaseItemDto[] = []
        const pushUnique = (item: BaseItemDto) => {
          if (!seenIds.has(item.Id)) {
            seenIds.add(item.Id)
            expanded.push(item)
          }
        }
        for (const it of directItems) {
          pushUnique(it)
        }
        if (includeTypes.has('Audio')) {
          for (const album of directItems) {
            if (album.Type === 'MusicAlbum' || store.libraryItems.has(album.Id)) {
              const trackIds = store.libraryItems.get(album.Id) || []
              for (const tid of trackIds) {
                const track = store.items.get(tid)
                if (track && track.Type === 'Audio') {
                  pushUnique(track)
                }
              }
              for (const candidate of store.items.values()) {
                if (
                  candidate.Type === 'Audio' &&
                  (candidate.ParentId === album.Id || candidate.AlbumId === album.Id)
                ) {
                  pushUnique(candidate)
                }
              }
            }
          }
        }
        if (includeTypes.has('MusicArtist')) {
          for (const candidate of store.items.values()) {
            if (
              candidate.Type === 'MusicArtist' &&
              (!candidate.ParentId || candidate.ParentId === parentId)
            ) {
              pushUnique(candidate)
            }
          }
        }
        pool = expanded
      } else {
        pool = directItems
      }
    } else if (parentId) {
      pool = []
    } else {
      pool = Array.from(store.items.values())
    }

    pool = pool
      .filter((it) => canDemoUserAccessItem(targetUser, it))
      .map((it) => projectItemForUser(it, targetUserId))

    if (includeTypes) {
      pool = pool.filter((it) => includeTypes.has(it.Type))
    }
    if (filters.includes('IsFavorite')) {
      pool = pool.filter((it) => Boolean(it.UserData?.IsFavorite))
    }
    if (filters.includes('IsPlayed')) {
      pool = pool.filter((it) => Boolean(it.UserData?.Played))
    }
    if (filters.includes('IsUnplayed')) {
      pool = pool.filter((it) => !it.UserData?.Played)
    }
    if (genresParam.length > 0) {
      pool = pool.filter((it) =>
        (it.Genres || []).some((g) => genresParam.includes(g.toLowerCase())),
      )
    }
    if (yearsParam.length > 0) {
      pool = pool.filter(
        (it) => it.ProductionYear != null && yearsParam.includes(String(it.ProductionYear)),
      )
    }
    if (tagsParam.length > 0) {
      pool = pool.filter((it) =>
        (it.Tags || []).some((t) => tagsParam.includes(t.toLowerCase())),
      )
    }
    if (studiosParam.length > 0) {
      pool = pool.filter((it) =>
        (it.Studios || []).some(
          (st) =>
            studiosParam.includes(st.Name.toLowerCase()) ||
            studiosParam.includes(st.Id.toLowerCase()),
        ),
      )
    }
    if (personIdsParam.length > 0) {
      pool = pool.filter((it) =>
        (it.People || []).some((p) => personIdsParam.includes(p.Id.toLowerCase())),
      )
    }
    if (personParam) {
      pool = pool.filter((it) =>
        (it.People || []).some((p) => p.Name.toLowerCase().includes(personParam)),
      )
    }
    if (searchTerm) {
      pool = pool.filter((it) => matchesFuzzySearch(it, searchTerm))
    }
    if (nameStartsWith) {
      pool = pool.filter((it) => {
        const sortKey = getDemoSortKey(it.SortName || it.Name)
        const nameKey = getDemoSortKey(it.Name)
        const rawSort = (it.SortName || it.Name).trim().toLowerCase()
        const rawName = it.Name.trim().toLowerCase()
        return (
          sortKey.startsWith(nameStartsWith) ||
          nameKey.startsWith(nameStartsWith) ||
          rawSort.startsWith(nameStartsWith) ||
          rawName.startsWith(nameStartsWith)
        )
      })
    }
    if (nameStartsWithOrGreater) {
      pool = pool.filter((it) => {
        const sortKey = getDemoSortKey(it.SortName || it.Name)
        return sortKey >= nameStartsWithOrGreater
      })
    }
    if (nameLessThan) {
      pool = pool.filter((it) => {
        const sortKey = getDemoSortKey(it.SortName || it.Name)
        return sortKey.length > 0 && sortKey < nameLessThan
      })
    }

    const sorted = sortItems(pool, sortBy, sortOrder)
    const paged = sorted.slice(startIndex, startIndex + limit)
    const res: QueryResult<BaseItemDto> = {
      Items: paged,
      TotalRecordCount: sorted.length,
    }
    return res as T
  }

  const userSingleItemMatch = pathname.match(/^\/emby\/Users\/([^/]+)\/Items\/([^/]+)$/)
  if (userSingleItemMatch && method === 'GET') {
    const userId = decodeURIComponent(userSingleItemMatch[1])
    const itemId = decodeURIComponent(userSingleItemMatch[2])
    requireSelfOrAdmin(caller, userId)
    const targetUser = resolveUserDto(userId, caller)

    const item = store.items.get(itemId)
    if (item) {
      if (!canDemoUserAccessItem(targetUser, item)) {
        throw new ApiError(403, '受家长控制或媒体库访问权限限制，无权查看该条目')
      }
      return projectItemForUser(item, userId) as T
    }
    const lib = store.libraries.find((l) => l.ItemId === itemId)
    if (lib) {
      if (!isLibraryAllowedForUser(targetUser, lib.ItemId)) {
        throw new ApiError(403, '无权访问该媒体库')
      }
      return libraryToViewDto(lib) as T
    }

    // 若 ID 命中任意可访问作品的演职员，则合成 Person DTO 返回
    let matchedPerson: { Id: string; Name: string; Type: string; PrimaryImageTag?: string } | null =
      null
    const knownTitles: string[] = []
    for (const it of store.items.values()) {
      if (!canDemoUserAccessItem(targetUser, it)) continue
      for (const p of it.People || []) {
        if (p.Id === itemId || p.Name === itemId) {
          if (!matchedPerson) {
            matchedPerson = p
          }
          if (!knownTitles.includes(it.Name)) {
            knownTitles.push(it.Name)
          }
        }
      }
    }
    if (matchedPerson) {
      const personDto: BaseItemDto = {
        Id: matchedPerson.Id,
        Name: matchedPerson.Name,
        ServerId: DEMO_SERVER_ID,
        Type: 'Person',
        Overview:
          knownTitles.length > 0
            ? `${matchedPerson.Name}，当前媒体库中收录其参与的作品共 ${knownTitles.length} 部，包括《${knownTitles.slice(0, 4).join('》《')}》。`
            : `${matchedPerson.Name} 的演职员资料。`,
        ImageTags: matchedPerson.PrimaryImageTag
          ? { Primary: matchedPerson.PrimaryImageTag }
          : undefined,
      }
      return personDto as T
    }

    throw new ApiError(404, `未找到项目 ${itemId}`)
  }

  // ==================== 5. 剧集季、分集与 NextUp ====================
  if (pathname === '/emby/Shows/NextUp' && method === 'GET') {
    const qUserId = qs.get('UserId') || caller.userId
    if (qs.get('UserId')) requireSelfOrAdmin(caller, qUserId)
    const targetUser = resolveUserDto(qUserId, caller)
    const seriesId = qs.get('SeriesId') || ''
    const limit = Number(qs.get('Limit') || 12)
    const targetSeriesIds = seriesId ? [seriesId] : Array.from(store.seasonsBySeries.keys())
    const nextUpList: BaseItemDto[] = []
    for (const sid of targetSeriesIds) {
      const series = store.items.get(sid)
      if (series && !canDemoUserAccessItem(targetUser, series)) continue
      const seasons = store.seasonsBySeries.get(sid) || []
      const allEps = seasons
        .flatMap((s) => store.episodesBySeason.get(s.Id) || [])
        .filter((ep) => canDemoUserAccessItem(targetUser, ep))
        .map((ep) => projectItemForUser(ep, qUserId))
      const nextEp =
        allEps.find((ep) => !ep.UserData?.Played && (ep.UserData?.PlaybackPositionTicks || 0) > 0) ||
        allEps.find((ep) => !ep.UserData?.Played) ||
        allEps[0]
      if (nextEp) nextUpList.push(nextEp)
    }
    const sliced = nextUpList.slice(0, limit)
    return { Items: sliced, TotalRecordCount: nextUpList.length } as T
  }

  const similarMatch = pathname.match(/^\/emby\/Items\/([^/]+)\/Similar$/)
  if (similarMatch && method === 'GET') {
    const itemId = decodeURIComponent(similarMatch[1])
    const qUserId = qs.get('UserId') || caller.userId
    if (qs.get('UserId')) requireSelfOrAdmin(caller, qUserId)
    const targetUser = resolveUserDto(qUserId, caller)
    const limit = Number(qs.get('Limit') || 12)
    const source = store.items.get(itemId)
    if (!source || !canDemoUserAccessItem(targetUser, source)) {
      return { Items: [], TotalRecordCount: 0 } as T
    }
    const srcGenres = new Set((source.Genres || []).map((g) => g.toLowerCase()))
    const candidates = Array.from(store.items.values())
      .filter((it) => it.Id !== itemId && it.Type === source.Type)
      .filter((it) => canDemoUserAccessItem(targetUser, it))
      .map((it) => {
        const overlap = (it.Genres || []).filter((g) => srcGenres.has(g.toLowerCase())).length
        return {
          item: projectItemForUser(it, qUserId),
          score: overlap * 10 + (it.CommunityRating || 0),
        }
      })
      .sort((a, b) => b.score - a.score)
      .slice(0, limit)
      .map((x) => x.item)
    return { Items: candidates, TotalRecordCount: candidates.length } as T
  }

  // ==================== 5b. 合集 (Collections) & 播放列表 (Playlists) CRUD ====================
  if (pathname === '/emby/Collections' && method === 'GET') {
    const qUserId = qs.get('UserId') || caller.userId
    if (qs.get('UserId')) requireSelfOrAdmin(caller, qUserId)
    const targetUser = resolveUserDto(qUserId, caller)
    const startIndex = Number(qs.get('StartIndex') || 0)
    const limit = Number(qs.get('Limit') || 50)

    const collections = Array.from(store.items.values())
      .filter((it) => it.Type === 'BoxSet')
      .filter((it) => canDemoUserAccessItem(targetUser, it))
      .map((col) => {
        const childIds = store.libraryItems.get(col.Id) || []
        const visibleCount = childIds.filter((cid) => {
          const child = store.items.get(cid)
          return child ? canDemoUserAccessItem(targetUser, child) : false
        }).length
        return {
          ...projectItemForUser(col, qUserId),
          ChildCount: visibleCount,
        }
      })

    const paged = collections.slice(startIndex, startIndex + limit)
    return { Items: paged, TotalRecordCount: collections.length } as T
  }

  if (pathname === '/emby/Collections' && method === 'POST') {
    requireDemoAdmin(caller)
    const body = parseJsonBody(options)
    const name = String(qs.get('Name') || body.Name || '').trim()
    if (!name) throw new ApiError(400, '合集名称不能为空')
    const idsRaw = qs.get('Ids') || (Array.isArray(body.Ids) ? body.Ids.join(',') : '')
    const ids = idsRaw
      .split(',')
      .map((s) => s.trim())
      .filter(Boolean)
    const id = `col-demo-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 6)}`
    const dto: BaseItemDto = {
      Id: id,
      Name: name,
      SortName: name.toLowerCase(),
      ServerId: DEMO_SERVER_ID,
      Type: 'BoxSet',
      IsFolder: true,
      ChildCount: ids.length,
      DateCreated: new Date().toISOString(),
    }
    store.items.set(id, dto)
    store.libraryItems.set(id, Array.from(new Set(ids)))
    pushActivity(`已创建合集「${name}」`, 'CollectionCreated', undefined, caller.userId)
    return { Id: id, Name: name } as T
  }

  if (pathname === '/emby/Playlists' && method === 'POST') {
    const body = parseJsonBody(options)
    const requestedUserId = String(qs.get('UserId') || body.UserId || '').trim()
    if (requestedUserId) requireSelfOrAdmin(caller, requestedUserId)
    const ownerUserId = requestedUserId || caller.userId
    const name = String(qs.get('Name') || body.Name || '').trim()
    if (!name) throw new ApiError(400, '播放列表名称不能为空')
    const idsRaw = qs.get('Ids') || (Array.isArray(body.Ids) ? body.Ids.join(',') : '')
    const ids = idsRaw
      .split(',')
      .map((s) => s.trim())
      .filter(Boolean)
    const id = `pl-demo-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 6)}`
    const dto: BaseItemDto = {
      Id: id,
      Name: name,
      SortName: name.toLowerCase(),
      ServerId: DEMO_SERVER_ID,
      Type: 'Playlist',
      MediaType: String(body.MediaType || 'Video'),
      IsFolder: true,
      ChildCount: ids.length,
      DateCreated: new Date().toISOString(),
    }
    store.items.set(id, dto)
    store.libraryItems.set(id, Array.from(new Set(ids)))
    demoPlaylistOwners.set(id, ownerUserId)
    pushActivity(`已创建播放列表「${name}」`, 'PlaylistCreated', undefined, caller.userId)
    return { Id: id, Name: name } as T
  }

  const movePlaylistItemMatch = pathname.match(
    /^\/emby\/Playlists\/([^/]+)\/Items\/([^/]+)\/Move\/(-?\d+)$/,
  )
  if (movePlaylistItemMatch && method === 'POST') {
    const playlistId = decodeURIComponent(movePlaylistItemMatch[1])
    const itemId = decodeURIComponent(movePlaylistItemMatch[2])
    const newIndexRaw = Number(movePlaylistItemMatch[3])
    const container = store.items.get(playlistId)
    if (!container || container.Type !== 'Playlist') {
      throw new ApiError(404, '播放列表不存在')
    }
    const ownerUserId = demoPlaylistOwners.get(playlistId)
    if (ownerUserId) requireSelfOrAdmin(caller, ownerUserId)
    const list = [...(store.libraryItems.get(playlistId) || [])]
    const fromIdx = list.indexOf(itemId)
    if (fromIdx < 0) {
      throw new ApiError(404, '播放列表中不存在该项目')
    }
    const clampedIndex = Math.max(0, Math.min(list.length - 1, newIndexRaw))
    list.splice(fromIdx, 1)
    list.splice(clampedIndex, 0, itemId)
    store.libraryItems.set(playlistId, list)
    return undefined as T
  }

  const containerItemsMatch = pathname.match(/^\/emby\/(Collections|Playlists)\/([^/]+)\/Items$/)
  if (containerItemsMatch) {
    const containerKind = containerItemsMatch[1]
    const containerId = decodeURIComponent(containerItemsMatch[2])
    const container = store.items.get(containerId)
    const expectedType = containerKind === 'Collections' ? 'BoxSet' : 'Playlist'
    if (!container || container.Type !== expectedType) {
      throw new ApiError(404, '合集或播放列表不存在')
    }
    if (method === 'GET') {
      const qUserId = qs.get('UserId') || caller.userId
      if (qs.get('UserId')) requireSelfOrAdmin(caller, qUserId)
      const targetUser = resolveUserDto(qUserId, caller)
      const ids = store.libraryItems.get(containerId) || []
      const items = ids
        .map((mid) => store.items.get(mid))
        .filter((it): it is BaseItemDto => Boolean(it))
        .filter((it) => canDemoUserAccessItem(targetUser, it))
        .map((it) => projectItemForUser(it, qUserId))
      return { Items: items, TotalRecordCount: items.length } as T
    }
    if (method === 'POST') {
      if (containerKind === 'Collections') {
        requireDemoAdmin(caller)
      } else {
        const ownerUserId = demoPlaylistOwners.get(containerId)
        if (ownerUserId) requireSelfOrAdmin(caller, ownerUserId)
      }
      const body = parseJsonBody(options)
      const idsRaw = qs.get('Ids') || (Array.isArray(body.Ids) ? body.Ids.join(',') : '')
      const toAdd = idsRaw
        .split(',')
        .map((s) => s.trim())
        .filter(Boolean)
      const existing = store.libraryItems.get(containerId) || []
      const merged = Array.from(new Set([...existing, ...toAdd]))
      store.libraryItems.set(containerId, merged)
      container.ChildCount = merged.length
      return undefined as T
    }
    if (method === 'DELETE') {
      if (containerKind === 'Collections') {
        requireDemoAdmin(caller)
      } else {
        const ownerUserId = demoPlaylistOwners.get(containerId)
        if (ownerUserId) requireSelfOrAdmin(caller, ownerUserId)
      }
      const idsRaw = qs.get('EntryIds') || qs.get('Ids') || ''
      const toRemove = new Set(
        idsRaw
          .split(',')
          .map((s) => s.trim())
          .filter(Boolean),
      )
      const existing = store.libraryItems.get(containerId) || []
      const next = existing.filter((mid) => !toRemove.has(mid))
      store.libraryItems.set(containerId, next)
      container.ChildCount = next.length
      return undefined as T
    }
  }

  const deleteContainerMatch = pathname.match(/^\/emby\/(Collections|Playlists)\/([^/]+)$/)
  if (deleteContainerMatch && method === 'DELETE') {
    const containerKind = deleteContainerMatch[1]
    if (containerKind === 'Collections') {
      requireDemoAdmin(caller)
    }
    const containerId = decodeURIComponent(deleteContainerMatch[2])
    const container = store.items.get(containerId)
    const expectedType = containerKind === 'Collections' ? 'BoxSet' : 'Playlist'
    if (!container || container.Type !== expectedType) {
      throw new ApiError(404, '合集或播放列表不存在')
    }
    if (containerKind === 'Playlists') {
      const ownerUserId = demoPlaylistOwners.get(containerId)
      if (ownerUserId) requireSelfOrAdmin(caller, ownerUserId)
    }
    store.items.delete(containerId)
    store.libraryItems.delete(containerId)
    demoPlaylistOwners.delete(containerId)
    pushActivity(`已删除「${container.Name}」`, 'ContainerDeleted', undefined, caller.userId)
    return undefined as T
  }

  const seasonsMatch = pathname.match(/^\/emby\/Shows\/([^/]+)\/Seasons$/)
  if (seasonsMatch && method === 'GET') {
    const seriesId = decodeURIComponent(seasonsMatch[1])
    const qUserId = qs.get('UserId') || caller.userId
    if (qs.get('UserId')) requireSelfOrAdmin(caller, qUserId)
    const targetUser = resolveUserDto(qUserId, caller)
    const series = store.items.get(seriesId)
    if (series && !canDemoUserAccessItem(targetUser, series)) {
      return { Items: [], TotalRecordCount: 0 } as T
    }
    const seasons = (store.seasonsBySeries.get(seriesId) || [])
      .filter((s) => canDemoUserAccessItem(targetUser, s))
      .map((s) => projectItemForUser(s, qUserId))
    return { Items: seasons, TotalRecordCount: seasons.length } as T
  }

  const episodesMatch = pathname.match(/^\/emby\/Shows\/([^/]+)\/Episodes$/)
  if (episodesMatch && method === 'GET') {
    const seriesId = decodeURIComponent(episodesMatch[1])
    const qUserId = qs.get('UserId') || caller.userId
    if (qs.get('UserId')) requireSelfOrAdmin(caller, qUserId)
    const targetUser = resolveUserDto(qUserId, caller)
    const series = store.items.get(seriesId)
    if (series && !canDemoUserAccessItem(targetUser, series)) {
      return { Items: [], TotalRecordCount: 0 } as T
    }
    const seasonId = qs.get('SeasonId') || ''
    let eps: BaseItemDto[] = []
    if (seasonId && store.episodesBySeason.has(seasonId)) {
      eps = store.episodesBySeason.get(seasonId) || []
    } else {
      const seasons = store.seasonsBySeries.get(seriesId) || []
      eps = seasons.flatMap((s) => store.episodesBySeason.get(s.Id) || [])
    }
    const filtered = eps
      .filter((ep) => canDemoUserAccessItem(targetUser, ep))
      .map((ep) => projectItemForUser(ep, qUserId))
    return { Items: filtered, TotalRecordCount: filtered.length } as T
  }

  // ==================== 6. 中文乱序模糊搜索 ====================
  if (pathname === '/emby/Search/Hints' && method === 'GET') {
    const qUserId = qs.get('UserId') || caller.userId
    if (qs.get('UserId')) requireSelfOrAdmin(caller, qUserId)
    const targetUser = resolveUserDto(qUserId, caller)
    const term = qs.get('SearchTerm') || ''
    const limit = Number(qs.get('Limit') || 20)
    const matched = Array.from(store.items.values())
      .filter(
        (it) =>
          it.Type === 'Movie' ||
          it.Type === 'Series' ||
          it.Type === 'Episode' ||
          it.Type === 'MusicAlbum' ||
          it.Type === 'Audio',
      )
      .filter((it) => canDemoUserAccessItem(targetUser, it))
      .filter((it) => matchesFuzzySearch(it, term))
      .slice(0, limit)

    const hints: SearchHint[] = matched.map((it) => ({
      ItemId: it.Id,
      Id: it.Id,
      Name: it.Name,
      MatchedTerm: term,
      ProductionYear: it.ProductionYear,
      PrimaryImageTag: it.ImageTags?.Primary,
      Type: it.Type,
      MediaType: it.MediaType,
      RunTimeTicks: it.RunTimeTicks,
      Series: it.SeriesName,
      SeriesId: it.SeriesId,
      IndexNumber: it.IndexNumber,
      ParentIndexNumber: it.ParentIndexNumber,
      BackdropImageTag: it.BackdropImageTags?.[0],
      BackdropImageItemId: it.Id,
    }))
    const res: SearchHintResult = {
      SearchHints: hints,
      TotalRecordCount: hints.length,
    }
    return res as T
  }

  // ==================== 7. 按用户隔离的收藏 / 赞踩 / 已看 / 继续观看移除 ====================
  const hideResumeMatch = pathname.match(
    /^\/emby\/Users\/([^/]+)\/Items\/([^/]+)\/HideFromResume$/,
  )
  if (hideResumeMatch && method === 'POST') {
    const userId = decodeURIComponent(hideResumeMatch[1])
    const itemId = decodeURIComponent(hideResumeMatch[2])
    requireSelfOrAdmin(caller, userId)
    const item = store.items.get(itemId)
    if (!item) throw new ApiError(404, '项目不存在')
    const ud = ensureUserDataForUser(userId, itemId)
    const hide = qs.get('Hide') !== 'false'
    if (hide) {
      ud.PlaybackPositionTicks = 0
      ud.PlayedPercentage = 0
    }
    return { ...ud } as T
  }

  const userItemRatingMatch = pathname.match(/^\/emby\/Users\/([^/]+)\/Items\/([^/]+)\/Rating$/)
  if (userItemRatingMatch && (method === 'POST' || method === 'DELETE')) {
    const userId = decodeURIComponent(userItemRatingMatch[1])
    const itemId = decodeURIComponent(userItemRatingMatch[2])
    requireSelfOrAdmin(caller, userId)
    const item = store.items.get(itemId)
    if (!item) throw new ApiError(404, '项目不存在')
    const ud = ensureUserDataForUser(userId, itemId)
    if (method === 'DELETE') {
      delete ud.Likes
    } else {
      const likesParam = qs.get('Likes')
      ud.Likes = likesParam === null ? true : likesParam.toLowerCase() === 'true'
    }
    return { ...ud } as T
  }

  const favMatch = pathname.match(/^\/emby\/Users\/([^/]+)\/FavoriteItems\/([^/]+)$/)
  if (favMatch && (method === 'POST' || method === 'DELETE')) {
    const userId = decodeURIComponent(favMatch[1])
    const itemId = decodeURIComponent(favMatch[2])
    requireSelfOrAdmin(caller, userId)
    const item = store.items.get(itemId)
    if (!item) throw new ApiError(404, '项目不存在')
    const ud = ensureUserDataForUser(userId, itemId)
    ud.IsFavorite = method === 'POST'
    return { ...ud } as T
  }

  const playedMatch = pathname.match(/^\/emby\/Users\/([^/]+)\/PlayedItems\/([^/]+)$/)
  if (playedMatch && (method === 'POST' || method === 'DELETE')) {
    const userId = decodeURIComponent(playedMatch[1])
    const itemId = decodeURIComponent(playedMatch[2])
    requireSelfOrAdmin(caller, userId)
    const item = store.items.get(itemId)
    if (!item) throw new ApiError(404, '项目不存在')
    const ud = ensureUserDataForUser(userId, itemId)
    ud.Played = method === 'POST'
    if (ud.Played) {
      ud.PlayCount = Math.max(1, ud.PlayCount + 1)
      ud.PlaybackPositionTicks = 0
      ud.PlayedPercentage = 100
      ud.LastPlayedDate = new Date().toISOString()
    } else {
      ud.PlayedPercentage = 0
    }
    if (item.Type === 'Series') {
      const seasons = store.seasonsBySeries.get(itemId) || []
      let totalEps = 0
      for (const season of seasons) {
        const sUd = ensureUserDataForUser(userId, season.Id)
        sUd.Played = ud.Played
        sUd.PlayedPercentage = ud.Played ? 100 : 0
        if (ud.Played) sUd.PlaybackPositionTicks = 0
        const eps = store.episodesBySeason.get(season.Id) || []
        totalEps += eps.length
        sUd.UnplayedItemCount = ud.Played ? 0 : eps.length
        for (const ep of eps) {
          const epUd = ensureUserDataForUser(userId, ep.Id)
          epUd.Played = ud.Played
          epUd.PlayedPercentage = ud.Played ? 100 : 0
          if (ud.Played) {
            epUd.PlayCount = Math.max(1, epUd.PlayCount + 1)
            epUd.PlaybackPositionTicks = 0
            epUd.LastPlayedDate = ud.LastPlayedDate
          }
        }
      }
      ud.UnplayedItemCount = ud.Played ? 0 : totalEps
    } else if (item.Type === 'Season') {
      const eps = store.episodesBySeason.get(itemId) || []
      for (const ep of eps) {
        const epUd = ensureUserDataForUser(userId, ep.Id)
        epUd.Played = ud.Played
        epUd.PlayedPercentage = ud.Played ? 100 : 0
        if (ud.Played) {
          epUd.PlayCount = Math.max(1, epUd.PlayCount + 1)
          epUd.PlaybackPositionTicks = 0
          epUd.LastPlayedDate = ud.LastPlayedDate
        }
      }
      ud.UnplayedItemCount = ud.Played ? 0 : eps.length
    }
    return { ...ud } as T
  }

  const localTrailersMatch = pathname.match(/^\/emby\/Users\/([^/]+)\/Items\/([^/]+)\/LocalTrailers$/)
  if (localTrailersMatch && method === 'GET') {
    const userId = decodeURIComponent(localTrailersMatch[1])
    const itemId = decodeURIComponent(localTrailersMatch[2])
    requireSelfOrAdmin(caller, userId)
    const trailers = store.localTrailersByItemId.get(itemId) || []
    return trailers.map((t) => projectItemForUser(t, userId)) as T
  }

  const specialFeaturesMatch = pathname.match(
    /^\/emby\/Users\/([^/]+)\/Items\/([^/]+)\/SpecialFeatures$/,
  )
  if (specialFeaturesMatch && method === 'GET') {
    const userId = decodeURIComponent(specialFeaturesMatch[1])
    const itemId = decodeURIComponent(specialFeaturesMatch[2])
    requireSelfOrAdmin(caller, userId)
    const extras = store.specialFeaturesByItemId.get(itemId) || []
    return extras.map((e) => projectItemForUser(e, userId)) as T
  }

  // ==================== 8. PlaybackInfo、音频歌词 & 播放状态上报（按用户隔离） ====================
  const audioLyricsMatch = pathname.match(/^\/emby\/Audio\/([^/]+)\/Lyrics$/)
  if (audioLyricsMatch && method === 'GET') {
    const itemId = decodeURIComponent(audioLyricsMatch[1])
    const lyrics = store.lyricsByItemId.get(itemId)
    if (lyrics) {
      return lyrics as T
    }
    return { Metadata: { IsSynced: false }, Lyrics: [] } as T
  }

  const playbackMatch = pathname.match(/^\/emby\/Items\/([^/]+)\/PlaybackInfo$/)
  if (playbackMatch && method === 'POST') {
    const itemId = decodeURIComponent(playbackMatch[1])
    const body = parseJsonBody(options)
    const qUserId = String(qs.get('UserId') || body.UserId || caller.userId || '')
    if (qUserId) requireSelfOrAdmin(caller, qUserId)
    const targetUser = resolveUserDto(qUserId, caller)
    if (targetUser?.Policy?.EnableMediaPlayback === false) {
      throw new ApiError(403, '当前账号已被禁止播放媒体')
    }
    const item = store.items.get(itemId)
    if (!item) throw new ApiError(404, '找不到可播放的媒体')
    if (!canDemoUserAccessItem(targetUser, item)) {
      throw new ApiError(403, '受家长控制或媒体库访问权限限制，无权播放该媒体')
    }
    const sources = item.MediaSources || []
    const res: PlaybackInfoResponse = {
      MediaSources: sources,
      PlaySessionId: `demo-play-${Date.now().toString(36)}`,
    }
    return res as T
  }

  const videoAttachmentsMatch = pathname.match(/^\/emby\/Videos\/([^/]+)\/([^/]+)\/Attachments$/)
  if (videoAttachmentsMatch && method === 'GET') {
    const itemId = decodeURIComponent(videoAttachmentsMatch[1])
    if (!store.items.has(itemId)) {
      throw new ApiError(404, '项目不存在')
    }
    return [] as T
  }

  const videoBifMatch = pathname.match(/^\/emby\/Videos\/([^/]+)\/index\.bif$/)
  if (videoBifMatch && method === 'GET') {
    const itemId = decodeURIComponent(videoBifMatch[1])
    if (!store.items.has(itemId)) {
      throw new ApiError(404, 'BIF 预览图不存在')
    }
    return makeDemoBifBuffer() as T
  }

  const thumbnailSetMatch = pathname.match(/^\/emby\/Items\/([^/]+)\/ThumbnailSet$/)
  if (thumbnailSetMatch && method === 'GET') {
    const itemId = decodeURIComponent(thumbnailSetMatch[1])
    if (!store.items.has(itemId)) {
      throw new ApiError(404, '缩略图集不存在')
    }
    return {
      AspectRatio: 1.777,
      Thumbnails: [0, 5, 10, 15, 20].map((sec, idx) => ({
        PositionTicks: sec * 10_000_000,
        ImageTag: `demo-bif-${idx}`,
      })),
    } as T
  }

  if (
    (pathname === '/emby/Sessions/Playing' ||
      pathname === '/emby/Sessions/Playing/Progress' ||
      pathname === '/emby/Sessions/Playing/Stopped') &&
    method === 'POST'
  ) {
    const body = parseJsonBody(options)
    const itemId = String(body.ItemId || '')
    const posTicks = Number(body.PositionTicks || 0)
    const sessionUserId = caller.userId || 'user-demo-admin'
    const item = store.items.get(itemId)
    if (item) {
      const ud = ensureUserDataForUser(sessionUserId, itemId)
      ud.PlaybackPositionTicks = posTicks
      ud.LastPlayedDate = new Date().toISOString()
      if (item.RunTimeTicks && item.RunTimeTicks > 0) {
        const ratio = posTicks / item.RunTimeTicks
        ud.PlayedPercentage = Math.min(100, Math.round(ratio * 100))
        const threshold = demoServerConfig.PlayedThreshold || 0.9
        if (ratio >= threshold) {
          ud.Played = true
          ud.PlayCount = Math.max(1, (ud.PlayCount || 0) + 1)
          if (pathname.endsWith('/Stopped')) {
            ud.PlaybackPositionTicks = 0
            ud.PlayedPercentage = 100
          }
        }
      }

      // 按当前请求用户隔离会话状态，绝不覆盖其他用户的播放会话
      let userSess = store.sessions.find((s) => s.UserId === sessionUserId)
      if (!userSess) {
        userSess = {
          Id: `sess-web-${sessionUserId}`,
          ServerId: DEMO_SERVER_ID,
          UserId: sessionUserId,
          UserName: caller.user?.Name || sessionUserId,
          Client: 'Ambra Web',
          ApplicationVersion: '0.12.3',
          DeviceName: 'Web Browser (Demo)',
          DeviceId: `dev-web-${sessionUserId}`,
          LastActivityDate: new Date().toISOString(),
          SupportsRemoteControl: true,
        }
        store.sessions.push(userSess)
      }
      userSess.LastActivityDate = new Date().toISOString()
      if (pathname.endsWith('/Stopped')) {
        userSess.NowPlayingItem = undefined
        userSess.PlayState = undefined
      } else {
        userSess.NowPlayingItem = projectItemForUser(item, sessionUserId)
        userSess.PlayState = {
          PositionTicks: posTicks,
          IsPaused: Boolean(body.IsPaused),
          IsMuted: Boolean(body.IsMuted),
          VolumeLevel: typeof body.VolumeLevel === 'number' ? body.VolumeLevel : 100,
          PlayMethod: String(body.PlayMethod || 'DirectPlay'),
          CanSeek: true,
        }
      }
    }
    return undefined as T
  }

  // ==================== 9. 扫描控制、健康治理与一键迁移 (/api/admin/**) ====================
  if (pathname.startsWith('/api/admin/')) {
    requireDemoAdmin(caller)
  }

  if (pathname === '/api/admin/migration/status' && method === 'GET') {
    const rep = demoMigrationState.report
    if (rep.status === 'running') {
      const nextProcessed = Math.min(rep.total_users, rep.processed_users + 1)
      const done = nextProcessed >= rep.total_users
      rep.processed_users = nextProcessed
      rep.current_user = done ? '' : DEMO_MIGRATION_SOURCE_USERS[nextProcessed]?.Name || ''
      rep.users_matched = Math.min(1, nextProcessed)
      rep.users_created = demoMigrationState.migrateUsers ? Math.max(0, nextProcessed - 1) : 0
      rep.items_scanned = nextProcessed * 142
      rep.items_matched = nextProcessed * 134
      rep.items_skipped = nextProcessed * 8
      rep.played_migrated = demoMigrationState.migratePlayed ? nextProcessed * 96 : 0
      rep.favorites_migrated = demoMigrationState.migrateFavorites ? nextProcessed * 24 : 0
      rep.resume_migrated = demoMigrationState.migrateResume ? nextProcessed * 14 : 0
      if (done) {
        rep.status = 'completed'
        rep.completed_at = new Date().toISOString()
        rep.conflicts = [
          {
            user_name: 'Admin',
            item_name: '沙丘 (2021)',
            item_type: 'Movie',
            reason: '本地存在多版本重复条目，已安全跳过',
          },
        ]
        if (rep.source_url) {
          demoMigrationState.checkpointByUrl.delete(rep.source_url)
        }
        pushActivity('已完成旧 Emby 数据迁移任务', 'MigrationCompleted', undefined, caller.userId)
      }
    }
    return {
      ...rep,
      conflicts: [...rep.conflicts],
      errors: [...rep.errors],
    } as T
  }

  if (pathname === '/api/admin/migration/test' && method === 'POST') {
    const body = parseJsonBody(options)
    const sourceUrl = validateHttpUrl(String(body.source_url || ''), '旧 Emby 服务器地址')
    const apiKey = String(body.api_key || '').trim()
    if (!apiKey) {
      throw new ApiError(400, '旧 Emby 管理员 API Key 不能为空')
    }
    return {
      server: {
        Id: 'demo-legacy-emby-01',
        ServerName: '旧 Emby NAS 影院 (Demo)',
        Version: '4.8.10.0',
        ProductName: 'Emby Server',
        OperatingSystem: 'Linux',
        LocalAddress: sourceUrl,
        Status: 'ok',
      },
      users: DEMO_MIGRATION_SOURCE_USERS.map((u) => ({ ...u, Policy: { ...u.Policy } })),
      virtual_folders: [
        { ItemId: 'src-lib-1', Name: '4K 电影', CollectionType: 'movies', Locations: ['/volume1/media/movies'] },
        { ItemId: 'src-lib-2', Name: '精选剧集', CollectionType: 'tvshows', Locations: ['/volume1/media/tv'] },
        { ItemId: 'src-lib-3', Name: '动画番剧', CollectionType: 'tvshows', Locations: ['/volume1/media/anime'] },
      ],
    } as T
  }

  if (pathname === '/api/admin/migration/dry-run' && method === 'POST') {
    const body = parseJsonBody(options)
    const sourceUrl = validateHttpUrl(String(body.source_url || ''), '旧 Emby 服务器地址')
    const apiKey = String(body.api_key || '').trim()
    if (!apiKey) {
      throw new ApiError(400, '旧 Emby 管理员 API Key 不能为空')
    }
    const userIds = Array.isArray(body.user_ids) ? body.user_ids.map((x) => String(x)) : []
    const userCount = Math.max(1, userIds.length || DEMO_MIGRATION_SOURCE_USERS.length)
    const migUsers = body.migrate_users !== false
    const migPlayed = body.migrate_played !== false
    const migFavs = body.migrate_favorites !== false
    const migResume = body.migrate_resume !== false
    const hasCp = !body.reset_checkpoint && demoMigrationState.checkpointByUrl.has(sourceUrl)
    const nowIso = new Date().toISOString()

    const dryReport: DemoMigrationReport = {
      status: 'completed',
      dry_run: true,
      source_url: sourceUrl,
      source_server_name: '旧 Emby NAS 影院 (Demo)',
      started_at: nowIso,
      completed_at: nowIso,
      total_users: userCount,
      processed_users: userCount,
      users_matched: 1,
      users_created: migUsers ? Math.max(0, userCount - 1) : 0,
      items_scanned: userCount * 142,
      items_matched: userCount * 134,
      items_skipped: userCount * 8,
      played_migrated: migPlayed ? userCount * 96 : 0,
      favorites_migrated: migFavs ? userCount * 24 : 0,
      resume_migrated: migResume ? userCount * 14 : 0,
      conflicts: [
        {
          user_name: 'Admin',
          item_name: '沙丘 (2021)',
          item_type: 'Movie',
          reason: '本地存在多版本重复条目，已安全跳过',
        },
      ],
      errors: [],
      resumed_from_checkpoint: hasCp,
      checkpoint_key: hasCp
        ? demoMigrationState.checkpointByUrl.get(sourceUrl)?.checkpointKey
        : undefined,
    }
    return dryReport as T
  }

  if (pathname === '/api/admin/migration/start' && method === 'POST') {
    const body = parseJsonBody(options)
    const sourceUrl = validateHttpUrl(String(body.source_url || ''), '旧 Emby 服务器地址')
    const apiKey = String(body.api_key || '').trim()
    if (!apiKey) {
      throw new ApiError(400, '旧 Emby 管理员 API Key 不能为空')
    }
    const userIds = Array.isArray(body.user_ids) ? body.user_ids.map((x) => String(x)) : []
    const totalUsers = Math.max(1, userIds.length || DEMO_MIGRATION_SOURCE_USERS.length)
    const resetCp = Boolean(body.reset_checkpoint)
    if (resetCp) {
      demoMigrationState.checkpointByUrl.delete(sourceUrl)
    }
    const savedCp = !resetCp ? demoMigrationState.checkpointByUrl.get(sourceUrl) : undefined
    const initialProcessed = savedCp ? Math.min(totalUsers - 1, savedCp.processedUsers) : 0

    demoMigrationState.migrateUsers = body.migrate_users !== false
    demoMigrationState.migratePlayed = body.migrate_played !== false
    demoMigrationState.migrateFavorites = body.migrate_favorites !== false
    demoMigrationState.migrateResume = body.migrate_resume !== false

    const startedReport: DemoMigrationReport = {
      status: 'running',
      dry_run: false,
      source_url: sourceUrl,
      source_server_name: '旧 Emby NAS 影院 (Demo)',
      started_at: new Date().toISOString(),
      total_users: totalUsers,
      processed_users: initialProcessed,
      current_user: DEMO_MIGRATION_SOURCE_USERS[initialProcessed]?.Name || 'Admin',
      users_matched: initialProcessed > 0 ? 1 : 0,
      users_created: demoMigrationState.migrateUsers ? Math.max(0, initialProcessed - 1) : 0,
      items_scanned: initialProcessed * 142,
      items_matched: initialProcessed * 134,
      items_skipped: initialProcessed * 8,
      played_migrated: demoMigrationState.migratePlayed ? initialProcessed * 96 : 0,
      favorites_migrated: demoMigrationState.migrateFavorites ? initialProcessed * 24 : 0,
      resume_migrated: demoMigrationState.migrateResume ? initialProcessed * 14 : 0,
      conflicts: [],
      errors: [],
      resumed_from_checkpoint: Boolean(savedCp),
      checkpoint_key: savedCp?.checkpointKey,
    }
    demoMigrationState.report = startedReport
    pushActivity('已启动旧 Emby 数据迁移任务', 'MigrationStarted', sourceUrl, caller.userId)
    return { ...startedReport, conflicts: [], errors: [] } as T
  }

  if (pathname === '/api/admin/migration/cancel' && method === 'POST') {
    const rep = demoMigrationState.report
    if (rep.status === 'running') {
      const processed = Math.max(1, rep.processed_users)
      const cpKey = `cp-${Date.now().toString(36)}`
      rep.status = 'cancelled'
      rep.processed_users = processed
      rep.completed_at = new Date().toISOString()
      rep.checkpoint_key = cpKey
      if (rep.source_url) {
        demoMigrationState.checkpointByUrl.set(rep.source_url, {
          checkpointKey: cpKey,
          processedUsers: processed,
        })
      }
      pushActivity('已取消迁移任务并持久化断点', 'MigrationCancelled', rep.source_url, caller.userId)
      return {
        cancelled: true,
        report: { ...rep, conflicts: [...rep.conflicts], errors: [...rep.errors] },
      } as T
    }
    return {
      cancelled: false,
      report: { ...rep, conflicts: [...rep.conflicts], errors: [...rep.errors] },
    } as T
  }

  if (pathname === '/api/admin/scanner/progress' && method === 'GET') {
    const nowMs = Date.now()
    const nowIso = new Date(nowMs).toISOString()

    if (!activeDemoScan) {
      const idle: ScanProgressSnapshot = {
        running: false,
        phase: 'idle',
        total: 0,
        processed: 0,
        added: 0,
        updated: 0,
        skipped: 0,
        failed: 0,
        percentage: 0,
        updated_at: nowIso,
        libraries: [],
      }
      return idle as T
    }

    const elapsed = Math.max(0, nowMs - activeDemoScan.startedAtMs)
    if (elapsed >= activeDemoScan.durationMs) {
      activeDemoScan = null
      const done: ScanProgressSnapshot = {
        running: false,
        phase: 'idle',
        total: 0,
        processed: 0,
        added: 0,
        updated: 0,
        skipped: 0,
        failed: 0,
        percentage: 0,
        updated_at: nowIso,
        libraries: [],
      }
      return done as T
    }

    const total = Math.max(1, activeDemoScan.items.length)
    const discoverWindow = activeDemoScan.durationMs * 0.2
    const isDiscovering = elapsed < discoverWindow
    const phase = isDiscovering ? 'discovering' : 'enriching'
    const currentAction = isDiscovering
      ? '扫描媒体目录与文件'
      : '刮削 TMDB 中文元数据与海报'

    let processed: number
    let percentage: number
    if (isDiscovering) {
      const ratio = elapsed / discoverWindow
      processed = Math.min(total, Math.max(1, Math.ceil(ratio * Math.max(1, Math.floor(total * 0.25)))))
      percentage = Math.min(19, Math.max(5, Math.round((elapsed / activeDemoScan.durationMs) * 100)))
    } else {
      const enrichRatio = (elapsed - discoverWindow) / (activeDemoScan.durationMs - discoverWindow)
      processed = Math.min(total, Math.max(1, Math.ceil(enrichRatio * total)))
      percentage = Math.min(99, Math.max(20, Math.round((elapsed / activeDemoScan.durationMs) * 100)))
    }

    const currentItem =
      activeDemoScan.items[Math.min(activeDemoScan.items.length - 1, Math.max(0, processed - 1))] ||
      '媒体索引项'
    const updated = Math.floor(processed * 0.7)
    const added = Math.floor(processed * 0.2)
    const skipped = Math.max(0, processed - updated - added)
    const startedIso = new Date(activeDemoScan.startedAtMs).toISOString()

    const libProgress: LibraryScanProgress = {
      library_id: activeDemoScan.libraryId || 'all',
      library_name: activeDemoScan.libraryName,
      phase,
      total,
      processed,
      added,
      updated,
      skipped,
      failed: 0,
      current_item: currentItem,
      current_action: currentAction,
      percentage,
      started_at: startedIso,
      updated_at: nowIso,
    }

    const snapshot: ScanProgressSnapshot = {
      running: true,
      paused: Boolean(activeDemoScan.paused),
      phase,
      library_id: activeDemoScan.libraryId || undefined,
      library_name: activeDemoScan.libraryName,
      total,
      processed,
      added,
      updated,
      skipped,
      failed: 0,
      current_item: currentItem,
      current_action: activeDemoScan.paused ? '扫描已暂停' : currentAction,
      percentage,
      started_at: startedIso,
      updated_at: nowIso,
      libraries: [libProgress],
    }
    return snapshot as T
  }

  if (pathname === '/api/admin/scanner/pause' && method === 'POST') {
    if (activeDemoScan && !activeDemoScan.paused) {
      activeDemoScan.paused = true
      activeDemoScan.pausedAtMs = Date.now()
      pushActivity('已暂停媒体库扫描任务', 'LibraryScanPaused', undefined, caller.userId)
    }
    return undefined as T
  }

  if (pathname === '/api/admin/scanner/resume' && method === 'POST') {
    if (activeDemoScan && activeDemoScan.paused) {
      const pausedDelta = activeDemoScan.pausedAtMs ? Date.now() - activeDemoScan.pausedAtMs : 0
      activeDemoScan.startedAtMs += pausedDelta
      activeDemoScan.paused = false
      activeDemoScan.pausedAtMs = undefined
      pushActivity('已恢复媒体库扫描任务', 'LibraryScanResumed', undefined, caller.userId)
    }
    return undefined as T
  }

  if (pathname === '/api/admin/scanner/cancel' && method === 'POST') {
    if (activeDemoScan) {
      activeDemoScan = null
      pushActivity('已取消当前媒体库扫描任务', 'LibraryScanCancelled', undefined, caller.userId)
    }
    return undefined as T
  }

  // ==================== 9b. 系统健康诊断与媒体库治理 (Governance) ====================
  if (pathname === '/api/admin/health' && method === 'GET') {
    const allItems = Array.from(store.items.values())
    const movieCount = allItems.filter((i) => i.Type === 'Movie').length
    const seriesCount = allItems.filter((i) => i.Type === 'Series').length
    const episodeCount = allItems.filter((i) => i.Type === 'Episode').length
    const pendingAudits = demoReviews.filter((r) => r.Status === 'pending').length
    const totalDisk = 2 * 1024 * 1024 * 1024 * 1024 // 2 TB
    const freeDisk = Math.floor(totalDisk * 0.62)
    return {
      CollectedAt: Math.floor(Date.now() / 1000),
      Status: 'healthy',
      GoVersion: 'go1.24.0 (Demo)',
      NumCpu: 8,
      NumGoroutine: 42,
      Memory: {
        AllocBytes: 96 * 1024 * 1024,
        TotalAllocBytes: 512 * 1024 * 1024,
        SysBytes: 184 * 1024 * 1024,
        HeapInuseBytes: 112 * 1024 * 1024,
        NumGC: 28,
      },
      Database: {
        DbPath: '/data/ambra.db',
        DbBytes: 24 * 1024 * 1024,
        WalBytes: 4 * 1024 * 1024,
        ShmBytes: 32 * 1024,
        TotalBytes: 28 * 1024 * 1024 + 32 * 1024,
      },
      Cache: {
        ImagesBytes: 186 * 1024 * 1024,
        ChaptersBytes: 24 * 1024 * 1024,
        BifBytes: 42 * 1024 * 1024,
        SubtitlesBytes: 12 * 1024 * 1024,
        TranscodeBytes: 0,
        TotalBytes: 264 * 1024 * 1024,
      },
      Disk: {
        Path: '/data',
        TotalBytes: totalDisk,
        FreeBytes: freeDisk,
        AvailableBytes: freeDisk,
        UsedPercent: 38.0,
      },
      Library: {
        TotalItems: allItems.length,
        MovieCount: movieCount,
        SeriesCount: seriesCount,
        EpisodeCount: episodeCount,
        UserCount: store.users.length,
        PendingAudits: pendingAudits,
      },
      Scheduler: {
        TotalTasks: store.scheduledTasks.length,
        ActiveTasks: activeDemoScan ? 1 : 0,
        BudgetInUse: activeDemoScan ? 2 : 0,
        BudgetCapacity: 8,
        BudgetUsagePct: activeDemoScan ? 25 : 0,
      },
    } as T
  }

  if (pathname === '/api/admin/governance/storage' && method === 'GET') {
    const librariesBreakdown = store.libraries.map((lib) => {
      const ids = store.libraryItems.get(lib.ItemId) || []
      let totalSize = 0
      for (const id of ids) {
        const it = store.items.get(id)
        for (const src of it?.MediaSources || []) {
          totalSize += src.Size || 0
        }
      }
      return {
        LibraryId: lib.ItemId,
        LibraryName: lib.Name,
        CollectionType: lib.CollectionType || 'mixed',
        ItemCount: ids.length,
        TotalBytes: totalSize,
      }
    })
    const totalMediaBytes = librariesBreakdown.reduce((acc, l) => acc + l.TotalBytes, 0)
    return {
      GeneratedAt: Math.floor(Date.now() / 1000),
      TotalMediaBytes: totalMediaBytes,
      CacheBytes: 264 * 1024 * 1024,
      DatabaseBytes: 28 * 1024 * 1024,
      Libraries: librariesBreakdown,
    } as T
  }

  if (pathname === '/api/admin/governance/duplicates' && method === 'GET') {
    const byKey = new Map<string, BaseItemDto[]>()
    for (const it of store.items.values()) {
      if (it.Type !== 'Movie' && it.Type !== 'Series') continue
      const key = it.ProviderIds?.Tmdb
        ? `${it.Type}:tmdb:${it.ProviderIds.Tmdb}`
        : `${it.Type}:name:${it.Name.toLowerCase()}`
      const list = byKey.get(key) || []
      list.push(it)
      byKey.set(key, list)
    }
    const groups = Array.from(byKey.entries())
      .filter(([, list]) => list.length > 1)
      .map(([key, list]) => ({
        GroupKey: key,
        Name: list[0].Name,
        Type: list[0].Type,
        Items: list.map((it) => ({
          Id: it.Id,
          Name: it.Name,
          Path: it.Path || '',
          Year: it.ProductionYear,
          MediaSourceCount: it.MediaSources?.length || 0,
        })),
      }))
    return {
      Groups: groups,
      TotalGroups: groups.length,
    } as T
  }

  if (pathname === '/api/admin/governance/duplicates/merge' && method === 'POST') {
    const body = parseJsonBody(options)
    const rawIds = Array.isArray(body.ItemIds)
      ? body.ItemIds
      : Array.isArray(body.item_ids)
        ? body.item_ids
        : Array.isArray(body.Ids)
          ? body.Ids
          : []
    const ids = rawIds.map((x) => String(x).trim()).filter(Boolean)
    if (ids.length < 2) {
      throw new ApiError(400, '至少需要选择 2 个重复条目进行合并')
    }
    const primaryId = String(body.PrimaryItemId || body.primary_item_id || ids[0]).trim()
    const primary = store.items.get(primaryId)
    if (!primary) throw new ApiError(404, '主条目不存在')
    const mergedSources = [...(primary.MediaSources || [])]
    const removedIds: string[] = []
    for (const otherId of ids) {
      if (otherId === primaryId) continue
      const other = store.items.get(otherId)
      if (!other) continue
      if (other.MediaSources) mergedSources.push(...other.MediaSources)
      store.items.delete(otherId)
      removedIds.push(otherId)
      for (const [libId, libIds] of store.libraryItems.entries()) {
        store.libraryItems.set(
          libId,
          libIds.filter((id) => id !== otherId),
        )
      }
    }
    primary.MediaSources = mergedSources
    pushActivity(
      `已合并 ${removedIds.length + 1} 个重复条目至「${primary.Name}」`,
      'DuplicatesMerged',
      undefined,
      caller.userId,
    )
    return {
      PrimaryItemId: primaryId,
      MergedCount: removedIds.length + 1,
      RemovedIds: removedIds,
    } as T
  }

  if (pathname === '/api/admin/governance/audit' && method === 'GET') {
    const allItems = Array.from(store.items.values()).filter(
      (it) => it.Type === 'Movie' || it.Type === 'Series',
    )
    const missingPosters: DemoOrphanItem[] = []
    const missingTmdb: DemoOrphanItem[] = []
    for (const it of allItems) {
      if (!it.ImageTags?.Primary) {
        missingPosters.push({
          Id: it.Id,
          Name: it.Name,
          Type: it.Type,
          LibraryId: it.ParentId || '',
          Path: it.Path || '',
          Year: it.ProductionYear,
          IsStrm: Boolean(it.Path?.toLowerCase().endsWith('.strm')),
        })
      }
      if (!it.ProviderIds?.Tmdb) {
        missingTmdb.push({
          Id: it.Id,
          Name: it.Name,
          Type: it.Type,
          LibraryId: it.ParentId || '',
          Path: it.Path || '',
          Year: it.ProductionYear,
          IsStrm: Boolean(it.Path?.toLowerCase().endsWith('.strm')),
        })
      }
    }
    return {
      ScannedAt: Math.floor(Date.now() / 1000),
      TotalChecked: allItems.length,
      MissingPosters: missingPosters,
      MissingTmdb: missingTmdb,
      OrphanItems: [...demoOrphans],
    } as T
  }

  if (pathname === '/api/admin/governance/orphans/cleanup' && method === 'POST') {
    const body = parseJsonBody(options)
    const rawIds = Array.isArray(body.ItemIds)
      ? body.ItemIds
      : Array.isArray(body.item_ids)
        ? body.item_ids
        : []
    const ids = rawIds.map((x) => String(x).trim()).filter(Boolean)
    if (ids.length === 0) {
      throw new ApiError(400, 'ItemIds must not be empty')
    }
    const idSet = new Set(ids)
    const deletedIds: string[] = []
    demoOrphans = demoOrphans.filter((o) => {
      if (idSet.has(o.Id)) {
        deletedIds.push(o.Id)
        return false
      }
      return true
    })
    for (const id of ids) {
      if (store.items.has(id) && !deletedIds.includes(id)) {
        store.items.delete(id)
        deletedIds.push(id)
      }
    }
    pushActivity(`已清理 ${deletedIds.length} 条磁盘孤立记录`, 'OrphansCleaned', undefined, caller.userId)
    return {
      Requested: ids.length,
      Deleted: deletedIds.length,
      DeletedIds: deletedIds,
    } as T
  }

  if (pathname === '/api/admin/governance/review' && method === 'GET') {
    const status = (qs.get('status') || 'pending').toLowerCase()
    const filtered =
      status === 'all'
        ? demoReviews
        : demoReviews.filter((r) => r.Status.toLowerCase() === status)
    return {
      Items: filtered.map((r) => ({ ...r, Candidates: [...r.Candidates] })),
      TotalRecordCount: filtered.length,
    } as T
  }

  const govReviewApproveMatch = pathname.match(
    /^\/api\/admin\/governance\/review\/([^/]+)\/approve$/,
  )
  if (govReviewApproveMatch && method === 'POST') {
    const reviewId = decodeURIComponent(govReviewApproveMatch[1])
    const audit = demoReviews.find((r) => r.Id === reviewId)
    if (!audit) throw new ApiError(404, '审校记录不存在')
    const body = parseJsonBody(options)
    const cand = body.Candidate as DemoScrapeCandidate | undefined
    const lockFields = Array.isArray(body.LockFields)
      ? body.LockFields.map((f) => String(f)).filter(Boolean)
      : []
    const targetItem = store.items.get(audit.ItemId)
    if (targetItem && cand) {
      if (cand.Title) targetItem.Name = cand.Title
      if (cand.OriginalTitle) targetItem.OriginalTitle = cand.OriginalTitle
      if (cand.Year) targetItem.ProductionYear = cand.Year
      if (cand.Overview) targetItem.Overview = cand.Overview
      if (cand.ProviderId) {
        targetItem.ProviderIds = { ...(targetItem.ProviderIds || {}), Tmdb: cand.ProviderId }
      }
      if (lockFields.length > 0) {
        targetItem.LockedFields = Array.from(
          new Set([...(targetItem.LockedFields || []), ...lockFields]),
        )
      }
    }
    audit.Status = 'approved'
    audit.UpdatedAt = Math.floor(Date.now() / 1000)
    pushActivity(`已通过审校项「${audit.ItemName}」`, 'ScrapeReviewApproved', undefined, caller.userId)
    return { ...audit } as T
  }

  const govReviewDismissMatch = pathname.match(
    /^\/api\/admin\/governance\/review\/([^/]+)\/dismiss$/,
  )
  if (govReviewDismissMatch && method === 'POST') {
    const reviewId = decodeURIComponent(govReviewDismissMatch[1])
    const audit = demoReviews.find((r) => r.Id === reviewId)
    if (!audit) throw new ApiError(404, '审校记录不存在')
    audit.Status = 'dismissed'
    audit.UpdatedAt = Math.floor(Date.now() / 1000)
    pushActivity(`已忽略审校项「${audit.ItemName}」`, 'ScrapeReviewDismissed', undefined, caller.userId)
    return { ...audit } as T
  }

  const govLockMatch = pathname.match(/^\/api\/admin\/governance\/items\/([^/]+)\/locks?$/)
  if (govLockMatch && method === 'GET') {
    const itemId = decodeURIComponent(govLockMatch[1])
    const item = store.items.get(itemId)
    if (!item) throw new ApiError(404, '项目不存在')
    const fields = [...(item.LockedFields || [])]
    const lockAll = Boolean(item.LockData)
    return {
      ItemId: itemId,
      LockedFields: fields,
      LockData: lockAll,
      item_id: itemId,
      locked_fields: fields,
      lock_data: lockAll,
    } as T
  }

  if (govLockMatch && method === 'POST') {
    const itemId = decodeURIComponent(govLockMatch[1])
    const item = store.items.get(itemId)
    if (!item) throw new ApiError(404, '项目不存在')
    const body = parseJsonBody(options)
    const rawFields = Array.isArray(body.LockedFields)
      ? body.LockedFields
      : Array.isArray(body.fields)
        ? body.fields
        : []
    const fields = rawFields.map((f) => String(f)).filter(Boolean)
    const lockAll = Boolean(body.LockData ?? body.lock_data)
    item.LockedFields = fields
    item.LockData = lockAll
    pushActivity(`已更新「${item.Name}」的元数据锁定配置`, 'ItemFieldsLocked', undefined, caller.userId)
    return {
      ItemId: itemId,
      LockedFields: fields,
      LockData: lockAll,
      item_id: itemId,
      locked_fields: fields,
      lock_data: lockAll,
    } as T
  }

  const govNfoMatch = pathname.match(/^\/api\/admin\/governance\/items\/([^/]+)\/nfo$/)
  if (govNfoMatch && method === 'POST') {
    const itemId = decodeURIComponent(govNfoMatch[1])
    const item = store.items.get(itemId)
    if (!item) throw new ApiError(404, '项目不存在')
    const rootTag =
      item.Type === 'Series' ? 'tvshow' : item.Type === 'Episode' ? 'episodedetails' : 'movie'
    const nfoPath = item.Path
      ? item.Path.replace(/\.[^/.]+$/, '.nfo')
      : `/media/${item.Name}/${rootTag}.nfo`
    pushActivity(`已原子回写「${item.Name}」的 NFO 文件`, 'ItemNfoWritten', nfoPath, caller.userId)
    return {
      ItemId: itemId,
      NfoPath: nfoPath,
      RootTag: rootTag,
      BytesSize: 1280,
      item_id: itemId,
      path: nfoPath,
      written: true,
    } as T
  }

  if (pathname === '/emby/Library/Refresh' && method === 'POST') {
    requireDemoAdmin(caller)
    const parentId = qs.get('ParentId')
    const lib = parentId ? store.libraries.find((l) => l.ItemId === parentId) : null
    const targetItems = lib
      ? (store.libraryItems.get(lib.ItemId) || [])
          .map((id) => store.items.get(id)?.Name)
          .filter((name): name is string => Boolean(name))
      : Array.from(store.items.values())
          .filter((it) => it.Type === 'Movie' || it.Type === 'Series')
          .map((it) => it.Name)

    activeDemoScan = {
      startedAtMs: Date.now(),
      durationMs: 4000,
      libraryId: lib?.ItemId || '',
      libraryName: lib?.Name || '全部媒体库',
      items: targetItems.length > 0 ? targetItems : ['流浪地球 2', '三体', '沙丘 2', '奥本海默'],
    }

    pushActivity(
      lib ? `已触发媒体库「${lib.Name}」扫描任务` : '已触发全部媒体库扫描任务',
      'LibraryScan',
      `正在扫描与刮削 ${activeDemoScan.items.length} 个媒体条目`,
      caller.userId,
    )
    return undefined as T
  }

  if (pathname === '/emby/Library/VirtualFolders/Name' && method === 'POST') {
    requireDemoAdmin(caller)
    const body = parseJsonBody(options)
    const oldName = String(qs.get('Name') || qs.get('name') || body.Name || '').trim()
    const newName = String(qs.get('NewName') || qs.get('newName') || body.NewName || '').trim()
    const id = String(body.Id || qs.get('Id') || '').trim()
    if (!newName) throw new ApiError(400, '新名称不能为空')
    const lib = store.libraries.find(
      (l) => (id && l.ItemId === id) || (oldName && l.Name === oldName),
    )
    if (!lib) throw new ApiError(404, '媒体库不存在')
    const prevName = lib.Name
    lib.Name = newName
    pushActivity(`已将媒体库「${prevName}」重命名为「${newName}」`, 'LibraryRenamed', undefined, caller.userId)
    return undefined as T
  }

  if (pathname === '/emby/Library/VirtualFolders/Paths') {
    requireDemoAdmin(caller)
    if (method === 'POST') {
      const body = parseJsonBody(options)
      const name = String(body.Name || qs.get('Name') || qs.get('name') || '').trim()
      const id = String(body.Id || qs.get('Id') || '').trim()
      const targetPath = String(
        body.Path || (body.PathInfo as { Path?: string } | undefined)?.Path || '',
      ).trim()
      if (!targetPath) throw new ApiError(400, '路径不能为空')
      const lib = store.libraries.find((l) => (id && l.ItemId === id) || (name && l.Name === name))
      if (!lib) throw new ApiError(404, '媒体库不存在')
      const locations = lib.Locations || []
      if (!locations.includes(targetPath)) {
        lib.Locations = [...locations, targetPath]
      }
      pushActivity(`为媒体库「${lib.Name}」添加路径`, 'LibraryPathAdded', targetPath, caller.userId)
      return undefined as T
    }
    if (method === 'DELETE') {
      const name = String(qs.get('Name') || qs.get('name') || '').trim()
      const id = String(qs.get('Id') || qs.get('id') || '').trim()
      const targetPath = String(qs.get('Path') || qs.get('path') || '').trim()
      const lib = store.libraries.find((l) => (id && l.ItemId === id) || (name && l.Name === name))
      if (!lib) throw new ApiError(404, '媒体库不存在')
      lib.Locations = (lib.Locations || []).filter((p) => p !== targetPath)
      pushActivity(`从媒体库「${lib.Name}」移除路径`, 'LibraryPathRemoved', targetPath, caller.userId)
      return undefined as T
    }
  }

  if (pathname === '/emby/Library/VirtualFolders/LibraryOptions' && method === 'POST') {
    requireDemoAdmin(caller)
    const body = parseJsonBody(options)
    const id = String(body.Id || qs.get('id') || '').trim()
    const lib = store.libraries.find((l) => l.ItemId === id)
    if (!lib) throw new ApiError(404, '未找到对应媒体库')
    const incoming = (body.LibraryOptions || {}) as Record<string, unknown>
    lib.LibraryOptions = {
      SaveLocalMetadata: true,
      ...(lib.LibraryOptions || {}),
      ...incoming,
    }
    pushActivity(
      `更新媒体库「${lib.Name}」元数据选项`,
      'LibraryOptionsUpdated',
      `保存本地 NFO/图片：${lib.LibraryOptions.SaveLocalMetadata === false ? '关闭' : '开启'}`,
      caller.userId,
    )
    return undefined as T
  }

  if (pathname === '/emby/Library/VirtualFolders') {
    if (method === 'GET') {
      const visibleLibs = caller.isAdmin
        ? store.libraries
        : getVisibleLibrariesForUser(caller.userId)
      return visibleLibs.map((l) => ({
        ...l,
        Locations: caller.isAdmin ? [...(l.Locations || [])] : [],
        LibraryOptions: {
          SaveLocalMetadata: true,
          ...(l.LibraryOptions || {}),
        },
      })) as T
    }
    requireDemoAdmin(caller)
    if (method === 'POST') {
      const body = parseJsonBody(options)
      const name = String(body.Name || qs.get('name') || '').trim()
      const collectionType = String(body.CollectionType || qs.get('collectionType') || 'movies')
      const paths = Array.isArray(body.Paths) ? body.Paths.map((p) => String(p)) : []
      if (!name) throw new ApiError(400, '媒体库名称不能为空')
      const incomingOpts = (body.LibraryOptions || {}) as Record<string, unknown>
      const { PathInfos: _ignoredPathInfos, ...persistedOpts } = incomingOpts
      void _ignoredPathInfos
      const newLib: VirtualFolderInfo = {
        ItemId: `lib-demo-${Date.now().toString(36)}`,
        Name: name,
        CollectionType: collectionType,
        Locations: paths,
        LibraryOptions: {
          ...persistedOpts,
          SaveLocalMetadata:
            typeof incomingOpts.SaveLocalMetadata === 'boolean'
              ? incomingOpts.SaveLocalMetadata
              : true,
        },
      }
      store.libraries.push(newLib)
      store.libraryItems.set(newLib.ItemId, [])
      const thumb = makeLibraryThumbDataUrl(name, collectionType.toUpperCase(), [
        '#1e293b',
        '#334155',
        '#f59e0b',
      ])
      store.artById.set(newLib.ItemId, { poster: thumb, backdrop: thumb })
      pushActivity(
        `新建媒体库「${name}」`,
        'LibraryCreated',
        `挂载目录：${paths.join(', ')}`,
        caller.userId,
      )
      return newLib as T
    }
    if (method === 'DELETE') {
      const id = qs.get('id') || ''
      const name = qs.get('name') || ''
      const idx = store.libraries.findIndex(
        (l) => (id && l.ItemId === id) || (name && l.Name === name),
      )
      if (idx >= 0) {
        const [removed] = store.libraries.splice(idx, 1)
        store.libraryItems.delete(removed.ItemId)
        pushActivity(`已移除媒体库「${removed.Name}」`, 'LibraryDeleted', undefined, caller.userId)
      }
      return undefined as T
    }
  }

  if (pathname === '/emby/Environment/Drives' && method === 'GET') {
    requireDemoAdmin(caller)
    return [...store.fsDrives] as T
  }

  if (pathname === '/emby/Environment/DirectoryContents' && method === 'GET') {
    requireDemoAdmin(caller)
    const rawPath = (qs.get('path') || '/').replace(/\/+$/, '') || '/'
    const entries = store.fsDirs[rawPath] || []
    return [...entries] as T
  }

  // ==================== 10. 会话、远程控制、活动日志、设备与 API Key ====================
  if (pathname === '/emby/Sessions' && method === 'GET') {
    const visibleSessions = caller.isAdmin
      ? store.sessions
      : store.sessions.filter((s) => s.UserId === caller.userId)
    return visibleSessions.map((s) => ({
      ...s,
      NowPlayingItem: s.NowPlayingItem
        ? projectItemForUser(s.NowPlayingItem, s.UserId || caller.userId)
        : undefined,
      PlayState: s.PlayState ? { ...s.PlayState } : undefined,
    })) as T
  }

  const sessionPlaystateMatch = pathname.match(/^\/emby\/Sessions\/([^/]+)\/Playing\/([^/]+)$/)
  if (sessionPlaystateMatch && method === 'POST') {
    requireDemoAdmin(caller)
    const sessionId = decodeURIComponent(sessionPlaystateMatch[1])
    const command = decodeURIComponent(sessionPlaystateMatch[2])
    const sess = store.sessions.find((s) => s.Id === sessionId)
    if (!sess) throw new ApiError(404, '会话不存在')
    const cmdLower = command.toLowerCase()
    if (cmdLower === 'stop') {
      sess.NowPlayingItem = undefined
      sess.PlayState = undefined
    } else if (sess.PlayState) {
      if (cmdLower === 'pause') {
        sess.PlayState.IsPaused = true
      } else if (cmdLower === 'unpause') {
        sess.PlayState.IsPaused = false
      } else if (cmdLower === 'playpause') {
        sess.PlayState.IsPaused = !sess.PlayState.IsPaused
      }
    }
    sess.LastActivityDate = new Date().toISOString()
    pushActivity(
      `已向会话「${sess.DeviceName || sess.Id}」发送远程指令：${command}`,
      'SessionRemoteControl',
      undefined,
      caller.userId,
    )
    if (typeof window !== 'undefined') {
      window.dispatchEvent(
        new CustomEvent('ambra:ws-mock', {
          detail: {
            MessageType: 'Playstate',
            Data: {
              Command: command,
              SessionId: sessionId,
            },
          },
        }),
      )
    }
    return undefined as T
  }

  const sessionMessageMatch = pathname.match(/^\/emby\/Sessions\/([^/]+)\/Message$/)
  if (sessionMessageMatch && method === 'POST') {
    requireDemoAdmin(caller)
    const sessionId = decodeURIComponent(sessionMessageMatch[1])
    const sess = store.sessions.find((s) => s.Id === sessionId)
    if (!sess) throw new ApiError(404, '会话不存在')
    const body = parseJsonBody(options)
    const header = String(body.Header || '系统通知')
    const text = String(body.Text || '')
    const timeoutMs = Number(body.TimeoutMs ?? 5000)
    sess.LastActivityDate = new Date().toISOString()
    pushActivity(
      `已向会话「${sess.DeviceName || sess.Id}」发送消息：${header}`,
      'SessionMessageSent',
      text,
      caller.userId,
    )
    if (typeof window !== 'undefined') {
      window.dispatchEvent(
        new CustomEvent('ambra:ws-mock', {
          detail: {
            MessageType: 'GeneralCommand',
            Data: {
              Name: 'DisplayMessage',
              Arguments: {
                Header: header,
                Text: text,
                TimeoutMs: String(timeoutMs),
              },
            },
          },
        }),
      )
    }
    return undefined as T
  }

  if (pathname === '/emby/System/ActivityLog/Entries' && method === 'GET') {
    requireDemoAdmin(caller)
    const startIndex = Math.max(0, Number(qs.get('StartIndex') || 0))
    const limit = Math.max(1, Number(qs.get('Limit') || 50))
    const severityRaw = (qs.get('Severity') || '').trim().toLowerCase()
    const searchRaw = (qs.get('SearchTerm') || '').trim().toLowerCase()

    let filtered = store.activityLogs
    if (severityRaw && severityRaw !== 'all') {
      filtered = filtered.filter((entry) => {
        const entrySev = (entry.Severity || 'Info').toLowerCase()
        if (severityRaw === 'warn' || severityRaw === 'warning') {
          return entrySev === 'warn' || entrySev === 'warning'
        }
        return entrySev === severityRaw
      })
    }
    if (searchRaw) {
      filtered = filtered.filter((entry) => {
        const haystack = [entry.Name, entry.ShortOverview, entry.Overview, entry.Type]
          .filter(Boolean)
          .join(' ')
          .toLowerCase()
        return haystack.includes(searchRaw)
      })
    }

    return {
      Items: filtered.slice(startIndex, startIndex + limit),
      TotalRecordCount: filtered.length,
    } as T
  }

  if (pathname === '/emby/Devices') {
    if (method === 'GET') {
      if (!caller.token || !caller.userId) {
        throw new ApiError(401, '未登录或登录已过期')
      }
      const visibleDevices = caller.isAdmin
        ? [...store.devices]
        : store.devices.filter((d) => d.LastUserId === caller.userId)
      return { Items: visibleDevices, TotalRecordCount: visibleDevices.length } as T
    }
    if (method === 'DELETE') {
      requireDemoAdmin(caller)
      const devId = qs.get('id') || ''
      const idx = store.devices.findIndex((d) => d.Id === devId)
      if (idx >= 0) {
        const [removed] = store.devices.splice(idx, 1)
        store.sessions = store.sessions.filter((s) => s.DeviceId !== devId)
        pushActivity(`已注销并踢出设备「${removed.Name}」`, 'DeviceRevoked', undefined, caller.userId)
      }
      return undefined as T
    }
  }

  if (pathname === '/emby/Auth/Keys') {
    requireDemoAdmin(caller)
    if (method === 'GET') {
      const items: ApiKeyInfo[] = store.apiKeys.map((k) => {
        const masked = k.MaskedToken || maskApiKeyToken(k.AccessToken)
        return {
          ...k,
          AccessToken: masked,
          MaskedToken: masked,
        }
      })
      return { Items: items, TotalRecordCount: items.length } as T
    }
    if (method === 'POST') {
      const body = parseJsonBody(options)
      const appName = String(body.AppName || body.App || qs.get('app') || '').trim()
      if (!appName) throw new ApiError(400, '应用名称不能为空')
      const id = `apikey-demo-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 6)}`
      const token =
        'ambra_demo_' +
        Math.random().toString(36).slice(2, 10) +
        Date.now().toString(36)
      const masked = maskApiKeyToken(token)
      const storedRecord: ApiKeyInfo = {
        Id: id,
        AccessToken: masked,
        MaskedToken: masked,
        AppName: appName,
        DateCreated: new Date().toISOString(),
        IsActive: true,
      }
      demoRawApiKeys.set(id, token)
      store.apiKeys.unshift(storedRecord)
      pushActivity(`为「${appName}」签发了新的 API Key`, 'ApiKeyCreated', undefined, caller.userId)
      // 一次性返回完整 AccessToken 供前端复制
      return {
        ...storedRecord,
        AccessToken: token,
      } as T
    }
  }

  const deleteKeyMatch = pathname.match(/^\/emby\/Auth\/Keys\/([^/]+)(?:\/Delete)?$/)
  if (deleteKeyMatch && (method === 'DELETE' || method === 'POST')) {
    requireDemoAdmin(caller)
    const key = decodeURIComponent(deleteKeyMatch[1])
    store.apiKeys = store.apiKeys.filter((k) => {
      const raw = k.Id ? demoRawApiKeys.get(k.Id) : undefined
      const match =
        k.Id === key || k.AccessToken === key || k.MaskedToken === key || (raw && raw === key)
      if (match) {
        if (raw) demoRevokedTokens.add(raw)
        if (k.AccessToken) demoRevokedTokens.add(k.AccessToken)
        if (k.Id) demoRawApiKeys.delete(k.Id)
      }
      return !match
    })
    pushActivity('已吊销一个第三方 API Key', 'ApiKeyRevoked', undefined, caller.userId)
    return undefined as T
  }

  // ==================== 11. 插件管理（支持双向联动、优先级更新与外部插件卸载） ====================
  if (pathname === '/emby/Plugins' && method === 'GET') {
    requireDemoAdmin(caller)
    return store.plugins.map((p) => ({ ...p })) as T
  }

  const pluginCfgMatch = pathname.match(/^\/emby\/Plugins\/([^/]+)\/Configuration$/)
  if (pluginCfgMatch) {
    requireDemoAdmin(caller)
    const rawPluginId = decodeURIComponent(pluginCfgMatch[1])
    const canonicalId = resolveCanonicalPluginId(rawPluginId)
    const personalTrakt = canonicalId === 'ambra.builtin.trakt'

    if (method === 'GET') {
      const cfg = store.pluginConfigs.get(canonicalId) || store.pluginConfigs.get(rawPluginId) || {}
      return maskPluginConfigForResponse(cfg) as T
    }
    if (method === 'POST') {
      const body = parseJsonBody(options)
      const prevCfg =
        store.pluginConfigs.get(canonicalId) || store.pluginConfigs.get(rawPluginId) || {}
      const nextCfg = personalTrakt
        ? Object.fromEntries(
            Object.entries(body).filter(([key]) =>
              ['Enabled', 'enabled', 'Priority', 'priority'].includes(key),
            ),
          )
        : mergePluginConfigForSave(prevCfg, body)
      store.pluginConfigs.set(canonicalId, nextCfg)

      const plugin = store.plugins.find((p) => p.Id === canonicalId || p.Id === rawPluginId)
      if (plugin) {
        if (typeof nextCfg.enabled === 'boolean') plugin.Enabled = nextCfg.enabled
        else if (typeof nextCfg.Enabled === 'boolean') plugin.Enabled = nextCfg.Enabled
        plugin.Status = plugin.Enabled ? 'Active' : 'Disabled'
        if (typeof nextCfg.priority === 'number') plugin.Priority = nextCfg.priority
        else if (typeof nextCfg.Priority === 'number') plugin.Priority = nextCfg.Priority
        pushActivity(`更新了插件「${plugin.Name}」的配置`, 'PluginConfigured', undefined, caller.userId)
      }

      // 与 /System/Configuration 双向联动
      if (canonicalId === 'ambra.metadata.tmdb') {
        const keyCandidate =
          typeof nextCfg.api_key === 'string'
            ? nextCfg.api_key
            : typeof nextCfg.ApiKey === 'string'
              ? nextCfg.ApiKey
              : undefined
        if (keyCandidate !== undefined) {
          demoRawTmdbApiKey = keyCandidate.trim()
        }
        const langCandidate =
          typeof nextCfg.language === 'string'
            ? nextCfg.language
            : typeof nextCfg.Language === 'string'
              ? nextCfg.Language
              : undefined
        if (langCandidate && langCandidate.trim()) {
          demoServerConfig.TMDBLanguage = langCandidate.trim()
          demoDefaultLanguage = langCandidate.trim()
        }
      } else if (canonicalId === 'ambra.stream.strm302') {
        const strmEnabled =
          typeof nextCfg.enable_302 === 'boolean'
            ? nextCfg.enable_302
            : typeof nextCfg.Enable302 === 'boolean'
              ? nextCfg.Enable302
              : typeof nextCfg.enabled === 'boolean'
                ? nextCfg.enabled
                : typeof nextCfg.Enabled === 'boolean'
                  ? nextCfg.Enabled
                  : undefined
        if (strmEnabled !== undefined) {
          demoServerConfig.EnableSTRM302 = strmEnabled
        }
      }
      return undefined as T
    }
  }

  const pluginDeleteMatch = pathname.match(/^\/emby\/Plugins\/([^/]+)$/)
  if (pluginDeleteMatch && method === 'DELETE') {
    requireDemoAdmin(caller)
    const pluginId = decodeURIComponent(pluginDeleteMatch[1])
    const idx = store.plugins.findIndex((p) => p.Id === pluginId)
    if (idx < 0) {
      throw new ApiError(404, '插件不存在')
    }
    if (store.plugins[idx].BuiltIn !== false) {
      throw new ApiError(400, '内置插件不可卸载')
    }
    const [removed] = store.plugins.splice(idx, 1)
    store.pluginConfigs.delete(pluginId)
    pushActivity(`已卸载外部插件「${removed.Name}」`, 'PluginUninstalled', pluginId, caller.userId)
    return undefined as T
  }

  // ==================== 12. 计划任务、触发规则、Live TV 与服务器日志 ====================
  if (pathname === '/emby/ScheduledTasks' && method === 'GET') {
    requireDemoAdmin(caller)
    return store.scheduledTasks.map((t) => ({
      ...t,
      Triggers: t.Triggers ? t.Triggers.map((tr) => ({ ...tr })) : [],
    })) as T
  }

  const taskTriggersMatch = pathname.match(/^\/emby\/ScheduledTasks\/([^/]+)\/Triggers$/)
  if (taskTriggersMatch && method === 'POST') {
    requireDemoAdmin(caller)
    const taskId = decodeURIComponent(taskTriggersMatch[1])
    const task = store.scheduledTasks.find((t) => t.Id === taskId)
    if (!task) throw new ApiError(404, '计划任务不存在')
    let parsedTriggers: TaskTriggerInfo[] = []
    if (typeof options.body === 'string') {
      try {
        const raw = JSON.parse(options.body)
        if (Array.isArray(raw)) {
          parsedTriggers = raw as TaskTriggerInfo[]
        }
      } catch {
        parsedTriggers = []
      }
    }
    task.Triggers = parsedTriggers
    pushActivity(
      `已更新计划任务「${task.Name}」的触发规则（${parsedTriggers.length} 条）`,
      'ScheduledTaskTriggersUpdated',
      undefined,
      caller.userId,
    )
    return undefined as T
  }

  // Live TV (M3U Tuner Hosts / XMLTV EPG / Channels / ChannelMappings / Timers / SeriesTimers)
  if (pathname === '/emby/LiveTv/Info' && method === 'GET') {
    const info: LiveTvInfo = {
      IsEnabled: true,
      Services: [{ Name: 'Ambra M3U / XMLTV', Status: 'Ok' }],
      EnabledUsers: store.users
        .filter((u) => u.user.Policy?.EnableLiveTvAccess !== false)
        .map((u) => u.user.Id),
    }
    return info as T
  }

  if (pathname === '/emby/LiveTv/Channels' && method === 'GET') {
    if (caller.user?.Policy?.EnableLiveTvAccess === false) {
      throw new ApiError(403, '当前账号无 Live TV 访问权限')
    }
    const limit = Number(qs.get('Limit') || 100)
    const sliced = demoLiveTvChannels.slice(0, limit)
    return { Items: sliced, TotalRecordCount: demoLiveTvChannels.length } as T
  }

  const liveTvStreamMatch = pathname.match(/^\/emby\/LiveTv\/Channels\/([^/]+)\/Stream$/)
  if (liveTvStreamMatch && method === 'GET') {
    if (caller.user?.Policy?.EnableLiveTvAccess === false) {
      throw new ApiError(403, '当前账号无 Live TV 访问权限')
    }
    const channelId = decodeURIComponent(liveTvStreamMatch[1])
    const channel = demoLiveTvChannels.find((ch) => ch.Id === channelId)
    if (!channel) {
      throw new ApiError(404, `频道「${channelId}」不存在`)
    }
    return {
      ChannelId: channel.Id,
      Name: channel.Name,
      StreamUrl: DEMO_VIDEO_DATA_URL,
      MimeType: 'video/mp4',
    } as T
  }

  if (pathname === '/emby/LiveTv/Programs' && method === 'GET') {
    if (caller.user?.Policy?.EnableLiveTvAccess === false) {
      throw new ApiError(403, '当前账号无 Live TV 访问权限')
    }
    const nowMs = Date.now()
    const halfHour = 30 * 60_000
    const oneHour = 60 * 60_000
    const channelIdsParam = qs.get('ChannelIds')
    const filterSet = channelIdsParam
      ? new Set(
          channelIdsParam
            .split(',')
            .map((s) => s.trim())
            .filter(Boolean),
        )
      : null
    const limit = Number(qs.get('Limit') || 100)

    const epgTemplates: Record<
      string,
      Array<{ name: string; overview: string; offsetStart: number; offsetEnd: number }>
    > = {
      'ch-demo-01': [
        {
          name: '新闻联播',
          overview: '每日晚间权威时政与国际要闻播报。',
          offsetStart: -halfHour,
          offsetEnd: halfHour,
        },
        {
          name: '焦点访谈',
          overview: '深度聚焦社会热点与民生话题。',
          offsetStart: halfHour,
          offsetEnd: halfHour + oneHour,
        },
        {
          name: '黄金档剧场',
          overview: '精品国产剧连播。',
          offsetStart: halfHour + oneHour,
          offsetEnd: halfHour + 2 * oneHour,
        },
      ],
      'ch-demo-02': [
        {
          name: '佳片有约：经典科幻之夜',
          overview: '深度影评导赏与高分科幻经典展映。',
          offsetStart: -halfHour,
          offsetEnd: oneHour,
        },
        {
          name: '中国电影报道',
          overview: '聚焦院线新片动态与幕后花絮。',
          offsetStart: oneHour,
          offsetEnd: 2 * oneHour,
        },
      ],
      'ch-demo-03': [
        {
          name: '4K HDR 自然纪录片：蔚蓝星球',
          overview: '超高清 60fps 实拍深海与极地奇观。',
          offsetStart: -halfHour,
          offsetEnd: oneHour,
        },
        {
          name: '杜比视界演示片合集',
          overview: '高动态范围色彩与声场测试节目。',
          offsetStart: oneHour,
          offsetEnd: 2 * oneHour,
        },
      ],
    }

    const allPrograms = demoLiveTvChannels.flatMap((ch) => {
      const templates = epgTemplates[ch.Id] || [
        {
          name: `${ch.Name} 实时节目`,
          overview: `${ch.Name} 正在播出的高清直播内容。`,
          offsetStart: -halfHour,
          offsetEnd: halfHour,
        },
        {
          name: `${ch.Name} 精彩预告`,
          overview: '下一时段精选节目即将开始。',
          offsetStart: halfHour,
          offsetEnd: halfHour + oneHour,
        },
      ]
      return templates.map((tpl, idx) => ({
        Id: `prog-${ch.Id}-${idx + 1}`,
        ChannelId: ch.Id,
        Name: tpl.name,
        Overview: tpl.overview,
        StartDate: new Date(nowMs + tpl.offsetStart).toISOString(),
        EndDate: new Date(nowMs + tpl.offsetEnd).toISOString(),
      }))
    })

    const filtered = filterSet
      ? allPrograms.filter((p) => filterSet.has(p.ChannelId))
      : allPrograms
    const sliced = filtered.slice(0, limit)
    return { Items: sliced, TotalRecordCount: filtered.length } as T
  }

  if (pathname === '/emby/LiveTv/Tuners/Discover' && method === 'GET') {
    requireDemoAdmin(caller)
    return [
      {
        Id: 'hdhr-demo-108a3f21',
        Url: 'http://192.168.1.58',
        Type: 'hdhomerun',
        DeviceId: '108A3F21',
        FriendlyName: 'HDHomeRun FLEX 4K',
        TunerCount: 4,
        ImportFavoritesOnly: false,
      },
      {
        Id: 'hdhr-demo-104c9b10',
        Url: 'http://192.168.1.62',
        Type: 'hdhomerun',
        DeviceId: '104C9B10',
        FriendlyName: 'HDHomeRun CONNECT DUO',
        TunerCount: 2,
        ImportFavoritesOnly: false,
      },
    ] as T
  }

  if (pathname === '/emby/LiveTv/TunerHosts') {
    requireDemoAdmin(caller)
    if (method === 'GET') {
      return demoTunerHosts.map((h) => ({ ...h })) as T
    }
    if (method === 'POST') {
      const body = parseJsonBody(options)
      const urlVal = String(body.Url || '').trim()
      if (!urlVal) throw new ApiError(400, 'M3U 地址不能为空')
      const created: LiveTvTunerHostInfo = {
        Id: `tuner-demo-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 5)}`,
        Type: String(body.Type || 'm3u'),
        FriendlyName: String(body.FriendlyName || '').trim() || 'M3U 直播源',
        Url: urlVal,
        TunerCount: typeof body.TunerCount === 'number' ? body.TunerCount : 4,
      }
      demoTunerHosts.push(created)
      pushActivity(
        `已添加 Live TV 直播源「${created.FriendlyName}」`,
        'LiveTvTunerAdded',
        urlVal,
        caller.userId,
      )
      return { ...created } as T
    }
    if (method === 'DELETE') {
      const id = qs.get('id') || ''
      const idx = demoTunerHosts.findIndex((h) => h.Id === id)
      if (idx >= 0) {
        const [removed] = demoTunerHosts.splice(idx, 1)
        pushActivity(
          `已删除 Live TV 直播源「${removed.FriendlyName || id}」`,
          'LiveTvTunerDeleted',
          undefined,
          caller.userId,
        )
      }
      return undefined as T
    }
  }

  const refreshTunerMatch = pathname.match(/^\/emby\/LiveTv\/TunerHosts\/([^/]+)\/Refresh$/)
  if (refreshTunerMatch && method === 'POST') {
    requireDemoAdmin(caller)
    const id = decodeURIComponent(refreshTunerMatch[1])
    const host = demoTunerHosts.find((h) => h.Id === id)
    if (!host) {
      throw new ApiError(404, '未找到指定的 Live TV 直播源')
    }
    if (!host.Url || !host.Url.trim()) {
      throw new ApiError(400, '该直播源未配置可刷新的远程地址')
    }
    const updated: LiveTvTunerHostInfo = {
      ...host,
      ImportedChannelCount: demoLiveTvChannels.length,
    }
    pushActivity(
      `已刷新 Live TV 直播源「${host.FriendlyName || id}」`,
      'LiveTvTunerRefreshed',
      `${demoLiveTvChannels.length} channels`,
      caller.userId,
    )
    return updated as T
  }

  if (pathname === '/emby/LiveTv/ListingProviders') {
    requireDemoAdmin(caller)
    if (method === 'GET') {
      return demoListingProviders.map((p) => ({ ...p })) as T
    }
    if (method === 'POST') {
      const body = parseJsonBody(options)
      const pathVal = String(body.Path || '').trim()
      if (!pathVal) throw new ApiError(400, 'EPG 路径不能为空')
      const created: LiveTvListingProviderInfo = {
        Id: `epg-demo-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 5)}`,
        Type: String(body.Type || 'xmltv'),
        Path: pathVal,
      }
      demoListingProviders.push(created)
      pushActivity(`已添加 XMLTV EPG 节目单来源`, 'LiveTvEpgAdded', pathVal, caller.userId)
      return { ...created } as T
    }
    if (method === 'DELETE') {
      const id = qs.get('id') || ''
      const idx = demoListingProviders.findIndex((p) => p.Id === id)
      if (idx >= 0) {
        demoListingProviders.splice(idx, 1)
        demoLiveTvChannelMappings = demoLiveTvChannelMappings.filter((m) => m.ProviderId !== id)
        pushActivity(`已删除 XMLTV EPG 节目单来源`, 'LiveTvEpgDeleted', undefined, caller.userId)
      }
      return undefined as T
    }
  }

  const canManageDvr = caller.isAdmin || Boolean(caller.user?.Policy?.EnableLiveTvManagement)

  if (pathname === '/emby/LiveTv/ChannelMappingOptions' && method === 'GET') {
    if (!canManageDvr) {
      throw new ApiError(403, '需要管理员或 Live TV 管理权限 (403 Forbidden)')
    }
    const providerId =
      qs.get('providerId') ||
      qs.get('ProviderId') ||
      demoListingProviders[0]?.Id ||
      'epg-demo-01'
    const activeMappings = demoLiveTvChannelMappings.filter(
      (m) => !m.ProviderId || m.ProviderId === providerId,
    )
    const tunerChannels = demoLiveTvChannels.map((ch) => ({
      Id: ch.Id,
      Name: ch.Name,
      Number: ch.Number,
      ProviderChannelId:
        activeMappings.find((m) => m.TunerChannelId === ch.Id)?.ProviderChannelId || undefined,
    }))
    const providerChannels = demoLiveTvChannels.map((ch, idx) => ({
      Id: `xmltv-${ch.Id || idx + 1}`,
      Name: `${ch.Name} (XMLTV)`,
    }))
    for (const m of activeMappings) {
      if (m.ProviderChannelId && !providerChannels.some((p) => p.Id === m.ProviderChannelId)) {
        providerChannels.push({
          Id: m.ProviderChannelId,
          Name: m.ProviderChannelName || m.ProviderChannelId,
        })
      }
    }
    return {
      TunerChannels: tunerChannels,
      ProviderChannels: providerChannels,
      Mappings: activeMappings.map((m) => ({ ...m })),
      ProviderName: providerId,
    } as T
  }

  if (pathname === '/emby/LiveTv/ChannelMappings') {
    if (!canManageDvr) {
      throw new ApiError(403, '需要管理员或 Live TV 管理权限 (403 Forbidden)')
    }
    if (method === 'GET') {
      return demoLiveTvChannelMappings.map((m) => ({ ...m })) as T
    }
    if (method === 'POST') {
      const body = parseJsonBody(options)
      const tunerChannelId = String(body.TunerChannelId || '').trim()
      const providerChannelId = String(body.ProviderChannelId ?? '').trim()
      const providerId = String(
        body.ProviderId || demoListingProviders[0]?.Id || 'epg-demo-01',
      ).trim()
      if (!tunerChannelId) {
        throw new ApiError(400, 'TunerChannelId 不能为空')
      }
      const ch = demoLiveTvChannels.find((c) => c.Id === tunerChannelId)
      const existingIdx = demoLiveTvChannelMappings.findIndex(
        (m) =>
          m.TunerChannelId === tunerChannelId && (!m.ProviderId || m.ProviderId === providerId),
      )
      if (!providerChannelId) {
        if (existingIdx >= 0) {
          demoLiveTvChannelMappings.splice(existingIdx, 1)
        }
        pushActivity(
          `已解除频道「${ch?.Name || tunerChannelId}」的 EPG 映射`,
          'LiveTvChannelUnmapped',
          undefined,
          caller.userId,
        )
        return {
          ProviderId: providerId,
          TunerChannelId: tunerChannelId,
          ProviderChannelId: '',
        } as T
      }
      const mapping: LiveTvChannelMapping = {
        ProviderId: providerId,
        TunerChannelId: tunerChannelId,
        TunerChannelName: String(body.TunerChannelName || ch?.Name || tunerChannelId),
        ProviderChannelId: providerChannelId,
        ProviderChannelName: String(body.ProviderChannelName || providerChannelId),
      }
      if (existingIdx >= 0) {
        demoLiveTvChannelMappings[existingIdx] = mapping
      } else {
        demoLiveTvChannelMappings.push(mapping)
      }
      pushActivity(
        `已更新频道映射「${mapping.TunerChannelName} -> ${mapping.ProviderChannelId}」`,
        'LiveTvChannelMapped',
        undefined,
        caller.userId,
      )
      return { ...mapping } as T
    }
  }

  // Live TV DVR 系列录制规则 (SeriesTimers)
  if (pathname === '/emby/LiveTv/SeriesTimers') {
    if (method === 'GET') {
      if (caller.user?.Policy?.EnableLiveTvAccess === false) {
        throw new ApiError(403, '当前账号无 Live TV 访问权限')
      }
      const items = demoLiveTvSeriesTimers.map((t) => ({
        ...t,
        Days: t.Days ? [...t.Days] : [],
      }))
      return { Items: items, TotalRecordCount: items.length } as T
    }
    if (method === 'POST') {
      if (!canManageDvr) throw new ApiError(403, '需要管理员或 DVR 录制管理权限')
      const body = parseJsonBody(options)
      const name = String(body.Name || '').trim()
      if (!name) throw new ApiError(400, '系列录制名称不能为空')
      const created: LiveTvSeriesTimerInfo = {
        Id: `stimer-demo-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 5)}`,
        ChannelId: body.ChannelId ? String(body.ChannelId) : undefined,
        ProgramId: body.ProgramId ? String(body.ProgramId) : undefined,
        SeriesId: body.SeriesId ? String(body.SeriesId) : undefined,
        Name: name,
        Overview: body.Overview ? String(body.Overview) : undefined,
        RecordAnyTime: Boolean(body.RecordAnyTime),
        RecordAnyChannel: Boolean(body.RecordAnyChannel),
        RecordNewOnly: body.RecordNewOnly !== false,
        Days: Array.isArray(body.Days) ? body.Days.map((d) => String(d)) : [],
        PrePaddingSeconds: typeof body.PrePaddingSeconds === 'number' ? body.PrePaddingSeconds : 60,
        PostPaddingSeconds:
          typeof body.PostPaddingSeconds === 'number' ? body.PostPaddingSeconds : 180,
      }
      demoLiveTvSeriesTimers.push(created)
      pushActivity(`已创建系列录制规则「${created.Name}」`, 'LiveTvSeriesTimerCreated', undefined, caller.userId)
      return { ...created } as T
    }
    if (method === 'DELETE') {
      if (!canManageDvr) throw new ApiError(403, '需要管理员或 DVR 录制管理权限')
      const id = qs.get('id') || qs.get('Id') || ''
      demoLiveTvSeriesTimers = demoLiveTvSeriesTimers.filter((t) => t.Id !== id)
      return undefined as T
    }
  }

  const seriesTimerSingleMatch = pathname.match(/^\/emby\/LiveTv\/SeriesTimers\/([^/]+)(?:\/Delete)?$/)
  if (seriesTimerSingleMatch && (method === 'DELETE' || method === 'POST')) {
    if (!canManageDvr) throw new ApiError(403, '需要管理员或 DVR 录制管理权限')
    const id = decodeURIComponent(seriesTimerSingleMatch[1])
    if (method === 'DELETE' || pathname.endsWith('/Delete')) {
      demoLiveTvSeriesTimers = demoLiveTvSeriesTimers.filter((t) => t.Id !== id)
      return undefined as T
    }
  }

  // Live TV DVR 预约录制 (Timers)
  if (pathname === '/emby/LiveTv/Timers') {
    if (method === 'GET') {
      if (caller.user?.Policy?.EnableLiveTvAccess === false) {
        throw new ApiError(403, '当前账号无 Live TV 访问权限')
      }
      const items = demoLiveTvTimers.map((t) => ({ ...t }))
      return { Items: items, TotalRecordCount: items.length } as T
    }
    if (method === 'POST') {
      if (!canManageDvr) throw new ApiError(403, '需要管理员或 DVR 录制管理权限')
      const body = parseJsonBody(options)
      const channelId = String(body.ChannelId || '').trim()
      const name = String(body.Name || '').trim()
      if (!channelId || !name) {
        throw new ApiError(400, '频道 ID 与录制名称不能为空')
      }
      const ch = demoLiveTvChannels.find((c) => c.Id === channelId)
      const created: LiveTvTimerInfo = {
        Id: `timer-demo-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 5)}`,
        ChannelId: channelId,
        ChannelName: String(body.ChannelName || ch?.Name || channelId),
        ProgramId: body.ProgramId ? String(body.ProgramId) : undefined,
        Name: name,
        Overview: body.Overview ? String(body.Overview) : undefined,
        StartDate: String(body.StartDate || new Date().toISOString()),
        EndDate: String(body.EndDate || new Date(Date.now() + 3600_000).toISOString()),
        Status: 'New',
        PrePaddingSeconds:
          typeof body.PrePaddingSeconds === 'number' ? body.PrePaddingSeconds : 0,
        PostPaddingSeconds:
          typeof body.PostPaddingSeconds === 'number' ? body.PostPaddingSeconds : 0,
      }
      demoLiveTvTimers.push(created)
      pushActivity(`已创建 DVR 录制计划「${created.Name}」`, 'LiveTvTimerCreated', undefined, caller.userId)
      return { ...created } as T
    }
    if (method === 'DELETE') {
      if (!canManageDvr) throw new ApiError(403, '需要管理员或 DVR 录制管理权限')
      const id = qs.get('id') || qs.get('Id') || ''
      const idx = demoLiveTvTimers.findIndex((t) => t.Id === id)
      if (idx >= 0) {
        const [removed] = demoLiveTvTimers.splice(idx, 1)
        pushActivity(`已取消 DVR 录制计划「${removed.Name}」`, 'LiveTvTimerDeleted', undefined, caller.userId)
      }
      return undefined as T
    }
  }

  const liveTvTimerDeleteMatch = pathname.match(/^\/emby\/LiveTv\/Timers\/([^/]+)$/)
  if (liveTvTimerDeleteMatch && method === 'DELETE') {
    if (!canManageDvr) throw new ApiError(403, '需要管理员或 DVR 录制管理权限')
    const id = decodeURIComponent(liveTvTimerDeleteMatch[1])
    const idx = demoLiveTvTimers.findIndex((t) => t.Id === id)
    if (idx >= 0) {
      const [removed] = demoLiveTvTimers.splice(idx, 1)
      pushActivity(`已取消 DVR 录制计划「${removed.Name}」`, 'LiveTvTimerDeleted', undefined, caller.userId)
    }
    return undefined as T
  }

  // Live TV DVR 已完成录制 (Recordings)
  if (pathname === '/emby/LiveTv/Recordings') {
    if (method === 'GET') {
      if (caller.user?.Policy?.EnableLiveTvAccess === false) {
        throw new ApiError(403, '当前账号无 Live TV 访问权限')
      }
      const items = demoLiveTvRecordings.map((r) => ({ ...r }))
      return { Items: items, TotalRecordCount: items.length } as T
    }
    if (method === 'DELETE') {
      if (!canManageDvr) throw new ApiError(403, '需要管理员或 DVR 录制管理权限')
      const id = qs.get('id') || qs.get('Id') || ''
      const idx = demoLiveTvRecordings.findIndex((r) => r.Id === id)
      if (idx >= 0) {
        const [removed] = demoLiveTvRecordings.splice(idx, 1)
        pushActivity(`已删除 DVR 录制文件「${removed.Name}」`, 'LiveTvRecordingDeleted', undefined, caller.userId)
      }
      return undefined as T
    }
  }

  const liveTvRecordingDeleteMatch = pathname.match(/^\/emby\/LiveTv\/Recordings\/([^/]+)$/)
  if (liveTvRecordingDeleteMatch && method === 'DELETE') {
    if (!canManageDvr) throw new ApiError(403, '需要管理员或 DVR 录制管理权限')
    const id = decodeURIComponent(liveTvRecordingDeleteMatch[1])
    const idx = demoLiveTvRecordings.findIndex((r) => r.Id === id)
    if (idx >= 0) {
      const [removed] = demoLiveTvRecordings.splice(idx, 1)
      pushActivity(`已删除 DVR 录制文件「${removed.Name}」`, 'LiveTvRecordingDeleted', undefined, caller.userId)
    }
    return undefined as T
  }

  // 离线同步与转码任务 (Sync Jobs)
  if (pathname === '/emby/Sync/Jobs' && method === 'GET') {
    const visibleJobs = caller.isAdmin
      ? demoSyncJobs
      : demoSyncJobs.filter((j) => j.UserId === caller.userId)
    const items = visibleJobs.map((j) => ({ ...j }))
    return { Items: items, TotalRecordCount: items.length } as T
  }

  const syncJobDeleteMatch = pathname.match(/^\/emby\/Sync\/Jobs\/([^/]+)$/)
  if (syncJobDeleteMatch && method === 'DELETE') {
    const id = decodeURIComponent(syncJobDeleteMatch[1])
    const idx = demoSyncJobs.findIndex((j) => j.Id === id)
    if (idx >= 0) {
      if (!caller.isAdmin && demoSyncJobs[idx].UserId !== caller.userId) {
        throw new ApiError(403, '无权取消其他用户的同步任务 (403 Forbidden)')
      }
      const [removed] = demoSyncJobs.splice(idx, 1)
      pushActivity(`已取消同步任务「${removed.Name}」`, 'SyncJobDeleted', undefined, caller.userId)
    }
    return undefined as T
  }

  // Webhooks 事件通知管理（含合法 http/https 协议校验）
  if (pathname === '/emby/Webhooks') {
    requireDemoAdmin(caller)
    if (method === 'GET') {
      return demoWebhooks.map((w) => sanitizeWebhookForResponse(w)) as T
    }
    if (method === 'POST') {
      const body = parseJsonBody(options)
      const name = String(body.Name || '').trim()
      const rawUrl = String(body.Url || '').trim()
      if (!name || !rawUrl) {
        throw new ApiError(400, 'Webhook 名称与推送 URL 不能为空')
      }
      const urlVal = validateHttpUrl(rawUrl, 'Webhook 推送 URL')
      const events = Array.isArray(body.Events)
        ? body.Events.map((e) => String(e).trim()).filter(Boolean)
        : ['*']
      const clearSecret = Boolean(body.ClearSecret)
      const incomingSecret =
        typeof body.Secret === 'string' && body.Secret.trim() ? body.Secret.trim() : undefined
      const enabledVal = body.Enabled !== undefined ? Boolean(body.Enabled) : true
      const existingId = typeof body.Id === 'string' ? body.Id.trim() : ''
      const existingIdx = existingId ? demoWebhooks.findIndex((w) => w.Id === existingId) : -1

      if (existingIdx >= 0) {
        const prev = demoWebhooks[existingIdx]
        const endpointKey = (raw: string): string => {
          try {
            const parsed = new URL(raw)
            return `${parsed.origin}${parsed.pathname}`
          } catch {
            return raw
          }
        }
        const endpointChanged = endpointKey(prev.Url) !== endpointKey(urlVal)
        const nextSecret = clearSecret
          ? undefined
          : (incomingSecret ?? (endpointChanged ? undefined : prev.Secret))
        const updated: WebhookInfo = {
          ...prev,
          Name: name,
          Url: urlVal,
          Secret: nextSecret,
          HasSecret: Boolean(nextSecret),
          Events: events.length > 0 ? events : ['*'],
          Enabled: enabledVal,
        }
        demoWebhooks[existingIdx] = updated
        pushActivity(`已更新 Webhook「${updated.Name}」`, 'WebhookUpdated', undefined, caller.userId)
        return sanitizeWebhookForResponse(updated) as T
      }

      const created: WebhookInfo = {
        Id: `wh-demo-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 5)}`,
        Name: name,
        Url: urlVal,
        Secret: incomingSecret,
        HasSecret: Boolean(incomingSecret),
        Events: events.length > 0 ? events : ['*'],
        Enabled: enabledVal,
        CreatedAt: Math.floor(Date.now() / 1000),
      }
      demoWebhooks.push(created)
      pushActivity(`已创建 Webhook「${created.Name}」`, 'WebhookCreated', undefined, caller.userId)
      return sanitizeWebhookForResponse(created) as T
    }
  }

  if (pathname === '/emby/Webhooks/Test' && method === 'POST') {
    requireDemoAdmin(caller)
    const body = parseJsonBody(options)
    const id = typeof body.Id === 'string' ? body.Id.trim() : ''
    const existing = id ? demoWebhooks.find((w) => w.Id === id) : undefined
    const rawTargetUrl = String(body.Url || existing?.Url || '').trim()
    const targetUrl = validateHttpUrl(rawTargetUrl, 'Webhook 测试 URL')
    if (targetUrl.includes('fail.invalid') || targetUrl.includes('/fail-test')) {
      const failRes: WebhookTestResult = {
        Success: false,
        StatusCode: 502,
        DurationMs: 128,
        Error: '远端 Webhook 端点返回 502 Bad Gateway',
      }
      return failRes as T
    }
    const res: WebhookTestResult = {
      Success: true,
      StatusCode: 200,
      DurationMs: 42,
    }
    return res as T
  }

  const webhookDeleteMatch = pathname.match(/^\/emby\/Webhooks\/([^/]+)$/)
  if (webhookDeleteMatch && method === 'DELETE') {
    requireDemoAdmin(caller)
    const id = decodeURIComponent(webhookDeleteMatch[1])
    const idx = demoWebhooks.findIndex((w) => w.Id === id)
    if (idx >= 0) {
      const [removed] = demoWebhooks.splice(idx, 1)
      pushActivity(`已删除 Webhook「${removed.Name}」`, 'WebhookDeleted', undefined, caller.userId)
    }
    return undefined as T
  }

  // 家长控制内容分级列表
  if (pathname === '/emby/Localization/ParentalRatings' && method === 'GET') {
    return DEMO_PARENTAL_RATINGS.map((r) => ({ ...r })) as T
  }

  const runTaskMatch = pathname.match(/^\/emby\/ScheduledTasks\/Running\/([^/]+)$/)
  if (runTaskMatch && method === 'POST') {
    requireDemoAdmin(caller)
    const taskId = decodeURIComponent(runTaskMatch[1])
    const task = store.scheduledTasks.find((t) => t.Id === taskId)
    if (!task) throw new ApiError(404, '计划任务不存在')
    const now = new Date().toISOString()
    task.State = 'Idle'
    task.CurrentProgressPercentage = 100
    task.LastExecutionResult = {
      Status: 'Completed',
      StartTimeUtc: now,
      EndTimeUtc: now,
    }
    pushActivity(`手动执行计划任务「${task.Name}」完成`, 'ScheduledTaskCompleted', undefined, caller.userId)
    return undefined as T
  }

  if (runTaskMatch && method === 'DELETE') {
    requireDemoAdmin(caller)
    const taskId = decodeURIComponent(runTaskMatch[1])
    const task = store.scheduledTasks.find((t) => t.Id === taskId)
    if (!task) throw new ApiError(404, '计划任务不存在')
    const now = new Date().toISOString()
    task.State = 'Idle'
    task.CurrentProgressPercentage = 0
    task.LastExecutionResult = {
      Status: 'Aborted',
      StartTimeUtc: now,
      EndTimeUtc: now,
    }
    pushActivity(`已手动停止计划任务「${task.Name}」`, 'ScheduledTaskStopped', undefined, caller.userId)
    return undefined as T
  }

  if (pathname === '/emby/System/Logs/Query' && method === 'GET') {
    requireDemoAdmin(caller)
    return [...store.serverLogs] as T
  }

  const logContentMatch = pathname.match(/^\/emby\/System\/Logs\/([^/]+)$/)
  if (logContentMatch && method === 'GET') {
    requireDemoAdmin(caller)
    const name = decodeURIComponent(logContentMatch[1])
    const content = store.serverLogContents.get(name)
    if (content === undefined) throw new ApiError(404, `日志文件 ${name} 不存在`)
    return content as T
  }

  // ==================== 13. 榜单缓存与自动打标 ====================
  if (pathname === '/emby/System/Rankings' && method === 'GET') {
    const summary: RankingsSummary = {
      douban_top250: {
        chart: 'douban_top250',
        source: '豆瓣电影 Top 250 (Demo 内存缓存)',
        updated_at: rankingsState.doubanUpdatedAt,
        count: 250,
      },
      imdb_top250: {
        chart: 'imdb_top250',
        source: 'IMDb Top 250 (Demo 内存缓存)',
        updated_at: rankingsState.imdbUpdatedAt,
        count: 250,
      },
      tmdb_trending: {
        chart: 'tmdb_trending',
        source: 'TMDB Trending Weekly (Demo 内存缓存)',
        updated_at: rankingsState.tmdbTrendingUpdatedAt,
        count: 40,
      },
      douban_trending_movie: {
        chart: 'douban_trending_movie',
        source: '豆瓣热门电影 (Demo 内存缓存)',
        updated_at: rankingsState.doubanTrendingMovieUpdatedAt,
        count: 50,
      },
      douban_trending_tv: {
        chart: 'douban_trending_tv',
        source: '豆瓣热门剧集 (Demo 内存缓存)',
        updated_at: rankingsState.doubanTrendingTvUpdatedAt,
        count: 50,
      },
    }
    return summary as T
  }

  if (pathname === '/emby/System/Rankings/Refresh' && method === 'POST') {
    requireDemoAdmin(caller)
    const now = new Date().toISOString()
    rankingsState.doubanUpdatedAt = now
    rankingsState.imdbUpdatedAt = now
    rankingsState.tmdbTrendingUpdatedAt = now
    rankingsState.doubanTrendingMovieUpdatedAt = now
    rankingsState.doubanTrendingTvUpdatedAt = now

    let matchedItems = 0
    let tagsAdded = 0

    for (const item of store.items.values()) {
      if (item.Type !== 'Movie' && item.Type !== 'Series') continue
      const tmdbId = item.ProviderIds?.Tmdb || ''
      const inDouban =
        DEMO_DOUBAN_TOP250_TMDB_IDS.has(tmdbId) || Boolean(item.Tags?.includes('豆瓣Top250'))
      const inImdb =
        DEMO_IMDB_TOP250_TMDB_IDS.has(tmdbId) || Boolean(item.Tags?.includes('IMDb Top250'))
      const inTmdbTrending =
        DEMO_TMDB_TRENDING_TMDB_IDS.has(tmdbId) || Boolean(item.Tags?.includes('TMDB热门'))
      const inDoubanTrendingMovie =
        item.Type === 'Movie' &&
        (DEMO_DOUBAN_TRENDING_MOVIE_TMDB_IDS.has(tmdbId) ||
          Boolean(item.Tags?.includes('豆瓣热门电影')))
      const inDoubanTrendingTv =
        item.Type === 'Series' &&
        (DEMO_DOUBAN_TRENDING_TV_TMDB_IDS.has(tmdbId) ||
          Boolean(item.Tags?.includes('豆瓣热门剧集')))
      const inAnyTrending =
        inTmdbTrending ||
        inDoubanTrendingMovie ||
        inDoubanTrendingTv ||
        Boolean(item.Tags?.includes('Trending'))

      if (!inDouban && !inImdb && !inAnyTrending) continue
      matchedItems++
      const tags = new Set(item.Tags || [])
      if (inDouban && !tags.has('豆瓣Top250')) {
        tags.add('豆瓣Top250')
        tagsAdded++
      }
      if (inImdb && !tags.has('IMDb Top250')) {
        tags.add('IMDb Top250')
        tagsAdded++
      }
      if (inAnyTrending && !tags.has('Trending')) {
        tags.add('Trending')
        tagsAdded++
      }
      if (inTmdbTrending && !tags.has('TMDB热门')) {
        tags.add('TMDB热门')
        tagsAdded++
      }
      if (inDoubanTrendingMovie && !tags.has('豆瓣热门电影')) {
        tags.add('豆瓣热门电影')
        tagsAdded++
      }
      if (inDoubanTrendingTv && !tags.has('豆瓣热门剧集')) {
        tags.add('豆瓣热门剧集')
        tagsAdded++
      }
      item.Tags = Array.from(tags)
    }

    pushActivity(
      '已同步豆瓣 Top 250 / IMDb Top 250 / TMDB 热门 / 豆瓣热门电影与剧集缓存',
      'RankingsSynced',
      `命中 ${matchedItems} 部影片，新增标签 ${tagsAdded} 个`,
      caller.userId,
    )

    const result: RankingsRefreshResult = {
      douban_count: 250,
      imdb_count: 250,
      tmdb_trending_count: 40,
      douban_trending_movie_count: 50,
      douban_trending_tv_count: 50,
      matched_items: matchedItems,
      tags_added: tagsAdded,
      synced_at: now,
    }
    return result as T
  }

  // ==================== 14. 媒体元数据、图片、远端字幕搜索下载、远端识别与多版本 ====================
  const remoteSubtitlesMatch = pathname.match(
    /^\/emby\/Items\/([^/]+)\/RemoteSearch\/Subtitles\/([^/]+)$/,
  )
  if (remoteSubtitlesMatch) {
    const itemId = decodeURIComponent(remoteSubtitlesMatch[1])
    const param = decodeURIComponent(remoteSubtitlesMatch[2])
    const item = store.items.get(itemId)
    if (!item) throw new ApiError(404, '项目不存在')

    if (method === 'GET') {
      const lang = param || 'chi'
      const results: RemoteSubtitleInfo[] = [
        {
          Id: `sub-remote-${itemId}-${lang}-1`,
          Name: `${item.Name}.${lang}.简体中英双语.srt`,
          ProviderName: 'OpenSubtitles (Demo)',
          Language: lang,
          ThreeLetterISOLanguageName: lang,
          Format: 'srt',
          CommunityRating: 9.4,
          DownloadCount: 1280,
          IsHashMatch: true,
        },
        {
          Id: `sub-remote-${itemId}-${lang}-2`,
          Name: `${item.Name}.${lang}.特效字幕.ass`,
          ProviderName: 'Assrt (Demo)',
          Language: lang,
          ThreeLetterISOLanguageName: lang,
          Format: 'ass',
          CommunityRating: 9.1,
          DownloadCount: 640,
          IsHashMatch: false,
        },
      ]
      return results as T
    }
    if (method === 'POST') {
      requireDemoAdmin(caller)
      const subtitleId = param
      const src = item.MediaSources?.[0]
      if (src) {
        const streams = src.MediaStreams || []
        const nextIdx = streams.reduce((max, s) => Math.max(max, s.Index), -1) + 1
        streams.push({
          Type: 'Subtitle',
          Index: nextIdx,
          Codec: 'vtt',
          Language: 'chi',
          DisplayLanguage: '简体中文',
          Title: `远端下载字幕 (${subtitleId})`,
          DisplayTitle: '远端下载字幕',
          SubVariant: 'chs',
          IsDefault: false,
          IsExternal: true,
          DeliveryMethod: 'External',
          DeliveryUrl: makeDemoVttDataUrl(item.Name),
        })
        src.MediaStreams = streams
      }
      pushActivity(`已为「${item.Name}」下载远端字幕`, 'RemoteSubtitleDownloaded', subtitleId, caller.userId)
      return undefined as T
    }
  }

  const remoteSearchMatch = pathname.match(/^\/emby\/Items\/RemoteSearch\/(Movie|Series)$/)
  if (remoteSearchMatch && method === 'POST') {
    requireDemoAdmin(caller)
    const itemType = remoteSearchMatch[1]
    const body = parseJsonBody(options)
    const searchInfo = (body.SearchInfo && typeof body.SearchInfo === 'object'
      ? (body.SearchInfo as Record<string, unknown>)
      : body) as {
      Name?: string
      Year?: number
      ProviderIds?: Record<string, string>
    }
    const qName = String(searchInfo.Name || '').trim()
    const qYear = searchInfo.Year ? Number(searchInfo.Year) : undefined
    const qTmdb = searchInfo.ProviderIds?.Tmdb?.trim()
    const qImdb = searchInfo.ProviderIds?.Imdb?.trim()

    const pool = Array.from(store.items.values()).filter((it) =>
      itemType === 'Series' ? it.Type === 'Series' : it.Type === 'Movie',
    )

    const scored = pool
      .map((it) => {
        let score = 0
        if (qTmdb && it.ProviderIds?.Tmdb === qTmdb) score += 100
        if (qImdb && it.ProviderIds?.Imdb === qImdb) score += 100
        if (qName && matchesFuzzySearch(it, qName)) score += 50
        if (qYear && it.ProductionYear === qYear) score += 20
        return { item: it, score }
      })
      .sort((a, b) => b.score - a.score)

    const topItems =
      scored.filter((s) => s.score > 0).length > 0
        ? scored
            .filter((s) => s.score > 0)
            .slice(0, 5)
            .map((s) => s.item)
        : pool.slice(0, 4)

    const results: RemoteSearchResult[] = topItems.map((it) => {
      const art = store.artById.get(it.Id)
      return {
        Name: it.Name,
        ProductionYear: it.ProductionYear,
        PremiereDate: it.PremiereDate,
        ImageUrl: art?.poster,
        BackdropUrl: art?.backdrop,
        SearchProviderName: 'TheMovieDb',
        Overview: it.Overview,
        ProviderIds: { ...(it.ProviderIds || {}) },
      }
    })

    if (qName && !results.some((r) => r.Name.toLowerCase() === qName.toLowerCase())) {
      const donor = topItems[0] || pool[0]
      const donorArt = donor ? store.artById.get(donor.Id) : undefined
      results.unshift({
        Name: qName,
        ProductionYear: qYear || 2024,
        ImageUrl: donorArt?.poster,
        BackdropUrl: donorArt?.backdrop,
        SearchProviderName: 'TheMovieDb',
        Overview: `来自 TMDB 的「${qName}」高清元数据匹配结果，支持一键写入片名、年份、简介、外部 ID 与官方海报/剧照。`,
        ProviderIds: {
          Tmdb: qTmdb || '990250',
          ...(qImdb ? { Imdb: qImdb } : { Imdb: 'tt9900250' }),
        },
      })
    }

    return results as T
  }

  const remoteApplyMatch = pathname.match(/^\/emby\/Items\/RemoteSearch\/Apply\/([^/]+)$/)
  if (remoteApplyMatch && method === 'POST') {
    requireDemoAdmin(caller)
    const itemId = decodeURIComponent(remoteApplyMatch[1])
    const item = store.items.get(itemId)
    if (!item) throw new ApiError(404, '项目不存在')
    const body = parseJsonBody(options) as unknown as RemoteSearchResult
    const replaceAllImages = qs.get('ReplaceAllImages') !== 'false'

    if (body.Name) {
      item.Name = body.Name
      item.SortName = body.Name.toLowerCase()
    }
    if (body.ProductionYear !== undefined && body.ProductionYear !== null) {
      item.ProductionYear = Number(body.ProductionYear)
    }
    if (body.PremiereDate) {
      item.PremiereDate = body.PremiereDate
    }
    if (body.Overview) {
      item.Overview = body.Overview
    }
    if (body.ProviderIds && Object.keys(body.ProviderIds).length > 0) {
      item.ProviderIds = { ...(item.ProviderIds || {}), ...body.ProviderIds }
    }
    if (replaceAllImages && (body.ImageUrl || body.BackdropUrl)) {
      const prevArt = store.artById.get(itemId) || { poster: '', backdrop: '' }
      const nextTag = `demo-id-${Date.now().toString(36)}`
      store.artById.set(itemId, {
        ...prevArt,
        poster: body.ImageUrl || prevArt.poster,
        backdrop: body.BackdropUrl || prevArt.backdrop,
      })
      if (body.ImageUrl) {
        item.ImageTags = { ...(item.ImageTags || {}), Primary: nextTag }
      }
      if (body.BackdropUrl) {
        item.BackdropImageTags = [nextTag]
      }
    }
    item.Etag = `etag-${Date.now().toString(36)}`
    pushActivity(`已将远端识别结果应用于「${item.Name}」`, 'ItemIdentified', undefined, caller.userId)
    return undefined as T
  }

  const itemThemeSongsMatch = pathname.match(/^\/emby\/Items\/([^/]+)\/ThemeSongs$/)
  if (itemThemeSongsMatch && method === 'GET') {
    const itemId = decodeURIComponent(itemThemeSongsMatch[1])
    const item = store.items.get(itemId)
    if (!item || !canDemoUserAccessItem(caller.user, item)) {
      return { Items: [], TotalRecordCount: 0 } as T
    }
    const themeSong: BaseItemDto = {
      Id: `theme-${item.Id}`,
      Name: `${item.Name} — 原声主题曲`,
      ServerId: DEMO_SERVER_ID,
      Type: 'Audio',
      MediaType: 'Audio',
      Album: `${item.Name} (Original Soundtrack)`,
      Artists: ['Ambra Demo Orchestra'],
      RunTimeTicks: 200_000_000,
      ParentId: item.Id,
    }
    return { Items: [themeSong], TotalRecordCount: 1 } as T
  }

  const itemInstantMixMatch = pathname.match(/^\/emby\/Items\/([^/]+)\/InstantMix$/)
  if (itemInstantMixMatch && method === 'GET') {
    const itemId = decodeURIComponent(itemInstantMixMatch[1])
    const qUserId = qs.get('UserId') || caller.userId
    if (qs.get('UserId')) requireSelfOrAdmin(caller, qUserId)
    const targetUser = resolveUserDto(qUserId, caller)
    const limit = Math.max(1, Number(qs.get('Limit') || 50))
    const seed = store.items.get(itemId)
    const audioItems = Array.from(store.items.values())
      .filter((it) => it.Type === 'Audio' || it.MediaType === 'Audio')
      .filter((it) => canDemoUserAccessItem(targetUser, it))
      .map((it) => projectItemForUser(it, qUserId))
    const ordered = [...audioItems].sort((a, b) => {
      const aSeed = a.Id === itemId ? -1 : 0
      const bSeed = b.Id === itemId ? -1 : 0
      if (aSeed !== bSeed) return aSeed - bSeed
      const aSameArtist =
        seed?.Artists?.length && a.Artists?.some((ar) => seed.Artists?.includes(ar)) ? -1 : 0
      const bSameArtist =
        seed?.Artists?.length && b.Artists?.some((ar) => seed.Artists?.includes(ar)) ? -1 : 0
      return aSameArtist - bSameArtist
    })
    const sliced = ordered.slice(0, limit)
    return { Items: sliced, TotalRecordCount: ordered.length } as T
  }

  const itemRefreshMatch = pathname.match(/^\/emby\/Items\/([^/]+)\/Refresh$/)
  if (itemRefreshMatch && method === 'POST') {
    requireDemoAdmin(caller)
    const itemId = decodeURIComponent(itemRefreshMatch[1])
    const item = store.items.get(itemId)
    if (!item) throw new ApiError(404, '项目不存在')
    item.Etag = `etag-${Date.now().toString(36)}`
    pushActivity(`已刷新「${item.Name}」的元数据`, 'ItemRefreshed', undefined, caller.userId)
    return undefined as T
  }

  const itemImagesListMatch = pathname.match(/^\/emby\/Items\/([^/]+)\/Images$/)
  if (itemImagesListMatch && method === 'GET') {
    const itemId = decodeURIComponent(itemImagesListMatch[1])
    const item = store.items.get(itemId)
    if (!item) throw new ApiError(404, '项目不存在')
    const art = store.artById.get(itemId)
    const list: ItemImageInfo[] = []
    if (item.ImageTags?.Primary && art?.poster) {
      list.push({
        ImageType: 'Primary',
        ImageIndex: 0,
        Filename: 'poster.jpg',
        Width: 1000,
        Height: 1500,
        Size: 286_400,
        ImageTag: item.ImageTags.Primary,
      })
    }
    if (item.BackdropImageTags && item.BackdropImageTags.length > 0 && art?.backdrop) {
      list.push({
        ImageType: 'Backdrop',
        ImageIndex: 0,
        Filename: 'backdrop.jpg',
        Width: 1920,
        Height: 1080,
        Size: 542_800,
        ImageTag: item.BackdropImageTags[0],
      })
    }
    return list as T
  }

  const remoteImagesDownloadMatch = pathname.match(
    /^\/emby\/Items\/([^/]+)\/RemoteImages\/Download$/,
  )
  if (remoteImagesDownloadMatch && method === 'POST') {
    requireDemoAdmin(caller)
    const itemId = decodeURIComponent(remoteImagesDownloadMatch[1])
    const item = store.items.get(itemId)
    if (!item) throw new ApiError(404, '项目不存在')
    const imgType = qs.get('Type') || 'Primary'
    const imageUrl = qs.get('ImageUrl') || ''
    if (!imageUrl) throw new ApiError(400, 'ImageUrl 不能为空')

    const prevArt = store.artById.get(itemId) || { poster: '', backdrop: '' }
    const nextTag = `demo-img-${Date.now().toString(36)}`
    if (imgType.toLowerCase() === 'backdrop') {
      store.artById.set(itemId, { ...prevArt, backdrop: imageUrl })
      item.BackdropImageTags = [nextTag]
    } else {
      store.artById.set(itemId, { ...prevArt, poster: imageUrl })
      item.ImageTags = { ...(item.ImageTags || {}), Primary: nextTag }
    }
    pushActivity(`已为「${item.Name}」替换远端 ${imgType} 图片`, 'ItemImageUpdated', undefined, caller.userId)
    return undefined as T
  }

  const remoteImagesMatch = pathname.match(/^\/emby\/Items\/([^/]+)\/RemoteImages$/)
  if (remoteImagesMatch && method === 'GET') {
    requireDemoAdmin(caller)
    const itemId = decodeURIComponent(remoteImagesMatch[1])
    const item = store.items.get(itemId)
    if (!item) throw new ApiError(404, '项目不存在')
    const filterType = qs.get('Type') || ''

    const images: RemoteImageInfo[] = []
    const seenUrls = new Set<string>()

    const candidateIds = [
      itemId,
      ...Array.from(store.items.values())
        .filter((it) => it.Id !== itemId && (it.Type === 'Movie' || it.Type === 'Series'))
        .map((it) => it.Id),
    ]

    for (const cid of candidateIds) {
      const art = store.artById.get(cid)
      if (!art) continue
      if (
        (!filterType || filterType.toLowerCase() === 'primary') &&
        art.poster &&
        art.poster.startsWith('http') &&
        !seenUrls.has(art.poster)
      ) {
        seenUrls.add(art.poster)
        images.push({
          ProviderName: 'TheMovieDb',
          Url: art.poster,
          ThumbnailUrl: art.poster,
          Width: 1000,
          Height: 1500,
          CommunityRating: cid === itemId ? 9.2 : 8.6,
          VoteCount: cid === itemId ? 128 : 64,
          Language: cid === itemId ? 'zh' : 'en',
          Type: 'Primary',
        })
      }
      if (
        (!filterType || filterType.toLowerCase() === 'backdrop') &&
        art.backdrop &&
        art.backdrop.startsWith('http') &&
        !seenUrls.has(art.backdrop)
      ) {
        seenUrls.add(art.backdrop)
        images.push({
          ProviderName: 'TheMovieDb',
          Url: art.backdrop,
          ThumbnailUrl: art.backdrop,
          Width: 1920,
          Height: 1080,
          CommunityRating: cid === itemId ? 9.4 : 8.8,
          VoteCount: cid === itemId ? 96 : 48,
          Language: 'null',
          Type: 'Backdrop',
        })
      }
      if (images.length >= 12) break
    }

    const res: RemoteImageResult = {
      Images: images,
      TotalRecordCount: images.length,
      Providers: ['TheMovieDb'],
    }
    return res as T
  }

  const imageByUrlMatch = pathname.match(
    /^\/emby\/Items\/([^/]+)\/Images\/([^/]+)(?:\/(\d+))?\/Url$/,
  )
  if (imageByUrlMatch && method === 'POST') {
    requireDemoAdmin(caller)
    const itemId = decodeURIComponent(imageByUrlMatch[1])
    const imgType = decodeURIComponent(imageByUrlMatch[2])
    const item = store.items.get(itemId)
    if (!item) throw new ApiError(404, '项目不存在')
    const body = parseJsonBody(options)
    const targetUrl = String(body.Url || body.url || '').trim()
    if (!targetUrl) throw new ApiError(400, '图片 URL 不能为空')

    const prevArt = store.artById.get(itemId) || { poster: '', backdrop: '' }
    const nextTag = `demo-url-${Date.now().toString(36)}`
    if (imgType.toLowerCase() === 'backdrop') {
      store.artById.set(itemId, { ...prevArt, backdrop: targetUrl })
      item.BackdropImageTags = [nextTag]
    } else {
      store.artById.set(itemId, { ...prevArt, poster: targetUrl })
      item.ImageTags = { ...(item.ImageTags || {}), Primary: nextTag }
    }
    pushActivity(`已通过 URL 更新「${item.Name}」的 ${imgType} 图片`, 'ItemImageUpdated', undefined, caller.userId)
    return undefined as T
  }

  const itemImageDeleteMatch = pathname.match(
    /^\/emby\/Items\/([^/]+)\/Images\/([^/]+)(?:\/(\d+))?$/,
  )
  if (itemImageDeleteMatch && method === 'DELETE') {
    requireDemoAdmin(caller)
    const itemId = decodeURIComponent(itemImageDeleteMatch[1])
    const imgType = decodeURIComponent(itemImageDeleteMatch[2])
    const item = store.items.get(itemId)
    if (!item) throw new ApiError(404, '项目不存在')
    const prevArt = store.artById.get(itemId) || { poster: '', backdrop: '' }
    if (imgType.toLowerCase() === 'backdrop') {
      store.artById.set(itemId, { ...prevArt, backdrop: '' })
      item.BackdropImageTags = []
    } else {
      store.artById.set(itemId, { ...prevArt, poster: '' })
      if (item.ImageTags) {
        const nextTags = { ...item.ImageTags }
        delete nextTags.Primary
        item.ImageTags = nextTags
      }
    }
    pushActivity(`已删除「${item.Name}」的 ${imgType} 图片`, 'ItemImageDeleted', undefined, caller.userId)
    return undefined as T
  }

  const itemImageUploadMatch = pathname.match(
    /^\/emby\/Items\/([^/]+)\/Images\/([^/]+)(?:\/(\d+))?$/,
  )
  if (itemImageUploadMatch && method === 'POST') {
    requireDemoAdmin(caller)
    const itemId = decodeURIComponent(itemImageUploadMatch[1])
    const imgType = decodeURIComponent(itemImageUploadMatch[2])
    const item = store.items.get(itemId)
    if (!item) throw new ApiError(404, '项目不存在')

    const rawBody = typeof options.body === 'string' ? options.body.trim() : ''
    if (!rawBody) throw new ApiError(400, '上传图片内容为空')
    const hdrs = new Headers(options.headers || {})
    const contentType = hdrs.get('Content-Type') || 'image/jpeg'
    const dataUrl = rawBody.startsWith('data:')
      ? rawBody
      : `data:${contentType};base64,${rawBody}`

    const prevArt = store.artById.get(itemId) || { poster: '', backdrop: '' }
    const nextTag = `demo-up-${Date.now().toString(36)}`
    if (imgType.toLowerCase() === 'backdrop') {
      store.artById.set(itemId, { ...prevArt, backdrop: dataUrl })
      item.BackdropImageTags = [nextTag]
    } else {
      store.artById.set(itemId, { ...prevArt, poster: dataUrl })
      item.ImageTags = { ...(item.ImageTags || {}), Primary: nextTag }
    }
    pushActivity(`已上传「${item.Name}」的 ${imgType} 图片`, 'ItemImageUploaded', undefined, caller.userId)
    return undefined as T
  }

  const uploadSubtitleMatch = pathname.match(/^\/emby\/Videos\/([^/]+)\/Subtitles$/)
  if (uploadSubtitleMatch && method === 'POST') {
    requireDemoAdmin(caller)
    const itemId = decodeURIComponent(uploadSubtitleMatch[1])
    const item = store.items.get(itemId)
    if (!item) throw new ApiError(404, '项目不存在')
    const body = parseJsonBody(options)
    const language = String(body.Language || 'chi').trim() || 'chi'
    const format = String(body.Format || 'srt').trim().toLowerCase() || 'srt'
    const isForced = Boolean(body.IsForced)
    const src = item.MediaSources?.[0]
    if (src) {
      const streams = src.MediaStreams || []
      const nextIdx = streams.reduce((max, s) => Math.max(max, s.Index), -1) + 1
      streams.push({
        Type: 'Subtitle',
        Index: nextIdx,
        Codec: format,
        Language: language,
        DisplayLanguage: language,
        Title: `外挂上传字幕 (${language.toUpperCase()} · ${format.toUpperCase()})`,
        DisplayTitle: `${language.toUpperCase()} (${format.toUpperCase()})`,
        IsForced: isForced,
        IsExternal: true,
        DeliveryMethod: 'External',
        DeliveryUrl: `data:text/plain;base64,${String(body.Data || '')}`,
      })
      src.MediaStreams = streams
    }
    pushActivity(
      `已为「${item.Name}」上传外挂字幕 (${format.toUpperCase()})`,
      'SubtitleUploaded',
      undefined,
      caller.userId,
    )
    return undefined as T
  }

  const deleteSubtitleMatch = pathname.match(/^\/emby\/Videos\/([^/]+)\/Subtitles\/(\d+)$/)
  if (deleteSubtitleMatch && method === 'DELETE') {
    requireDemoAdmin(caller)
    const itemId = decodeURIComponent(deleteSubtitleMatch[1])
    const subIdx = Number(deleteSubtitleMatch[2])
    const item = store.items.get(itemId)
    if (!item) throw new ApiError(404, '项目不存在')
    for (const src of item.MediaSources || []) {
      if (src.MediaStreams) {
        src.MediaStreams = src.MediaStreams.filter(
          (s) => !(s.Type === 'Subtitle' && s.Index === subIdx),
        )
      }
    }
    pushActivity(`已删除「${item.Name}」的外挂字幕 #${subIdx}`, 'SubtitleDeleted', undefined, caller.userId)
    return undefined as T
  }

  const splitVersionsMatch = pathname.match(/^\/emby\/Videos\/([^/]+)\/AlternateSources$/)
  if (splitVersionsMatch && method === 'DELETE') {
    requireDemoAdmin(caller)
    const itemId = decodeURIComponent(splitVersionsMatch[1])
    const item = store.items.get(itemId)
    if (!item) throw new ApiError(404, '项目不存在')
    const sources = item.MediaSources || []
    if (sources.length <= 1) return undefined as T

    const [first, ...rest] = sources
    item.MediaSources = [first]
    const parentArt = store.artById.get(itemId)

    for (let i = 0; i < rest.length; i++) {
      const src = rest[i]
      const newId = `${itemId}-split-${i + 1}-${Date.now().toString(36)}`
      const cloned: BaseItemDto = {
        ...item,
        Id: newId,
        Name: `${item.Name} (${src.Name})`,
        SortName: `${item.SortName || item.Name} ${i + 1}`,
        MediaSources: [src],
        DateCreated: new Date().toISOString(),
        ImageTags: item.ImageTags ? { ...item.ImageTags } : undefined,
        BackdropImageTags: item.BackdropImageTags ? [...item.BackdropImageTags] : undefined,
      }
      delete cloned.UserData
      store.items.set(newId, cloned)
      if (parentArt) {
        store.artById.set(newId, { ...parentArt })
      }
      for (const [libId, ids] of store.libraryItems.entries()) {
        if (ids.includes(itemId)) {
          store.libraryItems.set(libId, [...ids, newId])
        }
      }
    }

    pushActivity(
      `已拆分「${item.Name}」的 ${sources.length} 个多版本为独立条目`,
      'ItemVersionsSplit',
      undefined,
      caller.userId,
    )
    return undefined as T
  }

  if (pathname === '/emby/Videos/MergeVersions' && method === 'POST') {
    requireDemoAdmin(caller)
    const ids = (qs.get('Ids') || '')
      .split(',')
      .map((s) => s.trim())
      .filter(Boolean)
    if (ids.length >= 2) {
      const primary = store.items.get(ids[0])
      if (primary) {
        const mergedSources = [...(primary.MediaSources || [])]
        for (const otherId of ids.slice(1)) {
          const other = store.items.get(otherId)
          if (!other) continue
          if (other.MediaSources) mergedSources.push(...other.MediaSources)
          store.items.delete(otherId)
          for (const [libId, libIds] of store.libraryItems.entries()) {
            store.libraryItems.set(
              libId,
              libIds.filter((id) => id !== otherId),
            )
          }
        }
        primary.MediaSources = mergedSources
        pushActivity(
          `已将 ${ids.length} 个媒体合并为多版本「${primary.Name}」`,
          'ItemVersionsMerged',
          undefined,
          caller.userId,
        )
      }
    }
    return undefined as T
  }

  const singleItemDeleteInfoMatch = pathname.match(/^\/emby\/Items\/([^/]+)\/DeleteInfo$/)
  if (singleItemDeleteInfoMatch && method === 'GET') {
    const itemId = decodeURIComponent(singleItemDeleteInfoMatch[1])
    const item = store.items.get(itemId)
    if (!item) throw new ApiError(404, '项目不存在')
    if (!caller.isAdmin) {
      const canUserDelete =
        Boolean(caller.user?.Policy?.EnableContentDeletion) &&
        canDemoUserAccessItem(caller.user, item)
      if (!canUserDelete) {
        throw new ApiError(403, '无权查看删除信息（需要管理员或 EnableContentDeletion 权限）')
      }
    }

    const paths: string[] = []
    const pushMediaWithSidecars = (target: BaseItemDto) => {
      const primaryPath =
        target.Path ||
        target.MediaSources?.[0]?.Path ||
        `/media/${target.Type.toLowerCase()}/${target.Name}.mkv`
      if (primaryPath && !paths.includes(primaryPath)) {
        paths.push(primaryPath)
      }
      if (target.Type !== 'Series' && target.Type !== 'Season' && target.Type !== 'MusicAlbum') {
        const baseNoExt = primaryPath.replace(/\.[^/.]+$/, '')
        const nfoPath = `${baseNoExt}.nfo`
        if (!paths.includes(nfoPath)) {
          paths.push(nfoPath)
        }
        const extSubs = (target.MediaSources?.[0]?.MediaStreams || []).filter(
          (s) => s.Type === 'Subtitle' && s.IsExternal,
        )
        for (const sub of extSubs) {
          const subExt = (sub.Codec || 'srt').toLowerCase()
          const subLang = sub.Language || 'zh'
          const subPath = `${baseNoExt}.${subLang}.${subExt}`
          if (!paths.includes(subPath)) {
            paths.push(subPath)
          }
        }
      }
    }

    if (item.Type === 'Series') {
      const seriesRoot = item.Path || `/media/tvshows/${item.Name}`
      paths.push(`${seriesRoot}/tvshow.nfo`, `${seriesRoot}/poster.jpg`)
      const seasons = store.seasonsBySeries.get(itemId) || []
      for (const s of seasons) {
        const eps = store.episodesBySeason.get(s.Id) || []
        for (const ep of eps) {
          pushMediaWithSidecars(ep)
        }
      }
    } else if (item.Type === 'Season') {
      if (item.Path) {
        paths.push(`${item.Path}/season.nfo`)
      }
      const eps = store.episodesBySeason.get(itemId) || []
      for (const ep of eps) {
        pushMediaWithSidecars(ep)
      }
    } else {
      pushMediaWithSidecars(item)
    }

    return { Paths: paths } as T
  }

  const singleItemDeletePostMatch = pathname.match(/^\/emby\/Items\/([^/]+)\/Delete$/)
  const singleItemMutationMatch = pathname.match(/^\/emby\/Items\/([^/]+)$/)
  if (singleItemMutationMatch && method === 'POST') {
    requireDemoAdmin(caller)
    const itemId = decodeURIComponent(singleItemMutationMatch[1])
    const item = store.items.get(itemId)
    if (!item) throw new ApiError(404, '项目不存在')
    const body = parseJsonBody(options)

    if (typeof body.Name === 'string' && body.Name.trim()) {
      item.Name = body.Name.trim()
    }
    if (typeof body.OriginalTitle === 'string') {
      item.OriginalTitle = body.OriginalTitle
    }
    if (typeof body.SortName === 'string') {
      item.SortName = body.SortName
    }
    if (body.ProductionYear === null || body.ProductionYear === '' || body.ProductionYear === 0) {
      delete item.ProductionYear
    } else if (body.ProductionYear !== undefined) {
      item.ProductionYear = Number(body.ProductionYear)
    }
    if (body.CommunityRating === null || body.CommunityRating === '') {
      delete item.CommunityRating
    } else if (body.CommunityRating !== undefined) {
      item.CommunityRating = Number(body.CommunityRating)
    }
    if (body.OfficialRating === null) {
      item.OfficialRating = ''
    } else if (typeof body.OfficialRating === 'string') {
      item.OfficialRating = body.OfficialRating
    }
    if (typeof body.Overview === 'string') {
      item.Overview = body.Overview
    }
    if (Array.isArray(body.Genres)) {
      item.Genres = body.Genres.map((g) => String(g).trim()).filter(Boolean)
    }
    if (Array.isArray(body.Tags)) {
      item.Tags = body.Tags.map((t) => String(t).trim()).filter(Boolean)
    }
    if (body.ProviderIds && typeof body.ProviderIds === 'object') {
      const nextProviders: Record<string, string> = {}
      for (const [k, v] of Object.entries(body.ProviderIds as Record<string, unknown>)) {
        const val = String(v ?? '').trim()
        if (val) nextProviders[k] = val
      }
      item.ProviderIds = nextProviders
    }
    if (typeof body.LockData === 'boolean') {
      item.LockData = body.LockData
    }
    if (Array.isArray(body.LockedFields)) {
      item.LockedFields = body.LockedFields.map((f) => String(f).trim()).filter(Boolean)
    }
    item.Etag = `etag-${Date.now().toString(36)}`
    pushActivity(`已更新「${item.Name}」的元数据`, 'ItemMetadataUpdated', undefined, caller.userId)
    return undefined as T
  }

  if (
    (singleItemMutationMatch && method === 'DELETE') ||
    (singleItemDeletePostMatch && method === 'POST')
  ) {
    const rawId = singleItemMutationMatch
      ? singleItemMutationMatch[1]
      : singleItemDeletePostMatch![1]
    const itemId = decodeURIComponent(rawId)
    const item = store.items.get(itemId)
    if (!caller.isAdmin) {
      const canUserDelete =
        Boolean(caller.user?.Policy?.EnableContentDeletion) &&
        (!item || canDemoUserAccessItem(caller.user, item))
      if (!canUserDelete) {
        throw new ApiError(403, '无权删除媒体内容（需要管理员或 EnableContentDeletion 权限）')
      }
    }
    if (item) {
      store.items.delete(itemId)
      store.artById.delete(itemId)
      const seasons = store.seasonsBySeries.get(itemId) || []
      for (const s of seasons) {
        const eps = store.episodesBySeason.get(s.Id) || []
        for (const ep of eps) {
          store.items.delete(ep.Id)
          store.artById.delete(ep.Id)
        }
        store.episodesBySeason.delete(s.Id)
        store.items.delete(s.Id)
      }
      store.seasonsBySeries.delete(itemId)
      for (const [libId, libIds] of store.libraryItems.entries()) {
        store.libraryItems.set(
          libId,
          libIds.filter((id) => id !== itemId),
        )
      }
      pushActivity(`已删除媒体「${item.Name}」`, 'ItemDeleted', undefined, caller.userId)
    }
    return undefined as T
  }

  // ==================== 14.5 数据库备份与恢复 (BackupRestore) ====================
  if (pathname === '/emby/BackupRestore/BackupInfo' && method === 'GET') {
    requireDemoAdmin(caller)
    const res: BackupInfoResponse = {
      BackupDirectory: '/data/config/backups',
      Backups: demoBackups.map((b) => ({ ...b })),
    }
    return res as T
  }

  if (pathname === '/emby/BackupRestore/Backup' && method === 'POST') {
    requireDemoAdmin(caller)
    const now = new Date()
    const pad = (n: number) => String(n).padStart(2, '0')
    const stamp = `${now.getUTCFullYear()}${pad(now.getUTCMonth() + 1)}${pad(now.getUTCDate())}-${pad(now.getUTCHours())}${pad(now.getUTCMinutes())}${pad(now.getUTCSeconds())}`
    const name = `ambra-backup-${stamp}-${Math.random().toString(36).slice(2, 5)}.zip`
    const created: BackupFileInfo = {
      Name: name,
      Path: `/data/config/backups/${name}`,
      Size: 2_684_354,
      DateCreated: now.toISOString(),
    }
    demoBackups.unshift(created)
    pushActivity(`已创建数据库热备份「${name}」`, 'BackupCreated', undefined, caller.userId)
    return { ...created } as T
  }

  if (pathname === '/emby/BackupRestore/Restore' && method === 'POST') {
    requireDemoAdmin(caller)
    const body = parseJsonBody(options)
    const name = String(body.Name || '').trim()
    if (!name) throw new ApiError(400, '请指定要恢复的备份文件名')
    const exists = demoBackups.find((b) => b.Name === name)
    if (!exists) throw new ApiError(404, `备份文件「${name}」不存在`)
    pushActivity(`已暂存数据库备份恢复「${name}」（重启后生效）`, 'BackupRestoreStaged', undefined, caller.userId)
    const res: BackupRestoreResult = {
      Restored: true,
      BackupName: name,
      StagedPath: '/data/ambra.db.restore',
      RequiresRestart: true,
    }
    return res as T
  }

  // ==================== 14.6 应用内通知中心 (Notifications) ====================
  if (pathname === '/emby/Notifications/Admin' && method === 'POST') {
    requireDemoAdmin(caller)
    const body = parseJsonBody(options)
    const name = String(body.Name || '').trim()
    if (!name) {
      throw new ApiError(400, '通知标题不能为空')
    }
    const description = typeof body.Description === 'string' ? body.Description.trim() : undefined
    const targetUrl = typeof body.Url === 'string' && body.Url.trim() ? body.Url.trim() : undefined
    const level =
      body.Level === 'Warning' || body.Level === 'Error' || body.Level === 'Normal'
        ? body.Level
        : 'Normal'
    const targetUserId =
      typeof body.UserId === 'string' && body.UserId.trim() ? body.UserId.trim() : undefined

    const record: DemoNotificationRecord = {
      Id: `notif-demo-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 6)}`,
      UserId: targetUserId,
      Date: new Date().toISOString(),
      Name: name,
      Description: description,
      Url: targetUrl,
      Level: level,
      readByUsers: new Set(),
    }
    demoNotifications.unshift(record)
    pushActivity(`管理员发送了系统通知「${name}」`, 'NotificationSent', description, caller.userId)

    const item: NotificationItem = {
      Id: record.Id,
      UserId: record.UserId,
      Date: record.Date,
      IsRead: false,
      Name: record.Name,
      Description: record.Description,
      Url: record.Url,
      Level: record.Level,
    }
    return item as T
  }

  const notifSummaryMatch = pathname.match(/^\/emby\/Notifications\/([^/]+)\/Summary$/)
  if (notifSummaryMatch && method === 'GET') {
    const userId = decodeURIComponent(notifSummaryMatch[1])
    requireSelfOrAdmin(caller, userId)
    const visible = demoNotifications.filter((n) => !n.UserId || n.UserId === userId)
    const unread = visible.filter((n) => !n.defaultRead && !n.readByUsers.has(userId))
    let maxLevel: 'Normal' | 'Warning' | 'Error' = 'Normal'
    if (unread.some((n) => n.Level === 'Error')) {
      maxLevel = 'Error'
    } else if (unread.some((n) => n.Level === 'Warning')) {
      maxLevel = 'Warning'
    }
    const summary: NotificationsSummary = {
      UnreadCount: unread.length,
      MaxUnreadNotificationLevel: maxLevel,
    }
    return summary as T
  }

  const notifReadMatch = pathname.match(/^\/emby\/Notifications\/([^/]+)\/Read$/)
  if (notifReadMatch && method === 'POST') {
    const userId = decodeURIComponent(notifReadMatch[1])
    requireSelfOrAdmin(caller, userId)
    const body = parseJsonBody(options)
    const qsIds = (qs.get('Ids') || '')
      .split(',')
      .map((s) => s.trim())
      .filter(Boolean)
    const bodyIds = Array.isArray(body.Ids)
      ? body.Ids.map((id) => String(id).trim()).filter(Boolean)
      : []
    const targetIds = new Set([...qsIds, ...bodyIds])

    for (const notif of demoNotifications) {
      if (notif.UserId && notif.UserId !== userId) continue
      if (targetIds.size === 0 || targetIds.has(notif.Id)) {
        notif.readByUsers.add(userId)
      }
    }
    return undefined as T
  }

  const notifListMatch = pathname.match(/^\/emby\/Notifications\/([^/]+)$/)
  if (notifListMatch && method === 'GET') {
    const userId = decodeURIComponent(notifListMatch[1])
    requireSelfOrAdmin(caller, userId)
    const startIndex = Math.max(0, Number(qs.get('StartIndex') || '0') || 0)
    const limit = Math.max(1, Number(qs.get('Limit') || '20') || 20)
    const visible = demoNotifications.filter((n) => !n.UserId || n.UserId === userId)
    const paged: NotificationItem[] = visible.slice(startIndex, startIndex + limit).map((n) => ({
      Id: n.Id,
      UserId: n.UserId,
      Date: n.Date,
      IsRead: Boolean(n.defaultRead || n.readByUsers.has(userId)),
      Name: n.Name,
      Description: n.Description,
      Url: n.Url,
      Level: n.Level,
    }))
    const res: NotificationResult = {
      Notifications: paged,
      TotalRecordCount: visible.length,
    }
    return res as T
  }

  // ==================== 14.7 Quick Connect 设备授权码登录 ====================
  if (pathname === '/emby/QuickConnect/Enabled' && method === 'GET') {
    return (demoServerConfig.EnableQuickConnect !== false) as T
  }

  if (pathname === '/emby/QuickConnect/Initiate' && method === 'POST') {
    if (demoServerConfig.EnableQuickConnect === false) {
      throw new ApiError(403, 'Quick Connect 已在服务器设置中禁用')
    }
    const code = String(Math.floor(100000 + Math.random() * 900000))
    const secret = `qc-demo-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`
    const session: DemoQuickConnectSession = {
      Authenticated: false,
      Secret: secret,
      Code: code,
      DeviceId: 'dev-demo-web-01',
      DeviceName: 'Ambra Web Demo',
      AppName: 'Ambra Web',
      AppVersion: '0.12.3',
      DateAdded: new Date().toISOString(),
    }
    demoQuickConnectSessions.set(secret, session)
    const { AuthorizedUserId: _ignored, ...pub } = session
    return pub as T
  }

  if (pathname === '/emby/QuickConnect/Connect' && method === 'GET') {
    const secret = (qs.get('secret') || qs.get('Secret') || '').trim()
    const session = demoQuickConnectSessions.get(secret)
    if (!session) {
      throw new ApiError(404, 'Quick Connect 会话不存在或已过期')
    }
    const demoApprove = qs.get('demoApprove')
    if (demoApprove === '1' || demoApprove === 'true') {
      session.Authenticated = true
      session.AuthorizedUserId = caller.userId || store.users[0]?.user.Id || 'user-demo-admin'
    }
    const { AuthorizedUserId: _ignored, ...pub } = session
    return pub as T
  }

  if (pathname === '/emby/QuickConnect/Authorize' && method === 'POST') {
    const body = parseJsonBody(options)
    const code = String(
      qs.get('code') || qs.get('Code') || body.Code || body.code || '',
    ).trim()
    if (!code) {
      throw new ApiError(400, '请输入 6 位 Quick Connect 授权码')
    }
    let matched: DemoQuickConnectSession | undefined
    for (const session of demoQuickConnectSessions.values()) {
      if (session.Code === code) {
        matched = session
        break
      }
    }
    if (matched) {
      matched.Authenticated = true
      matched.AuthorizedUserId = caller.userId || store.users[0]?.user.Id || 'user-demo-admin'
      pushActivity(
        `已通过 Quick Connect 授权新设备登录（授权码 ${code}）`,
        'QuickConnectAuthorized',
        undefined,
        caller.userId,
      )
      return true as T
    }
    if (code === '123456') {
      pushActivity(
        '已通过 Quick Connect 授权演示设备登录（授权码 123456）',
        'QuickConnectAuthorized',
        undefined,
        caller.userId,
      )
      return true as T
    }
    throw new ApiError(404, '授权码无效或已过期，请核对后重试')
  }

  if (pathname === '/emby/Users/AuthenticateWithQuickConnect' && method === 'POST') {
    const body = parseJsonBody(options)
    const secret = String(body.Secret || body.secret || '').trim()
    const session = demoQuickConnectSessions.get(secret)
    if (!session || !session.Authenticated) {
      throw new ApiError(401, 'Quick Connect 尚未完成授权或已过期')
    }
    demoQuickConnectSessions.delete(secret)
    const rec =
      store.users.find((u) => u.user.Id === session.AuthorizedUserId) ||
      store.users.find((u) => u.user.Policy?.IsAdministrator) ||
      store.users[0]
    if (!rec) {
      throw new ApiError(401, 'Demo 用户不存在')
    }
    const now = new Date().toISOString()
    rec.user.LastLoginDate = now
    rec.user.LastActivityDate = now
    const token = `demo_tok_qc_${rec.user.Id}_${Date.now().toString(36)}`
    demoTokenUserMap.set(token, rec.user.Id)
    pushActivity(
      `${rec.user.Name} 通过 Quick Connect 登录了 Ambra Serverless Demo`,
      'AuthenticationSucceeded',
      'Quick Connect 设备授权',
      rec.user.Id,
    )
    const result: AuthenticationResult = {
      User: { ...rec.user, Policy: { ...rec.user.Policy } },
      AccessToken: token,
      ServerId: DEMO_SERVER_ID,
      SessionInfo: {
        Id: `sess-qc-${rec.user.Id}`,
        DeviceId: session.DeviceId || 'dev-demo-web-01',
      },
    }
    return result as T
  }

  // ==================== 15. 单用户查询/删除（放在更具体路由之后） ====================
  const singleUserMatch = pathname.match(/^\/emby\/Users\/([^/]+)$/)
  if (singleUserMatch) {
    const userId = decodeURIComponent(singleUserMatch[1])
    if (method === 'GET') {
      requireSelfOrAdmin(caller, userId)
      const rec = store.users.find((u) => u.user.Id === userId)
      if (!rec) throw new ApiError(404, '用户不存在')
      return { ...rec.user, Policy: { ...rec.user.Policy } } as T
    }
    if (method === 'DELETE') {
      requireDemoAdmin(caller)
      const idx = store.users.findIndex((u) => u.user.Id === userId)
      if (idx >= 0) {
        const [removed] = store.users.splice(idx, 1)
        store.userDataByUser.delete(userId)
        pushActivity(`已删除用户「${removed.user.Name}」`, 'UserDeleted', undefined, caller.userId)
      }
      return undefined as T
    }
  }

  throw new ApiError(404, `[Demo Mock] 未实现的端点: ${method} ${pathname}`)
}

export function getDemoVideoUrl(): string {
  return DEMO_VIDEO_DATA_URL
}

registerDemoServer({
  demoFetch,
  getDemoImageUrl,
  getDemoFallbackImageUrl,
  getDemoUserImageUrl,
  getDemoAudioUrl,
  getDemoVideoUrl,
})
