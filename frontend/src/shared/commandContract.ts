/** Command identities and defaults; feature bindings supply execution and context. */
import { DuplicateKind } from '../bridge'
import { InvalidNavigation, positiveID, routes } from './navigation/contract'
import type { IconName } from '../design/icons'
import type { SimpleMessageKey } from './i18n'

export const commandGroups = ['media', 'view', 'library', 'navigate', 'app'] as const
export type CommandGroup = (typeof commandGroups)[number]
export const commandZones = ['global', 'grid', 'viewer'] as const
export type CommandZone = (typeof commandZones)[number]
export interface ShortcutDefaults {
  readonly keys?: string
  readonly altKeys?: readonly string[]
  readonly locked?: boolean
}
export interface CommandMetadata {
  readonly group: CommandGroup
  readonly label?: SimpleMessageKey
  readonly icon?: IconName
  readonly zone?: CommandZone
  readonly shortcut?: ShortcutDefaults
  readonly repeatable?: boolean
  readonly danger?: boolean
  readonly compression?: true
}
function definition<const N extends string, const M extends Omit<CommandMetadata, 'group' | 'label'>>(
  id: N,
  group: CommandGroup,
  label: SimpleMessageKey,
  metadata: M,
) {
  return { id, group, label, ...metadata }
}

// Curated shortcuts retain their order; the same definitions drive execution.
const definitions = [
  definition('app.commandPalette', 'app', 'commands.title', { icon: 'command', shortcut: { keys: 'Ctrl+K' } }),
  definition('app.shortcuts', 'app', 'shortcuts.title', { icon: 'keyboard', shortcut: { keys: '?' } }),
  definition('app.toggleSidebar', 'view', 'shell.toggleSidebar', {
    icon: 'sidebar-collapse',
    shortcut: { keys: 'Ctrl+B' },
  }),
  definition('search.focus', 'library', 'search.command', {
    icon: 'search',
    shortcut: { keys: 'Ctrl+F', altKeys: ['/'] },
  }),
  definition('view.largerTiles', 'view', 'library.zoomIn', { icon: 'zoom-in', repeatable: true, shortcut: {} }),
  definition('view.smallerTiles', 'view', 'library.zoomOut', { icon: 'zoom-out', repeatable: true, shortcut: {} }),
  definition('media.open', 'media', 'library.open', { icon: 'image', zone: 'grid', shortcut: { keys: 'Enter' } }),
  definition('media.favourite', 'media', 'details.favourite', { icon: 'heart', zone: 'grid', shortcut: { keys: 'F' } }),
  definition('view.details', 'view', 'library.info', { icon: 'inspector', zone: 'grid', shortcut: { keys: 'I' } }),
  definition('library.files', 'media', 'files.title', { icon: 'files', zone: 'grid', shortcut: {} }),
  definition('library.compress', 'media', 'compression.action', {
    icon: 'files',
    zone: 'grid',
    compression: true,
    shortcut: {},
  }),
  definition('library.batch', 'media', 'files.batch', { icon: 'check', zone: 'grid', shortcut: {} }),
  definition('library.open', 'media', 'files.open', { icon: 'external', zone: 'grid', shortcut: {} }),
  definition('viewer.close', 'view', 'common.close', {
    icon: 'close',
    zone: 'viewer',
    shortcut: { keys: 'Escape', locked: true },
  }),
  definition('viewer.previous', 'view', 'viewer.prev', {
    icon: 'chevron-left',
    zone: 'viewer',
    repeatable: true,
    shortcut: { keys: 'ArrowLeft' },
  }),
  definition('viewer.next', 'view', 'viewer.next', {
    icon: 'chevron-right',
    zone: 'viewer',
    repeatable: true,
    shortcut: { keys: 'ArrowRight' },
  }),
  definition('viewer.zoomIn', 'view', 'viewer.zoomIn', {
    icon: 'zoom-in',
    zone: 'viewer',
    repeatable: true,
    shortcut: { keys: '=' },
  }),
  definition('viewer.zoomOut', 'view', 'viewer.zoomOut', {
    icon: 'zoom-out',
    zone: 'viewer',
    repeatable: true,
    shortcut: { keys: '-' },
  }),
  definition('viewer.fit', 'view', 'viewer.fit', {
    icon: 'maximize',
    zone: 'viewer',
    repeatable: true,
    shortcut: { keys: '0' },
  }),
  definition('viewer.actual', 'view', 'viewer.actual', {
    icon: 'image',
    zone: 'viewer',
    repeatable: true,
    shortcut: { keys: '1' },
  }),
  definition('viewer.favourite', 'media', 'details.favourite', {
    icon: 'heart',
    zone: 'viewer',
    shortcut: { keys: 'F' },
  }),
  definition('viewer.details', 'view', 'library.info', { icon: 'info', zone: 'viewer', shortcut: { keys: 'I' } }),
  definition('viewer.fullscreen', 'view', 'viewer.fullscreen', { icon: 'maximize', zone: 'viewer', shortcut: {} }),
  definition('viewer.filmstrip', 'view', 'viewer.filmstrip', { icon: 'film', zone: 'viewer', shortcut: {} }),
  definition('viewer.slideshow', 'view', 'viewer.slideshow', { icon: 'slideshow', zone: 'viewer', shortcut: {} }),
  definition('viewer.compare', 'view', 'viewer.compare', { icon: 'layers', zone: 'viewer', shortcut: {} }),
  definition('viewer.rotate', 'media', 'viewer.rotate', { icon: 'rotate', zone: 'viewer', shortcut: {} }),
  definition('viewer.flip', 'media', 'viewer.flip', { icon: 'flip', zone: 'viewer', shortcut: {} }),
  definition('viewer.resetAdjustment', 'media', 'viewer.resetAdjustment', {
    icon: 'undo',
    zone: 'viewer',
    shortcut: {},
  }),
  definition('viewer.trash', 'media', 'trash.move', { icon: 'trash', zone: 'viewer', shortcut: {} }),
  definition('viewer.compress', 'media', 'compression.action', {
    icon: 'files',
    zone: 'viewer',
    compression: true,
    shortcut: {},
  }),
  definition('about.logs', 'app', 'about.openLogs', { icon: 'folder' }),
  definition('about.report', 'app', 'about.saveReport', { icon: 'bug' }),
  definition('about.licences', 'app', 'about.licences', { icon: 'document' }),
  definition('albums.new', 'library', 'albums.new', { icon: 'plus' }),
  definition('albums.save', 'library', 'albums.save', { icon: 'save' }),
  definition('duplicates.refresh', 'view', 'duplicates.refresh', { icon: 'refresh' }),
  definition('events.refresh', 'view', 'events.refresh', { icon: 'refresh' }),
  definition('inspector.files', 'media', 'files.title', { icon: 'files' }),
  definition('inspector.open', 'media', 'files.open', { icon: 'external' }),
  definition('inspector.close', 'view', 'common.close', { icon: 'close' }),
  definition('inspector.favourite', 'media', 'details.favourite', { icon: 'heart' }),
  definition('inspector.trash', 'media', 'trash.move', { icon: 'trash' }),
  definition('inspector.restore', 'media', 'trash.restore', { icon: 'restore' }),
  definition('inspector.delete', 'media', 'trash.deleteFromDisk', { icon: 'trash' }),
  definition('inspector.block', 'media', 'blocked.block', { icon: 'eye-off' }),
  definition('inspector.folder', 'media', 'common.showInFolder', { icon: 'folder' }),
  definition('app.activity', 'app', 'status.open', {}),
  definition('library.favouriteSelection', 'media', 'details.favourite', { icon: 'heart' }),
  definition('library.albumSelection', 'media', 'library.album', { icon: 'albums' }),
  definition('library.exportSelection', 'media', 'library.export', { icon: 'download' }),
  definition('library.trashSelection', 'media', 'trash.move', { icon: 'trash', danger: true }),
  definition('library.unfavouriteSelection', 'media', 'details.unfavourite', { icon: 'heart' }),
  definition('library.tagSelection', 'media', 'details.addTag', { icon: 'tag' }),
  definition('library.removeAlbumSelection', 'media', 'files.removeAlbum', { icon: 'albums' }),
  definition('library.moveSelection', 'media', 'files.move', { icon: 'move' }),
  definition('library.renameSelection', 'media', 'files.rename', { icon: 'pencil' }),
  definition('library.revealSelection', 'media', 'library.showExplorer', { icon: 'folder' }),
  definition('library.copyPath', 'media', 'files.copyPath', { icon: 'copy' }),
  definition('library.rotateSelection', 'media', 'viewer.rotate', { icon: 'rotate' }),
  definition('library.blockSelection', 'media', 'blocked.block', { icon: 'blocked' }),
  definition('viewer.openFile', 'media', 'files.open', { icon: 'external', zone: 'viewer' }),
  definition('viewer.chooseApp', 'media', 'files.openWith', { icon: 'external', zone: 'viewer' }),
  definition('viewer.folder', 'media', 'common.showInFolder', { icon: 'folder', zone: 'viewer' }),
  definition('viewer.files', 'media', 'files.title', { icon: 'files', zone: 'viewer' }),
  definition('viewer.autoHide', 'view', 'viewer.autoHide', { icon: 'eye-off', zone: 'viewer' }),
  definition('viewer.doubleZoom', 'view', 'viewer.doubleZoom', { icon: 'zoom-in', zone: 'viewer' }),
  definition('viewer.restore', 'media', 'trash.restore', { icon: 'restore', zone: 'viewer' }),
  definition('viewer.delete', 'media', 'trash.deleteFromDisk', { icon: 'trash', zone: 'viewer' }),
  definition('welcome.addFolder', 'library', 'welcome.addFolder', { icon: 'folder-add' }),
  definition('welcome.start', 'library', 'welcome.start', { icon: 'play' }),
  definition('organise.preview', 'library', 'organise.preview', { icon: 'eye' }),
  definition('organise.move', 'library', 'files.move', { icon: 'organise' }),
  definition('people.refresh', 'view', 'people.refresh', { icon: 'refresh' }),
  definition('faces.reject', 'media', 'commands.rejectFaces', { icon: 'user-remove' }),
  definition('faces.cover', 'media', 'faces.cover', { icon: 'face' }),
  definition('faces.block', 'media', 'faces.block', { icon: 'blocked' }),
  definition('faces.clear', 'view', 'faces.clear', { icon: 'close' }),
  definition('faces.selectAll', 'view', 'faces.selectAll', { icon: 'check' }),
  definition('person.rename', 'view', 'person.rename', { icon: 'pencil' }),
  definition('person.review', 'view', 'person.review', { icon: 'face' }),
  definition('person.block', 'view', 'people.block', { icon: 'blocked' }),
  definition('places.refresh', 'view', 'places.refresh', { icon: 'refresh' }),
  definition('blocked.lock', 'view', 'blocked.lock', { icon: 'lock' }),
  definition('blocked.covered.back', 'view', 'covered.back', { icon: 'arrow-left' }),
  definition('blocked.covered.trash', 'media', 'trash.move', { icon: 'trash' }),
  definition('blocked.covered.clear', 'view', 'blocked.clearSelection', {}),
  definition('settings.addFolder', 'library', 'settings.addFolder', { icon: 'folder-add' }),
  definition('settings.rescan', 'library', 'settings.rescan', { icon: 'refresh' }),
  definition('settings.xmpExport', 'library', 'settings.xmpExport', { icon: 'sidecar' }),
  definition('settings.xmpDelete', 'library', 'settings.xmpDelete', { icon: 'recycle' }),
  definition('things.create', 'library', 'things.find', { icon: 'plus' }),
  definition('trash.restoreAll', 'library', 'trash.restoreAll', { icon: 'restore' }),
  definition('trash.deleteAll', 'library', 'trash.deleteAll', { icon: 'recycle', danger: true }),
  definition('trash.restore', 'media', 'trash.restore', { icon: 'restore' }),
  definition('trash.recycle', 'media', 'trash.deleteFromDisk', { icon: 'recycle', danger: true }),
] as const

const navigationGroups = [...new Set(routes.flatMap((route) => ('group' in route.meta ? [route.meta.group] : [])))]
const navigationDefinitions = navigationGroups.map((name) =>
  definition(`go.${name}` as const, 'navigate', name === 'compression' ? 'compression.title' : `nav.${name}`, {}),
)
export const staticCommands = [
  ...definitions,
  ...navigationDefinitions,
  definition('go.videos', 'navigate', 'commands.showVideos', { icon: 'video' }),
] as const
export type StaticCommand = (typeof staticCommands)[number]
export type StaticCommandId = StaticCommand['id']
export type ShortcutCommandId = Extract<StaticCommand, { shortcut: ShortcutDefaults }>['id']
export type CompressionCommandId = Extract<StaticCommand, { compression: true }>['id']
const staticById: ReadonlyMap<string, StaticCommand> = new Map(staticCommands.map((command) => [command.id, command]))

function numeric<
  const N extends string,
  const P extends string,
  const S extends string,
  const M extends Omit<CommandMetadata, 'group' | 'label' | 'shortcut' | 'zone'>,
>(name: N, prefix: P, suffix: S, group: CommandGroup, metadata: M) {
  return { name, prefix, suffix, group, ...metadata }
}
export const itemCommands = [
  numeric('album.edit', 'album.', '.edit', 'library', { icon: 'pencil' }),
  numeric('album.block', 'album.', '.block', 'library', { icon: 'eye-off' }),
  numeric('album.delete', 'album.', '.delete', 'library', { icon: 'trash', danger: true }),
  numeric('event.rename', 'event.', '.rename', 'view', { icon: 'pencil' }),
  numeric('event.pin', 'event.', '.pin', 'view', { icon: 'pin' }),
  numeric('event.reset', 'event.', '.reset', 'view', { icon: 'reset' }),
  numeric('event.block', 'event.', '.block', 'view', { icon: 'eye-off' }),
  numeric('organise.resume', 'organise.resume.', '', 'library', { icon: 'play' }),
  numeric('organise.undo', 'organise.undo.', '', 'library', { icon: 'undo' }),
  numeric('person.rename', 'person.', '.rename', 'view', { icon: 'pencil' }),
  numeric('person.review', 'person.', '.review', 'view', { icon: 'face' }),
  numeric('person.hide', 'person.', '.hide', 'view', { icon: 'eye-off' }),
  numeric('person.block', 'person.', '.block', 'view', { icon: 'blocked', danger: true }),
  numeric('place.preview', 'place.', '.preview', 'view', { icon: 'photos' }),
  numeric('place.block', 'place.', '.block', 'view', { icon: 'eye-off' }),
  numeric('blocked.mode', 'blocked.mode.', '', 'library', { icon: 'refresh' }),
  numeric('blocked.show', 'blocked.show.', '', 'view', { icon: 'eye' }),
  numeric('blocked.unblock', 'blocked.unblock.', '', 'library', { icon: 'unlock' }),
  numeric('thing.pin', 'thing.', '.pin', 'view', { icon: 'pin' }),
  numeric('thing.rename', 'thing.', '.rename', 'view', { icon: 'pencil' }),
  numeric('thing.reset', 'thing.', '.reset', 'view', { icon: 'reset' }),
  numeric('thing.block', 'thing.', '.block', 'view', { icon: 'eye-off' }),
  numeric('thing.delete', 'thing.', '.delete', 'view', { icon: 'trash', danger: true }),
] as const
export type ItemCommand = (typeof itemCommands)[number]
export type ItemCommandName = ItemCommand['name']
export interface ItemCommandId {
  readonly name: ItemCommandName
  readonly id: number
}
const itemsByName: ReadonlyMap<string, ItemCommand> = new Map(itemCommands.map((command) => [command.name, command]))
const duplicateKinds = [DuplicateKind.DuplicateExact, DuplicateKind.DuplicateSimilar] as const
export type CommandDuplicateKind = (typeof duplicateKinds)[number]
export interface DuplicateCommandId {
  readonly name: 'duplicates.review'
  readonly kind: CommandDuplicateKind
  readonly id: number
}
const duplicateMetadata: CommandMetadata = { group: 'library', icon: 'duplicates' }
export type CommandId = StaticCommandId | ItemCommandId | DuplicateCommandId
export interface CommandReference {
  readonly id: CommandId
}
export class InvalidCommandIdentity extends Error {
  constructor() {
    super('Invalid command identity')
  }
}

function readID(value: unknown): number | undefined {
  try {
    return positiveID(value)
  } catch (error) {
    if (!(error instanceof InvalidNavigation)) throw error
    return undefined
  }
}
export function isDuplicateCommandKind(value: unknown): value is CommandDuplicateKind {
  return duplicateKinds.some((kind) => kind === value)
}
export function itemCommand(name: ItemCommandName, id: number): ItemCommandId {
  const parsed = typeof id === 'number' ? readID(id) : undefined
  if (!itemsByName.has(name) || parsed === undefined) throw new InvalidCommandIdentity()
  return Object.freeze({ name, id: parsed })
}
export function duplicateCommand(kind: CommandDuplicateKind, id: number): DuplicateCommandId {
  const parsed = typeof id === 'number' ? readID(id) : undefined
  if (!isDuplicateCommandKind(kind) || parsed === undefined) throw new InvalidCommandIdentity()
  return Object.freeze({ name: 'duplicates.review' as const, kind, id: parsed })
}
export function isStaticCommandId(value: unknown): value is StaticCommandId {
  return typeof value === 'string' && staticById.has(value)
}
export function staticCommand(
  id: StaticCommandId,
): CommandMetadata & { readonly id: StaticCommandId; readonly label: SimpleMessageKey } {
  const command = staticById.get(id)
  if (!command) throw new InvalidCommandIdentity()
  return command
}
interface ResolvedCommand {
  readonly id: CommandId
  readonly key: string
  readonly name: string
  readonly metadata: CommandMetadata
}
/** Validate programmatic identities before lookup, without echoing rejected values. */
export function resolveCommand(value: unknown): ResolvedCommand {
  if (isStaticCommandId(value)) return { id: value, key: value, name: value, metadata: staticCommand(value) }
  if (
    typeof value !== 'object' ||
    value === null ||
    !('name' in value) ||
    typeof value.name !== 'string' ||
    !('id' in value) ||
    typeof value.id !== 'number'
  )
    throw new InvalidCommandIdentity()
  const id = readID(value.id)
  if (id === undefined) throw new InvalidCommandIdentity()
  if (
    value.name === 'duplicates.review' &&
    'kind' in value &&
    isDuplicateCommandKind(value.kind) &&
    Object.keys(value).every((key) => ['name', 'kind', 'id'].includes(key))
  ) {
    return {
      id: duplicateCommand(value.kind, id),
      key: `duplicates.review.${value.kind}.${id}`,
      name: value.name,
      metadata: duplicateMetadata,
    }
  }
  const command = itemsByName.get(value.name)
  if (!command || Object.keys(value).some((key) => !['name', 'id'].includes(key))) throw new InvalidCommandIdentity()
  return {
    id: itemCommand(command.name, id),
    key: `${command.prefix}${id}${command.suffix}`,
    name: command.name,
    metadata: command,
  }
}
export function commandKey(id: CommandId): string {
  return resolveCommand(id).key
}
/** The palette's untrusted row key is an explicit input boundary, never an execution overload. */
export function commandIdFromKey(value: unknown): CommandId | undefined {
  if (isStaticCommandId(value)) return value
  if (typeof value !== 'string') return
  for (const definition of itemCommands) {
    if (!value.startsWith(definition.prefix) || !value.endsWith(definition.suffix)) continue
    const id = readID(value.slice(definition.prefix.length, value.length - definition.suffix.length))
    if (id === undefined) continue
    const target = itemCommand(definition.name, id)
    if (commandKey(target) === value) return target
  }
  for (const kind of duplicateKinds) {
    const prefix = `duplicates.review.${kind}.`
    if (!value.startsWith(prefix)) continue
    const id = readID(value.slice(prefix.length))
    if (id === undefined) continue
    const target = duplicateCommand(kind, id)
    if (commandKey(target) === value) return target
  }
}
