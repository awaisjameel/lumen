import { afterEach, expect, it } from 'vitest'
import { DOMWrapper, flushPromises } from '@vue/test-utils'
import { mountInApp } from '../shared/testing'
import OfflineAtlas from './OfflineAtlas.vue'

let wrapper: ReturnType<typeof mountInApp> | undefined
afterEach(() => wrapper?.unmount())
it('lets the keyboard choose each city in an overlapping marker group', async () => {
  const place = {
    id: 1,
    name: 'Lahore',
    country: 'Pakistan',
    region: 'Punjab',
    count: 3,
    cover: 0,
    coverThumb: 0,
    lat: 31.55,
    lon: 74.34,
  }
  const nearby = { ...place, id: 2, name: 'Nearby city', lat: 31.56 }
  wrapper = mountInApp(OfflineAtlas, { props: { places: [place, nearby], active: 1 } })
  expect(wrapper.findAll('button')).toHaveLength(1)
  await wrapper.get('button[aria-label="Choose from 2 nearby cities"]').trigger('click')
  await flushPromises()
  const choice = new DOMWrapper(document.body)
    .findAll('button')
    .find((button) => button.text() === 'Nearby city · Pakistan')
  await choice?.trigger('focus')
  await choice?.trigger('click')
  expect(wrapper.findComponent(OfflineAtlas).emitted('preview')?.[0]).toEqual([nearby])
})
