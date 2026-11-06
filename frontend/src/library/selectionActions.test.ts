import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { effectScope } from 'vue'
import { BatchAction, engine } from '../bridge'
import type * as Bridge from '../bridge'
import type { BatchOutcome, GridItem } from '../bridge'
import { recordNotifications } from '../shared/testing'
import type { RecordedNotifications } from '../shared/testing'
import { useFavouriteWrites } from '../shared/favouriteWrites'
import { undoOutcomes } from '../shared/undo'
import { t } from '../shared/i18n'
import { useLibrary } from './store'
import { useSelectionActions } from './selectionActions'

vi.mock('../bridge', async (original) => ({
  ...(await original<typeof Bridge>()),
  engine: {
    applyBatch: vi.fn(),
    setFavourite: vi.fn(),
    searchHits: vi.fn(),
    counts: vi.fn(),
    albums: vi.fn(),
    selection: vi.fn(),
    timeline: vi.fn(),
  },
}))
let notifications: RecordedNotifications
beforeEach(() => {
  setActivePinia(createPinia())
  notifications = recordNotifications()
  vi.mocked(engine.albums).mockResolvedValue([])
  vi.mocked(engine.searchHits).mockResolvedValue([])
  vi.mocked(engine.setFavourite).mockReset().mockResolvedValue(undefined)
  vi.mocked(engine.timeline).mockResolvedValue([])
  vi.mocked(engine.selection).mockResolvedValue({ ids: [], revision: 1 })
  vi.mocked(engine.counts).mockResolvedValue({ photos: 0, videos: 0, favourites: 0, people: 0, trash: 0 })
  vi.mocked(engine.applyBatch)
    .mockReset()
    .mockImplementation((ids) => Promise.resolve(ids.map((id) => ({ id, code: '', error: '', changed: true }))))
})
afterEach(() => {
  for (const notice of notifications.shown) notice.closed?.()
  undoOutcomes.value = null
  notifications.stop()
})

function favouriteFixture(flags: boolean[]) {
  const scope = effectScope()
  const lib = useLibrary()
  const states = new Map(flags.map((flag, index) => [index + 1, flag]))
  const ids = [...states.keys()]
  lib.selectVisible(ids)
  vi.mocked(engine.searchHits).mockImplementation((part) =>
    Promise.resolve(part.map((id) => ({ id, w: 1, h: 1, t: 0, f: states.get(id) ?? false }))),
  )
  vi.mocked(engine.applyBatch).mockImplementation((part, action) => {
    part.forEach((id) => states.set(id, action === BatchAction.BatchFavourite))
    return Promise.resolve(part.map((id) => ({ id, code: '', error: '' })))
  })
  vi.mocked(engine.setFavourite).mockImplementation((id, value) => {
    states.set(id, value)
    return Promise.resolve()
  })
  const actions = scope.run(() => useSelectionActions(lib))
  if (!actions) throw new Error('Missing selection actions')
  return { scope, lib, states, ids, actions }
}

function startUndo() {
  const action = notifications.shown
    .flatMap((notice) => notice.actions ?? [])
    .find((action) => action.label === t('settings.undo'))
  if (!action) throw new Error('Missing Undo')
  action.run()
}

function currentUndo() {
  const view = notifications.shown
    .flatMap((notice) => notice.actions ?? [])
    .find((action) => action.label === t('library.outcomes'))
  if (!view) throw new Error('Missing outcomes action')
  view.run()
  const job = undoOutcomes.value
  if (!job) throw new Error('Missing Undo outcomes')
  return job
}

it.each([true, false])('restores mixed previous flags exactly after setting favourites to %s', async (favourite) => {
  const fixture = favouriteFixture([true, false, true, false])
  try {
    await fixture.actions.apply(favourite ? BatchAction.BatchFavourite : BatchAction.BatchUnfavourite)
    startUndo()
    await flushPromises()
    expect([...fixture.states.values()]).toEqual([true, false, true, false])
    expect(engine.applyBatch).toHaveBeenLastCalledWith(
      favourite ? [2, 4] : [1, 3],
      favourite ? BatchAction.BatchUnfavourite : BatchAction.BatchFavourite,
      0,
    )
  } finally {
    fixture.scope.stop()
  }
})

it('keeps newer edits and never restores unchanged targets', async () => {
  const fixture = favouriteFixture([false, false, true])
  try {
    await fixture.actions.apply(BatchAction.BatchFavourite)
    const writes = useFavouriteWrites()
    await writes.write([1, 3], async () => {
      await engine.setFavourite(1, false)
      await engine.setFavourite(3, false)
    })
    startUndo()
    await flushPromises()
    expect(engine.applyBatch).toHaveBeenLastCalledWith([2], BatchAction.BatchUnfavourite, 0)
    expect([...fixture.states.values()]).toEqual([false, false, false])
    expect(currentUndo().counts).toMatchObject({ done: 1, superseded: 1 })
  } finally {
    fixture.scope.stop()
  }
})

it('confirms an inverse hidden by a lost response without replaying its writes', async () => {
  const fixture = favouriteFixture([false, false])
  try {
    await fixture.actions.apply(BatchAction.BatchFavourite)
    vi.mocked(engine.applyBatch).mockImplementationOnce((part) => {
      part.forEach((id) => fixture.states.set(id, false))
      return Promise.reject(new Error('response lost'))
    })
    startUndo()
    await flushPromises()
    const job = currentUndo()
    expect(job.counts.failed).toBe(2)
    expect(job.outcomes[0]?.error).toContain('Completion could not be confirmed')
    await job.retry()
    expect(engine.applyBatch).toHaveBeenCalledTimes(2)
    expect([...fixture.states.values()]).toEqual([false, false])
    expect(job.counts).toMatchObject({ done: 2, failed: 0 })
  } finally {
    fixture.scope.stop()
  }
})

it('does not publish an older favourite request after a newer edit arrives during capture', async () => {
  const fixture = favouriteFixture([false, false])
  try {
    let finish: (items: GridItem[]) => void = () => undefined
    vi.mocked(engine.searchHits).mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve
        }),
    )
    const applying = fixture.actions.apply(BatchAction.BatchFavourite)
    await flushPromises()
    const newer = useFavouriteWrites().write([1], () => engine.setFavourite(1, false))
    finish(fixture.ids.map((id) => ({ id, w: 1, h: 1, t: 0, f: false })))
    await Promise.all([applying, newer])
    expect(engine.applyBatch).toHaveBeenCalledExactlyOnceWith([2], BatchAction.BatchFavourite, 0)
    expect([...fixture.states.values()]).toEqual([false, true])
  } finally {
    fixture.scope.stop()
  }
})

it('lets the new edit finish after an inverse already in flight', async () => {
  const fixture = favouriteFixture([false, false])
  try {
    await fixture.actions.apply(BatchAction.BatchFavourite)
    let finish: () => void = () => undefined
    vi.mocked(engine.applyBatch).mockImplementationOnce(
      (part) =>
        new Promise((resolve) => {
          finish = () => {
            part.forEach((id) => fixture.states.set(id, false))
            resolve(part.map((id) => ({ id, code: '', error: '' })))
          }
        }),
    )
    startUndo()
    await flushPromises()
    const newer = useFavouriteWrites().write([1], () => engine.setFavourite(1, true))
    expect(engine.setFavourite).not.toHaveBeenCalled()
    finish()
    await newer
    await flushPromises()
    expect([...fixture.states.values()]).toEqual([true, false])
  } finally {
    fixture.scope.stop()
  }
})

it('survives disposal without changing another view’s selection or focus', async () => {
  const fixture = favouriteFixture([false, false])
  await fixture.actions.apply(BatchAction.BatchFavourite)
  fixture.scope.stop()
  fixture.lib.selectVisible([88])
  fixture.lib.selected = { day: 'new view', id: 88 }
  startUndo()
  await flushPromises()
  expect([...fixture.states.values()]).toEqual([false, false])
  expect([...fixture.lib.selectedIDs]).toEqual([88])
  expect(fixture.lib.selected).toEqual({ day: 'new view', id: 88 })
})

it('cancels an inverse waiting for its write turn and retains a resumable remainder', async () => {
  const fixture = favouriteFixture([false, false])
  try {
    await fixture.actions.apply(BatchAction.BatchFavourite)
    let finish: () => void = () => undefined
    const turn = useFavouriteWrites().take(
      () =>
        new Promise<void>((resolve) => {
          finish = resolve
        }),
    )
    startUndo()
    await flushPromises()
    const job = currentUndo()
    job.cancel()
    finish()
    await turn
    await vi.waitFor(() => expect(job.busy).toBe(false))
    expect(engine.applyBatch).toHaveBeenCalledOnce()
    expect(job.counts.cancelled).toBe(2)
    await job.resume()
    expect([...fixture.states.values()]).toEqual([false, false])
    expect(job.counts.done).toBe(2)
  } finally {
    fixture.scope.stop()
  }
})

it('bounds outstanding Undo writes independently of a 500-item selection', async () => {
  const scope = effectScope()
  const lib = useLibrary()
  const ids = Array.from({ length: 500 }, (_, i) => i + 1)
  lib.selectVisible(ids)
  vi.mocked(engine.searchHits).mockImplementation((part) =>
    Promise.resolve(part.map((id) => ({ id, w: 1, h: 1, t: 0, f: false }))),
  )
  const actions = scope.run(() => useSelectionActions(lib))
  if (!actions) throw new Error('Missing selection actions')
  await actions.apply(BatchAction.BatchFavourite)
  vi.mocked(engine.searchHits).mockImplementation((part) =>
    Promise.resolve(part.map((id) => ({ id, w: 1, h: 1, t: 0, f: true }))),
  )
  const undo = notifications.shown.flatMap((n) => n.actions ?? []).find((action) => action.label === 'Undo')
  if (!undo) throw new Error('Missing Undo')
  let outstanding = 0
  let maximum = 0
  let finishing = false
  const pending: (() => void)[] = []
  function hold<T>(value: T): Promise<T> {
    if (finishing) return Promise.resolve(value)
    outstanding++
    maximum = Math.max(maximum, outstanding)
    return new Promise((resolve) =>
      pending.push(() => {
        outstanding--
        resolve(value)
      }),
    )
  }
  vi.mocked(engine.setFavourite).mockImplementation(() => hold(undefined))
  vi.mocked(engine.applyBatch).mockImplementation((part) => hold(part.map((id) => ({ id, code: '', error: '' }))))
  try {
    undo.run()
    await flushPromises()
    expect(maximum).toBe(1)
    expect(engine.applyBatch).toHaveBeenLastCalledWith(ids.slice(0, 200), BatchAction.BatchUnfavourite, 0)
  } finally {
    notifications.shown
      .flatMap((notice) => notice.actions ?? [])
      .find((action) => action.label === t('files.cancelRemaining'))
      ?.run()
    finishing = true
    pending.forEach((finish) => finish())
    await flushPromises()
    scope.stop()
  }
})

it('bounds previous-value reads before changing a large selection', async () => {
  const scope = effectScope()
  const lib = useLibrary()
  lib.selectVisible(Array.from({ length: 500 }, (_, i) => i + 1))
  const actions = scope.run(() => useSelectionActions(lib))
  if (!actions) throw new Error('Missing selection actions')
  try {
    await actions.apply(BatchAction.BatchFavourite)
    expect(vi.mocked(engine.searchHits).mock.calls.map(([ids]) => ids.length)).toEqual([200, 200, 100])
  } finally {
    scope.stop()
  }
})

it('cancels remaining chunks while retaining completed outcomes', async () => {
  const scope = effectScope()
  const lib = useLibrary()
  lib.selectVisible(Array.from({ length: 450 }, (_, i) => i + 1))
  let finish: (value: BatchOutcome[]) => void = () => undefined
  vi.mocked(engine.applyBatch).mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        finish = resolve
      }),
  )
  const actions = scope.run(() => useSelectionActions(lib))
  if (!actions) throw new Error('Missing selection actions')
  const applying = actions.apply(BatchAction.BatchTrash)
  actions.cancel()
  finish(Array.from({ length: 200 }, (_, i) => ({ id: i + 1, code: '', error: '' })))
  await applying
  expect(engine.applyBatch).toHaveBeenCalledTimes(1)
  expect(actions.outcomes.value.filter((row) => row.cancelled)).toHaveLength(250)
  expect(actions.outcomes.value.filter((row) => !row.cancelled)).toHaveLength(200)
  expect(actions.busy.value).toBe(false)
  scope.stop()
})

it('retries only failed items after partial success', async () => {
  const scope = effectScope()
  const lib = useLibrary()
  lib.selectVisible([1, 2])
  vi.mocked(engine.applyBatch).mockResolvedValueOnce([
    { id: 1, code: '', error: '' },
    { id: 2, code: '', error: 'disk busy' },
  ])
  const actions = scope.run(() => useSelectionActions(lib))
  if (!actions) throw new Error('Missing selection actions')
  await actions.apply(BatchAction.BatchAlbum, 8)
  expect(actions.failed.value.map((row) => row.id)).toEqual([2])
  await actions.retry()
  expect(engine.applyBatch).toHaveBeenLastCalledWith([2], BatchAction.BatchAlbum, 8)
  expect(actions.failed.value).toEqual([])
  scope.stop()
})

it('reconciles a partially committed album inverse and retains failed-only recovery', async () => {
  const scope = effectScope()
  const lib = useLibrary()
  lib.selectVisible([1, 2])
  const work = scope.run(() => useSelectionActions(lib))
  if (!work) throw new Error('Missing selection actions')
  await work.apply(BatchAction.BatchAlbum, 8)
  vi.mocked(engine.counts).mockClear()
  vi.mocked(engine.applyBatch).mockResolvedValueOnce([
    { id: 1, code: '', error: '' },
    { id: 2, code: 'not_found', error: 'Unavailable asset' },
  ])
  startUndo()
  await flushPromises()
  expect(engine.counts).toHaveBeenCalledOnce()
  expect(notifications.shown.flatMap((notice) => notice.actions ?? []).some((a) => a.label === t('undo.retry'))).toBe(
    true,
  )
  scope.stop()
})

it('does not report a superseded favourite target as a failed unconfirmed write', async () => {
  const fixture = favouriteFixture([false, false])
  await fixture.actions.apply(BatchAction.BatchFavourite)
  await useFavouriteWrites().write([1], () => engine.setFavourite(1, true))
  vi.mocked(engine.applyBatch).mockRejectedValueOnce(new Error('Inverse response lost'))
  startUndo()
  await flushPromises()
  const job = currentUndo()
  await vi.waitFor(() => expect(job.busy).toBe(false))
  expect(job.outcomes.map((row) => [row.id, row.state])).toEqual([
    [1, 'superseded'],
    [2, 'failed'],
  ])
  vi.mocked(engine.applyBatch).mockClear()
  await job.retry()
  expect(engine.applyBatch).toHaveBeenCalledExactlyOnceWith([2], BatchAction.BatchUnfavourite, 0)
  fixture.scope.stop()
})
