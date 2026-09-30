import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import { engine } from '../bridge'
import type * as Bridge from '../bridge'
import BlockedView from './BlockedView.vue'

vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  engine: {
    blockPINConfigured: vi.fn(),
    unlockBlocks: vi.fn(),
    lockBlocks: vi.fn(),
    blocks: vi.fn(),
  },
}))

let wrapper: ReturnType<typeof mount> | undefined

beforeEach(() => {
  setActivePinia(createPinia())
  vi.mocked(engine.blockPINConfigured).mockReset().mockResolvedValue(true)
  vi.mocked(engine.unlockBlocks).mockReset().mockResolvedValue(undefined)
  vi.mocked(engine.lockBlocks).mockReset().mockResolvedValue(undefined)
  vi.mocked(engine.blocks).mockReset().mockResolvedValue([])
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
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
