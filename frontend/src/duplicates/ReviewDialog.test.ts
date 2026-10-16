import { afterEach, expect, it } from 'vitest'
import { DOMWrapper, flushPromises } from '@vue/test-utils'
import { DuplicateKind } from '../bridge'
import type { DuplicateItem } from '../bridge'
import { mountInApp } from '../shared/testing'
import ReviewDialog from './ReviewDialog.vue'

const keep: DuplicateItem = {
  id: 1,
  paths: ['C:/keep.jpg', 'D:/keep.jpg'],
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
  keep: '',
}
let wrapper: ReturnType<typeof mountInApp> | undefined
afterEach(() => wrapper?.unmount())
const dialog = () => new DOMWrapper(document.body)

it('lists every path of the kept and removed similar assets and names the reversible destination', async () => {
  wrapper = mountInApp(ReviewDialog, {
    props: {
      busy: false,
      review: {
        kind: DuplicateKind.DuplicateSimilar,
        keep,
        keepPath: '',
        recycle: [],
        trash: [{ ...keep, id: 2, paths: ['C:/other.jpg', 'D:/other.jpg'] }],
      },
    },
  })
  await flushPromises()
  const review = dialog().get('[data-duplicate-review]')
  for (const path of ['C:/keep.jpg', 'D:/keep.jpg', 'C:/other.jpg', 'D:/other.jpg'])
    expect(review.text()).toContain(path)
  expect(dialog().get('[data-review-confirm]').text()).toBe('Move 1 item to Trash')
  expect(review.text()).toContain('Undo')
})

it('names the exact file count and the Windows Recycle Bin recovery route', async () => {
  wrapper = mountInApp(ReviewDialog, {
    props: {
      busy: false,
      review: {
        kind: DuplicateKind.DuplicateExact,
        keep,
        keepPath: 'C:/keep.jpg',
        recycle: ['D:/keep.jpg', 'E:/keep.jpg'],
        trash: [],
      },
    },
  })
  await flushPromises()
  expect(dialog().get('[data-review-confirm]').text()).toBe('Move 2 files to Recycle Bin')
  expect(dialog().get('[data-duplicate-review]').text()).toContain('Windows Recycle Bin')
  await dialog().get('[data-review-cancel]').trigger('click')
  expect(wrapper.findComponent(ReviewDialog).emitted('cancel')).toHaveLength(1)
})
