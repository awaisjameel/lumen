import { afterEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import { enableAutoUnmount, flushPromises } from '@vue/test-utils'
import App from '../App.vue'
import { useLibrary } from '../library/store'
import { usePreferences } from '../shared/preferences'
import { mountInApp } from '../shared/testing'
import type * as Bridge from '../bridge'

vi.mock('../bridge', async (original) => ({
  ...(await original<typeof Bridge>()),
  engine: { updateHealth: vi.fn().mockResolvedValue(undefined) },
}))
vi.mock('../app/AppOverlays.vue', () => ({ default: { template: '<span />' } }))
vi.mock('../about/CrashNotice.vue', () => ({ default: { template: '<span />' } }))
vi.mock('../updates/UpdateNotice.vue', () => ({ default: { template: '<span />' } }))
vi.mock('./Welcome.vue', () => ({
  __esModule: true,
  default: {
    emits: ['starting', 'complete'],
    template:
      '<div data-welcome-test><button data-begin @click="$emit(\'starting\')">Start</button><button data-finish @click="$emit(\'complete\')">Browse</button></div>',
  },
}))
vi.mock('../app/AppShell.vue', () => ({ default: { template: '<div data-shell-test><slot /></div>' } }))
enableAutoUnmount(afterEach)
it('keeps first-run progress visible after sources arrive until the user starts browsing', async () => {
  setActivePinia(createPinia())
  const lib = useLibrary()
  vi.spyOn(lib, 'init').mockImplementation(() => {
    lib.ready = true
    return Promise.resolve()
  })
  vi.spyOn(usePreferences(), 'load').mockResolvedValue()
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/', name: 'library', component: { render: () => null } }],
  })
  await router.push('/')
  const w = mountInApp(App, { plugins: [router] })
  await flushPromises()
  await w.get('[data-begin]').trigger('click')
  lib.sources = [{ id: 1, path: 'D:/Photos', enabled: true }]
  await flushPromises()
  expect(w.find('[data-welcome-test]').exists()).toBe(true)
  expect(w.find('[data-shell-test]').exists()).toBe(false)
  await w.get('[data-finish]').trigger('click')
  await flushPromises()
  expect(w.find('[data-welcome-test]').exists()).toBe(false)
  expect(w.find('[data-shell-test]').exists()).toBe(true)
})
