import { beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { effectScope } from 'vue'
import { BatchAction, engine } from '../bridge'
import type * as Bridge from '../bridge'
import type { BatchOutcome } from '../bridge'
import { useLibrary } from './store'
import { useSelectionActions } from './selectionActions'

vi.mock('../bridge', async (original) => ({
  ...(await original<typeof Bridge>()),
  engine: { applyBatch: vi.fn(), searchHits: vi.fn(), counts: vi.fn(), selection: vi.fn(), timeline: vi.fn() },
}))
beforeEach(() => {
  setActivePinia(createPinia())
  vi.mocked(engine.searchHits).mockResolvedValue([])
  vi.mocked(engine.timeline).mockResolvedValue([])
  vi.mocked(engine.selection).mockResolvedValue({ ids: [], revision: 1 })
  vi.mocked(engine.counts).mockResolvedValue({ photos: 0, videos: 0, favourites: 0, people: 0, trash: 0 })
  vi.mocked(engine.applyBatch)
    .mockReset()
    .mockImplementation((ids) => Promise.resolve(ids.map((id) => ({ id, code: '', error: '' }))))
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
