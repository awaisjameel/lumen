import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { engine, Filter } from '../bridge'
import type * as Bridge from '../bridge'
import { daySpan } from '../shared/format'
import { mountInApp } from '../shared/testing'
import SearchRefine from './SearchRefine.vue'

vi.mock('../bridge', async (original) => ({
  ...(await original<typeof Bridge>()),
  engine: { places: vi.fn() },
}))

const router = createRouter({
  history: createMemoryHistory(),
  routes: [{ path: '/search', name: 'search', component: { template: '<div />' } }],
})
let wrapper: VueWrapper | undefined

async function show(path: string, media = Filter.FilterAll, videos = true) {
  await router.push(path)
  wrapper = mountInApp(SearchRefine, { plugins: [router], props: { media, videos } })
  await flushPromises()
  return wrapper
}
const pressed = (w: VueWrapper) => w.findAll('[aria-pressed="true"]').map((chip) => chip.text().trim())

beforeEach(() => {
  vi.mocked(engine.places).mockResolvedValue([
    {
      id: 4,
      name: 'Lahore',
      region: 'Punjab',
      country: 'Pakistan',
      count: 3,
      cover: 0,
      coverThumb: 0,
      lat: 31.5,
      lon: 74.3,
    },
  ])
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
})

it('chooses one kind of item, with Favourites on or off beside it', async () => {
  const w = await show('/search?q=sea')
  expect(pressed(w)).toEqual(['All'])
  await w.get('[data-search-kind="videos"]').trigger('click')
  await flushPromises()
  await w.get('[data-search-favourites]').trigger('click')
  await flushPromises()
  expect(router.currentRoute.value.query).toEqual({ q: 'sea', media: 'videos', favourites: 'true' })
  await w.get('[data-search-favourites]').trigger('click')
  await flushPromises()
  expect(router.currentRoute.value.query).toEqual({ q: 'sea', media: 'videos' })
})

it('leaves out Videos while the library shows none', async () => {
  const w = await show('/search?q=sea', Filter.FilterAll, false)
  expect(w.find('[data-search-kind="videos"]').exists()).toBe(false)
})

it('names the chosen dates and place, and clears every refinement but the words', async () => {
  const w = await show('/search?q=sea&from=2026-10-03&to=2026-10-04&place=4&favourites=true', Filter.FilterPhotos)
  const days = daySpan('2026-10-03', '2026-10-04', true)
  expect(w.get('[data-search-when]').text()).toBe(days)
  expect(w.get('[data-search-where]').text()).toBe('Lahore')
  expect(pressed(w)).toEqual(['Photos', 'Favourites', days, 'Lahore'])
  await w.get('[data-search-clear-filters]').trigger('click')
  await flushPromises()
  expect(router.currentRoute.value.query).toEqual({ q: 'sea' })
})

it('offers no Clear filters while nothing is refined', async () => {
  const w = await show('/search?q=sea')
  expect(w.get('[data-search-when]').text()).toBe('Any time')
  expect(w.get('[data-search-where]').text()).toBe('Any place')
  expect(w.find('[data-search-clear-filters]').exists()).toBe(false)
  expect(engine.places).not.toHaveBeenCalled()
})
