import { afterEach, beforeEach, expect, it } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { enableAutoUnmount, DOMWrapper, flushPromises } from '@vue/test-utils'
import USelect from '@nuxt/ui/components/Select.vue'
import { mountInApp, recordNotifications } from '../../shared/testing'
import StatesShowcase from './StatesShowcase.vue'

enableAutoUnmount(afterEach)
beforeEach(() => setActivePinia(createPinia()))
async function show() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/', component: { render: () => null } }],
  })
  await router.push('/')
  return mountInApp(StatesShowcase, { plugins: [router] })
}
it('previews failure details and retry through the actual activity popover', async () => {
  const w = await show()
  w.findAllComponents(USelect)[0]?.vm.$emit('update:modelValue', 'failure-error')
  await flushPromises()
  await w.findAll('[data-activity-trigger]')[0]?.trigger('click')
  await flushPromises()
  const body = new DOMWrapper(document.body)
  expect(body.get('[data-activity-details] [role="alert"]').text()).toContain(
    'Lumen couldn’t read the failure details.',
  )
  await body
    .get('[data-activity-details]')
    .findAll('button')
    .find((b) => b.text() === 'Try again')
    ?.trigger('click')
  await flushPromises()
  expect(body.get('[data-activity-details]').text()).toContain('Damaged, or not really a photo or video')
  expect(body.get('[data-activity-details]').text()).toContain('Locked by another app, or not allowed')
})
it('validates the inline rename example and offers Undo feedback', async () => {
  const notices = recordNotifications()
  try {
    const w = await show()
    await w.get('#showcase-album').setValue('')
    await w.get('form').trigger('submit')
    expect(w.get('#showcase-album').attributes('aria-invalid')).toBe('true')
    expect(w.get('#showcase-album-hint').text()).toBe('Enter an album name.')
    await w.get('#showcase-album').setValue('Winter 2026')
    await w.get('form').trigger('submit')
    expect(notices.find('showcase.rename')?.title).toBe('Album renamed to “Winter 2026”.')
    await w
      .findAll('button')
      .find((b) => b.text() === 'Toast with Undo')
      ?.trigger('click')
    notices.find('showcase.undo')?.actions?.[0]?.run()
    expect(notices.find('showcase.undo')?.title).toBe('Restored 12 items to your library.')
  } finally {
    notices.stop()
  }
})
