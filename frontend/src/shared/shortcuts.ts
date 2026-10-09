// The commands whose shortcut the user may choose, for the shortcut settings.
// A command is registered only while its view is mounted (the viewer's exist
// only while a photo is open), so the settings cannot read them from the
// registry: this list names them. It repeats nothing a command decides: each
// entry's keys, zone and group are those its command declares, and
// shortcuts.test.ts fails when one drifts from the other.
import { MAX_SHORTCUTS } from '../bridge'
import { altKeysOf, keysOf, sameKeys, zonesOverlap } from './commands'
import type { CommandGroup } from './commands'
import type { MessageKey } from './i18n'

export interface ShortcutEntry {
  /** The command's id. */
  id: string
  group: CommandGroup
  /** What the command is called; one name where its own title follows the selection. */
  title: MessageKey
  /** The command's own shortcut, if it has one. */
  keys?: string
  altKeys?: readonly string[]
  /** Which key presses reach it (Command.zone); the whole page when left out. */
  zone?: 'grid' | 'viewer'
  /** Its shortcut cannot be changed: Escape always leaves the viewer. */
  locked?: boolean
}

export const shortcutCatalogue: readonly ShortcutEntry[] = [
  { id: 'app.commandPalette', group: 'app', title: 'commands.title', keys: 'Ctrl+K' },
  { id: 'app.shortcuts', group: 'app', title: 'shortcuts.title', keys: '?' },
  { id: 'app.toggleSidebar', group: 'view', title: 'shell.toggleSidebar', keys: 'Ctrl+B' },
  { id: 'search.focus', group: 'library', title: 'search.command', keys: 'Ctrl+F', altKeys: ['/'] },
  { id: 'view.largerTiles', group: 'view', title: 'library.zoomIn' },
  { id: 'view.smallerTiles', group: 'view', title: 'library.zoomOut' },

  { id: 'media.open', group: 'media', title: 'library.open', keys: 'Enter', zone: 'grid' },
  { id: 'media.favourite', group: 'media', title: 'details.favourite', keys: 'F', zone: 'grid' },
  { id: 'view.details', group: 'view', title: 'library.info', keys: 'I', zone: 'grid' },
  { id: 'library.files', group: 'media', title: 'files.title', zone: 'grid' },
  { id: 'library.batch', group: 'media', title: 'files.batch', zone: 'grid' },
  { id: 'library.open', group: 'media', title: 'files.open', zone: 'grid' },

  { id: 'viewer.close', group: 'view', title: 'common.close', keys: 'Escape', zone: 'viewer', locked: true },
  { id: 'viewer.previous', group: 'view', title: 'viewer.prev', keys: 'ArrowLeft', zone: 'viewer' },
  { id: 'viewer.next', group: 'view', title: 'viewer.next', keys: 'ArrowRight', zone: 'viewer' },
  { id: 'viewer.zoomIn', group: 'view', title: 'viewer.zoomIn', keys: '=', zone: 'viewer' },
  { id: 'viewer.zoomOut', group: 'view', title: 'viewer.zoomOut', keys: '-', zone: 'viewer' },
  { id: 'viewer.fit', group: 'view', title: 'viewer.fit', keys: '0', zone: 'viewer' },
  { id: 'viewer.actual', group: 'view', title: 'viewer.actual', keys: '1', zone: 'viewer' },
  { id: 'viewer.favourite', group: 'media', title: 'details.favourite', keys: 'F', zone: 'viewer' },
  { id: 'viewer.details', group: 'view', title: 'library.info', keys: 'I', zone: 'viewer' },
  { id: 'viewer.fullscreen', group: 'view', title: 'viewer.fullscreen', zone: 'viewer' },
  { id: 'viewer.filmstrip', group: 'view', title: 'viewer.filmstrip', zone: 'viewer' },
  { id: 'viewer.slideshow', group: 'view', title: 'viewer.slideshow', zone: 'viewer' },
  { id: 'viewer.compare', group: 'view', title: 'viewer.compare', zone: 'viewer' },
  { id: 'viewer.rotate', group: 'media', title: 'viewer.rotate', zone: 'viewer' },
  { id: 'viewer.flip', group: 'media', title: 'viewer.flip', zone: 'viewer' },
  { id: 'viewer.resetAdjustment', group: 'media', title: 'viewer.resetAdjustment', zone: 'viewer' },
  { id: 'viewer.trash', group: 'media', title: 'trash.move', zone: 'viewer' },
]

/** Where a shortcut works, in the order the settings list them. */
export const shortcutZones = ['global', 'grid', 'viewer'] as const
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
  return shortcutCatalogue.find((other) => {
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
