import { expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import { engine } from '../bridge'
import type * as Bridge from '../bridge'
import { useLibrary } from '../library/store'
import SettingsView from './SettingsView.vue'

vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  engine: { setTagConfidence: vi.fn<() => Promise<void>>().mockResolvedValue() },
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
