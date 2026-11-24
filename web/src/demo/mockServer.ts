import { ApiError, getStoredAuth } from '../api/client'
import type {
  ApiKeyInfo,
  AuthenticationResult,
  BaseItemDto,
  PlaybackInfoResponse,
  PublicSystemInfo,
  QueryResult,
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

/**
 * 重置 Demo 内存态数据（供单元测试与演示重置使用）
 */
export function resetDemoStore(): void {
  store = createInitialDemoState()
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

  // ==================== 13. 单用户查询/删除（放在更具体路由之后） ====================
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
