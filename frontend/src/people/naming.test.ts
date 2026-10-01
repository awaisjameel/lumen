import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { engine, ErrorCode } from '../bridge'
import type * as Bridge from '../bridge'
import { question } from '../shared/confirm'
import { namePerson } from './naming'

vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  engine: { namePerson: vi.fn(), personByName: vi.fn(), mergePeople: vi.fn() },
}))

beforeEach(() => {
  vi.mocked(engine.namePerson)
    .mockReset()
    .mockRejectedValue(new Error('taken', { cause: { code: ErrorCode.PersonExists } }))
  vi.mocked(engine.personByName).mockReset().mockResolvedValue(2)
  vi.mocked(engine.mergePeople).mockReset().mockResolvedValue(undefined)
})
afterEach(() => question.value?.answer(false))

it('merges into the named person only after confirming that they are the same person', async () => {
  const named = namePerson(1, ' Sara ')
  await Promise.resolve()
  expect(engine.namePerson).toHaveBeenCalledWith(1, 'Sara')
  expect(question.value?.title).toContain('Sara')
  expect(engine.personByName).not.toHaveBeenCalled()
  expect(engine.mergePeople).not.toHaveBeenCalled()
  question.value?.answer(true)
  await expect(named).resolves.toBe(2)
  expect(engine.mergePeople).toHaveBeenCalledWith(1, 2)
})

it('keeps both people when the merge is declined or the named person disappeared', async () => {
  const declined = namePerson(1, 'Sara')
  await Promise.resolve()
  question.value?.answer(false)
  await expect(declined).resolves.toBeNull()
  expect(engine.personByName).not.toHaveBeenCalled()
  vi.mocked(engine.personByName).mockResolvedValue(0)
  const missing = namePerson(1, 'Sara')
  await Promise.resolve()
  question.value?.answer(true)
  await expect(missing).resolves.toBeNull()
  expect(engine.mergePeople).not.toHaveBeenCalled()
})

it('propagates a blocked name without offering a merge', async () => {
  const blocked = new Error('blocked', { cause: { code: ErrorCode.BlockedName } })
  vi.mocked(engine.namePerson).mockRejectedValue(blocked)
  await expect(namePerson(1, 'Sara')).rejects.toBe(blocked)
  expect(question.value).toBeNull()
  expect(engine.personByName).not.toHaveBeenCalled()
  expect(engine.mergePeople).not.toHaveBeenCalled()
})

it('sends the name Go saves: a byte-order mark stays, next-line whitespace goes', async () => {
  vi.mocked(engine.namePerson).mockResolvedValue(undefined)
  await expect(namePerson(1, '\u0085\uFEFFAli ')).resolves.toBe(1)
  expect(engine.namePerson).toHaveBeenCalledWith(1, '\uFEFFAli')
})
