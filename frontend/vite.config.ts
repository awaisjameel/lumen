import { defineConfig } from 'vite'
import ui from '@nuxt/ui/vite'
import vue from '@vitejs/plugin-vue'
import wails from '@wailsio/runtime/plugins/vite'
import { uiOptions } from './nuxt-ui.config.ts'

// https://vitejs.dev/config/
export default defineConfig({
  server: {
    host: '127.0.0.1',
    port: Number(process.env.WAILS_VITE_PORT) || 9245,
    strictPort: true,
  },
  // The UI only ever runs in WebView2, which is evergreen Chromium, so ship
  // modern CSS and JS as written (e.g. light-dark() needs Chromium 123).
  // The licence of every package bundled into the UI, for the notices that
  // ship with Lumen (scripts/notices.py reads it).
  build: { target: 'chrome123', license: { fileName: '.vite/license.json' } },
  plugins: [vue(), ui(uiOptions), wails('./bindings')],
})
