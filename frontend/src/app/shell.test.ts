import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import type { Pinia } from 'pinia'
import { flushPromises } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import { h } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import type { Router } from 'vue-router'
import { engine } from '../bridge'
import type * as Bridge from '../bridge'
import { useLibrary } from '../library/store'
import { commandById, runCommand } from '../shared/commands'
import { dismiss, notify } from '../shared/notify'
import { usePreferences } from '../shared/preferences'
import { mountInApp } from '../shared/testing'
import AppNotifications from './AppNotifications.vue'
import AppOverlays from './AppOverlays.vue'
import AppShell from './AppShell.vue'
import { isActive, navEntries } from './nav'

vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  engine: {
    migratePreferences: vi.fn<typeof Bridge.engine.migratePreferences>(() =>
      Promise.resolve({ ...usePreferences().prefs }),
    ),
    setPreferences: vi.fn<typeof Bridge.engine.setPreferences>((p) => Promise.resolve(p)),
    blockPINConfigured: vi.fn<typeof Bridge.engine.blockPINConfigured>().mockResolvedValue(true),
  },
  onProgress: vi.fn<typeof Bridge.onProgress>(() => () => undefined),
  onLibraryChanged: vi.fn<typeof Bridge.onLibraryChanged>(() => () => undefined),
}))

// Functional components: a page, and hosts for the pieces under test.
const Blank = () => h('p', 'page')

let pinia: Pinia
let router: Router
let wrapper: VueWrapper | undefined
const realWidth = window.innerWidth

function setWidth(px: number) {
  Object.defineProperty(window, 'innerWidth', { configurable: true, value: px })
  window.dispatchEvent(new Event('resize'))
}

beforeEach(async () => {
  pinia = createPinia()
  setActivePinia(pinia)
  router = createRouter({
    history: createMemoryHistory(),
    routes: [
      ...navEntries.map((e) => ({ path: e.path, name: e.name, component: Blank })),
      { path: '/people/:id', name: 'person', component: Blank },
    ],
  })
  await router.push('/')
  vi.mocked(engine.setPreferences).mockClear()
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  setWidth(realWidth)
  document.body.innerHTML = ''
})

function mountShell() {
  wrapper = mountInApp(AppShell, {
    plugins: [pinia, router],
    slots: { default: () => h(Blank) },
    stubs: { IndexStatus: true },
  })
  return wrapper
}

const links = () => [...document.body.querySelectorAll('nav a')].map((a) => a.textContent.trim())

it('names every destination once, in one place', () => {
  expect(new Set(navEntries.map((e) => e.name)).size).toBe(navEntries.length)
  const people = navEntries.find((e) => e.name === 'people')
  const library = navEntries.find((e) => e.name === 'library')
  expect(people && isActive(people, '/people/3')).toBe(true)
  expect(people && isActive(people, '/people')).toBe(true)
  // The library is the root: it is not "under" every other page.
  expect(library && isActive(library, '/people')).toBe(false)
  expect(library && isActive(library, '/')).toBe(true)
})

it('shows the sidebar on a wide window, with Videos only while videos are indexed', async () => {
  setWidth(1400)
  const lib = useLibrary()
  mountShell()
  await flushPromises()
  expect(links()).toContain('Library')
  expect(links()).toContain('Settings')
  expect(links()).not.toContain('Videos')
  lib.settings = { ...lib.settings, videos: true }
  await flushPromises()
  expect(links()).toContain('Videos')
})

it('marks the section a deeper page belongs to as current', async () => {
  setWidth(1400)
  await router.push('/people/3')
  mountShell()
  await flushPromises()
  const current = [...document.body.querySelectorAll('nav a[aria-current="page"], nav a[data-active]')]
  expect(current.map((a) => a.textContent.trim())).toContain('People')
})

it('collapses and expands the sidebar as a saved preference, also from the keyboard command', async () => {
  setWidth(1400)
  const prefs = usePreferences()
  const shell = mountShell()
  await flushPromises()
  expect(shell.find('aside.sidebar').classes()).not.toContain('collapsed')

  expect(await runCommand('app.toggleSidebar')).toBe(true)
  await flushPromises()
  expect(prefs.prefs.sidebarCollapsed).toBe(true)
  expect(shell.find('aside.sidebar').classes()).toContain('collapsed')
  expect(engine.setPreferences).toHaveBeenLastCalledWith(expect.objectContaining({ sidebarCollapsed: true }))
  expect(commandById('app.toggleSidebar')?.keys).toBe('Ctrl+B')
})

it('resizes the sidebar from the keyboard within its limits', async () => {
  setWidth(1400)
  const prefs = usePreferences()
  await prefs.load()
  const shell = mountShell()
  await flushPromises()
  const handle = shell.get('[role="separator"]')
  const before = prefs.prefs.sidebarWidth
  await handle.trigger('keydown', { key: 'ArrowRight' })
  expect(prefs.prefs.sidebarWidth).toBe(before + 16)
  for (let i = 0; i < 40; i++) await handle.trigger('keydown', { key: 'ArrowRight' })
  expect(prefs.prefs.sidebarWidth).toBe(Number(handle.attributes('aria-valuemax')))
})

it('is a rail of icons at medium width and a drawer at narrow width', async () => {
  setWidth(1000)
  const shell = mountShell()
  await flushPromises()
  expect(shell.find('aside.sidebar').classes()).toContain('collapsed')
  expect(shell.find('[role="separator"]').exists()).toBe(false) // a rail is not resized

  setWidth(700)
  await flushPromises()
  expect(shell.find('aside.sidebar').exists()).toBe(false)
  const open = shell.get('header.narrow-bar button')
  expect(open.attributes('aria-label')).toBe('Open navigation')
  await open.trigger('click')
  await flushPromises()
  expect(document.body.querySelector('[role="dialog"]')?.textContent).toContain('Library')

  // Going somewhere puts the drawer away.
  await router.push('/people')
  await flushPromises()
  await vi.waitFor(() => expect(document.body.querySelector('[role="dialog"][data-state="open"]')).toBeNull())
})

it('offers navigation and its own commands only once there is a library', async () => {
  const lib = useLibrary()
  wrapper = mountInApp(AppOverlays, { plugins: [pinia, router] })
  await flushPromises()
  expect(await runCommand('go.people')).toBe(false)
  expect(await runCommand('app.commandPalette')).toBe(false)

  lib.ready = true
  lib.sources = [{ id: 1, path: 'C:\\Photos', enabled: true }]
  expect(await runCommand('go.people')).toBe(true)
  await flushPromises()
  expect(router.currentRoute.value.path).toBe('/people')

  expect(await runCommand('app.commandPalette')).toBe(true)
  // The palette is loaded when first asked for.
  const palette = () => document.body.querySelector('[role="dialog"]')?.textContent ?? ''
  await vi.waitFor(() => expect(palette()).toContain('Go to Library'))
  expect(palette()).toContain('Keyboard shortcuts')
})

it('lists every command that has a shortcut in the shortcut reference', async () => {
  const lib = useLibrary()
  lib.ready = true
  lib.sources = [{ id: 1, path: 'C:\\Photos', enabled: true }]
  wrapper = mountInApp(AppOverlays, { plugins: [pinia, router] })
  await flushPromises()
  expect(await runCommand('app.shortcuts')).toBe(true)
  const text = () => document.body.querySelector('[role="dialog"]')?.textContent ?? ''
  await vi.waitFor(() => expect(text()).toContain('Keyboard shortcuts'))
  expect(text()).toContain('Commands')
  expect(text()).toContain('Ctrl')
})

it('presents notifications as toasts, replacing one with the same id and telling when it closes', async () => {
  const Host = () => h(AppNotifications, null, { default: () => h(Blank) })
  // The toaster is UApp's: mount with it on, unlike mountInApp's default.
  const { mount } = await import('@vue/test-utils')
  const { default: UApp } = await import('@nuxt/ui/components/App.vue')
  const { default: ui } = await import('@nuxt/ui/vue-plugin')
  wrapper = mount(() => h(UApp, null, { default: () => h(Host) }), {
    attachTo: document.body,
    global: { plugins: [ui] },
  })
  const closed = vi.fn()
  const undo = vi.fn()
  notify({ id: 'trash.undo', kind: 'success', title: 'Moved 1 item to the Trash', closed })
  notify({
    id: 'trash.undo',
    kind: 'success',
    title: 'Moved 2 items to the Trash',
    actions: [{ label: 'Undo', run: undo }],
  })
  // The toasts on screen, by title (the screen reader's announcement of a
  // toast lingers in a hidden live region for a moment, so not the body's text).
  const titles = () => [...document.body.querySelectorAll('[data-slot="title"]')].map((el) => el.textContent.trim())
  await vi.waitFor(() => expect(titles()).toEqual(['Moved 2 items to the Trash']))
  expect(closed).not.toHaveBeenCalled() // replaced, not closed by the user

  const button = [...document.body.querySelectorAll('button')].find((b) => b.textContent.trim() === 'Undo')
  button?.click()
  expect(undo).toHaveBeenCalledTimes(1)

  const gone = vi.fn()
  notify({ id: 'note', kind: 'info', title: 'A note', duration: 0, closed: gone })
  await vi.waitFor(() => expect(titles()).toContain('A note'))
  dismiss('note')
  expect(gone).toHaveBeenCalledTimes(1)

  // A report action keeps its notice open, and an Undo offered immediately
  // after dismissing an older offer survives that toast's delayed removal.
  const report = vi.fn()
  notify({
    id: 'report',
    kind: 'info',
    title: 'Save a report',
    duration: 0,
    actions: [{ label: 'Save', dismiss: false, run: report }],
  })
  await vi.waitFor(() => expect(titles()).toContain('Save a report'))
  const save = [...document.body.querySelectorAll('button')].find((b) => b.textContent.trim() === 'Save')
  save?.click()
  expect(report).toHaveBeenCalledOnce()
  // Past the removal delay of a toast that closed: the notice is still open.
  await new Promise((resolve) => setTimeout(resolve, 300))
  const open = () =>
    [...document.body.querySelectorAll('li[data-state="open"] [data-slot="title"]')].map((el) => el.textContent.trim())
  expect(open()).toContain('Save a report')
  dismiss('report')
  notify({ id: 'report', kind: 'info', title: 'The newer report', duration: 0 })
  await new Promise((resolve) => setTimeout(resolve, 300))
  expect(titles()).toContain('The newer report')
  dismiss('report')

  // Replaced and dismissed in the same moment: the toast still goes.
  notify({ id: 'swap', kind: 'info', title: 'First', duration: 0 })
  await vi.waitFor(() => expect(open()).toContain('First'))
  notify({ id: 'swap', kind: 'info', title: 'Second', duration: 0 })
  dismiss('swap')
  await new Promise((resolve) => setTimeout(resolve, 300))
  expect(titles()).not.toContain('Second')
})
