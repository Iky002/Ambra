import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  ArrowUp,
  Check,
  ChevronRight,
  Folder,
  FolderOpen,
  FolderPlus,
  HardDrive,
  Loader2,
  Pencil,
  Plus,
  RefreshCw,
  Save,
  Trash2,
  X,
} from 'lucide-react'
import { api } from '../../api/client'
import type { VirtualFolderInfo } from '../../api/types'
import { ScanProgressBar } from '../../components/ScanProgressBar'
import { AdminModal, FeedbackBanner, ToggleSwitch } from './adminUtils'

const COLLECTION_TYPES = [
  { value: 'movies', label: '电影 (movies)' },
  { value: 'tvshows', label: '剧集 (tvshows)' },
  { value: 'music', label: '音乐 (music)' },
  { value: 'mixed', label: '混合内容 (mixed)' },
  { value: 'books', label: '图书 (books)' },
  { value: 'homevideos', label: '家庭视频 (homevideos)' },
]

function getParentPath(path: string): string {
  const normalized = path.replace(/\\/g, '/').replace(/\/+$/, '')
  if (!normalized || normalized === '') return ''
  // Windows drive root e.g. C:
  if (/^[A-Za-z]:$/.test(normalized)) return ''
  const idx = normalized.lastIndexOf('/')
  if (idx < 0) return ''
  if (idx === 0) return '/'
  const parent = normalized.slice(0, idx)
  if (/^[A-Za-z]:$/.test(parent)) return `${parent}/`
  return parent
}

export function LibrariesTab() {
  const queryClient = useQueryClient()
  const [feedback, setFeedback] = useState<{ type: 'success' | 'error'; message: string } | null>(
    null,
  )
  const [scanningId, setScanningId] = useState<string | null>(null)

  // 新增媒体库弹窗
  const [createOpen, setCreateOpen] = useState(false)
  const [libName, setLibName] = useState('')
  const [libType, setLibType] = useState('movies')
  const [paths, setPaths] = useState<string[]>([])
  const [manualPath, setManualPath] = useState('')
  const [refreshAfterCreate, setRefreshAfterCreate] = useState(true)
  const [saveLocalMetadata, setSaveLocalMetadata] = useState(true)
  const [creating, setCreating] = useState(false)

  // 服务器目录浏览器状态
  const [showBrowser, setShowBrowser] = useState(false)
  const [browsePath, setBrowsePath] = useState('')

  // 编辑媒体库弹窗
  const [editTarget, setEditTarget] = useState<VirtualFolderInfo | null>(null)
  const [editName, setEditName] = useState('')
  const [editLocations, setEditLocations] = useState<string[]>([])
  const [editSaveLocalMetadata, setEditSaveLocalMetadata] = useState(true)
  const [savingOptions, setSavingOptions] = useState(false)
  const [editNewPath, setEditNewPath] = useState('')
  const [editShowBrowser, setEditShowBrowser] = useState(false)
  const [editBrowsePath, setEditBrowsePath] = useState('')
  const [renaming, setRenaming] = useState(false)
  const [pathBusy, setPathBusy] = useState<string | null>(null)

  // 删除确认弹窗
  const [deleteTarget, setDeleteTarget] = useState<VirtualFolderInfo | null>(null)
  const [deleting, setDeleting] = useState(false)

  const { data: libraries = [], isLoading } = useQuery({
    queryKey: ['admin', 'virtualFolders'],
    queryFn: () => api.getVirtualFolders(),
  })

  const activeBrowsePath = editTarget ? editBrowsePath : browsePath
  const browserActive =
    (createOpen && showBrowser) || (Boolean(editTarget) && editShowBrowser)

  const {
    data: drives = [],
    isLoading: drivesLoading,
    error: drivesError,
  } = useQuery({
    queryKey: ['admin', 'env', 'drives'],
    queryFn: () => api.getDrives(),
    enabled: browserActive && activeBrowsePath === '',
  })

  const {
    data: dirEntries = [],
    isLoading: dirLoading,
    error: dirError,
  } = useQuery({
    queryKey: ['admin', 'env', 'dir', activeBrowsePath],
    queryFn: () => api.getDirectoryContents(activeBrowsePath, true, false),
    enabled: browserActive && activeBrowsePath !== '',
  })

  const handleScan = async (lib?: VirtualFolderInfo) => {
    const key = lib ? lib.ItemId : '__all__'
    setScanningId(key)
    setFeedback(null)
    try {
      await api.refreshLibrary(lib?.ItemId)
      await queryClient.invalidateQueries({ queryKey: ['scanProgress'] })
      setFeedback({
        type: 'success',
        message: lib ? `已触发媒体库「${lib.Name}」扫描任务` : '已触发全部媒体库扫描任务',
      })
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '触发扫描失败',
      })
    } finally {
      setScanningId(null)
    }
  }

  const addPath = (raw: string) => {
    const cleaned = raw.trim()
    if (!cleaned) return
    setPaths((prev) => (prev.includes(cleaned) ? prev : [...prev, cleaned]))
  }

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!libName.trim()) {
      setFeedback({ type: 'error', message: '请输入媒体库名称' })
      return
    }
    const finalPaths = manualPath.trim()
      ? Array.from(new Set([...paths, manualPath.trim()]))
      : paths

    if (finalPaths.length === 0) {
      setFeedback({ type: 'error', message: '请至少添加一个服务器媒体目录路径' })
      return
    }

    setCreating(true)
    setFeedback(null)
    try {
      await api.createVirtualFolder({
        Name: libName.trim(),
        CollectionType: libType,
        Paths: finalPaths,
        refreshLibrary: refreshAfterCreate,
        saveLocalMetadata,
      })
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['admin', 'virtualFolders'] }),
        queryClient.invalidateQueries({ queryKey: ['userViews'] }),
        ...(refreshAfterCreate
          ? [queryClient.invalidateQueries({ queryKey: ['scanProgress'] })]
          : []),
      ])
      setFeedback({ type: 'success', message: `已创建媒体库「${libName.trim()}」` })
      setCreateOpen(false)
      setLibName('')
      setLibType('movies')
      setPaths([])
      setManualPath('')
      setSaveLocalMetadata(true)
      setShowBrowser(false)
      setBrowsePath('')
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '创建媒体库失败',
      })
    } finally {
      setCreating(false)
    }
  }

  const openEditModal = (lib: VirtualFolderInfo) => {
    setEditTarget(lib)
    setEditName(lib.Name)
    setEditLocations([...(lib.Locations || [])])
    setEditSaveLocalMetadata(lib.LibraryOptions?.SaveLocalMetadata ?? true)
    setEditNewPath('')
    setEditShowBrowser(false)
    setEditBrowsePath('')
    setFeedback(null)
  }

  const handleToggleEditSaveLocal = async (next: boolean) => {
    if (!editTarget || savingOptions) return
    setEditSaveLocalMetadata(next)
    setSavingOptions(true)
    setFeedback(null)
    try {
      const nextOptions = {
        ...(editTarget.LibraryOptions || {}),
        SaveLocalMetadata: next,
      }
      await api.updateVirtualFolderOptions(editTarget.ItemId, nextOptions)
      setEditTarget({ ...editTarget, LibraryOptions: nextOptions })
      await queryClient.invalidateQueries({ queryKey: ['admin', 'virtualFolders'] })
      setFeedback({
        type: 'success',
        message: next
          ? `已开启「${editTarget.Name}」的本地 NFO 与图片保存`
          : `已关闭「${editTarget.Name}」的本地 NFO 与图片保存`,
      })
    } catch (err) {
      setEditSaveLocalMetadata(!next)
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '更新媒体库选项失败',
      })
    } finally {
      setSavingOptions(false)
    }
  }

  const handleRename = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!editTarget) return
    const nextName = editName.trim()
    if (!nextName) {
      setFeedback({ type: 'error', message: '媒体库名称不能为空' })
      return
    }
    if (nextName === editTarget.Name) return

    setRenaming(true)
    setFeedback(null)
    try {
      await api.renameVirtualFolder(editTarget.Name, nextName, editTarget.ItemId)
      setEditTarget({ ...editTarget, Name: nextName })
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['admin', 'virtualFolders'] }),
        queryClient.invalidateQueries({ queryKey: ['userViews'] }),
      ])
      setFeedback({ type: 'success', message: `已将媒体库重命名为「${nextName}」` })
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '重命名媒体库失败',
      })
    } finally {
      setRenaming(false)
    }
  }

  const handleAddEditPath = async (rawPath: string) => {
    if (!editTarget) return
    const cleaned = rawPath.trim()
    if (!cleaned) return
    if (editLocations.includes(cleaned)) {
      setEditNewPath('')
      return
    }

    setPathBusy(`add:${cleaned}`)
    setFeedback(null)
    try {
      await api.addVirtualFolderPath(editTarget.Name, cleaned, false, editTarget.ItemId)
      setEditLocations((prev) => (prev.includes(cleaned) ? prev : [...prev, cleaned]))
      setEditNewPath('')
      await queryClient.invalidateQueries({ queryKey: ['admin', 'virtualFolders'] })
      setFeedback({ type: 'success', message: `已为「${editTarget.Name}」添加挂载路径 ${cleaned}` })
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '添加路径失败',
      })
    } finally {
      setPathBusy(null)
    }
  }

  const handleRemoveEditPath = async (loc: string) => {
    if (!editTarget) return
    setPathBusy(`remove:${loc}`)
    setFeedback(null)
    try {
      await api.removeVirtualFolderPath(editTarget.Name, loc, editTarget.ItemId)
      setEditLocations((prev) => prev.filter((item) => item !== loc))
      await queryClient.invalidateQueries({ queryKey: ['admin', 'virtualFolders'] })
      setFeedback({ type: 'success', message: `已从「${editTarget.Name}」移除路径 ${loc}` })
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '移除路径失败',
      })
    } finally {
      setPathBusy(null)
    }
  }

  const handleDelete = async () => {
    if (!deleteTarget) return
    setDeleting(true)
    setFeedback(null)
    try {
      await api.deleteVirtualFolder({ name: deleteTarget.Name, id: deleteTarget.ItemId })
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['admin', 'virtualFolders'] }),
        queryClient.invalidateQueries({ queryKey: ['userViews'] }),
      ])
      setFeedback({ type: 'success', message: `已删除媒体库「${deleteTarget.Name}」` })
      setDeleteTarget(null)
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '删除媒体库失败',
      })
    } finally {
      setDeleting(false)
    }
  }

  const folderEntries = activeBrowsePath === '' ? drives : dirEntries
  const browserLoading = activeBrowsePath === '' ? drivesLoading : dirLoading
  const browserError = activeBrowsePath === '' ? drivesError : dirError

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-4">
        <div>
          <h2 className="flex items-center gap-2 text-base font-semibold text-zinc-100">
            <FolderOpen className="h-4 w-4 text-amber-400" />
            <span>媒体库管理</span>
          </h2>
          <p className="mt-1 text-xs text-zinc-400">
            新增或移除虚拟媒体库，浏览服务器本地路径并按需触发扫描
          </p>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <button
            type="button"
            disabled={scanningId !== null}
            onClick={() => handleScan()}
            className="inline-flex items-center gap-2 rounded-xl border border-white/10 bg-white/[0.04] px-3.5 py-2 text-xs font-medium text-zinc-200 transition-colors hover:border-amber-400/40 hover:text-amber-300 disabled:opacity-60"
          >
            <RefreshCw className={`h-3.5 w-3.5 ${scanningId === '__all__' ? 'animate-spin' : ''}`} />
            <span>全库扫描</span>
          </button>

          <button
            type="button"
            onClick={() => setCreateOpen(true)}
            className="inline-flex items-center gap-2 rounded-xl bg-amber-500 px-4 py-2 text-sm font-semibold text-zinc-950 shadow-lg shadow-amber-500/20 transition-colors hover:bg-amber-400"
          >
            <FolderPlus className="h-4 w-4" />
            <span>新增媒体库</span>
          </button>
        </div>
      </div>

      <FeedbackBanner
        message={feedback?.message}
        type={feedback?.type}
        onClose={() => setFeedback(null)}
      />

      <ScanProgressBar variant="card" />

      {isLoading ? (
        <div className="flex h-40 items-center justify-center rounded-2xl border border-white/8 bg-white/[0.02]">
          <Loader2 className="h-6 w-6 animate-spin text-amber-400" />
        </div>
      ) : libraries.length === 0 ? (
        <div className="rounded-2xl border border-white/8 bg-white/[0.02] py-14 text-center">
          <FolderOpen className="mx-auto mb-3 h-10 w-10 text-zinc-600" />
          <p className="text-sm font-medium text-zinc-300">尚未添加任何媒体库</p>
          <p className="mt-1 text-xs text-zinc-500">点击右上角「新增媒体库」挂载本地影视目录</p>
        </div>
      ) : (
        <div className="grid gap-4 md:grid-cols-2">
          {libraries.map((lib) => (
            <div
              key={lib.ItemId || lib.Name}
              className="flex flex-col justify-between rounded-2xl border border-white/8 bg-white/[0.03] p-4 transition-colors hover:border-white/15"
            >
              <div>
                <div className="flex items-start justify-between gap-2">
                  <div className="min-w-0">
                    <h3 className="truncate text-sm font-semibold text-zinc-100">{lib.Name}</h3>
                    <div className="mt-1 flex flex-wrap items-center gap-1.5">
                      <span className="inline-block rounded bg-amber-500/15 px-2 py-0.5 text-[11px] font-medium text-amber-300">
                        {COLLECTION_TYPES.find((t) => t.value === lib.CollectionType)?.label ||
                          lib.CollectionType ||
                          'mixed'}
                      </span>
                      {(lib.LibraryOptions?.SaveLocalMetadata ?? true) && (
                        <span className="inline-block rounded bg-emerald-500/12 px-2 py-0.5 text-[10px] font-medium text-emerald-300">
                          本地 NFO / 图片
                        </span>
                      )}
                    </div>
                  </div>
                  <span className="shrink-0 font-mono text-[10px] text-zinc-500">
                    {lib.ItemId?.slice(0, 8)}
                  </span>
                </div>

                <div className="mt-3 space-y-1.5">
                  <p className="text-[11px] font-medium text-zinc-500">挂载路径</p>
                  {lib.Locations && lib.Locations.length > 0 ? (
                    lib.Locations.map((loc) => (
                      <div
                        key={loc}
                        className="flex items-center gap-2 rounded-lg bg-black/30 px-2.5 py-1.5 font-mono text-xs text-zinc-300"
                      >
                        <Folder className="h-3.5 w-3.5 shrink-0 text-amber-400/80" />
                        <span className="truncate" title={loc}>
                          {loc}
                        </span>
                      </div>
                    ))
                  ) : (
                    <p className="text-xs text-zinc-500">未配置目录路径</p>
                  )}
                </div>
              </div>

              <div className="mt-4 flex items-center justify-between gap-2 border-t border-white/6 pt-3">
                <div className="flex items-center gap-2">
                  <button
                    type="button"
                    disabled={scanningId !== null}
                    onClick={() => handleScan(lib)}
                    className="inline-flex items-center gap-1.5 rounded-lg border border-white/10 bg-white/[0.05] px-3 py-1.5 text-xs font-medium text-zinc-200 transition-colors hover:border-amber-400/40 hover:text-amber-300 disabled:opacity-50"
                  >
                    <RefreshCw
                      className={`h-3.5 w-3.5 ${scanningId === lib.ItemId ? 'animate-spin' : ''}`}
                    />
                    <span>扫描此库</span>
                  </button>

                  <button
                    type="button"
                    onClick={() => openEditModal(lib)}
                    className="inline-flex items-center gap-1.5 rounded-lg border border-white/10 bg-white/[0.05] px-3 py-1.5 text-xs font-medium text-zinc-200 transition-colors hover:border-amber-400/40 hover:text-amber-300"
                  >
                    <Pencil className="h-3.5 w-3.5" />
                    <span>编辑</span>
                  </button>
                </div>

                <button
                  type="button"
                  onClick={() => setDeleteTarget(lib)}
                  className="inline-flex items-center gap-1.5 rounded-lg border border-red-500/20 bg-red-500/10 px-3 py-1.5 text-xs font-medium text-red-300 transition-colors hover:bg-red-500/20"
                >
                  <Trash2 className="h-3.5 w-3.5" />
                  <span>删除</span>
                </button>
              </div>
            </div>
          ))}
        </div>
      )}

      {/* 新增媒体库弹窗 */}
      <AdminModal
        open={createOpen}
        title="新增媒体库"
        subtitle="配置媒体库名称、类型并选择一个或多个服务器本地目录"
        maxWidthClass="max-w-2xl"
        onClose={() => setCreateOpen(false)}
      >
        <form onSubmit={handleCreate} className="space-y-4">
          <FeedbackBanner
            message={feedback?.message}
            type={feedback?.type}
            onClose={() => setFeedback(null)}
          />
          <div className="grid gap-4 sm:grid-cols-2">
            <div>
              <label className="mb-1.5 block text-xs font-medium text-zinc-400">媒体库名称 *</label>
              <input
                type="text"
                required
                value={libName}
                onChange={(e) => setLibName(e.target.value)}
                placeholder="例如: 4K 电影"
                className="w-full rounded-xl border border-white/10 bg-white/[0.04] px-3.5 py-2.5 text-sm text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/60"
              />
            </div>

            <div>
              <label className="mb-1.5 block text-xs font-medium text-zinc-400">内容类型 *</label>
              <select
                value={libType}
                onChange={(e) => setLibType(e.target.value)}
                className="w-full rounded-xl border border-white/10 bg-[#181822] px-3.5 py-2.5 text-sm text-zinc-100 outline-none focus:border-amber-400/60"
              >
                {COLLECTION_TYPES.map((t) => (
                  <option key={t.value} value={t.value}>
                    {t.label}
                  </option>
                ))}
              </select>
            </div>
          </div>

          {/* 路径输入与浏览 */}
          <div className="space-y-2.5">
            <div className="flex items-center justify-between">
              <label className="text-xs font-medium text-zinc-400">媒体目录路径 *</label>
              <button
                type="button"
                onClick={() => setShowBrowser((v) => !v)}
                className="inline-flex items-center gap-1.5 rounded-lg border border-amber-500/30 bg-amber-500/10 px-2.5 py-1 text-xs font-medium text-amber-300 transition-colors hover:bg-amber-500/20"
              >
                <HardDrive className="h-3.5 w-3.5" />
                <span>{showBrowser ? '收起目录浏览器' : '浏览服务器目录'}</span>
              </button>
            </div>

            <div className="flex gap-2">
              <input
                type="text"
                value={manualPath}
                onChange={(e) => setManualPath(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' && manualPath.trim()) {
                    e.preventDefault()
                    addPath(manualPath)
                    setManualPath('')
                  }
                }}
                placeholder="手动输入服务器路径，例如 /media/movies"
                className="flex-1 rounded-xl border border-white/10 bg-white/[0.04] px-3.5 py-2 font-mono text-xs text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/60"
              />
              <button
                type="button"
                onClick={() => {
                  addPath(manualPath)
                  setManualPath('')
                }}
                className="inline-flex items-center gap-1 rounded-xl border border-white/10 bg-white/[0.06] px-3 py-2 text-xs font-medium text-zinc-200 hover:bg-white/10"
              >
                <Plus className="h-3.5 w-3.5" />
                <span>添加</span>
              </button>
            </div>

            {/* 已选路径标签 */}
            {paths.length > 0 && (
              <div className="flex flex-wrap gap-2 pt-1">
                {paths.map((p) => (
                  <span
                    key={p}
                    className="inline-flex items-center gap-1.5 rounded-lg border border-amber-500/30 bg-amber-500/10 px-2.5 py-1 font-mono text-xs text-amber-200"
                  >
                    <Folder className="h-3 w-3 shrink-0" />
                    <span className="max-w-xs truncate">{p}</span>
                    <button
                      type="button"
                      onClick={() => setPaths((prev) => prev.filter((item) => item !== p))}
                      className="rounded p-0.5 text-amber-300/80 hover:bg-amber-500/20 hover:text-amber-100"
                    >
                      <X className="h-3 w-3" />
                    </button>
                  </span>
                ))}
              </div>
            )}

            {/* 内置服务器本地目录浏览器 */}
            {showBrowser && (
              <div className="overflow-hidden rounded-xl border border-white/10 bg-black/40">
                <div className="flex items-center justify-between gap-2 border-b border-white/8 bg-white/[0.03] px-3 py-2">
                  <div className="flex min-w-0 items-center gap-2">
                    <button
                      type="button"
                      disabled={!browsePath}
                      onClick={() => setBrowsePath(getParentPath(browsePath))}
                      className="inline-flex items-center gap-1 rounded-lg border border-white/10 bg-white/[0.05] px-2 py-1 text-[11px] font-medium text-zinc-200 hover:bg-white/10 disabled:opacity-40"
                    >
                      <ArrowUp className="h-3 w-3" />
                      <span>上级</span>
                    </button>
                    <button
                      type="button"
                      onClick={() => setBrowsePath('')}
                      className="rounded-lg px-2 py-1 text-[11px] text-zinc-400 hover:bg-white/8 hover:text-zinc-200"
                    >
                      磁盘根列表
                    </button>
                    <span className="truncate font-mono text-xs text-amber-300">
                      {browsePath || '根目录 / 驱动器'}
                    </span>
                  </div>

                  {browsePath && (
                    <button
                      type="button"
                      onClick={() => addPath(browsePath)}
                      className="inline-flex shrink-0 items-center gap-1 rounded-lg bg-amber-500/20 px-2.5 py-1 text-[11px] font-semibold text-amber-300 hover:bg-amber-500/30"
                    >
                      <Check className="h-3 w-3" />
                      <span>选择当前目录</span>
                    </button>
                  )}
                </div>

                <div className="max-h-56 overflow-y-auto p-2">
                  {browserLoading ? (
                    <div className="flex h-24 items-center justify-center">
                      <Loader2 className="h-5 w-5 animate-spin text-amber-400" />
                    </div>
                  ) : browserError ? (
                    <div className="px-3 py-6 text-center text-xs text-red-300">
                      读取服务器目录失败：
                      {browserError instanceof Error ? browserError.message : '接口暂不可用'}
                      <p className="mt-1 text-zinc-500">您仍可在上方输入框中直接手动输入路径</p>
                    </div>
                  ) : folderEntries.length === 0 ? (
                    <div className="py-8 text-center text-xs text-zinc-500">当前目录下没有子文件夹</div>
                  ) : (
                    <div className="space-y-1">
                      {folderEntries.map((entry) => {
                        const isSelected = paths.includes(entry.Path)
                        return (
                          <div
                            key={entry.Path}
                            className="flex items-center justify-between gap-2 rounded-lg px-2.5 py-1.5 text-xs hover:bg-white/[0.06]"
                          >
                            <button
                              type="button"
                              onClick={() => setBrowsePath(entry.Path)}
                              className="flex min-w-0 flex-1 items-center gap-2 text-left text-zinc-200"
                            >
                              {browsePath === '' ? (
                                <HardDrive className="h-3.5 w-3.5 shrink-0 text-amber-400" />
                              ) : (
                                <Folder className="h-3.5 w-3.5 shrink-0 text-amber-400/90" />
                              )}
                              <span className="truncate">{entry.Name || entry.Path}</span>
                              <ChevronRight className="h-3 w-3 shrink-0 text-zinc-600" />
                            </button>
                            <button
                              type="button"
                              onClick={() => addPath(entry.Path)}
                              className={`shrink-0 rounded-md px-2 py-0.5 text-[11px] font-medium transition-colors ${
                                isSelected
                                  ? 'bg-emerald-500/20 text-emerald-300'
                                  : 'bg-white/8 text-zinc-300 hover:bg-amber-500/25 hover:text-amber-200'
                              }`}
                            >
                              {isSelected ? '已添加' : '选中'}
                            </button>
                          </div>
                        )
                      })}
                    </div>
                  )}
                </div>
              </div>
            )}
          </div>

          <ToggleSwitch
            checked={saveLocalMetadata}
            onChange={setSaveLocalMetadata}
            label="保存元数据与图片到媒体文件夹"
            description="刮削完成后自动将 .nfo 与 poster.jpg / fanart.jpg 保存到媒体同目录（兼容 Emby / Kodi）"
          />

          <ToggleSwitch
            checked={refreshAfterCreate}
            onChange={setRefreshAfterCreate}
            label="创建后立即触发扫描"
            description="自动开始索引所选目录中的影音文件与元数据"
          />

          <div className="flex justify-end gap-2 pt-2">
            <button
              type="button"
              onClick={() => setCreateOpen(false)}
              className="rounded-xl border border-white/10 px-4 py-2 text-xs font-medium text-zinc-300 hover:bg-white/5"
            >
              取消
            </button>
            <button
              type="submit"
              disabled={creating}
              className="inline-flex items-center gap-2 rounded-xl bg-amber-500 px-4 py-2 text-xs font-semibold text-zinc-950 hover:bg-amber-400 disabled:opacity-60"
            >
              {creating && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              <span>创建媒体库</span>
            </button>
          </div>
        </form>
      </AdminModal>

      {/* 编辑媒体库弹窗 */}
      <AdminModal
        open={Boolean(editTarget)}
        title="编辑媒体库"
        subtitle={editTarget ? `重命名「${editTarget.Name}」或增删已挂载的媒体目录` : undefined}
        maxWidthClass="max-w-2xl"
        onClose={() => setEditTarget(null)}
      >
        <div className="space-y-5">
          <FeedbackBanner
            message={feedback?.message}
            type={feedback?.type}
            onClose={() => setFeedback(null)}
          />
          {/* 重命名表单 */}
          <form onSubmit={handleRename} className="space-y-2">
            <label className="block text-xs font-medium text-zinc-400">媒体库名称</label>
            <div className="flex gap-2">
              <input
                type="text"
                required
                value={editName}
                onChange={(e) => setEditName(e.target.value)}
                placeholder="输入新的媒体库名称"
                className="flex-1 rounded-xl border border-white/10 bg-white/[0.04] px-3.5 py-2 text-sm text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/60"
              />
              <button
                type="submit"
                disabled={renaming || !editName.trim() || editName.trim() === editTarget?.Name}
                className="inline-flex items-center gap-1.5 rounded-xl bg-amber-500 px-4 py-2 text-xs font-semibold text-zinc-950 transition-colors hover:bg-amber-400 disabled:opacity-50"
              >
                {renaming ? (
                  <Loader2 className="h-3.5 w-3.5 animate-spin" />
                ) : (
                  <Save className="h-3.5 w-3.5" />
                )}
                <span>保存名称</span>
              </button>
            </div>
          </form>

          {/* 当前挂载路径列表 */}
          <div className="space-y-2.5 border-t border-white/8 pt-4">
            <div className="flex items-center justify-between">
              <label className="text-xs font-medium text-zinc-400">
                已挂载目录 ({editLocations.length})
              </label>
              <button
                type="button"
                onClick={() => setEditShowBrowser((v) => !v)}
                className="inline-flex items-center gap-1.5 rounded-lg border border-amber-500/30 bg-amber-500/10 px-2.5 py-1 text-xs font-medium text-amber-300 transition-colors hover:bg-amber-500/20"
              >
                <HardDrive className="h-3.5 w-3.5" />
                <span>{editShowBrowser ? '收起目录浏览器' : '浏览服务器目录'}</span>
              </button>
            </div>

            {editLocations.length === 0 ? (
              <p className="rounded-xl border border-dashed border-white/10 bg-white/[0.02] py-4 text-center text-xs text-zinc-500">
                当前媒体库暂无挂载目录
              </p>
            ) : (
              <div className="space-y-1.5">
                {editLocations.map((loc) => (
                  <div
                    key={loc}
                    className="flex items-center justify-between gap-2 rounded-xl border border-white/8 bg-black/30 px-3 py-2 font-mono text-xs text-zinc-200"
                  >
                    <div className="flex min-w-0 items-center gap-2">
                      <Folder className="h-3.5 w-3.5 shrink-0 text-amber-400/80" />
                      <span className="truncate" title={loc}>
                        {loc}
                      </span>
                    </div>
                    <button
                      type="button"
                      disabled={pathBusy !== null}
                      onClick={() => void handleRemoveEditPath(loc)}
                      aria-label={`移除路径 ${loc}`}
                      className="inline-flex shrink-0 items-center gap-1 rounded-lg border border-red-500/20 bg-red-500/10 px-2 py-1 text-[11px] font-medium text-red-300 transition-colors hover:bg-red-500/20 disabled:opacity-50"
                    >
                      {pathBusy === `remove:${loc}` ? (
                        <Loader2 className="h-3 w-3 animate-spin" />
                      ) : (
                        <Trash2 className="h-3 w-3" />
                      )}
                      <span>移除</span>
                    </button>
                  </div>
                ))}
              </div>
            )}

            {/* 新增路径输入 */}
            <div className="flex gap-2 pt-1">
              <input
                type="text"
                value={editNewPath}
                onChange={(e) => setEditNewPath(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' && editNewPath.trim()) {
                    e.preventDefault()
                    void handleAddEditPath(editNewPath)
                  }
                }}
                placeholder="输入要新增的服务器目录路径，例如 /media/movies-4k"
                className="flex-1 rounded-xl border border-white/10 bg-white/[0.04] px-3.5 py-2 font-mono text-xs text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/60"
              />
              <button
                type="button"
                disabled={pathBusy !== null || !editNewPath.trim()}
                onClick={() => void handleAddEditPath(editNewPath)}
                className="inline-flex items-center gap-1 rounded-xl border border-white/10 bg-white/[0.06] px-3 py-2 text-xs font-medium text-zinc-200 hover:bg-white/10 disabled:opacity-50"
              >
                <Plus className="h-3.5 w-3.5" />
                <span>添加路径</span>
              </button>
            </div>

            {/* 复用服务器目录浏览器 */}
            {editShowBrowser && (
              <div className="overflow-hidden rounded-xl border border-white/10 bg-black/40">
                <div className="flex items-center justify-between gap-2 border-b border-white/8 bg-white/[0.03] px-3 py-2">
                  <div className="flex min-w-0 items-center gap-2">
                    <button
                      type="button"
                      disabled={!editBrowsePath}
                      onClick={() => setEditBrowsePath(getParentPath(editBrowsePath))}
                      className="inline-flex items-center gap-1 rounded-lg border border-white/10 bg-white/[0.05] px-2 py-1 text-[11px] font-medium text-zinc-200 hover:bg-white/10 disabled:opacity-40"
                    >
                      <ArrowUp className="h-3 w-3" />
                      <span>上级</span>
                    </button>
                    <button
                      type="button"
                      onClick={() => setEditBrowsePath('')}
                      className="rounded-lg px-2 py-1 text-[11px] text-zinc-400 hover:bg-white/8 hover:text-zinc-200"
                    >
                      磁盘根列表
                    </button>
                    <span className="truncate font-mono text-xs text-amber-300">
                      {editBrowsePath || '根目录 / 驱动器'}
                    </span>
                  </div>

                  {editBrowsePath && (
                    <button
                      type="button"
                      disabled={pathBusy !== null}
                      onClick={() => void handleAddEditPath(editBrowsePath)}
                      className="inline-flex shrink-0 items-center gap-1 rounded-lg bg-amber-500/20 px-2.5 py-1 text-[11px] font-semibold text-amber-300 hover:bg-amber-500/30 disabled:opacity-50"
                    >
                      <Check className="h-3 w-3" />
                      <span>添加当前目录</span>
                    </button>
                  )}
                </div>

                <div className="max-h-56 overflow-y-auto p-2">
                  {browserLoading ? (
                    <div className="flex h-24 items-center justify-center">
                      <Loader2 className="h-5 w-5 animate-spin text-amber-400" />
                    </div>
                  ) : browserError ? (
                    <div className="px-3 py-6 text-center text-xs text-red-300">
                      读取服务器目录失败：
                      {browserError instanceof Error ? browserError.message : '接口暂不可用'}
                    </div>
                  ) : folderEntries.length === 0 ? (
                    <div className="py-8 text-center text-xs text-zinc-500">当前目录下没有子文件夹</div>
                  ) : (
                    <div className="space-y-1">
                      {folderEntries.map((entry) => {
                        const isSelected = editLocations.includes(entry.Path)
                        return (
                          <div
                            key={entry.Path}
                            className="flex items-center justify-between gap-2 rounded-lg px-2.5 py-1.5 text-xs hover:bg-white/[0.06]"
                          >
                            <button
                              type="button"
                              onClick={() => setEditBrowsePath(entry.Path)}
                              className="flex min-w-0 flex-1 items-center gap-2 text-left text-zinc-200"
                            >
                              {editBrowsePath === '' ? (
                                <HardDrive className="h-3.5 w-3.5 shrink-0 text-amber-400" />
                              ) : (
                                <Folder className="h-3.5 w-3.5 shrink-0 text-amber-400/90" />
                              )}
                              <span className="truncate">{entry.Name || entry.Path}</span>
                              <ChevronRight className="h-3 w-3 shrink-0 text-zinc-600" />
                            </button>
                            <button
                              type="button"
                              disabled={pathBusy !== null || isSelected}
                              onClick={() => void handleAddEditPath(entry.Path)}
                              className={`shrink-0 rounded-md px-2 py-0.5 text-[11px] font-medium transition-colors ${
                                isSelected
                                  ? 'bg-emerald-500/20 text-emerald-300'
                                  : 'bg-white/8 text-zinc-300 hover:bg-amber-500/25 hover:text-amber-200'
                              }`}
                            >
                              {isSelected ? '已挂载' : '添加'}
                            </button>
                          </div>
                        )
                      })}
                    </div>
                  )}
                </div>
              </div>
            )}
          </div>

          {/* 刮削与元数据选项 */}
          <div className="space-y-2.5 border-t border-white/8 pt-4">
            <label className="block text-xs font-medium text-zinc-400">元数据与刮削选项</label>
            <ToggleSwitch
              checked={editSaveLocalMetadata}
              onChange={(next) => void handleToggleEditSaveLocal(next)}
              label="保存元数据与图片到媒体文件夹"
              description="刮削完成后自动将 .nfo 与 poster.jpg / fanart.jpg 保存到媒体同目录（兼容 Emby / Kodi）"
            />
          </div>

          <div className="flex justify-end border-t border-white/8 pt-3">
            <button
              type="button"
              onClick={() => setEditTarget(null)}
              className="rounded-xl border border-white/10 px-4 py-2 text-xs font-medium text-zinc-300 hover:bg-white/5"
            >
              完成
            </button>
          </div>
        </div>
      </AdminModal>

      {/* 删除媒体库确认弹窗 */}
      <AdminModal
        open={Boolean(deleteTarget)}
        title="确认删除媒体库"
        subtitle="仅从 Ambra 数据库中移除索引记录，不会删除磁盘上的原始媒体文件"
        maxWidthClass="max-w-md"
        onClose={() => setDeleteTarget(null)}
      >
        <p className="text-sm text-zinc-300">
          确定要移除媒体库 <span className="font-semibold text-white">{deleteTarget?.Name}</span>{' '}
          吗？
        </p>
        <div className="mt-6 flex justify-end gap-2">
          <button
            type="button"
            onClick={() => setDeleteTarget(null)}
            className="rounded-xl border border-white/10 px-4 py-2 text-xs font-medium text-zinc-300 hover:bg-white/5"
          >
            取消
          </button>
          <button
            type="button"
            disabled={deleting}
            onClick={handleDelete}
            className="inline-flex items-center gap-2 rounded-xl bg-red-500 px-4 py-2 text-xs font-semibold text-white hover:bg-red-400 disabled:opacity-60"
          >
            {deleting && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
            <span>确认删除</span>
          </button>
        </div>
      </AdminModal>
    </div>
  )
}
