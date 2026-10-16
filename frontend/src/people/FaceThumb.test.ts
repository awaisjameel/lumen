import { afterEach, expect, it } from 'vitest'
import { mountInApp } from '../shared/testing'
import { defineComponent, h, shallowRef } from 'vue'
import FaceThumb from './FaceThumb.vue'
import { faceFixture } from './testing'

let wrapper: ReturnType<typeof mountInApp> | undefined
afterEach(() => wrapper?.unmount())
it('shows a neutral fallback when a face preview fails and retries a replacement face', async () => {
  const face = shallowRef({ ...faceFixture, thumb: 1 })
  wrapper = mountInApp(
    defineComponent({ setup: () => () => h(FaceThumb, { face: face.value, size: 96, round: true }) }),
  )
  await wrapper.get('img').trigger('error')
  expect(wrapper.find('img').exists()).toBe(false)
  expect(wrapper.find('[title="Face preview unavailable"]').exists()).toBe(true)
  face.value = { ...faceFixture, id: 12, thumb: 2 }
  await wrapper.vm.$nextTick()
  expect(wrapper.find('img').exists()).toBe(true)
})
it('does not render invalid crop geometry or missing thumbnails as an image', () => {
  wrapper = mountInApp(FaceThumb, { props: { face: { ...faceFixture, thumb: 1, w: 0 }, size: 128 } })
  expect(wrapper.find('img').exists()).toBe(false)
})
