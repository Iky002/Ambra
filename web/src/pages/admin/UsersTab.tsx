import { useMemo, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Ban,
  KeyRound,
  Loader2,
  Shield,
  ShieldCheck,
  Trash2,
  UserPlus,
  Users,
} from 'lucide-react'
import { api, ApiError } from '../../api/client'
import type { UserDto, UserPolicy } from '../../api/types'
import { QueryErrorState } from '../../components/QueryErrorState'
import { useAuth } from '../../context/AuthContext'
import { useI18n } from '../../i18n/I18nContext'
import {
  AdminModal,
  adminLabel,
  FeedbackBanner,
  formatDateTime,
  formatRelativeTime,
  ToggleSwitch,
} from './adminUtils'

const UNRATED_ITEM_TYPES = [
  { value: 'Movie', labelKey: 'admin.users.unratedMovie' },
  { value: 'Series', labelKey: 'admin.users.unratedSeries' },
  { value: 'Episode', labelKey: 'admin.users.unratedEpisode' },
  { value: 'Video', labelKey: 'admin.users.unratedVideo' },
] as const

export function UsersTab() {
  const { auth, refreshUser } = useAuth()
  const { t, locale } = useI18n()
  const queryClient = useQueryClient()

  const [feedback, setFeedback] = useState<{ type: 'success' | 'error'; message: string } | null>(
    null,
  )
  const [modalError, setModalError] = useState<string | null>(null)

  const [createOpen, setCreateOpen] = useState(false)
  const [newName, setNewName] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [newIsAdmin, setNewIsAdmin] = useState(false)
  const [creating, setCreating] = useState(false)

  const [pwTarget, setPwTarget] = useState<UserDto | null>(null)
  const [currentPw, setCurrentPw] = useState('')
  const [nextPw, setNextPw] = useState('')
  const [resetEmptyPw, setResetEmptyPw] = useState(false)
  const [savingPw, setSavingPw] = useState(false)

  const [policyTarget, setPolicyTarget] = useState<UserDto | null>(null)
  const [draftPolicy, setDraftPolicy] = useState<UserPolicy>({})
  const [blockedTagsInput, setBlockedTagsInput] = useState('')
  const [allowedTagsInput, setAllowedTagsInput] = useState('')
  const [savingPolicy, setSavingPolicy] = useState(false)

  const [deleteTarget, setDeleteTarget] = useState<UserDto | null>(null)
  const [deleting, setDeleting] = useState(false)

  const {
    data: users = [],
    isLoading,
    isError,
    error,
    refetch,
  } = useQuery({
    queryKey: ['admin', 'users'],
    queryFn: () => api.getUsers(),
  })

  const { data: libraries = [] } = useQuery({
    queryKey: ['admin', 'virtualFolders'],
    queryFn: () => api.getVirtualFolders(),
  })

  const { data: parentalRatings = [] } = useQuery({
    queryKey: ['admin', 'parentalRatings'],
    queryFn: () => api.getParentalRatings(),
    staleTime: 300_000,
  })

  const groupedParentalRatings = useMemo(() => {
    const map = new Map<number, string[]>()
    for (const r of parentalRatings) {
      const names = map.get(r.Value) ?? []
      if (!names.includes(r.Name)) {
        names.push(r.Name)
      }
      map.set(r.Value, names)
    }
    return Array.from(map.entries())
      .sort((a, b) => a[0] - b[0])
      .map(([value, names]) => ({
        value,
        label: names.join(' / '),
        primaryName: names[0] || String(value),
      }))
  }, [parentalRatings])

  const invalidateUsers = async () => {
    await queryClient.invalidateQueries({ queryKey: ['admin', 'users'] })
  }

  const handleCreateUser = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!newName.trim()) {
      setModalError(t('admin.users.usernameRequired'))
      return
    }
    setCreating(true)
    setModalError(null)
    setFeedback(null)
    try {
      const createdName = newName.trim()
      await api.createUser({
        Name: createdName,
        Password: newPassword,
        IsAdministrator: newIsAdmin,
      })
      await invalidateUsers()
      setCreateOpen(false)
      setNewName('')
      setNewPassword('')
      setNewIsAdmin(false)
      setModalError(null)
      setFeedback({ type: 'success', message: t('admin.users.createdSuccess', { name: createdName }) })
    } catch (err) {
      setModalError(err instanceof Error ? err.message : t('admin.users.createFailed'))
    } finally {
      setCreating(false)
    }
  }

  const handleDeleteUser = async () => {
    if (!deleteTarget) return
    setDeleting(true)
    setModalError(null)
    setFeedback(null)
    try {
      await api.deleteUser(deleteTarget.Id)
      await invalidateUsers()
      setFeedback({
        type: 'success',
        message: t('admin.users.deletedSuccess', { name: deleteTarget.Name }),
      })
      setDeleteTarget(null)
      setModalError(null)
    } catch (err) {
      setModalError(err instanceof Error ? err.message : t('admin.users.deleteFailed'))
    } finally {
      setDeleting(false)
    }
  }

  const openPasswordModal = (u: UserDto) => {
    setModalError(null)
    setPwTarget(u)
    setCurrentPw('')
    setNextPw('')
    setResetEmptyPw(false)
  }

  const handleUpdatePassword = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!pwTarget) return
    const isSelf = pwTarget.Id === auth?.userId
    setSavingPw(true)
    setModalError(null)
    setFeedback(null)
    try {
      await api.updateUserPassword(pwTarget.Id, {
        CurrentPw: currentPw || undefined,
        NewPw: resetEmptyPw ? '' : nextPw,
        ResetPassword: resetEmptyPw,
      })
      await invalidateUsers()
      setFeedback({
        type: 'success',
        message: isSelf
          ? resetEmptyPw
            ? t('admin.users.pwResetSelfSuccess', { name: pwTarget.Name })
            : t('admin.users.pwUpdatedSelfSuccess', { name: pwTarget.Name })
          : resetEmptyPw
            ? t('admin.users.pwResetSuccess', { name: pwTarget.Name })
            : t('admin.users.pwUpdatedSuccess', { name: pwTarget.Name }),
      })
      setPwTarget(null)
      setModalError(null)
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        setPwTarget(null)
        setModalError(null)
        setFeedback({
          type: 'error',
          message: t('admin.users.sessionExpired'),
        })
        return
      }
      setModalError(err instanceof Error ? err.message : t('admin.users.pwUpdateFailed'))
    } finally {
      setSavingPw(false)
    }
  }

  const parseTagList = (raw: string): string[] => {
    const seen = new Set<string>()
    const result: string[] = []
    for (const part of raw.split(/[,，]/)) {
      const tag = part.trim()
      if (tag && !seen.has(tag)) {
        seen.add(tag)
        result.push(tag)
      }
    }
    return result
  }

  const getParentalRatingLabel = (value?: number): string | null => {
    if (value === undefined || value === null) return null
    const found = parentalRatings.find((r) => r.Value === value)
    return found ? found.Name : String(value)
  }

  const openPolicyModal = (u: UserDto) => {
    const p = u.Policy || {}
    const blockedTags = Array.isArray(p.BlockedTags) ? [...p.BlockedTags] : []
    const allowedTags = Array.isArray(p.AllowedTags) ? [...p.AllowedTags] : []
    const blockUnrated = Array.isArray(p.BlockUnratedItems) ? [...p.BlockUnratedItems] : []
    setModalError(null)
    setPolicyTarget(u)
    setBlockedTagsInput(blockedTags.join(', '))
    setAllowedTagsInput(allowedTags.join(', '))
    setDraftPolicy({
      ...p,
      IsAdministrator: Boolean(p.IsAdministrator),
      IsHidden: Boolean(p.IsHidden),
      IsDisabled: Boolean(p.IsDisabled),
      EnableAllFolders: p.EnableAllFolders ?? true,
      EnabledFolders: Array.isArray(p.EnabledFolders) ? [...p.EnabledFolders] : [],
      EnableMediaPlayback: p.EnableMediaPlayback ?? true,
      EnableAudioPlaybackTranscoding: p.EnableAudioPlaybackTranscoding ?? true,
      EnableVideoPlaybackTranscoding: p.EnableVideoPlaybackTranscoding ?? true,
      EnablePlaybackRemuxing: p.EnablePlaybackRemuxing ?? true,
      EnableContentDownloading: Boolean(p.EnableContentDownloading),
      EnableContentDeletion: Boolean(p.EnableContentDeletion),
      EnableRemoteAccess: p.EnableRemoteAccess ?? true,
      EnableLiveTvAccess: p.EnableLiveTvAccess ?? true,
      EnableLiveTvManagement: Boolean(p.EnableLiveTvManagement),
      MaxParentalRating: p.MaxParentalRating,
      BlockUnratedItems: blockUnrated,
      BlockedTags: blockedTags,
      AllowedTags: allowedTags,
    })
  }

  const toggleFolderAccess = (folderId: string) => {
    setDraftPolicy((prev) => {
      const current = prev.EnabledFolders || []
      const exists = current.includes(folderId)
      return {
        ...prev,
        EnabledFolders: exists ? current.filter((id) => id !== folderId) : [...current, folderId],
      }
    })
  }

  const toggleUnratedType = (itemType: string) => {
    setDraftPolicy((prev) => {
      const current = prev.BlockUnratedItems || []
      const exists = current.includes(itemType)
      return {
        ...prev,
        BlockUnratedItems: exists
          ? current.filter((item) => item !== itemType)
          : [...current, itemType],
      }
    })
  }

  const handleSavePolicy = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!policyTarget) return
    setSavingPolicy(true)
    setModalError(null)
    setFeedback(null)
    try {
      const nextPolicy: UserPolicy = {
        ...draftPolicy,
        MaxParentalRating: draftPolicy.MaxParentalRating,
        BlockUnratedItems: Array.isArray(draftPolicy.BlockUnratedItems)
          ? [...draftPolicy.BlockUnratedItems]
          : [],
        BlockedTags: parseTagList(blockedTagsInput),
        AllowedTags: parseTagList(allowedTagsInput),
      }
      await api.updateUserPolicy(policyTarget.Id, nextPolicy)
      await invalidateUsers()
      if (policyTarget.Id === auth?.userId) {
        await refreshUser()
      }
      setFeedback({
        type: 'success',
        message: t('admin.users.policySavedSuccess', { name: policyTarget.Name }),
      })
      setPolicyTarget(null)
      setModalError(null)
    } catch (err) {
      setModalError(err instanceof Error ? err.message : t('admin.users.policySaveFailed'))
    } finally {
      setSavingPolicy(false)
    }
  }

  const parsedBlockedTags = parseTagList(blockedTagsInput)
  const parsedAllowedTags = parseTagList(allowedTagsInput)

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-4">
        <div>
          <h2 className="flex items-center gap-2 text-base font-semibold text-zinc-100">
            <Users className="h-4 w-4 text-amber-400" />
            <span>{t('admin.usersTitle')}</span>
          </h2>
          <p className="mt-1 text-xs text-zinc-400">{t('admin.users.subtitle')}</p>
        </div>

        <button
          type="button"
          onClick={() => {
            setModalError(null)
            setCreateOpen(true)
          }}
          className="inline-flex items-center gap-2 rounded-xl bg-amber-500 px-4 py-2 text-sm font-semibold text-zinc-950 shadow-lg shadow-amber-500/20 transition-colors hover:bg-amber-400"
        >
          <UserPlus className="h-4 w-4" />
          <span>{t('admin.createUser')}</span>
        </button>
      </div>

      <FeedbackBanner
        message={feedback?.message}
        type={feedback?.type}
        onClose={() => setFeedback(null)}
      />

      {isLoading ? (
        <div className="flex h-40 items-center justify-center rounded-2xl border border-white/8 bg-white/[0.02]">
          <Loader2 className="h-6 w-6 animate-spin text-amber-400" />
        </div>
      ) : isError ? (
        <QueryErrorState
          compact
          title={t('admin.users.loadError')}
          error={error}
          onRetry={() => void refetch()}
        />
      ) : (
        <div className="grid gap-4 md:grid-cols-2">
          {users.map((u) => {
            const isSelf = u.Id === auth?.userId
            const isAdminUser = Boolean(u.Policy?.IsAdministrator)
            const isDisabled = Boolean(u.Policy?.IsDisabled)
            const allFolders = u.Policy?.EnableAllFolders ?? true
            const folderCount = u.Policy?.EnabledFolders?.length ?? 0

            return (
              <div
                key={u.Id}
                className={`rounded-2xl border p-4 transition-colors ${
                  isDisabled
                    ? 'border-red-500/20 bg-red-500/[0.03]'
                    : 'border-white/8 bg-white/[0.03] hover:border-white/15'
                }`}
              >
                <div className="flex items-start justify-between gap-3">
                  <div className="flex min-w-0 items-center gap-3">
                    <div
                      className={`flex h-11 w-11 shrink-0 items-center justify-center rounded-full text-sm font-bold ring-1 ${
                        isAdminUser
                          ? 'bg-gradient-to-br from-amber-500/30 to-orange-500/20 text-amber-300 ring-amber-400/40'
                          : 'bg-white/8 text-zinc-200 ring-white/15'
                      }`}
                    >
                      {u.Name.slice(0, 1).toUpperCase()}
                    </div>
                    <div className="min-w-0">
                      <div className="flex flex-wrap items-center gap-1.5">
                        <p className="truncate text-sm font-semibold text-zinc-100">{u.Name}</p>
                        {isSelf && (
                          <span className="rounded bg-white/10 px-1.5 py-0.5 text-[10px] font-medium text-zinc-300">
                            {t('admin.users.selfBadge')}
                          </span>
                        )}
                        {isAdminUser ? (
                          <span className="inline-flex items-center gap-1 rounded bg-amber-500/15 px-1.5 py-0.5 text-[10px] font-semibold text-amber-300 ring-1 ring-amber-500/30">
                            <ShieldCheck className="h-3 w-3" />
                            {t('admin.users.roleAdmin')}
                          </span>
                        ) : (
                          <span className="rounded bg-zinc-800 px-1.5 py-0.5 text-[10px] font-medium text-zinc-400">
                            {t('admin.users.roleMember')}
                          </span>
                        )}
                        {isDisabled && (
                          <span className="inline-flex items-center gap-1 rounded bg-red-500/15 px-1.5 py-0.5 text-[10px] font-semibold text-red-300 ring-1 ring-red-500/30">
                            <Ban className="h-3 w-3" />
                            {t('admin.users.disabledBadge')}
                          </span>
                        )}
                      </div>
                      <p
                        className="mt-1 text-xs text-zinc-400"
                        title={formatDateTime(u.LastActivityDate || u.LastLoginDate)}
                      >
                        {t('admin.users.lastActive', {
                          time: formatRelativeTime(u.LastActivityDate || u.LastLoginDate),
                        })}
                      </p>
                    </div>
                  </div>
                </div>

                <div className="mt-3 flex flex-wrap gap-1.5 text-[11px] text-zinc-400">
                  <span className="rounded-md bg-white/[0.04] px-2 py-0.5">
                    {allFolders
                      ? t('admin.users.libAll')
                      : t('admin.users.libLimited', { count: folderCount })}
                  </span>
                  <span className="rounded-md bg-white/[0.04] px-2 py-0.5">
                    {u.Policy?.EnableVideoPlaybackTranscoding !== false
                      ? t('admin.users.transcodeAllow')
                      : t('admin.users.transcodeDeny')}
                  </span>
                  <span className="rounded-md bg-white/[0.04] px-2 py-0.5">
                    {u.Policy?.EnableContentDownloading
                      ? t('admin.users.downloadAllow')
                      : t('admin.users.downloadDeny')}
                  </span>
                  <span className="rounded-md bg-white/[0.04] px-2 py-0.5">
                    {u.Policy?.EnableContentDeletion
                      ? t('admin.users.deleteAllow')
                      : t('admin.users.deleteDeny')}
                  </span>
                  {u.Policy?.MaxParentalRating !== undefined &&
                    u.Policy?.MaxParentalRating !== null && (
                      <span className="rounded-md bg-amber-500/10 px-2 py-0.5 text-amber-300 ring-1 ring-amber-500/20">
                        {t('admin.users.ratingBadge', {
                          rating: getParentalRatingLabel(u.Policy.MaxParentalRating) || '',
                        })}
                      </span>
                    )}
                  {(u.Policy?.BlockUnratedItems?.length ?? 0) > 0 && (
                    <span className="rounded-md bg-orange-500/10 px-2 py-0.5 text-orange-300 ring-1 ring-orange-500/20">
                      {t('admin.users.unratedBadge', {
                        count: u.Policy!.BlockUnratedItems!.length,
                      })}
                    </span>
                  )}
                  {(u.Policy?.BlockedTags?.length ?? 0) > 0 && (
                    <span className="rounded-md bg-rose-500/10 px-2 py-0.5 text-rose-300 ring-1 ring-rose-500/20">
                      {t('admin.users.blockedTagsBadge', { count: u.Policy!.BlockedTags!.length })}
                    </span>
                  )}
                  {(u.Policy?.AllowedTags?.length ?? 0) > 0 && (
                    <span className="rounded-md bg-emerald-500/10 px-2 py-0.5 text-emerald-300 ring-1 ring-emerald-500/20">
                      {t('admin.users.allowedTagsBadge', { count: u.Policy!.AllowedTags!.length })}
                    </span>
                  )}
                </div>

                <div className="mt-4 flex flex-wrap items-center gap-2 border-t border-white/6 pt-3">
                  <button
                    type="button"
                    onClick={() => openPolicyModal(u)}
                    className="inline-flex items-center gap-1.5 rounded-lg border border-white/10 bg-white/[0.05] px-3 py-1.5 text-xs font-medium text-zinc-200 transition-colors hover:border-amber-400/40 hover:text-amber-300"
                  >
                    <Shield className="h-3.5 w-3.5" />
                    <span>{t('admin.users.policyBtn')}</span>
                  </button>

                  <button
                    type="button"
                    onClick={() => openPasswordModal(u)}
                    className="inline-flex items-center gap-1.5 rounded-lg border border-white/10 bg-white/[0.05] px-3 py-1.5 text-xs font-medium text-zinc-200 transition-colors hover:border-amber-400/40 hover:text-amber-300"
                  >
                    <KeyRound className="h-3.5 w-3.5" />
                    <span>{t('admin.users.changePwBtn')}</span>
                  </button>

                  <button
                    type="button"
                    disabled={isSelf}
                    onClick={() => {
                      setModalError(null)
                      setDeleteTarget(u)
                    }}
                    title={isSelf ? t('admin.users.cannotDeleteSelf') : t('admin.users.deleteUserTitle')}
                    className="ml-auto inline-flex items-center gap-1.5 rounded-lg border border-red-500/20 bg-red-500/10 px-3 py-1.5 text-xs font-medium text-red-300 transition-colors hover:bg-red-500/20 disabled:cursor-not-allowed disabled:opacity-35"
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                    <span>{t('common.delete')}</span>
                  </button>
                </div>
              </div>
            )
          })}
        </div>
      )}

      {/* 新建用户弹窗 */}
      <AdminModal
        open={createOpen}
        title={t('admin.createUser')}
        subtitle={t('admin.users.createSubtitle')}
        onClose={() => {
          setCreateOpen(false)
          setModalError(null)
        }}
      >
        <form onSubmit={handleCreateUser} className="space-y-4">
          <FeedbackBanner
            message={modalError ?? undefined}
            type="error"
            onClose={() => setModalError(null)}
          />
          <div>
            <label className="mb-1.5 block text-xs font-medium text-zinc-400">
              {t('admin.users.usernameLabel')}
            </label>
            <input
              type="text"
              required
              value={newName}
              onChange={(e) => setNewName(e.target.value)}
              placeholder={t('admin.users.usernamePlaceholder')}
              className="w-full rounded-xl border border-white/10 bg-white/[0.04] px-3.5 py-2.5 text-sm text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/60"
            />
          </div>
          <div>
            <label className="mb-1.5 block text-xs font-medium text-zinc-400">
              {t('admin.users.initPwLabel')}
            </label>
            <input
              type="password"
              value={newPassword}
              onChange={(e) => setNewPassword(e.target.value)}
              placeholder={t('admin.users.initPwPlaceholder')}
              className="w-full rounded-xl border border-white/10 bg-white/[0.04] px-3.5 py-2.5 text-sm text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/60"
            />
          </div>
          <ToggleSwitch
            checked={newIsAdmin}
            onChange={setNewIsAdmin}
            label={t('admin.users.grantAdminLabel')}
            description={t('admin.users.grantAdminDesc')}
          />
          <div className="flex justify-end gap-2 pt-2">
            <button
              type="button"
              onClick={() => {
                setCreateOpen(false)
                setModalError(null)
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
              <span>{t('admin.users.confirmCreate')}</span>
            </button>
          </div>
        </form>
      </AdminModal>

      {/* 修改/重置密码弹窗 */}
      <AdminModal
        open={Boolean(pwTarget)}
        title={t('admin.users.pwModalTitle', { name: pwTarget?.Name || '' })}
        subtitle={t('admin.users.pwModalSubtitle')}
        onClose={() => {
          setPwTarget(null)
          setModalError(null)
        }}
      >
        <form onSubmit={handleUpdatePassword} className="space-y-4">
          <FeedbackBanner
            message={modalError ?? undefined}
            type="error"
            onClose={() => setModalError(null)}
          />
          {pwTarget?.Id === auth?.userId && (
            <div>
              <label className="mb-1.5 block text-xs font-medium text-zinc-400">
                {t('admin.users.currentPwLabel')}
              </label>
              <input
                type="password"
                value={currentPw}
                onChange={(e) => setCurrentPw(e.target.value)}
                placeholder={t('admin.users.currentPwPlaceholder')}
                className="w-full rounded-xl border border-white/10 bg-white/[0.04] px-3.5 py-2.5 text-sm text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/60"
              />
            </div>
          )}

          <ToggleSwitch
            checked={resetEmptyPw}
            onChange={setResetEmptyPw}
            label={t('admin.users.resetEmptyPwLabel')}
            description={t('admin.users.resetEmptyPwDesc')}
          />

          {!resetEmptyPw && (
            <div>
              <label className="mb-1.5 block text-xs font-medium text-zinc-400">
                {t('admin.users.newPwLabel')}
              </label>
              <input
                type="password"
                value={nextPw}
                onChange={(e) => setNextPw(e.target.value)}
                placeholder={t('admin.users.newPwPlaceholder')}
                className="w-full rounded-xl border border-white/10 bg-white/[0.04] px-3.5 py-2.5 text-sm text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/60"
              />
            </div>
          )}

          <div className="flex justify-end gap-2 pt-2">
            <button
              type="button"
              onClick={() => {
                setPwTarget(null)
                setModalError(null)
              }}
              className="rounded-xl border border-white/10 px-4 py-2 text-xs font-medium text-zinc-300 hover:bg-white/5"
            >
              {t('common.cancel')}
            </button>
            <button
              type="submit"
              disabled={savingPw}
              className="inline-flex items-center gap-2 rounded-xl bg-amber-500 px-4 py-2 text-xs font-semibold text-zinc-950 hover:bg-amber-400 disabled:opacity-60"
            >
              {savingPw && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              <span>{t('admin.users.savePwBtn')}</span>
            </button>
          </div>
        </form>
      </AdminModal>

      {/* 权限策略编辑器弹窗 */}
      <AdminModal
        open={Boolean(policyTarget)}
        title={t('admin.users.policyModalTitle', { name: policyTarget?.Name || '' })}
        subtitle={t('admin.users.policyModalSubtitle')}
        maxWidthClass="max-w-xl"
        onClose={() => {
          setPolicyTarget(null)
          setModalError(null)
        }}
      >
        <form onSubmit={handleSavePolicy} className="space-y-3">
          <FeedbackBanner
            message={modalError ?? undefined}
            type="error"
            onClose={() => setModalError(null)}
          />
          <ToggleSwitch
            checked={Boolean(draftPolicy.IsAdministrator)}
            onChange={(v) => setDraftPolicy((p) => ({ ...p, IsAdministrator: v }))}
            disabled={policyTarget?.Id === auth?.userId}
            label={t('admin.users.isAdminLabel')}
            description={t('admin.users.isAdminDesc')}
          />

          <ToggleSwitch
            checked={Boolean(draftPolicy.IsDisabled)}
            onChange={(v) => setDraftPolicy((p) => ({ ...p, IsDisabled: v }))}
            disabled={policyTarget?.Id === auth?.userId}
            label={t('admin.users.isDisabledLabel')}
            description={t('admin.users.isDisabledDesc')}
          />

          <ToggleSwitch
            checked={Boolean(draftPolicy.IsHidden)}
            onChange={(v) => setDraftPolicy((p) => ({ ...p, IsHidden: v }))}
            label={adminLabel(
              t,
              locale,
              'admin.users.isHiddenLabel',
              '在登录页隐藏账号 (IsHidden)',
              'Hide user from public login screen (IsHidden)',
            )}
            description={adminLabel(
              t,
              locale,
              'admin.users.isHiddenDesc',
              '开启后该账号不会显示在公开登录页的用户选择列表中，仍可手动输入用户名登录',
              'Hides this account from the public login screen user list while still allowing manual username sign-in',
            )}
          />

          <ToggleSwitch
            checked={draftPolicy.EnableRemoteAccess !== false}
            onChange={(v) => setDraftPolicy((p) => ({ ...p, EnableRemoteAccess: v }))}
            label={adminLabel(
              t,
              locale,
              'admin.users.remoteAccessLabel',
              '允许远程网络访问 (EnableRemoteAccess)',
              'Allow remote WAN connections (EnableRemoteAccess)',
            )}
            description={adminLabel(
              t,
              locale,
              'admin.users.remoteAccessDesc',
              '允许该账号从局域网以外的远程公网连接服务器',
              'Allow this user to connect from remote WAN networks outside the local LAN',
            )}
          />

          <ToggleSwitch
            checked={Boolean(draftPolicy.EnableAllFolders)}
            onChange={(v) => setDraftPolicy((p) => ({ ...p, EnableAllFolders: v }))}
            label={t('admin.users.allFoldersLabel')}
            description={t('admin.users.allFoldersDesc')}
          />

          {!draftPolicy.EnableAllFolders && (
            <div className="rounded-xl border border-amber-500/25 bg-amber-500/[0.04] p-4">
              <p className="mb-2.5 text-xs font-semibold text-amber-300">
                {t('admin.users.folderWhitelistTitle')}
              </p>
              {libraries.length === 0 ? (
                <p className="text-xs text-zinc-500">{t('admin.users.noLibraries')}</p>
              ) : (
                <div className="grid gap-2 sm:grid-cols-2">
                  {libraries.map((lib) => {
                    const checked = (draftPolicy.EnabledFolders || []).includes(lib.ItemId)
                    return (
                      <label
                        key={lib.ItemId}
                        className={`flex cursor-pointer items-center gap-2.5 rounded-lg border px-3 py-2 text-xs transition-colors ${
                          checked
                            ? 'border-amber-400/50 bg-amber-500/15 text-amber-100'
                            : 'border-white/8 bg-white/[0.02] text-zinc-300 hover:bg-white/[0.05]'
                        }`}
                      >
                        <input
                          type="checkbox"
                          checked={checked}
                          onChange={() => toggleFolderAccess(lib.ItemId)}
                          className="h-4 w-4 rounded accent-amber-500"
                        />
                        <span className="truncate font-medium">{lib.Name}</span>
                      </label>
                    )
                  })}
                </div>
              )}
            </div>
          )}

          <ToggleSwitch
            checked={draftPolicy.EnableMediaPlayback !== false}
            onChange={(v) => setDraftPolicy((p) => ({ ...p, EnableMediaPlayback: v }))}
            label={adminLabel(
              t,
              locale,
              'admin.users.mediaPlaybackLabel',
              '允许媒体播放 (EnableMediaPlayback)',
              'Allow media playback (EnableMediaPlayback)',
            )}
            description={adminLabel(
              t,
              locale,
              'admin.users.mediaPlaybackDesc',
              '允许该账号播放服务器中的视频与音频媒体内容',
              'Allow this user to play video and audio media streams',
            )}
          />

          <ToggleSwitch
            checked={Boolean(draftPolicy.EnableVideoPlaybackTranscoding)}
            onChange={(v) => setDraftPolicy((p) => ({ ...p, EnableVideoPlaybackTranscoding: v }))}
            label={t('admin.users.videoTranscodeLabel')}
            description={t('admin.users.videoTranscodeDesc')}
          />

          <ToggleSwitch
            checked={draftPolicy.EnableAudioPlaybackTranscoding !== false}
            onChange={(v) => setDraftPolicy((p) => ({ ...p, EnableAudioPlaybackTranscoding: v }))}
            label={adminLabel(
              t,
              locale,
              'admin.users.audioTranscodeLabel',
              '允许音频转码 (EnableAudioPlaybackTranscoding)',
              'Allow audio transcoding (EnableAudioPlaybackTranscoding)',
            )}
            description={adminLabel(
              t,
              locale,
              'admin.users.audioTranscodeDesc',
              '当客户端不支持原始音轨编码（如 TrueHD / DTS）时允许服务端实时转码音频',
              'Allow real-time audio transcoding when client does not support original audio codec',
            )}
          />

          <ToggleSwitch
            checked={draftPolicy.EnablePlaybackRemuxing !== false}
            onChange={(v) => setDraftPolicy((p) => ({ ...p, EnablePlaybackRemuxing: v }))}
            label={adminLabel(
              t,
              locale,
              'admin.users.remuxingLabel',
              '允许容器无损重封装 (EnablePlaybackRemuxing)',
              'Allow container remuxing (EnablePlaybackRemuxing)',
            )}
            description={adminLabel(
              t,
              locale,
              'admin.users.remuxingDesc',
              '允许在不重新编码视频流的前提下将 MKV / TS 等容器实时重封装为客户端兼容格式',
              'Allow lossless container remuxing without video re-encoding',
            )}
          />

          <ToggleSwitch
            checked={Boolean(draftPolicy.EnableContentDownloading)}
            onChange={(v) => setDraftPolicy((p) => ({ ...p, EnableContentDownloading: v }))}
            label={t('admin.users.contentDownloadLabel')}
            description={t('admin.users.contentDownloadDesc')}
          />

          <ToggleSwitch
            checked={Boolean(draftPolicy.EnableContentDeletion)}
            onChange={(v) => setDraftPolicy((p) => ({ ...p, EnableContentDeletion: v }))}
            label={t('admin.users.contentDeleteLabel')}
            description={t('admin.users.contentDeleteDesc')}
          />

          <ToggleSwitch
            checked={draftPolicy.EnableLiveTvAccess !== false}
            onChange={(v) => setDraftPolicy((p) => ({ ...p, EnableLiveTvAccess: v }))}
            label={t('admin.users.liveTvAccessLabel')}
            description={t('admin.users.liveTvAccessDesc')}
          />

          <ToggleSwitch
            checked={Boolean(draftPolicy.EnableLiveTvManagement)}
            onChange={(v) => setDraftPolicy((p) => ({ ...p, EnableLiveTvManagement: v }))}
            label={t('admin.users.liveTvManageLabel')}
            description={t('admin.users.liveTvManageDesc')}
          />

          <div className="space-y-3.5 rounded-xl border border-white/8 bg-white/[0.02] p-4">
            <p className="text-xs font-semibold text-amber-300">
              {t('admin.users.parentalSectionTitle')}
            </p>

            <div>
              <label
                htmlFor="user-max-parental-rating"
                className="mb-1.5 block text-xs font-medium text-zinc-400"
              >
                {t('admin.users.maxRatingLabel')}
              </label>
              <select
                id="user-max-parental-rating"
                value={
                  draftPolicy.MaxParentalRating !== undefined &&
                  draftPolicy.MaxParentalRating !== null
                    ? String(draftPolicy.MaxParentalRating)
                    : ''
                }
                onChange={(e) => {
                  const val = e.target.value
                  setDraftPolicy((p) => ({
                    ...p,
                    MaxParentalRating: val === '' ? undefined : Number(val),
                  }))
                }}
                className="w-full rounded-xl border border-white/10 bg-[#14141c] px-3.5 py-2.5 text-sm text-zinc-100 outline-none focus:border-amber-400/60"
              >
                <option value="">{t('admin.users.unrestricted')}</option>
                {groupedParentalRatings.map((group) => (
                  <option key={group.value} value={String(group.value)}>
                    {group.label} (≤ {group.value})
                  </option>
                ))}
              </select>
            </div>

            {/* 拦截未分级内容 (BlockUnratedItems) */}
            <div>
              <p className="text-xs font-medium text-zinc-300">
                {t('admin.users.blockUnratedLabel')}
              </p>
              <p className="mt-0.5 text-[11px] text-zinc-500">
                {t('admin.users.blockUnratedDesc')}
              </p>
              <div className="mt-2 grid grid-cols-2 gap-2 sm:grid-cols-4">
                {UNRATED_ITEM_TYPES.map((item) => {
                  const checked = (draftPolicy.BlockUnratedItems || []).includes(item.value)
                  return (
                    <label
                      key={item.value}
                      className={`flex cursor-pointer items-center gap-2 rounded-lg border px-2.5 py-2 text-xs transition-colors ${
                        checked
                          ? 'border-amber-400/50 bg-amber-500/15 text-amber-100'
                          : 'border-white/8 bg-white/[0.02] text-zinc-300 hover:bg-white/[0.05]'
                      }`}
                    >
                      <input
                        type="checkbox"
                        checked={checked}
                        onChange={() => toggleUnratedType(item.value)}
                        className="h-3.5 w-3.5 rounded accent-amber-500"
                      />
                      <span className="truncate">{t(item.labelKey)}</span>
                    </label>
                  )
                })}
              </div>
            </div>

            {/* 屏蔽标签 (BlockedTags) */}
            <div>
              <label
                htmlFor="user-blocked-tags"
                className="mb-1.5 block text-xs font-medium text-zinc-400"
              >
                {t('admin.users.blockedTagsLabel')}
              </label>
              <input
                id="user-blocked-tags"
                type="text"
                value={blockedTagsInput}
                onChange={(e) => setBlockedTagsInput(e.target.value)}
                placeholder={t('admin.users.blockedTagsPlaceholder')}
                className="w-full rounded-xl border border-white/10 bg-white/[0.04] px-3.5 py-2.5 text-sm text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/60"
              />
              {parsedBlockedTags.length > 0 && (
                <div className="mt-2 flex flex-wrap gap-1.5">
                  {parsedBlockedTags.map((tag) => (
                    <span
                      key={tag}
                      className="rounded-md bg-rose-500/15 px-2 py-0.5 text-[11px] text-rose-200 ring-1 ring-rose-500/25"
                    >
                      {tag}
                    </span>
                  ))}
                </div>
              )}
            </div>

            {/* 白名单允许标签 (AllowedTags) */}
            <div>
              <label
                htmlFor="user-allowed-tags"
                className="mb-1.5 block text-xs font-medium text-zinc-400"
              >
                {t('admin.users.allowedTagsLabel')}
              </label>
              <input
                id="user-allowed-tags"
                type="text"
                value={allowedTagsInput}
                onChange={(e) => setAllowedTagsInput(e.target.value)}
                placeholder={t('admin.users.allowedTagsPlaceholder')}
                className="w-full rounded-xl border border-white/10 bg-white/[0.04] px-3.5 py-2.5 text-sm text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/60"
              />
              {parsedAllowedTags.length > 0 && (
                <div className="mt-2 flex flex-wrap gap-1.5">
                  {parsedAllowedTags.map((tag) => (
                    <span
                      key={tag}
                      className="rounded-md bg-emerald-500/15 px-2 py-0.5 text-[11px] text-emerald-200 ring-1 ring-emerald-500/25"
                    >
                      {tag}
                    </span>
                  ))}
                </div>
              )}
            </div>
          </div>

          <div className="flex justify-end gap-2 pt-3">
            <button
              type="button"
              onClick={() => {
                setPolicyTarget(null)
                setModalError(null)
              }}
              className="rounded-xl border border-white/10 px-4 py-2 text-xs font-medium text-zinc-300 hover:bg-white/5"
            >
              {t('common.cancel')}
            </button>
            <button
              type="submit"
              disabled={savingPolicy}
              className="inline-flex items-center gap-2 rounded-xl bg-amber-500 px-4 py-2 text-xs font-semibold text-zinc-950 hover:bg-amber-400 disabled:opacity-60"
            >
              {savingPolicy && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              <span>{t('admin.users.savePolicyBtn')}</span>
            </button>
          </div>
        </form>
      </AdminModal>

      {/* 删除用户确认弹窗 */}
      <AdminModal
        open={Boolean(deleteTarget)}
        title={t('admin.users.deleteModalTitle')}
        subtitle={t('admin.users.deleteModalSubtitle')}
        maxWidthClass="max-w-md"
        onClose={() => {
          setDeleteTarget(null)
          setModalError(null)
        }}
      >
        <div className="space-y-4">
          <FeedbackBanner
            message={modalError ?? undefined}
            type="error"
            onClose={() => setModalError(null)}
          />
          <p className="text-sm text-zinc-300">
            {t('admin.users.deleteConfirmPrompt', { name: deleteTarget?.Name || '' })}
          </p>
          <div className="mt-6 flex justify-end gap-2">
            <button
              type="button"
              onClick={() => {
                setDeleteTarget(null)
                setModalError(null)
              }}
              className="rounded-xl border border-white/10 px-4 py-2 text-xs font-medium text-zinc-300 hover:bg-white/5"
            >
              {t('common.cancel')}
            </button>
            <button
              type="button"
              disabled={deleting}
              onClick={handleDeleteUser}
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
