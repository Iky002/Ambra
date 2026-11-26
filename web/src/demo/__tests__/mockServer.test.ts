import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  api,
  clearAuth,
  embyFetch,
  getFallbackImageUrl,
  getImageUrl,
  getUserImageUrl,
  resolveApiUrl,
  saveAuth,
} from '../../api/client'
import { isDemoMode, setDemoModeOverride } from '../demoMode'
import { resetDemoStore } from '../mockServer'

describe('Demo Mock 引擎 - Serverless 完整闭环测试', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    resetDemoStore()
    setDemoModeOverride(true)
  })

  afterEach(() => {
    setDemoModeOverride(null)
    localStorage.clear()
    sessionStorage.clear()
    vi.restoreAllMocks()
  })

  async function loginAsDemo(username: 'admin' | 'guest' = 'admin') {
    const res = await api.authenticateByName(username, '')
    saveAuth({
      accessToken: res.AccessToken,
      userId: res.User.Id,
      serverId: res.ServerId,
      userName: res.User.Name,
      isAdmin: Boolean(res.User.Policy?.IsAdministrator),
      deviceId: 'test-dev',
    })
    return res
  }

  it('Demo 模式触发逻辑与 TMDB 官方海报/剧照及离线 SVG 回退生成器正常生效，不发真实网络请求', async () => {
    expect(isDemoMode()).toBe(true)
    const fetchSpy = vi.spyOn(globalThis, 'fetch')

    const pub = await api.getPublicSystemInfo()
    expect(pub.ServerName).toContain('Ambra Serverless Demo')

    // 电影/剧集/季/集返回真实 TMDB 官方图片 URL
    const poster = getImageUrl('item-wandering-earth-2', 'Primary')
    const backdrop = getImageUrl('item-wandering-earth-2', 'Backdrop')
    const seasonPoster = getImageUrl('season-tb-1', 'Primary')
    const epThumb = getImageUrl('ep-tb-s1e1', 'Primary')
    expect(poster.startsWith('https://image.tmdb.org/t/p/w500/')).toBe(true)
    expect(backdrop.startsWith('https://image.tmdb.org/t/p/w1280/')).toBe(true)
    expect(seasonPoster.startsWith('https://image.tmdb.org/t/p/w500/')).toBe(true)
    expect(epThumb.startsWith('https://image.tmdb.org/t/p/w1280/')).toBe(true)

    // 离线回退图片返回 SVG Data URL，断网环境优雅降级
    const fallbackPoster = getFallbackImageUrl('item-wandering-earth-2', 'Primary')
    const fallbackBackdrop = getFallbackImageUrl('item-wandering-earth-2', 'Backdrop')
    expect(fallbackPoster?.startsWith('data:image/svg+xml;charset=utf-8,')).toBe(true)
    expect(fallbackBackdrop?.startsWith('data:image/svg+xml;charset=utf-8,')).toBe(true)

    // 媒体库封面与用户头像继续使用精致本地 SVG
    const libThumb = getImageUrl('lib-movies', 'Primary')
    const avatar = getUserImageUrl('user-demo-admin')
    expect(libThumb.startsWith('data:image/svg+xml;charset=utf-8,')).toBe(true)
    expect(avatar.startsWith('data:image/svg+xml;charset=utf-8,')).toBe(true)

    // 演职员头像使用真实 TMDB w185 URL，并提供离线 SVG 回退
    const actorAvatar = getImageUrl('p-tim-robbins', 'Primary')
    const actorFallback = getFallbackImageUrl('p-tim-robbins', 'Primary')
    expect(actorAvatar.startsWith('https://image.tmdb.org/t/p/w185/')).toBe(true)
    expect(actorFallback?.startsWith('data:image/svg+xml;charset=utf-8,')).toBe(true)

    // data: URL 不应被 resolveApiUrl 篡改
    expect(resolveApiUrl(libThumb)).toBe(libThumb)
    expect(fetchSpy).not.toHaveBeenCalled()
  })

  it('扩充经典 Top 250 与实时热门作品（总计 20 部影片/剧集）并支持 GET /Tags 与 Tags 筛选', async () => {
    const authRes = await loginAsDemo('admin')
    const uid = authRes.User.Id

    const allTitles = await api.getItems(uid, {
      includeItemTypes: 'Movie,Series',
      limit: 50,
    })
    expect(allTitles.TotalRecordCount).toBe(20)

    const names = allTitles.Items.map((i) => i.Name)
    expect(names).toEqual(
      expect.arrayContaining([
        '肖申克的救赎',
        '教父',
        '蝙蝠侠：黑暗骑士',
        '霸王别姬',
        '阿甘正传',
        '周处除三害',
        '头脑特工队 2',
        '机器人之梦',
        '幕府将军',
        '繁花',
        '漫长的季节',
      ]),
    )

    // GET /emby/Tags 返回榜单标签
    const tagsRes = await api.getTags({ userId: uid })
    const tagNames = tagsRes.Items.map((t) => t.Name)
    expect(tagNames).toContain('豆瓣Top250')
    expect(tagNames).toContain('IMDb Top250')
    expect(tagNames).toContain('TMDB热门')
    expect(tagNames).toContain('豆瓣热门电影')
    expect(tagNames).toContain('豆瓣热门剧集')

    // 验证 StartIndex & Limit 服务端分页准确生效
    const page1 = await api.getItems(uid, {
      includeItemTypes: 'Movie,Series',
      sortBy: 'DateCreated',
      sortOrder: 'Descending',
      startIndex: 0,
      limit: 8,
    })
    const page2 = await api.getItems(uid, {
      includeItemTypes: 'Movie,Series',
      sortBy: 'DateCreated',
      sortOrder: 'Descending',
      startIndex: 8,
      limit: 8,
    })
    expect(page1.TotalRecordCount).toBe(20)
    expect(page1.Items).toHaveLength(8)
    expect(page2.TotalRecordCount).toBe(20)
    expect(page2.Items).toHaveLength(8)
    expect(page1.Items[0].Id).not.toBe(page2.Items[0].Id)

    // GET /Items?Tags=豆瓣Top250 与 IMDb Top250 筛选生效
    const doubanItems = await api.getItems(uid, {
      includeItemTypes: 'Movie',
      tags: '豆瓣Top250',
    })
    expect(doubanItems.TotalRecordCount).toBeGreaterThanOrEqual(9)
    expect(doubanItems.Items.some((i) => i.Name === '霸王别姬')).toBe(true)
    expect(doubanItems.Items.some((i) => i.Name === '肖申克的救赎')).toBe(true)

    const imdbItems = await api.getItems(uid, {
      includeItemTypes: 'Movie',
      tags: 'IMDb Top250',
    })
    expect(imdbItems.TotalRecordCount).toBeGreaterThanOrEqual(9)
    expect(imdbItems.Items.some((i) => i.Name === '教父')).toBe(true)
    expect(imdbItems.Items.some((i) => i.Name === '霸王别姬')).toBe(false)
  })

  it('登录（admin/guest）、首页媒体库视图、继续观看与最新添加数据完整', async () => {
    const authRes = await loginAsDemo('admin')
    expect(authRes.User.Name).toBe('admin')
    expect(authRes.User.Policy?.IsAdministrator).toBe(true)

    const views = await api.getUserViews(authRes.User.Id)
    expect(views.Items.length).toBe(4)
    expect(views.Items.map((v) => v.Name)).toEqual(['电影', '剧集', '4K 精选', '无损音乐'])

    const resume = await api.getResumeItems(authRes.User.Id, 12)
    expect(resume.Items.length).toBeGreaterThanOrEqual(2)
    expect(resume.Items.some((i) => i.Name === '流浪地球 2')).toBe(true)

    const latest = await api.getLatestItems(authRes.User.Id, 'lib-movies', 10)
    expect(latest.length).toBeGreaterThanOrEqual(6)

    // 多季剧集与分集查询
    const seasons = await api.getSeasons('item-three-body', authRes.User.Id)
    expect(seasons.Items.length).toBe(2)
    const episodes = await api.getEpisodes('item-three-body', seasons.Items[0].Id, authRes.User.Id)
    expect(episodes.Items.length).toBeGreaterThanOrEqual(3)
  })

  it('支持中英文及多关键词乱序模糊搜索（片名、原名、导演/演员）', async () => {
    const authRes = await loginAsDemo('admin')

    // 乱序多词：地球 流浪 -> 流浪地球 2
    const r1 = await api.searchHints('地球 流浪', authRes.User.Id)
    expect(r1.SearchHints.some((h) => h.Name === '流浪地球 2')).toBe(true)

    // 导演 + 片名乱序：诺兰 盗梦 -> 盗梦空间
    const r2 = await api.searchHints('诺兰 盗梦', authRes.User.Id)
    expect(r2.SearchHints.some((h) => h.Name === '盗梦空间')).toBe(true)

    // 英文原名 + 年份：interstellar 2014 -> 星际穿越
    const r3 = await api.searchHints('interstellar 2014', authRes.User.Id)
    expect(r3.SearchHints.some((h) => h.Name === '星际穿越')).toBe(true)

    // 无空格中文乱序：球地浪流 -> 流浪地球 2
    const r4 = await api.searchHints('球地浪流', authRes.User.Id)
    expect(r4.SearchHints.some((h) => h.Name === '流浪地球 2')).toBe(true)
  })

  it('收藏 / 标已看状态可写并持久反映在查询结果中', async () => {
    const authRes = await loginAsDemo('admin')
    const uid = authRes.User.Id

    // 沙丘 2 初始未收藏未观看
    const before = await api.getItem(uid, 'item-dune-2')
    expect(before.UserData?.IsFavorite).toBe(false)
    expect(before.UserData?.Played).toBe(false)

    await api.toggleFavorite(uid, 'item-dune-2', false)
    await api.togglePlayed(uid, 'item-dune-2', false)

    const after = await api.getItem(uid, 'item-dune-2')
    expect(after.UserData?.IsFavorite).toBe(true)
    expect(after.UserData?.Played).toBe(true)

    const favs = await api.getItems(uid, { filters: 'IsFavorite' })
    expect(favs.Items.some((i) => i.Id === 'item-dune-2')).toBe(true)
  })

  it('管理控制台：新建用户、修改权限策略、新建媒体库与模拟目录浏览器、创建/吊销 API Key', async () => {
    await loginAsDemo('admin')

    // 1. 新建用户 & 修改 UserPolicy
    const createdUser = await api.createUser({
      Name: 'alice',
      Password: 'secret',
      IsAdministrator: false,
    })
    expect(createdUser.Name).toBe('alice')
    expect(createdUser.Policy?.IsAdministrator).toBe(false)

    await api.updateUserPolicy(createdUser.Id, {
      IsAdministrator: true,
      EnableAllFolders: false,
      EnabledFolders: ['lib-movies'],
    })
    const updatedUser = await api.getUser(createdUser.Id)
    expect(updatedUser.Policy?.IsAdministrator).toBe(true)
    expect(updatedUser.Policy?.EnabledFolders).toEqual(['lib-movies'])

    // 验证媒体库白名单过滤生效
    const restrictedViews = await api.getUserViews(createdUser.Id)
    expect(restrictedViews.Items.map((v) => v.Id)).toEqual(['lib-movies'])

    // 2. 模拟目录浏览器与新建/删除媒体库
    const drives = await api.getDrives()
    expect(drives.some((d) => d.Path === '/media')).toBe(true)
    expect(drives.some((d) => d.Path === '/mnt/nas')).toBe(true)

    const mediaDirs = await api.getDirectoryContents('/media', true, false)
    expect(mediaDirs.some((d) => d.Path === '/media/movies')).toBe(true)
    expect(mediaDirs.some((d) => d.Path === '/media/tvshows')).toBe(true)

    const newLib = await api.createVirtualFolder({
      Name: '纪录片精选',
      CollectionType: 'movies',
      Paths: ['/media/documentaries'],
    })
    expect(newLib.Name).toBe('纪录片精选')
    const libsAfterCreate = await api.getVirtualFolders()
    expect(libsAfterCreate.some((l) => l.ItemId === newLib.ItemId)).toBe(true)

    await api.deleteVirtualFolder({ id: newLib.ItemId })
    const libsAfterDelete = await api.getVirtualFolders()
    expect(libsAfterDelete.some((l) => l.ItemId === newLib.ItemId)).toBe(false)

    // 3. 创建与吊销 API Key（一次性完整 Token + 历史列表脱敏掩码 + 支持 Id/AccessToken 吊销）
    const newKey = await api.createApiKey('Infuse Sync')
    expect(newKey.AppName).toBe('Infuse Sync')
    expect(newKey.Id).toBeTruthy()
    expect(newKey.AccessToken).toBeTruthy()
    expect(newKey.AccessToken.includes('*')).toBe(false)
    const keysAfterCreate = await api.getApiKeys()
    const listedKey = keysAfterCreate.Items.find((k) => k.Id === newKey.Id)
    expect(listedKey).toBeDefined()
    expect(listedKey?.AccessToken).not.toBe(newKey.AccessToken)
    expect(listedKey?.MaskedToken || listedKey?.AccessToken).toMatch(/[•*]/)

    await api.deleteApiKey(newKey.Id || newKey.AccessToken)
    const keysAfterDelete = await api.getApiKeys()
    expect(keysAfterDelete.Items.some((k) => k.Id === newKey.Id)).toBe(false)

    // 4. 插件配置可写 & 计划任务 & 日志
    await api.updatePluginConfiguration('ambra.metadata.tmdb', {
      Enabled: true,
      Priority: 5,
      ApiKey: 'new-tmdb-key',
      Language: 'zh-TW',
    })
    const cfg = await api.getPluginConfiguration('ambra.metadata.tmdb')
    expect(cfg.ApiKey).toBe('********')
    expect(cfg.Language).toBe('zh-TW')

    await api.runScheduledTask('task-scan-libraries')
    const logs = await api.getServerLogs()
    expect(logs.length).toBeGreaterThan(0)
    const logContent = await api.getServerLogContent(logs[0].Name)
    expect(logContent).toContain('Ambra Media Server')
  })

  it('PlaybackInfo 返回离线可播视频源与内置 VTT/ASS 字幕 Data URL，并支持进度上报', async () => {
    const authRes = await loginAsDemo('admin')
    const pb = await api.getPlaybackInfo('item-wandering-earth-2', authRes.User.Id)
    expect(pb.PlaySessionId).toBeTruthy()
    expect(pb.MediaSources.length).toBe(2)

    const mainSource = pb.MediaSources[0]
    expect(mainSource.SupportsDirectPlay).toBe(true)
    expect(mainSource.DirectStreamUrl?.startsWith('data:video/mp4;base64,')).toBe(true)

    const subStreams = (mainSource.MediaStreams || []).filter((s) => s.Type === 'Subtitle')
    const vtt = subStreams.find((s) => s.Codec === 'vtt')
    const ass = subStreams.find((s) => s.Codec === 'ass')
    expect(vtt?.DeliveryUrl?.startsWith('data:text/vtt;charset=utf-8,')).toBe(true)
    expect(ass?.DeliveryUrl?.startsWith('data:text/plain;charset=utf-8,')).toBe(true)

    // 进度上报写入内存态
    await api.reportProgress({
      ItemId: 'item-dune-2',
      PositionTicks: 30 * 60 * 10_000_000,
      IsPaused: false,
      PlayMethod: 'DirectPlay',
    })
    const updatedDune = await api.getItem(authRes.User.Id, 'item-dune-2')
    expect(updatedDune.UserData?.PlaybackPositionTicks).toBe(30 * 60 * 10_000_000)

    clearAuth()
    await expect(api.getSystemInfo()).rejects.toMatchObject({ status: 401 })
  })

  it('管理员媒体管理闭环：元数据编辑、图片替换/上传/删除、远端识别、多版本拆分、删除与榜单刷新', async () => {
    const authRes = await loginAsDemo('admin')
    const uid = authRes.User.Id

    // 1. 元数据编辑与刷新
    await api.updateItemMetadata('item-wandering-earth-2', {
      Name: '流浪地球 2（重映版）',
      OriginalTitle: 'The Wandering Earth II',
      ProductionYear: 2023,
      CommunityRating: 8.8,
      OfficialRating: 'PG-13',
      Overview: '更新后的剧情简介内容',
      Genres: ['科幻', '灾难'],
      Tags: ['豆瓣Top250', '国产科幻'],
      ProviderIds: { Tmdb: '842675', Imdb: 'tt22050754', Douban: '35267208' },
    })
    const edited = await api.getItem(uid, 'item-wandering-earth-2')
    expect(edited.Name).toBe('流浪地球 2（重映版）')
    expect(edited.CommunityRating).toBe(8.8)
    expect(edited.Genres).toEqual(['科幻', '灾难'])
    expect(edited.Tags).toContain('豆瓣Top250')
    expect(edited.ProviderIds?.Douban).toBe('35267208')

    await expect(api.refreshItemMetadata('item-wandering-earth-2', true)).resolves.toBeUndefined()

    // 2. 图片管理：查询、远端下载替换、Base64 上传、URL 替换、删除
    const initialImgs = await api.getItemImages('item-wandering-earth-2')
    expect(initialImgs.some((i) => i.ImageType === 'Primary')).toBe(true)
    expect(initialImgs.some((i) => i.ImageType === 'Backdrop')).toBe(true)

    const remoteImgs = await api.getRemoteImages('item-wandering-earth-2', 'Primary')
    expect(remoteImgs.Images.length).toBeGreaterThan(1)
    const candidateUrl = remoteImgs.Images[1].Url
    await api.downloadRemoteImage('item-wandering-earth-2', 'Primary', candidateUrl)
    expect(getImageUrl('item-wandering-earth-2', 'Primary')).toBe(candidateUrl)

    const fakeBase64 = 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=='
    await api.uploadItemImage('item-wandering-earth-2', 'Backdrop', fakeBase64, 'image/png')
    expect(getImageUrl('item-wandering-earth-2', 'Backdrop')).toBe(
      `data:image/png;base64,${fakeBase64}`,
    )

    const customUrl = 'https://image.tmdb.org/t/p/w500/oYuLEt3zVCKq57qu2F8dT7NIa6f.jpg'
    await api.setItemImageByUrl('item-wandering-earth-2', 'Primary', customUrl)
    expect(getImageUrl('item-wandering-earth-2', 'Primary')).toBe(customUrl)

    await api.deleteItemImage('item-wandering-earth-2', 'Backdrop', 0)
    const afterDeleteImgs = await api.getItemImages('item-wandering-earth-2')
    expect(afterDeleteImgs.some((i) => i.ImageType === 'Backdrop')).toBe(false)

    // 3. 远端识别（Identify）与应用
    const candidates = await api.remoteSearch('Movie', { Name: '盗梦空间', Year: 2010 })
    expect(candidates.length).toBeGreaterThan(0)
    expect(candidates[0].Name).toBe('盗梦空间')
    await api.applyRemoteSearch('item-wandering-earth-2', candidates[0], true)
    const identified = await api.getItem(uid, 'item-wandering-earth-2')
    expect(identified.Name).toBe('盗梦空间')
    expect(identified.ProductionYear).toBe(2010)
    expect(identified.ProviderIds?.Tmdb).toBe('27205')

    // 4. 多版本拆分与合并
    const beforeSplit = await api.getItem(uid, 'item-shawshank-redemption')
    expect(beforeSplit.MediaSources?.length).toBe(2)
    const beforeTotal = (await api.getItems(uid, { includeItemTypes: 'Movie', limit: 50 }))
      .TotalRecordCount

    await api.splitItemVersions('item-shawshank-redemption')
    const afterSplit = await api.getItem(uid, 'item-shawshank-redemption')
    expect(afterSplit.MediaSources?.length).toBe(1)
    const afterMovies = await api.getItems(uid, { includeItemTypes: 'Movie', limit: 50 })
    expect(afterMovies.TotalRecordCount).toBe(beforeTotal + 1)

    const splitChild = afterMovies.Items.find((m) => m.Id.startsWith('item-shawshank-redemption-split-'))
    expect(splitChild).toBeDefined()
    await api.mergeItemVersions(['item-shawshank-redemption', splitChild!.Id])
    const afterMerge = await api.getItem(uid, 'item-shawshank-redemption')
    expect(afterMerge.MediaSources?.length).toBe(2)

    // 5. 榜单查询与刷新自动打标
    await api.updateItemMetadata('item-shawshank-redemption', { Tags: [] })
    const rankingsBefore = await api.getRankings()
    expect(rankingsBefore.douban_top250.count).toBe(250)
    expect(rankingsBefore.imdb_top250.count).toBe(250)

    const refreshRes = await api.refreshRankings()
    expect(refreshRes.matched_items).toBeGreaterThanOrEqual(8)
    expect(refreshRes.tags_added).toBeGreaterThanOrEqual(2)
    const retagged = await api.getItem(uid, 'item-shawshank-redemption')
    expect(retagged.Tags).toContain('豆瓣Top250')
    expect(retagged.Tags).toContain('IMDb Top250')

    // 6. 删除媒体
    await api.deleteItem('item-wandering-earth-2')
    await expect(api.getItem(uid, 'item-wandering-earth-2')).rejects.toMatchObject({ status: 404 })
  })

  it('支持 Trending 热门榜、Person 演职员详情与作品筛选、以及 BoxSet 合集查询', async () => {
    const authRes = await loginAsDemo('admin')
    const uid = authRes.User.Id

    // 1. TMDB Trending & 豆瓣热门电影/剧集榜单状态与 Tags 过滤
    const rankings = await api.getRankingsSummary()
    expect(rankings.tmdb_trending).toBeDefined()
    expect(rankings.tmdb_trending?.count).toBeGreaterThan(0)
    expect(rankings.douban_trending_movie).toBeDefined()
    expect(rankings.douban_trending_movie?.count).toBe(50)
    expect(rankings.douban_trending_tv).toBeDefined()
    expect(rankings.douban_trending_tv?.count).toBe(50)

    const trendingItems = await api.getItems(uid, {
      tags: 'TMDB热门',
      includeItemTypes: 'Movie,Series',
      limit: 50,
    })
    expect(trendingItems.TotalRecordCount).toBeGreaterThanOrEqual(6)
    expect(trendingItems.Items.some((i) => i.Id === 'item-dune-2')).toBe(true)
    expect(trendingItems.Items.some((i) => i.Id === 'item-shogun')).toBe(true)

    const doubanHotMovies = await api.getItems(uid, {
      tags: '豆瓣热门电影',
      includeItemTypes: 'Movie',
      limit: 50,
    })
    expect(doubanHotMovies.TotalRecordCount).toBeGreaterThanOrEqual(4)
    expect(doubanHotMovies.Items.some((i) => i.Name === '周处除三害')).toBe(true)
    expect(doubanHotMovies.Items.some((i) => i.Name === '机器人之梦')).toBe(true)

    const doubanHotTv = await api.getItems(uid, {
      tags: '豆瓣热门剧集',
      includeItemTypes: 'Series',
      limit: 50,
    })
    expect(doubanHotTv.TotalRecordCount).toBeGreaterThanOrEqual(4)
    expect(doubanHotTv.Items.some((i) => i.Name === '漫长的季节')).toBe(true)
    expect(doubanHotTv.Items.some((i) => i.Name === '繁花')).toBe(true)

    const refreshRes = await api.refreshRankings()
    expect(refreshRes.tmdb_trending_count).toBeGreaterThan(0)
    expect(refreshRes.douban_trending_movie_count).toBe(50)
    expect(refreshRes.douban_trending_tv_count).toBe(50)

    // 2. Person 演职员合成 DTO 与 PersonIds / Person 过滤
    const nolan = await api.getItem(uid, 'p-nolan')
    expect(nolan.Type).toBe('Person')
    expect(nolan.Name).toBe('克里斯托弗·诺兰')

    const nolanById = await api.getItems(uid, {
      personIds: 'p-nolan',
      includeItemTypes: 'Movie,Series',
    })
    expect(nolanById.TotalRecordCount).toBeGreaterThanOrEqual(3)
    expect(nolanById.Items.map((i) => i.Name)).toEqual(
      expect.arrayContaining(['盗梦空间', '星际穿越', '奥本海默']),
    )

    const nolanByName = await api.getItems(uid, {
      person: '诺兰',
      includeItemTypes: 'Movie,Series',
    })
    expect(nolanByName.TotalRecordCount).toBeGreaterThanOrEqual(3)

    // 3. BoxSet 合集列表与合集内子项查询
    const boxSets = await api.getItems(uid, {
      includeItemTypes: 'BoxSet',
      recursive: true,
    })
    expect(boxSets.TotalRecordCount).toBeGreaterThanOrEqual(2)
    const scifiBoxSet = boxSets.Items.find((b) => b.Id === 'boxset-scifi-epics')
    expect(scifiBoxSet).toBeDefined()

    const boxSetChildren = await api.getItems(uid, {
      parentId: 'boxset-scifi-epics',
      includeItemTypes: 'Movie,Series',
    })
    expect(boxSetChildren.TotalRecordCount).toBe(5)
    expect(boxSetChildren.Items.some((i) => i.Id === 'item-dune-2')).toBe(true)
  })

  it('P0-6 & P2-17: Demo Mock 完整支持系统健康诊断、媒体治理 (audit/storage/duplicates/locks/nfo/review) 与播放列表排序', async () => {
    const authRes = await loginAsDemo('admin')
    const uid = authRes.User.Id

    // 1. GET /api/admin/health
    const health = await embyFetch<{
      Status: string
      Library: { TotalItems: number; UserCount: number; PendingAudits: number }
      Disk: { TotalBytes: number }
    }>('/api/admin/health')
    expect(health.Status).toBe('healthy')
    expect(health.Library.TotalItems).toBeGreaterThan(0)
    expect(health.Library.UserCount).toBeGreaterThanOrEqual(2)
    expect(health.Disk.TotalBytes).toBeGreaterThan(0)

    // 2. GET /api/admin/governance/storage
    const storage = await embyFetch<{
      TotalMediaBytes: number
      Libraries: Array<{ LibraryId: string; ItemCount: number }>
    }>('/api/admin/governance/storage')
    expect(storage.Libraries.length).toBeGreaterThanOrEqual(4)
    expect(storage.TotalMediaBytes).toBeGreaterThanOrEqual(0)

    // 3. GET /api/admin/governance/audit & POST /api/admin/governance/orphans/cleanup
    const auditBefore = await embyFetch<{
      TotalChecked: number
      OrphanItems: Array<{ Id: string }>
    }>('/api/admin/governance/audit')
    expect(auditBefore.TotalChecked).toBeGreaterThan(0)
    expect(auditBefore.OrphanItems.length).toBe(1)

    const cleanupRes = await embyFetch<{ Deleted: number; DeletedIds: string[] }>(
      '/api/admin/governance/orphans/cleanup',
      {
        method: 'POST',
        body: JSON.stringify({ ItemIds: [auditBefore.OrphanItems[0].Id] }),
      },
    )
    expect(cleanupRes.Deleted).toBe(1)
    const auditAfter = await embyFetch<{ OrphanItems: Array<{ Id: string }> }>(
      '/api/admin/governance/audit',
    )
    expect(auditAfter.OrphanItems).toHaveLength(0)

    // 4. GET /api/admin/governance/review & approve/dismiss
    const reviews = await embyFetch<{ Items: Array<{ Id: string; Status: string }> }>(
      '/api/admin/governance/review?status=pending',
    )
    expect(reviews.Items.length).toBeGreaterThanOrEqual(1)
    const dismissed = await embyFetch<{ Status: string }>(
      `/api/admin/governance/review/${encodeURIComponent(reviews.Items[0].Id)}/dismiss`,
      { method: 'POST' },
    )
    expect(dismissed.Status).toBe('dismissed')

    // 5. GET/POST /api/admin/governance/items/{Id}/locks & /nfo
    await embyFetch('/api/admin/governance/items/item-dune-2/locks', {
      method: 'POST',
      body: JSON.stringify({ LockedFields: ['Name', 'Overview'], LockData: false }),
    })
    const lockState = await embyFetch<{ LockedFields: string[]; LockData: boolean }>(
      '/api/admin/governance/items/item-dune-2/locks',
    )
    expect(lockState.LockedFields).toEqual(['Name', 'Overview'])
    expect(lockState.LockData).toBe(false)

    const nfoRes = await api.writeItemNfo('item-dune-2')
    expect(nfoRes.NfoPath).toBeTruthy()
    expect(nfoRes.RootTag).toBe('movie')

    // 6. GET /api/admin/governance/duplicates & POST /merge
    await api.splitItemVersions('item-shawshank-redemption')
    const dupes = await embyFetch<{
      Groups: Array<{ Items: Array<{ Id: string }> }>
      TotalGroups: number
    }>('/api/admin/governance/duplicates')
    expect(dupes.TotalGroups).toBeGreaterThanOrEqual(1)
    const dupeIds = dupes.Groups[0].Items.map((i) => i.Id)
    const mergeRes = await embyFetch<{ MergedCount: number }>(
      '/api/admin/governance/duplicates/merge',
      {
        method: 'POST',
        body: JSON.stringify({ ItemIds: dupeIds }),
      },
    )
    expect(mergeRes.MergedCount).toBe(dupeIds.length)

    // 7. Playlist move reorder
    const pl = await api.createPlaylist('测试片单', uid, ['item-dune-2', 'item-oppenheimer'], 'Video')
    await api.movePlaylistItem(pl.Id, 'item-oppenheimer', 0)
    const plItems = await api.getPlaylistItems(pl.Id, uid)
    expect(plItems.Items.map((i) => i.Id)).toEqual(['item-oppenheimer', 'item-dune-2'])
  })

  it('M-22: 多用户观看进度、收藏、已看状态与实时播放会话完全隔离，切号不串号', async () => {
    const adminAuth = await loginAsDemo('admin')
    const adminId = adminAuth.User.Id

    // Admin 修改 item-dune-2 状态并上报播放会话
    await api.toggleFavorite(adminId, 'item-dune-2', false)
    await api.reportProgress({
      ItemId: 'item-dune-2',
      PositionTicks: 25 * 60 * 10_000_000,
      IsPaused: false,
      PlayMethod: 'DirectPlay',
    })
    const adminDune = await api.getItem(adminId, 'item-dune-2')
    expect(adminDune.UserData?.IsFavorite).toBe(true)
    expect(adminDune.UserData?.PlaybackPositionTicks).toBe(25 * 60 * 10_000_000)

    // 切换为 guest 账号，验证 item-dune-2 未受 admin 影响
    const guestAuth = await loginAsDemo('guest')
    const guestId = guestAuth.User.Id
    const guestDune = await api.getItem(guestId, 'item-dune-2')
    expect(guestDune.UserData?.IsFavorite).toBe(false)
    expect(guestDune.UserData?.PlaybackPositionTicks || 0).toBe(0)

    const guestResume = await api.getResumeItems(guestId, 20)
    expect(guestResume.Items.some((i) => i.Id === 'item-dune-2')).toBe(false)

    // Guest 独立操作 item-interstellar 并上报播放进度
    await api.togglePlayed(guestId, 'item-interstellar', false)
    await api.reportProgress({
      ItemId: 'item-oppenheimer',
      PositionTicks: 15 * 60 * 10_000_000,
      IsPaused: true,
      PlayMethod: 'DirectStream',
    })

    // 切回 admin 验证两者会话同时存在且各自观看状态独立
    await loginAsDemo('admin')
    const adminInterstellar = await api.getItem(adminId, 'item-interstellar')
    expect(adminInterstellar.UserData?.Played).toBe(false)

    const sessions = await api.getSessions()
    const adminSess = sessions.find((s) => s.UserId === adminId && s.NowPlayingItem)
    const guestSess = sessions.find((s) => s.UserId === guestId && s.NowPlayingItem)
    expect(adminSess?.NowPlayingItem?.Id).toBe('item-dune-2')
    expect(guestSess?.NowPlayingItem?.Id).toBe('item-oppenheimer')
  })

  it('M-22: 管理端写接口对非管理员强制返回 403，且仅在开启 EnableContentDeletion 时放行删片', async () => {
    const guestAuth = await loginAsDemo('guest')
    const guestId = guestAuth.User.Id

    await expect(
      api.createVirtualFolder({ Name: 'Hack', CollectionType: 'movies', Paths: ['/media/movies'] }),
    ).rejects.toMatchObject({ status: 403 })
    await expect(api.createUser({ Name: 'hacker' })).rejects.toMatchObject({ status: 403 })
    await expect(
      api.updateUserPolicy('user-demo-admin', { IsAdministrator: false }),
    ).rejects.toMatchObject({ status: 403 })
    await expect(api.updateServerConfig({ ServerName: 'Owned' })).rejects.toMatchObject({
      status: 403,
    })
    await expect(
      api.updatePluginConfiguration('ambra.metadata.tmdb', { Enabled: false }),
    ).rejects.toMatchObject({ status: 403 })
    await expect(
      api.saveWebhook({ Name: 'Hook', Url: 'https://example.com/hook', Events: ['*'] }),
    ).rejects.toMatchObject({ status: 403 })
    await expect(api.runScheduledTask('task-scan-libraries')).rejects.toMatchObject({ status: 403 })
    await expect(api.createBackup()).rejects.toMatchObject({ status: 403 })
    await expect(
      embyFetch('/api/admin/migration/status'),
    ).rejects.toMatchObject({ status: 403 })
    await expect(api.deleteItem('item-dune-2')).rejects.toMatchObject({ status: 403 })

    // 管理员授予 guest EnableContentDeletion 后，guest 可删除其有权访问的条目
    await loginAsDemo('admin')
    await api.updateUserPolicy(guestId, {
      IsAdministrator: false,
      EnableAllFolders: true,
      EnabledFolders: [],
      EnableContentDeletion: true,
    })

    await loginAsDemo('guest')
    await expect(api.deleteItem('item-dune-2')).resolves.toBeUndefined()
  })

  it('M-22: 媒体库白名单与家长控制 (MaxParentalRating / BlockUnratedItems / BlockedTags / AllowedTags) 全路由生效且局部更新不丢分级', async () => {
    await loginAsDemo('admin')
    const allLibs = await api.getVirtualFolders()
    const tvLibId =
      allLibs.find((l) => l.CollectionType === 'tvshows')?.ItemId || 'lib-series'

    // 将 item-wandering-earth-2 设为无分级以验证 BlockUnratedItems
    await api.updateItemMetadata('item-wandering-earth-2', { OfficialRating: '' })

    // 先配置媒体库白名单仅限电影库 + 最高 PG-13 (7) + 拦截未分级电影
    await api.updateUserPolicy('user-demo-guest', {
      IsAdministrator: false,
      EnableAllFolders: false,
      EnabledFolders: ['lib-movies'],
      MaxParentalRating: 7,
      BlockUnratedItems: ['Movie'],
      BlockedTags: [],
      AllowedTags: ['IMDb Top250'],
    })

    // 单独局部更新 BlockedTags 时，不应清除已有的 MaxParentalRating
    await embyFetch('/emby/Users/user-demo-guest/Policy', {
      method: 'POST',
      body: JSON.stringify({ BlockedTags: ['豆瓣热门电影'] }),
    })
    const guestProfile = await api.getUser('user-demo-guest')
    expect(guestProfile.Policy?.MaxParentalRating).toBe(7)
    expect(guestProfile.Policy?.BlockedTags).toEqual(['豆瓣热门电影'])

    // 切换为 guest，验证 Items / Resume / Latest / NextUp / Search / Collections / 单条目详情全部受控
    const guestAuth = await loginAsDemo('guest')
    const gid = guestAuth.User.Id

    const items = await api.getItems(gid, { includeItemTypes: 'Movie,Series', limit: 50 })
    // R 级电影（如肖申克的救赎/教父/奥本海默）、未分级电影（流浪地球 2）及剧集库条目均不应出现
    expect(items.Items.some((i) => i.Id === 'item-shawshank-redemption')).toBe(false)
    expect(items.Items.some((i) => i.Id === 'item-wandering-earth-2')).toBe(false)
    expect(items.Items.some((i) => i.Type === 'Series')).toBe(false)
    // PG-13 且带 IMDb Top250 标签的盗梦空间/星际穿越允许访问
    expect(items.Items.some((i) => i.Id === 'item-inception')).toBe(true)

    const nextUp = await api.getNextUp(gid, 10)
    expect(nextUp.Items).toHaveLength(0)

    const latestTv = await api.getLatestItems(gid, tvLibId, 10)
    expect(latestTv).toHaveLength(0)

    const hints = await api.searchHints('肖申克', gid)
    expect(hints.SearchHints).toHaveLength(0)

    await expect(api.getItem(gid, 'item-shawshank-redemption')).rejects.toMatchObject({
      status: 403,
    })

    const cols = await embyFetch<{ Items: Array<{ Id: string; ChildCount?: number }> }>(
      `/emby/Collections?UserId=${encodeURIComponent(gid)}`,
    )
    const scifiCol = cols.Items.find((c) => c.Id === 'boxset-scifi-epics')
    if (scifiCol) {
      const colChildren = await embyFetch<{ Items: Array<{ Id: string }> }>(
        `/emby/Collections/${scifiCol.Id}/Items?UserId=${encodeURIComponent(gid)}`,
      )
      expect(colChildren.Items.some((i) => i.Id === 'item-wandering-earth-2')).toBe(false)
    }
  })

  it('M-22: 支持标准 Authorization 头、配置与插件双向同步、6位 Hex PIN、Webhook URL 校验及迁移/转码新增路由', async () => {
    const adminAuth = await loginAsDemo('admin')
    const token = adminAuth.AccessToken

    // 1. 标准 Authorization 头解析
    clearAuth()
    const sysViaStdAuth = await embyFetch<{ Id: string }>('/emby/System/Info', {
      headers: { Authorization: `MediaBrowser Token="${token}"` },
    })
    expect(sysViaStdAuth.Id).toBeTruthy()

    // 恢复登录态继续测试
    await loginAsDemo('admin')

    // 2. /System/Configuration 返回掩码并与内置插件配置双向联动
    const cfg1 = await api.getServerConfig()
    expect(cfg1.TMDBApiKey).toBe('********')
    await api.updateServerConfig({
      ServerName: 'Ambra Pro',
      TMDBApiKey: '********',
      TMDBLanguage: 'ja-JP',
      EnableSTRM302: false,
      PlayedThreshold: 0.85,
      EnableDLNA: true,
      LocalNetworkSubnets: ['192.168.1.0/24'],
    })
    const tmdbPluginCfg = await api.getPluginConfiguration('ambra.metadata.tmdb')
    expect(tmdbPluginCfg.Language || tmdbPluginCfg.language).toBe('ja-JP')
    expect(tmdbPluginCfg.ApiKey || tmdbPluginCfg.api_key).toBe('********')

    await api.updatePluginConfiguration('ambra.metadata.tmdb', {
      Enabled: true,
      Priority: 8,
      ApiKey: '********',
      Language: 'ko-KR',
    })
    const cfg2 = await api.getServerConfig()
    expect(cfg2.TMDBLanguage).toBe('ko-KR')
    expect(cfg2.EnableDLNA).toBe(true)

    // 3. 找回密码支持 6 位十六进制 PIN，拒绝非法字符
    await expect(
      embyFetch('/emby/Users/ForgotPassword/Pin', {
        method: 'POST',
        body: JSON.stringify({ Pin: 'ZZZZZZ' }),
      }),
    ).rejects.toMatchObject({ status: 400 })
    const pinRes = await embyFetch<{ Success: boolean }>('/emby/Users/ForgotPassword/Pin', {
      method: 'POST',
      body: JSON.stringify({ Pin: 'A1B2F9' }),
    })
    expect(pinRes.Success).toBe(true)

    // 4. Webhook 测试校验合法 HTTP(S) URL 协议
    await expect(
      api.testWebhook({ Name: 'Bad', Url: 'ftp://internal.local/hook' }),
    ).rejects.toMatchObject({ status: 400 })

    // 5. Encoding FfmpegOptions & FullToneMapOptions 读写闭环
    await embyFetch('/emby/Encoding/FfmpegOptions', {
      method: 'POST',
      body: JSON.stringify({
        HardwareAccelerationMode: 5,
        EnableHardwareEncoding: true,
        AllowHevcEncoding: true,
        H264Crf: 20,
        H264Preset: 'fast',
        EnableThrottling: true,
        ThrottleBufferSize: 180,
        TranscodingTempPath: '/dev/shm/ambra-transcode',
      }),
    })
    const ffmpegOpts = await embyFetch<{ HardwareAccelerationMode: number; H264Preset: string }>(
      '/emby/Encoding/FfmpegOptions',
    )
    expect(ffmpegOpts.HardwareAccelerationMode).toBe(5)
    expect(ffmpegOpts.H264Preset).toBe('fast')

    await embyFetch('/emby/Encoding/FullToneMapOptions', {
      method: 'POST',
      body: JSON.stringify({ EnableToneMapping: true, ToneMappingAlgorithm: 'mobius' }),
    })
    const toneOpts = await embyFetch<{ EnableToneMapping: boolean; ToneMappingAlgorithm: string }>(
      '/emby/Encoding/FullToneMapOptions',
    )
    expect(toneOpts.EnableToneMapping).toBe(true)
    expect(toneOpts.ToneMappingAlgorithm).toBe('mobius')

    // 6. 迁移 5 路由完整闭环 (/api/admin/migration/{status,test,dry-run,start,cancel})
    const testConn = await embyFetch<{ users: Array<{ Id: string }> }>('/api/admin/migration/test', {
      method: 'POST',
      body: JSON.stringify({ source_url: 'http://192.168.1.88:8096', api_key: 'legacy-key' }),
    })
    expect(testConn.users.length).toBeGreaterThan(0)

    const dryRep = await embyFetch<{ dry_run: boolean; items_matched: number }>(
      '/api/admin/migration/dry-run',
      {
        method: 'POST',
        body: JSON.stringify({
          source_url: 'http://192.168.1.88:8096',
          api_key: 'legacy-key',
          user_ids: testConn.users.map((u) => u.Id),
          migrate_users: true,
          migrate_played: true,
          migrate_favorites: true,
          migrate_resume: true,
        }),
      },
    )
    expect(dryRep.dry_run).toBe(true)
    expect(dryRep.items_matched).toBeGreaterThan(0)

    const startRep = await embyFetch<{ status: string }>('/api/admin/migration/start', {
      method: 'POST',
      body: JSON.stringify({
        source_url: 'http://192.168.1.88:8096',
        api_key: 'legacy-key',
        user_ids: testConn.users.map((u) => u.Id),
      }),
    })
    expect(startRep.status).toBe('running')

    const cancelRes = await embyFetch<{ cancelled: boolean; report: { status: string } }>(
      '/api/admin/migration/cancel',
      { method: 'POST' },
    )
    expect(cancelRes.cancelled).toBe(true)
    expect(cancelRes.report.status).toBe('cancelled')

    // 7. LiveTv SeriesTimers / ChannelMappings 与在线字幕搜索模拟路由
    const seriesTimers = await embyFetch<{ Items: unknown[] }>('/emby/LiveTv/SeriesTimers')
    expect(Array.isArray(seriesTimers.Items)).toBe(true)
    const mappings = await embyFetch<unknown[]>('/emby/LiveTv/ChannelMappings')
    expect(Array.isArray(mappings)).toBe(true)
    const remoteSubs = await embyFetch<Array<{ Id: string }>>(
      '/emby/Items/item-dune-2/RemoteSearch/Subtitles/zh-CN',
    )
    expect(remoteSubs.length).toBeGreaterThan(0)
  })
})
