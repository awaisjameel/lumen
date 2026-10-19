import { createPinia, setActivePinia } from 'pinia'
import { mountInApp } from '../shared/testing'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { defineComponent, h, ref } from 'vue'
import { engine, MAX_NAME_LEN } from '../bridge'
import type { Face, Person } from '../bridge'
import type * as Bridge from '../bridge'
import PersonPicker from './PersonPicker.vue'
import { personFixture } from './testing'

vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  engine: {
    people: vi.fn().mockResolvedValue([]),
    suggestPeople: vi.fn().mockResolvedValue([]),
    personByName: vi.fn(),
  },
}))

beforeEach(() => {
  setActivePinia(createPinia())
  vi.mocked(engine.people).mockReset().mockResolvedValue([])
})

let wrapper: ReturnType<typeof mountInApp> | undefined
afterEach(() => {
  wrapper?.unmount()
  vi.useRealTimers()
})

it('rejects a typed current-person name using the engine’s canonical comparison', async () => {
  vi.mocked(engine.personByName).mockResolvedValue(1)
  wrapper = mountInApp(PersonPicker, { props: { placeholder: 'Name', exclude: 1 }, plugins: [testRouter()] })
  await wrapper.get('input').setValue('ALI')
  await wrapper.get('input').trigger('keydown', { key: 'Enter' })
  await flushPromises()
  expect(engine.personByName).toHaveBeenCalledWith('ALI')
  expect(wrapper.findComponent(PersonPicker).emitted('pick')).toBeUndefined()
  expect(wrapper.get('[role="alert"]').text()).toContain('Choose someone else')
  expect(wrapper.get<HTMLInputElement>('input').element.value).toBe('ALI')
})

it('allows the engine emoji limit and rejects longer typed names inline', async () => {
  wrapper = mountInApp(PersonPicker, { props: { placeholder: 'Name' }, plugins: [testRouter()] })
  expect(wrapper.get('input').attributes('maxlength')).toBeUndefined()
  await wrapper.get('input').setValue('😀'.repeat(MAX_NAME_LEN + 1))
  await wrapper.get('input').trigger('keydown', { key: 'Enter' })
  expect(wrapper.findComponent(PersonPicker).emitted('pick')).toBeUndefined()
  expect(wrapper.get('[role="alert"]').text()).toContain('80')
  await wrapper.get('input').setValue(` ${'😀'.repeat(MAX_NAME_LEN)} `)
  await wrapper.get('input').trigger('keydown', { key: 'Enter' })
  expect(wrapper.findComponent(PersonPicker).emitted('pick')).toEqual([['😀'.repeat(MAX_NAME_LEN)]])
})

it('portals suggestions while retaining input focus, exclusion and arrow-key selection', async () => {
  vi.useFakeTimers()
  const cover: Face = {
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
    person: 0,
    name: '',
    byUser: false,
  }
  const people: Person[] = [
    { id: 1, name: 'Ali', count: 1, cover },
    { id: 2, name: 'Sara', count: 1, cover },
  ]
  vi.mocked(engine.suggestPeople).mockResolvedValue(people)
  wrapper = mountInApp(PersonPicker, { props: { placeholder: 'Name', exclude: 1 }, plugins: [testRouter()] })
  const input = wrapper.get<HTMLInputElement>('input')
  input.element.focus()
  await input.setValue('a')
  await vi.advanceTimersByTimeAsync(200)
  await flushPromises()
  expect(document.activeElement).toBe(input.element)
  expect([...document.body.querySelectorAll('[data-person-name]')].map((option) => option.textContent.trim())).toEqual([
    'Sara',
  ])
  await input.trigger('keydown', { key: 'ArrowDown' })
  expect(input.attributes('aria-activedescendant')).toBeDefined()
  await input.trigger('keydown', { key: 'Enter' })
  expect(wrapper.findComponent(PersonPicker).emitted('pick')).toEqual([['Sara']])
  expect(input.element.value).toBe('')
  expect(input.attributes('aria-expanded')).toBe('false')
  expect(document.activeElement).toBe(input.element)
})

it('moves up from the typed text to the last suggestion and wraps both ways', async () => {
  vi.useFakeTimers()
  const cover: Face = {
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
    person: 0,
    name: '',
    byUser: false,
  }
  vi.mocked(engine.suggestPeople).mockResolvedValue(
    ['Ali', 'Sara', 'Zoya'].map((name, i): Person => ({ id: i + 1, name, count: 1, cover })),
  )
  wrapper = mountInApp(PersonPicker, { props: { placeholder: 'Name' }, plugins: [testRouter()] })
  const input = wrapper.get<HTMLInputElement>('input')
  input.element.focus()
  await input.setValue('a')
  await vi.advanceTimersByTimeAsync(200)
  await flushPromises()
  const active = () =>
    document
      .getElementById(input.attributes('aria-activedescendant') ?? '')
      ?.querySelector('[data-person-name]')
      ?.textContent.trim()
  await input.trigger('keydown', { key: 'ArrowUp' })
  expect(active()).toBe('Zoya')
  await input.trigger('keydown', { key: 'ArrowUp' })
  expect(active()).toBe('Sara')
  await input.trigger('keydown', { key: 'ArrowDown' })
  await input.trigger('keydown', { key: 'ArrowDown' })
  expect(active()).toBe('Ali')
  // Composing text: Enter is the input method's, not a pick.
  await input.trigger('keydown', { key: 'Enter', isComposing: true })
  expect(wrapper.findComponent(PersonPicker).emitted('pick')).toBeUndefined()
})

it('shows existing people with authoritative photo counts before a name is typed', async () => {
  vi.mocked(engine.people).mockResolvedValue([personFixture(1, 'Ali', 1284), personFixture(2, 'Sara', 812)])
  wrapper = mountInApp(PersonPicker, { props: { placeholder: 'Name', exclude: 1 }, plugins: [testRouter()] })
  await flushPromises()
  await wrapper.get('input').trigger('focus')
  await flushPromises()
  expect(document.body.textContent).toContain('812 photos')
  expect([...document.body.querySelectorAll('[data-person-name]')].map((el) => el.textContent.trim())).toEqual(['Sara'])
})

it('keeps the entered name after a managed move fails and blocks repeated picks while saving', async () => {
  const busy = ref(false),
    failure = ref('')
  const picked = vi.fn(() => {
    busy.value = true
  })
  const Host = defineComponent({
    setup: () => () =>
      h(PersonPicker, { placeholder: 'Name', busy: busy.value, error: failure.value, managed: true, onPick: picked }),
  })
  wrapper = mountInApp(Host, { plugins: [testRouter()] })
  const input = wrapper.get('input')
  await input.setValue('Sara')
  await input.trigger('keydown', { key: 'Enter' })
  expect(picked).toHaveBeenCalledOnce()
  await input.trigger('keydown', { key: 'Enter' })
  expect(picked).toHaveBeenCalledOnce()
  failure.value = 'The local library is busy.'
  busy.value = false
  await flushPromises()
  expect(wrapper.get<HTMLInputElement>('input').element.value).toBe('Sara')
  expect(wrapper.get('[role="alert"]').text()).toContain('local library is busy')
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
