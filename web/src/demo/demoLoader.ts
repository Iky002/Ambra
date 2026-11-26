export interface DemoServerBindings {
  demoFetch: <T>(path: string, options?: RequestInit, requireAuth?: boolean) => Promise<T>
  getDemoImageUrl: (
    itemId: string,
    type?: 'Primary' | 'Backdrop' | 'Thumb' | 'Logo' | 'Banner',
  ) => string
  getDemoFallbackImageUrl: (
    itemId: string,
    type?: 'Primary' | 'Backdrop' | 'Thumb' | 'Logo' | 'Banner',
  ) => string
  getDemoUserImageUrl: (userId: string) => string
  getDemoAudioUrl: (itemId: string) => string
  getDemoVideoUrl: () => string
}

let cachedDemoServer: DemoServerBindings | null = null
let pendingDemoLoad: Promise<DemoServerBindings> | null = null

export function registerDemoServer(bindings: DemoServerBindings): void {
  cachedDemoServer = bindings
}

export function getCachedDemoServer(): DemoServerBindings | null {
  return cachedDemoServer
}

export async function loadDemoServer(): Promise<DemoServerBindings> {
  if (cachedDemoServer) {
    return cachedDemoServer
  }
  if (!pendingDemoLoad) {
    pendingDemoLoad = import('./mockServer')
      .then((mod) => {
        const bindings: DemoServerBindings = {
          demoFetch: mod.demoFetch,
          getDemoImageUrl: mod.getDemoImageUrl,
          getDemoFallbackImageUrl: mod.getDemoFallbackImageUrl,
          getDemoUserImageUrl: mod.getDemoUserImageUrl,
          getDemoAudioUrl: mod.getDemoAudioUrl,
          getDemoVideoUrl: mod.getDemoVideoUrl,
        }
        cachedDemoServer = bindings
        return bindings
      })
      .catch((err) => {
        pendingDemoLoad = null
        throw err
      })
  }
  return pendingDemoLoad
}
