import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import { DuplicateKind, engine } from '../bridge'
import type * as Bridge from '../bridge'
import type { DuplicateItem } from '../bridge'
import { useDuplicates } from './store'
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
  ext: '.jpg',
  camera: false,
  exifDate: false,
  gps: false,
  addedAt: 0,
  mtimes: [0],
  best: false,
})
let wrapper: ReturnType<typeof mount> | undefined

function height(element: Element): number {
  return element.classList.contains('page-body')
    ? 600
    : element.classList.contains('virtual-row')
      ? element.querySelector('.group')
        ? 180 + (Number(element.getAttribute('data-index')) % 3) * 95
        : 88
      : 40
}

class TestResizeObserver implements ResizeObserver {
  constructor(private readonly callback: ResizeObserverCallback) {}
  observe(target: Element) {
    const size: ResizeObserverSize = { inlineSize: 900, blockSize: height(target) }
    this.callback(
      [
        {
          target,
          contentRect: new DOMRect(0, 0, 900, size.blockSize),
          borderBoxSize: [size],
          contentBoxSize: [size],
          devicePixelContentBoxSize: [size],
        },
      ],
      this,
    )
  }
  unobserve = vi.fn()
  disconnect = vi.fn()
}

beforeEach(() => {
  setActivePinia(createPinia())
  vi.stubGlobal('ResizeObserver', TestResizeObserver)
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
    return new DOMRect(
      0,
      this.classList.contains('virtual-list') ? 40 - (this.parentElement?.scrollTop ?? 0) : 0,
      900,
      height(this),
    )
  })
  vi.spyOn(HTMLElement.prototype, 'offsetHeight', 'get').mockImplementation(function (this: HTMLElement) {
    return height(this)
  })
  vi.mocked(engine.duplicateGroups)
    .mockReset()
    .mockResolvedValue([
      { kind: DuplicateKind.DuplicateSimilar, items: [photo(1, 'C:\\a\\one.jpg'), photo(2, 'C:\\b\\two.jpg')] },
    ])
  vi.mocked(engine.showInFolder).mockReset().mockResolvedValue(undefined)
})
afterEach(() => {
  wrapper?.unmount()
  vi.unstubAllGlobals()
})

it('names each similar photo and shows the copy it lists', async () => {
  wrapper = mount(DuplicatesView, { global: { stubs: { SearchBox: true } } })
  await flushPromises()
  const previews = wrapper.findAll('.preview-button').map((b) => b.attributes('aria-label'))
  expect(previews).toEqual(['Preview “one.jpg”', 'Preview “two.jpg”'])
  await wrapper.findAll('.similar-item .btn')[1]?.trigger('click')
  expect(engine.showInFolder).toHaveBeenCalledWith(2, 'C:\\b\\two.jpg')
})

it('keeps the DOM bounded and jumps directly to the final similar groups', async () => {
  vi.mocked(engine.duplicateGroups).mockResolvedValue(
    Array.from({ length: 1005 }, (_, index) => ({
      kind: index < 1000 ? DuplicateKind.DuplicateExact : DuplicateKind.DuplicateSimilar,
      items: [photo(index + 1, `C:\\Photos\\photo-${index + 1}.jpg`)],
    })),
  )
  wrapper = mount(DuplicatesView, { attachTo: document.body, global: { stubs: { SearchBox: true } } })
  await flushPromises()
  expect(wrapper.findAll('.group').length).toBeGreaterThan(0)
  expect(wrapper.findAll('.group').length).toBeLessThan(15)
  const scroller = wrapper.get<HTMLElement>('.page-body')
  const total = Number.parseFloat(wrapper.get<HTMLElement>('.virtual-list').element.style.height)
  scroller.element.scrollTop = total - 600
  await scroller.trigger('scroll')
  await flushPromises()
  expect(wrapper.text()).toContain('photo-1005.jpg')
  expect(wrapper.findAll('.group').length).toBeLessThan(15)
  expect(wrapper.text()).not.toContain('photo-1.jpg')
  vi.mocked(engine.duplicateGroups).mockResolvedValue([
    { kind: DuplicateKind.DuplicateExact, items: [photo(1, 'C:\\Photos\\photo-1.jpg')] },
  ])
  await useDuplicates().load()
  scroller.element.scrollTop = 0
  await scroller.trigger('scroll')
  await flushPromises()
  expect(wrapper.text()).toContain('photo-1.jpg')
  expect(wrapper.text()).not.toContain('photo-1005.jpg')
})

it('opens the preview from a virtual row', async () => {
  wrapper = mount(DuplicatesView, { global: { stubs: { SearchBox: true } } })
  await flushPromises()
  const dialog = wrapper.get<HTMLDialogElement>('dialog').element
  const showModal = vi.fn()
  dialog.showModal = showModal
  await wrapper.get('.preview-button').trigger('click')
  await flushPromises()
  expect(showModal).toHaveBeenCalledOnce()
  expect(wrapper.get('dialog img').attributes('alt')).toBe('one.jpg')
})

it('closes the preview when its asset disappears from refreshed groups', async () => {
  wrapper = mount(DuplicatesView, { global: { stubs: { SearchBox: true } } })
  await flushPromises()
  const dialog = wrapper.get<HTMLDialogElement>('dialog').element
  dialog.showModal = vi.fn(() => (dialog.open = true))
  const close = vi.fn(() => (dialog.open = false))
  dialog.close = close
  await wrapper.get('.preview-button').trigger('click')
  await flushPromises()
  vi.mocked(engine.duplicateGroups).mockResolvedValue([
    { kind: DuplicateKind.DuplicateSimilar, items: [photo(2, 'C:\\b\\two.jpg')] },
  ])
  await useDuplicates().load()
  await flushPromises()
  expect(close).toHaveBeenCalledOnce()
  expect(dialog.open).toBe(false)
  expect(wrapper.find('dialog img').exists()).toBe(false)
  expect(wrapper.get('dialog').text()).not.toContain('one.jpg')
})

it('updates the open preview when its paths and thumbnail change', async () => {
  wrapper = mount(DuplicatesView, { global: { stubs: { SearchBox: true } } })
  await flushPromises()
  const dialog = wrapper.get<HTMLDialogElement>('dialog').element
  dialog.showModal = vi.fn()
  const close = vi.fn()
  dialog.close = close
  await wrapper.get('.preview-button').trigger('click')
  await flushPromises()
  vi.mocked(engine.duplicateGroups).mockResolvedValue([
    { kind: DuplicateKind.DuplicateSimilar, items: [{ ...photo(1, 'C:\\renamed.jpg'), thumb: 2 }] },
  ])
  await useDuplicates().load()
  await flushPromises()
  expect(close).not.toHaveBeenCalled()
  expect(wrapper.get('dialog img').attributes('src')).toContain('?v=2')
  expect(wrapper.get('dialog img').attributes('alt')).toBe('renamed.jpg')
  expect(wrapper.get('dialog').text()).toContain('C:\\renamed.jpg')
  expect(wrapper.get('dialog').text()).not.toContain('one.jpg')
})

it('does not show cached paths on remount after visibility changes, even if the refresh fails', async () => {
  wrapper = mount(DuplicatesView, { global: { stubs: { SearchBox: true } } })
  await flushPromises()
  expect(wrapper.text()).toContain('one.jpg')
  wrapper.unmount()
  let finish!: (value: Bridge.DuplicateGroup[]) => void
  vi.mocked(engine.duplicateGroups)
    .mockReturnValueOnce(new Promise((resolve) => (finish = resolve)))
    .mockRejectedValue(new Error('read failed'))
  const invalidated = useDuplicates().invalidate()
  const rejected = expect(invalidated).rejects.toThrow('read failed')
  wrapper = mount(DuplicatesView, { global: { stubs: { SearchBox: true } } })
  await flushPromises()
  expect(wrapper.text()).not.toContain('one.jpg')
  finish([{ kind: DuplicateKind.DuplicateSimilar, items: [photo(1, 'C:\\a\\one.jpg')] }])
  await rejected
  await flushPromises()
  expect(wrapper.text()).not.toContain('one.jpg')
  expect(wrapper.get('[role="alert"]').text()).toContain('read failed')
})

it('retains unequal measured heights and the deep scroll position on an unchanged reload', async () => {
  const groups = Array.from({ length: 1005 }, (_, index) => ({
    kind: DuplicateKind.DuplicateExact,
    items: [photo(index + 1, `C:\\Photos\\photo-${index + 1}.jpg`)],
  }))
  vi.mocked(engine.duplicateGroups).mockResolvedValue(groups)
  wrapper = mount(DuplicatesView, { attachTo: document.body, global: { stubs: { SearchBox: true } } })
  await flushPromises()
  const scroller = wrapper.get<HTMLElement>('.page-body')
  scroller.element.scrollTop = 120000
  await scroller.trigger('scroll')
  await flushPromises()
  const measured = wrapper.findAll('.virtual-row').map((row) => height(row.element))
  expect(new Set(measured).size).toBeGreaterThan(1)
  const total = wrapper.get<HTMLElement>('.virtual-list').element.style.height
  const positions = () =>
    wrapper?.findAll('.virtual-row').map((row) => [row.attributes('data-index'), row.attributes('style')])
  const before = positions()
  expect(Number(wrapper.get('.virtual-row').attributes('data-index'))).toBeGreaterThan(400)
  vi.mocked(engine.duplicateGroups).mockResolvedValue(groups.map((group) => ({ ...group, items: [...group.items] })))
  await useDuplicates().load()
  await flushPromises()
  expect(scroller.element.scrollTop).toBe(120000)
  expect(wrapper.get<HTMLElement>('.virtual-list').element.style.height).toBe(total)
  expect(positions()).toEqual(before)
})
