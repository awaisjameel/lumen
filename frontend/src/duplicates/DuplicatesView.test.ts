import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import { DuplicateKind, engine } from '../bridge'
import type * as Bridge from '../bridge'
import type { DuplicateItem } from '../bridge'
import DuplicatesView from './DuplicatesView.vue'

vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  onLibraryChanged: vi.fn(() => () => undefined),
  engine: { duplicateGroups: vi.fn(), showFileInFolder: vi.fn() },
}))

const photo = (id: number, path: string): DuplicateItem => ({
  id,
  paths: [path],
  size: 2048,
  width: 400,
  height: 300,
  thumb: 1,
  takenAt: 0,
})
let wrapper: ReturnType<typeof mount> | undefined

beforeEach(() => {
  setActivePinia(createPinia())
  vi.mocked(engine.duplicateGroups)
    .mockReset()
    .mockResolvedValue([
      { kind: DuplicateKind.DuplicateSimilar, items: [photo(1, 'C:\\a\\one.jpg'), photo(2, 'C:\\b\\two.jpg')] },
    ])
  vi.mocked(engine.showFileInFolder).mockReset().mockResolvedValue(undefined)
})
afterEach(() => wrapper?.unmount())

it('names each similar photo and shows the copy it lists', async () => {
  wrapper = mount(DuplicatesView, { global: { stubs: { SearchBox: true } } })
  await flushPromises()
  const previews = wrapper.findAll('.preview-button').map((b) => b.attributes('aria-label'))
  expect(previews).toEqual(['Preview “one.jpg”', 'Preview “two.jpg”'])
  await wrapper.findAll('.similar-item .btn')[1]?.trigger('click')
  expect(engine.showFileInFolder).toHaveBeenCalledWith(2, 'C:\\b\\two.jpg')
})
