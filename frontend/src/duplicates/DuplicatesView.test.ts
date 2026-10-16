import { t } from '../shared/i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { flushPromises, DOMWrapper } from '@vue/test-utils'
import { DuplicateKind, engine } from '../bridge'
import type * as Bridge from '../bridge'
import type { DuplicateGroup, DuplicateItem } from '../bridge'
import { showBlocksChanged } from '../shared/block'
import { commands, runCommand } from '../shared/commands'
import { mountInApp, recordNotifications } from '../shared/testing'
import type { RecordedNotifications } from '../shared/testing'
import { KEPT_NOTIFICATION } from '../trash/actions'
import { TRASH_UNDO_NOTIFICATION } from '../trash/undo'
import { useDuplicates } from './store'
import DuplicatesView from './DuplicatesView.vue'

vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  onLibraryChanged: vi.fn(() => () => undefined),
  engine: {
    duplicateGroups: vi.fn(),
    showInFolder: vi.fn(),
    trashAssets: vi.fn(),
    deleteCopies: vi.fn(),
    restoreAssets: vi.fn(),
    counts: vi.fn(),
  },
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
let wrapper: ReturnType<typeof mountInApp> | undefined

function height(element: Element): number {
  return element.hasAttribute('data-duplicates-scroll')
    ? 600
    : element.classList.contains('virtual-row')
      ? element.querySelector('[data-duplicate-group]')
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

let notifications: RecordedNotifications

function mountView() {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: DuplicatesView }] })
  return mountInApp(DuplicatesView, { plugins: [router] })
}

beforeEach(() => {
  setActivePinia(createPinia())
  notifications = recordNotifications()
  vi.mocked(engine.restoreAssets).mockReset().mockResolvedValue(undefined)
  vi.mocked(engine.counts).mockReset().mockResolvedValue({ photos: 0, videos: 0, favourites: 0, people: 0, trash: 0 })
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
  notifications.stop()
  wrapper = undefined
})

it('names each similar photo and shows the copy it lists', async () => {
  wrapper = mountView()
  await flushPromises()
  const previews = wrapper.findAll('.preview-button').map((b) => b.attributes('aria-label'))
  expect(previews).toEqual(['Preview “one.jpg”', 'Preview “two.jpg”'])
  await wrapper.findAll('[data-show-file]')[1]?.trigger('click')
  expect(engine.showInFolder).toHaveBeenCalledWith(2, 'C:\\b\\two.jpg')
})

it('keeps the DOM bounded and jumps directly to the final similar groups', async () => {
  vi.mocked(engine.duplicateGroups).mockResolvedValue(
    Array.from({ length: 1005 }, (_, index) => ({
      kind: DuplicateKind.DuplicateSimilar,
      items: [photo(index + 1, `C:\\Photos\\photo-${index + 1}.jpg`)],
    })),
  )
  wrapper = mountView()
  await flushPromises()
  expect(wrapper.findAll('[data-duplicate-group]').length).toBeGreaterThan(0)
  expect(wrapper.findAll('[data-duplicate-group]').length).toBeLessThan(15)
  const scroller = wrapper.get<HTMLElement>('[data-duplicates-scroll]')
  const total = Number.parseFloat(wrapper.get<HTMLElement>('.virtual-list').element.style.height)
  scroller.element.scrollTop = total - 600
  await scroller.trigger('scroll')
  await flushPromises()
  expect(wrapper.text()).toContain('photo-1005.jpg')
  expect(wrapper.findAll('[data-duplicate-group]').length).toBeLessThan(15)
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

function previewDialog() {
  return document.body.querySelector('[role="dialog"][data-state="open"]')
}
function previewImage(): HTMLImageElement {
  const image = document.body.querySelector<HTMLImageElement>('[data-duplicate-preview] img')
  if (!image) throw new Error('the preview has no image')
  return image
}

it('opens the preview from a virtual row', async () => {
  wrapper = mountView()
  await flushPromises()
  await wrapper.get('.preview-button').trigger('click')
  await flushPromises()
  expect(previewDialog()).not.toBeNull()
  expect(previewImage().alt).toBe('one.jpg')
})

it('closes the preview when its asset disappears from refreshed groups', async () => {
  wrapper = mountView()
  await flushPromises()
  await wrapper.get('.preview-button').trigger('click')
  await flushPromises()
  expect(previewDialog()).not.toBeNull()
  vi.mocked(engine.duplicateGroups).mockResolvedValue([
    { kind: DuplicateKind.DuplicateSimilar, items: [photo(2, 'C:\\b\\two.jpg')] },
  ])
  await useDuplicates().load()
  await flushPromises()
  expect(previewDialog()).toBeNull()
  expect(document.body.querySelector('[data-duplicate-preview] img')).toBeNull()
  expect(document.body.querySelector('[data-duplicate-preview]')?.textContent ?? '').not.toContain('one.jpg')
})

it('updates the open preview when its paths and thumbnail change', async () => {
  wrapper = mountView()
  await flushPromises()
  await wrapper.get('.preview-button').trigger('click')
  await flushPromises()
  vi.mocked(engine.duplicateGroups).mockResolvedValue([
    { kind: DuplicateKind.DuplicateSimilar, items: [{ ...photo(1, 'C:\\renamed.jpg'), thumb: 2 }] },
  ])
  await useDuplicates().load()
  await flushPromises()
  expect(previewDialog()).not.toBeNull()
  expect(previewImage().src).toContain('?v=2')
  expect(previewImage().alt).toBe('renamed.jpg')
  expect(previewDialog()?.textContent).toContain('C:\\renamed.jpg')
  expect(previewDialog()?.textContent).not.toContain('one.jpg')
})

it('does not show cached paths on remount after visibility changes, even if the refresh fails', async () => {
  wrapper = mountView()
  await flushPromises()
  expect(wrapper.text()).toContain('one.jpg')
  wrapper.unmount()
  let finish: ((value: Bridge.DuplicateGroup[]) => void) | undefined
  vi.mocked(engine.duplicateGroups)
    .mockReturnValueOnce(new Promise((resolve) => (finish = resolve)))
    .mockRejectedValue(new Error('read failed'))
  const invalidated = useDuplicates().invalidate()
  const rejected = expect(invalidated).rejects.toThrow('read failed')
  wrapper = mountView()
  await flushPromises()
  expect(wrapper.text()).not.toContain('one.jpg')
  if (!finish) throw new Error('the duplicate read did not start')
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
  wrapper = mountView()
  await flushPromises()
  const scroller = wrapper.get<HTMLElement>('[data-duplicates-scroll]')
  scroller.element.scrollTop = 120000
  await scroller.trigger('scroll')
  await flushPromises()
  const measured = wrapper.findAll('.virtual-row').map((row) => height(row.element))
  expect(new Set(measured).size).toBeGreaterThan(1)
  const total = wrapper.get<HTMLElement>('.virtual-list').element.style.height
  const positions = (): [string, string][] =>
    (wrapper?.findAll('.virtual-row') ?? []).map((row) => [
      row.attributes('data-index') ?? '',
      row.attributes('style') ?? '',
    ])
  const before = positions()
  expect(Number(wrapper.get('.virtual-row').attributes('data-index'))).toBeGreaterThan(400)
  vi.mocked(engine.duplicateGroups).mockResolvedValue(groups.map((group) => ({ ...group, items: [...group.items] })))
  await useDuplicates().load()
  await flushPromises()
  expect(scroller.element.scrollTop).toBe(120000)
  expect(wrapper.get<HTMLElement>('.virtual-list').element.style.height).toBe(total)
  // Every row still drawn sits exactly where it was. (How many rows the
  // virtualiser keeps past the edge can differ by one between two renders,
  // so the rows are compared by index rather than as one list.)
  const after = new Map(positions())
  const kept = before.filter(([index]) => after.has(index))
  expect(kept.length).toBeGreaterThan(5)
  expect(after.keys().next().value).toBe(before[0]?.[0])
  for (const [index, style] of kept) expect(after.get(index), `row ${index}`).toBe(style)
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
    vi.mocked(engine.trashAssets).mockReset().mockResolvedValue(undefined)
    vi.mocked(engine.deleteCopies).mockReset().mockResolvedValue({ recycled: 1, kept: [] })
  })

  async function mountWith(...groups: DuplicateGroup[]) {
    wrapper?.unmount()
    vi.mocked(engine.duplicateGroups).mockResolvedValue(groups)
    wrapper = mountView()
    await flushPromises()
  }
  const radios = () => wrapper?.findAll('[data-duplicate-group] [role="radio"]') ?? []
  const checked = () => radios().map((r) => r.attributes('aria-checked') === 'true')
  const reviewDialog = () => {
    const found = document.body.querySelector<HTMLElement>('[role="dialog"][data-state="open"]')
    if (!found) throw new Error('the review did not open')
    return new DOMWrapper(found)
  }
  const reviewText = () => document.body.querySelector('[data-duplicate-review]')?.textContent ?? ''
  const opened = () => document.body.querySelector('[data-duplicate-review]') !== null
  const review = async () => {
    await wrapper?.get('[data-review]').trigger('click')
    await flushPromises()
  }
  const confirm = async () => {
    await reviewDialog().get('[data-review-confirm]').trigger('click')
    await flushPromises()
  }

  it('pre-selects the copy the engine suggests in each similar group and marks it', async () => {
    await mountWith(similar(2))
    expect(checked()).toEqual([false, true, false])
    expect(wrapper?.findAll('[data-suggested]').map((e) => e.text())).toEqual(['Suggested'])
    expect(wrapper?.get('[data-slot="item"]:nth-of-type(2)').text()).toContain('Suggested')
  })

  it('pre-selects the suggested file of an exact copy', async () => {
    await mountWith(exact)
    expect(checked()).toEqual([false, true])
    expect(wrapper?.get('[data-slot="item"]:nth-of-type(2)').text()).toContain('Suggested')
  })

  it('lets the user pick another copy without changing anything', async () => {
    await mountWith(similar(2))
    await radios()[0]?.trigger('click')
    expect(wrapper?.findAll('[data-suggested]').map((e) => e.text())).toEqual(['Suggested']) // the suggestion stays marked
    await review()
    expect(reviewDialog().get('.keeps').text()).toBe('C:\\Photos\\p1.jpg')
    expect(reviewDialog().get('.removes').text()).toContain('p2.jpg')
    await mountWith(exact)
    await radios()[0]?.trigger('click')
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
    await reviewDialog().find('[data-review-cancel]').trigger('click') // Cancel
    expect(opened()).toBe(false)
    expect(engine.trashAssets).not.toHaveBeenCalled()
    expect(engine.deleteCopies).not.toHaveBeenCalled()
  })

  it('moves the similar photos that are not kept to the Trash once confirmed, and offers to undo it', async () => {
    await mountWith(similar(2))
    await radios()[0]?.trigger('click') // keep photo 1
    await review()
    await confirm()
    expect(engine.trashAssets).toHaveBeenCalledExactlyOnceWith([2, 3])
    expect(engine.deleteCopies).not.toHaveBeenCalled()
    expect(notifications.find(TRASH_UNDO_NOTIFICATION)?.title).toBe('Moved 2 items to the Trash.')
    notifications.find(TRASH_UNDO_NOTIFICATION)?.actions?.[0]?.run()
    await flushPromises()
    expect(engine.restoreAssets).toHaveBeenCalledExactlyOnceWith([2, 3])
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
    expect(engine.deleteCopies).toHaveBeenCalledExactlyOnceWith(9, 'C:\\Photos\\b\\IMG_9 copy.jpg', [
      'C:\\Photos\\a\\IMG_9.jpg',
    ])
    expect(engine.trashAssets).not.toHaveBeenCalled()
    expect(notifications.find(TRASH_UNDO_NOTIFICATION)).toBeUndefined() // recycled files are not in the Trash to restore
  })

  it('recycles the other files when the user keeps another file of an exact copy', async () => {
    await mountWith(exact)
    await radios()[0]?.trigger('click')
    await review()
    await confirm()
    expect(engine.deleteCopies).toHaveBeenCalledExactlyOnceWith(9, 'C:\\Photos\\a\\IMG_9.jpg', [
      'C:\\Photos\\b\\IMG_9 copy.jpg',
    ])
  })

  it('says how many files the Recycle Bin would not take', async () => {
    vi.mocked(engine.deleteCopies).mockResolvedValue({ recycled: 0, kept: ['C:\\Photos\\a\\IMG_9.jpg'] })
    await mountWith(exact)
    await review()
    await confirm()
    expect(notifications.find(KEPT_NOTIFICATION)?.title).toContain('1 file couldn’t be moved')
  })

  it('shows why a confirmed move failed and reloads the groups', async () => {
    vi.mocked(engine.trashAssets).mockRejectedValue(new Error('disk on fire'))
    await mountWith(similar(2))
    await review()
    await confirm()
    expect(wrapper?.get('[role="alert"]').text()).toContain('disk on fire')
    expect(notifications.find(TRASH_UNDO_NOTIFICATION)).toBeUndefined()
    expect(engine.duplicateGroups).toHaveBeenCalledTimes(2)
  })

  it('keeps the user’s choice across a reload while the group still exists', async () => {
    await mountWith(similar(2))
    await radios()[2]?.trigger('click')
    // A reload brings the same group with a new member in front and the suggestion elsewhere.
    vi.mocked(engine.duplicateGroups).mockResolvedValue([
      { kind: DuplicateKind.DuplicateSimilar, items: [photo(4, 'C:\\Photos\\p4.jpg'), ...similar(1).items] },
    ])
    await useDuplicates().load()
    await flushPromises()
    expect(checked()).toEqual([false, false, false, true])
    // The same for an exact copy's file.
    await mountWith(exact)
    await radios()[0]?.trigger('click')
    vi.mocked(engine.duplicateGroups).mockResolvedValue([{ ...exact, items: exact.items.map((it) => ({ ...it })) }])
    await useDuplicates().load()
    await flushPromises()
    expect(checked()).toEqual([true, false])
  })

  it('forgets a choice once its photo has left the groups', async () => {
    await mountWith(similar(2))
    await radios()[2]?.trigger('click')
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
    expect(wrapper?.get<HTMLButtonElement>('[data-review]').element.disabled).toBe(true)
  })

  it('closes an open review when a reload no longer holds one of its photos', async () => {
    await mountWith(similar(2))
    await review()
    expect(opened()).toBe(true)
    expect(reviewDialog().text()).toContain('p3.jpg')
    // Photo 3 was blocked meanwhile: the group reloads without it.
    vi.mocked(engine.duplicateGroups).mockResolvedValue([{ ...similar(2), items: similar(2).items.slice(0, 2) }])
    await useDuplicates().load()
    await flushPromises()
    expect(opened()).toBe(false)
    expect(reviewText()).not.toContain('p3.jpg')
    expect(reviewText()).toBe('')
    expect(engine.trashAssets).not.toHaveBeenCalled()
  })

  it('closes an open review when its whole group is gone from the reload', async () => {
    await mountWith(exact)
    await review()
    expect(reviewDialog().text()).toContain('IMG_9')
    vi.mocked(engine.duplicateGroups).mockResolvedValue([])
    await useDuplicates().load()
    await flushPromises()
    expect(opened()).toBe(false)
    expect(reviewText()).toBe('')
  })

  it('closes an open review when a copy it did not list joins the group', async () => {
    await mountWith(exact)
    await review()
    const joined = exact.items.map((it) => ({
      ...it,
      paths: [...it.paths, 'C:\\Photos\\c\\IMG_9.jpg'],
      mtimes: [200, 100, 50],
    }))
    vi.mocked(engine.duplicateGroups).mockResolvedValue([{ ...exact, items: joined }])
    await useDuplicates().load()
    await flushPromises()
    expect(opened()).toBe(false)
  })

  it('keeps an open review when a reload only brings a new thumbnail', async () => {
    await mountWith(similar(2))
    await review()
    vi.mocked(engine.duplicateGroups).mockResolvedValue([
      { ...similar(2), items: similar(2).items.map((it) => ({ ...it, thumb: 2 })) },
    ])
    await useDuplicates().load()
    await flushPromises()
    expect(opened()).toBe(true)
    expect(reviewDialog().text()).toContain('p3.jpg')
  })

  it('disables Review and the confirmation while a confirmed move is under way, and acts once', async () => {
    let finish: ((value: { recycled: number; kept: string[] }) => void) | undefined
    vi.mocked(engine.deleteCopies).mockReturnValue(new Promise((resolve) => (finish = resolve)))
    await mountWith(exact)
    await review()
    await confirm()
    expect(wrapper?.get<HTMLButtonElement>('[data-review]').element.disabled).toBe(true)
    expect(reviewDialog().get<HTMLButtonElement>('[data-review-confirm]').element.disabled).toBe(true)
    await reviewDialog().get('[data-review-confirm]').trigger('click')
    await reviewDialog().find('[data-review-cancel]').trigger('click') // Cancel does not close it either
    expect(opened()).toBe(true)
    expect(engine.deleteCopies).toHaveBeenCalledTimes(1)
    if (!finish) throw new Error('the file operation did not start')
    finish({ recycled: 1, kept: [] })
    await flushPromises()
    expect(opened()).toBe(false)
    expect(wrapper?.get<HTMLButtonElement>('[data-review]').element.disabled).toBe(false)
    expect(engine.deleteCopies).toHaveBeenCalledTimes(1)
  })

  it('keeps the user’s choice when an unrelated block change refreshes the list', async () => {
    await mountWith(similar(2))
    await radios()[0]?.trigger('click')
    await showBlocksChanged()
    await flushPromises()
    await review()
    expect(reviewDialog().get('.keeps').text()).toBe('C:\\Photos\\p1.jpg')
  })

  it('still forgets the choice when the refresh after a block shows its photo is gone', async () => {
    await mountWith(similar(2))
    await radios()[0]?.trigger('click')
    vi.mocked(engine.duplicateGroups).mockResolvedValue([{ ...similar(2), items: similar(2).items.slice(1) }])
    await showBlocksChanged()
    await flushPromises()
    // Photo 1 is back later (unblocked): the suggestion applies, not the old choice.
    vi.mocked(engine.duplicateGroups).mockResolvedValue([similar(2)])
    await useDuplicates().load()
    await flushPromises()
    expect(checked()).toEqual([false, true, false])
  })

  it('shows the keep choice and removal count together before opening confirmation', async () => {
    await mountWith(similar(2))
    const summary = wrapper?.get('[data-duplicate-group] [role="toolbar"]')
    expect(summary?.text()).toContain('Keep: p2.jpg')
    expect(summary?.text()).toContain('2 other photos will go to the Trash after confirmation.')
    await radios()[0]?.trigger('click')
    expect(summary?.text()).toContain('Keep: p1.jpg')
    expect(engine.trashAssets).not.toHaveBeenCalled()
    await mountWith(exact)
    expect(wrapper?.get('[data-duplicate-group] [role="toolbar"]').text()).toContain(
      '1 extra file will go to the Recycle Bin after confirmation.',
    )
    expect(engine.deleteCopies).not.toHaveBeenCalled()
  })

  it('returns focus to the Review trigger after cancelling the modal', async () => {
    await mountWith(similar(2))
    const trigger = wrapper?.get<HTMLButtonElement>('[data-review]').element
    if (!trigger) throw new Error('the group has no Review action')
    trigger.focus()
    await review()
    await reviewDialog().get('[data-review-cancel]').trigger('click')
    await flushPromises()
    expect(opened()).toBe(false)
    expect(document.activeElement).toBe(trigger)
    expect(engine.trashAssets).not.toHaveBeenCalled()
    expect(engine.deleteCopies).not.toHaveBeenCalled()
  })

  it('uses the same confirmation when a group is reviewed from the command registry', async () => {
    await mountWith(similar(2))
    const command = commands.value.find((entry) => entry.id.startsWith('duplicates.review.'))
    if (!command) throw new Error('the group did not register its Review action')
    expect(await runCommand(command.id)).toBe(true)
    await flushPromises()
    expect(opened()).toBe(true)
    expect(reviewDialog().get('.keeps').text()).toBe('C:\\Photos\\p2.jpg')
    expect(engine.trashAssets).not.toHaveBeenCalled()
    await confirm()
    expect(engine.trashAssets).toHaveBeenCalledExactlyOnceWith([1, 3])
    wrapper?.unmount()
    wrapper = undefined
    expect(commands.value.some((entry) => entry.id === command.id)).toBe(false)
  })
})

it('pauses and resumes the queue without losing its position or keeper choices', async () => {
  vi.mocked(engine.duplicateGroups).mockResolvedValue(
    [1, 3].map((id) => ({
      kind: DuplicateKind.DuplicateSimilar,
      items: [photo(id, `C:/p${id}.jpg`), photo(id + 1, `C:/p${id + 1}.jpg`)],
    })),
  )
  wrapper = mountView()
  await flushPromises()
  await wrapper.get('[data-review-all]').trigger('click')
  await flushPromises()
  expect(wrapper.text()).toContain('Group 1 of 2')
  await wrapper.get('[data-duplicate-group] [role="radio"]').trigger('click')
  await wrapper.get('[data-queue-skip]').trigger('click')
  await flushPromises()
  expect(wrapper.text()).toContain('Group 2 of 2')
  await wrapper.get('[data-queue-later]').trigger('click')
  expect(wrapper.get('[data-review-all]').text()).toBe('Resume review')
  await wrapper.get('[data-review-all]').trigger('click')
  await flushPromises()
  expect(wrapper.text()).toContain('Group 2 of 2')
  expect(wrapper.text()).toContain('0 reviewed · 1 skipped')
  await wrapper.get('[data-queue-skip]').trigger('click')
  await flushPromises()
  expect(wrapper.text()).toContain('Review complete')
  expect(wrapper.text()).toContain('0 reviewed · 2 skipped')
})

it('shows exact and similar groups in separate tabs and reports their live counts', async () => {
  vi.mocked(engine.duplicateGroups).mockResolvedValue([
    {
      kind: DuplicateKind.DuplicateExact,
      items: [{ ...photo(1, 'C:/exact.jpg'), paths: ['C:/exact.jpg', 'D:/exact.jpg'] }],
    },
    { kind: DuplicateKind.DuplicateSimilar, items: [photo(2, 'C:/similar.jpg'), photo(3, 'C:/other.jpg')] },
  ])
  wrapper = mountView()
  await flushPromises()
  expect(wrapper.get('[role="radio"]').text()).toBe('Exact copies 1')
  expect(wrapper.findAll('[role="radio"]')[1]?.text()).toBe('Similar photos 1')
  expect(wrapper.text()).toContain('exact.jpg')
  expect(wrapper.text()).not.toContain('similar.jpg')
  await wrapper.get('[aria-label="Duplicate type"] button:nth-of-type(2)').trigger('click')
  await flushPromises()
  expect(wrapper.text()).toContain('similar.jpg')
  expect(wrapper.text()).not.toContain('exact.jpg')
})

it('browses further virtual groups from Show more groups', async () => {
  vi.mocked(engine.duplicateGroups).mockResolvedValue(
    Array.from({ length: 100 }, (_, i) => ({
      kind: DuplicateKind.DuplicateSimilar,
      items: [photo(i * 2 + 1, `C:/p${i * 2 + 1}.jpg`), photo(i * 2 + 2, `C:/p${i * 2 + 2}.jpg`)],
    })),
  )
  wrapper = mountView()
  await flushPromises()
  const scroller = wrapper.get<HTMLElement>('[data-duplicates-scroll]').element
  const scroll = vi.fn<(options: ScrollToOptions) => void>((options) => {
    scroller.scrollTop = options.top ?? 0
    scroller.dispatchEvent(new Event('scroll'))
  })
  Object.defineProperty(scroller, 'scrollTo', { value: scroll })
  Object.defineProperty(scroller, 'scrollHeight', { value: 30000 })
  await wrapper.get('[data-duplicates-more]').trigger('click')
  await flushPromises()
  expect(scroller.scrollTop).toBeGreaterThan(0)
  expect(scroll).toHaveBeenCalled()
  expect(wrapper.findAll('[data-duplicate-group]').length).toBeLessThan(15)
})

it('offers to check again once, from the empty state, when nothing is left to review', async () => {
  vi.mocked(engine.duplicateGroups).mockResolvedValue([])
  wrapper = mountView()
  await flushPromises()
  const checks = wrapper.findAll('button').filter((button) => button.text() === t('duplicates.refresh'))
  expect(checks).toHaveLength(1)
  expect(wrapper.get('header').text()).not.toContain(t('duplicates.refresh'))
})
