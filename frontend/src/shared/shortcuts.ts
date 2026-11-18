// Defaults and help presentation come from the same executable command definitions.
import { MAX_SHORTCUTS } from '../bridge'
import { altKeysOf, keysOf, sameKeys, zonesOverlap } from './commands'
import { commandZones, staticCommands } from './commandContract'
import type { CommandGroup, CommandZone, ShortcutCommandId, StaticCommandId } from './commandContract'
import type { SimpleMessageKey } from './i18n'

export interface ShortcutEntry {
  id: StaticCommandId
  group: CommandGroup
  title: SimpleMessageKey
  keys?: string
  altKeys?: readonly string[]
  zone?: CommandZone
  locked?: boolean
}
export interface EditableShortcut extends ShortcutEntry {
  id: ShortcutCommandId
}
export const shortcutCatalogue: readonly EditableShortcut[] = staticCommands.flatMap((command) =>
  'shortcut' in command
    ? [
        {
          id: command.id,
          group: command.group,
          title: command.label,
          ...command.shortcut,
          zone: 'zone' in command ? command.zone : undefined,
        },
      ]
    : [],
)
const conflictCatalogue: readonly ShortcutEntry[] = staticCommands.map((command) => ({
  id: command.id,
  group: command.group,
  title: command.label,
  ...('shortcut' in command ? command.shortcut : {}),
  zone: 'zone' in command ? command.zone : undefined,
}))

/** Where a shortcut works, in the order the settings list them. */
export const shortcutZones = commandZones
export type ShortcutZone = (typeof shortcutZones)[number]

export function zoneOf(entry: ShortcutEntry): ShortcutZone {
  return entry.zone ?? 'global'
}

/**
 * The command that already answers to keys where entry's would hear them
 * too, if there is one: giving entry the same keys would leave one of the
 * two unreachable.
 */
export function shortcutConflict(entry: ShortcutEntry, keys: string): ShortcutEntry | undefined {
  return conflictCatalogue.find((other) => {
    if (other.id === entry.id || !zonesOverlap(entry.zone, other.zone)) return false
    const taken = keysOf(other)
    return (taken !== undefined && sameKeys(taken, keys)) || altKeysOf(other).some((alt) => sameKeys(alt, keys))
  })
}

/**
 * What stands in the way of entry answering to keys, or to its own again
 * (undefined), which brings back the other keys that go with them: the first
 * of those keys another action already hears, and that action.
 */
export function assignmentConflict(
  entry: ShortcutEntry,
  keys: string | undefined,
): { keys: string; taken: ShortcutEntry } | undefined {
  const wanted = keys === undefined ? [entry.keys ?? '', ...(entry.altKeys ?? [])] : [keys]
  for (const candidate of wanted) {
    const taken = candidate ? shortcutConflict(entry, candidate) : undefined
    if (taken) return { keys: candidate, taken }
  }
  return undefined
}

/** Whether another shortcut may still be reassigned (core.MaxShortcuts bounds what is stored). */
export function canReassign(shortcuts: Readonly<Record<string, string>>, id: string): boolean {
  return id in shortcuts || Object.keys(shortcuts).length < MAX_SHORTCUTS
}
