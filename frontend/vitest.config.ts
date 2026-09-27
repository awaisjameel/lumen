import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  test: { environment: 'jsdom', pool: 'threads', maxWorkers: 2, clearMocks: true, restoreMocks: true },
})
