import { expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
import { mount } from '@vue/test-utils'
import VideoThumb from './VideoThumb.vue'

it('previews silently under the pointer and restores the thumbnail on leave', async () => {
  const wrapper = mount(VideoThumb, {
    props: { item: { id: 1, w: 1920, h: 1080, t: 1, v: true }, cell: 160 },
    attachTo: document.body,
  })
  vi.spyOn(wrapper.element, 'getBoundingClientRect').mockReturnValue({
    x: 0,
    y: 0,
    left: 0,
    top: 0,
    right: 160,
    bottom: 160,
    width: 160,
    height: 160,
    toJSON: () => ({}),
  })
  wrapper.element.dispatchEvent(
    Object.assign(new MouseEvent('pointermove', { bubbles: true, clientX: 80 }), { pointerType: 'mouse' }),
  )
  await nextTick()
  expect(wrapper.find('img').isVisible()).toBe(false)
  expect(wrapper.find('video').exists()).toBe(false)
  await wrapper.trigger('pointerleave')
  expect(wrapper.get('img').isVisible()).toBe(true)
  wrapper.element.dispatchEvent(
    Object.assign(new MouseEvent('pointermove', { bubbles: true, clientX: 80 }), { pointerType: 'touch' }),
  )
  await nextTick()
  expect(wrapper.get('img').isVisible()).toBe(true)
  wrapper.unmount()
})
