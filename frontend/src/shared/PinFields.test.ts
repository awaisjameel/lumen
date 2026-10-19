import { afterEach, expect, it } from 'vitest'
import { defineComponent, ref } from 'vue'
import type { VueWrapper } from '@vue/test-utils'
import { PIN_MAX_LEN, PIN_MIN_LEN, PIN_PATTERN } from '../bridge'
import { t } from './i18n'
import { mountInApp } from './testing'
import PinFields from './PinFields.vue'

let wrapper: VueWrapper | undefined
afterEach(() => {
  wrapper?.unmount()
})
function mountPIN(creating = false) {
  wrapper = mountInApp(
    defineComponent({
      components: { PinFields },
      setup: () => ({ pin: ref(''), creating }),
      template: '<PinFields v-model:pin="pin" :creating="creating" keypad />',
    }),
  )
  return wrapper
}
it('uses one real numeric input over the dots and caps entry at the engine limit', async () => {
  const view = mountPIN()
  const input = view.get('input')
  expect(view.findAll('input')).toHaveLength(1)
  expect(view.get('[data-pin-wrap]').find('input').exists()).toBe(true)
  expect(input.attributes()).toMatchObject({
    type: 'password',
    inputmode: 'numeric',
    pattern: PIN_PATTERN,
    minlength: String(PIN_MIN_LEN),
    maxlength: String(PIN_MAX_LEN),
  })
  await input.setValue('12a3456789012345')
  expect(input.element.value).toBe('123456789012')
  expect(view.findAll('[aria-hidden="true"] span')).toHaveLength(PIN_MAX_LEN)
})
it('edits that same input from the pad, clear and delete controls', async () => {
  const view = mountPIN()
  await view.get('button[aria-label="4"]').trigger('click')
  await view.get('button[aria-label="0"]').trigger('click')
  expect(view.get('input').element.value).toBe('40')
  await view.get(`button[aria-label="${t('pin.delete')}"]`).trigger('click')
  expect(view.get('input').element.value).toBe('4')
  await view.get(`button[aria-label="${t('pin.clear')}"]`).trigger('click')
  expect(view.get('input').element.value).toBe('')
  expect(document.activeElement).toBe(view.get('input').element)
})
it('links an invalid input to the visible reason and disables all entry while checking', () => {
  wrapper = mountInApp(PinFields, {
    props: { creating: false, pin: '', error: 'Wrong PIN', disabled: true, keypad: true },
  })
  const input = wrapper.get('input')
  expect(input.attributes('aria-invalid')).toBe('true')
  expect(wrapper.get(`[id="${input.attributes('aria-describedby')}"]`).text()).toBe('Wrong PIN')
  expect(input.attributes('disabled')).toBeDefined()
  expect(wrapper.findAll('button').every((button) => button.attributes('disabled') !== undefined)).toBe(true)
})
it('labels both creation fields with distinct ids and password-manager hints', () => {
  const view = mountPIN(true)
  const fields = view.findAll('input')
  expect(fields).toHaveLength(2)
  expect(fields[0]?.attributes('id')).not.toBe(fields[1]?.attributes('id'))
  expect(fields.every((input) => input.attributes('autocomplete') === 'new-password')).toBe(true)
})
