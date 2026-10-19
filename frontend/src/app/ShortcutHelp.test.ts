import { afterEach, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { mountInApp } from '../shared/testing'
import { registerCommands, setShortcutOverrides } from '../shared/commands'
import { shortcutCatalogue } from '../shared/shortcuts'
import ShortcutHelp from './ShortcutHelp.vue'

let wrapper: VueWrapper | undefined
let remove: (() => void) | undefined
afterEach(() => {
  wrapper?.unmount()
  remove?.()
  setShortcutOverrides({})
})
const router = createRouter({
  history: createMemoryHistory(),
  routes: [{ path: '/settings', component: { template: '<div />' } }],
})
async function start() {
  wrapper = mountInApp(ShortcutHelp, { props: { open: true }, plugins: [router] })
  await flushPromises()
}
function button(text: string) {
  const element = [...document.querySelectorAll('button')].find((el) => el.textContent.trim() === text)
  if (!element) throw new Error(`missing button: ${text}`)
  return element
}
async function search(query: string) {
  const input = document.querySelector<HTMLInputElement>('[aria-label="Search keyboard shortcuts"]')
  if (!input) throw new Error('missing search')
  input.value = query
  input.dispatchEvent(new Event('input', { bubbles: true }))
  await flushPromises()
}

it('lists every catalogue command even while its owner is unmounted, with overrides and locked Escape', async () => {
  setShortcutOverrides({ 'viewer.rotate': 'R', 'media.favourite': '' })
  await start()
  for (const command of shortcutCatalogue)
    expect(document.querySelector(`[data-shortcut="${command.id}"]`)).not.toBeNull()
  expect(document.querySelector('[data-shortcut="viewer.rotate"]')?.textContent).toContain('R')
  expect(document.querySelector('[data-shortcut="media.favourite"]')?.textContent).toContain('No shortcut')
  const close = document.querySelector('[data-shortcut="viewer.close"]')
  expect(close?.textContent).toContain('Always Escape')
  expect(close?.querySelector('button')).toBeNull()
  expect(document.body.textContent).toContain('While video has focus')
  expect(document.body.textContent).toContain('Uses the skip interval in Settings')
})

it('removes the alternative when Search is reassigned and filters with empty recovery', async () => {
  setShortcutOverrides({ 'search.focus': 'Ctrl+G' })
  await start()
  expect(document.querySelector('[data-shortcut="search.focus-/"]')).toBeNull()
  await search('viewer')
  expect(document.body.textContent).toContain('In the viewer')
  expect(document.querySelector('[data-shortcut="search.focus"]')).toBeNull()
  await search('zzzz')
  expect(document.body.textContent).toContain('No matching shortcuts')
  button('Show all shortcuts').click()
  await flushPromises()
  expect(document.querySelector('[data-shortcut="search.focus"]')).not.toBeNull()
})

it('hands shortcut changes to the real Settings accessibility category', async () => {
  await start()
  button('Customise shortcuts').click()
  await flushPromises()
  expect(router.currentRoute.value.fullPath).toBe('/settings?section=accessibility')
  expect(wrapper?.findComponent(ShortcutHelp).emitted('update:open')).toEqual([[false]])
})

it('returns to Commands through its registry action', async () => {
  const run = vi.fn()
  remove = registerCommands([{ id: 'app.commandPalette', group: 'app', title: () => 'Commands', run }])
  await start()
  button('Back to Commands').click()
  await vi.waitFor(() => expect(run).toHaveBeenCalledTimes(1))
})
