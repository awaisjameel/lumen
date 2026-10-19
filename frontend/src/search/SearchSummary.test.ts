import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { engine } from '../bridge'
import type * as Bridge from '../bridge'
import type { SearchInfo } from '../library/store'
import { registerCommands } from '../shared/commands'
import { mountInApp } from '../shared/testing'
import SearchSummary from './SearchSummary.vue'

vi.mock('../bridge', async (original) => ({
  ...(await original<typeof Bridge>()),
  engine: { people: vi.fn() },
}))

const router = createRouter({
  history: createMemoryHistory(),
  routes: [{ path: '/search', name: 'search', component: { template: '<div />' } }],
})
let wrapper: VueWrapper | undefined

const found = (info: Partial<SearchInfo>): SearchInfo => ({
  people: [],
  description: '',
  pending: 0,
  complete: true,
  noAI: false,
  blocked: false,
  ...info,
})

async function show(info: SearchInfo, total = 6, path = '/search?q=Amina%20at%20the%20beach&media=photos') {
  await router.push(path)
  wrapper = mountInApp(SearchSummary, { plugins: [router], props: { info, total, searching: false } })
  await flushPromises()
  return wrapper
}

beforeEach(() => {
  setActivePinia(createPinia())
  vi.mocked(engine.people).mockResolvedValue([])
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
})

it('takes a person or the description out of the search, keeping its refinements', async () => {
  const w = await show(found({ people: [{ id: 3, name: 'Amina' }], description: 'at the beach' }))
  const parts = w.findAll('[data-search-part]')
  expect(parts.map((p) => p.attributes('aria-label'))).toEqual([
    'Remove person Amina',
    'Remove description at the beach',
  ])
  await parts[0]?.trigger('click')
  await flushPromises()
  expect(router.currentRoute.value.query).toEqual({ q: 'at the beach', media: 'photos' })
  await parts[1]?.trigger('click')
  await flushPromises()
  expect(router.currentRoute.value.query).toEqual({ q: 'Amina', media: 'photos' })
})

it('says how many matched, and that more did when the results are cut short', async () => {
  const w = await show(found({ description: 'sea', complete: false }), 12)
  expect(w.findAll('[data-search-count] > *').map((part) => part.text())).toEqual([
    '12 matches, best first',
    'Showing the best 12. Add words to narrow it down.',
  ])
})

it('links photos still being understood to the library activity', async () => {
  const opened = vi.fn()
  const unregister = registerCommands([{ id: 'app.activity', group: 'app', title: () => 'Activity', run: opened }])
  const w = await show(found({ description: 'sea', pending: 1204 }))
  expect(w.get('[data-search-pending]').text()).toContain('Lumen is still understanding 1,204 photos')
  await w.get('[data-search-pending] button').trigger('click')
  await flushPromises()
  expect(opened).toHaveBeenCalledOnce()
  unregister()
})

it('says when only people were searched and when the search names something blocked', async () => {
  let w = await show(found({ people: [{ id: 3, name: 'Amina' }], description: 'beach', noAI: true }))
  expect(w.find('[data-search-no-ai]').exists()).toBe(true)
  expect(w.find('[data-search-blocked]').exists()).toBe(false)
  w.unmount()
  w = await show(found({ blocked: true }), 0)
  expect(w.find('[data-search-blocked]').exists()).toBe(true)
  expect(w.find('[data-search-count]').exists()).toBe(false)
})
