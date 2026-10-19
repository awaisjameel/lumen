import type * as Router from 'vue-router'
import USelect from '@nuxt/ui/components/Select.vue'
import { t } from '../shared/i18n'
import { mountInApp } from '../shared/testing'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { DOMWrapper, flushPromises } from '@vue/test-utils'
import { AlbumKind, engine, ErrorCode, Kind, MAX_NAME_LEN, thumbUrl } from '../bridge'
import type * as Bridge from '../bridge'
import type { Album } from '../bridge'
import { blockRequest } from '../shared/block'
import { emptyRule, useAlbums } from './store'
import AlbumsView from './AlbumsView.vue'

vi.mock('vue-router', async (importOriginal) => ({
  ...(await importOriginal<typeof Router>()),
  useRouter: () => ({ push: vi.fn() }),
}))
vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  onLibraryChanged: vi.fn(() => () => undefined),
  engine: { albums: vi.fn(), things: vi.fn(), people: vi.fn(), createAlbum: vi.fn(), updateAlbum: vi.fn() },
}))

const album: Album = {
  id: 1,
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
  vi.mocked(engine.updateAlbum).mockReset().mockResolvedValue(undefined)
})

afterEach(() => {
  blockRequest.value?.answer(false)
  blockRequest.value = null
  wrapper?.unmount()
})

async function mounted() {
  wrapper = mountInApp(AlbumsView, { plugins: [testRouter()] })
  await flushPromises()
  return wrapper
}

it('resets the edited album only after its block is saved successfully', async () => {
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
  expect(editor().get<HTMLInputElement>('form input').element.value).toBe('')
  expect(editor().text()).toContain('New album')
})

it('resets the editor when its album disappears from a reload', async () => {
  const w = await mounted()
  await menuAction(w, t('albums.editNamed', { name: album.name }))
  await flushPromises()
  vi.mocked(engine.albums).mockResolvedValue([])
  await useAlbums().load()
  await flushPromises()
  expect(editor().get<HTMLInputElement>('form input').element.value).toBe('')
  expect(w.text()).not.toContain(album.name)
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
  expect(engine.updateAlbum).toHaveBeenCalledWith(album.id, album.name, { ...rule, kind: Kind.$zero })
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
  [ErrorCode.AlbumReference, 'The selected Thing or person no longer exists.'],
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
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div />' } },
      ...['person', 'people', 'personFaces', 'thing', 'event', 'place', 'album'].map((name) => ({
        name,
        path: `/${name}/:id?`,
        component: { template: '<div />' },
      })),
    ],
  })
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
  expect(engine.updateAlbum).not.toHaveBeenCalled()
  expect(editor().get('[role="alert"]').text()).toContain('The start date must not follow the end date.')
  expect(editor().get<HTMLInputElement>('form input').element.value).toBe(album.name)
})

it('opens the existing album editor from a collection hero’s edit link', async () => {
  const router = testRouter()
  await router.push({ path: '/', query: { edit: '1' } })
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
