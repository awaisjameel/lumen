import { afterEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import { engine, MAX_NAME_LEN } from '../bridge'
import type * as Bridge from '../bridge'
import ThingsView from './ThingsView.vue'

vi.mock('vue-router', () => ({ useRouter: () => ({ push: vi.fn() }) }))
vi.mock('../library/store', () => ({ useLibrary: () => ({ progress: null }) }))
vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  onLibraryChanged: vi.fn(() => () => undefined),
  engine: { things: vi.fn().mockResolvedValue([]), createLabel: vi.fn().mockResolvedValue(1) },
}))

let wrapper: ReturnType<typeof mount> | undefined
afterEach(() => wrapper?.unmount())

it('validates names by code point before creating a Thing', async () => {
  setActivePinia(createPinia())
  wrapper = mount(ThingsView, { global: { stubs: { SearchBox: true, RouterLink: true } } })
  await flushPromises()
  expect(wrapper.get('form input').attributes('maxlength')).toBeUndefined()
  await wrapper.get('form input').setValue('😀'.repeat(MAX_NAME_LEN + 1))
  await wrapper.get('form').trigger('submit')
  await flushPromises()
  expect(wrapper.get('[role="alert"]').text()).toContain('80')
  expect(engine.createLabel).not.toHaveBeenCalled()
  await wrapper.get('form input').setValue(` ${'😀'.repeat(MAX_NAME_LEN)} `)
  await wrapper.get('form').trigger('submit')
  await flushPromises()
  expect(engine.createLabel).toHaveBeenCalledWith('😀'.repeat(MAX_NAME_LEN))
})
