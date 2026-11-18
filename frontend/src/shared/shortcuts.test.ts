import shortcutFixtures from '../../../internal/core/testdata/shortcut-contract.json'
import { staticCommand } from './commandContract'
import { afterEach, expect, it } from 'vitest'
import {
  chordOf,
  dispatchKey,
  keysOf,
  registerCommands,
  sameKeys,
  setShortcutOverrides,
  zonesOverlap,
  validShortcut,
} from './commands'
import type { Command } from './commands'
import { t } from './i18n'
import { assignmentConflict, canReassign, shortcutCatalogue, shortcutConflict } from './shortcuts'
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

it('derives every curated shortcut from executable metadata', () => {
  expect(shortcutCatalogue).toHaveLength(31)
  for (const e of shortcutCatalogue) {
    const command = staticCommand(e.id)
    expect({
      group: e.group,
      title: e.title,
      keys: e.keys,
      altKeys: e.altKeys,
      zone: e.zone,
      locked: e.locked,
    }).toEqual({ group: command.group, title: command.label, ...command.shortcut, zone: command.zone })
    expect(t(e.title)).not.toBe('')
  }
  expect(new Set(shortcutCatalogue.map((e) => e.id)).size).toBe(shortcutCatalogue.length)
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
    title: () => 'Zoom in',
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
  undo.push(registerCommands([{ id: 'app.shortcuts', title: () => '', run: () => ran++ }]))
  setShortcutOverrides({ 'app.shortcuts': 'Space' })
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
  expect(keysOf(entry('viewer.favourite'))).toBe('L')
  expect(shortcutConflict(entry('viewer.rotate'), 'F')).toBeUndefined()
  expect(shortcutConflict(entry('viewer.rotate'), 'I')).toBeUndefined()
  expect(shortcutConflict(entry('viewer.rotate'), 'l')?.id).toBe('viewer.favourite')
  setShortcutOverrides({ 'search.focus': '' })
  expect(shortcutConflict(entry('viewer.rotate'), '/')).toBeUndefined()
})

it('refuses to give an action back keys, or other keys of its own, that another has since taken', () => {
  const search = entry('search.focus')
  expect(assignmentConflict(search, undefined)).toBeUndefined()
  expect(assignmentConflict(search, '')).toBeUndefined() // no shortcut conflicts with nothing
  expect(assignmentConflict(entry('viewer.rotate'), 'Ctrl+F')).toEqual({ keys: 'Ctrl+F', taken: search })

  // Reassigned, search gives up "/" as well; the viewer may take it.
  setShortcutOverrides({ 'search.focus': 'Ctrl+J', 'viewer.rotate': '/' })
  expect(assignmentConflict(search, undefined)).toEqual({ keys: '/', taken: entry('viewer.rotate') })
  setShortcutOverrides({ 'search.focus': 'Ctrl+J', 'viewer.rotate': 'Ctrl+F' })
  expect(assignmentConflict(search, undefined)).toEqual({ keys: 'Ctrl+F', taken: entry('viewer.rotate') })
})

it('stops reassigning at the number of shortcuts the library keeps', () => {
  const many = Object.fromEntries(Array.from({ length: MAX_SHORTCUTS }, (_, i) => [`test.command${i}`, 'F2']))
  expect(canReassign(many, 'viewer.rotate')).toBe(false)
  expect(canReassign(many, 'test.command3')).toBe(true) // changing one already changed
  expect(canReassign({}, 'viewer.rotate')).toBe(true)
})

it('keeps known imported overrides active outside the editor and includes them in conflicts', () => {
  setShortcutOverrides({ 'go.people': 'Ctrl+G', 'legacy.action': 'Ctrl+L', 'viewer.close': 'X' })
  const run = () => undefined
  undo.push(
    registerCommands([
      { id: 'go.people', run },
      { id: 'viewer.close', run },
    ]),
  )
  expect(dispatchKey(press({ key: 'g', ctrlKey: true }), 'global')).toBe(true)
  expect(shortcutConflict(entry('viewer.rotate'), 'Ctrl+G')?.id).toBe('go.people')
  expect(shortcutConflict(entry('viewer.rotate'), 'Ctrl+L')).toBeUndefined()
  expect(dispatchKey(press({ key: 'x' }), 'viewer')).toBe(false)
  expect(dispatchKey(press({ key: 'Escape' }), 'viewer')).toBe(true)
})

it('validates stored chords against the same fixtures as core preferences', () => {
  for (const fixture of shortcutFixtures) expect(validShortcut(fixture.keys), fixture.keys).toBe(fixture.valid)
})
