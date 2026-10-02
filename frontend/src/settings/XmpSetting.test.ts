import { beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import { engine, ErrorCode } from '../bridge'
import type * as Bridge from '../bridge'
import { useLibrary } from '../library/store'
import XmpSetting from './XmpSetting.vue'

vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  engine: {
    setXMP: vi.fn<(on: boolean) => Promise<void>>(),
    exportXMPNow: vi.fn<() => Promise<Bridge.XMPExport>>(),
  },
}))

beforeEach(() => {
  setActivePinia(createPinia())
  vi.mocked(engine.setXMP).mockReset().mockResolvedValue(undefined)
  vi.mocked(engine.exportXMPNow).mockReset().mockResolvedValue({ written: 0, unchanged: 0, skipped: 0 })
})

it('is off until the user turns it on, says what it does and offers no export', () => {
  const box = mount(XmpSetting)
  expect(box.get('input').element.checked).toBe(false)
  expect(box.text()).toContain('Write XMP sidecar files beside my photos')
  expect(box.text()).toContain('never changes your photos')
  expect(box.text()).toContain('leaves out anything you have blocked')
  expect(box.find('button').exists()).toBe(false)
})

it('reflects the saved setting and offers Export now while it is on', () => {
  useLibrary().settings.xmp = true
  const box = mount(XmpSetting)
  expect(box.get('input').element.checked).toBe(true)
  expect(box.get('button').text()).toBe('Export now')
})

it('saves the choice and shows Export now once it is on', async () => {
  const lib = useLibrary()
  const box = mount(XmpSetting)
  await box.get('input').setValue(true)
  expect(engine.setXMP).toHaveBeenCalledExactlyOnceWith(true)
  expect(lib.settings.xmp).toBe(true)
  expect(box.find('button').exists()).toBe(true)
})

it('saves clicks in order, ending with the last', async () => {
  const lib = useLibrary()
  let finishFirst: () => void = () => {
    throw new Error('the first save has not started')
  }
  vi.mocked(engine.setXMP)
    .mockReturnValueOnce(new Promise<void>((resolve) => (finishFirst = resolve)))
    .mockResolvedValueOnce(undefined)
  const box = mount(XmpSetting)
  await box.get('input').setValue(true)
  await box.get('input').setValue(false)
  expect(engine.setXMP).toHaveBeenCalledExactlyOnceWith(true) // the second waits for the first
  finishFirst()
  await flushPromises()
  expect(engine.setXMP).toHaveBeenNthCalledWith(2, false)
  expect(lib.settings.xmp).toBe(false)
})

it('puts the box back and says why when the save fails', async () => {
  const lib = useLibrary()
  vi.mocked(engine.setXMP).mockRejectedValueOnce(new Error('the database is locked'))
  const box = mount(XmpSetting)
  await box.get('input').setValue(true)
  await flushPromises()
  expect(lib.settings.xmp).toBe(false)
  expect(lib.settingsError).toContain('the database is locked')
  expect(box.find('button').exists()).toBe(false)
})

it('exports now and reports what was written, left up to date and left alone', async () => {
  useLibrary().settings.xmp = true
  vi.mocked(engine.exportXMPNow).mockResolvedValueOnce({ written: 12, unchanged: 3400, skipped: 2 })
  const box = mount(XmpSetting)
  await box.get('button').trigger('click')
  await flushPromises()
  expect(box.get('[role="status"]').text()).toContain('Wrote 12, 3,400 already up to date, 2 left alone.')
  expect(box.get('[role="status"]').text()).toContain('another program made the file')
  expect(box.get('[role="status"]').text()).toContain('a read-only folder or a locked file')
})

it('does not explain "left alone" when nothing was', async () => {
  useLibrary().settings.xmp = true
  vi.mocked(engine.exportXMPNow).mockResolvedValueOnce({ written: 1, unchanged: 0, skipped: 0 })
  const box = mount(XmpSetting)
  await box.get('button').trigger('click')
  await flushPromises()
  expect(box.get('[role="status"]').text()).toBe('Wrote 1, 0 already up to date, 0 left alone.')
})

it('disables the button while it exports, and shows a failure', async () => {
  const lib = useLibrary()
  lib.settings.xmp = true
  let fail: (e: Error) => void = () => {
    throw new Error('the export has not started')
  }
  vi.mocked(engine.exportXMPNow).mockReturnValueOnce(new Promise((_resolve, reject) => (fail = reject)))
  const box = mount(XmpSetting)
  await box.get('button').trigger('click')
  expect(box.get('button').attributes('disabled')).toBeDefined()
  expect(box.get('button').text()).toBe('Exporting…')
  fail(new Error('the folder is read-only'))
  await flushPromises()
  expect(box.get('button').attributes('disabled')).toBeUndefined()
  expect(lib.settingsError).toContain('the folder is read-only')
})

it('hides the last result when it is turned off', async () => {
  useLibrary().settings.xmp = true
  const box = mount(XmpSetting)
  await box.get('button').trigger('click')
  await flushPromises()
  expect(box.find('[role="status"]').exists()).toBe(true)
  await box.get('input').setValue(false)
  expect(box.find('[role="status"]').exists()).toBe(false)
  expect(box.find('button').exists()).toBe(false)
})

it('explains an export refused because the setting is off', async () => {
  const lib = useLibrary()
  lib.settings.xmp = true
  vi.mocked(engine.exportXMPNow).mockRejectedValueOnce(
    new Error('XMP sidecars are off', { cause: { code: ErrorCode.XMPOff } }),
  )
  const box = mount(XmpSetting)
  await box.get('button').trigger('click')
  await flushPromises()
  expect(lib.settingsError).toBe('XMP sidecar files are turned off. Turn them on in Settings first.')
})
