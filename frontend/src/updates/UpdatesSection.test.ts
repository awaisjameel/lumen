import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import type { Pinia } from 'pinia'
import { flushPromises } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import type { Component } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import { engine, onUpdateStatus, UpdateChannel, UpdateFailure, UpdatePhase } from '../bridge'
import type * as Bridge from '../bridge'
import { t } from '../shared/i18n'
import { mountInApp, recordNotifications } from '../shared/testing'
import type { RecordedNotifications } from '../shared/testing'
import UpdateNotice from './UpdateNotice.vue'
import UpdatesSection from './UpdatesSection.vue'

vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  onUpdateStatus: vi.fn<typeof Bridge.onUpdateStatus>(() => vi.fn()),
  engine: {
    updateStatus: vi.fn<typeof Bridge.engine.updateStatus>(),
    dismissUpdate: vi.fn<typeof Bridge.engine.dismissUpdate>(),
  },
}))

function snapshot(patch: Partial<Bridge.UpdateStatus> = {}): Bridge.UpdateStatus {
  return {
    generation: 1,
    policy: { automatic: false, channel: UpdateChannel.UpdateStable, deferBattery: true, bandwidthKB: 0 },
    phase: UpdatePhase.UpdateIdle,
    installedVersion: '0.12.0',
    configured: true,
    managed: true,
    coordinator: true,
    lastCheck: 0,
    offer: { version: '', notes: '', downloadBytes: 0, expandedBytes: 0 },
    bytes: 0,
    error: UpdateFailure.$zero,
    dismissed: '',
    ...patch,
  }
}

let pinia: Pinia
let wrapper: VueWrapper | undefined
let notified: RecordedNotifications

beforeEach(() => {
  pinia = createPinia()
  setActivePinia(pinia)
  vi.clearAllMocks()
  vi.mocked(engine.dismissUpdate).mockResolvedValue()
  notified = recordNotifications()
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  notified.stop()
})

async function show(component: Component, status: Bridge.UpdateStatus) {
  vi.mocked(engine.updateStatus).mockResolvedValue(status)
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/settings', name: 'settings', component: { render: () => null } }],
  })
  wrapper = mountInApp(component, { plugins: [pinia, router] })
  await flushPromises()
  return wrapper
}

// The actions offered, by their labels (switches and radio buttons carry none).
const actions = (w: VueWrapper) => w.findAll('button').filter((button) => button.text())
const buttons = (w: VueWrapper) => actions(w).map((button) => button.text())

it('offers no download for the release an update has just installed', async () => {
  const w = await show(UpdatesSection, snapshot({ phase: UpdatePhase.UpdateComplete }))
  expect(w.text()).toContain(t('update.phase.complete'))
  expect(buttons(w)).toEqual([t('update.check')])
})

it('keeps a restart that failed ready to try again, and says what happened', async () => {
  const w = await show(
    UpdatesSection,
    snapshot({
      phase: UpdatePhase.UpdateReady,
      error: UpdateFailure.UpdateFailureFailed,
      offer: { version: '0.13.0', notes: 'Fixes', downloadBytes: 10, expandedBytes: 20 },
    }),
  )
  expect(buttons(w)).toEqual([t('update.check'), t('update.restart'), t('update.later'), t('update.discard')])
  expect(w.get('[role="alert"]').text()).toContain(t('update.failed'))
})

it('explains an observing window without calling it a failure', async () => {
  const w = await show(
    UpdatesSection,
    snapshot({
      coordinator: false,
      error: UpdateFailure.UpdateFailureObserver,
      phase: UpdatePhase.UpdateDownloading,
      offer: { version: '0.13.0', notes: '', downloadBytes: 10, expandedBytes: 20 },
    }),
  )
  expect(w.text()).toContain(t('update.observer'))
  expect(w.find('[role="alert"]').exists()).toBe(false)
  expect(buttons(w)).not.toContain(t('common.cancel'))
  for (const button of actions(w)) expect(button.attributes('disabled')).toBeDefined()
})

it('announces a completed update once and records that it was seen', async () => {
  await show(UpdateNotice, snapshot({ phase: UpdatePhase.UpdateComplete }))
  expect(notified.find('update')?.description).toBe(t('update.completed', { version: '0.12.0' }))
  expect(engine.dismissUpdate).toHaveBeenCalledOnce()

  const callback = vi.mocked(onUpdateStatus).mock.calls[0]?.[0]
  callback?.(snapshot({ generation: 2, phase: UpdatePhase.UpdateComplete, dismissed: '0.12.0' }))
  await flushPromises()
  expect(engine.dismissUpdate).toHaveBeenCalledOnce()
})

it('leaves notices to the window that coordinates the installation', async () => {
  await show(
    UpdateNotice,
    snapshot({
      coordinator: false,
      phase: UpdatePhase.UpdateAvailable,
      offer: { version: '0.13.0', notes: '', downloadBytes: 10, expandedBytes: 20 },
    }),
  )
  expect(notified.find('update')).toBeUndefined()
})
