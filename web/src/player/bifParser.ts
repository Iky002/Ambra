const BIF_MAGIC = new Uint8Array([0x89, 0x42, 0x49, 0x46, 0x0d, 0x0a, 0x1a, 0x0a])
const BIF_HEADER_SIZE = 64
const BIF_INDEX_ENTRY_SIZE = 8
const BIF_END_INDEX_MARKER = 0xffffffff
const MAX_BIF_FRAMES = 100_000

export interface BifFrameEntry {
  timestampMs: number
  offset: number
  length: number
}

export interface BifFrameResult {
  url: string
  index: number
  timestampSec: number
}

export class BifArchive {
  readonly version: number
  readonly timestampMultiplierMs: number
  readonly frames: ReadonlyArray<BifFrameEntry>
  private readonly data: Uint8Array
  private readonly urlCache = new Map<number, string>()
  private disposed = false

  constructor(
    data: Uint8Array,
    version: number,
    timestampMultiplierMs: number,
    frames: BifFrameEntry[],
  ) {
    this.data = data
    this.version = version
    this.timestampMultiplierMs = timestampMultiplierMs
    this.frames = frames
  }

  get frameCount(): number {
    return this.frames.length
  }

  get isDisposed(): boolean {
    return this.disposed
  }

  /**
   * 按播放秒数二分查找最接近且不超过目标时间的帧索引。
   */
  findFrameIndex(seconds: number): number {
    const count = this.frames.length
    if (count === 0) return -1
    if (!Number.isFinite(seconds) || seconds <= 0) return 0

    const targetMs = seconds * 1000
    if (targetMs < this.frames[0].timestampMs) return 0
    if (targetMs >= this.frames[count - 1].timestampMs) return count - 1

    let lo = 0
    let hi = count - 1
    while (lo <= hi) {
      const mid = (lo + hi) >>> 1
      const midTs = this.frames[mid].timestampMs
      if (midTs === targetMs) return mid
      if (midTs < targetMs) {
        lo = mid + 1
      } else {
        hi = mid - 1
      }
    }
    return Math.max(0, hi)
  }

  getFrameBytes(index: number): Uint8Array | null {
    if (this.disposed || index < 0 || index >= this.frames.length) return null
    const entry = this.frames[index]
    if (entry.length <= 0) return null
    return this.data.subarray(entry.offset, entry.offset + entry.length)
  }

  /**
   * 按秒数查找对应预览帧，惰性生成并缓存 Blob ObjectURL。
   */
  getFrameUrl(seconds: number): BifFrameResult | null {
    if (this.disposed) return null
    const idx = this.findFrameIndex(seconds)
    if (idx < 0) return null

    let url = this.urlCache.get(idx)
    if (!url) {
      const bytes = this.getFrameBytes(idx)
      if (!bytes) return null
      const copy = new Uint8Array(bytes.byteLength)
      copy.set(bytes)
      const blob = new Blob([copy], { type: 'image/jpeg' })
      url = URL.createObjectURL(blob)
      this.urlCache.set(idx, url)
    }

    return {
      url,
      index: idx,
      timestampSec: this.frames[idx].timestampMs / 1000,
    }
  }

  /**
   * 释放所有已创建的 ObjectURL，防止播放页切片或卸载产生内存泄漏。
   */
  dispose(): void {
    if (this.disposed) return
    this.disposed = true
    for (const url of this.urlCache.values()) {
      try {
        URL.revokeObjectURL(url)
      } catch {
        // 忽略释放异常
      }
    }
    this.urlCache.clear()
  }
}

/**
 * 解析 Roku / Emby 标准 BIF 二进制视频预览图归档。
 */
export function parseBif(input: ArrayBuffer | Uint8Array): BifArchive {
  const data = input instanceof Uint8Array ? input : new Uint8Array(input)
  if (data.byteLength < BIF_HEADER_SIZE) {
    throw new Error('BIF 数据长度不足（缺少 64 字节文件头）')
  }

  for (let i = 0; i < BIF_MAGIC.length; i++) {
    if (data[i] !== BIF_MAGIC[i]) {
      throw new Error('无效的 BIF 魔数头')
    }
  }

  const view = new DataView(data.buffer, data.byteOffset, data.byteLength)
  const version = view.getUint32(8, true)
  const imageCount = view.getUint32(12, true)
  const rawMultiplier = view.getUint32(16, true)
  const timestampMultiplierMs = rawMultiplier === 0 ? 1000 : rawMultiplier

  if (imageCount > MAX_BIF_FRAMES) {
    throw new Error(`BIF 帧数超限 (${imageCount})`)
  }

  const indexTableBytes = (imageCount + 1) * BIF_INDEX_ENTRY_SIZE
  if (data.byteLength < BIF_HEADER_SIZE + indexTableBytes) {
    throw new Error('BIF 索引表被截断')
  }

  const rawEntries: Array<{ tsIdx: number; offset: number }> = []
  for (let i = 0; i <= imageCount; i++) {
    const pos = BIF_HEADER_SIZE + i * BIF_INDEX_ENTRY_SIZE
    const tsIdx = view.getUint32(pos, true)
    const offset = view.getUint32(pos + 4, true)
    rawEntries.push({ tsIdx, offset })
    if (tsIdx === BIF_END_INDEX_MARKER) {
      break
    }
  }

  const dataRegionStart = BIF_HEADER_SIZE + rawEntries.length * BIF_INDEX_ENTRY_SIZE
  const MAX_BIF_FRAME_BYTES = 2 * 1024 * 1024
  const frames: BifFrameEntry[] = []
  const maxEntryIdx = Math.min(imageCount, rawEntries.length - 1)
  for (let i = 0; i < maxEntryIdx; i++) {
    const cur = rawEntries[i]
    if (cur.tsIdx === BIF_END_INDEX_MARKER) break
    const next = rawEntries[i + 1]
    if (!next || next.offset === 0 || next.offset < cur.offset) {
      continue
    }
    const endOffset = next.offset
    if (cur.offset < dataRegionStart || endOffset > data.byteLength) {
      continue
    }
    const length = endOffset - cur.offset
    if (length <= 0 || length > MAX_BIF_FRAME_BYTES) continue
    frames.push({
      timestampMs: cur.tsIdx * timestampMultiplierMs,
      offset: cur.offset,
      length,
    })
  }

  frames.sort((a, b) => a.timestampMs - b.timestampMs)

  return new BifArchive(data, version, timestampMultiplierMs, frames)
}

/**
 * 构造标准 Roku/Emby BIF v0 二进制载荷（用于 Demo Mock 与单元测试）。
 */
export function buildBifBuffer(
  frames: ReadonlyArray<{ timestampIndex: number; data: Uint8Array }>,
  timestampMultiplierMs = 1000,
): Uint8Array {
  const indexBytes = (frames.length + 1) * BIF_INDEX_ENTRY_SIZE
  const dataStart = BIF_HEADER_SIZE + indexBytes
  let totalBodyBytes = 0
  for (const f of frames) {
    totalBodyBytes += f.data.byteLength
  }

  const out = new Uint8Array(dataStart + totalBodyBytes)
  out.set(BIF_MAGIC, 0)
  const view = new DataView(out.buffer)
  view.setUint32(8, 0, true)
  view.setUint32(12, frames.length, true)
  view.setUint32(16, timestampMultiplierMs, true)

  let curOffset = dataStart
  for (let i = 0; i < frames.length; i++) {
    const pos = BIF_HEADER_SIZE + i * BIF_INDEX_ENTRY_SIZE
    view.setUint32(pos, frames[i].timestampIndex, true)
    view.setUint32(pos + 4, curOffset, true)
    out.set(frames[i].data, curOffset)
    curOffset += frames[i].data.byteLength
  }

  const endPos = BIF_HEADER_SIZE + frames.length * BIF_INDEX_ENTRY_SIZE
  view.setUint32(endPos, BIF_END_INDEX_MARKER, true)
  view.setUint32(endPos + 4, curOffset, true)
  return out
}
