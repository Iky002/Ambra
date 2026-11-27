/**
 * 纯 TypeScript 流式 EBML/MKV 解析 + HTTP Range 分块 + fMP4/MSE 封装器。
 * 无需将整文件拉入内存，突破浏览器 WASM 150MB 内存上限，支持 GB 级 MKV 边下边播与基于 Cues 的精确 Seek。
 */

import { isSameOriginUrl } from '../api/client'

// ==================== EBML / Matroska Element IDs ====================

export const EBML_ID = {
  EBML: 0x1a45dfa3,
  EBMLVersion: 0x4286,
  DocType: 0x4282,
  DocTypeVersion: 0x4287,

  Segment: 0x18538067,
  SeekHead: 0x114d9b74,
  Seek: 0x4dbb,
  SeekID: 0x53ab,
  SeekPosition: 0x53ac,

  Info: 0x1549a966,
  TimecodeScale: 0x2ad7b1,
  Duration: 0x4489,

  Tracks: 0x1654ae6b,
  TrackEntry: 0xae,
  TrackNumber: 0xd7,
  TrackUID: 0x73c5,
  TrackType: 0x83,
  CodecID: 0x86,
  CodecPrivate: 0x63a2,
  DefaultDuration: 0x23e383,
  Video: 0xe0,
  PixelWidth: 0xb0,
  PixelHeight: 0xba,
  Audio: 0xe1,
  SamplingFrequency: 0xb5,
  Channels: 0x9f,
  BitDepth: 0x6264,

  Cues: 0x1c53bb6b,
  CuePoint: 0xbb,
  CueTime: 0xb3,
  CueTrackPositions: 0xb7,
  CueTrack: 0xf7,
  CueClusterPosition: 0xf1,
  CueRelativePosition: 0xf0,

  Attachments: 0x1941a469,
  Chapters: 0x1043a770,
  Tags: 0x1254c367,
  Void: 0xec,

  Cluster: 0x1f43b675,
  Timecode: 0xe7,
  Position: 0xa7,
  PrevSize: 0xab,
  SilentTracks: 0x5854,
  SimpleBlock: 0xa3,
  BlockGroup: 0xa0,
  Block: 0xa1,
  BlockDuration: 0x9b,
  ReferenceBlock: 0xfb,
} as const

export const DEFAULT_TIMECODE_SCALE_NS = 1_000_000
export const DEFAULT_INITIAL_HEADER_BYTES = 512 * 1024 // 512 KB (bytes=0-524287)
export const DEFAULT_STREAM_CHUNK_BYTES = 2 * 1024 * 1024 // 2 MB Range chunk
export const DEFAULT_MAX_BUFFER_AHEAD_SEC = 30
export const MAX_SINGLE_FRAME_BYTES = 32 * 1024 * 1024 // 32 MB 单帧硬上限（兼容 4K UHD Remux 超大 IDR 帧）

// ==================== 类型定义 ====================

export interface VintResult {
  value: number
  length: number
  isUnknown: boolean
}

export interface EbmlElementHeader {
  id: number
  size: number
  headerSize: number
  dataOffset: number
  endOffset: number
  isUnknown: boolean
}

export interface EbmlHeaderInfo {
  docType: string
  docTypeVersion: number
  ebmlVersion: number
  headerEndOffset: number
  segmentOffset: number
  segmentDataOffset: number
  segmentSize: number
}

export interface MkvSeekEntry {
  id: number
  /** 相对于 Segment 数据起始位置的偏移 */
  position: number
  /** 文件绝对字节偏移 */
  fileOffset: number
}

export interface MkvSegmentInfo {
  timecodeScale: number
  durationRaw: number
  durationSec: number
}

export interface MkvTrackInfo {
  trackNumber: number
  trackType: 'video' | 'audio' | 'subtitle' | 'other'
  codecId: string
  codecPrivate?: Uint8Array
  defaultDurationNs?: number
  width?: number
  height?: number
  sampleRate?: number
  channels?: number
  bitDepth?: number
}

export interface MkvCuePoint {
  cueTime: number
  timeSec: number
  trackNumber: number
  /** 相对于 Segment 数据起始位置的 Cluster 偏移 */
  clusterPosition: number
  /** 文件绝对字节偏移 */
  fileOffset: number
  relativePosition?: number
}

export interface MkvFrame {
  trackNumber: number
  /** 帧时间戳（毫秒，基于 TimecodeScale） */
  timestampMs: number
  /** 帧持续时间（毫秒，可选） */
  durationMs?: number
  isKeyframe: boolean
  data: Uint8Array
}

export interface ClusterParseResult {
  frames: MkvFrame[]
  consumedBytes: number
  lastClusterTimecode: number
  /** 当前停在未收齐顶层/Cluster 子元素处所需的完整元素字节数（header + payload） */
  neededBytes?: number
}

export type StreamMkvFallbackReason =
  | 'unsupported_mse'
  | 'unsupported_codec'
  | 'invalid_mkv'
  | 'fetch_failed'
  | 'mse_error'
  | 'aborted'

export class StreamMkvFallbackError extends Error {
  readonly reason: StreamMkvFallbackReason

  constructor(reason: StreamMkvFallbackReason, message: string) {
    super(message)
    this.name = 'StreamMkvFallbackError'
    this.reason = reason
  }
}

export interface StreamMkvMetadata {
  docType: string
  durationSec: number
  totalBytes: number
  tracks: MkvTrackInfo[]
  cuesCount: number
  videoTrack?: MkvTrackInfo
  audioTrack?: MkvTrackInfo
}

export interface StreamMkvOptions {
  /** 单次 Range 拉取分块大小（默认 2MB，建议 2-4MB） */
  chunkSizeBytes?: number
  /** 首次拉取头部大小（默认 512KB） */
  initialHeaderBytes?: number
  /** 最大超前缓冲秒数，超过后暂停拉流避免内存堆积（默认 30s） */
  maxBufferAheadSec?: number
  /** 目标音轨索引（0-based 音轨序号，或回退匹配 MKV TrackNumber；未指定则默认选择第一条音轨） */
  audioTrackIndex?: number
  /** 仅解复用直通视频轨（用于 AC3/EAC3/DTS/TrueHD 等大体积 MKV 配合外挂轻量单音轨流同步播放） */
  videoOnly?: boolean
  /** 自定义 fetch 实现（方便单元测试注入） */
  fetchImpl?: typeof fetch
  /** 外部取消信号 */
  signal?: AbortSignal
  /** 进度回调 */
  onProgress?: (info: { loadedBytes: number; totalBytes: number; bufferedSec: number }) => void
  /** 运行时不可恢复错误回调（用于触发外层回退服务端 HLS） */
  onError?: (err: Error) => void
}

// ==================== EBML 基础二进制读取 ====================

/**
 * 读取 EBML 可变长整数（VINT）。
 * @param data 字节数组
 * @param offset 起始偏移
 * @param keepMarker true 时保留最高标记位（用于 Element ID）；false 时清除标记位（用于 Data Size / TrackNumber）
 */
export function readVint(data: Uint8Array, offset: number, keepMarker = false): VintResult | null {
  if (offset < 0 || offset >= data.length) return null
  const first = data[offset]
  if (first === 0) return null

  let mask = 0x80
  let length = 1
  while ((first & mask) === 0 && length <= 8) {
    mask >>= 1
    length++
  }
  if (length > 8 || offset + length > data.length) return null

  const firstPayload = first & (mask - 1)
  let value = keepMarker ? first : firstPayload
  let allOnes = !keepMarker && firstPayload === mask - 1

  for (let i = 1; i < length; i++) {
    const b = data[offset + i]
    value = value * 256 + b
    if (b !== 0xff) {
      allOnes = false
    }
  }

  if (allOnes) {
    return { value: -1, length, isUnknown: true }
  }
  return { value, length, isUnknown: false }
}

/** 读取 EBML 元素头（ID + Size） */
export function readElementHeader(data: Uint8Array, offset: number): EbmlElementHeader | null {
  const idVint = readVint(data, offset, true)
  if (!idVint) return null
  const sizeVint = readVint(data, offset + idVint.length, false)
  if (!sizeVint) return null

  const headerSize = idVint.length + sizeVint.length
  const dataOffset = offset + headerSize
  const endOffset = sizeVint.isUnknown ? data.length : dataOffset + sizeVint.value

  return {
    id: idVint.value,
    size: sizeVint.value,
    headerSize,
    dataOffset,
    endOffset,
    isUnknown: sizeVint.isUnknown,
  }
}

export function readUintBE(data: Uint8Array, offset: number, length: number): number {
  let val = 0
  const end = Math.min(data.length, offset + length)
  for (let i = offset; i < end; i++) {
    val = val * 256 + data[i]
  }
  return val
}

export function readFloatBE(data: Uint8Array, offset: number, length: number): number {
  if (offset + length > data.length) return 0
  const view = new DataView(data.buffer, data.byteOffset + offset, length)
  if (length === 4) return view.getFloat32(0, false)
  if (length === 8) return view.getFloat64(0, false)
  return 0
}

export function readAsciiString(data: Uint8Array, offset: number, length: number): string {
  const end = Math.min(data.length, offset + length)
  let out = ''
  for (let i = offset; i < end; i++) {
    if (data[i] === 0) break
    out += String.fromCharCode(data[i])
  }
  return out
}

// ==================== Matroska 元数据解析器 ====================

/**
 * 解析 EBML Header 以及紧随其后的 Segment 元素头。
 */
export function parseEbmlHeader(data: Uint8Array): EbmlHeaderInfo | null {
  const ebmlEl = readElementHeader(data, 0)
  if (!ebmlEl || ebmlEl.id !== EBML_ID.EBML || ebmlEl.isUnknown) {
    return null
  }

  let docType = ''
  let docTypeVersion = 1
  let ebmlVersion = 1

  let pos = ebmlEl.dataOffset
  const ebmlEnd = Math.min(data.length, ebmlEl.endOffset)
  while (pos < ebmlEnd) {
    const child = readElementHeader(data, pos)
    if (!child || child.isUnknown || child.endOffset > ebmlEnd) break
    if (child.id === EBML_ID.DocType) {
      docType = readAsciiString(data, child.dataOffset, child.size)
    } else if (child.id === EBML_ID.DocTypeVersion) {
      docTypeVersion = readUintBE(data, child.dataOffset, child.size)
    } else if (child.id === EBML_ID.EBMLVersion) {
      ebmlVersion = readUintBE(data, child.dataOffset, child.size)
    }
    pos = child.endOffset
  }

  let segmentOffset = ebmlEl.endOffset
  let segmentDataOffset = -1
  let segmentSize = -1

  if (segmentOffset < data.length) {
    const segEl = readElementHeader(data, segmentOffset)
    if (segEl && segEl.id === EBML_ID.Segment) {
      segmentDataOffset = segEl.dataOffset
      segmentSize = segEl.size
    } else {
      segmentOffset = -1
    }
  } else {
    segmentOffset = -1
  }

  return {
    docType,
    docTypeVersion,
    ebmlVersion,
    headerEndOffset: ebmlEl.endOffset,
    segmentOffset,
    segmentDataOffset,
    segmentSize,
  }
}

/** 解析 SeekHead，定位 Info / Tracks / Cues / Cluster 绝对偏移 */
export function parseSeekHead(data: Uint8Array, segmentDataOffset = 0): MkvSeekEntry[] {
  const entries: MkvSeekEntry[] = []
  let pos = 0
  let end = data.length

  const root = readElementHeader(data, 0)
  if (root && root.id === EBML_ID.SeekHead) {
    pos = root.dataOffset
    end = Math.min(data.length, root.endOffset)
  }

  while (pos < end) {
    const el = readElementHeader(data, pos)
    if (!el || el.isUnknown || el.endOffset > end) break
    if (el.id === EBML_ID.Seek) {
      let seekId = -1
      let seekPos = -1
      let inner = el.dataOffset
      while (inner < el.endOffset) {
        const sub = readElementHeader(data, inner)
        if (!sub || sub.isUnknown || sub.endOffset > el.endOffset) break
        if (sub.id === EBML_ID.SeekID) {
          seekId = readUintBE(data, sub.dataOffset, sub.size)
        } else if (sub.id === EBML_ID.SeekPosition) {
          seekPos = readUintBE(data, sub.dataOffset, sub.size)
        }
        inner = sub.endOffset
      }
      if (seekId >= 0 && seekPos >= 0) {
        entries.push({
          id: seekId,
          position: seekPos,
          fileOffset: segmentDataOffset + seekPos,
        })
      }
    }
    pos = el.endOffset
  }

  return entries
}

/** 解析 Segment Info（TimecodeScale 与 Duration） */
export function parseSegmentInfo(data: Uint8Array): MkvSegmentInfo {
  let timecodeScale = DEFAULT_TIMECODE_SCALE_NS
  let durationRaw = 0

  let pos = 0
  let end = data.length
  const root = readElementHeader(data, 0)
  if (root && root.id === EBML_ID.Info) {
    pos = root.dataOffset
    end = Math.min(data.length, root.endOffset)
  }

  while (pos < end) {
    const el = readElementHeader(data, pos)
    if (!el || el.isUnknown || el.endOffset > end) break
    if (el.id === EBML_ID.TimecodeScale) {
      const parsed = readUintBE(data, el.dataOffset, el.size)
      if (parsed > 0) timecodeScale = parsed
    } else if (el.id === EBML_ID.Duration) {
      durationRaw = readFloatBE(data, el.dataOffset, el.size)
    }
    pos = el.endOffset
  }

  const durationSec = durationRaw > 0 ? (durationRaw * timecodeScale) / 1_000_000_000 : 0
  return { timecodeScale, durationRaw, durationSec }
}

/**
 * 解析 Tracks 元素（可传入完整 Tracks 元素字节，或 Tracks 内部负载字节）。
 */
export function parseTracks(data: Uint8Array): MkvTrackInfo[] {
  const tracks: MkvTrackInfo[] = []
  let pos = 0
  let end = data.length

  const root = readElementHeader(data, 0)
  if (root && root.id === EBML_ID.Tracks) {
    pos = root.dataOffset
    end = Math.min(data.length, root.endOffset)
  }

  while (pos < end) {
    const entry = readElementHeader(data, pos)
    if (!entry || entry.isUnknown || entry.endOffset > end) break
    if (entry.id === EBML_ID.TrackEntry) {
      const track = parseTrackEntry(data, entry.dataOffset, entry.endOffset)
      if (track) tracks.push(track)
    }
    pos = entry.endOffset
  }

  return tracks
}

function parseTrackEntry(data: Uint8Array, start: number, end: number): MkvTrackInfo | null {
  let trackNumber = 0
  let trackTypeNum = 0
  let codecId = ''
  let codecPrivate: Uint8Array | undefined
  let defaultDurationNs: number | undefined
  let width: number | undefined
  let height: number | undefined
  let sampleRate: number | undefined
  let channels: number | undefined
  let bitDepth: number | undefined

  let pos = start
  while (pos < end) {
    const el = readElementHeader(data, pos)
    if (!el || el.isUnknown || el.endOffset > end) break

    switch (el.id) {
      case EBML_ID.TrackNumber:
        trackNumber = readUintBE(data, el.dataOffset, el.size)
        break
      case EBML_ID.TrackType:
        trackTypeNum = readUintBE(data, el.dataOffset, el.size)
        break
      case EBML_ID.CodecID:
        codecId = readAsciiString(data, el.dataOffset, el.size)
        break
      case EBML_ID.CodecPrivate:
        codecPrivate = data.slice(el.dataOffset, el.endOffset)
        break
      case EBML_ID.DefaultDuration:
        defaultDurationNs = readUintBE(data, el.dataOffset, el.size)
        break
      case EBML_ID.Video: {
        let vPos = el.dataOffset
        while (vPos < el.endOffset) {
          const vEl = readElementHeader(data, vPos)
          if (!vEl || vEl.isUnknown || vEl.endOffset > el.endOffset) break
          if (vEl.id === EBML_ID.PixelWidth) {
            width = readUintBE(data, vEl.dataOffset, vEl.size)
          } else if (vEl.id === EBML_ID.PixelHeight) {
            height = readUintBE(data, vEl.dataOffset, vEl.size)
          }
          vPos = vEl.endOffset
        }
        break
      }
      case EBML_ID.Audio: {
        let aPos = el.dataOffset
        while (aPos < el.endOffset) {
          const aEl = readElementHeader(data, aPos)
          if (!aEl || aEl.isUnknown || aEl.endOffset > el.endOffset) break
          if (aEl.id === EBML_ID.SamplingFrequency) {
            sampleRate = readFloatBE(data, aEl.dataOffset, aEl.size)
          } else if (aEl.id === EBML_ID.Channels) {
            channels = readUintBE(data, aEl.dataOffset, aEl.size)
          } else if (aEl.id === EBML_ID.BitDepth) {
            bitDepth = readUintBE(data, aEl.dataOffset, aEl.size)
          }
          aPos = aEl.endOffset
        }
        break
      }
    }

    pos = el.endOffset
  }

  if (trackNumber <= 0 || !codecId) return null

  const trackType: MkvTrackInfo['trackType'] =
    trackTypeNum === 1
      ? 'video'
      : trackTypeNum === 2
        ? 'audio'
        : trackTypeNum === 17
          ? 'subtitle'
          : 'other'

  return {
    trackNumber,
    trackType,
    codecId,
    codecPrivate,
    defaultDurationNs,
    width,
    height,
    sampleRate,
    channels,
    bitDepth,
  }
}

/**
 * 解析 Cues 索引表，建立 CueTime -> CueClusterPosition 映射用于精确 Seek。
 */
export function parseCues(
  data: Uint8Array,
  timecodeScale = DEFAULT_TIMECODE_SCALE_NS,
  segmentDataOffset = 0,
  videoTrackNumber?: number,
): MkvCuePoint[] {
  const allCues: MkvCuePoint[] = []
  let pos = 0
  let end = data.length

  const root = readElementHeader(data, 0)
  if (root && root.id === EBML_ID.Cues) {
    pos = root.dataOffset
    end = Math.min(data.length, root.endOffset)
  }

  while (pos < end) {
    const point = readElementHeader(data, pos)
    if (!point || point.isUnknown || point.endOffset > end) break

    if (point.id === EBML_ID.CuePoint) {
      let cueTime = -1
      const positions: Array<{ trackNumber: number; clusterPosition: number; relativePosition?: number }> = []

      let pPos = point.dataOffset
      while (pPos < point.endOffset) {
        const sub = readElementHeader(data, pPos)
        if (!sub || sub.isUnknown || sub.endOffset > point.endOffset) break
        if (sub.id === EBML_ID.CueTime) {
          cueTime = readUintBE(data, sub.dataOffset, sub.size)
        } else if (sub.id === EBML_ID.CueTrackPositions) {
          let trackNumber = 0
          let clusterPosition = -1
          let relativePosition: number | undefined
          let tpPos = sub.dataOffset
          while (tpPos < sub.endOffset) {
            const tpEl = readElementHeader(data, tpPos)
            if (!tpEl || tpEl.isUnknown || tpEl.endOffset > sub.endOffset) break
            if (tpEl.id === EBML_ID.CueTrack) {
              trackNumber = readUintBE(data, tpEl.dataOffset, tpEl.size)
            } else if (tpEl.id === EBML_ID.CueClusterPosition) {
              clusterPosition = readUintBE(data, tpEl.dataOffset, tpEl.size)
            } else if (tpEl.id === EBML_ID.CueRelativePosition) {
              relativePosition = readUintBE(data, tpEl.dataOffset, tpEl.size)
            }
            tpPos = tpEl.endOffset
          }
          if (clusterPosition >= 0) {
            positions.push({ trackNumber, clusterPosition, relativePosition })
          }
        }
        pPos = sub.endOffset
      }

      if (cueTime >= 0 && positions.length > 0) {
        const timeSec = (cueTime * timecodeScale) / 1_000_000_000
        for (const p of positions) {
          allCues.push({
            cueTime,
            timeSec,
            trackNumber: p.trackNumber,
            clusterPosition: p.clusterPosition,
            fileOffset: segmentDataOffset + p.clusterPosition,
            relativePosition: p.relativePosition,
          })
        }
      }
    }

    pos = point.endOffset
  }

  allCues.sort((a, b) => a.timeSec - b.timeSec)
  if (videoTrackNumber !== undefined && videoTrackNumber > 0) {
    const videoOnlyCues = allCues.filter((c) => c.trackNumber === videoTrackNumber)
    if (videoOnlyCues.length > 0) {
      return videoOnlyCues
    }
  }
  return allCues
}

/**
 * 在按时间升序排列的 Cues 索引表中二分查找 <= timeSec 的最近关键帧位置。
 */
export function lookupCueOffset(cues: MkvCuePoint[], timeSec: number): MkvCuePoint | null {
  if (!cues.length) return null
  if (timeSec <= cues[0].timeSec) return cues[0]
  if (timeSec >= cues[cues.length - 1].timeSec) return cues[cues.length - 1]

  let lo = 0
  let hi = cues.length - 1
  let best = 0

  while (lo <= hi) {
    const mid = (lo + hi) >>> 1
    if (cues[mid].timeSec <= timeSec) {
      best = mid
      lo = mid + 1
    } else {
      hi = mid - 1
    }
  }

  return cues[best]
}

// ==================== 流式 Cluster 帧解析 ====================

const VALID_CLUSTER_CHILD_IDS = new Set<number>([
  EBML_ID.Timecode,
  EBML_ID.SimpleBlock,
  EBML_ID.BlockGroup,
  EBML_ID.Position,
  EBML_ID.PrevSize,
  EBML_ID.SilentTracks,
  EBML_ID.Void,
])

/**
 * 在字节流中向前搜索下一个合法 Matroska Cluster 元素头（0x1F43B675）的偏移。
 * 用于无 Cues 线性字节 Seek 后从任意分块中间位置重新同步到最近的完整 Cluster。
 */
export function findNextClusterOffset(data: Uint8Array, startOffset = 0): number {
  const from = Math.max(0, startOffset)
  const limit = data.byteLength - 6
  for (let i = from; i <= limit; i++) {
    if (
      data[i] === 0x1f &&
      data[i + 1] === 0x43 &&
      data[i + 2] === 0xb6 &&
      data[i + 3] === 0x75
    ) {
      const el = readElementHeader(data, i)
      if (!el || el.id !== EBML_ID.Cluster) continue
      if (!el.isUnknown && el.size <= 0) continue
      if (el.dataOffset >= data.byteLength) continue
      const child = readElementHeader(data, el.dataOffset)
      if (!child || !VALID_CLUSTER_CHILD_IDS.has(child.id)) continue
      if (child.id === EBML_ID.Timecode && (child.isUnknown || child.size <= 0 || child.size > 8)) {
        continue
      }
      return i
    }
  }
  return -1
}

/**
 * 从 Range 分块中流式提取 Cluster 内的 SimpleBlock / BlockGroup 帧与时间戳。
 * 支持跨分块截断：返回 `consumedBytes` 表示已完整消费的字节数，调用方将剩余尾部字节拼到下一块头部继续解析。
 */
export function parseClusterFrames(
  data: Uint8Array,
  opts: {
    timecodeScale?: number
    clusterBaseTimecode?: number
    startOffset?: number
  } = {},
): ClusterParseResult {
  const timecodeScale = opts.timecodeScale ?? DEFAULT_TIMECODE_SCALE_NS
  const msFactor = timecodeScale / 1_000_000
  let clusterTimecode = opts.clusterBaseTimecode ?? 0
  const frames: MkvFrame[] = []

  let pos = opts.startOffset ?? 0
  let consumedBytes = pos
  let neededBytes: number | undefined

  while (pos < data.length) {
    const el = readElementHeader(data, pos)
    if (!el) {
      if (data[pos] !== 0 && data.length - pos < 16) {
        neededBytes = 16
      }
      break
    }

    if (el.id === EBML_ID.Cluster) {
      // 进入 Cluster 内部元素流式扫描（兼容 unknown size 的直播/大文件 Cluster）
      pos = el.dataOffset
      consumedBytes = pos
      continue
    }

    // 非 Cluster 容器元素且负载尚未在当前分块中完整到达 -> 记录所需字节数等待下一块拼接
    if (!el.isUnknown && el.endOffset > data.length) {
      neededBytes = el.endOffset - pos
      break
    }

    if (el.id === EBML_ID.Timecode) {
      clusterTimecode = readUintBE(data, el.dataOffset, el.size)
    } else if (el.id === EBML_ID.SimpleBlock) {
      const parsed = parseBlockPayload(
        data.subarray(el.dataOffset, el.endOffset),
        clusterTimecode,
        msFactor,
        true,
      )
      for (const f of parsed) frames.push(f)
    } else if (el.id === EBML_ID.BlockGroup) {
      let blockPayload: Uint8Array | null = null
      let durationRaw: number | undefined
      let hasReference = false

      let bgPos = el.dataOffset
      while (bgPos < el.endOffset) {
        const sub = readElementHeader(data, bgPos)
        if (!sub || sub.isUnknown || sub.endOffset > el.endOffset) break
        if (sub.id === EBML_ID.Block) {
          blockPayload = data.subarray(sub.dataOffset, sub.endOffset)
        } else if (sub.id === EBML_ID.BlockDuration) {
          durationRaw = readUintBE(data, sub.dataOffset, sub.size)
        } else if (sub.id === EBML_ID.ReferenceBlock) {
          hasReference = true
        }
        bgPos = sub.endOffset
      }

      if (blockPayload) {
        const parsed = parseBlockPayload(
          blockPayload,
          clusterTimecode,
          msFactor,
          false,
          !hasReference,
          durationRaw !== undefined ? durationRaw * msFactor : undefined,
        )
        for (const f of parsed) frames.push(f)
      }
    }

    if (el.isUnknown) {
      // 遇到非 Cluster 的未知长度容器，无法跳过，停止消费
      break
    }

    pos = el.endOffset
    consumedBytes = pos
  }

  return {
    frames,
    consumedBytes,
    lastClusterTimecode: clusterTimecode,
    neededBytes,
  }
}

function parseBlockPayload(
  block: Uint8Array,
  clusterTimecode: number,
  msFactor: number,
  isSimpleBlock: boolean,
  forcedKeyframe?: boolean,
  durationMs?: number,
): MkvFrame[] {
  const trackVint = readVint(block, 0, false)
  if (!trackVint || trackVint.isUnknown) return []

  const headerEnd = trackVint.length + 3
  if (block.length < headerEnd) return []

  const view = new DataView(block.buffer, block.byteOffset, block.byteLength)
  const relTime = view.getInt16(trackVint.length, false)
  const flags = block[trackVint.length + 2]

  const isKeyframe = isSimpleBlock ? (flags & 0x80) !== 0 : Boolean(forcedKeyframe)
  const lacing = (flags >> 1) & 0x03
  const timestampMs = Math.max(0, (clusterTimecode + relTime) * msFactor)

  if (lacing === 0) {
    return [
      {
        trackNumber: trackVint.value,
        timestampMs,
        durationMs,
        isKeyframe,
        data: block.slice(headerEnd),
      },
    ]
  }

  // 支持常见 Lacing (1=Xiph, 2=Fixed-size, 3=EBML)
  if (headerEnd >= block.length) return []
  const frameCount = block[headerEnd] + 1
  let offset = headerEnd + 1
  const sizes: number[] = []

  if (lacing === 2) {
    // Fixed-size lacing
    const remaining = block.length - offset
    const frameSize = Math.floor(remaining / frameCount)
    for (let i = 0; i < frameCount; i++) sizes.push(frameSize)
  } else if (lacing === 1) {
    // Xiph lacing
    let total = 0
    for (let i = 0; i < frameCount - 1; i++) {
      let sz = 0
      while (offset < block.length) {
        const b = block[offset++]
        sz += b
        if (b !== 0xff) break
      }
      sizes.push(sz)
      total += sz
    }
    sizes.push(Math.max(0, block.length - offset - total))
  } else if (lacing === 3) {
    // EBML lacing
    const firstSize = readVint(block, offset, false)
    if (!firstSize || firstSize.isUnknown) return []
    offset += firstSize.length
    let prevSize = firstSize.value
    sizes.push(prevSize)
    let total = prevSize

    for (let i = 1; i < frameCount - 1; i++) {
      const deltaVint = readVint(block, offset, false)
      if (!deltaVint || deltaVint.isUnknown) return []
      offset += deltaVint.length
      // Signed EBML VINT bias = 2^(7*len - 1) - 1
      const bias = Math.pow(2, 7 * deltaVint.length - 1) - 1
      prevSize += deltaVint.value - bias
      sizes.push(prevSize)
      total += prevSize
    }
    sizes.push(Math.max(0, block.length - offset - total))
  }

  const out: MkvFrame[] = []
  const perFrameDur = durationMs !== undefined ? durationMs / frameCount : undefined
  for (let i = 0; i < sizes.length && offset < block.length; i++) {
    const sz = Math.min(sizes[i], block.length - offset)
    out.push({
      trackNumber: trackVint.value,
      timestampMs: perFrameDur !== undefined ? timestampMs + i * perFrameDur : timestampMs,
      durationMs: perFrameDur,
      isKeyframe: i === 0 ? isKeyframe : false,
      data: block.slice(offset, offset + sz),
    })
    offset += sz
  }

  return out
}

// ==================== ISO BMFF (fMP4) 封装器 ====================

function concatBytes(...parts: Uint8Array[]): Uint8Array {
  let total = 0
  for (const p of parts) total += p.byteLength
  const out = new Uint8Array(total)
  let offset = 0
  for (const p of parts) {
    out.set(p, offset)
    offset += p.byteLength
  }
  return out
}

function asciiBytes(str: string): Uint8Array {
  const out = new Uint8Array(str.length)
  for (let i = 0; i < str.length; i++) {
    out[i] = str.charCodeAt(i) & 0xff
  }
  return out
}

function makeBox(type: string, ...payloads: Uint8Array[]): Uint8Array {
  let payloadSize = 0
  for (const p of payloads) payloadSize += p.byteLength
  const totalSize = 8 + payloadSize
  const box = new Uint8Array(totalSize)
  const view = new DataView(box.buffer)
  view.setUint32(0, totalSize, false)
  box.set(asciiBytes(type.slice(0, 4)), 4)
  let offset = 8
  for (const p of payloads) {
    box.set(p, offset)
    offset += p.byteLength
  }
  return box
}

function makeFullBox(type: string, version: number, flags: number, ...payloads: Uint8Array[]): Uint8Array {
  const vf = new Uint8Array(4)
  vf[0] = version & 0xff
  vf[1] = (flags >> 16) & 0xff
  vf[2] = (flags >> 8) & 0xff
  vf[3] = flags & 0xff
  return makeBox(type, vf, ...payloads)
}

function buildFtypBox(): Uint8Array {
  const minorVersion = new Uint8Array([0, 0, 2, 0])
  return makeBox(
    'ftyp',
    asciiBytes('isom'),
    minorVersion,
    asciiBytes('isom'),
    asciiBytes('iso5'),
    asciiBytes('iso6'),
    asciiBytes('mp41'),
  )
}

function buildMvhdBox(timescale: number, durationTicks: number, nextTrackId: number): Uint8Array {
  const body = new Uint8Array(96)
  const v = new DataView(body.buffer)
  v.setUint32(0, 0, false) // creation_time
  v.setUint32(4, 0, false) // modification_time
  v.setUint32(8, timescale, false)
  v.setUint32(12, Math.min(0xffffffff, Math.max(0, Math.round(durationTicks))), false)
  v.setUint32(16, 0x00010000, false) // rate = 1.0
  v.setUint16(20, 0x0100, false) // volume = 1.0
  // unity matrix (36 bytes at offset 32)
  v.setUint32(32, 0x00010000, false)
  v.setUint32(48, 0x00010000, false)
  v.setUint32(64, 0x40000000, false)
  v.setUint32(92, nextTrackId, false)
  return makeFullBox('mvhd', 0, 0, body)
}

function buildEsdsBox(audioConfig?: Uint8Array): Uint8Array {
  const cfg = audioConfig && audioConfig.byteLength > 0 ? audioConfig : new Uint8Array([0x12, 0x10]) // AAC-LC 44.1kHz Stereo fallback
  const decSpecificLen = 2 + cfg.byteLength
  const decConfigLen = 2 + 13 + decSpecificLen
  const esDescLen = 2 + 3 + decConfigLen + 3

  const out = new Uint8Array(esDescLen)
  let o = 0
  // ES_Descriptor
  out[o++] = 0x03
  out[o++] = esDescLen - 2
  out[o++] = 0x00
  out[o++] = 0x01 // ES_ID
  out[o++] = 0x00 // flags
  // DecoderConfigDescriptor
  out[o++] = 0x04
  out[o++] = decConfigLen - 2
  out[o++] = 0x40 // MPEG-4 Audio
  out[o++] = 0x15 // audio stream
  out[o++] = 0x00
  out[o++] = 0x00
  out[o++] = 0x00 // bufferSizeDB
  out[o++] = 0x00
  out[o++] = 0x02
  out[o++] = 0xee
  out[o++] = 0x00 // maxBitrate 192kbps
  out[o++] = 0x00
  out[o++] = 0x02
  out[o++] = 0xee
  out[o++] = 0x00 // avgBitrate
  // DecoderSpecificInfo
  out[o++] = 0x05
  out[o++] = cfg.byteLength
  out.set(cfg, o)
  o += cfg.byteLength
  // SLConfigDescriptor
  out[o++] = 0x06
  out[o++] = 0x01
  out[o++] = 0x02

  return makeFullBox('esds', 0, 0, out)
}

export function buildAudioSampleEntry(track: MkvTrackInfo): Uint8Array {
  const codecUpper = track.codecId.toUpperCase()
  const sampleRate = Math.round(track.sampleRate || 48000)
  const channels = track.channels || 2
  const entryHeader = new Uint8Array(28)
  const v = new DataView(entryHeader.buffer)
  v.setUint16(6, 1, false) // data_reference_index
  v.setUint16(16, channels, false)
  v.setUint16(18, track.bitDepth || 16, false)
  v.setUint32(24, (sampleRate << 16) >>> 0, false)

  if (codecUpper.includes('OPUS')) {
    const cp = track.codecPrivate
    const hasOpusMagic =
      cp !== undefined && cp.byteLength >= 19 && readAsciiString(cp, 0, 8) === 'OpusHead'

    if (hasOpusMagic && cp) {
      const cpView = new DataView(cp.buffer, cp.byteOffset, cp.byteLength)
      const outChannels = cp[9] || channels
      if (outChannels <= 0) {
        throw new StreamMkvFallbackError('unsupported_codec', 'Opus 声道数无效')
      }
      const preSkip = cpView.getUint16(10, true)
      const inputSampleRate = cpView.getUint32(12, true) || sampleRate
      const outputGain = cpView.getInt16(16, true)
      const family = cp[18]

      v.setUint16(16, outChannels, false)

      if (family === 0) {
        if (outChannels > 2) {
          throw new StreamMkvFallbackError(
            'unsupported_codec',
            `Opus MappingFamily 0 不支持 ${outChannels} 声道`,
          )
        }
        const dOpsBody = new Uint8Array(11)
        const dv = new DataView(dOpsBody.buffer)
        dOpsBody[0] = 0 // ISO BMFF dOps Version = 0
        dOpsBody[1] = outChannels
        dv.setUint16(2, preSkip, false) // Big-Endian
        dv.setUint32(4, inputSampleRate, false) // Big-Endian
        dv.setInt16(8, outputGain, false) // Big-Endian
        dOpsBody[10] = 0
        return makeBox('Opus', entryHeader, makeBox('dOps', dOpsBody))
      }

      const requiredLen = 21 + outChannels
      if (cp.byteLength < requiredLen) {
        throw new StreamMkvFallbackError(
          'unsupported_codec',
          'Opus 多声道 OpusHead 缺少完整的声道映射表',
        )
      }
      const streamCount = cp[19]
      const coupledCount = cp[20]
      if (streamCount <= 0) {
        throw new StreamMkvFallbackError('unsupported_codec', 'Opus StreamCount 无效')
      }
      const dOpsBody = new Uint8Array(13 + outChannels)
      const dv = new DataView(dOpsBody.buffer)
      dOpsBody[0] = 0 // ISO BMFF dOps Version = 0
      dOpsBody[1] = outChannels
      dv.setUint16(2, preSkip, false) // Big-Endian
      dv.setUint32(4, inputSampleRate, false) // Big-Endian
      dv.setInt16(8, outputGain, false) // Big-Endian
      dOpsBody[10] = family
      dOpsBody[11] = streamCount
      dOpsBody[12] = coupledCount
      dOpsBody.set(cp.subarray(21, 21 + outChannels), 13)
      return makeBox('Opus', entryHeader, makeBox('dOps', dOpsBody))
    }

    if (channels > 2) {
      throw new StreamMkvFallbackError(
        'unsupported_codec',
        `多声道 Opus (${channels}ch) 缺少合法 OpusHead，回退服务端 HLS`,
      )
    }

    const dOpsBody = new Uint8Array(11)
    const dv = new DataView(dOpsBody.buffer)
    dOpsBody[0] = 0 // Version
    dOpsBody[1] = channels
    dv.setUint16(2, 312, false) // PreSkip
    dv.setUint32(4, sampleRate, false)
    dv.setInt16(8, 0, false) // OutputGain
    dOpsBody[10] = 0 // ChannelMappingFamily
    return makeBox('Opus', entryHeader, makeBox('dOps', dOpsBody))
  }

  return makeBox('mp4a', entryHeader, buildEsdsBox(track.codecPrivate))
}

function buildSampleEntryBox(track: MkvTrackInfo): Uint8Array {
  const codecUpper = track.codecId.toUpperCase()
  if (track.trackType === 'video') {
    const width = track.width || 1920
    const height = track.height || 1080
    const entryHeader = new Uint8Array(78)
    const v = new DataView(entryHeader.buffer)
    v.setUint16(6, 1, false) // data_reference_index
    v.setUint16(24, width, false)
    v.setUint16(26, height, false)
    v.setUint32(28, 0x00480000, false) // horizresolution 72 dpi
    v.setUint32(32, 0x00480000, false) // vertresolution 72 dpi
    v.setUint16(40, 1, false) // frame_count
    v.setUint16(74, 0x0018, false) // depth
    v.setInt16(76, -1, false) // pre_defined

    const isHevc = codecUpper.includes('HEVC') || codecUpper.includes('H265')
    const boxType = isHevc ? 'hvc1' : 'avc1'
    const cfgType = isHevc ? 'hvcC' : 'avcC'
    const cfgData =
      track.codecPrivate && track.codecPrivate.byteLength > 0
        ? track.codecPrivate
        : new Uint8Array([0x01, 0x64, 0x00, 0x28, 0xff, 0xe1, 0x00, 0x00, 0x01, 0x00, 0x00])
    const cfgBox = makeBox(cfgType, cfgData)
    return makeBox(boxType, entryHeader, cfgBox)
  }

  return buildAudioSampleEntry(track)
}

function buildTrakBox(track: MkvTrackInfo, timescale: number, durationTicks: number): Uint8Array {
  const isVideo = track.trackType === 'video'

  // tkhd (version 0, flags = 3: enabled + in_movie)
  const tkhdBody = new Uint8Array(80)
  const tv = new DataView(tkhdBody.buffer)
  tv.setUint32(8, track.trackNumber, false) // track_ID
  tv.setUint32(16, Math.min(0xffffffff, Math.max(0, Math.round(durationTicks))), false)
  tv.setUint16(32, isVideo ? 0 : 0x0100, false) // volume
  // unity matrix at offset 36
  tv.setUint32(36, 0x00010000, false)
  tv.setUint32(52, 0x00010000, false)
  tv.setUint32(68, 0x40000000, false)
  if (isVideo) {
    tv.setUint32(72, ((track.width || 1920) << 16) >>> 0, false)
    tv.setUint32(76, ((track.height || 1080) << 16) >>> 0, false)
  }
  const tkhd = makeFullBox('tkhd', 0, 3, tkhdBody)

  // mdhd
  const mdhdBody = new Uint8Array(20)
  const mv = new DataView(mdhdBody.buffer)
  mv.setUint32(8, timescale, false)
  mv.setUint32(12, Math.min(0xffffffff, Math.max(0, Math.round(durationTicks))), false)
  mv.setUint16(16, 0x55c4, false) // 'und'
  const mdhd = makeFullBox('mdhd', 0, 0, mdhdBody)

  // hdlr
  const handlerType = isVideo ? 'vide' : 'soun'
  const handlerName = asciiBytes(isVideo ? 'VideoHandler\0' : 'SoundHandler\0')
  const hdlrFixed = new Uint8Array(20)
  hdlrFixed.set(asciiBytes(handlerType), 4)
  const hdlr = makeFullBox('hdlr', 0, 0, hdlrFixed, handlerName)

  // vmhd / smhd
  const mediaHeader = isVideo
    ? makeFullBox('vmhd', 0, 1, new Uint8Array(8))
    : makeFullBox('smhd', 0, 0, new Uint8Array(4))

  // dinf -> dref -> url
  const urlBox = makeFullBox('url ', 0, 1)
  const drefCount = new Uint8Array([0, 0, 0, 1])
  const dinf = makeBox('dinf', makeFullBox('dref', 0, 0, drefCount, urlBox))

  // stbl
  const stsdCount = new Uint8Array([0, 0, 0, 1])
  const stsd = makeFullBox('stsd', 0, 0, stsdCount, buildSampleEntryBox(track))
  const emptyCount = new Uint8Array(4)
  const stts = makeFullBox('stts', 0, 0, emptyCount)
  const stsc = makeFullBox('stsc', 0, 0, emptyCount)
  const stsz = makeFullBox('stsz', 0, 0, new Uint8Array(8))
  const stco = makeFullBox('stco', 0, 0, emptyCount)
  const stbl = makeBox('stbl', stsd, stts, stsc, stsz, stco)

  const minf = makeBox('minf', mediaHeader, dinf, stbl)
  const mdia = makeBox('mdia', mdhd, hdlr, minf)
  return makeBox('trak', tkhd, mdia)
}

function buildTrexBox(trackId: number): Uint8Array {
  const body = new Uint8Array(20)
  const v = new DataView(body.buffer)
  v.setUint32(0, trackId, false)
  v.setUint32(4, 1, false) // default_sample_description_index
  return makeFullBox('trex', 0, 0, body)
}

/**
 * 构建 fMP4 初始化分片（`ftyp + moov`），供 `SourceBuffer.appendBuffer` 使用。
 */
export function buildFmp4InitSegment(
  tracks: MkvTrackInfo[],
  opts: { durationSec?: number; timescale?: number } = {},
): Uint8Array {
  const timescale = opts.timescale ?? 1000
  const durationTicks = Math.round((opts.durationSec ?? 0) * timescale)
  const maxTrackId = tracks.reduce((m, t) => Math.max(m, t.trackNumber), 0)

  const mvhd = buildMvhdBox(timescale, durationTicks, maxTrackId + 1)
  const traks = tracks.map((t) => buildTrakBox(t, timescale, durationTicks))
  const mvex = makeBox('mvex', ...tracks.map((t) => buildTrexBox(t.trackNumber)))
  const moov = makeBox('moov', mvhd, ...traks, mvex)

  return concatBytes(buildFtypBox(), moov)
}

/**
 * 构建 fMP4 媒体分片（`moof + mdat`），将一组 MKV 帧封装为标准 ISO BMFF 分片。
 * 自动识别 B 帧（PTS 非单调重排）并推导单调 DTS 与有符号 CTS（`trun` v1 `sample_composition_time_offset`），
 * 避免画面抖动与倒退。
 */
export function buildFmp4MediaSegment(
  frames: MkvFrame[],
  opts: {
    sequenceNumber: number
    trackId: number
    timescale?: number
    defaultSampleDuration?: number
    baseMediaDecodeTime?: number
  },
): Uint8Array {
  const timescale = opts.timescale ?? 1000
  const scaleFactor = timescale / 1000
  const defaultDur = opts.defaultSampleDuration ?? Math.max(1, Math.round(40 * scaleFactor))
  const sampleCount = frames.length

  const ptsTicks = frames.map((f) => Math.max(0, Math.round(f.timestampMs * scaleFactor)))
  const hasReordering = ptsTicks.some((p, i) => i > 0 && p < ptsTicks[i - 1])

  // 估算帧间隔步长（优先取排序后相邻正 PTS 差值的最小值，防止 B 帧跨度污染）
  let inferredStep = defaultDur
  if (sampleCount > 1) {
    const sortedForStep = [...ptsTicks].sort((a, b) => a - b)
    let minPosDelta = Number.POSITIVE_INFINITY
    for (let i = 1; i < sortedForStep.length; i++) {
      const d = sortedForStep[i] - sortedForStep[i - 1]
      if (d > 0 && d < minPosDelta) {
        minPosDelta = d
      }
    }
    if (Number.isFinite(minPosDelta) && minPosDelta > 0) {
      inferredStep = minPosDelta
    }
  }

  const dtsTicks = new Array<number>(sampleCount)
  const ctsOffsets = new Array<number>(sampleCount)
  const sampleDurs = new Array<number>(sampleCount)

  if (sampleCount > 0) {
    if (hasReordering) {
      const sortedPts = [...ptsTicks].sort((a, b) => a - b)
      dtsTicks[0] =
        opts.baseMediaDecodeTime !== undefined
          ? Math.max(0, Math.round(opts.baseMediaDecodeTime))
          : sortedPts[0]
      for (let i = 1; i < sampleCount; i++) {
        if (opts.baseMediaDecodeTime !== undefined) {
          const delta = sortedPts[i] - sortedPts[i - 1]
          dtsTicks[i] = dtsTicks[i - 1] + (delta > 0 ? delta : inferredStep)
        } else {
          dtsTicks[i] = sortedPts[i] > dtsTicks[i - 1] ? sortedPts[i] : dtsTicks[i - 1] + inferredStep
        }
      }
      for (let i = 0; i < sampleCount; i++) {
        ctsOffsets[i] = ptsTicks[i] - dtsTicks[i]
      }
      for (let i = 0; i < sampleCount; i++) {
        if (i + 1 < sampleCount) {
          sampleDurs[i] = Math.max(1, dtsTicks[i + 1] - dtsTicks[i])
        } else {
          const f = frames[i]
          sampleDurs[i] =
            f.durationMs !== undefined && f.durationMs > 0
              ? Math.max(1, Math.round(f.durationMs * scaleFactor))
              : inferredStep
        }
      }
    } else {
      dtsTicks[0] =
        opts.baseMediaDecodeTime !== undefined
          ? Math.max(0, Math.round(opts.baseMediaDecodeTime))
          : ptsTicks[0]
      for (let i = 1; i < sampleCount; i++) {
        dtsTicks[i] = ptsTicks[i] > dtsTicks[i - 1] ? ptsTicks[i] : dtsTicks[i - 1] + inferredStep
      }
      for (let i = 0; i < sampleCount; i++) {
        ctsOffsets[i] = ptsTicks[i] - dtsTicks[i]
      }
      for (let i = 0; i < sampleCount; i++) {
        const f = frames[i]
        if (f.durationMs !== undefined && f.durationMs > 0) {
          sampleDurs[i] = Math.max(1, Math.round(f.durationMs * scaleFactor))
        } else if (i + 1 < sampleCount && dtsTicks[i + 1] > dtsTicks[i]) {
          sampleDurs[i] = Math.max(1, dtsTicks[i + 1] - dtsTicks[i])
        } else {
          sampleDurs[i] = defaultDur
        }
      }
    }
  }

  const baseDecodeTime =
    sampleCount > 0
      ? dtsTicks[0]
      : opts.baseMediaDecodeTime !== undefined
        ? Math.max(0, Math.round(opts.baseMediaDecodeTime))
        : 0

  const includeCts = hasReordering || ctsOffsets.some((c) => c !== 0)

  // mfhd
  const mfhdBody = new Uint8Array(4)
  new DataView(mfhdBody.buffer).setUint32(0, opts.sequenceNumber, false)
  const mfhd = makeFullBox('mfhd', 0, 0, mfhdBody)

  // tfhd (flags = 0x020000: default-base-is-moof)
  const tfhdBody = new Uint8Array(4)
  new DataView(tfhdBody.buffer).setUint32(0, opts.trackId, false)
  const tfhd = makeFullBox('tfhd', 0, 0x020000, tfhdBody)

  // tfdt (version 1, 64-bit baseMediaDecodeTime)
  const tfdtBody = new Uint8Array(8)
  const tfdtView = new DataView(tfdtBody.buffer)
  const hi = Math.floor(baseDecodeTime / 0x100000000)
  const lo = baseDecodeTime >>> 0
  tfdtView.setUint32(0, hi, false)
  tfdtView.setUint32(4, lo, false)
  const tfdt = makeFullBox('tfdt', 1, 0, tfdtBody)

  // trun:
  // - 含 B 帧 / 非零 CTS 时使用 version 1 + flags 0x000f01（支持有符号 int32 sample_composition_time_offset）
  // - 纯顺序帧使用 version 0 + flags 0x000701
  const trunVersion = includeCts ? 1 : 0
  const trunFlags = includeCts ? 0x000f01 : 0x000701
  const bytesPerSample = includeCts ? 16 : 12
  const trunBody = new Uint8Array(8 + sampleCount * bytesPerSample)
  const trunView = new DataView(trunBody.buffer)
  trunView.setUint32(0, sampleCount, false)
  // data_offset placeholder at offset 4, will be patched once moof size is known

  let cursor = 8
  for (let i = 0; i < sampleCount; i++) {
    const f = frames[i]
    const sampleFlags = f.isKeyframe ? 0x02000000 : 0x01010000

    trunView.setUint32(cursor, sampleDurs[i], false)
    trunView.setUint32(cursor + 4, f.data.byteLength, false)
    trunView.setUint32(cursor + 8, sampleFlags, false)
    if (includeCts) {
      trunView.setInt32(cursor + 12, ctsOffsets[i], false)
    }
    cursor += bytesPerSample
  }

  const trun = makeFullBox('trun', trunVersion, trunFlags, trunBody)
  const traf = makeBox('traf', tfhd, tfdt, trun)
  const moof = makeBox('moof', mfhd, traf)

  // Patch trun data_offset: moof.byteLength + 8 (mdat header size)
  // trun starts at: 8 (moof hdr) + mfhd.byteLength + 8 (traf hdr) + tfhd.byteLength + tfdt.byteLength
  const trunOffsetInMoof = 8 + mfhd.byteLength + 8 + tfhd.byteLength + tfdt.byteLength
  // Inside trun: 8 (box hdr) + 4 (version/flags) + 4 (sample_count) -> +16 is data_offset
  new DataView(moof.buffer).setInt32(trunOffsetInMoof + 16, moof.byteLength + 8, false)

  const mdat = makeBox('mdat', ...frames.map((f) => f.data))
  return concatBytes(moof, mdat)
}

// ==================== 编码映射与能力探测 ====================

export function isMkvContainer(container?: string): boolean {
  if (!container) return false
  const tokens = container
    .toLowerCase()
    .split(',')
    .map((t) => t.trim())
  return tokens.includes('mkv') || tokens.includes('matroska')
}

/**
 * 解析 ISO/IEC 14496-15 Annex E.3 HEVCDecoderConfigurationRecord (hvcC)，
 * 生成精准的 `hvc1.<profile_space><profile_idc>.<compat_hex>.<tier><level_idc>.<constraints>` 字符串，
 * 使 Main 10 (10-bit HDR/DV) 视频正确声明为 `hvc1.2.4.L...` 而非硬编码 8-bit `hvc1.1.6.L120.90`。
 */
export function buildHevcMimeCodec(codecPrivate?: Uint8Array): string {
  if (!codecPrivate || codecPrivate.byteLength < 13 || codecPrivate[0] !== 1) {
    return 'hvc1.1.6.L120.90'
  }
  const profileSpaceIdx = (codecPrivate[1] >> 6) & 0x03
  const profileSpace = ['', 'A', 'B', 'C'][profileSpaceIdx] ?? ''
  const tier = ((codecPrivate[1] >> 5) & 0x01) !== 0 ? 'H' : 'L'
  const profileIdc = codecPrivate[1] & 0x1f

  let compatReversed = 0
  for (let bit = 0; bit < 32; bit++) {
    const srcByte = codecPrivate[2 + (bit >> 3)]
    const srcBit = (srcByte >> (7 - (bit & 7))) & 1
    if (srcBit) {
      compatReversed = (compatReversed | ((1 << bit) >>> 0)) >>> 0
    }
  }
  if (compatReversed === 0 && profileIdc > 0 && profileIdc < 32) {
    compatReversed = (1 << profileIdc) >>> 0
  }
  const compatHex = (compatReversed >>> 0).toString(16).toUpperCase()

  const levelIdc = codecPrivate[12] || 120

  let constraintEnd = 11
  while (constraintEnd >= 6 && codecPrivate[constraintEnd] === 0) {
    constraintEnd--
  }
  const constraintParts: string[] = []
  for (let i = 6; i <= constraintEnd; i++) {
    constraintParts.push(codecPrivate[i].toString(16).toUpperCase())
  }
  const constraintSuffix = constraintParts.length > 0 ? `.${constraintParts.join('.')}` : ''

  return `hvc1.${profileSpace}${profileIdc}.${compatHex}.${tier}${levelIdc}${constraintSuffix}`
}

function mapVideoCodecToMime(codec?: string, codecPrivate?: Uint8Array): string | null {
  if (!codec) return null
  const c = codec.trim().toLowerCase()
  if (c === 'h264' || c === 'avc' || c === 'avc1' || c === 'v_mpeg4/iso/avc') {
    if (codecPrivate && codecPrivate.byteLength >= 4 && codecPrivate[0] === 1) {
      const hex = (n: number) => n.toString(16).padStart(2, '0')
      return `avc1.${hex(codecPrivate[1])}${hex(codecPrivate[2])}${hex(codecPrivate[3])}`
    }
    return 'avc1.640028'
  }
  if (c === 'hevc' || c === 'h265' || c === 'hvc1' || c === 'hev1' || c === 'v_mpegh/iso/hevc') {
    return buildHevcMimeCodec(codecPrivate)
  }
  return null
}

function mapAudioCodecToMime(codec?: string): string | null {
  if (!codec) return null
  const c = codec.trim().toLowerCase()
  if (c === 'aac' || c === 'mp4a' || c === 'a_aac' || c.startsWith('a_aac/')) {
    return 'mp4a.40.2'
  }
  if (c === 'opus' || c === 'a_opus') {
    return 'opus'
  }
  return null
}

/** 根据解析出的 MKV 轨信息生成精准 MSE MIME 字符串 */
export function resolveMkvMimeType(
  videoTrack?: MkvTrackInfo,
  audioTrack?: MkvTrackInfo,
): { videoMime: string | null; audioMime: string | null; combinedMime: string | null } {
  const vCodec = videoTrack ? mapVideoCodecToMime(videoTrack.codecId, videoTrack.codecPrivate) : null
  const aCodec = audioTrack ? mapAudioCodecToMime(audioTrack.codecId) : null

  const videoMime = vCodec ? `video/mp4; codecs="${vCodec}"` : null
  const audioMime = aCodec ? `audio/mp4; codecs="${aCodec}"` : null

  const codecs: string[] = []
  if (vCodec) codecs.push(vCodec)
  if (aCodec) codecs.push(aCodec)
  const combinedMime = codecs.length > 0 ? `video/mp4; codecs="${codecs.join(', ')}"` : null

  return { videoMime, audioMime, combinedMime }
}

/**
 * 检测当前浏览器是否支持通过 MSE 流式解复用并播放指定的视频/音频编码组合。
 * 遇到不支持的视频或音频编码（如 AC3/DTS/MPEG2/VC1 等）直接返回 false，由调用方自动回退服务端 HLS。
 */
export function canStreamMkvRemux(
  videoCodec?: string,
  audioCodec?: string,
  container?: string,
): boolean {
  if (container !== undefined && !isMkvContainer(container)) {
    return false
  }
  if (typeof MediaSource === 'undefined' || typeof MediaSource.isTypeSupported !== 'function') {
    return false
  }

  const vCodec = mapVideoCodecToMime(videoCodec)
  if (!vCodec) return false

  let aCodec: string | null = null
  if (audioCodec && audioCodec.trim() !== '') {
    aCodec = mapAudioCodecToMime(audioCodec)
    if (!aCodec) return false
  }

  const videoMime = `video/mp4; codecs="${vCodec}"`
  if (!MediaSource.isTypeSupported(videoMime)) {
    return false
  }
  if (aCodec) {
    const audioMime = `audio/mp4; codecs="${aCodec}"`
    const combinedMime = `video/mp4; codecs="${vCodec}, ${aCodec}"`
    if (!MediaSource.isTypeSupported(audioMime) && !MediaSource.isTypeSupported(combinedMime)) {
      return false
    }
  }
  return true
}

/**
 * 检测当前浏览器是否支持通过 MSE 仅流式解复用直通 MKV 的视频轨（忽略不兼容音轨，供双流混合播放模式使用）。
 */
export function canStreamMkvVideoOnly(videoCodec?: string, container?: string): boolean {
  if (container !== undefined && !isMkvContainer(container)) {
    return false
  }
  if (typeof MediaSource === 'undefined' || typeof MediaSource.isTypeSupported !== 'function') {
    return false
  }
  const vCodec = mapVideoCodecToMime(videoCodec)
  if (!vCodec) return false
  return MediaSource.isTypeSupported(`video/mp4; codecs="${vCodec}"`)
}

export function parseContentRangeTotal(header: string | null): number {
  if (!header) return 0
  const slashIdx = header.lastIndexOf('/')
  if (slashIdx < 0) return 0
  const totalStr = header.slice(slashIdx + 1).trim()
  if (totalStr === '*') return 0
  const total = Number(totalStr)
  return Number.isFinite(total) && total > 0 ? total : 0
}

// ==================== StreamMkvRemuxer 控制器 ====================

/**
 * 浏览器大体积 MKV 流式解复用控制器（HTTP Range + 纯 TS EBML/fMP4 + MSE）。
 * 1) 仅拉取前 512KB 解析 EBML Header / SeekHead / Info / Tracks / Cues；
 * 2) 若 Cues 在文件末尾，按 SeekHead 偏移单独发起小 Range 请求拉取；
 * 3) 按 2–4MB 分块流式拉取 Cluster 封装为 fMP4 喂给 MSE，带缓冲背压控制；
 * 4) 监听 videoEl 的 seeking 事件，通过 Cues 二分查找目标字节偏移发起新 Range 请求。
 */
export class StreamMkvRemuxer {
  private mediaSource: MediaSource | null = null
  private objectUrl: string | null = null
  private videoEl: HTMLVideoElement | null = null
  private videoSb: SourceBuffer | null = null
  private audioSb: SourceBuffer | null = null

  private streamUrl = ''
  private authHeaders: Record<string, string> = {}
  private fetchImpl: typeof fetch = (...args) => fetch(...args)
  private chunkSizeBytes = DEFAULT_STREAM_CHUNK_BYTES
  private maxBufferAheadSec = DEFAULT_MAX_BUFFER_AHEAD_SEC
  private onProgress?: StreamMkvOptions['onProgress']
  private onError?: StreamMkvOptions['onError']

  private totalBytes = 0
  private segmentDataOffset = 0
  private firstClusterOffset = -1
  private timecodeScale = DEFAULT_TIMECODE_SCALE_NS
  private durationSec = 0
  private videoTrack?: MkvTrackInfo
  private audioTrack?: MkvTrackInfo
  private cues: MkvCuePoint[] = []

  private currentByteOffset = 0
  private videoSeq = 1
  private audioSeq = 1
  private nextVideoDtsMs: number | null = null
  private pendingClusterResync = false
  private pendingKeyframeSync = false
  private pendingSeekTargetSec: number | null = null
  private disposed = false
  private pumpRunning = false
  private pumpGeneration = 0
  private activeAbort: AbortController | null = null
  private resumeResolver: (() => void) | null = null
  private seekChain: Promise<void> = Promise.resolve()

  private boundOnSeeking: (() => void) | null = null
  private boundOnTimeUpdate: (() => void) | null = null

  /**
   * 挂载到 HTMLVideoElement 并启动流式 Range + MSE 播放。
   */
  async attach(
    videoEl: HTMLVideoElement,
    streamUrl: string,
    authHeaders: Record<string, string> = {},
    opts: StreamMkvOptions = {},
  ): Promise<StreamMkvMetadata> {
    if (this.disposed) {
      throw new StreamMkvFallbackError('aborted', 'StreamMkvRemuxer 已销毁')
    }
    if (typeof MediaSource === 'undefined' || typeof URL === 'undefined' || typeof URL.createObjectURL !== 'function') {
      throw new StreamMkvFallbackError('unsupported_mse', '当前浏览器不支持 MediaSource Extensions')
    }

    this.videoEl = videoEl
    this.streamUrl = streamUrl
    this.authHeaders = { ...authHeaders }
    this.fetchImpl = opts.fetchImpl ?? ((...args) => fetch(...args))
    this.chunkSizeBytes = Math.max(256 * 1024, opts.chunkSizeBytes ?? DEFAULT_STREAM_CHUNK_BYTES)
    this.maxBufferAheadSec = opts.maxBufferAheadSec ?? DEFAULT_MAX_BUFFER_AHEAD_SEC
    this.onProgress = opts.onProgress
    this.onError = opts.onError

    const initialBytes = opts.initialHeaderBytes ?? DEFAULT_INITIAL_HEADER_BYTES
    const headerFetch = await this.fetchRange(0, initialBytes - 1, opts.signal)
    this.totalBytes = headerFetch.totalBytes || headerFetch.data.byteLength

    const ebml = parseEbmlHeader(headerFetch.data)
    if (!ebml || ebml.segmentDataOffset < 0) {
      throw new StreamMkvFallbackError('invalid_mkv', '无效的 MKV/EBML 头部结构')
    }
    this.segmentDataOffset = ebml.segmentDataOffset

    // 扫描顶层 Segment 子元素；遇到超过 512KB 的大 Attachments/Void/Chapters/Tags 时自动按偏移跳过并继续拉取头部窗口
    let seekEntries: MkvSeekEntry[] = []
    let segInfo: MkvSegmentInfo = {
      timecodeScale: DEFAULT_TIMECODE_SCALE_NS,
      durationRaw: 0,
      durationSec: 0,
    }
    let hasSegInfo = false
    let tracks: MkvTrackInfo[] = []
    let cues: MkvCuePoint[] = []
    let firstClusterOffset = -1

    let buf = headerFetch.data
    let bufStart = 0
    let absPos = ebml.segmentDataOffset
    let headerHops = 0
    const MAX_HEADER_HOPS = 12
    const MAX_METADATA_BOX_BYTES = 4 * 1024 * 1024

    const essentialHeaderIds = new Set<number>([
      EBML_ID.SeekHead,
      EBML_ID.Info,
      EBML_ID.Tracks,
      EBML_ID.Cues,
    ])

    while (absPos < this.totalBytes && headerHops <= MAX_HEADER_HOPS) {
      let localPos = absPos - bufStart
      // 若剩余缓冲不足以解析下一个 EBML 元素头（最多 16 字节），向前拉取新窗口
      if (localPos < 0 || localPos + 16 > buf.byteLength) {
        if (bufStart + buf.byteLength >= this.totalBytes && localPos >= buf.byteLength) {
          break
        }
        if (localPos < 0 || (localPos + 16 > buf.byteLength && bufStart + buf.byteLength < this.totalBytes)) {
          headerHops++
          if (headerHops > MAX_HEADER_HOPS) break
          const nextEnd = Math.min(this.totalBytes - 1, absPos + initialBytes - 1)
          if (nextEnd < absPos) break
          const nextRes = await this.fetchRange(absPos, nextEnd, opts.signal)
          if (nextRes.data.byteLength === 0) break
          buf = nextRes.data
          bufStart = absPos
          localPos = 0
        }
      }

      const el = readElementHeader(buf, localPos)
      if (!el) break
      if (el.id === EBML_ID.Cluster) {
        firstClusterOffset = absPos
        break
      }
      if (el.isUnknown) {
        break
      }

      const absEnd = bufStart + el.endOffset
      if (absEnd > bufStart + buf.byteLength) {
        if (essentialHeaderIds.has(el.id) && el.size > 0 && el.size <= MAX_METADATA_BOX_BYTES) {
          // 关键元数据盒被 512KB 边界截断：按精确大小拉取完整元素
          headerHops++
          if (headerHops > MAX_HEADER_HOPS) break
          const fetchEnd = Math.min(this.totalBytes - 1, absEnd - 1)
          const boxRes = await this.fetchRange(absPos, fetchEnd, opts.signal)
          const slice = boxRes.data
          if (el.id === EBML_ID.SeekHead) {
            seekEntries = seekEntries.concat(parseSeekHead(slice, this.segmentDataOffset))
          } else if (el.id === EBML_ID.Info) {
            segInfo = parseSegmentInfo(slice)
            hasSegInfo = true
          } else if (el.id === EBML_ID.Tracks) {
            tracks = parseTracks(slice)
          } else if (el.id === EBML_ID.Cues) {
            cues = parseCues(slice, segInfo.timecodeScale, this.segmentDataOffset)
          }
          absPos = absEnd
          continue
        }

        // 大型非播放必需元素（如 >512KB 的 Attachments 字体/封面、Void、Chapters、Tags）：直接跳过至 absEnd
        if (absEnd > absPos && absEnd <= this.totalBytes) {
          absPos = absEnd
          continue
        }
        break
      }

      const slice = buf.subarray(localPos, el.endOffset)
      if (el.id === EBML_ID.SeekHead) {
        seekEntries = seekEntries.concat(parseSeekHead(slice, this.segmentDataOffset))
      } else if (el.id === EBML_ID.Info) {
        segInfo = parseSegmentInfo(slice)
        hasSegInfo = true
      } else if (el.id === EBML_ID.Tracks) {
        tracks = parseTracks(slice)
      } else if (el.id === EBML_ID.Cues) {
        cues = parseCues(slice, segInfo.timecodeScale, this.segmentDataOffset)
      }
      absPos = absEnd
    }

    // 若 Info 或 Tracks 未在头部扫描中命中（例如位于文件尾部或超大附件之后），按 SeekHead 偏移定向拉取
    if (!hasSegInfo) {
      const infoSeek = seekEntries.find((s) => s.id === EBML_ID.Info)
      if (infoSeek && infoSeek.fileOffset > 0 && infoSeek.fileOffset < this.totalBytes) {
        try {
          const infoEnd = Math.min(this.totalBytes - 1, infoSeek.fileOffset + 64 * 1024 - 1)
          const infoRes = await this.fetchRange(infoSeek.fileOffset, infoEnd, opts.signal)
          segInfo = parseSegmentInfo(infoRes.data)
          hasSegInfo = true
        } catch {
          // 忽略 Info 定向拉取失败
        }
      }
    }

    if (tracks.length === 0) {
      const tracksSeek = seekEntries.find((s) => s.id === EBML_ID.Tracks)
      if (tracksSeek && tracksSeek.fileOffset > 0 && tracksSeek.fileOffset < this.totalBytes) {
        try {
          const tracksEnd = Math.min(this.totalBytes - 1, tracksSeek.fileOffset + 256 * 1024 - 1)
          const tracksRes = await this.fetchRange(tracksSeek.fileOffset, tracksEnd, opts.signal)
          tracks = parseTracks(tracksRes.data)
        } catch {
          // 忽略 Tracks 定向拉取失败
        }
      }
    }

    this.timecodeScale = segInfo.timecodeScale
    this.durationSec = segInfo.durationSec
    this.nextVideoDtsMs = null
    this.pendingClusterResync = false
    this.pendingKeyframeSync = false
    this.pendingSeekTargetSec = null

    this.videoTrack = tracks.find((t) => t.trackType === 'video')
    const videoTrackNum = this.videoTrack?.trackNumber

    // 若 Cues 不在头部但 SeekHead 中记录了 Cues 偏移（常见于大体积 MKV 尾部），按需发起一次小 Range 拉取
    if (cues.length === 0) {
      const cuesSeek = seekEntries.find((s) => s.id === EBML_ID.Cues)
      if (cuesSeek && cuesSeek.fileOffset > 0 && cuesSeek.fileOffset < this.totalBytes) {
        try {
          const cuesEnd = Math.min(this.totalBytes - 1, cuesSeek.fileOffset + 2 * 1024 * 1024 - 1)
          const cuesRes = await this.fetchRange(cuesSeek.fileOffset, cuesEnd, opts.signal)
          cues = parseCues(cuesRes.data, this.timecodeScale, this.segmentDataOffset, videoTrackNum)
        } catch {
          // Cues 拉取失败不影响正常顺序播放，仅降级为线性估算 Seek
        }
      }
    } else if (videoTrackNum !== undefined && videoTrackNum > 0) {
      const videoOnlyCues = cues.filter((c) => c.trackNumber === videoTrackNum)
      if (videoOnlyCues.length > 0) {
        cues = videoOnlyCues
      }
    }

    this.cues = cues
    const audioTracks = tracks.filter((t) => t.trackType === 'audio')
    if (opts.videoOnly) {
      this.audioTrack = undefined
    } else if (
      opts.audioTrackIndex !== undefined &&
      Number.isInteger(opts.audioTrackIndex) &&
      opts.audioTrackIndex >= 0
    ) {
      this.audioTrack =
        audioTracks[opts.audioTrackIndex] ??
        audioTracks.find((t) => t.trackNumber === opts.audioTrackIndex) ??
        audioTracks[0]
    } else {
      this.audioTrack = audioTracks[0]
    }

    const { videoMime, audioMime } = resolveMkvMimeType(this.videoTrack, this.audioTrack)
    if (!this.videoTrack || !videoMime || !MediaSource.isTypeSupported(videoMime)) {
      throw new StreamMkvFallbackError(
        'unsupported_codec',
        `浏览器 MSE 不支持视频编码: ${this.videoTrack?.codecId || 'unknown'}`,
      )
    }
    if (this.audioTrack && (!audioMime || !MediaSource.isTypeSupported(audioMime))) {
      throw new StreamMkvFallbackError(
        'unsupported_codec',
        `浏览器 MSE 不支持音频编码: ${this.audioTrack.codecId}`,
      )
    }

    // 确定首个 Cluster 偏移
    if (firstClusterOffset >= 0) {
      this.currentByteOffset = firstClusterOffset
      this.firstClusterOffset = firstClusterOffset
    } else if (this.cues.length > 0) {
      this.currentByteOffset = this.cues[0].fileOffset
      this.firstClusterOffset = this.cues[0].fileOffset
    } else {
      const clusterSeek = seekEntries.find((s) => s.id === EBML_ID.Cluster)
      this.currentByteOffset = clusterSeek ? clusterSeek.fileOffset : absPos
      this.firstClusterOffset = this.currentByteOffset
    }

    // 初始化 MediaSource 与 SourceBuffer（异常时清理已创建的 objectUrl，防止内存泄漏）
    const ms = new MediaSource()
    this.mediaSource = ms
    const url = URL.createObjectURL(ms)
    this.objectUrl = url

    try {
      await new Promise<void>((resolve, reject) => {
        const onOpen = () => {
          ms.removeEventListener('sourceopen', onOpen)
          resolve()
        }
        ms.addEventListener('sourceopen', onOpen, { once: true })
        if (opts.signal) {
          if (opts.signal.aborted) {
            ms.removeEventListener('sourceopen', onOpen)
            reject(new StreamMkvFallbackError('aborted', '已取消 MSE 初始化'))
            return
          }
          opts.signal.addEventListener(
            'abort',
            () => {
              ms.removeEventListener('sourceopen', onOpen)
              reject(new StreamMkvFallbackError('aborted', '已取消 MSE 初始化'))
            },
            { once: true },
          )
        }
        videoEl.src = url
      })

      if (this.disposed) {
        throw new StreamMkvFallbackError('aborted', 'StreamMkvRemuxer 已销毁')
      }

      if (this.durationSec > 0 && Number.isFinite(this.durationSec)) {
        try {
          ms.duration = this.durationSec
        } catch {
          // 某些浏览器或测试 stub 中 duration 可能只读
        }
      }

      // 为视频轨和音频轨分别建立独立 SourceBuffer 并写入 init segment
      this.videoSb = ms.addSourceBuffer(videoMime)
      const videoInit = buildFmp4InitSegment([this.videoTrack], { durationSec: this.durationSec })
      await this.appendToBuffer(this.videoSb, videoInit)

      if (this.audioTrack && audioMime) {
        this.audioSb = ms.addSourceBuffer(audioMime)
        const audioInit = buildFmp4InitSegment([this.audioTrack], { durationSec: this.durationSec })
        await this.appendToBuffer(this.audioSb, audioInit)
      }
    } catch (err) {
      if (this.objectUrl) {
        URL.revokeObjectURL(this.objectUrl)
        this.objectUrl = null
      }
      this.mediaSource = null
      this.videoSb = null
      this.audioSb = null
      throw err
    }

    // 绑定 seeking 与 timeupdate 事件监听
    this.boundOnSeeking = () => this.handleSeeking()
    this.boundOnTimeUpdate = () => this.wakePump()
    videoEl.addEventListener('seeking', this.boundOnSeeking)
    videoEl.addEventListener('timeupdate', this.boundOnTimeUpdate)

    // 启动后台流式 Range 拉取循环
    void this.startPumpLoop(++this.pumpGeneration)

    return {
      docType: ebml.docType || 'matroska',
      durationSec: this.durationSec,
      totalBytes: this.totalBytes,
      tracks,
      cuesCount: this.cues.length,
      videoTrack: this.videoTrack,
      audioTrack: this.audioTrack,
    }
  }

  /** 根据目标播放时间计算应跳转的字节偏移（优先使用 Cues 精确二分，否则基于首个 Cluster 线性估算） */
  resolveSeekByteOffset(targetTimeSec: number): number {
    const cue = lookupCueOffset(this.cues, targetTimeSec)
    if (cue && cue.fileOffset >= this.segmentDataOffset && cue.fileOffset < this.totalBytes) {
      return cue.fileOffset
    }
    const dataStart =
      this.firstClusterOffset >= this.segmentDataOffset
        ? this.firstClusterOffset
        : this.segmentDataOffset
    if (this.durationSec > 0 && this.totalBytes > dataStart) {
      const ratio = Math.max(0, Math.min(1, targetTimeSec / this.durationSec))
      const dataSpan = this.totalBytes - dataStart
      return Math.floor(dataStart + dataSpan * ratio)
    }
    return dataStart
  }

  private waitForBufferIdle(sb: SourceBuffer): Promise<void> {
    if (this.disposed || !sb.updating) return Promise.resolve()
    return new Promise<void>((resolve) => {
      const done = () => {
        sb.removeEventListener('updateend', done)
        sb.removeEventListener('error', done)
        resolve()
      }
      sb.addEventListener('updateend', done, { once: true })
      sb.addEventListener('error', done, { once: true })
    })
  }

  private removeFromBuffer(sb: SourceBuffer, start: number, end: number): Promise<void> {
    if (this.disposed || typeof sb.remove !== 'function' || end <= start) {
      return Promise.resolve()
    }
    return new Promise<void>((resolve) => {
      const doRemove = () => {
        if (this.disposed) {
          resolve()
          return
        }
        const onDone = () => {
          sb.removeEventListener('updateend', onDone)
          sb.removeEventListener('error', onDone)
          resolve()
        }
        sb.addEventListener('updateend', onDone, { once: true })
        sb.addEventListener('error', onDone, { once: true })
        try {
          sb.remove(start, end)
        } catch {
          sb.removeEventListener('updateend', onDone)
          sb.removeEventListener('error', onDone)
          resolve()
        }
      }
      if (sb.updating) {
        sb.addEventListener('updateend', doRemove, { once: true })
      } else {
        doRemove()
      }
    })
  }

  /** 定期驱逐 currentTime - 30 之前的旧 MSE 缓冲，防止长片触发 QuotaExceededError */
  private async evictOldBuffers(): Promise<void> {
    if (this.disposed || !this.videoEl) return
    const evictEnd = this.videoEl.currentTime - 30
    if (!Number.isFinite(evictEnd) || evictEnd <= 0) return

    const buffers = [this.videoSb, this.audioSb].filter((sb): sb is SourceBuffer => Boolean(sb))
    for (const sb of buffers) {
      if (this.disposed) return
      try {
        const buffered = sb.buffered
        if (!buffered || buffered.length === 0) continue
        const rangeStart = buffered.start(0)
        if (rangeStart < evictEnd - 1) {
          await this.removeFromBuffer(sb, rangeStart, evictEnd)
        }
      } catch {
        // 忽略测试 stub 或浏览器缓冲驱逐竞态
      }
    }
  }

  private handleSeeking(): void {
    if (this.disposed || !this.videoEl) return
    const targetTime = this.videoEl.currentTime
    if (this.isTimeBuffered(targetTime)) {
      this.wakePump()
      return
    }

    // 中止当前分块拉取，递增 generation 并通过 seekChain 串行化 abort + 重启泵循环
    const seekGen = ++this.pumpGeneration
    if (this.activeAbort) {
      this.activeAbort.abort()
      this.activeAbort = null
    }
    this.wakePump()

    this.seekChain = this.seekChain
      .then(async () => {
        if (this.disposed || seekGen !== this.pumpGeneration) return
        const buffers = [this.videoSb, this.audioSb].filter((sb): sb is SourceBuffer => Boolean(sb))
        for (const sb of buffers) {
          await this.waitForBufferIdle(sb)
          if (this.disposed || seekGen !== this.pumpGeneration) return
          if (this.mediaSource?.readyState === 'open' && typeof sb.abort === 'function') {
            try {
              sb.abort()
            } catch {
              // 忽略 abort 状态竞态
            }
          }
        }
        if (this.disposed || seekGen !== this.pumpGeneration) return
        const hitCue = Boolean(lookupCueOffset(this.cues, targetTime))
        this.pendingClusterResync = !hitCue
        this.pendingKeyframeSync = true
        this.pendingSeekTargetSec = targetTime
        this.nextVideoDtsMs = null
        this.currentByteOffset = this.resolveSeekByteOffset(targetTime)
        void this.startPumpLoop(seekGen)
      })
      .catch(() => undefined)
  }

  private isTimeBuffered(timeSec: number): boolean {
    if (!this.videoEl) return false
    const buf = this.videoEl.buffered
    for (let i = 0; i < buf.length; i++) {
      if (timeSec >= buf.start(i) && timeSec <= buf.end(i)) {
        return true
      }
    }
    return false
  }

  private getBufferedAheadSec(): number {
    if (!this.videoEl) return 0
    const cur = this.videoEl.currentTime
    const buf = this.videoEl.buffered
    for (let i = 0; i < buf.length; i++) {
      if (cur >= buf.start(i) - 0.5 && cur <= buf.end(i)) {
        return buf.end(i) - cur
      }
    }
    return 0
  }

  private wakePump(): void {
    if (this.resumeResolver) {
      const fn = this.resumeResolver
      this.resumeResolver = null
      fn()
    }
  }

  private waitForResume(): Promise<void> {
    return new Promise<void>((resolve) => {
      this.resumeResolver = resolve
    })
  }

  private async startPumpLoop(generation: number): Promise<void> {
    if (this.pumpRunning && generation !== this.pumpGeneration) return
    this.pumpRunning = true

    let leftover = new Uint8Array(0)
    let lastClusterTimecode = 0
    let needClusterResync = this.pendingClusterResync
    let needKeyframeSync = this.pendingKeyframeSync
    const seekWasLinear = this.pendingClusterResync
    const seekTargetSec = this.pendingSeekTargetSec
    this.pendingClusterResync = false
    this.pendingKeyframeSync = false
    this.pendingSeekTargetSec = null

    try {
      while (!this.disposed && generation === this.pumpGeneration) {
        if (this.totalBytes > 0 && this.currentByteOffset >= this.totalBytes) {
          if (this.mediaSource && this.mediaSource.readyState === 'open') {
            try {
              this.mediaSource.endOfStream()
            } catch {
              // 忽略 endOfStream 状态竞态
            }
          }
          break
        }

        // 背压控制：已缓冲超前超过阈值时暂停拉取
        if (this.getBufferedAheadSec() >= this.maxBufferAheadSec) {
          await this.waitForResume()
          continue
        }

        const start = this.currentByteOffset
        const end =
          this.totalBytes > 0
            ? Math.min(this.totalBytes - 1, start + this.chunkSizeBytes - 1)
            : start + this.chunkSizeBytes - 1

        const abortCtrl = new AbortController()
        this.activeAbort = abortCtrl

        let chunkRes: { data: Uint8Array; totalBytes: number }
        try {
          chunkRes = await this.fetchRange(start, end, abortCtrl.signal)
        } catch (err) {
          if (abortCtrl.signal.aborted || generation !== this.pumpGeneration || this.disposed) {
            break
          }
          throw err
        } finally {
          if (this.activeAbort === abortCtrl) {
            this.activeAbort = null
          }
        }

        if (generation !== this.pumpGeneration || this.disposed) break

        if (chunkRes.totalBytes > 0 && this.totalBytes === 0) {
          this.totalBytes = chunkRes.totalBytes
        }
        if (chunkRes.data.byteLength === 0) break

        this.currentByteOffset = start + chunkRes.data.byteLength

        let combined = leftover.byteLength > 0 ? concatBytes(leftover, chunkRes.data) : chunkRes.data

        // 无 Cues 线性 Seek 时可能落在 Cluster 中间字节：先重同步到下一个合法 Cluster 元素头
        if (needClusterResync) {
          const clusterOffset = findNextClusterOffset(combined, 0)
          if (clusterOffset < 0) {
            // 保留尾部 16 字节防止 4 字节 Cluster 同步头跨分块截断
            leftover =
              combined.byteLength > 16 ? combined.slice(combined.byteLength - 16) : combined.slice()
            continue
          }
          combined = combined.subarray(clusterOffset)
          needClusterResync = false
        }

        const parsed = parseClusterFrames(combined, {
          timecodeScale: this.timecodeScale,
          clusterBaseTimecode: lastClusterTimecode,
        })
        lastClusterTimecode = parsed.lastClusterTimecode

        // 处理跨分块未收齐的尾部字节（支持高达 32MB 的 4K UHD Remux 大关键帧拼接；超限精确对齐跳过或抛错回退 HLS，严禁原地清空停在帧中间）
        const remainingLen = combined.byteLength - parsed.consumedBytes
        if (remainingLen <= 0) {
          leftover = new Uint8Array(0)
        } else if (
          parsed.neededBytes !== undefined &&
          parsed.neededBytes > 0 &&
          parsed.neededBytes <= MAX_SINGLE_FRAME_BYTES &&
          remainingLen <= MAX_SINGLE_FRAME_BYTES
        ) {
          leftover = combined.slice(parsed.consumedBytes)
        } else if (
          (parsed.neededBytes !== undefined && parsed.neededBytes > MAX_SINGLE_FRAME_BYTES) ||
          remainingLen > MAX_SINGLE_FRAME_BYTES
        ) {
          const oversizedEl = readElementHeader(combined, parsed.consumedBytes)
          const isMediaBlock =
            oversizedEl?.id === EBML_ID.SimpleBlock || oversizedEl?.id === EBML_ID.BlockGroup
          if (
            oversizedEl &&
            !oversizedEl.isUnknown &&
            !isMediaBlock &&
            this.totalBytes > 0
          ) {
            const elementFileStart = this.currentByteOffset - remainingLen
            const skipOffset = elementFileStart + oversizedEl.endOffset
            if (skipOffset > this.currentByteOffset && skipOffset <= this.totalBytes) {
              this.currentByteOffset = skipOffset
              leftover = new Uint8Array(0)
            } else {
              throw new StreamMkvFallbackError(
                'invalid_mkv',
                'MKV 元素体积超过 32MB 安全上限，已回退服务端 HLS',
              )
            }
          } else {
            throw new StreamMkvFallbackError(
              'invalid_mkv',
              'MKV 单帧体积超过 32MB 硬上限，已平滑回退服务端 HLS',
            )
          }
        } else if (remainingLen <= 4 * 1024 * 1024 && parsed.consumedBytes > 0) {
          leftover = combined.slice(parsed.consumedBytes)
        } else {
          throw new StreamMkvFallbackError(
            'invalid_mkv',
            'MKV Cluster 数据流无法同步解析，已安全回退服务端 HLS',
          )
        }

        if (generation !== this.pumpGeneration || this.disposed) break

        if (parsed.frames.length > 0) {
          await this.evictOldBuffers()
          if (generation !== this.pumpGeneration || this.disposed) break

          let vFrames = this.videoTrack
            ? parsed.frames.filter((f) => f.trackNumber === this.videoTrack!.trackNumber)
            : []
          let aFrames = this.audioTrack
            ? parsed.frames.filter((f) => f.trackNumber === this.audioTrack!.trackNumber)
            : []

          // Seek 后精确对齐首个视频关键帧与音频时间戳偏移，防止黑屏/花屏与音画失步
          if (needKeyframeSync && this.videoTrack) {
            const kfIdx = vFrames.findIndex((f) => f.isKeyframe)
            if (kfIdx < 0) {
              continue
            }
            vFrames = vFrames.slice(kfIdx)
            const syncTimestampMs = vFrames[0].timestampMs
            aFrames = aFrames.filter((f) => f.timestampMs >= syncTimestampMs - 50)
            needKeyframeSync = false

            // 无 Cues 线性 Seek 若落点略晚于目标时间，主动对齐 video.currentTime 至真实关键帧时间避免卡死在未缓冲间隙
            if (
              seekWasLinear &&
              this.videoEl &&
              seekTargetSec !== null &&
              syncTimestampMs / 1000 > seekTargetSec + 0.5
            ) {
              try {
                this.videoEl.currentTime = syncTimestampMs / 1000
              } catch {
                // 忽略测试环境只读 currentTime
              }
            }
          }

          if (this.videoTrack && this.videoSb && vFrames.length > 0) {
            const defaultVideoDur = this.videoTrack.defaultDurationNs
              ? Math.max(1, Math.round(this.videoTrack.defaultDurationNs / 1_000_000))
              : 40
            const minPtsMs = vFrames.reduce(
              (m, f) => Math.min(m, f.timestampMs),
              Number.POSITIVE_INFINITY,
            )
            // 若与上一分片 DTS 连续（跨分块未发生 Seek 跳变），衔接 baseMediaDecodeTime 避免 B 帧跨分块 DTS 倒退
            const useChainedDts =
              this.nextVideoDtsMs !== null &&
              Math.abs(minPtsMs - this.nextVideoDtsMs) <= defaultVideoDur * 6
            const baseDts = useChainedDts ? this.nextVideoDtsMs! : undefined
            const seg = buildFmp4MediaSegment(vFrames, {
              sequenceNumber: this.videoSeq++,
              trackId: this.videoTrack.trackNumber,
              defaultSampleDuration: defaultVideoDur,
              baseMediaDecodeTime: baseDts,
            })
            const sortedPts = vFrames.map((f) => Math.round(f.timestampMs)).sort((a, b) => a - b)
            const segStartDts = baseDts ?? sortedPts[0]
            const segSpan =
              sortedPts.length > 1
                ? Math.max(defaultVideoDur * (sortedPts.length - 1), sortedPts[sortedPts.length - 1] - sortedPts[0])
                : 0
            this.nextVideoDtsMs = segStartDts + segSpan + defaultVideoDur
            await this.appendToBuffer(this.videoSb, seg)
            if (generation !== this.pumpGeneration || this.disposed) break
          }

          if (this.audioTrack && this.audioSb && aFrames.length > 0) {
            const seg = buildFmp4MediaSegment(aFrames, {
              sequenceNumber: this.audioSeq++,
              trackId: this.audioTrack.trackNumber,
              defaultSampleDuration: this.audioTrack.defaultDurationNs
                ? Math.max(1, Math.round(this.audioTrack.defaultDurationNs / 1_000_000))
                : 21,
            })
            await this.appendToBuffer(this.audioSb, seg)
          }
        }

        this.onProgress?.({
          loadedBytes: this.currentByteOffset,
          totalBytes: this.totalBytes,
          bufferedSec: this.getBufferedAheadSec(),
        })
      }
    } catch (err) {
      if (!this.disposed && generation === this.pumpGeneration) {
        const error = err instanceof Error ? err : new Error(String(err))
        this.onError?.(error)
      }
    } finally {
      if (generation === this.pumpGeneration) {
        this.pumpRunning = false
      }
    }
  }

  private async fetchRange(
    start: number,
    end: number,
    signal?: AbortSignal,
  ): Promise<{ data: Uint8Array; totalBytes: number }> {
    const headers: Record<string, string> = {
      ...this.authHeaders,
      Range: `bytes=${start}-${end}`,
    }

    let res: Response
    try {
      res = await this.fetchImpl(this.streamUrl, {
        method: 'GET',
        headers,
        signal,
      })
    } catch (err) {
      if (signal?.aborted || this.disposed) {
        throw new StreamMkvFallbackError('aborted', 'Range 请求已中止')
      }
      const msg = err instanceof Error ? err.message : '网络请求失败'
      throw new StreamMkvFallbackError('fetch_failed', `Range 拉取失败: ${msg}`)
    }

    if (res.redirected && res.url && !isSameOriginUrl(res.url)) {
      res.body?.cancel().catch(() => undefined)
      throw new StreamMkvFallbackError(
        'fetch_failed',
        '检测到跨源重定向响应，已中止流式拉取并安全回退',
      )
    }

    const expectedLen = Math.max(1, end - start + 1)

    if (res.status === 206) {
      const contentLength = Number(res.headers.get('content-length') || 0)
      if (
        Number.isFinite(contentLength) &&
        contentLength > expectedLen * 2 &&
        contentLength > 16 * 1024 * 1024
      ) {
        res.body?.cancel().catch(() => undefined)
        throw new StreamMkvFallbackError('fetch_failed', 'Range 响应体积超出分块预期上限')
      }
      const totalBytes = parseContentRangeTotal(res.headers.get('content-range')) || contentLength
      const buf = await res.arrayBuffer()
      return {
        data: new Uint8Array(buf),
        totalBytes: Number.isFinite(totalBytes) ? totalBytes : 0,
      }
    }

    if (res.status === 200) {
      // 服务端未响应 206：防止直接 arrayBuffer() 读取数 GB 整文件导致 OOM
      if (start > 0) {
        res.body?.cancel().catch(() => undefined)
        throw new StreamMkvFallbackError(
          'fetch_failed',
          '服务端不支持 HTTP Range 分块请求（返回 HTTP 200），自动回退服务端 HLS',
        )
      }
      const contentLength = Number(res.headers.get('content-length') || 0)
      if (res.body && typeof res.body.getReader === 'function') {
        const reader = res.body.getReader()
        const chunks: Uint8Array[] = []
        let received = 0
        let truncated = false
        try {
          while (received < expectedLen) {
            const { done, value } = await reader.read()
            if (done || !value) break
            const remain = expectedLen - received
            if (value.byteLength > remain) {
              chunks.push(value.subarray(0, remain))
              received += remain
              truncated = true
              await reader.cancel().catch(() => undefined)
              break
            }
            chunks.push(value)
            received += value.byteLength
          }
          if (!truncated && Number.isFinite(contentLength) && contentLength > expectedLen) {
            truncated = true
            await reader.cancel().catch(() => undefined)
          }
        } finally {
          reader.releaseLock?.()
        }
        if (truncated) {
          throw new StreamMkvFallbackError(
            'fetch_failed',
            '服务端忽略 Range 返回超大 HTTP 200 整文件，已截断并回退服务端 HLS',
          )
        }
        const merged = new Uint8Array(received)
        let off = 0
        for (const c of chunks) {
          merged.set(c, off)
          off += c.byteLength
        }
        return {
          data: merged,
          totalBytes: Number.isFinite(contentLength) && contentLength > 0 ? contentLength : received,
        }
      }

      if (Number.isFinite(contentLength) && contentLength > expectedLen) {
        throw new StreamMkvFallbackError(
          'fetch_failed',
          '服务端返回超大 HTTP 200 整文件，已拒绝加载以防 OOM',
        )
      }
      const buf = await res.arrayBuffer()
      if (buf.byteLength > expectedLen) {
        throw new StreamMkvFallbackError(
          'fetch_failed',
          '服务端返回 HTTP 200 体积超过分块上限，已拒绝加载',
        )
      }
      return {
        data: new Uint8Array(buf),
        totalBytes:
          Number.isFinite(contentLength) && contentLength > 0 ? contentLength : buf.byteLength,
      }
    }

    throw new StreamMkvFallbackError('fetch_failed', `Range 请求返回 HTTP ${res.status}`)
  }

  private isQuotaExceeded(err: unknown): boolean {
    if (!err || typeof err !== 'object') return false
    const name = 'name' in err ? String((err as { name?: unknown }).name) : ''
    const code = 'code' in err ? Number((err as { code?: unknown }).code) : 0
    const msg = 'message' in err ? String((err as { message?: unknown }).message) : ''
    return name === 'QuotaExceededError' || code === 22 || /quotaexceeded/i.test(msg)
  }

  private async evictForQuotaRecovery(targetSb: SourceBuffer): Promise<void> {
    if (this.disposed) return
    this.maxBufferAheadSec = Math.max(5, Math.floor(this.maxBufferAheadSec / 2))
    const curTime = this.videoEl?.currentTime ?? 0
    const evictEnd = curTime - 5
    const forwardKeepSec = Math.max(15, Math.min(this.maxBufferAheadSec, 30))
    const forwardEvictStart = curTime + forwardKeepSec
    const buffers = Array.from(
      new Set([targetSb, this.videoSb, this.audioSb].filter((b): b is SourceBuffer => Boolean(b))),
    )
    for (const sb of buffers) {
      if (this.disposed) return
      try {
        const buffered = sb.buffered
        if (!buffered || buffered.length === 0) continue
        const rangeStart = buffered.start(0)
        if (Number.isFinite(evictEnd) && evictEnd > rangeStart) {
          await this.removeFromBuffer(sb, rangeStart, evictEnd)
        }
        // 同时驱逐远端前向缓冲区间（例如用户在后续时间轴塞满 MSE 配额后 Seek 回到前 0~5s 时）
        const latestBuffered = sb.buffered
        if (latestBuffered && latestBuffered.length > 0) {
          for (let i = latestBuffered.length - 1; i >= 0; i--) {
            const rStart = latestBuffered.start(i)
            const rEnd = latestBuffered.end(i)
            if (rEnd > curTime + 15) {
              const removeStart = Math.max(rStart, Math.min(forwardEvictStart, curTime + 15))
              if (rEnd > removeStart) {
                await this.removeFromBuffer(sb, removeStart, rEnd)
              }
            }
          }
        }
      } catch {
        // 忽略缓冲驱逐竞态
      }
    }
  }

  private appendBufferOnce(sb: SourceBuffer, data: Uint8Array): Promise<void> {
    if (this.disposed) return Promise.resolve()
    return new Promise<void>((resolve, reject) => {
      const doAppend = () => {
        if (this.disposed) {
          resolve()
          return
        }
        const onEnd = () => {
          sb.removeEventListener('updateend', onEnd)
          sb.removeEventListener('error', onErr)
          resolve()
        }
        const onErr = () => {
          sb.removeEventListener('updateend', onEnd)
          sb.removeEventListener('error', onErr)
          reject(new StreamMkvFallbackError('mse_error', 'SourceBuffer appendBuffer 失败'))
        }
        sb.addEventListener('updateend', onEnd, { once: true })
        sb.addEventListener('error', onErr, { once: true })
        try {
          const copy = new Uint8Array(data.byteLength)
          copy.set(data)
          sb.appendBuffer(copy.buffer)
        } catch (err) {
          sb.removeEventListener('updateend', onEnd)
          sb.removeEventListener('error', onErr)
          reject(err)
        }
      }

      if (sb.updating) {
        sb.addEventListener('updateend', doAppend, { once: true })
      } else {
        doAppend()
      }
    })
  }

  private async appendToBuffer(sb: SourceBuffer, data: Uint8Array): Promise<void> {
    if (this.disposed) return
    try {
      await this.appendBufferOnce(sb, data)
    } catch (err) {
      if (!this.disposed && this.isQuotaExceeded(err)) {
        await this.evictForQuotaRecovery(sb)
        await this.waitForBufferIdle(sb)
        try {
          await this.appendBufferOnce(sb, data)
          return
        } catch (retryErr) {
          if (retryErr instanceof StreamMkvFallbackError) throw retryErr
          throw new StreamMkvFallbackError(
            'mse_error',
            retryErr instanceof Error ? retryErr.message : 'SourceBuffer QuotaExceeded 重试失败',
          )
        }
      }
      if (err instanceof StreamMkvFallbackError) throw err
      throw new StreamMkvFallbackError(
        'mse_error',
        err instanceof Error ? err.message : 'SourceBuffer appendBuffer 异常',
      )
    }
  }

  /** 中止所有 Range 拉取、移除事件监听并释放 MediaSource ObjectURL */
  dispose(): void {
    if (this.disposed) return
    this.disposed = true
    this.pumpGeneration++
    this.wakePump()

    if (this.activeAbort) {
      this.activeAbort.abort()
      this.activeAbort = null
    }

    if (this.videoEl) {
      if (this.boundOnSeeking) {
        this.videoEl.removeEventListener('seeking', this.boundOnSeeking)
      }
      if (this.boundOnTimeUpdate) {
        this.videoEl.removeEventListener('timeupdate', this.boundOnTimeUpdate)
      }
      this.boundOnSeeking = null
      this.boundOnTimeUpdate = null
      this.videoEl = null
    }

    if (this.mediaSource && this.mediaSource.readyState === 'open') {
      try {
        this.mediaSource.endOfStream()
      } catch {
        // 忽略关闭时的异常
      }
    }
    this.mediaSource = null
    this.videoSb = null
    this.audioSb = null

    if (this.objectUrl) {
      URL.revokeObjectURL(this.objectUrl)
      this.objectUrl = null
    }
  }
}
