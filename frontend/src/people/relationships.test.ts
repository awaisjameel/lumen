import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { engine, ErrorCode, MergeKeep, Relation } from '../bridge'
import type * as Bridge from '../bridge'
import { question } from '../shared/confirm'
import { mergePeople, relationOptions, relationText, roleText, setMe } from './relationships'
import { personFixture } from './testing'

vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  engine: { people: vi.fn(), person: vi.fn(), setMe: vi.fn(), mergePeople: vi.fn() },
}))

const conflict = () => new Error('conflict', { cause: { code: ErrorCode.RelationConflict } })
const tick = async () => {
  for (let i = 0; i < 5; i++) await Promise.resolve()
}

beforeEach(() => {
  setActivePinia(createPinia())
  vi.mocked(engine.people).mockReset().mockResolvedValue([])
  vi.mocked(engine.setMe).mockReset().mockResolvedValue(undefined)
  vi.mocked(engine.mergePeople).mockReset().mockResolvedValue(undefined)
})
afterEach(() => question.value?.answer(false))

it('words relationships, Me, and the user’s own labels apart from names', () => {
  const mum = { ...personFixture(1, 'Jane'), relation: Relation.RelationMother }
  expect(relationText(mum)).toBe('Mum')
  expect(roleText(mum)).toBe('Mum')
  expect(roleText({ ...mum, me: true, relation: Relation.RelationNone })).toBe('Me')
  expect(relationText({ relation: Relation.RelationCustom, relationLabel: 'Coach' })).toBe('Coach')
  expect(roleText(personFixture())).toBe('')
})

it('offers matching relationships first and the typed word as its own label', () => {
  expect(relationOptions('').map((o) => o.text)).toContain('Neighbour')
  expect(relationOptions('gran').map((o) => o.text)).toEqual([
    'Grandma',
    'Grandad',
    'Grandparent',
    'Grandchild',
    'Use “gran”',
  ])
  // A typed built-in name is that relationship, not a custom label.
  expect(relationOptions(' MUM ').map((o) => o.relation)).toEqual([Relation.RelationMother])
  const custom = relationOptions('  football   coach ').at(-1)
  expect(custom).toEqual({ relation: Relation.RelationCustom, label: 'football coach', text: 'Use “football coach”' })
})

it('makes someone Me at once when nothing would be replaced', async () => {
  await expect(setMe(personFixture(2, 'Sara'))).resolves.toBe(true)
  expect(engine.setMe).toHaveBeenCalledExactlyOnceWith(2, false)
  expect(question.value).toBeNull()
})

it('names the current Me and the relationship it drops before replacing them', async () => {
  vi.mocked(engine.setMe).mockRejectedValueOnce(conflict())
  vi.mocked(engine.people).mockResolvedValue([
    { ...personFixture(1, 'Ali'), me: true },
    { ...personFixture(2, 'Sara'), relation: Relation.RelationSister },
  ])
  const asked = setMe(personFixture(2, 'Sara'))
  await vi.waitFor(() => expect(question.value).not.toBeNull())
  expect(question.value?.title).toBe('Make Sara Me?')
  expect(question.value?.text).toContain('Ali is Me now and will no longer be.')
  expect(question.value?.text).toContain('“Sister” will be removed')
  question.value?.answer(true)
  await expect(asked).resolves.toBe(true)
  expect(engine.setMe).toHaveBeenLastCalledWith(2, true)
})

it('leaves Me alone when the replacement is declined', async () => {
  vi.mocked(engine.setMe).mockRejectedValueOnce(conflict())
  const asked = setMe(personFixture(2, 'Sara'))
  await vi.waitFor(() => expect(question.value).not.toBeNull())
  // A hidden Me is not named.
  expect(question.value?.text).toBe('Someone else is Me now and will no longer be.')
  question.value?.answer(false)
  await expect(asked).resolves.toBe(false)
  expect(engine.setMe).toHaveBeenCalledOnce()
})

it('asks which relationship two merged people keep, and keeps the one chosen', async () => {
  vi.mocked(engine.mergePeople).mockRejectedValueOnce(conflict())
  vi.mocked(engine.person).mockImplementation((id) =>
    Promise.resolve(
      id === 1
        ? { ...personFixture(1, 'Ali'), relation: Relation.RelationBrother }
        : { ...personFixture(2, 'Alistair'), relation: Relation.RelationFriend },
    ),
  )
  const merged = mergePeople(1, 2)
  await vi.waitFor(() => expect(question.value?.other).toBeDefined())
  expect(question.value?.text).toBe('One is your Brother and the other your Friend. A person has one relationship.')
  expect(question.value?.ok).toBe('Keep Friend')
  expect(question.value?.other?.label).toBe('Keep Brother')
  question.value?.other?.choose()
  await expect(merged).resolves.toBe(true)
  expect(engine.mergePeople).toHaveBeenLastCalledWith(1, 2, MergeKeep.MergeKeepFrom)
})

it('keeps Me through a merge only once the user agrees to drop the other relationship', async () => {
  vi.mocked(engine.mergePeople).mockRejectedValueOnce(conflict())
  vi.mocked(engine.person).mockImplementation((id) =>
    Promise.resolve(
      id === 1
        ? { ...personFixture(1, 'Ali'), me: true }
        : { ...personFixture(2, 'Al'), relation: Relation.RelationCousin },
    ),
  )
  const merged = mergePeople(1, 2)
  await vi.waitFor(() => expect(question.value).not.toBeNull())
  expect(question.value?.other).toBeUndefined()
  expect(question.value?.text).toContain('the other is your Cousin')
  question.value?.answer(false)
  await expect(merged).resolves.toBe(false)
  await tick()
  expect(engine.mergePeople).toHaveBeenCalledOnce()
})

it('passes on failures that are not a conflict without asking', async () => {
  const gone = new Error('gone', { cause: { code: ErrorCode.NotFound } })
  vi.mocked(engine.mergePeople).mockRejectedValueOnce(gone)
  await expect(mergePeople(1, 2)).rejects.toBe(gone)
  expect(question.value).toBeNull()
})
