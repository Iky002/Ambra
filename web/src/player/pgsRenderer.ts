/**
 * 纯 TypeScript 零依赖 Blu-ray PGS (.sup) 图形字幕解码与 Canvas 同步渲染器。
 *
 * 完整实现 Blu-ray Presentation Graphic Stream (PGS) 规范：
 * - 头部魔数 0x50 0x47 ('PG') + 90kHz PTS/DTS 时间戳
 * - 0x14 PDS (Palette Definition Segment): YCbCr (BT.709) + Alpha -> RGBA 调色板
 * - 0x15 ODS (Object Definition Segment): 支持跨分片拼接 (First/Last sequence flag) 与 PGS RLE 位图解码
 * - 0x16 PCS (Presentation Composition Segment): 画布尺寸、Composition State、对象坐标与裁剪区域
 * - 0x17 WDS (Window Definition Segment): 窗口定义
 * - 0x80 END (End of Display Set): 提交完整渲染帧
 */

interface PgsPaletteEntry {
  r: number
  g: number
  b: number
  a: number
}

interface PgsObject {
  id: number
  width: number
  height: number
  /** 索引色调色板像素数组 (length = width * height) */
  pixels: Uint8Array
}

interface PgsCompositionObject {
  objectId: number
  windowId: number
  x: number
  y: number
  cropped: boolean
  cropX: number
  cropY: number
  cropWidth: number
  cropHeight: number
}

interface PgsWindow {
  id: number
  x: number
  y: number
  width: number
  height: number
}

export interface PgsDisplaySet {
  pts: number // 秒
  width: number
  height: number
  paletteId: number
  objects: Array<{
    imageData: ImageData
    x: number
    y: number
  }>
  isClear: boolean
}

const SEG_PDS = 0x14
const SEG_ODS = 0x15
const SEG_PCS = 0x16
const SEG_WDS = 0x17
const SEG_END = 0x80

/** 限制单字幕帧最大像素数（4K 上限），防止恶意 .sup 字幕 OOM 炸弹 */
export const MAX_PGS_FRAME_PIXELS = 4096 * 2160
const MAX_PGS_ODS_BYTES = 16 * 1024 * 1024

function isValidPgsDimensions(width: number, height: number): boolean {
  return width > 0 && height > 0 && width * height <= MAX_PGS_FRAME_PIXELS
}

function clampByte(v: number): number {
  return v < 0 ? 0 : v > 255 ? 255 : v | 0
}

/**
 * BT.709 limited-range YCbCr -> RGBA 转换
 */
function ycbcrToRgba(y: number, cr: number, cb: number, a: number): PgsPaletteEntry {
  const yNorm = y - 16
  const cbNorm = cb - 128
  const crNorm = cr - 128

  const r = clampByte(1.164383 * yNorm + 1.792741 * crNorm)
  const g = clampByte(1.164383 * yNorm - 0.213249 * cbNorm - 0.532909 * crNorm)
  const b = clampByte(1.164383 * yNorm + 2.112402 * cbNorm)
  return { r, g, b, a }
}

/**
 * 解码 PGS RLE 压缩位图数据为调色板索引数组
 */
function decodePgsRle(
  data: Uint8Array,
  width: number,
  height: number,
): Uint8Array {
  if (!isValidPgsDimensions(width, height)) {
    return new Uint8Array(0)
  }
  const pixels = new Uint8Array(width * height)
  let x = 0
  let y = 0
  let i = 0
  const len = data.length

  while (i < len && y < height) {
    const b = data[i++]
    if (b !== 0) {
      if (x < width) pixels[y * width + x] = b
      x++
      continue
    }

    if (i >= len) break
    const flag = data[i++]
    if (flag === 0) {
      // 行结束 (EOL)
      x = 0
      y++
      continue
    }

    const isLong = (flag & 0x40) !== 0
    const hasColor = (flag & 0x80) !== 0
    let runLength = flag & 0x3f

    if (isLong) {
      if (i >= len) break
      runLength = (runLength << 8) | data[i++]
    }

    let color = 0
    if (hasColor) {
      if (i >= len) break
      color = data[i++]
    }

    if (color !== 0) {
      const rowOffset = y * width
      const endX = Math.min(width, x + runLength)
      for (let px = x; px < endX; px++) {
        pixels[rowOffset + px] = color
      }
    }
    x += runLength
  }

  return pixels
}

/**
 * 解析完整 .sup PGS 二进制流为按 PTS 排序的 DisplaySet 列表
 */
export function parsePgsSup(buffer: ArrayBuffer): PgsDisplaySet[] {
  const view = new DataView(buffer)
  const bytes = new Uint8Array(buffer)
  const displaySets: PgsDisplaySet[] = []

  const palettes = new Map<number, Map<number, PgsPaletteEntry>>()
  const objects = new Map<number, PgsObject>()
  const windows = new Map<number, PgsWindow>()

  // 多分片 ODS 拼接缓冲
  const pendingOds = new Map<
    number,
    { width: number; height: number; expectedLen: number; chunks: Uint8Array[]; totalLen: number }
  >()

  let currentPcs: {
    pts: number
    width: number
    height: number
    paletteId: number
    compositionObjects: PgsCompositionObject[]
  } | null = null

  let offset = 0
  const totalBytes = bytes.length

  while (offset + 13 <= totalBytes) {
    // 检查 'PG' 魔数 (0x50 0x47)
    if (bytes[offset] !== 0x50 || bytes[offset + 1] !== 0x47) {
      offset++
      continue
    }

    const ptsTicks = view.getUint32(offset + 2, false)
    // const dtsTicks = view.getUint32(offset + 6, false)
    const segType = bytes[offset + 10]
    const segSize = view.getUint16(offset + 11, false)
    const payloadStart = offset + 13
    const payloadEnd = payloadStart + segSize

    if (payloadEnd > totalBytes) break

    const pts = ptsTicks / 90000

    if (segType === SEG_PCS && segSize >= 11) {
      const width = view.getUint16(payloadStart, false)
      const height = view.getUint16(payloadStart + 2, false)
      // frameRate = bytes[payloadStart + 4]
      // compositionNumber = view.getUint16(payloadStart + 5, false)
      const compositionState = bytes[payloadStart + 7]
      // paletteUpdateFlag = bytes[payloadStart + 8]
      const paletteId = bytes[payloadStart + 9]
      const numObjects = bytes[payloadStart + 10]

      // Epoch Start (0x80) 重置状态
      if (compositionState === 0x80) {
        palettes.clear()
        objects.clear()
        windows.clear()
        pendingOds.clear()
      }

      const compositionObjects: PgsCompositionObject[] = []
      let pos = payloadStart + 11
      for (let idx = 0; idx < numObjects && pos + 8 <= payloadEnd; idx++) {
        const objectId = view.getUint16(pos, false)
        const windowId = bytes[pos + 2]
        const croppedFlag = bytes[pos + 3]
        const x = view.getUint16(pos + 4, false)
        const y = view.getUint16(pos + 6, false)
        pos += 8

        const cropped = (croppedFlag & 0x80) !== 0
        let cropX = 0
        let cropY = 0
        let cropWidth = 0
        let cropHeight = 0

        if (cropped && pos + 8 <= payloadEnd) {
          cropX = view.getUint16(pos, false)
          cropY = view.getUint16(pos + 2, false)
          cropWidth = view.getUint16(pos + 4, false)
          cropHeight = view.getUint16(pos + 6, false)
          pos += 8
        }

        compositionObjects.push({
          objectId,
          windowId,
          x,
          y,
          cropped,
          cropX,
          cropY,
          cropWidth,
          cropHeight,
        })
      }

      currentPcs = { pts, width, height, paletteId, compositionObjects }
    } else if (segType === SEG_WDS && segSize >= 1) {
      const numWindows = bytes[payloadStart]
      let pos = payloadStart + 1
      for (let idx = 0; idx < numWindows && pos + 9 <= payloadEnd; idx++) {
        const id = bytes[pos]
        const x = view.getUint16(pos + 1, false)
        const y = view.getUint16(pos + 3, false)
        const width = view.getUint16(pos + 5, false)
        const height = view.getUint16(pos + 7, false)
        windows.set(id, { id, x, y, width, height })
        pos += 9
      }
    } else if (segType === SEG_PDS && segSize >= 2) {
      const paletteId = bytes[payloadStart]
      let palette = palettes.get(paletteId)
      if (!palette) {
        palette = new Map()
        palettes.set(paletteId, palette)
      }
      let pos = payloadStart + 2
      while (pos + 5 <= payloadEnd) {
        const entryId = bytes[pos]
        const y = bytes[pos + 1]
        const cr = bytes[pos + 2]
        const cb = bytes[pos + 3]
        const alpha = bytes[pos + 4]
        palette.set(entryId, ycbcrToRgba(y, cr, cb, alpha))
        pos += 5
      }
    } else if (segType === SEG_ODS && segSize >= 4) {
      const objectId = view.getUint16(payloadStart, false)
      // version = bytes[payloadStart + 2]
      const seqFlag = bytes[payloadStart + 3]
      const isFirst = (seqFlag & 0x80) !== 0
      const isLast = (seqFlag & 0x40) !== 0

      if (isFirst && segSize >= 11) {
        const dataLen =
          (bytes[payloadStart + 4] << 16) |
          (bytes[payloadStart + 5] << 8) |
          bytes[payloadStart + 6]
        const width = view.getUint16(payloadStart + 7, false)
        const height = view.getUint16(payloadStart + 9, false)
        const rleSlice = bytes.subarray(payloadStart + 11, payloadEnd)

        if (!isValidPgsDimensions(width, height)) {
          pendingOds.delete(objectId)
          objects.delete(objectId)
        } else if (isLast) {
          const pixels = decodePgsRle(rleSlice, width, height)
          objects.set(objectId, { id: objectId, width, height, pixels })
          pendingOds.delete(objectId)
        } else if (rleSlice.length <= MAX_PGS_ODS_BYTES) {
          pendingOds.set(objectId, {
            width,
            height,
            expectedLen: Math.max(0, dataLen - 4),
            chunks: [rleSlice],
            totalLen: rleSlice.length,
          })
        }
      } else {
        const pending = pendingOds.get(objectId)
        if (pending) {
          const rleSlice = bytes.subarray(payloadStart + 4, payloadEnd)
          if (pending.totalLen + rleSlice.length > MAX_PGS_ODS_BYTES) {
            pendingOds.delete(objectId)
          } else {
            pending.chunks.push(rleSlice)
            pending.totalLen += rleSlice.length

            if (isLast) {
              const merged = new Uint8Array(pending.totalLen)
              let writePos = 0
              for (const c of pending.chunks) {
                merged.set(c, writePos)
                writePos += c.length
              }
              const pixels = decodePgsRle(merged, pending.width, pending.height)
              objects.set(objectId, {
                id: objectId,
                width: pending.width,
                height: pending.height,
                pixels,
              })
              pendingOds.delete(objectId)
            }
          }
        }
      }
    } else if (segType === SEG_END && currentPcs) {
      const palette = palettes.get(currentPcs.paletteId)
      const renderedObjects: PgsDisplaySet['objects'] = []

      if (
        palette &&
        currentPcs.compositionObjects.length > 0 &&
        (currentPcs.width === 0 ||
          currentPcs.height === 0 ||
          isValidPgsDimensions(currentPcs.width, currentPcs.height))
      ) {
        for (const comp of currentPcs.compositionObjects) {
          const obj = objects.get(comp.objectId)
          if (!obj || !isValidPgsDimensions(obj.width, obj.height)) continue

          const srcX = comp.cropped ? comp.cropX : 0
          const srcY = comp.cropped ? comp.cropY : 0
          const drawW = comp.cropped ? Math.min(comp.cropWidth, obj.width - srcX) : obj.width
          const drawH = comp.cropped ? Math.min(comp.cropHeight, obj.height - srcY) : obj.height
          if (!isValidPgsDimensions(drawW, drawH)) continue

          const rgba = new Uint8ClampedArray(drawW * drawH * 4)
          for (let row = 0; row < drawH; row++) {
            const objRowStart = (srcY + row) * obj.width + srcX
            const outRowStart = row * drawW * 4
            for (let col = 0; col < drawW; col++) {
              const colorIdx = obj.pixels[objRowStart + col]
              const entry = palette.get(colorIdx)
              if (entry && entry.a > 0) {
                const dst = outRowStart + col * 4
                rgba[dst] = entry.r
                rgba[dst + 1] = entry.g
                rgba[dst + 2] = entry.b
                rgba[dst + 3] = entry.a
              }
            }
          }

          renderedObjects.push({
            imageData: new ImageData(rgba, drawW, drawH),
            x: comp.x,
            y: comp.y,
          })
        }
      }

      const safeWidth = isValidPgsDimensions(currentPcs.width, currentPcs.height)
        ? currentPcs.width
        : 1920
      const safeHeight = isValidPgsDimensions(currentPcs.width, currentPcs.height)
        ? currentPcs.height
        : 1080

      displaySets.push({
        pts: currentPcs.pts,
        width: safeWidth,
        height: safeHeight,
        paletteId: currentPcs.paletteId,
        objects: renderedObjects,
        isClear: renderedObjects.length === 0,
      })

      currentPcs = null
    }

    offset = payloadEnd
  }

  return displaySets.sort((a, b) => a.pts - b.pts)
}

/**
 * PGS 字幕覆盖层 Canvas 控制器
 */
export class PgsCanvasRenderer {
  private canvas: HTMLCanvasElement
  private ctx: CanvasRenderingContext2D | null
  private displaySets: PgsDisplaySet[] = []
  private lastRenderedIndex = -2
  private timeOffset = 0
  private lastTimeSec: number | null = null
  private offscreenCanvas: HTMLCanvasElement
  private offscreenCtx: CanvasRenderingContext2D | null
  private abortController: AbortController | null = null

  constructor(canvas: HTMLCanvasElement) {
    this.canvas = canvas
    this.ctx = canvas.getContext('2d')
    this.offscreenCanvas = document.createElement('canvas')
    this.offscreenCtx = this.offscreenCanvas.getContext('2d')
  }

  /**
   * 动态调整 PGS 字幕时间轴偏移（秒），并按当前时间点立即重绘
   */
  setTimeOffset(seconds: number): void {
    const next = Number.isFinite(seconds) ? seconds : 0
    if (next === this.timeOffset) return
    this.timeOffset = next
    this.lastRenderedIndex = -2
    if (this.lastTimeSec !== null) {
      this.renderAtTime(this.lastTimeSec)
    }
  }

  async loadFromUrl(url: string): Promise<number> {
    this.cancel()
    this.abortController = new AbortController()
    const res = await fetch(url, { signal: this.abortController.signal })
    if (!res.ok) {
      throw new Error(`加载 PGS 字幕失败 (${res.status})`)
    }
    const buf = await res.arrayBuffer()
    this.displaySets = parsePgsSup(buf)
    this.lastRenderedIndex = -2
    this.clear()
    return this.displaySets.length
  }

  /**
   * 二分查找当前时间点（叠加时间轴偏移）生效的 PGS DisplaySet 并绘制到 Canvas
   */
  renderAtTime(currentTimeSec: number): void {
    this.lastTimeSec = currentTimeSec
    if (!this.ctx || this.displaySets.length === 0) return

    const effectiveTime = currentTimeSec + this.timeOffset
    let lo = 0
    let hi = this.displaySets.length - 1
    let idx = -1

    while (lo <= hi) {
      const mid = (lo + hi) >>> 1
      if (this.displaySets[mid].pts <= effectiveTime) {
        idx = mid
        lo = mid + 1
      } else {
        hi = mid - 1
      }
    }

    if (idx === this.lastRenderedIndex) return
    this.lastRenderedIndex = idx

    if (idx === -1) {
      this.clear()
      return
    }

    const ds = this.displaySets[idx]
    if (ds.isClear) {
      this.clear()
      return
    }

    // 与视频坐标系对齐
    if (this.canvas.width !== ds.width || this.canvas.height !== ds.height) {
      this.canvas.width = ds.width
      this.canvas.height = ds.height
    }

    this.ctx.clearRect(0, 0, this.canvas.width, this.canvas.height)

    for (const obj of ds.objects) {
      if (!this.offscreenCtx) continue
      this.offscreenCanvas.width = obj.imageData.width
      this.offscreenCanvas.height = obj.imageData.height
      this.offscreenCtx.putImageData(obj.imageData, 0, 0)
      this.ctx.drawImage(this.offscreenCanvas, obj.x, obj.y)
    }
  }

  clear(): void {
    if (this.ctx) {
      this.ctx.clearRect(0, 0, this.canvas.width, this.canvas.height)
    }
  }

  cancel(): void {
    if (this.abortController) {
      this.abortController.abort()
      this.abortController = null
    }
  }

  destroy(): void {
    this.cancel()
    this.displaySets = []
    this.lastRenderedIndex = -2
    this.lastTimeSec = null
    this.clear()
  }
}
