import { ApiError, getStoredAuth } from '../api/client'
import type {
  ApiKeyInfo,
  AuthenticationResult,
  BackupFileInfo,
  BackupInfoResponse,
  BackupRestoreResult,
  BaseItemDto,
  ItemImageInfo,
  LibraryScanProgress,
  LiveTvChannelInfo,
  LiveTvInfo,
  LiveTvListingProviderInfo,
  LiveTvRecordingInfo,
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
  ScanProgressSnapshot,
  SearchHint,
  SearchHintResult,
  ServerConfiguration,
  SetupStatus,
  TaskTriggerInfo,
  UserDto,
  UserItemDataDto,
  UserPolicy,
  VirtualFolderInfo,
  WebhookInfo,
  WebhookTestResult,
} from '../api/types'
import { DEMO_VIDEO_DATA_URL, makeDemoAudioDataUrl } from './demoMedia'
import { createInitialDemoState, DEMO_SERVER_ID, type DemoState } from './mockData'
import { makeAvatarDataUrl, makeLibraryThumbDataUrl, makePosterDataUrl } from './svgArt'

let store: DemoState = createInitialDemoState()

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
    TMDBApiKey: 'demo-tmdb-key-ambra',
    TMDBLanguage: 'zh-CN',
    EnableSTRM302: true,
    PlayedThreshold: 0.9,
    PreferredMetadataLanguage: 'zh',
    MetadataCountryCode: 'CN',
    UICulture: 'zh-cn',
  }
}

let demoServerConfig: ServerConfiguration = createInitialServerConfig()

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

let demoTunerHosts: LiveTvTunerHostInfo[] = createInitialTunerHosts()
let demoListingProviders: LiveTvListingProviderInfo[] = createInitialListingProviders()
let demoLiveTvChannels: LiveTvChannelInfo[] = createInitialLiveTvChannels()

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

function createInitialDemoWebhooks(): WebhookInfo[] {
  return [
    {
      Id: 'wh-demo-01',
      Name: 'Discord 媒体入库通知',
      Url: 'https://discord.com/api/webhooks/demo/ambra-notify',
      Secret: 'demo-hmac-secret',
      Events: ['LibraryChanged', 'PlaybackStart'],
      Enabled: true,
      CreatedAt: Math.floor(Date.now() / 1000) - 86400,
    },
  ]
}

let demoLiveTvTimers: LiveTvTimerInfo[] = createInitialLiveTvTimers()
let demoLiveTvRecordings: LiveTvRecordingInfo[] = createInitialLiveTvRecordings()
let demoWebhooks: WebhookInfo[] = createInitialDemoWebhooks()

const DEMO_PARENTAL_RATINGS: ParentalRatingOption[] = [
  { Name: 'G', Value: 1 },
  { Name: 'PG', Value: 5 },
  { Name: 'PG-13', Value: 7 },
  { Name: 'R', Value: 9 },
  { Name: 'NC-17', Value: 10 },
]

function createInitialDemoBackups(): BackupFileInfo[] {
  return [
    {
      Name: 'ambra-backup-20261124-030000.db',
      Path: '/data/backups/ambra-backup-20261124-030000.db',
      Size: 2_621_440,
      DateCreated: new Date(Date.now() - 8 * 3600_000).toISOString(),
    },
    {
      Name: 'ambra-backup-20261123-030000.db',
      Path: '/data/backups/ambra-backup-20261123-030000.db',
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
      Name: '欢迎体验 Ambra v0.9.3',
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

/**
 * 重置 Demo 内存态数据（供单元测试与演示重置使用）
 */
export function resetDemoStore(): void {
  store = createInitialDemoState()
  rankingsState = createInitialRankingsState()
  activeDemoScan = null
  demoSetupCompleted = true
  demoDefaultLanguage = 'zh-CN'
  demoServerConfig = createInitialServerConfig()
  demoOrphans = createInitialDemoOrphans()
  demoReviews = createInitialDemoReviews()
  demoTunerHosts = createInitialTunerHosts()
  demoListingProviders = createInitialListingProviders()
  demoLiveTvChannels = createInitialLiveTvChannels()
  demoLiveTvTimers = createInitialLiveTvTimers()
  demoLiveTvRecordings = createInitialLiveTvRecordings()
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

  // 单词直接子串命中
  if (haystack.includes(trimmed)) {
    return true
  }

  // 中文无空格乱序/跳字兜底（如“地球流浪”“诺兰梦”）
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

function pushActivity(name: string, type: string, shortOverview?: string, userId?: string): void {
  store.activityLogs.unshift({
    Id: store.nextActivityId++,
    Name: name,
    ShortOverview: shortOverview,
    Type: type,
    Date: new Date().toISOString(),
    UserId: userId,
    Severity: 'Info',
  })
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

function ensureUserData(item: BaseItemDto): UserItemDataDto {
  if (!item.UserData) {
    item.UserData = {
      PlaybackPositionTicks: 0,
      PlayCount: 0,
      IsFavorite: false,
      Played: false,
    }
  }
  return item.UserData
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

/**
 * 纯前端内存态 Emby Mock 请求引擎。
 * 由 src/api/client.ts 的 embyFetch 在 isDemoMode() === true 时直接调用。
 */
export async function demoFetch<T>(
  path: string,
  options: RequestInit = {},
  requireAuth = true,
): Promise<T> {
  const auth = getStoredAuth()
  if (requireAuth && !auth?.accessToken) {
    throw new ApiError(401, '未登录或登录已过期')
  }

  const method = (options.method || 'GET').toUpperCase()
  const url = new URL(path.startsWith('/') ? path : `/${path}`, 'http://ambra.demo')
  const pathname = url.pathname
  const qs = url.searchParams

  // ==================== 0. 首次运行初始化向导 ====================
  if (pathname === '/api/setup/status' && method === 'GET') {
    const status: SetupStatus = {
      completed: demoSetupCompleted,
      server_name: store.systemInfo.ServerName,
      default_language: demoDefaultLanguage,
      has_users: store.users.length > 0,
      has_tmdb_key: true,
    }
    return status as T
  }

  if (pathname === '/api/setup/admin' && method === 'POST') {
    const body = parseJsonBody(options)
    const serverName = String(body.server_name || '').trim()
    const language = String(body.language || '').trim()
    const username = String(body.username || '').trim()
    const password = String(body.password ?? '')

    if (!username) {
      throw new ApiError(400, '管理员用户名不能为空')
    }
    if (serverName) {
      store.systemInfo.ServerName = serverName
    }
    if (language) {
      demoDefaultLanguage = language
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
    const body = parseJsonBody(options)
    const serverName = String(body.server_name || '').trim()
    const tmdbLanguage = String(body.tmdb_language || '').trim()
    if (serverName) {
      store.systemInfo.ServerName = serverName
    }
    if (tmdbLanguage) {
      demoDefaultLanguage = tmdbLanguage
    }
    demoSetupCompleted = true
    store.systemInfo.StartupWizardCompleted = true
    pushActivity('已完成服务器首次运行初始化向导', 'SetupCompleted')
    return { completed: true } as T
  }

  // ==================== 1. 系统与公开信息 ====================
  if (pathname === '/emby/System/Configuration') {
    if (method === 'GET') {
      return {
        ...demoServerConfig,
        ServerName: store.systemInfo.ServerName,
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
        demoServerConfig.TMDBApiKey = body.TMDBApiKey.trim()
      }
      if (typeof body.TMDBLanguage === 'string' && body.TMDBLanguage.trim()) {
        demoServerConfig.TMDBLanguage = body.TMDBLanguage.trim()
        demoDefaultLanguage = body.TMDBLanguage.trim()
      }
      if (typeof body.EnableSTRM302 === 'boolean') {
        demoServerConfig.EnableSTRM302 = body.EnableSTRM302
      }
      if (typeof body.PlayedThreshold === 'number' && body.PlayedThreshold > 0 && body.PlayedThreshold <= 1) {
        demoServerConfig.PlayedThreshold = body.PlayedThreshold
      }
      pushActivity('已更新服务器常规设置', 'ServerConfigUpdated')
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
    return { ...store.systemInfo } as T
  }

  if (pathname === '/emby/Users/Public' && method === 'GET') {
    return store.users
      .filter((u) => !u.user.Policy?.IsDisabled && !u.user.Policy?.IsHidden)
      .map((u) => ({ ...u.user })) as T
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
    return undefined as T
  }

  // 忘记密码 & PIN 重置（Demo 模式：绝不将 PIN 写入文件名）
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
    if (pin !== '123456' && !/^\d{6}$/.test(pin)) {
      throw new ApiError(400, 'PIN 验证码无效，请输入 6 位数字（Demo 可使用 123456）')
    }
    pushActivity('已通过 PIN 码完成密码重置（Demo 模式）', 'ForgotPasswordReset')
    return {
      Success: true,
      UsersReset: ['admin'],
    } as T
  }

  // ==================== 3. 用户与权限管理 ====================
  if (pathname === '/emby/Users' && method === 'GET') {
    return store.users.map((u) => ({ ...u.user, Policy: { ...u.user.Policy } })) as T
  }

  if (pathname === '/emby/Users/New' && method === 'POST') {
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
    store.userAvatarById.set(id, makeAvatarDataUrl(name, ['#7c3aed', '#2e1065']))
    pushActivity(`管理员创建了新用户「${name}」`, 'UserCreated', isAdmin ? '管理员权限' : '普通用户')
    return { ...newUser } as T
  }

  const userPolicyMatch = pathname.match(/^\/emby\/Users\/([^/]+)\/Policy$/)
  if (userPolicyMatch && method === 'POST') {
    const userId = decodeURIComponent(userPolicyMatch[1])
    const rec = store.users.find((u) => u.user.Id === userId)
    if (!rec) throw new ApiError(404, '用户不存在')
    const patch = parseJsonBody(options) as UserPolicy
    const nextPolicy: UserPolicy = { ...rec.user.Policy, ...patch }
    if (
      patch.MaxParentalRating === null ||
      (patch.MaxParentalRating === undefined && Array.isArray(patch.BlockedTags))
    ) {
      delete nextPolicy.MaxParentalRating
    }
    rec.user.Policy = nextPolicy
    pushActivity(`已更新用户「${rec.user.Name}」的权限策略`, 'UserPolicyUpdated')
    return undefined as T
  }

  const userPasswordMatch = pathname.match(/^\/emby\/Users\/([^/]+)\/Password$/)
  if (userPasswordMatch && method === 'POST') {
    const userId = decodeURIComponent(userPasswordMatch[1])
    const rec = store.users.find((u) => u.user.Id === userId)
    if (!rec) throw new ApiError(404, '用户不存在')
    const body = parseJsonBody(options)
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
    pushActivity(`已更新用户「${rec.user.Name}」的登录密码`, 'UserPasswordChanged')
    return undefined as T
  }

  const userImageMatch = pathname.match(/^\/emby\/Users\/([^/]+)\/Images\/Primary(?:\/0)?$/)
  if (userImageMatch) {
    const userId = decodeURIComponent(userImageMatch[1])
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
      pushActivity(`用户「${rec.user.Name}」更新了个人头像`, 'UserImageUpdated')
      return undefined as T
    }
    if (method === 'DELETE') {
      store.userAvatarById.delete(userId)
      delete rec.user.PrimaryImageTag
      pushActivity(`用户「${rec.user.Name}」移除了个人头像`, 'UserImageDeleted')
      return undefined as T
    }
  }

  // ==================== 4. 媒体视图 / 首页 / 列表 / 详情 ====================
  const userViewsMatch = pathname.match(/^\/emby\/Users\/([^/]+)\/Views$/)
  if (userViewsMatch && method === 'GET') {
    const userId = decodeURIComponent(userViewsMatch[1])
    const visible = getVisibleLibrariesForUser(userId).map(libraryToViewDto)
    const res: QueryResult<BaseItemDto> = {
      Items: visible,
      TotalRecordCount: visible.length,
    }
    return res as T
  }

  const userResumeMatch = pathname.match(/^\/emby\/Users\/([^/]+)\/Items\/Resume$/)
  if (userResumeMatch && method === 'GET') {
    const limit = Number(qs.get('Limit') || 12)
    const resumable = Array.from(store.items.values())
      .filter(
        (it) =>
          (it.Type === 'Movie' || it.Type === 'Episode') &&
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
    const parentId = qs.get('ParentId') || ''
    const limit = Number(qs.get('Limit') || 16)
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
    return sortItems(list, 'DateCreated', 'Descending').slice(0, limit) as T
  }

  if ((pathname === '/emby/Tags' || pathname === '/Tags') && method === 'GET') {
    const parentId = qs.get('ParentId') || ''
    const pool =
      parentId && store.libraryItems.has(parentId)
        ? (store.libraryItems.get(parentId) || [])
            .map((id) => store.items.get(id))
            .filter((it): it is BaseItemDto => Boolean(it))
        : Array.from(store.items.values())

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

  if ((pathname === '/emby/Genres' || pathname === '/Genres') && method === 'GET') {
    const parentId = qs.get('ParentId') || ''
    const pool =
      parentId && store.libraryItems.has(parentId)
        ? (store.libraryItems.get(parentId) || [])
            .map((id) => store.items.get(id))
            .filter((it): it is BaseItemDto => Boolean(it))
        : Array.from(store.items.values())

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

  if ((pathname === '/emby/Years' || pathname === '/Years') && method === 'GET') {
    const parentId = qs.get('ParentId') || ''
    const pool =
      parentId && store.libraryItems.has(parentId)
        ? (store.libraryItems.get(parentId) || [])
            .map((id) => store.items.get(id))
            .filter((it): it is BaseItemDto => Boolean(it))
        : Array.from(store.items.values())

    const yearSet = new Set<number>()
    for (const it of pool) {
      if (it.ProductionYear) yearSet.add(it.ProductionYear)
    }
    const items = Array.from(yearSet)
      .sort((a, b) => b - a)
      .map((y) => ({ Name: String(y), Id: `year-${y}` }))
    return { Items: items, TotalRecordCount: items.length } as T
  }

  const userItemsMatch = pathname.match(/^\/emby\/Users\/([^/]+)\/Items$/)
  const isGlobalItemsQuery = pathname === '/emby/Items' || pathname === '/Items'
  if ((userItemsMatch || isGlobalItemsQuery) && method === 'GET') {
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

    let pool: BaseItemDto[]
    if (parentId && store.libraryItems.has(parentId)) {
      pool = (store.libraryItems.get(parentId) || [])
        .map((id) => store.items.get(id))
        .filter((it): it is BaseItemDto => Boolean(it))
    } else if (parentId) {
      pool = []
    } else {
      pool = Array.from(store.items.values())
    }

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
      pool = pool.filter((it) => it.ProductionYear != null && yearsParam.includes(String(it.ProductionYear)))
    }
    if (tagsParam.length > 0) {
      pool = pool.filter((it) =>
        (it.Tags || []).some((t) => tagsParam.includes(t.toLowerCase())),
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
        const sortKey = (it.SortName || it.Name).trim().toLowerCase()
        const nameKey = it.Name.trim().toLowerCase()
        return sortKey.startsWith(nameStartsWith) || nameKey.startsWith(nameStartsWith)
      })
    }
    if (nameStartsWithOrGreater) {
      pool = pool.filter((it) => {
        const sortKey = (it.SortName || it.Name).trim().toLowerCase()
        return sortKey >= nameStartsWithOrGreater
      })
    }
    if (nameLessThan) {
      pool = pool.filter((it) => {
        const sortKey = (it.SortName || it.Name).trim().toLowerCase()
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
    const itemId = decodeURIComponent(userSingleItemMatch[2])
    const item = store.items.get(itemId)
    if (item) return { ...item } as T
    const lib = store.libraries.find((l) => l.ItemId === itemId)
    if (lib) return libraryToViewDto(lib) as T

    // 若 ID 命中任意作品的演职员，则合成 Person DTO 返回
    let matchedPerson: { Id: string; Name: string; Type: string; PrimaryImageTag?: string } | null =
      null
    const knownTitles: string[] = []
    for (const it of store.items.values()) {
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
    const seriesId = qs.get('SeriesId') || ''
    const limit = Number(qs.get('Limit') || 12)
    const targetSeriesIds = seriesId ? [seriesId] : Array.from(store.seasonsBySeries.keys())
    const nextUpList: BaseItemDto[] = []
    for (const sid of targetSeriesIds) {
      const seasons = store.seasonsBySeries.get(sid) || []
      const allEps = seasons.flatMap((s) => store.episodesBySeason.get(s.Id) || [])
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
    const limit = Number(qs.get('Limit') || 12)
    const source = store.items.get(itemId)
    if (!source) return { Items: [], TotalRecordCount: 0 } as T
    const srcGenres = new Set((source.Genres || []).map((g) => g.toLowerCase()))
    const candidates = Array.from(store.items.values())
      .filter((it) => it.Id !== itemId && it.Type === source.Type)
      .map((it) => {
        const overlap = (it.Genres || []).filter((g) => srcGenres.has(g.toLowerCase())).length
        return { item: it, score: overlap * 10 + (it.CommunityRating || 0) }
      })
      .sort((a, b) => b.score - a.score)
      .slice(0, limit)
      .map((x) => x.item)
    return { Items: candidates, TotalRecordCount: candidates.length } as T
  }

  // ==================== 5b. 合集 (Collections) & 播放列表 (Playlists) CRUD ====================
  if (pathname === '/emby/Collections' && method === 'POST') {
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
    pushActivity(`已创建合集「${name}」`, 'CollectionCreated')
    return { Id: id, Name: name } as T
  }

  if (pathname === '/emby/Playlists' && method === 'POST') {
    const body = parseJsonBody(options)
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
    pushActivity(`已创建播放列表「${name}」`, 'PlaylistCreated')
    return { Id: id, Name: name } as T
  }

  const movePlaylistItemMatch = pathname.match(
    /^\/(?:emby\/)?Playlists\/([^/]+)\/Items\/([^/]+)\/Move\/(-?\d+)$/,
  )
  if (movePlaylistItemMatch && method === 'POST') {
    const playlistId = decodeURIComponent(movePlaylistItemMatch[1])
    const itemId = decodeURIComponent(movePlaylistItemMatch[2])
    const newIndexRaw = Number(movePlaylistItemMatch[3])
    const container = store.items.get(playlistId)
    if (!container || container.Type !== 'Playlist') {
      throw new ApiError(404, '播放列表不存在')
    }
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
    const containerId = decodeURIComponent(containerItemsMatch[2])
    const container = store.items.get(containerId)
    if (!container) throw new ApiError(404, '合集或播放列表不存在')
    if (method === 'GET') {
      const ids = store.libraryItems.get(containerId) || []
      const items = ids
        .map((mid) => store.items.get(mid))
        .filter((it): it is BaseItemDto => Boolean(it))
      return { Items: items, TotalRecordCount: items.length } as T
    }
    if (method === 'POST') {
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
    const containerId = decodeURIComponent(deleteContainerMatch[2])
    const container = store.items.get(containerId)
    if (container) {
      store.items.delete(containerId)
      store.libraryItems.delete(containerId)
      pushActivity(`已删除「${container.Name}」`, 'ContainerDeleted')
    }
    return undefined as T
  }

  const seasonsMatch = pathname.match(/^\/emby\/Shows\/([^/]+)\/Seasons$/)
  if (seasonsMatch && method === 'GET') {
    const seriesId = decodeURIComponent(seasonsMatch[1])
    const seasons = store.seasonsBySeries.get(seriesId) || []
    return { Items: seasons, TotalRecordCount: seasons.length } as T
  }

  const episodesMatch = pathname.match(/^\/emby\/Shows\/([^/]+)\/Episodes$/)
  if (episodesMatch && method === 'GET') {
    const seriesId = decodeURIComponent(episodesMatch[1])
    const seasonId = qs.get('SeasonId') || ''
    let eps: BaseItemDto[] = []
    if (seasonId && store.episodesBySeason.has(seasonId)) {
      eps = store.episodesBySeason.get(seasonId) || []
    } else {
      const seasons = store.seasonsBySeries.get(seriesId) || []
      eps = seasons.flatMap((s) => store.episodesBySeason.get(s.Id) || [])
    }
    return { Items: eps, TotalRecordCount: eps.length } as T
  }

  // ==================== 6. 中文乱序模糊搜索 ====================
  if (pathname === '/emby/Search/Hints' && method === 'GET') {
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

  // ==================== 7. 收藏 / 已看 / 从继续观看移除 ====================
  const hideResumeMatch = pathname.match(
    /^\/emby\/Users\/([^/]+)\/Items\/([^/]+)\/HideFromResume$/,
  )
  if (hideResumeMatch && method === 'POST') {
    const itemId = decodeURIComponent(hideResumeMatch[2])
    const item = store.items.get(itemId)
    if (!item) throw new ApiError(404, '项目不存在')
    const ud = ensureUserData(item)
    const hide = qs.get('Hide') !== 'false'
    if (hide) {
      ud.PlaybackPositionTicks = 0
      ud.PlayedPercentage = 0
    }
    return { ...ud } as T
  }

  const favMatch = pathname.match(/^\/emby\/Users\/([^/]+)\/FavoriteItems\/([^/]+)$/)
  if (favMatch && (method === 'POST' || method === 'DELETE')) {
    const itemId = decodeURIComponent(favMatch[2])
    const item = store.items.get(itemId)
    if (!item) throw new ApiError(404, '项目不存在')
    const ud = ensureUserData(item)
    ud.IsFavorite = method === 'POST'
    return { ...ud } as T
  }

  const playedMatch = pathname.match(/^\/emby\/Users\/([^/]+)\/PlayedItems\/([^/]+)$/)
  if (playedMatch && (method === 'POST' || method === 'DELETE')) {
    const itemId = decodeURIComponent(playedMatch[2])
    const item = store.items.get(itemId)
    if (!item) throw new ApiError(404, '项目不存在')
    const ud = ensureUserData(item)
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
        const sUd = ensureUserData(season)
        sUd.Played = ud.Played
        sUd.PlayedPercentage = ud.Played ? 100 : 0
        if (ud.Played) sUd.PlaybackPositionTicks = 0
        const eps = store.episodesBySeason.get(season.Id) || []
        totalEps += eps.length
        sUd.UnplayedItemCount = ud.Played ? 0 : eps.length
        for (const ep of eps) {
          const epUd = ensureUserData(ep)
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
        const epUd = ensureUserData(ep)
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
    const itemId = decodeURIComponent(localTrailersMatch[2])
    const trailers = store.localTrailersByItemId.get(itemId) || []
    return trailers.map((t) => ({ ...t })) as T
  }

  const specialFeaturesMatch = pathname.match(
    /^\/emby\/Users\/([^/]+)\/Items\/([^/]+)\/SpecialFeatures$/,
  )
  if (specialFeaturesMatch && method === 'GET') {
    const itemId = decodeURIComponent(specialFeaturesMatch[2])
    const extras = store.specialFeaturesByItemId.get(itemId) || []
    return extras.map((e) => ({ ...e })) as T
  }

  // ==================== 8. PlaybackInfo、音频歌词 & 播放状态上报 ====================
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
    const item = store.items.get(itemId)
    if (!item) throw new ApiError(404, '找不到可播放的媒体')
    const sources = item.MediaSources || []
    const res: PlaybackInfoResponse = {
      MediaSources: sources,
      PlaySessionId: `demo-play-${Date.now().toString(36)}`,
    }
    return res as T
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
    const item = store.items.get(itemId)
    if (item) {
      const ud = ensureUserData(item)
      ud.PlaybackPositionTicks = posTicks
      ud.LastPlayedDate = new Date().toISOString()
      if (item.RunTimeTicks && item.RunTimeTicks > 0) {
        ud.PlayedPercentage = Math.min(100, Math.round((posTicks / item.RunTimeTicks) * 100))
      }
      const webSess = store.sessions[0]
      if (webSess) {
        webSess.LastActivityDate = new Date().toISOString()
        if (pathname.endsWith('/Stopped')) {
          webSess.NowPlayingItem = undefined
          webSess.PlayState = undefined
        } else {
          webSess.NowPlayingItem = item
          webSess.PlayState = {
            PositionTicks: posTicks,
            IsPaused: Boolean(body.IsPaused),
            IsMuted: Boolean(body.IsMuted),
            VolumeLevel: typeof body.VolumeLevel === 'number' ? body.VolumeLevel : 100,
            PlayMethod: String(body.PlayMethod || 'DirectPlay'),
            CanSeek: true,
          }
        }
      }
    }
    return undefined as T
  }

  // ==================== 9. 媒体库管理与模拟目录浏览器 ====================
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
      pushActivity('已暂停媒体库扫描任务', 'LibraryScanPaused')
    }
    return undefined as T
  }

  if (pathname === '/api/admin/scanner/resume' && method === 'POST') {
    if (activeDemoScan && activeDemoScan.paused) {
      const pausedDelta = activeDemoScan.pausedAtMs ? Date.now() - activeDemoScan.pausedAtMs : 0
      activeDemoScan.startedAtMs += pausedDelta
      activeDemoScan.paused = false
      activeDemoScan.pausedAtMs = undefined
      pushActivity('已恢复媒体库扫描任务', 'LibraryScanResumed')
    }
    return undefined as T
  }

  if (pathname === '/api/admin/scanner/cancel' && method === 'POST') {
    if (activeDemoScan) {
      activeDemoScan = null
      pushActivity('已取消当前媒体库扫描任务', 'LibraryScanCancelled')
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
        SubtitlesBytes: 12 * 1024 * 1024,
        TranscodeBytes: 0,
        TotalBytes: 198 * 1024 * 1024,
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
      CacheBytes: 198 * 1024 * 1024,
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
    pushActivity(`已合并 ${removedIds.length + 1} 个重复条目至「${primary.Name}」`, 'DuplicatesMerged')
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
    pushActivity(`已清理 ${deletedIds.length} 条磁盘孤立记录`, 'OrphansCleaned')
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
    pushActivity(`已通过审校项「${audit.ItemName}」`, 'ScrapeReviewApproved')
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
    pushActivity(`已忽略审校项「${audit.ItemName}」`, 'ScrapeReviewDismissed')
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
    pushActivity(`已更新「${item.Name}」的元数据锁定配置`, 'ItemFieldsLocked')
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
    pushActivity(`已原子回写「${item.Name}」的 NFO 文件`, 'ItemNfoWritten', nfoPath)
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
    )
    return undefined as T
  }

  if (pathname === '/emby/Library/VirtualFolders/Name' && method === 'POST') {
    const body = parseJsonBody(options)
    const oldName = String(qs.get('Name') || qs.get('name') || body.Name || '').trim()
    const newName = String(qs.get('NewName') || qs.get('newName') || body.NewName || '').trim()
    const id = String(body.Id || qs.get('Id') || '').trim()
    if (!newName) throw new ApiError(400, '新名称不能为空')
    const lib = store.libraries.find((l) => (id && l.ItemId === id) || (oldName && l.Name === oldName))
    if (!lib) throw new ApiError(404, '媒体库不存在')
    const prevName = lib.Name
    lib.Name = newName
    pushActivity(`已将媒体库「${prevName}」重命名为「${newName}」`, 'LibraryRenamed')
    return undefined as T
  }

  if (pathname === '/emby/Library/VirtualFolders/Paths') {
    if (method === 'POST') {
      const body = parseJsonBody(options)
      const name = String(body.Name || qs.get('Name') || qs.get('name') || '').trim()
      const id = String(body.Id || qs.get('Id') || '').trim()
      const targetPath = String(body.Path || (body.PathInfo as { Path?: string } | undefined)?.Path || '').trim()
      if (!targetPath) throw new ApiError(400, '路径不能为空')
      const lib = store.libraries.find((l) => (id && l.ItemId === id) || (name && l.Name === name))
      if (!lib) throw new ApiError(404, '媒体库不存在')
      const locations = lib.Locations || []
      if (!locations.includes(targetPath)) {
        lib.Locations = [...locations, targetPath]
      }
      pushActivity(`为媒体库「${lib.Name}」添加路径`, 'LibraryPathAdded', targetPath)
      return undefined as T
    }
    if (method === 'DELETE') {
      const name = String(qs.get('Name') || qs.get('name') || '').trim()
      const id = String(qs.get('Id') || qs.get('id') || '').trim()
      const targetPath = String(qs.get('Path') || qs.get('path') || '').trim()
      const lib = store.libraries.find((l) => (id && l.ItemId === id) || (name && l.Name === name))
      if (!lib) throw new ApiError(404, '媒体库不存在')
      lib.Locations = (lib.Locations || []).filter((p) => p !== targetPath)
      pushActivity(`从媒体库「${lib.Name}」移除路径`, 'LibraryPathRemoved', targetPath)
      return undefined as T
    }
  }

  if (pathname === '/emby/Library/VirtualFolders/LibraryOptions' && method === 'POST') {
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
    )
    return undefined as T
  }

  if (pathname === '/emby/Library/VirtualFolders') {
    if (method === 'GET') {
      return store.libraries.map((l) => ({
        ...l,
        Locations: [...(l.Locations || [])],
        LibraryOptions: {
          SaveLocalMetadata: true,
          ...(l.LibraryOptions || {}),
        },
      })) as T
    }
    if (method === 'POST') {
      const body = parseJsonBody(options)
      const name = String(body.Name || qs.get('name') || '').trim()
      const collectionType = String(body.CollectionType || qs.get('collectionType') || 'movies')
      const paths = Array.isArray(body.Paths)
        ? body.Paths.map((p) => String(p))
        : []
      if (!name) throw new ApiError(400, '媒体库名称不能为空')
      const incomingOpts = (body.LibraryOptions || {}) as Record<string, unknown>
      const newLib: VirtualFolderInfo = {
        ItemId: `lib-demo-${Date.now().toString(36)}`,
        Name: name,
        CollectionType: collectionType,
        Locations: paths,
        LibraryOptions: {
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
      pushActivity(`新建媒体库「${name}」`, 'LibraryCreated', `挂载目录：${paths.join(', ')}`)
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
        pushActivity(`已移除媒体库「${removed.Name}」`, 'LibraryDeleted')
      }
      return undefined as T
    }
  }

  if (pathname === '/emby/Environment/Drives' && method === 'GET') {
    return [...store.fsDrives] as T
  }

  if (pathname === '/emby/Environment/DirectoryContents' && method === 'GET') {
    const rawPath = (qs.get('path') || '/').replace(/\/+$/, '') || '/'
    const entries = store.fsDirs[rawPath] || []
    return [...entries] as T
  }

  // ==================== 10. 会话、远程控制、活动日志、设备与 API Key ====================
  if (pathname === '/emby/Sessions' && method === 'GET') {
    return [...store.sessions] as T
  }

  const sessionPlaystateMatch = pathname.match(/^\/emby\/Sessions\/([^/]+)\/Playing\/([^/]+)$/)
  if (sessionPlaystateMatch && method === 'POST') {
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
    const startIndex = Number(qs.get('StartIndex') || 0)
    const limit = Number(qs.get('Limit') || 50)
    return {
      Items: store.activityLogs.slice(startIndex, startIndex + limit),
      TotalRecordCount: store.activityLogs.length,
    } as T
  }

  if (pathname === '/emby/Devices') {
    if (method === 'GET') {
      return { Items: [...store.devices], TotalRecordCount: store.devices.length } as T
    }
    if (method === 'DELETE') {
      const devId = qs.get('id') || ''
      const idx = store.devices.findIndex((d) => d.Id === devId)
      if (idx >= 0) {
        const [removed] = store.devices.splice(idx, 1)
        store.sessions = store.sessions.filter((s) => s.DeviceId !== devId)
        pushActivity(`已注销并踢出设备「${removed.Name}」`, 'DeviceRevoked')
      }
      return undefined as T
    }
  }

  if (pathname === '/emby/Auth/Keys') {
    if (method === 'GET') {
      return { Items: [...store.apiKeys], TotalRecordCount: store.apiKeys.length } as T
    }
    if (method === 'POST') {
      const body = parseJsonBody(options)
      const appName = String(body.AppName || body.App || qs.get('app') || '').trim()
      if (!appName) throw new ApiError(400, '应用名称不能为空')
      const token =
        'ambra_demo_' +
        Math.random().toString(36).slice(2, 10) +
        Date.now().toString(36)
      const info: ApiKeyInfo = {
        AccessToken: token,
        AppName: appName,
        DateCreated: new Date().toISOString(),
        IsActive: true,
      }
      store.apiKeys.unshift(info)
      pushActivity(`为「${appName}」签发了新的 API Key`, 'ApiKeyCreated')
      return info as T
    }
  }

  const deleteKeyMatch = pathname.match(/^\/emby\/Auth\/Keys\/([^/]+)$/)
  if (deleteKeyMatch && method === 'DELETE') {
    const key = decodeURIComponent(deleteKeyMatch[1])
    store.apiKeys = store.apiKeys.filter((k) => k.AccessToken !== key)
    pushActivity('已吊销一个第三方 API Key', 'ApiKeyRevoked')
    return undefined as T
  }

  // ==================== 11. 插件配置 ====================
  if (pathname === '/emby/Plugins' && method === 'GET') {
    return store.plugins.map((p) => ({ ...p })) as T
  }

  const pluginCfgMatch = pathname.match(/^\/emby\/Plugins\/([^/]+)\/Configuration$/)
  if (pluginCfgMatch) {
    const pluginId = decodeURIComponent(pluginCfgMatch[1])
    if (method === 'GET') {
      return { ...(store.pluginConfigs.get(pluginId) || {}) } as T
    }
    if (method === 'POST') {
      const nextCfg = parseJsonBody(options)
      store.pluginConfigs.set(pluginId, nextCfg)
      const plugin = store.plugins.find((p) => p.Id === pluginId)
      if (plugin) {
        if (typeof nextCfg.enabled === 'boolean') plugin.Enabled = nextCfg.enabled
        else if (typeof nextCfg.Enabled === 'boolean') plugin.Enabled = nextCfg.Enabled
        if (typeof nextCfg.priority === 'number') plugin.Priority = nextCfg.priority
        else if (typeof nextCfg.Priority === 'number') plugin.Priority = nextCfg.Priority
        pushActivity(`更新了插件「${plugin.Name}」的配置`, 'PluginConfigured')
      }
      return undefined as T
    }
  }

  // ==================== 12. 计划任务、触发规则、Live TV 与服务器日志 ====================
  if (pathname === '/emby/ScheduledTasks' && method === 'GET') {
    return store.scheduledTasks.map((t) => ({
      ...t,
      Triggers: t.Triggers ? t.Triggers.map((tr) => ({ ...tr })) : [],
    })) as T
  }

  const taskTriggersMatch = pathname.match(/^\/emby\/ScheduledTasks\/([^/]+)\/Triggers$/)
  if (taskTriggersMatch && method === 'POST') {
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
    )
    return undefined as T
  }

  // Live TV (M3U Tuner Hosts / XMLTV EPG / Channels)
  if (pathname === '/emby/LiveTv/Info' && method === 'GET') {
    const info: LiveTvInfo = {
      IsEnabled: true,
      Services: [{ Name: 'Ambra M3U / XMLTV', Status: 'Ok' }],
      EnabledUsers: store.users.map((u) => u.user.Id),
    }
    return info as T
  }

  if (pathname === '/emby/LiveTv/Channels' && method === 'GET') {
    const limit = Number(qs.get('Limit') || 100)
    const sliced = demoLiveTvChannels.slice(0, limit)
    return { Items: sliced, TotalRecordCount: demoLiveTvChannels.length } as T
  }

  const liveTvStreamMatch = pathname.match(/^\/emby\/LiveTv\/Channels\/([^/]+)\/Stream$/)
  if (liveTvStreamMatch && method === 'GET') {
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

  if (pathname === '/emby/LiveTv/TunerHosts') {
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
      pushActivity(`已添加 Live TV 直播源「${created.FriendlyName}」`, 'LiveTvTunerAdded', urlVal)
      return { ...created } as T
    }
    if (method === 'DELETE') {
      const id = qs.get('id') || ''
      const idx = demoTunerHosts.findIndex((h) => h.Id === id)
      if (idx >= 0) {
        const [removed] = demoTunerHosts.splice(idx, 1)
        pushActivity(`已删除 Live TV 直播源「${removed.FriendlyName || id}」`, 'LiveTvTunerDeleted')
      }
      return undefined as T
    }
  }

  if (pathname === '/emby/LiveTv/ListingProviders') {
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
      pushActivity(`已添加 XMLTV EPG 节目单来源`, 'LiveTvEpgAdded', pathVal)
      return { ...created } as T
    }
    if (method === 'DELETE') {
      const id = qs.get('id') || ''
      const idx = demoListingProviders.findIndex((p) => p.Id === id)
      if (idx >= 0) {
        demoListingProviders.splice(idx, 1)
        pushActivity(`已删除 XMLTV EPG 节目单来源`, 'LiveTvEpgDeleted')
      }
      return undefined as T
    }
  }

  // Live TV DVR 预约录制 (Timers)
  if (pathname === '/emby/LiveTv/Timers') {
    if (method === 'GET') {
      const items = demoLiveTvTimers.map((t) => ({ ...t }))
      return { Items: items, TotalRecordCount: items.length } as T
    }
    if (method === 'POST') {
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
      pushActivity(`已创建 DVR 录制计划「${created.Name}」`, 'LiveTvTimerCreated')
      return { ...created } as T
    }
    if (method === 'DELETE') {
      const id = qs.get('id') || qs.get('Id') || ''
      const idx = demoLiveTvTimers.findIndex((t) => t.Id === id)
      if (idx >= 0) {
        const [removed] = demoLiveTvTimers.splice(idx, 1)
        pushActivity(`已取消 DVR 录制计划「${removed.Name}」`, 'LiveTvTimerDeleted')
      }
      return undefined as T
    }
  }

  const liveTvTimerDeleteMatch = pathname.match(/^\/emby\/LiveTv\/Timers\/([^/]+)$/)
  if (liveTvTimerDeleteMatch && method === 'DELETE') {
    const id = decodeURIComponent(liveTvTimerDeleteMatch[1])
    const idx = demoLiveTvTimers.findIndex((t) => t.Id === id)
    if (idx >= 0) {
      const [removed] = demoLiveTvTimers.splice(idx, 1)
      pushActivity(`已取消 DVR 录制计划「${removed.Name}」`, 'LiveTvTimerDeleted')
    }
    return undefined as T
  }

  // Live TV DVR 已完成录制 (Recordings)
  if (pathname === '/emby/LiveTv/Recordings') {
    if (method === 'GET') {
      const items = demoLiveTvRecordings.map((r) => ({ ...r }))
      return { Items: items, TotalRecordCount: items.length } as T
    }
    if (method === 'DELETE') {
      const id = qs.get('id') || qs.get('Id') || ''
      const idx = demoLiveTvRecordings.findIndex((r) => r.Id === id)
      if (idx >= 0) {
        const [removed] = demoLiveTvRecordings.splice(idx, 1)
        pushActivity(`已删除 DVR 录制文件「${removed.Name}」`, 'LiveTvRecordingDeleted')
      }
      return undefined as T
    }
  }

  const liveTvRecordingDeleteMatch = pathname.match(/^\/emby\/LiveTv\/Recordings\/([^/]+)$/)
  if (liveTvRecordingDeleteMatch && method === 'DELETE') {
    const id = decodeURIComponent(liveTvRecordingDeleteMatch[1])
    const idx = demoLiveTvRecordings.findIndex((r) => r.Id === id)
    if (idx >= 0) {
      const [removed] = demoLiveTvRecordings.splice(idx, 1)
      pushActivity(`已删除 DVR 录制文件「${removed.Name}」`, 'LiveTvRecordingDeleted')
    }
    return undefined as T
  }

  // Webhooks 事件通知管理
  if (pathname === '/emby/Webhooks') {
    if (method === 'GET') {
      return demoWebhooks.map((w) => ({ ...w, Events: [...w.Events] })) as T
    }
    if (method === 'POST') {
      const body = parseJsonBody(options)
      const name = String(body.Name || '').trim()
      const urlVal = String(body.Url || '').trim()
      if (!name || !urlVal) {
        throw new ApiError(400, 'Webhook 名称与推送 URL 不能为空')
      }
      const events = Array.isArray(body.Events)
        ? body.Events.map((e) => String(e).trim()).filter(Boolean)
        : ['*']
      const secretVal = typeof body.Secret === 'string' && body.Secret.trim() ? body.Secret.trim() : undefined
      const enabledVal = body.Enabled !== undefined ? Boolean(body.Enabled) : true
      const existingId = typeof body.Id === 'string' ? body.Id.trim() : ''
      const existingIdx = existingId ? demoWebhooks.findIndex((w) => w.Id === existingId) : -1

      if (existingIdx >= 0) {
        const updated: WebhookInfo = {
          ...demoWebhooks[existingIdx],
          Name: name,
          Url: urlVal,
          Secret: secretVal,
          Events: events.length > 0 ? events : ['*'],
          Enabled: enabledVal,
        }
        demoWebhooks[existingIdx] = updated
        pushActivity(`已更新 Webhook「${updated.Name}」`, 'WebhookUpdated')
        return { ...updated, Events: [...updated.Events] } as T
      }

      const created: WebhookInfo = {
        Id: `wh-demo-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 5)}`,
        Name: name,
        Url: urlVal,
        Secret: secretVal,
        Events: events.length > 0 ? events : ['*'],
        Enabled: enabledVal,
        CreatedAt: Math.floor(Date.now() / 1000),
      }
      demoWebhooks.push(created)
      pushActivity(`已创建 Webhook「${created.Name}」`, 'WebhookCreated')
      return { ...created, Events: [...created.Events] } as T
    }
  }

  if (pathname === '/emby/Webhooks/Test' && method === 'POST') {
    const res: WebhookTestResult = {
      Success: true,
      StatusCode: 200,
      DurationMs: 42,
    }
    return res as T
  }

  const webhookDeleteMatch = pathname.match(/^\/emby\/Webhooks\/([^/]+)$/)
  if (webhookDeleteMatch && method === 'DELETE') {
    const id = decodeURIComponent(webhookDeleteMatch[1])
    const idx = demoWebhooks.findIndex((w) => w.Id === id)
    if (idx >= 0) {
      const [removed] = demoWebhooks.splice(idx, 1)
      pushActivity(`已删除 Webhook「${removed.Name}」`, 'WebhookDeleted')
    }
    return undefined as T
  }

  // 家长控制内容分级列表
  if (pathname === '/emby/Localization/ParentalRatings' && method === 'GET') {
    return DEMO_PARENTAL_RATINGS.map((r) => ({ ...r })) as T
  }

  const runTaskMatch = pathname.match(/^\/emby\/ScheduledTasks\/Running\/([^/]+)$/)
  if (runTaskMatch && method === 'POST') {
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
    pushActivity(`手动执行计划任务「${task.Name}」完成`, 'ScheduledTaskCompleted')
    return undefined as T
  }

  if (runTaskMatch && method === 'DELETE') {
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
    pushActivity(`已手动停止计划任务「${task.Name}」`, 'ScheduledTaskStopped')
    return undefined as T
  }

  if (pathname === '/emby/System/Logs/Query' && method === 'GET') {
    return [...store.serverLogs] as T
  }

  const logContentMatch = pathname.match(/^\/emby\/System\/Logs\/([^/]+)$/)
  if (logContentMatch && method === 'GET') {
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

  // ==================== 14. 媒体元数据、图片管理、远端识别与多版本拆分 ====================
  const remoteSearchMatch = pathname.match(/^\/emby\/Items\/RemoteSearch\/(Movie|Series)$/)
  if (remoteSearchMatch && method === 'POST') {
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
        ? scored.filter((s) => s.score > 0).slice(0, 5).map((s) => s.item)
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

    // 若用户输入了自定义搜索词且未完全精确命中，额外附带一条精确命名的高清候选方便演示
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
    pushActivity(`已将远端识别结果应用于「${item.Name}」`, 'ItemIdentified')
    return undefined as T
  }

  const itemThemeSongsMatch = pathname.match(/^\/emby\/Items\/([^/]+)\/ThemeSongs$/)
  if (itemThemeSongsMatch && method === 'GET') {
    const itemId = decodeURIComponent(itemThemeSongsMatch[1])
    const item = store.items.get(itemId)
    if (!item) {
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
    const limit = Math.max(1, Number(qs.get('Limit') || 50))
    const seed = store.items.get(itemId)
    const audioItems = Array.from(store.items.values()).filter(
      (it) => it.Type === 'Audio' || it.MediaType === 'Audio',
    )
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
    const itemId = decodeURIComponent(itemRefreshMatch[1])
    const item = store.items.get(itemId)
    if (!item) throw new ApiError(404, '项目不存在')
    item.Etag = `etag-${Date.now().toString(36)}`
    pushActivity(`已刷新「${item.Name}」的元数据`, 'ItemRefreshed')
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
    pushActivity(`已为「${item.Name}」替换远端 ${imgType} 图片`, 'ItemImageUpdated')
    return undefined as T
  }

  const remoteImagesMatch = pathname.match(/^\/emby\/Items\/([^/]+)\/RemoteImages$/)
  if (remoteImagesMatch && method === 'GET') {
    const itemId = decodeURIComponent(remoteImagesMatch[1])
    const item = store.items.get(itemId)
    if (!item) throw new ApiError(404, '项目不存在')
    const filterType = qs.get('Type') || ''

    const images: RemoteImageInfo[] = []
    const seenUrls = new Set<string>()

    // 优先放入当前项目与库内其它已验证 TMDB 高清图
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
    pushActivity(`已通过 URL 更新「${item.Name}」的 ${imgType} 图片`, 'ItemImageUpdated')
    return undefined as T
  }

  const itemImageDeleteMatch = pathname.match(
    /^\/emby\/Items\/([^/]+)\/Images\/([^/]+)(?:\/(\d+))?$/,
  )
  if (itemImageDeleteMatch && method === 'DELETE') {
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
    pushActivity(`已删除「${item.Name}」的 ${imgType} 图片`, 'ItemImageDeleted')
    return undefined as T
  }

  const itemImageUploadMatch = pathname.match(
    /^\/emby\/Items\/([^/]+)\/Images\/([^/]+)(?:\/(\d+))?$/,
  )
  if (itemImageUploadMatch && method === 'POST') {
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
    pushActivity(`已上传「${item.Name}」的 ${imgType} 图片`, 'ItemImageUploaded')
    return undefined as T
  }

  const uploadSubtitleMatch = pathname.match(/^\/emby\/Videos\/([^/]+)\/Subtitles$/)
  if (uploadSubtitleMatch && method === 'POST') {
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
    pushActivity(`已为「${item.Name}」上传外挂字幕 (${format.toUpperCase()})`, 'SubtitleUploaded')
    return undefined as T
  }

  const deleteSubtitleMatch = pathname.match(/^\/emby\/Videos\/([^/]+)\/Subtitles\/(\d+)$/)
  if (deleteSubtitleMatch && method === 'DELETE') {
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
    pushActivity(`已删除「${item.Name}」的外挂字幕 #${subIdx}`, 'SubtitleDeleted')
    return undefined as T
  }

  const splitVersionsMatch = pathname.match(/^\/emby\/Videos\/([^/]+)\/AlternateSources$/)
  if (splitVersionsMatch && method === 'DELETE') {
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
        UserData: item.UserData ? { ...item.UserData } : undefined,
        ImageTags: item.ImageTags ? { ...item.ImageTags } : undefined,
        BackdropImageTags: item.BackdropImageTags ? [...item.BackdropImageTags] : undefined,
      }
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
    )
    return undefined as T
  }

  if (pathname === '/emby/Videos/MergeVersions' && method === 'POST') {
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
        pushActivity(`已将 ${ids.length} 个媒体合并为多版本「${primary.Name}」`, 'ItemVersionsMerged')
      }
    }
    return undefined as T
  }

  const singleItemMutationMatch = pathname.match(/^\/emby\/Items\/([^/]+)$/)
  if (singleItemMutationMatch && method === 'POST') {
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
    if (body.ProductionYear !== undefined && body.ProductionYear !== null && body.ProductionYear !== '') {
      item.ProductionYear = Number(body.ProductionYear)
    }
    if (body.CommunityRating !== undefined && body.CommunityRating !== null && body.CommunityRating !== '') {
      item.CommunityRating = Number(body.CommunityRating)
    }
    if (typeof body.OfficialRating === 'string') {
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
    item.Etag = `etag-${Date.now().toString(36)}`
    pushActivity(`已更新「${item.Name}」的元数据`, 'ItemMetadataUpdated')
    return undefined as T
  }

  if (singleItemMutationMatch && method === 'DELETE') {
    const itemId = decodeURIComponent(singleItemMutationMatch[1])
    const item = store.items.get(itemId)
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
      pushActivity(`已删除媒体「${item.Name}」`, 'ItemDeleted')
    }
    return undefined as T
  }

  // ==================== 14.5 数据库备份与恢复 (BackupRestore) ====================
  if (pathname === '/emby/BackupRestore/BackupInfo' && method === 'GET') {
    const res: BackupInfoResponse = {
      BackupDirectory: '/data/backups',
      Backups: demoBackups.map((b) => ({ ...b })),
    }
    return res as T
  }

  if (pathname === '/emby/BackupRestore/Backup' && method === 'POST') {
    const now = new Date()
    const pad = (n: number) => String(n).padStart(2, '0')
    const stamp = `${now.getUTCFullYear()}${pad(now.getUTCMonth() + 1)}${pad(now.getUTCDate())}-${pad(now.getUTCHours())}${pad(now.getUTCMinutes())}${pad(now.getUTCSeconds())}`
    const name = `ambra-backup-${stamp}-${Math.random().toString(36).slice(2, 5)}.db`
    const created: BackupFileInfo = {
      Name: name,
      Path: `/data/backups/${name}`,
      Size: 2_684_354,
      DateCreated: now.toISOString(),
    }
    demoBackups.unshift(created)
    pushActivity(`已创建数据库热备份「${name}」`, 'BackupCreated')
    return { ...created } as T
  }

  if (pathname === '/emby/BackupRestore/Restore' && method === 'POST') {
    const body = parseJsonBody(options)
    const name = String(body.Name || '').trim()
    if (!name) throw new ApiError(400, '请指定要恢复的备份文件名')
    const exists = demoBackups.find((b) => b.Name === name)
    if (!exists) throw new ApiError(404, `备份文件「${name}」不存在`)
    pushActivity(`已暂存数据库备份恢复「${name}」（重启后生效）`, 'BackupRestoreStaged')
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
    pushActivity(`管理员发送了系统通知「${name}」`, 'NotificationSent', description, auth?.userId)

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
    return true as T
  }

  if (pathname === '/emby/QuickConnect/Initiate' && method === 'POST') {
    const code = String(Math.floor(100000 + Math.random() * 900000))
    const secret = `qc-demo-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`
    const session: DemoQuickConnectSession = {
      Authenticated: false,
      Secret: secret,
      Code: code,
      DeviceId: 'dev-demo-web-01',
      DeviceName: 'Ambra Web Demo',
      AppName: 'Ambra Web',
      AppVersion: '0.9.3',
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
      session.AuthorizedUserId = auth?.userId || store.users[0]?.user.Id || 'user-demo-admin'
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
      matched.AuthorizedUserId = auth?.userId || store.users[0]?.user.Id || 'user-demo-admin'
      pushActivity(
        `已通过 Quick Connect 授权新设备登录（授权码 ${code}）`,
        'QuickConnectAuthorized',
        undefined,
        auth?.userId,
      )
      return true as T
    }
    if (code === '123456') {
      pushActivity(
        '已通过 Quick Connect 授权演示设备登录（授权码 123456）',
        'QuickConnectAuthorized',
        undefined,
        auth?.userId,
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
      const rec = store.users.find((u) => u.user.Id === userId)
      if (!rec) throw new ApiError(404, '用户不存在')
      return { ...rec.user, Policy: { ...rec.user.Policy } } as T
    }
    if (method === 'DELETE') {
      const idx = store.users.findIndex((u) => u.user.Id === userId)
      if (idx >= 0) {
        const [removed] = store.users.splice(idx, 1)
        pushActivity(`已删除用户「${removed.user.Name}」`, 'UserDeleted')
      }
      return undefined as T
    }
  }

  throw new ApiError(404, `[Demo Mock] 未实现的端点: ${method} ${pathname}`)
}
