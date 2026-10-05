import { expect, it } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import UApp from '@nuxt/ui/components/App.vue'
import { defineComponent, h } from 'vue'
import { useFullscreenPortal } from './overlay'
import { mountInApp } from './testing'
import ConfirmDialog from './ConfirmDialog.vue'
import { confirm, question } from './confirm'

it('keeps global confirmations inside fullscreen and returns their portal to the body on exit', async () => {
  const fullscreen = document.createElement('div')
  document.body.append(fullscreen)
  let current: Element | null = fullscreen
  const property = Object.getOwnPropertyDescriptor(document, 'fullscreenElement')
  Object.defineProperty(document, 'fullscreenElement', { configurable: true, get: () => current })
  const wrapper = mountInApp(
    defineComponent({
      setup() {
        const portal = useFullscreenPortal()
        return () => h(UApp, { portal: portal.value, toaster: null }, { default: () => h(ConfirmDialog) })
      },
    }),
  )
  try {
    const answer = confirm('Export', 'Make a new copy?', 'Export')
    await flushPromises()
    const dialog = fullscreen.querySelector('[role="dialog"]')
    expect(dialog).not.toBeNull()
    current = null
    document.dispatchEvent(new Event('fullscreenchange'))
    await flushPromises()
    expect(fullscreen.querySelector('[role="dialog"]')).toBeNull()
    expect(document.body.contains(dialog)).toBe(true)
    question.value?.answer(false)
    await expect(answer).resolves.toBe(false)
  } finally {
    question.value?.answer(false)
    wrapper.unmount()
    fullscreen.remove()
    if (property) Object.defineProperty(document, 'fullscreenElement', property)
    else Reflect.deleteProperty(document, 'fullscreenElement')
  }
})
