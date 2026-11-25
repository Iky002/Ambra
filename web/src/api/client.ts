import { getCachedDemoServer, loadDemoServer } from '../demo/demoLoader'
import { DEMO_AUTH_STORAGE_KEY, isDemoMode } from '../demo/demoMode'
import { getActiveLocale } from '../i18n/I18nContext'
import type {
  ActivityLogEntry,
  ApiKeyInfo,
  AudioLyricsResponse,
  AuthSession,
  AuthenticationResult,
  BackupFileInfo,
  BackupInfoResponse,
  BackupRestoreResult,
  BaseItemDto,
  DeviceInfo,
  FileSystemEntry,
  ItemImageInfo,
  ItemMetadataUpdatePayload,
  LiveTvChannelInfo,
  LiveTvInfo,
  LiveTvListingProviderInfo,
  LiveTvRecordingInfo,
  LiveTvTimerInfo,
  LiveTvTunerHostInfo,
  MediaAttachmentInfo,
  NameIdPair,
  NotificationItem,
  NotificationResult,
  NotificationsSummary,
  ParentalRatingOption,
  PlaybackInfoResponse,
  PluginInfo,
  PublicSystemInfo,
  QueryResult,
  QuickConnectResult,
  RankingsRefreshResult,
  RankingsSummary,
  RemoteImageResult,
  RemoteSearchResult,
  ScanProgressSnapshot,
  ScheduledTaskInfo,
  SearchHintResult,
  ServerConfiguration,
  ServerLogFile,
  SessionInfo,
  SetupAdminRequest,
  SetupCompleteRequest,
  SetupStatus,
  SystemInfo,
  TaskTriggerInfo,
  UserConfiguration,
  UserDto,
  UserItemDataDto,
  UserPolicy,
  VirtualFolderInfo,
  WebhookInfo,
  WebhookTestResult,
} from './types'

const STORAGE_KEY = 'ambra_web_auth_v1'
const DEVICE_ID_KEY = 'ambra_web_device_id'

function getAuthStorageKey(): string {
  return isDemoMode() ? DEMO_AUTH_STORAGE_KEY : STORAGE_KEY
}

export function isSameOriginUrl(rawUrl: string): boolean {
  if (!rawUrl) return false
  if (typeof window === 'undefined' || !window.location?.origin) {
    return !/^[a-zA-Z][a-zA-Z\d+\-.]*:/.test(rawUrl) && !rawUrl.startsWith('//')
  }
  try {
    const parsed = new URL(rawUrl, window.location.origin)
    return parsed.origin === window.location.origin
  } catch {
    return false
  }
}

export function getDeviceId(): string {
  let id = localStorage.getItem(DEVICE_ID_KEY)
  if (!id) {
    id = 'ambra-web-' + Math.random().toString(36).slice(2) + Date.now().toString(36)
    localStorage.setItem(DEVICE_ID_KEY, id)
  }
  return id
}

export function getStoredAuth(): AuthSession | null {
  try {
    const raw = localStorage.getItem(getAuthStorageKey())
    if (!raw) return null
    const parsed = JSON.parse(raw) as AuthSession
    if (!parsed.accessToken || !parsed.userId) return null
    return parsed
  } catch {
    return null
  }
}

export function saveAuth(auth: AuthSession): void {
  localStorage.setItem(getAuthStorageKey(), JSON.stringify(auth))
}

export function clearAuth(): void {
  localStorage.removeItem(getAuthStorageKey())
}

export function buildEmbyAuthHeader(token?: string): string {
  const deviceId = getDeviceId()
  const parts = [
    'Client="Ambra Web"',
    'Device="Web Browser"',
    `DeviceId="${deviceId}"`,
    'Version="0.10.5"',
  ]
  if (token) {
    parts.push(`Token="${token}"`)
  }
  return `MediaBrowser ${parts.join(', ')}`
}

export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

export async function embyFetch<T>(
  path: string,
  options: RequestInit = {},
  requireAuth = true,
): Promise<T> {
  const normalizedPath = path.startsWith('/') ? path : `/${path}`

  if (isDemoMode()) {
    const demo = await loadDemoServer()
    return demo.demoFetch<T>(normalizedPath, options, requireAuth)
  }

  const auth = getStoredAuth()
  const headers = new Headers(options.headers || {})

  if (!headers.has('Content-Type') && options.body && typeof options.body === 'string') {
    headers.set('Content-Type', 'application/json')
  }
  headers.set('Accept', 'application/json')
  if (!headers.has('Accept-Language')) {
    headers.set('Accept-Language', getActiveLocale())
  }

  const token = auth?.accessToken
  headers.set('X-Emby-Authorization', buildEmbyAuthHeader(token))
  if (token) {
    headers.set('X-Emby-Token', token)
  } else if (requireAuth) {
    throw new ApiError(401, '未登录或登录已过期')
  }

  const res = await fetch(normalizedPath, {
    ...options,
    headers,
  })

  if (res.status === 401 && requireAuth) {
    clearAuth()
    window.dispatchEvent(new CustomEvent('ambra:unauthorized'))
    throw new ApiError(401, '登录凭证已失效，请重新登录')
  }

  if (!res.ok) {
    const text = await res.text().catch(() => '')
    throw new ApiError(res.status, text || `请求失败 (${res.status})`)
  }

  if (res.status === 204) {
    return undefined as unknown as T
  }

  const contentType = res.headers.get('content-type') || ''
  if (contentType.includes('application/json')) {
    return (await res.json()) as T
  }
  return (await res.text()) as unknown as T
}

export function resolveApiUrl(urlOrPath: string): string {
  if (!urlOrPath) return ''
  if (/^(data|blob):/i.test(urlOrPath)) {
    return urlOrPath
  }
  const auth = getStoredAuth()
  const token = auth?.accessToken
  const hasProtocol = /^https?:\/\//i.test(urlOrPath)
  const isProtocolRelative = urlOrPath.startsWith('//')
  const base =
    hasProtocol || isProtocolRelative
      ? urlOrPath
      : urlOrPath.startsWith('/')
        ? urlOrPath
        : `/${urlOrPath}`

  if (!isSameOriginUrl(base)) {
    return base
  }
  if (!token || base.includes('api_key=') || base.includes('X-Emby-Token=')) {
    return base
  }
  const sep = base.includes('?') ? '&' : '?'
  return `${base}${sep}api_key=${encodeURIComponent(token)}`
}

export function getImageUrl(
  itemId: string,
  type: 'Primary' | 'Backdrop' | 'Thumb' | 'Logo' | 'Banner' | (string & {}) = 'Primary',
  options?: { maxWidth?: number; maxHeight?: number; tag?: string; quality?: number; index?: number },
): string {
  if (!itemId) return ''
  if (isDemoMode()) {
    const demoType: 'Primary' | 'Backdrop' | 'Thumb' | 'Logo' | 'Banner' =
      type === 'Backdrop'
        ? 'Backdrop'
        : type === 'Thumb'
          ? 'Thumb'
          : type === 'Logo'
            ? 'Logo'
            : type === 'Banner'
              ? 'Banner'
              : 'Primary'
    return getCachedDemoServer()?.getDemoImageUrl(itemId, demoType) ?? ''
  }
  const params = new URLSearchParams()
  if (options?.maxWidth) params.set('maxWidth', String(options.maxWidth))
  if (options?.maxHeight) params.set('maxHeight', String(options.maxHeight))
  if (options?.tag) params.set('tag', options.tag)
  params.set('quality', String(options?.quality ?? 88))
  const indexSegment =
    options?.index !== undefined && Number.isInteger(options.index) && options.index >= 0
      ? `/${options.index}`
      : ''
  return `/emby/Items/${encodeURIComponent(itemId)}/Images/${encodeURIComponent(type)}${indexSegment}?${params.toString()}`
}

export function getFallbackImageUrl(
  itemId: string,
  type: 'Primary' | 'Backdrop' | 'Thumb' | 'Logo' | 'Banner' = 'Primary',
): string | null {
  if (!itemId || !isDemoMode()) return null
  return getCachedDemoServer()?.getDemoFallbackImageUrl(itemId, type) ?? null
}

export function getUserImageUrl(userId: string, tag?: string): string {
  if (isDemoMode()) {
    return getCachedDemoServer()?.getDemoUserImageUrl(userId) ?? ''
  }
  const params = new URLSearchParams({ maxWidth: '200', quality: '90' })
  if (tag) params.set('tag', tag)
  return `/emby/Users/${encodeURIComponent(userId)}/Images/Primary?${params.toString()}`
}

export function getBackdropUrl(item: BaseItemDto, maxWidth = 1920): string | null {
  if (item.BackdropImageTags && item.BackdropImageTags.length > 0) {
    return getImageUrl(item.Id, 'Backdrop', { maxWidth, tag: item.BackdropImageTags[0] })
  }
  if (item.SeriesId) {
    return getImageUrl(item.SeriesId, 'Backdrop', { maxWidth })
  }
  if (item.ImageTags?.Primary) {
    return getImageUrl(item.Id, 'Primary', { maxWidth, tag: item.ImageTags.Primary })
  }
  return null
}

export function getAudioStreamUrl(itemId: string): string {
  if (!itemId) return ''
  if (isDemoMode()) {
    return getCachedDemoServer()?.getDemoAudioUrl(itemId) ?? ''
  }
  const auth = getStoredAuth()
  const params = new URLSearchParams({
    Container: 'mp3,flac,aac,m4a,ogg,opus,wav',
    AudioCodec: 'aac,mp3,flac,opus,vorbis',
    MaxStreamingBitrate: '140000000',
  })
  if (auth?.userId) params.set('UserId', auth.userId)
  if (auth?.accessToken) params.set('api_key', auth.accessToken)
  return `/emby/Audio/${encodeURIComponent(itemId)}/universal?${params.toString()}`
}

// ==================== API Endpoints ====================

export const api = {
  getPublicSystemInfo: () =>
    embyFetch<PublicSystemInfo>('/emby/System/Info/Public', {}, false),

  getSystemInfo: () => embyFetch<SystemInfo>('/emby/System/Info'),

  getPublicUsers: () => embyFetch<UserDto[]>('/emby/Users/Public', {}, false),

  authenticateByName: (username: string, password: string) =>
    embyFetch<AuthenticationResult>(
      '/emby/Users/AuthenticateByName',
      {
        method: 'POST',
        body: JSON.stringify({
          Username: username,
          Pw: password,
        }),
      },
      false,
    ),

  logout: () =>
    embyFetch<void>('/emby/Sessions/Logout', {
      method: 'POST',
    }),

  getUserViews: (userId: string) =>
    embyFetch<QueryResult<BaseItemDto>>(`/emby/Users/${encodeURIComponent(userId)}/Views`),

  getResumeItems: (userId: string, limit = 12) => {
    const q = new URLSearchParams({
      Limit: String(limit),
      Recursive: 'true',
      Fields: 'PrimaryImageAspectRatio,Overview,MediaSources,Genres',
      ImageTypeLimit: '1',
      EnableImageTypes: 'Primary,Backdrop,Thumb',
      MediaTypes: 'Video',
    })
    return embyFetch<QueryResult<BaseItemDto>>(
      `/emby/Users/${encodeURIComponent(userId)}/Items/Resume?${q.toString()}`,
    )
  },

  getLatestItems: (userId: string, parentId?: string, limit = 16) => {
    const q = new URLSearchParams({
      Limit: String(limit),
      Fields: 'PrimaryImageAspectRatio,Overview,MediaSources,Genres,CommunityRating,ProductionYear',
      ImageTypeLimit: '1',
      EnableImageTypes: 'Primary,Backdrop,Thumb',
    })
    if (parentId) q.set('ParentId', parentId)
    return embyFetch<BaseItemDto[]>(
      `/emby/Users/${encodeURIComponent(userId)}/Items/Latest?${q.toString()}`,
    )
  },

  getItems: (
    userId: string,
    params: {
      parentId?: string
      includeItemTypes?: string
      sortBy?: string
      sortOrder?: 'Ascending' | 'Descending'
      filters?: string
      genres?: string
      years?: string
      tags?: string
      personIds?: string
      person?: string
      startIndex?: number
      limit?: number
      recursive?: boolean
      searchTerm?: string
      nameStartsWith?: string
      nameStartsWithOrGreater?: string
      nameLessThan?: string
      fields?: string
    } = {},
  ) => {
    const q = new URLSearchParams()
    if (params.parentId) q.set('ParentId', params.parentId)
    if (params.includeItemTypes) q.set('IncludeItemTypes', params.includeItemTypes)
    if (params.sortBy) q.set('SortBy', params.sortBy)
    if (params.sortOrder) q.set('SortOrder', params.sortOrder)
    if (params.filters) q.set('Filters', params.filters)
    if (params.genres) q.set('Genres', params.genres)
    if (params.years) q.set('Years', params.years)
    if (params.tags) q.set('Tags', params.tags)
    if (params.personIds) q.set('PersonIds', params.personIds)
    if (params.person) q.set('Person', params.person)
    if (params.startIndex !== undefined) q.set('StartIndex', String(params.startIndex))
    if (params.limit !== undefined) q.set('Limit', String(params.limit))
    if (params.recursive !== false) q.set('Recursive', 'true')
    if (params.searchTerm) q.set('SearchTerm', params.searchTerm)
    if (params.nameStartsWith) q.set('NameStartsWith', params.nameStartsWith)
    if (params.nameStartsWithOrGreater)
      q.set('NameStartsWithOrGreater', params.nameStartsWithOrGreater)
    if (params.nameLessThan) q.set('NameLessThan', params.nameLessThan)
    q.set(
      'Fields',
      params.fields ??
        'PrimaryImageAspectRatio,Overview,MediaSources,Genres,Tags,Studios,ProviderIds,CommunityRating,ProductionYear,DateCreated',
    )
    return embyFetch<QueryResult<BaseItemDto>>(
      `/emby/Users/${encodeURIComponent(userId)}/Items?${q.toString()}`,
    )
  },

  getTags: (params?: { userId?: string; parentId?: string }) => {
    const q = new URLSearchParams()
    if (params?.userId) q.set('UserId', params.userId)
    if (params?.parentId) q.set('ParentId', params.parentId)
    const qs = q.toString()
    return embyFetch<QueryResult<NameIdPair>>(`/emby/Tags${qs ? `?${qs}` : ''}`)
  },

  getGenres: (params?: { userId?: string; parentId?: string }) => {
    const q = new URLSearchParams()
    if (params?.userId) q.set('UserId', params.userId)
    if (params?.parentId) q.set('ParentId', params.parentId)
    const qs = q.toString()
    return embyFetch<QueryResult<NameIdPair>>(`/emby/Genres${qs ? `?${qs}` : ''}`)
  },

  getYears: (params?: { userId?: string; parentId?: string }) => {
    const q = new URLSearchParams()
    if (params?.userId) q.set('UserId', params.userId)
    if (params?.parentId) q.set('ParentId', params.parentId)
    const qs = q.toString()
    return embyFetch<QueryResult<NameIdPair>>(`/emby/Years${qs ? `?${qs}` : ''}`)
  },

  getNextUp: (
    userId: string,
    options?: { seriesId?: string; limit?: number; parentId?: string },
  ) => {
    const q = new URLSearchParams({
      UserId: userId,
      Limit: String(options?.limit ?? 12),
      Fields: 'Overview,PrimaryImageAspectRatio,MediaSources,UserData',
    })
    if (options?.seriesId) q.set('SeriesId', options.seriesId)
    if (options?.parentId) q.set('ParentId', options.parentId)
    return embyFetch<QueryResult<BaseItemDto>>(`/emby/Shows/NextUp?${q.toString()}`)
  },

  getSimilarItems: (itemId: string, userId: string, limit = 12) => {
    const q = new URLSearchParams({
      UserId: userId,
      Limit: String(limit),
      Fields: 'PrimaryImageAspectRatio,Overview,Genres,CommunityRating,ProductionYear',
    })
    return embyFetch<QueryResult<BaseItemDto>>(
      `/emby/Items/${encodeURIComponent(itemId)}/Similar?${q.toString()}`,
    )
  },

  getDownloadUrl: (itemId: string) =>
    resolveApiUrl(`/emby/Items/${encodeURIComponent(itemId)}/Download`),

  getAudioLyrics: (itemId: string) =>
    embyFetch<AudioLyricsResponse | string>(
      `/emby/Audio/${encodeURIComponent(itemId)}/Lyrics`,
    ),

  // ==================== Collections & Playlists CRUD ====================

  createCollection: (name: string, ids: string[] = []) => {
    const q = new URLSearchParams({ Name: name })
    if (ids.length > 0) q.set('Ids', ids.join(','))
    return embyFetch<{ Id: string; Name: string }>(`/emby/Collections?${q.toString()}`, {
      method: 'POST',
      body: JSON.stringify({ Name: name, Ids: ids }),
    })
  },

  deleteCollection: (collectionId: string) =>
    embyFetch<void>(`/emby/Collections/${encodeURIComponent(collectionId)}`, {
      method: 'DELETE',
    }),

  getCollectionItems: (collectionId: string, userId?: string) => {
    const q = new URLSearchParams({
      Fields: 'PrimaryImageAspectRatio,Overview,Genres,CommunityRating,ProductionYear,UserData',
    })
    if (userId) q.set('UserId', userId)
    return embyFetch<QueryResult<BaseItemDto>>(
      `/emby/Collections/${encodeURIComponent(collectionId)}/Items?${q.toString()}`,
    )
  },

  addCollectionItems: (collectionId: string, itemIds: string[]) => {
    const q = new URLSearchParams({ Ids: itemIds.join(',') })
    return embyFetch<void>(
      `/emby/Collections/${encodeURIComponent(collectionId)}/Items?${q.toString()}`,
      {
        method: 'POST',
        body: JSON.stringify({ Ids: itemIds }),
      },
    )
  },

  removeCollectionItems: (collectionId: string, itemIds: string[]) => {
    const q = new URLSearchParams({ Ids: itemIds.join(',') })
    return embyFetch<void>(
      `/emby/Collections/${encodeURIComponent(collectionId)}/Items?${q.toString()}`,
      {
        method: 'DELETE',
      },
    )
  },

  createPlaylist: (name: string, userId: string, ids: string[] = [], mediaType = 'Video') =>
    embyFetch<{ Id: string; Name: string }>('/emby/Playlists', {
      method: 'POST',
      body: JSON.stringify({
        Name: name,
        UserId: userId,
        Ids: ids,
        MediaType: mediaType,
      }),
    }),

  deletePlaylist: (playlistId: string) =>
    embyFetch<void>(`/emby/Playlists/${encodeURIComponent(playlistId)}`, {
      method: 'DELETE',
    }),

  getPlaylistItems: (playlistId: string, userId?: string) => {
    const q = new URLSearchParams({
      Fields: 'PrimaryImageAspectRatio,Overview,Genres,CommunityRating,ProductionYear,UserData',
    })
    if (userId) q.set('UserId', userId)
    return embyFetch<QueryResult<BaseItemDto>>(
      `/emby/Playlists/${encodeURIComponent(playlistId)}/Items?${q.toString()}`,
    )
  },

  addPlaylistItems: (playlistId: string, itemIds: string[], userId?: string) => {
    const q = new URLSearchParams({ Ids: itemIds.join(',') })
    if (userId) q.set('UserId', userId)
    return embyFetch<void>(
      `/emby/Playlists/${encodeURIComponent(playlistId)}/Items?${q.toString()}`,
      {
        method: 'POST',
        body: JSON.stringify({ Ids: itemIds }),
      },
    )
  },

  removePlaylistItems: (playlistId: string, itemIds: string[]) => {
    const q = new URLSearchParams({
      EntryIds: itemIds.join(','),
      Ids: itemIds.join(','),
    })
    return embyFetch<void>(
      `/emby/Playlists/${encodeURIComponent(playlistId)}/Items?${q.toString()}`,
      {
        method: 'DELETE',
      },
    )
  },

  movePlaylistItem: (playlistId: string, itemId: string, newIndex: number) =>
    embyFetch<void>(
      `/emby/Playlists/${encodeURIComponent(playlistId)}/Items/${encodeURIComponent(itemId)}/Move/${newIndex}`,
      {
        method: 'POST',
      },
    ),

  uploadUserImage: (userId: string, base64: string, contentType: string) => {
    const cleanBase64 = base64.replace(/^data:[^;]+;base64,/, '')
    return embyFetch<void>(`/emby/Users/${encodeURIComponent(userId)}/Images/Primary`, {
      method: 'POST',
      headers: {
        'Content-Type': contentType || 'image/jpeg',
      },
      body: cleanBase64,
    })
  },

  deleteUserImage: (userId: string) =>
    embyFetch<void>(`/emby/Users/${encodeURIComponent(userId)}/Images/Primary`, {
      method: 'DELETE',
    }),

  getItem: (userId: string, itemId: string) =>
    embyFetch<BaseItemDto>(
      `/emby/Users/${encodeURIComponent(userId)}/Items/${encodeURIComponent(itemId)}`,
    ),

  getSeasons: (seriesId: string, userId: string) => {
    const q = new URLSearchParams({
      UserId: userId,
      Fields: 'ItemCounts,PrimaryImageAspectRatio,Overview',
    })
    return embyFetch<QueryResult<BaseItemDto>>(
      `/emby/Shows/${encodeURIComponent(seriesId)}/Seasons?${q.toString()}`,
    )
  },

  getEpisodes: (seriesId: string, seasonId: string, userId: string) => {
    const q = new URLSearchParams({
      UserId: userId,
      SeasonId: seasonId,
      Fields: 'Overview,MediaSources,PrimaryImageAspectRatio,UserData',
    })
    return embyFetch<QueryResult<BaseItemDto>>(
      `/emby/Shows/${encodeURIComponent(seriesId)}/Episodes?${q.toString()}`,
    )
  },

  searchHints: (searchTerm: string, userId?: string, limit = 20) => {
    const q = new URLSearchParams({
      SearchTerm: searchTerm,
      Limit: String(limit),
    })
    if (userId) q.set('UserId', userId)
    return embyFetch<SearchHintResult>(`/emby/Search/Hints?${q.toString()}`)
  },

  toggleFavorite: (userId: string, itemId: string, isFavorite: boolean) =>
    embyFetch<UserItemDataDto>(
      `/emby/Users/${encodeURIComponent(userId)}/FavoriteItems/${encodeURIComponent(itemId)}`,
      { method: isFavorite ? 'DELETE' : 'POST' },
    ),

  togglePlayed: (userId: string, itemId: string, isPlayed: boolean) =>
    embyFetch<UserItemDataDto>(
      `/emby/Users/${encodeURIComponent(userId)}/PlayedItems/${encodeURIComponent(itemId)}`,
      { method: isPlayed ? 'DELETE' : 'POST' },
    ),

  /**
   * 请求播放信息。
   * 核心零转码策略：在 DeviceProfile.SubtitleProfiles 中将 ass / ssa / pgssub / vtt / srt 显式声明为 External，
   * 让服务端直接返回外挂字幕流 URL，交由客户端 JASSUB / PGS Canvas / Native VTT 渲染，不触发服务端视频烧录转码。
   */
  getPlaybackInfo: (
    itemId: string,
    userId: string,
    options?: {
      mediaSourceId?: string
      audioStreamIndex?: number
      subtitleStreamIndex?: number
      startTimeTicks?: number
    },
  ) => {
    const q = new URLSearchParams({ UserId: userId })
    const body = {
      UserId: userId,
      MediaSourceId: options?.mediaSourceId,
      AudioStreamIndex: options?.audioStreamIndex,
      SubtitleStreamIndex: options?.subtitleStreamIndex,
      StartTimeTicks: options?.startTimeTicks ?? 0,
      AutoOpenLiveStream: true,
      DeviceProfile: {
        Name: 'Ambra Modern Web Client (Zero-Transcode)',
        MaxStreamingBitrate: 140_000_000,
        MaxStaticBitrate: 140_000_000,
        DirectPlayProfiles: [
          {
            Container: 'mp4,m4v,webm,mkv,mov,ts',
            Type: 'Video',
            VideoCodec: 'h264,hevc,h265,vp8,vp9,av1',
            AudioCodec: 'aac,mp3,opus,flac,vorbis,ac3,eac3',
          },
          {
            Container: 'mp3,flac,aac,ogg,opus,wav,m4a',
            Type: 'Audio',
          },
        ],
        TranscodingProfiles: [
          {
            Container: 'ts',
            Type: 'Video',
            VideoCodec: 'h264',
            AudioCodec: 'aac,mp3',
            Protocol: 'hls',
            EstimateContentLength: false,
            EnableMpegtsM2TsMode: false,
            TranscodeSeekInfo: 'Auto',
            CopyTimestamps: false,
            Context: 'Streaming',
          },
        ],
        SubtitleProfiles: [
          { Format: 'ass', Method: 'External' },
          { Format: 'ssa', Method: 'External' },
          { Format: 'pgssub', Method: 'External' },
          { Format: 'pgs', Method: 'External' },
          { Format: 'sup', Method: 'External' },
          { Format: 'vtt', Method: 'External' },
          { Format: 'webvtt', Method: 'External' },
          { Format: 'srt', Method: 'External' },
          { Format: 'subrip', Method: 'External' },
        ],
      },
    }

    return embyFetch<PlaybackInfoResponse>(
      `/emby/Items/${encodeURIComponent(itemId)}/PlaybackInfo?${q.toString()}`,
      {
        method: 'POST',
        body: JSON.stringify(body),
      },
    )
  },

  getMediaAttachments: (itemId: string, mediaSourceId: string): Promise<MediaAttachmentInfo[]> => {
    if (isDemoMode() || !itemId || !mediaSourceId) {
      return Promise.resolve([])
    }
    return embyFetch<MediaAttachmentInfo[]>(
      `/emby/Videos/${encodeURIComponent(itemId)}/${encodeURIComponent(mediaSourceId)}/Attachments`,
    ).catch(() => [])
  },

  reportPlaying: (payload: {
    ItemId: string
    MediaSourceId?: string
    PlaySessionId?: string
    PositionTicks: number
    IsPaused: boolean
    IsMuted?: boolean
    VolumeLevel?: number
    AudioStreamIndex?: number
    SubtitleStreamIndex?: number
    PlayMethod?: string
  }) =>
    embyFetch<void>('/emby/Sessions/Playing', {
      method: 'POST',
      body: JSON.stringify(payload),
    }).catch(() => undefined),

  reportProgress: (payload: {
    ItemId: string
    MediaSourceId?: string
    PlaySessionId?: string
    PositionTicks: number
    IsPaused: boolean
    IsMuted?: boolean
    VolumeLevel?: number
    AudioStreamIndex?: number
    SubtitleStreamIndex?: number
    PlayMethod?: string
    EventName?: string
  }) =>
    embyFetch<void>('/emby/Sessions/Playing/Progress', {
      method: 'POST',
      body: JSON.stringify(payload),
    }).catch(() => undefined),

  reportStopped: (payload: {
    ItemId: string
    MediaSourceId?: string
    PlaySessionId?: string
    PositionTicks: number
    AudioStreamIndex?: number
    SubtitleStreamIndex?: number
  }) =>
    embyFetch<void>('/emby/Sessions/Playing/Stopped', {
      method: 'POST',
      body: JSON.stringify(payload),
    }).catch(() => undefined),

  refreshLibrary: (parentId?: string) => {
    const q = parentId ? `?ParentId=${encodeURIComponent(parentId)}` : ''
    return embyFetch<void>(`/emby/Library/Refresh${q}`, { method: 'POST' })
  },

  // ==================== Admin & Management APIs ====================

  getUser: (userId: string) =>
    embyFetch<UserDto>(`/emby/Users/${encodeURIComponent(userId)}`),

  getUsers: () => embyFetch<UserDto[]>('/emby/Users'),

  createUser: (payload: { Name: string; Password?: string; IsAdministrator?: boolean }) =>
    embyFetch<UserDto>('/emby/Users/New', {
      method: 'POST',
      body: JSON.stringify(payload),
    }),

  deleteUser: (userId: string) =>
    embyFetch<void>(`/emby/Users/${encodeURIComponent(userId)}`, {
      method: 'DELETE',
    }),

  updateUserPassword: (
    userId: string,
    payload: { CurrentPw?: string; NewPw?: string; ResetPassword?: boolean },
  ) =>
    embyFetch<void>(`/emby/Users/${encodeURIComponent(userId)}/Password`, {
      method: 'POST',
      body: JSON.stringify(payload),
    }),

  updateUserPolicy: (userId: string, policy: UserPolicy) =>
    embyFetch<void>(`/emby/Users/${encodeURIComponent(userId)}/Policy`, {
      method: 'POST',
      body: JSON.stringify(policy),
    }),

  updateUserConfiguration: (userId: string, config: UserConfiguration) =>
    embyFetch<void>(`/emby/Users/${encodeURIComponent(userId)}/Configuration`, {
      method: 'POST',
      body: JSON.stringify(config),
    }),

  getSessions: () => embyFetch<SessionInfo[]>('/emby/Sessions'),

  getActivityLogs: (startIndex = 0, limit = 50) => {
    const q = new URLSearchParams({
      StartIndex: String(startIndex),
      Limit: String(limit),
    })
    return embyFetch<QueryResult<ActivityLogEntry>>(
      `/emby/System/ActivityLog/Entries?${q.toString()}`,
    )
  },

  getVirtualFolders: () => embyFetch<VirtualFolderInfo[]>('/emby/Library/VirtualFolders'),

  createVirtualFolder: (payload: {
    Name: string
    CollectionType: string
    Paths: string[]
    refreshLibrary?: boolean
    saveLocalMetadata?: boolean
  }) => {
    const q = new URLSearchParams({
      name: payload.Name,
      collectionType: payload.CollectionType,
      refreshLibrary: String(payload.refreshLibrary ?? true),
    })
    return embyFetch<VirtualFolderInfo>(`/emby/Library/VirtualFolders?${q.toString()}`, {
      method: 'POST',
      body: JSON.stringify({
        Name: payload.Name,
        CollectionType: payload.CollectionType,
        Paths: payload.Paths,
        LibraryOptions: {
          PathInfos: payload.Paths.map((p) => ({ Path: p })),
          SaveLocalMetadata: payload.saveLocalMetadata ?? true,
        },
      }),
    })
  },

  updateVirtualFolderOptions: (id: string, libraryOptions: Record<string, unknown>) =>
    embyFetch<void>('/emby/Library/VirtualFolders/LibraryOptions', {
      method: 'POST',
      body: JSON.stringify({
        Id: id,
        LibraryOptions: libraryOptions,
      }),
    }),

  deleteVirtualFolder: (params: { name?: string; id?: string }) => {
    const q = new URLSearchParams()
    if (params.name) q.set('name', params.name)
    if (params.id) q.set('id', params.id)
    return embyFetch<void>(`/emby/Library/VirtualFolders?${q.toString()}`, {
      method: 'DELETE',
    })
  },

  renameVirtualFolder: (name: string, newName: string, id?: string) => {
    const q = new URLSearchParams({
      Name: name,
      NewName: newName,
    })
    if (id) q.set('Id', id)
    return embyFetch<void>(`/emby/Library/VirtualFolders/Name?${q.toString()}`, {
      method: 'POST',
      body: JSON.stringify({ Name: name, NewName: newName, ...(id ? { Id: id } : {}) }),
    })
  },

  addVirtualFolderPath: (name: string, path: string, refreshLibrary = false, id?: string) =>
    embyFetch<void>('/emby/Library/VirtualFolders/Paths', {
      method: 'POST',
      body: JSON.stringify({
        Name: name,
        Path: path,
        RefreshLibrary: refreshLibrary,
        ...(id ? { Id: id } : {}),
      }),
    }),

  removeVirtualFolderPath: (name: string, path: string, id?: string) => {
    const q = new URLSearchParams({
      Name: name,
      Path: path,
    })
    if (id) q.set('Id', id)
    return embyFetch<void>(`/emby/Library/VirtualFolders/Paths?${q.toString()}`, {
      method: 'DELETE',
    })
  },

  getServerConfig: () => embyFetch<ServerConfiguration>('/emby/System/Configuration'),

  updateServerConfig: (cfg: Partial<ServerConfiguration>) =>
    embyFetch<void>('/emby/System/Configuration', {
      method: 'POST',
      body: JSON.stringify(cfg),
    }),

  getDrives: () => embyFetch<FileSystemEntry[]>('/emby/Environment/Drives'),

  getDirectoryContents: (path: string, includeDirectories = true, includeFiles = false) => {
    const q = new URLSearchParams({
      path,
      includeDirectories: String(includeDirectories),
      includeFiles: String(includeFiles),
    })
    return embyFetch<FileSystemEntry[]>(
      `/emby/Environment/DirectoryContents?${q.toString()}`,
    )
  },

  getDevices: () => embyFetch<QueryResult<DeviceInfo>>('/emby/Devices'),

  deleteDevice: (deviceId: string) =>
    embyFetch<void>(`/emby/Devices?id=${encodeURIComponent(deviceId)}`, {
      method: 'DELETE',
    }),

  getApiKeys: () => embyFetch<QueryResult<ApiKeyInfo>>('/emby/Auth/Keys'),

  createApiKey: (appName: string) =>
    embyFetch<ApiKeyInfo>(`/emby/Auth/Keys?app=${encodeURIComponent(appName)}`, {
      method: 'POST',
      body: JSON.stringify({ App: appName, AppName: appName }),
    }),

  deleteApiKey: (key: string) =>
    embyFetch<void>(`/emby/Auth/Keys/${encodeURIComponent(key)}`, {
      method: 'DELETE',
    }),

  getPlugins: () => embyFetch<PluginInfo[]>('/emby/Plugins'),

  getPluginConfiguration: (pluginId: string) =>
    embyFetch<Record<string, unknown>>(
      `/emby/Plugins/${encodeURIComponent(pluginId)}/Configuration`,
    ),

  updatePluginConfiguration: (pluginId: string, config: Record<string, unknown>) =>
    embyFetch<void>(`/emby/Plugins/${encodeURIComponent(pluginId)}/Configuration`, {
      method: 'POST',
      body: JSON.stringify(config),
    }),

  getScheduledTasks: () => embyFetch<ScheduledTaskInfo[]>('/emby/ScheduledTasks'),

  runScheduledTask: (taskId: string) =>
    embyFetch<void>(`/emby/ScheduledTasks/Running/${encodeURIComponent(taskId)}`, {
      method: 'POST',
    }),

  stopScheduledTask: (taskId: string) =>
    embyFetch<void>(`/emby/ScheduledTasks/Running/${encodeURIComponent(taskId)}`, {
      method: 'DELETE',
    }),

  getServerLogs: async (): Promise<ServerLogFile[]> => {
    const res = await embyFetch<ServerLogFile[] | QueryResult<ServerLogFile>>(
      '/emby/System/Logs/Query',
    )
    if (Array.isArray(res)) return res
    if (res && Array.isArray(res.Items)) return res.Items
    return []
  },

  getServerLogContent: (name: string) =>
    embyFetch<string>(`/emby/System/Logs/${encodeURIComponent(name)}`, {
      headers: { Accept: 'text/plain, application/json' },
    }),

  // ==================== Media Metadata, Images & Rankings Admin APIs ====================

  updateItemMetadata: (itemId: string, payload: ItemMetadataUpdatePayload) =>
    embyFetch<void>(`/emby/Items/${encodeURIComponent(itemId)}`, {
      method: 'POST',
      body: JSON.stringify(payload),
    }),

  refreshItemMetadata: (
    itemId: string,
    replaceAllMetadata = false,
    replaceAllImages = false,
  ) => {
    const q = new URLSearchParams({
      Recursive: 'true',
      MetadataRefreshMode: replaceAllMetadata ? 'FullRefresh' : 'Default',
      ImageRefreshMode: replaceAllImages ? 'FullRefresh' : 'Default',
      ReplaceAllMetadata: String(replaceAllMetadata),
      ReplaceAllImages: String(replaceAllImages),
    })
    return embyFetch<void>(
      `/emby/Items/${encodeURIComponent(itemId)}/Refresh?${q.toString()}`,
      { method: 'POST' },
    )
  },

  deleteItem: (itemId: string) =>
    embyFetch<void>(`/emby/Items/${encodeURIComponent(itemId)}`, {
      method: 'DELETE',
    }),

  splitItemVersions: (itemId: string) =>
    embyFetch<void>(`/emby/Videos/${encodeURIComponent(itemId)}/AlternateSources`, {
      method: 'DELETE',
    }),

  mergeItemVersions: (itemIds: string[]) =>
    embyFetch<void>(
      `/emby/Videos/MergeVersions?Ids=${encodeURIComponent(itemIds.join(','))}`,
      { method: 'POST' },
    ),

  remoteSearch: (
    itemType: 'Movie' | 'Series',
    query: { Name?: string; Year?: number; ProviderIds?: Record<string, string> },
  ) =>
    embyFetch<RemoteSearchResult[]>(`/emby/Items/RemoteSearch/${itemType}`, {
      method: 'POST',
      body: JSON.stringify({
        SearchInfo: {
          Name: query.Name || undefined,
          Year: query.Year || undefined,
          ProviderIds: query.ProviderIds || undefined,
        },
      }),
    }),

  applyRemoteSearch: (
    itemId: string,
    result: RemoteSearchResult,
    replaceAllImages = true,
  ) => {
    const q = new URLSearchParams({
      ReplaceAllImages: String(replaceAllImages),
    })
    return embyFetch<void>(
      `/emby/Items/RemoteSearch/Apply/${encodeURIComponent(itemId)}?${q.toString()}`,
      {
        method: 'POST',
        body: JSON.stringify(result),
      },
    )
  },

  getItemImages: (itemId: string) =>
    embyFetch<ItemImageInfo[]>(`/emby/Items/${encodeURIComponent(itemId)}/Images`),

  getRemoteImages: (itemId: string, type?: 'Primary' | 'Backdrop') => {
    const q = new URLSearchParams()
    if (type) q.set('Type', type)
    const qs = q.toString()
    return embyFetch<RemoteImageResult>(
      `/emby/Items/${encodeURIComponent(itemId)}/RemoteImages${qs ? `?${qs}` : ''}`,
    )
  },

  downloadRemoteImage: (
    itemId: string,
    type: 'Primary' | 'Backdrop' | string,
    imageUrl: string,
  ) => {
    const q = new URLSearchParams({
      Type: type,
      ImageUrl: imageUrl,
    })
    return embyFetch<void>(
      `/emby/Items/${encodeURIComponent(itemId)}/RemoteImages/Download?${q.toString()}`,
      { method: 'POST' },
    )
  },

  uploadItemImage: (
    itemId: string,
    type: 'Primary' | 'Backdrop' | string,
    base64: string,
    contentType: string,
  ) => {
    const cleanBase64 = base64.replace(/^data:[^;]+;base64,/, '')
    return embyFetch<void>(
      `/emby/Items/${encodeURIComponent(itemId)}/Images/${encodeURIComponent(type)}`,
      {
        method: 'POST',
        headers: {
          'Content-Type': contentType || 'image/jpeg',
        },
        body: cleanBase64,
      },
    )
  },

  setItemImageByUrl: (
    itemId: string,
    type: 'Primary' | 'Backdrop' | string,
    url: string,
  ) =>
    embyFetch<void>(
      `/emby/Items/${encodeURIComponent(itemId)}/Images/${encodeURIComponent(type)}/0/Url`,
      {
        method: 'POST',
        body: JSON.stringify({ Url: url }),
      },
    ),

  deleteItemImage: (
    itemId: string,
    type: 'Primary' | 'Backdrop' | string,
    index = 0,
  ) =>
    embyFetch<void>(
      `/emby/Items/${encodeURIComponent(itemId)}/Images/${encodeURIComponent(type)}/${index}`,
      { method: 'DELETE' },
    ),

  getRankings: () => embyFetch<RankingsSummary>('/emby/System/Rankings'),

  getRankingsSummary: () => embyFetch<RankingsSummary>('/emby/System/Rankings'),

  refreshRankings: () =>
    embyFetch<RankingsRefreshResult>('/emby/System/Rankings/Refresh', {
      method: 'POST',
    }),

  getScanProgress: () => embyFetch<ScanProgressSnapshot>('/api/admin/scanner/progress'),

  pauseScan: () =>
    embyFetch<void>('/api/admin/scanner/pause', {
      method: 'POST',
    }),

  resumeScan: () =>
    embyFetch<void>('/api/admin/scanner/resume', {
      method: 'POST',
    }),

  cancelScan: () =>
    embyFetch<void>('/api/admin/scanner/cancel', {
      method: 'POST',
    }),

  lockItemFields: (itemId: string, fields: string[], lockData: boolean) =>
    embyFetch<{ item_id?: string; locked_fields?: string[]; lock_data?: boolean }>(
      `/api/admin/governance/items/${encodeURIComponent(itemId)}/lock`,
      {
        method: 'POST',
        body: JSON.stringify({
          LockedFields: fields,
          fields,
          LockData: lockData,
          lock_data: lockData,
        }),
      },
    ),

  writeItemNfo: (itemId: string) =>
    embyFetch<{
      ItemId?: string
      NfoPath?: string
      RootTag?: string
      BytesSize?: number
      item_id?: string
      path?: string
      written?: boolean
    }>(`/api/admin/governance/items/${encodeURIComponent(itemId)}/nfo`, {
      method: 'POST',
    }),

  // ==================== First-Run Setup Wizard APIs ====================

  getSetupStatus: () => embyFetch<SetupStatus>('/api/setup/status', {}, false),

  setupAdmin: (req: SetupAdminRequest) => {
    const auth = getStoredAuth()
    const headers: Record<string, string> = {}
    if (auth?.accessToken) {
      headers['X-Emby-Authorization'] = buildEmbyAuthHeader(auth.accessToken)
      headers['X-Emby-Token'] = auth.accessToken
    }
    return embyFetch<AuthenticationResult>(
      '/api/setup/admin',
      {
        method: 'POST',
        headers,
        body: JSON.stringify(req),
      },
      false,
    )
  },

  completeSetup: (req: SetupCompleteRequest) =>
    embyFetch<{ completed: boolean }>(
      '/api/setup/complete',
      {
        method: 'POST',
        body: JSON.stringify(req),
      },
      true,
    ),

  // ==================== v0.8.0 Emby Parity APIs ====================

  hideFromResume: (userId: string, itemId: string, hide = true) =>
    embyFetch<UserItemDataDto>(
      `/emby/Users/${encodeURIComponent(userId)}/Items/${encodeURIComponent(itemId)}/HideFromResume?Hide=${hide}`,
      { method: 'POST' },
    ),

  uploadSubtitle: (
    itemId: string,
    payload: { Language: string; Format: string; IsForced?: boolean; Data: string },
  ) =>
    embyFetch<void>(`/emby/Videos/${encodeURIComponent(itemId)}/Subtitles`, {
      method: 'POST',
      body: JSON.stringify(payload),
    }),

  deleteSubtitle: (itemId: string, index: number) =>
    embyFetch<void>(`/emby/Videos/${encodeURIComponent(itemId)}/Subtitles/${index}`, {
      method: 'DELETE',
    }),

  sendSessionPlaystateCommand: (
    sessionId: string,
    command: 'Pause' | 'Unpause' | 'PlayPause' | 'Stop' | string,
  ) =>
    embyFetch<void>(
      `/emby/Sessions/${encodeURIComponent(sessionId)}/Playing/${encodeURIComponent(command)}`,
      { method: 'POST' },
    ),

  sendSessionMessage: (
    sessionId: string,
    payload: { Header: string; Text: string; TimeoutMs?: number },
  ) =>
    embyFetch<void>(`/emby/Sessions/${encodeURIComponent(sessionId)}/Message`, {
      method: 'POST',
      body: JSON.stringify({
        Header: payload.Header,
        Text: payload.Text,
        TimeoutMs: payload.TimeoutMs ?? 5000,
      }),
    }),

  updateTaskTriggers: (taskId: string, triggers: TaskTriggerInfo[]) =>
    embyFetch<void>(`/emby/ScheduledTasks/${encodeURIComponent(taskId)}/Triggers`, {
      method: 'POST',
      body: JSON.stringify(triggers),
    }),

  getLiveTvInfo: () => embyFetch<LiveTvInfo>('/emby/LiveTv/Info'),

  getLiveTvChannels: (limit = 100) =>
    embyFetch<QueryResult<LiveTvChannelInfo>>(`/emby/LiveTv/Channels?Limit=${limit}`),

  getTunerHosts: () => embyFetch<LiveTvTunerHostInfo[]>('/emby/LiveTv/TunerHosts'),

  addTunerHost: (host: LiveTvTunerHostInfo) =>
    embyFetch<LiveTvTunerHostInfo>('/emby/LiveTv/TunerHosts', {
      method: 'POST',
      body: JSON.stringify(host),
    }),

  deleteTunerHost: (id: string) =>
    embyFetch<void>(`/emby/LiveTv/TunerHosts?id=${encodeURIComponent(id)}`, {
      method: 'DELETE',
    }),

  getListingProviders: () =>
    embyFetch<LiveTvListingProviderInfo[]>('/emby/LiveTv/ListingProviders'),

  addListingProvider: (provider: LiveTvListingProviderInfo) =>
    embyFetch<LiveTvListingProviderInfo>('/emby/LiveTv/ListingProviders', {
      method: 'POST',
      body: JSON.stringify(provider),
    }),

  deleteListingProvider: (id: string) =>
    embyFetch<void>(`/emby/LiveTv/ListingProviders?id=${encodeURIComponent(id)}`, {
      method: 'DELETE',
    }),

  // ==================== v0.9.0 Backup & Extras APIs ====================

  getLocalTrailers: (userId: string, itemId: string) =>
    embyFetch<BaseItemDto[]>(
      `/emby/Users/${encodeURIComponent(userId)}/Items/${encodeURIComponent(itemId)}/LocalTrailers`,
    ),

  getSpecialFeatures: (userId: string, itemId: string) =>
    embyFetch<BaseItemDto[]>(
      `/emby/Users/${encodeURIComponent(userId)}/Items/${encodeURIComponent(itemId)}/SpecialFeatures`,
    ),

  getBackupInfo: () => embyFetch<BackupInfoResponse>('/emby/BackupRestore/BackupInfo'),

  createBackup: () =>
    embyFetch<BackupFileInfo>('/emby/BackupRestore/Backup', {
      method: 'POST',
    }),

  restoreBackup: (name: string) =>
    embyFetch<BackupRestoreResult>('/emby/BackupRestore/Restore', {
      method: 'POST',
      body: JSON.stringify({ Name: name }),
    }),

  // ==================== v0.9.1 Theme Songs, ForgotPassword & Live TV Stream ====================

  getThemeSongs: (itemId: string, inheritFromParent = true) =>
    embyFetch<QueryResult<BaseItemDto>>(
      `/emby/Items/${encodeURIComponent(itemId)}/ThemeSongs?InheritFromParent=${inheritFromParent}`,
    ),

  forgotPassword: (enteredUsername: string) =>
    embyFetch<{ Action?: string; PinFile?: string; PinExpirationDate?: string }>(
      '/emby/Users/ForgotPassword',
      {
        method: 'POST',
        body: JSON.stringify({ EnteredUsername: enteredUsername }),
      },
      false,
    ),

  forgotPasswordPin: (pin: string) =>
    embyFetch<{ Success: boolean; UsersReset?: string[] }>(
      '/emby/Users/ForgotPassword/Pin',
      {
        method: 'POST',
        body: JSON.stringify({ Pin: pin }),
      },
      false,
    ),

  getLiveTvPrograms: (channelIds?: string[], limit = 100) => {
    const q = new URLSearchParams({ Limit: String(limit) })
    if (channelIds && channelIds.length > 0) {
      q.set('ChannelIds', channelIds.join(','))
    }
    return embyFetch<
      QueryResult<{
        Id: string
        ChannelId: string
        Name: string
        Overview?: string
        StartDate?: string
        EndDate?: string
      }>
    >(`/emby/LiveTv/Programs?${q.toString()}`)
  },

  // ==================== v0.9.2 DVR, Webhooks, Parental & InstantMix APIs ====================

  getLiveTvTimers: () => embyFetch<QueryResult<LiveTvTimerInfo>>('/emby/LiveTv/Timers'),

  createLiveTvTimer: (timer: {
    ChannelId: string
    ChannelName?: string
    ProgramId?: string
    Name: string
    Overview?: string
    StartDate: string
    EndDate: string
    PrePaddingSeconds?: number
    PostPaddingSeconds?: number
  }) =>
    embyFetch<LiveTvTimerInfo>('/emby/LiveTv/Timers', {
      method: 'POST',
      body: JSON.stringify(timer),
    }),

  deleteLiveTvTimer: (id: string) =>
    embyFetch<void>(`/emby/LiveTv/Timers/${encodeURIComponent(id)}`, {
      method: 'DELETE',
    }),

  getLiveTvRecordings: () =>
    embyFetch<QueryResult<LiveTvRecordingInfo>>('/emby/LiveTv/Recordings'),

  deleteLiveTvRecording: (id: string) =>
    embyFetch<void>(`/emby/LiveTv/Recordings/${encodeURIComponent(id)}`, {
      method: 'DELETE',
    }),

  getWebhooks: () => embyFetch<WebhookInfo[]>('/emby/Webhooks'),

  saveWebhook: (webhook: Partial<WebhookInfo> & { Name: string; Url: string; Events: string[] }) =>
    embyFetch<WebhookInfo>('/emby/Webhooks', {
      method: 'POST',
      body: JSON.stringify(webhook),
    }),

  deleteWebhook: (id: string) =>
    embyFetch<void>(`/emby/Webhooks/${encodeURIComponent(id)}`, {
      method: 'DELETE',
    }),

  testWebhook: (payload: { Id?: string; Url?: string; Secret?: string; Name?: string }) =>
    embyFetch<WebhookTestResult>('/emby/Webhooks/Test', {
      method: 'POST',
      body: JSON.stringify(payload),
    }),

  getParentalRatings: () => embyFetch<ParentalRatingOption[]>('/emby/Localization/ParentalRatings'),

  getInstantMix: (itemId: string, userId: string, limit = 50) =>
    embyFetch<QueryResult<BaseItemDto>>(
      `/emby/Items/${encodeURIComponent(itemId)}/InstantMix?UserId=${encodeURIComponent(userId)}&Limit=${limit}`,
    ),

  // ==================== v0.9.3 Notifications & QuickConnect APIs ====================

  getNotifications: (userId: string, startIndex = 0, limit = 20) =>
    embyFetch<NotificationResult>(
      `/emby/Notifications/${encodeURIComponent(userId)}?StartIndex=${startIndex}&Limit=${limit}`,
    ),

  getNotificationsSummary: (userId: string) =>
    embyFetch<NotificationsSummary>(
      `/emby/Notifications/${encodeURIComponent(userId)}/Summary`,
    ),

  markNotificationsRead: (userId: string, ids?: string[]) => {
    const qs = ids && ids.length > 0 ? `?Ids=${encodeURIComponent(ids.join(','))}` : ''
    return embyFetch<void>(`/emby/Notifications/${encodeURIComponent(userId)}/Read${qs}`, {
      method: 'POST',
      body: JSON.stringify({ Ids: ids ?? [] }),
    })
  },

  sendAdminNotification: (payload: {
    Name: string
    Description?: string
    Url?: string
    Level?: 'Normal' | 'Warning' | 'Error'
    UserId?: string
  }) =>
    embyFetch<NotificationItem>('/emby/Notifications/Admin', {
      method: 'POST',
      body: JSON.stringify(payload),
    }),

  getQuickConnectEnabled: () => embyFetch<boolean>('/emby/QuickConnect/Enabled', {}, false),

  quickConnectInitiate: () =>
    embyFetch<QuickConnectResult>(
      '/emby/QuickConnect/Initiate',
      { method: 'POST' },
      false,
    ),

  quickConnectConnect: (secret: string) =>
    embyFetch<QuickConnectResult>(
      `/emby/QuickConnect/Connect?secret=${encodeURIComponent(secret)}`,
      {},
      false,
    ),

  quickConnectAuthorize: (code: string) =>
    embyFetch<boolean>(
      `/emby/QuickConnect/Authorize?code=${encodeURIComponent(code)}`,
      {
        method: 'POST',
        body: JSON.stringify({ Code: code }),
      },
      true,
    ),

  authenticateWithQuickConnect: (secret: string) =>
    embyFetch<AuthenticationResult>(
      '/emby/Users/AuthenticateWithQuickConnect',
      {
        method: 'POST',
        body: JSON.stringify({ Secret: secret }),
      },
      false,
    ),
}

export function getLiveTvChannelStreamUrl(channelId: string): string {
  return resolveApiUrl(`/emby/LiveTv/Channels/${encodeURIComponent(channelId)}/Stream`)
}

export function getLiveTvRecordingStreamUrl(recordingId: string): string {
  return resolveApiUrl(`/emby/LiveTv/LiveRecordings/${encodeURIComponent(recordingId)}/stream`)
}

export function getChapterImageUrl(
  itemId: string,
  chapterIndex: number,
  tag?: string,
  maxWidth = 480,
): string {
  if (isDemoMode()) {
    return getCachedDemoServer()?.getDemoImageUrl(itemId, 'Backdrop') ?? ''
  }
  const q = new URLSearchParams({ maxWidth: String(maxWidth), quality: '85' })
  if (tag) q.set('tag', tag)
  return resolveApiUrl(
    `/emby/Items/${encodeURIComponent(itemId)}/Images/Chapter/${chapterIndex}?${q.toString()}`,
  )
}
