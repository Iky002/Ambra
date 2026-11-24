import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Loader2, Search } from 'lucide-react'
import { useSearchParams } from 'react-router-dom'
import { api } from '../api/client'
import { MediaCard } from '../components/MediaCard'
import { useAuth } from '../context/AuthContext'

export function SearchPage() {
  const { auth } = useAuth()
  const [searchParams, setSearchParams] = useSearchParams()
  const [input, setInput] = useState(searchParams.get('q') || '')
  const [debounced, setDebounced] = useState(input)

  useEffect(() => {
    const t = window.setTimeout(() => {
      setDebounced(input.trim())
      if (input.trim()) {
        setSearchParams({ q: input.trim() }, { replace: true })
      } else {
        setSearchParams({}, { replace: true })
      }
    }, 250)
    return () => window.clearTimeout(t)
  }, [input, setSearchParams])

  const { data, isLoading } = useQuery({
    queryKey: ['searchItems', auth?.userId, debounced],
    queryFn: () =>
      api.getItems(auth!.userId, {
        searchTerm: debounced,
        includeItemTypes: 'Movie,Series,Episode',
        limit: 48,
      }),
    enabled: debounced.length > 0,
  })

  const items = data?.Items || []

  return (
    <div className="space-y-6 px-4 pt-6 lg:px-8">
      <div className="max-w-2xl">
        <h1 className="mb-4 text-2xl font-bold text-white">探索与搜索</h1>
        <div className="relative">
          <Search className="pointer-events-none absolute left-4 top-1/2 h-5 w-5 -translate-y-1/2 text-amber-400" />
          <input
            type="text"
            autoFocus
            value={input}
            onChange={(e) => setInput(e.target.value)}
            placeholder="输入片名、原名、拼音或乱序关键词..."
            className="w-full rounded-2xl border border-white/12 bg-white/[0.05] py-3.5 pl-12 pr-4 text-base text-zinc-100 placeholder-zinc-500 outline-none transition-all focus:border-amber-400/60 focus:bg-white/[0.08]"
          />
        </div>
      </div>

      {!debounced ? (
        <div className="rounded-2xl border border-white/6 bg-white/[0.02] py-16 text-center text-sm text-zinc-500">
          支持中英文及多关键词乱序搜索，随时按 <kbd className="rounded bg-white/10 px-1.5 py-0.5 text-zinc-300">⌘K</kbd> 快速唤起全局搜索
        </div>
      ) : isLoading ? (
        <div className="flex h-48 items-center justify-center">
          <Loader2 className="h-7 w-7 animate-spin text-amber-400" />
        </div>
      ) : items.length === 0 ? (
        <div className="rounded-2xl border border-white/6 bg-white/[0.02] py-16 text-center text-sm text-zinc-500">
          未找到与 “{debounced}” 相关的结果
        </div>
      ) : (
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
          {items.map((item) => (
            <MediaCard key={item.Id} item={item} />
          ))}
        </div>
      )}
    </div>
  )
}
