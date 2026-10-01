import { beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { DuplicateKind, engine } from '../bridge'
import type * as Bridge from '../bridge'
import type { DuplicateGroup } from '../bridge'
import { showBlocksChanged } from '../shared/block'
import { useDuplicates } from './store'

vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  engine: { duplicateGroups: vi.fn() },
}))
vi.mock('../library/store', () => ({ useLibrary: () => ({ refreshVisibility: vi.fn().mockResolvedValue(undefined) }) }))

const groups: DuplicateGroup[] = [
  {
    kind: DuplicateKind.DuplicateExact,
    items: [{ id: 1, paths: ['C:\\private.jpg'], size: 100, width: 10, height: 10, thumb: 1, takenAt: 0 }],
  },
]

beforeEach(() => {
  setActivePinia(createPinia())
  vi.mocked(engine.duplicateGroups).mockReset().mockResolvedValue(groups)
})

it('clears cached duplicate paths immediately after a block and keeps them cleared if the refresh fails', async () => {
  const duplicates = useDuplicates()
  await duplicates.load()
  let fail!: (reason: Error) => void
  vi.mocked(engine.duplicateGroups).mockReturnValueOnce(new Promise((_resolve, reject) => (fail = reject)))
  const changed = showBlocksChanged()
  expect(duplicates.list).toEqual([])
  expect(duplicates.loaded).toBe(false)
  const rejected = expect(changed).rejects.toThrow('read failed')
  fail(new Error('read failed'))
  await rejected
  expect(useDuplicates().list).toEqual([])
  expect(duplicates.error).toContain('read failed')
})

it('supersedes an initial duplicate read that was pending when visibility changed', async () => {
  let finish!: (value: DuplicateGroup[]) => void
  vi.mocked(engine.duplicateGroups)
    .mockReturnValueOnce(new Promise((resolve) => (finish = resolve)))
    .mockResolvedValueOnce([])
  const duplicates = useDuplicates()
  const initial = duplicates.load()
  const changed = showBlocksChanged()
  finish(groups)
  await Promise.all([initial, changed])
  expect(duplicates.list).toEqual([])
  expect(duplicates.loaded).toBe(true)
  expect(engine.duplicateGroups).toHaveBeenCalledTimes(2)
})

it('does not fetch duplicates that have never been opened when blocks change', async () => {
  await showBlocksChanged()
  expect(engine.duplicateGroups).not.toHaveBeenCalled()
})
