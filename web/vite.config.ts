import { defineConfig, loadEnv } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, '.', 'VITE_')
  const isDemoBuild =
    mode === 'demo' || env.VITE_DEMO_MODE === 'true' || env.VITE_DEMO === 'true'

  return {
    base: isDemoBuild ? '/' : './',
    plugins: [react(), tailwindcss()],
    optimizeDeps: {
      exclude: ['jassub', '@ffmpeg/ffmpeg', '@ffmpeg/util'],
    },
    build: {
      outDir: 'dist',
      emptyOutDir: true,
      assetsInlineLimit: 0,
      rollupOptions: {
        output: {
          manualChunks(id) {
            if (id.includes('node_modules/hls.js')) return 'vendor-hls'
            if (id.includes('node_modules/jassub')) return 'vendor-jassub'
            if (id.includes('node_modules/@ffmpeg')) return 'vendor-ffmpeg'
            if (
              id.includes('node_modules/react') ||
              id.includes('node_modules/react-dom') ||
              id.includes('node_modules/react-router') ||
              id.includes('node_modules/@tanstack')
            ) {
              return 'vendor-react'
            }
            if (id.includes('node_modules/lucide-react')) return 'vendor-icons'
          },
        },
      },
    },
    server: {
      port: 5173,
      proxy: {
        '/api': {
          target: 'http://localhost:8096',
          changeOrigin: true,
        },
        '/emby': {
          target: 'http://localhost:8096',
          changeOrigin: true,
        },
        '/embywebsocket': {
          target: 'http://localhost:8096',
          ws: true,
          changeOrigin: true,
        },
      },
    },
  }
})
