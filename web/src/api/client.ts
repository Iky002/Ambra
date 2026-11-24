import { isDemoMode } from '../demo/demoMode'
import {
  demoFetch,
  getDemoFallbackImageUrl,
  getDemoImageUrl,
  getDemoUserImageUrl,
} from '../demo/mockServer'
import type {
  ActivityLogEntry,
  ApiKeyInfo,
  AuthSession,
  AuthenticationResult,
  BaseItemDto,
  DeviceInfo,
  FileSystemEntry,
  ItemImageInfo,
  ItemMetadataUpdatePayload,
  MediaAttachmentInfo,
  NameIdPair,
  PlaybackInfoResponse,
  PluginInfo,
  PublicSystemInfo,
  QueryResult,
  RankingsRefreshResult,
  RankingsSummary,
  RemoteImageResult,
  RemoteSearchResult,
  ScanProgressSnapshot,
  ScheduledTaskInfo,
  SearchHintResult,
  ServerLogFile,
  SessionInfo,
  SetupAdminRequest,
  SetupCompleteRequest,
  SetupStatus,
  SystemInfo,
  UserDto,
  UserItemDataDto,
  UserPolicy,
  VirtualFolderInfo,
} from './types'

const STORAGE_KEY = 'ambra_web_auth_v1'
const DEVICE_ID_KEY = 'ambra_web_device_id'

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
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return null
    const parsed = JSON.parse(raw) as AuthSession
    if (!parsed.accessToken || !parsed.userId) return null
    return parsed
  } catch {
    return null
  }
}

export function saveAuth(auth: AuthSession): void {
  localStorage.setItem(STORAGE_KEY, JSON.stringify(auth))
}

export function clearAuth(): void {
  localStorage.removeItem(STORAGE_KEY)
}

export function buildEmbyAuthHeader(token?: string): string {
  const deviceId = getDeviceId()
  const parts = [
    'Client="Ambra Web"',
    'Device="Web Browser"',
    `DeviceId="${deviceId}"`,
    'Version="0.1.0"',
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
    return demoFetch<T>(normalizedPath, options, requireAuth)
  }

  const auth = getStoredAuth()
  const headers = new Headers(options.headers || {})

  if (!headers.has('Content-Type') && options.body && typeof options.body === 'string') {
    headers.set('Content-Type', 'application/json')
  }
  headers.set('Accept', 'application/json')

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
  const base = hasProtocol
    ? urlOrPath
    : urlOrPath.startsWith('/')
      ? urlOrPath
      : `/${urlOrPath}`

  if (!token || base.includes('api_key=') || base.includes('X-Emby-Token=')) {
    return base
  }
  const sep = base.includes('?') ? '&' : '?'
  return `${base}${sep}api_key=${encodeURIComponent(token)}`
}

export function getImageUrl(
  itemId: string,
  type: 'Primary' | 'Backdrop' | 'Thumb' | 'Logo' | 'Banner' = 'Primary',
  options?: { maxWidth?: number; maxHeight?: number; tag?: string; quality?: number },
): string {
  if (!itemId) return ''
  if (isDemoMode()) {
    return getDemoImageUrl(itemId, type)
  }
  const params = new URLSearchParams()
  if (options?.maxWidth) params.set('maxWidth', String(options.maxWidth))
  if (options?.maxHeight) params.set('maxHeight', String(options.maxHeight))
  if (options?.tag) params.set('tag', options.tag)
  params.set('quality', String(options?.quality ?? 88))
  const auth = getStoredAuth()
  if (auth?.accessToken) params.set('api_key', auth.accessToken)
  return `/emby/Items/${encodeURIComponent(itemId)}/Images/${type}?${params.toString()}`
}

export function getFallbackImageUrl(
  itemId: string,
  type: 'Primary' | 'Backdrop' | 'Thumb' | 'Logo' | 'Banner' = 'Primary',
): string | null {
  if (!itemId || !isDemoMode()) return null
  return getDemoFallbackImageUrl(itemId, type)
}

export function getUserImageUrl(userId: string, tag?: string): string {
  if (isDemoMode()) {
    return getDemoUserImageUrl(userId)
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
      tags?: string
      personIds?: string
      person?: string
      startIndex?: number
      limit?: number
      recursive?: boolean
      searchTerm?: string
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
    if (params.tags) q.set('Tags', params.tags)
    if (params.personIds) q.set('PersonIds', params.personIds)
    if (params.person) q.set('Person', params.person)
    if (params.startIndex !== undefined) q.set('StartIndex', String(params.startIndex))
    if (params.limit !== undefined) q.set('Limit', String(params.limit))
    if (params.recursive !== false) q.set('Recursive', 'true')
    if (params.searchTerm) q.set('SearchTerm', params.searchTerm)
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
        },
      }),
    })
  },

  deleteVirtualFolder: (params: { name?: string; id?: string }) => {
    const q = new URLSearchParams()
    if (params.name) q.set('name', params.name)
    if (params.id) q.set('id', params.id)
    return embyFetch<void>(`/emby/Library/VirtualFolders?${q.toString()}`, {
      method: 'DELETE',
    })
  },

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

  getServerLogs: () => embyFetch<ServerLogFile[]>('/emby/System/Logs/Query'),

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

  refreshItemMetadata: (itemId: string, replaceAllMetadata = false) => {
    const q = new URLSearchParams({
      Recursive: 'true',
      MetadataRefreshMode: replaceAllMetadata ? 'FullRefresh' : 'Default',
      ImageRefreshMode: replaceAllMetadata ? 'FullRefresh' : 'Default',
      ReplaceAllMetadata: String(replaceAllMetadata),
      ReplaceAllImages: 'false',
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
      `/emby/Items/${encodeURIComponent(itemId)}/Images/${encodeURIComponent(type)}/Url`,
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

  // ==================== First-Run Setup Wizard APIs ====================

  getSetupStatus: () => embyFetch<SetupStatus>('/api/setup/status', {}, false),

  setupAdmin: (req: SetupAdminRequest) =>
    embyFetch<AuthenticationResult>(
      '/api/setup/admin',
      {
        method: 'POST',
        body: JSON.stringify(req),
      },
      false,
    ),

  completeSetup: (req: SetupCompleteRequest) =>
    embyFetch<{ completed: boolean }>(
      '/api/setup/complete',
      {
        method: 'POST',
        body: JSON.stringify(req),
      },
      true,
    ),
}
