import { expect, it } from 'vitest'
import { matchesSettings, useRowFilter } from './rowFilter'

it('finds everyday synonyms and labels across settings', () => {
  expect(matchesSettings(' captions ', 'viewer.subtitles')).toBe(true)
  expect(matchesSettings('DARKROOM', 'settings.theme')).toBe(true)
  expect(matchesSettings('RAW', 'settings.formats')).toBe(true)
  expect(matchesSettings('CPU', 'settings.threadsLabel')).toBe(true)
  expect(matchesSettings('captions', 'settings.theme')).toBe(false)
})

it('reads the latest search without keeping a stale filter', () => {
  let query = 'font'
  const shows = useRowFilter(() => query)
  expect(shows('settings.textSize')).toBe(true)
  query = 'unknown setting'
  expect(shows('settings.textSize')).toBe(false)
  query = ' '
  expect(shows('settings.textSize')).toBe(true)
})
