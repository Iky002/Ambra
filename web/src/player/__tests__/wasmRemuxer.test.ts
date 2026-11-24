import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  canUseWasmRemux,
  DEFAULT_WASM_MAX_BYTES,
  formatBytes,
  getPreferWasmRemux,
  needsClientRemux,
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
