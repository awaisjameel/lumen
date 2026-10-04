import { expect, it, vi } from 'vitest'
import { Call, CancelError, CancellablePromise, CancelledRejectionError } from '@wailsio/runtime'
import { LibraryService, SearchService, SystemService } from '../../bindings/lumen/services'
import { engine, ErrorCode, isCancelled, logUIError } from './index'
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
  SystemService: { LogUIError: vi.fn() },
}))

it('cannot recursively report either a synchronous or asynchronous logging failure', async () => {
  vi.mocked(SystemService.LogUIError)
    .mockImplementationOnce(() => {
      throw new Error('runtime unavailable')
    })
    .mockReturnValueOnce(CancellablePromise.reject(new Error('logging unavailable')))
  logUIError({ message: 'first', stack: '', where: 'window' })
  logUIError({ message: 'second', stack: '', where: 'vue' })
  await new Promise((resolve) => setTimeout(resolve, 0))
  expect(SystemService.LogUIError).toHaveBeenCalledTimes(2)
})

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

it('recognises a late cancelled runtime read without hiding another late failure', () => {
  const promise = CancellablePromise.resolve()
  expect(isCancelled(new CancelError('superseded'))).toBe(true)
  expect(isCancelled(new CancelledRejectionError(promise, new Call.RuntimeError('context canceled')))).toBe(true)
  expect(isCancelled(new Call.RuntimeError('context canceled'))).toBe(false)
  expect(isCancelled(new CancelledRejectionError(promise, new Call.RuntimeError('disk full')))).toBe(false)
  expect(isCancelled(new CancelledRejectionError(promise, new Error('context canceled')))).toBe(false)
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
