import { afterEach, expect, it } from 'vitest'
import { mountInApp } from '../shared/testing'
import PersonPickerOptions from './PersonPickerOptions.vue'
import { personFixture } from './testing'

let wrapper: ReturnType<typeof mountInApp> | undefined
afterEach(() => wrapper?.unmount())
it('labels each person with their photo count and returns the selected identity', async () => {
  const person = personFixture(2, 'Sara', 812)
  wrapper = mountInApp(PersonPickerOptions, { props: { id: 'picker', people: [person], active: 0 } })
  const option = wrapper.get('[role="option"]')
  expect(option.text()).toContain('812 photos')
  expect(option.attributes('aria-selected')).toBe('true')
  await option.trigger('click')
  expect(wrapper.findComponent(PersonPickerOptions).emitted('pick')).toEqual([[person]])
})
