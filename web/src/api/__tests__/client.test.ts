import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  api,
  ApiError,
  buildEmbyAuthHeader,
  clearAuth,
  embyFetch,
  getBackdropUrl,
  getDeviceId,
  getImageUrl,
  getStoredAuth,
  resolveApiUrl,
  saveAuth,
} from '../client'

function mockJsonResponse(data: unknown, status = 200, contentType = 'application/json') {
  return {
    ok: status >= 200 && status < 300,
    status,
    headers: new Headers({ 'content-type': contentType }),
    json: async () => data,
    text: async () => (typeof data === 'string' ? data : JSON.stringify(data)),
  } as Response
}

const TEST_AUTH = {
  accessToken: 'secret-token-123',
  userId: 'user-admin-1',
  serverId: 'srv-001',
  userName: 'admin',
  isAdmin: true,
  deviceId: 'dev-001',
}

describe('api/client - 凭证管理、请求头注入与 401 处理', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('持久化 DeviceId 与 AuthSession，并生成标准 X-Emby-Authorization 头', () => {
    const devId = getDeviceId()
    expect(devId).toMatch(/^ambra-web-/)
    expect(getDeviceId()).toBe(devId)

    expect(getStoredAuth()).toBeNull()
    saveAuth(TEST_AUTH)
    expect(getStoredAuth()).toEqual(TEST_AUTH)

    const authHeader = buildEmbyAuthHeader(TEST_AUTH.accessToken)
    expect(authHeader).toContain('MediaBrowser ')
    expect(authHeader).toContain('Client="Ambra Web"')
    expect(authHeader).toContain(`DeviceId="${devId}"`)
    expect(authHeader).toContain(`Token="${TEST_AUTH.accessToken}"`)

    clearAuth()
    expect(getStoredAuth()).toBeNull()
  })

  it('embyFetch 注入 X-Emby-Token 与 X-Emby-Authorization 请求头', async () => {
    saveAuth(TEST_AUTH)
    const fetchSpy = vi
      .spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(mockJsonResponse({ ServerName: 'Ambra NAS' }))

    const res = await embyFetch<{ ServerName: string }>('/emby/System/Info')
    expect(res.ServerName).toBe('Ambra NAS')

    expect(fetchSpy).toHaveBeenCalledTimes(1)
    const [url, init] = fetchSpy.mock.calls[0]
    expect(url).toBe('/emby/System/Info')
    const headers = init?.headers as Headers
    expect(headers.get('X-Emby-Token')).toBe(TEST_AUTH.accessToken)
    expect(headers.get('X-Emby-Authorization')).toContain(`Token="${TEST_AUTH.accessToken}"`)
    expect(headers.get('Accept')).toBe('application/json')
  })

  it('未登录访问受保护接口直接抛出 ApiError(401) 且不发送网络请求', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch')
    await expect(api.getSystemInfo()).rejects.toMatchObject({
      status: 401,
    })
    expect(fetchSpy).not.toHaveBeenCalled()
  })

  it('服务端返回 401 时清空本地登录态并派发 ambra:unauthorized 事件', async () => {
    saveAuth(TEST_AUTH)
    const onUnauthorized = vi.fn()
    window.addEventListener('ambra:unauthorized', onUnauthorized)

    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(mockJsonResponse('Unauthorized', 401))

    await expect(api.getUser(TEST_AUTH.userId)).rejects.toBeInstanceOf(ApiError)
    expect(getStoredAuth()).toBeNull()
    expect(onUnauthorized).toHaveBeenCalledTimes(1)

    window.removeEventListener('ambra:unauthorized', onUnauthorized)
  })

  it('resolveApiUrl / getImageUrl / getBackdropUrl 正确拼接 api_key 与参数', () => {
    saveAuth(TEST_AUTH)
    expect(resolveApiUrl('/emby/Videos/1/stream.mp4')).toBe(
      '/emby/Videos/1/stream.mp4?api_key=secret-token-123',
    )
    expect(resolveApiUrl('/emby/Videos/1/stream.mp4?Static=true')).toBe(
      '/emby/Videos/1/stream.mp4?Static=true&api_key=secret-token-123',
    )
    expect(getImageUrl('item-1', 'Primary', { maxWidth: 400, tag: 't1' })).toContain(
      '/emby/Items/item-1/Images/Primary?',
    )
    expect(
      getBackdropUrl({
        Id: 'm1',
        Name: 'Movie',
        Type: 'Movie',
        BackdropImageTags: ['bg-tag'],
      }),
    ).toContain('/emby/Items/m1/Images/Backdrop?')
  })
})

describe('api/client - 核心媒体与 PlaybackInfo 零转码策略', () => {
  beforeEach(() => {
    localStorage.clear()
    saveAuth(TEST_AUTH)
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('authenticateByName 无需登录态即可提交用户名密码', async () => {
    clearAuth()
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      mockJsonResponse({
        AccessToken: 'new-tok',
        ServerId: 'srv-1',
        User: { Id: 'u1', Name: 'alice', ServerId: 'srv-1' },
      }),
    )

    const result = await api.authenticateByName('alice', 'pass123')
    expect(result.AccessToken).toBe('new-tok')
    const [url, init] = fetchSpy.mock.calls[0]
    expect(url).toBe('/emby/Users/AuthenticateByName')
    expect(init?.method).toBe('POST')
    expect(JSON.parse(String(init?.body))).toEqual({ Username: 'alice', Pw: 'pass123' })
  })

  it('getPlaybackInfo 显式声明 ass / ssa / pgssub / pgs / sup / vtt / srt 为 External 零转码', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      mockJsonResponse({ MediaSources: [], PlaySessionId: 'ps-1' }),
    )

    await api.getPlaybackInfo('movie-42', TEST_AUTH.userId, {
      mediaSourceId: 'ms-1',
      audioStreamIndex: 1,
      subtitleStreamIndex: 2,
    })

    expect(fetchSpy).toHaveBeenCalledTimes(1)
    const [url, init] = fetchSpy.mock.calls[0]
    expect(url).toBe(`/emby/Items/movie-42/PlaybackInfo?UserId=${TEST_AUTH.userId}`)
    expect(init?.method).toBe('POST')

    const body = JSON.parse(String(init?.body))
    const subtitleProfiles: Array<{ Format: string; Method: string }> =
      body.DeviceProfile.SubtitleProfiles

    const externalFormats = ['ass', 'ssa', 'pgssub', 'pgs', 'sup', 'vtt', 'webvtt', 'srt', 'subrip']
    for (const fmt of externalFormats) {
      expect(subtitleProfiles).toContainEqual({ Format: fmt, Method: 'External' })
    }
  })

  it('媒体库与用户视图相关接口调用路径与方法正确', async () => {
    const fetchSpy = vi
      .spyOn(globalThis, 'fetch')
      .mockImplementation(async () => mockJsonResponse({ Items: [], TotalRecordCount: 0 }))

    await api.getUserViews('u1')
    await api.getResumeItems('u1', 6)
    await api.getLatestItems('u1', 'lib-1', 10)
    await api.getItems('u1', { parentId: 'lib-1', searchTerm: '星际', limit: 20 })
    await api.toggleFavorite('u1', 'item-9', false)
    await api.togglePlayed('u1', 'item-9', true)
    await api.refreshLibrary('lib-1')

    const calls = fetchSpy.mock.calls.map(([u, init]) => [u, init?.method || 'GET'])
    expect(calls[0]).toEqual(['/emby/Users/u1/Views', 'GET'])
    expect(calls[1][0]).toContain('/emby/Users/u1/Items/Resume?')
    expect(calls[2][0]).toContain('/emby/Users/u1/Items/Latest?')
    expect(calls[3][0]).toContain('/emby/Users/u1/Items?')
    expect(calls[4]).toEqual(['/emby/Users/u1/FavoriteItems/item-9', 'POST'])
    expect(calls[5]).toEqual(['/emby/Users/u1/PlayedItems/item-9', 'DELETE'])
    expect(calls[6]).toEqual(['/emby/Library/Refresh?ParentId=lib-1', 'POST'])
  })
})

describe('api/client - 管理控制台全部 API 方法', () => {
  beforeEach(() => {
    localStorage.clear()
    saveAuth(TEST_AUTH)
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('覆盖系统信息、用户管理、会话日志、媒体库目录、设备密钥、插件与计划任务全部端点', async () => {
    const fetchSpy = vi
      .spyOn(globalThis, 'fetch')
      .mockImplementation(async (input) => {
        const url = String(input)
        if (url.startsWith('/emby/System/Logs/server.log')) {
          return mockJsonResponse('[INFO] Ambra started', 200, 'text/plain')
        }
        return mockJsonResponse({}, 200)
      })

    await api.getSystemInfo()
    await api.getUser('u1')
    await api.getUsers()
    await api.createUser({ Name: 'bob', Password: 'pwd', IsAdministrator: false })
    await api.deleteUser('u2')
    await api.updateUserPassword('u2', { NewPw: 'newpwd' })
    await api.updateUserPolicy('u2', { IsAdministrator: true })
    await api.getSessions()
    await api.getActivityLogs(0, 25)
    await api.getVirtualFolders()
    await api.createVirtualFolder({ Name: '电影', CollectionType: 'movies', Paths: ['/media/movies'] })
    await api.deleteVirtualFolder({ name: '电影', id: 'vf-1' })
    await api.getDrives()
    await api.getDirectoryContents('/media', true, false)
    await api.getDevices()
    await api.deleteDevice('dev-9')
    await api.getApiKeys()
    await api.createApiKey('Infuse')
    await api.deleteApiKey('key-abc')
    await api.getPlugins()
    await api.getPluginConfiguration('tmdb')
    await api.updatePluginConfiguration('tmdb', { ApiKey: 'xyz' })
    await api.getScheduledTasks()
    await api.runScheduledTask('task-scan')
    await api.getServerLogs()
    const logText = await api.getServerLogContent('server.log')
    expect(logText).toBe('[INFO] Ambra started')

    const endpoints = fetchSpy.mock.calls.map(([u, init]) => `${init?.method || 'GET'} ${u}`)
    expect(endpoints).toEqual([
      'GET /emby/System/Info',
      'GET /emby/Users/u1',
      'GET /emby/Users',
      'POST /emby/Users/New',
      'DELETE /emby/Users/u2',
      'POST /emby/Users/u2/Password',
      'POST /emby/Users/u2/Policy',
      'GET /emby/Sessions',
      'GET /emby/System/ActivityLog/Entries?StartIndex=0&Limit=25',
      'GET /emby/Library/VirtualFolders',
      'POST /emby/Library/VirtualFolders?name=%E7%94%B5%E5%BD%B1&collectionType=movies&refreshLibrary=true',
      'DELETE /emby/Library/VirtualFolders?name=%E7%94%B5%E5%BD%B1&id=vf-1',
      'GET /emby/Environment/Drives',
      'GET /emby/Environment/DirectoryContents?path=%2Fmedia&includeDirectories=true&includeFiles=false',
      'GET /emby/Devices',
      'DELETE /emby/Devices?id=dev-9',
      'GET /emby/Auth/Keys',
      'POST /emby/Auth/Keys?app=Infuse',
      'DELETE /emby/Auth/Keys/key-abc',
      'GET /emby/Plugins',
      'GET /emby/Plugins/tmdb/Configuration',
      'POST /emby/Plugins/tmdb/Configuration',
      'GET /emby/ScheduledTasks',
      'POST /emby/ScheduledTasks/Running/task-scan',
      'GET /emby/System/Logs/Query',
      'GET /emby/System/Logs/server.log',
    ])
  })

  it('覆盖元数据编辑、图片管理、远端识别、多版本拆分合并与榜单管理全部端点', async () => {
    const fetchSpy = vi
      .spyOn(globalThis, 'fetch')
      .mockImplementation(async () => mockJsonResponse({}, 200))

    await api.updateItemMetadata('item-1', { Name: '测试影片' })
    await api.refreshItemMetadata('item-1', true)
    await api.deleteItem('item-1')
    await api.splitItemVersions('item-1')
    await api.mergeItemVersions(['item-1', 'item-2'])
    await api.remoteSearch('Movie', { Name: '盗梦空间', Year: 2010 })
    await api.applyRemoteSearch('item-1', { Name: '盗梦空间' }, true)
    await api.getItemImages('item-1')
    await api.getRemoteImages('item-1', 'Primary')
    await api.downloadRemoteImage('item-1', 'Primary', 'https://img.test/a.jpg')
    await api.uploadItemImage('item-1', 'Primary', 'data:image/png;base64,AAAA', 'image/png')
    await api.setItemImageByUrl('item-1', 'Backdrop', 'https://img.test/b.jpg')
    await api.deleteItemImage('item-1', 'Primary', 0)
    await api.getRankings()
    await api.refreshRankings()

    const endpoints = fetchSpy.mock.calls.map(([u, init]) => `${init?.method || 'GET'} ${u}`)
    expect(endpoints).toEqual([
      'POST /emby/Items/item-1',
      'POST /emby/Items/item-1/Refresh?Recursive=true&MetadataRefreshMode=FullRefresh&ImageRefreshMode=FullRefresh&ReplaceAllMetadata=true&ReplaceAllImages=false',
      'DELETE /emby/Items/item-1',
      'DELETE /emby/Videos/item-1/AlternateSources',
      'POST /emby/Videos/MergeVersions?Ids=item-1%2Citem-2',
      'POST /emby/Items/RemoteSearch/Movie',
      'POST /emby/Items/RemoteSearch/Apply/item-1?ReplaceAllImages=true',
      'GET /emby/Items/item-1/Images',
      'GET /emby/Items/item-1/RemoteImages?Type=Primary',
      'POST /emby/Items/item-1/RemoteImages/Download?Type=Primary&ImageUrl=https%3A%2F%2Fimg.test%2Fa.jpg',
      'POST /emby/Items/item-1/Images/Primary',
      'POST /emby/Items/item-1/Images/Backdrop/Url',
      'DELETE /emby/Items/item-1/Images/Primary/0',
      'GET /emby/System/Rankings',
      'POST /emby/System/Rankings/Refresh',
    ])
  })
})
