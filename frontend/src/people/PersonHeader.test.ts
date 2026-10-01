import { afterEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import { engine, ErrorCode, MAX_NAME_LEN, onLibraryChanged } from '../bridge'
import type * as Bridge from '../bridge'
import type { Changes, Person } from '../bridge'
import PersonHeader from './PersonHeader.vue'

vi.mock('vue-router', () => ({ useRouter: () => ({ replace: vi.fn() }) }))
vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  onLibraryChanged: vi.fn(() => () => undefined),
  engine: { person: vi.fn(), namePerson: vi.fn(), people: vi.fn() },
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
let wrapper: ReturnType<typeof mount> | undefined
afterEach(() => {
  wrapper?.unmount()
  vi.useRealTimers()
})

it('discards an earlier person response after navigating away and back', async () => {
  setActivePinia(createPinia())
  let finish!: (value: Person) => void
  vi.mocked(engine.person)
    .mockReturnValueOnce(new Promise((resolve) => (finish = resolve)))
    .mockImplementation((id) => Promise.resolve(person(id, 'Current name')))
  wrapper = mount(PersonHeader, { props: { id: 1 }, global: { stubs: { RouterLink: true } } })
  await wrapper.setProps({ id: 2 })
  await flushPromises()
  await wrapper.setProps({ id: 1 })
  await flushPromises()
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
  wrapper = mount(PersonHeader, { props: { id: 1 }, global: { stubs: { RouterLink: true } } })
  await flushPromises()
  await wrapper.get('button.name').trigger('click')
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
  await wrapper.setProps({ id: 2 })
  await flushPromises()
  expect(wrapper.find('[role="alert"]').exists()).toBe(false)
})

it('accepts a full emoji name and leaves an overlong name in the editor with validation', async () => {
  setActivePinia(createPinia())
  vi.mocked(engine.person).mockReset().mockResolvedValue(person(1, 'Ali'))
  vi.mocked(engine.namePerson).mockReset().mockResolvedValue(undefined)
  vi.mocked(engine.people).mockResolvedValue([])
  wrapper = mount(PersonHeader, { props: { id: 1 }, global: { stubs: { RouterLink: true } } })
  await flushPromises()
  await wrapper.get('button.name').trigger('click')
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
  wrapper = mount(PersonHeader, { attachTo: document.body, props: { id: 1 }, global: { stubs: { RouterLink: true } } })
  await flushPromises()
  await wrapper.get('button.name').trigger('click')
  await wrapper.get('input').setValue('Sara')
  await wrapper.get('input').trigger('keydown', { key: 'Enter' })
  await flushPromises()
  expect(wrapper.get('button.name').attributes('aria-disabled')).toBe('true')
  expect(document.activeElement).toBe(wrapper.get('button.name').element)
  await wrapper.get('button.name').trigger('click')
  expect(wrapper.find('input').exists()).toBe(false)
  expect(engine.namePerson).toHaveBeenCalledOnce()
  finish?.()
  await flushPromises()
  expect(wrapper.get('button.name').attributes('aria-disabled')).toBeUndefined()
  await wrapper.get('button.name').trigger('click')
  expect(wrapper.find('input').exists()).toBe(true)
})
