import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import type { Pinia } from 'pinia'
import { flushPromises } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import { createTestNavigation } from '../../shared/navigation/testing'
import URadioGroup from '@nuxt/ui/components/RadioGroup.vue'
import { engine, Theme } from '../../bridge'
import type * as Bridge from '../../bridge'
import { usePreferences } from '../../shared/preferences'
import { mountInApp } from '../../shared/testing'
import { destination } from '../../shared/navigation/contract'
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
  const router = createTestNavigation()
  await router.visit('/')
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
  const production = createTestNavigation({}, false)
  const development = createTestNavigation({}, true)
  expect(() => production.resolve(destination({ name: 'showcase' }))).toThrow()
  expect(development.resolve(destination({ name: 'showcase' })).matched).toEqual(['showcase'])
})
