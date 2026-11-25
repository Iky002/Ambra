import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { api, saveAuth } from '../../api/client'
import type { BaseItemDto, UserDto } from '../../api/types'
import { AudioPlayerProvider } from '../../context/AudioPlayerContext'
import { AuthProvider } from '../../context/AuthContext'
import { I18nProvider } from '../../i18n/I18nContext'
import { DEFAULT_PLAYBACK_PREFS, savePlaybackPrefs } from '../../player/playbackPrefs'
import { ItemDetailPage } from '../ItemDetailPage'
import { ProfilePage } from '../ProfilePage'

const mockNavigate = vi.fn()

vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom')
  return {
    ...actual,
    useNavigate: () => mockNavigate,
  }
})

function makeUser(overrides?: Partial<UserDto>): UserDto {
  return {
    Id: 'u-1',
    Name: 'alice',
    ServerId: 'srv-1',
    Policy: { IsAdministrator: false },
    Configuration: {
      PlayDefaultAudioTrack: true,
      AudioLanguagePreference: '',
      SubtitleLanguagePreference: 'auto',
      SubtitleMode: 'Default',
      EnableNextEpisodeAutoPlay: true,
      RememberAudioSelections: true,
      RememberSubtitleSelections: true,
    },
    ...overrides,
  }
}

function renderWithProviders(ui: React.ReactElement, initialEntries = ['/']) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <I18nProvider>
      <QueryClientProvider client={qc}>
        <AuthProvider>
          <AudioPlayerProvider>
            <MemoryRouter initialEntries={initialEntries}>{ui}</MemoryRouter>
          </AudioPlayerProvider>
        </AuthProvider>
      </QueryClientProvider>
    </I18nProvider>,
  )
}

describe('Task 2: ProfilePage UserConfiguration sync & ItemDetailPage track selection', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    vi.restoreAllMocks()
    mockNavigate.mockReset()
    saveAuth({
      accessToken: 'tok-1',
      userId: 'u-1',
      serverId: 'srv-1',
      userName: 'alice',
      isAdmin: false,
      deviceId: 'dev-1',
    })
  })

  it('ProfilePage syncs preferred audio/subtitle/mode to backend UserConfiguration', async () => {
    const user = makeUser({
      Configuration: {
        AudioLanguagePreference: 'eng',
        SubtitleLanguagePreference: 'chs',
        SubtitleMode: 'Smart',
        EnableNextEpisodeAutoPlay: true,
      },
    })
    vi.spyOn(api, 'getUser').mockResolvedValue(user)
    const updateCfgSpy = vi.spyOn(api, 'updateUserConfiguration').mockResolvedValue(undefined)

    renderWithProviders(<ProfilePage />, ['/profile'])

    const subSelect = await screen.findByLabelText('首选字幕语言')
    await waitFor(() => {
      expect((subSelect as HTMLSelectElement).value).toBe('chs')
    })

    fireEvent.change(subSelect, { target: { value: 'auto' } })
    await waitFor(() => {
      expect(updateCfgSpy).toHaveBeenCalledWith(
        'u-1',
        expect.objectContaining({
          SubtitleLanguagePreference: 'auto',
          AudioLanguagePreference: 'eng',
          SubtitleMode: 'Smart',
        }),
      )
    })
  })

  it('ItemDetailPage auto-selects Chinese subtitle on zh-CN and does not force ?subtitle=off when unmatched', async () => {
    vi.spyOn(api, 'getUser').mockResolvedValue(makeUser())
    const movie: BaseItemDto = {
      Id: 'm-1',
      Name: 'Test Movie',
      ServerId: 'srv-1',
      Type: 'Movie',
      MediaType: 'Video',
      IsFolder: false,
      MediaSources: [
        {
          Id: 'ms-1',
          Name: 'Main',
          Protocol: 'File',
          Container: 'mkv',
          IsRemote: false,
          SupportsDirectPlay: true,
          SupportsDirectStream: true,
          SupportsTranscoding: true,
          MediaStreams: [
            { Type: 'Video', Index: 0, Codec: 'h264', Width: 1920, Height: 1080 },
            { Type: 'Audio', Index: 1, Codec: 'aac', Language: 'eng', IsDefault: false },
            { Type: 'Audio', Index: 2, Codec: 'aac', Language: 'jpn', IsDefault: true },
            { Type: 'Subtitle', Index: 3, Codec: 'srt', Language: 'eng', IsDefault: true },
            { Type: 'Subtitle', Index: 4, Codec: 'ass', Language: 'chi', Title: '简体中文', SubVariant: 'chs' },
          ],
        },
      ],
    }
    vi.spyOn(api, 'getItem').mockResolvedValue(movie)
    vi.spyOn(api, 'getSimilarItems').mockResolvedValue({ Items: [], TotalRecordCount: 0 })
    vi.spyOn(api, 'getLocalTrailers').mockResolvedValue([])
    vi.spyOn(api, 'getSpecialFeatures').mockResolvedValue([])
    vi.spyOn(api, 'getThemeSongs').mockResolvedValue({ Items: [], TotalRecordCount: 0 })

    savePlaybackPrefs({ ...DEFAULT_PLAYBACK_PREFS })

    renderWithProviders(
      <Routes>
        <Route path="/item/:id" element={<ItemDetailPage />} />
      </Routes>,
      ['/item/m-1'],
    )

    const subSelect = (await screen.findByLabelText(/字幕/)) as HTMLSelectElement
    await waitFor(() => {
      // zh-CN auto selects chs subtitle (4)
      expect(subSelect.value).toBe('4')
    })

    // Play button should pass audio=2 (IsDefault) and subtitle=4
    const playBtn = screen.getByRole('button', { name: /立即播放|播放/ })
    fireEvent.click(playBtn)
    expect(mockNavigate).toHaveBeenCalledWith('/play/m-1?mediaSourceId=ms-1&audio=2&subtitle=4')

    // Explicitly switch subtitle to Off (-1) -> now play URL MUST include subtitle=off
    mockNavigate.mockReset()
    fireEvent.change(subSelect, { target: { value: '-1' } })
    fireEvent.click(playBtn)
    expect(mockNavigate).toHaveBeenCalledWith('/play/m-1?mediaSourceId=ms-1&audio=2&subtitle=off')
  })

  it('ItemDetailPage does NOT append ?subtitle=off when no subtitle matches and user did not disable', async () => {
    vi.spyOn(api, 'getUser').mockResolvedValue(
      makeUser({
        Configuration: {
          AudioLanguagePreference: '',
          SubtitleLanguagePreference: 'media',
          SubtitleMode: 'Default',
        },
      }),
    )
    const movie: BaseItemDto = {
      Id: 'm-2',
      Name: 'No Default Sub Movie',
      ServerId: 'srv-1',
      Type: 'Movie',
      MediaType: 'Video',
      IsFolder: false,
      MediaSources: [
        {
          Id: 'ms-2',
          Name: 'Main',
          Protocol: 'File',
          Container: 'mkv',
          IsRemote: false,
          SupportsDirectPlay: true,
          SupportsDirectStream: true,
          SupportsTranscoding: true,
          MediaStreams: [
            { Type: 'Video', Index: 0, Codec: 'h264' },
            { Type: 'Audio', Index: 1, Codec: 'aac', Language: 'eng', IsDefault: true },
            { Type: 'Subtitle', Index: 3, Codec: 'srt', Language: 'spa', IsDefault: false },
          ],
        },
      ],
    }
    vi.spyOn(api, 'getItem').mockResolvedValue(movie)
    vi.spyOn(api, 'getSimilarItems').mockResolvedValue({ Items: [], TotalRecordCount: 0 })
    vi.spyOn(api, 'getLocalTrailers').mockResolvedValue([])
    vi.spyOn(api, 'getSpecialFeatures').mockResolvedValue([])
    vi.spyOn(api, 'getThemeSongs').mockResolvedValue({ Items: [], TotalRecordCount: 0 })

    savePlaybackPrefs({
      autoPlayNextEpisode: true,
      preferredAudioLang: 'media',
      preferredSubtitleLang: 'media',
      subtitleMode: 'Default',
    })

    renderWithProviders(
      <Routes>
        <Route path="/item/:id" element={<ItemDetailPage />} />
      </Routes>,
      ['/item/m-2'],
    )

    const playBtn = await screen.findByRole('button', { name: /立即播放|播放/ })
    fireEvent.click(playBtn)
    expect(mockNavigate).toHaveBeenCalledWith('/play/m-2?mediaSourceId=ms-2&audio=1')
  })
})
