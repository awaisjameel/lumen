import { beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import { engine, ErrorCode, Link } from '../bridge'
import type * as Bridge from '../bridge'
import { t } from '../shared/i18n'
import AboutSection from './AboutSection.vue'
import CrashNotice from './CrashNotice.vue'
import { useAbout } from './store'

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

function about(lastRunCrashed = false): Bridge.About {
  return { version: '0.9.0', dataDir: 'C:/Lumen', releasesURL: RELEASES, lastRunCrashed }
}

beforeEach(() => {
  setActivePinia(createPinia())
  vi.mocked(engine.about).mockReset().mockResolvedValue(about())
  vi.mocked(engine.openLink).mockReset().mockResolvedValue()
  vi.mocked(engine.openNotices).mockReset().mockResolvedValue()
  vi.mocked(engine.openLogFolder).mockReset().mockResolvedValue()
  vi.mocked(engine.saveProblemReport).mockReset().mockResolvedValue('')
})

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
  const section = mount(AboutSection)
  await flushPromises()
  expect(section.get('[role="alert"]').text()).toContain('about unavailable')
  await useAbout().load()
  expect(section.find('[role="alert"]').exists()).toBe(false)
  expect(section.text()).toContain('Version 0.9.0')
  section.unmount()
})

it('shows the version and where new ones are, opening it only when clicked', async () => {
  const section = mount(AboutSection)
  await flushPromises()
  expect(section.text()).toContain('Version 0.9.0')
  expect(engine.openLink).not.toHaveBeenCalled()
  const link = section.findAll('a').find((a) => a.text() === RELEASES)
  if (!link) throw new Error('no releases link')
  await link.trigger('click')
  expect(engine.openLink).toHaveBeenCalledExactlyOnceWith(Link.LinkReleases)
  const credit = section.findAll('a').find((a) => a.text() === 'GeoNames')
  await credit?.trigger('click')
  expect(engine.openLink).toHaveBeenLastCalledWith(Link.LinkGeoNames)
})

it('saves a problem report where the user chose, and says so', async () => {
  vi.mocked(engine.saveProblemReport).mockResolvedValue('C:/Users/me/Desktop/Lumen problem report.zip')
  const section = mount(AboutSection)
  const save = section.findAll('button').find((b) => b.text() === t('about.saveReport'))
  await save?.trigger('click')
  await flushPromises()
  expect(section.get('[role="status"]').text()).toBe('Saved to C:/Users/me/Desktop/Lumen problem report.zip')
})

it('says nothing when the user cancels, and why when a call fails', async () => {
  const section = mount(AboutSection)
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
  const notice = mount(CrashNotice)
  await flushPromises()
  expect(notice.find('.crash').exists()).toBe(false)
})

it('says once that the last run crashed, offers a report, and goes when dismissed', async () => {
  vi.mocked(engine.about).mockResolvedValue(about(true))
  vi.mocked(engine.saveProblemReport).mockResolvedValue('C:/report.zip')
  const notice = mount(CrashNotice)
  await flushPromises()
  expect(notice.get('.crash').text()).toContain('Lumen closed unexpectedly last time.')
  await notice.get('.crash .btn').trigger('click')
  await flushPromises()
  expect(notice.get('[role="status"]').text()).toBe('Saved to C:/report.zip')
  await notice.get('.icon-btn').trigger('click')
  expect(notice.find('.crash').exists()).toBe(false)
  // The details are read once, for the notice and the About section alike.
  mount(AboutSection)
  await flushPromises()
  expect(engine.about).toHaveBeenCalledOnce()
})
