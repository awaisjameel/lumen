import { reactive } from 'vue'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { DOMWrapper, flushPromises } from '@vue/test-utils'
import { mountInApp, recordNotifications } from '../shared/testing'
import type { RecordedNotifications } from '../shared/testing'
import { t } from '../shared/i18n'
import { engine } from '../bridge'
import type * as Bridge from '../bridge'
import TimeGap from './TimeGap.vue'

vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  engine: { eventCount: vi.fn<(hours: number, signal?: AbortSignal) => Promise<number>>() },
}))

const lib = reactive({
  settings: { eventGapHours: 12 },
  settingErrors: { eventGapHours: '' },
  saveEventGap: vi.fn<(hours: number) => Promise<void>>(),
})
vi.mock('../library/store', () => ({ useLibrary: () => lib }))
let wrapper: ReturnType<typeof mountInApp> | undefined
let notices: RecordedNotifications
const reload = vi.fn<() => Promise<void>>()
beforeEach(() => {
  notices = recordNotifications()
  lib.settings.eventGapHours = 12
  lib.settingErrors.eventGapHours = ''
  lib.saveEventGap.mockReset().mockImplementation((value) => {
    lib.settings.eventGapHours = value
    return Promise.resolve()
  })
  reload.mockReset().mockResolvedValue(undefined)
  vi.mocked(engine.eventCount)
    .mockReset()
    .mockImplementation((hours) => Promise.resolve(hours < 12 ? 9 : 4))
})
afterEach(() => {
  wrapper?.unmount()
  notices.stop()
})
async function open() {
  wrapper = mountInApp(TimeGap, { props: { count: 6, reload } })
  await wrapper.get('[data-action="event-gap"]').trigger('click')
  await flushPromises()
  return new DOMWrapper(document.body)
}

it('previews a draft without writing and applies whole hours with Undo', async () => {
  const body = await open()
  const slider = body.get('[role="slider"]')
  expect(slider.attributes('aria-valuemin')).toBe('1')
  expect(slider.attributes('aria-valuemax')).toBe('72')
  expect(slider.attributes('aria-valuenow')).toBe('12')
  expect(body.get('[data-gap-preview]').text()).toContain(t('events.groupCount', { n: '6' }))
  expect(body.get('[data-gap-preview]').text()).toContain(t('events.previewSame'))
  await slider.trigger('keydown', { key: 'Home' })
  await vi.waitFor(() => expect(body.get('[data-gap-preview]').text()).toContain(t('events.previewMore')))
  expect(body.get('[data-gap-preview]').text()).toContain(t('events.groupCount', { n: '9' }))
  expect(engine.eventCount).toHaveBeenCalledExactlyOnceWith(1, expect.any(AbortSignal))
  expect(lib.saveEventGap).not.toHaveBeenCalled()
  const apply = body.findAll('button').find((b) => b.text() === t('events.gapApply'))
  await apply?.trigger('click')
  await flushPromises()
  expect(lib.saveEventGap).toHaveBeenCalledWith(1)
  expect(reload).toHaveBeenCalledOnce()
  notices.find('events.gap')?.actions?.[0]?.run()
  await flushPromises()
  expect(lib.saveEventGap).toHaveBeenLastCalledWith(12)
})

it('retains a failed draft for retry and never announces a failed save as success', async () => {
  lib.saveEventGap.mockImplementation(() => {
    lib.settingErrors.eventGapHours = 'Library is busy'
    return Promise.resolve()
  })
  const body = await open()
  await body.get('[role="slider"]').trigger('keydown', { key: 'End' })
  await body
    .findAll('button')
    .find((b) => b.text() === t('events.gapApply'))
    ?.trigger('click')
  await flushPromises()
  expect(lib.saveEventGap).toHaveBeenCalledWith(72)
  expect(body.get('[role="alert"]').text()).toContain('Library is busy')
  expect(body.get('[role="slider"]').attributes('aria-valuenow')).toBe('72')
  expect(notices.find('events.gap')).toBeUndefined()
  expect(reload).not.toHaveBeenCalled()
})

it('cancels a draft on Escape and restores the saved gap when reopened', async () => {
  const body = await open()
  await body.get('[role="slider"]').trigger('keydown', { key: 'End' })
  await body.get('[role="slider"]').trigger('keydown', { key: 'Escape' })
  await flushPromises()
  await wrapper?.get('[data-action="event-gap"]').trigger('click')
  await flushPromises()
  expect(body.get('[role="slider"]').attributes('aria-valuenow')).toBe('12')
  expect(lib.saveEventGap).not.toHaveBeenCalled()
})

it('keeps Undo available after a saved gap fails to refresh and retries only the read', async () => {
  reload.mockRejectedValueOnce(new Error('refresh failed'))
  const body = await open()
  await body.get('[role="slider"]').trigger('keydown', { key: 'End' })
  await body
    .findAll('button')
    .find((b) => b.text() === t('events.gapApply'))
    ?.trigger('click')
  await flushPromises()
  expect(lib.settings.eventGapHours).toBe(72)
  expect(notices.find('events.gap')?.actions).toHaveLength(1)
  await body.get('[role="alert"] button').trigger('click')
  await flushPromises()
  expect(reload).toHaveBeenCalledTimes(2)
  expect(lib.saveEventGap).toHaveBeenCalledTimes(1)
  notices.find('events.gap')?.actions?.[0]?.run()
  await flushPromises()
  expect(lib.saveEventGap).toHaveBeenLastCalledWith(12)
})

it('counts only the draft the slider rests on and cancels a superseded count', async () => {
  const signals: AbortSignal[] = []
  vi.mocked(engine.eventCount).mockImplementation((_hours, signal) => {
    if (signal) signals.push(signal)
    return new Promise<number>(() => undefined)
  })
  vi.useFakeTimers()
  try {
    const body = await open()
    const slider = body.get('[role="slider"]')
    await slider.trigger('keydown', { key: 'ArrowRight' })
    await slider.trigger('keydown', { key: 'ArrowRight' })
    await vi.advanceTimersByTimeAsync(250)
    expect(engine.eventCount).toHaveBeenCalledExactlyOnceWith(14, expect.any(AbortSignal))
    expect(body.get('[data-gap-preview]').text()).toContain(t('events.previewCounting'))
    await slider.trigger('keydown', { key: 'ArrowRight' })
    await vi.advanceTimersByTimeAsync(250)
    expect(signals.map((signal) => signal.aborted)).toEqual([true, false])
    expect(engine.eventCount).toHaveBeenLastCalledWith(15, expect.any(AbortSignal))
  } finally {
    vi.useRealTimers()
  }
})

it('says why a preview could not be counted and counts it again on request', async () => {
  vi.mocked(engine.eventCount).mockRejectedValueOnce(new Error('library busy'))
  const body = await open()
  await body.get('[role="slider"]').trigger('keydown', { key: 'End' })
  await vi.waitFor(() => expect(body.get('[data-gap-preview] [role="alert"]').text()).toContain('library busy'))
  await body.get('[data-gap-preview] button').trigger('click')
  await vi.waitFor(() => expect(body.get('[data-gap-preview]').text()).toContain(t('events.previewFewer')))
  expect(engine.eventCount).toHaveBeenCalledTimes(2)
})
