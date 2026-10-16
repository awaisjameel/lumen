import { afterEach, expect, it } from 'vitest'
import { defineComponent, h, ref } from 'vue'
import Segmented from './Segmented.vue'
import { mountInApp } from './testing'

let wrapper: ReturnType<typeof mountInApp> | undefined
afterEach(() => wrapper?.unmount())

function mounted(look?: 'segmented' | 'tabs') {
  const value = ref<'exact' | 'similar'>('exact')
  const Host = defineComponent({
    setup: () => () =>
      h(Segmented<'exact' | 'similar'>, {
        modelValue: value.value,
        'onUpdate:modelValue': (next: 'exact' | 'similar') => (value.value = next),
        label: 'Duplicate type',
        look,
        items: [
          { value: 'exact', label: 'Exact copies', count: 1234 },
          { value: 'similar', label: 'Similar photos', count: 0 },
        ],
      }),
  })
  wrapper = mountInApp(Host)
  return { value, wrapper }
}

it('draws the choice as underlined tabs with formatted counts, keeping the radio group', async () => {
  const { value, wrapper } = mounted('tabs')
  const group = wrapper.get('[role="radiogroup"][aria-label="Duplicate type"]')
  expect(group.classes()).toContain('tabs')
  expect(group.classes()).not.toContain('segmented')
  const [exact, similar] = wrapper.findAll('[role="radio"]')
  expect(exact?.text()).toBe('Exact copies 1,234')
  expect(similar?.get('.count').text()).toBe('0')
  await group.trigger('keydown', { key: 'ArrowRight' })
  expect(value.value).toBe('similar')
  expect(similar?.attributes('aria-checked')).toBe('true')
})

it('keeps the sliding segmented look by default', () => {
  const { wrapper } = mounted()
  expect(wrapper.get('[role="radiogroup"]').classes()).toContain('segmented')
})
