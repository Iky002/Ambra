import { afterEach, describe, expect, it, vi } from 'vitest'
import { buildBifBuffer, parseBif } from '../bifParser'

describe('bifParser (H-22)', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('parses valid BIF v0 buffer, performs binary search by seconds, caches ObjectURL and disposes cleanly', () => {
    const f0 = new Uint8Array([0xff, 0xd8, 0x00, 0x01, 0xff, 0xd9])
    const f1 = new Uint8Array([0xff, 0xd8, 0x00, 0x02, 0xff, 0xd9])
    const f2 = new Uint8Array([0xff, 0xd8, 0x00, 0x03, 0xff, 0xd9])

    const raw = buildBifBuffer(
      [
        { timestampIndex: 0, data: f0 },
        { timestampIndex: 10, data: f1 },
        { timestampIndex: 25, data: f2 },
      ],
      1000,
    )

    let nextUrlId = 1
    const createSpy = vi
      .spyOn(URL, 'createObjectURL')
      .mockImplementation(() => `blob:bif-frame-${nextUrlId++}`)
    const revokeSpy = vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {})

    const archive = parseBif(raw)
    expect(archive.version).toBe(0)
    expect(archive.frameCount).toBe(3)
    expect(archive.timestampMultiplierMs).toBe(1000)

    expect(archive.findFrameIndex(-5)).toBe(0)
    expect(archive.findFrameIndex(0)).toBe(0)
    expect(archive.findFrameIndex(9.9)).toBe(0)
    expect(archive.findFrameIndex(10)).toBe(1)
    expect(archive.findFrameIndex(24.5)).toBe(1)
    expect(archive.findFrameIndex(25)).toBe(2)
    expect(archive.findFrameIndex(999)).toBe(2)

    expect(archive.getFrameBytes(1)).toEqual(f1)

    // Lazy creation + cache reuse
    const res1a = archive.getFrameUrl(12)
    const res1b = archive.getFrameUrl(15)
    expect(res1a).toEqual({ url: 'blob:bif-frame-1', index: 1, timestampSec: 10 })
    expect(res1b).toEqual(res1a)
    expect(createSpy).toHaveBeenCalledTimes(1)

    const res2 = archive.getFrameUrl(30)
    expect(res2).toEqual({ url: 'blob:bif-frame-2', index: 2, timestampSec: 25 })
    expect(createSpy).toHaveBeenCalledTimes(2)

    // Dispose revokes all cached ObjectURLs and prevents further allocations
    archive.dispose()
    expect(archive.isDisposed).toBe(true)
    expect(revokeSpy).toHaveBeenCalledTimes(2)
    expect(revokeSpy).toHaveBeenCalledWith('blob:bif-frame-1')
    expect(revokeSpy).toHaveBeenCalledWith('blob:bif-frame-2')
    expect(archive.getFrameUrl(0)).toBeNull()
  })

  it('defaults timestampMultiplierMs=0 to 1000ms per Roku BIF spec', () => {
    const raw = buildBifBuffer([{ timestampIndex: 5, data: new Uint8Array([0xff, 0xd8, 0xff, 0xd9]) }], 0)
    const archive = parseBif(raw)
    expect(archive.timestampMultiplierMs).toBe(1000)
    expect(archive.frames[0].timestampMs).toBe(5000)
  })

  it('rejects invalid magic header or truncated data', () => {
    expect(() => parseBif(new Uint8Array(10))).toThrow(/长度不足/)
    const bad = new Uint8Array(64)
    bad.set([0x00, 0x42, 0x49, 0x46])
    expect(() => parseBif(bad)).toThrow(/魔数头/)
  })

  it('[Round2] sorts unsorted timestamps ascending, rejects header-overlapping offsets and zero next offsets', () => {
    const fLate = new Uint8Array([0xff, 0xd8, 0x00, 0x20, 0xff, 0xd9])
    const fEarly = new Uint8Array([0xff, 0xd8, 0x00, 0x05, 0xff, 0xd9])
    const unsorted = buildBifBuffer(
      [
        { timestampIndex: 20, data: fLate },
        { timestampIndex: 5, data: fEarly },
      ],
      1000,
    )
    const sortedArchive = parseBif(unsorted)
    expect(sortedArchive.frameCount).toBe(2)
    expect(sortedArchive.frames.map((f) => f.timestampMs)).toEqual([5000, 20000])
    expect(sortedArchive.getFrameBytes(0)).toEqual(fEarly)
    expect(sortedArchive.getFrameBytes(1)).toEqual(fLate)

    // 篡改结束标记 offset 为 0 -> 拒绝该帧，绝不回退到 data.byteLength
    const corruptedZeroEnd = unsorted.slice()
    const view = new DataView(corruptedZeroEnd.buffer)
    const endPos = 64 + 2 * 8
    view.setUint32(endPos + 4, 0, true)
    const zeroEndArchive = parseBif(corruptedZeroEnd)
    expect(zeroEndArchive.frameCount).toBe(1)

    // 篡改第一帧 offset 落入索引表区域 (< 64 + 3*8) -> 拒绝该帧
    const corruptedOverlap = unsorted.slice()
    const view2 = new DataView(corruptedOverlap.buffer)
    view2.setUint32(64 + 4, 64, true)
    const overlapArchive = parseBif(corruptedOverlap)
    expect(overlapArchive.frameCount).toBe(1)
  })
})
