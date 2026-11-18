import { afterEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { createTestNavigation } from '../shared/navigation/testing'
import { enableAutoUnmount, flushPromises } from '@vue/test-utils'
import App from '../App.vue'
import { useLibrary } from '../library/store'
import { usePreferences } from '../shared/preferences'
import { engine } from '../bridge'
import { deferred, mountInApp } from '../shared/testing'
import type * as Bridge from '../bridge'

vi.mock('../bridge', async (original) => ({
  ...(await original<typeof Bridge>()),
  engine: { updateHealth: vi.fn().mockResolvedValue(undefined), lockBlocks: vi.fn().mockResolvedValue(undefined) },
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
  const router = createTestNavigation()
  await router.visit('/')
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

it('closes the previous frontend lifetime’s PIN session before starting a new one on an incoming location', async () => {
  setActivePinia(createPinia())
  const lib = useLibrary()
  const start = vi.spyOn(lib, 'init').mockImplementation(() => {
    lib.ready = true
    return Promise.resolve()
  })
  vi.spyOn(usePreferences(), 'load').mockResolvedValue()
  const locked = deferred<undefined>()
  vi.mocked(engine.lockBlocks).mockReturnValueOnce(locked.promise)
  const navigation = createTestNavigation()
  await navigation.visit('/people/0')
  mountInApp(App, { plugins: [navigation] })
  await flushPromises()
  expect(engine.lockBlocks).toHaveBeenCalledOnce()
  expect(engine.updateHealth).not.toHaveBeenCalled()
  expect(start).not.toHaveBeenCalled()
  locked.resolve(undefined)
  await flushPromises()
  expect(engine.updateHealth).toHaveBeenCalledOnce()
  expect(start).toHaveBeenCalledOnce()
})
