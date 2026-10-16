import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { enableAutoUnmount, flushPromises } from '@vue/test-utils'
import { mountInApp, recordNotifications } from '../shared/testing'
import type { RecordedNotifications } from '../shared/testing'
import { engine, ErrorCode, Link } from '../bridge'
import type * as Bridge from '../bridge'
import { t } from '../shared/i18n'
import AboutSection from './AboutSection.vue'
import CrashNotice from './CrashNotice.vue'
import { CRASH_NOTIFICATION, useAbout } from './store'

enableAutoUnmount(afterEach)

vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  engine: {
    about: vi.fn<() => Promise<Bridge.About>>(),
    openLink: vi.fn<(link: Bridge.Link) => Promise<void>>(),
    openNotices: vi.fn<() => Promise<void>>(),
    openLogFolder: vi.fn<() => Promise<void>>(),
    saveProblemReport: vi.fn<() => Promise<string>>(),
  },
}))

const RELEASES = 'https://github.com/awaisjameel/lumen/releases'
let notifications: RecordedNotifications

function about(lastRunCrashed = false): Bridge.About {
  return { version: '0.9.0', dataDir: 'C:/Lumen', releasesURL: RELEASES, lastRunCrashed }
}

beforeEach(() => {
  notifications = recordNotifications()
  setActivePinia(createPinia())
  vi.mocked(engine.about).mockReset().mockResolvedValue(about())
  vi.mocked(engine.openLink).mockReset().mockResolvedValue()
  vi.mocked(engine.openNotices).mockReset().mockResolvedValue()
  vi.mocked(engine.openLogFolder).mockReset().mockResolvedValue()
  vi.mocked(engine.saveProblemReport).mockReset().mockResolvedValue('')
})
afterEach(() => notifications.stop())

it('shares one report save across callers and remains busy until it finishes', async () => {
  let finish: ((path: string) => void) | undefined
  vi.mocked(engine.saveProblemReport).mockImplementation(() => new Promise<string>((resolve) => (finish = resolve)))
  const store = useAbout()
  const first = store.saveReport()
  const second = store.saveReport()
  expect(engine.saveProblemReport).toHaveBeenCalledOnce()
  expect(store.savingReport).toBe(true)
  finish?.('report.zip')
  expect(await first).toBe('report.zip')
  expect(await second).toBe('report.zip')
  expect(store.savingReport).toBe(false)
})

it('shows an About read failure and retries on the next load', async () => {
  vi.mocked(engine.about).mockRejectedValueOnce(new Error('about unavailable'))
  const section = mountInApp(AboutSection)
  await flushPromises()
  expect(section.get('[role="alert"]').text()).toContain('about unavailable')
  await useAbout().load()
  expect(section.find('[role="alert"]').exists()).toBe(false)
  expect(section.text()).toContain('Version 0.9.0')
  section.unmount()
})

it('shows the version and where new ones are, opening it only when clicked', async () => {
  const section = mountInApp(AboutSection)
  await flushPromises()
  expect(section.text()).toContain('Version 0.9.0')
  expect(engine.openLink).not.toHaveBeenCalled()
  const link = section.findAll('button').find((a) => a.text() === t('about.viewReleases'))
  if (!link) throw new Error('no releases link')
  await link.trigger('click')
  expect(engine.openLink).toHaveBeenCalledExactlyOnceWith(Link.LinkReleases)
  const credit = section.findAll('button').find((a) => a.text() === 'GeoNames')
  await credit?.trigger('click')
  expect(engine.openLink).toHaveBeenLastCalledWith(Link.LinkGeoNames)
})

it('saves a problem report where the user chose, and says so', async () => {
  vi.mocked(engine.saveProblemReport).mockResolvedValue('C:/Users/me/Desktop/Lumen problem report.zip')
  const section = mountInApp(AboutSection)
  const save = section.findAll('button').find((b) => b.text() === t('about.saveReport'))
  await save?.trigger('click')
  await flushPromises()
  expect(section.get('[role="status"]').text()).toBe('Saved to C:/Users/me/Desktop/Lumen problem report.zip')
})

it('says nothing when the user cancels, and why when a call fails', async () => {
  const section = mountInApp(AboutSection)
  const button = (text: string) => section.findAll('button').find((b) => b.text().includes(text))
  await button(t('about.saveReport'))?.trigger('click')
  await flushPromises()
  expect(section.find('[role="status"]').exists()).toBe(false)
  vi.mocked(engine.openNotices).mockRejectedValue(
    new Error('the licence notices are not beside Lumen.exe', { cause: { code: ErrorCode.NotFound } }),
  )
  await button(t('about.licences'))?.trigger('click')
  await flushPromises()
  expect(section.get('[role="alert"]').text()).toBe(t('error.notFound'))
  await button(t('about.openLogs'))?.trigger('click')
  await flushPromises()
  expect(engine.openLogFolder).toHaveBeenCalledOnce()
  expect(section.find('[role="alert"]').exists()).toBe(false) // a new action clears the old failure
})

it('says nothing after a clean run', async () => {
  mountInApp(CrashNotice)
  await flushPromises()
  expect(notifications.find(CRASH_NOTIFICATION)).toBeUndefined()
})

it('says once that the last run crashed, offers a report, and goes when dismissed', async () => {
  vi.mocked(engine.about).mockResolvedValue(about(true))
  vi.mocked(engine.saveProblemReport).mockResolvedValue('C:/report.zip')
  mountInApp(CrashNotice)
  await flushPromises()
  expect(notifications.find(CRASH_NOTIFICATION)?.title).toBe('Lumen closed unexpectedly last time.')
  expect(notifications.find(CRASH_NOTIFICATION)?.duration).toBe(0)
  notifications.find(CRASH_NOTIFICATION)?.actions?.[0]?.run()
  await flushPromises()
  expect(notifications.find(CRASH_NOTIFICATION)?.description).toBe('Saved to C:/report.zip')
  notifications.find(CRASH_NOTIFICATION)?.closed?.()
  await flushPromises()
  expect(notifications.find(CRASH_NOTIFICATION)).toBeUndefined()
  // The details are read once, for the notice and the About section alike.
  mountInApp(AboutSection)
  await flushPromises()
  expect(engine.about).toHaveBeenCalledOnce()
})

it('keeps the crash notice through report cancellation and failure, and allows a retry', async () => {
  vi.mocked(engine.about).mockResolvedValue(about(true))
  mountInApp(CrashNotice)
  await flushPromises()
  notifications.find(CRASH_NOTIFICATION)?.actions?.[0]?.run()
  await flushPromises()
  expect(notifications.find(CRASH_NOTIFICATION)?.description).toBe(t('about.crashedHint'))
  vi.mocked(engine.saveProblemReport).mockRejectedValueOnce(new Error('report unavailable'))
  notifications.find(CRASH_NOTIFICATION)?.actions?.[0]?.run()
  await flushPromises()
  expect(notifications.find(CRASH_NOTIFICATION)?.description).toContain('report unavailable')
  vi.mocked(engine.saveProblemReport).mockResolvedValueOnce('C:/retry.zip')
  notifications.find(CRASH_NOTIFICATION)?.actions?.[0]?.run()
  await flushPromises()
  expect(notifications.find(CRASH_NOTIFICATION)?.description).toBe('Saved to C:/retry.zip')
})

it('keeps the report action disabled while saving and does not revive a dismissed crash notice', async () => {
  vi.mocked(engine.about).mockResolvedValue(about(true))
  let finish: ((path: string) => void) | undefined
  vi.mocked(engine.saveProblemReport).mockReturnValueOnce(new Promise((resolve) => (finish = resolve)))
  mountInApp(CrashNotice)
  await flushPromises()
  notifications.find(CRASH_NOTIFICATION)?.actions?.[0]?.run()
  await flushPromises()
  const action = notifications.find(CRASH_NOTIFICATION)?.actions?.[0]
  expect(action?.disabled).toBe(true)
  expect(action?.dismiss).toBe(false)
  action?.run()
  expect(engine.saveProblemReport).toHaveBeenCalledOnce()
  notifications.find(CRASH_NOTIFICATION)?.closed?.()
  await flushPromises()
  finish?.('C:/late.zip')
  await flushPromises()
  expect(notifications.find(CRASH_NOTIFICATION)).toBeUndefined()
})
