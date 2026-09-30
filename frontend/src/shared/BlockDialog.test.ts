import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import { BlockKind, engine } from '../bridge'
import type * as Bridge from '../bridge'
import type { BlockChange } from '../bridge'
import { block, blockRequest, clearBlockUndo } from './block'
import BlockDialog from './BlockDialog.vue'

vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  engine: { blockPINConfigured: vi.fn(), setBlock: vi.fn() },
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
