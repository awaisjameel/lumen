import { beforeEach, expect, it, vi } from 'vitest'
import { createPinia } from 'pinia'
import { flushPromises } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { reactive } from 'vue'
import { engine } from '../bridge'
import type * as Bridge from '../bridge'
import { mountInApp, recordNotifications } from '../shared/testing'
import CollectionHead from './CollectionHead.vue'

vi.mock('../bridge', async (original) => ({
  ...(await original<typeof Bridge>()),
  engine: { setEventName: vi.fn(), setEventPinned: vi.fn(), things: vi.fn(), albums: vi.fn() },
}))
beforeEach(() => {
  vi.mocked(engine.setEventName).mockReset().mockResolvedValue(undefined)
  vi.mocked(engine.setEventPinned).mockReset().mockResolvedValue(undefined)
})
async function mountHead() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/albums', name: 'albums', component: { template: '<div />' } },
      { path: '/places', name: 'places', component: { template: '<div />' } },
    ],
  })
  await router.push('/albums')
  const props = reactive({
    kind: 'event' as const,
    id: 1,
    name: 'Weekend',
    count: 248,
    days: ['2026-10-04', '2026-10-03'],
  })
  const wrapper = mountInApp(CollectionHead, { props, plugins: [createPinia(), router] })
  return { wrapper, props }
}
it('renames inline with Enter and keeps Undo bound to the original event', async () => {
  const notices = recordNotifications()
  const { wrapper, props } = await mountHead()
  await wrapper
    .findAll('button')
    .find((b) => b.text() === 'Rename…')
    ?.trigger('click')
  await wrapper.get('[aria-label="Collection name"]').setValue('  At the sea  ')
  await wrapper.get('form').trigger('submit')
  await flushPromises()
  expect(engine.setEventName).toHaveBeenCalledExactlyOnceWith(1, 'At the sea')
  props.id = 2
  notices.find('collection.name')?.actions?.[0]?.run()
  await flushPromises()
  expect(engine.setEventName).toHaveBeenLastCalledWith(1, 'Weekend')
  wrapper.unmount()
  notices.stop()
})
it('keeps an invalid name and explains the limit without saving', async () => {
  const { wrapper } = await mountHead()
  await wrapper
    .findAll('button')
    .find((b) => b.text() === 'Rename…')
    ?.trigger('click')
  await wrapper.get('input').setValue('x'.repeat(81))
  await wrapper.get('form').trigger('submit')
  expect(engine.setEventName).not.toHaveBeenCalled()
  expect(wrapper.get('[role="alert"]').text()).toContain('1 to 80')
  wrapper.unmount()
})
it('allows a blank event name to restore the generated name and Escape abandons editing', async () => {
  const { wrapper } = await mountHead()
  await wrapper
    .findAll('button')
    .find((b) => b.text() === 'Rename…')
    ?.trigger('click')
  await wrapper.get('input').setValue(' ')
  await wrapper.get('form').trigger('submit')
  await flushPromises()
  expect(engine.setEventName).toHaveBeenCalledWith(1, '')
  await wrapper
    .findAll('button')
    .find((b) => b.text() === 'Rename…')
    ?.trigger('click')
  await wrapper.get('form').trigger('keydown', { key: 'Escape' })
  expect(wrapper.find('input').exists()).toBe(false)
  wrapper.unmount()
})
it('retains a failed rename draft for retry', async () => {
  vi.mocked(engine.setEventName).mockRejectedValueOnce(new Error('read only'))
  const { wrapper } = await mountHead()
  await wrapper
    .findAll('button')
    .find((b) => b.text() === 'Rename…')
    ?.trigger('click')
  await wrapper.get('input').setValue('Retained')
  await wrapper.get('form').trigger('submit')
  await flushPromises()
  expect(wrapper.get('[role="alert"]').text()).toContain('read only')
  expect(wrapper.get('input').element.value).toBe('Retained')
  wrapper.unmount()
})
