import { beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { engine } from '../bridge'
import type * as Bridge from '../bridge'
import type * as Errors from '../shared/errors'
import { logError, reportError } from '../shared/errors'
import { useRecentSearches } from './recent'

vi.mock('../bridge', async (original) => ({
  ...(await original<typeof Bridge>()),
  engine: { recentSearches: vi.fn(), rememberSearch: vi.fn(), clearRecentSearches: vi.fn() },
}))
vi.mock('../shared/errors', async (original) => ({
  ...(await original<typeof Errors>()),
  reportError: vi.fn(),
  logError: vi.fn(),
}))

function deferred<T>() {
  let resolve: (value: T) => void = vi.fn()
  let reject: (reason: unknown) => void = vi.fn()
  const promise = new Promise<T>((done, fail) => {
    resolve = done
    reject = fail
  })
  return { promise, resolve, reject }
}

beforeEach(() => {
  setActivePinia(createPinia())
  vi.resetAllMocks()
  vi.mocked(engine.recentSearches).mockResolvedValue([])
  vi.mocked(engine.rememberSearch).mockResolvedValue([])
  vi.mocked(engine.clearRecentSearches).mockResolvedValue(undefined)
})

it('keeps the recent searches when forgetting them fails, and says so', async () => {
  vi.mocked(engine.recentSearches).mockResolvedValue(['beach'])
  vi.mocked(engine.clearRecentSearches).mockRejectedValue(new Error('busy'))
  const recent = useRecentSearches()
  await recent.load()
  await recent.clear()
  expect(recent.list).toEqual(['beach'])
  expect(reportError).toHaveBeenCalledOnce()
})

it.each(['remember', 'load'] as const)(
  'does not roll back a newer successful %s after an older clear fails',
  async (action) => {
    const pending = deferred<undefined>()
    vi.mocked(engine.clearRecentSearches).mockReturnValue(pending.promise)
    vi.mocked(engine.recentSearches).mockResolvedValue(['new'])
    vi.mocked(engine.rememberSearch).mockResolvedValue(['new'])
    const recent = useRecentSearches()
    recent.list = ['old']
    const clearing = recent.clear()
    expect(recent.list).toEqual([])
    await recent[action]('new')
    pending.reject(new Error('busy'))
    await clearing
    expect(recent.list).toEqual(['new'])
    expect(engine.recentSearches).toHaveBeenCalledTimes(action === 'load' ? 1 : 0)
    expect(reportError).toHaveBeenCalledOnce()
  },
)

it('reads authoritative filtered history instead of restoring a cached snapshot', async () => {
  const recent = useRecentSearches()
  recent.list = ['now blocked', 'beach']
  vi.mocked(engine.clearRecentSearches).mockRejectedValue(new Error('busy'))
  vi.mocked(engine.recentSearches).mockResolvedValue(['beach'])
  await recent.clear()
  expect(recent.list).toEqual(['beach'])
  expect(engine.recentSearches).toHaveBeenCalledOnce()
  expect(reportError).toHaveBeenCalledOnce()
})

it('keeps history empty and recoverable if reconciliation also fails', async () => {
  const recent = useRecentSearches()
  recent.list = ['now blocked']
  vi.mocked(engine.clearRecentSearches).mockRejectedValue(new Error('busy'))
  vi.mocked(engine.recentSearches).mockRejectedValueOnce(new Error('read failed')).mockResolvedValue(['beach'])
  await recent.clear()
  expect(recent.list).toEqual([])
  expect(reportError).toHaveBeenCalledOnce()
  expect(logError).toHaveBeenCalledOnce()
  await recent.load()
  expect(recent.list).toEqual(['beach'])
})

it('does not restore cached history while a newer search is pending or when it fails', async () => {
  const clearing = deferred<undefined>()
  const remembering = deferred<string[]>()
  vi.mocked(engine.clearRecentSearches).mockReturnValue(clearing.promise)
  vi.mocked(engine.rememberSearch).mockReturnValue(remembering.promise)
  const recent = useRecentSearches()
  recent.list = ['old']
  const clear = recent.clear()
  const remember = recent.remember('new')
  clearing.reject(new Error('busy'))
  await clear
  expect(recent.list).toEqual([])
  expect(engine.recentSearches).not.toHaveBeenCalled()
  remembering.reject(new Error('write failed'))
  await remember
  expect(recent.list).toEqual([])
  vi.mocked(engine.recentSearches).mockResolvedValue(['current'])
  await recent.load()
  expect(recent.list).toEqual(['current'])
})

it('does not let a failed-clear reconciliation overwrite a newer search', async () => {
  const reading = deferred<string[]>()
  vi.mocked(engine.clearRecentSearches).mockRejectedValue(new Error('busy'))
  vi.mocked(engine.recentSearches).mockReturnValue(reading.promise)
  vi.mocked(engine.rememberSearch).mockResolvedValue(['new'])
  const recent = useRecentSearches()
  recent.list = ['old']
  const clear = recent.clear()
  await vi.waitFor(() => expect(engine.recentSearches).toHaveBeenCalledOnce())
  await recent.remember('new')
  reading.resolve(['old'])
  await clear
  expect(recent.list).toEqual(['new'])
})

it.each([true, false])(
  'keeps a newer successful clear when an older clear fails (older finishes first: %s)',
  async (olderFirst) => {
    const older = deferred<undefined>()
    const newer = deferred<undefined>()
    vi.mocked(engine.clearRecentSearches).mockReturnValueOnce(older.promise).mockReturnValueOnce(newer.promise)
    const recent = useRecentSearches()
    recent.list = ['old']
    const first = recent.clear()
    const second = recent.clear()
    if (olderFirst) {
      older.reject(new Error('busy'))
      await first
    }
    newer.resolve(undefined)
    await second
    if (!olderFirst) older.reject(new Error('busy'))
    await first
    expect(recent.list).toEqual([])
    expect(engine.recentSearches).not.toHaveBeenCalled()
    expect(reportError).toHaveBeenCalledOnce()
  },
)

it('reconciles a newer failed clear after an older successful clear', async () => {
  const older = deferred<undefined>()
  const newer = deferred<undefined>()
  vi.mocked(engine.clearRecentSearches).mockReturnValueOnce(older.promise).mockReturnValueOnce(newer.promise)
  const recent = useRecentSearches()
  recent.list = ['old']
  const first = recent.clear()
  const second = recent.clear()
  older.resolve(undefined)
  await first
  newer.reject(new Error('busy'))
  await second
  expect(recent.list).toEqual([])
  expect(engine.recentSearches).toHaveBeenCalledOnce()
  expect(reportError).toHaveBeenCalledOnce()
})

it('reconciles two failed clears without restoring either optimistic snapshot', async () => {
  const older = deferred<undefined>()
  vi.mocked(engine.clearRecentSearches).mockReturnValueOnce(older.promise).mockRejectedValueOnce(new Error('busy'))
  vi.mocked(engine.recentSearches).mockResolvedValue(['current'])
  const recent = useRecentSearches()
  recent.list = ['old']
  const first = recent.clear()
  await recent.clear()
  expect(recent.list).toEqual(['current'])
  older.reject(new Error('busy'))
  await first
  expect(recent.list).toEqual(['current'])
  expect(engine.recentSearches).toHaveBeenCalledOnce()
  expect(reportError).toHaveBeenCalledTimes(2)
})

it('keeps the newest answer when loads and searches overlap', async () => {
  let answer: (list: string[]) => void = vi.fn()
  vi.mocked(engine.recentSearches).mockReturnValue(new Promise((resolve) => (answer = resolve)))
  vi.mocked(engine.rememberSearch).mockResolvedValue(['sunset', 'beach'])
  const recent = useRecentSearches()
  const load = recent.load()
  await recent.remember('sunset')
  answer(['beach'])
  await load
  expect(recent.list).toEqual(['sunset', 'beach'])
})

it('matches by any word, ignoring case as names do', () => {
  const recent = useRecentSearches()
  recent.list = ['Été à la plage', 'birthday cake', 'Beach sunset', 'beach']
  expect(recent.matching('')).toEqual(recent.list)
  expect(recent.matching('BEACH')).toEqual(['Beach sunset'])
  expect(recent.matching('été CAKE')).toEqual(['Été à la plage', 'birthday cake'])
})
