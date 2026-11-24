import type {
  ActivityLogEntry,
  ApiKeyInfo,
  BaseItemDto,
  BaseItemPerson,
  DeviceInfo,
  FileSystemEntry,
  MediaSourceInfo,
  PluginInfo,
  ScheduledTaskInfo,
  ServerLogFile,
  SessionInfo,
  SystemInfo,
  UserDto,
  VirtualFolderInfo,
} from '../api/types'
import { DEMO_VIDEO_DATA_URL, makeDemoAssDataUrl, makeDemoVttDataUrl } from './demoMedia'
import {
  makeAvatarDataUrl,
  makeBackdropDataUrl,
  makeLibraryThumbDataUrl,
  makePosterDataUrl,
} from './svgArt'

export const DEMO_SERVER_ID = 'ambra-serverless-demo-01'

export interface DemoUserRecord {
  user: UserDto
  password: string
}

export interface DemoArtMeta {
  poster: string
  backdrop: string
  fallbackPoster?: string
  fallbackBackdrop?: string
}

const TMDB_POSTER_BASE = 'https://image.tmdb.org/t/p/w500'
const TMDB_BACKDROP_BASE = 'https://image.tmdb.org/t/p/w1280'

/**
 * 经实测校验 HTTP 200 的 TMDB 官方海报（w500）与高清剧照背景（w1280）
 */
const TMDB_MEDIA_ART = {
  'item-wandering-earth-2': {
    poster: `${TMDB_POSTER_BASE}/pR858ihc6Ls9xohpdRJVjV787ml.jpg`,
    backdrop: `${TMDB_BACKDROP_BASE}/94cS0mzODEoNIXFT7nhPcI8V4IJ.jpg`,
  },
  'item-inception': {
    poster: `${TMDB_POSTER_BASE}/oYuLEt3zVCKq57qu2F8dT7NIa6f.jpg`,
    backdrop: `${TMDB_BACKDROP_BASE}/8ZTVqvKDQ8emSGUEMjsS4yHAwrp.jpg`,
  },
  'item-interstellar': {
    poster: `${TMDB_POSTER_BASE}/gEU2QniE6E77NI6lCU6MxlNBvIx.jpg`,
    backdrop: `${TMDB_BACKDROP_BASE}/xJHokMbljvjADYdit5fK5VQsXEG.jpg`,
  },
  'item-dune-2': {
    poster: `${TMDB_POSTER_BASE}/1pdfLvkbY9ohJlCjQH2CZjjYVvJ.jpg`,
    backdrop: `${TMDB_BACKDROP_BASE}/xOMo8BRK7PfcJv9JCnx7s5hj0PX.jpg`,
  },
  'item-oppenheimer': {
    poster: `${TMDB_POSTER_BASE}/8Gxv8gSFCU0XGDykEGv7zR1n2ua.jpg`,
    backdrop: `${TMDB_BACKDROP_BASE}/nb3xI8XI3w4pMVZ38VijbsyBqP4.jpg`,
  },
  'item-spirited-away': {
    poster: `${TMDB_POSTER_BASE}/39wmItIWsg5sZMyRUHLkWBcuVCM.jpg`,
    backdrop: `${TMDB_BACKDROP_BASE}/Ab8mkHmkYADjU7wQiOkia9BzGvS.jpg`,
  },
  'item-blade-runner-2049': {
    poster: `${TMDB_POSTER_BASE}/gajva2L0rPYkEWjzgFlBXCAVBE5.jpg`,
    backdrop: `${TMDB_BACKDROP_BASE}/askFH4GSk2u9z3ZE5ypdKIMeqLJ.jpg`,
  },
  'item-three-body': {
    poster: `${TMDB_POSTER_BASE}/ykZ7hlShkdRQaL2aiieXdEMmrLb.jpg`,
    backdrop: `${TMDB_BACKDROP_BASE}/ciizJ9Okzt9tBBGK7Q3T14LFT2j.jpg`,
  },
  'item-aot': {
    poster: `${TMDB_POSTER_BASE}/hTP1DtLGFamjfu8WqjnuQdP1n4i.jpg`,
    backdrop: `${TMDB_BACKDROP_BASE}/rqbCbjB19amtOtFQbb3K2lgm2zv.jpg`,
  },
} as const

export interface DemoState {
  systemInfo: SystemInfo
  users: DemoUserRecord[]
  libraries: VirtualFolderInfo[]
  libraryItems: Map<string, string[]> // libraryId -> top-level itemIds
  items: Map<string, BaseItemDto>
  seasonsBySeries: Map<string, BaseItemDto[]>
  episodesBySeason: Map<string, BaseItemDto[]>
  artById: Map<string, DemoArtMeta>
  userAvatarById: Map<string, string>
  sessions: SessionInfo[]
  devices: DeviceInfo[]
  apiKeys: ApiKeyInfo[]
  plugins: PluginInfo[]
  pluginConfigs: Map<string, Record<string, unknown>>
  scheduledTasks: ScheduledTaskInfo[]
  activityLogs: ActivityLogEntry[]
  serverLogs: ServerLogFile[]
  serverLogContents: Map<string, string>
  fsDrives: FileSystemEntry[]
  fsDirs: Record<string, FileSystemEntry[]>
  nextActivityId: number
}

const HOUR_TICKS = 3600 * 10_000_000
const MIN_TICKS = 60 * 10_000_000

function isoAgo(minutesAgo: number): string {
  return new Date(Date.now() - minutesAgo * 60_000).toISOString()
}

function makeMediaSources(
  itemId: string,
  title: string,
  opts: {
    resolution: '4K' | '1080P'
    videoRange: string
    codec: string
    runTimeTicks: number
    multiVersion?: boolean
    hasPgs?: boolean
  },
): MediaSourceInfo[] {
  const vttUrl = makeDemoVttDataUrl(title)
  const assUrl = makeDemoAssDataUrl(title)
  const is4k = opts.resolution === '4K'

  const primary: MediaSourceInfo = {
    Protocol: 'File',
    Id: `${itemId}-src-main`,
    Path: `/media/${is4k ? '4k-remux' : 'movies'}/${title}.${opts.resolution}.${opts.videoRange}.mp4`,
    Type: 'Default',
    Container: 'mp4',
    Size: is4k ? 48_318_382_080 : 12_884_901_888,
    Name: is4k
      ? `4K ${opts.videoRange} Remux · Atmos`
      : `1080P BluRay · ${opts.codec.toUpperCase()}`,
    IsRemote: false,
    RunTimeTicks: opts.runTimeTicks,
    Bitrate: is4k ? 62_400_000 : 18_500_000,
    SupportsTranscoding: true,
    SupportsDirectStream: true,
    SupportsDirectPlay: true,
    DirectStreamUrl: DEMO_VIDEO_DATA_URL,
    DefaultAudioStreamIndex: 1,
    DefaultSubtitleStreamIndex: 3,
    MediaStreams: [
      {
        Type: 'Video',
        Index: 0,
        Codec: opts.codec,
        Width: is4k ? 3840 : 1920,
        Height: is4k ? 2160 : 1080,
        VideoRange: opts.videoRange,
        BitRate: is4k ? 56_000_000 : 16_000_000,
        IsDefault: true,
        DisplayTitle: `${is4k ? '4K' : '1080P'} ${opts.videoRange} ${opts.codec.toUpperCase()}`,
      },
      {
        Type: 'Audio',
        Index: 1,
        Codec: 'aac',
        Language: 'chi',
        DisplayLanguage: '中文',
        Title: is4k ? '国语 Dolby Atmos (AAC 兼容直出)' : '原声立体声',
        DisplayTitle: is4k ? '国语 · Dolby Atmos 7.1' : '原声 · AAC 5.1',
        Channels: is4k ? 8 : 6,
        SampleRate: 48000,
        BitRate: 768_000,
        IsDefault: true,
      },
      {
        Type: 'Audio',
        Index: 2,
        Codec: 'aac',
        Language: 'eng',
        DisplayLanguage: 'English',
        Title: 'English Original Audio',
        DisplayTitle: 'English · TrueHD Atmos 7.1',
        Channels: 8,
        SampleRate: 48000,
        BitRate: 640_000,
        IsDefault: false,
      },
      {
        Type: 'Subtitle',
        Index: 3,
        Codec: 'vtt',
        Language: 'chi',
        DisplayLanguage: '简体中文',
        Title: '简英双语字幕 (WebVTT)',
        DisplayTitle: '简英双语',
        SubVariant: 'bilingual',
        IsDefault: true,
        IsExternal: true,
        DeliveryMethod: 'External',
        DeliveryUrl: vttUrl,
      },
      {
        Type: 'Subtitle',
        Index: 4,
        Codec: 'ass',
        Language: 'chi',
        DisplayLanguage: '简体中文',
        Title: '简体特效字幕 (ASS)',
        DisplayTitle: '简体特效',
        SubVariant: 'chs',
        IsDefault: false,
        IsExternal: true,
        DeliveryMethod: 'External',
        DeliveryUrl: assUrl,
      },
      ...(opts.hasPgs
        ? [
            {
              Type: 'Subtitle',
              Index: 5,
              Codec: 'pgssub',
              Language: 'chi',
              DisplayLanguage: '繁體中文',
              Title: '繁体蓝光原盘字幕 (PGS)',
              DisplayTitle: '繁体蓝光',
              SubVariant: 'cht',
              IsDefault: false,
              IsExternal: true,
              DeliveryMethod: 'External',
              DeliveryUrl: vttUrl,
            },
          ]
        : []),
    ],
  }

  if (!opts.multiVersion) {
    return [primary]
  }

  const secondary: MediaSourceInfo = {
    ...primary,
    Id: `${itemId}-src-1080p`,
    Name: '1080P Web-DL · 轻量化直播',
    Size: 4_831_838_208,
    Bitrate: 8_200_000,
    MediaStreams: primary.MediaStreams?.map((s) =>
      s.Type === 'Video'
        ? {
            ...s,
            Codec: 'h264',
            Width: 1920,
            Height: 1080,
            VideoRange: 'SDR',
            DisplayTitle: '1080P SDR H264',
          }
        : s,
    ),
  }

  return [primary, secondary]
}

function makePeople(
  list: Array<{ id: string; name: string; role: string; type: string; palette: [string, string] }>,
  artById: Map<string, DemoArtMeta>,
): BaseItemPerson[] {
  return list.map((p) => {
    if (!artById.has(p.id)) {
      const av = makeAvatarDataUrl(p.name, p.palette)
      artById.set(p.id, { poster: av, backdrop: av })
    }
    return {
      Id: p.id,
      Name: p.name,
      Role: p.role,
      Type: p.type,
      PrimaryImageTag: 'demo-avatar',
    }
  })
}

export function createInitialDemoState(): DemoState {
  const artById = new Map<string, DemoArtMeta>()
  const userAvatarById = new Map<string, string>()

  // ==================== 1. 媒体库 ====================
  const libraries: VirtualFolderInfo[] = [
    {
      ItemId: 'lib-movies',
      Name: '电影',
      CollectionType: 'movies',
      Locations: ['/media/movies'],
    },
    {
      ItemId: 'lib-tvshows',
      Name: '剧集',
      CollectionType: 'tvshows',
      Locations: ['/media/tvshows'],
    },
    {
      ItemId: 'lib-4k',
      Name: '4K 精选',
      CollectionType: 'movies',
      Locations: ['/mnt/nas/4k-remux', '/mnt/nas/dolby-vision'],
    },
  ]

  artById.set('lib-movies', {
    poster: makeLibraryThumbDataUrl('电影', 'MOVIES · 4K HDR', ['#1e1b4b', '#312e81', '#f59e0b']),
    backdrop: makeLibraryThumbDataUrl('电影', 'MOVIES · 4K HDR', ['#1e1b4b', '#312e81', '#f59e0b']),
  })
  artById.set('lib-tvshows', {
    poster: makeLibraryThumbDataUrl('剧集', 'TV SERIES · MULTI-SEASON', [
      '#0f172a',
      '#134e4a',
      '#14b8a6',
    ]),
    backdrop: makeLibraryThumbDataUrl('剧集', 'TV SERIES · MULTI-SEASON', [
      '#0f172a',
      '#134e4a',
      '#14b8a6',
    ]),
  })
  artById.set('lib-4k', {
    poster: makeLibraryThumbDataUrl('4K 精选', 'DOLBY VISION · ATMOS', [
      '#27100b',
      '#7c2d12',
      '#fb923c',
    ]),
    backdrop: makeLibraryThumbDataUrl('4K 精选', 'DOLBY VISION · ATMOS', [
      '#27100b',
      '#7c2d12',
      '#fb923c',
    ]),
  })

  // ==================== 2. 影片与剧集 ====================
  const items = new Map<string, BaseItemDto>()
  const seasonsBySeries = new Map<string, BaseItemDto[]>()
  const episodesBySeason = new Map<string, BaseItemDto[]>()

  const registerItem = (
    item: BaseItemDto,
    palette: [string, string, string],
    badge?: string,
    tmdbArt?: { poster: string; backdrop: string },
  ) => {
    items.set(item.Id, item)
    const fallbackPoster = makePosterDataUrl({
      title: item.Name,
      subtitle: item.OriginalTitle,
      year: item.ProductionYear,
      badge,
      rating: item.CommunityRating,
      palette,
    })
    const fallbackBackdrop = makeBackdropDataUrl({
      title: item.Name,
      subtitle: item.OriginalTitle,
      year: item.ProductionYear,
      palette,
    })
    const resolvedTmdb =
      tmdbArt ||
      (item.Id in TMDB_MEDIA_ART
        ? TMDB_MEDIA_ART[item.Id as keyof typeof TMDB_MEDIA_ART]
        : undefined)

    artById.set(item.Id, {
      poster: resolvedTmdb?.poster || fallbackPoster,
      backdrop: resolvedTmdb?.backdrop || fallbackBackdrop,
      fallbackPoster,
      fallbackBackdrop,
    })
  }

  registerItem(
    {
      Id: 'item-wandering-earth-2',
      Name: '流浪地球 2',
      OriginalTitle: 'The Wandering Earth II',
      SortName: 'liulangdiqiu 2 wandering earth 2',
      ServerId: DEMO_SERVER_ID,
      Type: 'Movie',
      MediaType: 'Video',
      ParentId: 'lib-movies',
      ProductionYear: 2023,
      PremiereDate: '2023-01-22T00:00:00.000Z',
      DateCreated: isoAgo(60),
      OfficialRating: 'PG-13',
      CommunityRating: 8.3,
      RunTimeTicks: 2 * HOUR_TICKS + 53 * MIN_TICKS,
      Genres: ['科幻', '冒险', '灾难'],
      Overview:
        '太阳即将毁灭，人类在地球表面建造出巨大的推进器，寻找新的家园。然而宇宙之路危机四伏，为了拯救地球，流浪地球时代的年轻人再次挺身而出，展开争分夺秒的生死之战。',
      People: makePeople(
        [
          { id: 'p-guofan', name: '郭帆', role: '导演', type: 'Director', palette: ['#b45309', '#451a03'] },
          { id: 'p-wujing', name: '吴京', role: '刘培强', type: 'Actor', palette: ['#1d4ed8', '#172554'] },
          { id: 'p-liudehua', name: '刘德华', role: '图恒宇', type: 'Actor', palette: ['#0f766e', '#042f2e'] },
          { id: 'p-lixuejian', name: '李雪健', role: '周喆直', type: 'Actor', palette: ['#7c2d12', '#27100b'] },
        ],
        artById,
      ),
      MediaSources: makeMediaSources('item-wandering-earth-2', '流浪地球 2', {
        resolution: '4K',
        videoRange: 'Dolby Vision',
        codec: 'hevc',
        runTimeTicks: 2 * HOUR_TICKS + 53 * MIN_TICKS,
        multiVersion: true,
        hasPgs: true,
      }),
      ImageTags: { Primary: 'demo-p' },
      BackdropImageTags: ['demo-b'],
      PrimaryImageAspectRatio: 0.667,
      UserData: {
        PlaybackPositionTicks: 66 * MIN_TICKS,
        PlayCount: 1,
        IsFavorite: true,
        Played: false,
        PlayedPercentage: 38,
        LastPlayedDate: isoAgo(18),
      },
    },
    ['#0c1e35', '#1e3a5f', '#38bdf8'],
    '4K DV · ATMOS',
  )

  registerItem(
    {
      Id: 'item-inception',
      Name: '盗梦空间',
      OriginalTitle: 'Inception',
      SortName: 'daomengkongjian inception',
      ServerId: DEMO_SERVER_ID,
      Type: 'Movie',
      MediaType: 'Video',
      ParentId: 'lib-movies',
      ProductionYear: 2010,
      PremiereDate: '2010-07-16T00:00:00.000Z',
      DateCreated: isoAgo(180),
      OfficialRating: 'PG-13',
      CommunityRating: 9.4,
      RunTimeTicks: 2 * HOUR_TICKS + 28 * MIN_TICKS,
      Genres: ['科幻', '悬疑', '动作'],
      Overview:
        '道姆·柯布是一名经验老道的窃贼，他在人们精神最为脆弱的时候潜入梦境深处窃取秘密。这一次，他的任务不是窃取思想，而是植入想法——“奠基”。',
      People: makePeople(
        [
          { id: 'p-nolan', name: '克里斯托弗·诺兰', role: '导演', type: 'Director', palette: ['#3730a3', '#1e1b4b'] },
          { id: 'p-leo', name: '莱昂纳多·迪卡普里奥', role: 'Dom Cobb', type: 'Actor', palette: ['#9a3412', '#431407'] },
          { id: 'p-jgl', name: '约瑟夫·高登-莱维特', role: 'Arthur', type: 'Actor', palette: ['#155e75', '#083344'] },
        ],
        artById,
      ),
      MediaSources: makeMediaSources('item-inception', '盗梦空间', {
        resolution: '4K',
        videoRange: 'HDR10',
        codec: 'hevc',
        runTimeTicks: 2 * HOUR_TICKS + 28 * MIN_TICKS,
        multiVersion: true,
        hasPgs: true,
      }),
      ImageTags: { Primary: 'demo-p' },
      BackdropImageTags: ['demo-b'],
      PrimaryImageAspectRatio: 0.667,
      UserData: {
        PlaybackPositionTicks: 0,
        PlayCount: 2,
        IsFavorite: true,
        Played: true,
        LastPlayedDate: isoAgo(1440),
      },
    },
    ['#1c1917', '#44403c', '#f59e0b'],
    '4K HDR10',
  )

  registerItem(
    {
      Id: 'item-interstellar',
      Name: '星际穿越',
      OriginalTitle: 'Interstellar',
      SortName: 'xingjichuanyue interstellar',
      ServerId: DEMO_SERVER_ID,
      Type: 'Movie',
      MediaType: 'Video',
      ParentId: 'lib-movies',
      ProductionYear: 2014,
      PremiereDate: '2014-11-07T00:00:00.000Z',
      DateCreated: isoAgo(240),
      OfficialRating: 'PG-13',
      CommunityRating: 9.4,
      RunTimeTicks: 2 * HOUR_TICKS + 49 * MIN_TICKS,
      Genres: ['科幻', '剧情', '冒险'],
      Overview:
        '近未来的地球黄沙遍野，小麦、秋葵等基础农作物相继因枯萎病灭绝。前宇航员库珀在女儿墨菲的书房发现引力异常，随后受命穿越虫洞，为人类寻找新的宜居星球。',
      People: makePeople(
        [
          { id: 'p-nolan', name: '克里斯托弗·诺兰', role: '导演', type: 'Director', palette: ['#3730a3', '#1e1b4b'] },
          { id: 'p-matthew', name: '马修·麦康纳', role: 'Cooper', type: 'Actor', palette: ['#92400e', '#451a03'] },
          { id: 'p-anne', name: '安妮·海瑟薇', role: 'Amelia Brand', type: 'Actor', palette: ['#6b21a8', '#3b0764'] },
        ],
        artById,
      ),
      MediaSources: makeMediaSources('item-interstellar', '星际穿越', {
        resolution: '4K',
        videoRange: 'HDR10',
        codec: 'hevc',
        runTimeTicks: 2 * HOUR_TICKS + 49 * MIN_TICKS,
        multiVersion: true,
        hasPgs: true,
      }),
      ImageTags: { Primary: 'demo-p' },
      BackdropImageTags: ['demo-b'],
      PrimaryImageAspectRatio: 0.667,
      UserData: {
        PlaybackPositionTicks: 105 * MIN_TICKS,
        PlayCount: 1,
        IsFavorite: true,
        Played: false,
        PlayedPercentage: 62,
        LastPlayedDate: isoAgo(52),
      },
    },
    ['#0b1120', '#1e293b', '#fbbf24'],
    '4K IMAX HDR',
  )

  registerItem(
    {
      Id: 'item-dune-2',
      Name: '沙丘 2',
      OriginalTitle: 'Dune: Part Two',
      SortName: 'shaqiu 2 dune part two',
      ServerId: DEMO_SERVER_ID,
      Type: 'Movie',
      MediaType: 'Video',
      ParentId: 'lib-movies',
      ProductionYear: 2024,
      PremiereDate: '2024-03-01T00:00:00.000Z',
      DateCreated: isoAgo(30),
      OfficialRating: 'PG-13',
      CommunityRating: 8.6,
      RunTimeTicks: 2 * HOUR_TICKS + 46 * MIN_TICKS,
      Genres: ['科幻', '动作', '冒险'],
      Overview:
        '保罗·厄崔迪与契妮和弗雷曼人联手，踏上对毁灭其家族的阴谋者的复仇之路。在面临一生挚爱与已知宇宙命运的抉择时，他必须竭力阻止只有他能预见的可怕未来。',
      People: makePeople(
        [
          { id: 'p-denis', name: '丹尼斯·维伦纽瓦', role: '导演', type: 'Director', palette: ['#9a3412', '#431407'] },
          { id: 'p-timothee', name: '提莫西·查拉梅', role: 'Paul Atreides', type: 'Actor', palette: ['#b45309', '#451a03'] },
          { id: 'p-zendaya', name: '赞达亚', role: 'Chani', type: 'Actor', palette: ['#7c2d12', '#27100b'] },
        ],
        artById,
      ),
      MediaSources: makeMediaSources('item-dune-2', '沙丘 2', {
        resolution: '4K',
        videoRange: 'Dolby Vision',
        codec: 'hevc',
        runTimeTicks: 2 * HOUR_TICKS + 46 * MIN_TICKS,
        multiVersion: true,
        hasPgs: true,
      }),
      ImageTags: { Primary: 'demo-p' },
      BackdropImageTags: ['demo-b'],
      PrimaryImageAspectRatio: 0.667,
      UserData: {
        PlaybackPositionTicks: 0,
        PlayCount: 0,
        IsFavorite: false,
        Played: false,
      },
    },
    ['#3b1806', '#7c2d12', '#fb923c'],
    '4K DV · ATMOS',
  )

  registerItem(
    {
      Id: 'item-oppenheimer',
      Name: '奥本海默',
      OriginalTitle: 'Oppenheimer',
      SortName: 'aobenhaimo oppenheimer',
      ServerId: DEMO_SERVER_ID,
      Type: 'Movie',
      MediaType: 'Video',
      ParentId: 'lib-movies',
      ProductionYear: 2023,
      PremiereDate: '2023-07-21T00:00:00.000Z',
      DateCreated: isoAgo(120),
      OfficialRating: 'R',
      CommunityRating: 8.8,
      RunTimeTicks: 3 * HOUR_TICKS,
      Genres: ['传记', '剧情', '历史'],
      Overview:
        '聚焦美国“原子弹之父”罗伯特·奥本海默在曼哈顿计划中的关键抉择与战后听证会风暴，以宏大而克制的叙事刻画科学与良知的剧烈碰撞。',
      People: makePeople(
        [
          { id: 'p-nolan', name: '克里斯托弗·诺兰', role: '导演', type: 'Director', palette: ['#3730a3', '#1e1b4b'] },
          { id: 'p-cillian', name: '基里安·墨菲', role: 'J. Robert Oppenheimer', type: 'Actor', palette: ['#991b1b', '#450a0a'] },
          { id: 'p-rdj', name: '小罗伯特·唐尼', role: 'Lewis Strauss', type: 'Actor', palette: ['#475569', '#0f172a'] },
        ],
        artById,
      ),
      MediaSources: makeMediaSources('item-oppenheimer', '奥本海默', {
        resolution: '4K',
        videoRange: 'HDR10',
        codec: 'hevc',
        runTimeTicks: 3 * HOUR_TICKS,
        hasPgs: true,
      }),
      ImageTags: { Primary: 'demo-p' },
      BackdropImageTags: ['demo-b'],
      PrimaryImageAspectRatio: 0.667,
      UserData: {
        PlaybackPositionTicks: 0,
        PlayCount: 0,
        IsFavorite: false,
        Played: false,
      },
    },
    ['#2a0a0a', '#7f1d1d', '#f97316'],
    '4K HDR10',
  )

  registerItem(
    {
      Id: 'item-spirited-away',
      Name: '千与千寻',
      OriginalTitle: 'Spirited Away',
      SortName: 'qianyuqianxun spirited away',
      ServerId: DEMO_SERVER_ID,
      Type: 'Movie',
      MediaType: 'Video',
      ParentId: 'lib-movies',
      ProductionYear: 2001,
      PremiereDate: '2001-07-20T00:00:00.000Z',
      DateCreated: isoAgo(360),
      OfficialRating: 'PG',
      CommunityRating: 9.4,
      RunTimeTicks: 2 * HOUR_TICKS + 5 * MIN_TICKS,
      Genres: ['动画', '奇幻', '冒险'],
      Overview:
        '十岁少女千寻随父母搬家途中误入神灵异界，父母因贪吃变成猪，千寻在白龙的帮助下进入汤婆婆的油屋工作，学会勇敢、善良并寻找回家的路。',
      People: makePeople(
        [
          { id: 'p-miyazaki', name: '宫崎骏', role: '导演', type: 'Director', palette: ['#047857', '#022c22'] },
          { id: 'p-hisaishi', name: '久石让', role: '配乐', type: 'Composer', palette: ['#0369a1', '#082f49'] },
        ],
        artById,
      ),
      MediaSources: makeMediaSources('item-spirited-away', '千与千寻', {
        resolution: '1080P',
        videoRange: 'SDR',
        codec: 'h264',
        runTimeTicks: 2 * HOUR_TICKS + 5 * MIN_TICKS,
        hasPgs: true,
      }),
      ImageTags: { Primary: 'demo-p' },
      BackdropImageTags: ['demo-b'],
      PrimaryImageAspectRatio: 0.667,
      UserData: {
        PlaybackPositionTicks: 0,
        PlayCount: 3,
        IsFavorite: true,
        Played: true,
      },
    },
    ['#064e3b', '#115e59', '#34d399'],
    '1080P · ASS',
  )

  registerItem(
    {
      Id: 'item-blade-runner-2049',
      Name: '银翼杀手 2049',
      OriginalTitle: 'Blade Runner 2049',
      SortName: 'yinyishashou 2049 blade runner 2049',
      ServerId: DEMO_SERVER_ID,
      Type: 'Movie',
      MediaType: 'Video',
      ParentId: 'lib-4k',
      ProductionYear: 2017,
      PremiereDate: '2017-10-06T00:00:00.000Z',
      DateCreated: isoAgo(90),
      OfficialRating: 'R',
      CommunityRating: 8.5,
      RunTimeTicks: 2 * HOUR_TICKS + 44 * MIN_TICKS,
      Genres: ['科幻', '赛博朋克', '悬疑'],
      Overview:
        '新一代银翼杀手 K 在执行任务时发现了一个足以颠覆人类与复制人社会秩序的惊天秘密，线索指引他去寻找失踪三十年的前任银翼杀手瑞克·戴克。',
      People: makePeople(
        [
          { id: 'p-denis', name: '丹尼斯·维伦纽瓦', role: '导演', type: 'Director', palette: ['#9a3412', '#431407'] },
          { id: 'p-gosling', name: '瑞恩·高斯林', role: 'Officer K', type: 'Actor', palette: ['#db2777', '#500724'] },
        ],
        artById,
      ),
      MediaSources: makeMediaSources('item-blade-runner-2049', '银翼杀手 2049', {
        resolution: '4K',
        videoRange: 'Dolby Vision',
        codec: 'hevc',
        runTimeTicks: 2 * HOUR_TICKS + 44 * MIN_TICKS,
        multiVersion: true,
        hasPgs: true,
      }),
      ImageTags: { Primary: 'demo-p' },
      BackdropImageTags: ['demo-b'],
      PrimaryImageAspectRatio: 0.667,
      UserData: {
        PlaybackPositionTicks: 0,
        PlayCount: 0,
        IsFavorite: false,
        Played: false,
      },
    },
    ['#3b0764', '#831843', '#f472b6'],
    '4K DV · ATMOS',
  )

  // ==================== 3. 多季剧集：《三体》 ====================
  registerItem(
    {
      Id: 'item-three-body',
      Name: '三体',
      OriginalTitle: 'Three-Body',
      SortName: 'santi three body',
      ServerId: DEMO_SERVER_ID,
      Type: 'Series',
      ParentId: 'lib-tvshows',
      ProductionYear: 2023,
      PremiereDate: '2023-01-15T00:00:00.000Z',
      DateCreated: isoAgo(150),
      OfficialRating: 'TV-14',
      CommunityRating: 8.7,
      Genres: ['科幻', '悬疑', '剧情'],
      Overview:
        '2007年，地球基础科学出现异常扰动，纳米科学家汪淼与刑警史强联手调查“科学边界”组织，逐步揭开三体文明与红岸基地跨越半个世纪的宇宙史诗。',
      People: makePeople(
        [
          { id: 'p-zhangluyi', name: '张鲁一', role: '汪淼', type: 'Actor', palette: ['#0f766e', '#042f2e'] },
          { id: 'p-yuhewei', name: '于和伟', role: '史强', type: 'Actor', palette: ['#b45309', '#451a03'] },
          { id: 'p-chenjin', name: '陈瑾', role: '叶文洁', type: 'Actor', palette: ['#3730a3', '#1e1b4b'] },
        ],
        artById,
      ),
      ImageTags: { Primary: 'demo-p' },
      BackdropImageTags: ['demo-b'],
      PrimaryImageAspectRatio: 0.667,
      ChildCount: 2,
      UserData: {
        PlaybackPositionTicks: 0,
        PlayCount: 0,
        IsFavorite: false,
        Played: false,
        UnplayedItemCount: 4,
      },
    },
    ['#042f2e', '#115e59', '#2dd4bf'],
    '4K · 2 季',
  )

  const tbS1: BaseItemDto = {
    Id: 'season-tb-1',
    Name: '第 1 季 · 科学边界',
    ServerId: DEMO_SERVER_ID,
    Type: 'Season',
    SeriesId: 'item-three-body',
    SeriesName: '三体',
    IndexNumber: 1,
    ProductionYear: 2023,
    ImageTags: { Primary: 'demo-p' },
  }
  const tbS2: BaseItemDto = {
    Id: 'season-tb-2',
    Name: '第 2 季 · 黑暗森林（前瞻）',
    ServerId: DEMO_SERVER_ID,
    Type: 'Season',
    SeriesId: 'item-three-body',
    SeriesName: '三体',
    IndexNumber: 2,
    ProductionYear: 2025,
    ImageTags: { Primary: 'demo-p' },
  }
  seasonsBySeries.set('item-three-body', [tbS1, tbS2])
  registerItem(tbS1, ['#042f2e', '#115e59', '#2dd4bf'], 'S01', TMDB_MEDIA_ART['item-three-body'])
  registerItem(tbS2, ['#042f2e', '#115e59', '#2dd4bf'], 'S02', TMDB_MEDIA_ART['item-three-body'])

  const tbS1Eps: BaseItemDto[] = [
    {
      Id: 'ep-tb-s1e1',
      Name: '物理学不存在了',
      ServerId: DEMO_SERVER_ID,
      Type: 'Episode',
      MediaType: 'Video',
      SeriesId: 'item-three-body',
      SeriesName: '三体',
      SeasonId: 'season-tb-1',
      SeasonName: tbS1.Name,
      ParentIndexNumber: 1,
      IndexNumber: 1,
      RunTimeTicks: 46 * MIN_TICKS,
      Overview: '知名物理学家杨冬离世，汪淼受邀协助作战中心调查“射手与农场主”假说。',
      MediaSources: makeMediaSources('ep-tb-s1e1', '三体 S01E01 物理学不存在了', {
        resolution: '4K',
        videoRange: 'HDR10',
        codec: 'hevc',
        runTimeTicks: 46 * MIN_TICKS,
      }),
      ImageTags: { Primary: 'demo-p' },
      UserData: { PlaybackPositionTicks: 0, PlayCount: 1, IsFavorite: false, Played: true },
    },
    {
      Id: 'ep-tb-s1e2',
      Name: '幽灵倒计时',
      ServerId: DEMO_SERVER_ID,
      Type: 'Episode',
      MediaType: 'Video',
      SeriesId: 'item-three-body',
      SeriesName: '三体',
      SeasonId: 'season-tb-1',
      SeasonName: tbS1.Name,
      ParentIndexNumber: 1,
      IndexNumber: 2,
      RunTimeTicks: 48 * MIN_TICKS,
      Overview: '汪淼拍摄的胶片上出现神秘倒计时，连视网膜中也浮现出无法消除的数字。',
      MediaSources: makeMediaSources('ep-tb-s1e2', '三体 S01E02 幽灵倒计时', {
        resolution: '4K',
        videoRange: 'HDR10',
        codec: 'hevc',
        runTimeTicks: 48 * MIN_TICKS,
      }),
      ImageTags: { Primary: 'demo-p' },
      UserData: { PlaybackPositionTicks: 0, PlayCount: 1, IsFavorite: false, Played: true },
    },
    {
      Id: 'ep-tb-s1e3',
      Name: '整个宇宙将为你闪烁',
      ServerId: DEMO_SERVER_ID,
      Type: 'Episode',
      MediaType: 'Video',
      SeriesId: 'item-three-body',
      SeriesName: '三体',
      SeasonId: 'season-tb-1',
      SeasonName: tbS1.Name,
      ParentIndexNumber: 1,
      IndexNumber: 3,
      RunTimeTicks: 50 * MIN_TICKS,
      Overview: '密云射电天文观测基地内，宇宙微波背景辐射开始按照倒计时规律剧烈闪烁。',
      MediaSources: makeMediaSources('ep-tb-s1e3', '三体 S01E03 整个宇宙将为你闪烁', {
        resolution: '4K',
        videoRange: 'HDR10',
        codec: 'hevc',
        runTimeTicks: 50 * MIN_TICKS,
      }),
      ImageTags: { Primary: 'demo-p' },
      UserData: {
        PlaybackPositionTicks: 24 * MIN_TICKS,
        PlayCount: 0,
        IsFavorite: true,
        Played: false,
        PlayedPercentage: 48,
        LastPlayedDate: isoAgo(35),
      },
    },
  ]
  const tbS2Eps: BaseItemDto[] = [
    {
      Id: 'ep-tb-s2e1',
      Name: '面壁计划启动',
      ServerId: DEMO_SERVER_ID,
      Type: 'Episode',
      MediaType: 'Video',
      SeriesId: 'item-three-body',
      SeriesName: '三体',
      SeasonId: 'season-tb-2',
      SeasonName: tbS2.Name,
      ParentIndexNumber: 2,
      IndexNumber: 1,
      RunTimeTicks: 52 * MIN_TICKS,
      Overview: '面对智子的全天候监控，联合国宣布启动利用人类思维隐蔽性的“面壁计划”。',
      MediaSources: makeMediaSources('ep-tb-s2e1', '三体 S02E01 面壁计划启动', {
        resolution: '4K',
        videoRange: 'Dolby Vision',
        codec: 'hevc',
        runTimeTicks: 52 * MIN_TICKS,
      }),
      ImageTags: { Primary: 'demo-p' },
      UserData: { PlaybackPositionTicks: 0, PlayCount: 0, IsFavorite: false, Played: false },
    },
  ]
  episodesBySeason.set('season-tb-1', tbS1Eps)
  episodesBySeason.set('season-tb-2', tbS2Eps)
  for (const ep of [...tbS1Eps, ...tbS2Eps]) {
    registerItem(
      ep,
      ['#042f2e', '#0f766e', '#2dd4bf'],
      `S0${ep.ParentIndexNumber}E0${ep.IndexNumber}`,
      {
        poster: TMDB_MEDIA_ART['item-three-body'].backdrop,
        backdrop: TMDB_MEDIA_ART['item-three-body'].backdrop,
      },
    )
  }

  // ==================== 4. 多季剧集：《进击的巨人》 ====================
  registerItem(
    {
      Id: 'item-aot',
      Name: '进击的巨人',
      OriginalTitle: 'Attack on Titan',
      SortName: 'jinjidejuren attack on titan shingeki no kyojin',
      ServerId: DEMO_SERVER_ID,
      Type: 'Series',
      ParentId: 'lib-tvshows',
      ProductionYear: 2013,
      PremiereDate: '2013-04-07T00:00:00.000Z',
      DateCreated: isoAgo(210),
      OfficialRating: 'TV-MA',
      CommunityRating: 9.6,
      Genres: ['动画', '动作', '奇幻'],
      Overview:
        '百年前世界突然出现身长数米至数十米的“巨人”，人类筑起三重高墙苟延残喘。直到超大型巨人击破玛利亚之墙，少年艾伦立誓驱逐所有巨人，揭开高墙之外的真相。',
      People: makePeople(
        [
          { id: 'p-araki', name: '荒木哲郎', role: '导演', type: 'Director', palette: ['#7f1d1d', '#450a0a'] },
          { id: 'p-kaji', name: '梶裕贵', role: '艾伦·耶格尔', type: 'Actor', palette: ['#1e3a8a', '#172554'] },
          { id: 'p-sawano', name: '泽野弘之', role: '配乐', type: 'Composer', palette: ['#b45309', '#451a03'] },
        ],
        artById,
      ),
      ImageTags: { Primary: 'demo-p' },
      BackdropImageTags: ['demo-b'],
      PrimaryImageAspectRatio: 0.667,
      ChildCount: 2,
      UserData: {
        PlaybackPositionTicks: 0,
        PlayCount: 0,
        IsFavorite: true,
        Played: false,
        UnplayedItemCount: 3,
      },
    },
    ['#1c1917', '#7f1d1d', '#ef4444'],
    '1080P · ASS',
  )

  const aotS1: BaseItemDto = {
    Id: 'season-aot-1',
    Name: '第 1 季 · 玛利亚之墙陷落',
    ServerId: DEMO_SERVER_ID,
    Type: 'Season',
    SeriesId: 'item-aot',
    SeriesName: '进击的巨人',
    IndexNumber: 1,
    ProductionYear: 2013,
    ImageTags: { Primary: 'demo-p' },
  }
  const aotS2: BaseItemDto = {
    Id: 'season-aot-4',
    Name: '最终季 · The Final Season',
    ServerId: DEMO_SERVER_ID,
    Type: 'Season',
    SeriesId: 'item-aot',
    SeriesName: '进击的巨人',
    IndexNumber: 4,
    ProductionYear: 2023,
    ImageTags: { Primary: 'demo-p' },
  }
  seasonsBySeries.set('item-aot', [aotS1, aotS2])
  registerItem(aotS1, ['#1c1917', '#7f1d1d', '#ef4444'], 'S01', TMDB_MEDIA_ART['item-aot'])
  registerItem(aotS2, ['#1c1917', '#7f1d1d', '#ef4444'], 'S04', TMDB_MEDIA_ART['item-aot'])

  const aotS1Eps: BaseItemDto[] = [
    {
      Id: 'ep-aot-s1e1',
      Name: '致二千年后的你',
      ServerId: DEMO_SERVER_ID,
      Type: 'Episode',
      MediaType: 'Video',
      SeriesId: 'item-aot',
      SeriesName: '进击的巨人',
      SeasonId: 'season-aot-1',
      SeasonName: aotS1.Name,
      ParentIndexNumber: 1,
      IndexNumber: 1,
      RunTimeTicks: 24 * MIN_TICKS,
      Overview: '那一天，人类回想起了曾经一度被他们所支配的恐怖，还有囚禁于鸟笼中的屈辱。',
      MediaSources: makeMediaSources('ep-aot-s1e1', '进击的巨人 S01E01 致二千年后的你', {
        resolution: '1080P',
        videoRange: 'SDR',
        codec: 'h264',
        runTimeTicks: 24 * MIN_TICKS,
        hasPgs: true,
      }),
      ImageTags: { Primary: 'demo-p' },
      UserData: { PlaybackPositionTicks: 0, PlayCount: 1, IsFavorite: true, Played: true },
    },
    {
      Id: 'ep-aot-s1e2',
      Name: '那一天',
      ServerId: DEMO_SERVER_ID,
      Type: 'Episode',
      MediaType: 'Video',
      SeriesId: 'item-aot',
      SeriesName: '进击的巨人',
      SeasonId: 'season-aot-1',
      SeasonName: aotS1.Name,
      ParentIndexNumber: 1,
      IndexNumber: 2,
      RunTimeTicks: 24 * MIN_TICKS,
      Overview: '铠之巨人撞破内门，希干希纳区彻底沦陷，艾伦、三笠与阿明踏上逃难之船。',
      MediaSources: makeMediaSources('ep-aot-s1e2', '进击的巨人 S01E02 那一天', {
        resolution: '1080P',
        videoRange: 'SDR',
        codec: 'h264',
        runTimeTicks: 24 * MIN_TICKS,
      }),
      ImageTags: { Primary: 'demo-p' },
      UserData: { PlaybackPositionTicks: 0, PlayCount: 0, IsFavorite: false, Played: false },
    },
  ]
  const aotS4Eps: BaseItemDto[] = [
    {
      Id: 'ep-aot-s4e1',
      Name: '海的对面',
      ServerId: DEMO_SERVER_ID,
      Type: 'Episode',
      MediaType: 'Video',
      SeriesId: 'item-aot',
      SeriesName: '进击的巨人',
      SeasonId: 'season-aot-4',
      SeasonName: aotS2.Name,
      ParentIndexNumber: 4,
      IndexNumber: 1,
      RunTimeTicks: 25 * MIN_TICKS,
      Overview: '跨越海洋的另一端，持续四年的马莱与中东联合军之战迎来终局。',
      MediaSources: makeMediaSources('ep-aot-s4e1', '进击的巨人 S04E01 海的对面', {
        resolution: '1080P',
        videoRange: 'SDR',
        codec: 'hevc',
        runTimeTicks: 25 * MIN_TICKS,
      }),
      ImageTags: { Primary: 'demo-p' },
      UserData: { PlaybackPositionTicks: 0, PlayCount: 0, IsFavorite: false, Played: false },
    },
  ]
  episodesBySeason.set('season-aot-1', aotS1Eps)
  episodesBySeason.set('season-aot-4', aotS4Eps)
  for (const ep of [...aotS1Eps, ...aotS4Eps]) {
    registerItem(
      ep,
      ['#27100b', '#7f1d1d', '#ef4444'],
      `S0${ep.ParentIndexNumber}E0${ep.IndexNumber}`,
      {
        poster: TMDB_MEDIA_ART['item-aot'].backdrop,
        backdrop: TMDB_MEDIA_ART['item-aot'].backdrop,
      },
    )
  }

  // 媒体库包含关系
  const libraryItems = new Map<string, string[]>([
    [
      'lib-movies',
      [
        'item-wandering-earth-2',
        'item-dune-2',
        'item-oppenheimer',
        'item-inception',
        'item-interstellar',
        'item-spirited-away',
        'item-blade-runner-2049',
      ],
    ],
    ['lib-tvshows', ['item-three-body', 'item-aot']],
    [
      'lib-4k',
      [
        'item-wandering-earth-2',
        'item-dune-2',
        'item-interstellar',
        'item-inception',
        'item-oppenheimer',
        'item-blade-runner-2049',
      ],
    ],
  ])

  // ==================== 5. 用户列表 ====================
  const users: DemoUserRecord[] = [
    {
      password: '',
      user: {
        Id: 'user-demo-admin',
        Name: 'admin',
        ServerId: DEMO_SERVER_ID,
        HasPassword: false,
        HasConfiguredPassword: false,
        PrimaryImageTag: 'demo-user',
        LastLoginDate: isoAgo(5),
        LastActivityDate: isoAgo(1),
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
      },
    },
    {
      password: '',
      user: {
        Id: 'user-demo-guest',
        Name: 'guest',
        ServerId: DEMO_SERVER_ID,
        HasPassword: false,
        HasConfiguredPassword: false,
        PrimaryImageTag: 'demo-user',
        LastLoginDate: isoAgo(42),
        LastActivityDate: isoAgo(15),
        Policy: {
          IsAdministrator: false,
          IsDisabled: false,
          EnableAllFolders: true,
          EnabledFolders: [],
          EnableMediaPlayback: true,
          EnableAudioPlaybackTranscoding: true,
          EnableVideoPlaybackTranscoding: true,
          EnablePlaybackRemuxing: true,
          EnableContentDeletion: false,
          EnableContentDownloading: false,
          EnableRemoteAccess: true,
        },
      },
    },
  ]

  userAvatarById.set('user-demo-admin', makeAvatarDataUrl('A', ['#d97706', '#78350f']))
  userAvatarById.set('user-demo-guest', makeAvatarDataUrl('G', ['#0284c7', '#1e3a8a']))

  // ==================== 6. 系统、会话、设备、API Keys、插件、任务与日志 ====================
  const systemInfo: SystemInfo = {
    Id: DEMO_SERVER_ID,
    ServerName: 'Ambra Serverless Demo',
    ProductName: 'Ambra',
    Version: '0.1.0-demo',
    LocalAddress: 'https://demo.ambra.local (Browser Memory)',
    OperatingSystem: 'WebAssembly / Browser Sandbox',
    OperatingSystemDisplayName: 'Serverless Demo Engine',
    SystemArchitecture: 'wasm64',
    HttpServerPortNumber: 8096,
    SupportsHttps: true,
    SupportsLibraryMonitor: true,
    ProgramDataPath: '/var/lib/ambra/data',
    CachePath: '/var/lib/ambra/cache',
    LogPath: '/var/lib/ambra/logs',
    TranscodingTempPath: '/tmp/ambra-transcode',
    StartupWizardCompleted: true,
  }

  const nowPlayingMovie = items.get('item-wandering-earth-2')
  const sessions: SessionInfo[] = [
    {
      Id: 'sess-demo-web',
      ServerId: DEMO_SERVER_ID,
      UserId: 'user-demo-admin',
      UserName: 'admin',
      Client: 'Ambra Web',
      ApplicationVersion: '0.1.0',
      DeviceName: 'Chrome on macOS (Demo)',
      DeviceId: 'dev-demo-web-01',
      LastActivityDate: isoAgo(1),
      SupportsRemoteControl: true,
      NowPlayingItem: nowPlayingMovie,
      PlayState: {
        PositionTicks: 66 * MIN_TICKS,
        CanSeek: true,
        IsPaused: false,
        IsMuted: false,
        VolumeLevel: 85,
        PlayMethod: 'DirectPlay',
      },
    },
    {
      Id: 'sess-demo-atv',
      ServerId: DEMO_SERVER_ID,
      UserId: 'user-demo-guest',
      UserName: 'guest',
      Client: 'Infuse',
      ApplicationVersion: '8.0.4',
      DeviceName: 'Living Room Apple TV 4K',
      DeviceId: 'dev-demo-atv-02',
      LastActivityDate: isoAgo(12),
      SupportsRemoteControl: false,
    },
  ]

  const devices: DeviceInfo[] = [
    {
      Id: 'dev-demo-web-01',
      Name: 'Chrome on macOS (Demo)',
      AppName: 'Ambra Web',
      AppVersion: '0.1.0',
      LastUserId: 'user-demo-admin',
      LastUserName: 'admin',
      DateLastActivity: isoAgo(1),
    },
    {
      Id: 'dev-demo-atv-02',
      Name: 'Living Room Apple TV 4K',
      AppName: 'Infuse',
      AppVersion: '8.0.4',
      LastUserId: 'user-demo-guest',
      LastUserName: 'guest',
      DateLastActivity: isoAgo(12),
    },
    {
      Id: 'dev-demo-ios-03',
      Name: 'iPhone 16 Pro',
      AppName: 'SenPlayer',
      AppVersion: '4.2.1',
      LastUserId: 'user-demo-admin',
      LastUserName: 'admin',
      DateLastActivity: isoAgo(95),
    },
  ]

  const apiKeys: ApiKeyInfo[] = [
    {
      AccessToken: 'ambra_demo_key_moviepilot_9f2a8c4e1d7b',
      AppName: 'MoviePilot',
      DateCreated: isoAgo(2880),
      IsActive: true,
    },
    {
      AccessToken: 'ambra_demo_key_sonarr_3b7e5a90c14f',
      AppName: 'Sonarr',
      DateCreated: isoAgo(1440),
      IsActive: true,
    },
  ]

  const plugins: PluginInfo[] = [
    {
      Id: 'ambra.metadata.tmdb',
      Name: 'TMDB 元数据刮削器',
      Version: '1.4.0',
      Description: '自动从 The Movie Database 刮削中英文标题、简介、海报、背景图及演职员表。',
      Author: 'Ambra Team',
      BuiltIn: true,
      Enabled: true,
      Priority: 10,
      Kinds: ['metadata', 'image'],
      Status: 'Active',
    },
    {
      Id: 'ambra.stream.strm302',
      Name: 'STRM 302 云盘直链解析',
      Version: '1.2.0',
      Description: '解析 .strm 文件并在播放时返回 302 重定向至云盘直链，实现零带宽中转播放。',
      Author: 'Ambra Team',
      BuiltIn: true,
      Enabled: true,
      Priority: 20,
      Kinds: ['stream'],
      Status: 'Active',
    },
    {
      Id: 'ambra.subtitle.chinese-variant',
      Name: '中文简繁字幕智能识别器',
      Version: '1.0.3',
      Description: '自动识别内封与外挂字幕中的 CHS / CHT / SC / TC / 双语标签并输出清晰轨道名称。',
      Author: 'Ambra Team',
      BuiltIn: true,
      Enabled: true,
      Priority: 15,
      Kinds: ['subtitle'],
      Status: 'Active',
    },
    {
      Id: 'ambra.notifier.telegram',
      Name: 'Telegram 入库通知',
      Version: '0.9.1',
      Description: '新片入库或定时任务完成时向指定 Telegram 频道推送带海报的富文本通知。',
      Author: 'Community',
      BuiltIn: false,
      Enabled: false,
      Priority: 50,
      Kinds: ['notifier'],
      Status: 'Disabled',
    },
  ]

  const pluginConfigs = new Map<string, Record<string, unknown>>([
    [
      'ambra.metadata.tmdb',
      {
        Enabled: true,
        Priority: 10,
        ApiKey: 'demo_tmdb_v3_key_xxxxxxxx',
        Language: 'zh-CN',
        IncludeAdult: false,
        FallbackToEnglishBackdrop: true,
      },
    ],
    [
      'ambra.stream.strm302',
      {
        Enabled: true,
        Priority: 20,
        Enable302: true,
        CacheTTLSeconds: 300,
      },
    ],
    [
      'ambra.subtitle.chinese-variant',
      {
        Enabled: true,
        Priority: 15,
        PreferSimplifiedChinese: true,
      },
    ],
    [
      'ambra.notifier.telegram',
      {
        Enabled: false,
        Priority: 50,
        ChatId: '',
      },
    ],
  ])

  const scheduledTasks: ScheduledTaskInfo[] = [
    {
      Id: 'task-scan-libraries',
      Key: 'RefreshLibrary',
      Name: '扫描全部媒体库',
      Category: '媒体库',
      Description: '增量扫描本地与 NAS 挂载目录，自动匹配 TMDB 元数据与中文简繁字幕。',
      State: 'Idle',
      CurrentProgressPercentage: 100,
      LastExecutionResult: {
        Status: 'Completed',
        StartTimeUtc: isoAgo(125),
        EndTimeUtc: isoAgo(124),
      },
    },
    {
      Id: 'task-clean-cache',
      Key: 'CleanTranscodeCache',
      Name: '清理转码与图像缓存',
      Category: '维护',
      Description: '清理超过 7 天的临时 HLS 切片与缩略图缓存文件，释放磁盘空间。',
      State: 'Idle',
      CurrentProgressPercentage: 100,
      LastExecutionResult: {
        Status: 'Completed',
        StartTimeUtc: isoAgo(720),
        EndTimeUtc: isoAgo(719),
      },
    },
    {
      Id: 'task-refresh-people',
      Key: 'RefreshPeople',
      Name: '更新演职员头像与索引',
      Category: '元数据',
      Description: '补全缺失的导演、演员头像并重建中文乱序搜索倒排索引。',
      State: 'Idle',
      CurrentProgressPercentage: 100,
      LastExecutionResult: {
        Status: 'Completed',
        StartTimeUtc: isoAgo(1440),
        EndTimeUtc: isoAgo(1438),
      },
    },
  ]

  const activityLogs: ActivityLogEntry[] = [
    {
      Id: 1004,
      Name: 'admin 开始播放《流浪地球 2》(4K Dolby Vision · DirectPlay)',
      ShortOverview: '客户端: Ambra Web · 字幕引擎: Native VTT / JASSUB',
      Type: 'VideoPlayback',
      Date: isoAgo(18),
      UserId: 'user-demo-admin',
      Severity: 'Info',
    },
    {
      Id: 1003,
      Name: '媒体库「4K 精选」增量扫描完成，共索引 6 部高码率影片',
      ShortOverview: '耗时 1.2s · TMDB 元数据命中率 100%',
      Type: 'LibraryScan',
      Date: isoAgo(124),
      Severity: 'Info',
    },
    {
      Id: 1002,
      Name: 'guest 从 Living Room Apple TV 4K 登录成功',
      ShortOverview: 'Client: Infuse 8.0.4 · RBAC 角色: 普通用户',
      Type: 'AuthenticationSucceeded',
      Date: isoAgo(42),
      UserId: 'user-demo-guest',
      Severity: 'Info',
    },
    {
      Id: 1001,
      Name: 'Ambra Serverless Demo 内存引擎已就绪',
      ShortOverview: '所有读写操作均在浏览器内存中即时生效，刷新页面自动还原初始演示状态',
      Type: 'SystemStarted',
      Date: isoAgo(180),
      Severity: 'Info',
    },
  ]

  const serverLogs: ServerLogFile[] = [
    {
      Name: 'ambra-server-2026-11-24.log',
      DateCreated: isoAgo(360),
      DateModified: isoAgo(1),
      Size: 18_420,
    },
    {
      Name: 'ambra-scanner-2026-11-24.log',
      DateCreated: isoAgo(180),
      DateModified: isoAgo(124),
      Size: 6_144,
    },
  ]

  const serverLogContents = new Map<string, string>([
    [
      'ambra-server-2026-11-24.log',
      [
        '2026-11-24T08:00:01Z INFO  [server] Ambra Media Server v0.1.0-demo starting (Serverless Memory Engine)',
        '2026-11-24T08:00:01Z INFO  [db]     Loaded 3 virtual folders, 9 curated titles, 2 users (admin, guest)',
        '2026-11-24T08:00:02Z INFO  [search] Chinese out-of-order fuzzy search index ready (CJK + Pinyin + Cast)',
        '2026-11-24T08:00:02Z INFO  [plugin] Enabled: ambra.metadata.tmdb (priority=10), ambra.stream.strm302 (priority=20)',
        '2026-11-24T08:05:11Z INFO  [auth]   User "admin" authenticated (IsAdministrator=true)',
        '2026-11-24T08:10:22Z INFO  [play]   PlaybackInfo item=item-wandering-earth-2 strategy=DirectPlay subtitles=External(vtt,ass,pgs)',
        '2026-11-24T08:12:45Z WARN  [strm]   STRM 302 cache miss for /mnt/nas/strm-cloud/demo.strm, refreshed in 14ms',
      ].join('\n'),
    ],
    [
      'ambra-scanner-2026-11-24.log',
      [
        '2026-11-24T06:10:00Z INFO  [scan]   Scanning /media/movies (7 items discovered)',
        '2026-11-24T06:10:00Z INFO  [scan]   Scanning /media/tvshows (2 series, 4 seasons, 7 episodes)',
        '2026-11-24T06:10:01Z INFO  [scan]   Scanning /mnt/nas/4k-remux (6 4K HDR/DV titles indexed)',
        '2026-11-24T06:10:01Z INFO  [sub]    Detected Chinese variants: CHS ASS, CHT PGS, Bilingual WebVTT',
        '2026-11-24T06:10:01Z INFO  [scan]   Library scan completed in 1.2s with 0 errors',
      ].join('\n'),
    ],
  ])

  const fsDrives: FileSystemEntry[] = [
    { Name: '本地媒体盘 (/media)', Path: '/media', Type: 'Directory' },
    { Name: '群晖 NAS 挂载 (/mnt/nas)', Path: '/mnt/nas', Type: 'NetworkShare' },
    { Name: '数据存储 (/data)', Path: '/data', Type: 'Directory' },
  ]

  const fsDirs: Record<string, FileSystemEntry[]> = {
    '/': fsDrives,
    '/media': [
      { Name: 'movies', Path: '/media/movies', Type: 'Directory' },
      { Name: 'tvshows', Path: '/media/tvshows', Type: 'Directory' },
      { Name: 'anime', Path: '/media/anime', Type: 'Directory' },
      { Name: 'documentaries', Path: '/media/documentaries', Type: 'Directory' },
    ],
    '/media/movies': [
      { Name: '流浪地球2 (2023)', Path: '/media/movies/流浪地球2 (2023)', Type: 'Directory' },
      { Name: '沙丘2 (2024)', Path: '/media/movies/沙丘2 (2024)', Type: 'Directory' },
    ],
    '/media/tvshows': [
      { Name: '三体 (2023)', Path: '/media/tvshows/三体 (2023)', Type: 'Directory' },
      { Name: '进击的巨人 (2013)', Path: '/media/tvshows/进击的巨人 (2013)', Type: 'Directory' },
    ],
    '/mnt/nas': [
      { Name: '4k-remux', Path: '/mnt/nas/4k-remux', Type: 'Directory' },
      { Name: 'dolby-vision', Path: '/mnt/nas/dolby-vision', Type: 'Directory' },
      { Name: 'strm-cloud', Path: '/mnt/nas/strm-cloud', Type: 'Directory' },
    ],
    '/data': [
      { Name: 'downloads', Path: '/data/downloads', Type: 'Directory' },
      { Name: 'music', Path: '/data/music', Type: 'Directory' },
    ],
  }

  return {
    systemInfo,
    users,
    libraries,
    libraryItems,
    items,
    seasonsBySeries,
    episodesBySeason,
    artById,
    userAvatarById,
    sessions,
    devices,
    apiKeys,
    plugins,
    pluginConfigs,
    scheduledTasks,
    activityLogs,
    serverLogs,
    serverLogContents,
    fsDrives,
    fsDirs,
    nextActivityId: 1005,
  }
}
