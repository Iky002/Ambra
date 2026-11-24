import { render, screen, fireEvent } from '@testing-library/react'
import { Link, MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ScrollToTop } from '../ScrollToTop'

describe('ScrollToTop', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    Object.defineProperty(window, 'scrollY', { value: 0, writable: true, configurable: true })
    window.scrollTo = vi.fn((arg1?: number | ScrollToOptions, arg2?: number) => {
      const top = typeof arg1 === 'object' && arg1 !== null ? (arg1.top ?? 0) : (arg2 ?? 0)
      Object.defineProperty(window, 'scrollY', { value: top, writable: true, configurable: true })
    }) as typeof window.scrollTo
  })

  it('scrolls to top when navigating from HomePage to ItemDetailPage', () => {
    render(
      <MemoryRouter initialEntries={['/']}>
        <ScrollToTop />
        <Routes>
          <Route path="/" element={<Link to="/item/movie-1">进入详情</Link>} />
          <Route path="/item/:id" element={<div>影片详情页</div>} />
        </Routes>
      </MemoryRouter>,
    )

    // Simulate user scrolling down on HomePage
    Object.defineProperty(window, 'scrollY', { value: 960, writable: true, configurable: true })
    document.documentElement.scrollTop = 960

    fireEvent.click(screen.getByText('进入详情'))

    expect(screen.getByText('影片详情页')).toBeInTheDocument()
    expect(window.scrollY).toBe(0)
    expect(document.documentElement.scrollTop).toBe(0)
  })
})
