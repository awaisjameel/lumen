import { expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
import { mount } from '@vue/test-utils'
import TimelineScrubber from './TimelineScrubber.vue'
import { buildLayout } from './layout'

it('offers year buttons with a current marker and seeks on keyboard activation', async () => {
  vi.spyOn(HTMLElement.prototype, 'clientHeight', 'get').mockReturnValue(500)
  const buckets = [
    { day: '2026-10-01', count: 100 },
    { day: '2025-10-01', count: 100 },
  ]
  const layout = buildLayout(buckets, 800, 160)
  const wrapper = mount(TimelineScrubber, { props: { buckets, layout, scrollTop: 0, viewportH: 500 } })
  await nextTick()
  const current = wrapper.get('button[aria-current="date"]')
  expect(current.text()).toBe('2026')
  const older = wrapper.get('button[aria-label="Jump to 2025"]')
  await older.trigger('click')
  expect(wrapper.emitted('seek')?.[0]?.[0]).toBeGreaterThan(0)
  expect(wrapper.find('[role="slider"]').exists()).toBe(false)
  wrapper.unmount()
})

it('does not emit an invalid seek before the track has a height', async () => {
  const buckets = [{ day: '2026-10-01', count: 100 }]
  const wrapper = mount(TimelineScrubber, {
    props: { buckets, layout: buildLayout(buckets, 800, 160), scrollTop: 0, viewportH: 500 },
  })
  wrapper
    .get('[role="navigation"]')
    .element.dispatchEvent(
      Object.assign(new MouseEvent('pointerdown', { bubbles: true, clientY: 20 }), { pointerId: 1 }),
    )
  await nextTick()
  expect(wrapper.emitted('seek')).toBeUndefined()
  wrapper.unmount()
})
