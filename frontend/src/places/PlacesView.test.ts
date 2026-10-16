import { t } from '../shared/i18n'
import { mountInApp } from '../shared/testing'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { DOMWrapper, flushPromises } from '@vue/test-utils'
import type { Place } from '../bridge'
import { engine } from '../bridge'
import { createPinia } from 'pinia'
import { blockRequest, showBlocksChanged } from '../shared/block'
import PlacesView from './PlacesView.vue'

const place: Place = {
  id: 1,
  name: 'Lahore',
  region: 'Punjab',
  country: 'Pakistan',
  lat: 31.5,
  lon: 74.3,
  count: 3,
  cover: 0,
  coverThumb: 0,
}

// A plain function rather than vi.fn: Vitest's spy reports a rejection it
// returned from this path as the test's own failure, although the view
// handles it (the refresh error is shown below).
let places: () => Promise<Place[]>
let reads = 0
vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<object>()),
  onLibraryChanged: () => () => undefined,
  engine: {
    places: () => {
      reads++
      return places()
    },
    timeline: vi.fn().mockResolvedValue([{ day: '2026-10-01', count: 1 }]),
    days: vi.fn().mockResolvedValue([{ day: '2026-10-01', items: [{ id: 4, w: 100, h: 100, t: 1 }] }]),
  },
}))

let wrapper: ReturnType<typeof mountInApp> | undefined

beforeEach(() => {
  reads = 0
  places = () => Promise.resolve([place])
})
afterEach(() => {
  blockRequest.value?.answer(false)
  blockRequest.value = null
  wrapper?.unmount()
})

it('keeps the place after cancelling a block and clears it before a saved block refresh fails', async () => {
  wrapper = mountInApp(PlacesView, { plugins: [testRouter(), createPinia()] })
  await flushPromises()
  await menuAction(wrapper, t('blocked.confirmTitle', { name: place.name }))
  blockRequest.value?.answer(false)
  await flushPromises()
  expect(wrapper.find('[data-collection]').exists()).toBe(true)
  expect(reads).toBe(1)

  let fail: ((reason: Error) => void) | undefined
  places = () => new Promise((_resolve, reject) => (fail = reject))
  await menuAction(wrapper, t('blocked.confirmTitle', { name: place.name }))
  blockRequest.value?.answer(true)
  await flushPromises()
  expect(wrapper.find('[data-collection]').exists()).toBe(false)
  fail?.(new Error('read failed'))
  await flushPromises()
  expect(wrapper.find('[data-collection]').exists()).toBe(false)
  expect(wrapper.get('[role="alert"]').text()).toContain('read failed')
})

function testRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div />' } },
      { path: '/settings', component: { template: '<div />' } },
      { path: '/blocked', component: { template: '<div />' } },
      ...['person', 'people', 'personFaces', 'thing', 'event', 'place', 'album'].map((name) => ({
        name,
        path: `/${name}/:id?`,
        component: { template: '<div />' },
      })),
    ],
  })
}

it('filters the atlas and cards together by country and highlights the focused card', async () => {
  places = () => Promise.resolve([place, { ...place, id: 2, name: 'Paris', country: 'France', lat: 48.86, lon: 2.35 }])
  wrapper = mountInApp(PlacesView, { plugins: [testRouter(), createPinia()] })
  await flushPromises()
  await wrapper
    .findAll('button')
    .find((b) => b.text() === 'France 1')
    ?.trigger('click')
  await flushPromises()
  expect(wrapper.findAll('[data-collection]')).toHaveLength(1)
  expect(wrapper.get('[data-collection="2"]').text()).toContain('Paris')
  expect(wrapper.find('button[aria-label^="Lahore, Pakistan"]').exists()).toBe(false)
  await wrapper.get('[data-collection="2"]').trigger('focusin')
  expect(wrapper.get('[data-collection="2"]').attributes('data-active')).toBe('true')
})

it('shows the place a place page sent to the map, with its preview open, once', async () => {
  places = () => Promise.resolve([place, { ...place, id: 2, name: 'Paris', country: 'France', lat: 48.86, lon: 2.35 }])
  const router = testRouter()
  await router.push({ path: '/', query: { place: '2' } })
  wrapper = mountInApp(PlacesView, { plugins: [router, createPinia()] })
  await flushPromises()
  expect(wrapper.get('#place-preview').text()).toContain('Paris')
  expect(engine.timeline).toHaveBeenCalledWith({ filter: '', place: 2 }, expect.any(AbortSignal))
  expect(router.currentRoute.value.query.place).toBeUndefined()
})

it('opens a visibility-checked preview, closes it on Escape and returns focus', async () => {
  wrapper = mountInApp(PlacesView, { plugins: [testRouter(), createPinia()] })
  await flushPromises()
  const trigger = wrapper.get<HTMLButtonElement>('button[aria-label="Preview Lahore"]')
  trigger.element.focus()
  await trigger.trigger('click')
  await flushPromises()
  expect(document.activeElement).toBe(wrapper.get('#place-preview').element)
  expect(engine.timeline).toHaveBeenCalledWith({ filter: '', place: 1 }, expect.any(AbortSignal))
  expect(wrapper.get('button[aria-label="Open photo 4"] img').attributes('src')).toContain('4')
  expect(wrapper.get('#place-preview a').attributes('href')).toBe('/place/1')
  await wrapper.get('#place-preview').trigger('keydown', { key: 'Escape' })
  await flushPromises()
  expect(wrapper.find('#place-preview').exists()).toBe(false)
  expect(document.activeElement).toBe(trigger.element)
})

it('clears an open preview and the atlas cover before a block refresh can fail', async () => {
  wrapper = mountInApp(PlacesView, { plugins: [testRouter(), createPinia()] })
  await flushPromises()
  await wrapper.get('button[aria-label="Preview Lahore"]').trigger('click')
  await flushPromises()
  places = () => Promise.reject(new Error('refresh failed'))
  await expect(showBlocksChanged()).rejects.toThrow('refresh failed')
  await flushPromises()
  expect(wrapper.find('#place-preview').exists()).toBe(false)
  expect(wrapper.find('[data-collection]').exists()).toBe(false)
  expect(wrapper.find('button[aria-label="Preview Lahore"]').exists()).toBe(false)
})

it('removes the last filtered city without retaining its atlas cover and offers to reset the filter', async () => {
  const paris = { ...place, id: 2, name: 'Paris', country: 'France', lat: 48.86, lon: 2.35 }
  places = () => Promise.resolve([place, paris])
  wrapper = mountInApp(PlacesView, { plugins: [testRouter(), createPinia()] })
  await flushPromises()
  await wrapper
    .findAll('button')
    .find((button) => button.text() === 'Pakistan 1')
    ?.trigger('click')
  places = () => Promise.resolve([paris])
  await showBlocksChanged()
  await flushPromises()
  expect(wrapper.find('button[aria-label="Preview Lahore"]').exists()).toBe(false)
  expect(wrapper.find('[data-collection]').exists()).toBe(false)
  const reset = wrapper.findAll('button').find((button) => button.text() === t('places.showAll'))
  expect(reset).toBeDefined()
  await reset?.trigger('click')
  expect(wrapper.get('[data-collection="2"]').text()).toContain('Paris')
})

it('announces initial loading and offers a retry after a failed read', async () => {
  let fail: ((error: Error) => void) | undefined
  places = () => new Promise((_resolve, reject) => (fail = reject))
  wrapper = mountInApp(PlacesView, { plugins: [testRouter(), createPinia()] })
  expect(wrapper.get('section[aria-label="Loading places…"]').attributes('aria-busy')).toBe('true')
  fail?.(new Error('Library busy'))
  await flushPromises()
  expect(wrapper.get('[role="alert"]').text()).toContain('Library busy')
  places = () => Promise.resolve([place])
  await wrapper.get('[role="alert"] button').trigger('click')
  await flushPromises()
  expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  expect(wrapper.get('[data-collection="1"]').text()).toContain('Lahore')
})

it('offers blocked-item recovery when the last visible place disappears', async () => {
  wrapper = mountInApp(PlacesView, { plugins: [testRouter(), createPinia()] })
  await flushPromises()
  places = () => Promise.resolve([])
  await showBlocksChanged()
  await flushPromises()
  expect(wrapper.text()).toContain(t('places.cleared'))
  expect(wrapper.get('a[href="/blocked"]').text()).toBe(t('places.manageBlocked'))
})

vi.mock('../search/SearchBox.vue', () => ({ default: { render: () => null } }))

async function menuAction(
  _w: ReturnType<typeof mountInApp>,
  label: string,
  trigger = 'button[aria-label^="Actions for"]',
) {
  await new DOMWrapper(document.body).get(trigger).trigger('keydown', { key: 'Enter' })
  await flushPromises()
  const item = [...document.body.querySelectorAll<HTMLElement>('[role="menuitem"]')].find(
    (el) => el.textContent.trim() === label,
  )
  if (!item) throw new Error(`No menu item named ${label}`)
  item.click()
  await flushPromises()
}
