import { useEffect, useRef, useState } from 'react'
import { Film, Loader2, Search, Tv, X } from 'lucide-react'
import { useNavigate } from 'react-router-dom'
import { api, getFallbackImageUrl, getImageUrl } from '../api/client'
import type { SearchHint } from '../api/types'
import { useAuth } from '../context/AuthContext'

interface SearchModalProps {
  open: boolean
  onClose: () => void
}

export function SearchModal({ open, onClose }: SearchModalProps) {
  const { auth } = useAuth()
  const navigate = useNavigate()
  const inputRef = useRef<HTMLInputElement>(null)
  const [query, setQuery] = useState('')
  const [hints, setHints] = useState<SearchHint[]>([])
  const [loading, setLoading] = useState(false)
  const [activeIndex, setActiveIndex] = useState(0)

  useEffect(() => {
    if (open) {
      setTimeout(() => inputRef.current?.focus(), 30)
    } else {
      setQuery('')
      setHints([])
      setActiveIndex(0)
    }
  }, [open])

  useEffect(() => {
    if (!open) return
    const trimmed = query.trim()
    if (!trimmed) {
      setHints([])
      setLoading(false)
      return
    }

    let cancelled = false
    setLoading(true)
    const timer = window.setTimeout(async () => {
      try {
        const res = await api.searchHints(trimmed, auth?.userId, 16)
        if (!cancelled) {
          setHints(res.SearchHints || [])
          setActiveIndex(0)
        }
      } catch {
        if (!cancelled) setHints([])
      } finally {
        if (!cancelled) setLoading(false)
      }
    }, 180)

    return () => {
      cancelled = true
      window.clearTimeout(timer)
    }
  }, [query, open, auth?.userId])

  if (!open) return null

  const handleSelect = (hint: SearchHint) => {
    const targetId = hint.ItemId || hint.Id
    onClose()
    if (hint.Type === 'Person') {
      navigate(`/person/${encodeURIComponent(targetId)}?name=${encodeURIComponent(hint.Name)}`)
      return
    }
    if (hint.Type === 'BoxSet' || hint.Type === 'Playlist') {
      navigate(`/collections/${encodeURIComponent(targetId)}`)
      return
    }
    navigate(`/item/${encodeURIComponent(targetId)}`)
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Escape') {
      onClose()
    } else if (e.key === 'ArrowDown') {
      e.preventDefault()
      setActiveIndex((prev) => Math.min(prev + 1, Math.max(0, hints.length - 1)))
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      setActiveIndex((prev) => Math.max(0, prev - 1))
    } else if (e.key === 'Enter') {
      e.preventDefault()
      if (hints[activeIndex]) {
        handleSelect(hints[activeIndex])
      } else if (query.trim()) {
        onClose()
        navigate(`/search?q=${encodeURIComponent(query.trim())}`)
      }
    }
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-start justify-center bg-black/70 px-4 pt-[12vh] backdrop-blur-md"
      onClick={onClose}
    >
      <div
        className="animate-modal-enter w-full max-w-2xl overflow-hidden rounded-2xl border border-white/12 bg-zinc-950/95 shadow-2xl shadow-black/80"
        onClick={(e) => e.stopPropagation()}
        onKeyDown={handleKeyDown}
      >
        <div className="flex items-center gap-3 border-b border-white/10 px-4 py-3.5">
          <Search className="h-5 w-5 text-amber-400" />
          <input
            ref={inputRef}
            type="text"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="搜索影片、剧集、演员（支持中英文乱序模糊匹配）..."
            className="w-full bg-transparent text-base text-zinc-100 placeholder-zinc-500 outline-none"
          />
          {loading && <Loader2 className="h-4 w-4 animate-spin text-amber-400" />}
          <button
            type="button"
            onClick={onClose}
            className="rounded-lg p-1 text-zinc-400 hover:bg-white/10 hover:text-zinc-100"
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        <div className="max-h-[60vh] overflow-y-auto p-2">
          {!query.trim() ? (
            <div className="px-4 py-10 text-center text-sm text-zinc-500">
              输入关键词即可即时检索整个媒体库，支持按 ↑ ↓ 选择，Enter 直达详情页
            </div>
          ) : hints.length === 0 && !loading ? (
            <div className="px-4 py-10 text-center text-sm text-zinc-500">
              未找到与 “{query}” 匹配的媒体
            </div>
          ) : (
            <div className="space-y-1">
              {hints.map((hint, idx) => {
                const id = hint.ItemId || hint.Id
                const img = hint.PrimaryImageTag
                  ? getImageUrl(id, 'Primary', { maxWidth: 120, tag: hint.PrimaryImageTag })
                  : ''
                const fallbackImg = hint.PrimaryImageTag
                  ? getFallbackImageUrl(id, 'Primary')
                  : null
                const isActive = idx === activeIndex
                return (
                  <button
                    key={id}
                    type="button"
                    onMouseEnter={() => setActiveIndex(idx)}
                    onClick={() => handleSelect(hint)}
                    className={`flex w-full items-center gap-3.5 rounded-xl px-3 py-2 text-left transition-colors ${
                      isActive ? 'bg-amber-500/15 text-zinc-50' : 'text-zinc-300 hover:bg-white/5'
                    }`}
                  >
                    <div className="flex h-14 w-10 shrink-0 items-center justify-center overflow-hidden rounded-lg bg-zinc-900 ring-1 ring-white/10">
                      {img ? (
                        <img
                          src={img}
                          alt={hint.Name}
                          onError={(e) => {
                            const el = e.currentTarget
                            if (
                              fallbackImg &&
                              !el.dataset.fallbackApplied &&
                              el.src !== fallbackImg
                            ) {
                              el.dataset.fallbackApplied = '1'
                              el.src = fallbackImg
                              return
                            }
                            el.style.display = 'none'
                          }}
                          className="h-full w-full object-cover"
                        />
                      ) : hint.Type === 'Series' || hint.Type === 'Episode' ? (
                        <Tv className="h-4 w-4 text-zinc-500" />
                      ) : (
                        <Film className="h-4 w-4 text-zinc-500" />
                      )}
                    </div>
                    <div className="min-w-0 flex-1">
                      <p className="truncate text-sm font-medium text-zinc-100">{hint.Name}</p>
                      <p className="mt-0.5 truncate text-xs text-zinc-400">
                        {[
                          hint.Type === 'Movie'
                            ? '电影'
                            : hint.Type === 'Series'
                              ? '剧集'
                              : hint.Type === 'Episode'
                                ? hint.Series || '剧集单集'
                                : hint.Type,
                          hint.ProductionYear,
                        ]
                          .filter(Boolean)
                          .join(' · ')}
                      </p>
                    </div>
                    <span className="rounded-md bg-white/5 px-2 py-0.5 text-[11px] text-zinc-400">
                      Enter
                    </span>
                  </button>
                )
              })}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
