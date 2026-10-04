import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import type { Pinia } from 'pinia'
import { flushPromises } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import URadioGroup from '@nuxt/ui/components/RadioGroup.vue'
import { engine, Theme } from '../../bridge'
import type * as Bridge from '../../bridge'
import { usePreferences } from '../../shared/preferences'
import { mountInApp } from '../../shared/testing'
import { router as appRouter } from '../../router'
import Showcase from './Showcase.vue'

vi.mock('../../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  engine: { setPreferences: vi.fn<typeof Bridge.engine.setPreferences>((p) => Promise.resolve(p)) },
}))

let pinia: Pinia
let wrapper: VueWrapper | undefined

beforeEach(() => {
  pinia = createPinia()
  setActivePinia(pinia)
  localStorage.clear()
  document.documentElement.className = ''
  vi.mocked(engine.setPreferences).mockClear()
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
})

async function mountShowcase() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/', component: { render: () => null } }],
  })
  await router.push('/')
  wrapper = mountInApp(Showcase, { plugins: [pinia, router] })
  await flushPromises()
  return wrapper
}

it('shows every state and composition the screens are reviewed against', async () => {
  const w = await mountShowcase()
  const text = w.text()
  for (const heading of [
    'Buttons',
    'Form controls',
    'Overlays and feedback',
    'States',
    'Collection cards',
    'Media tiles',
  ])
    expect(text).toContain(heading)
  // The three feature states, each announced as what it is.
  expect(w.find('[role="status"][aria-busy="true"]').exists()).toBe(true)
  expect(w.findAll('[role="alert"]').length).toBeGreaterThan(0)
  // Every icon of the manifest is drawn.
  expect(text).toMatch(/Icons \(\d+\)/)
})

it('tries a theme on without saving it, and gives the user’s own back on leaving', async () => {
  const prefs = usePreferences()
  const w = await mountShowcase()
  expect(document.documentElement.classList.contains('dark')).toBe(false)
  w.findComponent(URadioGroup).vm.$emit('update:modelValue', Theme.ThemeDark)
  await flushPromises()
  expect(document.documentElement.classList.contains('dark')).toBe(true)
  // Tried on only: the preference and the library are untouched.
  expect(prefs.prefs.theme).toBe(Theme.ThemeSystem)
  expect(engine.setPreferences).not.toHaveBeenCalled()
  w.unmount()
  wrapper = undefined
  expect(document.documentElement.classList.contains('dark')).toBe(false)
})

it('has no route in a production build', () => {
  // Tests run as a development build, where the route exists; the
  // production bundle is checked for its absence when it is built (see the
  // Phase 7 record). Here: it is the only route guarded by DEV.
  const showcase = appRouter.getRoutes().filter((r) => r.name === 'showcase')
  expect(showcase).toHaveLength(import.meta.env.DEV ? 1 : 0)
})
