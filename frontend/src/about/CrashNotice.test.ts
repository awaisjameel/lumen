import { afterEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { enableAutoUnmount, flushPromises } from '@vue/test-utils'
import { engine } from '../bridge'
import type * as Bridge from '../bridge'
import { mountInApp, recordNotifications } from '../shared/testing'
import { CRASH_NOTIFICATION } from './store'
import CrashNotice from './CrashNotice.vue'

vi.mock('../bridge', async (original) => ({
  ...(await original<typeof Bridge>()),
  engine: {
    about: vi.fn<typeof Bridge.engine.about>(),
    saveProblemReport: vi.fn<typeof Bridge.engine.saveProblemReport>(),
  },
}))
enableAutoUnmount(afterEach)
it('uses the crash icon and offers a specific retry after a failed local save', async () => {
  setActivePinia(createPinia())
  const notices = recordNotifications()
  try {
    vi.mocked(engine.about).mockResolvedValue({
      version: '0.13.0',
      dataDir: 'D:/Scratch',
      releasesURL: '',
      lastRunCrashed: true,
    })
    vi.mocked(engine.saveProblemReport).mockRejectedValueOnce(new Error('Drive unavailable'))
    mountInApp(CrashNotice)
    await flushPromises()
    expect(notices.find(CRASH_NOTIFICATION)?.icon).toBe('bug')
    notices.find(CRASH_NOTIFICATION)?.actions?.[0]?.run()
    await flushPromises()
    expect(notices.find(CRASH_NOTIFICATION)?.actions?.[0]?.label).toBe('Try saving again…')
    expect(notices.find(CRASH_NOTIFICATION)?.duration).toBe(0)
  } finally {
    notices.stop()
  }
})
