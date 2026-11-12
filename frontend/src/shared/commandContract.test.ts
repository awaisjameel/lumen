import { expect, it } from 'vitest'
import { DuplicateKind } from '../bridge'
import {
  commandIdFromKey,
  commandKey,
  duplicateCommand,
  InvalidCommandIdentity,
  itemCommand,
  itemCommands,
  resolveCommand,
  staticCommands,
} from './commandContract'

it('has unique static identities and round-trips every item family without widening execution inputs', () => {
  expect(new Set(staticCommands.map((command) => command.id)).size).toBe(staticCommands.length)
  for (const command of staticCommands) expect(commandIdFromKey(commandKey(command.id))).toBe(command.id)
  for (const command of itemCommands) {
    const id = itemCommand(command.name, 12)
    expect(commandIdFromKey(commandKey(id))).toEqual(id)
    expect(commandKey(id)).toBe(`${command.prefix}12${command.suffix}`)
  }
  for (const kind of [DuplicateKind.DuplicateExact, DuplicateKind.DuplicateSimilar] as const) {
    const id = duplicateCommand(kind, 12)
    expect(commandIdFromKey(commandKey(id))).toEqual(id)
  }
})

it('rejects malformed identities without echoing rejected private values', () => {
  for (const value of [
    null,
    undefined,
    '',
    'media.favourtie',
    'person.12.rename',
    { name: 'person.rename', id: '12' },
    { name: 'person.rename', id: 0 },
    { name: 'person.rename', id: -1 },
    { name: 'person.rename', id: Number.MAX_SAFE_INTEGER + 1 },
    { name: 'person.rename', id: 1.2 },
    { name: 'person.rename', id: 1, privateName: 'private' },
    { name: 'duplicates.review', id: 1, kind: 'unknown' },
  ]) {
    expect(() => resolveCommand(value)).toThrow(new InvalidCommandIdentity())
  }
  for (const value of [
    'person.01.rename',
    'person.-1.rename',
    'person.1.5.rename',
    'person.NaN.rename',
    'person.999999999999999999.rename',
    'duplicates.review.unknown.1',
  ])
    expect(commandIdFromKey(value)).toBeUndefined()
  expect(() => itemCommand('person.rename', 0)).toThrow(InvalidCommandIdentity)
})
