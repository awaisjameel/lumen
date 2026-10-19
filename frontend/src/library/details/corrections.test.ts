import { expect, it } from 'vitest'
import { dateInput, parseDateInput } from './corrections'

it('keeps wall-clock seconds independent of the local time zone, including epoch and pre-epoch dates', () => {
  for (const date of [
    '1800-01-01T00:00:00',
    '1969-12-31T23:59:59',
    '1970-01-01T00:00:00',
    '2024-02-29T12:34:56',
    '2199-12-31T23:59:59',
  ]) {
    const seconds = parseDateInput(date)
    expect(seconds).toBeDefined()
    if (seconds === undefined) throw new Error('Missing parsed date')
    expect(dateInput(seconds)).toBe(date)
  }
  expect(parseDateInput('1970-01-01T00:00')).toBe(0)
})

it('rejects invalid calendar days, incomplete input and dates outside the engine range', () => {
  for (const date of [
    '',
    '2026-02-29T12:00',
    '2026-04-31T12:00',
    '2026-13-01T12:00',
    '2026-10-01T24:00',
    '1799-12-31T23:59',
    '2200-01-01T00:00',
    '2026-01-01',
    '2026-01-01T12:00Z',
  ])
    expect(parseDateInput(date)).toBeUndefined()
})
