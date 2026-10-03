// The command registry: one description of each thing the user can do, which
// toolbars, context menus, the command palette and keyboard shortcuts all
// read, so an action has one label, one icon, one shortcut and one rule for
// when it is available. A feature registers its commands while its view is
// mounted; the shell registers navigation. The engine remains the authority:
// a command only asks, and every service checks again.
import { computed, onScopeDispose, shallowRef } from 'vue'
import type { IconName } from '../design/icons'
import { isTyping } from './keys'
import { shortcutsSuspended } from './overlay'

/** Where a command is grouped in the palette and the shortcut reference. */
export type CommandGroup = 'navigate' | 'view' | 'media' | 'library' | 'app'

export interface Command {
  /** Stable and unique, e.g. "media.favourite". */
  id: string
  group: CommandGroup
  /** The label, read when shown so it follows what is selected. */
  title: () => string
  icon?: IconName
  /**
   * The shortcut, as it is shown: "F", "Ctrl+K", "Delete". Keys join with
   * "+"; modifiers are Ctrl, Shift and Alt.
   */
  keys?: string
  /** Other keys that do the same ("/" beside Ctrl+F); only keys is shown. */
  altKeys?: readonly string[]
  /**
   * Which key events reach the shortcut: "global" from anywhere on the
   * page, or the name of the zone whose owner passes its own key events to
   * dispatchKey (the grid, the viewer).
   */
  zone?: string
  /** Whether it applies at all right now (else it is not offered). */
  available?: () => boolean
  /** Whether it can run now; false shows it disabled (a save in flight). */
  enabled?: () => boolean
  run: () => unknown
}

const registered = shallowRef<readonly Command[]>([])

/** Every registered command, in registration order. */
export const commands = computed(() => registered.value)

/** The commands that apply now. */
export const availableCommands = computed(() => registered.value.filter(isAvailable))

export function isAvailable(c: Command): boolean {
  return c.available?.() ?? true
}

export function isEnabled(c: Command): boolean {
  return isAvailable(c) && (c.enabled?.() ?? true)
}

/** Adds commands; the returned function removes them again. */
export function registerCommands(cmds: readonly Command[]): () => void {
  const ids = new Set(cmds.map((c) => c.id))
  // A view remounting registers the same ids again: the newest wins.
  registered.value = [...registered.value.filter((c) => !ids.has(c.id)), ...cmds]
  return () => {
    registered.value = registered.value.filter((c) => !cmds.includes(c))
  }
}

/** Registers commands for as long as the calling component or store lives. */
export function useCommands(cmds: readonly Command[]): void {
  onScopeDispose(registerCommands(cmds))
}

export function commandById(id: string): Command | undefined {
  return registered.value.find((c) => c.id === id)
}

/** Runs a command if it can run now; resolves whether it did. */
export async function runCommand(id: string): Promise<boolean> {
  const c = commandById(id)
  if (!c || !isEnabled(c)) return false
  await c.run()
  return true
}

interface Chord {
  key: string
  ctrl: boolean
  shift: boolean
  alt: boolean
}

function parseKeys(keys: string): Chord {
  const parts = keys.split('+')
  return {
    key: (parts.at(-1) ?? '').toLowerCase(),
    ctrl: parts.includes('Ctrl'),
    shift: parts.includes('Shift'),
    alt: parts.includes('Alt'),
  }
}

/** The shortcut's keys as separate caps, for display: "Ctrl+K" gives ["Ctrl", "K"]. */
export function keyCaps(keys: string): string[] {
  return keys.split('+')
}

function matches(e: KeyboardEvent, keys: string): boolean {
  const chord = parseKeys(keys)
  // A character's Shift is part of the character ("?"), so only named keys
  // and letters compare it.
  const shiftMatters = chord.key.length > 1 || /[a-z0-9]/.test(chord.key)
  return (
    e.key.toLowerCase() === chord.key &&
    (e.ctrlKey || e.metaKey) === chord.ctrl &&
    e.altKey === chord.alt &&
    (!shiftMatters || e.shiftKey === chord.shift)
  )
}

/**
 * Runs the command whose shortcut e is, among those of zone, and says
 * whether one ran (the event is then spent). A zone's owner calls this from
 * its own keydown handler; the shell calls it for "global".
 */
export function dispatchKey(e: KeyboardEvent, zone: string): boolean {
  if (e.defaultPrevented || e.isComposing) return false
  const c = registered.value.find(
    (cmd) =>
      (cmd.zone ?? 'global') === zone &&
      ((cmd.keys !== undefined && matches(e, cmd.keys)) || (cmd.altKeys?.some((k) => matches(e, k)) ?? false)),
  )
  if (!c || !isEnabled(c)) return false
  e.preventDefault()
  void c.run()
  return true
}

/**
 * The page-wide shortcut handler: global commands, unless the user is typing
 * (a modified chord such as Ctrl+K still works there) or an overlay owns
 * the keyboard.
 */
export function onGlobalKey(e: KeyboardEvent): void {
  const modified = e.ctrlKey || e.metaKey || e.altKey
  if (isTyping(e) && !modified) return
  if (shortcutsSuspended()) return
  dispatchKey(e, 'global')
}
