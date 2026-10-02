import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import { DuplicateKind, engine } from '../bridge'
import type * as Bridge from '../bridge'
import type { DuplicateGroup, DuplicateItem } from '../bridge'
import { notice } from '../shared/errors'
import { clearTrashUndo, trashUndo } from '../trash/undo'
import { useDuplicates } from './store'
import DuplicatesView from './DuplicatesView.vue'

vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  onLibraryChanged: vi.fn(() => () => undefined),
  engine: { duplicateGroups: vi.fn(), showInFolder: vi.fn(), trashAssets: vi.fn(), deleteCopies: vi.fn() },
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
  clearTrashUndo()
  notice.value = ''
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

describe('choosing the copy to keep', () => {
  const similar = (best: number): DuplicateGroup => ({
    kind: DuplicateKind.DuplicateSimilar,
    items: [1, 2, 3].map((id) => ({ ...photo(id, `C:\\Photos\\p${id}.jpg`), best: id === best })),
  })
  const exact: DuplicateGroup = {
    kind: DuplicateKind.DuplicateExact,
    items: [
      {
        ...photo(9, 'C:\\Photos\\a\\IMG_9.jpg'),
        paths: ['C:\\Photos\\a\\IMG_9.jpg', 'C:\\Photos\\b\\IMG_9 copy.jpg'],
        mtimes: [200, 100],
        best: true,
        keep: 'C:\\Photos\\b\\IMG_9 copy.jpg',
      },
    ],
  }

  beforeEach(() => {
    // jsdom has no modal dialogs: opening one marks it open.
    HTMLDialogElement.prototype.showModal = vi.fn(function (this: HTMLDialogElement) {
      this.setAttribute('open', '')
    })
    HTMLDialogElement.prototype.close = vi.fn(function (this: HTMLDialogElement) {
      this.removeAttribute('open')
    })
    vi.mocked(engine.trashAssets).mockReset().mockResolvedValue(undefined)
    vi.mocked(engine.deleteCopies).mockReset().mockResolvedValue({ recycled: 1, kept: [] })
  })

  async function mountWith(...groups: DuplicateGroup[]) {
    wrapper?.unmount()
    vi.mocked(engine.duplicateGroups).mockResolvedValue(groups)
    wrapper = mount(DuplicatesView, { global: { stubs: { SearchBox: true } } })
    await flushPromises()
  }
  const radios = () => wrapper?.findAll<HTMLInputElement>('input[type="radio"]') ?? []
  const checked = () => radios().map((r) => r.element.checked)
  const reviewDialog = () => {
    const found = wrapper?.find<HTMLDialogElement>('dialog.review')
    if (!found?.exists()) throw new Error('no review dialog')
    return found
  }
  const opened = () => reviewDialog().element.hasAttribute('open')
  const review = async () => {
    await wrapper?.get('.group-actions .btn').trigger('click')
    await flushPromises()
  }
  const confirm = async () => {
    await reviewDialog().get('.btn-primary').trigger('click')
    await flushPromises()
  }

  it('pre-selects the copy the engine suggests in each similar group and marks it', async () => {
    await mountWith(similar(2))
    expect(checked()).toEqual([false, true, false])
    expect(wrapper?.findAll('.keep em').map((e) => e.text())).toEqual(['Suggested'])
    expect(wrapper?.get('.similar-item:nth-child(2) .keep').text()).toContain('Suggested')
  })

  it('pre-selects the suggested file of an exact copy', async () => {
    await mountWith(exact)
    expect(checked()).toEqual([false, true])
    expect(wrapper?.get('.item li:nth-child(2) .keep').text()).toContain('Suggested')
  })

  it('lets the user pick another copy without changing anything', async () => {
    await mountWith(similar(2))
    await radios()[0]?.setValue()
    expect(wrapper?.findAll('.keep em').map((e) => e.text())).toEqual(['Suggested']) // the suggestion stays marked
    await review()
    expect(reviewDialog().get('.keeps').text()).toBe('C:\\Photos\\p1.jpg')
    expect(reviewDialog().get('.removes').text()).toContain('p2.jpg')
    await mountWith(exact)
    await radios()[0]?.setValue()
    await review()
    expect(reviewDialog().get('.keeps').text()).toBe('C:\\Photos\\a\\IMG_9.jpg')
    expect(reviewDialog().get('.removes').text()).toBe('C:\\Photos\\b\\IMG_9 copy.jpg')
    expect(engine.trashAssets).not.toHaveBeenCalled()
    expect(engine.deleteCopies).not.toHaveBeenCalled()
  })

  it('does nothing until the user confirms, and lists what would happen', async () => {
    await mountWith(similar(2))
    expect(opened()).toBe(false)
    await review()
    expect(opened()).toBe(true)
    const text = reviewDialog().text()
    expect(reviewDialog().get('.keeps').text()).toBe('C:\\Photos\\p2.jpg')
    expect(reviewDialog().get('.removes').text()).toContain('p1.jpg')
    expect(reviewDialog().get('.removes').text()).toContain('p3.jpg')
    expect(reviewDialog().get('.removes').text()).not.toContain('p2.jpg')
    expect(text).toContain('These 2 photos move to Lumen’s Trash')
    expect(engine.trashAssets).not.toHaveBeenCalled()
    await reviewDialog().findAll('.btn')[0]?.trigger('click') // Cancel
    expect(opened()).toBe(false)
    expect(engine.trashAssets).not.toHaveBeenCalled()
    expect(engine.deleteCopies).not.toHaveBeenCalled()
  })

  it('moves the similar photos that are not kept to the Trash once confirmed, and offers to undo it', async () => {
    await mountWith(similar(2))
    await radios()[0]?.setValue() // keep photo 1
    await review()
    await confirm()
    expect(engine.trashAssets).toHaveBeenCalledExactlyOnceWith([2, 3])
    expect(engine.deleteCopies).not.toHaveBeenCalled()
    expect(trashUndo.value).toEqual([2, 3])
    expect(opened()).toBe(false)
    expect(engine.duplicateGroups).toHaveBeenCalledTimes(2) // reloaded
  })

  it('recycles only the extra files of exact copies once confirmed', async () => {
    await mountWith(exact)
    await review()
    expect(reviewDialog().text()).toContain('This file moves to the Windows Recycle Bin')
    expect(reviewDialog().get('.removes').text()).toBe('C:\\Photos\\a\\IMG_9.jpg')
    expect(reviewDialog().get('.keeps').text()).toBe('C:\\Photos\\b\\IMG_9 copy.jpg')
    expect(engine.deleteCopies).not.toHaveBeenCalled()
    await confirm()
    expect(engine.deleteCopies).toHaveBeenCalledExactlyOnceWith(9, 'C:\\Photos\\b\\IMG_9 copy.jpg')
    expect(engine.trashAssets).not.toHaveBeenCalled()
    expect(trashUndo.value).toBeNull() // recycled files are not in the Trash to restore
  })

  it('recycles the other files when the user keeps another file of an exact copy', async () => {
    await mountWith(exact)
    await radios()[0]?.setValue()
    await review()
    await confirm()
    expect(engine.deleteCopies).toHaveBeenCalledExactlyOnceWith(9, 'C:\\Photos\\a\\IMG_9.jpg')
  })

  it('says how many files the Recycle Bin would not take', async () => {
    vi.mocked(engine.deleteCopies).mockResolvedValue({ recycled: 0, kept: ['C:\\Photos\\a\\IMG_9.jpg'] })
    await mountWith(exact)
    await review()
    await confirm()
    expect(notice.value).toContain('1 file couldn’t be moved')
  })

  it('shows why a confirmed move failed and reloads the groups', async () => {
    vi.mocked(engine.trashAssets).mockRejectedValue(new Error('disk on fire'))
    await mountWith(similar(2))
    await review()
    await confirm()
    expect(wrapper?.get('[role="alert"]').text()).toContain('disk on fire')
    expect(trashUndo.value).toBeNull()
    expect(engine.duplicateGroups).toHaveBeenCalledTimes(2)
  })

  it('keeps the user’s choice across a reload while the group still exists', async () => {
    await mountWith(similar(2))
    await radios()[2]?.setValue()
    // A reload brings the same group with a new member in front and the suggestion elsewhere.
    vi.mocked(engine.duplicateGroups).mockResolvedValue([
      { kind: DuplicateKind.DuplicateSimilar, items: [photo(4, 'C:\\Photos\\p4.jpg'), ...similar(1).items] },
    ])
    await useDuplicates().load()
    await flushPromises()
    expect(checked()).toEqual([false, false, false, true])
    // The same for an exact copy's file.
    await mountWith(exact)
    await radios()[0]?.setValue()
    vi.mocked(engine.duplicateGroups).mockResolvedValue([{ ...exact, items: exact.items.map((it) => ({ ...it })) }])
    await useDuplicates().load()
    await flushPromises()
    expect(checked()).toEqual([true, false])
  })

  it('forgets a choice once its photo has left the groups', async () => {
    await mountWith(similar(2))
    await radios()[2]?.setValue()
    vi.mocked(engine.duplicateGroups).mockResolvedValue([])
    await useDuplicates().load()
    await flushPromises()
    vi.mocked(engine.duplicateGroups).mockResolvedValue([similar(2)])
    await useDuplicates().load()
    await flushPromises()
    expect(checked()).toEqual([false, true, false]) // the suggestion again
  })

  it('has nothing to review in a group with a single file', async () => {
    await mountWith({
      kind: DuplicateKind.DuplicateExact,
      items: [{ ...photo(1, 'C:\\one.jpg'), best: true, keep: 'C:\\one.jpg' }],
    })
    expect(wrapper?.get<HTMLButtonElement>('.group-actions .btn').element.disabled).toBe(true)
  })
})
