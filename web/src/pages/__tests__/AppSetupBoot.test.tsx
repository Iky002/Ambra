import { render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import App, { queryClient } from '../../App'
import { api } from '../../api/client'
import type { SetupStatus } from '../../api/types'

describe('Task 1: App first-boot setupStatus loading gate and /setup redirect', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    queryClient.clear()
    vi.restoreAllMocks()
    window.location.hash = '#/'
  })

  it('waits for setupStatus instead of flashing /login, then routes to /setup when has_users is false', async () => {
    let resolveStatus!: (val: SetupStatus) => void
    const pending = new Promise<SetupStatus>((resolve) => {
      resolveStatus = resolve
    })
    vi.spyOn(api, 'getSetupStatus').mockReturnValue(pending)
    vi.spyOn(api, 'getPublicSystemInfo').mockResolvedValue({
      Id: 'srv-1',
      ServerName: 'Ambra',
      Version: '4.10.2.0',
    })
    vi.spyOn(api, 'getPublicUsers').mockResolvedValue([])

    render(<App />)

    // While setupStatus is loading, must NOT redirect to /login
    expect(window.location.hash).not.toContain('/login')

    resolveStatus({
      completed: false,
      server_name: 'Ambra',
      default_language: 'zh-CN',
      has_users: false,
      has_tmdb_key: false,
    })

    await waitFor(() => {
      expect(window.location.hash).toContain('/setup')
    })
    const matches = await screen.findAllByText(/服务器设置|管理员账号/)
    expect(matches.length).toBeGreaterThan(0)
  })
})
