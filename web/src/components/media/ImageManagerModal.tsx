import { useEffect, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Check,
  CloudDownload,
  Image as ImageIcon,
  Link2,
  Loader2,
  Trash2,
  Upload,
} from 'lucide-react'
import { api, getFallbackImageUrl, getImageUrl } from '../../api/client'
import type { BaseItemDto } from '../../api/types'
import { AdminModal, FeedbackBanner } from '../../pages/admin/adminUtils'

type ModalTab = 'current' | 'remote' | 'upload'
type ImageKind = 'Primary' | 'Backdrop'

const ALLOWED_IMAGE_MIME_TYPES = new Set(['image/jpeg', 'image/png', 'image/webp', 'image/gif'])
const MAX_POSTER_UPLOAD_BYTES = 15 * 1024 * 1024 // 15 MB

export interface ImageManagerModalProps {
  open: boolean
  item: BaseItemDto | null
  onClose: () => void
  onUpdated?: (message: string) => void
}

export function ImageManagerModal({
  open,
  item,
  onClose,
  onUpdated,
}: ImageManagerModalProps) {
  const queryClient = useQueryClient()
  const fileInputRef = useRef<HTMLInputElement | null>(null)

  const [activeTab, setActiveTab] = useState<ModalTab>('current')
  const [remoteType, setRemoteType] = useState<ImageKind>('Primary')
  const [uploadType, setUploadType] = useState<ImageKind>('Primary')
  const [customUrl, setCustomUrl] = useState('')
  const [dragging, setDragging] = useState(false)
  const [feedback, setFeedback] = useState<{ type: 'success' | 'error'; message: string } | null>(
    null,
  )

  const itemId = item?.Id || ''

  useEffect(() => {
    setActiveTab('current')
    setRemoteType('Primary')
    setUploadType('Primary')
    setCustomUrl('')
    setDragging(false)
    setFeedback(null)
  }, [open, itemId])

  const { data: currentImages = [], isLoading: imagesLoading } = useQuery({
    queryKey: ['item-images', itemId],
    queryFn: () => api.getItemImages(itemId),
    enabled: Boolean(open && itemId),
  })

  const { data: remoteResult, isLoading: remoteLoading } = useQuery({
    queryKey: ['item-remote-images', itemId, remoteType],
    queryFn: () => api.getRemoteImages(itemId, remoteType),
    enabled: Boolean(open && itemId && activeTab === 'remote'),
  })

  const invalidateAll = async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ['item'] }),
      queryClient.invalidateQueries({ queryKey: ['item', itemId] }),
      queryClient.invalidateQueries({ queryKey: ['item-images', itemId] }),
      queryClient.invalidateQueries({ queryKey: ['items'] }),
      queryClient.invalidateQueries({ queryKey: ['latestItems'] }),
      queryClient.invalidateQueries({ queryKey: ['admin-metadata-items'] }),
    ])
  }

  const deleteMutation = useMutation({
    mutationFn: ({ type, index }: { type: string; index?: number }) =>
      api.deleteItemImage(itemId, type, index ?? 0),
    onSuccess: async (_, vars) => {
      await invalidateAll()
      const lower = vars.type.toLowerCase()
      const label =
        lower === 'backdrop'
          ? '剧照 (Backdrop)'
          : lower === 'primary'
            ? '海报 (Primary)'
            : vars.type
      const msg = `已删除 ${label}`
      setFeedback({ type: 'success', message: msg })
      onUpdated?.(msg)
    },
    onError: (err) => {
      setFeedback({ type: 'error', message: err instanceof Error ? err.message : '删除图片失败' })
    },
  })

  const downloadMutation = useMutation({
    mutationFn: ({ type, url }: { type: ImageKind; url: string }) =>
      api.downloadRemoteImage(itemId, type, url),
    onSuccess: async (_, vars) => {
      await invalidateAll()
      const msg = `已将候选图片设为当前${vars.type === 'Backdrop' ? '剧照' : '海报'}`
      setFeedback({ type: 'success', message: msg })
      onUpdated?.(msg)
    },
    onError: (err) => {
      setFeedback({ type: 'error', message: err instanceof Error ? err.message : '下载替换图片失败' })
    },
  })

  const uploadMutation = useMutation({
    mutationFn: ({
      type,
      base64,
      contentType,
    }: {
      type: ImageKind
      base64: string
      contentType: string
    }) => api.uploadItemImage(itemId, type, base64, contentType),
    onSuccess: async (_, vars) => {
      await invalidateAll()
      const msg = `已上传并替换${vars.type === 'Backdrop' ? '剧照' : '海报'}`
      setFeedback({ type: 'success', message: msg })
      onUpdated?.(msg)
    },
    onError: (err) => {
      setFeedback({ type: 'error', message: err instanceof Error ? err.message : '上传图片失败' })
    },
  })

  const urlMutation = useMutation({
    mutationFn: ({ type, url }: { type: ImageKind; url: string }) =>
      api.setItemImageByUrl(itemId, type, url),
    onSuccess: async (_, vars) => {
      await invalidateAll()
      setCustomUrl('')
      const msg = `已通过 URL 替换${vars.type === 'Backdrop' ? '剧照' : '海报'}`
      setFeedback({ type: 'success', message: msg })
      onUpdated?.(msg)
    },
    onError: (err) => {
      setFeedback({ type: 'error', message: err instanceof Error ? err.message : 'URL 替换失败' })
    },
  })

  const handleFile = (file?: File | null) => {
    if (!file) return
    if (!ALLOWED_IMAGE_MIME_TYPES.has((file.type || '').toLowerCase())) {
      setFeedback({ type: 'error', message: '请选择有效的图片文件（JPG / PNG / WebP / GIF）' })
      return
    }
    if (file.size > MAX_POSTER_UPLOAD_BYTES) {
      setFeedback({ type: 'error', message: '图片文件过大，请选择不超过 15 MB 的图片' })
      return
    }
    const reader = new FileReader()
    reader.onload = () => {
      const result = typeof reader.result === 'string' ? reader.result : ''
      if (!result) {
        setFeedback({ type: 'error', message: '读取本地图片失败' })
        return
      }
      uploadMutation.mutate({
        type: uploadType,
        base64: result,
        contentType: file.type || 'image/jpeg',
      })
    }
    reader.onerror = () => {
      setFeedback({ type: 'error', message: '读取本地图片失败' })
    }
    reader.readAsDataURL(file)
  }

  const handleUrlSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    const trimmed = customUrl.trim()
    if (!trimmed) {
      setFeedback({ type: 'error', message: '请输入有效的图片 URL' })
      return
    }
    urlMutation.mutate({ type: uploadType, url: trimmed })
  }

  return (
    <AdminModal
      open={open}
      title="图片与海报管理"
      subtitle={item ? `${item.Name} · 管理海报（Primary）与剧照（Backdrop）` : undefined}
      maxWidthClass="max-w-4xl"
      onClose={onClose}
    >
      <div className="space-y-5">
        {/* Tab 切换栏 */}
        <div className="flex flex-wrap gap-2 border-b border-white/8 pb-3">
          {[
            { id: 'current' as const, label: '当前图片', icon: ImageIcon },
            { id: 'remote' as const, label: '远端选取（TMDB 候选）', icon: CloudDownload },
            { id: 'upload' as const, label: '上传 / URL 替换', icon: Upload },
          ].map((tab) => {
            const Icon = tab.icon
            const active = activeTab === tab.id
            return (
              <button
                key={tab.id}
                type="button"
                onClick={() => {
                  setActiveTab(tab.id)
                  setFeedback(null)
                }}
                className={`inline-flex items-center gap-2 rounded-xl px-4 py-2 text-xs font-semibold transition-all ${
                  active
                    ? 'bg-amber-500 text-zinc-950 shadow-md shadow-amber-500/20'
                    : 'border border-white/8 bg-white/[0.03] text-zinc-300 hover:bg-white/8 hover:text-white'
                }`}
              >
                <Icon className="h-3.5 w-3.5" />
                <span>{tab.label}</span>
              </button>
            )
          })}
        </div>

        <FeedbackBanner
          message={feedback?.message}
          type={feedback?.type}
          onClose={() => setFeedback(null)}
        />

        {/* Tab 1: 当前图片 */}
        {activeTab === 'current' && (
          <div>
            {imagesLoading ? (
              <div className="flex h-44 items-center justify-center">
                <Loader2 className="h-6 w-6 animate-spin text-amber-400" />
              </div>
            ) : currentImages.length === 0 ? (
              <div className="rounded-2xl border border-dashed border-white/12 bg-white/[0.02] py-12 text-center">
                <ImageIcon className="mx-auto h-8 w-8 text-zinc-600" />
                <p className="mt-2 text-sm text-zinc-400">当前条目暂无海报或剧照图片</p>
                <p className="mt-1 text-xs text-zinc-500">
                  可切换至「远端选取」或「上传 / URL 替换」快速补充图片
                </p>
              </div>
            ) : (
              <div className="grid gap-4 sm:grid-cols-2">
                {currentImages.map((img) => {
                  const rawType = img.ImageType || 'Primary'
                  const lowerType = rawType.toLowerCase()
                  const isBackdrop = lowerType === 'backdrop'
                  const isWide =
                    isBackdrop ||
                    lowerType === 'banner' ||
                    lowerType === 'thumb' ||
                    lowerType === 'logo' ||
                    lowerType === 'art'
                  const previewUrl = getImageUrl(itemId, rawType, {
                    maxWidth: isWide ? 800 : 400,
                    tag: img.ImageTag,
                    index: img.ImageIndex,
                  })
                  const fallback = getFallbackImageUrl(
                    itemId,
                    isBackdrop ? 'Backdrop' : 'Primary',
                  )
                  const typeLabel = isBackdrop
                    ? '剧照 · Backdrop'
                    : lowerType === 'primary'
                      ? '海报 · Primary'
                      : rawType
                  return (
                    <div
                      key={`${img.ImageType}-${img.ImageIndex ?? 0}`}
                      className="overflow-hidden rounded-2xl border border-white/10 bg-zinc-950/70"
                    >
                      <div
                        className={`relative w-full overflow-hidden bg-zinc-900 ${
                          isWide ? 'aspect-video' : 'aspect-[2/3] max-h-80'
                        }`}
                      >
                        <img
                          src={previewUrl}
                          alt={rawType}
                          onError={(e) => {
                            if (fallback && e.currentTarget.src !== fallback) {
                              e.currentTarget.src = fallback
                            }
                          }}
                          className="h-full w-full object-cover"
                        />
                        <span className="absolute left-3 top-3 rounded-lg bg-black/70 px-2.5 py-1 text-[11px] font-semibold text-amber-300 backdrop-blur-sm">
                          {typeLabel}
                        </span>
                      </div>
                      <div className="flex items-center justify-between gap-3 px-4 py-3">
                        <div className="min-w-0 text-xs text-zinc-400">
                          <p className="truncate font-medium text-zinc-200">
                            {img.Filename || `${img.ImageType}.jpg`}
                          </p>
                          <p className="mt-0.5 text-[11px] text-zinc-500">
                            {img.Width && img.Height ? `${img.Width} × ${img.Height}` : '已缓存'}
                          </p>
                        </div>
                        <button
                          type="button"
                          disabled={deleteMutation.isPending}
                          onClick={() =>
                            deleteMutation.mutate({
                              type: img.ImageType,
                              index: img.ImageIndex ?? 0,
                            })
                          }
                          className="inline-flex items-center gap-1.5 rounded-xl border border-red-500/30 bg-red-500/10 px-3 py-1.5 text-xs font-medium text-red-300 transition-colors hover:bg-red-500/20 disabled:opacity-50"
                        >
                          <Trash2 className="h-3.5 w-3.5" />
                          删除
                        </button>
                      </div>
                    </div>
                  )
                })}
              </div>
            )}
          </div>
        )}

        {/* Tab 2: 远端选取（TMDB 候选） */}
        {activeTab === 'remote' && (
          <div className="space-y-4">
            <div className="flex items-center justify-between gap-3">
              <div className="inline-flex rounded-xl border border-white/10 bg-zinc-950 p-1">
                {(['Primary', 'Backdrop'] as const).map((t) => (
                  <button
                    key={t}
                    type="button"
                    onClick={() => setRemoteType(t)}
                    className={`rounded-lg px-3.5 py-1.5 text-xs font-semibold transition-all ${
                      remoteType === t
                        ? 'bg-amber-500 text-zinc-950'
                        : 'text-zinc-400 hover:text-zinc-100'
                    }`}
                  >
                    {t === 'Primary' ? '海报 (Primary)' : '剧照 (Backdrop)'}
                  </button>
                ))}
              </div>
              <span className="text-xs text-zinc-500">
                共 {remoteResult?.TotalRecordCount ?? 0} 张候选图片
              </span>
            </div>

            {remoteLoading ? (
              <div className="flex h-48 items-center justify-center">
                <Loader2 className="h-6 w-6 animate-spin text-amber-400" />
              </div>
            ) : !remoteResult?.Images?.length ? (
              <div className="rounded-2xl border border-dashed border-white/12 bg-white/[0.02] py-12 text-center text-sm text-zinc-400">
                未检索到远端候选图片
              </div>
            ) : (
              <div
                className={`grid gap-4 ${
                  remoteType === 'Primary'
                    ? 'grid-cols-2 sm:grid-cols-3 md:grid-cols-4'
                    : 'grid-cols-1 sm:grid-cols-2'
                }`}
              >
                {remoteResult.Images.map((img, idx) => (
                  <div
                    key={`${img.Url}-${idx}`}
                    className="group overflow-hidden rounded-2xl border border-white/10 bg-zinc-950/80 transition-all hover:border-amber-400/50"
                  >
                    <div
                      className={`relative overflow-hidden bg-zinc-900 ${
                        remoteType === 'Primary' ? 'aspect-[2/3]' : 'aspect-video'
                      }`}
                    >
                      <img
                        src={img.ThumbnailUrl || img.Url}
                        alt={`候选图片 ${idx + 1}`}
                        loading="lazy"
                        className="h-full w-full object-cover transition-transform duration-300 group-hover:scale-105"
                      />
                      <div className="absolute inset-x-2 top-2 flex items-center justify-between gap-1">
                        {img.ProviderName && (
                          <span className="rounded-md bg-black/75 px-2 py-0.5 text-[10px] font-semibold text-amber-300 backdrop-blur-sm">
                            {img.ProviderName}
                          </span>
                        )}
                        {img.Width && img.Height && (
                          <span className="rounded-md bg-black/75 px-2 py-0.5 text-[10px] font-medium text-zinc-200 backdrop-blur-sm">
                            {img.Width}×{img.Height}
                          </span>
                        )}
                      </div>
                    </div>
                    <div className="p-2.5">
                      <button
                        type="button"
                        disabled={downloadMutation.isPending}
                        onClick={() =>
                          downloadMutation.mutate({ type: remoteType, url: img.Url })
                        }
                        className="inline-flex w-full items-center justify-center gap-1.5 rounded-xl bg-amber-500/90 px-3 py-1.5 text-xs font-semibold text-zinc-950 transition-colors hover:bg-amber-400 disabled:opacity-50"
                      >
                        {downloadMutation.isPending ? (
                          <Loader2 className="h-3.5 w-3.5 animate-spin" />
                        ) : (
                          <Check className="h-3.5 w-3.5" />
                        )}
                        <span>设为当前</span>
                      </button>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>
        )}

        {/* Tab 3: 上传 / URL 替换 */}
        {activeTab === 'upload' && (
          <div className="space-y-5">
            <div className="flex items-center gap-3">
              <span className="text-xs font-medium text-zinc-400">目标图片类型：</span>
              <div className="inline-flex rounded-xl border border-white/10 bg-zinc-950 p-1">
                {(['Primary', 'Backdrop'] as const).map((t) => (
                  <button
                    key={t}
                    type="button"
                    onClick={() => setUploadType(t)}
                    className={`rounded-lg px-3.5 py-1.5 text-xs font-semibold transition-all ${
                      uploadType === t
                        ? 'bg-amber-500 text-zinc-950'
                        : 'text-zinc-400 hover:text-zinc-100'
                    }`}
                  >
                    {t === 'Primary' ? '海报 (Primary)' : '剧照 (Backdrop)'}
                  </button>
                ))}
              </div>
            </div>

            {/* 本地文件拖拽上传区 */}
            <div
              onDragOver={(e) => {
                e.preventDefault()
                setDragging(true)
              }}
              onDragLeave={() => setDragging(false)}
              onDrop={(e) => {
                e.preventDefault()
                setDragging(false)
                handleFile(e.dataTransfer.files?.[0])
              }}
              onClick={() => fileInputRef.current?.click()}
              className={`flex cursor-pointer flex-col items-center justify-center rounded-2xl border-2 border-dashed px-6 py-10 text-center transition-all ${
                dragging
                  ? 'border-amber-400 bg-amber-500/10'
                  : 'border-white/12 bg-white/[0.02] hover:border-amber-400/50 hover:bg-white/[0.04]'
              }`}
            >
              <input
                ref={fileInputRef}
                type="file"
                accept="image/jpeg,image/png,image/webp,image/gif"
                className="hidden"
                onChange={(e) => {
                  handleFile(e.target.files?.[0])
                  e.target.value = ''
                }}
              />
              {uploadMutation.isPending ? (
                <Loader2 className="h-8 w-8 animate-spin text-amber-400" />
              ) : (
                <Upload className="h-8 w-8 text-amber-400" />
              )}
              <p className="mt-3 text-sm font-medium text-zinc-100">
                拖拽本地图片到此处，或点击选择文件
              </p>
              <p className="mt-1 text-xs text-zinc-500">
                支持 JPG / PNG / WebP / GIF（≤ 15MB），选择后立即上传替换当前
                {uploadType === 'Backdrop' ? '剧照' : '海报'}
              </p>
            </div>

            {/* 图片 URL 一键替换 */}
            <form
              onSubmit={handleUrlSubmit}
              className="space-y-2.5 rounded-2xl border border-white/10 bg-white/[0.02] p-4"
            >
              <label className="flex items-center gap-1.5 text-xs font-medium text-zinc-300">
                <Link2 className="h-3.5 w-3.5 text-amber-400" />
                通过图片 URL 一键替换
              </label>
              <div className="flex flex-col gap-2 sm:flex-row">
                <input
                  type="url"
                  value={customUrl}
                  onChange={(e) => setCustomUrl(e.target.value)}
                  placeholder="https://image.tmdb.org/t/p/w500/..."
                  className="flex-1 rounded-xl border border-white/10 bg-zinc-900 px-3.5 py-2 text-sm text-zinc-100 outline-none focus:border-amber-400/60"
                />
                <button
                  type="submit"
                  disabled={urlMutation.isPending}
                  className="inline-flex items-center justify-center gap-2 rounded-xl bg-amber-500 px-5 py-2 text-sm font-semibold text-zinc-950 transition-colors hover:bg-amber-400 disabled:opacity-60"
                >
                  {urlMutation.isPending && <Loader2 className="h-4 w-4 animate-spin" />}
                  <span>URL 替换</span>
                </button>
              </div>
            </form>
          </div>
        )}
      </div>
    </AdminModal>
  )
}
