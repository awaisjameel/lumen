import { expect, it } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useMutationWrites } from './mutationWrites'

it('separates field and album ownership while sharing each domain across the app', () => {
  setActivePinia(createPinia())
  const registry = useMutationWrites()
  const date = registry.owner('date').begin([1])
  const place = registry.owner('place').begin([1])
  const album = registry.owner('album:8').begin([1])
  const anotherAlbum = registry.owner('album:9').begin([1])
  useMutationWrites().owner('album:8').invalidate()
  expect(date.current(1)).toBe(true)
  expect(place.current(1)).toBe(true)
  expect(album.current(1)).toBe(false)
  expect(anotherAlbum.current(1)).toBe(true)
  const replacement = registry.owner('album:8').begin([1])
  expect(album.current(1)).toBe(false)
  expect(replacement.current(1)).toBe(true)
  for (const lease of [date, place, album, anotherAlbum, replacement]) lease.release()
})
