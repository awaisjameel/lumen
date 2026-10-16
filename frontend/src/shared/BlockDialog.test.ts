import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { DOMWrapper, flushPromises } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import { mountInApp, recordNotifications } from './testing'
import type { RecordedNotifications } from './testing'
import { t } from './i18n'
import { BlockKind, BlockMode, engine } from '../bridge'
import type * as Bridge from '../bridge'
import type { BlockChange } from '../bridge'
import { block, blockAssets, blockRequest, BLOCK_UNDO_NOTIFICATION } from './block'
import BlockDialog from './BlockDialog.vue'

vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  engine: {
    blockPINConfigured: vi.fn(),
    setBlock: vi.fn(),
    blockFace: vi.fn(),
    undoBlock: vi.fn(),
    createBlockPIN: vi.fn(),
  },
}))

function deferred<T>() {
  let resolve: (value: T) => void = () => {
    throw new Error('the promise has no resolver')
  }
  const promise = new Promise<T>((done) => (resolve = done))
  return { promise, resolve }
}

let wrapper: VueWrapper | undefined
let notifications: RecordedNotifications
const body = new DOMWrapper(document.body)

function mountDialog() {
  return mountInApp(BlockDialog)
}

it('cancels a batch between items while settling the current item once', async () => {
  const pending = deferred<BlockChange>()
  vi.mocked(engine.setBlock).mockReturnValueOnce(pending.promise)
  const outcome = vi.fn()
  wrapper = mountDialog()
  const saved = blockAssets([1, 2, 3], outcome)
  await flushPromises()
  await body.get('[data-block-form]').trigger('submit')
  await body.get('[data-block="cancel"]').trigger('click')
  expect(blockRequest.value).not.toBeNull()
  pending.resolve({ id: 10, created: true })
  await flushPromises()
  expect(await saved).toBe(true)
  expect(engine.setBlock).toHaveBeenCalledExactlyOnceWith(BlockKind.BlockAsset, 1, '', BlockMode.BlockPhotos)
  expect(outcome).toHaveBeenCalledExactlyOnceWith(1, '')
  expect(blockRequest.value).toBeNull()
})

async function choose(label: string) {
  const field = body.findAll('[for]').find((item) => item.text() === label)
  const id = field?.attributes('for')
  if (!id) throw new Error(`the dialog has no radio labelled ${label}`)
  await body.get(`[id="${id}"][role="radio"]`).trigger('click')
}
beforeEach(() => {
  setActivePinia(createPinia())
  notifications = recordNotifications()
  vi.mocked(engine.blockPINConfigured).mockReset().mockResolvedValue(true)
  vi.mocked(engine.createBlockPIN).mockReset().mockResolvedValue(undefined)
  vi.mocked(engine.setBlock).mockReset()
  vi.mocked(engine.blockFace).mockReset()
  vi.mocked(engine.undoBlock).mockReset().mockResolvedValue(undefined)
})
afterEach(() => {
  blockRequest.value?.answer(false)
  blockRequest.value = null
  notifications.stop()
  wrapper?.unmount()
  wrapper = undefined
})

it('does not close a replacement request when an earlier save completes', async () => {
  const pending = deferred<BlockChange>()
  vi.mocked(engine.setBlock).mockReturnValueOnce(pending.promise)
  wrapper = mountDialog()
  const first = block(BlockKind.BlockTag, 1, 'First')
  await flushPromises()
  await body.get('[data-block-form]').trigger('submit')
  const second = block(BlockKind.BlockTag, 2, 'Second')
  await flushPromises()
  pending.resolve({ id: 3, created: true })
  await flushPromises()
  expect(await first).toBe(false)
  expect(blockRequest.value?.targetID).toBe(2)
  expect(body.text()).toContain('Second')
  await body.get('[data-block="cancel"]').trigger('click')
  expect(await second).toBe(false)
})

it('does not apply stale PIN configuration to a replacement request', async () => {
  const pending = deferred<boolean>()
  vi.mocked(engine.blockPINConfigured).mockReturnValueOnce(pending.promise)
  wrapper = mountDialog()
  const first = block(BlockKind.BlockTag, 1, 'First')
  await flushPromises()
  const second = block(BlockKind.BlockTag, 2, 'Second')
  await flushPromises()
  pending.resolve(false)
  await flushPromises()
  expect(body.find('input[type="password"]').exists()).toBe(false)
  expect(blockRequest.value?.targetID).toBe(2)
  await body.get('[data-block="cancel"]').trigger('click')
  expect(await first).toBe(false)
  expect(await second).toBe(false)
})

it('blocks just one face by default, and offers Undo for the new block', async () => {
  vi.mocked(engine.blockFace).mockResolvedValueOnce({ id: 9, created: true })
  wrapper = mountDialog()
  const saved = block(BlockKind.BlockFace, 11, 'Ali')
  await flushPromises()
  expect(body.text()).toContain('Only this face')
  expect(body.text()).toContain('This face and look-alikes')
  await body.get('[data-block-form]').trigger('submit')
  await flushPromises()
  expect(await saved).toBe(true)
  expect(engine.blockFace).toHaveBeenCalledWith(11, BlockMode.BlockPhotos, false)
  expect(engine.setBlock).not.toHaveBeenCalled()
  expect(notifications.find(BLOCK_UNDO_NOTIFICATION)?.title).toBe(t('blocked.saved', { name: 'Ali' }))
  expect(notifications.find(BLOCK_UNDO_NOTIFICATION)?.actions?.[0]?.label).toBe(t('blocked.undo'))
  notifications.find(BLOCK_UNDO_NOTIFICATION)?.actions?.[0]?.run()
  await flushPromises()
  expect(engine.undoBlock).toHaveBeenCalledExactlyOnceWith(9)
})

it('blocks the face and its look-alikes in label mode when the user chooses so', async () => {
  vi.mocked(engine.blockFace).mockResolvedValueOnce({ id: 9, created: true })
  wrapper = mountDialog()
  const saved = block(BlockKind.BlockFace, 11, 'this face')
  await flushPromises()
  await choose('This face and look-alikes')
  await choose(t('block.hideName'))
  await body.get('[data-block-form]').trigger('submit')
  await flushPromises()
  expect(await saved).toBe(true)
  expect(engine.blockFace).toHaveBeenCalledWith(11, BlockMode.BlockLabel, true)
})

it('does not offer the face choice for other kinds of block, nor keep it for the next face', async () => {
  vi.mocked(engine.setBlock).mockResolvedValueOnce({ id: 3, created: false })
  vi.mocked(engine.blockFace).mockResolvedValue({ id: 9, created: false })
  wrapper = mountDialog()
  const tag = block(BlockKind.BlockTag, 1, 'Cat')
  await flushPromises()
  expect(body.text()).not.toContain('look-alikes')
  await body.get('[data-block-form]').trigger('submit')
  await flushPromises()
  expect(await tag).toBe(true)
  expect(engine.setBlock).toHaveBeenCalledWith(BlockKind.BlockTag, 1, '', BlockMode.BlockPhotos)
  expect(notifications.find(BLOCK_UNDO_NOTIFICATION)).toBeUndefined()

  const first = block(BlockKind.BlockFace, 11, 'Ali')
  await flushPromises()
  await choose(t('blocked.faceLookalikes'))
  await body.get('[data-block-form]').trigger('submit')
  await flushPromises()
  expect(await first).toBe(true)
  const second = block(BlockKind.BlockFace, 12, 'Sara')
  await flushPromises()
  await body.get('[data-block-form]').trigger('submit')
  await flushPromises()
  expect(await second).toBe(true)
  expect(vi.mocked(engine.blockFace).mock.calls.map((c) => c[2])).toEqual([true, false])
  expect(notifications.find(BLOCK_UNDO_NOTIFICATION)).toBeUndefined()
})

it('returns focus to the initiating control when the modal is cancelled', async () => {
  const trigger = document.createElement('button')
  document.body.append(trigger)
  try {
    trigger.focus()
    wrapper = mountDialog()
    const saved = block(BlockKind.BlockTag, 1, 'Cat')
    await flushPromises()
    expect(document.body.querySelector('[role="dialog"]')).not.toBeNull()
    await body.get('[data-block="cancel"]').trigger('click')
    expect(await saved).toBe(false)
    await flushPromises()
    expect(document.activeElement).toBe(trigger)
  } finally {
    trigger.remove()
  }
})

it('hands the first block to PIN setup, keeps its scope and explains a mismatch before saving', async () => {
  vi.mocked(engine.blockPINConfigured).mockResolvedValueOnce(false)
  vi.mocked(engine.setBlock).mockResolvedValueOnce({ id: 12, created: true })
  wrapper = mountDialog()
  const saved = block(BlockKind.BlockAlbum, 8, 'Documents')
  await flushPromises()
  await choose(t('block.hideName'))
  await body.get('[data-block-form]').trigger('submit')
  expect(body.text()).toContain(t('block.createTitle'))
  const fields = body.findAll('input[type="password"]')
  await fields[0]?.setValue('123456')
  await fields[1]?.setValue('654321')
  await body.get('[data-block-form]').trigger('submit')
  expect(body.text()).toContain(t('blocked.pinMismatch'))
  expect(engine.createBlockPIN).not.toHaveBeenCalled()
  await fields[1]?.setValue('123456')
  await body.get('[data-block-form]').trigger('submit')
  await flushPromises()
  expect(await saved).toBe(true)
  expect(engine.createBlockPIN).toHaveBeenCalledExactlyOnceWith('123456')
  expect(engine.setBlock).toHaveBeenCalledWith(BlockKind.BlockAlbum, 8, '', BlockMode.BlockLabel)
})

it('retries a failed PIN configuration read without saving until protection is known', async () => {
  vi.mocked(engine.blockPINConfigured).mockRejectedValueOnce(new Error('library busy'))
  wrapper = mountDialog()
  const saved = block(BlockKind.BlockTag, 4, 'Receipts')
  await flushPromises()
  expect(body.get('[data-block="save"]').attributes('disabled')).toBeDefined()
  await body
    .findAll('button')
    .find((button) => button.text() === t('blocked.retry'))
    ?.trigger('click')
  await flushPromises()
  expect(body.get('[data-block="save"]').attributes('disabled')).toBeUndefined()
  await body.get('[data-block="cancel"]').trigger('click')
  expect(await saved).toBe(false)
})
