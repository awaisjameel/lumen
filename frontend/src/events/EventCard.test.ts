import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import type { Event } from '../bridge'
import { mountInApp } from '../shared/testing'
import { t } from '../shared/i18n'
import EventCard from './EventCard.vue'

const event: Event = {
  id: 1,
  name: 'Garden walk',
  count: 123456,
  cover: 0,
  coverThumb: 0,
  placeId: 0,
  pinned: false,
  startAt: Date.UTC(2026, 9, 1) / 1000,
  endAt: Date.UTC(2026, 9, 3) / 1000,
}
let wrapper: ReturnType<typeof mountInApp> | undefined
afterEach(() => wrapper?.unmount())
function mounted(rename = vi.fn().mockResolvedValue(true), item = event) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div />' } },
      { name: 'event', path: '/events/:id', component: { template: '<div />' } },
    ],
  })
  wrapper = mountInApp(EventCard, { props: { event: item, busy: false, rename }, plugins: [router] })
  return wrapper
}
it('shows a real date range, formatted counts and a named collection link without a preview', async () => {
  const w = mounted()
  await flushPromises()
  expect(w.text().replace(/\s/gu, '')).toContain('1–3Oct2026')
  expect(w.text()).toContain('123,456')
  expect(w.get('a[aria-label="Garden walk"]').attributes('href')).toBe('/events/1')
  expect(w.text()).toContain(t('events.noPreview'))
})
it('shows separate photo and video totals from the collection contract', () => {
  const w = mounted(undefined, { ...event, count: 123456, videos: 1 })
  expect(w.text()).toContain(t('events.photos', { n: 123455 }))
  expect(w.text()).toContain(t('events.videos', { n: 1 }))
})
it('leaves out a kind the event has none of', () => {
  const photos = mounted(undefined, { ...event, count: 9, videos: 0 })
  expect(photos.get('.text .meta').text()).toBe(t('events.photos', { n: 9 }))
  photos.unmount()
  const videos = mounted(undefined, { ...event, count: 2, videos: 2 })
  expect(videos.get('.text .meta').text()).toBe(t('events.videos', { n: 2 }))
})
it('starts renaming from the title and saves a blank draft as an automatic-name reset', async () => {
  const rename = vi.fn().mockResolvedValue(true)
  const w = mounted(rename)
  await w.get('button[title="Rename Garden walk"]').trigger('click')
  await flushPromises()
  await w.get('input[aria-label="Event name"]').setValue('')
  await w.get('form').trigger('submit')
  await flushPromises()
  expect(rename).toHaveBeenCalledWith(event, '')
  expect(w.find('form').exists()).toBe(false)
})
