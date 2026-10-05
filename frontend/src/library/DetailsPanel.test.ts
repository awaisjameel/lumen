import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createPinia, getActivePinia, setActivePinia } from 'pinia'
import { DOMWrapper, flushPromises } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import { defineComponent, h, nextTick, shallowRef } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import { mountInApp } from '../shared/testing'
import { t } from '../shared/i18n'
import { usePreferences } from '../shared/preferences'
import type { Sel } from './store'
import { AlbumKind, BlockKind, DateSource, engine, Kind, MAX_NAME_LEN, onLibraryChanged, Playback } from '../bridge'
import type * as Bridge from '../bridge'
import type * as Block from '../shared/block'
import type { AssetDetail, Changes, Event, Face } from '../bridge'
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
    migratePreferences: vi.fn<typeof Bridge.engine.migratePreferences>(() =>
      Promise.resolve({ ...usePreferences().prefs }),
    ),
    asset: vi.fn(),
    inspect: vi.fn<typeof Bridge.engine.inspect>().mockResolvedValue({
      orientation: 1,
      hasProfile: false,
      profile: '',
      lens: '',
      exposure: '',
      aperture: '',
      iso: '',
      frameRate: '',
      pages: 0,
      tracks: [],
    }),
    assetTags: vi.fn(),
    assetFaces: vi.fn(),
    assetAlbums: vi.fn(),
    albums: vi.fn(),
    createAlbum: vi.fn(),
    setAlbumItem: vi.fn(),
    place: vi.fn(),
    tagAsset: vi.fn(),
    showInFolder: vi.fn<typeof Bridge.engine.showInFolder>().mockResolvedValue(undefined),
    event: vi.fn(),
    setPreferences: vi.fn().mockImplementation((prefs) => Promise.resolve(prefs)),
  },
}))

function deferred<T>() {
  let complete: ((value: T) => void) | undefined
  const promise = new Promise<T>((done) => {
    complete = done
  })
  return {
    promise,
    resolve(value: T) {
      if (!complete) throw new Error('Deferred promise was not initialised')
      complete(value)
    },
  }
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
  caps: { preview: true, original: true, render: false, directPlay: false, convertPlay: false },
  video: null,
})
let wrapper: VueWrapper | undefined
const selection = shallowRef<Sel | null>(null)

function mountDetails(sel: Sel | null) {
  selection.value = sel
  const Host = defineComponent({ render: () => h(DetailsPanel, { sel: selection.value }) })
  const pinia = getActivePinia()
  if (!pinia) throw new Error('No active test Pinia')
  const Page = { render: () => h('div') }
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: Page },
      { path: '/people/:id', name: 'person', component: Page },
      { path: '/things/:id', name: 'thing', component: Page },
    ],
  })
  return mountInApp(Host, { plugins: [pinia, router] })
}

async function select(sel: Sel | null) {
  selection.value = sel
  await nextTick()
}

beforeEach(() => {
  vi.stubGlobal('innerWidth', 1200)
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
  document.body.innerHTML = ''
  vi.unstubAllGlobals()
})

function liveChanges() {
  const callbacks: ((change: Changes) => void)[] = []
  vi.mocked(onLibraryChanged).mockImplementation((callback) => {
    callbacks.push(callback)
    return () => undefined
  })
  return (change: Changes) => callbacks.forEach((callback) => callback(change))
}

it('opens the file copy whose folder button was chosen', async () => {
  const paths = ['C:/Photos/first.jpg', 'D:/Backup/second.jpg']
  vi.mocked(engine.asset).mockResolvedValueOnce({ ...detail(1), paths })
  wrapper = mountDetails({ day: '2026-01-01', id: 1 })
  await flushPromises()
  const buttons = wrapper.findAll('button').filter((b) => b.attributes('aria-label') === t('common.showInFolder'))
  expect(buttons).toHaveLength(2)
  await buttons[1]?.trigger('click')
  expect(engine.showInFolder).toHaveBeenCalledExactlyOnceWith(1, paths[1])
})

it('does not reload metadata for grid updates or tag-only changes', async () => {
  vi.useFakeTimers()
  const changed = liveChanges()
  const lib = useLibrary()
  lib.days = new Map([['results', [{ id: 1, w: 100, h: 100, t: 1, f: false }]]])
  wrapper = mountDetails({ day: 'results', id: 1 })
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
  wrapper = mountDetails({ day: 'results', id: 1 })
  await flushPromises()
  expect(wrapper.text()).not.toContain('Current')
  await wrapper.get('.album-option [role="checkbox"]').trigger('click')
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
  wrapper = mountDetails({ day: 'results', id: 1 })
  await flushPromises()
  const pending = deferred<AssetDetail>()
  vi.mocked(engine.asset).mockReturnValueOnce(pending.promise)
  changed({ assets: true, thumbs: false, tags: false, people: false })
  await vi.advanceTimersByTimeAsync(3000)
  await select({ day: 'results', id: 2 })
  await flushPromises()
  pending.resolve(detail(1, 'Stale indexed camera'))
  await flushPromises()
  expect(wrapper.text()).toContain('Current')
  expect(wrapper.text()).not.toContain('Stale indexed camera')
})

it('validates full Unicode names without truncation and rejects overlong tag and album drafts', async () => {
  wrapper = mountDetails({ day: 'results', id: 1 })
  await flushPromises()
  const tag = wrapper.get<HTMLInputElement>('[data-tag-draft]')
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
  wrapper = mountDetails({ day: 'results', id: 1 })
  await select({ day: 'results', id: 2 })
  await flushPromises()
  await select({ day: 'results', id: 1 })
  await flushPromises()
  old.resolve(detail(1, 'Stale camera'))
  await flushPromises()
  expect(wrapper.text()).toContain('Current')
  expect(wrapper.text()).not.toContain('Stale camera')
})

it('does not clear the next selection draft when album creation finishes', async () => {
  const pending = deferred<number>()
  vi.mocked(engine.createAlbum).mockReturnValueOnce(pending.promise)
  wrapper = mountDetails({ day: 'results', id: 1 })
  await flushPromises()
  await wrapper.get('.album-new input').setValue('First album')
  await wrapper.get('.album-new').trigger('submit')
  await select({ day: 'results', id: 2 })
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
  wrapper = mountDetails({ day: 'results', id: 1 })
  await flushPromises()
  const box = wrapper.get('.album-option [role="checkbox"]')
  await box.trigger('click')
  await flushPromises()
  expect(box.attributes('aria-checked')).toBe('false')
  expect(wrapper.text()).toContain('album deleted')
})

it('preserves the drive-root separator when blocking a root-level file folder', async () => {
  vi.mocked(engine.asset).mockResolvedValueOnce({ ...detail(1), paths: ['C:\\photo.jpg'] })
  wrapper = mountDetails({ day: 'results', id: 1 })
  await flushPromises()
  await wrapper.get('[data-block-folder]').trigger('click')
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
  wrapper = mountDetails({ day: 'results', id: 1 })
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
  wrapper = mountDetails({ day: 'results', id: 1 })
  await flushPromises()
  const asked = vi.mocked(engine.assetFaces).mock.calls.length
  await wrapper.get('.block-face').trigger('click')
  await flushPromises()
  expect(vi.mocked(engine.assetFaces).mock.calls).toHaveLength(asked)
})

const eventOf = (name: string): Event => ({
  id: 1,
  name,
  startAt: 0,
  endAt: 0,
  count: 2,
  cover: 1,
  coverThumb: 0,
  placeId: 0,
  pinned: false,
})

it('offers to block the event holding the photo, titled with its name', async () => {
  vi.mocked(engine.event).mockReset().mockResolvedValueOnce(eventOf('Wedding'))
  vi.mocked(block).mockResolvedValueOnce(true)
  wrapper = mountDetails({ day: 'results', id: 1 })
  await flushPromises()
  await wrapper.get('.block-event').trigger('click')
  await flushPromises()
  expect(engine.event).toHaveBeenCalledWith(1)
  expect(block).toHaveBeenLastCalledWith(BlockKind.BlockEvent, 1, 'Wedding')
})

it('blocks nothing and says why when the photo is in no event it can show', async () => {
  vi.mocked(engine.event).mockReset().mockRejectedValueOnce(new Error('not found'))
  vi.mocked(block).mockClear()
  wrapper = mountDetails({ day: 'results', id: 1 })
  await flushPromises()
  await wrapper.get('.block-event').trigger('click')
  await flushPromises()
  expect(block).not.toHaveBeenCalled()
  expect(wrapper.text()).toContain('not found')
})

it('states missing metadata rather than inventing a date, camera or location', async () => {
  vi.mocked(engine.asset).mockResolvedValueOnce({ ...detail(1), width: 0, height: 0, size: 0 })
  wrapper = mountDetails({ day: 'results', id: 1 })
  await flushPromises()
  for (const title of ['Overview', 'Camera', 'Location', 'People', 'Tags', 'Albums', 'Files']) {
    expect(wrapper.text()).toContain(title)
  }
  expect(wrapper.findAll('.meta-row').filter((row) => row.text().includes('Not available'))).toHaveLength(12)
  expect(wrapper.text()).toContain('Photo')
  expect(wrapper.text()).toContain(t('details.dateFrom.file'))
})

it('resizes the inline inspector from its left edge and saves through the preference owner', async () => {
  wrapper = mountDetails({ day: 'results', id: 1 })
  await flushPromises()
  const prefs = usePreferences()
  const before = prefs.prefs.inspectorWidth
  await wrapper.get('[role="separator"]').trigger('keydown', { key: 'ArrowLeft' })
  await flushPromises()
  expect(prefs.prefs.inspectorWidth).toBe(before + 16)
  expect(wrapper.get('aside').attributes('style')).toContain(`width: ${before + 16}px`)
  expect(engine.setPreferences).toHaveBeenCalledWith(expect.objectContaining({ inspectorWidth: before + 16 }))
})

it('renders a narrow inspector as a sheet and closes it without closing the viewer', async () => {
  vi.stubGlobal('innerWidth', 480)
  const lib = useLibrary()
  lib.showDetails = true
  lib.viewer = { day: 'results', id: 1 }
  wrapper = mountDetails({ day: 'results', id: 1 })
  await flushPromises()
  const body = new DOMWrapper(document.body)
  expect(body.find('aside').exists()).toBe(false)
  expect(body.get('[role="dialog"]').text()).toContain('Current')
  await body.get('button[aria-label="Close"]').trigger('click')
  expect(lib.showDetails).toBe(false)
  expect(lib.viewer).toEqual({ day: 'results', id: 1 })
})

it('replaces metadata with a not-found state when a live read loses access to the photo', async () => {
  vi.useFakeTimers()
  const changed = liveChanges()
  wrapper = mountDetails({ day: 'results', id: 1 })
  await flushPromises()
  expect(wrapper.text()).toContain('Current')
  vi.mocked(engine.asset).mockRejectedValueOnce(new Error('not found'))
  changed({ assets: true, thumbs: false, tags: false, people: false })
  await vi.advanceTimersByTimeAsync(3000)
  await flushPromises()
  expect(wrapper.get('[role="alert"]').text()).toContain('not found')
  expect(wrapper.text()).not.toContain('Current')
})

it('does not roll back an album checkbox on the next photo when an earlier edit fails', async () => {
  let rejectEdit: ((reason: Error) => void) | undefined
  const pending = new Promise<void>((_resolve, reject) => {
    rejectEdit = reject
  })
  vi.mocked(engine.albums).mockResolvedValue([
    { id: 7, name: 'Trip', kind: AlbumKind.AlbumManual, rule: emptyRule(), count: 0, cover: 0, coverThumb: 0 },
  ])
  vi.mocked(engine.setAlbumItem).mockReturnValueOnce(pending)
  vi.mocked(engine.assetAlbums).mockResolvedValueOnce([7]).mockResolvedValueOnce([])
  wrapper = mountDetails({ day: 'results', id: 1 })
  await flushPromises()
  await wrapper.get('[role="checkbox"]').trigger('click')
  await select({ day: 'results', id: 2 })
  await flushPromises()
  if (!rejectEdit) throw new Error('Album edit was not initialised')
  rejectEdit(new Error('old edit failed'))
  await flushPromises()
  expect(wrapper.get('[role="checkbox"]').attributes('aria-checked')).toBe('false')
  expect(wrapper.text()).not.toContain('old edit failed')
})

it('names the colour profile a photo embeds, or says only that it has one', async () => {
  const inspection = {
    orientation: 1,
    hasProfile: true,
    profile: 'Display P3',
    lens: '',
    exposure: '',
    aperture: '',
    iso: '',
    frameRate: '',
    pages: 1,
    tracks: [],
  }
  const profileRow = () =>
    wrapper
      ?.get(`section[aria-label="${t('details.metadata')}"]`)
      .findAll('.meta-row')
      .find((row) => row.text().includes(t('details.profile')))
      ?.text()
  vi.mocked(engine.inspect).mockResolvedValueOnce(inspection)
  wrapper = mountDetails({ day: 'results', id: 1 })
  await flushPromises()
  expect(profileRow()).toContain('Display P3')
  wrapper.unmount()

  vi.mocked(engine.inspect).mockResolvedValueOnce({ ...inspection, profile: '' })
  wrapper = mountDetails({ day: 'results', id: 1 })
  await flushPromises()
  expect(profileRow()).toContain(t('details.profileEmbedded'))
  wrapper.unmount()

  vi.mocked(engine.inspect).mockResolvedValueOnce({ ...inspection, hasProfile: false, profile: '' })
  wrapper = mountDetails({ day: 'results', id: 1 })
  await flushPromises()
  expect(profileRow()).not.toContain(t('details.profileEmbedded'))
})

it('lists only what a video records and offers to copy only the values it has', async () => {
  vi.mocked(engine.asset).mockResolvedValueOnce({
    ...detail(1, 'Fixture Cam'),
    kind: Kind.KindVideo,
    video: { duration: 1000, codec: 'h264', audio: 'aac', play: Playback.PlayDirect },
  })
  wrapper = mountDetails({ day: 'results', id: 1 })
  await flushPromises()
  const section = wrapper.get(`section[aria-label="${t('details.metadata')}"]`)
  const labels = section.findAll('.meta-row').map((row) => row.text())
  expect(labels.some((text) => text.includes(t('details.frameRate')))).toBe(true)
  expect(labels.some((text) => text.includes(t('details.lens')))).toBe(false)
  expect(section.find(`button[aria-label="${t('files.copyField', { name: t('details.camera') })}"]`).exists()).toBe(
    true,
  )
  expect(section.find(`button[aria-label="${t('files.copyField', { name: t('details.frameRate') })}"]`).exists()).toBe(
    false,
  )
})
