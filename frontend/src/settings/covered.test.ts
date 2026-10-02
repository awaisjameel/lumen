import { beforeEach, expect, it, vi } from 'vitest'
import { BlockKind, BlockMode, engine, ErrorCode } from '../bridge'
import type * as Bridge from '../bridge'
import type { Block, CoveredItem } from '../bridge'
import { notice } from '../shared/errors'
import { useCoveredPhotos } from './covered'

vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  engine: { blockedItems: vi.fn(), trashCovered: vi.fn() },
}))

const block = (id: number): Block => ({
  id,
  kind: BlockKind.BlockFolder,
  mode: BlockMode.BlockPhotos,
  targetId: 0,
  path: 'C:\\Photos',
  name: `Block ${id}`,
})
const item = (id: number): CoveredItem => ({ id, w: 4, h: 3, t: 1, hiddenBy: [] })
const locked = () => new Error('locked', { cause: { code: ErrorCode.BlocksLocked } })

const hooks = { used: vi.fn(), failed: vi.fn() }

beforeEach(() => {
  hooks.used.mockReset()
  hooks.failed.mockReset()
  vi.mocked(engine.blockedItems)
    .mockReset()
    .mockResolvedValue([item(1), item(2), item(3)])
  vi.mocked(engine.trashCovered).mockReset().mockResolvedValue(undefined)
  notice.value = ''
})

async function opened() {
  const covered = useCoveredPhotos(hooks)
  await covered.open(block(5))
  hooks.used.mockClear()
  return covered
}

it('loads the photos of the block it is opened on, and counts that as a use', async () => {
  const covered = useCoveredPhotos(hooks)
  const loading = covered.open(block(5))
  expect(covered.loading).toBe(true)
  await loading
  expect(engine.blockedItems).toHaveBeenCalledExactlyOnceWith(5)
  expect(covered.block?.id).toBe(5)
  expect(covered.items.map((it) => it.id)).toEqual([1, 2, 3])
  expect(covered.loading).toBe(false)
  expect(hooks.used).toHaveBeenCalledOnce()
})

it('drops the answer for a block that was left before it arrived', async () => {
  let finish!: (items: CoveredItem[]) => void
  vi.mocked(engine.blockedItems).mockReturnValueOnce(new Promise((resolve) => (finish = resolve)))
  const covered = useCoveredPhotos(hooks)
  const loading = covered.open(block(5))
  covered.close()
  finish([item(9)])
  await loading
  expect(covered.items).toEqual([])
  expect(covered.block).toBeNull()
  expect(hooks.used).not.toHaveBeenCalled()
})

it('closes and hands the failure to the screen when the photos cannot be read', async () => {
  const failure = locked()
  vi.mocked(engine.blockedItems).mockRejectedValue(failure)
  const covered = useCoveredPhotos(hooks)
  await covered.open(block(5))
  expect(covered.block).toBeNull()
  expect(covered.loading).toBe(false)
  expect(hooks.failed).toHaveBeenCalledExactlyOnceWith(failure)
})

it('forgets the photos and the open photo when closed', async () => {
  const covered = await opened()
  covered.view(2)
  covered.close()
  expect(covered.items).toEqual([])
  expect(covered.block).toBeNull()
  expect(covered.viewing).toBeNull()
  expect(covered.source.item).toBeNull()
})

it('gives the Viewer the photo shown and steps through the photos in order, stopping at the ends', async () => {
  const covered = await opened()
  covered.view(2)
  expect(covered.source.id).toBe(2)
  expect(covered.source.item?.id).toBe(2)
  await covered.source.step(1)
  expect(covered.viewing).toBe(3)
  await covered.source.step(1)
  expect(covered.viewing).toBe(3)
  await covered.source.step(-1)
  await covered.source.step(-1)
  await covered.source.step(-1)
  expect(covered.viewing).toBe(1)
})

it('offers the Viewer Move to Trash and nothing that edits or inspects', async () => {
  const covered = await opened()
  const { actions } = covered.source
  expect(actions.moveToTrash).toBeTypeOf('function')
  expect(actions.favourite).toBeUndefined()
  expect(actions.restore).toBeUndefined()
  expect(actions.deleteFromDisk).toBeUndefined()
  expect(actions.details).toBe(false)
  expect(covered.source.sel).toBeNull()
})

it('closes the Viewer when it is closed', async () => {
  const covered = await opened()
  covered.view(1)
  covered.source.close()
  expect(covered.viewing).toBeNull()
  expect(covered.items).toHaveLength(3) // the browser stays open behind it
})

it.each([
  ['a middle photo', 2, 3, [1, 3]],
  ['the last photo', 3, 2, [1, 2]],
  ['the first photo', 1, 2, [2, 3]],
])('moves %s to the Trash and the Viewer on to its neighbour', async (_name, id, next, left) => {
  const covered = await opened()
  covered.view(id)
  covered.source.actions.moveToTrash?.()
  await vi.waitFor(() => expect(covered.items.map((it) => it.id)).toEqual(left))
  expect(engine.trashCovered).toHaveBeenCalledExactlyOnceWith([id])
  expect(covered.viewing).toBe(next)
})

it('closes the Viewer when the photo moved to the Trash was the last', async () => {
  vi.mocked(engine.blockedItems).mockResolvedValue([item(1)])
  const covered = await opened()
  covered.view(1)
  covered.source.actions.moveToTrash?.()
  await vi.waitFor(() => expect(covered.items).toEqual([]))
  expect(covered.viewing).toBeNull()
})

it('keeps everything and says why when the Trash refuses', async () => {
  const covered = await opened()
  covered.view(2)
  vi.mocked(engine.trashCovered).mockRejectedValue(new Error('disk on fire'))
  covered.source.actions.moveToTrash?.()
  await vi.waitFor(() => expect(notice.value).toContain('disk on fire'))
  expect(covered.items).toHaveLength(3)
  expect(covered.viewing).toBe(2)
  expect(hooks.failed).not.toHaveBeenCalled()
})

it('leaves it to the screen to relock when the Trash says the screen is locked', async () => {
  const covered = await opened()
  covered.view(2)
  const failure = locked()
  vi.mocked(engine.trashCovered).mockRejectedValue(failure)
  covered.source.actions.moveToTrash?.()
  await vi.waitFor(() => expect(hooks.failed).toHaveBeenCalledExactlyOnceWith(failure))
  expect(notice.value).toBe('')
})

it('does not bring photos back when the browser was closed while the Trash was working', async () => {
  const covered = await opened()
  covered.view(2)
  hooks.used.mockClear()
  let finish!: () => void
  vi.mocked(engine.trashCovered).mockReturnValue(new Promise<void>((resolve) => (finish = resolve)))
  covered.source.actions.moveToTrash?.()
  covered.close()
  finish()
  await Promise.resolve()
  expect(covered.items).toEqual([])
  expect(covered.viewing).toBeNull()
  expect(hooks.used).not.toHaveBeenCalled()
})
