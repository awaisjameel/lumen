import { defineConfig } from 'vitest/config'
import ui from '@nuxt/ui/vite'
import vue from '@vitejs/plugin-vue'
import { uiOptions } from './nuxt-ui.config.ts'

export default defineConfig({
  // Files in public/ (/lumen.png) are served as they are: the tests' DOM
  // needs no import of them, which Node could not resolve.
  plugins: [vue({ template: { transformAssetUrls: { includeAbsolute: false } } }), ui(uiOptions)],
  test: {
    environment: 'jsdom',
    pool: 'threads',
    maxWorkers: 2,
    clearMocks: true,
    restoreMocks: true,
    setupFiles: ['src/testSetup.ts'],
  },
})
