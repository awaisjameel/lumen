import { expect, it, vi } from 'vitest'
import { Call, CancelError, CancellablePromise, CancelledRejectionError, Events } from '@wailsio/runtime'
import { LibraryService, SearchService, SystemService } from '../../bindings/lumen/services'
import { engine, ErrorCode, isCancelled, logUIError, onLibraryChanged, SourceAvailability } from './index'
import type { GridItem } from './index'
import { errorText } from '../shared/errors'

vi.mock(import('@wailsio/runtime'), async (importOriginal) => {
  const runtime = await importOriginal()
  return { ...runtime, Events: { ...runtime.Events, On: vi.fn() } }
})

vi.mock('../../bindings/lumen/services', () => ({
  LibraryService: {
    Sources: vi.fn(),
    RecheckSources: vi.fn().mockResolvedValue(undefined),
    UnlockBlocks: vi.fn().mockResolvedValue(undefined),
    LockBlocks: vi.fn().mockResolvedValue(undefined),
    SetAlbumItem: vi.fn().mockResolvedValue(undefined),
    Counts: vi.fn(),
    FindCities: vi.fn(),
    SetTakenAt: vi.fn().mockResolvedValue(undefined),
    RestoreTakenAt: vi.fn().mockResolvedValue(undefined),
    SetLocation: vi.fn().mockResolvedValue(undefined),
    RestoreLocation: vi.fn().mockResolvedValue(undefined),
    Unblock: vi.fn().mockResolvedValue(9),
    UndoUnblock: vi.fn().mockResolvedValue(undefined),
  },
  MediaService: {},
  PeopleService: {},
  SearchService: { Hits: vi.fn() },
  SystemService: { LogUIError: vi.fn() },
}))

it('passes correction batches and the unblock receipt through without changing their arguments', async () => {
  await engine.setTakenAt([17, 43], 0)
  await engine.restoreTakenAt([17, 43])
  await engine.setLocation([17], -90, 180)
  await engine.restoreLocation([17])
  const receipt = await engine.unblock(7)
  await engine.undoUnblock(receipt)
  expect(LibraryService.SetTakenAt).toHaveBeenCalledWith([17, 43], 0)
  expect(LibraryService.RestoreTakenAt).toHaveBeenCalledWith([17, 43])
  expect(LibraryService.SetLocation).toHaveBeenCalledWith([17], -90, 180)
  expect(LibraryService.RestoreLocation).toHaveBeenCalledWith([17])
  expect(LibraryService.Unblock).toHaveBeenCalledWith(7)
  expect(LibraryService.UndoUnblock).toHaveBeenCalledWith(9)
})

it('normalises empty offline city suggestions and cancels superseded typing', async () => {
  const controller = new AbortController()
  const call = CancellablePromise.resolve<null>(null)
  const cancelled = vi.spyOn(call, 'cancelOn')
  vi.mocked(LibraryService.FindCities).mockReturnValueOnce(call)
  expect(await engine.findCities('São', 5, controller.signal)).toEqual([])
  expect(LibraryService.FindCities).toHaveBeenCalledWith('São', 5)
  expect(cancelled).toHaveBeenCalledWith(controller.signal)
})

it('returns sidebar totals and localises correction errors', async () => {
  const counts = { photos: 12480, videos: 1104, favourites: 81, people: 23, trash: 7 }
  vi.mocked(LibraryService.Counts).mockReturnValueOnce(CancellablePromise.resolve(counts))
  expect(await engine.counts()).toEqual(counts)
  for (const code of [
    ErrorCode.CorrectionSelection,
    ErrorCode.CorrectionDate,
    ErrorCode.CorrectionLocation,
    ErrorCode.InvalidFilter,
  ]) {
    expect(errorText(new Error('private engine wording', { cause: { code } }))).not.toContain('private engine wording')
  }
  expect(errorText(new Error('', { cause: { code: ErrorCode.CorrectionDate } }))).toBe(
    'Enter a date between 1800 and 2199.',
  )
})

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

it('normalizes no remaining search hits and passes cancellation to the engine', async () => {
  const controller = new AbortController()
  const call = CancellablePromise.resolve<GridItem[] | null>(null)
  const cancelled = vi.spyOn(call, 'cancelOn')
  vi.mocked(SearchService.Hits).mockReturnValueOnce(call)
  expect(await engine.searchHits([1, 2], controller.signal)).toEqual([])
  expect(SearchService.Hits).toHaveBeenCalledWith([1, 2])
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

it('reads cached source availability and requests a background recheck', async () => {
  const sources = [{ id: 7, path: 'D:/Photos', enabled: true, availability: SourceAvailability.SourceUnavailable }]
  vi.mocked(LibraryService.Sources).mockReturnValueOnce(CancellablePromise.resolve(sources))
  expect(await engine.sources()).toEqual(sources)
  await engine.recheckSources()
  expect(LibraryService.RecheckSources).toHaveBeenCalledWith()
  vi.mocked(LibraryService.Sources).mockReturnValueOnce(CancellablePromise.resolve(null))
  expect(await engine.sources()).toEqual([])
})

it('forwards source availability changes through the library event and returns its unsubscribe', () => {
  const stop = vi.fn()
  const listen = vi.mocked(Events.On).mockReturnValueOnce(stop)
  const changed = vi.fn()
  const unsubscribe = onLibraryChanged(changed)
  const event = new Events.WailsEvent('library:changed', {
    sources: true,
    assets: false,
    thumbs: false,
    tags: false,
    people: false,
  })
  expect(listen.mock.calls[0]?.[0]).toBe('library:changed')
  listen.mock.calls[0]?.[1](event)
  expect(changed).toHaveBeenCalledWith(event.data)
  unsubscribe()
  expect(stop).toHaveBeenCalledOnce()
})
