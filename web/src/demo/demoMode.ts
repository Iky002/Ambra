let demoModeOverride: boolean | null = null

/**
 * 仅供单元测试使用的 Demo 模式开关覆写。
 * 传 null 恢复默认环境变量与 URL 检测逻辑。
 */
export function setDemoModeOverride(override: boolean | null): void {
  demoModeOverride = override
}

/**
 * 判断当前是否处于 Serverless Demo 模式：
 * 1. 构建时环境变量 VITE_DEMO_MODE === 'true'
 * 2. 浏览器 URL 查询参数含 ?demo=1 或 ?demo=true（兼容 HashRouter 如 #/?demo=1）
 */
export function isDemoMode(): boolean {
  if (demoModeOverride !== null) {
    return demoModeOverride
  }

  if (import.meta.env.VITE_DEMO_MODE === 'true') {
    return true
  }

  if (typeof window !== 'undefined') {
    try {
      const searchParams = new URLSearchParams(window.location.search)
      const q = searchParams.get('demo')
      if (q === '1' || q === 'true') {
        return true
      }

      const hash = window.location.hash || ''
      const hashQueryIdx = hash.indexOf('?')
      if (hashQueryIdx !== -1) {
        const hashParams = new URLSearchParams(hash.slice(hashQueryIdx + 1))
        const hq = hashParams.get('demo')
        if (hq === '1' || hq === 'true') {
          return true
        }
      }
    } catch {
      // 忽略异常 URL
    }
  }

  return false
}
