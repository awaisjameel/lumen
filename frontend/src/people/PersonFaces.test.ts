import type * as Router from 'vue-router'
import { mountInApp, recordNotifications } from '../shared/testing'
import { blockRequest } from '../shared/block'
import { BlockKind } from '../bridge'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises } from '@vue/test-utils'
import { engine, ErrorCode, onLibraryChanged } from '../bridge'
import type * as Bridge from '../bridge'
import type { Changes, Face, Person } from '../bridge'
import PersonFaces from './PersonFaces.vue'

const { replace } = vi.hoisted(() => ({ replace: vi.fn() }))
vi.mock('vue-router', async (importOriginal) => ({
  ...(await importOriginal<typeof Router>()),
  useRoute: () => ({ params: { id: '1' }, query: {} }),
  useRouter: () => ({ replace }),
}))
vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  onLibraryChanged: vi.fn(() => () => undefined),
  engine: {
    person: vi.fn(),
    personFaces: vi.fn(),
    rejectFaces: vi.fn(),
    people: vi.fn(),
    timeline: vi.fn().mockResolvedValue([]),
    setPersonCover: vi.fn().mockResolvedValue(undefined),
  },
}))

const face: Face = {
  id: 11,
  assetId: 2,
  thumb: 0,
  assetW: 400,
  assetH: 300,
  video: false,
  frame: 0,
  x: 0,
  y: 0,
  w: 0.1,
  h: 0.1,
  person: 1,
  name: 'Ali',
  byUser: false,
}
const person: Person = { id: 1, name: 'Ali', count: 1, cover: face }
let wrapper: ReturnType<typeof mountInApp> | undefined

beforeEach(() => {
  setActivePinia(createPinia())
  vi.mocked(engine.person).mockReset().mockResolvedValue(person)
  vi.mocked(engine.personFaces).mockReset().mockResolvedValue([face])
  vi.mocked(engine.rejectFaces).mockReset().mockResolvedValue(undefined)
  vi.mocked(engine.people).mockResolvedValue([])
})
afterEach(() => {
  wrapper?.unmount()
  blockRequest.value?.answer(false)
  blockRequest.value = null
  vi.useRealTimers()
})

it('keeps the dock absent without a selection and explains why two faces cannot be a picture', async () => {
  vi.mocked(engine.personFaces).mockResolvedValue([face, { ...face, id: 12 }])
  wrapper = mountInApp(PersonFaces, { plugins: [testRouter()] })
  await flushPromises()
  expect(wrapper.find('[role="toolbar"][aria-label="0 selected"]').exists()).toBe(false)
  expect(wrapper.find('[data-action="reject"]').exists()).toBe(false)
  await wrapper.get('[data-face="11"] [data-tile-check]').trigger('click')
  await wrapper.get('[data-face="12"] [data-tile-check]').trigger('click')
  expect(wrapper.find('[role="toolbar"][aria-label="2 selected"]').exists()).toBe(true)
  expect(wrapper.get('[title="Select one face to use as their picture."]').attributes('disabled')).toBeDefined()
  await wrapper.get('[aria-label="Clear selection"]').trigger('click')
  expect(wrapper.find('[data-action="reject"]').exists()).toBe(false)
})

it('keeps a failed correction selected and prevents changes while the correction is saving', async () => {
  let fail: ((reason: Error) => void) | undefined
  vi.mocked(engine.rejectFaces).mockImplementation(
    () =>
      new Promise((_resolve, reject) => {
        fail = reject
      }),
  )
  const w = await mounted()
  await w.get('[data-action="reject"]').trigger('click')
  expect(w.get('[aria-label="Face 1"]').attributes('disabled')).toBeDefined()
  await w.get('[aria-label="Face 1"]').trigger('click')
  expect(w.get('[aria-label="Face 1"]').attributes('aria-pressed')).toBe('true')
  fail?.(new Error('library busy'))
  await flushPromises()
  expect(w.get('[aria-label="Face 1"]').attributes('aria-pressed')).toBe('true')
  expect(w.get('[role="alert"]').text()).toContain('library busy')
})

it('opens the shared face block flow from a quick action without changing the selection', async () => {
  const w = await mounted()
  await w.get('[aria-label="Block face 1"]').trigger('click')
  expect(blockRequest.value?.kind).toBe(BlockKind.BlockFace)
  expect(blockRequest.value?.targetID).toBe(11)
  expect(w.get('[aria-label="Face 1"]').attributes('aria-pressed')).toBe('true')
})

it('restores the previous representative face through the quiet picture notification', async () => {
  const notices = recordNotifications()
  try {
    vi.mocked(engine.person).mockResolvedValue({ ...person, cover: { ...face, id: 9 } })
    const w = await mounted()
    await w.get('[title="Use as their picture"]').trigger('click')
    await flushPromises()
    expect(engine.setPersonCover).toHaveBeenCalledWith(1, 11)
    notices.find('faces.picture')?.actions?.[0]?.run()
    await flushPromises()
    expect(engine.setPersonCover).toHaveBeenLastCalledWith(1, 9)
  } finally {
    notices.stop()
  }
})

async function mounted() {
  wrapper = mountInApp(PersonFaces, { plugins: [testRouter()] })
  await flushPromises()
  await wrapper.get('[aria-label="Face 1"]').trigger('click')
  return wrapper
}

it('shows a failed reload after a correction without treating it as a missing person', async () => {
  const w = await mounted()
  vi.mocked(engine.personFaces).mockRejectedValue(new Error('disk full'))
  await w.get('[data-action="reject"]').trigger('click')
  await flushPromises()
  expect(engine.rejectFaces).toHaveBeenCalledWith(1, [11])
  expect(replace).not.toHaveBeenCalled()
  expect(w.get('[role="alert"]').text()).toContain('disk full')
})

it('leaves face review when a correction removes the person', async () => {
  const w = await mounted()
  vi.mocked(engine.person).mockRejectedValue(new Error('gone', { cause: { code: ErrorCode.NotFound } }))
  await w.get('[data-action="reject"]').trigger('click')
  await flushPromises()
  expect(replace).toHaveBeenCalledWith({ name: 'people' })
  expect(w.find('[role="alert"]').exists()).toBe(false)
})

it('updates memoised faces and prunes the selection when a reload replaces the list', async () => {
  vi.useFakeTimers()
  const listeners = new Set<(c: Changes) => void>()
  vi.mocked(onLibraryChanged).mockImplementation((cb) => {
    listeners.add(cb)
    return () => {
      listeners.delete(cb)
    }
  })
  const w = await mounted()
  expect(w.get('[aria-label="Face 1"]').attributes('aria-pressed')).toBe('true')
  await w.get('[aria-label="Face 1"]').trigger('click')
  expect(w.get('[aria-label="Face 1"]').attributes('aria-pressed')).toBe('false')
  await w.get('[aria-label="Face 1"]').trigger('click')
  vi.mocked(engine.personFaces).mockResolvedValue([{ ...face, id: 12, byUser: true }])
  for (const changed of listeners) changed({ assets: false, thumbs: false, tags: false, people: true })
  await vi.advanceTimersByTimeAsync(3000)
  await flushPromises()
  expect(w.get('[data-face="12"] button[aria-pressed]').attributes('aria-label')).toContain('placed by you')
  expect(w.get('[data-face="12"] button[aria-pressed]').attributes('aria-pressed')).toBe('false')
  expect(w.text()).not.toContain('1 selected')
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
