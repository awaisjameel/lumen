import { defineConfig } from 'vite'
import ui from '@nuxt/ui/vite'
import vue from '@vitejs/plugin-vue'
import wails from '@wailsio/runtime/plugins/vite'
import { uiOptions } from './nuxt-ui.config.ts'
import { routes } from './src/shared/navigation/contract.ts'

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
  plugins: [
    vue(),
    ui(uiOptions),
    wails('./bindings'),
    {
      name: 'lumen-navigation',
      generateBundle() {
        // The asset server recognises client route shapes on a hard reload,
        // including malformed IDs containing a dot. Vue owns validation.
        this.emitFile({
          type: 'asset',
          fileName: 'navigation.json',
          source: JSON.stringify(routes.filter((route) => !('development' in route.meta)).map((route) => route.path)),
        })
      },
    },
  ],
})
