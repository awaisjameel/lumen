import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import { BlockKind, BlockMode, engine } from '../bridge'
import type * as Bridge from '../bridge'
import type { BlockChange } from '../bridge'
import { block, blockRequest, blockUndo, clearBlockUndo } from './block'
import BlockDialog from './BlockDialog.vue'

vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  engine: { blockPINConfigured: vi.fn(), setBlock: vi.fn(), blockFace: vi.fn() },
}))

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => (resolve = done))
  return { promise, resolve }
}

let wrapper: ReturnType<typeof mount> | undefined

function mountDialog() {
  const mounted = mount(BlockDialog)
  const dialog = mounted.get<HTMLDialogElement>('dialog').element
  dialog.showModal = vi.fn(() => {
    dialog.open = true
  })
  dialog.close = vi.fn(() => {
    dialog.open = false
  })
  return mounted
}

beforeEach(() => {
  setActivePinia(createPinia())
  vi.mocked(engine.blockPINConfigured).mockReset().mockResolvedValue(true)
  vi.mocked(engine.setBlock).mockReset()
  vi.mocked(engine.blockFace).mockReset()
})
afterEach(() => {
  blockRequest.value?.answer(false)
  blockRequest.value = null
  clearBlockUndo()
  wrapper?.unmount()
  wrapper = undefined
})

it('does not close a replacement request when an earlier save completes', async () => {
  const pending = deferred<BlockChange>()
  vi.mocked(engine.setBlock).mockReturnValueOnce(pending.promise)
  wrapper = mountDialog()
  const first = block(BlockKind.BlockTag, 1, 'First')
  await flushPromises()
  await wrapper.get('form').trigger('submit')
  const second = block(BlockKind.BlockTag, 2, 'Second')
  await flushPromises()
  pending.resolve({ id: 3, created: true })
  await flushPromises()
  expect(await first).toBe(false)
  expect(blockRequest.value?.targetID).toBe(2)
  expect(wrapper.text()).toContain('Second')
  await wrapper.get('button[type="button"]').trigger('click')
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
  expect(wrapper.find('input[type="password"]').exists()).toBe(false)
  expect(blockRequest.value?.targetID).toBe(2)
  await wrapper.get('button[type="button"]').trigger('click')
  expect(await first).toBe(false)
  expect(await second).toBe(false)
})

it('blocks just one face by default, and offers Undo for the new block', async () => {
  vi.mocked(engine.blockFace).mockResolvedValueOnce({ id: 9, created: true })
  wrapper = mountDialog()
  const saved = block(BlockKind.BlockFace, 11, 'Ali')
  await flushPromises()
  expect(wrapper.text()).toContain('Only this face')
  expect(wrapper.text()).toContain('This face and look-alikes')
  await wrapper.get('form').trigger('submit')
  await flushPromises()
  expect(await saved).toBe(true)
  expect(engine.blockFace).toHaveBeenCalledWith(11, BlockMode.BlockPhotos, false)
  expect(engine.setBlock).not.toHaveBeenCalled()
  expect(blockUndo.value).toEqual({ id: 9, name: 'Ali' })
})

it('blocks the face and its look-alikes in label mode when the user chooses so', async () => {
  vi.mocked(engine.blockFace).mockResolvedValueOnce({ id: 9, created: true })
  wrapper = mountDialog()
  const saved = block(BlockKind.BlockFace, 11, 'this face')
  await flushPromises()
  const radios = wrapper.findAll<HTMLInputElement>('input[type="radio"]')
  const choose = async (label: string) => {
    const radio = radios.find((r) => r.element.parentElement?.textContent === label)
    await radio?.setValue(true)
  }
  await choose('This face and look-alikes')
  await choose('Label only')
  await wrapper.get('form').trigger('submit')
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
  expect(wrapper.text()).not.toContain('look-alikes')
  await wrapper.get('form').trigger('submit')
  await flushPromises()
  expect(await tag).toBe(true)
  expect(engine.setBlock).toHaveBeenCalledWith(BlockKind.BlockTag, 1, '', BlockMode.BlockPhotos)

  const first = block(BlockKind.BlockFace, 11, 'Ali')
  await flushPromises()
  await wrapper
    .findAll<HTMLInputElement>('input[type="radio"]')
    .find((r) => r.element.value === 'true')
    ?.setValue(true)
  await wrapper.get('form').trigger('submit')
  await flushPromises()
  expect(await first).toBe(true)
  const second = block(BlockKind.BlockFace, 12, 'Sara')
  await flushPromises()
  await wrapper.get('form').trigger('submit')
  await flushPromises()
  expect(await second).toBe(true)
  expect(vi.mocked(engine.blockFace).mock.calls.map((c) => c[2])).toEqual([true, false])
})
