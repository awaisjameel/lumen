import { afterEach, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import { defineComponent, h, nextTick } from 'vue'
import UApp from '@nuxt/ui/components/App.vue'
import UToast from '@nuxt/ui/components/Toast.vue'
import ui from '@nuxt/ui/vue-plugin'
import { useToast } from '@nuxt/ui/composables'
import { createRouter, createMemoryHistory } from 'vue-router'
import { dismiss, notify, notificationToaster } from '../shared/notify'
import AppNotifications from './AppNotifications.vue'

let wrapper: VueWrapper | undefined
let clear = () => undefined
afterEach(() => {
  clear()
  wrapper?.unmount()
  wrapper = undefined
  vi.useRealTimers()
})
function mountToaster() {
  const Host = defineComponent({
    setup() {
      const toast = useToast()
      clear = () => {
        toast.clear()
        return undefined
      }
      return () => h(AppNotifications)
    },
  })
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/', component: { render: () => null } }],
  })
  wrapper = mount(() => h(UApp, { toaster: notificationToaster }, { default: () => h(Host) }), {
    attachTo: document.body,
    global: { plugins: [ui, router] },
  })
  return wrapper
}
it('uses a six-second polite toast, persistent assertive errors and readable action buttons', async () => {
  const w = mountToaster()
  notify({
    id: 'success',
    kind: 'success',
    title: 'Moved 12 items',
    actions: [{ label: 'Undo', run: () => undefined }],
  })
  await flushPromises()
  const success = w.findComponent(UToast)
  expect(success.props('duration')).toBe(6000)
  expect(success.props('type')).toBe('background')
  expect(success.props('actions')?.[0]).toMatchObject({ label: 'Undo', variant: 'ghost' })
  notify({ id: 'failure', kind: 'error', title: 'Remaining files were left where they are' })
  await flushPromises()
  const failure = w
    .findAllComponents(UToast)
    .find((t) => t.props('title') === 'Remaining files were left where they are')
  expect(failure?.props('duration')).toBe(0)
  expect(failure?.props('type')).toBe('foreground')
  dismiss('success')
  dismiss('failure')
})
it('pauses the timer for pointer and keyboard activity and closes once after the remaining time', async () => {
  vi.useFakeTimers()
  const w = mountToaster()
  const closed = vi.fn()
  notify({ id: 'timer', kind: 'info', title: 'Timed notice', closed })
  await nextTick()
  await nextTick()
  await nextTick()
  await vi.advanceTimersByTimeAsync(100)
  const viewport = document.querySelector('[data-slot="viewport"]')
  if (!viewport) throw new Error('Toast viewport was not mounted')
  await vi.advanceTimersByTimeAsync(2000)
  viewport.dispatchEvent(new Event('pointermove'))
  await vi.advanceTimersByTimeAsync(7000)
  expect(closed).not.toHaveBeenCalled()
  viewport.dispatchEvent(new Event('pointerleave'))
  viewport.dispatchEvent(new Event('focusin'))
  await vi.advanceTimersByTimeAsync(7000)
  expect(closed).not.toHaveBeenCalled()
  viewport.dispatchEvent(new FocusEvent('focusout', { relatedTarget: document.body }))
  await vi.advanceTimersByTimeAsync(4100)
  expect(closed).toHaveBeenCalledOnce()
  expect(w.findComponent(UToast).exists()).toBe(false)
})

it('retains persistent errors when more than five notices arrive', async () => {
  const w = mountToaster()
  const closed = vi.fn()
  for (let i = 0; i < 7; i++) notify({ id: `persistent-${i}`, kind: 'error', title: `Failure ${i}`, closed })
  await flushPromises()
  expect(w.findAllComponents(UToast)).toHaveLength(7)
  expect(closed).not.toHaveBeenCalled()
  for (let i = 0; i < 7; i++) dismiss(`persistent-${i}`)
  expect(closed).toHaveBeenCalledTimes(7)
})

it('replaces long-work progress in place and clears it on completion', async () => {
  const w = mountToaster()
  notify({ id: 'moving', kind: 'info', title: 'Moving files', work: { done: 320, total: 1284 } })
  await flushPromises()
  const progress = () => document.querySelector('[role="progressbar"][aria-label="Moving files"]')
  expect(progress()?.getAttribute('aria-valuenow')).toBe('320')
  notify({ id: 'moving', kind: 'info', title: 'Moving files', work: { done: 640, total: 1284 } })
  await flushPromises()
  expect(w.findAllComponents(UToast)).toHaveLength(1)
  expect(progress()?.getAttribute('aria-valuenow')).toBe('640')
  notify({ id: 'moving', kind: 'success', title: 'Moved all files' })
  await flushPromises()
  expect(progress()).toBeNull()
  expect(w.findComponent(UToast).props('duration')).toBe(6000)
  dismiss('moving')
})
