import { AlertTriangle, ArrowLeft, RefreshCw } from 'lucide-react'
import { Link } from 'react-router-dom'
import { getActiveLocale, translateForLocale, useI18n } from '../i18n/I18nContext'

export interface QueryErrorStateProps {
  title?: string
  error?: unknown
  message?: string
  onRetry?: () => void
  backTo?: string
  backLabel?: string
  compact?: boolean
}

export function extractErrorMessage(
  error: unknown,
  fallback = translateForLocale(getActiveLocale(), 'queryError.defaultMessage'),
): string {
  if (error instanceof Error && error.message.trim()) {
    return error.message
  }
  if (typeof error === 'string' && error.trim()) {
    return error
  }
  return fallback
}

export function QueryErrorState({
  title,
  error,
  message,
  onRetry,
  backTo,
  backLabel,
  compact = false,
}: QueryErrorStateProps) {
  const { t } = useI18n()
  const resolvedTitle = title ?? t('queryError.defaultTitle')
  const resolvedBackLabel = backLabel ?? t('queryError.backHome')
  const detail = message || extractErrorMessage(error, t('queryError.defaultMessage'))

  return (
    <div
      role="alert"
      data-testid="query-error-state"
      className={
        compact
          ? 'rounded-2xl border border-red-500/25 bg-red-950/20 px-5 py-8 text-center'
          : 'flex min-h-[42vh] items-center justify-center px-4 py-8'
      }
    >
      <div
        className={
          compact
            ? 'mx-auto max-w-md space-y-3'
            : 'w-full max-w-md rounded-2xl border border-white/10 bg-zinc-950/90 p-8 text-center shadow-2xl'
        }
      >
        <div
          className={`mx-auto flex items-center justify-center rounded-2xl bg-red-500/15 text-red-400 ring-1 ring-red-500/30 ${
            compact ? 'h-10 w-10' : 'mb-4 h-12 w-12'
          }`}
        >
          <AlertTriangle className={compact ? 'h-5 w-5' : 'h-6 w-6'} />
        </div>
        <h2 className={compact ? 'text-sm font-semibold text-white' : 'text-lg font-semibold text-white'}>
          {resolvedTitle}
        </h2>
        <p className={compact ? 'text-xs text-zinc-400' : 'mt-2 text-sm text-zinc-400'}>
          {detail}
        </p>
        {(onRetry || backTo) && (
          <div className={`flex flex-wrap items-center justify-center gap-3 ${compact ? 'pt-2' : 'mt-6'}`}>
            {onRetry && (
              <button
                type="button"
                onClick={onRetry}
                className="inline-flex items-center gap-1.5 rounded-xl bg-amber-500 px-5 py-2.5 text-xs font-semibold text-zinc-950 transition-colors hover:bg-amber-400"
              >
                <RefreshCw className="h-3.5 w-3.5" />
                <span>{t('queryError.retry')}</span>
              </button>
            )}
            {backTo && (
              <Link
                to={backTo}
                className="inline-flex items-center gap-1.5 rounded-xl border border-white/12 bg-white/[0.05] px-5 py-2.5 text-xs font-medium text-zinc-200 transition-colors hover:bg-white/10"
              >
                <ArrowLeft className="h-3.5 w-3.5" />
                <span>{resolvedBackLabel}</span>
              </Link>
            )}
          </div>
        )}
      </div>
    </div>
  )
}
