import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api, saveAuth } from '../../api/client'
import type { UserTraktStatus } from '../../api/types'
import { setDemoModeOverride } from '../../demo/demoMode'
import { resetDemoStore } from '../../demo/mockServer'
import { I18nProvider } from '../../i18n/I18nContext'
import { TraktSettings } from '../TraktSettings'

const empty: UserTraktStatus = {
  Enabled: true, Configured: false, ClientId: '', AccessTokenConfigured: false,
  ScrobblePlayback: true, SyncWatched: true,
}
const bound: UserTraktStatus = {
  ...empty, Configured: true, ClientId: 'alice-app', AccessTokenConfigured: true,
}
function mount(userId = 'alice') {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return {
    client,
    ...render(<I18nProvider><QueryClientProvider client={client}>
      <TraktSettings key={userId} userId={userId} />
    </QueryClientProvider></I18nProvider>),
  }
}

describe('personal Trakt settings', () => {
  beforeEach(() => {
    localStorage.clear()
    localStorage.setItem('ambra_locale_v1', 'zh-CN')
    setDemoModeOverride(false)
  })
  afterEach(() => {
    vi.restoreAllMocks()
    setDemoModeOverride(null)
    localStorage.clear()
  })
  it('binds personal credentials and never displays the saved token', async () => {
    vi.spyOn(api, 'getUserTrakt').mockResolvedValue(empty)
    const save = vi.spyOn(api, 'updateUserTrakt').mockResolvedValue(bound)
    mount()
    fireEvent.change(await screen.findByLabelText('Client ID'), { target: { value: 'alice-app' } })
    const token = screen.getByLabelText('Access Token') as HTMLInputElement
    expect(token.type).toBe('password')
    fireEvent.change(token, { target: { value: 'alice-secret' } })
    fireEvent.click(screen.getByLabelText('同步已看记录'))
    fireEvent.click(screen.getByRole('button', { name: '保存绑定与偏好' }))
    await waitFor(() => expect(save).toHaveBeenCalledWith({
      ClientId: 'alice-app', AccessToken: 'alice-secret', ScrobblePlayback: true, SyncWatched: false,
    }))
    await screen.findByText('Trakt 绑定与同步偏好已保存')
    expect(token.value).toBe('')
    expect(screen.queryByDisplayValue('alice-secret')).not.toBeInTheDocument()
  })
  it('preserves an existing token when saving only personal preferences', async () => {
    vi.spyOn(api, 'getUserTrakt').mockResolvedValue(bound)
    const save = vi.spyOn(api, 'updateUserTrakt').mockResolvedValue({ ...bound, ScrobblePlayback: false })
    mount()
    await screen.findByDisplayValue('alice-app')
    fireEvent.click(screen.getByLabelText('同步播放进度 (Scrobble)'))
    fireEvent.click(screen.getByRole('button', { name: '保存绑定与偏好' }))
    await waitFor(() => expect(save).toHaveBeenCalledWith({
      ClientId: 'alice-app', ScrobblePlayback: false, SyncWatched: true,
    }))
  })
  it('requires a new token when changing the OAuth Client ID', async () => {
    vi.spyOn(api, 'getUserTrakt').mockResolvedValue(bound)
    mount()
    fireEvent.change(await screen.findByLabelText('Client ID'), { target: { value: 'different-app' } })
    expect(screen.getByRole('button', { name: '保存绑定与偏好' })).toBeDisabled()
    fireEvent.change(screen.getByLabelText('Access Token'), { target: { value: 'new-token' } })
    expect(screen.getByRole('button', { name: '保存绑定与偏好' })).toBeEnabled()
  })
  it('honors the administrator switch while still allowing disconnect', async () => {
    vi.spyOn(api, 'getUserTrakt').mockResolvedValue({ ...bound, Enabled: false })
    const disconnect = vi.spyOn(api, 'disconnectUserTrakt').mockResolvedValue(undefined)
    mount()
    expect(await screen.findByLabelText('Client ID')).toBeDisabled()
    expect(screen.getByRole('button', { name: '保存绑定与偏好' })).toBeDisabled()
    fireEvent.click(screen.getByRole('button', { name: '解除绑定' }))
    await waitFor(() => expect(disconnect).toHaveBeenCalledOnce())
    await screen.findByText('已解除你的 Trakt 绑定')
    expect(screen.queryByDisplayValue('alice-app')).not.toBeInTheDocument()
  })
  it('allows retry after loading errors and displays save errors', async () => {
    vi.spyOn(api, 'getUserTrakt').mockRejectedValueOnce(new Error('offline')).mockResolvedValue(empty)
    vi.spyOn(api, 'updateUserTrakt').mockRejectedValue(new Error('save unavailable'))
    mount()
    fireEvent.click(await screen.findByRole('button', { name: /无法加载 Trakt 设置/ }))
    fireEvent.change(await screen.findByLabelText('Client ID'), { target: { value: 'app' } })
    fireEvent.change(screen.getByLabelText('Access Token'), { target: { value: 'token' } })
    fireEvent.click(screen.getByRole('button', { name: '保存绑定与偏好' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('save unavailable')
  })
  it('keeps cached account settings scoped to the signed-in user', async () => {
    vi.spyOn(api, 'getUserTrakt').mockResolvedValue(bound)
    const { client } = mount()
    await screen.findByDisplayValue('alice-app')
    expect(client.getQueryData(['user', 'alice', 'trakt'])).toEqual(bound)
    expect(client.getQueryData(['user', 'bob', 'trakt'])).toBeUndefined()
  })
  it('the demo follows the same multi-user binding and administrator availability rules', async () => {
    resetDemoStore()
    setDemoModeOverride(true)
    const signIn = (id: string) => saveAuth({
      accessToken: 'demo-token', userId: id, serverId: 'demo', userName: id, deviceId: 'test',
    })
    signIn('user-demo-admin')
    await api.updatePluginConfiguration('ambra.builtin.trakt', { enabled: true })
    await api.updateUserTrakt({ ClientId: 'admin-app', AccessToken: 'admin-token' })
    signIn('user-demo-guest')
    expect((await api.getUserTrakt()).Configured).toBe(false)
    await api.updateUserTrakt({ ClientId: 'guest-app', AccessToken: 'guest-token' })
    await expect(api.updatePluginConfiguration('ambra.builtin.trakt', { enabled: false })).rejects.toThrow()
    signIn('user-demo-admin')
    expect((await api.getUserTrakt()).ClientId).toBe('admin-app')
    await api.updatePluginConfiguration('ambra.builtin.trakt', { enabled: false })
    signIn('user-demo-guest')
    expect(await api.getUserTrakt()).toMatchObject({ ClientId: 'guest-app', Enabled: false })
    await expect(api.updateUserTrakt({ SyncWatched: false })).rejects.toThrow()
    await api.disconnectUserTrakt()
    expect((await api.getUserTrakt()).Configured).toBe(false)
    resetDemoStore()
  })
})
