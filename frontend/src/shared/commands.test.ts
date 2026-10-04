import { afterEach, expect, it, vi } from 'vitest'
import { effectScope, nextTick, ref, shallowRef } from 'vue'
import {
  availableCommands,
  commandById,
  commands,
  dispatchKey,
  isEnabled,
  keyCaps,
  onGlobalKey,
  registerCommands,
  runCommand,
  useCommands,
  useItemCommands,
} from './commands'
import type { Command } from './commands'

let undo: (() => void)[] = []
function register(cmds: Command[]) {
  undo.push(registerCommands(cmds))
}
afterEach(() => {
  for (const u of undo) u()
  undo = []
  document.body.innerHTML = ''
})

function key(init: KeyboardEventInit, target: EventTarget = document.body): KeyboardEvent {
  const e = new KeyboardEvent('keydown', { ...init, bubbles: true, cancelable: true })
  target.dispatchEvent(e)
  return e
}

it('offers a command only while it is available, and runs it only while enabled', async () => {
  let selected = false
  let busy = false
  const run = vi.fn()
  register([
    {
      id: 'media.favourite',
      group: 'media',
      title: () => 'Favourite',
      available: () => selected,
      enabled: () => !busy,
      run,
    },
  ])
  expect(commands.value.map((c) => c.id)).toEqual(['media.favourite'])
  expect(availableCommands.value).toEqual([])
  expect(await runCommand('media.favourite')).toBe(false)

  selected = true
  busy = true
  const c = commandById('media.favourite')
  expect(c && isEnabled(c)).toBe(false)
  expect(await runCommand('media.favourite')).toBe(false)

  busy = false
  expect(await runCommand('media.favourite')).toBe(true)
  expect(run).toHaveBeenCalledTimes(1)
})

it('removes a command when its owner goes, and lets a remounted owner replace its own', () => {
  const scope = effectScope()
  scope.run(() => useCommands([{ id: 'view.details', group: 'view', title: () => 'Details', run: vi.fn() }]))
  const replacement = vi.fn()
  register([{ id: 'view.details', group: 'view', title: () => 'Details', run: replacement }])
  expect(commands.value.filter((c) => c.id === 'view.details')).toHaveLength(1)
  scope.stop() // the first owner's disposal must not remove the newer registration
  expect(commandById('view.details')?.run).toBe(replacement)
})

it('runs a shortcut only in its own zone, and spends the event when it does', () => {
  const favourite = vi.fn()
  const palette = vi.fn()
  register([
    { id: 'media.favourite', group: 'media', title: () => 'Favourite', keys: 'F', zone: 'grid', run: favourite },
    { id: 'app.palette', group: 'app', title: () => 'Commands', keys: 'Ctrl+K', run: palette },
  ])
  const inGlobal = key({ key: 'f' })
  expect(dispatchKey(inGlobal, 'global')).toBe(false)
  expect(inGlobal.defaultPrevented).toBe(false)

  const inGrid = key({ key: 'f' })
  expect(dispatchKey(inGrid, 'grid')).toBe(true)
  expect(inGrid.defaultPrevented).toBe(true)
  expect(favourite).toHaveBeenCalledTimes(1)

  // A plain K is not Ctrl+K, and Ctrl+F is not F.
  expect(dispatchKey(key({ key: 'k' }), 'global')).toBe(false)
  expect(dispatchKey(key({ key: 'f', ctrlKey: true }), 'grid')).toBe(false)
  expect(dispatchKey(key({ key: 'k', ctrlKey: true }), 'global')).toBe(true)
  expect(palette).toHaveBeenCalledTimes(1)
})

it('takes a second key for the same command, and a character typed with Shift', () => {
  const search = vi.fn()
  const help = vi.fn()
  register([
    { id: 'search.focus', group: 'library', title: () => 'Search', keys: 'Ctrl+F', altKeys: ['/'], run: search },
    { id: 'app.shortcuts', group: 'app', title: () => 'Shortcuts', keys: '?', run: help },
  ])
  expect(dispatchKey(key({ key: '/' }), 'global')).toBe(true)
  expect(dispatchKey(key({ key: 'F', ctrlKey: true }), 'global')).toBe(true)
  expect(search).toHaveBeenCalledTimes(2)
  expect(dispatchKey(key({ key: '?', shiftKey: true }), 'global')).toBe(true)
  expect(help).toHaveBeenCalledTimes(1)
})

it('does not run a shortcut whose command cannot run now', () => {
  const run = vi.fn()
  register([{ id: 'media.trash', group: 'media', title: () => 'Trash', keys: 'Delete', enabled: () => false, run }])
  const e = key({ key: 'Delete' })
  expect(dispatchKey(e, 'global')).toBe(false)
  expect(e.defaultPrevented).toBe(false)
  expect(run).not.toHaveBeenCalled()
})

it('leaves single keys to a text field, but lets a modified chord through', () => {
  const help = vi.fn()
  const palette = vi.fn()
  register([
    { id: 'app.shortcuts', group: 'app', title: () => 'Shortcuts', keys: '?', run: help },
    { id: 'app.palette', group: 'app', title: () => 'Commands', keys: 'Ctrl+K', run: palette },
  ])
  const input = document.createElement('input')
  document.body.append(input)
  input.addEventListener('keydown', onGlobalKey)
  key({ key: '?' }, input)
  expect(help).not.toHaveBeenCalled()
  key({ key: 'k', ctrlKey: true }, input)
  expect(palette).toHaveBeenCalledTimes(1)
})

it('suspends page shortcuts while an overlay owns the keyboard', () => {
  const palette = vi.fn()
  register([{ id: 'app.palette', group: 'app', title: () => 'Commands', keys: 'Ctrl+K', run: palette }])
  document.body.addEventListener('keydown', onGlobalKey)
  const dialog = document.createElement('div')
  dialog.setAttribute('role', 'dialog')
  dialog.dataset.state = 'open'
  document.body.append(dialog)
  key({ key: 'k', ctrlKey: true })
  expect(palette).not.toHaveBeenCalled()
  dialog.remove()
  key({ key: 'k', ctrlKey: true })
  expect(palette).toHaveBeenCalledTimes(1)
  document.body.removeEventListener('keydown', onGlobalKey)
})

it('splits a shortcut into the caps it is shown as', () => {
  expect(keyCaps('Ctrl+Shift+K')).toEqual(['Ctrl', 'Shift', 'K'])
  expect(keyCaps('F')).toEqual(['F'])
})

it('keeps collection menus and the palette on the current item and availability', async () => {
  const scope = effectScope()
  const items = shallowRef([
    { id: 1, name: 'Before' },
    { id: 2, name: 'Other' },
  ])
  const busy = ref(false)
  const run = vi.fn<(name: string) => void>()
  const menu = scope.run(() =>
    useItemCommands(
      items,
      (item) => item.id,
      (item) => [
        {
          id: `item.${item.id}.edit`,
          group: 'view',
          title: () => item.name,
          enabled: () => !busy.value,
          run: () => run(item.name),
        },
      ],
    ),
  )
  if (!menu) throw new Error('The command scope did not start')
  try {
    const original = menu(1)[0]
    expect(original?.label).toBe('Before')
    expect(menu(2).map((item) => item.label)).toEqual(['Other'])
    busy.value = true
    expect(menu(1)[0]?.disabled).toBe(true)
    await original?.onSelect?.(new Event('select'))
    expect(run).not.toHaveBeenCalled()
    busy.value = false
    items.value = [{ id: 1, name: 'After' }]
    await nextTick()
    expect(menu(1)[0]?.label).toBe('After')
    expect(commandById('item.1.edit')?.title()).toBe('After')
    expect(menu(2)).toEqual([])
    expect(commandById('item.2.edit')).toBeUndefined()
    await original?.onSelect?.(new Event('select'))
    expect(run).toHaveBeenCalledExactlyOnceWith('After')
    items.value = []
    await nextTick()
    await original?.onSelect?.(new Event('select'))
    expect(run).toHaveBeenCalledOnce()
  } finally {
    scope.stop()
  }
  expect(commandById('item.1.edit')).toBeUndefined()
})
