import { FFmpeg } from '@ffmpeg/ffmpeg'
import { isSameOriginUrl } from '../api/client'

/** localStorage 键：是否优先启用客户端 WASM 解复用/转封装 */
export const WASM_REMUX_PREF_KEY = 'ambra_player_prefer_wasm_remux'

/** 默认安全文件大小上限（150 MB），防止浏览器 WASM 内存溢出 */
export const DEFAULT_WASM_MAX_BYTES = 150 * 1024 * 1024

/** 固定版本 @ffmpeg/core 资产 SHA-256 摘要（用于公网 CDN 完整性校验） */
export const FFMPEG_CORE_VERSION = '0.12.6'
export const FFMPEG_CORE_SHA256 = {
  'ffmpeg-core.js': 'c972f5abeafcd5f3949e54edfbdc74a9badb27025809feb1945d468ffbcfb7f1',
  'ffmpeg-core.wasm': '2390efa7fb66e7e42dbae15427571a5ffc96b829480904c30f471f0a78967f61',
} as const

/** 基于 Vite BASE_URL 计算本地自托管 @ffmpeg/core 路径 */
export function resolveLocalCoreBase(explicitBaseUrl?: string): string {
  const rawBase =
    explicitBaseUrl ??
    (typeof import.meta !== 'undefined' && import.meta.env?.BASE_URL
      ? import.meta.env.BASE_URL
      : '/')
  if (!rawBase || rawBase === './' || rawBase === '.') {
    if (typeof window !== 'undefined' && window.location?.pathname?.startsWith('/web/')) {
      return '/web/ffmpeg'
    }
    return './ffmpeg'
  }
  const trimmed = rawBase.replace(/\/+$/, '')
  return `${trimmed}/ffmpeg`
}

function isLocalCoreBase(base: string): boolean {
  return base.startsWith('/') || base.startsWith('./') || base.startsWith('../')
}

let localCoreProbeState: 'unknown' | 'available' | 'unavailable' = 'unknown'

export function resetLocalCoreProbeCache(): void {
  localCoreProbeState = 'unknown'
}

export async function probeLocalCoreAvailable(base: string, signal?: AbortSignal): Promise<boolean> {
  if (localCoreProbeState === 'available') return true
  if (localCoreProbeState === 'unavailable') return false
  try {
    const res = await fetch(`${base}/ffmpeg-core.js`, { method: 'HEAD', signal })
    if (!res.ok) {
      localCoreProbeState = 'unavailable'
      return false
    }
    const ct = (res.headers.get('content-type') || '').toLowerCase()
    if (ct.includes('text/html') || ct.includes('application/xhtml')) {
      localCoreProbeState = 'unavailable'
      return false
    }
    localCoreProbeState = 'available'
    return true
  } catch {
    localCoreProbeState = 'unavailable'
    return false
  }
}

/** 默认加载与转封装超时时间（本地自托管快速探测，CDN 缩短超时避免卡死 25 秒） */
const DEFAULT_LOCAL_LOAD_TIMEOUT_MS = 4_000
const DEFAULT_CDN_LOAD_TIMEOUT_MS = 10_000
const DEFAULT_REMUX_TIMEOUT_MS = 120_000

/**
 * 单线程 @ffmpeg/core 公网 CDN 源列表（不依赖 SharedArrayBuffer）
 */
const CDN_SINGLE_THREAD_CORE_BASES = [
  `https://unpkg.com/@ffmpeg/core@${FFMPEG_CORE_VERSION}/dist/esm`,
  `https://cdn.jsdelivr.net/npm/@ffmpeg/core@${FFMPEG_CORE_VERSION}/dist/esm`,
]

/** 浏览器原生 <video> 通常无法直连的封装容器 */
const UNSUPPORTED_CONTAINERS = new Set([
  'mkv',
  'matroska',
  'ts',
  'm2ts',
  'mts',
  'mpegts',
  'avi',
  'flv',
  'wmv',
  'asf',
  'rmvb',
  'rm',
  'vob',
  'mpg',
  'mpeg',
  'divx',
  'f4v',
])

/** 浏览器原生 <video> 普遍无法硬解的音轨编码 */
const UNSUPPORTED_AUDIO_CODECS = new Set([
  'ac3',
  'ac-3',
  'eac3',
  'ec3',
  'ec-3',
  'dts',
  'dca',
  'truehd',
  'mlp',
  'wma',
  'wmav1',
  'wmav2',
  'wmapro',
  'pcm_bluray',
  'pcm_dvd',
])

export type WasmRemuxStage = 'init' | 'fetch' | 'remux' | 'done'

export type WasmFallbackReason =
  | 'unsupported'
  | 'disabled'
  | 'size_exceeded'
  | 'timeout'
  | 'aborted'
  | 'fetch_failed'
  | 'load_failed'
  | 'integrity_failed'
  | 'exec_failed'

export class WasmRemuxFallbackError extends Error {
  readonly reason: WasmFallbackReason

  constructor(reason: WasmFallbackReason, message: string) {
    super(message)
    this.name = 'WasmRemuxFallbackError'
    this.reason = reason
  }
}

export interface WasmRemuxOptions {
  /** 允许的最大源文件字节数，默认 150MB */
  maxBytes?: number
  /** 已知的文件大小（来自 MediaSourceInfo.Size），超限时无需拉流直接降级 */
  knownSizeBytes?: number
  /** 源容器名（如 mkv / ts / avi），用于为虚拟文件选择正确扩展名 */
  container?: string
  /** 目标音轨索引（0-based 音频流序号，传入 -map 0:v:0 -map 0:a:<idx>） */
  audioStreamIndex?: number
  /** 转封装超时时间（毫秒），默认 120000 */
  timeoutMs?: number
  /** 可选自定义 @ffmpeg/core 基础 URL（例如自托管静态目录） */
  coreBaseUrl?: string
  /** 外部取消信号 */
  signal?: AbortSignal
  /** 实时进度回调：stage 阶段 + 0-100 百分比 + 可选描述 */
  onProgress?: (stage: WasmRemuxStage, percent: number, detail?: string) => void
}

export interface WasmRemuxResult {
  /** 可直接赋给 <video src> 的 fMP4 Blob Object URL */
  url: string
  /** 转封装后 MP4 大小（字节） */
  sizeBytes: number
  /** 总耗时（毫秒） */
  elapsedMs: number
  /** 是否使用了多线程 WASM */
  multithreaded: boolean
}

/** 读取用户持久化偏好（默认开启） */
export function getPreferWasmRemux(): boolean {
  try {
    const raw = localStorage.getItem(WASM_REMUX_PREF_KEY)
    if (raw === null) return true
    return raw === 'true' || raw === '1'
  } catch {
    return true
  }
}

/** 持久化用户偏好 */
export function setPreferWasmRemux(enabled: boolean): void {
  try {
    localStorage.setItem(WASM_REMUX_PREF_KEY, enabled ? 'true' : 'false')
  } catch {
    // 隐私模式或存储不可用时静默忽略
  }
}

/** 检测当前页面是否支持 SharedArrayBuffer（跨源隔离环境） */
export function hasSharedArrayBufferSupport(): boolean {
  return (
    typeof SharedArrayBuffer !== 'undefined' &&
    typeof globalThis.crossOriginIsolated === 'boolean' &&
    globalThis.crossOriginIsolated
  )
}

/**
 * 探测浏览器是否具备运行客户端 WASM 解复用的基础能力。
 * 不强依赖 SharedArrayBuffer，允许优雅使用单线程 @ffmpeg/core 以兼容普通 HTTP 局域网 NAS。
 */
export function canUseWasmRemux(options?: { enabled?: boolean }): boolean {
  if (options?.enabled === false) return false
  if (typeof window === 'undefined') return false
  if (typeof WebAssembly !== 'object' || typeof WebAssembly.instantiate !== 'function') {
    return false
  }
  if (typeof Worker === 'undefined') return false
  if (typeof Blob === 'undefined' || typeof URL === 'undefined' || typeof URL.createObjectURL !== 'function') {
    return false
  }
  return true
}

/**
 * 判断媒体容器或音轨编码是否需要客户端 WASM 解复用/音频转封装。
 * 原生兼容容器（mp4 / m4v / webm / mov）且音轨为浏览器原生支持时返回 false（走 DirectPlay）。
 */
export function needsClientRemux(container?: string, audioCodec?: string): boolean {
  const normalizedAudio = (audioCodec || '').trim().toLowerCase()
  if (normalizedAudio && UNSUPPORTED_AUDIO_CODECS.has(normalizedAudio)) {
    return true
  }

  if (!container) return false

  const tokens = container
    .toLowerCase()
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)

  if (tokens.length === 0) return false

  // ffprobe 常将 MKV 标识为 "matroska,webm"，优先检测非原生容器
  for (const token of tokens) {
    if (UNSUPPORTED_CONTAINERS.has(token)) {
      return true
    }
  }

  return false
}

/** 根据容器名规范化虚拟输入文件扩展名 */
function resolveInputExtension(container?: string): string {
  if (!container) return 'mkv'
  const tokens = container
    .toLowerCase()
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)

  for (const token of tokens) {
    if (token === 'matroska') return 'mkv'
    if (token === 'mpegts') return 'ts'
    if (UNSUPPORTED_CONTAINERS.has(token)) return token
  }
  return tokens[0] || 'mkv'
}

export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB']
  let idx = 0
  let val = bytes
  while (val >= 1024 && idx < units.length - 1) {
    val /= 1024
    idx++
  }
  return `${val >= 100 || idx === 0 ? val.toFixed(0) : val.toFixed(1)} ${units[idx]}`
}

/** 计算 ArrayBuffer 的小写十六进制 SHA-256 摘要 */
export async function computeSha256Hex(buffer: ArrayBuffer): Promise<string> {
  if (typeof crypto === 'undefined' || typeof crypto.subtle?.digest !== 'function') {
    throw new WasmRemuxFallbackError(
      'integrity_failed',
      '当前运行环境不支持 Web Crypto SHA-256 校验，拒绝加载未校验的远端 WASM 资产',
    )
  }
  const digest = await crypto.subtle.digest('SHA-256', buffer)
  return Array.from(new Uint8Array(digest))
    .map((b) => b.toString(16).padStart(2, '0'))
    .join('')
}

function isHtmlPayload(bytes: Uint8Array): boolean {
  let i = 0
  while (i < bytes.byteLength && (bytes[i] === 0x20 || bytes[i] === 0x09 || bytes[i] === 0x0a || bytes[i] === 0x0d)) {
    i++
  }
  if (i >= bytes.byteLength) return false
  return bytes[i] === 0x3c // '<'
}

function hasWasmMagic(bytes: Uint8Array): boolean {
  return (
    bytes.byteLength >= 4 &&
    bytes[0] === 0x00 &&
    bytes[1] === 0x61 &&
    bytes[2] === 0x73 &&
    bytes[3] === 0x6d
  )
}

/**
 * 拉取 FFmpeg Core JS/WASM 资产并校验 HTTP 状态、非 HTML Content-Type、WASM 魔数以及 SHA-256 完整性，
 * 通过校验后再创建可执行的 Blob URL。
 */
export async function fetchVerifiedCoreBlobUrl(
  url: string,
  mimeType: 'text/javascript' | 'application/wasm',
  opts?: {
    expectedSha256?: string
    signal?: AbortSignal
  },
): Promise<string> {
  let res: Response
  try {
    res = await fetch(url, { method: 'GET', signal: opts?.signal })
  } catch (err) {
    if (opts?.signal?.aborted) {
      throw new WasmRemuxFallbackError('aborted', '已取消 WASM 核心资产加载')
    }
    const msg = err instanceof Error ? err.message : '网络请求失败'
    throw new WasmRemuxFallbackError('load_failed', `拉取 WASM 资产失败 (${url}): ${msg}`)
  }

  if (!res.ok) {
    throw new WasmRemuxFallbackError('load_failed', `WASM 资产返回 HTTP ${res.status}: ${url}`)
  }

  const contentType = (res.headers.get('content-type') || '').toLowerCase()
  if (contentType.includes('text/html') || contentType.includes('application/xhtml')) {
    throw new WasmRemuxFallbackError(
      'load_failed',
      `WASM 资产返回无效 HTML 响应 (${contentType}): ${url}`,
    )
  }

  const arrayBuffer = await res.arrayBuffer()
  const bytes = new Uint8Array(arrayBuffer)
  if (bytes.byteLength === 0) {
    throw new WasmRemuxFallbackError('load_failed', `WASM 资产内容为空: ${url}`)
  }

  if (mimeType === 'application/wasm' && !hasWasmMagic(bytes)) {
    throw new WasmRemuxFallbackError('load_failed', `无效的 WebAssembly 二进制魔数: ${url}`)
  }
  if (mimeType === 'text/javascript' && isHtmlPayload(bytes)) {
    throw new WasmRemuxFallbackError('load_failed', `WASM 脚本内容疑似 HTML 回退页: ${url}`)
  }

  if (opts?.expectedSha256) {
    const actualSha256 = await computeSha256Hex(arrayBuffer)
    if (actualSha256 !== opts.expectedSha256.toLowerCase()) {
      throw new WasmRemuxFallbackError(
        'integrity_failed',
        `WASM 资产 SHA-256 完整性校验失败 (${url})`,
      )
    }
  }

  const blob = new Blob([arrayBuffer], { type: mimeType })
  return URL.createObjectURL(blob)
}

function resolveExpectedCoreSha256(
  base: string,
  assetName: keyof typeof FFMPEG_CORE_SHA256,
): string | undefined {
  if (isLocalCoreBase(base)) {
    // 同源本地自托管允许管理员替换自定义编译版本，由 HTTP 状态 + MIME + 魔数校验
    return undefined
  }
  // 远端公网 CDN 强校验固定版本 SHA-256
  return FFMPEG_CORE_SHA256[assetName]
}

function mergeAbortSignals(a?: AbortSignal, b?: AbortSignal): AbortSignal | undefined {
  if (!a) return b
  if (!b) return a
  const ctrl = new AbortController()
  const onAbort = () => ctrl.abort()
  if (a.aborted || b.aborted) {
    ctrl.abort()
    return ctrl.signal
  }
  a.addEventListener('abort', onAbort, { once: true })
  b.addEventListener('abort', onAbort, { once: true })
  return ctrl.signal
}

function withTimeout<T>(promise: Promise<T>, ms: number, reason: WasmFallbackReason, msg: string): Promise<T> {
  if (ms <= 0) return promise
  return new Promise<T>((resolve, reject) => {
    const timer = window.setTimeout(() => {
      reject(new WasmRemuxFallbackError(reason, msg))
    }, ms)
    promise.then(
      (val) => {
        window.clearTimeout(timer)
        resolve(val)
      },
      (err) => {
        window.clearTimeout(timer)
        reject(err)
      },
    )
  })
}

/**
 * 客户端 ffmpeg.wasm 轻量解复用/转封装器：
 * - 视频流采用 `-c:v copy` 零重编码极速转封装；
 * - 音轨统一转为浏览器全兼容的 AAC 192k；
 * - 输出 fMP4（frag_keyframe+empty_moov+faststart）Blob URL；
 * - 支持单线程 @ffmpeg/core，在非 HTTPS 普通局域网 NAS 上也可稳定运行。
 */
export class WasmRemuxer {
  private ffmpeg: FFmpeg | null = null
  private loaded = false
  private loadingPromise: Promise<void> | null = null
  private activeObjectUrls = new Set<string>()
  private coreBlobUrls: string[] = []
  private activeAbortController: AbortController | null = null
  private remuxMutex: Promise<void> = Promise.resolve()
  private jobCounter = 0
  private disposed = false

  /** 懒加载单线程 @ffmpeg/core（自动多源回退，避免强依赖 SharedArrayBuffer） */
  private async ensureLoaded(opts?: {
    coreBaseUrl?: string
    signal?: AbortSignal
    onProgress?: WasmRemuxOptions['onProgress']
  }): Promise<FFmpeg> {
    if (this.disposed) {
      throw new WasmRemuxFallbackError('aborted', 'WASM 转封装器已销毁')
    }
    if (this.ffmpeg && this.loaded) {
      return this.ffmpeg
    }
    if (this.loadingPromise) {
      await this.loadingPromise
      if (this.ffmpeg && this.loaded) return this.ffmpeg
    }

    this.loadingPromise = (async () => {
      opts?.onProgress?.('init', 10, '正在初始化 FFmpeg WASM 引擎...')
      const ffmpeg = new FFmpeg()
      const defaultLocalBase = resolveLocalCoreBase()
      let bases: string[]
      if (opts?.coreBaseUrl) {
        bases = [opts.coreBaseUrl.replace(/\/+$/, ''), ...CDN_SINGLE_THREAD_CORE_BASES]
      } else {
        const hasLocal = await probeLocalCoreAvailable(defaultLocalBase, opts?.signal)
        bases = hasLocal
          ? [defaultLocalBase, ...CDN_SINGLE_THREAD_CORE_BASES]
          : [...CDN_SINGLE_THREAD_CORE_BASES]
      }

      let lastErr: unknown = null
      for (const base of bases) {
        if (opts?.signal?.aborted || this.disposed) {
          throw new WasmRemuxFallbackError('aborted', '已取消 WASM 引擎加载')
        }
        const isLocalBase = isLocalCoreBase(base)
        const loadTimeoutMs = isLocalBase ? DEFAULT_LOCAL_LOAD_TIMEOUT_MS : DEFAULT_CDN_LOAD_TIMEOUT_MS
        const pendingBlobUrls: string[] = []
        try {
          opts?.onProgress?.(
            'init',
            35,
            isLocalBase ? '正在加载本地离线 WASM 核心...' : '正在加载并校验单线程 WASM 核心...',
          )
          const [coreURL, wasmURL] = await withTimeout(
            Promise.all([
              fetchVerifiedCoreBlobUrl(`${base}/ffmpeg-core.js`, 'text/javascript', {
                expectedSha256: resolveExpectedCoreSha256(base, 'ffmpeg-core.js'),
                signal: opts?.signal,
              }),
              fetchVerifiedCoreBlobUrl(`${base}/ffmpeg-core.wasm`, 'application/wasm', {
                expectedSha256: resolveExpectedCoreSha256(base, 'ffmpeg-core.wasm'),
                signal: opts?.signal,
              }),
            ]),
            loadTimeoutMs,
            'timeout',
            '加载 FFmpeg WASM 核心超时，自动回退服务端 HLS',
          )
          pendingBlobUrls.push(coreURL, wasmURL)

          opts?.onProgress?.('init', 80, '正在启动 WASM Worker...')
          await withTimeout(
            ffmpeg.load({ coreURL, wasmURL }, { signal: opts?.signal }),
            loadTimeoutMs,
            'timeout',
            '启动 FFmpeg WASM Worker 超时，自动回退服务端 HLS',
          )

          this.coreBlobUrls.push(...pendingBlobUrls)
          this.ffmpeg = ffmpeg
          this.loaded = true
          opts?.onProgress?.('init', 100, 'WASM 引擎就绪')
          return
        } catch (err) {
          for (const url of pendingBlobUrls) {
            URL.revokeObjectURL(url)
          }
          lastErr = err
          if (err instanceof WasmRemuxFallbackError && err.reason === 'aborted') {
            throw err
          }
        }
      }

      const msg = lastErr instanceof Error ? lastErr.message : '未知加载错误'
      throw new WasmRemuxFallbackError('load_failed', `FFmpeg WASM 加载失败: ${msg}`)
    })()

    try {
      await this.loadingPromise
    } finally {
      this.loadingPromise = null
    }

    if (!this.ffmpeg) {
      throw new WasmRemuxFallbackError('load_failed', 'FFmpeg WASM 初始化失败')
    }
    return this.ffmpeg
  }

  /**
   * 拉取远程流并在浏览器内零重编码转封装为 fMP4 Blob URL。
   * 超出安全体积阈值、超时或出错时抛出 WasmRemuxFallbackError，由调用方无感回退服务端 HLS。
   */
  async remuxToMP4(
    streamUrl: string,
    authHeaders?: Record<string, string>,
    opts: WasmRemuxOptions = {},
  ): Promise<WasmRemuxResult> {
    if (!canUseWasmRemux()) {
      throw new WasmRemuxFallbackError('unsupported', '当前浏览器不支持 WebAssembly 或 Web Worker')
    }

    const maxBytes = opts.maxBytes ?? DEFAULT_WASM_MAX_BYTES
    if (opts.knownSizeBytes !== undefined && opts.knownSizeBytes > maxBytes) {
      throw new WasmRemuxFallbackError(
        'size_exceeded',
        `媒体体积 ${formatBytes(opts.knownSizeBytes)} 超过客户端 WASM 安全阈值 (${formatBytes(maxBytes)})`,
      )
    }

    const jobId = ++this.jobCounter
    const prevLock = this.remuxMutex
    let releaseMutex!: () => void
    this.remuxMutex = new Promise<void>((resolve) => {
      releaseMutex = resolve
    })

    await prevLock

    const startedAt = performance.now()
    const localAbort = new AbortController()
    this.activeAbortController = localAbort
    const signal = mergeAbortSignals(localAbort.signal, opts.signal)

    const inputExt = resolveInputExtension(opts.container)
    const inputName = `input_${jobId}.${inputExt}`
    const outputName = `output_${jobId}.mp4`
    let loadedFfmpeg: FFmpeg | null = null
    let vfsDirty = false

    try {
      if (signal?.aborted || this.disposed) {
        throw new WasmRemuxFallbackError('aborted', '已取消客户端转封装')
      }

      // 1. 先检查与流式拉取媒体数据（实时上报进度并动态检测体积上限）
      opts.onProgress?.('fetch', 0, '正在连接媒体流...')
      const inputBytes = await this.fetchStreamWithLimit(
        streamUrl,
        authHeaders,
        maxBytes,
        signal,
        opts.onProgress,
      )

      if (signal?.aborted || this.disposed) {
        throw new WasmRemuxFallbackError('aborted', '已取消客户端转封装')
      }

      // 2. 加载单线程 FFmpeg WASM 核心
      const ffmpeg = await this.ensureLoaded({
        coreBaseUrl: opts.coreBaseUrl,
        signal,
        onProgress: opts.onProgress,
      })
      loadedFfmpeg = ffmpeg

      if (signal?.aborted || this.disposed) {
        throw new WasmRemuxFallbackError('aborted', '已取消客户端转封装')
      }

      // 3. 写入虚拟文件系统并执行零重编码转封装
      opts.onProgress?.('remux', 2, '正在写入虚拟内存文件系统...')
      vfsDirty = true
      await ffmpeg.writeFile(inputName, inputBytes, { signal })

      const onFfmpegProgress = ({ progress }: { progress: number }) => {
        const clamped = Math.max(0, Math.min(1, Number.isFinite(progress) ? progress : 0))
        const pct = Math.min(99, Math.max(5, Math.round(clamped * 100)))
        opts.onProgress?.('remux', pct, `视频零重编码转封装中 (${pct}%)...`)
      }

      ffmpeg.on('progress', onFfmpegProgress)
      let exitCode: number
      try {
        const execTimeout = opts.timeoutMs ?? DEFAULT_REMUX_TIMEOUT_MS
        const mapArgs =
          opts.audioStreamIndex !== undefined &&
          Number.isInteger(opts.audioStreamIndex) &&
          opts.audioStreamIndex >= 0
            ? ['-map', '0:v:0', '-map', `0:a:${opts.audioStreamIndex}`]
            : []
        exitCode = await withTimeout(
          ffmpeg.exec(
            [
              '-i',
              inputName,
              ...mapArgs,
              '-c:v',
              'copy',
              '-c:a',
              'aac',
              '-b:a',
              '192k',
              '-movflags',
              'frag_keyframe+empty_moov+faststart',
              outputName,
            ],
            execTimeout,
            { signal },
          ),
          execTimeout + 5_000,
          'timeout',
          '客户端 WASM 转封装超时，自动回退服务端 HLS',
        )
      } finally {
        ffmpeg.off('progress', onFfmpegProgress)
      }

      if (exitCode !== 0) {
        throw new WasmRemuxFallbackError('exec_failed', `FFmpeg WASM 退出码异常 (${exitCode})`)
      }

      // 4. 读取输出并构造 Blob URL
      const rawOutput = await ffmpeg.readFile(outputName, undefined, { signal })
      const outputBytes =
        typeof rawOutput === 'string'
          ? new TextEncoder().encode(rawOutput)
          : new Uint8Array(rawOutput)

      if (outputBytes.byteLength === 0) {
        throw new WasmRemuxFallbackError('exec_failed', 'WASM 转封装输出为空')
      }

      const blob = new Blob([outputBytes], { type: 'video/mp4' })
      const url = URL.createObjectURL(blob)
      this.activeObjectUrls.add(url)

      opts.onProgress?.('done', 100, '客户端转封装完成，准备播放')

      return {
        url,
        sizeBytes: blob.size,
        elapsedMs: Math.round(performance.now() - startedAt),
        multithreaded: false,
      }
    } catch (err) {
      if (err instanceof WasmRemuxFallbackError) {
        throw err
      }
      if (signal?.aborted || (err instanceof DOMException && err.name === 'AbortError')) {
        throw new WasmRemuxFallbackError('aborted', '客户端 WASM 转封装已中止')
      }
      const message = err instanceof Error ? err.message : String(err)
      throw new WasmRemuxFallbackError('exec_failed', `客户端 WASM 转封装失败: ${message}`)
    } finally {
      // 无论成功、失败、抛错还是超时，均清理 WASM 虚拟文件系统中的唯一临时输入/输出文件，防止高达 150MB 内存泄漏
      if (loadedFfmpeg && vfsDirty) {
        await Promise.allSettled([
          loadedFfmpeg.deleteFile(inputName),
          loadedFfmpeg.deleteFile(outputName),
        ])
      }
      if (this.activeAbortController === localAbort) {
        this.activeAbortController = null
      }
      releaseMutex()
    }
  }

  /** 带进度与最大字节数保护的流式拉取 */
  private async fetchStreamWithLimit(
    streamUrl: string,
    authHeaders: Record<string, string> | undefined,
    maxBytes: number,
    signal: AbortSignal | undefined,
    onProgress?: WasmRemuxOptions['onProgress'],
  ): Promise<Uint8Array> {
    let res: Response
    try {
      res = await fetch(streamUrl, {
        method: 'GET',
        headers: authHeaders,
        signal,
      })
    } catch (err) {
      if (signal?.aborted) {
        throw new WasmRemuxFallbackError('aborted', '拉流已取消')
      }
      const msg = err instanceof Error ? err.message : '网络请求失败'
      throw new WasmRemuxFallbackError('fetch_failed', `拉取媒体流失败: ${msg}`)
    }

    if (res.redirected && res.url && !isSameOriginUrl(res.url)) {
      res.body?.cancel().catch(() => undefined)
      throw new WasmRemuxFallbackError(
        'fetch_failed',
        '检测到跨源重定向响应，已中止客户端拉流并安全回退',
      )
    }

    if (!res.ok) {
      throw new WasmRemuxFallbackError('fetch_failed', `拉取媒体流返回 HTTP ${res.status}`)
    }

    const contentLengthHeader = res.headers.get('content-length')
    const totalBytes = contentLengthHeader ? Number(contentLengthHeader) : 0
    if (Number.isFinite(totalBytes) && totalBytes > maxBytes) {
      res.body?.cancel().catch(() => undefined)
      throw new WasmRemuxFallbackError(
        'size_exceeded',
        `媒体流体积 ${formatBytes(totalBytes)} 超过客户端 WASM 阈值 (${formatBytes(maxBytes)})`,
      )
    }

    if (!res.body) {
      const buf = await res.arrayBuffer()
      if (buf.byteLength > maxBytes) {
        throw new WasmRemuxFallbackError(
          'size_exceeded',
          `媒体流体积 ${formatBytes(buf.byteLength)} 超过客户端 WASM 阈值 (${formatBytes(maxBytes)})`,
        )
      }
      onProgress?.('fetch', 100, `已拉取 ${formatBytes(buf.byteLength)}`)
      return new Uint8Array(buf)
    }

    const reader = res.body.getReader()
    const chunks: Uint8Array[] = []
    let received = 0

    try {
      while (true) {
        const { done, value } = await reader.read()
        if (done) break
        if (!value) continue

        received += value.byteLength
        if (received > maxBytes) {
          await reader.cancel().catch(() => undefined)
          throw new WasmRemuxFallbackError(
            'size_exceeded',
            `媒体流体积超过客户端 WASM 阈值 (${formatBytes(maxBytes)})`,
          )
        }

        chunks.push(value)
        if (totalBytes > 0) {
          const pct = Math.min(100, Math.round((received / totalBytes) * 100))
          onProgress?.(
            'fetch',
            pct,
            `正在拉取媒体流 ${formatBytes(received)} / ${formatBytes(totalBytes)}`,
          )
        } else {
          // 未知总大小时给出渐进估算
          const estPct = Math.min(95, Math.round((received / maxBytes) * 100))
          onProgress?.('fetch', estPct, `正在拉取媒体流 ${formatBytes(received)}`)
        }
      }
    } catch (err) {
      if (err instanceof WasmRemuxFallbackError) throw err
      if (signal?.aborted) {
        throw new WasmRemuxFallbackError('aborted', '拉流已取消')
      }
      const msg = err instanceof Error ? err.message : '流读取中断'
      throw new WasmRemuxFallbackError('fetch_failed', `读取媒体流失败: ${msg}`)
    }

    const merged = new Uint8Array(received)
    let offset = 0
    for (const chunk of chunks) {
      merged.set(chunk, offset)
      offset += chunk.byteLength
    }
    onProgress?.('fetch', 100, `媒体流拉取完成 (${formatBytes(received)})`)
    return merged
  }

  /** 释放单个或多个 Object URL */
  revokeUrl(url: string): void {
    if (this.activeObjectUrls.has(url)) {
      URL.revokeObjectURL(url)
      this.activeObjectUrls.delete(url)
    }
  }

  /** 终止进行中的任务并彻底释放 ObjectURL 与 WASM 内存 */
  dispose(): void {
    this.disposed = true
    if (this.activeAbortController) {
      this.activeAbortController.abort()
      this.activeAbortController = null
    }
    for (const url of this.activeObjectUrls) {
      URL.revokeObjectURL(url)
    }
    this.activeObjectUrls.clear()

    for (const url of this.coreBlobUrls) {
      URL.revokeObjectURL(url)
    }
    this.coreBlobUrls = []

    if (this.ffmpeg) {
      try {
        this.ffmpeg.terminate()
      } catch {
        // 忽略已终止 worker 的异常
      }
      this.ffmpeg = null
      this.loaded = false
    }
  }
}
