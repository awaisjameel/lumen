import { beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises } from '@vue/test-utils'
import { engine, onUpdateStatus, UpdateChannel, UpdateFailure, UpdatePhase } from '../bridge'
import type * as Bridge from '../bridge'
import { useUpdates } from './store'

vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  onUpdateStatus: vi.fn<typeof Bridge.onUpdateStatus>(() => vi.fn()),
  engine: {
    updateStatus: vi.fn<typeof Bridge.engine.updateStatus>(),
    setUpdatePolicy: vi.fn<typeof Bridge.engine.setUpdatePolicy>(),
    checkUpdates: vi.fn<typeof Bridge.engine.checkUpdates>(),
    downloadUpdate: vi.fn<typeof Bridge.engine.downloadUpdate>(),
    cancelUpdate: vi.fn<typeof Bridge.engine.cancelUpdate>(),
    discardUpdate: vi.fn<typeof Bridge.engine.discardUpdate>(),
    dismissUpdate: vi.fn<typeof Bridge.engine.dismissUpdate>(),
    restartUpdate: vi.fn<typeof Bridge.engine.restartUpdate>(),
  },
}))

function snapshot(generation = 1): Bridge.UpdateStatus {
  return {
    generation,
    policy: { automatic: false, channel: UpdateChannel.UpdateStable, deferBattery: true, bandwidthKB: 0 },
    phase: UpdatePhase.UpdateIdle,
    installedVersion: '0.11.0',
    configured: true,
    managed: true,
    coordinator: true,
    lastCheck: 0,
    offer: { version: '', notes: '', downloadBytes: 0, expandedBytes: 0 },
    bytes: 0,
    error: UpdateFailure.$zero,
    dismissed: '',
  }
}

beforeEach(() => {
  setActivePinia(createPinia())
  vi.clearAllMocks()
  vi.mocked(engine.updateStatus).mockResolvedValue(snapshot())
  vi.mocked(engine.setUpdatePolicy).mockResolvedValue()
  vi.mocked(engine.checkUpdates).mockResolvedValue()
  vi.mocked(engine.cancelUpdate).mockResolvedValue()
  vi.mocked(engine.dismissUpdate).mockResolvedValue()
})

it('subscribes once and ignores a late snapshot older than an event', async () => {
  let resolve: ((s: Bridge.UpdateStatus) => void) | undefined
  vi.mocked(engine.updateStatus).mockReturnValue(
    new Promise((done) => {
      resolve = done
    }),
  )
  const store = useUpdates()
  const first = store.start()
  const second = store.start()
  await flushPromises()
  const callback = vi.mocked(onUpdateStatus).mock.calls[0]?.[0]
  callback?.({ ...snapshot(3), phase: UpdatePhase.UpdateReady })
  resolve?.(snapshot(2))
  await Promise.all([first, second])
  expect(onUpdateStatus).toHaveBeenCalledTimes(1)
  expect(store.status?.generation).toBe(3)
  expect(store.status?.phase).toBe(UpdatePhase.UpdateReady)
})

it('retries an initial status failure and removes the failed subscription', async () => {
  const stop = vi.fn()
  vi.mocked(onUpdateStatus).mockReturnValueOnce(stop)
  vi.mocked(engine.updateStatus).mockRejectedValueOnce(new Error('Status is unavailable.'))
  const store = useUpdates()
  await store.start()
  expect(store.error).toContain('Status is unavailable.')
  expect(stop).toHaveBeenCalledOnce()
  await store.start()
  expect(store.status?.phase).toBe(UpdatePhase.UpdateIdle)
  expect(store.error).toBe('')
  expect(onUpdateStatus).toHaveBeenCalledTimes(2)
})

it('allows cancellation while a manual check is still waiting', async () => {
  const store = useUpdates()
  await store.start()
  let resolve: (() => void) | undefined
  vi.mocked(engine.checkUpdates).mockReturnValue(
    new Promise((done) => {
      resolve = done
    }),
  )
  const checking = store.check()
  expect(store.busy).toBe(true)
  await store.cancel()
  expect(engine.cancelUpdate).toHaveBeenCalledOnce()
  resolve?.()
  await checking
})

it('orders policy patches and keeps the unsaved channel after a failed save', async () => {
  const store = useUpdates()
  await store.start()
  vi.mocked(engine.setUpdatePolicy).mockRejectedValueOnce(new Error('write refused'))
  const failed = store.setPolicy({ channel: UpdateChannel.UpdatePreview })
  const next = store.setPolicy({ automatic: true })
  await Promise.all([failed, next])
  expect(engine.setUpdatePolicy).toHaveBeenNthCalledWith(2, { ...snapshot().policy, automatic: true })
  expect(store.status?.policy.channel).toBe(UpdateChannel.UpdateStable)
  expect(store.saving).toBe(false)
})

it('dismisses a notice while a download request is still waiting', async () => {
  const store = useUpdates()
  await store.start()
  let resolve: (() => void) | undefined
  vi.mocked(engine.downloadUpdate).mockReturnValue(
    new Promise((done) => {
      resolve = done
    }),
  )
  const downloading = store.download()
  await store.dismiss()
  expect(engine.dismissUpdate).toHaveBeenCalledOnce()
  resolve?.()
  await downloading
})

it('offers Cancel for a check or a download, and not while the restart commits', async () => {
  const store = useUpdates()
  await store.start()
  const callback = vi.mocked(onUpdateStatus).mock.calls[0]?.[0]
  callback?.({ ...snapshot(2), phase: UpdatePhase.UpdateDownloading })
  expect([store.busy, store.cancellable]).toEqual([true, true])
  callback?.({ ...snapshot(3), phase: UpdatePhase.UpdateQuiescing })
  expect([store.busy, store.cancellable]).toEqual([true, false])
  callback?.({ ...snapshot(4), phase: UpdatePhase.UpdateReady })
  expect([store.busy, store.cancellable]).toEqual([false, false])
})
