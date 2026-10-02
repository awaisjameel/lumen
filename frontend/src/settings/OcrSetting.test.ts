import { beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import { engine } from '../bridge'
import type * as Bridge from '../bridge'
import { useLibrary } from '../library/store'
import OcrSetting from './OcrSetting.vue'

vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  engine: { setOCR: vi.fn<(on: boolean) => Promise<void>>() },
}))

beforeEach(() => {
  setActivePinia(createPinia())
  vi.mocked(engine.setOCR).mockReset().mockResolvedValue(undefined)
})

it('is off until the user turns it on, and says what it does', () => {
  const box = mount(OcrSetting)
  expect(box.get('input').element.checked).toBe(false)
  expect(box.text()).toContain('Find text in screenshots and documents')
  expect(box.text()).toContain('processor')
  expect(box.text()).toContain('only on those photos')
})

it('reflects the saved setting', () => {
  useLibrary().settings.ocr = true
  expect(mount(OcrSetting).get('input').element.checked).toBe(true)
})

it('saves clicks in order, one at a time, ending with the last', async () => {
  const lib = useLibrary()
  let finishFirst: () => void = () => {
    throw new Error('the first save has not started')
  }
  vi.mocked(engine.setOCR)
    .mockReturnValueOnce(new Promise<void>((resolve) => (finishFirst = resolve)))
    .mockResolvedValueOnce(undefined)
  const box = mount(OcrSetting)
  await box.get('input').setValue(true)
  await box.get('input').setValue(false)
  expect(engine.setOCR).toHaveBeenCalledExactlyOnceWith(true) // the second waits for the first
  finishFirst()
  await flushPromises()
  expect(engine.setOCR).toHaveBeenNthCalledWith(2, false)
  expect(lib.settings.ocr).toBe(false)
})

it('puts the box back and says why when the newest save fails', async () => {
  const lib = useLibrary()
  vi.mocked(engine.setOCR).mockRejectedValueOnce(new Error('the models are missing'))
  const box = mount(OcrSetting)
  await box.get('input').setValue(true)
  await flushPromises()
  expect(lib.settings.ocr).toBe(false)
  expect(lib.settingsError).toContain('the models are missing')
})

it('keeps saves in order when Settings is closed and opened again', async () => {
  const lib = useLibrary()
  let finishFirst: () => void = () => {
    throw new Error('the first save has not started')
  }
  vi.mocked(engine.setOCR)
    .mockReturnValueOnce(new Promise<void>((resolve) => (finishFirst = resolve)))
    .mockResolvedValueOnce(undefined)
  const first = mount(OcrSetting)
  await first.get('input').setValue(true)
  first.unmount()
  const next = mount(OcrSetting)
  await next.get('input').setValue(false)
  expect(engine.setOCR).toHaveBeenCalledExactlyOnceWith(true) // the newer click waits for the older save
  finishFirst()
  await flushPromises()
  expect(engine.setOCR).toHaveBeenNthCalledWith(2, false)
  expect(lib.settings.ocr).toBe(false)
})
