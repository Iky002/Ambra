import { ApiError, getStoredAuth } from '../api/client'
import type {
  ApiKeyInfo,
  AuthenticationResult,
  BaseItemDto,
  ItemImageInfo,
  PlaybackInfoResponse,
  PublicSystemInfo,
  QueryResult,
  RankingsRefreshResult,
  RankingsSummary,
  RemoteImageInfo,
  RemoteImageResult,
  RemoteSearchResult,
  SearchHint,
  SearchHintResult,
  UserDto,
  UserItemDataDto,
  UserPolicy,
  VirtualFolderInfo,
} from '../api/types'
import { createInitialDemoState, DEMO_SERVER_ID, type DemoState } from './mockData'
import { makeAvatarDataUrl, makeLibraryThumbDataUrl, makePosterDataUrl } from './svgArt'

let store: DemoState = createInitialDemoState()

interface DemoRankingsState {
  doubanUpdatedAt: string
  imdbUpdatedAt: string
}

function createInitialRankingsState(): DemoRankingsState {
  const threeHoursAgo = new Date(Date.now() - 3 * 3600_000).toISOString()
  return {
    doubanUpdatedAt: threeHoursAgo,
    imdbUpdatedAt: threeHoursAgo,
  }
}

let rankingsState: DemoRankingsState = createInitialRankingsState()

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

  // ==================== 1. 系统与公开信息 ====================
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
    rec.user.Policy = { ...rec.user.Policy, ...patch }
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
    const tagsParam = (qs.get('Tags') || '')
      .split(/[|,]/)
      .map((s) => s.trim().toLowerCase())
      .filter(Boolean)
    const searchTerm = qs.get('SearchTerm') || ''
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
    if (tagsParam.length > 0) {
      pool = pool.filter((it) =>
        (it.Tags || []).some((t) => tagsParam.includes(t.toLowerCase())),
      )
    }
    if (searchTerm) {
      pool = pool.filter((it) => matchesFuzzySearch(it, searchTerm))
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
    throw new ApiError(404, `未找到项目 ${itemId}`)
  }

  // ==================== 5. 剧集季与分集 ====================
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
      .filter((it) => it.Type === 'Movie' || it.Type === 'Series' || it.Type === 'Episode')
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

  // ==================== 7. 收藏 / 已看写入 ====================
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
    return { ...ud } as T
  }

  // ==================== 8. PlaybackInfo & 播放状态上报 ====================
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
  if (pathname === '/emby/Library/Refresh' && method === 'POST') {
    const parentId = qs.get('ParentId')
    const lib = parentId ? store.libraries.find((l) => l.ItemId === parentId) : null
    pushActivity(
      lib ? `已触发媒体库「${lib.Name}」扫描任务` : '已触发全部媒体库扫描任务',
      'LibraryScan',
      'Demo 模拟扫描已在 0.4s 内完成',
    )
    return undefined as T
  }

  if (pathname === '/emby/Library/VirtualFolders') {
    if (method === 'GET') {
      return store.libraries.map((l) => ({ ...l, Locations: [...(l.Locations || [])] })) as T
    }
    if (method === 'POST') {
      const body = parseJsonBody(options)
      const name = String(body.Name || qs.get('name') || '').trim()
      const collectionType = String(body.CollectionType || qs.get('collectionType') || 'movies')
      const paths = Array.isArray(body.Paths)
        ? body.Paths.map((p) => String(p))
        : []
      if (!name) throw new ApiError(400, '媒体库名称不能为空')
      const newLib: VirtualFolderInfo = {
        ItemId: `lib-demo-${Date.now().toString(36)}`,
        Name: name,
        CollectionType: collectionType,
        Locations: paths,
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

  // ==================== 10. 会话、活动日志、设备与 API Key ====================
  if (pathname === '/emby/Sessions' && method === 'GET') {
    return [...store.sessions] as T
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
        if (typeof nextCfg.Enabled === 'boolean') plugin.Enabled = nextCfg.Enabled
        if (typeof nextCfg.Priority === 'number') plugin.Priority = nextCfg.Priority
        pushActivity(`更新了插件「${plugin.Name}」的配置`, 'PluginConfigured')
      }
      return undefined as T
    }
  }

  // ==================== 12. 计划任务与服务器日志 ====================
  if (pathname === '/emby/ScheduledTasks' && method === 'GET') {
    return store.scheduledTasks.map((t) => ({ ...t })) as T
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
    }
    return summary as T
  }

  if (pathname === '/emby/System/Rankings/Refresh' && method === 'POST') {
    const now = new Date().toISOString()
    rankingsState.doubanUpdatedAt = now
    rankingsState.imdbUpdatedAt = now

    let matchedItems = 0
    let tagsAdded = 0

    for (const item of store.items.values()) {
      if (item.Type !== 'Movie' && item.Type !== 'Series') continue
      const tmdbId = item.ProviderIds?.Tmdb || ''
      const inDouban =
        DEMO_DOUBAN_TOP250_TMDB_IDS.has(tmdbId) || Boolean(item.Tags?.includes('豆瓣Top250'))
      const inImdb =
        DEMO_IMDB_TOP250_TMDB_IDS.has(tmdbId) || Boolean(item.Tags?.includes('IMDb Top250'))

      if (!inDouban && !inImdb) continue
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
      item.Tags = Array.from(tags)
    }

    pushActivity(
      '已同步豆瓣 Top 250 / IMDb Top 250 榜单缓存',
      'RankingsSynced',
      `命中 ${matchedItems} 部影片，新增标签 ${tagsAdded} 个`,
    )

    const result: RankingsRefreshResult = {
      douban_count: 250,
      imdb_count: 250,
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
