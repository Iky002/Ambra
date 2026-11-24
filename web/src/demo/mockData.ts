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
const TMDB_PROFILE_BASE = 'https://image.tmdb.org/t/p/w185'

/**
 * 经实测校验 HTTP 200 的 TMDB 官方海报（w500）与高清剧照背景（w1280）
 */
const TMDB_MEDIA_ART = {
  'item-shawshank-redemption': {
    poster: `${TMDB_POSTER_BASE}/aAdnwqwkKX5PPcr8EdtaiA8AZVl.jpg`,
    backdrop: `${TMDB_BACKDROP_BASE}/v8xVDqt8uCul3c3mgx4VpGCwxJC.jpg`,
  },
  'item-the-godfather': {
    poster: `${TMDB_POSTER_BASE}/y03tzUKvkRCYwJ5NWys4W4bnS9m.jpg`,
    backdrop: `${TMDB_BACKDROP_BASE}/ejdD20cdHNFAYAN2DlqPToXKyzx.jpg`,
  },
  'item-the-dark-knight': {
    poster: `${TMDB_POSTER_BASE}/qYB7QqwT1NtTW9aCwtopGy80rmA.jpg`,
    backdrop: `${TMDB_BACKDROP_BASE}/cfT29Im5VDvjE0RpyKOSdCKZal7.jpg`,
  },
  'item-farewell-my-concubine': {
    poster: `${TMDB_POSTER_BASE}/5AFb0onaTd0Pbo5Q2uAVTFL8XTX.jpg`,
    backdrop: `${TMDB_BACKDROP_BASE}/bBiZN1epQTu3F8iFLBuMhc4TRzr.jpg`,
  },
  'item-forrest-gump': {
    poster: `${TMDB_POSTER_BASE}/pplybKImR7LKzSVzRylK6Cl4dzm.jpg`,
    backdrop: `${TMDB_BACKDROP_BASE}/66Kn4XWhkuPkJxOJyPEx4U2CUfN.jpg`,
  },
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
  list: Array<{
    id: string
    name: string
    role: string
    type: string
    palette: [string, string]
    tmdbProfile?: string
  }>,
  artById: Map<string, DemoArtMeta>,
): BaseItemPerson[] {
  return list.map((p) => {
    if (!artById.has(p.id)) {
      const av = makeAvatarDataUrl(p.name, p.palette)
      const profileUrl = p.tmdbProfile ? `${TMDB_PROFILE_BASE}/${p.tmdbProfile}` : av
      artById.set(p.id, {
        poster: profileUrl,
        backdrop: profileUrl,
        fallbackPoster: av,
        fallbackBackdrop: av,
      })
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
      Id: 'item-shawshank-redemption',
      Name: '肖申克的救赎',
      OriginalTitle: 'The Shawshank Redemption',
      SortName: 'xiaoshenkedejiushu the shawshank redemption',
      ServerId: DEMO_SERVER_ID,
      Type: 'Movie',
      MediaType: 'Video',
      ParentId: 'lib-movies',
      ProductionYear: 1994,
      PremiereDate: '1994-09-23T00:00:00.000Z',
      DateCreated: isoAgo(15),
      OfficialRating: 'R',
      CommunityRating: 9.7,
      RunTimeTicks: 2 * HOUR_TICKS + 22 * MIN_TICKS,
      Genres: ['剧情', '犯罪'],
      Tags: ['豆瓣Top250', 'IMDb Top250'],
      ProviderIds: { Tmdb: '278', Imdb: 'tt0111161' },
      Studios: [{ Id: 'st-castle-rock', Name: 'Castle Rock Entertainment' }],
      Overview:
        '1947年，银行家安迪·杜佛兰因被控杀害妻子及其情人，尽管始终坚称清白，仍被判处两项无期徒刑，送入缅因州的肖申克监狱服刑。在残酷而压抑的监狱生活中，安迪凭借冷静、智慧和始终未曾熄灭的希望逐渐赢得狱友们的尊重，并与能够在狱中弄到各种物品的瑞德建立起深厚的友谊。漫长的岁月里，他始终没有让高墙彻底改变自己，也开始用自己的方式影响身边的人，以及这座看似牢不可破的监狱……',
      People: makePeople(
        [
          { id: 'p-frank-darabont', name: '弗兰克·德拉邦特', role: '导演', type: 'Director', palette: ['#334155', '#0f172a'], tmdbProfile: 'vZ50guP86otYTiBSGfi35GNHWVf.jpg' },
          { id: 'p-tim-robbins', name: '蒂姆·罗宾斯', role: '安迪·杜佛兰', type: 'Actor', palette: ['#1d4ed8', '#172554'], tmdbProfile: '3FfJMIVwXgsIXbAT8ECBSZJAncR.jpg' },
          { id: 'p-morgan-freeman', name: '摩根·弗里曼', role: '埃利斯·“瑞德”·雷丁', type: 'Actor', palette: ['#92400e', '#451a03'], tmdbProfile: 'jPsLqiYGSofU4s6BjrxnefMfabb.jpg' },
          { id: 'p-bob-gunton', name: '鲍勃·冈顿', role: '典狱长诺顿', type: 'Actor', palette: ['#475569', '#0f172a'], tmdbProfile: 'ulbVvuBToBN3aCGcV028hwO0MOP.jpg' },
        ],
        artById,
      ),
      MediaSources: makeMediaSources('item-shawshank-redemption', '肖申克的救赎', {
        resolution: '4K',
        videoRange: 'HDR10',
        codec: 'hevc',
        runTimeTicks: 2 * HOUR_TICKS + 22 * MIN_TICKS,
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
        LastPlayedDate: isoAgo(620),
      },
    },
    ['#1e293b', '#334155', '#f59e0b'],
    '4K HDR10',
  )

  registerItem(
    {
      Id: 'item-the-godfather',
      Name: '教父',
      OriginalTitle: 'The Godfather',
      SortName: 'jiaofu the godfather',
      ServerId: DEMO_SERVER_ID,
      Type: 'Movie',
      MediaType: 'Video',
      ParentId: 'lib-movies',
      ProductionYear: 1972,
      PremiereDate: '1972-03-14T00:00:00.000Z',
      DateCreated: isoAgo(22),
      OfficialRating: 'R',
      CommunityRating: 9.3,
      RunTimeTicks: 2 * HOUR_TICKS + 55 * MIN_TICKS,
      Genres: ['剧情', '犯罪'],
      Tags: ['豆瓣Top250', 'IMDb Top250'],
      ProviderIds: { Tmdb: '238', Imdb: 'tt0068646' },
      Studios: [{ Id: 'st-paramount', Name: 'Paramount Pictures' }],
      Overview:
        '40年代的美国，“教父”维托·唐·柯里昂是黑手党柯里昂家族的首领，带领家族从事非法的勾当，但同时他也是许多弱小平民的保护神，深得人们爱戴。因为拒绝了毒枭索洛索的毒品交易要求，柯里昂家族和纽约其他几个黑手党家族的矛盾激化。血雨腥风和温情脉脉，在这部里程碑式的黑帮史诗巨片里真实上演。',
      People: makePeople(
        [
          { id: 'p-coppola', name: '弗朗西斯·科波拉', role: '导演', type: 'Director', palette: ['#7c2d12', '#27100b'], tmdbProfile: '3Pblihd6KjXliie9vj4iQJwbNPU.jpg' },
          { id: 'p-brando', name: '马龙·白兰度', role: '维托·柯里昂', type: 'Actor', palette: ['#451a03', '#1c1917'], tmdbProfile: 'yIvLpf9U9NfAvC7fuAgOQ8tu51T.jpg' },
          { id: 'p-pacino', name: '阿尔·帕西诺', role: '迈克尔·柯里昂', type: 'Actor', palette: ['#991b1b', '#450a0a'], tmdbProfile: 'm8HAAjq1T75JypKk0v1FFQn4ysZ.jpg' },
          { id: 'p-caan', name: '詹姆斯·凯恩', role: '桑尼·柯里昂', type: 'Actor', palette: ['#3f3f46', '#18181b'], tmdbProfile: 'z2Lz3rtxZ7aJjzBUkCnExvo8stn.jpg' },
        ],
        artById,
      ),
      MediaSources: makeMediaSources('item-the-godfather', '教父', {
        resolution: '4K',
        videoRange: 'Dolby Vision',
        codec: 'hevc',
        runTimeTicks: 2 * HOUR_TICKS + 55 * MIN_TICKS,
        multiVersion: true,
        hasPgs: true,
      }),
      ImageTags: { Primary: 'demo-p' },
      BackdropImageTags: ['demo-b'],
      PrimaryImageAspectRatio: 0.667,
      UserData: {
        PlaybackPositionTicks: 0,
        PlayCount: 1,
        IsFavorite: true,
        Played: false,
      },
    },
    ['#1c1917', '#451a03', '#d97706'],
    '4K DV',
  )

  registerItem(
    {
      Id: 'item-the-dark-knight',
      Name: '蝙蝠侠：黑暗骑士',
      OriginalTitle: 'The Dark Knight',
      SortName: 'bianfuxia heianqishi the dark knight',
      ServerId: DEMO_SERVER_ID,
      Type: 'Movie',
      MediaType: 'Video',
      ParentId: 'lib-movies',
      ProductionYear: 2008,
      PremiereDate: '2008-07-18T00:00:00.000Z',
      DateCreated: isoAgo(40),
      OfficialRating: 'PG-13',
      CommunityRating: 9.2,
      RunTimeTicks: 2 * HOUR_TICKS + 32 * MIN_TICKS,
      Genres: ['动作', '惊悚', '犯罪'],
      Tags: ['豆瓣Top250', 'IMDb Top250'],
      ProviderIds: { Tmdb: '155', Imdb: 'tt0468569' },
      Studios: [
        { Id: 'st-warner', Name: 'Warner Bros. Pictures' },
        { Id: 'st-legendary', Name: 'Legendary Pictures' },
      ],
      Overview:
        '蝙蝠侠在吉姆·戈登警官和检察官哈维·登特的协助下，致力于彻底铲除哥谭市的犯罪组织。然而，三人的合作很快遭到犯罪天才小丑的破坏。小丑将哥谭市拖入混乱，迫使蝙蝠侠游走于英雄与私刑者的界限之间。',
      People: makePeople(
        [
          { id: 'p-nolan', name: '克里斯托弗·诺兰', role: '导演', type: 'Director', palette: ['#3730a3', '#1e1b4b'], tmdbProfile: 'xuAIuYSmsUzKlUMBFGVZaWsY3DZ.jpg' },
          { id: 'p-bale', name: '克里斯蒂安·贝尔', role: '布鲁斯·韦恩 / 蝙蝠侠', type: 'Actor', palette: ['#1e293b', '#020617'], tmdbProfile: '7Pxez9J8fuPd2Mn9kex13YALrCQ.jpg' },
          { id: 'p-ledger', name: '希斯·莱杰', role: '小丑', type: 'Actor', palette: ['#581c87', '#1e1b4b'], tmdbProfile: 'p2z2bURSg7nuMsN9P2s61e2RvNz.jpg' },
          { id: 'p-eckhart', name: '艾伦·艾克哈特', role: '哈维·登特', type: 'Actor', palette: ['#0f766e', '#042f2e'], tmdbProfile: '5EFQvRHlpP1Iaw2e6vjOaBny6DV.jpg' },
          { id: 'p-caine', name: '迈克尔·凯恩', role: '阿尔弗雷德', type: 'Actor', palette: ['#475569', '#0f172a'], tmdbProfile: 'bVZRMlpjTAO2pJK6v90buFgVbSW.jpg' },
        ],
        artById,
      ),
      MediaSources: makeMediaSources('item-the-dark-knight', '蝙蝠侠：黑暗骑士', {
        resolution: '4K',
        videoRange: 'HDR10',
        codec: 'hevc',
        runTimeTicks: 2 * HOUR_TICKS + 32 * MIN_TICKS,
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
      },
    },
    ['#090d16', '#1e293b', '#38bdf8'],
    '4K IMAX HDR',
  )

  registerItem(
    {
      Id: 'item-farewell-my-concubine',
      Name: '霸王别姬',
      OriginalTitle: '霸王别姬',
      SortName: 'bawangbieji farewell my concubine',
      ServerId: DEMO_SERVER_ID,
      Type: 'Movie',
      MediaType: 'Video',
      ParentId: 'lib-movies',
      ProductionYear: 1993,
      PremiereDate: '1993-01-01T00:00:00.000Z',
      DateCreated: isoAgo(50),
      OfficialRating: 'R',
      CommunityRating: 9.6,
      RunTimeTicks: 2 * HOUR_TICKS + 51 * MIN_TICKS,
      Genres: ['剧情'],
      Tags: ['豆瓣Top250'],
      ProviderIds: { Tmdb: '10997', Imdb: 'tt0106332' },
      Studios: [{ Id: 'st-tomson', Name: '汤臣电影有限公司' }],
      Overview:
        '段小楼与程蝶衣是一对自幼一起学戏长大的师兄弟，两人在舞台上配合天衣无缝，尤其一出《霸王别姬》更是誉满京城，并约定要合演一辈子。然而，两人对戏剧与人生的理解截然不同：段小楼深知戏非人生，程蝶衣却早已人戏不分。后来，段小楼迎娶菊仙，三人之间由此产生复杂的情感纠葛。随着时代风云不断变迁，他们的命运也一次次被卷入其中，舞台上的霸王与虞姬，逐渐与现实中的爱恨、选择和人生交织在一起……',
      People: makePeople(
        [
          { id: 'p-chen-kaige', name: '陈凯歌', role: '导演', type: 'Director', palette: ['#7f1d1d', '#450a0a'], tmdbProfile: 'rRESKBe0rTxP253M0URp2f0TAtW.jpg' },
          { id: 'p-leslie-cheung', name: '张国荣', role: '程蝶衣', type: 'Actor', palette: ['#991b1b', '#450a0a'], tmdbProfile: '6H8gTq1QCB0bD9CGDhrIJITiGVX.jpg' },
          { id: 'p-zhang-fengyi', name: '张丰毅', role: '段小楼', type: 'Actor', palette: ['#92400e', '#451a03'], tmdbProfile: '1PoEPyXiHgXdk8jm1auaNBUjiZa.jpg' },
          { id: 'p-gong-li', name: '巩俐', role: '菊仙', type: 'Actor', palette: ['#be123c', '#4c0519'], tmdbProfile: 'm3q1rIEdZmOnUVC0MSo1FcIGvp9.jpg' },
        ],
        artById,
      ),
      MediaSources: makeMediaSources('item-farewell-my-concubine', '霸王别姬', {
        resolution: '4K',
        videoRange: 'HDR10',
        codec: 'hevc',
        runTimeTicks: 2 * HOUR_TICKS + 51 * MIN_TICKS,
        hasPgs: true,
      }),
      ImageTags: { Primary: 'demo-p' },
      BackdropImageTags: ['demo-b'],
      PrimaryImageAspectRatio: 0.667,
      UserData: {
        PlaybackPositionTicks: 0,
        PlayCount: 1,
        IsFavorite: true,
        Played: false,
      },
    },
    ['#450a0a', '#7f1d1d', '#f59e0b'],
    '4K 修复版',
  )

  registerItem(
    {
      Id: 'item-forrest-gump',
      Name: '阿甘正传',
      OriginalTitle: 'Forrest Gump',
      SortName: 'aganzhengzhuan forrest gump',
      ServerId: DEMO_SERVER_ID,
      Type: 'Movie',
      MediaType: 'Video',
      ParentId: 'lib-movies',
      ProductionYear: 1994,
      PremiereDate: '1994-07-06T00:00:00.000Z',
      DateCreated: isoAgo(55),
      OfficialRating: 'PG-13',
      CommunityRating: 9.5,
      RunTimeTicks: 2 * HOUR_TICKS + 22 * MIN_TICKS,
      Genres: ['喜剧', '剧情', '爱情'],
      Tags: ['豆瓣Top250', 'IMDb Top250'],
      ProviderIds: { Tmdb: '13', Imdb: 'tt0109830' },
      Studios: [{ Id: 'st-paramount', Name: 'Paramount Pictures' }],
      Overview:
        '阿甘出生在美国南方阿拉巴马州一个闭塞的小镇，智商只有75，然而他的妈妈是一位性格坚强的女性，她常常鼓励阿甘要自强不息。阿甘像普通孩子一样上学，并认识了一生的朋友和挚爱珍妮。在珍妮和妈妈的爱护下，阿甘凭着自己出众的奔跑能力，开始了一生不停的奔跑。他先后成为橄榄球明星、越战英雄、乒乓球外交使者和亿万富翁，但始终忘不了珍妮。',
      People: makePeople(
        [
          { id: 'p-zemeckis', name: '罗伯特·泽米吉斯', role: '导演', type: 'Director', palette: ['#0369a1', '#082f49'], tmdbProfile: 'lPYDQ5LYNJ12rJZENtyASmVZ1Ql.jpg' },
          { id: 'p-tom-hanks', name: '汤姆·汉克斯', role: '福雷斯特·甘', type: 'Actor', palette: ['#b45309', '#451a03'], tmdbProfile: 'oFvZoKI6lvU03n4YoNGAll9rkas.jpg' },
          { id: 'p-robin-wright', name: '罗宾·怀特', role: '珍妮·库兰', type: 'Actor', palette: ['#be185d', '#500724'], tmdbProfile: 'd3rIv0y2p0jMsQ7ViR7O1606NZa.jpg' },
          { id: 'p-gary-sinise', name: '加里·西尼斯', role: '丹·泰勒中尉', type: 'Actor', palette: ['#0f766e', '#042f2e'], tmdbProfile: 'olRjiV8ZhBixQiTvrGwXhpVXxsV.jpg' },
        ],
        artById,
      ),
      MediaSources: makeMediaSources('item-forrest-gump', '阿甘正传', {
        resolution: '4K',
        videoRange: 'Dolby Vision',
        codec: 'hevc',
        runTimeTicks: 2 * HOUR_TICKS + 22 * MIN_TICKS,
        hasPgs: true,
      }),
      ImageTags: { Primary: 'demo-p' },
      BackdropImageTags: ['demo-b'],
      PrimaryImageAspectRatio: 0.667,
      UserData: {
        PlaybackPositionTicks: 0,
        PlayCount: 1,
        IsFavorite: false,
        Played: true,
      },
    },
    ['#0c4a6e', '#0369a1', '#fbbf24'],
    '4K DV',
  )

  registerItem(
    {
      Id: 'item-wandering-earth-2',
      Name: '流浪地球 2',
      OriginalTitle: '流浪地球2',
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
      Genres: ['科幻', '动作', '剧情'],
      ProviderIds: { Tmdb: '842675', Imdb: 'tt22050754' },
      Studios: [{ Id: 'st-cfgc', Name: '中国电影股份有限公司' }],
      Overview:
        '在并不遥远的未来，太阳急速衰老与膨胀，再过几百年整个太阳系将被它吞噬毁灭。为了应对这场史无前例的危机，地球各国放下芥蒂，成立联合政府，最终推着地球逃出太阳系的“移山计划”获得压倒性胜利。满腔赤诚的刘培强、量子科学家图恒宇、联合政府中国代表周喆直以及无数平凡的地球人，共同构成了这项伟大计划的重要一环……',
      People: makePeople(
        [
          { id: 'p-guofan', name: '郭帆', role: '导演', type: 'Director', palette: ['#b45309', '#451a03'], tmdbProfile: 'wvRKczLxlqeDRMsJilWZpbHM9QA.jpg' },
          { id: 'p-wujing', name: '吴京', role: '刘培强', type: 'Actor', palette: ['#1d4ed8', '#172554'], tmdbProfile: 's7DOAkkBII5JuS7CcjaifGnJZlG.jpg' },
          { id: 'p-liudehua', name: '刘德华', role: '图恒宇', type: 'Actor', palette: ['#0f766e', '#042f2e'], tmdbProfile: 'eYZL8INePysNznxnRSDmPY9uiKc.jpg' },
          { id: 'p-lixuejian', name: '李雪健', role: '周喆直', type: 'Actor', palette: ['#7c2d12', '#27100b'], tmdbProfile: '22QFJKWJPNj3O3A6PdRKJc0qeKK.jpg' },
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
      Genres: ['动作', '科幻', '冒险'],
      Tags: ['豆瓣Top250', 'IMDb Top250'],
      ProviderIds: { Tmdb: '27205', Imdb: 'tt1375666' },
      Studios: [
        { Id: 'st-warner', Name: 'Warner Bros. Pictures' },
        { Id: 'st-syncopy', Name: 'Syncopy' },
      ],
      Overview:
        '道姆·柯布与同事阿瑟在一次针对日本能源大亨齐藤的盗梦行动中失败，反被齐藤利用。齐藤威逼利诱流亡海外的柯布，要求他在竞争对手继承人罗伯特·费希尔的深层潜意识中种下放弃家族公司、自立门户的想法。在一层层递进的梦境中，柯布不仅要对付潜意识的本能反抗，还必须直面已逝妻子梅尔的处处破坏……',
      People: makePeople(
        [
          { id: 'p-nolan', name: '克里斯托弗·诺兰', role: '导演', type: 'Director', palette: ['#3730a3', '#1e1b4b'], tmdbProfile: 'xuAIuYSmsUzKlUMBFGVZaWsY3DZ.jpg' },
          { id: 'p-leo', name: '莱昂纳多·迪卡普里奥', role: '道姆·柯布', type: 'Actor', palette: ['#9a3412', '#431407'], tmdbProfile: 'wo2hJpn04vbtmh0B9utCFdsQhxM.jpg' },
          { id: 'p-jgl', name: '约瑟夫·高登-莱维特', role: '阿瑟', type: 'Actor', palette: ['#155e75', '#083344'], tmdbProfile: 'z2FA8js799xqtfiFjBTicFYdfk.jpg' },
          { id: 'p-watanabe', name: '渡边谦', role: '齐藤', type: 'Actor', palette: ['#3f3f46', '#18181b'], tmdbProfile: 'AqK1DSCibb8VuMO6t5qH1LVMIYG.jpg' },
          { id: 'p-tom-hardy', name: '汤姆·哈迪', role: '艾姆斯', type: 'Actor', palette: ['#7c2d12', '#27100b'], tmdbProfile: 'd81K0RH8UX7tZj49tZaQhZ9ewH.jpg' },
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
      Genres: ['冒险', '剧情', '科幻'],
      Tags: ['豆瓣Top250', 'IMDb Top250'],
      ProviderIds: { Tmdb: '157336', Imdb: 'tt0816692' },
      Studios: [
        { Id: 'st-paramount', Name: 'Paramount Pictures' },
        { Id: 'st-syncopy', Name: 'Syncopy' },
      ],
      Overview:
        '近未来，地球环境持续恶化，沙尘暴与农作物枯萎病让人类面临日益严峻的生存危机。曾是NASA飞行员的库珀如今与家人在农场生活，一次发生在女儿墨菲房间里的异常重力现象，让他们意外发现了仍在秘密运作的NASA基地。为了寻找人类延续文明的希望，库珀不得不告别家人，与艾米莉亚·布兰德等人踏上穿越虫洞的星际旅程……',
      People: makePeople(
        [
          { id: 'p-nolan', name: '克里斯托弗·诺兰', role: '导演', type: 'Director', palette: ['#3730a3', '#1e1b4b'], tmdbProfile: 'xuAIuYSmsUzKlUMBFGVZaWsY3DZ.jpg' },
          { id: 'p-matthew', name: '马修·麦康纳', role: '约瑟夫·库珀', type: 'Actor', palette: ['#92400e', '#451a03'], tmdbProfile: 'lCySuYjhXix3FzQdS4oceDDrXKI.jpg' },
          { id: 'p-anne', name: '安妮·海瑟薇', role: '艾米莉亚·布兰德', type: 'Actor', palette: ['#6b21a8', '#3b0764'], tmdbProfile: 's6tflSD20MGz04ZR2R1lZvhmC4Y.jpg' },
          { id: 'p-chastain', name: '杰西卡·查斯坦', role: '墨菲·库珀', type: 'Actor', palette: ['#9a3412', '#431407'], tmdbProfile: 'lodMzLKSdrPcBry6TdoDsMN3Vge.jpg' },
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
      Genres: ['科幻', '冒险'],
      Tags: ['IMDb Top250'],
      ProviderIds: { Tmdb: '693134', Imdb: 'tt15239678' },
      Studios: [
        { Id: 'st-legendary', Name: 'Legendary Pictures' },
        { Id: 'st-warner', Name: 'Warner Bros. Pictures' },
      ],
      Overview:
        '改编自弗兰克·赫伯特的科幻小说《沙丘》。保罗·厄崔迪与契妮及弗雷曼人结盟，踏上向摧毁其家族的阴谋者复仇之路。面对挚爱与已知宇宙命运之间的抉择，他必须努力阻止自己预见的可怕未来。',
      People: makePeople(
        [
          { id: 'p-denis', name: '丹尼斯·维伦纽瓦', role: '导演', type: 'Director', palette: ['#9a3412', '#431407'], tmdbProfile: 'xzQYqb4nR8xT7Zdw5itbEL9K3fd.jpg' },
          { id: 'p-timothee', name: '提莫西·查拉梅', role: '保罗·厄崔迪', type: 'Actor', palette: ['#b45309', '#451a03'], tmdbProfile: 'c3NJxKbnKkQsttxA2XVh4pwPpIt.jpg' },
          { id: 'p-zendaya', name: '赞达亚', role: '契妮', type: 'Actor', palette: ['#7c2d12', '#27100b'], tmdbProfile: '1qup8tSt95HLbcy2c2xrx4iJNxv.jpg' },
          { id: 'p-rebecca', name: '丽贝卡·弗格森', role: '杰西卡夫人', type: 'Actor', palette: ['#475569', '#0f172a'], tmdbProfile: 'lJloTOheuQSirSLXNA3JHsrMNfH.jpg' },
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
      RunTimeTicks: 3 * HOUR_TICKS + 1 * MIN_TICKS,
      Genres: ['剧情', '历史', '战争'],
      Tags: ['豆瓣Top250', 'IMDb Top250'],
      ProviderIds: { Tmdb: '872585', Imdb: 'tt15398776' },
      Studios: [
        { Id: 'st-universal', Name: 'Universal Pictures' },
        { Id: 'st-syncopy', Name: 'Syncopy' },
      ],
      Overview:
        '第二次世界大战期间，为抢占军事科技的先机，美国启动绝密的“曼哈顿计划”。理论物理学家J·罗伯特·奥本海默受莱斯利·格罗夫斯将军邀请，率领众多科学家在洛斯阿拉莫斯研制原子弹。经过数年的努力，世界上首次核爆试验取得成功，人类历史由此迈入核武器时代，而奥本海默也逐渐卷入一场影响其命运的政治风暴……',
      People: makePeople(
        [
          { id: 'p-nolan', name: '克里斯托弗·诺兰', role: '导演', type: 'Director', palette: ['#3730a3', '#1e1b4b'], tmdbProfile: 'xuAIuYSmsUzKlUMBFGVZaWsY3DZ.jpg' },
          { id: 'p-cillian', name: '基里安·墨菲', role: 'J·罗伯特·奥本海默', type: 'Actor', palette: ['#991b1b', '#450a0a'], tmdbProfile: '2lKs67r7FI4bPu0AXxMUJZxmUXn.jpg' },
          { id: 'p-emily-blunt', name: '艾米莉·布朗特', role: '凯蒂·奥本海默', type: 'Actor', palette: ['#7c2d12', '#27100b'], tmdbProfile: '5nCSG5TL1bP1geD8aaBfaLnLLCD.jpg' },
          { id: 'p-matt-damon', name: '马特·达蒙', role: '莱斯利·格罗夫斯', type: 'Actor', palette: ['#0f766e', '#042f2e'], tmdbProfile: 'aCvBXTAR9B1qRjIRzMBYhhbm1fR.jpg' },
          { id: 'p-rdj', name: '小罗伯特·唐尼', role: '路易斯·施特劳斯', type: 'Actor', palette: ['#475569', '#0f172a'], tmdbProfile: 'f9toX5MahujsDZTJoKIa5kzdhAE.jpg' },
        ],
        artById,
      ),
      MediaSources: makeMediaSources('item-oppenheimer', '奥本海默', {
        resolution: '4K',
        videoRange: 'HDR10',
        codec: 'hevc',
        runTimeTicks: 3 * HOUR_TICKS + 1 * MIN_TICKS,
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
      OriginalTitle: '千と千尋の神隠し',
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
      Genres: ['动画', '家庭', '奇幻', '冒险'],
      Tags: ['豆瓣Top250', 'IMDb Top250'],
      ProviderIds: { Tmdb: '129', Imdb: 'tt0245429' },
      Studios: [{ Id: 'st-ghibli', Name: 'Studio Ghibli' }],
      Overview:
        '10岁的千寻跟随父母搬家途中，意外穿过一条神秘隧道，闯入了一个属于神灵的奇异世界。父母因为擅自吃下供奉给神灵的食物而被变成猪，惊慌失措的千寻则在神秘少年白龙的帮助下得以留下。为了在这个世界生存并寻找救回父母的方法，她不得不到汤婆婆掌管的“油屋”浴场工作……',
      People: makePeople(
        [
          { id: 'p-miyazaki', name: '宫崎骏', role: '导演 / 编剧', type: 'Director', palette: ['#047857', '#022c22'], tmdbProfile: 'ouhjt9KugzhWtdEyBPipihB3ic8.jpg' },
          { id: 'p-rumi-hiiragi', name: '柊瑠美', role: '千寻（配音）', type: 'Actor', palette: ['#0369a1', '#082f49'], tmdbProfile: 'nTKM4GgfkrZvAOFL55uhEuckt7M.jpg' },
          { id: 'p-miyu-irino', name: '入野自由', role: '白龙（配音）', type: 'Actor', palette: ['#0f766e', '#042f2e'], tmdbProfile: '8qEEhHUObNvGQr4e6eqLu5z4qTz.jpg' },
          { id: 'p-mari-natsuki', name: '夏木真理', role: '汤婆婆 / 钱婆婆（配音）', type: 'Actor', palette: ['#7c2d12', '#27100b'], tmdbProfile: 'MIFzUc77Sx57FQOZsoiGWEbpH2.jpg' },
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
      Genres: ['科幻', '剧情'],
      ProviderIds: { Tmdb: '335984', Imdb: 'tt1856101' },
      Studios: [
        { Id: 'st-alcon', Name: 'Alcon Entertainment' },
        { Id: 'st-warner', Name: 'Warner Bros. Pictures' },
      ],
      Overview:
        '在《银翼杀手》（1982）事件发生三十年后，新一代银翼杀手、洛杉矶警察“K”发现了一个尘封已久的秘密，这个秘密有可能使所剩无几的社会陷入混乱。K的这一发现引领他踏上寻找失踪三十年的前洛杉矶警察银翼杀手瑞克·戴克之路。',
      People: makePeople(
        [
          { id: 'p-denis', name: '丹尼斯·维伦纽瓦', role: '导演', type: 'Director', palette: ['#9a3412', '#431407'], tmdbProfile: 'xzQYqb4nR8xT7Zdw5itbEL9K3fd.jpg' },
          { id: 'p-gosling', name: '瑞恩·高斯林', role: '警官 K', type: 'Actor', palette: ['#db2777', '#500724'], tmdbProfile: 'lyUyVARQKhGxaxy0FbPJCQRpiaW.jpg' },
          { id: 'p-harrison-ford', name: '哈里森·福特', role: '瑞克·戴克', type: 'Actor', palette: ['#92400e', '#451a03'], tmdbProfile: 'pjBMJVPpcZK23Vt1nzr1zEBTWrP.jpg' },
          { id: 'p-ana-de-armas', name: '安娜·德·阿玛斯', role: '乔伊', type: 'Actor', palette: ['#be185d', '#500724'], tmdbProfile: 'ngHnBOofQzCO3AvMyuW3UMRWg1B.jpg' },
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
      OriginalTitle: '三体',
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
      ProviderIds: { Tmdb: '204541', Imdb: 'tt20242042' },
      Studios: [{ Id: 'st-tencent', Name: '企鹅影视 / 灵河文化' }],
      Overview:
        '2007年，地球基础科学出现了异常的扰动，一时间科学界风雨飘飘，人心惶惶。纳米科学家汪淼被警官史强带到联合作战中心，并潜入名为“科学边界”的组织协助调查。随着ETO与作战中心你来我往的不断博弈，汪淼和史强逐渐揭开两个文明为了生存空间孤注一掷的生死相逐。',
      People: makePeople(
        [
          { id: 'p-zhangluyi', name: '张鲁一', role: '汪淼', type: 'Actor', palette: ['#0f766e', '#042f2e'], tmdbProfile: 'sivFYbchmSPxnWG7hpXSG1XKyfd.jpg' },
          { id: 'p-yuhewei', name: '于和伟', role: '史强', type: 'Actor', palette: ['#b45309', '#451a03'], tmdbProfile: 'rtHbXVr0NUWYkV1vqCCdhgyyNXG.jpg' },
          { id: 'p-chenjin', name: '陈瑾', role: '老年叶文洁', type: 'Actor', palette: ['#3730a3', '#1e1b4b'], tmdbProfile: 'sO6FMRaUDmIL0eM89F01rtN3uQv.jpg' },
          { id: 'p-wangziwen', name: '王子文', role: '青年叶文洁', type: 'Actor', palette: ['#be185d', '#500724'], tmdbProfile: 'qcbGx4uKq3HAbAZRsZkrOztVgGe.jpg' },
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
      OriginalTitle: '進撃の巨人',
      SortName: 'jinjidejuren attack on titan shingeki no kyojin',
      ServerId: DEMO_SERVER_ID,
      Type: 'Series',
      ParentId: 'lib-tvshows',
      ProductionYear: 2013,
      PremiereDate: '2013-04-07T00:00:00.000Z',
      DateCreated: isoAgo(210),
      OfficialRating: 'TV-MA',
      CommunityRating: 9.6,
      Genres: ['动画', '动作冒险', '奇幻'],
      ProviderIds: { Tmdb: '1429', Imdb: 'tt2560140' },
      Studios: [
        { Id: 'st-wit', Name: 'Wit Studio' },
        { Id: 'st-mappa', Name: 'MAPPA' },
      ],
      Overview:
        '曾几何时，世界上突然出现无数身形庞大的巨人，普通人类的生命受到前所未有的威胁。为了保护自身安全，最后的人类筑起三座高墙。845 年，高达 60 米的超大型巨人和铠之巨人摧毁最外层的玛莉亚之壁，少年艾伦·耶格尔立誓加入调查兵团，人类和巨人的战争再度拉开序幕。',
      People: makePeople(
        [
          { id: 'p-kaji', name: '梶裕贵', role: '艾伦·耶格尔（配音）', type: 'Actor', palette: ['#1e3a8a', '#172554'], tmdbProfile: '8wKdPV11IwowfwoqGqMMNt9hmp6.jpg' },
          { id: 'p-inoue', name: '井上麻里奈', role: '阿尔敏·亚鲁雷特（配音）', type: 'Actor', palette: ['#0369a1', '#082f49'], tmdbProfile: 'f7PuEx1h2g4FLgMzk9wCrWuS9Eu.jpg' },
          { id: 'p-shimono', name: '下野纮', role: '康尼·斯普林格（配音）', type: 'Actor', palette: ['#b45309', '#451a03'], tmdbProfile: 'yrSDcgFefHtWkFmLnTrcw2t0MV.jpg' },
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
        'item-shawshank-redemption',
        'item-the-godfather',
        'item-the-dark-knight',
        'item-farewell-my-concubine',
        'item-forrest-gump',
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
        'item-shawshank-redemption',
        'item-the-godfather',
        'item-the-dark-knight',
        'item-farewell-my-concubine',
        'item-forrest-gump',
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
        '2026-11-24T08:00:01Z INFO  [db]     Loaded 3 virtual folders, 14 curated titles, 2 users (admin, guest)',
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
        '2026-11-24T06:10:00Z INFO  [scan]   Scanning /media/movies (12 items discovered)',
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
