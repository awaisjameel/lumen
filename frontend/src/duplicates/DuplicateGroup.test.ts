import { afterEach, expect, it } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises } from '@vue/test-utils'
import { DuplicateKind } from '../bridge'
import type { DuplicateGroup } from '../bridge'
import { mountInApp } from '../shared/testing'
import DuplicateGroupCard from './DuplicateGroup.vue'
import { useKeepChoices } from './store'

const group: DuplicateGroup = {
  kind: DuplicateKind.DuplicateExact,
  items: [
    {
      id: 1,
      paths: ['C:/one.jpg', 'D:/two.jpg'],
      size: 100,
      width: 10,
      height: 10,
      thumb: 0,
      takenAt: 0,
      ext: '.jpg',
      camera: false,
      exifDate: false,
      gps: false,
      addedAt: 0,
      mtimes: [0, 0],
      best: true,
      keep: 'C:/one.jpg',
    },
  ],
}
let wrapper: ReturnType<typeof mountInApp> | undefined
afterEach(() => wrapper?.unmount())

it('chooses a keeper from its thumbnail and previews the precise file from a separate action', async () => {
  setActivePinia(createPinia())
  wrapper = mountInApp(DuplicateGroupCard, { props: { group, busy: false } })
  await flushPromises()
  await wrapper.findAll('button[aria-pressed]')[1]?.trigger('click')
  expect(useKeepChoices().keeperOf(group)?.path).toBe('D:/two.jpg')
  expect(wrapper.findComponent(DuplicateGroupCard).emitted('preview')).toBeUndefined()
  await wrapper.findAll('[data-copy-preview]')[1]?.trigger('click')
  expect(wrapper.findComponent(DuplicateGroupCard).emitted('preview')?.[0]).toEqual([group.items[0], 'D:/two.jpg'])
})
