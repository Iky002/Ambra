import { afterEach, describe, expect, it, vi } from 'vitest'
import { parsePgsSup, PgsCanvasRenderer } from '../pgsRenderer'

/**
 * 构造单个标准 Blu-ray PGS 段（13 字节头部 + payload）
 */
function buildPgsSegment(ptsSeconds: number, segType: number, payload: Uint8Array): Uint8Array {
  const seg = new Uint8Array(13 + payload.length)
  const view = new DataView(seg.buffer)
  // 'PG' 魔数
  seg[0] = 0x50
  seg[1] = 0x47
  // 90kHz PTS / DTS
  const ptsTicks = Math.round(ptsSeconds * 90000)
  view.setUint32(2, ptsTicks, false)
  view.setUint32(6, 0, false)
  seg[10] = segType
  view.setUint16(11, payload.length, false)
  seg.set(payload, 13)
  return seg
}

function concatBytes(parts: Uint8Array[]): ArrayBuffer {
  const total = parts.reduce((sum, p) => sum + p.length, 0)
  const out = new Uint8Array(total)
  let offset = 0
  for (const p of parts) {
    out.set(p, offset)
    offset += p.length
  }
  return out.buffer
}

function buildPcsPayload(opts: {
  width: number
  height: number
  state?: number
  paletteId?: number
  objects?: Array<{
    objectId: number
    windowId: number
    x: number
    y: number
    crop?: { x: number; y: number; w: number; h: number }
  }>
}): Uint8Array {
  const objs = opts.objects ?? []
  const extraLen = objs.reduce((sum, o) => sum + (o.crop ? 16 : 8), 0)
  const buf = new Uint8Array(11 + extraLen)
  const view = new DataView(buf.buffer)
  view.setUint16(0, opts.width, false)
  view.setUint16(2, opts.height, false)
  buf[4] = 0x10 // frameRate
  view.setUint16(5, 1, false) // compositionNumber
  buf[7] = opts.state ?? 0x80 // Epoch Start
  buf[8] = 0x00 // paletteUpdateFlag
  buf[9] = opts.paletteId ?? 0
  buf[10] = objs.length

  let pos = 11
  for (const o of objs) {
    view.setUint16(pos, o.objectId, false)
    buf[pos + 2] = o.windowId
    buf[pos + 3] = o.crop ? 0x80 : 0x00
    view.setUint16(pos + 4, o.x, false)
    view.setUint16(pos + 6, o.y, false)
    pos += 8
    if (o.crop) {
      view.setUint16(pos, o.crop.x, false)
      view.setUint16(pos + 2, o.crop.y, false)
      view.setUint16(pos + 4, o.crop.w, false)
      view.setUint16(pos + 6, o.crop.h, false)
      pos += 8
    }
  }
  return buf
}

function buildWdsPayload(
  windows: Array<{ id: number; x: number; y: number; width: number; height: number }>,
): Uint8Array {
  const buf = new Uint8Array(1 + windows.length * 9)
  const view = new DataView(buf.buffer)
  buf[0] = windows.length
  let pos = 1
  for (const w of windows) {
    buf[pos] = w.id
    view.setUint16(pos + 1, w.x, false)
    view.setUint16(pos + 3, w.y, false)
    view.setUint16(pos + 5, w.width, false)
    view.setUint16(pos + 7, w.height, false)
    pos += 9
  }
  return buf
}

function buildPdsPayload(
  paletteId: number,
  entries: Array<{ id: number; y: number; cr: number; cb: number; a: number }>,
): Uint8Array {
  const buf = new Uint8Array(2 + entries.length * 5)
  buf[0] = paletteId
  buf[1] = 0 // version
  let pos = 2
  for (const e of entries) {
    buf[pos] = e.id
    buf[pos + 1] = e.y
    buf[pos + 2] = e.cr
    buf[pos + 3] = e.cb
    buf[pos + 4] = e.a
    pos += 5
  }
  return buf
}

function buildSingleOdsPayload(
  objectId: number,
  width: number,
  height: number,
  rleData: Uint8Array,
): Uint8Array {
  const buf = new Uint8Array(11 + rleData.length)
  const view = new DataView(buf.buffer)
  view.setUint16(0, objectId, false)
  buf[2] = 0 // version
  buf[3] = 0xc0 // first (0x80) | last (0x40)
  const dataLen = rleData.length + 4
  buf[4] = (dataLen >> 16) & 0xff
  buf[5] = (dataLen >> 8) & 0xff
  buf[6] = dataLen & 0xff
  view.setUint16(7, width, false)
  view.setUint16(9, height, false)
  buf.set(rleData, 11)
  return buf
}

function buildSampleSupBuffer(): ArrayBuffer {
  // DisplaySet 1 (PTS = 2.0s): 4x2 位图，位于 (100, 900)
  // Row 0: [1, 1, 2, 0] -> 短着色游程 (0x00, 0x82, 0x01) + 直接像素 (0x02) + 透明零游程 (0x00, 0x01) + EOL (0x00, 0x00)
  // Row 1: [2, 2, 2, 2] -> 长着色游程 (0x00, 0xC0, 0x04, 0x02) + EOL (0x00, 0x00)
  const rle = new Uint8Array([
    0x00, 0x82, 0x01, 0x02, 0x00, 0x01, 0x00, 0x00,
    0x00, 0xc0, 0x04, 0x02, 0x00, 0x00,
  ])

  const segs: Uint8Array[] = [
    // DisplaySet 1 @ 2.0s
    buildPgsSegment(
      2.0,
      0x16,
      buildPcsPayload({
        width: 1920,
        height: 1080,
        state: 0x80,
        paletteId: 0,
        objects: [{ objectId: 1, windowId: 0, x: 100, y: 900 }],
      }),
    ),
    buildPgsSegment(2.0, 0x17, buildWdsPayload([{ id: 0, x: 100, y: 900, width: 4, height: 2 }])),
    buildPgsSegment(
      2.0,
      0x14,
      buildPdsPayload(0, [
        { id: 0, y: 16, cr: 128, cb: 128, a: 0 }, // 透明背景
        { id: 1, y: 235, cr: 128, cb: 128, a: 255 }, // BT.709 纯白
        { id: 2, y: 81, cr: 240, cb: 90, a: 200 }, // 近似纯红 (alpha=200)
      ]),
    ),
    buildPgsSegment(2.0, 0x15, buildSingleOdsPayload(1, 4, 2, rle)),
    buildPgsSegment(2.0, 0x80, new Uint8Array(0)),

    // DisplaySet 2 @ 5.0s: 清屏帧 (numObjects = 0)
    buildPgsSegment(
      5.0,
      0x16,
      buildPcsPayload({
        width: 1920,
        height: 1080,
        state: 0x00,
        paletteId: 0,
        objects: [],
      }),
    ),
    buildPgsSegment(5.0, 0x80, new Uint8Array(0)),
  ]

  return concatBytes(segs)
}

describe('pgsRenderer - parsePgsSup 二进制解码', () => {
  it('精确解析 PCS + WDS + PDS + ODS RLE + END，校验调色板颜色、位图与时间戳', () => {
    const buffer = buildSampleSupBuffer()
    const displaySets = parsePgsSup(buffer)

    expect(displaySets).toHaveLength(2)

    // 第一帧：2.0s，含 1 个 4x2 位图对象
    const ds1 = displaySets[0]
    expect(ds1.pts).toBeCloseTo(2.0, 4)
    expect(ds1.width).toBe(1920)
    expect(ds1.height).toBe(1080)
    expect(ds1.isClear).toBe(false)
    expect(ds1.objects).toHaveLength(1)

    const obj = ds1.objects[0]
    expect(obj.x).toBe(100)
    expect(obj.y).toBe(900)
    expect(obj.imageData.width).toBe(4)
    expect(obj.imageData.height).toBe(2)

    const rgba = obj.imageData.data
    // (0,0) = 索引 1 (纯白 Y=235, Cr=128, Cb=128, A=255 -> 1.164383 * 219 = 254.999 -> clampByte 截断为 254)
    expect(rgba[0]).toBeGreaterThanOrEqual(254)
    expect(rgba[1]).toBeGreaterThanOrEqual(254)
    expect(rgba[2]).toBeGreaterThanOrEqual(254)
    expect(rgba[3]).toBe(255)

    // (1,0) = 索引 1 (纯白)
    expect(rgba[4]).toBeGreaterThanOrEqual(254)
    expect(rgba[7]).toBe(255)

    // (2,0) = 索引 2 (红色，R 高、G/B 低、A=200)
    expect(rgba[8]).toBeGreaterThan(220)
    expect(rgba[9]).toBeLessThan(40)
    expect(rgba[10]).toBeLessThan(40)
    expect(rgba[11]).toBe(200)

    // (3,0) = 索引 0 (透明)
    expect(rgba[15]).toBe(0)

    // Row 1 (4个像素全为索引 2 红色)
    for (let col = 0; col < 4; col++) {
      const idx = (4 + col) * 4
      expect(rgba[idx]).toBeGreaterThan(220)
      expect(rgba[idx + 3]).toBe(200)
    }

    // 第二帧：5.0s 清屏帧
    const ds2 = displaySets[1]
    expect(ds2.pts).toBeCloseTo(5.0, 4)
    expect(ds2.isClear).toBe(true)
    expect(ds2.objects).toHaveLength(0)
  })

  it('支持跨分片 ODS 拼接 (First + Last) 与 PCS 裁剪区域', () => {
    // 2x2 全白像素分两片发送
    const chunk1 = new Uint8Array([0x01, 0x01, 0x00, 0x00])
    const chunk2 = new Uint8Array([0x01, 0x01, 0x00, 0x00])

    const odsFirst = new Uint8Array(11 + chunk1.length)
    const v1 = new DataView(odsFirst.buffer)
    v1.setUint16(0, 7, false)
    odsFirst[2] = 0
    odsFirst[3] = 0x80 // First only
    const totalDataLen = chunk1.length + chunk2.length + 4
    odsFirst[4] = (totalDataLen >> 16) & 0xff
    odsFirst[5] = (totalDataLen >> 8) & 0xff
    odsFirst[6] = totalDataLen & 0xff
    v1.setUint16(7, 2, false)
    v1.setUint16(9, 2, false)
    odsFirst.set(chunk1, 11)

    const odsLast = new Uint8Array(4 + chunk2.length)
    const v2 = new DataView(odsLast.buffer)
    v2.setUint16(0, 7, false)
    odsLast[2] = 0
    odsLast[3] = 0x40 // Last only
    odsLast.set(chunk2, 4)

    const buf = concatBytes([
      buildPgsSegment(
        1.0,
        0x16,
        buildPcsPayload({
          width: 1280,
          height: 720,
          objects: [{ objectId: 7, windowId: 0, x: 20, y: 30, crop: { x: 1, y: 0, w: 1, h: 2 } }],
        }),
      ),
      buildPgsSegment(1.0, 0x14, buildPdsPayload(0, [{ id: 1, y: 235, cr: 128, cb: 128, a: 255 }])),
      buildPgsSegment(1.0, 0x15, odsFirst),
      buildPgsSegment(1.0, 0x15, odsLast),
      buildPgsSegment(1.0, 0x80, new Uint8Array(0)),
    ])

    const sets = parsePgsSup(buf)
    expect(sets).toHaveLength(1)
    expect(sets[0].objects[0].imageData.width).toBe(1)
    expect(sets[0].objects[0].imageData.height).toBe(2)
    expect(sets[0].objects[0].imageData.data[3]).toBe(255)
    expect(sets[0].objects[0].imageData.data[7]).toBe(255)
  })
})

describe('pgsRenderer - PgsCanvasRenderer 同步绘制与二分查找', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('从 URL 加载 .sup 并按时间轴二分查找同步绘制与清屏', async () => {
    const supBuffer = buildSampleSupBuffer()

    // Mock 主 Canvas 与离屏 Canvas 2D Context
    const mainCtx = {
      clearRect: vi.fn(),
      drawImage: vi.fn(),
    } as unknown as CanvasRenderingContext2D

    const offscreenCtx = {
      putImageData: vi.fn(),
    } as unknown as CanvasRenderingContext2D

    const mainCanvas = document.createElement('canvas')
    vi.spyOn(mainCanvas, 'getContext').mockReturnValue(mainCtx)

    const origCreateElement = document.createElement.bind(document)
    vi.spyOn(document, 'createElement').mockImplementation((tagName: string) => {
      const el = origCreateElement(tagName)
      if (tagName.toLowerCase() === 'canvas') {
        vi.spyOn(el as HTMLCanvasElement, 'getContext').mockReturnValue(offscreenCtx)
      }
      return el
    })

    vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      status: 200,
      arrayBuffer: async () => supBuffer,
    } as Response)

    const renderer = new PgsCanvasRenderer(mainCanvas)
    const count = await renderer.loadFromUrl('/emby/Videos/1/Subtitles/3/Stream.sup')
    expect(count).toBe(2)

    // 1. 第一帧前 (t = 1.0s): 二分查找 idx = -1，仅清屏不绘制
    renderer.renderAtTime(1.0)
    expect(mainCtx.drawImage).not.toHaveBeenCalled()

    // 2. 命中第一帧 (t = 2.5s): 调整画布为 1920x1080 并绘制位于 (100, 900) 的对象
    renderer.renderAtTime(2.5)
    expect(mainCanvas.width).toBe(1920)
    expect(mainCanvas.height).toBe(1080)
    expect(offscreenCtx.putImageData).toHaveBeenCalledTimes(1)
    expect(mainCtx.drawImage).toHaveBeenCalledTimes(1)
    expect(mainCtx.drawImage).toHaveBeenLastCalledWith(expect.anything(), 100, 900)

    // 3. 同帧内推进 (t = 3.8s): 索引未变，应跳过重复重绘
    renderer.renderAtTime(3.8)
    expect(mainCtx.drawImage).toHaveBeenCalledTimes(1)

    // 4. 命中清屏帧 (t = 5.1s): 清屏且不再调用 drawImage
    const clearCallsBefore = (mainCtx.clearRect as ReturnType<typeof vi.fn>).mock.calls.length
    renderer.renderAtTime(5.1)
    expect((mainCtx.clearRect as ReturnType<typeof vi.fn>).mock.calls.length).toBeGreaterThan(
      clearCallsBefore,
    )
    expect(mainCtx.drawImage).toHaveBeenCalledTimes(1)

    // 5. 拖动进度条回退至第一帧 (t = 2.1s): 重新绘制
    renderer.renderAtTime(2.1)
    expect(mainCtx.drawImage).toHaveBeenCalledTimes(2)

    renderer.destroy()
  })
})
