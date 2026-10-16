import { t } from '../shared/i18n'
import { mountInApp, recordNotifications } from '../shared/testing'
import type { RecordedNotifications } from '../shared/testing'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { DOMWrapper, flushPromises } from '@vue/test-utils'
import { BlockKind, engine, MAX_NAME_LEN, onLibraryChanged } from '../bridge'
import type * as Bridge from '../bridge'
import type { Changes, Event } from '../bridge'
import { blockRequest, showBlocksChanged } from '../shared/block'
import { commands, runCommand } from '../shared/commands'
import EventsView from './EventsView.vue'

vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  onLibraryChanged: vi.fn(() => () => undefined),
  engine: { events: vi.fn(), places: vi.fn().mockResolvedValue([]), setEventName: vi.fn(), setEventPinned: vi.fn() },
}))
vi.mock('../library/store', () => ({ useLibrary: () => ({ settings: { eventGapHours: 12 }, settingErrors: {} }) }))

const trip: Event = {
  id: 7,
  name: 'Lahore, 2 Jan 2024',
  startAt: 0,
  endAt: 0,
  count: 3,
  cover: 0,
  coverThumb: 0,
  placeId: 0,
  pinned: false,
}
let wrapper: ReturnType<typeof mountInApp> | undefined
let notices: RecordedNotifications

beforeEach(() => {
  setActivePinia(createPinia())
  notices = recordNotifications()
  vi.useFakeTimers()
  vi.mocked(engine.events).mockReset().mockResolvedValue([trip])
  vi.mocked(engine.setEventName).mockReset().mockResolvedValue(undefined)
  vi.mocked(engine.setEventPinned).mockReset().mockResolvedValue(undefined)
})
afterEach(() => {
  blockRequest.value?.answer(false)
  blockRequest.value = null
  wrapper?.unmount()
  vi.useRealTimers()
  notices.stop()
})

async function mounted() {
  wrapper = mountInApp(EventsView, { plugins: [testRouter(), createPinia()] })
  await flushPromises()
  return wrapper
}

it('focuses the name to edit, and Escape puts focus back on the pencil', async () => {
  const w = await mounted()
  await menuAction(w, `Rename ${trip.name}`)
  await flushPromises()
  const input = w.get<HTMLInputElement>('form[data-rename-event] input').element
  expect(document.activeElement).toBe(input)
  await w.get('form[data-rename-event]').trigger('keydown', { key: 'Escape' })
  await flushPromises()
  expect(w.find('form[data-rename-event]').exists()).toBe(false)
  expect(document.activeElement).toBe(w.get('button[aria-label^="Actions for"]').element)
})

it('keeps a rename failure on screen while the list reloads around it', async () => {
  let changed: ((c: Changes) => void) | undefined
  vi.mocked(onLibraryChanged).mockImplementation((cb) => {
    changed = cb
    return () => undefined
  })
  vi.mocked(engine.setEventName).mockRejectedValue(new Error('disk full'))
  const w = await mounted()
  await menuAction(w, `Rename ${trip.name}`)
  await flushPromises()
  await w.get('form[data-rename-event]').trigger('submit')
  await flushPromises()
  expect(w.get('[role="alert"]').text()).toContain('disk full')
  changed?.({ assets: true, thumbs: false, tags: false, people: false })
  await vi.advanceTimersByTimeAsync(3000) // the live reload
  await flushPromises()
  expect(engine.events).toHaveBeenCalledTimes(2)
  expect(w.get('[role="alert"]').text()).toContain('disk full')
})

it('validates emoji names by code point and still allows resetting an overlong draft', async () => {
  const w = await mounted()
  await menuAction(w, `Rename ${trip.name}`)
  await flushPromises()
  expect(w.get('form[data-rename-event] input').attributes('maxlength')).toBeUndefined()
  await w.get('form[data-rename-event] input').setValue('😀'.repeat(MAX_NAME_LEN + 1))
  await w.get('form[data-rename-event]').trigger('submit')
  await flushPromises()
  expect(w.get('[role="alert"]').text()).toContain('80')
  expect(engine.setEventName).not.toHaveBeenCalled()
  await w.get('form[data-rename-event] input').setValue('😀'.repeat(MAX_NAME_LEN))
  await w.get('form[data-rename-event]').trigger('submit')
  await flushPromises()
  expect(engine.setEventName).toHaveBeenCalledWith(trip.id, '😀'.repeat(MAX_NAME_LEN))
  await menuAction(w, `Rename ${trip.name}`)
  await flushPromises()
  await w.get('form[data-rename-event] input').setValue('😀'.repeat(MAX_NAME_LEN + 1))
  await w.findAll('form[data-rename-event] button')[2]?.trigger('click')
  await flushPromises()
  expect(engine.setEventName).toHaveBeenLastCalledWith(trip.id, '')
})

it('discards a rename draft when its event disappears and later returns', async () => {
  let changed: ((c: Changes) => void) | undefined
  vi.mocked(onLibraryChanged).mockImplementation((cb) => {
    changed = cb
    return () => undefined
  })
  const w = await mounted()
  await menuAction(w, `Rename ${trip.name}`)
  await w.get('form[data-rename-event] input').setValue('Unsaved name')
  vi.mocked(engine.events).mockResolvedValue([])
  changed?.({ assets: true, thumbs: false, tags: false, people: false })
  await vi.advanceTimersByTimeAsync(3000)
  await flushPromises()
  expect(w.find('form[data-rename-event]').exists()).toBe(false)
  vi.mocked(engine.events).mockResolvedValue([trip])
  changed?.({ assets: true, thumbs: false, tags: false, people: false })
  await vi.advanceTimersByTimeAsync(3000)
  await flushPromises()
  expect(w.find('form[data-rename-event]').exists()).toBe(false)
  await menuAction(w, `Rename ${trip.name}`)
  expect(w.get<HTMLInputElement>('form[data-rename-event] input').element.value).toBe(trip.name)
})

it('pins an event, shows the pin and unpins it again', async () => {
  const w = await mounted()
  expect(w.find('[data-pinned]').exists()).toBe(false)
  vi.mocked(engine.events).mockResolvedValue([{ ...trip, pinned: true }])
  await menuAction(w, `Pin ${trip.name}`)
  await flushPromises()
  expect(engine.setEventPinned).toHaveBeenCalledWith(7, true)
  expect(w.get('[data-pinned]').attributes('aria-label')).toBe('Pinned')
  vi.mocked(engine.events).mockResolvedValue([trip])
  await menuAction(w, `Unpin ${trip.name}`)
  await flushPromises()
  expect(engine.setEventPinned).toHaveBeenLastCalledWith(7, false)
  expect(w.find('[data-pinned]').exists()).toBe(false)
})

it('says why a pin failed', async () => {
  vi.mocked(engine.setEventPinned).mockRejectedValue(new Error('disk full'))
  const w = await mounted()
  await menuAction(w, `Pin ${trip.name}`)
  await flushPromises()
  expect(w.get('[role="alert"]').text()).toContain('disk full')
})

it('offers to block an event through the shared block flow', async () => {
  const w = await mounted()
  expect(blockRequest.value).toBeNull()
  await menuAction(w, t('blocked.confirmTitle', { name: trip.name }))
  expect(blockRequest.value).toMatchObject({
    kind: BlockKind.BlockEvent,
    targetID: trip.id,
    name: trip.name,
    labelOnly: true,
    photosAllowed: true,
  })
})

it('drops a blocked event from the list and shows it again when the block is undone', async () => {
  const w = await mounted()
  expect(w.findAll('[data-collection]')).toHaveLength(1)
  vi.mocked(engine.events).mockResolvedValue([])
  await showBlocksChanged() // what the block dialog does once the block is saved
  await flushPromises()
  expect(w.findAll('[data-collection]')).toHaveLength(0)
  expect(w.text()).toContain(t('events.cleared'))
  vi.mocked(engine.events).mockResolvedValue([trip])
  await showBlocksChanged() // and what Undo, or unblocking, does
  await flushPromises()
  expect(w.findAll('[data-collection]')).toHaveLength(1)
})

it('stops reloading for block changes once the view is gone', async () => {
  const w = await mounted()
  expect(engine.events).toHaveBeenCalledTimes(1)
  w.unmount()
  await showBlocksChanged()
  await flushPromises()
  expect(engine.events).toHaveBeenCalledTimes(1)
})

it('shares event actions with the command palette and removes commands for hidden events', async () => {
  const w = await mounted()
  expect(commands.value.some((command) => command.id === 'event.7.pin')).toBe(true)
  await runCommand('event.7.pin')
  expect(engine.setEventPinned).toHaveBeenCalledWith(trip.id, true)
  vi.mocked(engine.events).mockResolvedValue([])
  await showBlocksChanged()
  await flushPromises()
  expect(commands.value.some((command) => command.id.startsWith('event.7.'))).toBe(false)
  w.unmount()
  expect(commands.value.some((command) => command.id.startsWith('events.'))).toBe(false)
})

function testRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div />' } },
      { path: '/settings', component: { template: '<div />' } },
      { path: '/blocked', component: { template: '<div />' } },
      ...['person', 'people', 'personFaces', 'thing', 'event', 'place', 'album'].map((name) => ({
        name,
        path: `/${name}/:id?`,
        component: { template: '<div />' },
      })),
    ],
  })
}

it('groups pinned events first then unpinned events by camera-clock month', async () => {
  vi.mocked(engine.events).mockResolvedValue([
    { ...trip, id: 1, startAt: Date.UTC(2025, 0, 1) / 1000, endAt: Date.UTC(2025, 0, 1) / 1000 },
    { ...trip, id: 2, pinned: true },
    { ...trip, id: 3, startAt: Date.UTC(2026, 9, 1) / 1000, endAt: Date.UTC(2026, 9, 2) / 1000 },
  ])
  const w = await mounted()
  expect(w.findAll('[data-group] h2').map((el) => el.text())).toEqual(['Pinned', 'October', 'January'])
  expect(w.findAll('[data-group] time').map((el) => el.text())).toEqual(['2026', '2025'])
  expect(w.findAll('[data-collection]').map((el) => el.attributes('data-collection'))).toEqual(['2', '3', '1'])
})

it('offers Undo for names and pins through the same saved commands', async () => {
  const w = await mounted()
  await menuAction(w, `Pin ${trip.name}`)
  notices.find('events.change.7')?.actions?.[0]?.run()
  await flushPromises()
  expect(engine.setEventPinned).toHaveBeenLastCalledWith(7, false)
  await menuAction(w, `Rename ${trip.name}`)
  await w.get('form[data-rename-event] input').setValue('Garden walk')
  await w.get('form[data-rename-event]').trigger('submit')
  await flushPromises()
  notices.find('events.change.7')?.actions?.[0]?.run()
  await flushPromises()
  expect(engine.setEventName).toHaveBeenLastCalledWith(7, '')
})

it('restores an explicit prior name on Undo while automatic names restore an empty override', async () => {
  vi.mocked(engine.events).mockResolvedValue([{ ...trip, name: 'Our trip', nameOverride: 'Our trip' }])
  const w = await mounted()
  await w.get('button[title="Rename Our trip"]').trigger('click')
  await w.get('form[data-rename-event] input').setValue('Garden walk')
  await w.get('form[data-rename-event]').trigger('submit')
  await flushPromises()
  notices.find('events.change.7')?.actions?.[0]?.run()
  await flushPromises()
  expect(engine.setEventName).toHaveBeenLastCalledWith(7, 'Our trip')
})

it('returns focus to the title after cancelling a title rename', async () => {
  const w = await mounted()
  await w.get(`button[title="Rename ${trip.name}"]`).trigger('click')
  await flushPromises()
  expect(document.activeElement).toBe(w.get('form[data-rename-event] input').element)
  await w.get('form[data-rename-event]').trigger('keydown', { key: 'Escape' })
  await flushPromises()
  expect(document.activeElement).toBe(w.get(`button[title="Rename ${trip.name}"]`).element)
})

it('keeps only the latest event name editor open', async () => {
  vi.mocked(engine.events).mockResolvedValue([trip, { ...trip, id: 8, name: 'Next event' }])
  const w = await mounted()
  await w.get(`button[title="Rename ${trip.name}"]`).trigger('click')
  await w.get('button[title="Rename Next event"]').trigger('click')
  await flushPromises()
  expect(w.findAll('form[data-rename-event]')).toHaveLength(1)
  expect(w.get<HTMLInputElement>('form[data-rename-event] input').element.value).toBe('Next event')
})

it('keeps a saved rename refresh failure visible when its editor closes and retries only the read', async () => {
  const w = await mounted()
  await menuAction(w, `Rename ${trip.name}`)
  vi.mocked(engine.events).mockRejectedValueOnce(new Error('refresh failed'))
  await w.get('form[data-rename-event]').trigger('submit')
  await flushPromises()
  expect(w.find('form[data-rename-event]').exists()).toBe(false)
  expect(w.get('[role="alert"]').text()).toContain('refresh failed')
  await w.get('[role="alert"] button').trigger('click')
  await flushPromises()
  expect(engine.setEventName).toHaveBeenCalledTimes(1)
  expect(notices.find('events.change.7')?.actions).toHaveLength(1)
})

vi.mock('../search/SearchBox.vue', () => ({ default: { render: () => null } }))

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
