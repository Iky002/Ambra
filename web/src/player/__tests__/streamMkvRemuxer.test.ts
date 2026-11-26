import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  buildAudioSampleEntry,
  buildFmp4InitSegment,
  buildFmp4MediaSegment,
  canStreamMkvRemux,
  canStreamMkvVideoOnly,
  EBML_ID,
  findNextClusterOffset,
  isMkvContainer,
  lookupCueOffset,
  parseClusterFrames,
  parseContentRangeTotal,
  parseCues,
  parseEbmlHeader,
  parseSeekHead,
  parseSegmentInfo,
  parseTracks,
  readElementHeader,
  readVint,
  resolveMkvMimeType,
  StreamMkvFallbackError,
  StreamMkvRemuxer,
  type MkvFrame,
  type MkvTrackInfo,
} from '../streamMkvRemuxer'

// ==================== 测试用 EBML 编码辅助工具 ====================

function encodeVintSize(value: number): Uint8Array {
  if (value < 0x7f) return new Uint8Array([0x80 | value])
  if (value < 0x3fff) return new Uint8Array([0x40 | (value >> 8), value & 0xff])
  if (value < 0x1fffff) {
    return new Uint8Array([0x20 | (value >> 16), (value >> 8) & 0xff, value & 0xff])
  }
  return new Uint8Array([
    0x10 | (value >> 24),
    (value >> 16) & 0xff,
    (value >> 8) & 0xff,
    value & 0xff,
  ])
}

function encodeId(id: number): Uint8Array {
  if (id <= 0xff) return new Uint8Array([id])
  if (id <= 0xffff) return new Uint8Array([(id >> 8) & 0xff, id & 0xff])
  if (id <= 0xffffff) return new Uint8Array([(id >> 16) & 0xff, (id >> 8) & 0xff, id & 0xff])
  return new Uint8Array([
    (id >>> 24) & 0xff,
    (id >>> 16) & 0xff,
    (id >>> 8) & 0xff,
    id & 0xff,
  ])
}

function concat(...arrays: Uint8Array[]): Uint8Array {
  const total = arrays.reduce((s, a) => s + a.byteLength, 0)
  const out = new Uint8Array(total)
  let off = 0
  for (const a of arrays) {
    out.set(a, off)
    off += a.byteLength
  }
  return out
}

function ebmlEl(id: number, ...payloads: Uint8Array[]): Uint8Array {
  const body = concat(...payloads)
  return concat(encodeId(id), encodeVintSize(body.byteLength), body)
}

function uintBytes(val: number, len?: number): Uint8Array {
  const bytes: number[] = []
  let v = val
  do {
    bytes.unshift(v & 0xff)
    v = Math.floor(v / 256)
  } while (v > 0)
  while (len && bytes.length < len) bytes.unshift(0)
  return new Uint8Array(bytes)
}

function float64Bytes(val: number): Uint8Array {
  const buf = new ArrayBuffer(8)
  new DataView(buf).setFloat64(0, val, false)
  return new Uint8Array(buf)
}

function strBytes(s: string): Uint8Array {
  return new TextEncoder().encode(s)
}

function makeSimpleBlock(
  trackNum: number,
  relTime: number,
  flags: number,
  payload: Uint8Array,
): Uint8Array {
  const trk = encodeVintSize(trackNum)
  const hdr = new Uint8Array(trk.byteLength + 3)
  hdr.set(trk, 0)
  new DataView(hdr.buffer).setInt16(trk.byteLength, relTime, false)
  hdr[trk.byteLength + 2] = flags
  return ebmlEl(EBML_ID.SimpleBlock, concat(hdr, payload))
}

function buildSyntheticMkvFile(opts?: { videoCodecId?: string; audioCodecId?: string }): {
  bytes: Uint8Array
  segmentDataOffset: number
  cluster1Offset: number
  cluster2Offset: number
} {
  const videoCodec = opts?.videoCodecId ?? 'V_MPEG4/ISO/AVC'
  const audioCodec = opts?.audioCodecId ?? 'A_AAC'

  const ebmlHeader = ebmlEl(
    EBML_ID.EBML,
    ebmlEl(EBML_ID.EBMLVersion, uintBytes(1)),
    ebmlEl(EBML_ID.DocType, strBytes('matroska')),
    ebmlEl(EBML_ID.DocTypeVersion, uintBytes(4)),
  )

  const info = ebmlEl(
    EBML_ID.Info,
    ebmlEl(EBML_ID.TimecodeScale, uintBytes(1_000_000)),
    ebmlEl(EBML_ID.Duration, float64Bytes(120_000)), // 120 seconds
  )

  const avcC = new Uint8Array([0x01, 0x64, 0x00, 0x28, 0xff, 0xe1, 0x00, 0x04, 0x67, 0x64, 0x00, 0x28])
  const aacCfg = new Uint8Array([0x11, 0x90]) // AAC-LC 48kHz stereo

  const videoTrackEntry = ebmlEl(
    EBML_ID.TrackEntry,
    ebmlEl(EBML_ID.TrackNumber, uintBytes(1)),
    ebmlEl(EBML_ID.TrackType, uintBytes(1)),
    ebmlEl(EBML_ID.CodecID, strBytes(videoCodec)),
    ebmlEl(EBML_ID.CodecPrivate, avcC),
    ebmlEl(EBML_ID.DefaultDuration, uintBytes(40_000_000)),
    ebmlEl(
      EBML_ID.Video,
      ebmlEl(EBML_ID.PixelWidth, uintBytes(1920)),
      ebmlEl(EBML_ID.PixelHeight, uintBytes(1080)),
    ),
  )

  const audioTrackEntry = ebmlEl(
    EBML_ID.TrackEntry,
    ebmlEl(EBML_ID.TrackNumber, uintBytes(2)),
    ebmlEl(EBML_ID.TrackType, uintBytes(2)),
    ebmlEl(EBML_ID.CodecID, strBytes(audioCodec)),
    ebmlEl(EBML_ID.CodecPrivate, aacCfg),
    ebmlEl(
      EBML_ID.Audio,
      ebmlEl(EBML_ID.SamplingFrequency, float64Bytes(48000)),
      ebmlEl(EBML_ID.Channels, uintBytes(2)),
      ebmlEl(EBML_ID.BitDepth, uintBytes(16)),
    ),
  )

  const tracks = ebmlEl(EBML_ID.Tracks, videoTrackEntry, audioTrackEntry)

  const cluster1 = ebmlEl(
    EBML_ID.Cluster,
    ebmlEl(EBML_ID.Timecode, uintBytes(0)),
    makeSimpleBlock(1, 0, 0x80, new Uint8Array([0x00, 0x00, 0x00, 0x02, 0x65, 0x88])),
    makeSimpleBlock(1, 40, 0x00, new Uint8Array([0x00, 0x00, 0x00, 0x02, 0x41, 0x9a])),
    makeSimpleBlock(2, 0, 0x80, new Uint8Array([0xde, 0xad, 0xbe, 0xef])),
  )

  const cluster2 = ebmlEl(
    EBML_ID.Cluster,
    ebmlEl(EBML_ID.Timecode, uintBytes(30_000)),
    makeSimpleBlock(1, 0, 0x80, new Uint8Array([0x00, 0x00, 0x00, 0x02, 0x65, 0x99])),
    makeSimpleBlock(2, 0, 0x80, new Uint8Array([0xca, 0xfe, 0xba, 0xbe])),
  )

  // 计算各元素相对于 Segment 数据的偏移（先放置固定长度的占位 SeekHead）
  const buildSeekHeadWithCuesPos = (cuesPos: number) =>
    ebmlEl(
      EBML_ID.SeekHead,
      ebmlEl(
        EBML_ID.Seek,
        ebmlEl(EBML_ID.SeekID, encodeId(EBML_ID.Cues)),
        ebmlEl(EBML_ID.SeekPosition, uintBytes(cuesPos, 4)),
      ),
    )

  const seekHeadPlaceholder = buildSeekHeadWithCuesPos(0)
  const cluster1Rel = seekHeadPlaceholder.byteLength + info.byteLength + tracks.byteLength
  const cluster2Rel = cluster1Rel + cluster1.byteLength
  const cuesRel = cluster2Rel + cluster2.byteLength

  const seekHead = buildSeekHeadWithCuesPos(cuesRel)
  const cues = ebmlEl(
    EBML_ID.Cues,
    ebmlEl(
      EBML_ID.CuePoint,
      ebmlEl(EBML_ID.CueTime, uintBytes(0)),
      ebmlEl(
        EBML_ID.CueTrackPositions,
        ebmlEl(EBML_ID.CueTrack, uintBytes(1)),
        ebmlEl(EBML_ID.CueClusterPosition, uintBytes(cluster1Rel)),
      ),
    ),
    ebmlEl(
      EBML_ID.CuePoint,
      ebmlEl(EBML_ID.CueTime, uintBytes(30_000)),
      ebmlEl(
        EBML_ID.CueTrackPositions,
        ebmlEl(EBML_ID.CueTrack, uintBytes(1)),
        ebmlEl(EBML_ID.CueClusterPosition, uintBytes(cluster2Rel)),
      ),
    ),
  )

  const segmentBody = concat(seekHead, info, tracks, cluster1, cluster2, cues)
  const segment = ebmlEl(EBML_ID.Segment, segmentBody)
  const segmentHeaderSize = segment.byteLength - segmentBody.byteLength
  const segmentDataOffset = ebmlHeader.byteLength + segmentHeaderSize

  return {
    bytes: concat(ebmlHeader, segment),
    segmentDataOffset,
    cluster1Offset: segmentDataOffset + cluster1Rel,
    cluster2Offset: segmentDataOffset + cluster2Rel,
  }
}

function readBoxType(buf: Uint8Array, offset: number): string {
  return String.fromCharCode(buf[offset + 4], buf[offset + 5], buf[offset + 6], buf[offset + 7])
}

function readBoxSize(buf: Uint8Array, offset: number): number {
  return new DataView(buf.buffer, buf.byteOffset + offset, 4).getUint32(0, false)
}

function findChildBox(buf: Uint8Array, start: number, end: number, targetType: string): { offset: number; size: number } | null {
  let pos = start
  while (pos + 8 <= end) {
    const size = readBoxSize(buf, pos)
    if (size < 8 || pos + size > end) break
    if (readBoxType(buf, pos) === targetType) {
      return { offset: pos, size }
    }
    pos += size
  }
  return null
}

// ==================== 单元测试 ====================

describe('EBML VINT & Header Parser', () => {
  it('parses 1-byte, 2-byte, 4-byte VINTs and unknown size markers', () => {
    // 1-byte data size: 0x85 -> 5
    expect(readVint(new Uint8Array([0x85]), 0)).toEqual({ value: 5, length: 1, isUnknown: false })
    // 1-byte keepMarker (Element ID): 0xA3 -> 0xA3
    expect(readVint(new Uint8Array([0xa3]), 0, true)).toEqual({
      value: 0xa3,
      length: 1,
      isUnknown: false,
    })
    // 2-byte data size: 0x41 0x00 -> 256
    expect(readVint(new Uint8Array([0x41, 0x00]), 0)).toEqual({
      value: 256,
      length: 2,
      isUnknown: false,
    })
    // 4-byte Element ID: EBML 0x1A45DFA3
    expect(readVint(new Uint8Array([0x1a, 0x45, 0xdf, 0xa3]), 0, true)).toEqual({
      value: EBML_ID.EBML,
      length: 4,
      isUnknown: false,
    })
    // Unknown size (all data bits = 1): 0xFF and 0x01FFFFFFFFFFFFFF
    expect(readVint(new Uint8Array([0xff]), 0)).toEqual({ value: -1, length: 1, isUnknown: true })
    expect(
      readVint(new Uint8Array([0x01, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff]), 0),
    ).toEqual({ value: -1, length: 8, isUnknown: true })
    // Invalid / truncated
    expect(readVint(new Uint8Array([0x00]), 0)).toBeNull()
    expect(readVint(new Uint8Array([0x40]), 0)).toBeNull()
  })

  it('parses EBML header, Segment Info, SeekHead, Tracks, and Cues from synthetic MKV', () => {
    const { bytes, segmentDataOffset, cluster1Offset, cluster2Offset } = buildSyntheticMkvFile()

    const header = parseEbmlHeader(bytes)
    expect(header).not.toBeNull()
    expect(header!.docType).toBe('matroska')
    expect(header!.docTypeVersion).toBe(4)
    expect(header!.segmentDataOffset).toBe(segmentDataOffset)

    // 扫描顶层子元素
    let pos = header!.segmentDataOffset
    const seekEntries = []
    let infoDuration = 0
    let tracks: MkvTrackInfo[] = []
    let cues = []

    while (pos < bytes.byteLength) {
      const el = readElementHeader(bytes, pos)
      if (!el || el.endOffset > bytes.byteLength) break
      const slice = bytes.subarray(pos, el.endOffset)
      if (el.id === EBML_ID.SeekHead) {
        seekEntries.push(...parseSeekHead(slice, segmentDataOffset))
      } else if (el.id === EBML_ID.Info) {
        const info = parseSegmentInfo(slice)
        expect(info.timecodeScale).toBe(1_000_000)
        infoDuration = info.durationSec
      } else if (el.id === EBML_ID.Tracks) {
        tracks = parseTracks(slice)
      } else if (el.id === EBML_ID.Cues) {
        cues = parseCues(slice, 1_000_000, segmentDataOffset)
      }
      pos = el.endOffset
    }

    expect(infoDuration).toBeCloseTo(120, 3)
    expect(seekEntries.some((s) => s.id === EBML_ID.Cues)).toBe(true)

    expect(tracks).toHaveLength(2)
    expect(tracks[0]).toMatchObject({
      trackNumber: 1,
      trackType: 'video',
      codecId: 'V_MPEG4/ISO/AVC',
      width: 1920,
      height: 1080,
    })
    expect(tracks[0].codecPrivate?.byteLength).toBe(12)
    expect(tracks[1]).toMatchObject({
      trackNumber: 2,
      trackType: 'audio',
      codecId: 'A_AAC',
      sampleRate: 48000,
      channels: 2,
    })

    expect(cues).toHaveLength(2)
    expect(cues[0]).toMatchObject({ timeSec: 0, trackNumber: 1, fileOffset: cluster1Offset })
    expect(cues[1]).toMatchObject({ timeSec: 30, trackNumber: 1, fileOffset: cluster2Offset })

    // 二分查找 Seek 偏移
    expect(lookupCueOffset(cues, 0)?.fileOffset).toBe(cluster1Offset)
    expect(lookupCueOffset(cues, 15)?.fileOffset).toBe(cluster1Offset)
    expect(lookupCueOffset(cues, 30)?.fileOffset).toBe(cluster2Offset)
    expect(lookupCueOffset(cues, 95)?.fileOffset).toBe(cluster2Offset)
    expect(lookupCueOffset([], 10)).toBeNull()
  })
})

describe('Cluster Streaming Parser', () => {
  it('extracts SimpleBlock, BlockGroup, and handles chunk boundary truncation', () => {
    const blockPayload = concat(
      encodeVintSize(1),
      new Uint8Array([0x00, 0x50, 0x00]), // relTime = 80ms, flags = 0
      new Uint8Array([0x11, 0x22, 0x33]),
    )
    const blockGroup = ebmlEl(
      EBML_ID.BlockGroup,
      ebmlEl(EBML_ID.Block, blockPayload),
      ebmlEl(EBML_ID.BlockDuration, uintBytes(40)),
    )

    const cluster = ebmlEl(
      EBML_ID.Cluster,
      ebmlEl(EBML_ID.Timecode, uintBytes(1000)),
      makeSimpleBlock(1, 0, 0x80, new Uint8Array([0xaa, 0xbb])),
      makeSimpleBlock(2, 20, 0x80, new Uint8Array([0xcc, 0xdd])),
      blockGroup,
    )

    // 模拟在倒数第 5 字节被 Range 分块截断
    const splitAt = cluster.byteLength - 5
    const part1 = cluster.subarray(0, splitAt)
    const part2 = cluster.subarray(splitAt)

    const res1 = parseClusterFrames(part1)
    expect(res1.frames).toHaveLength(2)
    expect(res1.frames[0]).toMatchObject({ trackNumber: 1, timestampMs: 1000, isKeyframe: true })
    expect(res1.frames[1]).toMatchObject({ trackNumber: 2, timestampMs: 1020, isKeyframe: true })
    expect(res1.consumedBytes).toBeLessThan(part1.byteLength)

    // 拼接剩余字节 + part2 继续解析
    const combined = concat(part1.subarray(res1.consumedBytes), part2)
    const res2 = parseClusterFrames(combined, { clusterBaseTimecode: res1.lastClusterTimecode })
    expect(res2.frames).toHaveLength(1)
    expect(res2.frames[0]).toMatchObject({
      trackNumber: 1,
      timestampMs: 1080,
      durationMs: 40,
      isKeyframe: true, // 无 ReferenceBlock -> 关键帧
    })
    expect(Array.from(res2.frames[0].data)).toEqual([0x11, 0x22, 0x33])
  })

  it('supports fixed-size lacing inside SimpleBlock', () => {
    // flags = 0x84 (keyframe + fixed-size lacing = 2 << 1)
    const lacedPayload = new Uint8Array([
      0x01, // 2 frames (count - 1 = 1)
      0xaa, 0xbb, // frame 1
      0xcc, 0xdd, // frame 2
    ])
    const cluster = ebmlEl(
      EBML_ID.Cluster,
      ebmlEl(EBML_ID.Timecode, uintBytes(500)),
      makeSimpleBlock(2, 0, 0x84, lacedPayload),
    )

    const res = parseClusterFrames(cluster)
    expect(res.frames).toHaveLength(2)
    expect(Array.from(res.frames[0].data)).toEqual([0xaa, 0xbb])
    expect(Array.from(res.frames[1].data)).toEqual([0xcc, 0xdd])
  })
})

describe('fMP4 Box Builder (Init & Media Segments)', () => {
  it('builds valid ftyp + moov init segment for AVC + AAC and HEVC + Opus tracks', () => {
    const tracks: MkvTrackInfo[] = [
      {
        trackNumber: 1,
        trackType: 'video',
        codecId: 'V_MPEG4/ISO/AVC',
        width: 1920,
        height: 1080,
        codecPrivate: new Uint8Array([0x01, 0x64, 0x00, 0x28, 0xff, 0xe1, 0x00, 0x02, 0x67, 0x64]),
      },
      {
        trackNumber: 2,
        trackType: 'audio',
        codecId: 'A_AAC',
        sampleRate: 48000,
        channels: 2,
        codecPrivate: new Uint8Array([0x11, 0x90]),
      },
    ]

    const init = buildFmp4InitSegment(tracks, { durationSec: 60 })
    const ftyp = findChildBox(init, 0, init.byteLength, 'ftyp')
    const moov = findChildBox(init, 0, init.byteLength, 'moov')
    expect(ftyp).not.toBeNull()
    expect(moov).not.toBeNull()
    expect(ftyp!.size + moov!.size).toBe(init.byteLength)

    const mvhd = findChildBox(init, moov!.offset + 8, moov!.offset + moov!.size, 'mvhd')
    const mvex = findChildBox(init, moov!.offset + 8, moov!.offset + moov!.size, 'mvex')
    expect(mvhd).not.toBeNull()
    expect(mvex).not.toBeNull()

    // HEVC + Opus init segment
    const hevcOpus = buildFmp4InitSegment([
      { trackNumber: 1, trackType: 'video', codecId: 'V_MPEGH/ISO/HEVC', width: 3840, height: 2160 },
      { trackNumber: 2, trackType: 'audio', codecId: 'A_OPUS', sampleRate: 48000, channels: 2 },
    ])
    const ascii = new TextDecoder('latin1').decode(hevcOpus)
    expect(ascii).toContain('hvc1')
    expect(ascii).toContain('hvcC')
    expect(ascii).toContain('Opus')
    expect(ascii).toContain('dOps')
  })

  it('builds valid moof + mdat media segment with accurate trun data_offset', () => {
    const frames: MkvFrame[] = [
      { trackNumber: 1, timestampMs: 1000, isKeyframe: true, data: new Uint8Array([1, 2, 3, 4]) },
      { trackNumber: 1, timestampMs: 1040, isKeyframe: false, data: new Uint8Array([5, 6, 7]) },
    ]

    const seg = buildFmp4MediaSegment(frames, { sequenceNumber: 3, trackId: 1 })
    const moof = findChildBox(seg, 0, seg.byteLength, 'moof')
    const mdat = findChildBox(seg, 0, seg.byteLength, 'mdat')
    expect(moof).not.toBeNull()
    expect(mdat).not.toBeNull()
    expect(moof!.size + mdat!.size).toBe(seg.byteLength)
    expect(mdat!.size).toBe(8 + 4 + 3)

    // 验证 trun 中的 data_offset === moof.size + 8
    const traf = findChildBox(seg, moof!.offset + 8, moof!.offset + moof!.size, 'traf')
    expect(traf).not.toBeNull()
    const trun = findChildBox(seg, traf!.offset + 8, traf!.offset + traf!.size, 'trun')
    expect(trun).not.toBeNull()
    const dataOffset = new DataView(seg.buffer, seg.byteOffset + trun!.offset + 16, 4).getInt32(0, false)
    expect(dataOffset).toBe(moof!.size + 8)
  })
})

describe('Codec Capability Detection & Content-Range', () => {
  const originalMediaSource = globalThis.MediaSource

  afterEach(() => {
    if (originalMediaSource === undefined) {
      // @ts-expect-error restore test global
      delete globalThis.MediaSource
    } else {
      globalThis.MediaSource = originalMediaSource
    }
  })

  it('detects supported and unsupported codecs via MediaSource.isTypeSupported', () => {
    const isTypeSupported = vi.fn((mime: string) => {
      if (mime.includes('hvc1')) return false
      return true
    })
    // @ts-expect-error mock MediaSource
    globalThis.MediaSource = { isTypeSupported }

    expect(isMkvContainer('matroska,webm')).toBe(true)
    expect(isMkvContainer('mp4')).toBe(false)

    expect(canStreamMkvRemux('h264', 'aac', 'mkv')).toBe(true)
    expect(canStreamMkvRemux('h264', 'ac3', 'mkv')).toBe(false) // AC3 不支持，需回退
    expect(canStreamMkvRemux('hevc', 'aac', 'mkv')).toBe(false) // mock 浏览器不支持 HEVC
    expect(canStreamMkvRemux('mpeg2video', 'aac', 'mkv')).toBe(false)
    expect(canStreamMkvRemux('h264', 'aac', 'mp4')).toBe(false)

    const resolved = resolveMkvMimeType(
      {
        trackNumber: 1,
        trackType: 'video',
        codecId: 'V_MPEG4/ISO/AVC',
        codecPrivate: new Uint8Array([0x01, 0x4d, 0x40, 0x1f]),
      },
      { trackNumber: 2, trackType: 'audio', codecId: 'A_AAC' },
    )
    expect(resolved.videoMime).toBe('video/mp4; codecs="avc1.4d401f"')
    expect(resolved.audioMime).toBe('audio/mp4; codecs="mp4a.40.2"')
  })

  it('parses total bytes from Content-Range header', () => {
    expect(parseContentRangeTotal('bytes 0-524287/5368709120')).toBe(5368709120)
    expect(parseContentRangeTotal('bytes 0-100/*')).toBe(0)
    expect(parseContentRangeTotal(null)).toBe(0)
  })
})

describe('StreamMkvRemuxer Controller (Range + MSE + Seek + Dispose)', () => {
  const originalMediaSource = globalThis.MediaSource
  const originalCreateObjectURL = URL.createObjectURL
  const originalRevokeObjectURL = URL.revokeObjectURL

  afterEach(() => {
    if (originalMediaSource === undefined) {
      // @ts-expect-error restore
      delete globalThis.MediaSource
    } else {
      globalThis.MediaSource = originalMediaSource
    }
    URL.createObjectURL = originalCreateObjectURL
    URL.revokeObjectURL = originalRevokeObjectURL
  })

  function installMockMSE() {
    const appendedByMime = new Map<string, Uint8Array[]>()
    const createdUrls: string[] = []
    const revokedUrls: string[] = []

    class FakeSourceBuffer extends EventTarget {
      updating = false
      private mime: string
      constructor(mime: string) {
        super()
        this.mime = mime
        appendedByMime.set(mime, [])
      }
      appendBuffer(data: BufferSource) {
        const bytes =
          data instanceof ArrayBuffer
            ? new Uint8Array(data.slice(0))
            : new Uint8Array(data.buffer.slice(data.byteOffset, data.byteOffset + data.byteLength))
        appendedByMime.get(this.mime)!.push(bytes)
        this.updating = true
        queueMicrotask(() => {
          this.updating = false
          this.dispatchEvent(new Event('updateend'))
        })
      }
    }

    class FakeMediaSource extends EventTarget {
      readyState: ReadyState = 'closed'
      duration = NaN
      sourceBuffers: FakeSourceBuffer[] = []
      static isTypeSupported = vi.fn((mime: string) => !mime.includes('unsupported'))

      constructor() {
        super()
        queueMicrotask(() => {
          this.readyState = 'open'
          this.dispatchEvent(new Event('sourceopen'))
        })
      }

      addSourceBuffer(mime: string) {
        const sb = new FakeSourceBuffer(mime)
        this.sourceBuffers.push(sb)
        return sb
      }

      endOfStream() {
        this.readyState = 'ended'
      }
    }

    // @ts-expect-error mock global MediaSource
    globalThis.MediaSource = FakeMediaSource
    URL.createObjectURL = vi.fn(() => {
      const u = `blob:mock-mse-${createdUrls.length + 1}`
      createdUrls.push(u)
      return u
    })
    URL.revokeObjectURL = vi.fn((u: string) => {
      revokedUrls.push(u)
    })

    return { appendedByMime, createdUrls, revokedUrls }
  }

  it('fetches header + chunks via Range, appends fMP4 to MSE, seeks via Cues, and disposes cleanly', async () => {
    const { bytes, cluster2Offset } = buildSyntheticMkvFile()
    const { appendedByMime, createdUrls, revokedUrls } = installMockMSE()

    const requestedRanges: string[] = []
    const mockFetch = vi.fn(async (_input: RequestInfo | URL, init?: RequestInit) => {
      const headers = (init?.headers || {}) as Record<string, string>
      const range = headers.Range || headers.range || ''
      requestedRanges.push(range)
      const match = /bytes=(\d+)-(\d+)/.exec(range)
      const start = match ? Number(match[1]) : 0
      const end = match ? Math.min(bytes.byteLength - 1, Number(match[2])) : bytes.byteLength - 1
      const slice = bytes.slice(start, end + 1)
      return new Response(slice, {
        status: 206,
        headers: {
          'Content-Range': `bytes ${start}-${end}/${bytes.byteLength}`,
          'Content-Length': String(slice.byteLength),
        },
      })
    }) as unknown as typeof fetch

    const videoEl = document.createElement('video')
    Object.defineProperty(videoEl, 'buffered', {
      configurable: true,
      value: { length: 0, start: () => 0, end: () => 0 },
    })

    const remuxer = new StreamMkvRemuxer()
    const meta = await remuxer.attach(videoEl, '/emby/Videos/1/stream', { 'X-Emby-Token': 'tok' }, {
      fetchImpl: mockFetch,
      initialHeaderBytes: 512 * 1024,
      chunkSizeBytes: 256 * 1024,
    })

    expect(meta.docType).toBe('matroska')
    expect(meta.durationSec).toBeCloseTo(120, 2)
    expect(meta.cuesCount).toBe(2)
    expect(createdUrls).toHaveLength(1)
    expect(requestedRanges[0]).toBe('bytes=0-524287')

    // 等待微任务让后台 pumpLoop 完成分块写入
    await new Promise((r) => setTimeout(r, 30))

    const videoChunks = appendedByMime.get('video/mp4; codecs="avc1.640028"') || []
    const audioChunks = appendedByMime.get('audio/mp4; codecs="mp4a.40.2"') || []
    // 至少包含 1 个 init segment (ftyp+moov) + 1 个 media segment (moof+mdat)
    expect(videoChunks.length).toBeGreaterThanOrEqual(2)
    expect(readBoxType(videoChunks[0], 0)).toBe('ftyp')
    expect(readBoxType(videoChunks[1], 0)).toBe('moof')
    expect(audioChunks.length).toBeGreaterThanOrEqual(2)

    // 模拟拖动 Seek 到 35 秒 -> 应命中 Cue 2 并按 cluster2Offset 发起新 Range 请求
    expect(remuxer.resolveSeekByteOffset(35)).toBe(cluster2Offset)
    Object.defineProperty(videoEl, 'currentTime', { configurable: true, writable: true, value: 35 })
    videoEl.dispatchEvent(new Event('seeking'))
    await new Promise((r) => setTimeout(r, 30))

    expect(requestedRanges.some((r) => r.startsWith(`bytes=${cluster2Offset}-`))).toBe(true)

    remuxer.dispose()
    expect(revokedUrls).toEqual(createdUrls)
  })

  it('throws StreamMkvFallbackError when MKV contains unsupported audio codec (e.g. AC3)', async () => {
    const { bytes } = buildSyntheticMkvFile({ audioCodecId: 'A_AC3' })
    installMockMSE()

    const mockFetch = vi.fn(async () => {
      return new Response(bytes, {
        status: 206,
        headers: {
          'Content-Range': `bytes 0-${bytes.byteLength - 1}/${bytes.byteLength}`,
        },
      })
    }) as unknown as typeof fetch

    const videoEl = document.createElement('video')
    const remuxer = new StreamMkvRemuxer()
    await expect(
      remuxer.attach(videoEl, '/emby/Videos/2/stream', {}, { fetchImpl: mockFetch }),
    ).rejects.toThrow(StreamMkvFallbackError)
    remuxer.dispose()
  })

  it('rejects oversized HTTP 200 non-Range response and revokes objectUrl on attach failure', async () => {
    const { createdUrls, revokedUrls } = installMockMSE()

    let cancelled = false
    const stream = new ReadableStream<Uint8Array>({
      pull(controller) {
        controller.enqueue(new Uint8Array(256 * 1024))
      },
      cancel() {
        cancelled = true
      },
    })
    const mockFetch = vi.fn(async () => {
      return new Response(stream, {
        status: 200,
        headers: {
          'Content-Length': String(100 * 1024 * 1024),
        },
      })
    }) as unknown as typeof fetch

    const videoEl = document.createElement('video')
    const remuxer = new StreamMkvRemuxer()
    await expect(
      remuxer.attach(videoEl, '/emby/Videos/3/stream', {}, {
        fetchImpl: mockFetch,
        initialHeaderBytes: 64 * 1024,
      }),
    ).rejects.toThrow(StreamMkvFallbackError)

    expect(cancelled).toBe(true)
    expect(revokedUrls).toEqual(createdUrls)
  })

  it('builds trun v1 with signed CTS offsets for B-frame reordered PTS sequence', () => {
    // 典型 I-P-B-B 解码顺序：PTS = [0, 120, 40, 80] (每帧 40ms)
    const frames: MkvFrame[] = [
      { trackNumber: 1, timestampMs: 0, isKeyframe: true, data: new Uint8Array([1, 2]) },
      { trackNumber: 1, timestampMs: 120, isKeyframe: false, data: new Uint8Array([3, 4]) },
      { trackNumber: 1, timestampMs: 40, isKeyframe: false, data: new Uint8Array([5, 6]) },
      { trackNumber: 1, timestampMs: 80, isKeyframe: false, data: new Uint8Array([7, 8]) },
    ]

    const seg = buildFmp4MediaSegment(frames, { sequenceNumber: 1, trackId: 1 })
    const moof = findChildBox(seg, 0, seg.byteLength, 'moof')!
    const traf = findChildBox(seg, moof.offset + 8, moof.offset + moof.size, 'traf')!
    const trun = findChildBox(seg, traf.offset + 8, traf.offset + traf.size, 'trun')!
    const view = new DataView(seg.buffer, seg.byteOffset + trun.offset, trun.size)

    // version === 1, flags === 0x000f01 (包含 sample_composition_time_offset)
    expect(view.getUint8(8)).toBe(1)
    const flags = (view.getUint8(9) << 16) | (view.getUint8(10) << 8) | view.getUint8(11)
    expect(flags).toBe(0x000f01)
    expect(view.getUint32(12, false)).toBe(4)

    // 每个 sample 16 字节: duration(4), size(4), flags(4), cts(int32, 4)
    const durs: number[] = []
    const ctsList: number[] = []
    let dts = 0
    for (let i = 0; i < 4; i++) {
      const off = 20 + i * 16
      const dur = view.getUint32(off, false)
      const cts = view.getInt32(off + 12, false)
      durs.push(dur)
      ctsList.push(cts)
      expect(dts + cts).toBe(frames[i].timestampMs)
      dts += dur
    }
    expect(durs).toEqual([40, 40, 40, 40])
    expect(ctsList).toEqual([0, 80, -40, -40])
  })

  it('skips >512KB Attachments element before Tracks/Cluster during MKV header scan', async () => {
    const ebmlHeader = ebmlEl(
      EBML_ID.EBML,
      ebmlEl(EBML_ID.EBMLVersion, uintBytes(1)),
      ebmlEl(EBML_ID.DocType, strBytes('matroska')),
    )
    const info = ebmlEl(
      EBML_ID.Info,
      ebmlEl(EBML_ID.TimecodeScale, uintBytes(1_000_000)),
      ebmlEl(EBML_ID.Duration, float64Bytes(60_000)),
    )
    // 模拟 600KB 字体附件位于 Tracks 之前（超过 512KB 初始拉取窗口）
    const largeAttachment = ebmlEl(EBML_ID.Attachments, new Uint8Array(600 * 1024))
    const avcC = new Uint8Array([0x01, 0x64, 0x00, 0x28, 0xff, 0xe1, 0x00, 0x04, 0x67, 0x64, 0x00, 0x28])
    const tracks = ebmlEl(
      EBML_ID.Tracks,
      ebmlEl(
        EBML_ID.TrackEntry,
        ebmlEl(EBML_ID.TrackNumber, uintBytes(1)),
        ebmlEl(EBML_ID.TrackType, uintBytes(1)),
        ebmlEl(EBML_ID.CodecID, strBytes('V_MPEG4/ISO/AVC')),
        ebmlEl(EBML_ID.CodecPrivate, avcC),
        ebmlEl(EBML_ID.Video, ebmlEl(EBML_ID.PixelWidth, uintBytes(1920)), ebmlEl(EBML_ID.PixelHeight, uintBytes(1080))),
      ),
    )
    const cluster = ebmlEl(
      EBML_ID.Cluster,
      ebmlEl(EBML_ID.Timecode, uintBytes(0)),
      makeSimpleBlock(1, 0, 0x80, new Uint8Array([0x00, 0x00, 0x00, 0x02, 0x65, 0x88])),
    )
    const segment = ebmlEl(EBML_ID.Segment, concat(info, largeAttachment, tracks, cluster))
    const bytes = concat(ebmlHeader, segment)

    installMockMSE()
    const mockFetch = vi.fn(async (_input: RequestInfo | URL, init?: RequestInit) => {
      const headers = (init?.headers || {}) as Record<string, string>
      const range = headers.Range || headers.range || ''
      const match = /bytes=(\d+)-(\d+)/.exec(range)
      const start = match ? Number(match[1]) : 0
      const end = match ? Math.min(bytes.byteLength - 1, Number(match[2])) : bytes.byteLength - 1
      const slice = bytes.slice(start, end + 1)
      return new Response(slice, {
        status: 206,
        headers: {
          'Content-Range': `bytes ${start}-${end}/${bytes.byteLength}`,
          'Content-Length': String(slice.byteLength),
        },
      })
    }) as unknown as typeof fetch

    const videoEl = document.createElement('video')
    Object.defineProperty(videoEl, 'buffered', {
      configurable: true,
      value: { length: 0, start: () => 0, end: () => 0 },
    })

    const remuxer = new StreamMkvRemuxer()
    const meta = await remuxer.attach(videoEl, '/emby/Videos/big-att/stream', {}, {
      fetchImpl: mockFetch,
      initialHeaderBytes: 512 * 1024,
    })

    expect(meta.videoTrack?.codecId).toBe('V_MPEG4/ISO/AVC')
    expect(meta.durationSec).toBeCloseTo(60, 2)
    remuxer.dispose()
  })

  it('resyncs to valid Cluster header and aligns keyframe on linear Seek without Cues', async () => {
    const ebmlHeader = ebmlEl(
      EBML_ID.EBML,
      ebmlEl(EBML_ID.DocType, strBytes('matroska')),
    )
    const info = ebmlEl(
      EBML_ID.Info,
      ebmlEl(EBML_ID.TimecodeScale, uintBytes(1_000_000)),
      ebmlEl(EBML_ID.Duration, float64Bytes(100_000)),
    )
    const avcC = new Uint8Array([0x01, 0x64, 0x00, 0x28, 0xff, 0xe1, 0x00, 0x04, 0x67, 0x64, 0x00, 0x28])
    const tracks = ebmlEl(
      EBML_ID.Tracks,
      ebmlEl(
        EBML_ID.TrackEntry,
        ebmlEl(EBML_ID.TrackNumber, uintBytes(1)),
        ebmlEl(EBML_ID.TrackType, uintBytes(1)),
        ebmlEl(EBML_ID.CodecID, strBytes('V_MPEG4/ISO/AVC')),
        ebmlEl(EBML_ID.CodecPrivate, avcC),
      ),
    )
    // Cluster 1 带大负载以便线性 Seek 落在 Cluster 1 中间字节
    const cluster1 = ebmlEl(
      EBML_ID.Cluster,
      ebmlEl(EBML_ID.Timecode, uintBytes(0)),
      makeSimpleBlock(1, 0, 0x80, new Uint8Array(4000).fill(0x55)),
    )
    // Cluster 2 开头先有一帧非关键帧 (50000ms)，再有一帧关键帧 (50040ms)
    const cluster2 = ebmlEl(
      EBML_ID.Cluster,
      ebmlEl(EBML_ID.Timecode, uintBytes(50_000)),
      makeSimpleBlock(1, 0, 0x00, new Uint8Array([0x41, 0x01])),
      makeSimpleBlock(1, 40, 0x80, new Uint8Array([0x65, 0x02])),
    )
    const segment = ebmlEl(EBML_ID.Segment, concat(info, tracks, cluster1, cluster2))
    const bytes = concat(ebmlHeader, segment)

    // 先直接验证 findNextClusterOffset 能在随机垃圾字节中精确定位 Cluster2
    const junk = new Uint8Array([0x00, 0x11, 0x1f, 0x43, 0xb6, 0x75, 0x00, 0x99])
    const probeBuf = concat(junk, cluster2)
    expect(findNextClusterOffset(probeBuf, 0)).toBe(junk.byteLength)

    const { appendedByMime } = installMockMSE()
    const mockFetch = vi.fn(async (_input: RequestInfo | URL, init?: RequestInit) => {
      const headers = (init?.headers || {}) as Record<string, string>
      const range = headers.Range || headers.range || ''
      const match = /bytes=(\d+)-(\d+)/.exec(range)
      const start = match ? Number(match[1]) : 0
      const end = match ? Math.min(bytes.byteLength - 1, Number(match[2])) : bytes.byteLength - 1
      const slice = bytes.slice(start, end + 1)
      return new Response(slice, {
        status: 206,
        headers: {
          'Content-Range': `bytes ${start}-${end}/${bytes.byteLength}`,
          'Content-Length': String(slice.byteLength),
        },
      })
    }) as unknown as typeof fetch

    const videoEl = document.createElement('video')
    Object.defineProperty(videoEl, 'buffered', {
      configurable: true,
      value: { length: 0, start: () => 0, end: () => 0 },
    })

    const remuxer = new StreamMkvRemuxer()
    const meta = await remuxer.attach(videoEl, '/emby/Videos/no-cues/stream', {}, {
      fetchImpl: mockFetch,
      initialHeaderBytes: 512 * 1024,
      chunkSizeBytes: 256 * 1024,
    })
    expect(meta.cuesCount).toBe(0)
    await new Promise((r) => setTimeout(r, 30))

    // 无 Cues 线性 Seek 到 45 秒 -> 落在 Cluster 1 中间，应重同步到 Cluster 2 并跳至 50.04s 关键帧
    const beforeCount = (appendedByMime.get('video/mp4; codecs="avc1.640028"') || []).length
    Object.defineProperty(videoEl, 'currentTime', { configurable: true, writable: true, value: 45 })
    videoEl.dispatchEvent(new Event('seeking'))
    await new Promise((r) => setTimeout(r, 40))

    const videoChunks = appendedByMime.get('video/mp4; codecs="avc1.640028"') || []
    expect(videoChunks.length).toBeGreaterThan(beforeCount)
    // 验证最新写入的分片 tfdt baseMediaDecodeTime 对齐到关键帧 50040ms（跳过了 50000ms 非关键帧）
    const lastSeg = videoChunks[videoChunks.length - 1]
    const moof = findChildBox(lastSeg, 0, lastSeg.byteLength, 'moof')!
    const traf = findChildBox(lastSeg, moof.offset + 8, moof.offset + moof.size, 'traf')!
    const tfdt = findChildBox(lastSeg, traf.offset + 8, traf.offset + traf.size, 'tfdt')!
    const tfdtView = new DataView(lastSeg.buffer, lastSeg.byteOffset + tfdt.offset + 12, 8)
    const baseDecodeMs = tfdtView.getUint32(0, false) * 0x100000000 + tfdtView.getUint32(4, false)
    expect(baseDecodeMs).toBe(50040)
    expect(videoEl.currentTime).toBeCloseTo(50.04, 2)

    remuxer.dispose()
  })

  it('[H-14] parses 5.1/7.1 OpusHead CodecPrivate into big-endian ISO BMFF dOps with channel mapping and rejects >2ch without OpusHead', () => {
    // 构造 5.1 (6 声道, family = 1) OpusHead (小端序):
    // Magic "OpusHead"(8) + Version(1)=1 + Channels(1)=6 + PreSkip(2, LE)=312(0x0138 -> [0x38,0x01])
    // + InputSampleRate(4, LE)=48000(0x0000BB80 -> [0x80,0xbb,0x00,0x00])
    // + OutputGain(2, LE)=-256(0xFF00 -> [0x00,0xff])
    // + MappingFamily(1)=1 + StreamCount(1)=4 + CoupledCount(1)=2 + Mapping(6)=[0,4,1,2,3,5]
    const opusHead51 = new Uint8Array([
      0x4f, 0x70, 0x75, 0x73, 0x48, 0x65, 0x61, 0x64,
      1,
      6,
      0x38, 0x01,
      0x80, 0xbb, 0x00, 0x00,
      0x00, 0xff,
      1,
      4,
      2,
      0, 4, 1, 2, 3, 5,
    ])

    const sampleEntry = buildAudioSampleEntry({
      trackNumber: 2,
      trackType: 'audio',
      codecId: 'A_OPUS',
      channels: 6,
      sampleRate: 48000,
      codecPrivate: opusHead51,
    })

    expect(readBoxType(sampleEntry, 0)).toBe('Opus')
    const dOps = findChildBox(sampleEntry, 36, sampleEntry.byteLength, 'dOps')
    expect(dOps).not.toBeNull()
    const dOpsBody = sampleEntry.subarray(dOps!.offset + 8, dOps!.offset + dOps!.size)
    const dv = new DataView(dOpsBody.buffer, dOpsBody.byteOffset, dOpsBody.byteLength)

    expect(dv.getUint8(0)).toBe(0) // dOps Version = 0
    expect(dv.getUint8(1)).toBe(6) // OutputChannelCount = 6
    expect(dv.getUint16(2, false)).toBe(312) // PreSkip big-endian
    expect(dv.getUint32(4, false)).toBe(48000) // InputSampleRate big-endian
    expect(dv.getInt16(8, false)).toBe(-256) // OutputGain big-endian
    expect(dv.getUint8(10)).toBe(1) // ChannelMappingFamily = 1
    expect(dv.getUint8(11)).toBe(4) // StreamCount
    expect(dv.getUint8(12)).toBe(2) // CoupledCount
    expect(Array.from(dOpsBody.subarray(13, 19))).toEqual([0, 4, 1, 2, 3, 5])

    // 缺少 OpusHead 且 channels > 2 时必须抛出 StreamMkvFallbackError 平滑回退
    expect(() =>
      buildAudioSampleEntry({
        trackNumber: 2,
        trackType: 'audio',
        codecId: 'A_OPUS',
        channels: 6,
        sampleRate: 48000,
      }),
    ).toThrow(StreamMkvFallbackError)
  })

  it('[H-14] reassembles >4MB single UHD IDR SimpleBlock across Range chunks without losing sync', async () => {
    const ebmlHeader = ebmlEl(EBML_ID.EBML, ebmlEl(EBML_ID.DocType, strBytes('matroska')))
    const info = ebmlEl(
      EBML_ID.Info,
      ebmlEl(EBML_ID.TimecodeScale, uintBytes(1_000_000)),
      ebmlEl(EBML_ID.Duration, float64Bytes(10_000)),
    )
    const avcC = new Uint8Array([0x01, 0x64, 0x00, 0x28, 0xff, 0xe1, 0x00, 0x04, 0x67, 0x64, 0x00, 0x28])
    const tracks = ebmlEl(
      EBML_ID.Tracks,
      ebmlEl(
        EBML_ID.TrackEntry,
        ebmlEl(EBML_ID.TrackNumber, uintBytes(1)),
        ebmlEl(EBML_ID.TrackType, uintBytes(1)),
        ebmlEl(EBML_ID.CodecID, strBytes('V_MPEG4/ISO/AVC')),
        ebmlEl(EBML_ID.CodecPrivate, avcC),
      ),
    )

    // 构造 5MB 单帧（超过旧 4MB MAX_LEFTOVER_BYTES 限制，小于 32MB 硬上限）
    const fiveMbPayload = new Uint8Array(5 * 1024 * 1024)
    fiveMbPayload[0] = 0x65
    fiveMbPayload[fiveMbPayload.byteLength - 1] = 0xab
    const cluster = ebmlEl(
      EBML_ID.Cluster,
      ebmlEl(EBML_ID.Timecode, uintBytes(0)),
      makeSimpleBlock(1, 0, 0x80, fiveMbPayload),
      makeSimpleBlock(1, 40, 0x00, new Uint8Array([0x41, 0x02])),
    )

    // 先验证 parseClusterFrames 对截断的大帧精确返回 neededBytes
    const partial = cluster.subarray(0, 1024 * 1024)
    const partialRes = parseClusterFrames(partial)
    expect(partialRes.consumedBytes).toBeGreaterThan(0) // Cluster header + Timecode consumed
    const remainingBlockStart = cluster.subarray(partialRes.consumedBytes, 1024 * 1024)
    const blockRes = parseClusterFrames(remainingBlockStart)
    expect(blockRes.consumedBytes).toBe(0)
    expect(blockRes.neededBytes).toBeGreaterThan(5 * 1024 * 1024)

    // 再验证完整泵循环跨多个 1MB Range 请求成功拼齐并推入 MSE
    const segment = ebmlEl(EBML_ID.Segment, concat(info, tracks, cluster))
    const bytes = concat(ebmlHeader, segment)
    const { appendedByMime } = installMockMSE()
    const onError = vi.fn()

    const mockFetch = vi.fn(async (_input: RequestInfo | URL, init?: RequestInit) => {
      const headers = (init?.headers || {}) as Record<string, string>
      const range = headers.Range || headers.range || ''
      const match = /bytes=(\d+)-(\d+)/.exec(range)
      const start = match ? Number(match[1]) : 0
      const end = match ? Math.min(bytes.byteLength - 1, Number(match[2])) : bytes.byteLength - 1
      const slice = bytes.slice(start, end + 1)
      return new Response(slice, {
        status: 206,
        headers: {
          'Content-Range': `bytes ${start}-${end}/${bytes.byteLength}`,
          'Content-Length': String(slice.byteLength),
        },
      })
    }) as unknown as typeof fetch

    const videoEl = document.createElement('video')
    Object.defineProperty(videoEl, 'buffered', {
      configurable: true,
      value: { length: 0, start: () => 0, end: () => 0 },
    })

    const remuxer = new StreamMkvRemuxer()
    await remuxer.attach(videoEl, '/emby/Videos/uhd-5mb/stream', {}, {
      fetchImpl: mockFetch,
      initialHeaderBytes: 64 * 1024,
      chunkSizeBytes: 1024 * 1024, // 每次仅拉 1MB，需要连续拼接 6 次才能收齐 5MB IDR 帧
      onError,
    })

    await new Promise((r) => setTimeout(r, 80))
    expect(onError).not.toHaveBeenCalled()
    const videoChunks = appendedByMime.get('video/mp4; codecs="avc1.640028"') || []
    expect(videoChunks.length).toBeGreaterThanOrEqual(2)
    expect(videoChunks[1].byteLength).toBeGreaterThan(5 * 1024 * 1024)

    remuxer.dispose()
  })

  it('[M-18, H-01, H-14] recovers from MSE QuotaExceededError, supports videoOnly mode for AC3 MKV, and aborts cross-origin 302 redirects', async () => {
    const { bytes } = buildSyntheticMkvFile({ audioCodecId: 'A_AC3' })
    const { appendedByMime } = installMockMSE()

    expect(canStreamMkvVideoOnly('h264', 'mkv')).toBe(true)

    // 1. 跨源重定向必须在 StreamMkvRemuxer.fetchRange 中立即拦截抛出 fetch_failed
    const crossOriginRes = new Response(bytes.subarray(0, 1024), { status: 206 })
    Object.defineProperty(crossOriginRes, 'redirected', { value: true })
    Object.defineProperty(crossOriginRes, 'url', { value: 'https://evil-cdn.example.com/movie.mkv' })
    const redirectFetch = vi.fn(async () => crossOriginRes) as unknown as typeof fetch

    const videoRedirect = document.createElement('video')
    const redirectRemuxer = new StreamMkvRemuxer()
    await expect(
      redirectRemuxer.attach(videoRedirect, '/emby/Videos/strm/stream', {}, { fetchImpl: redirectFetch }),
    ).rejects.toSatisfy(
      (err: unknown) => err instanceof StreamMkvFallbackError && err.reason === 'fetch_failed',
    )
    redirectRemuxer.dispose()

    // 2. videoOnly 模式忽略不兼容的 A_AC3 音轨，且遇到 QuotaExceededError 时驱逐 currentTime - 5 之前的已播缓冲并重试
    const mockFetch = vi.fn(async (_input: RequestInfo | URL, init?: RequestInit) => {
      const headers = (init?.headers || {}) as Record<string, string>
      const range = headers.Range || headers.range || ''
      const match = /bytes=(\d+)-(\d+)/.exec(range)
      const start = match ? Number(match[1]) : 0
      const end = match ? Math.min(bytes.byteLength - 1, Number(match[2])) : bytes.byteLength - 1
      const slice = bytes.slice(start, end + 1)
      return new Response(slice, {
        status: 206,
        headers: {
          'Content-Range': `bytes ${start}-${end}/${bytes.byteLength}`,
          'Content-Length': String(slice.byteLength),
        },
      })
    }) as unknown as typeof fetch

    const removedRanges: Array<[number, number]> = []
    let throwQuotaOnce = true
    const OrigMediaSource = globalThis.MediaSource
    class QuotaMediaSource extends OrigMediaSource {
      addSourceBuffer(mime: string) {
        const sb = super.addSourceBuffer(mime) as unknown as SourceBuffer & EventTarget
        Object.defineProperty(sb, 'buffered', {
          configurable: true,
          value: {
            length: 1,
            start: () => 0,
            end: () => 20,
          },
        })
        sb.remove = vi.fn((start: number, end: number) => {
          removedRanges.push([start, end])
          Object.defineProperty(sb, 'updating', { configurable: true, writable: true, value: true })
          queueMicrotask(() => {
            Object.defineProperty(sb, 'updating', { configurable: true, writable: true, value: false })
            sb.dispatchEvent(new Event('updateend'))
          })
        })
        const origAppend = sb.appendBuffer.bind(sb)
        sb.appendBuffer = (data: BufferSource) => {
          if (throwQuotaOnce) {
            throwQuotaOnce = false
            const quotaErr = new DOMException('Buffer full', 'QuotaExceededError')
            throw quotaErr
          }
          origAppend(data)
        }
        return sb
      }
    }
    globalThis.MediaSource = QuotaMediaSource

    const videoEl = document.createElement('video')
    Object.defineProperty(videoEl, 'currentTime', { configurable: true, writable: true, value: 18 })
    Object.defineProperty(videoEl, 'buffered', {
      configurable: true,
      value: { length: 1, start: () => 0, end: () => 20 },
    })

    const remuxer = new StreamMkvRemuxer()
    const meta = await remuxer.attach(videoEl, '/emby/Videos/ac3-mkv/stream', {}, {
      fetchImpl: mockFetch,
      videoOnly: true,
    })

    expect(meta.audioTrack).toBeFalsy()
    await new Promise((r) => setTimeout(r, 40))

    // 验证 QuotaExceededError 触发了 remove(0, 13) (currentTime 18 - 5 = 13，不受 30s 限制) 并成功重试写入
    expect(removedRanges).toEqual([[0, 13]])
    expect((appendedByMime.get('video/mp4; codecs="avc1.640028"') || []).length).toBeGreaterThanOrEqual(2)

    remuxer.dispose()
  })
})
