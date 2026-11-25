import { useEffect, useRef, useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import {
  AlertCircle,
  Camera,
  CheckCircle2,
  KeyRound,
  Loader2,
  QrCode,
  ShieldCheck,
  Sliders,
  Trash2,
  User as UserIcon,
} from 'lucide-react'
import { api, getUserImageUrl } from '../api/client'
import { useAuth } from '../context/AuthContext'
import {
  loadPlaybackPrefs,
  savePlaybackPrefs,
  type PlaybackPrefs,
} from '../player/playbackPrefs'
import { FeedbackBanner } from './admin/adminUtils'

const AUDIO_LANG_OPTIONS = [
  { value: '', label: '跟随媒体默认' },
  { value: 'chi', label: '中文 (国语/粤语)' },
  { value: 'eng', label: '英语 (English)' },
  { value: 'jpn', label: '日语 (日本語)' },
  { value: 'kor', label: '韩语 (한국어)' },
]

const SUBTITLE_LANG_OPTIONS = [
  { value: '', label: '跟随媒体默认' },
  { value: 'chs', label: '简体中文优先' },
  { value: 'cht', label: '繁体中文优先' },
  { value: 'chi', label: '中文（不限简繁）' },
  { value: 'eng', label: '英语 (English)' },
  { value: 'jpn', label: '日语 (日本語)' },
  { value: 'off', label: '默认关闭字幕' },
]

export function ProfilePage() {
  const { auth, user, isAdmin, refreshUser } = useAuth()
  const userId = auth?.userId || ''
  const fileInputRef = useRef<HTMLInputElement>(null)

  const [avatarError, setAvatarError] = useState(false)
  const [feedback, setFeedback] = useState<{ type: 'success' | 'error'; message: string } | null>(
    null,
  )

  // 修改密码表单
  const [currentPw, setCurrentPw] = useState('')
  const [newPw, setNewPw] = useState('')
  const [confirmPw, setConfirmPw] = useState('')

  // Quick Connect 授权新设备
  const [qcCode, setQcCode] = useState('')
  const [qcFeedback, setQcFeedback] = useState<{
    type: 'success' | 'error'
    message: string
  } | null>(null)

  // 本地播放偏好
  const [prefs, setPrefs] = useState<PlaybackPrefs>(() => loadPlaybackPrefs())

  useEffect(() => {
    setAvatarError(false)
  }, [user?.PrimaryImageTag])

  useEffect(() => {
    if (!feedback) return
    const t = window.setTimeout(() => setFeedback(null), 4500)
    return () => window.clearTimeout(t)
  }, [feedback])

  const uploadAvatarMutation = useMutation({
    mutationFn: async (file: File) => {
      const dataUrl = await new Promise<string>((resolve, reject) => {
        const reader = new FileReader()
        reader.onload = () => resolve(String(reader.result || ''))
        reader.onerror = () => reject(new Error('读取图片文件失败'))
        reader.readAsDataURL(file)
      })
      await api.uploadUserImage(userId, dataUrl, file.type || 'image/jpeg')
    },
    onSuccess: async () => {
      await refreshUser()
      setAvatarError(false)
      setFeedback({ type: 'success', message: '头像已更新' })
    },
    onError: (err) => {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '上传头像失败',
      })
    },
  })

  const deleteAvatarMutation = useMutation({
    mutationFn: () => api.deleteUserImage(userId),
    onSuccess: async () => {
      await refreshUser()
      setAvatarError(false)
      setFeedback({ type: 'success', message: '已移除个人头像' })
    },
    onError: (err) => {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '移除头像失败',
      })
    },
  })

  const passwordMutation = useMutation({
    mutationFn: () =>
      api.updateUserPassword(userId, {
        CurrentPw: currentPw,
        NewPw: newPw,
      }),
    onSuccess: () => {
      setCurrentPw('')
      setNewPw('')
      setConfirmPw('')
      setFeedback({ type: 'success', message: '登录密码已修改成功' })
    },
    onError: (err) => {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '修改密码失败，请检查当前密码',
      })
    },
  })

  const handlePasswordSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    if (!newPw.trim()) {
      setFeedback({ type: 'error', message: '请输入新密码' })
      return
    }
    if (newPw !== confirmPw) {
      setFeedback({ type: 'error', message: '两次输入的新密码不一致' })
      return
    }
    passwordMutation.mutate()
  }

  const quickConnectMutation = useMutation({
    mutationFn: (code: string) => api.quickConnectAuthorize(code),
    onSuccess: (ok) => {
      if (ok === false) {
        setQcFeedback({ type: 'error', message: '授权码无效或已过期，请核对后重试' })
        return
      }
      setQcCode('')
      setQcFeedback({ type: 'success', message: '已成功授权该设备登录' })
    },
    onError: (err) => {
      setQcFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '授权失败，请检查授权码是否正确或已过期',
      })
    },
  })

  const handleQuickConnectSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    const trimmed = qcCode.trim()
    if (!trimmed) {
      setQcFeedback({ type: 'error', message: '请输入 6 位 Quick Connect 授权码' })
      return
    }
    setQcFeedback(null)
    quickConnectMutation.mutate(trimmed)
  }

  const handlePrefsChange = (patch: Partial<PlaybackPrefs>) => {
    const updated = savePlaybackPrefs(patch)
    setPrefs(updated)
    setFeedback({ type: 'success', message: '播放偏好已保存至本地浏览器' })
  }

  if (!auth) return null

  const hasAvatar = Boolean(user?.PrimaryImageTag) && !avatarError
  const avatarUrl = hasAvatar ? getUserImageUrl(userId, user?.PrimaryImageTag) : null
  const displayName = user?.Name || auth?.userName || '用户'

  return (
    <div className="mx-auto max-w-4xl space-y-6 px-4 pt-6 lg:px-8">
      <div className="border-b border-white/8 pb-5">
        <h1 className="flex items-center gap-2.5 text-2xl font-bold tracking-tight text-white">
          <UserIcon className="h-6 w-6 text-amber-400" />
          <span>个人中心</span>
        </h1>
        <p className="mt-1 text-xs text-zinc-400">
          管理您的账号头像、登录密码与本机播放器偏好设置
        </p>
      </div>

      {feedback && (
        <FeedbackBanner
          type={feedback.type}
          message={feedback.message}
          onClose={() => setFeedback(null)}
        />
      )}

      {/* 账号与头像 */}
      <section className="rounded-2xl border border-white/10 bg-zinc-950/70 p-6 backdrop-blur-md">
        <div className="flex flex-col gap-5 sm:flex-row sm:items-center sm:justify-between">
          <div className="flex items-center gap-4">
            <div className="relative flex h-20 w-20 shrink-0 items-center justify-center overflow-hidden rounded-full bg-gradient-to-br from-amber-500/30 to-orange-500/20 text-2xl font-bold text-amber-300 ring-2 ring-amber-400/35">
              {avatarUrl ? (
                <img
                  src={avatarUrl}
                  alt={displayName}
                  onError={() => setAvatarError(true)}
                  className="h-full w-full object-cover"
                />
              ) : (
                <span>{displayName.slice(0, 1).toUpperCase()}</span>
              )}
            </div>
            <div>
              <div className="flex items-center gap-2">
                <h2 className="text-lg font-semibold text-white">{displayName}</h2>
                <span
                  className={`inline-flex items-center gap-1 rounded-full px-2.5 py-0.5 text-[11px] font-medium ${
                    isAdmin
                      ? 'bg-amber-500/20 text-amber-300 ring-1 ring-amber-400/35'
                      : 'bg-white/10 text-zinc-300'
                  }`}
                >
                  {isAdmin && <ShieldCheck className="h-3 w-3" />}
                  {isAdmin ? '管理员' : '普通成员'}
                </span>
              </div>
              <p className="mt-1 text-xs text-zinc-400">
                用户 ID：<span className="font-mono text-zinc-300">{userId}</span>
              </p>
              {user?.LastLoginDate && (
                <p className="mt-0.5 text-[11px] text-zinc-500">
                  最近登录：{new Date(user.LastLoginDate).toLocaleString('zh-CN')}
                </p>
              )}
            </div>
          </div>

          <div className="flex flex-wrap items-center gap-2.5">
            <input
              ref={fileInputRef}
              type="file"
              accept="image/*"
              aria-label="选择头像图片"
              className="hidden"
              onChange={(e) => {
                const file = e.target.files?.[0]
                if (file) {
                  uploadAvatarMutation.mutate(file)
                }
                e.target.value = ''
              }}
            />
            <button
              type="button"
              disabled={uploadAvatarMutation.isPending}
              onClick={() => fileInputRef.current?.click()}
              className="inline-flex items-center gap-1.5 rounded-xl bg-amber-500 px-4 py-2 text-xs font-semibold text-zinc-950 transition-colors hover:bg-amber-400 disabled:opacity-50"
            >
              {uploadAvatarMutation.isPending ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : (
                <Camera className="h-3.5 w-3.5" />
              )}
              <span>上传头像</span>
            </button>
            {user?.PrimaryImageTag && (
              <button
                type="button"
                disabled={deleteAvatarMutation.isPending}
                onClick={() => deleteAvatarMutation.mutate()}
                className="inline-flex items-center gap-1.5 rounded-xl border border-red-500/30 bg-red-500/10 px-3.5 py-2 text-xs font-medium text-red-300 transition-colors hover:bg-red-500/20 disabled:opacity-50"
              >
                <Trash2 className="h-3.5 w-3.5" />
                <span>移除头像</span>
              </button>
            )}
          </div>
        </div>
      </section>

      <div className="grid gap-6 md:grid-cols-2">
        {/* 修改密码 */}
        <section className="rounded-2xl border border-white/10 bg-zinc-950/70 p-6 backdrop-blur-md">
          <div className="mb-4 flex items-center gap-2">
            <KeyRound className="h-4 w-4 text-amber-400" />
            <h2 className="text-base font-semibold text-white">修改密码</h2>
          </div>

          <form onSubmit={handlePasswordSubmit} className="space-y-4">
            <label className="block space-y-1.5">
              <span className="text-xs font-medium text-zinc-300">当前密码</span>
              <input
                type="password"
                value={currentPw}
                onChange={(e) => setCurrentPw(e.target.value)}
                placeholder="如未设置过密码可留空"
                autoComplete="current-password"
                className="w-full rounded-xl border border-white/10 bg-white/[0.04] px-3.5 py-2.5 text-sm text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/60"
              />
            </label>

            <label className="block space-y-1.5">
              <span className="text-xs font-medium text-zinc-300">新密码</span>
              <input
                type="password"
                value={newPw}
                onChange={(e) => setNewPw(e.target.value)}
                placeholder="请输入新密码"
                autoComplete="new-password"
                className="w-full rounded-xl border border-white/10 bg-white/[0.04] px-3.5 py-2.5 text-sm text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/60"
              />
            </label>

            <label className="block space-y-1.5">
              <span className="text-xs font-medium text-zinc-300">确认新密码</span>
              <input
                type="password"
                value={confirmPw}
                onChange={(e) => setConfirmPw(e.target.value)}
                placeholder="再次输入新密码"
                autoComplete="new-password"
                className="w-full rounded-xl border border-white/10 bg-white/[0.04] px-3.5 py-2.5 text-sm text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/60"
              />
            </label>

            <button
              type="submit"
              disabled={passwordMutation.isPending}
              className="inline-flex w-full items-center justify-center gap-2 rounded-xl bg-amber-500 px-4 py-2.5 text-sm font-semibold text-zinc-950 transition-colors hover:bg-amber-400 disabled:opacity-50"
            >
              {passwordMutation.isPending ? (
                <Loader2 className="h-4 w-4 animate-spin" />
              ) : (
                <CheckCircle2 className="h-4 w-4" />
              )}
              <span>保存新密码</span>
            </button>
          </form>
        </section>

        {/* 本地播放偏好 */}
        <section className="rounded-2xl border border-white/10 bg-zinc-950/70 p-6 backdrop-blur-md">
          <div className="mb-4 flex items-center gap-2">
            <Sliders className="h-4 w-4 text-amber-400" />
            <h2 className="text-base font-semibold text-white">播放偏好设置</h2>
          </div>

          <div className="space-y-5">
            <div className="flex items-start justify-between gap-4 rounded-xl border border-white/8 bg-white/[0.03] p-3.5">
              <div>
                <p className="text-sm font-medium text-zinc-100">剧集自动连播下一集</p>
                <p className="mt-0.5 text-xs text-zinc-400">
                  分集播放结束时显示 8 秒倒计时，自动衔接播放下一集
                </p>
              </div>
              <button
                type="button"
                role="switch"
                aria-label="剧集自动连播下一集"
                aria-checked={prefs.autoPlayNextEpisode}
                onClick={() =>
                  handlePrefsChange({ autoPlayNextEpisode: !prefs.autoPlayNextEpisode })
                }
                className={`mt-0.5 inline-flex h-6 w-11 shrink-0 items-center rounded-full p-0.5 transition-colors ${
                  prefs.autoPlayNextEpisode ? 'bg-amber-500' : 'bg-zinc-700'
                }`}
              >
                <span
                  className={`h-5 w-5 rounded-full bg-white shadow transition-transform ${
                    prefs.autoPlayNextEpisode ? 'translate-x-5' : 'translate-x-0'
                  }`}
                />
              </button>
            </div>

            <label className="block space-y-1.5">
              <span className="text-xs font-medium text-zinc-300">首选音轨语言</span>
              <select
                value={prefs.preferredAudioLang}
                onChange={(e) => handlePrefsChange({ preferredAudioLang: e.target.value })}
                className="w-full rounded-xl border border-white/10 bg-zinc-900 px-3.5 py-2.5 text-sm text-zinc-100 outline-none focus:border-amber-400/60"
              >
                {AUDIO_LANG_OPTIONS.map((opt) => (
                  <option key={opt.value} value={opt.value}>
                    {opt.label}
                  </option>
                ))}
              </select>
            </label>

            <label className="block space-y-1.5">
              <span className="text-xs font-medium text-zinc-300">首选字幕语言</span>
              <select
                value={prefs.preferredSubtitleLang}
                onChange={(e) => handlePrefsChange({ preferredSubtitleLang: e.target.value })}
                className="w-full rounded-xl border border-white/10 bg-zinc-900 px-3.5 py-2.5 text-sm text-zinc-100 outline-none focus:border-amber-400/60"
              >
                {SUBTITLE_LANG_OPTIONS.map((opt) => (
                  <option key={opt.value} value={opt.value}>
                    {opt.label}
                  </option>
                ))}
              </select>
            </label>
          </div>
        </section>
      </div>

      {/* Quick Connect 授权新设备 */}
      <section className="rounded-2xl border border-white/10 bg-zinc-950/70 p-6 backdrop-blur-md">
        <div className="mb-4 flex items-center gap-2">
          <QrCode className="h-4 w-4 text-amber-400" />
          <h2 className="text-base font-semibold text-white">Quick Connect 授权新设备</h2>
        </div>
        <p className="mb-4 text-xs text-zinc-400">
          在电视、移动端或其他浏览器登录界面点击「Quick Connect 授权码登录」，将屏幕上显示的 6
          位授权码输入下方即可免密授权登录。
        </p>

        <form onSubmit={handleQuickConnectSubmit} className="space-y-3.5">
          <div className="flex flex-col gap-3 sm:flex-row sm:items-end">
            <label className="block flex-1 space-y-1.5">
              <span className="text-xs font-medium text-zinc-300">6 位设备授权码</span>
              <input
                type="text"
                inputMode="numeric"
                maxLength={12}
                value={qcCode}
                onChange={(e) => {
                  setQcCode(e.target.value)
                  if (qcFeedback) setQcFeedback(null)
                }}
                placeholder="例如：123456"
                className="w-full rounded-xl border border-white/10 bg-white/[0.04] px-3.5 py-2.5 font-mono text-sm tracking-[0.25em] text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/60"
              />
            </label>

            <button
              type="submit"
              disabled={quickConnectMutation.isPending}
              className="inline-flex items-center justify-center gap-2 rounded-xl bg-amber-500 px-5 py-2.5 text-sm font-semibold text-zinc-950 transition-colors hover:bg-amber-400 disabled:opacity-50 sm:shrink-0"
            >
              {quickConnectMutation.isPending ? (
                <Loader2 className="h-4 w-4 animate-spin" />
              ) : (
                <CheckCircle2 className="h-4 w-4" />
              )}
              <span>确认授权</span>
            </button>
          </div>

          {qcFeedback && (
            <div
              role="status"
              className={`flex items-start gap-2 rounded-xl border px-3.5 py-2.5 text-xs ${
                qcFeedback.type === 'success'
                  ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-300'
                  : 'border-red-500/30 bg-red-500/10 text-red-300'
              }`}
            >
              {qcFeedback.type === 'success' ? (
                <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0" />
              ) : (
                <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" />
              )}
              <span>{qcFeedback.message}</span>
            </div>
          )}
        </form>
      </section>
    </div>
  )
}
