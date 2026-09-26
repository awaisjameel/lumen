import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import { router } from './router'
import './shared/styles.css'
import './shared/theme'

createApp(App).use(createPinia()).use(router).mount('#app')
