// The command registry: one description of each thing the user can do, which
// toolbars, context menus, the command palette and keyboard shortcuts all
// read, so an action has one label, one icon, one shortcut and one rule for
// when it is available. A feature registers its commands while its view is
// mounted; the shell registers navigation. The engine remains the authority:
// a command only asks, and every service checks again.
import type { DropdownMenuItem } from '@nuxt/ui/components/DropdownMenu.vue'
import { computed, onScopeDispose, shallowRef, toValue, watch } from 'vue'
import type { MaybeRefOrGetter } from 'vue'
import { icons } from '../design/icons'
import type { IconName } from '../design/icons'
import { isTyping } from './keys'
import { shortcutsSuspended } from './overlay'

/** Where a command is grouped, in the order the palette and the shortcut reference show them. */
export const commandGroups = ['media', 'view', 'library', 'navigate', 'app'] as const
export type CommandGroup = (typeof commandGroups)[number]

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

function isAvailable(c: Command): boolean {
  return c.available?.() ?? true
}

export function isEnabled(c: Command): boolean {
  return isAvailable(c) && (c.enabled?.() ?? true)
}

/** Adds commands; the returned function removes them again. */
export function registerCommands(cmds: readonly Command[]): () => void {
  const ids = new Set(cmds.map((c) => c.id))
  const owned = new Set(cmds)
  // A view remounting registers the same ids again: the newest wins.
  registered.value = [...registered.value.filter((c) => !ids.has(c.id)), ...cmds]
  return () => {
    registered.value = registered.value.filter((c) => !owned.has(c))
  }
}

/** Registers commands for as long as the calling component or store lives. */
export function useCommands(cmds: readonly Command[]): void {
  onScopeDispose(registerCommands(cmds))
}

/**
 * Registers a collection's actions and indexes its menus by item. Building
 * every card's menu therefore visits only that card's commands, rather
 * than searching all the collection's commands for every rendered card.
 * A menu chosen after the list changed resolves the current registration.
 */
export function useItemCommands<T, K extends string | number>(
  items: MaybeRefOrGetter<readonly T[]>,
  key: (item: T) => K,
  create: (item: T) => readonly Command[],
): (id: K) => DropdownMenuItem[] {
  const groups = computed(() => new Map(toValue(items).map((item) => [key(item), create(item)])))
  watch(groups, (current, _previous, cleanup) => cleanup(registerCommands([...current.values()].flat())), {
    immediate: true,
  })
  return (id) => menuItems(groups.value.get(id) ?? [])
}

/** A menu of these commands: those that apply now, run through the registry when chosen. */
export function menuItems(cmds: readonly Command[]): DropdownMenuItem[] {
  return cmds.filter(isAvailable).map((c) => ({
    label: c.title(),
    icon: c.icon ? icons[c.icon] : undefined,
    disabled: !isEnabled(c),
    onSelect: () => runCommand(c.id),
  }))
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
