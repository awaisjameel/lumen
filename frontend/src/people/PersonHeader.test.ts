import type * as Router from 'vue-router'
import { defineComponent, h, nextTick, ref } from 'vue'
import { mountInApp } from '../shared/testing'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises } from '@vue/test-utils'
import { engine, ErrorCode, MAX_NAME_LEN, onLibraryChanged } from '../bridge'
import type * as Bridge from '../bridge'
import type { Changes, Person } from '../bridge'
import PersonHeader from './PersonHeader.vue'
import { showBlocksChanged } from '../shared/block'

vi.mock('vue-router', async (importOriginal) => ({
  ...(await importOriginal<typeof Router>()),
  useRouter: () => ({ replace: vi.fn() }),
}))
vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  onLibraryChanged: vi.fn(() => () => undefined),
  engine: {
    person: vi.fn(),
    namePerson: vi.fn(),
    people: vi.fn(),
    timeline: vi.fn().mockResolvedValue([]),
    personFacts: vi.fn().mockResolvedValue({ faces: 0, companions: [] }),
    topPlaces: vi.fn().mockResolvedValue([]),
    suggestPeople: vi.fn().mockResolvedValue([]),
  },
}))

const person = (id: number, name: string): Person => ({
  id,
  name,
  count: 1,
  cover: {
    id: 0,
    assetId: 0,
    thumb: 0,
    assetW: 0,
    assetH: 0,
    video: false,
    frame: 0,
    x: 0,
    y: 0,
    w: 0,
    h: 0,
    person: id,
    name,
    byUser: false,
  },
})
let wrapper: ReturnType<typeof mountInApp> | undefined
afterEach(() => {
  wrapper?.unmount()
  vi.useRealTimers()
})

it('discards an earlier person response after navigating away and back', async () => {
  setActivePinia(createPinia())
  let finish: ((value: Person) => void) | undefined
  vi.mocked(engine.person)
    .mockReturnValueOnce(new Promise((resolve) => (finish = resolve)))
    .mockImplementation((id) => Promise.resolve(person(id, 'Current name')))
  wrapper = mountHeader()
  headerId.value = 2
  await nextTick()
  await flushPromises()
  headerId.value = 1
  await nextTick()
  await flushPromises()
  if (!finish) throw new Error('The first person read did not start')
  finish(person(1, 'Stale name'))
  await flushPromises()
  expect(wrapper.text()).toContain('Current name')
  expect(wrapper.text()).not.toContain('Stale name')
})

it('keeps a rename failure on screen while the person reloads around it', async () => {
  setActivePinia(createPinia())
  vi.useFakeTimers()
  let changed: ((c: Changes) => void) | undefined
  vi.mocked(onLibraryChanged).mockImplementation((cb) => {
    changed = cb
    return () => undefined
  })
  vi.mocked(engine.person).mockReset().mockResolvedValue(person(1, 'Ali'))
  vi.mocked(engine.namePerson).mockRejectedValue(new Error('blocked', { cause: { code: ErrorCode.BlockedName } }))
  wrapper = mountHeader()
  await flushPromises()
  await wrapper.get('[data-action="rename"]').trigger('click')
  await flushPromises()
  await wrapper.get('input').setValue('Blocked name')
  await wrapper.get('input').trigger('keydown', { key: 'Enter' })
  await flushPromises()
  const failure = wrapper.get('[role="alert"]').text()
  expect(failure).toContain('blocked')
  changed?.({ assets: true, thumbs: false, tags: false, people: false })
  await vi.advanceTimersByTimeAsync(3000)
  await flushPromises()
  expect(engine.person).toHaveBeenCalledTimes(2)
  expect(wrapper.get('[role="alert"]').text()).toBe(failure)
  headerId.value = 2
  await nextTick()
  await flushPromises()
  expect(wrapper.find('[role="alert"]').exists()).toBe(false)
})

it('accepts a full emoji name and leaves an overlong name in the editor with validation', async () => {
  setActivePinia(createPinia())
  vi.mocked(engine.person).mockReset().mockResolvedValue(person(1, 'Ali'))
  vi.mocked(engine.namePerson).mockReset().mockResolvedValue(undefined)
  vi.mocked(engine.people).mockResolvedValue([])
  wrapper = mountHeader()
  await flushPromises()
  await wrapper.get('[data-action="rename"]').trigger('click')
  await flushPromises()
  expect(wrapper.get('input').attributes('maxlength')).toBeUndefined()
  await wrapper.get('input').setValue('😀'.repeat(MAX_NAME_LEN + 1))
  await wrapper.get('input').trigger('keydown', { key: 'Enter' })
  await flushPromises()
  expect(wrapper.get('[role="alert"]').text()).toContain('80')
  expect(engine.namePerson).not.toHaveBeenCalled()
  expect(wrapper.find('input').exists()).toBe(true)
  await wrapper.get('input').setValue('😀'.repeat(MAX_NAME_LEN))
  await wrapper.get('input').trigger('keydown', { key: 'Enter' })
  await flushPromises()
  expect(engine.namePerson).toHaveBeenCalledWith(1, '😀'.repeat(MAX_NAME_LEN))
})

it('waits for a rename to finish before allowing another rename', async () => {
  setActivePinia(createPinia())
  let finish: (() => void) | undefined
  vi.mocked(engine.person).mockReset().mockResolvedValue(person(1, 'Ali'))
  vi.mocked(engine.people).mockResolvedValue([])
  vi.mocked(engine.namePerson)
    .mockReset()
    .mockReturnValue(new Promise((resolve) => (finish = resolve)))
  wrapper = mountHeader()
  await flushPromises()
  await wrapper.get('[data-action="rename"]').trigger('click')
  await wrapper.get('input').setValue('Sara')
  await wrapper.get('input').trigger('keydown', { key: 'Enter' })
  await flushPromises()
  expect(wrapper.get('input').attributes('disabled')).toBeDefined()
  await wrapper.get('input').trigger('keydown', { key: 'Enter' })
  expect(wrapper.get<HTMLInputElement>('input').element.value).toBe('Sara')
  expect(engine.namePerson).toHaveBeenCalledOnce()
  finish?.()
  await flushPromises()
  expect(document.activeElement).toBe(wrapper.get('[data-action="rename"]').element)
  await wrapper.get('[data-action="rename"]').trigger('click')
  expect(wrapper.find('input').exists()).toBe(true)
})

it('derives the date range from visible photo days and clears the old portrait before a block refresh returns', async () => {
  setActivePinia(createPinia())
  vi.mocked(engine.person).mockReset().mockResolvedValue(person(1, 'Ali'))
  vi.mocked(engine.timeline).mockResolvedValue([
    { day: '2026-01-01', count: 2 },
    { day: '2014-02-01', count: 1 },
  ])
  wrapper = mountHeader()
  await flushPromises()
  expect(wrapper.text()).toContain('1 photo · 2014–2026')
  let finish: ((value: Person) => void) | undefined
  vi.mocked(engine.person).mockReturnValue(
    new Promise((resolve) => {
      finish = resolve
    }),
  )
  const refreshing = showBlocksChanged()
  await nextTick()
  expect(wrapper.text()).not.toContain('Ali')
  finish?.(person(1, 'Current name'))
  await refreshing
  await flushPromises()
  expect(wrapper.text()).toContain('Current name')
})

it('names who is most often in photos with them and where most were taken, and counts their faces', async () => {
  setActivePinia(createPinia())
  vi.mocked(engine.person).mockReset().mockResolvedValue(person(1, 'Ali'))
  vi.mocked(engine.personFacts).mockResolvedValue({ faces: 36, companions: [person(2, 'Omar'), person(3, 'Sara')] })
  vi.mocked(engine.topPlaces).mockResolvedValue([
    { id: 7, name: 'Karachi' },
    { id: 8, name: 'Lahore' },
  ])
  wrapper = mountHeader()
  await flushPromises()
  expect(engine.topPlaces).toHaveBeenCalledWith(expect.objectContaining({ person: 1 }))
  expect(wrapper.findAll('[data-person-companion]').map((c) => [c.text(), c.attributes('href')])).toEqual([
    ['Omar', '/person/2'],
    ['Sara', '/person/3'],
  ])
  expect(wrapper.findAll('[data-person-place]').map((p) => [p.text(), p.attributes('href')])).toEqual([
    ['Karachi', '/place/7'],
    ['Lahore', '/place/8'],
  ])
  expect(wrapper.get('.person-tabs').text()).toContain('Faces36')
  // Blocking someone takes what was said about them away at once.
  vi.mocked(engine.person).mockReturnValue(new Promise(() => undefined))
  void showBlocksChanged()
  await nextTick()
  expect(wrapper.find('[data-person-facts]').exists()).toBe(false)
})

const headerId = ref(1)
function mountHeader() {
  headerId.value = 1
  const Host = defineComponent({ setup: () => () => h(PersonHeader, { id: headerId.value }) })
  return mountInApp(Host, { plugins: [testRouter()] })
}

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
