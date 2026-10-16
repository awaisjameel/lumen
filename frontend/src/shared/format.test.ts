import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { dayLabel, daySpan } from './format'
import { t } from './i18n'

beforeEach(() => {
  vi.useFakeTimers()
  vi.setSystemTime(new Date(2026, 9, 17, 12))
})
afterEach(() => vi.useRealTimers())

it('names grid days in full, adding the year only outside this one and without a comma', () => {
  expect(dayLabel('2026-10-17')).toBe('Today')
  expect(dayLabel('2026-10-16')).toBe('Yesterday')
  expect(dayLabel('2026-10-03')).toBe('Saturday 3 October')
  expect(dayLabel('2021-01-03')).toBe('Sunday 3 January 2021')
})

it('spans a collection by month and an event by day', () => {
  // Intl sets the dash between thin spaces; the words are what is checked.
  const span = (first: string, last: string, byDay?: boolean) => daySpan(first, last, byDay).replace(/\s/gu, ' ')
  expect(span('2021-01-03', '2026-10-03')).toBe('Jan 2021 – Oct 2026')
  expect(span('2026-06-01', '2026-10-03')).toBe('Jun – Oct 2026')
  expect(span('2026-10-03', '2026-10-03')).toBe('Oct 2026')
  expect(span('2026-10-03', '2026-10-04', true)).toBe('3 – 4 Oct 2026')
})

it('counts one item in the singular', () => {
  expect(t('library.dayCount', { n: 1 })).toBe('1 item')
  expect(t('library.dayCount', { n: 1234 })).toBe('1,234 items')
  expect(t('collection.items', { n: 1 })).toBe('1 item')
})
