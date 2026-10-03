import { _api } from '@iconify/vue'
import ui from '@nuxt/ui/vue-plugin'
import { createPinia } from 'pinia'
import { createApp } from 'vue'
import App from './App.vue'
import { router } from './router'
import { reportUnexpected } from './shared/errors'
import './design/tokens.css'

// Icons come from the bundle alone (design/icons.ts). Iconify would
// otherwise ask its public API for a name it does not know; this module
// answers every such request with "not found" without sending anything.
_api.setAPIModule('', {
  prepare: () => [],
  send: (_host, _params, callback) => callback('abort', 424),
})

const app = createApp(App)
// Failures nothing else caught: logged for a problem report, and the user
// told. A window error without an Error (a ResizeObserver loop, a script
// the browser won't describe) is logged alone: there is nothing to tell.
app.config.errorHandler = (e, _instance, info) => reportUnexpected(e, `vue: ${info}`)
window.addEventListener('error', (e) => reportUnexpected(e.error ?? e.message, 'window', e.error instanceof Error))
window.addEventListener('unhandledrejection', (e) => reportUnexpected(e.reason, 'promise'))
app.use(createPinia()).use(router).use(ui).mount('#app')
