import type * as Router from 'vue-router'
import { mountInApp } from '../shared/testing'
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
  useRoute: () => ({ params: { id: '1' } }),
  useRouter: () => ({ replace }),
}))
vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  onLibraryChanged: vi.fn(() => () => undefined),
  engine: { person: vi.fn(), personFaces: vi.fn(), rejectFaces: vi.fn(), people: vi.fn() },
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
  vi.useRealTimers()
})

async function mounted() {
  wrapper = mountInApp(PersonFaces, { plugins: [testRouter()] })
  await flushPromises()
  await wrapper.get('.face-tile').trigger('click')
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
  let changed: ((c: Changes) => void) | undefined
  vi.mocked(onLibraryChanged).mockImplementation((cb) => {
    changed = cb
    return () => undefined
  })
  const w = await mounted()
  expect(w.get('.face-tile').attributes('aria-pressed')).toBe('true')
  await w.get('.face-tile').trigger('click')
  expect(w.get('.face-tile').attributes('aria-pressed')).toBe('false')
  await w.get('.face-tile').trigger('click')
  vi.mocked(engine.personFaces).mockResolvedValue([{ ...face, id: 12, byUser: true }])
  changed?.({ assets: false, thumbs: false, tags: false, people: true })
  await vi.advanceTimersByTimeAsync(3000)
  await flushPromises()
  expect(w.get('.face-tile').attributes('aria-label')).toContain('placed by you')
  expect(w.get('.face-tile').attributes('aria-pressed')).toBe('false')
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
