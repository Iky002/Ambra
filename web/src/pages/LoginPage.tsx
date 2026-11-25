import { useCallback, useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  AlertCircle,
  ArrowRight,
  CheckCircle2,
  KeyRound,
  Loader2,
  Lock,
  QrCode,
  RefreshCw,
  Server,
  ShieldCheck,
  Sparkles,
  User,
  UserCheck,
  X,
} from 'lucide-react'
import { Navigate, useNavigate } from 'react-router-dom'
import { api, embyFetch, getUserImageUrl } from '../api/client'
import type { QuickConnectResult } from '../api/types'
import { AmbraLogo } from '../components/AmbraLogo'
import { useAuth } from '../context/AuthContext'
import { isDemoMode } from '../demo/demoMode'

export function LoginPage() {
  const { login, applyAuthResult } = useAuth()
  const navigate = useNavigate()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')
  const [resetSuccessMessage, setResetSuccessMessage] = useState('')

  // 忘记密码 / PIN 重置两步弹窗状态
  const [forgotOpen, setForgotOpen] = useState(false)
  const [forgotStep, setForgotStep] = useState<1 | 2>(1)
  const [forgotUsername, setForgotUsername] = useState('')
  const [forgotPinFile, setForgotPinFile] = useState('')
  const [forgotPinExpiration, setForgotPinExpiration] = useState('')
  const [forgotPin, setForgotPin] = useState('')
  const [forgotLoading, setForgotLoading] = useState(false)
  const [forgotError, setForgotError] = useState('')

  // Quick Connect 授权码登录弹窗状态
  const [qcOpen, setQcOpen] = useState(false)
  const [qcLoading, setQcLoading] = useState(false)
  const [qcAuthenticating, setQcAuthenticating] = useState(false)
  const [qcResult, setQcResult] = useState<QuickConnectResult | null>(null)
  const [qcError, setQcError] = useState('')
  const qcPollTimerRef = useRef<number | null>(null)
  const qcAuthenticatingRef = useRef(false)

  const demoActive = isDemoMode()

  const clearQcPollTimer = useCallback(() => {
    if (qcPollTimerRef.current !== null) {
      window.clearInterval(qcPollTimerRef.current)
      qcPollTimerRef.current = null
    }
  }, [])

  const closeQcModal = useCallback(() => {
    clearQcPollTimer()
    qcAuthenticatingRef.current = false
    setQcOpen(false)
    setQcLoading(false)
    setQcAuthenticating(false)
    setQcResult(null)
    setQcError('')
  }, [clearQcPollTimer])

  const completeQcLogin = useCallback(
    async (secret: string) => {
      if (qcAuthenticatingRef.current) return
      qcAuthenticatingRef.current = true
      clearQcPollTimer()
      setQcAuthenticating(true)
      setQcError('')
      try {
        const authRes = await api.authenticateWithQuickConnect(secret)
        applyAuthResult(authRes)
        setQcOpen(false)
        navigate('/', { replace: true })
      } catch (err) {
        qcAuthenticatingRef.current = false
        setQcAuthenticating(false)
        setQcError(err instanceof Error ? err.message : 'Quick Connect 登录失败，请重新获取授权码')
      }
    },
    [applyAuthResult, clearQcPollTimer, navigate],
  )

  const startQuickConnect = useCallback(async () => {
    clearQcPollTimer()
    qcAuthenticatingRef.current = false
    setQcOpen(true)
    setQcLoading(true)
    setQcAuthenticating(false)
    setQcError('')
    setQcResult(null)
    try {
      const res = await api.quickConnectInitiate()
      setQcResult(res)
    } catch (err) {
      setQcError(err instanceof Error ? err.message : '获取 Quick Connect 授权码失败，请稍后重试')
    } finally {
      setQcLoading(false)
    }
  }, [clearQcPollTimer])

  useEffect(() => {
    if (!qcOpen || !qcResult?.Secret || qcResult.Authenticated) {
      clearQcPollTimer()
      return
    }
    const secret = qcResult.Secret
    const timerId = window.setInterval(() => {
      if (qcAuthenticatingRef.current) return
      void api
        .quickConnectConnect(secret)
        .then((status) => {
          if (status?.Authenticated) {
            setQcResult(status)
            void completeQcLogin(secret)
          }
        })
        .catch(() => {
          // 轮询期间忽略瞬时网络错误
        })
    }, 2000)
    qcPollTimerRef.current = timerId
    return () => {
      window.clearInterval(timerId)
      if (qcPollTimerRef.current === timerId) {
        qcPollTimerRef.current = null
      }
    }
  }, [qcOpen, qcResult?.Secret, qcResult?.Authenticated, clearQcPollTimer, completeQcLogin])

  useEffect(() => {
    return () => {
      clearQcPollTimer()
    }
  }, [clearQcPollTimer])

  useEffect(() => {
    if (!qcOpen) return
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !qcAuthenticating) {
        closeQcModal()
      }
    }
    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [qcOpen, qcAuthenticating, closeQcModal])

  const handleDemoApproveQuickConnect = async () => {
    if (!qcResult?.Secret || qcAuthenticatingRef.current) return
    try {
      const approved = await embyFetch<QuickConnectResult>(
        `/emby/QuickConnect/Connect?secret=${encodeURIComponent(qcResult.Secret)}&demoApprove=1`,
        {},
        false,
      )
      if (approved?.Authenticated) {
        setQcResult(approved)
        await completeQcLogin(qcResult.Secret)
      }
    } catch (err) {
      setQcError(err instanceof Error ? err.message : 'Demo 模拟授权失败')
    }
  }

  const openForgotModal = () => {
    setForgotUsername(username.trim())
    setForgotPinFile('')
    setForgotPinExpiration('')
    setForgotPin('')
    setForgotError('')
    setForgotStep(1)
    setForgotOpen(true)
  }

  const closeForgotModal = () => {
    setForgotOpen(false)
    setForgotError('')
    setForgotLoading(false)
  }

  useEffect(() => {
    if (!forgotOpen) return
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !forgotLoading) {
        closeForgotModal()
      }
    }
    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [forgotOpen, forgotLoading])

  const formatPinExpiration = (raw: string): string => {
    const parsed = new Date(raw)
    if (Number.isNaN(parsed.getTime())) return raw
    return parsed.toLocaleString('zh-CN')
  }

  const handleForgotStep1 = async (e: React.FormEvent) => {
    e.preventDefault()
    const targetUser = forgotUsername.trim()
    if (!targetUser) {
      setForgotError('请输入需要重置密码的用户名')
      return
    }
    setForgotError('')
    setForgotLoading(true)
    try {
      const res = await api.forgotPassword(targetUser)
      setForgotPinFile(res?.PinFile || '')
      setForgotPinExpiration(res?.PinExpirationDate || '')
      setForgotStep(2)
    } catch (err) {
      setForgotError(err instanceof Error ? err.message : '发起密码重置失败，请稍后重试')
    } finally {
      setForgotLoading(false)
    }
  }

  const handleForgotStep2 = async (e: React.FormEvent) => {
    e.preventDefault()
    const trimmedPin = forgotPin.trim()
    if (!trimmedPin) {
      setForgotError('请输入 6 位重置 PIN 码')
      return
    }
    setForgotError('')
    setForgotLoading(true)
    try {
      const res = await api.forgotPasswordPin(trimmedPin)
      if (!res?.Success) {
        setForgotError('PIN 码无效或已过期，请重新检查后输入')
        return
      }
      if (forgotUsername.trim()) {
        setUsername(forgotUsername.trim())
      }
      setPassword('')
      setError('')
      setResetSuccessMessage('密码已重置，请使用空密码或新密码登录')
      closeForgotModal()
    } catch (err) {
      setForgotError(err instanceof Error ? err.message : '验证 PIN 码失败，请检查后重试')
    } finally {
      setForgotLoading(false)
    }
  }

  const handleDemoQuickLogin = async (role: 'admin' | 'guest') => {
    setError('')
    setSubmitting(true)
    try {
      await login(role, '')
      navigate('/', { replace: true })
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Demo 一键登录失败')
    } finally {
      setSubmitting(false)
    }
  }

  const { data: setupStatus } = useQuery({
    queryKey: ['setupStatus'],
    queryFn: () => api.getSetupStatus(),
    enabled: !demoActive,
    retry: false,
  })

  const { data: sysInfo } = useQuery({
    queryKey: ['publicSystemInfo'],
    queryFn: () => api.getPublicSystemInfo(),
    retry: 1,
  })

  const { data: publicUsers } = useQuery({
    queryKey: ['publicUsers'],
    queryFn: () => api.getPublicUsers(),
    retry: 1,
  })

  const { data: quickConnectEnabled } = useQuery({
    queryKey: ['quickConnectEnabled'],
    queryFn: async () => {
      try {
        return await api.getQuickConnectEnabled()
      } catch {
        return false
      }
    },
    retry: false,
  })

  const showQuickConnect = Boolean(quickConnectEnabled || demoActive)

  if (!demoActive && setupStatus && setupStatus.completed === false) {
    return <Navigate to="/setup" replace />
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!username.trim()) {
      setError('请输入用户名')
      return
    }
    setError('')
    setSubmitting(true)
    try {
      await login(username.trim(), password)
      navigate('/', { replace: true })
    } catch (err) {
      setError(err instanceof Error ? err.message : '登录失败，请检查用户名或密码')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="relative flex min-h-screen items-center justify-center overflow-hidden bg-[#07070b] px-4 py-10">
      {/* 影院氛围背景光晕 */}
      <div className="pointer-events-none absolute -left-32 -top-32 h-96 w-96 rounded-full bg-amber-500/15 blur-[130px]" />
      <div className="pointer-events-none absolute -bottom-32 -right-32 h-96 w-96 rounded-full bg-orange-600/15 blur-[130px]" />

      <div className="relative z-10 w-full max-w-md">
        {/* Logo 与服务器名称 */}
        <div className="mb-8 text-center">
          <div className="mb-4 flex justify-center">
            <AmbraLogo size={60} />
          </div>
          <h1 className="text-2xl font-bold tracking-tight text-zinc-50">
            {sysInfo?.ServerName || 'Ambra Media Server'}
          </h1>
          <p className="mt-1.5 flex items-center justify-center gap-1.5 text-xs text-zinc-400">
            <Server className="h-3.5 w-3.5 text-amber-400/80" />
            <span>
              {sysInfo?.ProductName || 'Ambra'}{' '}
              {sysInfo?.AmbraVersion || (sysInfo?.Version ? `v${sysInfo.Version}` : 'v0.9.3')}
            </span>
          </p>
        </div>

        {/* 公开用户快速选择 */}
        {publicUsers && publicUsers.length > 0 && (
          <div className="mb-6">
            <p className="mb-2.5 text-center text-xs font-medium text-zinc-400">选择账号快速登录</p>
            <div className="flex flex-wrap justify-center gap-3">
              {publicUsers.map((u) => {
                const selected = username === u.Name
                return (
                  <button
                    key={u.Id}
                    type="button"
                    onClick={() => {
                      setUsername(u.Name)
                      setError('')
                    }}
                    className={`group flex flex-col items-center gap-1.5 rounded-2xl px-3 py-2.5 transition-all ${
                      selected
                        ? 'bg-amber-500/15 ring-1 ring-amber-400/60'
                        : 'bg-white/[0.03] ring-1 ring-white/8 hover:bg-white/[0.07]'
                    }`}
                  >
                    <div className="flex h-12 w-12 items-center justify-center overflow-hidden rounded-full bg-gradient-to-br from-zinc-800 to-zinc-900 text-base font-bold text-amber-300 ring-2 ring-white/10">
                      {u.PrimaryImageTag ? (
                        <img
                          src={getUserImageUrl(u.Id, u.PrimaryImageTag)}
                          alt={u.Name}
                          className="h-full w-full object-cover"
                        />
                      ) : (
                        u.Name.slice(0, 1).toUpperCase()
                      )}
                    </div>
                    <span className="max-w-[76px] truncate text-xs font-medium text-zinc-200">
                      {u.Name}
                    </span>
                  </button>
                )
              })}
            </div>
          </div>
        )}

        {/* 登录表单卡片 */}
        <form
          onSubmit={handleSubmit}
          className="rounded-2xl border border-white/10 bg-zinc-950/80 p-6 shadow-2xl backdrop-blur-xl sm:p-7"
        >
          <div className="space-y-4">
            <div>
              <label className="mb-1.5 block text-xs font-medium text-zinc-400">用户名</label>
              <div className="relative">
                <User className="pointer-events-none absolute left-3.5 top-1/2 h-4 w-4 -translate-y-1/2 text-zinc-500" />
                <input
                  type="text"
                  autoComplete="username"
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  placeholder="输入用户名"
                  className="w-full rounded-xl border border-white/10 bg-white/[0.04] py-2.5 pl-10 pr-4 text-sm text-zinc-100 placeholder-zinc-500 outline-none transition-colors focus:border-amber-400/60 focus:bg-white/[0.06]"
                />
              </div>
            </div>

            <div>
              <label className="mb-1.5 block text-xs font-medium text-zinc-400">密码</label>
              <div className="relative">
                <Lock className="pointer-events-none absolute left-3.5 top-1/2 h-4 w-4 -translate-y-1/2 text-zinc-500" />
                <input
                  type="password"
                  autoComplete="current-password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  placeholder="留空表示无密码账号"
                  className="w-full rounded-xl border border-white/10 bg-white/[0.04] py-2.5 pl-10 pr-4 text-sm text-zinc-100 placeholder-zinc-500 outline-none transition-colors focus:border-amber-400/60 focus:bg-white/[0.06]"
                />
              </div>
            </div>

            {resetSuccessMessage && (
              <div className="flex items-start gap-2 rounded-xl border border-emerald-500/30 bg-emerald-500/10 px-3.5 py-2.5 text-xs text-emerald-300">
                <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0" />
                <span>{resetSuccessMessage}</span>
              </div>
            )}

            {error && (
              <div className="flex items-start gap-2 rounded-xl border border-red-500/30 bg-red-500/10 px-3.5 py-2.5 text-xs text-red-300">
                <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" />
                <span>{error}</span>
              </div>
            )}

            <button
              type="submit"
              disabled={submitting}
              className="mt-2 flex w-full items-center justify-center gap-2 rounded-xl bg-gradient-to-r from-amber-500 to-orange-500 py-2.5 text-sm font-semibold text-zinc-950 shadow-lg shadow-amber-500/25 transition-all hover:from-amber-400 hover:to-orange-400 disabled:opacity-60"
            >
              {submitting ? (
                <>
                  <Loader2 className="h-4 w-4 animate-spin" />
                  <span>正在连接影院...</span>
                </>
              ) : (
                <>
                  <span>登录 Ambra</span>
                  <ArrowRight className="h-4 w-4" />
                </>
              )}
            </button>
          </div>
        </form>

        {/* Quick Connect 授权码登录入口 */}
        {showQuickConnect && (
          <div className="mt-4">
            <button
              type="button"
              disabled={submitting}
              onClick={() => void startQuickConnect()}
              className="flex w-full items-center justify-center gap-2 rounded-xl border border-amber-400/30 bg-amber-500/10 py-2.5 text-sm font-semibold text-amber-200 transition-colors hover:border-amber-400/50 hover:bg-amber-500/20 disabled:opacity-60"
            >
              <QrCode className="h-4 w-4 text-amber-300" />
              <span>Quick Connect 授权码登录</span>
            </button>
          </div>
        )}

        {/* 忘记密码入口 */}
        <div className="mt-4 text-center">
          <button
            type="button"
            onClick={openForgotModal}
            className="inline-flex items-center gap-1.5 text-xs font-medium text-zinc-400 transition-colors hover:text-amber-300"
          >
            <KeyRound className="h-3.5 w-3.5" />
            <span>忘记密码？</span>
          </button>
        </div>

        {/* Serverless Demo 一键登录面板（仅 Demo 模式渲染） */}
        {demoActive && (
          <div className="mt-4 rounded-2xl border border-amber-500/25 bg-amber-500/[0.06] p-4 backdrop-blur-xl">
            <div className="mb-3 flex items-center gap-1.5 text-xs font-semibold text-amber-300">
              <Sparkles className="h-3.5 w-3.5" />
              <span>Serverless Demo 一键体验（无需密码）</span>
            </div>
            <div className="grid grid-cols-2 gap-2.5">
              <button
                type="button"
                disabled={submitting}
                onClick={() => void handleDemoQuickLogin('admin')}
                className="inline-flex items-center justify-center gap-1.5 rounded-xl bg-amber-500/20 px-3 py-2 text-xs font-semibold text-amber-200 ring-1 ring-amber-400/35 transition-colors hover:bg-amber-500/30 disabled:opacity-50"
              >
                <ShieldCheck className="h-3.5 w-3.5" />
                <span>管理员 (admin)</span>
              </button>
              <button
                type="button"
                disabled={submitting}
                onClick={() => void handleDemoQuickLogin('guest')}
                className="inline-flex items-center justify-center gap-1.5 rounded-xl bg-white/[0.06] px-3 py-2 text-xs font-medium text-zinc-200 ring-1 ring-white/12 transition-colors hover:bg-white/12 disabled:opacity-50"
              >
                <UserCheck className="h-3.5 w-3.5" />
                <span>普通用户 (guest)</span>
              </button>
            </div>
          </div>
        )}
      </div>

      {/* 忘记密码两步重置弹窗 */}
      {forgotOpen && (
        <div
          role="dialog"
          aria-modal="true"
          aria-labelledby="forgot-password-title"
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/75 p-4 backdrop-blur-sm"
          onClick={() => {
            if (!forgotLoading) closeForgotModal()
          }}
        >
          <div
            onClick={(e) => e.stopPropagation()}
            className="w-full max-w-md rounded-2xl border border-white/10 bg-zinc-950 p-6 shadow-2xl"
          >
            <div className="mb-4 flex items-start justify-between gap-3">
              <div>
                <h2 id="forgot-password-title" className="text-base font-semibold text-zinc-50">
                  找回密码（第 {forgotStep} / 2 步）
                </h2>
                <p className="mt-1 text-xs text-zinc-400">
                  {forgotStep === 1
                    ? '输入用户名后，服务器将在数据目录生成一次性重置 PIN 文件'
                    : '请在服务器数据目录查看重置 PIN 文件，并在下方输入 6 位 PIN 码'}
                </p>
              </div>
              <button
                type="button"
                aria-label="关闭找回密码弹窗"
                disabled={forgotLoading}
                onClick={() => {
                  if (!forgotLoading) closeForgotModal()
                }}
                className="rounded-lg p-1 text-zinc-400 transition-colors hover:bg-white/10 hover:text-zinc-100 disabled:opacity-50"
              >
                <X className="h-4 w-4" />
              </button>
            </div>

            {forgotError && (
              <div className="mb-4 flex items-start gap-2 rounded-xl border border-red-500/30 bg-red-500/10 px-3.5 py-2.5 text-xs text-red-300">
                <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" />
                <span>{forgotError}</span>
              </div>
            )}

            {forgotStep === 1 ? (
              <form onSubmit={handleForgotStep1} className="space-y-4">
                <div>
                  <label
                    htmlFor="forgot-username-input"
                    className="mb-1.5 block text-xs font-medium text-zinc-400"
                  >
                    用户名
                  </label>
                  <input
                    id="forgot-username-input"
                    type="text"
                    value={forgotUsername}
                    onChange={(e) => setForgotUsername(e.target.value)}
                    placeholder="输入需要重置密码的用户名"
                    autoFocus
                    className="w-full rounded-xl border border-white/10 bg-white/[0.04] px-3.5 py-2.5 text-sm text-zinc-100 placeholder-zinc-500 outline-none transition-colors focus:border-amber-400/60"
                  />
                </div>
                <div className="flex items-center justify-end gap-2 pt-1">
                  <button
                    type="button"
                    onClick={closeForgotModal}
                    className="rounded-xl border border-white/10 bg-white/[0.04] px-4 py-2 text-xs font-medium text-zinc-300 hover:bg-white/10"
                  >
                    取消
                  </button>
                  <button
                    type="submit"
                    disabled={forgotLoading}
                    className="inline-flex items-center gap-1.5 rounded-xl bg-amber-500 px-4 py-2 text-xs font-semibold text-zinc-950 transition-colors hover:bg-amber-400 disabled:opacity-60"
                  >
                    {forgotLoading && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
                    <span>下一步</span>
                  </button>
                </div>
              </form>
            ) : (
              <form onSubmit={handleForgotStep2} className="space-y-4">
                <div className="space-y-1.5 rounded-xl border border-amber-500/25 bg-amber-500/[0.07] p-3.5 text-xs text-amber-200">
                  <p className="font-medium">请在服务器数据目录查看重置 PIN 文件</p>
                  {forgotPinFile && (
                    <code className="block break-all rounded-lg bg-black/40 px-2.5 py-1.5 font-mono text-[11px] text-amber-300">
                      {forgotPinFile}
                    </code>
                  )}
                  {forgotPinExpiration && (
                    <p className="text-[11px] text-amber-200/75">
                      有效期至：{formatPinExpiration(forgotPinExpiration)}
                    </p>
                  )}
                </div>

                <div>
                  <label
                    htmlFor="forgot-pin-input"
                    className="mb-1.5 block text-xs font-medium text-zinc-400"
                  >
                    6 位重置 PIN 码
                  </label>
                  <input
                    id="forgot-pin-input"
                    type="text"
                    inputMode="numeric"
                    maxLength={12}
                    value={forgotPin}
                    onChange={(e) => setForgotPin(e.target.value)}
                    placeholder="输入 6 位 PIN 码"
                    autoFocus
                    className="w-full rounded-xl border border-white/10 bg-white/[0.04] px-3.5 py-2.5 font-mono text-sm tracking-widest text-zinc-100 placeholder-zinc-500 outline-none transition-colors focus:border-amber-400/60"
                  />
                </div>

                <div className="flex items-center justify-between gap-2 pt-1">
                  <button
                    type="button"
                    onClick={() => {
                      setForgotStep(1)
                      setForgotError('')
                    }}
                    className="rounded-xl border border-white/10 bg-white/[0.04] px-3.5 py-2 text-xs font-medium text-zinc-300 hover:bg-white/10"
                  >
                    返回上一步
                  </button>
                  <button
                    type="submit"
                    disabled={forgotLoading}
                    className="inline-flex items-center gap-1.5 rounded-xl bg-amber-500 px-4 py-2 text-xs font-semibold text-zinc-950 transition-colors hover:bg-amber-400 disabled:opacity-60"
                  >
                    {forgotLoading && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
                    <span>验证并重置密码</span>
                  </button>
                </div>
              </form>
            )}
          </div>
        </div>
      )}

      {/* Quick Connect 授权码登录弹窗 */}
      {qcOpen && (
        <div
          role="dialog"
          aria-modal="true"
          aria-labelledby="quick-connect-title"
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/75 p-4 backdrop-blur-sm"
          onClick={() => {
            if (!qcAuthenticating) closeQcModal()
          }}
        >
          <div
            onClick={(e) => e.stopPropagation()}
            className="w-full max-w-md rounded-2xl border border-white/10 bg-zinc-950 p-6 shadow-2xl"
          >
            <div className="mb-4 flex items-start justify-between gap-3">
              <div>
                <h2
                  id="quick-connect-title"
                  className="flex items-center gap-2 text-base font-semibold text-zinc-50"
                >
                  <QrCode className="h-4 w-4 text-amber-400" />
                  <span>Quick Connect 授权码登录</span>
                </h2>
                <p className="mt-1 text-xs text-zinc-400">
                  在已登录的 Ambra 网页端「个人中心 → Quick Connect」输入此授权码
                </p>
              </div>
              <button
                type="button"
                aria-label="关闭 Quick Connect 弹窗"
                disabled={qcAuthenticating}
                onClick={() => {
                  if (!qcAuthenticating) closeQcModal()
                }}
                className="rounded-lg p-1 text-zinc-400 transition-colors hover:bg-white/10 hover:text-zinc-100 disabled:opacity-50"
              >
                <X className="h-4 w-4" />
              </button>
            </div>

            {qcError && (
              <div className="mb-4 flex items-start gap-2 rounded-xl border border-red-500/30 bg-red-500/10 px-3.5 py-2.5 text-xs text-red-300">
                <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" />
                <span>{qcError}</span>
              </div>
            )}

            {qcLoading ? (
              <div className="flex flex-col items-center justify-center gap-3 py-8 text-xs text-zinc-400">
                <Loader2 className="h-6 w-6 animate-spin text-amber-400" />
                <span>正在生成 6 位 Quick Connect 授权码...</span>
              </div>
            ) : qcResult ? (
              <div className="space-y-4">
                <div className="rounded-2xl border border-amber-400/25 bg-gradient-to-b from-amber-500/10 to-white/[0.02] px-4 py-6 text-center">
                  <p className="mb-2 text-[11px] font-medium uppercase tracking-widest text-amber-300/80">
                    一次性设备授权码
                  </p>
                  <div
                    data-testid="quick-connect-code"
                    className="select-all font-mono text-4xl font-bold tracking-[0.35em] text-zinc-50 pl-[0.35em]"
                  >
                    {qcResult.Code}
                  </div>
                </div>

                <div className="flex items-center justify-center gap-2 text-xs text-zinc-400">
                  {qcAuthenticating ? (
                    <>
                      <Loader2 className="h-3.5 w-3.5 animate-spin text-emerald-400" />
                      <span className="text-emerald-300">授权已确认，正在登录影院...</span>
                    </>
                  ) : (
                    <>
                      <Loader2 className="h-3.5 w-3.5 animate-spin text-amber-400" />
                      <span>等待已登录设备确认授权中（每 2 秒自动检查）...</span>
                    </>
                  )}
                </div>

                {demoActive && !qcAuthenticating && (
                  <button
                    type="button"
                    onClick={() => void handleDemoApproveQuickConnect()}
                    className="flex w-full items-center justify-center gap-1.5 rounded-xl border border-emerald-500/35 bg-emerald-500/15 px-4 py-2 text-xs font-semibold text-emerald-200 transition-colors hover:bg-emerald-500/25"
                  >
                    <CheckCircle2 className="h-3.5 w-3.5" />
                    <span>Demo 模拟立即授权登录</span>
                  </button>
                )}

                <div className="flex items-center justify-between gap-2 pt-1">
                  <button
                    type="button"
                    disabled={qcAuthenticating}
                    onClick={() => void startQuickConnect()}
                    className="inline-flex items-center gap-1.5 rounded-xl border border-white/10 bg-white/[0.04] px-3.5 py-2 text-xs font-medium text-zinc-300 hover:bg-white/10 disabled:opacity-50"
                  >
                    <RefreshCw className="h-3.5 w-3.5" />
                    <span>重新生成</span>
                  </button>
                  <button
                    type="button"
                    disabled={qcAuthenticating}
                    onClick={closeQcModal}
                    className="rounded-xl border border-white/10 bg-white/[0.04] px-4 py-2 text-xs font-medium text-zinc-300 hover:bg-white/10 disabled:opacity-50"
                  >
                    取消
                  </button>
                </div>
              </div>
            ) : (
              <div className="flex items-center justify-end gap-2 pt-2">
                <button
                  type="button"
                  onClick={closeQcModal}
                  className="rounded-xl border border-white/10 bg-white/[0.04] px-4 py-2 text-xs font-medium text-zinc-300 hover:bg-white/10"
                >
                  关闭
                </button>
                <button
                  type="button"
                  onClick={() => void startQuickConnect()}
                  className="inline-flex items-center gap-1.5 rounded-xl bg-amber-500 px-4 py-2 text-xs font-semibold text-zinc-950 transition-colors hover:bg-amber-400"
                >
                  <RefreshCw className="h-3.5 w-3.5" />
                  <span>重试</span>
                </button>
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  )
}
