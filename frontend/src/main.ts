import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import { router } from './router'
import { reportUnexpected } from './shared/errors'
import './shared/styles.css'
import './shared/theme'

const app = createApp(App)
// Failures nothing else caught: logged for a problem report, and the user
// told. A window error without an Error (a ResizeObserver loop, a script
// the browser won't describe) is logged alone: there is nothing to tell.
app.config.errorHandler = (e, _instance, info) => reportUnexpected(e, `vue: ${info}`)
window.addEventListener('error', (e) => reportUnexpected(e.error ?? e.message, 'window', e.error instanceof Error))
window.addEventListener('unhandledrejection', (e) => reportUnexpected(e.reason, 'promise'))
app.use(createPinia()).use(router).mount('#app')
