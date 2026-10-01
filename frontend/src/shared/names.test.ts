import { expect, it } from 'vitest'
import { MAX_NAME_LEN } from '../bridge'
import { nameLength, nameTooLong } from './names'

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
