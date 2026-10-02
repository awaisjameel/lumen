import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import { AlbumKind, BlockKind, DateSource, engine, Kind, MAX_NAME_LEN, onLibraryChanged } from '../bridge'
import type * as Bridge from '../bridge'
import type * as Block from '../shared/block'
import type { AssetDetail, Changes, Face } from '../bridge'
import { emptyRule } from '../albums/store'
import { block } from '../shared/block'
import { useLibrary } from './store'
import DetailsPanel from './DetailsPanel.vue'

vi.mock('../shared/block', async (importOriginal) => ({
  ...(await importOriginal<typeof Block>()),
  block: vi.fn().mockResolvedValue(false),
}))

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
    place: vi.fn(),
    tagAsset: vi.fn(),
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
  placeId: 0,
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
  vi.mocked(engine.createAlbum).mockReset().mockResolvedValue(4)
  vi.mocked(engine.place).mockReset()
  vi.mocked(engine.tagAsset).mockReset().mockResolvedValue(undefined)
  vi.mocked(onLibraryChanged)
    .mockReset()
    .mockReturnValue(() => undefined)
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  vi.useRealTimers()
})

function liveChanges() {
  const callbacks: ((change: Changes) => void)[] = []
  vi.mocked(onLibraryChanged).mockImplementation((callback) => {
    callbacks.push(callback)
    return () => undefined
  })
  return (change: Changes) => callbacks.forEach((callback) => callback(change))
}

it('does not reload metadata for grid updates or tag-only changes', async () => {
  vi.useFakeTimers()
  const changed = liveChanges()
  const lib = useLibrary()
  lib.days = new Map([['results', [{ id: 1, w: 100, h: 100, t: 1, f: false }]]])
  wrapper = mount(DetailsPanel, { props: { sel: { day: 'results', id: 1 } } })
  await flushPromises()
  lib.days = new Map([['results', [{ id: 1, w: 100, h: 100, t: 2, f: false }]]])
  changed({ assets: false, thumbs: false, tags: true, people: false })
  await vi.advanceTimersByTimeAsync(3000)
  await flushPromises()
  expect(engine.asset).toHaveBeenCalledTimes(1)
})

it('refreshes metadata and the place action on asset changes without clearing an edit failure', async () => {
  vi.useFakeTimers()
  const changed = liveChanges()
  vi.mocked(engine.asset).mockResolvedValueOnce({ ...detail(1), width: 0, height: 0 })
  vi.mocked(engine.setAlbumItem).mockRejectedValueOnce(new Error('album edit failed'))
  vi.mocked(engine.albums).mockResolvedValue([
    { id: 7, name: 'Trip', kind: AlbumKind.AlbumManual, rule: emptyRule(), count: 0, cover: 0, coverThumb: 0 },
  ])
  wrapper = mount(DetailsPanel, { props: { sel: { day: 'results', id: 1 } } })
  await flushPromises()
  expect(wrapper.text()).not.toContain('Current')
  await wrapper.get('.album-option input').setValue(true)
  await flushPromises()
  vi.mocked(engine.asset).mockResolvedValueOnce({
    ...detail(1, 'Indexed camera'),
    width: 3000,
    height: 2000,
    lat: 24.8,
    lon: 67.0,
    placeId: 9,
  })
  vi.mocked(engine.place).mockResolvedValueOnce({
    id: 9,
    name: 'Karachi',
    count: 1,
    cover: 1,
    coverThumb: 1,
    lat: 24.8,
    lon: 67.0,
    country: 'PK',
    region: 'Sindh',
  })
  changed({ assets: true, thumbs: false, tags: false, people: false })
  await vi.advanceTimersByTimeAsync(3000)
  await flushPromises()
  expect(wrapper.text()).toContain('Indexed camera')
  expect(wrapper.text()).toContain('3,000')
  expect(wrapper.text()).toContain('24.80000, 67.00000')
  expect(wrapper.text()).toContain('Karachi')
  expect(wrapper.text()).toContain('album edit failed')
  const button = wrapper.findAll('button').find((button) => button.text() === 'Block this place')
  expect(button).toBeDefined()
  await button?.trigger('click')
  expect(block).toHaveBeenCalledWith(BlockKind.BlockPlace, 9, 'Karachi')
})

it('ignores an older live metadata response after changing selection', async () => {
  vi.useFakeTimers()
  const changed = liveChanges()
  wrapper = mount(DetailsPanel, { props: { sel: { day: 'results', id: 1 } } })
  await flushPromises()
  const pending = deferred<AssetDetail>()
  vi.mocked(engine.asset).mockReturnValueOnce(pending.promise)
  changed({ assets: true, thumbs: false, tags: false, people: false })
  await vi.advanceTimersByTimeAsync(3000)
  await wrapper.setProps({ sel: { day: 'results', id: 2 } })
  await flushPromises()
  pending.resolve(detail(1, 'Stale indexed camera'))
  await flushPromises()
  expect(wrapper.text()).toContain('Current')
  expect(wrapper.text()).not.toContain('Stale indexed camera')
})

it('validates full Unicode names without truncation and rejects overlong tag and album drafts', async () => {
  wrapper = mount(DetailsPanel, { props: { sel: { day: 'results', id: 1 } } })
  await flushPromises()
  const tag = wrapper.get<HTMLInputElement>('.add-tag')
  const album = wrapper.get<HTMLInputElement>('.album-new input')
  expect(tag.attributes('maxlength')).toBeUndefined()
  expect(album.attributes('maxlength')).toBeUndefined()
  const overlong = '😀'.repeat(MAX_NAME_LEN + 1)
  await tag.setValue(overlong)
  await album.setValue(overlong)
  expect(tag.element.value).toBe(overlong)
  expect(tag.attributes('aria-invalid')).toBe('true')
  expect(album.attributes('aria-invalid')).toBe('true')
  expect(wrapper.get<HTMLButtonElement>('.album-new button').element.disabled).toBe(true)
  expect(wrapper.findAll('[role="alert"]')).toHaveLength(2)
  await tag.trigger('keydown', { key: 'Enter' })
  await wrapper.get('.album-new').trigger('submit')
  expect(engine.tagAsset).not.toHaveBeenCalled()
  expect(engine.createAlbum).not.toHaveBeenCalled()
  const valid = '😀'.repeat(MAX_NAME_LEN)
  await tag.setValue(` ${valid} `)
  await album.setValue(` ${valid} `)
  expect(tag.attributes('aria-invalid')).toBeUndefined()
  expect(album.attributes('aria-invalid')).toBeUndefined()
  await tag.trigger('keydown', { key: 'Enter' })
  await wrapper.get('.album-new').trigger('submit')
  await flushPromises()
  expect(engine.tagAsset).toHaveBeenCalledWith(1, valid)
  expect(engine.createAlbum).toHaveBeenCalledWith(valid, AlbumKind.AlbumManual, emptyRule(), [1])
})

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

it('preserves the drive-root separator when blocking a root-level file folder', async () => {
  vi.mocked(engine.asset).mockResolvedValueOnce({ ...detail(1), paths: ['C:\\photo.jpg'] })
  wrapper = mount(DetailsPanel, { props: { sel: { day: 'results', id: 1 } } })
  await flushPromises()
  await wrapper.get('.path button').trigger('click')
  expect(block).toHaveBeenCalledWith(BlockKind.BlockFolder, 0, 'C:\\', false, 'C:\\')
})

const faceOf = (id: number, name: string): Face => ({
  id,
  assetId: 1,
  thumb: 0,
  assetW: 400,
  assetH: 300,
  video: false,
  frame: 0,
  x: 0.1,
  y: 0.1,
  w: 0.2,
  h: 0.2,
  person: name ? 5 : 0,
  name,
  byUser: false,
})

it('offers to block each face, named or not, and shows the faces left afterwards', async () => {
  vi.mocked(engine.assetFaces)
    .mockResolvedValueOnce([faceOf(11, 'Ali'), faceOf(12, '')])
    .mockResolvedValueOnce([faceOf(12, '')])
  vi.mocked(block).mockResolvedValueOnce(true)
  wrapper = mount(DetailsPanel, { props: { sel: { day: 'results', id: 1 } } })
  await flushPromises()
  const buttons = wrapper.findAll('.block-face')
  expect(buttons.map((b) => b.text())).toEqual(['Block this face…', 'Block this face…'])
  await buttons[0]?.trigger('click')
  await flushPromises()
  expect(block).toHaveBeenCalledWith(BlockKind.BlockFace, 11, 'Ali')
  expect(wrapper.findAll('.faces li')).toHaveLength(1) // the blocked face has left the list
  await wrapper.get('.block-face').trigger('click')
  expect(block).toHaveBeenLastCalledWith(BlockKind.BlockFace, 12, 'this face')
})

it('leaves the faces as they were when the face block is cancelled', async () => {
  vi.mocked(engine.assetFaces).mockResolvedValue([faceOf(11, 'Ali')])
  vi.mocked(block).mockResolvedValueOnce(false)
  wrapper = mount(DetailsPanel, { props: { sel: { day: 'results', id: 1 } } })
  await flushPromises()
  const asked = vi.mocked(engine.assetFaces).mock.calls.length
  await wrapper.get('.block-face').trigger('click')
  await flushPromises()
  expect(vi.mocked(engine.assetFaces).mock.calls).toHaveLength(asked)
})
