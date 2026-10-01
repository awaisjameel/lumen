import { expect, it, vi } from 'vitest'
import { CancellablePromise } from '@wailsio/runtime'
import { LibraryService, SearchService } from '../../bindings/lumen/services'
import { engine, ErrorCode } from './index'
import { errorText } from '../shared/errors'

vi.mock('../../bindings/lumen/services', () => ({
  LibraryService: {
    UnlockBlocks: vi.fn().mockResolvedValue(undefined),
    LockBlocks: vi.fn().mockResolvedValue(undefined),
    SetAlbumItem: vi.fn().mockResolvedValue(undefined),
  },
  MediaService: {},
  PeopleService: {},
  SearchService: { VisibleAssets: vi.fn() },
}))

it('normalizes an empty visible subset and passes cancellation to the engine', async () => {
  const controller = new AbortController()
  const call = CancellablePromise.resolve<number[] | null>(null)
  const cancelled = vi.spyOn(call, 'cancelOn')
  vi.mocked(SearchService.VisibleAssets).mockReturnValueOnce(call)
  expect(await engine.visibleAssets([1, 2], controller.signal)).toEqual([])
  expect(SearchService.VisibleAssets).toHaveBeenCalledWith([1, 2])
  expect(cancelled).toHaveBeenCalledWith(controller.signal)
})

it('uses asset-first album membership arguments end to end', async () => {
  await engine.setAlbumItem(17, 43, true)
  expect(LibraryService.SetAlbumItem).toHaveBeenCalledWith(17, 43, true)
})

it('localizes every invalid-album reason without showing engine prose', () => {
  for (const code of [
    ErrorCode.InvalidAlbum,
    ErrorCode.AlbumMediaKind,
    ErrorCode.AlbumSelection,
    ErrorCode.AlbumDate,
    ErrorCode.AlbumDateOrder,
    ErrorCode.AlbumSmartItems,
    ErrorCode.AlbumKind,
    ErrorCode.AlbumReference,
    ErrorCode.AlbumManualOnly,
  ]) {
    expect(errorText(new Error('private engine wording', { cause: { code } }))).not.toContain('private engine wording')
  }
  expect(errorText(new Error('invalid album: date', { cause: { code: ErrorCode.AlbumDate } }))).toBe(
    'Enter a valid date in YYYY-MM-DD format.',
  )
})

it('orders unlock, navigation lock, and the next unlock without overlapping sessions', async () => {
  const calls: string[] = []
  let resolve!: () => void
  vi.mocked(LibraryService.UnlockBlocks)
    .mockImplementationOnce(() => {
      calls.push('old unlock')
      return new CancellablePromise<void>((done) => (resolve = done))
    })
    .mockImplementationOnce(() => {
      calls.push('new unlock')
      return CancellablePromise.resolve()
    })
  vi.mocked(LibraryService.LockBlocks).mockImplementationOnce(() => {
    calls.push('lock')
    return CancellablePromise.resolve()
  })
  const first = engine.unlockBlocks('123456')
  const lock = engine.lockBlocks()
  const next = engine.unlockBlocks('654321')
  await Promise.resolve()
  expect(calls).toEqual(['old unlock'])
  resolve()
  await Promise.all([first, lock, next])
  expect(calls).toEqual(['old unlock', 'lock', 'new unlock'])
})

it('still locks after a failed unlock', async () => {
  vi.mocked(LibraryService.UnlockBlocks).mockRejectedValueOnce(new Error('incorrect PIN'))
  const unlock = engine.unlockBlocks('000000')
  const lock = engine.lockBlocks()
  await expect(unlock).rejects.toThrow('incorrect PIN')
  await expect(lock).resolves.toBeUndefined()
})
