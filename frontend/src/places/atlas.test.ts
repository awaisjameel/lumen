import { expect, it } from 'vitest'
import type { Place } from '../bridge'
import { atlasDots, atlasMarkers, atlasPosition } from './atlas'
import { ATLAS_COLUMNS, ATLAS_ROWS, atlasCells } from './atlasData'

const lahore: Place = {
  id: 1,
  name: 'Lahore',
  country: 'Pakistan',
  region: 'Punjab',
  lat: 31.55,
  lon: 74.34,
  count: 3,
  cover: 0,
  coverThumb: 0,
}

it('projects real cities into the approved atlas coordinates', () => {
  const position = atlasPosition(lahore.lat, lahore.lon)
  expect(position.x).toBeCloseTo(67.3, 1)
  expect(position.y).toBeCloseTo(33.6, 1)
  expect(atlasPosition(48.86, 2.35).x).toBeCloseTo(47.3, 1)
  expect(atlasPosition(60.39, 5.32).y).toBeCloseTo(13.6, 1)
  expect(atlasPosition(0, -180)).toEqual(atlasPosition(0, 180))
})

it('decodes a compact complete cell grid without duplicate or out-of-range dots', () => {
  expect(atlasCells).toHaveLength(ATLAS_ROWS)
  expect(atlasDots.length).toBeGreaterThan(1000)
  expect(new Set(atlasDots.map((dot) => dot.key)).size).toBe(atlasDots.length)
  expect(
    atlasDots.every(
      (dot) => dot.x > 0 && dot.x < 100 && dot.y > 0 && dot.y < 100 && dot.key < ATLAS_COLUMNS * ATLAS_ROWS,
    ),
  ).toBe(true)
})

it('clusters nearby cities at small sizes while preserving every city as a choice', () => {
  const skardu = { ...lahore, id: 2, name: 'Skardu', lat: 35.3, lon: 75.63 }
  const paris = { ...lahore, id: 3, name: 'Paris', lat: 48.86, lon: 2.35 }
  const narrow = atlasMarkers([lahore, skardu, paris], 360)
  expect(narrow).toHaveLength(2)
  expect(narrow[0]?.places.map((p) => p.id)).toEqual([1, 2])
  expect(
    narrow
      .flatMap((m) => m.places)
      .map((p) => p.id)
      .sort(),
  ).toEqual([1, 2, 3])
  expect(atlasMarkers([lahore, skardu, paris], 3200)).toHaveLength(3)
})
