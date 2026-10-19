import type * as Router from 'vue-router'
import { effectScope, reactive } from 'vue'
import { t } from '../shared/i18n'
import { mountInApp, recordNotifications } from '../shared/testing'
import type { RecordedNotifications } from '../shared/testing'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { DOMWrapper, flushPromises } from '@vue/test-utils'
import { engine, ErrorCode, MAX_NAME_LEN, TagKind } from '../bridge'
import type { Tag } from '../bridge'
import type * as Bridge from '../bridge'
import ThingsView from './ThingsView.vue'
import { question } from '../shared/confirm'
import { useCommands } from '../shared/commands'

vi.mock('vue-router', async (importOriginal) => ({
  ...(await importOriginal<typeof Router>()),
  useRouter: () => ({ push: vi.fn() }),
}))
const library = reactive({ progress: { ai: { error: '' }, embedding: 0 } })
vi.mock('../library/store', () => ({ useLibrary: () => library }))
vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  onLibraryChanged: vi.fn(() => () => undefined),
  engine: {
    things: vi.fn().mockResolvedValue([]),
    albums: vi.fn().mockResolvedValue([]),
    createLabel: vi.fn().mockResolvedValue(1),
    renameThing: vi.fn().mockResolvedValue(undefined),
    setThingPinned: vi.fn().mockResolvedValue(undefined),
    deleteTag: vi.fn().mockResolvedValue(undefined),
  },
}))

let wrapper: ReturnType<typeof mountInApp> | undefined
let notices: RecordedNotifications
beforeEach(() => {
  notices = recordNotifications()
  vi.mocked(engine.albums).mockResolvedValue([])
  library.progress.ai.error = ''
})
afterEach(() => {
  question.value?.answer(false)
  wrapper?.unmount()
  notices.stop()
})

it('validates names by code point before creating a Thing', async () => {
  setActivePinia(createPinia())
  wrapper = mountInApp(ThingsView, { plugins: [testRouter(), createPinia()] })
  await flushPromises()
  expect(wrapper.get('form[data-create-thing] input').attributes('maxlength')).toBeUndefined()
  await wrapper.get('form[data-create-thing] input').setValue('😀'.repeat(MAX_NAME_LEN + 1))
  await wrapper.get('form[data-create-thing]').trigger('submit')
  await flushPromises()
  expect(wrapper.get('[role="alert"]').text()).toContain('80')
  expect(engine.createLabel).not.toHaveBeenCalled()
  await wrapper.get('form[data-create-thing] input').setValue(` ${'😀'.repeat(MAX_NAME_LEN)} `)
  await wrapper.get('form[data-create-thing]').trigger('submit')
  await flushPromises()
  expect(engine.createLabel).toHaveBeenCalledWith('😀'.repeat(MAX_NAME_LEN))
})

const dogs: Tag = {
  id: 4,
  name: 'Puppies',
  kind: TagKind.TagBuiltin,
  count: 3,
  cover: 0,
  coverThumb: 0,
  pinned: false,
  original: 'Dogs',
}

async function mountWith(tags: Tag[]) {
  setActivePinia(createPinia())
  vi.mocked(engine.things).mockReset().mockResolvedValue(tags)
  vi.mocked(engine.renameThing).mockReset().mockResolvedValue(undefined)
  vi.mocked(engine.setThingPinned).mockReset().mockResolvedValue(undefined)
  wrapper = mountInApp(ThingsView, { plugins: [testRouter(), createPinia()] })
  await flushPromises()
  return wrapper
}

it('pins an unpinned Thing and unpins a pinned one', async () => {
  const w = await mountWith([dogs])
  expect(w.find('[data-pinned]').exists()).toBe(false)
  await menuAction(w, t('things.pinNamed', { name: 'Puppies' }))
  await flushPromises()
  expect(engine.setThingPinned).toHaveBeenCalledWith(4, true)
  vi.mocked(engine.things).mockResolvedValue([{ ...dogs, pinned: true }])
  await menuAction(w, t('things.pinNamed', { name: 'Puppies' }))
  await flushPromises()
  expect(w.get('[data-pinned]').attributes('aria-label')).toBe('Pinned')
  await menuAction(w, t('things.unpinNamed', { name: 'Puppies' }))
  await flushPromises()
  expect(engine.setThingPinned).toHaveBeenLastCalledWith(4, false)
})

it('renames a Thing from its card and shows the new name', async () => {
  const w = await mountWith([dogs])
  await menuAction(w, t('things.renameNamed', { name: 'Puppies' }))
  await flushPromises()
  await w.get('form[data-rename-thing] input').setValue('Good boys')
  vi.mocked(engine.things).mockResolvedValue([{ ...dogs, name: 'Good boys' }])
  await w.get('form[data-rename-thing]').trigger('submit')
  await flushPromises()
  expect(engine.renameThing).toHaveBeenCalledWith(4, 'Good boys')
  expect(w.find('form[data-rename-thing]').exists()).toBe(false)
  expect(w.get('[data-collection] button[title^="Rename"]').text()).toBe('Good boys')
})

it('resets a renamed built-in with an empty name', async () => {
  const w = await mountWith([dogs])
  await menuAction(w, 'Reset name')
  await flushPromises()
  expect(engine.renameThing).toHaveBeenCalledWith(4, '')
})

it('offers no reset for a Thing that was not renamed', async () => {
  const w = await mountWith([{ ...dogs, name: 'Dogs', original: '' }])
  await w.get('button[aria-label^="Actions for"]').trigger('keydown', { key: 'Enter' })
  await flushPromises()
  expect(
    [...document.body.querySelectorAll('[role="menuitem"]')].some((el) => el.textContent.trim() === 'Reset name'),
  ).toBe(false)
})

it('keeps the editor open and says why when a name is refused', async () => {
  const w = await mountWith([dogs])
  await menuAction(w, t('things.renameNamed', { name: 'Puppies' }))
  await flushPromises()
  vi.mocked(engine.renameThing).mockRejectedValue(new Error('x', { cause: { code: ErrorCode.TagExists } }))
  await w.get('form[data-rename-thing]').trigger('submit')
  await flushPromises()
  expect(w.find('form[data-rename-thing]').exists()).toBe(true)
  expect(w.get('form[data-rename-thing] [role="alert"]').text()).toBe('A Thing already has that name.')
})

it('lists pinned Things first, built-in or not, and the rest in their sections', async () => {
  const mineTag: Tag = { ...dogs, id: 1, name: 'Mine', kind: TagKind.TagLabel, original: '' }
  const pinnedBuiltin: Tag = { ...dogs, id: 2, name: 'Birds', original: '', pinned: true }
  const pinnedMine: Tag = { ...mineTag, id: 3, name: 'Pinned mine', pinned: true }
  const plainBuiltin: Tag = { ...dogs, id: 4, name: 'Cars', original: '' }
  // The engine sends pinned Things first, the newest pin first.
  const w = await mountWith([pinnedBuiltin, pinnedMine, mineTag, plainBuiltin])
  expect(w.findAll('[data-collection] button[title^="Rename"]').map((n) => n.text())).toEqual([
    'Birds',
    'Pinned mine',
    'Mine',
    'Cars',
  ])
  expect(w.findAll('[data-group] h2').map((h) => h.text())).toEqual(['Pinned', 'Made by you', 'From Lumen'])
})

it('distinguishes built-in and custom Things and filters names and original prompts without reloading', async () => {
  const custom: Tag = { ...dogs, id: 8, name: 'Eiffel Tower', kind: TagKind.TagLabel, original: '' }
  const w = await mountWith([dogs, custom])
  expect(w.findAll('[data-collection]').map((card) => card.text())).toEqual([
    expect.stringContaining(t('things.yours')),
    expect.stringContaining(t('things.builtin')),
  ])
  const reads = vi.mocked(engine.things).mock.calls.length
  const filter = w.get('input[aria-label="Filter Things"]')
  await filter.setValue('dogs')
  expect(w.findAll('[data-collection] button[title^="Rename"]').map((name) => name.text())).toEqual(['Puppies'])
  await filter.setValue('tower')
  expect(w.findAll('[data-collection] button[title^="Rename"]').map((name) => name.text())).toEqual(['Eiffel Tower'])
  await filter.setValue('unmatched')
  expect(w.findAll('[data-collection]')).toHaveLength(0)
  expect(w.text()).toContain(t('things.noMatches'))
  await filter.setValue('')
  expect(w.findAll('[data-collection]')).toHaveLength(2)
  expect(engine.things).toHaveBeenCalledTimes(reads)
})

it('keeps focus in the rename field and returns it to the action menu on Escape', async () => {
  const w = await mountWith([dogs])
  await menuAction(w, t('things.renameNamed', { name: dogs.name }))
  // The field takes the focus once the menu, which held it, has closed.
  await vi.waitFor(() => expect(document.activeElement).toBe(w.get('form[data-rename-thing] input').element))
  await w.get('form[data-rename-thing]').trigger('keydown', { key: 'Escape' })
  await flushPromises()
  expect(w.find('form[data-rename-thing]').exists()).toBe(false)
  expect(document.activeElement).toBe(w.get('button[aria-label^="Actions for"]').element)
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

it('keeps a created Thing available when refreshing its result fails without creating it again', async () => {
  const w = await mountWith([])
  vi.mocked(engine.createLabel).mockReset().mockResolvedValue(99)
  vi.mocked(engine.things).mockRejectedValueOnce(new Error('refresh failed'))
  await w.get('form[data-create-thing] input').setValue('Red boats')
  await w.get('form[data-create-thing]').trigger('submit')
  await flushPromises()
  expect(w.get('a[href="/thing/99"]').text()).toBe(t('things.openPhotos'))
  expect(w.get<HTMLInputElement>('form[data-create-thing] input').element.value).toBe('')
  expect(w.get('[role="alert"]').text()).toContain('refresh failed')
  vi.mocked(engine.things).mockResolvedValue([{ ...dogs, id: 99, name: 'Red boats', kind: TagKind.TagLabel, count: 0 }])
  await w.get('[role="alert"] button').trigger('click')
  await flushPromises()
  expect(engine.createLabel).toHaveBeenCalledTimes(1)
  expect(w.text()).toContain(t('things.viewEmptyHint'))
})

it('offers Undo for a saved pin and restores the prior value through the engine', async () => {
  const w = await mountWith([dogs])
  await menuAction(w, t('things.pinNamed', { name: dogs.name }))
  notices.find('things.change.4')?.actions?.[0]?.run()
  await flushPromises()
  expect(engine.setThingPinned).toHaveBeenLastCalledWith(4, false)
})

it('publishes Undo after the saved pin finishes refreshing so it can run immediately', async () => {
  const w = await mountWith([dogs])
  let finish: ((value: Tag[]) => void) | undefined
  vi.mocked(engine.things).mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        finish = resolve
      }),
  )
  await menuAction(w, t('things.pinNamed', { name: dogs.name }))
  expect(notices.find('things.change.4')).toBeUndefined()
  finish?.([{ ...dogs, pinned: true }])
  await flushPromises()
  notices.find('things.change.4')?.actions?.[0]?.run()
  await flushPromises()
  expect(engine.setThingPinned).toHaveBeenLastCalledWith(4, false)
})

it('names a custom Thing in its destructive confirmation and cancellation preserves it', async () => {
  const tag = { ...dogs, kind: TagKind.TagLabel }
  const w = await mountWith([tag])
  await menuAction(w, t('things.deleteNamed', { name: tag.name }))
  expect(question.value).toMatchObject({ title: t('things.deleteTitle', { name: tag.name }), danger: true })
  question.value?.answer(false)
  await flushPromises()
  expect(w.find('[data-collection="4"]').exists()).toBe(true)
})

it('keeps a refused rename beside its draft and cancels back to the title that opened it', async () => {
  const w = await mountWith([dogs])
  await w.get('button[title="Rename “Puppies”"]').trigger('click')
  await w.get('form[data-rename-thing] input').setValue('New name')
  vi.mocked(engine.renameThing).mockRejectedValueOnce(new Error('Library is busy'))
  await w.get('form[data-rename-thing]').trigger('submit')
  await flushPromises()
  expect(w.get<HTMLInputElement>('form[data-rename-thing] input').element.value).toBe('New name')
  expect(w.get('form[data-rename-thing] [role="alert"]').text()).toContain('Library is busy')
  await w.get('form[data-rename-thing]').trigger('keydown', { key: 'Escape' })
  await flushPromises()
  expect(w.find('[role="alert"]').exists()).toBe(false)
  expect(document.activeElement).toBe(w.get('button[title="Rename “Puppies”"]').element)
})

it('keeps a saved rename refresh failure visible after the editor closes and retries only the read', async () => {
  const w = await mountWith([dogs])
  await w.get('button[title="Rename “Puppies”"]').trigger('click')
  vi.mocked(engine.things).mockRejectedValueOnce(new Error('refresh failed'))
  await w.get('form[data-rename-thing]').trigger('submit')
  await flushPromises()
  expect(w.find('form[data-rename-thing]').exists()).toBe(false)
  expect(w.get('[role="alert"]').text()).toContain('refresh failed')
  await w.get('[role="alert"] button').trigger('click')
  await flushPromises()
  expect(engine.renameThing).toHaveBeenCalledTimes(1)
  expect(notices.find('things.change.4')?.actions).toHaveLength(1)
})

it('shows real creation progress, marks the result as new and preserves the confidence guidance for no matches', async () => {
  const w = await mountWith([])
  let finish: ((id: number) => void) | undefined
  vi.mocked(engine.createLabel).mockImplementationOnce(() => new Promise((resolve) => (finish = resolve)))
  await w.get('form[data-create-thing] input').setValue('Red boats')
  await w.get('input[aria-label="Filter Things"]').setValue('unmatched')
  await w.get('form[data-create-thing]').trigger('submit')
  expect(w.get('[data-thing-progress]').text()).toContain(t('things.lookingFor', { name: 'Red boats' }))
  expect(w.find('[data-thing-progress] [role="progressbar"]').exists()).toBe(true)
  vi.mocked(engine.things).mockResolvedValue([{ ...dogs, id: 99, name: 'Red boats', kind: TagKind.TagLabel, count: 0 }])
  finish?.(99)
  await flushPromises()
  expect(w.get('[data-created-thing="99"]').text()).toContain(t('things.viewEmptyHint'))
  expect(w.get('[data-collection="99"]').text()).toContain(t('things.fresh'))
  expect(w.get<HTMLInputElement>('input[aria-label="Filter Things"]').element.value).toBe('')
})

it('keeps the collection readable when a refresh fails and retries the read', async () => {
  const w = await mountWith([dogs])
  vi.mocked(engine.things).mockRejectedValueOnce(new Error('Library busy'))
  await menuAction(w, t('things.pinNamed', { name: dogs.name }))
  expect(w.get('[data-collection="4"]').text()).toContain(dogs.name)
  expect(w.get('[role="alert"]').text()).toContain('Library busy')
  await w.get('[role="alert"] button').trigger('click')
  await flushPromises()
  expect(w.find('[role="alert"]').exists()).toBe(false)
  expect(engine.setThingPinned).toHaveBeenCalledTimes(1)
})

it('keeps saved Things available while AI is unavailable and explains why Find is disabled', async () => {
  library.progress.ai.error = 'model unavailable'
  const w = await mountWith([dogs])
  expect(w.find('[data-collection="4"]').exists()).toBe(true)
  expect(w.text()).toContain(t('things.aiHint'))
  expect(w.get('form[data-create-thing] button').attributes('disabled')).toBeDefined()
  expect(w.get('form[data-create-thing] button').attributes('title')).toBe(t('things.aiUnavailable'))
})

it('deletes a custom Thing only after its destructive confirmation', async () => {
  const w = await mountWith([{ ...dogs, kind: TagKind.TagLabel }])
  await menuAction(w, t('things.deleteNamed', { name: dogs.name }))
  vi.mocked(engine.things).mockResolvedValue([])
  question.value?.answer(true)
  await flushPromises()
  expect(engine.deleteTag).toHaveBeenCalledWith(dogs.id)
  expect(w.find('[data-collection]').exists()).toBe(false)
  expect(w.text()).toContain(t('things.cleared'))
})

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

it('searches photos through the palette, leaving the field to filter Things', async () => {
  const palette = vi.fn()
  const scope = effectScope()
  scope.run(() => useCommands([{ id: 'app.commandPalette', group: 'app', title: () => 'Palette', run: palette }]))
  await mountWith([dogs])
  await wrapper?.get(`button[aria-label="${t('shell.search')}"]`).trigger('click')
  await flushPromises()
  expect(palette).toHaveBeenCalledOnce()
  await wrapper?.get(`input[aria-label="${t('things.search')}"]`).setValue('zzz')
  expect(wrapper?.text()).toContain(t('things.shown', { n: '0' }))
  scope.stop()
})
