import JASSUB from 'jassub'
import workerUrl from 'jassub/dist/jassub-worker.js?url'
import wasmUrl from 'jassub/dist/jassub-worker.wasm?url'
import modernWasmUrl from 'jassub/dist/jassub-worker-modern.wasm?url'
import fallbackFontUrl from 'jassub/dist/default.woff2?url'

export interface AssMountOptions {
  /** MKV 内封字体附件 URL 列表，自动挂载给 libass/JASSUB */
  fonts?: string[]
  /** 初始字幕时间轴偏移（秒） */
  timeOffset?: number
}

type JassubInstanceWithExtras = JASSUB & {
  ready?: Promise<void>
  timeOffset?: number
  setCurrentTime?: (isPaused?: boolean, currentTime?: number, rate?: number) => void
}

type JassubConstructorOptions = ConstructorParameters<typeof JASSUB>[0] & {
  fonts?: string[]
  timeOffset?: number
}

/**
 * JASSUB (libass WASM + Web Worker) ASS/SSA 特效字幕渲染控制器。
 * 使用 Vite 静态资源 URL 注入本地打包的 Worker/WASM/默认字体，支持局域网离线与 Go embed.FS 无缝运行。
 */
export class AssJassubRenderer {
  private instance: JassubInstanceWithExtras | null = null
  private video: HTMLVideoElement | null = null
  private timeOffset = 0
  private abortController: AbortController | null = null
  private destroyed = false
  private mountSeq = 0
  private destroyedInstances = new WeakSet<object>()

  private async safeDestroyInstance(inst: JassubInstanceWithExtras | null): Promise<void> {
    if (!inst || this.destroyedInstances.has(inst)) return
    this.destroyedInstances.add(inst)
    try {
      await inst.destroy()
    } catch {
      // ignore teardown errors
    }
  }

  private async teardownActive(): Promise<void> {
    if (this.abortController) {
      this.abortController.abort()
      this.abortController = null
    }
    this.video = null
    if (this.instance) {
      const inst = this.instance
      this.instance = null
      await this.safeDestroyInstance(inst)
    }
  }

  async mount(
    video: HTMLVideoElement,
    canvas: HTMLCanvasElement,
    subtitleUrl: string,
    options?: AssMountOptions | string[],
  ): Promise<void> {
    const seq = ++this.mountSeq
    this.destroyed = false
    await this.teardownActive()
    if (seq !== this.mountSeq || this.destroyed) {
      return
    }

    const abortController = new AbortController()
    this.abortController = abortController
    const { signal } = abortController
    this.video = video

    const opts: AssMountOptions = Array.isArray(options) ? { fonts: options } : (options ?? {})
    this.timeOffset = Number.isFinite(opts.timeOffset) ? Number(opts.timeOffset) : 0
    const attachmentFonts = (opts.fonts ?? []).filter((u) => typeof u === 'string' && u.length > 0)

    const res = await fetch(subtitleUrl, { signal })
    if (seq !== this.mountSeq || signal.aborted || this.destroyed) {
      return
    }
    if (!res.ok) {
      throw new Error(`加载 ASS/SSA 字幕失败 (${res.status})`)
    }
    const subContent = await res.text()
    if (seq !== this.mountSeq || signal.aborted || this.destroyed) {
      return
    }

    const jassubOptions: JassubConstructorOptions = {
      video,
      canvas,
      subContent,
      workerUrl,
      wasmUrl,
      modernWasmUrl,
      timeOffset: this.timeOffset,
      fonts: attachmentFonts.length > 0 ? attachmentFonts : undefined,
      fallbackFont: 'liberation sans',
      availableFonts: {
        'liberation sans': fallbackFontUrl,
      },
      prescaleFactor: 1.0,
      offscreenRender: false,
    }
    const instance = new JASSUB(jassubOptions) as JassubInstanceWithExtras
    if (seq !== this.mountSeq || signal.aborted || this.destroyed) {
      await this.safeDestroyInstance(instance)
      return
    }
    this.instance = instance

    try {
      if (instance.ready) {
        await instance.ready
      }
    } catch (err) {
      if (this.instance === instance) {
        this.instance = null
      }
      await this.safeDestroyInstance(instance)
      if (seq === this.mountSeq && !signal.aborted && !this.destroyed) {
        throw err
      }
      return
    }

    if (seq !== this.mountSeq || signal.aborted || this.destroyed) {
      if (this.instance === instance) {
        this.instance = null
      }
      await this.safeDestroyInstance(instance)
    }
  }

  /**
   * 动态调整 ASS 字幕渲染时间轴偏移（秒），立即触发重绘。
   */
  setTimeOffset(seconds: number): void {
    this.timeOffset = Number.isFinite(seconds) ? seconds : 0
    if (!this.instance) return
    this.instance.timeOffset = this.timeOffset
    if (this.video && typeof this.instance.setCurrentTime === 'function') {
      try {
        this.instance.setCurrentTime(
          this.video.paused,
          Math.max(0, this.video.currentTime + this.timeOffset),
          this.video.playbackRate,
        )
      } catch {
        // ignore sync race during teardown
      }
    }
  }

  resize(): void {
    try {
      this.instance?.resize(true)
    } catch {
      // ignore resize race during teardown
    }
  }

  async destroy(): Promise<void> {
    this.mountSeq += 1
    this.destroyed = true
    await this.teardownActive()
  }
}

export { AssJassubRenderer as AssSubtitleRenderer }
