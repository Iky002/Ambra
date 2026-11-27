/**
 * Clean-room MIT shim for HTMLVideoElement.requestVideoFrameCallback.
 *
 * Modern browsers (Chromium, Safari, Firefox 132+) natively implement
 * requestVideoFrameCallback; in those environments this module is a pure no-op
 * pass-through. In legacy or headless test environments (such as jsdom) where
 * native support is absent, a lightweight requestAnimationFrame / timer
 * fallback is registered so consumers never pull in GPL-licensed polyfills.
 *
 * SPDX-License-Identifier: MIT
 */

export interface RvfcFrameMetadata {
  presentationTime: number
  expectedDisplayTime: number
  width: number
  height: number
  mediaTime: number
  presentedFrames: number
  processingDuration?: number
}

export type RvfcCallback = (now: number, metadata: RvfcFrameMetadata) => void

type TimerRecord = {
  kind: 'raf' | 'timeout'
  id: number
}

const activeCallbacks = new Map<number, TimerRecord>()
const presentedCounter = new WeakMap<HTMLVideoElement, number>()
let nextHandleId = 1

function scheduleNextFrame(fn: (timestamp: number) => void): TimerRecord {
  if (typeof requestAnimationFrame === 'function') {
    return {
      kind: 'raf',
      id: requestAnimationFrame(fn),
    }
  }
  const timerId = setTimeout(() => {
    const now = typeof performance !== 'undefined' ? performance.now() : Date.now()
    fn(now)
  }, 16) as unknown as number
  return {
    kind: 'timeout',
    id: timerId,
  }
}

function cancelScheduledFrame(rec: TimerRecord): void {
  if (rec.kind === 'raf' && typeof cancelAnimationFrame === 'function') {
    cancelAnimationFrame(rec.id)
  } else {
    clearTimeout(rec.id)
  }
}

export function installRvfcShim(
  videoProto: Partial<HTMLVideoElement> | undefined = typeof HTMLVideoElement !== 'undefined'
    ? HTMLVideoElement.prototype
    : undefined,
): boolean {
  if (!videoProto) return false
  if (typeof videoProto.requestVideoFrameCallback === 'function') {
    return false
  }

  videoProto.requestVideoFrameCallback = function requestVideoFrameCallbackShim(
    this: HTMLVideoElement,
    callback: VideoFrameRequestCallback,
  ): number {
    if (typeof callback !== 'function') {
      throw new TypeError('requestVideoFrameCallback requires a callback function')
    }
    const handle = nextHandleId++
    const rec = scheduleNextFrame((now) => {
      activeCallbacks.delete(handle)
      const nextCount = (presentedCounter.get(this) ?? 0) + 1
      presentedCounter.set(this, nextCount)
      callback(now, {
        presentationTime: now,
        expectedDisplayTime: now + 16.67,
        width: this.videoWidth || 0,
        height: this.videoHeight || 0,
        mediaTime: Math.max(0, Number.isFinite(this.currentTime) ? this.currentTime : 0),
        presentedFrames: nextCount,
        processingDuration: 0,
      })
    })
    activeCallbacks.set(handle, rec)
    return handle
  }

  videoProto.cancelVideoFrameCallback = function cancelVideoFrameCallbackShim(handle: number): void {
    const rec = activeCallbacks.get(handle)
    if (!rec) return
    activeCallbacks.delete(handle)
    cancelScheduledFrame(rec)
  }

  return true
}

installRvfcShim()
