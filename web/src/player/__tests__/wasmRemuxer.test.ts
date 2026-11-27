import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const ffmpegMockHooks = vi.hoisted(() => ({
  load: null as ((...args: unknown[]) => Promise<boolean>) | null,
  terminate: vi.fn(),
}))

vi.mock('@ffmpeg/ffmpeg', () => ({
  FFmpeg: class MockFFmpeg {
    load = (...args: unknown[]) =>
      ffmpegMockHooks.load ? ffmpegMockHooks.load(...args) : Promise.resolve(true)
    terminate = () => {
      ffmpegMockHooks.terminate()
    }
    on = vi.fn()
    off = vi.fn()
    writeFile = vi.fn()
    exec = vi.fn()
    readFile = vi.fn()
    deleteFile = vi.fn()
  },
}))
import {
  canUseWasmRemux,
  computeSha256Hex,
  DEFAULT_WASM_MAX_BYTES,
  fetchVerifiedCoreBlobUrl,
  FFMPEG_CORE_SHA256,
  formatBytes,
  getPreferWasmRemux,
  needsClientRemux,
  probeLocalCoreAvailable,
  resetLocalCoreProbeCache,
  resolveLocalCoreBase,
  setPreferWasmRemux,
  WASM_REMUX_PREF_KEY,
  WasmRemuxer,
  WasmRemuxFallbackError,
} from '../wasmRemuxer'

describe('wasmRemuxer - 容器与音轨转封装判定', () => {
  it('非浏览器原生容器（mkv / matroska,webm / ts / m2ts / avi）需要客户端转封装', () => {
    expect(needsClientRemux('mkv', 'aac')).toBe(true)
    expect(needsClientRemux('matroska,webm', 'aac')).toBe(true)
    expect(needsClientRemux('ts', 'aac')).toBe(true)
    expect(needsClientRemux('m2ts', 'mp3')).toBe(true)
    expect(needsClientRemux('AVI', 'aac')).toBe(true)
  })

  it('非浏览器原生音轨（ac3 / eac3 / dts / truehd）即使在 mp4 中也需要客户端转封装', () => {
    expect(needsClientRemux('mp4', 'ac3')).toBe(true)
    expect(needsClientRemux('mp4', 'eac3')).toBe(true)
    expect(needsClientRemux('mp4', 'dts')).toBe(true)
    expect(needsClientRemux('mp4', 'TrueHD')).toBe(true)
  })

  it('浏览器原生兼容组合（mp4/aac, webm/opus, mov/mp3）无需客户端转封装', () => {
    expect(needsClientRemux('mp4', 'aac')).toBe(false)
    expect(needsClientRemux('m4v', 'aac')).toBe(false)
    expect(needsClientRemux('webm', 'opus')).toBe(false)
    expect(needsClientRemux('mov', 'mp3')).toBe(false)
    expect(needsClientRemux(undefined, undefined)).toBe(false)
  })
})

describe('wasmRemuxer - 浏览器能力探测与用户偏好持久化', () => {
  const originalWorker = globalThis.Worker
  const originalCreateObjectURL = URL.createObjectURL

  beforeEach(() => {
    localStorage.clear()
  })

  afterEach(() => {
    globalThis.Worker = originalWorker
    URL.createObjectURL = originalCreateObjectURL
    vi.restoreAllMocks()
  })

  it('canUseWasmRemux 正确响应开关选项与环境能力', () => {
    expect(canUseWasmRemux({ enabled: false })).toBe(false)

    // 模拟完整浏览器环境
    globalThis.Worker = class MockWorker {} as unknown as typeof Worker
    URL.createObjectURL = vi.fn(() => 'blob:mock-url')
    expect(canUseWasmRemux()).toBe(true)

    // 缺少 Worker 时返回 false
    // @ts-expect-error 测试缺少 Worker 的降级分支
    globalThis.Worker = undefined
    expect(canUseWasmRemux()).toBe(false)
  })

  it('getPreferWasmRemux / setPreferWasmRemux 正确读写 localStorage', () => {
    // 默认开启
    expect(getPreferWasmRemux()).toBe(true)

    setPreferWasmRemux(false)
    expect(localStorage.getItem(WASM_REMUX_PREF_KEY)).toBe('false')
    expect(getPreferWasmRemux()).toBe(false)

    setPreferWasmRemux(true)
    expect(localStorage.getItem(WASM_REMUX_PREF_KEY)).toBe('true')
    expect(getPreferWasmRemux()).toBe(true)
  })

  it('formatBytes 格式化人类可读体积', () => {
    expect(formatBytes(0)).toBe('0 B')
    expect(formatBytes(1024)).toBe('1.0 KB')
    expect(formatBytes(150 * 1024 * 1024)).toBe('150 MB')
  })
})

describe('wasmRemuxer - 超大文件 (>150MB) 降级保护', () => {
  const originalWorker = globalThis.Worker
  const originalCreateObjectURL = URL.createObjectURL

  beforeEach(() => {
    globalThis.Worker = class MockWorker {} as unknown as typeof Worker
    URL.createObjectURL = vi.fn(() => 'blob:mock-url')
  })

  afterEach(() => {
    globalThis.Worker = originalWorker
    URL.createObjectURL = originalCreateObjectURL
    vi.restoreAllMocks()
  })

  it('已知文件体积 knownSizeBytes > 150MB 时直接抛出 WasmRemuxFallbackError("size_exceeded") 且不发起拉流', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch')
    const remuxer = new WasmRemuxer()

    const oversizeBytes = DEFAULT_WASM_MAX_BYTES + 10 * 1024 * 1024 // 160 MB
    await expect(
      remuxer.remuxToMP4('/emby/Videos/1/stream.mkv', undefined, {
        knownSizeBytes: oversizeBytes,
        container: 'mkv',
      }),
    ).rejects.toSatisfy((err: unknown) => {
      return err instanceof WasmRemuxFallbackError && err.reason === 'size_exceeded'
    })

    expect(fetchSpy).not.toHaveBeenCalled()
    remuxer.dispose()
  })

  it('响应头 Content-Length 超过 150MB 时主动取消拉流并抛出 size_exceeded', async () => {
    const cancelSpy = vi.fn().mockResolvedValue(undefined)
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      status: 200,
      headers: new Headers({ 'content-length': String(200 * 1024 * 1024) }),
      body: { cancel: cancelSpy } as unknown as ReadableStream<Uint8Array>,
    } as Response)

    const remuxer = new WasmRemuxer()
    await expect(
      remuxer.remuxToMP4('/emby/Videos/2/stream.mkv', { 'X-Emby-Token': 'tok' }),
    ).rejects.toSatisfy((err: unknown) => {
      return err instanceof WasmRemuxFallbackError && err.reason === 'size_exceeded'
    })

    expect(cancelSpy).toHaveBeenCalledTimes(1)
    remuxer.dispose()
  })

  it('无 Content-Length 但流式读取累计超出阈值时中止并抛出 size_exceeded', async () => {
    const chunk = new Uint8Array(64)
    const cancelSpy = vi.fn().mockResolvedValue(undefined)
    let reads = 0
    const fakeStream = {
      getReader: () => ({
        read: vi.fn(async () => {
          reads++
          if (reads <= 3) return { done: false, value: chunk }
          return { done: true, value: undefined }
        }),
        cancel: cancelSpy,
      }),
    }

    vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      status: 200,
      headers: new Headers(),
      body: fakeStream as unknown as ReadableStream<Uint8Array>,
    } as Response)

    const remuxer = new WasmRemuxer()
    await expect(
      remuxer.remuxToMP4('/emby/Videos/3/stream.ts', undefined, { maxBytes: 100 }),
    ).rejects.toSatisfy((err: unknown) => {
      return err instanceof WasmRemuxFallbackError && err.reason === 'size_exceeded'
    })

    expect(cancelSpy).toHaveBeenCalledTimes(1)
    remuxer.dispose()
  })
})

describe('wasmRemuxer - H-07 完整性校验、HTML/404 快速失败与 VFS 泄漏防护', () => {
  const originalWorker = globalThis.Worker
  const originalCreateObjectURL = URL.createObjectURL
  const originalRevokeObjectURL = URL.revokeObjectURL

  beforeEach(() => {
    globalThis.Worker = class MockWorker {} as unknown as typeof Worker
    URL.createObjectURL = vi.fn(() => 'blob:verified-core-url')
    URL.revokeObjectURL = vi.fn()
  })

  afterEach(() => {
    globalThis.Worker = originalWorker
    URL.createObjectURL = originalCreateObjectURL
    URL.revokeObjectURL = originalRevokeObjectURL
    vi.restoreAllMocks()
  })

  it('fetchVerifiedCoreBlobUrl 校验固定版本 SHA-256，哈希不匹配时拒绝创建 Blob URL', async () => {
    const jsBytes = new TextEncoder().encode('export default function createFFmpegCore() {}')
    const expectedHash = await computeSha256Hex(jsBytes.buffer)

    expect(FFMPEG_CORE_SHA256['ffmpeg-core.js']).toMatch(/^[0-9a-f]{64}$/)
    expect(FFMPEG_CORE_SHA256['ffmpeg-core.wasm']).toMatch(/^[0-9a-f]{64}$/)

    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(jsBytes, {
        status: 200,
        headers: { 'content-type': 'application/javascript' },
      }),
    )

    const blobUrl = await fetchVerifiedCoreBlobUrl(
      'https://cdn.jsdelivr.net/npm/@ffmpeg/core@0.12.6/dist/esm/ffmpeg-core.js',
      'text/javascript',
      { expectedSha256: expectedHash },
    )
    expect(blobUrl).toBe('blob:verified-core-url')

    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(jsBytes, {
        status: 200,
        headers: { 'content-type': 'application/javascript' },
      }),
    )

    await expect(
      fetchVerifiedCoreBlobUrl(
        'https://cdn.jsdelivr.net/npm/@ffmpeg/core@0.12.6/dist/esm/ffmpeg-core.js',
        'text/javascript',
        { expectedSha256: '0'.repeat(64) },
      ),
    ).rejects.toSatisfy(
      (err: unknown) => err instanceof WasmRemuxFallbackError && err.reason === 'integrity_failed',
    )
  })

  it('fetchVerifiedCoreBlobUrl 对 404、HTML 回退页和非法 WASM 魔数立即失败', async () => {
    // 1. 404 快速失败
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(new Response('Not Found', { status: 404 }))
    await expect(
      fetchVerifiedCoreBlobUrl('/web/ffmpeg/ffmpeg-core.js', 'text/javascript'),
    ).rejects.toSatisfy(
      (err: unknown) => err instanceof WasmRemuxFallbackError && err.reason === 'load_failed',
    )

    // 2. SPA HTML 回退响应快速失败
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response('<!doctype html><html></html>', {
        status: 200,
        headers: { 'content-type': 'text/html; charset=utf-8' },
      }),
    )
    await expect(
      fetchVerifiedCoreBlobUrl('/web/ffmpeg/ffmpeg-core.js', 'text/javascript'),
    ).rejects.toSatisfy(
      (err: unknown) => err instanceof WasmRemuxFallbackError && err.reason === 'load_failed',
    )

    // 3. 非法 WASM 魔数快速失败
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(new Uint8Array([0x01, 0x02, 0x03, 0x04]), {
        status: 200,
        headers: { 'content-type': 'application/wasm' },
      }),
    )
    await expect(
      fetchVerifiedCoreBlobUrl('/web/ffmpeg/ffmpeg-core.wasm', 'application/wasm'),
    ).rejects.toSatisfy(
      (err: unknown) => err instanceof WasmRemuxFallbackError && err.reason === 'load_failed',
    )
  })

  it('remuxToMP4 无论转封装失败（非零退出码）还是抛错，都在 finally 中清理 VFS 临时文件', async () => {
    const deleteFileSpy = vi.fn().mockResolvedValue(true)
    const writeFileSpy = vi.fn().mockResolvedValue(true)
    const execSpy = vi.fn().mockResolvedValueOnce(1).mockRejectedValueOnce(new Error('wasm trap'))
    const fakeFfmpeg = {
      writeFile: writeFileSpy,
      exec: execSpy,
      readFile: vi.fn(),
      deleteFile: deleteFileSpy,
      on: vi.fn(),
      off: vi.fn(),
      terminate: vi.fn(),
    }

    vi.spyOn(globalThis, 'fetch').mockImplementation(
      async () =>
        new Response(new Uint8Array([0x1a, 0x45, 0xdf, 0xa3]), {
          status: 200,
          headers: { 'content-length': '4' },
        }),
    )

    const remuxer = new WasmRemuxer()
    // 注入已加载的 mock ffmpeg 实例
    const internal = remuxer as unknown as { ffmpeg: unknown; loaded: boolean }
    internal.ffmpeg = fakeFfmpeg
    internal.loaded = true

    // 1) 非零退出码 -> 必须清理独立 input_<jobId>.mkv 与 output_<jobId>.mp4
    await expect(
      remuxer.remuxToMP4('/emby/Videos/fail-1/stream', undefined, { container: 'mkv' }),
    ).rejects.toSatisfy(
      (err: unknown) => err instanceof WasmRemuxFallbackError && err.reason === 'exec_failed',
    )
    expect(deleteFileSpy).toHaveBeenCalledWith(expect.stringMatching(/^input_\d+\.mkv$/))
    expect(deleteFileSpy).toHaveBeenCalledWith(expect.stringMatching(/^output_\d+\.mp4$/))

    deleteFileSpy.mockClear()

    // 2) exec 抛错 -> 依然清理独立 input_<jobId>.ts 与 output_<jobId>.mp4
    await expect(
      remuxer.remuxToMP4('/emby/Videos/fail-2/stream', undefined, { container: 'ts' }),
    ).rejects.toSatisfy(
      (err: unknown) => err instanceof WasmRemuxFallbackError && err.reason === 'exec_failed',
    )
    expect(deleteFileSpy).toHaveBeenCalledWith(expect.stringMatching(/^input_\d+\.ts$/))
    expect(deleteFileSpy).toHaveBeenCalledWith(expect.stringMatching(/^output_\d+\.mp4$/))

    remuxer.dispose()
  })

  it('[L-11] 并发调用 remuxToMP4 时通过异步互斥锁串行执行并使用隔离的 MEMFS 文件名', async () => {
    const events: string[] = []
    const writtenFiles: string[] = []
    const deletedFiles: string[] = []
    const fakeFfmpeg = {
      on: vi.fn(),
      off: vi.fn(),
      writeFile: vi.fn().mockImplementation(async (name: string) => {
        writtenFiles.push(name)
        events.push(`write:${name}`)
      }),
      exec: vi.fn().mockImplementation(async (args: string[]) => {
        const inFile = args[1]
        events.push(`exec-start:${inFile}`)
        await new Promise((r) => setTimeout(r, 15))
        events.push(`exec-end:${inFile}`)
        return 0
      }),
      readFile: vi.fn().mockResolvedValue(new Uint8Array([0x00, 0x00, 0x00, 0x18])),
      deleteFile: vi.fn().mockImplementation(async (name: string) => {
        deletedFiles.push(name)
      }),
      terminate: vi.fn(),
    }

    vi.spyOn(globalThis, 'fetch').mockImplementation(async () =>
      new Response(new Uint8Array([1, 2, 3, 4]), {
        status: 200,
        headers: { 'content-length': '4' },
      }),
    )
    vi.spyOn(URL, 'createObjectURL').mockImplementation(() => `blob:test-${Math.random()}`)
    vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => undefined)

    const remuxer = new WasmRemuxer()
    const internal = remuxer as unknown as { ffmpeg: unknown; loaded: boolean }
    internal.ffmpeg = fakeFfmpeg
    internal.loaded = true

    const [res1, res2] = await Promise.all([
      remuxer.remuxToMP4('/emby/Videos/job-1/stream', undefined, { container: 'mkv' }),
      remuxer.remuxToMP4('/emby/Videos/job-2/stream', undefined, { container: 'mkv' }),
    ])

    expect(res1.url).toContain('blob:test-')
    expect(res2.url).toContain('blob:test-')
    expect(writtenFiles).toHaveLength(2)
    expect(writtenFiles[0]).not.toBe(writtenFiles[1])
    // 第一个任务必须完整 exec-end 后第二个任务才开始 write
    const firstEndIdx = events.findIndex((e) => e.startsWith('exec-end:'))
    const secondWriteIdx = events.findIndex((e, idx) => idx > 0 && e.startsWith('write:'))
    expect(firstEndIdx).toBeGreaterThanOrEqual(0)
    expect(secondWriteIdx).toBeGreaterThan(firstEndIdx)
    expect(deletedFiles).toHaveLength(4)

    remuxer.dispose()
  })

  it('[H-01 & L-08] resolveLocalCoreBase 适配 BASE_URL，probeLocalCoreAvailable 优雅探测缓存，且跨源重定向被立即阻断', async () => {
    expect(resolveLocalCoreBase('/')).toBe('/ffmpeg')
    expect(resolveLocalCoreBase('/web/')).toBe('/web/ffmpeg')
    expect(resolveLocalCoreBase('./')).toBe('./ffmpeg')

    resetLocalCoreProbeCache()
    const headSpy = vi
      .spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(
        new Response(null, {
          status: 200,
          headers: { 'content-type': 'text/html' }, // SPA fallback HTML -> not available
        }),
      )
    expect(await probeLocalCoreAvailable('/ffmpeg')).toBe(false)
    // 命中缓存，不重复发 HEAD 请求
    expect(await probeLocalCoreAvailable('/ffmpeg')).toBe(false)
    expect(headSpy).toHaveBeenCalledTimes(1)
    resetLocalCoreProbeCache()

    // 跨源重定向阻断测试
    const redirectedRes = new Response(new Uint8Array([1, 2, 3, 4]), { status: 200 })
    Object.defineProperty(redirectedRes, 'redirected', { value: true })
    Object.defineProperty(redirectedRes, 'url', { value: 'https://thirdparty.example.org/v.ts' })
    headSpy.mockResolvedValueOnce(redirectedRes)

    const remuxer = new WasmRemuxer()
    await expect(remuxer.remuxToMP4('/emby/Videos/strm/stream')).rejects.toSatisfy(
      (err: unknown) => err instanceof WasmRemuxFallbackError && err.reason === 'fetch_failed',
    )
    remuxer.dispose()
  })

  it('ensureLoaded 在 ffmpeg.load() 进行中调用 dispose() 时立即终止 Worker 并释放 Blob URLs', async () => {
    const jsBytes = new TextEncoder().encode('export default function createFFmpegCore() {}')
    const wasmBytes = new Uint8Array([0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00])

    let blobCounter = 0
    const createdUrls: string[] = []
    vi.spyOn(URL, 'createObjectURL').mockImplementation(() => {
      const url = `blob:mid-load-${++blobCounter}`
      createdUrls.push(url)
      return url
    })
    const revokeSpy = vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => undefined)

    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url.endsWith('/ffmpeg-core.js')) {
        return new Response(jsBytes, {
          status: 200,
          headers: { 'content-type': 'application/javascript' },
        })
      }
      if (url.endsWith('/ffmpeg-core.wasm')) {
        return new Response(wasmBytes, {
          status: 200,
          headers: { 'content-type': 'application/wasm' },
        })
      }
      return new Response(new Uint8Array([0x1a, 0x45, 0xdf, 0xa3]), {
        status: 200,
        headers: { 'content-length': '4' },
      })
    })

    ffmpegMockHooks.terminate.mockClear()
    let resolveLoad!: (val: boolean) => void
    const loadStarted = new Promise<void>((resolve) => {
      ffmpegMockHooks.load = () => {
        resolve()
        return new Promise<boolean>((res) => {
          resolveLoad = res
        })
      }
    })

    const remuxer = new WasmRemuxer()
    const remuxPromise = remuxer.remuxToMP4('/emby/Videos/mid-dispose/stream', undefined, {
      container: 'mkv',
      coreBaseUrl: '/ffmpeg',
    })

    try {
      // 等待进入 ffmpeg.load()，此时触发 dispose()，再让 load() 完成
      await loadStarted
      remuxer.dispose()
      resolveLoad(true)

      await expect(remuxPromise).rejects.toSatisfy(
        (err: unknown) => err instanceof WasmRemuxFallbackError && err.reason === 'aborted',
      )

      expect(ffmpegMockHooks.terminate).toHaveBeenCalledTimes(1)
      expect(createdUrls).toHaveLength(2)
      for (const url of createdUrls) {
        expect(revokeSpy).toHaveBeenCalledWith(url)
      }
    } finally {
      ffmpegMockHooks.load = null
    }
  })
})
