import { ATLAS_COLUMNS, ATLAS_ROWS, ATLAS_WEST, ATLAS_NORTH, ATLAS_SOUTH, atlasCells } from './atlasData'
import type { Place } from '../bridge'

/** The same cylindrical projection as the offline city-cell atlas. */
export function atlasPosition(lat: number, lon: number) {
  return {
    x: ((((lon - ATLAS_WEST) % 360) + 360) % 360) / 3.6,
    y: Math.max(0, Math.min(100, ((ATLAS_NORTH - lat) / (ATLAS_NORTH - ATLAS_SOUTH)) * 100)),
  }
}

export const atlasDots = atlasCells.flatMap((row, y) =>
  BigInt(`0x${row}`)
    .toString(2)
    .padStart(ATLAS_COLUMNS, '0')
    .split('')
    .flatMap((cell, x) =>
      cell === '1'
        ? [{ key: y * ATLAS_COLUMNS + x, x: ((x + 0.5) / ATLAS_COLUMNS) * 100, y: ((y + 0.5) / ATLAS_ROWS) * 100 }]
        : [],
    ),
)

/** Nearby cities share a marker, so each 24-pixel target remains reachable. */
export function atlasMarkers(places: Place[], width: number) {
  const remaining = places.map((place) => ({ place, ...atlasPosition(place.lat, place.lon) }))
  const groups: { id: number; x: number; y: number; places: Place[] }[] = []
  while (remaining.length) {
    const first = remaining.shift()
    if (!first) break
    const members = [first]
    for (const member of members) {
      for (let j = remaining.length - 1; j >= 0; j--) {
        const next = remaining[j]
        if (next && Math.hypot(((member.x - next.x) / 100) * width, (((member.y - next.y) / 100) * width) / 2.5) < 32) {
          members.push(next)
          remaining.splice(j, 1)
        }
      }
    }
    groups.push({ id: first.place.id, x: first.x, y: first.y, places: members.map((m) => m.place) })
  }
  return groups
}
