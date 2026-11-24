import { useLayoutEffect } from 'react'
import { useLocation, useNavigationType } from 'react-router-dom'

const scrollPositions = new Map<string, number>()

function scrollWindowTo(top: number) {
  try {
    window.scrollTo({ top, left: 0, behavior: 'instant' as ScrollBehavior })
  } catch {
    window.scrollTo(0, top)
  }
  if (document.documentElement) {
    document.documentElement.scrollTop = top
  }
  if (document.body) {
    document.body.scrollTop = top
  }
}

/**
 * Resets window scroll position to top on forward/replace route transitions
 * (e.g. clicking a movie card on HomePage to open ItemDetailPage), while
 * preserving and restoring scroll position on browser Back/Forward (POP).
 */
export function ScrollToTop() {
  const location = useLocation()
  const navigationType = useNavigationType()

  useLayoutEffect(() => {
    const key = location.key || `${location.pathname}${location.search}`

    if (navigationType === 'POP' && scrollPositions.has(key)) {
      const savedTop = scrollPositions.get(key) ?? 0
      scrollWindowTo(savedTop)
    } else {
      scrollWindowTo(0)
    }

    return () => {
      const currentY =
        window.scrollY ||
        document.documentElement?.scrollTop ||
        document.body?.scrollTop ||
        0
      scrollPositions.set(key, currentY)
    }
  }, [location.key, location.pathname, location.search, navigationType])

  return null
}
