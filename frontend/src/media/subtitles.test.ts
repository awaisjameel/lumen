import { expect, it } from 'vitest'
import { subtitleCues } from './subtitles'

it('reads timed cues with identifiers and settings while excluding malformed or reversed times', () => {
  expect(
    subtitleCues(
      'WEBVTT\n\nid\n00:00:01.000 --> 00:00:03.250 align:start\nFirst line\nSecond line\n\n00:00:05.000 --> 00:00:04.000\nInvalid',
    ),
  ).toEqual([{ start: 1, end: 3.25, text: 'First line\nSecond line' }])
})
