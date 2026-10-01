import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import { AlbumKind, engine, MAX_NAME_LEN } from '../bridge'
import type * as Bridge from '../bridge'
import type { Album } from '../bridge'
import { blockRequest } from '../shared/block'
import { emptyRule, useAlbums } from './store'
import AlbumsView from './AlbumsView.vue'

vi.mock('vue-router', () => ({ useRouter: () => ({ push: vi.fn() }) }))
vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  onLibraryChanged: vi.fn(() => () => undefined),
  engine: { albums: vi.fn(), things: vi.fn(), people: vi.fn(), createAlbum: vi.fn() },
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
let wrapper: ReturnType<typeof mount> | undefined

beforeEach(() => {
  setActivePinia(createPinia())
  vi.mocked(engine.albums).mockReset().mockResolvedValue([album])
  vi.mocked(engine.things).mockResolvedValue([])
  vi.mocked(engine.people).mockResolvedValue([])
  vi.mocked(engine.createAlbum).mockReset().mockResolvedValue(2)
})

afterEach(() => {
  blockRequest.value?.answer(false)
  blockRequest.value = null
  wrapper?.unmount()
})

async function mounted() {
  wrapper = mount(AlbumsView, { global: { stubs: { SearchBox: true, RouterLink: true } } })
  wrapper.get<HTMLInputElement>('form input').element.scrollIntoView = vi.fn()
  await flushPromises()
  return wrapper
}

it('resets the edited album only after its block is saved successfully', async () => {
  const w = await mounted()
  await w.get('button[title="Edit album"]').trigger('click')
  await flushPromises()
  await w.get('button[title="Block"]').trigger('click')
  blockRequest.value?.answer(false)
  await flushPromises()
  expect(w.get<HTMLInputElement>('form input').element.value).toBe(album.name)
  await w.get('button[title="Block"]').trigger('click')
  blockRequest.value?.answer(true)
  await flushPromises()
  expect(w.get<HTMLInputElement>('form input').element.value).toBe('')
  expect(w.text()).toContain('New album')
})

it('resets the editor when its album disappears from a reload', async () => {
  const w = await mounted()
  await w.get('button[title="Edit album"]').trigger('click')
  await flushPromises()
  vi.mocked(engine.albums).mockResolvedValue([])
  await useAlbums().load()
  await flushPromises()
  expect(w.get<HTMLInputElement>('form input').element.value).toBe('')
  expect(w.text()).not.toContain(album.name)
})

it('validates names by code point before creating an album', async () => {
  const w = await mounted()
  expect(w.get('form input').attributes('maxlength')).toBeUndefined()
  await w.get('form input').setValue('😀'.repeat(MAX_NAME_LEN + 1))
  await w.get('form').trigger('submit')
  await flushPromises()
  expect(w.get('[role="alert"]').text()).toContain('80')
  expect(engine.createAlbum).not.toHaveBeenCalled()
  await w.get('form input').setValue(` ${'😀'.repeat(MAX_NAME_LEN)} `)
  await w.get('form').trigger('submit')
  await flushPromises()
  expect(engine.createAlbum).toHaveBeenCalledOnce()
})
