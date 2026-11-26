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
import { QueryErrorState } from '../../components/QueryErrorState'
import { ScanProgressBar } from '../../components/ScanProgressBar'
import { useI18n } from '../../i18n/I18nContext'
import { AdminModal, FeedbackBanner, ToggleSwitch } from './adminUtils'

function getParentPath(path: string): string {
  const normalized = path.replace(/\\/g, '/').replace(/\/+$/, '')
  if (!normalized || normalized === '') return ''
  if (/^[A-Za-z]:$/.test(normalized)) return ''
  const idx = normalized.lastIndexOf('/')
  if (idx < 0) return ''
  if (idx === 0) return '/'
  const parent = normalized.slice(0, idx)
  if (/^[A-Za-z]:$/.test(parent)) return `${parent}/`
  return parent
}

export function LibrariesTab() {
  const { t } = useI18n()
  const queryClient = useQueryClient()
  const [feedback, setFeedback] = useState<{ type: 'success' | 'error'; message: string } | null>(
    null,
  )
  const [modalFeedback, setModalFeedback] = useState<{
    type: 'success' | 'error'
    message: string
  } | null>(null)
  const [scanningId, setScanningId] = useState<string | null>(null)

  const [createOpen, setCreateOpen] = useState(false)
  const [libName, setLibName] = useState('')
  const [libType, setLibType] = useState('movies')
  const [paths, setPaths] = useState<string[]>([])
  const [manualPath, setManualPath] = useState('')
  const [refreshAfterCreate, setRefreshAfterCreate] = useState(true)
  const [saveLocalMetadata, setSaveLocalMetadata] = useState(true)
  const [creating, setCreating] = useState(false)

  const [showBrowser, setShowBrowser] = useState(false)
  const [browsePath, setBrowsePath] = useState('')

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

  const [deleteTarget, setDeleteTarget] = useState<VirtualFolderInfo | null>(null)
  const [deleting, setDeleting] = useState(false)

  const collectionTypes = [
    { value: 'movies', label: t('admin.libraries.typeMovies') },
    { value: 'tvshows', label: t('admin.libraries.typeTvShows') },
    { value: 'music', label: t('admin.libraries.typeMusic') },
    { value: 'mixed', label: t('admin.libraries.typeMixed') },
    { value: 'books', label: t('admin.libraries.typeBooks') },
    { value: 'homevideos', label: t('admin.libraries.typeHomeVideos') },
  ]

  const {
    data: libraries = [],
    isLoading,
    isError,
    error,
    refetch,
  } = useQuery({
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
        message: lib
          ? t('admin.libraries.scanSingleSuccess', { name: lib.Name })
          : t('admin.libraries.scanAllSuccess'),
      })
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : t('admin.scanTriggerFailed'),
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
      setModalFeedback({ type: 'error', message: t('admin.libraries.errNameRequired') })
      return
    }
    const finalPaths = manualPath.trim()
      ? Array.from(new Set([...paths, manualPath.trim()]))
      : paths

    if (finalPaths.length === 0) {
      setModalFeedback({ type: 'error', message: t('admin.libraries.errPathRequired') })
      return
    }

    setCreating(true)
    setModalFeedback(null)
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
      setFeedback({
        type: 'success',
        message: t('admin.libraries.createdSuccess', { name: libName.trim() }),
      })
      setModalFeedback(null)
      setCreateOpen(false)
      setLibName('')
      setLibType('movies')
      setPaths([])
      setManualPath('')
      setSaveLocalMetadata(true)
      setShowBrowser(false)
      setBrowsePath('')
    } catch (err) {
      setModalFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : t('admin.libraries.createFailed'),
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
    setModalFeedback(null)
  }

  const handleToggleEditSaveLocal = async (next: boolean) => {
    if (!editTarget || savingOptions) return
    setEditSaveLocalMetadata(next)
    setSavingOptions(true)
    setModalFeedback(null)
    try {
      const nextOptions = {
        ...(editTarget.LibraryOptions || {}),
        SaveLocalMetadata: next,
      }
      await api.updateVirtualFolderOptions(editTarget.ItemId, nextOptions)
      setEditTarget({ ...editTarget, LibraryOptions: nextOptions })
      await queryClient.invalidateQueries({ queryKey: ['admin', 'virtualFolders'] })
      setModalFeedback({
        type: 'success',
        message: next
          ? t('admin.libraries.nfoEnabledSuccess', { name: editTarget.Name })
          : t('admin.libraries.nfoDisabledSuccess', { name: editTarget.Name }),
      })
    } catch (err) {
      setEditSaveLocalMetadata(!next)
      setModalFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : t('admin.libraries.optionsUpdateFailed'),
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
      setModalFeedback({ type: 'error', message: t('admin.libraries.errNameEmpty') })
      return
    }
    if (nextName === editTarget.Name) return

    setRenaming(true)
    setModalFeedback(null)
    try {
      await api.renameVirtualFolder(editTarget.Name, nextName, editTarget.ItemId)
      setEditTarget({ ...editTarget, Name: nextName })
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['admin', 'virtualFolders'] }),
        queryClient.invalidateQueries({ queryKey: ['userViews'] }),
      ])
      setModalFeedback({
        type: 'success',
        message: t('admin.libraries.renamedSuccess', { name: nextName }),
      })
    } catch (err) {
      setModalFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : t('admin.libraries.renameFailed'),
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
    setModalFeedback(null)
    try {
      await api.addVirtualFolderPath(editTarget.Name, cleaned, false, editTarget.ItemId)
      setEditLocations((prev) => (prev.includes(cleaned) ? prev : [...prev, cleaned]))
      setEditNewPath('')
      await queryClient.invalidateQueries({ queryKey: ['admin', 'virtualFolders'] })
      setModalFeedback({
        type: 'success',
        message: t('admin.libraries.pathAddedSuccess', { name: editTarget.Name, path: cleaned }),
      })
    } catch (err) {
      setModalFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : t('admin.libraries.pathAddFailed'),
      })
    } finally {
      setPathBusy(null)
    }
  }

  const handleRemoveEditPath = async (loc: string) => {
    if (!editTarget) return
    setPathBusy(`remove:${loc}`)
    setModalFeedback(null)
    try {
      await api.removeVirtualFolderPath(editTarget.Name, loc, editTarget.ItemId)
      setEditLocations((prev) => prev.filter((item) => item !== loc))
      await queryClient.invalidateQueries({ queryKey: ['admin', 'virtualFolders'] })
      setModalFeedback({
        type: 'success',
        message: t('admin.libraries.pathRemovedSuccess', { name: editTarget.Name, path: loc }),
      })
    } catch (err) {
      setModalFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : t('admin.libraries.pathRemoveFailed'),
      })
    } finally {
      setPathBusy(null)
    }
  }

  const handleDelete = async () => {
    if (!deleteTarget) return
    setDeleting(true)
    setModalFeedback(null)
    try {
      await api.deleteVirtualFolder({ name: deleteTarget.Name, id: deleteTarget.ItemId })
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['admin', 'virtualFolders'] }),
        queryClient.invalidateQueries({ queryKey: ['userViews'] }),
      ])
      setFeedback({
        type: 'success',
        message: t('admin.libraries.deletedSuccess', { name: deleteTarget.Name }),
      })
      setModalFeedback(null)
      setDeleteTarget(null)
    } catch (err) {
      setModalFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : t('admin.libraries.deleteFailed'),
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
            <span>{t('admin.tabLibraries')}</span>
          </h2>
          <p className="mt-1 text-xs text-zinc-400">{t('admin.libraries.subtitle')}</p>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <button
            type="button"
            disabled={scanningId !== null}
            onClick={() => handleScan()}
            className="inline-flex items-center gap-2 rounded-xl border border-white/10 bg-white/[0.04] px-3.5 py-2 text-xs font-medium text-zinc-200 transition-colors hover:border-amber-400/40 hover:text-amber-300 disabled:opacity-60"
          >
            <RefreshCw className={`h-3.5 w-3.5 ${scanningId === '__all__' ? 'animate-spin' : ''}`} />
            <span>{t('admin.libraries.scanAllBtn')}</span>
          </button>

          <button
            type="button"
            onClick={() => {
              setModalFeedback(null)
              setCreateOpen(true)
            }}
            className="inline-flex items-center gap-2 rounded-xl bg-amber-500 px-4 py-2 text-sm font-semibold text-zinc-950 shadow-lg shadow-amber-500/20 transition-colors hover:bg-amber-400"
          >
            <FolderPlus className="h-4 w-4" />
            <span>{t('admin.libraries.addLibBtn')}</span>
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
      ) : isError ? (
        <QueryErrorState
          compact
          title={t('admin.libraries.loadError')}
          error={error}
          onRetry={() => void refetch()}
        />
      ) : libraries.length === 0 ? (
        <div className="rounded-2xl border border-white/8 bg-white/[0.02] py-14 text-center">
          <FolderOpen className="mx-auto mb-3 h-10 w-10 text-zinc-600" />
          <p className="text-sm font-medium text-zinc-300">{t('admin.libraries.emptyTitle')}</p>
          <p className="mt-1 text-xs text-zinc-500">{t('admin.libraries.emptyDesc')}</p>
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
                        {collectionTypes.find((item) => item.value === lib.CollectionType)?.label ||
                          lib.CollectionType ||
                          'mixed'}
                      </span>
                      {(lib.LibraryOptions?.SaveLocalMetadata ?? true) && (
                        <span className="inline-block rounded bg-emerald-500/12 px-2 py-0.5 text-[10px] font-medium text-emerald-300">
                          {t('admin.libraries.localNfoBadge')}
                        </span>
                      )}
                    </div>
                  </div>
                  <span className="shrink-0 font-mono text-[10px] text-zinc-500">
                    {lib.ItemId?.slice(0, 8)}
                  </span>
                </div>

                <div className="mt-3 space-y-1.5">
                  <p className="text-[11px] font-medium text-zinc-500">
                    {t('admin.libraries.mountPathsLabel')}
                  </p>
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
                    <p className="text-xs text-zinc-500">{t('admin.libraries.noPathsConfigured')}</p>
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
                    <span>{t('admin.libraries.scanThisBtn')}</span>
                  </button>

                  <button
                    type="button"
                    onClick={() => openEditModal(lib)}
                    className="inline-flex items-center gap-1.5 rounded-lg border border-white/10 bg-white/[0.05] px-3 py-1.5 text-xs font-medium text-zinc-200 transition-colors hover:border-amber-400/40 hover:text-amber-300"
                  >
                    <Pencil className="h-3.5 w-3.5" />
                    <span>{t('common.edit')}</span>
                  </button>
                </div>

                <button
                  type="button"
                  onClick={() => {
                    setModalFeedback(null)
                    setDeleteTarget(lib)
                  }}
                  className="inline-flex items-center gap-1.5 rounded-lg border border-red-500/20 bg-red-500/10 px-3 py-1.5 text-xs font-medium text-red-300 transition-colors hover:bg-red-500/20"
                >
                  <Trash2 className="h-3.5 w-3.5" />
                  <span>{t('common.delete')}</span>
                </button>
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Create Library Modal */}
      <AdminModal
        open={createOpen}
        title={t('admin.libraries.addLibBtn')}
        subtitle={t('admin.libraries.createModalSubtitle')}
        maxWidthClass="max-w-2xl"
        onClose={() => {
          setCreateOpen(false)
          setModalFeedback(null)
        }}
      >
        <form onSubmit={handleCreate} className="space-y-4">
          <FeedbackBanner
            message={modalFeedback?.message}
            type={modalFeedback?.type}
            onClose={() => setModalFeedback(null)}
          />
          <div className="grid gap-4 sm:grid-cols-2">
            <div>
              <label className="mb-1.5 block text-xs font-medium text-zinc-400">
                {t('admin.libraries.libNameLabel')}
              </label>
              <input
                type="text"
                required
                value={libName}
                onChange={(e) => setLibName(e.target.value)}
                placeholder={t('admin.libraries.libNamePlaceholder')}
                className="w-full rounded-xl border border-white/10 bg-white/[0.04] px-3.5 py-2.5 text-sm text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/60"
              />
            </div>

            <div>
              <label className="mb-1.5 block text-xs font-medium text-zinc-400">
                {t('admin.libraries.libTypeLabel')}
              </label>
              <select
                value={libType}
                onChange={(e) => setLibType(e.target.value)}
                className="w-full rounded-xl border border-white/10 bg-[#181822] px-3.5 py-2.5 text-sm text-zinc-100 outline-none focus:border-amber-400/60"
              >
                {collectionTypes.map((item) => (
                  <option key={item.value} value={item.value}>
                    {item.label}
                  </option>
                ))}
              </select>
            </div>
          </div>

          <div className="space-y-2.5">
            <div className="flex items-center justify-between">
              <label className="text-xs font-medium text-zinc-400">
                {t('admin.libraries.mediaDirLabel')}
              </label>
              <button
                type="button"
                onClick={() => setShowBrowser((v) => !v)}
                className="inline-flex items-center gap-1.5 rounded-lg border border-amber-500/30 bg-amber-500/10 px-2.5 py-1 text-xs font-medium text-amber-300 transition-colors hover:bg-amber-500/20"
              >
                <HardDrive className="h-3.5 w-3.5" />
                <span>
                  {showBrowser
                    ? t('admin.libraries.collapseBrowser')
                    : t('admin.libraries.browseServerDir')}
                </span>
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
                placeholder={t('admin.libraries.manualPathPlaceholder')}
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
                <span>{t('admin.tasks.addBtn')}</span>
              </button>
            </div>

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
                      <span>{t('admin.libraries.upLevel')}</span>
                    </button>
                    <button
                      type="button"
                      onClick={() => setBrowsePath('')}
                      className="rounded-lg px-2 py-1 text-[11px] text-zinc-400 hover:bg-white/8 hover:text-zinc-200"
                    >
                      {t('admin.libraries.diskRootList')}
                    </button>
                    <span className="truncate font-mono text-xs text-amber-300">
                      {browsePath || t('admin.libraries.rootDrives')}
                    </span>
                  </div>

                  {browsePath && (
                    <button
                      type="button"
                      onClick={() => addPath(browsePath)}
                      className="inline-flex shrink-0 items-center gap-1 rounded-lg bg-amber-500/20 px-2.5 py-1 text-[11px] font-semibold text-amber-300 hover:bg-amber-500/30"
                    >
                      <Check className="h-3 w-3" />
                      <span>{t('admin.libraries.selectCurrentDir')}</span>
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
                      {t('admin.libraries.browseDirFailed', {
                        error:
                          browserError instanceof Error
                            ? browserError.message
                            : t('admin.livetv.retryLater'),
                      })}
                      <p className="mt-1 text-zinc-500">{t('admin.libraries.manualFallbackHint')}</p>
                    </div>
                  ) : folderEntries.length === 0 ? (
                    <div className="py-8 text-center text-xs text-zinc-500">
                      {t('admin.libraries.noSubfolders')}
                    </div>
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
                              {isSelected
                                ? t('admin.libraries.addedBadge')
                                : t('admin.libraries.selectBtn')}
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
            label={t('admin.libraries.saveLocalMetaLabel')}
            description={t('admin.libraries.saveLocalMetaDesc')}
          />

          <ToggleSwitch
            checked={refreshAfterCreate}
            onChange={setRefreshAfterCreate}
            label={t('admin.libraries.scanAfterCreateLabel')}
            description={t('admin.libraries.scanAfterCreateDesc')}
          />

          <div className="flex justify-end gap-2 pt-2">
            <button
              type="button"
              onClick={() => {
                setCreateOpen(false)
                setModalFeedback(null)
              }}
              className="rounded-xl border border-white/10 px-4 py-2 text-xs font-medium text-zinc-300 hover:bg-white/5"
            >
              {t('common.cancel')}
            </button>
            <button
              type="submit"
              disabled={creating}
              className="inline-flex items-center gap-2 rounded-xl bg-amber-500 px-4 py-2 text-xs font-semibold text-zinc-950 hover:bg-amber-400 disabled:opacity-60"
            >
              {creating && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              <span>{t('admin.libraries.createConfirmBtn')}</span>
            </button>
          </div>
        </form>
      </AdminModal>

      {/* Edit Library Modal */}
      <AdminModal
        open={Boolean(editTarget)}
        title={t('admin.libraries.editModalTitle')}
        subtitle={
          editTarget
            ? t('admin.libraries.editModalSubtitle', { name: editTarget.Name })
            : undefined
        }
        maxWidthClass="max-w-2xl"
        onClose={() => {
          setEditTarget(null)
          setModalFeedback(null)
        }}
      >
        <div className="space-y-5">
          <FeedbackBanner
            message={modalFeedback?.message}
            type={modalFeedback?.type}
            onClose={() => setModalFeedback(null)}
          />
          <form onSubmit={handleRename} className="space-y-2">
            <label className="block text-xs font-medium text-zinc-400">
              {t('admin.libraries.libNameLabel').replace(/\s*\*$/, '')}
            </label>
            <div className="flex gap-2">
              <input
                type="text"
                required
                value={editName}
                onChange={(e) => setEditName(e.target.value)}
                placeholder={t('admin.libraries.newNamePlaceholder')}
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
                <span>{t('admin.libraries.saveNameBtn')}</span>
              </button>
            </div>
          </form>

          <div className="space-y-2.5 border-t border-white/8 pt-4">
            <div className="flex items-center justify-between">
              <label className="text-xs font-medium text-zinc-400">
                {t('admin.libraries.mountedDirsCount', { count: editLocations.length })}
              </label>
              <button
                type="button"
                onClick={() => setEditShowBrowser((v) => !v)}
                className="inline-flex items-center gap-1.5 rounded-lg border border-amber-500/30 bg-amber-500/10 px-2.5 py-1 text-xs font-medium text-amber-300 transition-colors hover:bg-amber-500/20"
              >
                <HardDrive className="h-3.5 w-3.5" />
                <span>
                  {editShowBrowser
                    ? t('admin.libraries.collapseBrowser')
                    : t('admin.libraries.browseServerDir')}
                </span>
              </button>
            </div>

            {editLocations.length === 0 ? (
              <p className="rounded-xl border border-dashed border-white/10 bg-white/[0.02] py-4 text-center text-xs text-zinc-500">
                {t('admin.libraries.noMountedDirs')}
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
                      aria-label={t('admin.libraries.removePathAria', { path: loc })}
                      className="inline-flex shrink-0 items-center gap-1 rounded-lg border border-red-500/20 bg-red-500/10 px-2 py-1 text-[11px] font-medium text-red-300 transition-colors hover:bg-red-500/20 disabled:opacity-50"
                    >
                      {pathBusy === `remove:${loc}` ? (
                        <Loader2 className="h-3 w-3 animate-spin" />
                      ) : (
                        <Trash2 className="h-3 w-3" />
                      )}
                      <span>{t('admin.libraries.removeBtn')}</span>
                    </button>
                  </div>
                ))}
              </div>
            )}

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
                placeholder={t('admin.libraries.addNewPathPlaceholder')}
                className="flex-1 rounded-xl border border-white/10 bg-white/[0.04] px-3.5 py-2 font-mono text-xs text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/60"
              />
              <button
                type="button"
                disabled={pathBusy !== null || !editNewPath.trim()}
                onClick={() => void handleAddEditPath(editNewPath)}
                className="inline-flex items-center gap-1 rounded-xl border border-white/10 bg-white/[0.06] px-3 py-2 text-xs font-medium text-zinc-200 hover:bg-white/10 disabled:opacity-50"
              >
                <Plus className="h-3.5 w-3.5" />
                <span>{t('admin.libraries.addPathBtn')}</span>
              </button>
            </div>

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
                      <span>{t('admin.libraries.upLevel')}</span>
                    </button>
                    <button
                      type="button"
                      onClick={() => setEditBrowsePath('')}
                      className="rounded-lg px-2 py-1 text-[11px] text-zinc-400 hover:bg-white/8 hover:text-zinc-200"
                    >
                      {t('admin.libraries.diskRootList')}
                    </button>
                    <span className="truncate font-mono text-xs text-amber-300">
                      {editBrowsePath || t('admin.libraries.rootDrives')}
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
                      <span>{t('admin.libraries.addCurrentDir')}</span>
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
                      {t('admin.libraries.browseDirFailed', {
                        error:
                          browserError instanceof Error
                            ? browserError.message
                            : t('admin.livetv.retryLater'),
                      })}
                    </div>
                  ) : folderEntries.length === 0 ? (
                    <div className="py-8 text-center text-xs text-zinc-500">
                      {t('admin.libraries.noSubfolders')}
                    </div>
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
                              {isSelected
                                ? t('admin.libraries.mountedBadge')
                                : t('admin.tasks.addBtn')}
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

          <div className="space-y-2.5 border-t border-white/8 pt-4">
            <label className="block text-xs font-medium text-zinc-400">
              {t('admin.libraries.metaOptionsLabel')}
            </label>
            <ToggleSwitch
              checked={editSaveLocalMetadata}
              onChange={(next) => void handleToggleEditSaveLocal(next)}
              label={t('admin.libraries.saveLocalMetaLabel')}
              description={t('admin.libraries.saveLocalMetaDesc')}
            />
          </div>

          <div className="flex justify-end border-t border-white/8 pt-3">
            <button
              type="button"
              onClick={() => {
                setEditTarget(null)
                setModalFeedback(null)
              }}
              className="rounded-xl border border-white/10 px-4 py-2 text-xs font-medium text-zinc-300 hover:bg-white/5"
            >
              {t('admin.libraries.doneBtn')}
            </button>
          </div>
        </div>
      </AdminModal>

      {/* Delete Library Confirm Modal */}
      <AdminModal
        open={Boolean(deleteTarget)}
        title={t('admin.libraries.deleteModalTitle')}
        subtitle={t('admin.libraries.deleteModalSubtitle')}
        maxWidthClass="max-w-md"
        onClose={() => {
          setDeleteTarget(null)
          setModalFeedback(null)
        }}
      >
        <div className="space-y-4">
          <FeedbackBanner
            message={modalFeedback?.message}
            type={modalFeedback?.type}
            onClose={() => setModalFeedback(null)}
          />
          <p className="text-sm text-zinc-300">
            {t('admin.libraries.deleteConfirmPrompt', { name: deleteTarget?.Name || '' })}
          </p>
          <div className="mt-6 flex justify-end gap-2">
            <button
              type="button"
              onClick={() => {
                setDeleteTarget(null)
                setModalFeedback(null)
              }}
              className="rounded-xl border border-white/10 px-4 py-2 text-xs font-medium text-zinc-300 hover:bg-white/5"
            >
              {t('common.cancel')}
            </button>
            <button
              type="button"
              disabled={deleting}
              onClick={handleDelete}
              className="inline-flex items-center gap-2 rounded-xl bg-red-500 px-4 py-2 text-xs font-semibold text-white hover:bg-red-400 disabled:opacity-60"
            >
              {deleting && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              <span>{t('admin.users.confirmDelete')}</span>
            </button>
          </div>
        </div>
      </AdminModal>
    </div>
  )
}
