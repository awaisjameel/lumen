import { afterEach, expect, it } from 'vitest'
import {
  chordOf,
  dispatchKey,
  keysOf,
  registerCommands,
  sameKeys,
  setShortcutOverrides,
  zonesOverlap,
} from './commands'
import type { Command } from './commands'
import { t } from './i18n'
import { canReassign, currentKeys, shortcutCatalogue, shortcutConflict } from './shortcuts'
import type { ShortcutEntry } from './shortcuts'
import { MAX_SHORTCUTS } from '../bridge'

let undo: (() => void)[] = []
afterEach(() => {
  for (const u of undo) u()
  undo = []
  setShortcutOverrides({})
})

function entry(id: string): ShortcutEntry {
  const found = shortcutCatalogue.find((e) => e.id === id)
  if (!found) throw new Error(`no catalogue entry ${id}`)
  return found
}

function press(init: KeyboardEventInit): KeyboardEvent {
  return new KeyboardEvent('keydown', { ...init, cancelable: true })
}

// Every command as the views declare it, read from their source: id, and the
// group, keys and zone written beside it.
function declared(): Map<string, { group?: string; keys?: string; altKeys?: string[]; zone?: string }> {
  const sources = import.meta.glob<string>('../**/*.vue', { query: '?raw', import: 'default', eager: true })
  const out = new Map<string, { group?: string; keys?: string; altKeys?: string[]; zone?: string }>()
  for (const source of Object.values(sources)) {
    for (const block of source.split(/\n\s*\{\n\s*id: '/).slice(1)) {
      const id = block.slice(0, block.indexOf("'"))
      const body = block.slice(0, block.search(/\n\s*run[:(]|\n\s*run,/))
      const field = (name: string) => new RegExp(`\\n\\s*${name}: '([^']+)'`).exec(body)?.[1]
      const aliases = /\n\s*altKeys: \[([^\]]*)\]/.exec(body)?.[1]
      const altKeys = aliases === undefined ? undefined : [...aliases.matchAll(/'([^']+)'/g)].flatMap((m) => m[1] ?? [])
      out.set(id, { group: field('group'), keys: field('keys'), altKeys, zone: field('zone') })
    }
  }
  return out
}

it('lists each command exactly as its view declares it', () => {
  const commands = declared()
  expect(commands.size).toBeGreaterThan(40)
  for (const e of shortcutCatalogue) {
    const command = commands.get(e.id)
    expect(command, `${e.id} is not a declared command`).toBeDefined()
    expect({ id: e.id, group: e.group, keys: e.keys, altKeys: e.altKeys, zone: e.zone }).toEqual({
      id: e.id,
      ...command,
    })
    expect(t(e.title)).not.toBe('')
  }
  // No command with a shortcut of its own is missing from the settings.
  const listed = new Set(shortcutCatalogue.map((e) => e.id))
  for (const [id, command] of commands) if (command.keys) expect(listed, id).toContain(id)
  expect(listed.size).toBe(shortcutCatalogue.length)
})

it('starts with no two actions on the same keys where both would hear them', () => {
  for (const e of shortcutCatalogue) {
    if (e.keys) expect(shortcutConflict(e, e.keys), `${e.id} shares ${e.keys}`).toBeUndefined()
  }
  // Only the viewer's Close is fixed, and it is Escape.
  expect(shortcutCatalogue.filter((e) => e.locked).map((e) => [e.id, e.keys])).toEqual([['viewer.close', 'Escape']])
})

it('runs a command by the keys the user gave it, and no longer by its own', () => {
  let ran = 0
  const command: Command = {
    id: 'viewer.zoomIn',
    group: 'view',
    title: () => 'Zoom in',
    keys: '=',
    altKeys: ['+'],
    zone: 'viewer',
    run: () => ran++,
  }
  undo.push(registerCommands([command]))
  expect(dispatchKey(press({ key: '=' }), 'viewer')).toBe(true)

  setShortcutOverrides({ 'viewer.zoomIn': 'Ctrl+Shift+Z' })
  expect(keysOf(command)).toBe('Ctrl+Shift+Z')
  expect(dispatchKey(press({ key: '=' }), 'viewer')).toBe(false)
  expect(dispatchKey(press({ key: '+' }), 'viewer')).toBe(false) // its other key went with its own
  expect(dispatchKey(press({ key: 'Z', ctrlKey: true, shiftKey: true }), 'viewer')).toBe(true)
  expect(dispatchKey(press({ key: 'z', ctrlKey: true }), 'viewer')).toBe(false)

  // "" leaves the command without a shortcut at all.
  setShortcutOverrides({ 'viewer.zoomIn': '' })
  expect(keysOf(command)).toBeUndefined()
  expect(dispatchKey(press({ key: '=' }), 'viewer')).toBe(false)
  expect(ran).toBe(2)

  setShortcutOverrides({})
  expect(keysOf(command)).toBe('=')
})

it('spells a key press as the shortcut it is stored as', () => {
  expect(chordOf(press({ key: 'k', ctrlKey: true }))).toBe('Ctrl+K')
  expect(chordOf(press({ key: 'P', ctrlKey: true, shiftKey: true, altKey: true }))).toBe('Ctrl+Shift+Alt+P')
  expect(chordOf(press({ key: 'ArrowLeft', shiftKey: true }))).toBe('Shift+ArrowLeft')
  expect(chordOf(press({ key: ' ' }))).toBe('Space')
  expect(chordOf(press({ key: '?', shiftKey: true }))).toBe('?') // the character already says Shift
  expect(chordOf(press({ key: 'Shift', shiftKey: true }))).toBe('') // only a modifier so far
  for (const key of ['Escape', 'Tab', '+']) expect(chordOf(press({ key }))).toBeNull()
  expect(chordOf(press({ key: 'F4', altKey: true }))).toBeNull()
  // The space bar then runs what was given "Space".
  let ran = 0
  undo.push(registerCommands([{ id: 'test.space', group: 'view', title: () => '', keys: 'Space', run: () => ran++ }]))
  expect(dispatchKey(press({ key: ' ' }), 'global')).toBe(true)
  expect(ran).toBe(1)
})

it('finds a conflict only between actions that hear the same key presses', () => {
  expect(sameKeys('Ctrl+k', 'Ctrl+K')).toBe(true)
  expect(sameKeys('Ctrl+K', 'Ctrl+Shift+K')).toBe(false)
  expect(zonesOverlap('grid', 'viewer')).toBe(false)
  expect(zonesOverlap('grid', undefined)).toBe(true)
  expect(zonesOverlap(undefined, undefined)).toBe(true)

  // The grid and the viewer share F today, and may: one is behind the other.
  expect(shortcutConflict(entry('viewer.rotate'), 'F')?.id).toBe('viewer.favourite')
  expect(shortcutConflict(entry('library.files'), 'ArrowLeft')).toBeUndefined()
  // A page-wide shortcut would be heard in the viewer too.
  expect(shortcutConflict(entry('viewer.rotate'), 'Ctrl+K')?.id).toBe('app.commandPalette')
  expect(shortcutConflict(entry('viewer.rotate'), '/')?.id).toBe('search.focus')
  expect(shortcutConflict(entry('view.largerTiles'), 'I')?.id).toBe('view.details')

  // Keys an action gave up are free; keys it was given are taken.
  setShortcutOverrides({ 'viewer.favourite': 'L', 'viewer.details': '' })
  expect(currentKeys(entry('viewer.favourite'))).toBe('L')
  expect(shortcutConflict(entry('viewer.rotate'), 'F')).toBeUndefined()
  expect(shortcutConflict(entry('viewer.rotate'), 'I')).toBeUndefined()
  expect(shortcutConflict(entry('viewer.rotate'), 'l')?.id).toBe('viewer.favourite')
  setShortcutOverrides({ 'search.focus': '' })
  expect(shortcutConflict(entry('viewer.rotate'), '/')).toBeUndefined()
})

it('stops reassigning at the number of shortcuts the library keeps', () => {
  const many = Object.fromEntries(Array.from({ length: MAX_SHORTCUTS }, (_, i) => [`test.command${i}`, 'F2']))
  expect(canReassign(many, 'viewer.rotate')).toBe(false)
  expect(canReassign(many, 'test.command3')).toBe(true) // changing one already changed
  expect(canReassign({}, 'viewer.rotate')).toBe(true)
})
