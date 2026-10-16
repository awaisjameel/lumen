import { afterEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { enableAutoUnmount, flushPromises } from '@vue/test-utils'
import { UpdateChannel, UpdateFailure, UpdatePhase } from '../bridge'
import { mountInApp, recordNotifications } from '../shared/testing'
import { useUpdates } from './store'
import UpdateNotice from './UpdateNotice.vue'

enableAutoUnmount(afterEach)
it('announces an already-known offer and replaces it with the ready notice under the same identity', async () => {
  setActivePinia(createPinia())
  const updates = useUpdates()
  vi.spyOn(updates, 'start').mockResolvedValue()
  updates.status = {
    generation: 1,
    policy: { automatic: false, channel: UpdateChannel.UpdateStable, deferBattery: true, bandwidthKB: 0 },
    phase: UpdatePhase.UpdateAvailable,
    installedVersion: '0.12.0',
    configured: true,
    managed: true,
    coordinator: true,
    lastCheck: 0,
    offer: { version: '0.13.0', notes: '', downloadBytes: 10, expandedBytes: 20 },
    bytes: 0,
    error: UpdateFailure.$zero,
    dismissed: '',
  }
  const notices = recordNotifications()
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/settings', name: 'settings', component: { render: () => null } }],
  })
  await router.push('/settings')
  try {
    mountInApp(UpdateNotice, { plugins: [router] })
    await flushPromises()
    expect(notices.find('update')).toMatchObject({ icon: 'update', duration: 0 })
    updates.status = { ...updates.status, generation: 2, phase: UpdatePhase.UpdateReady }
    await flushPromises()
    expect(notices.shown).toHaveLength(1)
    expect(notices.find('update')?.title).toContain('ready')
    notices.find('update')?.actions?.[0]?.run()
    await flushPromises()
    expect(router.currentRoute.value.query.section).toBe('updates')
  } finally {
    notices.stop()
  }
})
