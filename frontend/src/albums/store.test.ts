import { expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { AlbumKind, engine, Kind } from '../bridge'
import type * as Bridge from '../bridge'
import type { Album } from '../bridge'
import { useAlbums } from './store'

vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  engine: { albums: vi.fn() },
}))

it('refreshes album membership counts after an overlapping read', async () => {
  setActivePinia(createPinia())
  let finish!: (value: Album[]) => void
  const album: Album = {
    id: 1,
    name: 'Trip',
    kind: AlbumKind.AlbumManual,
    count: 1,
    cover: 3,
    coverThumb: 1,
    rule: { kind: Kind.$zero, favourites: false, from: '', to: '', tag: 0, person: 0 },
  }
  vi.mocked(engine.albums)
    .mockReturnValueOnce(new Promise((resolve) => (finish = resolve)))
    .mockResolvedValueOnce([{ ...album, count: 2 }])
  const albums = useAlbums()
  const initial = albums.load()
  const afterEdit = albums.load()
  finish([album])
  await Promise.all([initial, afterEdit])
  expect(albums.byId.get(1)?.count).toBe(2)
})
