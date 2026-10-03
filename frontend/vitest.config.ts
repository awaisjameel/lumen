import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  // Files in public/ (/lumen.png) are served as they are: the tests' DOM
  // needs no import of them, which Node could not resolve.
  plugins: [vue({ template: { transformAssetUrls: { includeAbsolute: false } } })],
  test: {
    environment: 'jsdom',
    pool: 'threads',
    maxWorkers: 2,
    clearMocks: true,
    restoreMocks: true,
    setupFiles: ['src/testSetup.ts'],
  },
})
