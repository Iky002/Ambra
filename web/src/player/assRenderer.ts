import JASSUB from 'jassub'
import workerUrl from 'jassub/dist/jassub-worker.js?url'
import wasmUrl from 'jassub/dist/jassub-worker.wasm?url'
import modernWasmUrl from 'jassub/dist/jassub-worker-modern.wasm?url'
import fallbackFontUrl from 'jassub/dist/default.woff2?url'

/**
 * JASSUB (libass WASM + Web Worker) ASS/SSA 特效字幕渲染控制器。
 * 使用 Vite 静态资源 URL 注入本地打包的 Worker/WASM/默认字体，支持局域网离线与 Go embed.FS 无缝运行。
 */
export class AssJassubRenderer {
  private instance: JASSUB | null = null
  private abortController: AbortController | null = null

  async mount(
    video: HTMLVideoElement,
    canvas: HTMLCanvasElement,
    subtitleUrl: string,
  ): Promise<void> {
    await this.destroy()
    this.abortController = new AbortController()

    const res = await fetch(subtitleUrl, { signal: this.abortController.signal })
    if (!res.ok) {
      throw new Error(`加载 ASS/SSA 字幕失败 (${res.status})`)
    }
    const subContent = await res.text()

    this.instance = new JASSUB({
      video,
      canvas,
      subContent,
      workerUrl,
      wasmUrl,
      modernWasmUrl,
      fallbackFont: 'liberation sans',
      availableFonts: {
        'liberation sans': fallbackFontUrl,
      },
      prescaleFactor: 1.0,
      offscreenRender: false,
    })

    if (this.instance.ready) {
      await this.instance.ready
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
    if (this.abortController) {
      this.abortController.abort()
      this.abortController = null
    }
    if (this.instance) {
      const inst = this.instance
      this.instance = null
      try {
        await inst.destroy()
      } catch {
        // ignore teardown errors
      }
    }
  }
}
