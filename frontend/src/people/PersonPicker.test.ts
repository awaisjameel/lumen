import { mountInApp } from '../shared/testing'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { engine, MAX_NAME_LEN } from '../bridge'
import type { Face, Person } from '../bridge'
import type * as Bridge from '../bridge'
import PersonPicker from './PersonPicker.vue'

vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  engine: { suggestPeople: vi.fn().mockResolvedValue([]) },
}))

let wrapper: ReturnType<typeof mountInApp> | undefined
afterEach(() => {
  wrapper?.unmount()
  vi.useRealTimers()
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
  expect([...document.body.querySelectorAll('[role="option"]')].map((option) => option.textContent.trim())).toEqual([
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
