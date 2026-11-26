import { defineConfig, mergeConfig } from 'vitest/config'
import viteConfig from './vite.config'

export default defineConfig((env) =>
  mergeConfig(
    typeof viteConfig === 'function' ? viteConfig(env) : viteConfig,
    {
      test: {
        environment: 'jsdom',
        setupFiles: ['./src/test/setup.ts'],
        globals: true,
        css: false,
        restoreMocks: true,
        clearMocks: true,
        coverage: {
          provider: 'v8',
          reporter: ['text', 'html'],
          include: ['src/**/*.{ts,tsx}'],
          exclude: ['src/**/__tests__/**', 'src/test/**', 'src/main.tsx'],
        },
      },
    },
  ),
)
