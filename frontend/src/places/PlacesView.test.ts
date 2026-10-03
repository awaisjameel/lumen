import { t } from '../shared/i18n'
import { mountInApp } from '../shared/testing'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { DOMWrapper, flushPromises } from '@vue/test-utils'
import type { Place } from '../bridge'
import { blockRequest } from '../shared/block'
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
  wrapper = mountInApp(PlacesView, { plugins: [testRouter()] })
  await flushPromises()
  await menuAction(wrapper, t('blocked.confirmTitle', { name: place.name }))
  blockRequest.value?.answer(false)
  await flushPromises()
  expect(wrapper.find('.collection-card').exists()).toBe(true)
  expect(reads).toBe(1)

  let fail: ((reason: Error) => void) | undefined
  places = () => new Promise((_resolve, reject) => (fail = reject))
  await menuAction(wrapper, t('blocked.confirmTitle', { name: place.name }))
  blockRequest.value?.answer(true)
  await flushPromises()
  expect(wrapper.find('.collection-card').exists()).toBe(false)
  fail?.(new Error('read failed'))
  await flushPromises()
  expect(wrapper.find('.collection-card').exists()).toBe(false)
  expect(wrapper.get('[role="alert"]').text()).toContain('read failed')
})

function testRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div />' } },
      ...['person', 'people', 'personFaces', 'thing', 'event', 'place', 'album'].map((name) => ({
        name,
        path: `/${name}/:id?`,
        component: { template: '<div />' },
      })),
    ],
  })
}

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
