import { afterEach, expect, it } from 'vitest'
import type { VueWrapper } from '@vue/test-utils'
import WorkProgress from './WorkProgress.vue'
import { mountInApp } from './testing'

let wrapper: VueWrapper | undefined
afterEach(() => wrapper?.unmount())
it('announces progress, preserves the bar values and allows activity styling', () => {
  wrapper = mountInApp(WorkProgress, {
    props: { label: 'Moving files…', detail: '40 of 100', done: 40, total: 100, activity: true },
  })
  const bar = wrapper.get('[role="progressbar"]')
  expect(bar.attributes('aria-label')).toBe('Moving files…')
  expect(bar.attributes('aria-valuenow')).toBe('40')
  expect(bar.attributes('aria-valuemax')).toBe('100')
  expect(wrapper.get('[role="status"]').text()).toContain('40 of 100')
})
it('keeps waiting work indeterminate and describes why it waits', () => {
  wrapper = mountInApp(WorkProgress, { props: { label: 'Waiting for mains power', indeterminate: true } })
  expect(wrapper.get('[role="progressbar"]').attributes('aria-valuenow')).toBeUndefined()
  expect(wrapper.get('[role="status"]').text()).toBe('Waiting for mains power')
})
