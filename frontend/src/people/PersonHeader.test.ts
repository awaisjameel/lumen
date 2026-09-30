import { afterEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import { engine } from '../bridge'
import type * as Bridge from '../bridge'
import type { Person } from '../bridge'
import PersonHeader from './PersonHeader.vue'

vi.mock('vue-router', () => ({ useRouter: () => ({ replace: vi.fn() }) }))
vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  onLibraryChanged: vi.fn(() => () => undefined),
  engine: { person: vi.fn() },
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
afterEach(() => wrapper?.unmount())

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
