import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { DOMWrapper, flushPromises } from '@vue/test-utils'
import { DateSource, engine, ErrorCode, Filter, Kind } from '../bridge'
import type * as Bridge from '../bridge'
import type * as Confirm from '../shared/confirm'
import type { AssetDetail } from '../bridge'
import DetailsPanel from '../library/DetailsPanel.vue'
import Viewer from '../library/Viewer.vue'
import { useLibrary } from '../library/store'
import { confirm } from '../shared/confirm'
import { ERROR_NOTIFICATION } from '../shared/errors'
import { mountInApp, recordNotifications } from '../shared/testing'
import type { RecordedNotifications } from '../shared/testing'
import { deleteAllFromDisk, deleteFromDisk, KEPT_NOTIFICATION, restoreAll } from './actions'
import TrashBar from './TrashBar.vue'
import { showTrashUndo, TRASH_UNDO_NOTIFICATION } from './undo'

vi.mock('../shared/confirm', async (importOriginal) => ({
  ...(await importOriginal<typeof Confirm>()),
  confirm: vi.fn(),
}))

vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  onLibraryChanged: vi.fn(() => () => undefined),
  engine: {
    timeline: vi.fn(),
    days: vi.fn(),
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
    place: vi.fn(),
    trashAssets: vi.fn(),
    restoreAssets: vi.fn(),
    restoreAllTrash: vi.fn(),
    trashCount: vi.fn(),
    trashSelection: vi.fn(),
    deleteFromDisk: vi.fn(),
  },
}))

const day = '2026-09-01'
const detail = (id: number): AssetDetail => ({
  id,
  camera: '',
  kind: Kind.KindPhoto,
  takenAt: 0,
  dateFrom: DateSource.DateFromFile,
  width: 100,
  height: 100,
  lat: null,
  lon: null,
  placeId: 0,
  favourite: false,
  paths: ['C:\\a.jpg'],
  size: 100,
  ext: '.jpg',
  caps: { preview: true, original: true, render: false, directPlay: false, convertPlay: false },
  video: null,
})

/** The library showing a view with photos 1, 2 and 3 on one day. */
async function libraryShowing(filter: Filter, ids = [1, 2, 3]) {
  const lib = useLibrary()
  vi.mocked(engine.timeline).mockResolvedValue([{ day, count: ids.length }])
  await lib.setScope({ kind: 'timeline', view: { filter } })
  lib.days = new Map([[day, ids.map((id) => ({ id, w: 10, h: 10, t: 1 }))]])
  return lib
}

let wrapper: ReturnType<typeof mountInApp> | undefined
let notifications: RecordedNotifications

function viewer() {
  const el = document.body.querySelector<HTMLElement>('.viewer')
  if (!el) throw new Error('the viewer did not open')
  return new DOMWrapper(el)
}

beforeEach(() => {
  setActivePinia(createPinia())
  notifications = recordNotifications()
  vi.mocked(confirm).mockReset().mockResolvedValue(true)
  vi.mocked(engine.timeline).mockReset()
  vi.mocked(engine.trashAssets).mockReset().mockResolvedValue(undefined)
  vi.mocked(engine.restoreAssets).mockReset().mockResolvedValue(undefined)
  vi.mocked(engine.restoreAllTrash).mockReset().mockResolvedValue(undefined)
  vi.mocked(engine.trashCount).mockReset().mockResolvedValue(0)
  vi.mocked(engine.trashSelection)
    .mockReset()
    .mockResolvedValue({ assets: [1], files: 2 })
  vi.mocked(engine.deleteFromDisk).mockReset().mockResolvedValue({ recycled: 1, kept: [] })
  vi.mocked(engine.asset)
    .mockReset()
    .mockImplementation((id) => Promise.resolve(detail(id)))
  vi.mocked(engine.assetTags).mockReset().mockResolvedValue([])
  vi.mocked(engine.assetFaces).mockReset().mockResolvedValue([])
  vi.mocked(engine.assetAlbums).mockReset().mockResolvedValue([])
  vi.mocked(engine.albums).mockReset().mockResolvedValue([])
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  notifications.stop()
})

it('moves a photo to the Trash, moves the viewer on to the next one and offers Undo', async () => {
  const lib = await libraryShowing(Filter.FilterAll)
  lib.viewer = { day, id: 2 }
  await lib.moveToTrash({ day, id: 2 })
  expect(engine.trashAssets).toHaveBeenCalledWith([2])
  expect(lib.viewer).toEqual({ day, id: 3 })
  expect(notifications.find(TRASH_UNDO_NOTIFICATION)?.title).toBe('Moved 1 item to the Trash.')
  notifications.find(TRASH_UNDO_NOTIFICATION)?.actions?.[0]?.run()
  await flushPromises()
  expect(engine.restoreAssets).toHaveBeenCalledWith([2])
  expect(engine.trashCount).toHaveBeenCalled()
})

it('closes the viewer when the last photo has nothing to move on to', async () => {
  const lib = await libraryShowing(Filter.FilterAll, [1])
  lib.viewer = { day, id: 1 }
  await lib.moveToTrash({ day, id: 1 })
  expect(lib.viewer).toBeNull()
})

it('undoes moving to the Trash by restoring what was moved', async () => {
  const lib = await libraryShowing(Filter.FilterAll)
  await lib.moveToTrash({ day, id: 1 })
  const undo = notifications.find(TRASH_UNDO_NOTIFICATION)
  expect(undo?.title).toContain('Moved 1 item to the Trash.')
  expect(undo?.actions?.[0]?.label).toBe('Undo')
  undo?.actions?.[0]?.run()
  await flushPromises()
  expect(engine.restoreAssets).toHaveBeenCalledWith([1])
  expect(notifications.find(TRASH_UNDO_NOTIFICATION)).toBeUndefined()
})

it('leaves the viewer and the offer alone when the engine refuses to trash', async () => {
  const lib = await libraryShowing(Filter.FilterAll)
  lib.viewer = { day, id: 2 }
  vi.mocked(engine.trashAssets).mockRejectedValue(new Error('no'))
  await lib.moveToTrash({ day, id: 2 })
  expect(lib.viewer).toEqual({ day, id: 2 })
  expect(notifications.find(TRASH_UNDO_NOTIFICATION)).toBeUndefined()
  expect(notifications.find(ERROR_NOTIFICATION)?.title).toBeTruthy()
})

it('restores a trashed photo and moves the viewer on', async () => {
  const lib = await libraryShowing(Filter.FilterTrash)
  lib.viewer = { day, id: 3 }
  await lib.restoreFromTrash({ day, id: 3 })
  expect(engine.restoreAssets).toHaveBeenCalledWith([3])
  expect(lib.viewer).toEqual({ day, id: 2 })
  expect(notifications.find(TRASH_UNDO_NOTIFICATION)).toBeUndefined()
})

it('restores everything in the Trash', async () => {
  const lib = await libraryShowing(Filter.FilterTrash)
  await restoreAll(lib)
  expect(engine.restoreAllTrash).toHaveBeenCalledOnce()
})

it('calls nothing when the delete-from-disk question is declined', async () => {
  const lib = await libraryShowing(Filter.FilterTrash)
  vi.mocked(confirm).mockResolvedValue(false)
  await deleteFromDisk(lib, { day, id: 1 })
  await deleteAllFromDisk(lib)
  expect(engine.deleteFromDisk).not.toHaveBeenCalled()
})

it('says how many files go to the Recycle Bin before deleting them from disk', async () => {
  const lib = await libraryShowing(Filter.FilterTrash)
  await deleteFromDisk(lib, { day, id: 1 })
  expect(confirm).toHaveBeenCalledWith(
    'Delete from disk?',
    expect.stringContaining('2 files will be moved to the Windows Recycle Bin'),
    'Move to Recycle Bin',
  )
  expect(engine.deleteFromDisk).toHaveBeenCalledWith({ assets: [1], files: 2 })
  expect(notifications.find(ERROR_NOTIFICATION)).toBeUndefined()
})

it('deletes exactly the selection the user confirmed, for one photo and for the whole Trash', async () => {
  const lib = await libraryShowing(Filter.FilterTrash)
  vi.mocked(engine.trashSelection).mockResolvedValueOnce({ assets: [1], files: 5 })
  await deleteFromDisk(lib, { day, id: 1 })
  expect(engine.trashSelection).toHaveBeenLastCalledWith([1])
  expect(confirm).toHaveBeenLastCalledWith(expect.anything(), expect.stringContaining('5 files'), expect.anything())
  expect(engine.deleteFromDisk).toHaveBeenLastCalledWith({ assets: [1], files: 5 })
  vi.mocked(engine.trashSelection).mockResolvedValueOnce({ assets: [1, 2, 3], files: 7 })
  await deleteAllFromDisk(lib)
  expect(engine.trashSelection).toHaveBeenLastCalledWith([])
  expect(confirm).toHaveBeenLastCalledWith(expect.anything(), expect.stringContaining('7 files'), expect.anything())
  expect(engine.deleteFromDisk).toHaveBeenLastCalledWith({ assets: [1, 2, 3], files: 7 })
})

it('says the Trash changed when the engine refuses what was confirmed', async () => {
  const lib = await libraryShowing(Filter.FilterTrash)
  vi.mocked(engine.deleteFromDisk).mockRejectedValue(new Error('changed', { cause: { code: ErrorCode.TrashChanged } }))
  await deleteAllFromDisk(lib)
  expect(notifications.find(ERROR_NOTIFICATION)?.title).toBe(
    'The Trash changed while you were deciding, so nothing was deleted. Try again.',
  )
  expect(engine.trashCount).toHaveBeenCalled()
})

it('deletes nothing and asks nothing when the file count cannot be read', async () => {
  const lib = await libraryShowing(Filter.FilterTrash)
  vi.mocked(engine.trashSelection).mockRejectedValue(new Error('no'))
  await deleteFromDisk(lib, { day, id: 1 })
  expect(confirm).not.toHaveBeenCalled()
  expect(engine.deleteFromDisk).not.toHaveBeenCalled()
})

it('advances the viewer to the neighbour even when a reload lands before the trash call answers', async () => {
  const lib = await libraryShowing(Filter.FilterAll)
  lib.viewer = { day, id: 2 }
  let answer: (() => void) | undefined
  vi.mocked(engine.trashAssets).mockReturnValue(new Promise<void>((done) => (answer = done)))
  vi.mocked(engine.timeline).mockResolvedValue([{ day, count: 2 }])
  vi.mocked(engine.days).mockResolvedValue([{ day, items: [1, 3].map((id) => ({ id, w: 10, h: 10, t: 1 })) }])
  const moving = lib.moveToTrash({ day, id: 2 })
  await flushPromises()
  await lib.refresh() // library:changed, before the engine has answered
  await lib.ensureDays([day])
  expect(lib.viewer).toEqual({ day, id: 3 })
  if (!answer) throw new Error('the trash operation did not start')
  answer()
  await moving
  expect(lib.viewer).toEqual({ day, id: 3 })
})

it('reports the files the Recycle Bin would not take', async () => {
  const lib = await libraryShowing(Filter.FilterTrash)
  vi.mocked(engine.deleteFromDisk).mockResolvedValue({ recycled: 0, kept: ['D:\\share\\a.jpg'] })
  await deleteFromDisk(lib, { day, id: 1 })
  expect(notifications.find(KEPT_NOTIFICATION)?.title).toContain('1 file couldn’t be moved to the Recycle Bin')
  vi.mocked(engine.deleteFromDisk).mockResolvedValue({ recycled: 1, kept: ['a', 'b'] })
  await deleteAllFromDisk(lib)
  expect(notifications.find(KEPT_NOTIFICATION)?.title).toContain('2 files couldn’t be moved')
})

it('shows Restore and Delete from disk, not the editing controls, in the Viewer for a trashed photo', async () => {
  const lib = await libraryShowing(Filter.FilterTrash)
  lib.viewer = { day, id: 1 }
  wrapper = mountInApp(Viewer)
  await flushPromises()
  const labels = viewer()
    .findAll('button')
    .map((b) => b.text() || b.attributes('aria-label'))
  expect(labels).toContain('Restore')
  expect(labels).toContain('Delete from disk…')
  expect(labels).not.toContain('Move to Trash')
  expect(labels).not.toContain('Favourite')
})

it('confirms the engine’s file count from the Viewer even before the photo’s details have loaded', async () => {
  const lib = await libraryShowing(Filter.FilterTrash)
  lib.viewer = { day, id: 1 }
  vi.mocked(engine.asset).mockReturnValue(new Promise(() => undefined)) // the details never arrive
  wrapper = mountInApp(Viewer)
  await flushPromises()
  // The viewer is a layer over the page, outside the wrapper's own element.
  const button = viewer()
    .findAll('button')
    .find((b) => b.text() === 'Delete from disk…')
  expect(button).toBeDefined()
  await button?.trigger('click')
  await flushPromises()
  expect(engine.trashSelection).toHaveBeenCalledWith([1])
  expect(confirm).toHaveBeenCalledWith(expect.anything(), expect.stringContaining('2 files'), expect.anything())
})

it('confirms the number of files, not of photos, when deleting the whole Trash from its bar', async () => {
  const lib = await libraryShowing(Filter.FilterTrash)
  vi.mocked(engine.trashSelection).mockResolvedValue({ assets: [1, 2, 3], files: 5 })
  wrapper = mountInApp(TrashBar)
  const button = wrapper.findAll('button').find((b) => b.text().includes('Delete all from disk'))
  await button?.trigger('click')
  await flushPromises()
  expect(lib.total).toBe(3)
  expect(engine.trashSelection).toHaveBeenCalledWith([])
  expect(confirm).toHaveBeenCalledWith(expect.anything(), expect.stringContaining('5 files'), expect.anything())
})

it('offers Move to Trash in the Viewer outside the Trash', async () => {
  const lib = await libraryShowing(Filter.FilterAll)
  lib.viewer = { day, id: 1 }
  wrapper = mountInApp(Viewer)
  await flushPromises()
  await viewer().get('button[aria-label="Move to Trash"]').trigger('click')
  await flushPromises()
  expect(engine.trashAssets).toHaveBeenCalledWith([1])
})

it('hides tags, albums, people, favourite and block in the Details panel of a trashed photo', async () => {
  await libraryShowing(Filter.FilterTrash)
  wrapper = mountInApp(DetailsPanel, { props: { sel: { day, id: 1 } } })
  await flushPromises()
  const text = wrapper.text()
  expect(text).toContain('Restore')
  expect(text).toContain('Delete from disk…')
  for (const hidden of ['Block', 'Favourite', 'Move to Trash']) expect(text).not.toContain(hidden)
  expect(wrapper.find('input').exists()).toBe(false)
  expect(engine.assetTags).not.toHaveBeenCalled()
})

it('confirms before deleting a trashed photo from the Details panel', async () => {
  await libraryShowing(Filter.FilterTrash)
  vi.mocked(confirm).mockResolvedValue(false)
  wrapper = mountInApp(DetailsPanel, { props: { sel: { day, id: 1 } } })
  await flushPromises()
  const button = wrapper.findAll('button').find((b) => b.text().includes('Delete from disk…'))
  await button?.trigger('click')
  await flushPromises()
  expect(confirm).toHaveBeenCalledOnce()
  expect(engine.deleteFromDisk).not.toHaveBeenCalled()
})

it('offers Move to Trash in the Details panel of a library photo', async () => {
  await libraryShowing(Filter.FilterAll)
  wrapper = mountInApp(DetailsPanel, { props: { sel: { day, id: 1 } } })
  await flushPromises()
  const button = wrapper.findAll('button').find((b) => b.text().includes('Move to Trash'))
  await button?.trigger('click')
  await flushPromises()
  expect(engine.trashAssets).toHaveBeenCalledWith([1])
})

it('disables both recovery actions while a restore is pending and restores once', async () => {
  await libraryShowing(Filter.FilterTrash)
  let finish: (() => void) | undefined
  vi.mocked(engine.restoreAllTrash).mockReturnValue(new Promise<void>((resolve) => (finish = resolve)))
  wrapper = mountInApp(TrashBar)
  await wrapper.get('[data-trash-restore-all]').trigger('click')
  expect(wrapper.get<HTMLButtonElement>('[data-trash-restore-all]').element.disabled).toBe(true)
  expect(wrapper.get<HTMLButtonElement>('[data-trash-delete-all]').element.disabled).toBe(true)
  await wrapper.get('[data-trash-restore-all]').trigger('click')
  expect(engine.restoreAllTrash).toHaveBeenCalledOnce()
  expect(engine.deleteFromDisk).not.toHaveBeenCalled()
  if (!finish) throw new Error('the restore did not start')
  finish()
  await flushPromises()
  expect(wrapper.get<HTMLButtonElement>('[data-trash-restore-all]').element.disabled).toBe(false)
  expect(wrapper.get<HTMLButtonElement>('[data-trash-delete-all]').element.disabled).toBe(false)
})

it('offers no recovery workspace actions when the Trash is empty', async () => {
  await libraryShowing(Filter.FilterTrash, [])
  wrapper = mountInApp(TrashBar)
  expect(wrapper.find('[role="toolbar"]').exists()).toBe(false)
  expect(wrapper.find('[data-trash-restore-all]').exists()).toBe(false)
  expect(wrapper.find('[data-trash-delete-all]').exists()).toBe(false)
})

it('dismisses Undo once used and reports a failed restore through the notification channel', async () => {
  const restore = vi.fn<(ids: number[]) => Promise<unknown>>().mockRejectedValue(new Error('restore refused'))
  showTrashUndo([1, 3], restore)
  const offer = notifications.find(TRASH_UNDO_NOTIFICATION)
  expect(offer?.title).toBe('Moved 2 items to the Trash.')
  expect(offer?.duration).toBe(8000)
  expect(offer?.actions).toHaveLength(1)
  offer?.actions?.[0]?.run()
  expect(notifications.find(TRASH_UNDO_NOTIFICATION)).toBeUndefined()
  await flushPromises()
  expect(restore).toHaveBeenCalledExactlyOnceWith([1, 3])
  expect(notifications.find(ERROR_NOTIFICATION)?.title).toContain('restore refused')
})
