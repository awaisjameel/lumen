import { destination } from '../shared/navigation/contract'
import USelect from '@nuxt/ui/components/Select.vue'
import { dismiss } from '../shared/notify'
import { question } from '../shared/confirm'
import { t } from '../shared/i18n'
import { deferred, mountInApp, recordNotifications } from '../shared/testing'
import { createTestNavigation } from '../shared/navigation/testing'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { DOMWrapper, flushPromises } from '@vue/test-utils'
import { AlbumKind, engine, ErrorCode, Kind, MAX_NAME_LEN, TagKind, thumbUrl } from '../bridge'
import type * as Bridge from '../bridge'
import type { Album } from '../bridge'
import { blockRequest, showBlocksChanged } from '../shared/block'
import { useThings } from '../things/store'
import { personFixture } from '../people/testing'
import { emptyRule, useAlbums } from './store'
import AlbumsView from './AlbumsView.vue'

vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  onLibraryChanged: vi.fn(() => () => undefined),
  engine: {
    albums: vi.fn(),
    things: vi.fn(),
    people: vi.fn(),
    createAlbum: vi.fn(),
    saveAlbum: vi.fn(),
    previewAlbum: vi.fn(),
    counts: vi.fn().mockResolvedValue({ photos: 0, videos: 0, favourites: 0, people: 0, trash: 0 }),
  },
}))

const album: Album = {
  id: 1,
  version: { identity: 'original-album', revision: 'initial-definition' },
  name: 'Private trip',
  kind: AlbumKind.AlbumManual,
  count: 1,
  cover: 0,
  coverThumb: 0,
  rule: emptyRule(),
}
let wrapper: ReturnType<typeof mountInApp> | undefined

beforeEach(() => {
  setActivePinia(createPinia())
  vi.mocked(engine.albums).mockReset().mockResolvedValue([album])
  vi.mocked(engine.things).mockResolvedValue([])
  vi.mocked(engine.people).mockResolvedValue([])
  vi.mocked(engine.createAlbum).mockReset().mockResolvedValue(2)
  vi.mocked(engine.saveAlbum).mockReset().mockResolvedValue(album.version)
  vi.mocked(engine.previewAlbum).mockReset().mockResolvedValue({ count: 0, items: [] })
})

afterEach(() => {
  question.value?.answer(false)
  blockRequest.value?.answer(false)
  blockRequest.value = null
  wrapper?.unmount()
  vi.useRealTimers()
})

async function mounted() {
  wrapper = mountInApp(AlbumsView, { plugins: [testRouter()] })
  await flushPromises()
  return wrapper
}

it('closes the edited album only after its block is saved successfully', async () => {
  const w = await mounted()
  await menuAction(w, t('albums.editNamed', { name: album.name }))
  await flushPromises()
  await menuAction(
    w,
    t('blocked.confirmTitle', { name: album.name }),
    '[role="dialog"] button[aria-label^="Actions for"]',
  )
  blockRequest.value?.answer(false)
  await flushPromises()
  expect(editor().get<HTMLInputElement>('form input').element.value).toBe(album.name)
  await menuAction(
    w,
    t('blocked.confirmTitle', { name: album.name }),
    '[role="dialog"] button[aria-label^="Actions for"]',
  )
  blockRequest.value?.answer(true)
  await flushPromises()
  expect(document.querySelector('#album-editor')).toBeNull()
})

it('closes the editor when its album disappears from a successful reload', async () => {
  const w = await mounted()
  await menuAction(w, t('albums.editNamed', { name: album.name }))
  await flushPromises()
  vi.mocked(engine.albums).mockResolvedValue([])
  await useAlbums().load()
  await flushPromises()
  expect(document.querySelector('#album-editor')).toBeNull()
  expect(w.text()).not.toContain(album.name)
})

it('preserves an existing album draft and target after an unrelated delayed privacy refresh', async () => {
  const w = await mounted()
  await menuAction(w, t('albums.editNamed', { name: album.name }))
  await editor().get('form input').setValue('Unsaved local name')
  const pending = deferred<Album[]>()
  vi.mocked(engine.albums).mockReturnValueOnce(pending.promise)
  const refreshing = showBlocksChanged()
  await flushPromises()
  expect(useAlbums().list).toEqual([])
  expect(useAlbums().loaded).toBe(false)
  pending.resolve([album])
  await refreshing
  await flushPromises()
  expect(editor().get<HTMLInputElement>('form input').element.value).toBe('Unsaved local name')
  await editor().get('form').trigger('submit')
  await flushPromises()
  expect(engine.saveAlbum).toHaveBeenCalledExactlyOnceWith({
    id: album.id,
    version: album.version,
    name: 'Unsaved local name',
    rule: emptyRule(),
  })
  expect(engine.createAlbum).not.toHaveBeenCalled()
})

it('does not apply a draft after the saved album definition changes behind the editor', async () => {
  const w = await mounted()
  await menuAction(w, t('albums.editNamed', { name: album.name }))
  await editor().get('form input').setValue('Older local draft')
  vi.mocked(engine.albums).mockResolvedValue([
    { ...album, name: 'Changed saved definition', version: { ...album.version, revision: 'changed' } },
  ])
  await useAlbums().load()
  await flushPromises()
  const form = document.querySelector('#album-editor')
  if (form) await new DOMWrapper(form).trigger('submit')
  await flushPromises()
  expect(engine.saveAlbum).not.toHaveBeenCalled()
  expect(engine.createAlbum).not.toHaveBeenCalled()
})

it('validates names by code point before creating an album', async () => {
  const w = await mounted()
  await w.get('[data-action="new-album"]').trigger('click')
  await flushPromises()
  expect(editor().get('form input').attributes('maxlength')).toBeUndefined()
  await editor()
    .get('form input')
    .setValue('😀'.repeat(MAX_NAME_LEN + 1))
  await editor().get('form').trigger('submit')
  await flushPromises()
  expect(editor().get('[role="alert"]').text()).toContain('80')
  expect(engine.createAlbum).not.toHaveBeenCalled()
  await editor()
    .get('form input')
    .setValue(` ${'😀'.repeat(MAX_NAME_LEN)} `)
  await editor().get('form').trigger('submit')
  await flushPromises()
  expect(engine.createAlbum).toHaveBeenCalledOnce()
})

it('retries a failed smart preview without saving or replacing its draft', async () => {
  vi.useFakeTimers()
  vi.mocked(engine.albums).mockResolvedValue([{ ...album, kind: AlbumKind.AlbumSmart }])
  vi.mocked(engine.previewAlbum)
    .mockRejectedValueOnce(new Error('Preview refused'))
    .mockResolvedValue({ count: 2, items: [{ id: 7, w: 1, h: 1, t: 1 }] })
  const w = await mounted()
  await menuAction(w, t('albums.editNamed', { name: album.name }))
  await vi.advanceTimersByTimeAsync(200)
  await flushPromises()
  const preview = editor().get('[data-album-preview]')
  expect(preview.text()).toContain('Preview refused')
  expect(preview.findAll('.preview-grid > div')).toHaveLength(0)
  const retry = preview.findAll('button').find((button) => button.text() === t('welcome.retry'))
  if (!retry) throw new Error('The preview retry control is missing')
  await retry.trigger('click')
  await flushPromises()
  expect(preview.text()).toContain(t('albums.matches', { n: 2 }))
  expect(preview.text()).not.toContain('Preview refused')
  expect(preview.findAll('.preview-grid > div')).toHaveLength(1)
  expect(editor().get<HTMLInputElement>('form input').element.value).toBe(album.name)
  expect(engine.saveAlbum).not.toHaveBeenCalled()
})

it('keeps every smart rule field and maps all media back to the engine zero value', async () => {
  const rule = { ...emptyRule(), kind: Kind.KindPhoto, favourites: true, from: '2024-01-01', to: '2024-02-01' }
  vi.mocked(engine.albums).mockResolvedValue([{ ...album, kind: AlbumKind.AlbumSmart, rule }])
  const w = await mounted()
  await menuAction(w, t('albums.editNamed', { name: album.name }))
  const media = w.findAllComponents(USelect)[1]
  if (!media) throw new Error('The media choice is missing')
  expect(media.props()).toMatchObject({ modelValue: 1 })
  expect(editor().get<HTMLInputElement>('input[type="date"][aria-label="Taken on or after"]').element.value).toBe(
    rule.from,
  )
  expect(editor().get<HTMLInputElement>('input[type="date"][aria-label="Taken on or before"]').element.value).toBe(
    rule.to,
  )
  expect(editor().get('[role="checkbox"]').attributes('aria-checked')).toBe('true')
  media.vm.$emit('update:modelValue', 0)
  await flushPromises()
  await editor().get('form').trigger('submit')
  await flushPromises()
  expect(engine.saveAlbum).toHaveBeenCalledWith({
    id: album.id,
    version: album.version,
    name: album.name,
    rule: { ...rule, kind: Kind.$zero },
  })
})

it('focuses the name in the modal and returns focus to New album when cancelled', async () => {
  const w = await mounted()
  const trigger = w.get('[data-action="new-album"]')
  await trigger.trigger('click')
  await flushPromises()
  expect(document.activeElement).toBe(editor().get('form input').element)
  await editor().get('[data-action="cancel-album"]').trigger('click')
  await flushPromises()
  expect(document.activeElement).toBe(trigger.element)
})

it.each([
  [ErrorCode.AlbumExists, 'An album already has that name.'],
  [ErrorCode.InvalidAlbum, 'This album can’t be saved.'],
  [ErrorCode.AlbumMediaKind, 'Choose photos, videos, or all media.'],
  [ErrorCode.AlbumSelection, 'Choose a valid Thing or person.'],
  [ErrorCode.AlbumDate, 'Enter a valid date in YYYY-MM-DD format.'],
  [ErrorCode.AlbumDateOrder, 'The start date must not follow the end date.'],
  [ErrorCode.AlbumSmartItems, 'Smart albums choose their own items.'],
  [ErrorCode.AlbumKind, 'Choose a manual or smart album.'],
  [ErrorCode.AlbumReference, 'The selected Thing or person is unavailable.'],
  [ErrorCode.AlbumManualOnly, 'Only manual albums accept items.'],
])('explains %s in the album editor and keeps the unsaved draft', async (code, message) => {
  vi.mocked(engine.createAlbum).mockRejectedValue(new Error('engine details', { cause: { code } }))
  const w = await mounted()
  await w.get('[data-action="new-album"]').trigger('click')
  await flushPromises()
  await editor().get('form input').setValue('New trip')
  await editor().get('form').trigger('submit')
  await flushPromises()
  expect(editor().get('[role="alert"]').text()).toBe(message)
  expect(editor().get<HTMLInputElement>('form input').element.value).toBe('New trip')
})

function testRouter() {
  return createTestNavigation()
}

async function menuAction(
  _w: ReturnType<typeof mountInApp>,
  label: string,
  trigger = 'button[aria-label^="Actions for"]',
) {
  await new DOMWrapper(document.body).get(trigger).trigger('keydown', { key: 'Enter' })
  await flushPromises()
  const item = [...document.body.querySelectorAll<HTMLElement>('[role="menuitem"]')].find(
    (el) => el.textContent.trim() === label,
  )
  if (!item) throw new Error(`No menu item named ${label}`)
  item.click()
  await flushPromises()
}

function editor() {
  const dialog = document.body.querySelector('[role="dialog"]')
  if (!dialog) throw new Error('The album editor is closed')
  return new DOMWrapper(dialog)
}

it('shows a live caption and filters manual and smart cards without changing saved albums', async () => {
  vi.mocked(engine.albums).mockResolvedValue([
    album,
    { ...album, id: 2, kind: AlbumKind.AlbumSmart, name: 'Favourites' },
  ])
  const w = await mounted()
  expect(w.get('[aria-live="polite"]').text()).toBe('2 albums · 1 smart')
  await w.get('[role="radiogroup"][aria-label="Album type"] button:nth-of-type(3)').trigger('click')
  expect(w.text()).toContain('Favourites')
  expect(w.text()).not.toContain(album.name)
  expect(useAlbums().list).toHaveLength(2)
})

it('shows each album by the cover it chose, without reading its photos', async () => {
  vi.mocked(engine.albums).mockResolvedValue([{ ...album, count: 30, cover: 7, coverThumb: 3 }])
  const w = await mounted()
  expect(w.get('.collection-card .cover img').attributes('src')).toBe(thumbUrl(7, 3))
  expect(w.get('h2').text()).toBe(t('albums.collections'))
})

it('blocks saving an inverted date range while retaining the draft', async () => {
  const rule = { ...emptyRule(), from: '2026-10-16', to: '2026-10-15' }
  vi.mocked(engine.albums).mockResolvedValue([{ ...album, kind: AlbumKind.AlbumSmart, rule }])
  const w = await mounted()
  await menuAction(w, t('albums.editNamed', { name: album.name }))
  await editor().get('form').trigger('submit')
  await flushPromises()
  expect(engine.saveAlbum).not.toHaveBeenCalled()
  expect(editor().get('[role="alert"]').text()).toContain('The start date must not follow the end date.')
  expect(editor().get<HTMLInputElement>('form input').element.value).toBe(album.name)
})

it('opens the existing album editor from a collection hero’s edit link', async () => {
  const router = testRouter()
  await router.push(destination({ name: 'albums', query: { edit: 1 } }))
  wrapper = mountInApp(AlbumsView, { plugins: [router] })
  await flushPromises()
  expect(editor().get<HTMLInputElement>('form input').element.value).toBe(album.name)
  expect(editor().text()).toContain('The album type stays the same when editing.')
})

it('keeps the saving draft when the empty-state creation action is used again', async () => {
  vi.mocked(engine.albums).mockResolvedValue([])
  let finish: ((id: number) => void) | undefined
  vi.mocked(engine.createAlbum).mockReturnValueOnce(
    new Promise((resolve) => {
      finish = resolve
    }),
  )
  const w = await mounted()
  await w.get('[data-action="new-album"]').trigger('click')
  await flushPromises()
  await editor().get('form input').setValue('Summer memories')
  await editor().get('form').trigger('submit')
  await flushPromises()
  const createAgain = w
    .findAll('button')
    .filter((button) => button.text() === 'New album')
    .at(-1)
  await createAgain?.trigger('click')
  expect(editor().get<HTMLInputElement>('form input').element.value).toBe('Summer memories')
  if (!finish) throw new Error('The save did not start')
  finish(2)
  await flushPromises()
  expect(engine.createAlbum).toHaveBeenCalledOnce()
})

it('suspends private fields during a failed privacy read and resumes them through a read-only retry', async () => {
  const w = await mounted()
  await menuAction(w, t('albums.editNamed', { name: album.name }))
  await editor().get('form input').setValue('Last valid local input')
  const pending = deferred<Album[]>()
  vi.mocked(engine.albums).mockReturnValueOnce(pending.promise)
  const refreshing = showBlocksChanged().catch(() => undefined)
  await flushPromises()
  expect(document.querySelector('#album-editor')).toBeNull()
  expect(document.body.textContent).not.toContain('Last valid local input')
  pending.reject(new Error('Read unavailable'))
  await refreshing
  await flushPromises()
  expect(document.querySelector('#album-editor')).toBeNull()
  await editor().get('[data-action="retry-draft"]').trigger('click')
  await flushPromises()
  expect(editor().get<HTMLInputElement>('form input').element.value).toBe('Last valid local input')
  expect(engine.saveAlbum).not.toHaveBeenCalled()
  expect(engine.createAlbum).not.toHaveBeenCalled()
})

it('restores the focused field and caret after a valid draft is readmitted', async () => {
  const w = await mounted()
  await menuAction(w, t('albums.editNamed', { name: album.name }))
  const field = editor().get<HTMLInputElement>('form input')
  field.element.focus()
  await field.setValue('Local name')
  field.element.setSelectionRange(3, 5)
  await field.trigger('select')
  const pending = deferred<Album[]>()
  vi.mocked(engine.albums).mockReturnValueOnce(pending.promise)
  const refreshing = showBlocksChanged()
  await flushPromises()
  pending.resolve([album])
  await refreshing
  await flushPromises()
  const restored = editor().get<HTMLInputElement>('form input').element
  expect(document.activeElement).toBe(restored)
  expect([restored.selectionStart, restored.selectionEnd]).toEqual([3, 5])
})

it('closes a draft instead of saving into a replacement album with the same numeric ID', async () => {
  const w = await mounted()
  await menuAction(w, t('albums.editNamed', { name: album.name }))
  await editor().get('form input').setValue('Old entity draft')
  vi.mocked(engine.albums).mockResolvedValue([{ ...album, version: { ...album.version, identity: 'replacement' } }])
  await useAlbums().load()
  await flushPromises()
  expect(document.querySelector('#album-editor')).toBeNull()
  expect(engine.saveAlbum).not.toHaveBeenCalled()
  expect(engine.createAlbum).not.toHaveBeenCalled()
  expect(document.activeElement).toBe(w.get('[data-action="new-album"]').element)
})

it('keeps conflict input until an explicit confirmed reload replaces it with the current saved definition', async () => {
  const w = await mounted()
  await menuAction(w, t('albums.editNamed', { name: album.name }))
  await editor().get('form input').setValue('Unsaved conflict')
  const changed = { ...album, name: 'Current saved', version: { ...album.version, revision: 'newer' } }
  vi.mocked(engine.albums).mockResolvedValue([changed])
  await useAlbums().load()
  await flushPromises()
  expect(editor().get<HTMLInputElement>('form input').element.value).toBe('Unsaved conflict')
  await editor().get('[data-action="reload-album"]').trigger('click')
  question.value?.answer(false)
  await flushPromises()
  expect(editor().get<HTMLInputElement>('form input').element.value).toBe('Unsaved conflict')
  await editor().get('[data-action="reload-album"]').trigger('click')
  question.value?.answer(true)
  await flushPromises()
  expect(editor().get<HTMLInputElement>('form input').element.value).toBe(changed.name)
  await editor().get('form input').setValue('Fresh edit')
  await editor().get('form').trigger('submit')
  await flushPromises()
  expect(engine.saveAlbum).toHaveBeenCalledExactlyOnceWith({
    id: album.id,
    version: changed.version,
    name: 'Fresh edit',
    rule: emptyRule(),
  })
})

it('retires hidden rule identities and requires an explicit replacement rather than broadening the rule', async () => {
  vi.useFakeTimers()
  const smart = { ...album, kind: AlbumKind.AlbumSmart, rule: { ...emptyRule(), tag: 7, person: 9 } }
  vi.mocked(engine.albums).mockResolvedValue([smart])
  vi.mocked(engine.things).mockResolvedValue([
    {
      id: 7,
      name: 'Hidden Thing',
      kind: TagKind.TagManual,
      original: '',
      count: 0,
      cover: 0,
      coverThumb: 0,
      pinned: false,
    },
  ])
  vi.mocked(engine.people).mockResolvedValue([personFixture(9, 'Hidden person', 0)])
  const w = await mounted()
  await menuAction(w, t('albums.editNamed', { name: album.name }))
  await vi.advanceTimersByTimeAsync(200)
  vi.mocked(engine.things).mockResolvedValue([])
  vi.mocked(engine.people).mockResolvedValue([])
  await showBlocksChanged()
  await flushPromises()
  await vi.advanceTimersByTimeAsync(200)
  expect(editor().text()).not.toContain('Hidden Thing')
  expect(editor().text()).not.toContain('Hidden person')
  expect(editor().text()).toContain(t('albums.referenceMissing'))
  const choices = w.findAllComponents(USelect)
  const tag = choices[2]
  const person = choices[3]
  if (!tag || !person) throw new Error('Rule choices are missing')
  expect(tag.props()).toMatchObject({ modelValue: -1 })
  expect(person.props()).toMatchObject({ modelValue: -1 })
  await editor().get('form').trigger('submit')
  expect(engine.saveAlbum).not.toHaveBeenCalled()
  tag.vm.$emit('update:modelValue', 0)
  person.vm.$emit('update:modelValue', 0)
  await flushPromises()
  await editor().get('form').trigger('submit')
  await flushPromises()
  expect(engine.saveAlbum).toHaveBeenCalledExactlyOnceWith({
    id: album.id,
    version: album.version,
    name: album.name,
    rule: emptyRule(),
  })
})

it('does not resurrect a removed target after its in-flight save settles', async () => {
  const w = await mounted()
  await menuAction(w, t('albums.editNamed', { name: album.name }))
  const pending = deferred<Bridge.AlbumVersion>()
  vi.mocked(engine.saveAlbum).mockReturnValueOnce(pending.promise)
  await editor().get('form input').setValue('Saving original')
  await editor().get('form').trigger('submit')
  vi.mocked(engine.albums).mockResolvedValue([])
  await useAlbums().load()
  await flushPromises()
  expect(document.querySelector('#album-editor')).toBeNull()
  pending.resolve({ ...album.version, revision: 'committed' })
  await flushPromises()
  await w.get('[data-action="new-album"]').trigger('click')
  await flushPromises()
  expect(editor().get<HTMLInputElement>('form input').element.value).toBe('')
  expect(engine.createAlbum).not.toHaveBeenCalled()
})

it('never replays a committed save after its list refresh fails', async () => {
  const w = await mounted()
  await menuAction(w, t('albums.editNamed', { name: album.name }))
  await editor().get('form input').setValue('Committed')
  vi.mocked(engine.albums).mockRejectedValueOnce(new Error('Post-commit read failed'))
  await editor().get('form').trigger('submit')
  await flushPromises()
  expect(document.querySelector('#album-editor')).toBeNull()
  expect(engine.saveAlbum).toHaveBeenCalledOnce()
  await useAlbums().load()
  await flushPromises()
  expect(engine.saveAlbum).toHaveBeenCalledOnce()
})

it('does not reopen a cancelled edit link when its route cleanup is delayed beyond privacy readmission', async () => {
  const router = testRouter()
  await router.push(destination({ name: 'albums', query: { edit: album.id } }))
  wrapper = mountInApp(AlbumsView, { plugins: [router] })
  await flushPromises()
  const cleanup = deferred<Awaited<ReturnType<typeof router.replace>>>()
  vi.spyOn(router, 'replace').mockReturnValueOnce(cleanup.promise)
  const pending = deferred<Album[]>()
  vi.mocked(engine.albums).mockReturnValueOnce(pending.promise)
  const refreshing = showBlocksChanged()
  await flushPromises()
  await editor().get('[data-action="cancel-album"]').trigger('click')
  await flushPromises()
  pending.resolve([album])
  await refreshing
  await flushPromises()
  expect(document.querySelector('#album-editor')).toBeNull()
  cleanup.resolve(true)
  expect(engine.saveAlbum).not.toHaveBeenCalled()
  expect(engine.createAlbum).not.toHaveBeenCalled()
})

it('reloads the authoritative definition after a save-window conflict before its background event arrives', async () => {
  const w = await mounted()
  await menuAction(w, t('albums.editNamed', { name: album.name }))
  await editor().get('form input').setValue('Conflicting local input')
  vi.mocked(engine.saveAlbum).mockRejectedValueOnce(new Error('changed', { cause: { code: ErrorCode.AlbumChanged } }))
  const changed = { ...album, name: 'Current server definition', version: { ...album.version, revision: 'concurrent' } }
  vi.mocked(engine.albums).mockResolvedValue([changed])
  await editor().get('form').trigger('submit')
  await flushPromises()
  expect(editor().get<HTMLInputElement>('form input').element.value).toBe('Conflicting local input')
  await editor().get('[data-action="reload-album"]').trigger('click')
  question.value?.answer(true)
  await flushPromises()
  expect(editor().get<HTMLInputElement>('form input').element.value).toBe(changed.name)
})

it('keeps the local conflict draft when its confirmed saved-version reload fails', async () => {
  const w = await mounted()
  await menuAction(w, t('albums.editNamed', { name: album.name }))
  await editor().get('form input').setValue('Recoverable conflict input')
  const changed = { ...album, name: 'Newer saved', version: { ...album.version, revision: 'newer' } }
  vi.mocked(engine.albums).mockResolvedValue([changed])
  await useAlbums().load()
  await flushPromises()
  vi.mocked(engine.albums).mockRejectedValueOnce(new Error('Saved-version read refused'))
  await editor().get('[data-action="reload-album"]').trigger('click')
  question.value?.answer(true)
  await flushPromises()
  expect(document.querySelector('#album-editor')).toBeNull()
  await editor().get('[data-action="retry-draft"]').trigger('click')
  await flushPromises()
  expect(editor().get<HTMLInputElement>('form input').element.value).toBe('Recoverable conflict input')
  expect(editor().find('[data-action="reload-album"]').exists()).toBe(true)
  expect(engine.saveAlbum).not.toHaveBeenCalled()
})

it('explains a refused missing target without exposing its old name or creating another album', async () => {
  dismiss('albums.unavailable')
  const notices = recordNotifications()
  expect(notices.find('albums.unavailable')).toBeUndefined()
  try {
    const w = await mounted()
    await menuAction(w, t('albums.editNamed', { name: album.name }))
    vi.mocked(engine.saveAlbum).mockRejectedValueOnce(new Error('missing', { cause: { code: ErrorCode.NotFound } }))
    await editor().get('form').trigger('submit')
    await flushPromises()
    expect(document.querySelector('#album-editor')).toBeNull()
    expect(notices.find('albums.unavailable')?.title).toBe(t('albums.draftUnavailable'))
    expect(notices.find('albums.unavailable')?.title).not.toContain(album.name)
    expect(engine.createAlbum).not.toHaveBeenCalled()
  } finally {
    notices.stop()
  }
})

it.each([ErrorCode.NotFound, ErrorCode.AlbumReference])(
  'retires an unsafe selected reference when preview reports %s before a global refresh',
  async (code) => {
    vi.useFakeTimers()
    const tag = {
      id: 7,
      name: 'Retired reference',
      kind: TagKind.TagManual,
      original: '',
      count: 0,
      cover: 0,
      coverThumb: 0,
      pinned: false,
    }
    vi.mocked(engine.things).mockResolvedValue([tag])
    vi.mocked(engine.albums).mockResolvedValue([
      { ...album, kind: AlbumKind.AlbumSmart, rule: { ...emptyRule(), tag: tag.id } },
    ])
    const pending = deferred<Bridge.AlbumPreview>()
    vi.mocked(engine.previewAlbum).mockReturnValueOnce(pending.promise)
    const w = await mounted()
    await menuAction(w, t('albums.editNamed', { name: album.name }))
    await vi.advanceTimersByTimeAsync(200)
    expect(editor().text()).toContain(tag.name)
    vi.mocked(engine.things).mockResolvedValue([])
    pending.reject(new Error('reference refused', { cause: { code } }))
    await flushPromises()
    await vi.advanceTimersByTimeAsync(200)
    expect(editor().text()).not.toContain(tag.name)
    expect(editor().text()).toContain(t('albums.referenceMissing'))
    await editor().get('form').trigger('submit')
    expect(engine.saveAlbum).not.toHaveBeenCalled()
    expect(engine.previewAlbum).toHaveBeenCalledOnce()
  },
)

it('does not retire a valid reference from stale choices while its authoritative refresh is pending', async () => {
  const oldTag = {
    id: 7,
    name: 'Earlier choice',
    kind: TagKind.TagManual,
    original: '',
    count: 0,
    cover: 0,
    coverThumb: 0,
    pinned: false,
  }
  const currentTag = { ...oldTag, id: 8, name: 'Current valid choice' }
  vi.mocked(engine.things).mockResolvedValue([oldTag])
  await useThings().load()
  const pending = deferred<Bridge.Tag[]>()
  vi.mocked(engine.things).mockReturnValueOnce(pending.promise)
  const smart = { ...album, kind: AlbumKind.AlbumSmart, rule: { ...emptyRule(), tag: currentTag.id } }
  vi.mocked(engine.albums).mockResolvedValue([smart])
  const w = await mounted()
  await menuAction(w, t('albums.editNamed', { name: album.name }))
  pending.resolve([currentTag])
  await flushPromises()
  expect(editor().text()).toContain(currentTag.name)
  expect(editor().text()).not.toContain(t('albums.referenceMissing'))
  expect(document.activeElement).toBe(editor().get('form input').element)
  await editor().get('form').trigger('submit')
  await flushPromises()
  expect(engine.saveAlbum).toHaveBeenCalledExactlyOnceWith({
    id: album.id,
    version: album.version,
    name: album.name,
    rule: smart.rule,
  })
})

it('refreshes current reference choices before adopting a changed saved definition', async () => {
  const oldTag = {
    id: 7,
    name: 'Earlier choice',
    kind: TagKind.TagManual,
    original: '',
    count: 0,
    cover: 0,
    coverThumb: 0,
    pinned: false,
  }
  const currentTag = { ...oldTag, id: 8, name: 'New saved choice' }
  vi.mocked(engine.things).mockResolvedValue([oldTag])
  const smart = { ...album, kind: AlbumKind.AlbumSmart, rule: { ...emptyRule(), tag: oldTag.id } }
  vi.mocked(engine.albums).mockResolvedValue([smart])
  const w = await mounted()
  await menuAction(w, t('albums.editNamed', { name: album.name }))
  const changed = {
    ...smart,
    version: { ...album.version, revision: 'new-rule' },
    rule: { ...emptyRule(), tag: currentTag.id },
  }
  vi.mocked(engine.albums).mockResolvedValue([changed])
  await useAlbums().load()
  await flushPromises()
  vi.mocked(engine.things).mockResolvedValue([currentTag])
  await editor().get('[data-action="reload-album"]').trigger('click')
  question.value?.answer(true)
  await flushPromises()
  expect(editor().text()).toContain(currentTag.name)
  expect(editor().text()).not.toContain(t('albums.referenceMissing'))
})
