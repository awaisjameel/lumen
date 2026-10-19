import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { AlbumKind, engine } from '../../bridge'
import type * as Bridge from '../../bridge'
import { emptyRule } from '../../albums/store'
import { t } from '../../shared/i18n'
import { mountInApp, recordNotifications } from '../../shared/testing'
import AlbumPicker from './AlbumPicker.vue'
import { body, press } from './testing'

vi.mock('../../bridge', async (original) => ({
  ...(await original<typeof Bridge>()),
  engine: { albums: vi.fn(), setAlbumItem: vi.fn(), createAlbum: vi.fn() },
}))
let wrapper: VueWrapper | undefined
let notices: ReturnType<typeof recordNotifications>
const album = (id: number, name: string, kind = AlbumKind.AlbumManual) => ({
  id,
  name,
  kind,
  rule: emptyRule(),
  count: 0,
  cover: 0,
  coverThumb: 0,
})
beforeEach(() => {
  notices = recordNotifications()
  vi.mocked(engine.albums)
    .mockReset()
    .mockResolvedValue([album(1, 'Summer'), album(2, 'Family'), album(3, 'Rule', AlbumKind.AlbumSmart)])
  vi.mocked(engine.setAlbumItem).mockReset().mockResolvedValue(undefined)
  vi.mocked(engine.createAlbum).mockReset().mockResolvedValue(4)
})
afterEach(() => {
  wrapper?.unmount()
  notices.stop()
  document.body.innerHTML = ''
})
function mountPicker(membership: number[]) {
  wrapper = mountInApp(AlbumPicker, { props: { open: true, id: 9, membership }, plugins: [createPinia()] })
}

it('stages manual album checkbox changes until Apply and offers Undo', async () => {
  mountPicker([1])
  await flushPromises()
  const boxes = body().findAll('[role="checkbox"]')
  expect(boxes).toHaveLength(2)
  expect(boxes[0]?.attributes('aria-checked')).toBe('true')
  await boxes[0]?.trigger('click')
  await boxes[1]?.trigger('click')
  expect(engine.setAlbumItem).not.toHaveBeenCalled()
  await press(t('details.album.apply'))
  expect(engine.setAlbumItem).toHaveBeenCalledWith(9, 1, false)
  expect(engine.setAlbumItem).toHaveBeenCalledWith(9, 2, true)
  notices.find('albums-9')?.actions?.[0]?.run()
  await flushPromises()
  expect(engine.setAlbumItem).toHaveBeenCalledWith(9, 1, true)
  expect(engine.setAlbumItem).toHaveBeenCalledWith(9, 2, false)
})

it('searches manual album names and creates a named album with the focused photo', async () => {
  mountPicker([])
  await flushPromises()
  await body().get('input[type="search"]').setValue('sUm')
  expect(body().findAll('[role="checkbox"]')).toHaveLength(1)
  await body().get('input[type="search"]').setValue('  Trip  ')
  expect(body().findAll('[role="checkbox"]')).toHaveLength(0)
  await press(t('details.album.create', { name: 'Trip' }))
  expect(engine.createAlbum).toHaveBeenCalledWith('Trip', AlbumKind.AlbumManual, emptyRule(), [9])
  notices.find('albums-9')?.actions?.[0]?.run()
  await flushPromises()
  expect(engine.setAlbumItem).toHaveBeenCalledWith(9, 4, false)
})

it('keeps staged membership and an error when Apply fails', async () => {
  vi.mocked(engine.setAlbumItem).mockRejectedValueOnce(new Error('Album unavailable'))
  mountPicker([])
  await flushPromises()
  await body().get('[role="checkbox"]').trigger('click')
  await press(t('details.album.apply'))
  expect(body().get('[role="alert"]').text()).toContain('Album unavailable')
  expect(body().get('[role="checkbox"]').attributes('aria-checked')).toBe('true')
  expect(notices.shown).toHaveLength(0)
  await press(t('details.album.apply'))
  expect(engine.setAlbumItem).toHaveBeenCalledTimes(2)
})
