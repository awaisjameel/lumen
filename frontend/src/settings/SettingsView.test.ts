import { expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import { engine } from '../bridge'
import type * as Bridge from '../bridge'
import { useLibrary } from '../library/store'
import SettingsView from './SettingsView.vue'

vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  engine: {
    setTagConfidence: vi.fn<() => Promise<void>>().mockResolvedValue(),
    setVideos: vi.fn<() => Promise<void>>(),
  },
}))

it('persists the final slider value when leaving settings before the debounce', async () => {
  setActivePinia(createPinia())
  vi.spyOn(useLibrary(), 'refresh').mockResolvedValue()
  const wrapper = mount(SettingsView)
  await wrapper.get('#confidence').setValue('60')
  const value = useLibrary().settings.tagConfidence
  expect(engine.setTagConfidence).not.toHaveBeenCalled()
  wrapper.unmount()
  await flushPromises()
  expect(engine.setTagConfidence).toHaveBeenCalledExactlyOnceWith(value)
})

it('puts the videos checkbox back when the engine refuses', async () => {
  setActivePinia(createPinia())
  vi.mocked(engine.setVideos).mockRejectedValueOnce(new Error('database is locked'))
  const wrapper = mount(SettingsView)
  const box = wrapper.get<HTMLInputElement>('.toggle input')
  await box.setValue(true)
  await flushPromises()
  expect(useLibrary().settings.videos).toBe(false)
  expect(box.element.checked).toBe(false)
  expect(wrapper.text()).toContain('database is locked')
  wrapper.unmount()
})
