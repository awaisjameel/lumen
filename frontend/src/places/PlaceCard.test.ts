import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, expect, it } from 'vitest'
import { mountInApp } from '../shared/testing'
import PlaceCard from './PlaceCard.vue'

let wrapper: ReturnType<typeof mountInApp> | undefined
afterEach(() => wrapper?.unmount())
it('provides a collection link and a separate preview action with formatted counts', async () => {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div />' } },
      { name: 'place', path: '/places/:id', component: { template: '<div />' } },
    ],
  })
  const place = {
    id: 1,
    name: 'Lahore',
    country: 'Pakistan',
    region: 'Punjab',
    count: 123456,
    cover: 0,
    coverThumb: 0,
    lat: 31.55,
    lon: 74.34,
  }
  wrapper = mountInApp(PlaceCard, { plugins: [router], props: { place, active: true } })
  expect(wrapper.get('a[aria-label="Lahore"]').attributes('href')).toBe('/places/1')
  expect(wrapper.text()).toContain('123,456')
  await wrapper.get('button[aria-label="Preview Lahore"]').trigger('click')
  expect(wrapper.findComponent(PlaceCard).emitted('preview')).toHaveLength(1)
})
