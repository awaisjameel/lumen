import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import { engine } from '../../bridge'
import type * as Bridge from '../../bridge'
import type { CityMatch } from '../../bridge'
import { t } from '../../shared/i18n'
import { mountInApp, recordNotifications } from '../../shared/testing'
import PlaceDialog from './PlaceDialog.vue'
import { asset, body, press } from './testing'

vi.mock('../../bridge', async (original) => ({
  ...(await original<typeof Bridge>()),
  engine: { setLocation: vi.fn(), restoreLocation: vi.fn(), findCities: vi.fn(), validateSelection: vi.fn() },
}))
let wrapper: VueWrapper | undefined
let notices: ReturnType<typeof recordNotifications>
const city: CityMatch = { id: 7, name: 'Karachi', region: 'Sindh', country: 'Pakistan', lat: 24.8, lon: 67 }
const latitude = () => body().get<HTMLInputElement>(`input[aria-label="${t('details.place.latitude')}"]`)
const longitude = () => body().get<HTMLInputElement>(`input[aria-label="${t('details.place.longitude')}"]`)
beforeEach(() => {
  notices = recordNotifications()
  vi.mocked(engine.setLocation).mockReset().mockResolvedValue(undefined)
  vi.mocked(engine.restoreLocation).mockReset().mockResolvedValue(undefined)
  vi.mocked(engine.findCities).mockReset().mockResolvedValue([city])
  vi.mocked(engine.validateSelection).mockReset().mockResolvedValue(undefined)
})
afterEach(() => {
  wrapper?.unmount()
  notices.stop()
  document.body.innerHTML = ''
  vi.useRealTimers()
})

it('finds a city offline, fills coordinates and saves them for the selection', async () => {
  wrapper = mountInApp(PlaceDialog, { props: { open: true, assets: [asset(), asset(2)] } })
  await flushPromises()
  await body().get('input[type="search"]').setValue('Kar')
  await vi.waitFor(() => expect(engine.findCities).toHaveBeenCalledWith('Kar', 20, expect.any(AbortSignal)))
  await flushPromises()
  await press('Karachi · Sindh · Pakistan')
  expect(latitude().element.value).toBe('24.8')
  expect(longitude().element.value).toBe('67')
  await body().get('form').trigger('submit')
  await flushPromises()
  expect(engine.setLocation).toHaveBeenCalledWith([1, 2], 24.8, 67)
  notices.find('correct-place')?.actions?.[0]?.run()
  await flushPromises()
  expect(engine.restoreLocation).toHaveBeenCalledWith([1])
  expect(engine.restoreLocation).toHaveBeenCalledWith([2])
})

it('rejects blank and out-of-range coordinates and accepts zero and range endpoints', async () => {
  wrapper = mountInApp(PlaceDialog, { props: { open: true, assets: [asset()] } })
  await flushPromises()
  for (const [lat, lon] of [
    ['', ''],
    ['91', '0'],
    ['0', '-181'],
  ]) {
    await latitude().setValue(lat)
    await longitude().setValue(lon)
    await body().get('form').trigger('submit')
    await flushPromises()
    expect(body().get('[role="alert"]').text()).toContain(t('details.place.invalid'))
  }
  expect(engine.setLocation).not.toHaveBeenCalled()
  await latitude().setValue('-90')
  await longitude().setValue('180')
  await body().get('form').trigger('submit')
  await flushPromises()
  expect(engine.setLocation).toHaveBeenCalledWith([1], -90, 180)
})

it('restores absent GPS and undoes to the previous correction', async () => {
  wrapper = mountInApp(PlaceDialog, {
    props: { open: true, assets: [{ ...asset(), lat: 0, lon: 0, originalLocation: { lat: null, lon: null } }] },
  })
  await flushPromises()
  await press(t('details.restoreOriginal'))
  expect(engine.restoreLocation).toHaveBeenCalledWith([1])
  notices.find('correct-place')?.actions?.[0]?.run()
  await flushPromises()
  expect(engine.setLocation).toHaveBeenCalledWith([1], 0, 0)
})

it('aborts obsolete city searches and never replaces newer matches with a late reply', async () => {
  let resolveOld: (value: CityMatch[]) => void = () => undefined
  vi.mocked(engine.findCities).mockReturnValueOnce(new Promise((resolve) => (resolveOld = resolve)))
  wrapper = mountInApp(PlaceDialog, { props: { open: true, assets: [asset()] } })
  await flushPromises()
  const search = body().get('input[type="search"]')
  await search.setValue('Old')
  await vi.waitFor(() => expect(engine.findCities).toHaveBeenCalledOnce())
  const signal = vi.mocked(engine.findCities).mock.calls[0]?.[2]
  await search.setValue('Kar')
  expect(signal?.aborted).toBe(true)
  await vi.waitFor(() => expect(engine.findCities).toHaveBeenCalledTimes(2))
  resolveOld([{ ...city, id: 8, name: 'Old city' }])
  await flushPromises()
  expect(body().text()).toContain('Karachi')
  expect(body().text()).not.toContain('Old city')
})

it('keeps coordinates after save failure and exposes recoverable city-search errors', async () => {
  vi.mocked(engine.findCities).mockRejectedValueOnce(new Error('Catalogue unavailable'))
  vi.mocked(engine.setLocation).mockRejectedValueOnce(new Error('Read-only library'))
  wrapper = mountInApp(PlaceDialog, { props: { open: true, assets: [asset()] } })
  await flushPromises()
  await body().get('input[type="search"]').setValue('Kar')
  await vi.waitFor(() => expect(body().text()).toContain('Catalogue unavailable'))
  await latitude().setValue('0')
  await longitude().setValue('0')
  await body().get('form').trigger('submit')
  await flushPromises()
  expect(body().get('[role="alert"]').text()).toContain('Read-only library')
  expect(latitude().element.value).toBe('0')
  expect(notices.shown).toHaveLength(0)
})
