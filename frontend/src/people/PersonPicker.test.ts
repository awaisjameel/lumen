import { afterEach, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { MAX_NAME_LEN } from '../bridge'
import type * as Bridge from '../bridge'
import PersonPicker from './PersonPicker.vue'

vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  engine: { suggestPeople: vi.fn().mockResolvedValue([]) },
}))

let wrapper: ReturnType<typeof mount> | undefined
afterEach(() => wrapper?.unmount())

it('allows the engine emoji limit and rejects longer typed names inline', async () => {
  wrapper = mount(PersonPicker, { props: { placeholder: 'Name' } })
  expect(wrapper.get('input').attributes('maxlength')).toBeUndefined()
  await wrapper.get('input').setValue('😀'.repeat(MAX_NAME_LEN + 1))
  await wrapper.get('input').trigger('keydown', { key: 'Enter' })
  expect(wrapper.emitted('pick')).toBeUndefined()
  expect(wrapper.get('[role="alert"]').text()).toContain('80')
  await wrapper.get('input').setValue(` ${'😀'.repeat(MAX_NAME_LEN)} `)
  await wrapper.get('input').trigger('keydown', { key: 'Enter' })
  expect(wrapper.emitted('pick')).toEqual([['😀'.repeat(MAX_NAME_LEN)]])
})
