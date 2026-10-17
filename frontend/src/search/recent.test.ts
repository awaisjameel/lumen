import { beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { engine } from '../bridge'
import type * as Bridge from '../bridge'
import type * as Errors from '../shared/errors'
import { reportError } from '../shared/errors'
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

beforeEach(() => {
  setActivePinia(createPinia())
  vi.mocked(reportError).mockClear()
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
