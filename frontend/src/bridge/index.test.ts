import { expect, it, vi } from 'vitest'
import { CancellablePromise } from '@wailsio/runtime'
import { LibraryService } from '../../bindings/lumen/services'
import { engine } from './index'

vi.mock('../../bindings/lumen/services', () => ({
  LibraryService: {
    UnlockBlocks: vi.fn().mockResolvedValue(undefined),
    LockBlocks: vi.fn().mockResolvedValue(undefined),
  },
  MediaService: {},
  PeopleService: {},
  SearchService: {},
}))

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
