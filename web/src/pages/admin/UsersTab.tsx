import { useState } from 'react'
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
import { api } from '../../api/client'
import type { UserDto, UserPolicy } from '../../api/types'
import { useAuth } from '../../context/AuthContext'
import {
  AdminModal,
  FeedbackBanner,
  formatDateTime,
  formatRelativeTime,
  ToggleSwitch,
} from './adminUtils'

export function UsersTab() {
  const { auth, refreshUser } = useAuth()
  const queryClient = useQueryClient()

  const [feedback, setFeedback] = useState<{ type: 'success' | 'error'; message: string } | null>(
    null,
  )

  // 新建用户弹窗状态
  const [createOpen, setCreateOpen] = useState(false)
  const [newName, setNewName] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [newIsAdmin, setNewIsAdmin] = useState(false)
  const [creating, setCreating] = useState(false)

  // 密码重置弹窗状态
  const [pwTarget, setPwTarget] = useState<UserDto | null>(null)
  const [currentPw, setCurrentPw] = useState('')
  const [nextPw, setNextPw] = useState('')
  const [resetEmptyPw, setResetEmptyPw] = useState(false)
  const [savingPw, setSavingPw] = useState(false)

  // 权限策略弹窗状态
  const [policyTarget, setPolicyTarget] = useState<UserDto | null>(null)
  const [draftPolicy, setDraftPolicy] = useState<UserPolicy>({})
  const [savingPolicy, setSavingPolicy] = useState(false)

  // 删除确认弹窗状态
  const [deleteTarget, setDeleteTarget] = useState<UserDto | null>(null)
  const [deleting, setDeleting] = useState(false)

  const { data: users = [], isLoading } = useQuery({
    queryKey: ['admin', 'users'],
    queryFn: () => api.getUsers(),
  })

  const { data: libraries = [] } = useQuery({
    queryKey: ['admin', 'virtualFolders'],
    queryFn: () => api.getVirtualFolders(),
  })

  const invalidateUsers = async () => {
    await queryClient.invalidateQueries({ queryKey: ['admin', 'users'] })
  }

  const handleCreateUser = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!newName.trim()) {
      setFeedback({ type: 'error', message: '请输入用户名' })
      return
    }
    setCreating(true)
    setFeedback(null)
    try {
      await api.createUser({
        Name: newName.trim(),
        Password: newPassword,
        IsAdministrator: newIsAdmin,
      })
      await invalidateUsers()
      setCreateOpen(false)
      setNewName('')
      setNewPassword('')
      setNewIsAdmin(false)
      setFeedback({ type: 'success', message: `用户「${newName.trim()}」创建成功` })
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '创建用户失败',
      })
    } finally {
      setCreating(false)
    }
  }

  const handleDeleteUser = async () => {
    if (!deleteTarget) return
    setDeleting(true)
    setFeedback(null)
    try {
      await api.deleteUser(deleteTarget.Id)
      await invalidateUsers()
      setFeedback({ type: 'success', message: `已删除用户「${deleteTarget.Name}」` })
      setDeleteTarget(null)
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '删除用户失败',
      })
    } finally {
      setDeleting(false)
    }
  }

  const openPasswordModal = (u: UserDto) => {
    setPwTarget(u)
    setCurrentPw('')
    setNextPw('')
    setResetEmptyPw(false)
  }

  const handleUpdatePassword = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!pwTarget) return
    setSavingPw(true)
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
        message: resetEmptyPw
          ? `已将用户「${pwTarget.Name}」的密码重置为空`
          : `已更新用户「${pwTarget.Name}」的密码`,
      })
      setPwTarget(null)
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '更新密码失败',
      })
    } finally {
      setSavingPw(false)
    }
  }

  const openPolicyModal = (u: UserDto) => {
    const p = u.Policy || {}
    setPolicyTarget(u)
    setDraftPolicy({
      ...p,
      IsAdministrator: Boolean(p.IsAdministrator),
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

  const handleSavePolicy = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!policyTarget) return
    setSavingPolicy(true)
    setFeedback(null)
    try {
      await api.updateUserPolicy(policyTarget.Id, draftPolicy)
      await invalidateUsers()
      if (policyTarget.Id === auth?.userId) {
        await refreshUser()
      }
      setFeedback({ type: 'success', message: `已更新「${policyTarget.Name}」的权限策略` })
      setPolicyTarget(null)
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '保存权限策略失败',
      })
    } finally {
      setSavingPolicy(false)
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-4">
        <div>
          <h2 className="flex items-center gap-2 text-base font-semibold text-zinc-100">
            <Users className="h-4 w-4 text-amber-400" />
            <span>用户与权限管理 (RBAC)</span>
          </h2>
          <p className="mt-1 text-xs text-zinc-400">
            管理服务器账号、密码策略、管理员角色与媒体库访问白名单
          </p>
        </div>

        <button
          type="button"
          onClick={() => setCreateOpen(true)}
          className="inline-flex items-center gap-2 rounded-xl bg-amber-500 px-4 py-2 text-sm font-semibold text-zinc-950 shadow-lg shadow-amber-500/20 transition-colors hover:bg-amber-400"
        >
          <UserPlus className="h-4 w-4" />
          <span>新建用户</span>
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
                            当前账号
                          </span>
                        )}
                        {isAdminUser ? (
                          <span className="inline-flex items-center gap-1 rounded bg-amber-500/15 px-1.5 py-0.5 text-[10px] font-semibold text-amber-300 ring-1 ring-amber-500/30">
                            <ShieldCheck className="h-3 w-3" />
                            管理员
                          </span>
                        ) : (
                          <span className="rounded bg-zinc-800 px-1.5 py-0.5 text-[10px] font-medium text-zinc-400">
                            普通用户
                          </span>
                        )}
                        {isDisabled && (
                          <span className="inline-flex items-center gap-1 rounded bg-red-500/15 px-1.5 py-0.5 text-[10px] font-semibold text-red-300 ring-1 ring-red-500/30">
                            <Ban className="h-3 w-3" />
                            已禁用
                          </span>
                        )}
                      </div>
                      <p
                        className="mt-1 text-xs text-zinc-400"
                        title={formatDateTime(u.LastActivityDate || u.LastLoginDate)}
                      >
                        最后活跃: {formatRelativeTime(u.LastActivityDate || u.LastLoginDate)}
                      </p>
                    </div>
                  </div>
                </div>

                {/* 权限摘要徽章 */}
                <div className="mt-3 flex flex-wrap gap-1.5 text-[11px] text-zinc-400">
                  <span className="rounded-md bg-white/[0.04] px-2 py-0.5">
                    媒体库: {allFolders ? '全部' : `仅限 ${folderCount} 个`}
                  </span>
                  <span className="rounded-md bg-white/[0.04] px-2 py-0.5">
                    转码: {u.Policy?.EnableVideoPlaybackTranscoding !== false ? '允许' : '禁止'}
                  </span>
                  <span className="rounded-md bg-white/[0.04] px-2 py-0.5">
                    下载: {u.Policy?.EnableContentDownloading ? '允许' : '禁止'}
                  </span>
                  <span className="rounded-md bg-white/[0.04] px-2 py-0.5">
                    删除: {u.Policy?.EnableContentDeletion ? '允许' : '禁止'}
                  </span>
                </div>

                {/* 操作栏 */}
                <div className="mt-4 flex flex-wrap items-center gap-2 border-t border-white/6 pt-3">
                  <button
                    type="button"
                    onClick={() => openPolicyModal(u)}
                    className="inline-flex items-center gap-1.5 rounded-lg border border-white/10 bg-white/[0.05] px-3 py-1.5 text-xs font-medium text-zinc-200 transition-colors hover:border-amber-400/40 hover:text-amber-300"
                  >
                    <Shield className="h-3.5 w-3.5" />
                    <span>权限策略</span>
                  </button>

                  <button
                    type="button"
                    onClick={() => openPasswordModal(u)}
                    className="inline-flex items-center gap-1.5 rounded-lg border border-white/10 bg-white/[0.05] px-3 py-1.5 text-xs font-medium text-zinc-200 transition-colors hover:border-amber-400/40 hover:text-amber-300"
                  >
                    <KeyRound className="h-3.5 w-3.5" />
                    <span>修改密码</span>
                  </button>

                  <button
                    type="button"
                    disabled={isSelf}
                    onClick={() => setDeleteTarget(u)}
                    title={isSelf ? '不能删除当前登录账号' : '删除用户'}
                    className="ml-auto inline-flex items-center gap-1.5 rounded-lg border border-red-500/20 bg-red-500/10 px-3 py-1.5 text-xs font-medium text-red-300 transition-colors hover:bg-red-500/20 disabled:cursor-not-allowed disabled:opacity-35"
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                    <span>删除</span>
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
        title="新建用户"
        subtitle="创建一个新的 Ambra 媒体服务器账号"
        onClose={() => setCreateOpen(false)}
      >
        <form onSubmit={handleCreateUser} className="space-y-4">
          <div>
            <label className="mb-1.5 block text-xs font-medium text-zinc-400">用户名 *</label>
            <input
              type="text"
              required
              value={newName}
              onChange={(e) => setNewName(e.target.value)}
              placeholder="例如: alice"
              className="w-full rounded-xl border border-white/10 bg-white/[0.04] px-3.5 py-2.5 text-sm text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/60"
            />
          </div>
          <div>
            <label className="mb-1.5 block text-xs font-medium text-zinc-400">
              初始密码（留空表示无密码）
            </label>
            <input
              type="password"
              value={newPassword}
              onChange={(e) => setNewPassword(e.target.value)}
              placeholder="输入登录密码"
              className="w-full rounded-xl border border-white/10 bg-white/[0.04] px-3.5 py-2.5 text-sm text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/60"
            />
          </div>
          <ToggleSwitch
            checked={newIsAdmin}
            onChange={setNewIsAdmin}
            label="授予管理员权限"
            description="管理员可访问管理控制台并管理用户、媒体库、插件与系统配置"
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
              <span>确认创建</span>
            </button>
          </div>
        </form>
      </AdminModal>

      {/* 修改/重置密码弹窗 */}
      <AdminModal
        open={Boolean(pwTarget)}
        title={`修改密码 · ${pwTarget?.Name || ''}`}
        subtitle="管理员可直接为其他用户设置新密码或重置为空密码"
        onClose={() => setPwTarget(null)}
      >
        <form onSubmit={handleUpdatePassword} className="space-y-4">
          {pwTarget?.Id === auth?.userId && (
            <div>
              <label className="mb-1.5 block text-xs font-medium text-zinc-400">
                当前密码（修改本人密码时填写）
              </label>
              <input
                type="password"
                value={currentPw}
                onChange={(e) => setCurrentPw(e.target.value)}
                placeholder="如原密码为空可留空"
                className="w-full rounded-xl border border-white/10 bg-white/[0.04] px-3.5 py-2.5 text-sm text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/60"
              />
            </div>
          )}

          <ToggleSwitch
            checked={resetEmptyPw}
            onChange={setResetEmptyPw}
            label="重置为空密码"
            description="开启后将清除该用户的登录密码"
          />

          {!resetEmptyPw && (
            <div>
              <label className="mb-1.5 block text-xs font-medium text-zinc-400">新密码</label>
              <input
                type="password"
                value={nextPw}
                onChange={(e) => setNextPw(e.target.value)}
                placeholder="输入新密码"
                className="w-full rounded-xl border border-white/10 bg-white/[0.04] px-3.5 py-2.5 text-sm text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/60"
              />
            </div>
          )}

          <div className="flex justify-end gap-2 pt-2">
            <button
              type="button"
              onClick={() => setPwTarget(null)}
              className="rounded-xl border border-white/10 px-4 py-2 text-xs font-medium text-zinc-300 hover:bg-white/5"
            >
              取消
            </button>
            <button
              type="submit"
              disabled={savingPw}
              className="inline-flex items-center gap-2 rounded-xl bg-amber-500 px-4 py-2 text-xs font-semibold text-zinc-950 hover:bg-amber-400 disabled:opacity-60"
            >
              {savingPw && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              <span>保存密码</span>
            </button>
          </div>
        </form>
      </AdminModal>

      {/* 权限策略编辑器弹窗 */}
      <AdminModal
        open={Boolean(policyTarget)}
        title={`权限策略 · ${policyTarget?.Name || ''}`}
        subtitle="精细配置账号角色、媒体库可见范围与播放/管理权限"
        maxWidthClass="max-w-xl"
        onClose={() => setPolicyTarget(null)}
      >
        <form onSubmit={handleSavePolicy} className="space-y-3">
          <ToggleSwitch
            checked={Boolean(draftPolicy.IsAdministrator)}
            onChange={(v) => setDraftPolicy((p) => ({ ...p, IsAdministrator: v }))}
            label="管理员权限 (IsAdministrator)"
            description="允许管理服务器设置、用户、媒体库、设备与插件"
          />

          <ToggleSwitch
            checked={Boolean(draftPolicy.IsDisabled)}
            onChange={(v) => setDraftPolicy((p) => ({ ...p, IsDisabled: v }))}
            disabled={policyTarget?.Id === auth?.userId}
            label="禁用账号 (IsDisabled)"
            description="禁用后该账号将无法登录或继续使用现有会话"
          />

          <ToggleSwitch
            checked={Boolean(draftPolicy.EnableAllFolders)}
            onChange={(v) => setDraftPolicy((p) => ({ ...p, EnableAllFolders: v }))}
            label="允许访问全部媒体库 (EnableAllFolders)"
            description="关闭后仅可访问下方勾选的媒体库白名单"
          />

          {!draftPolicy.EnableAllFolders && (
            <div className="rounded-xl border border-amber-500/25 bg-amber-500/[0.04] p-4">
              <p className="mb-2.5 text-xs font-semibold text-amber-300">
                媒体库访问白名单 (EnabledFolders)
              </p>
              {libraries.length === 0 ? (
                <p className="text-xs text-zinc-500">当前暂无媒体库可选</p>
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
            checked={Boolean(draftPolicy.EnableVideoPlaybackTranscoding)}
            onChange={(v) => setDraftPolicy((p) => ({ ...p, EnableVideoPlaybackTranscoding: v }))}
            label="允许视频转码 (EnableVideoPlaybackTranscoding)"
            description="当客户端不支持直接播放时允许服务端进行实时转码"
          />

          <ToggleSwitch
            checked={Boolean(draftPolicy.EnableContentDownloading)}
            onChange={(v) => setDraftPolicy((p) => ({ ...p, EnableContentDownloading: v }))}
            label="允许下载媒体内容 (EnableContentDownloading)"
            description="允许用户下载原始视频或离线缓存文件"
          />

          <ToggleSwitch
            checked={Boolean(draftPolicy.EnableContentDeletion)}
            onChange={(v) => setDraftPolicy((p) => ({ ...p, EnableContentDeletion: v }))}
            label="允许删除媒体内容 (EnableContentDeletion)"
            description="危险权限：允许该账号从媒体库中删除媒体文件"
          />

          <div className="flex justify-end gap-2 pt-3">
            <button
              type="button"
              onClick={() => setPolicyTarget(null)}
              className="rounded-xl border border-white/10 px-4 py-2 text-xs font-medium text-zinc-300 hover:bg-white/5"
            >
              取消
            </button>
            <button
              type="submit"
              disabled={savingPolicy}
              className="inline-flex items-center gap-2 rounded-xl bg-amber-500 px-4 py-2 text-xs font-semibold text-zinc-950 hover:bg-amber-400 disabled:opacity-60"
            >
              {savingPolicy && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              <span>保存策略</span>
            </button>
          </div>
        </form>
      </AdminModal>

      {/* 删除用户确认弹窗 */}
      <AdminModal
        open={Boolean(deleteTarget)}
        title="确认删除用户"
        subtitle="此操作不可恢复，将清除该用户的登录凭证与个人数据"
        maxWidthClass="max-w-md"
        onClose={() => setDeleteTarget(null)}
      >
        <p className="text-sm text-zinc-300">
          确定要永久删除用户 <span className="font-semibold text-white">{deleteTarget?.Name}</span>{' '}
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
            onClick={handleDeleteUser}
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
