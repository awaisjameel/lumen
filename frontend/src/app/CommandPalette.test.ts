import { afterEach, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import { defineComponent, h, ref } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import { mountInApp, recordNotifications } from '../shared/testing'
import { registerCommands, setShortcutOverrides } from '../shared/commands'
import type { Command } from '../shared/commands'
import CommandPalette from './CommandPalette.vue'

let wrapper: VueWrapper | undefined
let remove: (() => void) | undefined
const opened = ref(true)
const router = createRouter({
  history: createMemoryHistory(),
  routes: [{ path: '/', component: { template: '<div />' } }],
})
afterEach(() => {
  wrapper?.unmount()
  remove?.()
  setShortcutOverrides({})
})
async function start(commands: Command[]) {
  remove = registerCommands(commands)
  opened.value = true
  const Host = defineComponent({
    setup: () => () =>
      h(CommandPalette, {
        open: opened.value,
        'onUpdate:open': (value: boolean) => {
          opened.value = value
        },
      }),
  })
  wrapper = mountInApp(Host, { plugins: [router] })
  await flushPromises()
}
async function search(text: string) {
  const input = document.querySelector<HTMLInputElement>('[role="dialog"] input')
  if (!input) throw new Error('missing palette input')
  input.value = text
  input.dispatchEvent(new Event('input', { bubbles: true }))
  await new Promise((resolve) => setTimeout(resolve, 30))
  await flushPromises()
  return input
}
function row(text: string) {
  const element = [...document.querySelectorAll<HTMLElement>('[role="option"]')].find((el) =>
    el.textContent.includes(text),
  )
  if (!element) throw new Error(`missing command: ${text}`)
  return element
}

it('matches and highlights subsequences while preserving group order and command shortcuts', async () => {
  setShortcutOverrides({ 'go.people': 'Ctrl+G' })
  await start([
    { id: 'go.people', group: 'navigate', title: () => 'Go to People', icon: 'people', run: vi.fn() },
    { id: 'search.focus', group: 'library', title: () => 'Search your photos', run: vi.fn() },
  ])
  await search('gtp')
  expect(row('Go to People').querySelectorAll('mark')).toHaveLength(3)
  expect(row('Go to People').textContent).toContain('CtrlG')
  expect(document.querySelectorAll('[role="option"]')).toHaveLength(1)
})

it('omits unavailable actions and explains disabled commands without allowing execution', async () => {
  const run = vi.fn()
  const enabled = ref(false)
  await start([
    { id: 'hidden', group: 'media', title: () => 'Open selected', available: () => false, run },
    {
      id: 'busy',
      group: 'media',
      title: () => 'Favourite',
      enabled: () => enabled.value,
      reason: () => 'Saving favourite… Wait for the current change to finish.',
      run,
    },
  ])
  expect(document.body.textContent).not.toContain('Open selected')
  expect(row('Favourite').hasAttribute('disabled')).toBe(true)
  expect(row('Favourite').textContent).toContain('Saving favourite…')
  row('Favourite').click()
  await flushPromises()
  expect(run).not.toHaveBeenCalled()
  enabled.value = true
  await flushPromises()
  expect(row('Favourite').hasAttribute('disabled')).toBe(false)
})

it('recovers from an empty match without closing the palette', async () => {
  await start([{ id: 'go.people', group: 'navigate', title: () => 'Go to People', run: vi.fn() }])
  await search('zzzz')
  expect(document.body.textContent).toContain('No commands match')
  const reset = [...document.querySelectorAll('button')].find((el) => el.textContent === 'Show all commands')
  reset?.click()
  await flushPromises()
  expect(row('Go to People')).toBeDefined()
})

it('runs a command after dismissal and offers it as a current-session recent on reopening', async () => {
  const run = vi.fn()
  await start([{ id: 'go.people', group: 'navigate', title: () => 'Go to People', run }])
  row('Go to People').click()
  await vi.waitFor(() => expect(run).toHaveBeenCalledTimes(1))
  opened.value = true
  await flushPromises()
  expect(document.body.textContent).toContain('Recent commands')
  expect(document.querySelectorAll('[role="option"]')).toHaveLength(2)
})

it('reports a failed action through the shared notification channel', async () => {
  const notices = recordNotifications()
  try {
    await start([
      { id: 'fail', group: 'media', title: () => 'Favourite', run: () => Promise.reject(new Error('save failed')) },
    ])
    row('Favourite').click()
    await vi.waitFor(() => expect(notices.shown).toHaveLength(1))
    expect(notices.shown[0]?.title).toBeTruthy()
  } finally {
    notices.stop()
  }
})

it('moves the active command with arrows and executes it with Enter from the input', async () => {
  const first = vi.fn(),
    second = vi.fn()
  await start([
    { id: 'first', group: 'view', title: () => 'First action', run: first },
    { id: 'second', group: 'view', title: () => 'Second action', run: second },
  ])
  const input = await search('action')
  input.focus()
  input.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true, cancelable: true }))
  await flushPromises()
  input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }))
  await vi.waitFor(() => expect(first.mock.calls.length + second.mock.calls.length).toBe(1))
  expect(opened.value).toBe(false)
})

it('closes with Escape and restores the focus that opened the modal', async () => {
  const trigger = document.createElement('button')
  document.body.append(trigger)
  trigger.focus()
  try {
    await start([{ id: 'go.people', group: 'navigate', title: () => 'Go to People', run: vi.fn() }])
    const input = await search('')
    input.focus()
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await vi.waitFor(() => expect(opened.value).toBe(false))
    await vi.waitFor(() => expect(document.activeElement).toBe(trigger))
  } finally {
    trigger.remove()
  }
})

it('opens the shortcut reference with its actual keys from inside the palette', async () => {
  setShortcutOverrides({ 'app.shortcuts': 'Ctrl+G' })
  const run = vi.fn()
  await start([{ id: 'app.shortcuts', group: 'app', title: () => 'Keyboard shortcuts', keys: '?', run }])
  const input = await search('')
  input.dispatchEvent(new KeyboardEvent('keydown', { key: '?', bubbles: true, cancelable: true }))
  await flushPromises()
  expect(run).not.toHaveBeenCalled()
  input.dispatchEvent(new KeyboardEvent('keydown', { key: 'g', ctrlKey: true, bubbles: true, cancelable: true }))
  await vi.waitFor(() => expect(run).toHaveBeenCalledTimes(1))
  expect(opened.value).toBe(false)
})
