import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { errorText } from './errors'
import { t } from './i18n'
import { recordNotifications } from './testing'
import type { RecordedNotifications } from './testing'
import { offerBatchUndo, undoOutcomes } from './undo'
import type { UndoJob, UndoResult } from './undo'

const range = (n: number) => Array.from({ length: n }, (_, i) => i + 1)
const done = (ids: number[]): UndoResult[] => ids.map((id) => ({ id, state: 'done', error: '' }))
let notifications: RecordedNotifications
beforeEach(() => {
  notifications = recordNotifications()
})
afterEach(() => {
  for (const notice of notifications.shown) notice.closed?.()
  undoOutcomes.value = null
  notifications.stop()
})

it('runs one bounded unit at a time, coalesces duplicate activation and resumes only cancelled targets', async () => {
  let finish: (rows: UndoResult[]) => void = () => undefined
  const apply = vi
    .fn<(ids: number[]) => Promise<UndoResult[]>>()
    .mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve
        }),
    )
    .mockImplementation((ids) => Promise.resolve(done(ids)))
  const reconcile = vi.fn().mockResolvedValue(undefined)
  const job = offerBatchUndo({ title: 'Saved', undoTitle: 'Undo', ids: range(500), apply, reconcile })
  const first = job.run()
  expect(job.run()).toBe(first)
  expect(apply).toHaveBeenCalledOnce()
  expect(apply.mock.calls[0]?.[0]).toEqual(range(200))
  job.cancel()
  finish(done(range(200)))
  await first
  expect(job.counts).toMatchObject({ done: 200, cancelled: 300, pending: 0 })
  expect(reconcile).toHaveBeenCalledOnce()
  const notice = notifications.shown.at(-1)
  expect(notice?.duration).toBe(0)
  expect(notice?.actions?.map((a) => a.label)).toContain(t('undo.resume'))
  await job.resume()
  expect(apply.mock.calls.map(([ids]) => ids.length)).toEqual([200, 200, 100])
  expect(apply.mock.calls.flatMap(([ids]) => ids)).toEqual(range(500))
  expect(job.counts).toMatchObject({ done: 500, cancelled: 0, pending: 0 })
  await job.run()
  expect(apply).toHaveBeenCalledTimes(3)
})

it('retains successes and superseded edits while retrying failed rows only', async () => {
  const apply = vi
    .fn<(ids: number[]) => Promise<UndoResult[]>>()
    .mockResolvedValueOnce([
      { id: 1, state: 'done', error: '' },
      { id: 2, state: 'failed', error: 'busy' },
      { id: 3, state: 'superseded', error: '' },
    ])
    .mockImplementation((ids) => Promise.resolve(done(ids)))
  const job = offerBatchUndo({
    title: 'Saved',
    undoTitle: 'Undo',
    ids: [1, 2, 3],
    apply,
    reconcile: () => Promise.resolve(),
  })
  await job.run()
  expect(job.counts).toMatchObject({ done: 1, failed: 1, superseded: 1 })
  await job.retry()
  expect(apply.mock.calls.map(([ids]) => ids)).toEqual([[1, 2, 3], [2]])
  expect(job.counts).toMatchObject({ done: 2, failed: 0, superseded: 1 })
})

it('reconciles prior commits after a later call fails and retries only that unit', async () => {
  const apply = vi
    .fn<(ids: number[]) => Promise<UndoResult[]>>()
    .mockImplementationOnce((ids) => Promise.resolve(done(ids)))
    .mockRejectedValueOnce(new Error('busy'))
    .mockImplementation((ids) => Promise.resolve(done(ids)))
  const reconcile = vi.fn().mockResolvedValue(undefined)
  const job = offerBatchUndo({ title: 'Saved', undoTitle: 'Undo', ids: range(450), apply, reconcile })
  await job.run()
  expect(job.counts).toMatchObject({ done: 250, failed: 200 })
  expect(job.outcomes.find((row) => row.id === 201)?.error).toBe(
    t('undo.unconfirmed', { error: errorText(new Error('busy')) }),
  )
  expect(reconcile).toHaveBeenCalledOnce()
  await job.retry()
  expect(apply.mock.calls.at(-1)?.[0]).toEqual(range(400).slice(200))
  expect(job.counts).toMatchObject({ done: 450, failed: 0 })
})

it('keeps refresh failure separate and retries the read without replaying a committed inverse', async () => {
  const apply = vi.fn((ids: number[]) => Promise.resolve(done(ids)))
  const reconcile = vi.fn().mockRejectedValueOnce(new Error('read failed')).mockResolvedValue(undefined)
  const job = offerBatchUndo({ title: 'Saved', undoTitle: 'Undo', ids: [1], apply, reconcile })
  await job.run()
  expect(job.counts.done).toBe(1)
  expect(job.counts.failed).toBe(0)
  expect(job.refreshError).toContain('read failed')
  expect(notifications.shown.at(-1)?.actions?.map((a) => a.label)).toContain(t('undo.refresh'))
  await job.refresh()
  expect(apply).toHaveBeenCalledOnce()
  expect(reconcile).toHaveBeenCalledTimes(2)
  expect(job.refreshError).toBe('')
})

it('refuses expired offers and releases ownership once', async () => {
  const apply = vi.fn((ids: number[]) => Promise.resolve(done(ids)))
  const release = vi.fn()
  const job = offerBatchUndo({
    title: 'Saved',
    undoTitle: 'Undo',
    ids: [1],
    apply,
    reconcile: () => Promise.resolve(),
    release,
  })
  const notice = notifications.shown.at(-1)
  notice?.closed?.()
  notice?.closed?.()
  await job.run()
  expect(apply).not.toHaveBeenCalled()
  expect(release).toHaveBeenCalledOnce()
  expect(job.available).toBe(false)
})

it('stops future units when dismissed during a write but reconciles the completed unit', async () => {
  let finish: (rows: UndoResult[]) => void = () => undefined
  const apply = vi.fn(
    () =>
      new Promise<UndoResult[]>((resolve) => {
        finish = resolve
      }),
  )
  const reconcile = vi.fn().mockResolvedValue(undefined)
  const job = offerBatchUndo({ title: 'Saved', undoTitle: 'Undo', ids: range(450), apply, reconcile })
  const pending = job.run()
  notifications.shown.at(-1)?.closed?.()
  finish(done(range(200)))
  await pending
  expect(apply).toHaveBeenCalledOnce()
  expect(reconcile).toHaveBeenCalledOnce()
  expect(job.counts).toMatchObject({ done: 200, cancelled: 250 })
  expect(job.available).toBe(false)
})

it('reports missing results as failures without claiming unreported work succeeded', async () => {
  const job = offerBatchUndo({
    title: 'Saved',
    undoTitle: 'Undo',
    ids: [1, 2],
    apply: () => Promise.resolve(done([1, 99])),
    reconcile: () => Promise.resolve(),
  })
  await job.run()
  expect(job.outcomes).toEqual([
    { id: 1, state: 'done', error: '' },
    { id: 2, state: 'failed', error: t('error.unexpected') },
  ])
})

it('opens live outcomes without cancelling the operation', async () => {
  const job = offerBatchUndo({
    title: 'Saved',
    undoTitle: 'Undo',
    ids: [1],
    apply: (ids) => Promise.resolve(done(ids)),
    reconcile: () => Promise.resolve(),
  })
  job.show()
  expect(undoOutcomes.value).toBe(job)
  await job.run()
  await flushPromises()
  expect(undoOutcomes.value?.counts.done).toBe(1)
})

it('pages composite targets through cancellable single-item units while retaining their labels on resume', async () => {
  const ids = ['album:8:asset:1', 'album:9:asset:1', 'album:9:asset:2']
  const apply = vi.fn((part: string[]) => {
    job.cancel()
    return Promise.resolve(part.map((id) => ({ id, state: 'done' as const, error: '' })))
  })
  const job: UndoJob<string> = offerBatchUndo({
    title: 'Saved',
    undoTitle: 'Memberships',
    ids,
    unitSize: 1,
    label: (id) => `Original ${id}`,
    apply,
    reconcile: () => Promise.resolve(),
  })
  await job.run()
  expect(apply).toHaveBeenCalledExactlyOnceWith([ids[0]], expect.any(Function))
  expect(job.counts).toMatchObject({ done: 1, cancelled: 2 })
  apply.mockImplementation((part) => Promise.resolve(part.map((id) => ({ id, state: 'done', error: '' }))))
  await job.resume()
  expect(apply.mock.calls.map(([part]) => part)).toEqual([[ids[0]], [ids[1]], [ids[2]]])
  expect(job.outcomes.map((row) => row.label)).toEqual(ids.map((id) => `Original ${id}`))
})
