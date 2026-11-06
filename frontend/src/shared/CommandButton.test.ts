import { afterEach, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import { defineComponent, h, ref } from 'vue'
import type { VNode } from 'vue'
import CommandButton from './CommandButton.vue'
import IconButton from './IconButton.vue'
import { registerCommands, runCommand, setShortcutOverrides } from './commands'
import { deferred, mountInApp } from './testing'

function host(render: () => VNode) {
  return defineComponent({ setup: () => render })
}
let wrapper: VueWrapper | undefined
const dispose: (() => void)[] = []
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  for (const remove of dispose.splice(0)) remove()
  setShortcutOverrides({})
})
it('shares pending admission, title and icon across worded and icon controls', async () => {
  const waiting = deferred<undefined>()
  const run = vi.fn(() => waiting.promise)
  dispose.push(registerCommands([{ id: 'about.report', run }]))
  const Host = host(() =>
    h('div', [h(CommandButton, { command: 'about.report' }), h(IconButton, { command: 'about.report' })]),
  )
  wrapper = mountInApp(Host)
  expect(wrapper.findAll('button')).toHaveLength(2)
  expect(wrapper.get('button').text()).toBe('Save a problem report…')
  expect(wrapper.findAll('button')[1]?.attributes('aria-label')).toBe('Save a problem report…')
  await wrapper.get('button').trigger('click')
  await flushPromises()
  expect(wrapper.findAll('button').every((button) => button.attributes('disabled') !== undefined)).toBe(true)
  expect(await runCommand('about.report')).toBe(false)
  expect(run).toHaveBeenCalledOnce()
  waiting.resolve(undefined)
  await flushPromises()
  expect(wrapper.findAll('button').every((button) => button.attributes('disabled') === undefined)).toBe(true)
})
it('does not run an absent command or override an explicitly disabled control', async () => {
  const run = vi.fn()
  const disabled = ref(false)
  const Host = host(() => h(CommandButton, { command: 'about.report', disabled: disabled.value }))
  wrapper = mountInApp(Host)
  expect(wrapper.get('button').attributes('disabled')).toBeDefined()
  dispose.push(registerCommands([{ id: 'about.report', run }]))
  disabled.value = true
  await flushPromises()
  expect(wrapper.get('button').attributes('disabled')).toBeDefined()
  await wrapper.get('button').trigger('click')
  expect(run).not.toHaveBeenCalled()
})
