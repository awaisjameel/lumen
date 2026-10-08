import { expect, it } from 'vitest'
import { preferredSubtitle, subtitleCues } from './subtitles'

it('reads timed cues with identifiers and settings while excluding malformed or reversed times', () => {
  expect(
    subtitleCues(
      'WEBVTT\n\nid\n00:00:01.000 --> 00:00:03.250 align:start\nFirst line\nSecond line\n\n00:00:05.000 --> 00:00:04.000\nInvalid',
    ),
  ).toEqual([{ start: 1, end: 3.25, text: 'First line\nSecond line' }])
})

it('chooses the subtitle track in the preferred language, else the first that can be shown', () => {
  const tracks = [
    { index: 2, language: 'ger', supported: true },
    { index: 3, language: 'eng', supported: false }, // a bitmap track
    { index: 4, language: 'eng', supported: true },
    { index: 5, language: '', supported: true },
  ]
  expect(preferredSubtitle(tracks, 'en')).toBe(4)
  expect(preferredSubtitle(tracks, 'EN-gb')).toBe(4)
  expect(preferredSubtitle(tracks, 'fr')).toBe(2)
  expect(preferredSubtitle(tracks, '')).toBe(2)
  expect(preferredSubtitle([{ index: 3, language: 'eng', supported: false }], 'en')).toBe(-1)
  expect(preferredSubtitle([], 'en')).toBe(-1)
})
