import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createPinia, getActivePinia, setActivePinia } from 'pinia'
import { DOMWrapper, flushPromises } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import { defineComponent, h, nextTick, shallowRef } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import { mountInApp, recordNotifications } from '../shared/testing'
import { t } from '../shared/i18n'
import { usePreferences } from '../shared/preferences'
import type { Sel } from './store'
import {
  AlbumKind,
  BlockKind,
  DateSource,
  engine,
  INSPECTOR_WIDTH_DEFAULT,
  Kind,
  MAX_NAME_LEN,
  onLibraryChanged,
  Playback,
  TagKind,
  TagSource,
} from '../bridge'
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
    untagAsset: vi.fn().mockResolvedValue(undefined),
    rejectFaces: vi.fn().mockResolvedValue(undefined),
    nameFaces: vi.fn().mockResolvedValue(5),
    openMedia: vi.fn().mockResolvedValue(undefined),
    tagAsset: vi.fn(),
    showInFolder: vi.fn<typeof Bridge.engine.showInFolder>().mockResolvedValue(undefined),
    event: vi.fn(),
    setPreferences: vi.fn().mockImplementation((prefs) => Promise.resolve(prefs)),
    setTakenAt: vi.fn().mockResolvedValue(undefined),
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
  caps: { preview: true, original: true, render: false, paged: false, directPlay: false, convertPlay: false },
  video: null,
})
let wrapper: VueWrapper | undefined
const selection = shallowRef<Sel | null>(null)
// Whether the panel follows the library's focus, as LibraryView mounts it,
// rather than the item a test gives it.
const followFocus = shallowRef(false)

function mountDetails(sel: Sel | null) {
  selection.value = sel
  const lib = useLibrary()
  const Host = defineComponent({
    render: () => h(DetailsPanel, { sel: followFocus.value ? lib.selected : selection.value }),
  })
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
  followFocus.value = false
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

async function press(label: string) {
  const button = new DOMWrapper(document.body).findAll('button').find((button) => button.text() === label)
  if (!button) throw new Error(`Missing ${label}`)
  await button.trigger('click')
  await flushPromises()
}

it('validates full Unicode tag names in the add tag dialog without truncation', async () => {
  wrapper = mountDetails({ day: 'results', id: 1 })
  await flushPromises()
  await press(t('details.addTag'))
  const body = new DOMWrapper(document.body)
  const tag = body.get<HTMLInputElement>('[data-tag-draft]')
  expect(tag.attributes('maxlength')).toBeUndefined()
  const overlong = '😀'.repeat(MAX_NAME_LEN + 1)
  await tag.setValue(overlong)
  expect(tag.element.value).toBe(overlong)
  expect(tag.attributes('aria-invalid')).toBe('true')
  expect(body.get('[role="alert"]').text()).toContain(t('error.invalidName', { max: MAX_NAME_LEN }))
  await tag.trigger('keydown', { key: 'Enter' })
  expect(engine.tagAsset).not.toHaveBeenCalled()
  const valid = '😀'.repeat(MAX_NAME_LEN)
  await tag.setValue(` ${valid} `)
  expect(tag.attributes('aria-invalid')).toBeUndefined()
  await press(t('details.tag.save'))
  expect(engine.tagAsset).toHaveBeenCalledWith(1, valid)
})

it('validates full Unicode album names in the creation dialog without truncation', async () => {
  wrapper = mountDetails({ day: 'results', id: 1 })
  await flushPromises()
  await press(t('albums.createForItem'))
  const body = new DOMWrapper(document.body)
  const album = body.get<HTMLInputElement>(`input[aria-label="${t('albums.createForItem')}"]`)
  expect(album.attributes('maxlength')).toBeUndefined()
  const overlong = '😀'.repeat(MAX_NAME_LEN + 1)
  await album.setValue(overlong)
  expect(album.attributes('aria-invalid')).toBe('true')
  await album.trigger('keydown', { key: 'Enter' })
  expect(engine.createAlbum).not.toHaveBeenCalled()
  const valid = '😀'.repeat(MAX_NAME_LEN)
  await album.setValue(` ${valid} `)
  await body.get('form').trigger('submit')
  await flushPromises()
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
  await press(t('albums.createForItem'))
  await new DOMWrapper(document.body).get(`input[aria-label="${t('albums.createForItem')}"]`).setValue('First album')
  await new DOMWrapper(document.body).get('form').trigger('submit')
  await select({ day: 'results', id: 2 })
  await flushPromises()
  await press(t('albums.createForItem'))
  await new DOMWrapper(document.body).get(`input[aria-label="${t('albums.createForItem')}"]`).setValue('Second album')
  pending.resolve(4)
  await flushPromises()
  expect(engine.createAlbum).toHaveBeenCalledWith('First album', AlbumKind.AlbumManual, emptyRule(), [1])
  expect(
    new DOMWrapper(document.body).get<HTMLInputElement>(`input[aria-label="${t('albums.createForItem')}"]`).element
      .value,
  ).toBe('Second album')
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
  for (const title of ['Overview', 'Camera', 'Location', 'People', 'Things', 'Albums', 'Files']) {
    expect(wrapper.text()).toContain(title)
  }
  expect(wrapper.findAll('.meta-row').filter((row) => row.text().includes('Not available'))).toHaveLength(9)
  expect(wrapper.text()).toContain('Photo')
  expect(wrapper.text()).toContain(t('details.dateFrom.file'))
})

it('labels a corrected date with its retained original', async () => {
  vi.mocked(engine.asset).mockResolvedValueOnce({
    ...detail(1),
    dateFrom: DateSource.DateFromUser,
    originalDate: { takenAt: 1, from: DateSource.DateFromFile },
  })
  wrapper = mountDetails({ day: 'results', id: 1 })
  await flushPromises()
  expect(wrapper.text()).toContain('Corrected by you · original kept')
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

it('opens the medium-width inspector as a sheet and Escape returns focus to the grid trigger', async () => {
  vi.stubGlobal('innerWidth', 720)
  const trigger = document.createElement('button')
  trigger.textContent = 'Grid photo'
  document.body.append(trigger)
  trigger.focus()
  const lib = useLibrary()
  lib.showDetails = true
  wrapper = mountDetails({ day: 'results', id: 1 })
  await flushPromises()
  expect(wrapper.find('aside').exists()).toBe(false)
  const dialog = new DOMWrapper(document.body).get('[role="dialog"]')
  // The sheet keeps the width the user gave the inline inspector.
  expect(dialog.get('[data-slot="body"] > div').attributes('style')).toContain(`${INSPECTOR_WIDTH_DEFAULT}px`)
  await dialog.trigger('keydown', { key: 'Escape' })
  await flushPromises()
  expect(lib.showDetails).toBe(false)
  wrapper.unmount()
  await flushPromises()
  expect(document.activeElement).toBe(trigger)
})

/** Mounts the panel as the library does: on the item the library has in focus. */
function mountOnTheFocus() {
  followFocus.value = true
  return mountDetails(null)
}

it('shows one of several selected items at a time and corrects only the one shown', async () => {
  const lib = useLibrary()
  lib.buckets = [{ day: '2026-10-03', count: 3 }]
  lib.days = new Map([['2026-10-03', [1, 2, 3].map((id) => ({ id, w: 4, h: 3, t: 1 }))]])
  lib.selectedIDs = new Set([1, 3])
  wrapper = mountOnTheFocus()
  await flushPromises()
  // Nothing was in focus, so the panel takes the first selected item.
  expect(lib.selected).toEqual({ day: '2026-10-03', id: 1 })
  expect(vi.mocked(engine.asset).mock.calls.at(-1)?.[0]).toBe(1)
  const stepper = wrapper.get('[data-selection-step]')
  expect(stepper.text()).toContain(t('details.multiple', { n: '2' }))
  expect(stepper.text()).toContain(t('details.stepPosition', { i: '1', n: '2' }))
  expect(stepper.get('[data-step="previous"]').attributes('disabled')).toBeDefined()

  await stepper.get('[data-step="next"]').trigger('click')
  await flushPromises()
  // The next selected item, skipping the one not selected; the selection stays.
  expect(lib.selected).toEqual({ day: '2026-10-03', id: 3 })
  expect([...lib.selectedIDs]).toEqual([1, 3])
  expect(vi.mocked(engine.asset).mock.calls.at(-1)?.[0]).toBe(3)
  expect(wrapper.get('[data-step="next"]').attributes('disabled')).toBeDefined()

  await wrapper.get('[data-edit-date]').trigger('click')
  await flushPromises()
  const body = new DOMWrapper(document.body)
  await body.get('#correct-date-input').setValue('2020-05-01T10:00:00')
  await body.get('form#correct-date').trigger('submit')
  await flushPromises()
  expect(engine.setTakenAt).toHaveBeenCalledExactlyOnceWith([3], Date.UTC(2020, 4, 1, 10) / 1000)
})

it('moves the focus onto the selection when the item in focus is not selected', async () => {
  const lib = useLibrary()
  lib.buckets = [{ day: '2026-10-03', count: 3 }]
  lib.days = new Map([['2026-10-03', [1, 2, 3].map((id) => ({ id, w: 4, h: 3, t: 1 }))]])
  lib.selected = { day: '2026-10-03', id: 1 }
  lib.selectedIDs = new Set([2, 3])
  wrapper = mountOnTheFocus()
  await flushPromises()
  expect(lib.selected).toEqual({ day: '2026-10-03', id: 2 })
  expect(wrapper.get('[data-selection-step]').text()).toContain(t('details.stepPosition', { i: '1', n: '2' }))
})

it('removes a suggested tag immediately and Undo adds it back as a user choice', async () => {
  const notices = recordNotifications()
  try {
    vi.mocked(engine.assetTags)
      .mockResolvedValueOnce([{ id: 7, name: 'Sunset', source: TagSource.TagByAI, kind: TagKind.$zero, score: 0.9 }])
      .mockResolvedValueOnce([])
    wrapper = mountDetails({ day: 'results', id: 1 })
    await flushPromises()
    await wrapper.get(`button[aria-label="${t('details.removeTag', { name: 'Sunset' })}"]`).trigger('click')
    await flushPromises()
    expect(engine.untagAsset).toHaveBeenCalledWith(1, 7)
    notices.find('tag-1-7')?.actions?.[0]?.run()
    await flushPromises()
    expect(engine.tagAsset).toHaveBeenCalledWith(1, 'Sunset')
  } finally {
    notices.stop()
  }
})

it('corrects a wrong named face and Undo restores its named membership', async () => {
  const notices = recordNotifications()
  try {
    vi.mocked(engine.assetFaces)
      .mockResolvedValueOnce([faceOf(11, 'Amina')])
      .mockResolvedValueOnce([])
    wrapper = mountDetails({ day: 'results', id: 1 })
    await flushPromises()
    await wrapper.get('button[aria-label="Not Amina"]').trigger('click')
    await flushPromises()
    expect(engine.rejectFaces).toHaveBeenCalledWith(5, [11])
    notices.find('face-11')?.actions?.[0]?.run()
    await flushPromises()
    expect(engine.nameFaces).toHaveBeenCalledWith([11], 'Amina')
  } finally {
    notices.stop()
  }
})

it('offers recovery with the exact local path after a Windows hand-off fails', async () => {
  vi.mocked(engine.asset).mockResolvedValueOnce({ ...detail(1), paths: ['C:/Photos/a.jpg'] })
  vi.mocked(engine.openMedia).mockRejectedValueOnce(new Error('Drive disconnected'))
  wrapper = mountDetails({ day: 'results', id: 1 })
  await flushPromises()
  await wrapper.get(`button[aria-label="${t('files.open')}"]`).trigger('click')
  await flushPromises()
  const body = new DOMWrapper(document.body)
  expect(body.get('[role="dialog"]').text()).toContain('C:/Photos/a.jpg')
  expect(body.get('[role="alert"]').text()).toContain('Drive disconnected')
  await press(t('common.retry'))
  expect(engine.openMedia).toHaveBeenLastCalledWith(1, 'C:/Photos/a.jpg', false)
})
