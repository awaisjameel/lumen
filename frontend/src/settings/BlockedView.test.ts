import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import { BLOCK_UNLOCK_IDLE_MINUTES, BlockKind, BlockMode, engine, ErrorCode } from '../bridge'
import type * as Bridge from '../bridge'
import type * as Block from '../shared/block'
import BlockedView from './BlockedView.vue'

vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  engine: {
    blockPINConfigured: vi.fn(),
    unlockBlocks: vi.fn(),
    lockBlocks: vi.fn(),
    blocks: vi.fn(),
    setBlock: vi.fn(),
    removeBlock: vi.fn(),
  },
}))
vi.mock('../shared/block', async (importOriginal) => ({
  ...(await importOriginal<typeof Block>()),
  showBlocksChanged: vi.fn().mockResolvedValue(undefined),
}))
vi.mock('../shared/confirm', () => ({ confirm: vi.fn().mockResolvedValue(true) }))

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
  vi.mocked(engine.setBlock).mockReset().mockResolvedValue({ id: 1, created: false })
  vi.mocked(engine.removeBlock).mockReset().mockResolvedValue(undefined)
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  vi.useRealTimers()
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
    if (action === 'mode') vi.mocked(engine.setBlock).mockRejectedValueOnce(locked)
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
