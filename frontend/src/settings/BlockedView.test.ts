import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import {
  BLOCK_UNLOCK_IDLE_MINUTES,
  BlockKind,
  BlockMode,
  DateSource,
  engine,
  ErrorCode,
  Kind,
  Playback,
} from '../bridge'
import type * as Bridge from '../bridge'
import type { AssetDetail, CoveredItem } from '../bridge'
import type * as Block from '../shared/block'
import { confirm } from '../shared/confirm'
import { notice } from '../shared/errors'
import { t } from '../shared/i18n'
import BlockedView from './BlockedView.vue'

vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  engine: {
    blockPINConfigured: vi.fn(),
    unlockBlocks: vi.fn(),
    lockBlocks: vi.fn(),
    blocks: vi.fn(),
    setBlockMode: vi.fn(),
    removeBlock: vi.fn(),
    resetBlockPIN: vi.fn(),
    blockedItems: vi.fn(),
    trashCovered: vi.fn(),
    asset: vi.fn(),
  },
  videosAvailable: vi.fn(() => true),
}))
vi.mock('../shared/block', async (importOriginal) => ({
  ...(await importOriginal<typeof Block>()),
  showBlocksChanged: vi.fn().mockResolvedValue(undefined),
}))
vi.mock('../shared/confirm', () => ({ confirm: vi.fn() }))

const privateBlock = {
  id: 1,
  kind: BlockKind.BlockPerson,
  mode: BlockMode.BlockPhotos,
  targetId: 1,
  path: '',
  name: 'Private name',
}
const idle = BLOCK_UNLOCK_IDLE_MINUTES * 60 * 1000

async function opened() {
  wrapper = mount(BlockedView, { attachTo: document.body, global: { stubs: { RouterLink: true } } })
  await flushPromises()
  await wrapper.get('input').setValue('123456')
  await wrapper.get('form').trigger('submit')
  await flushPromises()
  return wrapper
}

let wrapper: ReturnType<typeof mount> | undefined

beforeEach(() => {
  setActivePinia(createPinia())
  vi.mocked(engine.blockPINConfigured).mockReset().mockResolvedValue(true)
  vi.mocked(engine.unlockBlocks).mockReset().mockResolvedValue(undefined)
  vi.mocked(engine.lockBlocks).mockReset().mockResolvedValue(undefined)
  vi.mocked(engine.blocks).mockReset().mockResolvedValue([])
  vi.mocked(engine.setBlockMode).mockReset().mockResolvedValue(undefined)
  vi.mocked(engine.removeBlock).mockReset().mockResolvedValue(undefined)
  vi.mocked(engine.resetBlockPIN).mockReset().mockResolvedValue(undefined)
  vi.mocked(engine.blockedItems).mockReset().mockResolvedValue([])
  vi.mocked(engine.trashCovered).mockReset().mockResolvedValue(undefined)
  vi.mocked(engine.asset).mockReset().mockResolvedValue(photoDetail(1))
  vi.mocked(confirm).mockReset().mockResolvedValue(true)
  notice.value = ''
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  vi.useRealTimers()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

it('clears private names and focuses the PIN form on idle expiry', async () => {
  vi.useFakeTimers()
  vi.mocked(engine.blocks).mockResolvedValue([privateBlock])
  const view = await opened()
  expect(view.text()).toContain('Private name')
  await vi.advanceTimersByTimeAsync(idle)
  expect(view.text()).not.toContain('Private name')
  expect(view.find('form').exists()).toBe(true)
  expect(document.activeElement).toBe(view.get('input').element)
})

it.each(['mode', 'unblock'] as const)('extends the idle deadline after a successful %s action', async (action) => {
  vi.useFakeTimers()
  vi.mocked(engine.blocks).mockResolvedValue([privateBlock])
  const view = await opened()
  await vi.advanceTimersByTimeAsync(idle - 1000)
  await view.get(action === 'mode' ? '.list button:first-of-type' : '.list button:last-of-type').trigger('click')
  await flushPromises()
  await vi.advanceTimersByTimeAsync(1000)
  expect(view.find('form').exists()).toBe(false)
  await vi.advanceTimersByTimeAsync(idle - 1000)
  expect(view.find('form').exists()).toBe(true)
})

it.each(['mode', 'unblock', 'reload'] as const)(
  'relocks and clears the list when %s reports blocks_locked',
  async (action) => {
    vi.useFakeTimers()
    vi.mocked(engine.blocks).mockResolvedValue([privateBlock])
    const view = await opened()
    const locked = new Error('locked', { cause: { code: ErrorCode.BlocksLocked } })
    if (action === 'mode') vi.mocked(engine.setBlockMode).mockRejectedValueOnce(locked)
    else if (action === 'unblock') vi.mocked(engine.removeBlock).mockRejectedValueOnce(locked)
    else vi.mocked(engine.blocks).mockRejectedValueOnce(locked)
    await view.get(action === 'unblock' ? '.list button:last-of-type' : '.list button:first-of-type').trigger('click')
    await flushPromises()
    expect(view.text()).not.toContain('Private name')
    expect(view.find('form').exists()).toBe(true)
    expect(document.activeElement).toBe(view.get('input').element)
  },
)

it('does not reinstall a private list response that finishes after expiry', async () => {
  vi.useFakeTimers()
  vi.mocked(engine.blocks).mockResolvedValue([privateBlock])
  const view = await opened()
  let finish!: (value: (typeof privateBlock)[]) => void
  vi.mocked(engine.blocks).mockReturnValueOnce(new Promise((resolve) => (finish = resolve)))
  await view.get('.list button:first-of-type').trigger('click')
  await flushPromises()
  await vi.advanceTimersByTimeAsync(idle)
  finish([privateBlock])
  await flushPromises()
  expect(view.find('form').exists()).toBe(true)
  expect(view.text()).not.toContain('Private name')
})

it('disposes the idle timer on unmount', async () => {
  vi.useFakeTimers()
  await opened()
  expect(vi.getTimerCount()).toBe(1)
  wrapper?.unmount()
  wrapper = undefined
  expect(vi.getTimerCount()).toBe(0)
})

it('locks once on leaving and does not let a delayed unlock lock a newer screen', async () => {
  let resolve!: () => void
  vi.mocked(engine.unlockBlocks).mockReturnValueOnce(new Promise<void>((done) => (resolve = done)))
  wrapper = mount(BlockedView, { global: { stubs: { RouterLink: true } } })
  await flushPromises()
  await wrapper.get('input').setValue('123456')
  await wrapper.get('form').trigger('submit')
  wrapper.unmount()
  wrapper = undefined
  expect(engine.lockBlocks).toHaveBeenCalledTimes(1)
  resolve()
  await flushPromises()
  expect(engine.blocks).not.toHaveBeenCalled()
  expect(engine.lockBlocks).toHaveBeenCalledTimes(1)
})

it('shows the private list only after unlock and relocks on leaving', async () => {
  wrapper = mount(BlockedView, { global: { stubs: { RouterLink: true } } })
  await flushPromises()
  expect(engine.blocks).not.toHaveBeenCalled()
  await wrapper.get('input').setValue('123456')
  await wrapper.get('form').trigger('submit')
  await flushPromises()
  expect(engine.unlockBlocks).toHaveBeenCalledWith('123456')
  expect(engine.blocks).toHaveBeenCalledTimes(1)
  expect(wrapper.find('form').exists()).toBe(false)
  wrapper.unmount()
  wrapper = undefined
  expect(engine.lockBlocks).toHaveBeenCalledTimes(1)
})

async function forgotten() {
  wrapper = mount(BlockedView, { attachTo: document.body, global: { stubs: { RouterLink: true } } })
  await flushPromises()
  await wrapper.get('button[type="button"]').trigger('click')
  return wrapper
}

async function chooseNewPIN(view: ReturnType<typeof mount>, first: string, second: string) {
  const [pin, confirmation] = view.findAll('input')
  await pin?.setValue(first)
  await confirmation?.setValue(second)
  await view.get('form').trigger('submit')
  await flushPromises()
}

it('resets a forgotten PIN after the Windows sign-in and asks for the new PIN to unlock', async () => {
  const view = await forgotten()
  expect(view.text()).toContain(t('blocked.resetHint'))
  await chooseNewPIN(view, '654321', '654321')
  expect(engine.resetBlockPIN).toHaveBeenCalledExactlyOnceWith('654321')
  expect(view.text()).toContain(t('blocked.pinChanged'))
  expect(view.find('.page-error').exists()).toBe(false)
  // Still locked: the list is not opened by the reset, and the form asks for the PIN.
  expect(engine.unlockBlocks).not.toHaveBeenCalled()
  expect(engine.blocks).not.toHaveBeenCalled()
  expect(view.findAll('input')).toHaveLength(1)
  expect(view.get('input').element).toHaveProperty('value', '')
  await view.get('input').setValue('654321')
  await view.get('form').trigger('submit')
  await flushPromises()
  expect(engine.unlockBlocks).toHaveBeenCalledExactlyOnceWith('654321')
  expect(view.text()).not.toContain(t('blocked.pinChanged'))
})

it('makes no call when the two new PINs differ', async () => {
  const view = await forgotten()
  await chooseNewPIN(view, '654321', '654322')
  expect(engine.resetBlockPIN).not.toHaveBeenCalled()
  expect(view.get('.page-error').text()).toBe(t('blocked.pinMismatch'))
  expect(view.text()).not.toContain(t('blocked.pinChanged'))
})

it.each([
  [ErrorCode.SigninFailed, 'error.signinFailed'],
  [ErrorCode.SigninUnavailable, 'error.signinUnavailable'],
  [ErrorCode.InvalidPIN, 'error.invalidPIN'],
] as const)('explains a reset that fails with %s and keeps the form for another try', async (code, key) => {
  vi.mocked(engine.resetBlockPIN).mockRejectedValueOnce(new Error('refused', { cause: { code } }))
  const view = await forgotten()
  await chooseNewPIN(view, '654321', '654321')
  expect(view.get('.page-error').text()).toBe(t(key, { pinMin: 6, pinMax: 12 }))
  expect(view.text()).not.toContain(t('blocked.pinChanged'))
  expect(view.findAll('input')).toHaveLength(2)
  await view.get('form').trigger('submit')
  await flushPromises()
  expect(engine.resetBlockPIN).toHaveBeenCalledTimes(2)
  expect(view.find('.page-error').exists()).toBe(false)
  expect(view.text()).toContain(t('blocked.pinChanged'))
})

it('goes back to the PIN form from the reset form without calling the engine', async () => {
  const view = await forgotten()
  await view.get('input').setValue('654321')
  const buttons = view.findAll('button[type="button"]')
  await buttons[0]?.trigger('click')
  expect(view.findAll('input')).toHaveLength(1)
  expect(view.get('input').element).toHaveProperty('value', '')
  expect(engine.resetBlockPIN).not.toHaveBeenCalled()
})

it('offers no reset while there is no PIN to reset', async () => {
  vi.mocked(engine.blockPINConfigured).mockResolvedValue(false)
  wrapper = mount(BlockedView, { global: { stubs: { RouterLink: true } } })
  await flushPromises()
  expect(wrapper.text()).not.toContain(t('blocked.forgotPIN'))
})

// --- The browser of covered photos ----------------------------------------

const photoDetail = (id: number): AssetDetail => ({
  id,
  kind: Kind.KindPhoto,
  takenAt: 0,
  dateFrom: DateSource.DateFromFile,
  width: 640,
  height: 480,
  camera: '',
  lat: null,
  lon: null,
  placeId: 0,
  favourite: false,
  paths: [],
  size: 100,
  ext: '.jpg',
  webNative: true,
  video: null,
})

const covered = (id: number, more: Partial<CoveredItem> = {}): CoveredItem => ({
  id,
  w: 640,
  h: 480,
  t: 1,
  hiddenBy: [],
  ...more,
})

const manyCovered = (n: number) => Array.from({ length: n }, (_, i) => covered(i + 1))

// jsdom has no layout: give the grid a 900 x 600 viewport, which the
// virtualiser reads through ResizeObserver and the element's size.
class FixedResizeObserver implements ResizeObserver {
  constructor(private readonly callback: ResizeObserverCallback) {}
  observe(target: Element) {
    const size: ResizeObserverSize = { inlineSize: 900, blockSize: 600 }
    this.callback(
      [
        {
          target,
          contentRect: new DOMRect(0, 0, 900, 600),
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

function giveTheGridASize() {
  vi.stubGlobal('ResizeObserver', FixedResizeObserver)
  vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockReturnValue(900)
  vi.spyOn(HTMLElement.prototype, 'offsetWidth', 'get').mockReturnValue(900)
  vi.spyOn(HTMLElement.prototype, 'offsetHeight', 'get').mockReturnValue(600)
}

/** Unlocks the screen, with the blocks given, and opens the browser on the first. */
async function browsing(
  items: CoveredItem[],
  blocks = [privateBlock],
  stubs: Record<string, boolean> = { teleport: true },
) {
  giveTheGridASize()
  vi.mocked(engine.blocks).mockResolvedValue(blocks)
  vi.mocked(engine.blockedItems).mockResolvedValue(items)
  wrapper = mount(BlockedView, { attachTo: document.body, global: { stubs: { RouterLink: true, ...stubs } } })
  await flushPromises()
  await wrapper.get('input').setValue('123456')
  await wrapper.get('form').trigger('submit')
  await flushPromises()
  await showPhotos(0)
  return wrapper
}

async function showPhotos(block: number) {
  const buttons = wrapper?.findAll('.list button').filter((b) => b.text() === t('covered.show'))
  await buttons?.[block]?.trigger('click')
  await flushPromises()
}

const cells = () => wrapper?.findAll('.cell') ?? []

it('offers the covered photos only once the PIN has unlocked the screen', async () => {
  wrapper = mount(BlockedView, { global: { stubs: { RouterLink: true } } })
  await flushPromises()
  expect(wrapper.text()).not.toContain(t('covered.show'))
  expect(engine.blockedItems).not.toHaveBeenCalled()
  vi.mocked(engine.blocks).mockResolvedValue([privateBlock])
  await wrapper.get('input').setValue('123456')
  await wrapper.get('form').trigger('submit')
  await flushPromises()
  expect(wrapper.text()).toContain(t('covered.show'))
  expect(engine.blockedItems).not.toHaveBeenCalled() // nothing is read until a block is opened
})

it('lists the photos of the block that Show photos opens', async () => {
  const view = await browsing([covered(1), covered(2)])
  expect(engine.blockedItems).toHaveBeenCalledExactlyOnceWith(privateBlock.id)
  expect(view.text()).toContain(t('covered.title', { name: 'Private name' }))
  expect(view.text()).toContain(t('covered.count', { n: 2 }))
  expect(cells().map((c) => c.find('img').attributes('src'))).toEqual([
    '/media/thumb/1/256?v=1',
    '/media/thumb/2/256?v=1',
  ])
  expect(view.find('.list').exists()).toBe(false)
})

it('draws only the photos on screen, however many a block covers', async () => {
  await browsing(manyCovered(5000))
  expect(cells().length).toBeGreaterThan(0)
  expect(cells().length).toBeLessThan(100)
})

it('says which other blocks also hide a photo, and nothing under the others', async () => {
  const view = await browsing([
    covered(1, {
      hiddenBy: [
        { id: 2, name: 'Trip' },
        { id: 3, name: 'Ali' },
      ],
    }),
    covered(2),
  ])
  const also = view.findAll('.also')
  expect(also).toHaveLength(1)
  expect(also[0]?.text()).toBe(t('covered.hiddenBy', { names: 'Trip and Ali' }))
  expect(view.findAll('.entry')[0]?.text()).toContain('Trip and Ali')
  expect(view.findAll('.entry')[1]?.find('.also').exists()).toBe(false)
})

it('says that a label-only block leaves its photos shown in the library', async () => {
  const view = await browsing([covered(1)], [{ ...privateBlock, mode: BlockMode.BlockLabel }])
  expect(view.text()).toContain(t('covered.stillShown'))
})

it('says so when a block covers nothing', async () => {
  const view = await browsing([])
  expect(view.text()).toContain(t('covered.empty'))
})

it('goes back to the list and forgets the photos', async () => {
  const view = await browsing([covered(1)])
  await view.get('.head button').trigger('click')
  expect(view.find('.list').exists()).toBe(true)
  expect(cells()).toHaveLength(0)
  expect(engine.lockBlocks).not.toHaveBeenCalled() // the screen is still open
})

it('opens a photo in the Viewer, which moves it to the Trash and on to the next photo', async () => {
  const view = await browsing([covered(1), covered(2), covered(3)])
  await cells()[1]?.trigger('click')
  await flushPromises()
  expect(view.find('.viewer').exists()).toBe(true)
  expect(engine.asset).toHaveBeenLastCalledWith(2)
  await view.get('.viewer button[aria-label="Move to Trash"]').trigger('click')
  await flushPromises()
  expect(engine.trashCovered).toHaveBeenCalledExactlyOnceWith([2])
  expect(engine.asset).toHaveBeenLastCalledWith(3)
  expect(cells()).toHaveLength(2)
  await view.get('.viewer .prev').trigger('click')
  await flushPromises()
  expect(engine.asset).toHaveBeenLastCalledWith(1)
})

it('offers nothing but Move to Trash in the Viewer of a covered photo', async () => {
  const view = await browsing([covered(1)])
  await cells()[0]?.trigger('click')
  await flushPromises()
  const labels = view.findAll('.viewer .bar button').map((b) => b.text() || b.attributes('aria-label'))
  expect(labels).toEqual([t('common.close'), t('trash.move')])
})

it('plays a covered video in the Viewer', async () => {
  vi.mocked(engine.asset).mockResolvedValue({
    ...photoDetail(7),
    kind: Kind.KindVideo,
    ext: '.mp4',
    webNative: false,
    video: { duration: 60000, codec: 'h264', audio: 'aac', play: Playback.PlayDirect },
  })
  const view = await browsing([covered(7, { v: true, d: 60000 })])
  await cells()[0]?.trigger('click')
  await flushPromises()
  expect(view.get('.viewer video').attributes('src')).toContain('/video/7')
})

it('closes the browser and the Viewer, and forgets the photos, when the engine reports the screen locked', async () => {
  const view = await browsing([covered(1, { hiddenBy: [{ id: 2, name: 'Trip' }] }), covered(2)])
  await cells()[0]?.trigger('click')
  await flushPromises()
  vi.mocked(engine.trashCovered).mockRejectedValueOnce(new Error('locked', { cause: { code: ErrorCode.BlocksLocked } }))
  await view.get('.viewer button[aria-label="Move to Trash"]').trigger('click')
  await flushPromises()
  expect(view.find('.viewer').exists()).toBe(false)
  expect(cells()).toHaveLength(0)
  expect(view.text()).not.toContain('Trip')
  expect(view.text()).not.toContain('Private name')
  expect(view.find('form').exists()).toBe(true)
})

it('relocks when the photos cannot be read because the screen is locked', async () => {
  giveTheGridASize()
  vi.mocked(engine.blocks).mockResolvedValue([privateBlock])
  vi.mocked(engine.blockedItems).mockRejectedValue(new Error('locked', { cause: { code: ErrorCode.BlocksLocked } }))
  wrapper = mount(BlockedView, { attachTo: document.body, global: { stubs: { RouterLink: true } } })
  await flushPromises()
  await wrapper.get('input').setValue('123456')
  await wrapper.get('form').trigger('submit')
  await flushPromises()
  await showPhotos(0)
  expect(wrapper.find('form').exists()).toBe(true)
  expect(wrapper.find('.covered').exists()).toBe(false)
})

it('keeps the browser and the photo when the Trash refuses, and says why', async () => {
  const view = await browsing([covered(1), covered(2)])
  await cells()[0]?.trigger('click')
  await flushPromises()
  vi.mocked(engine.trashCovered).mockRejectedValueOnce(new Error('disk on fire'))
  await view.get('.viewer button[aria-label="Move to Trash"]').trigger('click')
  await flushPromises()
  expect(view.find('.viewer').exists()).toBe(true)
  expect(cells()).toHaveLength(2)
  expect(notice.value).toContain('disk on fire')
})

it('closes the browser and the Viewer when the screen relocks from being left idle', async () => {
  vi.useFakeTimers()
  const view = await browsing([covered(1, { hiddenBy: [{ id: 2, name: 'Trip' }] })])
  await cells()[0]?.trigger('click')
  await flushPromises()
  await vi.advanceTimersByTimeAsync(idle)
  expect(view.find('.viewer').exists()).toBe(false)
  expect(cells()).toHaveLength(0)
  expect(view.text()).not.toContain('Trip')
  expect(view.find('form').exists()).toBe(true)
})

it.each([
  [BlockKind.BlockEvent, BlockMode.BlockPhotos, BlockMode.BlockLabel],
  [BlockKind.BlockFace, BlockMode.BlockLabel, BlockMode.BlockPhotos],
  [BlockKind.BlockPerson, BlockMode.BlockPhotos, BlockMode.BlockLabel],
])('switches the mode of a %s block by the block’s own id, not its target', async (kind, mode, next) => {
  // For events and faces the target is a row of its own, which SetBlock would read as something else.
  vi.mocked(engine.blocks).mockResolvedValue([{ ...privateBlock, id: 41, kind, mode, targetId: 7 }])
  const view = await opened()
  await view.get('.list button:first-of-type').trigger('click')
  await flushPromises()
  expect(engine.setBlockMode).toHaveBeenCalledExactlyOnceWith(41, next)
})

it('names event and face blocks by their kind', async () => {
  vi.mocked(engine.blocks).mockResolvedValue([
    { ...privateBlock, id: 1, kind: BlockKind.BlockEvent, name: 'Birthday' },
    { ...privateBlock, id: 2, kind: BlockKind.BlockFace, name: 'A face' },
  ])
  const view = await opened()
  const kinds = view.findAll('.item-name small').map((el) => el.text())
  expect(kinds[0]).toContain(t('nav.events'))
  expect(kinds[1]).toContain(t('blocked.kindFace'))
})

it('does not bring the browser back when the screen is unlocked again after a relock', async () => {
  vi.useFakeTimers()
  const view = await browsing([covered(1, { hiddenBy: [{ id: 2, name: 'Trip' }] })])
  await vi.advanceTimersByTimeAsync(idle)
  vi.mocked(engine.blockedItems).mockClear()
  await view.get('input').setValue('123456')
  await view.get('form').trigger('submit')
  await flushPromises()
  expect(view.find('.list').exists()).toBe(true)
  expect(view.find('.covered').exists()).toBe(false)
  expect(cells()).toHaveLength(0)
  expect(view.text()).not.toContain('Trip')
  expect(engine.blockedItems).not.toHaveBeenCalled()
})

it('counts browsing as use, so a photo being looked at does not relock', async () => {
  vi.useFakeTimers()
  const view = await browsing([covered(1), covered(2)])
  await vi.advanceTimersByTimeAsync(idle - 1000)
  await cells()[0]?.trigger('click') // opening a photo is a use
  await flushPromises()
  await vi.advanceTimersByTimeAsync(idle - 1000)
  expect(view.find('.viewer').exists()).toBe(true)
  await vi.advanceTimersByTimeAsync(1000)
  expect(view.find('.viewer').exists()).toBe(false)
})

it('closes the Viewer and locks the engine on leaving the screen', async () => {
  // The Viewer teleports to the body, so it is not part of the wrapper.
  await browsing([covered(1)], [privateBlock], {})
  await cells()[0]?.trigger('click')
  await flushPromises()
  expect(document.querySelector('.viewer')).not.toBeNull()
  wrapper?.unmount()
  wrapper = undefined
  expect(document.querySelector('.viewer')).toBeNull()
  expect(engine.lockBlocks).toHaveBeenCalledTimes(1)
})

it('locks the engine as well when the screen relocks from being left idle', async () => {
  vi.useFakeTimers()
  await browsing([covered(1)])
  expect(engine.lockBlocks).not.toHaveBeenCalled()
  await vi.advanceTimersByTimeAsync(idle)
  expect(engine.lockBlocks).toHaveBeenCalledTimes(1)
})

it('counts scrolling the covered photos as use of the screen', async () => {
  vi.useFakeTimers()
  const view = await browsing([covered(1), covered(2)])
  for (let i = 0; i < 3; i++) {
    await vi.advanceTimersByTimeAsync(idle - 1000)
    await view.get('.scroller').trigger('scroll')
  }
  expect(view.find('.covered').exists()).toBe(true)
  expect(engine.lockBlocks).not.toHaveBeenCalled()
  await vi.advanceTimersByTimeAsync(idle)
  expect(view.find('.covered').exists()).toBe(false)
  expect(engine.lockBlocks).toHaveBeenCalledTimes(1)
})

it('counts a covered video playing as use of the screen', async () => {
  vi.useFakeTimers()
  vi.mocked(engine.asset).mockResolvedValue({
    ...photoDetail(7),
    kind: Kind.KindVideo,
    ext: '.mp4',
    webNative: false,
    video: { duration: 60000, codec: 'h264', audio: 'aac', play: Playback.PlayDirect },
  })
  const view = await browsing([covered(7, { v: true, d: 60000 })])
  await cells()[0]?.trigger('click')
  await flushPromises()
  for (let i = 0; i < 3; i++) {
    await vi.advanceTimersByTimeAsync(idle - 1000)
    await view.get('.viewer video').trigger('timeupdate')
  }
  expect(view.find('.viewer').exists()).toBe(true)
  await vi.advanceTimersByTimeAsync(idle)
  expect(view.find('.viewer').exists()).toBe(false)
})

it('asks before moving a covered photo to the Trash, and moves nothing if the user declines', async () => {
  const view = await browsing([covered(1), covered(2)])
  await cells()[0]?.trigger('click')
  await flushPromises()
  vi.mocked(confirm).mockResolvedValue(false)
  await view.get('.viewer button[aria-label="Move to Trash"]').trigger('click')
  await flushPromises()
  expect(confirm).toHaveBeenCalledExactlyOnceWith(t('covered.trashTitle'), t('covered.trashText'), t('trash.move'))
  expect(engine.trashCovered).not.toHaveBeenCalled()
  expect(cells()).toHaveLength(2)
})
