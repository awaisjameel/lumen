import type * as Router from 'vue-router'
import { t } from '../shared/i18n'
import { mountInApp } from '../shared/testing'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { DOMWrapper, flushPromises } from '@vue/test-utils'
import { engine, ErrorCode, MAX_NAME_LEN, TagKind } from '../bridge'
import type { Tag } from '../bridge'
import type * as Bridge from '../bridge'
import ThingsView from './ThingsView.vue'

vi.mock('vue-router', async (importOriginal) => ({
  ...(await importOriginal<typeof Router>()),
  useRouter: () => ({ push: vi.fn() }),
}))
vi.mock('../library/store', () => ({ useLibrary: () => ({ progress: null }) }))
vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  onLibraryChanged: vi.fn(() => () => undefined),
  engine: {
    things: vi.fn().mockResolvedValue([]),
    createLabel: vi.fn().mockResolvedValue(1),
    renameThing: vi.fn().mockResolvedValue(undefined),
    setThingPinned: vi.fn().mockResolvedValue(undefined),
  },
}))

let wrapper: ReturnType<typeof mountInApp> | undefined
afterEach(() => wrapper?.unmount())

it('validates names by code point before creating a Thing', async () => {
  setActivePinia(createPinia())
  wrapper = mountInApp(ThingsView, { plugins: [testRouter()] })
  await flushPromises()
  expect(wrapper.get('form.new input').attributes('maxlength')).toBeUndefined()
  await wrapper.get('form.new input').setValue('😀'.repeat(MAX_NAME_LEN + 1))
  await wrapper.get('form.new').trigger('submit')
  await flushPromises()
  expect(wrapper.get('[role="alert"]').text()).toContain('80')
  expect(engine.createLabel).not.toHaveBeenCalled()
  await wrapper.get('form.new input').setValue(` ${'😀'.repeat(MAX_NAME_LEN)} `)
  await wrapper.get('form.new').trigger('submit')
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
  wrapper = mountInApp(ThingsView, { plugins: [testRouter()] })
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
  await w.get('form.rename input').setValue('Good boys')
  vi.mocked(engine.things).mockResolvedValue([{ ...dogs, name: 'Good boys' }])
  await w.get('form.rename').trigger('submit')
  await flushPromises()
  expect(engine.renameThing).toHaveBeenCalledWith(4, 'Good boys')
  expect(w.find('form.rename').exists()).toBe(false)
  expect(w.get('.collection-card .name').text()).toBe('Good boys')
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
  await w.get('form.rename').trigger('submit')
  await flushPromises()
  expect(w.find('form.rename').exists()).toBe(true)
  expect(w.get('[role="alert"]').text()).toBe('A Thing already has that name.')
})

it('lists pinned Things first, built-in or not, and the rest in their sections', async () => {
  const mineTag: Tag = { ...dogs, id: 1, name: 'Mine', kind: TagKind.TagLabel, original: '' }
  const pinnedBuiltin: Tag = { ...dogs, id: 2, name: 'Birds', original: '', pinned: true }
  const pinnedMine: Tag = { ...mineTag, id: 3, name: 'Pinned mine', pinned: true }
  const plainBuiltin: Tag = { ...dogs, id: 4, name: 'Cars', original: '' }
  // The engine sends pinned Things first, the newest pin first.
  const w = await mountWith([pinnedBuiltin, pinnedMine, mineTag, plainBuiltin])
  expect(w.findAll('.collection-card .name').map((n) => n.text())).toEqual(['Birds', 'Pinned mine', 'Mine', 'Cars'])
  expect(w.findAll('h2').map((h) => h.text())).toEqual(['Pinned', 'Made by you', 'Things'])
})

it('distinguishes built-in and custom Things and filters names and original prompts without reloading', async () => {
  const custom: Tag = { ...dogs, id: 8, name: 'Eiffel Tower', kind: TagKind.TagLabel, original: '' }
  const w = await mountWith([dogs, custom])
  expect(w.findAll('.collection-card').map((card) => card.text())).toEqual([
    expect.stringContaining(t('things.custom')),
    expect.stringContaining(t('things.builtin')),
  ])
  const reads = vi.mocked(engine.things).mock.calls.length
  const filter = w.get('input[aria-label="Filter Things"]')
  await filter.setValue('dogs')
  expect(w.findAll('.collection-card .name').map((name) => name.text())).toEqual(['Puppies'])
  await filter.setValue('tower')
  expect(w.findAll('.collection-card .name').map((name) => name.text())).toEqual(['Eiffel Tower'])
  await filter.setValue('unmatched')
  expect(w.findAll('.collection-card')).toHaveLength(0)
  expect(w.text()).toContain(t('things.noMatches'))
  await filter.setValue('')
  expect(w.findAll('.collection-card')).toHaveLength(2)
  expect(engine.things).toHaveBeenCalledTimes(reads)
})

it('keeps focus in the rename field and returns it to the action menu on Escape', async () => {
  const w = await mountWith([dogs])
  await menuAction(w, t('things.renameNamed', { name: dogs.name }))
  // The field takes the focus once the menu, which held it, has closed.
  await vi.waitFor(() => expect(document.activeElement).toBe(w.get('form.rename input').element))
  await w.get('form.rename').trigger('keydown', { key: 'Escape' })
  await flushPromises()
  expect(w.find('form.rename').exists()).toBe(false)
  expect(document.activeElement).toBe(w.get('button[aria-label^="Actions for"]').element)
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
