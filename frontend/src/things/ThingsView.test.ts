import { afterEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import { engine, ErrorCode, MAX_NAME_LEN, TagKind } from '../bridge'
import type { Tag } from '../bridge'
import type * as Bridge from '../bridge'
import ThingsView from './ThingsView.vue'

vi.mock('vue-router', () => ({ useRouter: () => ({ push: vi.fn() }) }))
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

let wrapper: ReturnType<typeof mount> | undefined
afterEach(() => wrapper?.unmount())

it('validates names by code point before creating a Thing', async () => {
  setActivePinia(createPinia())
  wrapper = mount(ThingsView, { global: { stubs: { SearchBox: true, RouterLink: true } } })
  await flushPromises()
  expect(wrapper.get('form input').attributes('maxlength')).toBeUndefined()
  await wrapper.get('form input').setValue('😀'.repeat(MAX_NAME_LEN + 1))
  await wrapper.get('form').trigger('submit')
  await flushPromises()
  expect(wrapper.get('[role="alert"]').text()).toContain('80')
  expect(engine.createLabel).not.toHaveBeenCalled()
  await wrapper.get('form input').setValue(` ${'😀'.repeat(MAX_NAME_LEN)} `)
  await wrapper.get('form').trigger('submit')
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
  wrapper = mount(ThingsView, { global: { stubs: { SearchBox: true, RouterLink: true } } })
  await flushPromises()
  return wrapper
}

it('pins an unpinned Thing and unpins a pinned one', async () => {
  const w = await mountWith([dogs])
  expect(w.find('.pin-badge').exists()).toBe(false)
  await w.get('button[aria-label="Pin “Puppies”"]').trigger('click')
  await flushPromises()
  expect(engine.setThingPinned).toHaveBeenCalledWith(4, true)
  vi.mocked(engine.things).mockResolvedValue([{ ...dogs, pinned: true }])
  await w.get('button[aria-label="Pin “Puppies”"]').trigger('click')
  await flushPromises()
  expect(w.get('.pin-badge').attributes('aria-label')).toBe('Pinned')
  await w.get('button[aria-label="Unpin “Puppies”"]').trigger('click')
  await flushPromises()
  expect(engine.setThingPinned).toHaveBeenLastCalledWith(4, false)
})

it('renames a Thing from its card and shows the new name', async () => {
  const w = await mountWith([dogs])
  await w.get('button[aria-label="Rename “Puppies”"]').trigger('click')
  await flushPromises()
  await w.get('form.rename input').setValue('Good boys')
  vi.mocked(engine.things).mockResolvedValue([{ ...dogs, name: 'Good boys' }])
  await w.get('form.rename').trigger('submit')
  await flushPromises()
  expect(engine.renameThing).toHaveBeenCalledWith(4, 'Good boys')
  expect(w.find('form.rename').exists()).toBe(false)
  expect(w.get('.card-name').text()).toBe('Good boys')
})

it('resets a renamed built-in with an empty name', async () => {
  const w = await mountWith([dogs])
  await w.get('button.reset').trigger('click')
  await flushPromises()
  expect(engine.renameThing).toHaveBeenCalledWith(4, '')
})

it('offers no reset for a Thing that was not renamed', async () => {
  const w = await mountWith([{ ...dogs, name: 'Dogs', original: '' }])
  expect(w.find('button.reset').exists()).toBe(false)
})

it('keeps the editor open and says why when a name is refused', async () => {
  const w = await mountWith([dogs])
  await w.get('button[aria-label="Rename “Puppies”"]').trigger('click')
  await flushPromises()
  vi.mocked(engine.renameThing).mockRejectedValue(new Error('x', { cause: { code: ErrorCode.TagExists } }))
  await w.get('form.rename').trigger('submit')
  await flushPromises()
  expect(w.find('form.rename').exists()).toBe(true)
  expect(w.get('.page-error[role="alert"]').text()).toBe('A Thing already has that name.')
})

it('lists pinned Things first, built-in or not, and the rest in their sections', async () => {
  const mineTag: Tag = { ...dogs, id: 1, name: 'Mine', kind: TagKind.TagLabel, original: '' }
  const pinnedBuiltin: Tag = { ...dogs, id: 2, name: 'Birds', original: '', pinned: true }
  const pinnedMine: Tag = { ...mineTag, id: 3, name: 'Pinned mine', pinned: true }
  const plainBuiltin: Tag = { ...dogs, id: 4, name: 'Cars', original: '' }
  // The engine sends pinned Things first, the newest pin first.
  const w = await mountWith([pinnedBuiltin, pinnedMine, mineTag, plainBuiltin])
  expect(w.findAll('.card-name').map((n) => n.text())).toEqual(['Birds', 'Pinned mine', 'Mine', 'Cars'])
  expect(w.findAll('h2').map((h) => h.text())).toEqual(['Pinned', 'Made by you', 'Things'])
})
