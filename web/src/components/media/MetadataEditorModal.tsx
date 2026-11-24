import { useEffect, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import {
  Award,
  ChevronDown,
  ChevronUp,
  FileCode2,
  Loader2,
  Lock,
  Plus,
  Save,
  Tag,
  Unlock,
  X,
} from 'lucide-react'
import { api } from '../../api/client'
import type { BaseItemDto, ItemMetadataUpdatePayload } from '../../api/types'
import { AdminModal, FeedbackBanner } from '../../pages/admin/adminUtils'

const LOCKABLE_FIELDS = [
  { key: 'Name', label: '片名 (Name)' },
  { key: 'Overview', label: '简介 (Overview)' },
  { key: 'Genres', label: '流派 (Genres)' },
  { key: 'Tags', label: '标签 (Tags)' },
  { key: 'Cast', label: '演职员 (Cast)' },
] as const

const RANKING_QUICK_TAGS = [
  {
    value: '豆瓣Top250',
    label: '豆瓣 Top250',
    activeClass: 'border-emerald-400/60 bg-emerald-500/20 text-emerald-200',
    idleClass: 'border-white/10 bg-white/[0.03] text-zinc-400 hover:border-emerald-400/40 hover:text-emerald-200',
  },
  {
    value: 'IMDb Top250',
    label: 'IMDb Top250',
    activeClass: 'border-amber-400/60 bg-amber-500/20 text-amber-200',
    idleClass: 'border-white/10 bg-white/[0.03] text-zinc-400 hover:border-amber-400/40 hover:text-amber-200',
  },
] as const

export interface MetadataEditorModalProps {
  open: boolean
  item: BaseItemDto | null
  onClose: () => void
  onSaved?: (message: string) => void
}

export function MetadataEditorModal({
  open,
  item,
  onClose,
  onSaved,
}: MetadataEditorModalProps) {
  const queryClient = useQueryClient()

  const [name, setName] = useState('')
  const [originalTitle, setOriginalTitle] = useState('')
  const [sortName, setSortName] = useState('')
  const [productionYear, setProductionYear] = useState('')
  const [communityRating, setCommunityRating] = useState('')
  const [officialRating, setOfficialRating] = useState('')
  const [overview, setOverview] = useState('')
  const [genres, setGenres] = useState<string[]>([])
  const [genreInput, setGenreInput] = useState('')
  const [tags, setTags] = useState<string[]>([])
  const [tagInput, setTagInput] = useState('')
  const [tmdbId, setTmdbId] = useState('')
  const [imdbId, setImdbId] = useState('')
  const [doubanId, setDoubanId] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [govFeedback, setGovFeedback] = useState<{
    type: 'success' | 'error'
    message: string
  } | null>(null)
  const [govExpanded, setGovExpanded] = useState(true)
  const [lockedFields, setLockedFields] = useState<string[]>([])
  const [lockData, setLockData] = useState(false)
  const [locking, setLocking] = useState(false)
  const [writingNfo, setWritingNfo] = useState(false)

  useEffect(() => {
    if (!open || !item) return
    setName(item.Name || '')
    setOriginalTitle(item.OriginalTitle || '')
    setSortName(item.SortName || '')
    setProductionYear(item.ProductionYear != null ? String(item.ProductionYear) : '')
    setCommunityRating(item.CommunityRating != null ? String(item.CommunityRating) : '')
    setOfficialRating(item.OfficialRating || '')
    setOverview(item.Overview || '')
    setGenres([...(item.Genres || [])])
    setGenreInput('')
    setTags([...(item.Tags || [])])
    setTagInput('')
    setTmdbId(item.ProviderIds?.Tmdb || '')
    setImdbId(item.ProviderIds?.Imdb || '')
    setDoubanId(item.ProviderIds?.Douban || '')
    setLockedFields([...(item.LockedFields || [])])
    setLockData(Boolean(item.LockData))
    setError(null)
    setGovFeedback(null)
  }, [open, item])

  const saveMutation = useMutation({
    mutationFn: async (payload: ItemMetadataUpdatePayload) => {
      if (!item) return
      await api.updateItemMetadata(item.Id, payload)
    },
    onSuccess: async () => {
      if (!item) return
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['item'] }),
        queryClient.invalidateQueries({ queryKey: ['item', item.Id] }),
        queryClient.invalidateQueries({ queryKey: ['items'] }),
        queryClient.invalidateQueries({ queryKey: ['tags'] }),
        queryClient.invalidateQueries({ queryKey: ['latestItems'] }),
        queryClient.invalidateQueries({ queryKey: ['admin-metadata-items'] }),
      ])
      onSaved?.(`已保存「${name.trim() || item.Name}」的元数据`)
      onClose()
    },
    onError: (err) => {
      setError(err instanceof Error ? err.message : '保存元数据失败')
    },
  })

  const addGenre = () => {
    const parts = genreInput
      .split(/[,，、]/)
      .map((s) => s.trim())
      .filter(Boolean)
    if (parts.length === 0) return
    setGenres((prev) => Array.from(new Set([...prev, ...parts])))
    setGenreInput('')
  }

  const removeGenre = (target: string) => {
    setGenres((prev) => prev.filter((g) => g !== target))
  }

  const addTag = () => {
    const parts = tagInput
      .split(/[,，、]/)
      .map((s) => s.trim())
      .filter(Boolean)
    if (parts.length === 0) return
    setTags((prev) => Array.from(new Set([...prev, ...parts])))
    setTagInput('')
  }

  const removeTag = (target: string) => {
    setTags((prev) => prev.filter((t) => t !== target))
  }

  const toggleQuickTag = (tagVal: string) => {
    setTags((prev) =>
      prev.includes(tagVal) ? prev.filter((t) => t !== tagVal) : [...prev, tagVal],
    )
  }

  const toggleLockedField = (fieldKey: string) => {
    setLockedFields((prev) =>
      prev.includes(fieldKey) ? prev.filter((f) => f !== fieldKey) : [...prev, fieldKey],
    )
  }

  const handleSaveLocks = async () => {
    if (!item) return
    setLocking(true)
    setGovFeedback(null)
    try {
      const fieldsToSave = lockData
        ? Array.from(new Set([...lockedFields, ...LOCKABLE_FIELDS.map((f) => f.key)]))
        : lockedFields
      await api.lockItemFields(item.Id, fieldsToSave, lockData)
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['item'] }),
        queryClient.invalidateQueries({ queryKey: ['item', item.Id] }),
        queryClient.invalidateQueries({ queryKey: ['admin-metadata-items'] }),
      ])
      setGovFeedback({
        type: 'success',
        message: lockData
          ? '已开启全局元数据锁定（刮削将跳过所有字段）'
          : `已保存字段锁定（${fieldsToSave.length} 项）`,
      })
    } catch (err) {
      setGovFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '保存字段锁定失败',
      })
    } finally {
      setLocking(false)
    }
  }

  const handleWriteNfo = async () => {
    if (!item) return
    setWritingNfo(true)
    setGovFeedback(null)
    try {
      const res = await api.writeItemNfo(item.Id)
      const nfoPath = res?.NfoPath || res?.path
      setGovFeedback({
        type: 'success',
        message: nfoPath ? `已原子回写 NFO：${nfoPath}` : `已成功回写「${item.Name}」的 .nfo 文件`,
      })
    } catch (err) {
      setGovFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '回写 .nfo 失败',
      })
    } finally {
      setWritingNfo(false)
    }
  }

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    if (!item) return
    const trimmedName = name.trim()
    if (!trimmedName) {
      setError('中文片名不能为空')
      return
    }
    setError(null)

    const parsedYear = productionYear.trim() ? Number(productionYear.trim()) : undefined
    const parsedRating = communityRating.trim() ? Number(communityRating.trim()) : undefined

    const providerIds: Record<string, string> = { ...(item.ProviderIds || {}) }
    if (tmdbId.trim()) providerIds.Tmdb = tmdbId.trim()
    else delete providerIds.Tmdb
    if (imdbId.trim()) providerIds.Imdb = imdbId.trim()
    else delete providerIds.Imdb
    if (doubanId.trim()) providerIds.Douban = doubanId.trim()
    else delete providerIds.Douban

    const payload: ItemMetadataUpdatePayload = {
      Name: trimmedName,
      OriginalTitle: originalTitle.trim(),
      SortName: sortName.trim() || trimmedName.toLowerCase(),
      ProductionYear: Number.isFinite(parsedYear) ? parsedYear : undefined,
      CommunityRating: Number.isFinite(parsedRating) ? parsedRating : undefined,
      OfficialRating: officialRating.trim(),
      Overview: overview.trim(),
      Genres: genres,
      Tags: tags,
      ProviderIds: providerIds,
    }

    saveMutation.mutate(payload)
  }

  return (
    <AdminModal
      open={open}
      title="编辑元数据"
      subtitle={item ? `${item.Name} · ${item.Type}` : undefined}
      maxWidthClass="max-w-2xl"
      onClose={onClose}
    >
      <form onSubmit={handleSubmit} className="space-y-5">
        <FeedbackBanner message={error ?? undefined} type="error" onClose={() => setError(null)} />

        {/* 基础标题 */}
        <div className="grid gap-4 sm:grid-cols-2">
          <label className="block space-y-1.5">
            <span className="text-xs font-medium text-zinc-300">中文片名 *</span>
            <input
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="例如：肖申克的救赎"
              className="w-full rounded-xl border border-white/10 bg-zinc-900/90 px-3.5 py-2 text-sm text-zinc-100 outline-none transition-colors focus:border-amber-400/60"
            />
          </label>
          <label className="block space-y-1.5">
            <span className="text-xs font-medium text-zinc-300">原名（OriginalTitle）</span>
            <input
              type="text"
              value={originalTitle}
              onChange={(e) => setOriginalTitle(e.target.value)}
              placeholder="例如：The Shawshank Redemption"
              className="w-full rounded-xl border border-white/10 bg-zinc-900/90 px-3.5 py-2 text-sm text-zinc-100 outline-none transition-colors focus:border-amber-400/60"
            />
          </label>
        </div>

        <div className="grid gap-4 sm:grid-cols-4">
          <label className="block space-y-1.5 sm:col-span-2">
            <span className="text-xs font-medium text-zinc-300">排序名（SortName）</span>
            <input
              type="text"
              value={sortName}
              onChange={(e) => setSortName(e.target.value)}
              placeholder="用于拼音/字母排序"
              className="w-full rounded-xl border border-white/10 bg-zinc-900/90 px-3.5 py-2 text-sm text-zinc-100 outline-none transition-colors focus:border-amber-400/60"
            />
          </label>
          <label className="block space-y-1.5">
            <span className="text-xs font-medium text-zinc-300">上映年份</span>
            <input
              type="number"
              min={1880}
              max={2100}
              value={productionYear}
              onChange={(e) => setProductionYear(e.target.value)}
              placeholder="1994"
              className="w-full rounded-xl border border-white/10 bg-zinc-900/90 px-3.5 py-2 text-sm text-zinc-100 outline-none transition-colors focus:border-amber-400/60"
            />
          </label>
          <label className="block space-y-1.5">
            <span className="text-xs font-medium text-zinc-300">社区评分</span>
            <input
              type="number"
              step="0.1"
              min={0}
              max={10}
              value={communityRating}
              onChange={(e) => setCommunityRating(e.target.value)}
              placeholder="9.7"
              className="w-full rounded-xl border border-white/10 bg-zinc-900/90 px-3.5 py-2 text-sm text-zinc-100 outline-none transition-colors focus:border-amber-400/60"
            />
          </label>
        </div>

        <div className="grid gap-4 sm:grid-cols-4">
          <label className="block space-y-1.5">
            <span className="text-xs font-medium text-zinc-300">内容分级</span>
            <input
              type="text"
              value={officialRating}
              onChange={(e) => setOfficialRating(e.target.value)}
              placeholder="PG-13 / R"
              className="w-full rounded-xl border border-white/10 bg-zinc-900/90 px-3.5 py-2 text-sm text-zinc-100 outline-none transition-colors focus:border-amber-400/60"
            />
          </label>
          <label className="block space-y-1.5">
            <span className="text-xs font-medium text-zinc-300">TMDB ID</span>
            <input
              type="text"
              value={tmdbId}
              onChange={(e) => setTmdbId(e.target.value)}
              placeholder="278"
              className="w-full rounded-xl border border-white/10 bg-zinc-900/90 px-3.5 py-2 text-sm text-zinc-100 outline-none transition-colors focus:border-amber-400/60"
            />
          </label>
          <label className="block space-y-1.5">
            <span className="text-xs font-medium text-zinc-300">IMDb ID</span>
            <input
              type="text"
              value={imdbId}
              onChange={(e) => setImdbId(e.target.value)}
              placeholder="tt0111161"
              className="w-full rounded-xl border border-white/10 bg-zinc-900/90 px-3.5 py-2 text-sm text-zinc-100 outline-none transition-colors focus:border-amber-400/60"
            />
          </label>
          <label className="block space-y-1.5">
            <span className="text-xs font-medium text-zinc-300">豆瓣 ID</span>
            <input
              type="text"
              value={doubanId}
              onChange={(e) => setDoubanId(e.target.value)}
              placeholder="1292052"
              className="w-full rounded-xl border border-white/10 bg-zinc-900/90 px-3.5 py-2 text-sm text-zinc-100 outline-none transition-colors focus:border-amber-400/60"
            />
          </label>
        </div>

        {/* 剧情简介 */}
        <label className="block space-y-1.5">
          <span className="text-xs font-medium text-zinc-300">剧情简介</span>
          <textarea
            rows={4}
            value={overview}
            onChange={(e) => setOverview(e.target.value)}
            placeholder="输入影片或剧集简介..."
            className="w-full rounded-xl border border-white/10 bg-zinc-900/90 px-3.5 py-2.5 text-sm leading-relaxed text-zinc-100 outline-none transition-colors focus:border-amber-400/60"
          />
        </label>

        {/* 流派编辑 */}
        <div className="space-y-2 rounded-xl border border-white/8 bg-white/[0.02] p-3.5">
          <span className="text-xs font-medium text-zinc-300">流派（Genres）</span>
          <div className="flex flex-wrap gap-1.5">
            {genres.map((g) => (
              <span
                key={g}
                className="inline-flex items-center gap-1 rounded-full bg-white/10 px-2.5 py-1 text-xs text-zinc-200"
              >
                {g}
                <button
                  type="button"
                  onClick={() => removeGenre(g)}
                  aria-label={`移除流派 ${g}`}
                  className="rounded-full p-0.5 text-zinc-400 hover:bg-white/10 hover:text-white"
                >
                  <X className="h-3 w-3" />
                </button>
              </span>
            ))}
          </div>
          <div className="flex gap-2">
            <input
              type="text"
              value={genreInput}
              onChange={(e) => setGenreInput(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') {
                  e.preventDefault()
                  addGenre()
                }
              }}
              placeholder="输入流派（逗号分隔可批量添加），如：科幻, 悬疑"
              className="flex-1 rounded-lg border border-white/10 bg-zinc-900 px-3 py-1.5 text-xs text-zinc-100 outline-none focus:border-amber-400/60"
            />
            <button
              type="button"
              onClick={addGenre}
              className="inline-flex items-center gap-1 rounded-lg border border-white/12 bg-white/6 px-3 py-1.5 text-xs font-medium text-zinc-200 hover:bg-white/12"
            >
              <Plus className="h-3.5 w-3.5" />
              添加
            </button>
          </div>
        </div>

        {/* 标签编辑 + 榜单快捷打标 */}
        <div className="space-y-2.5 rounded-xl border border-white/8 bg-white/[0.02] p-3.5">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <span className="flex items-center gap-1.5 text-xs font-medium text-zinc-300">
              <Tag className="h-3.5 w-3.5 text-amber-400" />
              标签（Tags）
            </span>
            <div className="flex flex-wrap items-center gap-1.5">
              {RANKING_QUICK_TAGS.map((qt) => {
                const active = tags.includes(qt.value)
                return (
                  <button
                    key={qt.value}
                    type="button"
                    onClick={() => toggleQuickTag(qt.value)}
                    className={`inline-flex items-center gap-1 rounded-lg border px-2.5 py-1 text-[11px] font-semibold transition-all ${
                      active ? qt.activeClass : qt.idleClass
                    }`}
                  >
                    <Award className="h-3 w-3" />
                    <span>{active ? `已标记 ${qt.label}` : `+ ${qt.label}`}</span>
                  </button>
                )
              })}
            </div>
          </div>

          <div className="flex flex-wrap gap-1.5">
            {tags.length === 0 && <span className="text-xs text-zinc-500">暂无标签</span>}
            {tags.map((t) => (
              <span
                key={t}
                className="inline-flex items-center gap-1 rounded-full border border-amber-500/25 bg-amber-500/10 px-2.5 py-1 text-xs text-amber-200"
              >
                {t}
                <button
                  type="button"
                  onClick={() => removeTag(t)}
                  aria-label={`移除标签 ${t}`}
                  className="rounded-full p-0.5 text-amber-300/70 hover:bg-amber-500/20 hover:text-amber-100"
                >
                  <X className="h-3 w-3" />
                </button>
              </span>
            ))}
          </div>

          <div className="flex gap-2">
            <input
              type="text"
              value={tagInput}
              onChange={(e) => setTagInput(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') {
                  e.preventDefault()
                  addTag()
                }
              }}
              placeholder="输入自定义标签后回车添加"
              className="flex-1 rounded-lg border border-white/10 bg-zinc-900 px-3 py-1.5 text-xs text-zinc-100 outline-none focus:border-amber-400/60"
            />
            <button
              type="button"
              onClick={addTag}
              className="inline-flex items-center gap-1 rounded-lg border border-white/12 bg-white/6 px-3 py-1.5 text-xs font-medium text-zinc-200 hover:bg-white/12"
            >
              <Plus className="h-3.5 w-3.5" />
              添加
            </button>
          </div>
        </div>

        {/* 元数据锁定与 NFO 回写 */}
        <div className="rounded-xl border border-amber-500/20 bg-amber-500/[0.04] p-3.5">
          <button
            type="button"
            onClick={() => setGovExpanded((v) => !v)}
            className="flex w-full items-center justify-between text-left"
          >
            <span className="flex items-center gap-1.5 text-xs font-semibold text-amber-200">
              <Lock className="h-3.5 w-3.5 text-amber-400" />
              <span>元数据锁定与 NFO 回写</span>
            </span>
            {govExpanded ? (
              <ChevronUp className="h-4 w-4 text-zinc-400" />
            ) : (
              <ChevronDown className="h-4 w-4 text-zinc-400" />
            )}
          </button>

          {govExpanded && (
            <div className="mt-3 space-y-3 border-t border-white/8 pt-3">
              <FeedbackBanner
                message={govFeedback?.message}
                type={govFeedback?.type}
                onClose={() => setGovFeedback(null)}
              />

              <div className="flex flex-wrap items-center gap-1.5">
                {LOCKABLE_FIELDS.map((field) => {
                  const isLocked = lockData || lockedFields.includes(field.key)
                  return (
                    <button
                      key={field.key}
                      type="button"
                      disabled={lockData}
                      onClick={() => toggleLockedField(field.key)}
                      className={`inline-flex items-center gap-1 rounded-lg border px-2.5 py-1 text-[11px] font-medium transition-colors disabled:opacity-70 ${
                        isLocked
                          ? 'border-amber-400/50 bg-amber-500/20 text-amber-200'
                          : 'border-white/10 bg-white/[0.03] text-zinc-400 hover:border-white/20 hover:text-zinc-200'
                      }`}
                    >
                      {isLocked ? <Lock className="h-3 w-3" /> : <Unlock className="h-3 w-3" />}
                      <span>{field.label}</span>
                    </button>
                  )
                })}

                <button
                  type="button"
                  onClick={() => setLockData((v) => !v)}
                  className={`inline-flex items-center gap-1 rounded-lg border px-2.5 py-1 text-[11px] font-semibold transition-colors ${
                    lockData
                      ? 'border-red-400/50 bg-red-500/20 text-red-200'
                      : 'border-white/10 bg-white/[0.03] text-zinc-300 hover:border-red-400/40 hover:text-red-200'
                  }`}
                >
                  <Lock className="h-3 w-3" />
                  <span>{lockData ? '已锁定全部 (LockData)' : '锁定全部字段'}</span>
                </button>
              </div>

              <div className="flex flex-wrap items-center justify-between gap-2 pt-1">
                <button
                  type="button"
                  disabled={locking}
                  onClick={() => void handleSaveLocks()}
                  className="inline-flex items-center gap-1.5 rounded-lg border border-amber-500/35 bg-amber-500/15 px-3 py-1.5 text-xs font-medium text-amber-200 transition-colors hover:bg-amber-500/25 disabled:opacity-50"
                >
                  {locking ? (
                    <Loader2 className="h-3.5 w-3.5 animate-spin" />
                  ) : (
                    <Lock className="h-3.5 w-3.5" />
                  )}
                  <span>保存锁定配置</span>
                </button>

                <button
                  type="button"
                  disabled={writingNfo}
                  onClick={() => void handleWriteNfo()}
                  className="inline-flex items-center gap-1.5 rounded-lg border border-emerald-500/35 bg-emerald-500/15 px-3 py-1.5 text-xs font-medium text-emerald-200 transition-colors hover:bg-emerald-500/25 disabled:opacity-50"
                >
                  {writingNfo ? (
                    <Loader2 className="h-3.5 w-3.5 animate-spin" />
                  ) : (
                    <FileCode2 className="h-3.5 w-3.5" />
                  )}
                  <span>原子回写 .nfo</span>
                </button>
              </div>
            </div>
          )}
        </div>

        {/* 底部操作按钮 */}
        <div className="flex items-center justify-end gap-3 border-t border-white/8 pt-4">
          <button
            type="button"
            onClick={onClose}
            className="rounded-xl border border-white/10 bg-white/[0.04] px-4 py-2 text-sm font-medium text-zinc-300 transition-colors hover:bg-white/10"
          >
            取消
          </button>
          <button
            type="submit"
            disabled={saveMutation.isPending}
            className="inline-flex items-center gap-2 rounded-xl bg-amber-500 px-5 py-2 text-sm font-semibold text-zinc-950 shadow-lg shadow-amber-500/20 transition-all hover:bg-amber-400 disabled:opacity-60"
          >
            {saveMutation.isPending ? (
              <Loader2 className="h-4 w-4 animate-spin" />
            ) : (
              <Save className="h-4 w-4" />
            )}
            <span>{saveMutation.isPending ? '保存中...' : '保存元数据'}</span>
          </button>
        </div>
      </form>
    </AdminModal>
  )
}
