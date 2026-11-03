import { afterEach, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import type { Component } from 'vue'
import { defineComponent, h, nextTick, ref } from 'vue'
import UApp from '@nuxt/ui/components/App.vue'
import UToast from '@nuxt/ui/components/Toast.vue'
import UModal from '@nuxt/ui/components/Modal.vue'
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
function mountToaster(overlay?: Component) {
  const Host = defineComponent({
    setup() {
      const toast = useToast()
      clear = () => {
        toast.clear()
        return undefined
      }
      return () => h('div', [h(AppNotifications), overlay ? h(overlay) : null])
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
  expect(document.querySelector('[data-notification="success"]')).not.toBeNull()
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

it('retains recovery when Escape closes a portalled outcome dialog', async () => {
  vi.useFakeTimers()
  const closed = vi.fn()
  const opened = ref(false)
  const dialog = defineComponent(
    () => () =>
      opened.value ? h(UModal, { defaultOpen: true, title: 'Outcomes', description: 'Recovery outcomes' }) : null,
  )
  const w = mountToaster(dialog)
  notify({ id: 'recovery', kind: 'error', title: 'Retry available', closed })
  await flushPromises()
  const original = document.querySelector('[data-notification="recovery"]')
  opened.value = true // The recovery toast exists before its outcomes dialog opens.
  await flushPromises()
  expect(document.querySelector('[role="dialog"][data-state="open"]')).not.toBeNull()
  document.activeElement?.dispatchEvent(
    new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }),
  )
  await flushPromises()
  expect(document.querySelector('[role="dialog"][data-state="open"]')).toBeNull()
  await vi.advanceTimersByTimeAsync(300) // a delayed toast removal must not discard recovery
  expect(closed).not.toHaveBeenCalled()
  expect(document.querySelector('[data-notification="recovery"]')).toBe(original)
  expect(w.findAllComponents(UToast)).toHaveLength(1)
  dismiss('recovery')
  await flushPromises()
  expect(closed).toHaveBeenCalledOnce()
  await vi.advanceTimersByTimeAsync(300)
  expect(w.findAllComponents(UToast)).toHaveLength(0)
})

it('does not publish an invalid countdown when a live interactive notice completes', async () => {
  vi.useFakeTimers()
  mountToaster()
  const errors = vi.spyOn(console, 'error').mockImplementation(() => undefined)
  const actions = [{ label: 'View outcomes', dismiss: false, run: () => undefined }]
  notify({ id: 'inverse', kind: 'success', title: 'Saved', duration: 8000, actions })
  await flushPromises()
  notify({ id: 'inverse', kind: 'info', title: 'Undoing', duration: 0, actions, work: { done: 0, total: 500 } })
  await flushPromises()
  notify({ id: 'inverse', kind: 'success', title: 'Restored', duration: 6000, actions })
  await flushPromises()
  expect(errors).not.toHaveBeenCalled()
})
