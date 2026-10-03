import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { enableAutoUnmount, flushPromises } from '@vue/test-utils'
import { mountInApp } from '../shared/testing'
import { engine, ErrorCode } from '../bridge'
import type * as Bridge from '../bridge'
import { useLibrary } from '../library/store'
import { question } from '../shared/confirm'
import XmpSetting from './XmpSetting.vue'

enableAutoUnmount(afterEach)

vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  engine: {
    setXMP: vi.fn<(on: boolean) => Promise<void>>(),
    exportXMPNow: vi.fn<() => Promise<Bridge.XMPExport>>(),
    deleteXMP: vi.fn<() => Promise<Bridge.XMPDelete>>(),
  },
}))

beforeEach(() => {
  setActivePinia(createPinia())
  vi.mocked(engine.setXMP).mockReset().mockResolvedValue(undefined)
  vi.mocked(engine.exportXMPNow).mockReset().mockResolvedValue({ written: 0, unchanged: 0, skipped: 0 })
  vi.mocked(engine.deleteXMP).mockReset().mockResolvedValue({ deleted: 0, kept: 0 })
  question.value = null
})

type Box = ReturnType<typeof mountInApp>

const exportButton = (box: Box) => box.findAll('button').find((b) => b.text().startsWith('Export'))

function deleteButton(box: Box) {
  return box.get('[data-xmp="delete"]')
}

/** Clicks the delete button and answers the question it asks. */
async function askToDelete(box: Box, yes: boolean) {
  await deleteButton(box).trigger('click')
  expect(question.value?.title).toBe('Delete Lumen’s sidecar files?')
  expect(question.value?.text).toContain('Recycle Bin')
  expect(question.value?.ok).toBe('Move to Recycle Bin')
  question.value?.answer(yes)
  await flushPromises()
}

it('is off until the user turns it on, says what it does and offers no export', () => {
  const box = mountInApp(XmpSetting)
  expect(box.get('[role="switch"]').attributes('aria-checked')).toBe('false')
  expect(box.text()).toContain('Write XMP sidecar files beside my photos')
  expect(box.text()).toContain('never changes your photos')
  expect(box.text()).toContain('leaves out anything you have blocked')
  expect(exportButton(box)).toBeUndefined()
})

it('reflects the saved setting and offers Export now while it is on', () => {
  useLibrary().settings.xmp = true
  const box = mountInApp(XmpSetting)
  expect(box.get('[role="switch"]').attributes('aria-checked')).toBe('true')
  expect(box.get('[data-xmp="export"]').text()).toBe('Export now')
})

it('saves the choice and shows Export now once it is on', async () => {
  const lib = useLibrary()
  const box = mountInApp(XmpSetting)
  await box.get('[role="switch"]').trigger('click')
  expect(engine.setXMP).toHaveBeenCalledExactlyOnceWith(true)
  expect(lib.settings.xmp).toBe(true)
  expect(exportButton(box)).toBeDefined()
})

it('saves clicks in order, ending with the last', async () => {
  const lib = useLibrary()
  let finishFirst: () => void = () => {
    throw new Error('the first save has not started')
  }
  vi.mocked(engine.setXMP)
    .mockReturnValueOnce(new Promise<void>((resolve) => (finishFirst = resolve)))
    .mockResolvedValueOnce(undefined)
  const box = mountInApp(XmpSetting)
  await box.get('[role="switch"]').trigger('click')
  await box.get('[role="switch"]').trigger('click')
  expect(engine.setXMP).toHaveBeenCalledExactlyOnceWith(true) // the second waits for the first
  finishFirst()
  await flushPromises()
  expect(engine.setXMP).toHaveBeenNthCalledWith(2, false)
  expect(lib.settings.xmp).toBe(false)
})

it('puts the box back and says why when the save fails', async () => {
  const lib = useLibrary()
  vi.mocked(engine.setXMP).mockRejectedValueOnce(new Error('the database is locked'))
  const box = mountInApp(XmpSetting)
  await box.get('[role="switch"]').trigger('click')
  await flushPromises()
  expect(lib.settings.xmp).toBe(false)
  expect(lib.settingsError).toContain('the database is locked')
  expect(exportButton(box)).toBeUndefined()
})

it('exports now and reports what was written, left up to date and left alone', async () => {
  useLibrary().settings.xmp = true
  vi.mocked(engine.exportXMPNow).mockResolvedValueOnce({ written: 12, unchanged: 3400, skipped: 2 })
  const box = mountInApp(XmpSetting)
  await box.get('[data-xmp="export"]').trigger('click')
  await flushPromises()
  expect(box.get('[role="status"]').text()).toContain('Wrote 12, 3,400 already up to date, 2 left alone.')
  expect(box.get('[role="status"]').text()).toContain('another program made the file')
  expect(box.get('[role="status"]').text()).toContain('a read-only folder or a locked file')
})

it('does not explain "left alone" when nothing was', async () => {
  useLibrary().settings.xmp = true
  vi.mocked(engine.exportXMPNow).mockResolvedValueOnce({ written: 1, unchanged: 0, skipped: 0 })
  const box = mountInApp(XmpSetting)
  await box.get('[data-xmp="export"]').trigger('click')
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
  const box = mountInApp(XmpSetting)
  await box.get('[data-xmp="export"]').trigger('click')
  expect(box.get('[data-xmp="export"]').attributes('disabled')).toBeDefined()
  expect(box.get('[data-xmp="export"]').text()).toBe('Exporting…')
  fail(new Error('the folder is read-only'))
  await flushPromises()
  expect(box.get('[data-xmp="export"]').attributes('disabled')).toBeUndefined()
  expect(lib.settingsError).toContain('the folder is read-only')
})

it('hides the last result when it is turned off', async () => {
  useLibrary().settings.xmp = true
  const box = mountInApp(XmpSetting)
  await box.get('[data-xmp="export"]').trigger('click')
  await flushPromises()
  expect(box.find('[role="status"]').exists()).toBe(true)
  await box.get('[role="switch"]').trigger('click')
  expect(box.find('[role="status"]').exists()).toBe(false)
  expect(exportButton(box)).toBeUndefined()
})

it('explains an export refused because the setting is off', async () => {
  const lib = useLibrary()
  lib.settings.xmp = true
  vi.mocked(engine.exportXMPNow).mockRejectedValueOnce(
    new Error('XMP sidecars are off', { cause: { code: ErrorCode.XMPOff } }),
  )
  const box = mountInApp(XmpSetting)
  await box.get('[data-xmp="export"]').trigger('click')
  await flushPromises()
  expect(lib.settingsError).toBe('XMP sidecar files are turned off. Turn them on in Settings first.')
})

it('offers to delete Lumen’s sidecar files whether the setting is on or off', () => {
  const box = mountInApp(XmpSetting)
  expect(deleteButton(box).text()).toBe('Delete Lumen’s sidecar files…')
  expect(box.text()).toContain('sidecar files from other programs, are not touched')
  useLibrary().settings.xmp = true
  expect(deleteButton(mountInApp(XmpSetting)).text()).toBe('Delete Lumen’s sidecar files…')
})

it('deletes nothing unless the user confirms', async () => {
  useLibrary().settings.xmp = true
  const box = mountInApp(XmpSetting)
  await askToDelete(box, false)
  expect(engine.deleteXMP).not.toHaveBeenCalled()
  expect(engine.setXMP).not.toHaveBeenCalled()
  expect(useLibrary().settings.xmp).toBe(true)
})

it('turns the setting off, deletes and says how many went to the Recycle Bin', async () => {
  const lib = useLibrary()
  lib.settings.xmp = true
  const order: string[] = []
  vi.mocked(engine.setXMP).mockImplementationOnce((on) => {
    order.push(`set ${on}`)
    return Promise.resolve()
  })
  vi.mocked(engine.deleteXMP).mockImplementationOnce(() => {
    order.push('delete')
    return Promise.resolve({ deleted: 4073, kept: 0 })
  })
  const box = mountInApp(XmpSetting)
  await askToDelete(box, true)
  expect(order).toEqual(['set false', 'delete'])
  expect(lib.settings.xmp).toBe(false)
  expect(box.get('[role="switch"]').attributes('aria-checked')).toBe('false')
  expect(exportButton(box)).toBeUndefined()
  expect(box.get('[role="status"]').text()).toBe('Moved 4,073 sidecar files to the Recycle Bin.')
})

it('says which sidecar files were left alone, and when there were none to delete', async () => {
  vi.mocked(engine.deleteXMP).mockResolvedValueOnce({ deleted: 1, kept: 2 })
  const box = mountInApp(XmpSetting)
  await askToDelete(box, true)
  expect(box.get('[role="status"]').text()).toContain('Moved 1 sidecar file to the Recycle Bin.')
  expect(box.get('[role="status"]').text()).toContain('2 were left alone: they were changed after Lumen wrote them')

  await askToDelete(box, true)
  expect(box.get('[role="status"]').text()).toBe('There were no sidecar files from Lumen to delete.')
})

it('disables the controls while it deletes, and shows a failure', async () => {
  const lib = useLibrary()
  lib.settings.xmp = true
  let fail: (e: Error) => void = () => {
    throw new Error('the delete has not started')
  }
  vi.mocked(engine.deleteXMP).mockReturnValueOnce(new Promise((_resolve, reject) => (fail = reject)))
  const box = mountInApp(XmpSetting)
  await askToDelete(box, true)
  expect(deleteButton(box).attributes('disabled')).toBeDefined()
  expect(deleteButton(box).text()).toBe('Deleting…')
  expect(box.get('[role="switch"]').attributes('disabled')).toBeDefined()
  fail(new Error('the Recycle Bin is not available'))
  await flushPromises()
  expect(deleteButton(box).attributes('disabled')).toBeUndefined()
  expect(box.get('[role="switch"]').attributes('disabled')).toBeUndefined()
  expect(lib.settingsError).toContain('the Recycle Bin is not available')
})

it('does not delete when the setting could not be turned off', async () => {
  const lib = useLibrary()
  lib.settings.xmp = true
  vi.mocked(engine.setXMP).mockRejectedValueOnce(new Error('the database is locked'))
  const box = mountInApp(XmpSetting)
  await askToDelete(box, true)
  expect(engine.deleteXMP).not.toHaveBeenCalled()
  expect(lib.settings.xmp).toBe(true)
  expect(lib.settingsError).toContain('the database is locked')
})

it('filters sidecar controls by their labels and descriptions', () => {
  useLibrary().settings.xmp = true
  // Words from the description of "delete" alone: only that control stays.
  const box = mountInApp(XmpSetting, { props: { query: 'takes back every sidecar' } })
  expect(box.find('[role="switch"]').exists()).toBe(false)
  expect(box.find('[data-xmp="export"]').exists()).toBe(false)
  expect(box.get('[data-xmp="delete"]').text()).toBe('Delete Lumen’s sidecar files…')
})
