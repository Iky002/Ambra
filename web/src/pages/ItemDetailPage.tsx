import { useEffect, useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  AlertTriangle,
  Check,
  CheckCircle2,
  ChevronDown,
  ChevronUp,
  Disc3,
  Download,
  Film,
  Flame,
  FolderPlus,
  GitMerge,
  Heart,
  Image as ImageIcon,
  Layers,
  ListVideo,
  Loader2,
  Music,
  Pause,
  Pencil,
  Play,
  Plus,
  RefreshCw,
  ScanSearch,
  Split,
  Star,
  Subtitles,
  Trash2,
  User,
  Volume2,
} from 'lucide-react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api, getBackdropUrl, getFallbackImageUrl, getImageUrl } from '../api/client'
import type { BaseItemDto, MediaSourceInfo, MediaStream } from '../api/types'
import { formatAudioTime } from '../components/AudioPlayerBar'
import { MediaCard } from '../components/MediaCard'
import { IdentifyModal } from '../components/media/IdentifyModal'
import { ImageManagerModal } from '../components/media/ImageManagerModal'
import { MergeVersionsModal } from '../components/media/MergeVersionsModal'
import { MetadataEditorModal } from '../components/media/MetadataEditorModal'
import { useAudioPlayer } from '../context/AudioPlayerContext'
import { useAuth } from '../context/AuthContext'
import { loadPlaybackPrefs } from '../player/playbackPrefs'
import {
  detectChineseVariant,
  formatTicks,
  getAudioLabel,
  getSubtitleMeta,
} from '../player/subtitleUtils'
import { AdminModal, FeedbackBanner } from './admin/adminUtils'

function matchesPreferredLang(stream: MediaStream, pref: string): boolean {
  const normalized = pref.trim().toLowerCase()
  if (!normalized) return false
  const haystack = [stream.Language, stream.DisplayTitle, stream.Title]
    .filter(Boolean)
    .join(' ')
    .toLowerCase()

  if (normalized === 'zh' || normalized === 'chi' || normalized === 'zho') {
    return (
      Boolean(detectChineseVariant(stream)) ||
      /\b(zh|chi|zho|chs|cht|cmn|yue)\b/.test(haystack) ||
      /中文|国语|國語|普通话|粤语|粵語|简体|繁體|繁体/.test(haystack)
    )
  }
  if (normalized === 'zh-cn' || normalized === 'chs' || normalized === 'sc') {
    return detectChineseVariant(stream) === '简体' || /\b(zh-cn|chs|sc)\b|简体|简中/.test(haystack)
  }
  if (normalized === 'zh-tw' || normalized === 'zh-hk' || normalized === 'cht' || normalized === 'tc') {
    return detectChineseVariant(stream) === '繁体' || /\b(zh-tw|zh-hk|cht|tc)\b|繁体|繁體|繁中/.test(haystack)
  }
  if (normalized === 'en' || normalized === 'eng') {
    return /\b(en|eng)\b|english|英语|英文/.test(haystack)
  }
  if (normalized === 'ja' || normalized === 'jpn') {
    return /\b(ja|jpn|jp)\b|japanese|日语|日文/.test(haystack)
  }
  return haystack.includes(normalized)
}

function resolvePreferredTracks(source?: MediaSourceInfo): {
  audioIdx: number | undefined
  subIdx: number | undefined
} {
  if (!source) return { audioIdx: undefined, subIdx: undefined }
  const prefs = loadPlaybackPrefs()
  const allStreams = source.MediaStreams || []
  const audios = allStreams.filter((s) => s.Type === 'Audio')
  const subs = allStreams.filter((s) => s.Type === 'Subtitle')

  let audioIdx = source.DefaultAudioStreamIndex
  if (prefs.preferredAudioLang) {
    const matchedAudio = audios.find((s) => matchesPreferredLang(s, prefs.preferredAudioLang))
    if (matchedAudio) audioIdx = matchedAudio.Index
  }

  let subIdx = source.DefaultSubtitleStreamIndex
  const prefSub = prefs.preferredSubtitleLang.trim().toLowerCase()
  if (prefSub === 'off' || prefSub === 'none') {
    subIdx = undefined
  } else if (prefSub) {
    const matchedSub = subs.find((s) => matchesPreferredLang(s, prefSub))
    if (matchedSub) subIdx = matchedSub.Index
  }

  return { audioIdx, subIdx }
}

export function ItemDetailPage() {
  const { id } = useParams<{ id: string }>()
  const { auth, user, isAdmin } = useAuth()
  const {
    currentTrack,
    isPlaying: isAudioPlaying,
    playQueue,
    playTrack,
    togglePlay: toggleAudioPlay,
  } = useAudioPlayer()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const userId = auth!.userId

  // 管理员与交互弹窗状态
  const [editModalOpen, setEditModalOpen] = useState(false)
  const [imageModalOpen, setImageModalOpen] = useState(false)
  const [identifyModalOpen, setIdentifyModalOpen] = useState(false)
  const [mergeModalOpen, setMergeModalOpen] = useState(false)
  const [deleteConfirmOpen, setDeleteConfirmOpen] = useState(false)
  const [addToColOpen, setAddToColOpen] = useState(false)
  const [addToColTab, setAddToColTab] = useState<'BoxSet' | 'Playlist'>('BoxSet')
  const [newColName, setNewColName] = useState('')
  const [showAllCast, setShowAllCast] = useState(false)
  const [toast, setToast] = useState<{ type: 'success' | 'error'; message: string } | null>(null)

  const showToast = (type: 'success' | 'error', message: string) => {
    setToast({ type, message })
  }

  useEffect(() => {
    if (!toast) return
    const timer = window.setTimeout(() => setToast(null), 4500)
    return () => window.clearTimeout(timer)
  }, [toast])

  const { data: item, isLoading, isError, error, refetch } = useQuery({
    queryKey: ['item', userId, id],
    queryFn: () => api.getItem(userId, id!),
    enabled: Boolean(id),
  })

  const isSeries = item?.Type === 'Series'
  const isMusicAlbum = item?.Type === 'MusicAlbum'
  const isMusicArtist = item?.Type === 'MusicArtist'
  const isAudio = item?.Type === 'Audio'

  const { data: albumTracksData, isLoading: albumTracksLoading } = useQuery({
    queryKey: ['albumTracks', id, userId],
    queryFn: () =>
      api.getItems(userId, {
        parentId: id,
        includeItemTypes: 'Audio',
        sortBy: 'ParentIndexNumber,IndexNumber,SortName',
        sortOrder: 'Ascending',
        limit: 200,
      }),
    enabled: Boolean(id && isMusicAlbum),
  })
  const albumTracks = albumTracksData?.Items || []

  const { data: artistAlbumsData, isLoading: artistAlbumsLoading } = useQuery({
    queryKey: ['artistAlbums', id, userId],
    queryFn: () =>
      api.getItems(userId, {
        parentId: id,
        includeItemTypes: 'MusicAlbum',
        sortBy: 'PremiereDate',
        sortOrder: 'Descending',
        limit: 100,
      }),
    enabled: Boolean(id && isMusicArtist),
  })
  const artistAlbums = artistAlbumsData?.Items || []

  const { data: artistTracksData, isLoading: artistTracksLoading } = useQuery({
    queryKey: ['artistTracks', id, userId],
    queryFn: () =>
      api.getItems(userId, {
        parentId: id,
        includeItemTypes: 'Audio',
        sortBy: 'PlayCount',
        sortOrder: 'Descending',
        limit: 50,
      }),
    enabled: Boolean(id && isMusicArtist),
  })
  const artistTracks = artistTracksData?.Items || []

  const { data: seasonsData } = useQuery({
    queryKey: ['seasons', id, userId],
    queryFn: () => api.getSeasons(id!, userId),
    enabled: Boolean(id && isSeries),
  })

  const seasons = seasonsData?.Items || []
  const [selectedSeasonId, setSelectedSeasonId] = useState<string | undefined>(undefined)

  useEffect(() => {
    setSelectedSeasonId(undefined)
  }, [id])

  useEffect(() => {
    if (seasons.length > 0 && !seasons.some((s) => s.Id === selectedSeasonId)) {
      setSelectedSeasonId(seasons[0].Id)
    }
  }, [seasons, selectedSeasonId])

  const { data: episodesData, isLoading: episodesLoading } = useQuery({
    queryKey: ['episodes', id, selectedSeasonId, userId],
    queryFn: () => api.getEpisodes(id!, selectedSeasonId!, userId),
    enabled: Boolean(id && isSeries && selectedSeasonId),
  })

  const episodes = episodesData?.Items || []
  const selectedSeason = seasons.find((s) => s.Id === selectedSeasonId) || null

  // 剧集「下一集 / 继续观看」查询（用于主播放按钮精准续播）
  const { data: nextUpData } = useQuery({
    queryKey: ['nextUp', userId, id],
    queryFn: () => api.getNextUp(userId, { seriesId: id!, limit: 1 }),
    enabled: Boolean(id && isSeries),
  })
  const nextUpEpisode = nextUpData?.Items?.[0] || null

  // 相似推荐查询
  const { data: similarData } = useQuery({
    queryKey: ['similarItems', id, userId],
    queryFn: () => api.getSimilarItems(id!, userId, 12),
    enabled: Boolean(id && item?.Id),
    staleTime: 60_000,
  })
  const similarItems = similarData?.Items || []

  // 加入合集 / 播放列表弹窗数据
  const { data: boxSetsForModal } = useQuery({
    queryKey: ['collections', userId, 'BoxSet'],
    queryFn: () =>
      api.getItems(userId, {
        includeItemTypes: 'BoxSet',
        recursive: true,
        sortBy: 'SortName',
        sortOrder: 'Ascending',
        limit: 100,
      }),
    enabled: addToColOpen,
  })

  const { data: playlistsForModal } = useQuery({
    queryKey: ['collections', userId, 'Playlist'],
    queryFn: () =>
      api.getItems(userId, {
        includeItemTypes: 'Playlist',
        recursive: true,
        sortBy: 'SortName',
        sortOrder: 'Ascending',
        limit: 100,
      }),
    enabled: addToColOpen,
  })

  // 多版本 / 音轨 / 字幕选择状态
  const mediaSources: MediaSourceInfo[] = item?.MediaSources || []
  const [selectedSourceId, setSelectedSourceId] = useState<string>('')
  const [selectedAudioIdx, setSelectedAudioIdx] = useState<number | undefined>(undefined)
  const [selectedSubIdx, setSelectedSubIdx] = useState<number | undefined>(undefined)

  useEffect(() => {
    if (mediaSources.length > 0) {
      const first = mediaSources[0]
      const { audioIdx, subIdx } = resolvePreferredTracks(first)
      setSelectedSourceId(first.Id)
      setSelectedAudioIdx(audioIdx)
      setSelectedSubIdx(subIdx)
    }
  }, [item?.Id, mediaSources.length])

  const activeSource = useMemo(
    () => mediaSources.find((s) => s.Id === selectedSourceId) || mediaSources[0],
    [mediaSources, selectedSourceId],
  )

  const streams = activeSource?.MediaStreams || []
  const videoStream = streams.find((s) => s.Type === 'Video')
  const audioStreams = streams.filter((s) => s.Type === 'Audio')
  const subtitleStreams = streams.filter((s) => s.Type === 'Subtitle')

  // 技术徽章提取（4K / HDR / 编码 / 简繁双语字幕）
  const techBadges = useMemo(() => {
    const badges: Array<{ label: string; accent?: boolean }> = []
    if (videoStream?.Width) {
      if (videoStream.Width >= 3800) badges.push({ label: '4K UHD', accent: true })
      else if (videoStream.Width >= 1900) badges.push({ label: '1080P' })
      else if (videoStream.Width >= 1200) badges.push({ label: '720P' })
    }
    if (videoStream?.VideoRange && videoStream.VideoRange.toUpperCase() !== 'SDR') {
      badges.push({ label: videoStream.VideoRange.toUpperCase(), accent: true })
    }
    if (videoStream?.Codec) {
      badges.push({ label: videoStream.Codec.toUpperCase() })
    }
    if (activeSource?.Container) {
      badges.push({ label: activeSource.Container.toUpperCase() })
    }

    const subTags = new Set<string>()
    for (const s of subtitleStreams) {
      const variant = detectChineseVariant(s)
      if (variant) subTags.add(`${variant}字幕`)
      const meta = getSubtitleMeta(s)
      if (meta.formatBadge === 'ASS' || meta.formatBadge === 'PGS') {
        subTags.add(meta.formatBadge)
      }
    }
    subTags.forEach((tag) => badges.push({ label: tag }))
    return badges
  }, [videoStream, activeSource, subtitleStreams])

  const favMutation = useMutation({
    mutationFn: () => api.toggleFavorite(userId, item!.Id, Boolean(item!.UserData?.IsFavorite)),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['item', userId, id] })
      queryClient.invalidateQueries({ queryKey: ['favorites', userId] })
    },
  })

  const playedMutation = useMutation({
    mutationFn: () => api.togglePlayed(userId, item!.Id, Boolean(item!.UserData?.Played)),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['item', userId, id] })
      queryClient.invalidateQueries({ queryKey: ['resumeItems', userId] })
    },
  })

  // 分集快速标记已看 / 收藏
  const episodePlayedMutation = useMutation({
    mutationFn: (ep: BaseItemDto) =>
      api.togglePlayed(userId, ep.Id, Boolean(ep.UserData?.Played)),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['episodes', id, selectedSeasonId, userId] })
      queryClient.invalidateQueries({ queryKey: ['nextUp', userId, id] })
      queryClient.invalidateQueries({ queryKey: ['seasons', id, userId] })
      queryClient.invalidateQueries({ queryKey: ['resumeItems', userId] })
    },
  })

  const episodeFavMutation = useMutation({
    mutationFn: (ep: BaseItemDto) =>
      api.toggleFavorite(userId, ep.Id, Boolean(ep.UserData?.IsFavorite)),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['episodes', id, selectedSeasonId, userId] })
      queryClient.invalidateQueries({ queryKey: ['favorites', userId] })
    },
  })

  // 标记整季已看 / 未看
  const seasonAllPlayed =
    episodes.length > 0 && episodes.every((ep) => Boolean(ep.UserData?.Played))
  const seasonPlayedCount = episodes.filter((ep) => Boolean(ep.UserData?.Played)).length

  const seasonPlayedMutation = useMutation({
    mutationFn: async () => {
      if (!selectedSeason) return
      const markAsUnplayed = seasonAllPlayed
      const targets = episodes.filter((ep) => Boolean(ep.UserData?.Played) === markAsUnplayed)
      await Promise.all([
        api.togglePlayed(userId, selectedSeason.Id, markAsUnplayed).catch(() => undefined),
        ...targets.map((ep) => api.togglePlayed(userId, ep.Id, markAsUnplayed)),
      ])
    },
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['episodes', id, selectedSeasonId, userId] }),
        queryClient.invalidateQueries({ queryKey: ['seasons', id, userId] }),
        queryClient.invalidateQueries({ queryKey: ['nextUp', userId, id] }),
        queryClient.invalidateQueries({ queryKey: ['resumeItems', userId] }),
      ])
      showToast(
        'success',
        seasonAllPlayed
          ? `已将「${selectedSeason?.Name}」标记为未看`
          : `已将「${selectedSeason?.Name}」标记为已看`,
      )
    },
    onError: (err) => {
      showToast('error', err instanceof Error ? err.message : '更新本季观看状态失败')
    },
  })

  // 加入已有合集 / 播放列表
  const addToContainerMutation = useMutation({
    mutationFn: async (target: { id: string; name: string; type: 'BoxSet' | 'Playlist' }) => {
      if (target.type === 'Playlist') {
        await api.addPlaylistItems(target.id, [item!.Id], userId)
      } else {
        await api.addCollectionItems(target.id, [item!.Id])
      }
      return target
    },
    onSuccess: async (target) => {
      await queryClient.invalidateQueries({ queryKey: ['collections', userId] })
      setAddToColOpen(false)
      showToast('success', `已将「${item!.Name}」加入「${target.name}」`)
    },
    onError: (err) => {
      showToast('error', err instanceof Error ? err.message : '加入失败')
    },
  })

  // 新建合集 / 播放列表并立即加入当前条目
  const createAndAddMutation = useMutation({
    mutationFn: async () => {
      const name = newColName.trim()
      if (!name) throw new Error('请输入名称')
      if (addToColTab === 'Playlist') {
        const mediaType = ['Audio', 'MusicAlbum', 'MusicArtist'].includes(item!.Type)
          ? 'Audio'
          : 'Video'
        return api.createPlaylist(name, userId, [item!.Id], mediaType)
      }
      return api.createCollection(name, [item!.Id])
    },
    onSuccess: async (created) => {
      setNewColName('')
      setAddToColOpen(false)
      await queryClient.invalidateQueries({ queryKey: ['collections', userId] })
      showToast('success', `已创建「${created.Name}」并加入「${item!.Name}」`)
    },
    onError: (err) => {
      showToast('error', err instanceof Error ? err.message : '创建并加入失败')
    },
  })

  const refreshMetadataMutation = useMutation({
    mutationFn: () => api.refreshItemMetadata(item!.Id, false),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['item', userId, id] })
      showToast('success', `已触发「${item!.Name}」的元数据刷新`)
    },
    onError: (err) => {
      showToast('error', err instanceof Error ? err.message : '刷新元数据失败')
    },
  })

  const splitVersionsMutation = useMutation({
    mutationFn: () => api.splitItemVersions(item!.Id),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['item', userId, id] }),
        queryClient.invalidateQueries({ queryKey: ['items'] }),
        queryClient.invalidateQueries({ queryKey: ['latestItems'] }),
      ])
      showToast('success', `已拆分「${item!.Name}」的多版本为独立条目`)
    },
    onError: (err) => {
      showToast('error', err instanceof Error ? err.message : '拆分多版本失败')
    },
  })

  const deleteItemMutation = useMutation({
    mutationFn: () => api.deleteItem(item!.Id),
    onSuccess: async () => {
      const targetPath = item?.ParentId ? `/library/${item.ParentId}` : '/'
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['items'] }),
        queryClient.invalidateQueries({ queryKey: ['latestItems'] }),
        queryClient.invalidateQueries({ queryKey: ['resumeItems'] }),
      ])
      setDeleteConfirmOpen(false)
      navigate(targetPath, { replace: true })
    },
    onError: (err) => {
      setDeleteConfirmOpen(false)
      showToast('error', err instanceof Error ? err.message : '删除媒体失败')
    },
  })

  if (isLoading) {
    return (
      <div className="flex h-[60vh] items-center justify-center">
        <Loader2 className="h-8 w-8 animate-spin text-amber-400" />
      </div>
    )
  }

  if (isError || !item) {
    const errorMsg =
      error instanceof Error && error.message ? error.message : '未找到该媒体项目或加载失败'
    return (
      <div className="flex min-h-[60vh] items-center justify-center px-4">
        <div className="w-full max-w-md rounded-2xl border border-white/10 bg-zinc-950/90 p-8 text-center shadow-2xl">
          <div className="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-2xl bg-red-500/15 text-red-400 ring-1 ring-red-500/30">
            <AlertTriangle className="h-6 w-6" />
          </div>
          <h2 className="text-lg font-semibold text-white">无法加载媒体详情</h2>
          <p className="mt-2 text-sm text-zinc-400">{errorMsg}</p>
          <div className="mt-6 flex items-center justify-center gap-3">
            <button
              type="button"
              onClick={() => void refetch()}
              className="inline-flex items-center gap-1.5 rounded-xl bg-amber-500 px-5 py-2.5 text-xs font-semibold text-zinc-950 transition-colors hover:bg-amber-400"
            >
              <RefreshCw className="h-3.5 w-3.5" />
              <span>重试</span>
            </button>
            <button
              type="button"
              onClick={() => navigate('/')}
              className="inline-flex items-center gap-1.5 rounded-xl border border-white/12 bg-white/[0.05] px-5 py-2.5 text-xs font-medium text-zinc-200 transition-colors hover:bg-white/10"
            >
              <span>返回首页</span>
            </button>
          </div>
        </div>
      </div>
    )
  }

  const backdrop = getBackdropUrl(item, 1920)
  const backdropFallback = getFallbackImageUrl(item.Id, 'Backdrop')
  const poster = item.ImageTags?.Primary
    ? getImageUrl(item.Id, 'Primary', { maxWidth: 500, tag: item.ImageTags.Primary })
    : item.SeriesId
      ? getImageUrl(item.SeriesId, 'Primary', { maxWidth: 500 })
      : null
  const posterFallback = item.ImageTags?.Primary
    ? getFallbackImageUrl(item.Id, 'Primary')
    : item.SeriesId
      ? getFallbackImageUrl(item.SeriesId, 'Primary')
      : null

  const applyFallbackOrHide = (
    e: React.SyntheticEvent<HTMLImageElement>,
    fallback: string | null,
  ) => {
    const el = e.currentTarget
    if (fallback && !el.dataset.fallbackApplied && el.src !== fallback) {
      el.dataset.fallbackApplied = '1'
      el.src = fallback
      return
    }
    el.style.display = 'none'
  }

  const resumeTicks = item.UserData?.PlaybackPositionTicks || 0
  const hasResume = resumeTicks > 10_000_000 * 15 // 超过 15 秒显示续播
  const canDownload =
    !isSeries &&
    !isMusicAlbum &&
    !isMusicArtist &&
    (isAdmin || Boolean(user?.Policy?.EnableContentDownloading))

  const buildFilterLink = (param: 'genre' | 'year' | 'tag', value: string) => {
    if (item.ParentId && (item.Type === 'Movie' || item.Type === 'Series')) {
      return `/library/${encodeURIComponent(item.ParentId)}?${param}=${encodeURIComponent(value)}`
    }
    return `/search?q=${encodeURIComponent(value)}`
  }

  const formatEpisodeCode = (ep: BaseItemDto) => {
    if (ep.ParentIndexNumber != null && ep.IndexNumber != null) {
      return `S${String(ep.ParentIndexNumber).padStart(2, '0')}E${String(ep.IndexNumber).padStart(2, '0')}`
    }
    if (ep.IndexNumber != null) {
      return `第 ${ep.IndexNumber} 集`
    }
    return ep.Name
  }

  const primarySeriesTarget = nextUpEpisode || episodes[0] || null
  const seriesPlayLabel = primarySeriesTarget
    ? (primarySeriesTarget.UserData?.PlaybackPositionTicks || 0) > 10_000_000 * 15 ||
      (primarySeriesTarget.IndexNumber != null && primarySeriesTarget.IndexNumber > 1) ||
      (primarySeriesTarget.ParentIndexNumber != null && primarySeriesTarget.ParentIndexNumber > 1)
      ? `继续播放 ${formatEpisodeCode(primarySeriesTarget)}`
      : `播放 ${formatEpisodeCode(primarySeriesTarget)}`
    : '播放第 1 集'

  const customTags = (item.Tags || []).filter((t) => {
    const lower = t.toLowerCase()
    return (
      lower !== 'trending' &&
      t !== '豆瓣Top250' &&
      t !== '豆瓣 Top250' &&
      t !== 'IMDb Top250' &&
      t !== 'IMDb Top 250'
    )
  })

  const handlePlay = (targetId = item.Id, startTicks = 0) => {
    const params = new URLSearchParams()
    if (targetId === item.Id) {
      if (activeSource?.Id) params.set('mediaSourceId', activeSource.Id)
      if (selectedAudioIdx !== undefined) params.set('audio', String(selectedAudioIdx))
      if (selectedSubIdx !== undefined) params.set('subtitle', String(selectedSubIdx))
    }
    if (startTicks > 0) params.set('startTicks', String(startTicks))
    const qs = params.toString()
    navigate(`/play/${targetId}${qs ? `?${qs}` : ''}`)
  }

  return (
    <div className="relative">
      {/* 沉浸式大背景图与暗色渐变融合 */}
      <div className="relative min-h-[420px] w-full overflow-hidden">
        {backdrop ? (
          <img
            src={backdrop}
            alt={item.Name}
            onError={(e) => applyFallbackOrHide(e, backdropFallback)}
            className="absolute inset-0 h-full w-full object-cover object-top opacity-45"
          />
        ) : (
          <div className="absolute inset-0 bg-gradient-to-br from-amber-950/20 via-zinc-900 to-[#09090d]" />
        )}
        <div className="absolute inset-0 bg-gradient-to-t from-[#09090d] via-[#09090d]/75 to-[#09090d]/30" />
        <div className="absolute inset-0 bg-gradient-to-r from-[#09090d]/90 via-[#09090d]/45 to-transparent" />

        <div className="relative z-10 mx-auto max-w-7xl px-4 pb-10 pt-10 lg:px-8 lg:pt-14">
          <div className="flex flex-col gap-8 md:flex-row">
            {/* 海报 */}
            <div className="w-44 shrink-0 self-start overflow-hidden rounded-2xl bg-zinc-900 shadow-2xl shadow-black/80 ring-1 ring-white/15 sm:w-52 md:w-60">
              {poster ? (
                <img
                  src={poster}
                  alt={item.Name}
                  onError={(e) => applyFallbackOrHide(e, posterFallback)}
                  className="aspect-[2/3] w-full object-cover"
                />
              ) : (
                <div className="flex aspect-[2/3] items-center justify-center text-zinc-600">
                  <Film className="h-14 w-14 opacity-40" />
                </div>
              )}
            </div>

            {/* 核心元信息与交互操作 */}
            <div className="min-w-0 flex-1 space-y-5">
              <div>
                {item.SeriesName && item.SeriesId && (
                  <Link
                    to={`/item/${item.SeriesId}`}
                    className="mb-1 inline-block text-sm font-medium text-amber-400 hover:text-amber-300"
                  >
                    {item.SeriesName}
                  </Link>
                )}
                <h1 className="text-3xl font-bold tracking-tight text-white sm:text-4xl">
                  {item.Name}
                </h1>
                {item.OriginalTitle && item.OriginalTitle !== item.Name && (
                  <p className="mt-1 text-sm text-zinc-400">{item.OriginalTitle}</p>
                )}
              </div>

              {/* 评分与基础信息 */}
              <div className="flex flex-wrap items-center gap-3 text-sm text-zinc-300">
                {item.CommunityRating ? (
                  <span className="flex items-center gap-1 rounded-lg bg-amber-500/15 px-2.5 py-1 font-semibold text-amber-400 ring-1 ring-amber-400/30">
                    <Star className="h-3.5 w-3.5 fill-amber-400" />
                    {item.CommunityRating.toFixed(1)}
                  </span>
                ) : null}
                {item.ProductionYear && (
                  <Link
                    to={buildFilterLink('year', String(item.ProductionYear))}
                    className="rounded-md px-1.5 py-0.5 transition-colors hover:bg-amber-500/15 hover:text-amber-300"
                    title={`查看 ${item.ProductionYear} 年作品`}
                  >
                    {item.ProductionYear}
                  </Link>
                )}
                {item.OfficialRating && (
                  <span className="rounded border border-white/20 px-1.5 py-0.5 text-xs">
                    {item.OfficialRating}
                  </span>
                )}
                {item.RunTimeTicks && <span>{formatTicks(item.RunTimeTicks)}</span>}
                {item.Genres?.map((g) => (
                  <Link
                    key={g}
                    to={buildFilterLink('genre', g)}
                    className="rounded-full bg-white/8 px-2.5 py-0.5 text-xs text-zinc-300 transition-colors hover:bg-amber-500/20 hover:text-amber-200"
                    title={`按流派「${g}」筛选`}
                  >
                    {g}
                  </Link>
                ))}
              </div>

              {/* 榜单与技术徽章：热门 / 豆瓣 Top250 / IMDb Top250 / 标签 / 4K / HDR / 简繁字幕 / ASS / PGS */}
              {(techBadges.length > 0 || (item.Tags && item.Tags.length > 0)) && (
                <div className="flex flex-wrap items-center gap-2">
                  {item.Tags?.some((t) => t.toLowerCase() === 'trending') && (
                    <Link
                      to="/trending"
                      className="inline-flex items-center gap-1 rounded-md border border-orange-400/55 bg-gradient-to-r from-orange-950/90 via-amber-900/80 to-orange-950/90 px-2.5 py-0.5 text-[11px] font-semibold tracking-wide text-orange-200 shadow-[0_0_12px_rgba(249,115,22,0.22)] transition-opacity hover:opacity-90"
                    >
                      <Flame className="h-3 w-3 text-orange-400" />
                      <span>热门趋势</span>
                    </Link>
                  )}
                  {item.Tags?.some((t) => t === '豆瓣Top250' || t === '豆瓣 Top250') && (
                    <Link
                      to={buildFilterLink('tag', '豆瓣Top250')}
                      className="inline-flex items-center rounded-md border border-emerald-400/50 bg-gradient-to-r from-emerald-950/90 via-emerald-900/80 to-teal-950/90 px-2.5 py-0.5 text-[11px] font-semibold tracking-wide text-emerald-200 shadow-[0_0_12px_rgba(16,185,129,0.2)] transition-opacity hover:opacity-90"
                    >
                      豆瓣 Top250
                    </Link>
                  )}
                  {item.Tags?.some((t) => t === 'IMDb Top250' || t === 'IMDb Top 250') && (
                    <Link
                      to={buildFilterLink('tag', 'IMDb Top250')}
                      className="inline-flex items-center rounded-md border border-amber-400/55 bg-gradient-to-r from-amber-950/90 via-yellow-900/75 to-amber-950/90 px-2.5 py-0.5 text-[11px] font-semibold tracking-wide text-amber-200 shadow-[0_0_12px_rgba(245,158,11,0.22)] transition-opacity hover:opacity-90"
                    >
                      IMDb Top250
                    </Link>
                  )}
                  {customTags.map((tag) => (
                    <Link
                      key={tag}
                      to={buildFilterLink('tag', tag)}
                      className="rounded-md bg-white/[0.06] px-2 py-0.5 text-[11px] font-medium text-zinc-300 ring-1 ring-white/10 transition-colors hover:bg-amber-500/20 hover:text-amber-200"
                    >
                      #{tag}
                    </Link>
                  ))}
                  {techBadges.map((b) => (
                    <span
                      key={b.label}
                      className={`rounded-md px-2 py-0.5 text-[11px] font-semibold tracking-wide ${
                        b.accent
                          ? 'bg-amber-500/20 text-amber-300 ring-1 ring-amber-400/40'
                          : 'bg-white/8 text-zinc-200 ring-1 ring-white/10'
                      }`}
                    >
                      {b.label}
                    </span>
                  ))}
                </div>
              )}

              {/* 播放 / 收藏 / 标已看 / 加入合集 / 下载按钮 */}
              <div className="flex flex-wrap items-center gap-3 pt-1">
                {isMusicAlbum ? (
                  <button
                    type="button"
                    disabled={albumTracks.length === 0}
                    onClick={() => playQueue(albumTracks, 0)}
                    className="inline-flex items-center gap-2 rounded-xl bg-amber-500 px-6 py-3 text-sm font-semibold text-zinc-950 shadow-lg shadow-amber-500/25 transition-all duration-200 hover:-translate-y-0.5 hover:bg-amber-400 active:scale-[0.97] disabled:opacity-50"
                  >
                    <Play className="h-4 w-4 fill-current" />
                    <span>播放全部</span>
                  </button>
                ) : isMusicArtist ? (
                  artistTracks.length > 0 ? (
                    <button
                      type="button"
                      onClick={() => playQueue(artistTracks, 0)}
                      className="inline-flex items-center gap-2 rounded-xl bg-amber-500 px-6 py-3 text-sm font-semibold text-zinc-950 shadow-lg shadow-amber-500/25 transition-all duration-200 hover:-translate-y-0.5 hover:bg-amber-400 active:scale-[0.97]"
                    >
                      <Play className="h-4 w-4 fill-current" />
                      <span>播放全部</span>
                    </button>
                  ) : null
                ) : isAudio ? (
                  <button
                    type="button"
                    onClick={() => playTrack(item)}
                    className="inline-flex items-center gap-2 rounded-xl bg-amber-500 px-6 py-3 text-sm font-semibold text-zinc-950 shadow-lg shadow-amber-500/25 transition-all duration-200 hover:-translate-y-0.5 hover:bg-amber-400 active:scale-[0.97]"
                  >
                    <Play className="h-4 w-4 fill-current" />
                    <span>立即播放</span>
                  </button>
                ) : !isSeries ? (
                  <>
                    <button
                      type="button"
                      onClick={() => handlePlay(item.Id, hasResume ? resumeTicks : 0)}
                      className="inline-flex items-center gap-2 rounded-xl bg-amber-500 px-6 py-3 text-sm font-semibold text-zinc-950 shadow-lg shadow-amber-500/25 transition-all duration-200 hover:-translate-y-0.5 hover:bg-amber-400 active:scale-[0.97]"
                    >
                      <Play className="h-4 w-4 fill-current" />
                      <span>{hasResume ? '继续播放' : '立即播放'}</span>
                    </button>
                    {hasResume && (
                      <button
                        type="button"
                        onClick={() => handlePlay(item.Id, 0)}
                        className="inline-flex items-center gap-2 rounded-xl border border-white/15 bg-white/8 px-4 py-3 text-sm font-medium text-zinc-100 transition-all duration-200 hover:-translate-y-0.5 hover:bg-white/12 active:scale-[0.97]"
                      >
                        从头播放
                      </button>
                    )}
                  </>
                ) : primarySeriesTarget ? (
                  <button
                    type="button"
                    onClick={() =>
                      handlePlay(
                        primarySeriesTarget.Id,
                        primarySeriesTarget.UserData?.PlaybackPositionTicks || 0,
                      )
                    }
                    className="inline-flex items-center gap-2 rounded-xl bg-amber-500 px-6 py-3 text-sm font-semibold text-zinc-950 shadow-lg shadow-amber-500/25 transition-all duration-200 hover:-translate-y-0.5 hover:bg-amber-400 active:scale-[0.97]"
                  >
                    <Play className="h-4 w-4 fill-current" />
                    <span>{seriesPlayLabel}</span>
                  </button>
                ) : null}

                <button
                  type="button"
                  onClick={() => favMutation.mutate()}
                  className={`inline-flex items-center gap-2 rounded-xl border px-4 py-3 text-sm font-medium transition-all duration-200 hover:-translate-y-0.5 active:scale-[0.97] ${
                    item.UserData?.IsFavorite
                      ? 'border-rose-500/50 bg-rose-500/15 text-rose-300'
                      : 'border-white/12 bg-white/[0.05] text-zinc-200 hover:bg-white/10'
                  }`}
                >
                  <Heart
                    className={`h-4 w-4 ${item.UserData?.IsFavorite ? 'fill-current' : ''}`}
                  />
                  <span>{item.UserData?.IsFavorite ? '已收藏' : '收藏'}</span>
                </button>

                <button
                  type="button"
                  onClick={() => playedMutation.mutate()}
                  className={`inline-flex items-center gap-2 rounded-xl border px-4 py-3 text-sm font-medium transition-all duration-200 hover:-translate-y-0.5 active:scale-[0.97] ${
                    item.UserData?.Played
                      ? 'border-emerald-500/50 bg-emerald-500/15 text-emerald-300'
                      : 'border-white/12 bg-white/[0.05] text-zinc-200 hover:bg-white/10'
                  }`}
                >
                  <Check className="h-4 w-4" />
                  <span>{item.UserData?.Played ? '已观看' : '标记已看'}</span>
                </button>

                <button
                  type="button"
                  onClick={() => setAddToColOpen(true)}
                  className="inline-flex items-center gap-2 rounded-xl border border-white/12 bg-white/[0.05] px-4 py-3 text-sm font-medium text-zinc-200 transition-all duration-200 hover:-translate-y-0.5 hover:border-amber-400/40 hover:bg-white/10 hover:text-amber-200 active:scale-[0.97]"
                >
                  <FolderPlus className="h-4 w-4" />
                  <span>加入合集 / 片单</span>
                </button>

                {canDownload && (
                  <a
                    href={api.getDownloadUrl(item.Id)}
                    download
                    className="inline-flex items-center gap-2 rounded-xl border border-white/12 bg-white/[0.05] px-4 py-3 text-sm font-medium text-zinc-200 transition-all duration-200 hover:-translate-y-0.5 hover:border-amber-400/40 hover:bg-white/10 hover:text-amber-200 active:scale-[0.97]"
                  >
                    <Download className="h-4 w-4" />
                    <span>下载原文件</span>
                  </a>
                )}
              </div>

              {toast && (
                <FeedbackBanner
                  message={toast.message}
                  type={toast.type}
                  onClose={() => setToast(null)}
                />
              )}

              {/* 管理员操作栏 */}
              {isAdmin && (
                <div className="space-y-3 pt-1">
                  <div className="flex flex-wrap items-center gap-2 rounded-2xl border border-amber-500/20 bg-zinc-950/65 p-2.5 backdrop-blur-md">
                    <span className="px-2 text-[11px] font-semibold uppercase tracking-wider text-amber-400/90">
                      管理员操作
                    </span>
                    <button
                      type="button"
                      onClick={() => setEditModalOpen(true)}
                      className="inline-flex items-center gap-1.5 rounded-xl border border-white/10 bg-white/[0.04] px-3 py-1.5 text-xs font-medium text-zinc-200 transition-colors hover:border-amber-400/40 hover:bg-white/10 hover:text-amber-200"
                    >
                      <Pencil className="h-3.5 w-3.5" />
                      <span>编辑元数据</span>
                    </button>
                    <button
                      type="button"
                      onClick={() => setImageModalOpen(true)}
                      className="inline-flex items-center gap-1.5 rounded-xl border border-white/10 bg-white/[0.04] px-3 py-1.5 text-xs font-medium text-zinc-200 transition-colors hover:border-amber-400/40 hover:bg-white/10 hover:text-amber-200"
                    >
                      <ImageIcon className="h-3.5 w-3.5" />
                      <span>编辑图片/海报</span>
                    </button>
                    <button
                      type="button"
                      onClick={() => setIdentifyModalOpen(true)}
                      className="inline-flex items-center gap-1.5 rounded-xl border border-white/10 bg-white/[0.04] px-3 py-1.5 text-xs font-medium text-zinc-200 transition-colors hover:border-amber-400/40 hover:bg-white/10 hover:text-amber-200"
                    >
                      <ScanSearch className="h-3.5 w-3.5" />
                      <span>识别</span>
                    </button>
                    <button
                      type="button"
                      disabled={refreshMetadataMutation.isPending}
                      onClick={() => refreshMetadataMutation.mutate()}
                      className="inline-flex items-center gap-1.5 rounded-xl border border-white/10 bg-white/[0.04] px-3 py-1.5 text-xs font-medium text-zinc-200 transition-colors hover:border-amber-400/40 hover:bg-white/10 hover:text-amber-200 disabled:opacity-50"
                    >
                      <RefreshCw
                        className={`h-3.5 w-3.5 ${refreshMetadataMutation.isPending ? 'animate-spin' : ''}`}
                      />
                      <span>刷新元数据</span>
                    </button>
                    {(item.Type === 'Movie' || item.Type === 'Video') && (
                      <button
                        type="button"
                        onClick={() => setMergeModalOpen(true)}
                        className="inline-flex items-center gap-1.5 rounded-xl border border-white/10 bg-white/[0.04] px-3 py-1.5 text-xs font-medium text-zinc-200 transition-colors hover:border-amber-400/40 hover:bg-white/10 hover:text-amber-200"
                      >
                        <GitMerge className="h-3.5 w-3.5" />
                        <span>合并版本</span>
                      </button>
                    )}
                    {mediaSources.length > 1 && (
                      <button
                        type="button"
                        disabled={splitVersionsMutation.isPending}
                        onClick={() => splitVersionsMutation.mutate()}
                        className="inline-flex items-center gap-1.5 rounded-xl border border-amber-500/30 bg-amber-500/10 px-3 py-1.5 text-xs font-medium text-amber-200 transition-colors hover:bg-amber-500/20 disabled:opacity-50"
                      >
                        <Split className="h-3.5 w-3.5" />
                        <span>拆分多版本</span>
                      </button>
                    )}
                    <button
                      type="button"
                      onClick={() => setDeleteConfirmOpen(true)}
                      className="inline-flex items-center gap-1.5 rounded-xl border border-red-500/30 bg-red-500/10 px-3 py-1.5 text-xs font-medium text-red-300 transition-colors hover:bg-red-500/20"
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                      <span>删除媒体</span>
                    </button>
                  </div>
                </div>
              )}

              {/* 多版本、音轨、字幕选择器 */}
              {!isSeries && !isMusicAlbum && !isMusicArtist && !isAudio && mediaSources.length > 0 && (
                <div className="grid gap-3 rounded-2xl border border-white/10 bg-zinc-950/70 p-4 backdrop-blur-md sm:grid-cols-3">
                  {mediaSources.length > 1 && (
                    <label className="block space-y-1.5">
                      <span className="flex items-center gap-1.5 text-xs font-medium text-zinc-400">
                        <Layers className="h-3.5 w-3.5 text-amber-400" />
                        视频版本
                      </span>
                      <select
                        value={activeSource?.Id || ''}
                        onChange={(e) => {
                          const next = mediaSources.find((s) => s.Id === e.target.value)
                          const { audioIdx, subIdx } = resolvePreferredTracks(next)
                          setSelectedSourceId(e.target.value)
                          setSelectedAudioIdx(audioIdx)
                          setSelectedSubIdx(subIdx)
                        }}
                        className="w-full rounded-xl border border-white/10 bg-zinc-900 px-3 py-2 text-xs text-zinc-100 outline-none focus:border-amber-400/50"
                      >
                        {mediaSources.map((src) => (
                          <option key={src.Id} value={src.Id}>
                            {src.Name}
                          </option>
                        ))}
                      </select>
                    </label>
                  )}

                  <label className="block space-y-1.5">
                    <span className="flex items-center gap-1.5 text-xs font-medium text-zinc-400">
                      <Volume2 className="h-3.5 w-3.5 text-amber-400" />
                      音轨 ({audioStreams.length})
                    </span>
                    <select
                      value={selectedAudioIdx ?? ''}
                      onChange={(e) =>
                        setSelectedAudioIdx(e.target.value ? Number(e.target.value) : undefined)
                      }
                      className="w-full rounded-xl border border-white/10 bg-zinc-900 px-3 py-2 text-xs text-zinc-100 outline-none focus:border-amber-400/50"
                    >
                      {audioStreams.length === 0 && <option value="">默认音轨</option>}
                      {audioStreams.map((a) => (
                        <option key={a.Index} value={a.Index}>
                          {getAudioLabel(a)}
                        </option>
                      ))}
                    </select>
                  </label>

                  <label className="block space-y-1.5">
                    <span className="flex items-center gap-1.5 text-xs font-medium text-zinc-400">
                      <Subtitles className="h-3.5 w-3.5 text-amber-400" />
                      字幕 ({subtitleStreams.length})
                    </span>
                    <select
                      value={selectedSubIdx ?? -1}
                      onChange={(e) => {
                        const v = Number(e.target.value)
                        setSelectedSubIdx(v === -1 ? undefined : v)
                      }}
                      className="w-full rounded-xl border border-white/10 bg-zinc-900 px-3 py-2 text-xs text-zinc-100 outline-none focus:border-amber-400/50"
                    >
                      <option value={-1}>关闭字幕</option>
                      {subtitleStreams.map((s) => {
                        const meta = getSubtitleMeta(s)
                        return (
                          <option key={s.Index} value={s.Index}>
                            {meta.displayLabel}
                          </option>
                        )
                      })}
                    </select>
                  </label>
                </div>
              )}

              {/* 剧情简介 */}
              {item.Overview && (
                <p className="max-w-3xl text-sm leading-relaxed text-zinc-300">{item.Overview}</p>
              )}
            </div>
          </div>
        </div>
      </div>

      {/* 音乐专辑：曲目列表 */}
      {isMusicAlbum && (
        <section
          data-testid="album-tracklist"
          className="mx-auto max-w-7xl space-y-4 px-4 pt-6 lg:px-8"
        >
          <div className="flex items-center justify-between gap-3">
            <div className="flex items-center gap-2">
              <Disc3 className="h-5 w-5 text-amber-400" />
              <h2 className="text-lg font-semibold text-zinc-100">
                专辑曲目
                <span className="ml-2 text-xs font-normal text-zinc-500">
                  ({albumTracks.length} 首)
                </span>
              </h2>
            </div>
            {albumTracks.length > 0 && (
              <button
                type="button"
                onClick={() => playQueue(albumTracks, 0)}
                className="inline-flex items-center gap-1.5 rounded-xl border border-amber-500/35 bg-amber-500/15 px-3.5 py-1.5 text-xs font-medium text-amber-300 transition-colors hover:bg-amber-500/25"
              >
                <Play className="h-3.5 w-3.5 fill-current" />
                <span>顺序播放全部</span>
              </button>
            )}
          </div>

          {albumTracksLoading ? (
            <div className="flex h-36 items-center justify-center">
              <Loader2 className="h-6 w-6 animate-spin text-amber-400" />
            </div>
          ) : albumTracks.length === 0 ? (
            <div className="rounded-2xl border border-white/8 bg-white/[0.02] py-12 text-center text-sm text-zinc-500">
              当前专辑暂无音轨
            </div>
          ) : (
            <div className="overflow-hidden rounded-2xl border border-white/8 bg-white/[0.02]">
              <table className="w-full text-left text-sm">
                <thead className="border-b border-white/8 text-[11px] uppercase tracking-wider text-zinc-500">
                  <tr>
                    <th className="w-14 px-4 py-3 text-center">#</th>
                    <th className="px-3 py-3">曲目</th>
                    <th className="hidden px-3 py-3 sm:table-cell">艺术家</th>
                    <th className="w-24 px-4 py-3 text-right">时长</th>
                    <th className="w-20 px-4 py-3 text-right">操作</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-white/5">
                  {albumTracks.map((track, idx) => {
                    const isActive = currentTrack?.Id === track.Id
                    const artistLabel =
                      track.Artists?.join(' / ') || track.AlbumArtist || item.AlbumArtist || '未知艺术家'
                    return (
                      <tr
                        key={track.Id}
                        onClick={() => {
                          if (isActive) {
                            toggleAudioPlay()
                          } else {
                            playQueue(albumTracks, idx)
                          }
                        }}
                        className={`group cursor-pointer transition-colors ${
                          isActive
                            ? 'bg-amber-500/12 text-amber-300'
                            : 'text-zinc-200 hover:bg-white/[0.04]'
                        }`}
                      >
                        <td className="px-4 py-3 text-center text-xs tabular-nums text-zinc-500">
                          {isActive ? (
                            <span className="inline-flex items-center justify-center text-amber-400">
                              <Music className="h-3.5 w-3.5 animate-pulse" />
                            </span>
                          ) : (
                            track.IndexNumber ?? idx + 1
                          )}
                        </td>
                        <td className="px-3 py-3">
                          <div className="flex items-center gap-2">
                            <span className="font-medium">{track.Name}</span>
                            {isActive && (
                              <span className="rounded-full bg-amber-500/20 px-2 py-0.5 text-[10px] font-semibold text-amber-300">
                                {isAudioPlaying ? '正在播放' : '已暂停'}
                              </span>
                            )}
                          </div>
                        </td>
                        <td className="hidden px-3 py-3 text-xs text-zinc-400 sm:table-cell">
                          {artistLabel}
                        </td>
                        <td className="px-4 py-3 text-right text-xs tabular-nums text-zinc-400">
                          {track.RunTimeTicks
                            ? formatAudioTime(track.RunTimeTicks / 10_000_000)
                            : '--:--'}
                        </td>
                        <td className="px-4 py-3 text-right">
                          <button
                            type="button"
                            aria-label={isActive && isAudioPlaying ? `暂停 ${track.Name}` : `播放 ${track.Name}`}
                            onClick={(e) => {
                              e.stopPropagation()
                              if (isActive) {
                                toggleAudioPlay()
                              } else {
                                playQueue(albumTracks, idx)
                              }
                            }}
                            className={`inline-flex h-8 w-8 items-center justify-center rounded-full transition-colors ${
                              isActive
                                ? 'bg-amber-500 text-zinc-950'
                                : 'bg-white/[0.06] text-zinc-200 hover:bg-amber-500 hover:text-zinc-950'
                            }`}
                          >
                            {isActive && isAudioPlaying ? (
                              <Pause className="h-3.5 w-3.5 fill-current" />
                            ) : (
                              <Play className="ml-0.5 h-3.5 w-3.5 fill-current" />
                            )}
                          </button>
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>
          )}
        </section>
      )}

      {/* 音乐人：专辑与热门曲目 */}
      {isMusicArtist && (
        <section className="mx-auto max-w-7xl space-y-8 px-4 pt-6 lg:px-8">
          <div className="space-y-4">
            <h2 className="text-lg font-semibold text-zinc-100">
              专辑
              <span className="ml-2 text-xs font-normal text-zinc-500">
                ({artistAlbums.length})
              </span>
            </h2>
            {artistAlbumsLoading ? (
              <div className="flex h-32 items-center justify-center">
                <Loader2 className="h-6 w-6 animate-spin text-amber-400" />
              </div>
            ) : artistAlbums.length === 0 ? (
              <p className="text-sm text-zinc-500">暂无收录专辑</p>
            ) : (
              <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5">
                {artistAlbums.map((album) => (
                  <MediaCard key={album.Id} item={album} />
                ))}
              </div>
            )}
          </div>

          <div className="space-y-4">
            <h2 className="text-lg font-semibold text-zinc-100">
              热门曲目
              <span className="ml-2 text-xs font-normal text-zinc-500">
                ({artistTracks.length})
              </span>
            </h2>
            {artistTracksLoading ? (
              <div className="flex h-32 items-center justify-center">
                <Loader2 className="h-6 w-6 animate-spin text-amber-400" />
              </div>
            ) : artistTracks.length === 0 ? (
              <p className="text-sm text-zinc-500">暂无收录曲目</p>
            ) : (
              <div className="divide-y divide-white/5 overflow-hidden rounded-2xl border border-white/8 bg-white/[0.02]">
                {artistTracks.map((track, idx) => {
                  const isActive = currentTrack?.Id === track.Id
                  return (
                    <div
                      key={track.Id}
                      onClick={() =>
                        isActive ? toggleAudioPlay() : playQueue(artistTracks, idx)
                      }
                      className={`flex cursor-pointer items-center justify-between gap-4 px-4 py-3 transition-colors ${
                        isActive
                          ? 'bg-amber-500/12 text-amber-300'
                          : 'text-zinc-200 hover:bg-white/[0.04]'
                      }`}
                    >
                      <div className="flex min-w-0 items-center gap-3">
                        <span className="w-6 text-center text-xs text-zinc-500">{idx + 1}</span>
                        <div className="min-w-0">
                          <p className="truncate text-sm font-medium">{track.Name}</p>
                          {track.Album && (
                            <p className="truncate text-xs text-zinc-500">{track.Album}</p>
                          )}
                        </div>
                      </div>
                      <div className="flex shrink-0 items-center gap-3">
                        <span className="text-xs tabular-nums text-zinc-400">
                          {track.RunTimeTicks
                            ? formatAudioTime(track.RunTimeTicks / 10_000_000)
                            : '--:--'}
                        </span>
                        <button
                          type="button"
                          aria-label={isActive && isAudioPlaying ? `暂停 ${track.Name}` : `播放 ${track.Name}`}
                          onClick={(e) => {
                            e.stopPropagation()
                            if (isActive) toggleAudioPlay()
                            else playQueue(artistTracks, idx)
                          }}
                          className="inline-flex h-8 w-8 items-center justify-center rounded-full bg-white/[0.06] text-zinc-200 hover:bg-amber-500 hover:text-zinc-950"
                        >
                          {isActive && isAudioPlaying ? (
                            <Pause className="h-3.5 w-3.5 fill-current" />
                          ) : (
                            <Play className="ml-0.5 h-3.5 w-3.5 fill-current" />
                          )}
                        </button>
                      </div>
                    </div>
                  )
                })}
              </div>
            )}
          </div>
        </section>
      )}

      {/* 剧集：季 Tab、当前季信息与分集列表 */}
      {isSeries && seasons.length > 0 && (
        <section className="mx-auto max-w-7xl space-y-5 px-4 pt-6 lg:px-8">
          <div className="no-scrollbar -mx-1 flex items-center gap-2 overflow-x-auto border-b border-white/10 px-1 pb-4 pt-2">
            {seasons.map((season) => {
              const active = season.Id === selectedSeasonId
              return (
                <button
                  key={season.Id}
                  type="button"
                  onClick={() => setSelectedSeasonId(season.Id)}
                  className={`shrink-0 rounded-xl px-4 py-2 text-sm font-medium transition-all duration-200 active:scale-[0.97] ${
                    active
                      ? 'bg-amber-500 text-zinc-950 shadow-md shadow-amber-500/25'
                      : 'bg-white/[0.04] text-zinc-300 hover:bg-white/10 hover:text-zinc-100'
                  }`}
                >
                  {season.Name}
                </button>
              )
            })}
          </div>

          {/* 当前选中季的海报、简介与整季已看操作 */}
          {selectedSeason && (
            <div className="flex flex-col gap-4 rounded-2xl border border-white/8 bg-white/[0.025] p-4 sm:flex-row sm:items-center sm:justify-between">
              <div className="flex items-start gap-4">
                {(selectedSeason.ImageTags?.Primary || poster) && (
                  <img
                    src={
                      selectedSeason.ImageTags?.Primary
                        ? getImageUrl(selectedSeason.Id, 'Primary', {
                            maxWidth: 200,
                            tag: selectedSeason.ImageTags.Primary,
                          })
                        : poster!
                    }
                    alt={selectedSeason.Name}
                    onError={(e) =>
                      applyFallbackOrHide(
                        e,
                        selectedSeason.ImageTags?.Primary
                          ? getFallbackImageUrl(selectedSeason.Id, 'Primary')
                          : posterFallback,
                      )
                    }
                    className="h-24 w-16 shrink-0 rounded-xl object-cover ring-1 ring-white/10"
                  />
                )}
                <div className="min-w-0 space-y-1">
                  <div className="flex flex-wrap items-center gap-2">
                    <h2 className="text-base font-semibold text-white">{selectedSeason.Name}</h2>
                    <span className="rounded-full bg-white/10 px-2 py-0.5 text-[11px] text-zinc-300">
                      共 {episodes.length} 集 · 已看 {seasonPlayedCount} 集
                    </span>
                  </div>
                  {selectedSeason.Overview && (
                    <p className="line-clamp-2 max-w-2xl text-xs leading-relaxed text-zinc-400">
                      {selectedSeason.Overview}
                    </p>
                  )}
                </div>
              </div>

              {episodes.length > 0 && (
                <button
                  type="button"
                  disabled={seasonPlayedMutation.isPending}
                  onClick={() => seasonPlayedMutation.mutate()}
                  className={`inline-flex shrink-0 items-center gap-1.5 self-start rounded-xl border px-3.5 py-2 text-xs font-medium transition-colors sm:self-center disabled:opacity-50 ${
                    seasonAllPlayed
                      ? 'border-emerald-500/45 bg-emerald-500/15 text-emerald-300 hover:bg-emerald-500/25'
                      : 'border-white/12 bg-white/[0.04] text-zinc-200 hover:bg-white/10'
                  }`}
                >
                  {seasonPlayedMutation.isPending ? (
                    <Loader2 className="h-3.5 w-3.5 animate-spin" />
                  ) : (
                    <CheckCircle2 className="h-3.5 w-3.5" />
                  )}
                  <span>{seasonAllPlayed ? '标记本季未看' : '标记本季已看'}</span>
                </button>
              )}
            </div>
          )}

          {episodesLoading ? (
            <div className="flex h-36 items-center justify-center">
              <Loader2 className="h-6 w-6 animate-spin text-amber-400" />
            </div>
          ) : (
            <div key={selectedSeasonId} className="animate-page-enter grid gap-4 md:grid-cols-2">
              {episodes.map((ep) => {
                const epThumb = ep.ImageTags?.Primary
                  ? getImageUrl(ep.Id, 'Primary', { maxWidth: 480, tag: ep.ImageTags.Primary })
                  : backdrop
                const epFallback = ep.ImageTags?.Primary
                  ? getFallbackImageUrl(ep.Id, 'Primary')
                  : backdropFallback
                const epProgress =
                  ep.UserData?.PlayedPercentage ??
                  (ep.UserData?.PlaybackPositionTicks && ep.RunTimeTicks
                    ? Math.min(100, (ep.UserData.PlaybackPositionTicks / ep.RunTimeTicks) * 100)
                    : 0)
                const epPlayed = Boolean(ep.UserData?.Played)
                const epFav = Boolean(ep.UserData?.IsFavorite)

                return (
                  <div
                    key={ep.Id}
                    onClick={() => handlePlay(ep.Id, ep.UserData?.PlaybackPositionTicks || 0)}
                    className="group flex cursor-pointer gap-4 overflow-hidden rounded-2xl border border-white/8 bg-white/[0.03] p-3 transition-all duration-200 ease-out hover:translate-x-1 hover:border-amber-400/50 hover:bg-white/[0.06] hover:shadow-[0_8px_24px_rgba(0,0,0,0.4)]"
                  >
                    <div className="relative aspect-video w-40 shrink-0 overflow-hidden rounded-xl bg-zinc-900">
                      {epThumb ? (
                        <img
                          src={epThumb}
                          alt={ep.Name}
                          onError={(e) => applyFallbackOrHide(e, epFallback)}
                          className="h-full w-full object-cover transition-transform duration-300 group-hover:scale-105"
                        />
                      ) : (
                        <div className="flex h-full w-full items-center justify-center text-zinc-600">
                          <Film className="h-6 w-6" />
                        </div>
                      )}
                      {epPlayed && (
                        <div
                          title="已观看"
                          className="absolute right-1.5 top-1.5 flex h-5 w-5 items-center justify-center rounded-full bg-emerald-500 text-zinc-950 shadow-md"
                        >
                          <Check className="h-3 w-3 stroke-[3]" />
                        </div>
                      )}
                      <div className="absolute inset-0 flex items-center justify-center bg-black/40 opacity-0 transition-opacity group-hover:opacity-100">
                        <div className="flex h-10 w-10 items-center justify-center rounded-full bg-amber-500 text-zinc-950">
                          <Play className="ml-0.5 h-4 w-4 fill-current" />
                        </div>
                      </div>
                      {epProgress > 0 && !epPlayed && (
                        <div className="absolute inset-x-0 bottom-0 h-1 bg-black/60">
                          <div
                            className="h-full bg-amber-400"
                            style={{ width: `${Math.max(4, epProgress)}%` }}
                          />
                        </div>
                      )}
                    </div>

                    <div className="flex min-w-0 flex-1 flex-col justify-between py-0.5">
                      <div>
                        <div className="flex items-center justify-between gap-2">
                          <p className="truncate text-sm font-semibold text-zinc-100 group-hover:text-amber-300">
                            {ep.IndexNumber != null ? `${ep.IndexNumber}. ` : ''}
                            {ep.Name}
                          </p>
                          {ep.RunTimeTicks && (
                            <span className="shrink-0 text-xs text-zinc-500">
                              {formatTicks(ep.RunTimeTicks)}
                            </span>
                          )}
                        </div>
                        {ep.Overview && (
                          <p className="mt-1.5 line-clamp-2 text-xs leading-relaxed text-zinc-400">
                            {ep.Overview}
                          </p>
                        )}
                      </div>

                      <div className="mt-2 flex items-center justify-end gap-1.5">
                        <button
                          type="button"
                          onClick={(e) => {
                            e.stopPropagation()
                            episodePlayedMutation.mutate(ep)
                          }}
                          title={epPlayed ? '标记为未看' : '标记为已看'}
                          aria-label={epPlayed ? `取消 ${ep.Name} 已看` : `标记 ${ep.Name} 已看`}
                          className={`inline-flex items-center gap-1 rounded-lg px-2 py-1 text-[11px] transition-colors ${
                            epPlayed
                              ? 'bg-emerald-500/15 text-emerald-300 hover:bg-emerald-500/25'
                              : 'bg-white/[0.04] text-zinc-400 hover:bg-white/10 hover:text-zinc-200'
                          }`}
                        >
                          <Check className="h-3 w-3" />
                          <span>{epPlayed ? '已看' : '标已看'}</span>
                        </button>
                        <button
                          type="button"
                          onClick={(e) => {
                            e.stopPropagation()
                            episodeFavMutation.mutate(ep)
                          }}
                          title={epFav ? '取消收藏' : '收藏本集'}
                          aria-label={epFav ? `取消收藏 ${ep.Name}` : `收藏 ${ep.Name}`}
                          className={`rounded-lg p-1.5 transition-colors ${
                            epFav
                              ? 'bg-rose-500/15 text-rose-400 hover:bg-rose-500/25'
                              : 'bg-white/[0.04] text-zinc-400 hover:bg-white/10 hover:text-rose-300'
                          }`}
                        >
                          <Heart className={`h-3 w-3 ${epFav ? 'fill-current' : ''}`} />
                        </button>
                      </div>
                    </div>
                  </div>
                )
              })}
            </div>
          )}
        </section>
      )}

      {/* 演职员列表 */}
      {item.People && item.People.length > 0 && (
        <section className="mx-auto max-w-7xl space-y-4 px-4 pt-10 lg:px-8">
          <div className="flex items-center justify-between gap-3">
            <h2 className="text-lg font-semibold text-zinc-100">
              演职员
              <span className="ml-2 text-xs font-normal text-zinc-500">
                ({item.People.length})
              </span>
            </h2>
            {item.People.length > 16 && (
              <button
                type="button"
                onClick={() => setShowAllCast((prev) => !prev)}
                className="inline-flex items-center gap-1 rounded-xl border border-white/10 bg-white/[0.04] px-3 py-1.5 text-xs font-medium text-zinc-300 transition-colors hover:border-amber-400/40 hover:bg-white/10 hover:text-amber-200"
              >
                {showAllCast ? (
                  <>
                    <span>收起演职员</span>
                    <ChevronUp className="h-3.5 w-3.5" />
                  </>
                ) : (
                  <>
                    <span>展开全部演职员 ({item.People.length})</span>
                    <ChevronDown className="h-3.5 w-3.5" />
                  </>
                )}
              </button>
            )}
          </div>
          <div
            className={
              showAllCast
                ? 'grid grid-cols-3 gap-4 sm:grid-cols-5 md:grid-cols-6 lg:grid-cols-8'
                : 'no-scrollbar -mx-1 flex gap-4 overflow-x-auto px-1 pb-4 pt-2'
            }
          >
            {(showAllCast ? item.People : item.People.slice(0, 16)).map((person) => {
              const avatar = person.PrimaryImageTag
                ? getImageUrl(person.Id, 'Primary', {
                    maxWidth: 200,
                    tag: person.PrimaryImageTag,
                  })
                : null
              const avatarFallback = person.PrimaryImageTag
                ? getFallbackImageUrl(person.Id, 'Primary')
                : null
              return (
                <Link
                  key={`${person.Id}-${person.Role}`}
                  to={`/person/${encodeURIComponent(person.Id || person.Name)}?name=${encodeURIComponent(person.Name)}`}
                  className="group w-28 shrink-0 text-center transition-transform duration-200 hover:-translate-y-0.5"
                >
                  <div className="mx-auto flex h-24 w-24 items-center justify-center overflow-hidden rounded-full border border-white/10 bg-zinc-900 transition-all duration-300 group-hover:border-amber-400/70 group-hover:shadow-[0_0_18px_rgba(245,158,11,0.22)]">
                    {avatar ? (
                      <img
                        src={avatar}
                        alt={person.Name}
                        loading="lazy"
                        onError={(e) => applyFallbackOrHide(e, avatarFallback)}
                        className="h-full w-full object-cover object-top transition-transform duration-300 ease-out group-hover:scale-110"
                      />
                    ) : (
                      <User className="h-8 w-8 text-zinc-600" />
                    )}
                  </div>
                  <p className="mt-2 truncate text-xs font-medium text-zinc-200 transition-colors group-hover:text-amber-300">
                    {person.Name}
                  </p>
                  <p className="truncate text-[11px] text-zinc-500">{person.Role || person.Type}</p>
                </Link>
              )
            })}
          </div>
        </section>
      )}

      {/* 相似推荐横向书架 */}
      {similarItems.length > 0 && (
        <section className="mx-auto max-w-7xl space-y-4 px-4 pt-10 lg:px-8">
          <h2 className="text-lg font-semibold text-zinc-100">相似推荐</h2>
          <div className="no-scrollbar -mx-1 flex gap-4 overflow-x-auto px-1 pb-4 pt-1">
            {similarItems.map((sim) => (
              <div key={sim.Id} className="w-36 shrink-0 sm:w-40 md:w-44">
                <MediaCard item={sim} />
              </div>
            ))}
          </div>
        </section>
      )}

      {/* 加入合集 / 播放列表弹窗 */}
      <AdminModal
        open={addToColOpen}
        title="加入合集 / 播放列表"
        subtitle={`将「${item.Name}」添加到已有合集或新建片单`}
        maxWidthClass="max-w-md"
        onClose={() => setAddToColOpen(false)}
      >
        <div className="space-y-4">
          <div className="flex gap-2">
            <button
              type="button"
              onClick={() => setAddToColTab('BoxSet')}
              className={`inline-flex flex-1 items-center justify-center gap-1.5 rounded-xl border px-3 py-2 text-xs font-medium transition-colors ${
                addToColTab === 'BoxSet'
                  ? 'border-amber-400/50 bg-amber-500/15 text-amber-300'
                  : 'border-white/10 bg-white/[0.03] text-zinc-400 hover:text-zinc-200'
              }`}
            >
              <Layers className="h-3.5 w-3.5" />
              <span>合集 (BoxSet)</span>
            </button>
            <button
              type="button"
              onClick={() => setAddToColTab('Playlist')}
              className={`inline-flex flex-1 items-center justify-center gap-1.5 rounded-xl border px-3 py-2 text-xs font-medium transition-colors ${
                addToColTab === 'Playlist'
                  ? 'border-amber-400/50 bg-amber-500/15 text-amber-300'
                  : 'border-white/10 bg-white/[0.03] text-zinc-400 hover:text-zinc-200'
              }`}
            >
              <ListVideo className="h-3.5 w-3.5" />
              <span>播放列表</span>
            </button>
          </div>

          <div className="max-h-56 space-y-1.5 overflow-y-auto">
            {(addToColTab === 'BoxSet'
              ? boxSetsForModal?.Items || []
              : playlistsForModal?.Items || []
            ).length === 0 ? (
              <p className="rounded-xl border border-dashed border-white/10 py-6 text-center text-xs text-zinc-500">
                暂无{addToColTab === 'BoxSet' ? '合集' : '播放列表'}，可在下方直接新建
              </p>
            ) : (
              (addToColTab === 'BoxSet'
                ? boxSetsForModal?.Items || []
                : playlistsForModal?.Items || []
              ).map((target) => (
                <button
                  key={target.Id}
                  type="button"
                  disabled={addToContainerMutation.isPending}
                  onClick={() =>
                    addToContainerMutation.mutate({
                      id: target.Id,
                      name: target.Name,
                      type: addToColTab,
                    })
                  }
                  className="flex w-full items-center justify-between rounded-xl border border-white/8 bg-white/[0.03] px-3.5 py-2.5 text-left text-xs text-zinc-200 transition-colors hover:border-amber-400/40 hover:bg-white/10"
                >
                  <span className="truncate font-medium">{target.Name}</span>
                  <span className="shrink-0 text-[11px] text-amber-400">加入</span>
                </button>
              ))
            )}
          </div>

          <form
            onSubmit={(e) => {
              e.preventDefault()
              createAndAddMutation.mutate()
            }}
            className="flex items-center gap-2 border-t border-white/10 pt-3"
          >
            <input
              type="text"
              value={newColName}
              onChange={(e) => setNewColName(e.target.value)}
              placeholder={
                addToColTab === 'BoxSet' ? '输入新合集名称并加入...' : '输入新播放列表名称并加入...'
              }
              className="flex-1 rounded-xl border border-white/10 bg-white/[0.04] px-3 py-2 text-xs text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/60"
            />
            <button
              type="submit"
              disabled={createAndAddMutation.isPending || !newColName.trim()}
              className="inline-flex items-center gap-1 rounded-xl bg-amber-500 px-3 py-2 text-xs font-semibold text-zinc-950 hover:bg-amber-400 disabled:opacity-50"
            >
              <Plus className="h-3.5 w-3.5" />
              <span>新建并加入</span>
            </button>
          </form>
        </div>
      </AdminModal>

      {/* 管理员弹窗组 */}
      {isAdmin && (
        <>
          <MetadataEditorModal
            open={editModalOpen}
            item={item}
            onClose={() => setEditModalOpen(false)}
            onSaved={(msg) => showToast('success', msg)}
          />
          <ImageManagerModal
            open={imageModalOpen}
            item={item}
            onClose={() => setImageModalOpen(false)}
            onUpdated={(msg) => showToast('success', msg)}
          />
          <IdentifyModal
            open={identifyModalOpen}
            item={item}
            onClose={() => setIdentifyModalOpen(false)}
            onApplied={(msg) => showToast('success', msg)}
          />
          <MergeVersionsModal
            open={mergeModalOpen}
            item={item}
            onClose={() => setMergeModalOpen(false)}
            onMerged={(msg) => showToast('success', msg)}
          />
          <AdminModal
            open={deleteConfirmOpen}
            title="确认删除媒体"
            subtitle="此操作不可撤销"
            maxWidthClass="max-w-md"
            onClose={() => setDeleteConfirmOpen(false)}
          >
            <div className="space-y-5">
              <div className="flex items-start gap-3 rounded-xl border border-red-500/25 bg-red-500/10 p-4 text-sm text-red-200">
                <AlertTriangle className="mt-0.5 h-5 w-5 shrink-0 text-red-400" />
                <div>
                  <p className="font-semibold">确定要删除「{item.Name}」吗？</p>
                  <p className="mt-1 text-xs leading-relaxed text-red-200/80">
                    将从媒体库索引中移除该条目的元数据、图片与关联记录，删除后将返回上一级媒体库。
                  </p>
                </div>
              </div>
              <div className="flex items-center justify-end gap-3">
                <button
                  type="button"
                  onClick={() => setDeleteConfirmOpen(false)}
                  className="rounded-xl border border-white/10 bg-white/[0.04] px-4 py-2 text-sm font-medium text-zinc-300 hover:bg-white/10"
                >
                  取消
                </button>
                <button
                  type="button"
                  disabled={deleteItemMutation.isPending}
                  onClick={() => deleteItemMutation.mutate()}
                  className="inline-flex items-center gap-2 rounded-xl bg-red-600 px-5 py-2 text-sm font-semibold text-white transition-colors hover:bg-red-500 disabled:opacity-60"
                >
                  {deleteItemMutation.isPending && <Loader2 className="h-4 w-4 animate-spin" />}
                  <span>确认删除</span>
                </button>
              </div>
            </div>
          </AdminModal>
        </>
      )}
    </div>
  )
}
