declare module 'jassub' {
  export interface JassubOptions {
    video?: HTMLVideoElement
    canvas?: HTMLCanvasElement
    subUrl?: string
    subContent?: string
    workerUrl?: string
    wasmUrl?: string
    modernWasmUrl?: string
    legacyWasmUrl?: string
    fallbackFont?: string
    fonts?: string[]
    availableFonts?: Record<string, string>
    debug?: boolean
    prescaleFactor?: number
    prescaleHeightLimit?: number
    maxRenderHeight?: number
    dropAllAnimations?: boolean
    offscreenRender?: boolean
  }

  export default class JASSUB {
    constructor(options: JassubOptions)
    ready: Promise<void>
    setTrackByUrl(url: string): void
    setTrack(content: string): void
    freeTrack(): void
    setVideo(video: HTMLVideoElement): void
    resize(force?: boolean, width?: number, height?: number, top?: number, left?: number): void
    destroy(): Promise<void> | void
  }
}

declare module 'jassub/dist/jassub-worker.js?url' {
  const src: string
  export default src
}
declare module 'jassub/dist/jassub-worker.wasm?url' {
  const src: string
  export default src
}
declare module 'jassub/dist/jassub-worker-modern.wasm?url' {
  const src: string
  export default src
}
declare module 'jassub/dist/default.woff2?url' {
  const src: string
  export default src
}
