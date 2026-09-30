import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import { AlbumKind, DateSource, engine, Kind } from '../bridge'
import type * as Bridge from '../bridge'
import type { AssetDetail } from '../bridge'
import { emptyRule } from '../albums/store'
import DetailsPanel from './DetailsPanel.vue'

vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  onLibraryChanged: vi.fn(() => () => undefined),
  engine: {
    asset: vi.fn(),
    assetTags: vi.fn(),
    assetFaces: vi.fn(),
    assetAlbums: vi.fn(),
    albums: vi.fn(),
    createAlbum: vi.fn(),
    setAlbumItem: vi.fn(),
  },
}))

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => (resolve = done))
  return { promise, resolve }
}

const detail = (id: number, camera = ''): AssetDetail => ({
  id,
  camera,
  kind: Kind.KindPhoto,
  takenAt: 0,
  dateFrom: DateSource.DateFromFile,
  width: 100,
  height: 100,
  lat: null,
  lon: null,
  favourite: false,
  paths: [],
  size: 100,
  ext: '.jpg',
  webNative: true,
  video: null,
})
let wrapper: ReturnType<typeof mount> | undefined

beforeEach(() => {
  setActivePinia(createPinia())
  vi.mocked(engine.asset)
    .mockReset()
    .mockImplementation((id) => Promise.resolve(detail(id, 'Current')))
  vi.mocked(engine.assetTags).mockReset().mockResolvedValue([])
  vi.mocked(engine.assetFaces).mockReset().mockResolvedValue([])
  vi.mocked(engine.assetAlbums).mockReset().mockResolvedValue([])
  vi.mocked(engine.albums).mockReset().mockResolvedValue([])
  vi.mocked(engine.setAlbumItem).mockReset().mockResolvedValue(undefined)
})
afterEach(() => wrapper?.unmount())

it('keeps the newest details after selecting A, B, then A again', async () => {
  const old = deferred<AssetDetail>()
  vi.mocked(engine.asset).mockReturnValueOnce(old.promise)
  wrapper = mount(DetailsPanel, { props: { sel: { day: 'results', id: 1 } } })
  await wrapper.setProps({ sel: { day: 'results', id: 2 } })
  await flushPromises()
  await wrapper.setProps({ sel: { day: 'results', id: 1 } })
  await flushPromises()
  old.resolve(detail(1, 'Stale camera'))
  await flushPromises()
  expect(wrapper.text()).toContain('Current')
  expect(wrapper.text()).not.toContain('Stale camera')
})

it('does not clear the next selection draft when album creation finishes', async () => {
  const pending = deferred<number>()
  vi.mocked(engine.createAlbum).mockReturnValueOnce(pending.promise)
  wrapper = mount(DetailsPanel, { props: { sel: { day: 'results', id: 1 } } })
  await flushPromises()
  await wrapper.get('.album-new input').setValue('First album')
  await wrapper.get('.album-new').trigger('submit')
  await wrapper.setProps({ sel: { day: 'results', id: 2 } })
  await flushPromises()
  await wrapper.get('.album-new input').setValue('Second album')
  pending.resolve(4)
  await flushPromises()
  expect(engine.createAlbum).toHaveBeenCalledWith('First album', AlbumKind.AlbumManual, emptyRule(), [1])
  expect(wrapper.get<HTMLInputElement>('.album-new input').element.value).toBe('Second album')
})

it('puts an album checkbox back when the change fails', async () => {
  vi.mocked(engine.albums).mockResolvedValue([
    { id: 7, name: 'Trip', kind: AlbumKind.AlbumManual, rule: emptyRule(), count: 0, cover: 0, coverThumb: 0 },
  ])
  vi.mocked(engine.setAlbumItem).mockRejectedValueOnce(new Error('album deleted'))
  wrapper = mount(DetailsPanel, { props: { sel: { day: 'results', id: 1 } } })
  await flushPromises()
  const box = wrapper.get<HTMLInputElement>('.album-option input')
  await box.setValue(true)
  await flushPromises()
  expect(box.element.checked).toBe(false)
  expect(wrapper.text()).toContain('album deleted')
})
