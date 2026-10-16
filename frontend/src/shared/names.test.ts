import { expect, it } from 'vitest'
import { MAX_NAME_LEN } from '../bridge'
import { compareNames, nameIncludes, nameLength, nameTooLong, trimName } from './names'

it('counts astral characters once and ignores surrounding whitespace', () => {
  expect(nameLength(' \t😀Ali😀\n ')).toBe(5)
  expect(nameTooLong(` \t${'😀'.repeat(MAX_NAME_LEN)}\n `)).toBe(false)
  expect(nameTooLong('😀'.repeat(MAX_NAME_LEN + 1))).toBe(true)
  expect(nameTooLong('a'.repeat(MAX_NAME_LEN + 1))).toBe(true)
})

it('trims the whitespace Go trims, including next-line but excluding the byte-order mark', () => {
  expect(nameLength('\u0085 Ali \u0085')).toBe(3)
  expect(nameLength('\uFEFFAli\uFEFF')).toBe(5)
  expect(nameLength(' \r\n\t ')).toBe(0)
})

it('trims names as Go does, unlike String.prototype.trim', () => {
  expect(trimName('\u0085 Ali \u0085')).toBe('Ali')
  expect(trimName('\uFEFFAli ')).toBe('\uFEFFAli')
  expect('\uFEFFAli '.trim()).toBe('Ali') // why trimName exists
})

it('filters literal names with Unicode simple folding', () => {
  expect(nameIncludes('Émile', 'ém')).toBe(true)
  expect(nameIncludes('Σίσυφος', 'ΣΊΣΥΦΟΣ')).toBe(true)
  expect(nameIncludes('Kelvin K', 'kelvin k')).toBe(true)
  expect(nameIncludes('Long ſ', 'long s')).toBe(true)
  expect(nameIncludes('İ', 'i')).toBe(false)
  expect(nameIncludes('A [literal].* name', '[literal].*')).toBe(true)
  expect(nameIncludes('Something else', '[literal].*')).toBe(false)
})

it('sorts accents beside their base letters and numbers by value', () => {
  expect(['Zoë', 'Trip 10', 'Émile', 'Trip 2', 'Ali'].sort(compareNames)).toEqual([
    'Ali',
    'Émile',
    'Trip 2',
    'Trip 10',
    'Zoë',
  ])
})
