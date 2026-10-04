import { expect, it } from 'vitest'
import { ORGANISE_DEFAULT_TEMPLATE } from '../bridge'
import { renderExample, unknownTokens } from './template'
import type { Example } from './template'
import parity from './template.parity.json'

// 5 August 2024, 14:30 on the camera's clock.
const takenAt = Date.UTC(2024, 7, 5, 14, 30) / 1000
const photo: Example = {
  takenAt,
  event: 'Beach day',
  place: 'Karachi',
  camera: 'Pixel 8',
  path: 'D:\\Photos\\2024\\IMG_0042.JPG',
}

it('shows what the default scheme does to a real photo', () => {
  expect(renderExample(ORGANISE_DEFAULT_TEMPLATE, photo)).toBe('2024\\08\\Beach day\\IMG_0042.JPG')
})

it('fills every token from the photo and keeps the file’s own extension', () => {
  expect(renderExample('{Year}-{Month}-{Day}/{MonthName}/{Place}/{Camera}/{Ext}/{Name}', photo)).toBe(
    '2024-08-05\\August\\Karachi\\Pixel 8\\jpg\\IMG_0042.JPG',
  )
})

it('uses the engine’s fallbacks where the photo has no event, place or camera', () => {
  const bare: Example = { ...photo, event: '', place: '  ', camera: '' }
  expect(renderExample('{Event}/{Place}/{Camera}/{Name}', bare)).toBe(
    'Unknown event\\Unknown place\\Unknown camera\\IMG_0042.JPG',
  )
})

it('makes names Windows accepts, as the engine does', () => {
  const messy: Example = { ...photo, event: 'Eid: "Mubarak"? a/b...', camera: '   ' }
  expect(renderExample('{Event}/{Camera}/{Name}', messy)).toBe('Eid_ _Mubarak__ a_b\\Unknown camera\\IMG_0042.JPG')
})

it('accepts either kind of slash and ignores empty folders', () => {
  expect(renderExample('{Year}\\\\{Month}//{Name}', photo)).toBe('2024\\08\\IMG_0042.JPG')
  expect(renderExample('', photo)).toBe('')
})

it('leaves a token it does not know as it was typed', () => {
  expect(renderExample('{Year}/{Nope}/{Name}', photo)).toBe('2024\\{Nope}\\IMG_0042.JPG')
})

// The engine's fileops tests check the same cases.
it.each(parity)('renders $case as the engine does', ({ template, example, want }) => {
  expect(renderExample(template, example)).toBe(want)
})

it('reads the date as the camera’s clock, not this PC’s zone', () => {
  const lateNight: Example = { ...photo, takenAt: Date.UTC(2024, 11, 31, 23, 59) / 1000 }
  expect(renderExample('{Year}/{Month}/{Day}/{Name}', lateNight)).toBe('2024\\12\\31\\IMG_0042.JPG')
})

it('lists each unknown token once', () => {
  expect(unknownTokens('{Year}/{Nope}/{Nope}/{year}/{Name}')).toEqual(['Nope', 'year'])
  expect(unknownTokens('{Year}/{Month}/{Name}')).toEqual([])
  expect(unknownTokens('plain/{}')).toEqual([''])
})
