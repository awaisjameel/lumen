import { expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import LibraryThumb from './LibraryThumb.vue'

it('offers a retry for an unavailable thumbnail and resets for a reused tile', async () => {
  const wrapper = mount(LibraryThumb, { props: { id: 1, thumb: 2 } })
  await wrapper.get('img').trigger('error')
  expect(wrapper.get('[role="status"]').text()).toContain('Thumbnail unavailable')
  await wrapper.get('button').trigger('click')
  expect(wrapper.get('img').attributes('src')).toContain('retry=1')
  await wrapper.get('img').trigger('error')
  await wrapper.setProps({ id: 3 })
  expect(wrapper.get('img').attributes('src')).toBe('/media/thumb/3/256?v=2')
  wrapper.unmount()
})
