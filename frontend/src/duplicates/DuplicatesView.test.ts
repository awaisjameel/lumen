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
  engine: { duplicateGroups: vi.fn(), showInFolder: vi.fn() },
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
  vi.mocked(engine.showInFolder).mockReset().mockResolvedValue(undefined)
})
afterEach(() => wrapper?.unmount())

it('names each similar photo and shows the copy it lists', async () => {
  wrapper = mount(DuplicatesView, { global: { stubs: { SearchBox: true } } })
  await flushPromises()
  const previews = wrapper.findAll('.preview-button').map((b) => b.attributes('aria-label'))
  expect(previews).toEqual(['Preview “one.jpg”', 'Preview “two.jpg”'])
  await wrapper.findAll('.similar-item .btn')[1]?.trigger('click')
  expect(engine.showInFolder).toHaveBeenCalledWith(2, 'C:\\b\\two.jpg')
})

it('renders a page of groups at a time as the list scrolls', async () => {
  let seen: IntersectionObserverCallback | undefined
  const observe = vi.fn()
  vi.stubGlobal(
    'IntersectionObserver',
    class {
      constructor(cb: IntersectionObserverCallback) {
        seen = cb
      }
      observe = observe
      unobserve = vi.fn()
      disconnect = vi.fn()
    },
  )
  try {
    vi.mocked(engine.duplicateGroups).mockResolvedValue(
      Array.from({ length: 100 }, (_, i) => ({
        kind: i < 30 ? DuplicateKind.DuplicateExact : DuplicateKind.DuplicateSimilar,
        items: [photo(2 * i + 1, `C:\\a\\${i}.jpg`), photo(2 * i + 2, `C:\\b\\${i}.jpg`)],
      })),
    )
    wrapper = mount(DuplicatesView, { global: { stubs: { SearchBox: true } } })
    await flushPromises()
    // Every exact group first, then the similar ones up to the page.
    expect(wrapper.findAll('.group')).toHaveLength(40)
    expect(wrapper.findAll('.group.similar')).toHaveLength(10)
    expect(observe).toHaveBeenCalled()

    seen?.([{ isIntersecting: true } as IntersectionObserverEntry], {} as IntersectionObserver)
    await flushPromises()
    expect(wrapper.findAll('.group')).toHaveLength(80)
    seen?.([{ isIntersecting: true } as IntersectionObserverEntry], {} as IntersectionObserver)
    await flushPromises()
    expect(wrapper.findAll('.group')).toHaveLength(100)
    expect(wrapper.find('.more').exists()).toBe(false)
  } finally {
    vi.unstubAllGlobals()
  }
})
