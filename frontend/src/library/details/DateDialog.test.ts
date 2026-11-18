import { createPinia } from 'pinia'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import { engine, DateSource } from '../../bridge'
import type * as Bridge from '../../bridge'
import { t } from '../../shared/i18n'
import { recordNotifications } from '../../shared/testing'
import DateDialog from './DateDialog.vue'
import { asset, body, press, mountCorrection, undo } from './testing'

vi.mock('../../bridge', async (original) => ({
  ...(await original<typeof Bridge>()),
  engine: {
    asset: vi.fn(),
    albums: vi.fn(),
    counts: vi.fn(),
    setTakenAt: vi.fn(),
    restoreTakenAt: vi.fn(),
    validateSelection: vi.fn(),
  },
}))
let wrapper: VueWrapper | undefined
let notices: ReturnType<typeof recordNotifications>
beforeEach(() => {
  notices = recordNotifications()
  vi.mocked(engine.albums).mockResolvedValue([])
  vi.mocked(engine.counts).mockResolvedValue({ photos: 2, videos: 0, favourites: 0, people: 0, trash: 0 })
  vi.mocked(engine.setTakenAt).mockReset().mockResolvedValue(undefined)
  vi.mocked(engine.restoreTakenAt).mockReset().mockResolvedValue(undefined)
  vi.mocked(engine.validateSelection).mockReset().mockResolvedValue(undefined)
})
afterEach(() => {
  wrapper?.unmount()
  for (const notice of notices.shown) notice.closed?.()
  notices.stop()
  document.body.innerHTML = ''
})

it('saves a selection atomically and undoes each previous correction separately', async () => {
  wrapper = mountCorrection(DateDialog, {
    props: {
      open: true,
      assets: [
        asset(),
        {
          ...asset(2),
          takenAt: 100,
          dateFrom: DateSource.DateFromUser,
          originalDate: { takenAt: 50, from: DateSource.DateFromEXIF },
        },
      ],
      selection: { ids: [1, 2], revision: 1 },
    },
  })
  await flushPromises()
  await body().get('input[type="datetime-local"]').setValue('1969-12-31T23:59:59')
  await body().get('form').trigger('submit')
  await flushPromises()
  expect(engine.validateSelection).toHaveBeenCalledOnce()
  expect(engine.setTakenAt).toHaveBeenCalledWith([1, 2], -1)
  wrapper.unmount()
  await undo(notices)
  expect(engine.restoreTakenAt).toHaveBeenCalledWith([1])
  expect(engine.setTakenAt).toHaveBeenLastCalledWith([2], 100)
})

it('restores the original date and Undo reinstates the previous correction', async () => {
  wrapper = mountCorrection(DateDialog, {
    props: {
      open: true,
      assets: [{ ...asset(), takenAt: 123, originalDate: { takenAt: 0, from: DateSource.DateFromEXIF } }],
    },
  })
  await flushPromises()
  await press(t('details.restoreOriginal'))
  expect(engine.restoreTakenAt).toHaveBeenCalledWith([1])
  await undo(notices)
  expect(engine.setTakenAt).toHaveBeenLastCalledWith([1], 123)
})

it('keeps the draft and error in the dialog when saving fails and allows retry', async () => {
  vi.mocked(engine.setTakenAt).mockRejectedValueOnce(new Error('Library is read-only'))
  wrapper = mountCorrection(DateDialog, { props: { open: true, assets: [asset()] } })
  await flushPromises()
  await body().get('input').setValue('2026-10-16T10:24:00')
  await body().get('form').trigger('submit')
  await flushPromises()
  expect(body().get('[role="alert"]').text()).toContain('Library is read-only')
  expect(body().get<HTMLInputElement>('input').element.value).toContain('2026-10-16T10:24')
  expect(notices.shown).toHaveLength(0)
  await body().get('form').trigger('submit')
  await flushPromises()
  expect(engine.setTakenAt).toHaveBeenCalledTimes(2)
})

it('rejects an empty date and prevents duplicate writes while saving', async () => {
  wrapper = mountCorrection(DateDialog, { props: { open: true, assets: [asset()] } })
  await flushPromises()
  await body().get('input').setValue('')
  await body().get('form').trigger('submit')
  expect(body().get('[role="alert"]').text()).toContain(t('details.date.invalid'))
  expect(engine.setTakenAt).not.toHaveBeenCalled()
  let done: () => void = () => undefined
  vi.mocked(engine.setTakenAt).mockReturnValueOnce(new Promise((resolve) => (done = resolve)))
  await body().get('input').setValue('1970-01-01T00:00')
  await body().get('form').trigger('submit')
  await body().get('form').trigger('submit')
  expect(engine.setTakenAt).toHaveBeenCalledExactlyOnceWith([1], 0)
  expect(body().get('input').attributes('disabled')).toBeDefined()
  done()
  await flushPromises()
})

it('does not write a correction after its owner disappears during selection validation', async () => {
  let validated: () => void = () => undefined
  vi.mocked(engine.validateSelection).mockReturnValueOnce(new Promise((resolve) => (validated = resolve)))
  wrapper = mountCorrection(DateDialog, {
    props: { open: true, assets: [asset()], selection: { ids: [1], revision: 1 } },
  })
  await flushPromises()
  await body().get('form').trigger('submit')
  wrapper.unmount()
  validated()
  await flushPromises()
  expect(engine.setTakenAt).not.toHaveBeenCalled()
  expect(notices.shown).toHaveLength(0)
})

it('reconciles an earlier restored date when a later inverse is refused', async () => {
  wrapper = mountCorrection(DateDialog, { props: { open: true, assets: [asset(), asset(2)] } })
  await flushPromises()
  await body().get('form').trigger('submit')
  await flushPromises()
  vi.mocked(engine.restoreTakenAt)
    .mockResolvedValueOnce(undefined)
    .mockRejectedValueOnce(new Error('Second date refused'))
  await undo(notices)
  expect(engine.restoreTakenAt).toHaveBeenCalledTimes(2)
  expect(wrapper.findComponent(DateDialog).emitted('changed')).toHaveLength(2)
})

it('captures the current user correction beside the write instead of trusting stale dialog metadata', async () => {
  wrapper = mountCorrection(DateDialog, { props: { open: true, assets: [asset()] } })
  await flushPromises()
  vi.mocked(engine.asset).mockResolvedValueOnce({
    ...asset(),
    takenAt: 100,
    originalDate: { takenAt: 50, from: DateSource.DateFromEXIF },
  })
  await body().get('form').trigger('submit')
  await flushPromises()
  await undo(notices)
  expect(engine.setTakenAt).toHaveBeenLastCalledWith([1], 100)
  expect(engine.restoreTakenAt).not.toHaveBeenCalled()
})

it('does not replace a newer correction with an older inverse in the same app', async () => {
  const pinia = createPinia()
  wrapper = mountCorrection(DateDialog, { pinia, props: { open: true, assets: [asset()] } })
  await flushPromises()
  await body().get('input').setValue('2001-01-01T00:00')
  await body().get('form').trigger('submit')
  await flushPromises()
  wrapper.unmount()
  wrapper = mountCorrection(DateDialog, {
    pinia,
    props: {
      open: true,
      assets: [
        {
          ...asset(),
          takenAt: 978307200,
          originalDate: { takenAt: 0, from: DateSource.DateFromEXIF },
        },
      ],
    },
  })
  await flushPromises()
  await body().get('input').setValue('2002-01-01T00:00')
  await body().get('form').trigger('submit')
  await flushPromises()
  vi.mocked(engine.setTakenAt).mockClear()
  const job = await undo(notices)
  expect(job.counts).toMatchObject({ superseded: 1, done: 0 })
  expect(engine.setTakenAt).not.toHaveBeenCalled()
  expect(engine.restoreTakenAt).not.toHaveBeenCalled()
})

it('retries a correction refresh without repeating the completed inverse', async () => {
  wrapper = mountCorrection(DateDialog, { props: { open: true, assets: [asset()] } })
  await flushPromises()
  await body().get('form').trigger('submit')
  await flushPromises()
  vi.mocked(engine.counts).mockRejectedValueOnce(new Error('Counts temporarily unavailable'))
  const job = await undo(notices)
  expect(job.counts).toMatchObject({ done: 1, failed: 0 })
  expect(job.refreshError).toContain('Counts temporarily unavailable')
  vi.mocked(engine.restoreTakenAt).mockClear()
  await job.refresh()
  expect(job.refreshError).toBe('')
  expect(engine.restoreTakenAt).not.toHaveBeenCalled()
})
