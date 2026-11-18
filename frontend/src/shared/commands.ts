// One command owner serves controls, menus, palette and keyboard. Features
// retain contextual predicates and accepted work; the registry owns admission.
import type { NavigationMenuItem as DropdownMenuItem } from './navigation/menu'
import { computed, shallowRef, toValue, watch } from 'vue'
import type { MaybeRefOrGetter } from 'vue'
import { icons } from '../design/icons'
import type { IconName } from '../design/icons'
import { errorCode, isCancelled } from '../bridge'
import { reportError, reportUnexpected } from './errors'
import { t } from './i18n'
import { isTyping } from './keys'
import { shortcutsSuspended } from './overlay'
import { commandKey, isStaticCommandId, resolveCommand, staticCommand } from './commandContract'
import type {
  CommandGroup,
  CommandId,
  CommandMetadata,
  CommandReference,
  CommandZone,
  DuplicateCommandId,
  ItemCommandId,
  StaticCommandId,
} from './commandContract'
export {
  commandGroups,
  commandZones,
  commandKey,
  itemCommand,
  duplicateCommand,
  commandIdFromKey,
} from './commandContract'
export type { CommandGroup, CommandId, CommandReference, CommandZone } from './commandContract'

interface Binding {
  readonly title?: () => string
  readonly icon?: IconName
  readonly available?: () => boolean
  readonly enabled?: () => boolean
  readonly reason?: () => string | undefined
  readonly danger?: boolean
  /** Resolving may mean work was accepted; features own form-level outcomes and long jobs. */
  readonly run: () => unknown
}
export type Command = Binding &
  ({ readonly id: StaticCommandId } | { readonly id: ItemCommandId | DuplicateCommandId; readonly title: () => string })
/** Read-only presentation deliberately exposes no raw execution callback. */
export interface ExecutableCommand extends CommandReference {
  readonly key: string
  readonly group: CommandGroup
  readonly icon?: IconName
  readonly danger?: boolean
  readonly keys?: string
  readonly altKeys?: readonly string[]
  readonly zone?: CommandZone
  readonly title: () => string
  readonly available: () => boolean
  readonly enabled: () => boolean
  readonly reason: () => string | undefined
}
interface Entry {
  readonly token: symbol
  readonly id: CommandId
  readonly key: string
  readonly name: string
  readonly metadata: CommandMetadata
  readonly binding: Command
  readonly view: ExecutableCommand
}
const registered = shallowRef<ReadonlyMap<string, Entry>>(new Map())
const pending = shallowRef<ReadonlyMap<symbol, number>>(new Map())
const executing = new Set<symbol>()
const overrides = shallowRef<ReadonlyMap<string, string>>(new Map())

/** Stored unknown keys remain in preferences but cannot become active commands. */
export function setShortcutOverrides(shortcuts: Readonly<Record<string, string>>): void {
  overrides.value = new Map(
    Object.entries(shortcuts).filter(
      ([id, keys]) => isStaticCommandId(id) && !staticCommand(id).shortcut?.locked && validShortcut(keys),
    ),
  )
}
export function keysOf(command: CommandReference): string | undefined {
  const resolved = resolveCommand(command.id)
  const own = overrides.value.get(resolved.key)
  return own === undefined ? resolved.metadata.shortcut?.keys : own || undefined
}
export function altKeysOf(command: CommandReference): readonly string[] {
  const resolved = resolveCommand(command.id)
  return overrides.value.has(resolved.key) ? [] : (resolved.metadata.shortcut?.altKeys ?? [])
}
export const commands = computed(() => [...registered.value.values()].map((entry) => entry.view))
export const availableCommands = computed(() =>
  [...registered.value.values()].filter(available).map((entry) => entry.view),
)
function available(entry: Entry): boolean {
  return entry.binding.available?.() ?? true
}
function enabled(entry: Entry): boolean {
  return (
    available(entry) &&
    (entry.binding.enabled?.() ?? true) &&
    (entry.metadata.repeatable === true || !pending.value.has(entry.token))
  )
}
export function isEnabled(command: CommandReference): boolean {
  const entry = registered.value.get(commandKey(command.id))
  return entry !== undefined && enabled(entry)
}
function reason(entry: Entry): string | undefined {
  return pending.value.has(entry.token) && !entry.metadata.repeatable ? t('commands.running') : entry.binding.reason?.()
}
function fallback(metadata: CommandMetadata): string {
  return t(metadata.label ?? 'commands.unavailable')
}
function entryFor(binding: Command): Entry {
  const resolved = resolveCommand(binding.id)
  const { key, metadata, id } = resolved
  const current = () => registered.value.get(key)
  const view: ExecutableCommand = {
    id,
    key,
    group: metadata.group,
    zone: metadata.zone,
    icon: binding.icon ?? metadata.icon,
    danger: binding.danger ?? metadata.danger,
    keys: metadata.shortcut?.keys,
    altKeys: metadata.shortcut?.altKeys,
    title: () => current()?.binding.title?.() ?? fallback(metadata),
    available: () => {
      const entry = current()
      return entry !== undefined && available(entry)
    },
    enabled: () => {
      const entry = current()
      return entry !== undefined && enabled(entry)
    },
    reason: () => {
      const entry = current()
      return entry ? reason(entry) : undefined
    },
  }
  return { ...resolved, binding, token: Symbol('command registration'), view }
}
/** Newest registration wins; an older disposer can remove only its own token. */
export function registerCommands(bindings: readonly Command[]): () => void {
  const entries = bindings.map(entryFor)
  const unique = new Set(entries.map((entry) => entry.key))
  if (unique.size !== entries.length) throw new Error('Duplicate command registration')
  const next = new Map(registered.value)
  const waiting = new Map(pending.value)
  for (const entry of entries) {
    const previous = next.get(entry.key)
    if (previous) waiting.delete(previous.token)
    next.delete(entry.key)
    next.set(entry.key, entry)
  }
  registered.value = next
  pending.value = waiting
  return () => {
    const remaining = new Map(registered.value)
    const waiting = new Map(pending.value)
    let changed = false
    for (const entry of entries) {
      if (remaining.get(entry.key)?.token === entry.token) {
        remaining.delete(entry.key)
        changed = true
      }
      waiting.delete(entry.token)
    }
    if (changed) registered.value = remaining
    if (waiting.size !== pending.value.size) pending.value = waiting
  }
}
/** Reactive item sources retire stale bindings before another activation can run. */
export function useCommands(bindings: MaybeRefOrGetter<readonly Command[]>): void {
  watch(
    () => toValue(bindings),
    (current, _old, cleanup) => cleanup(registerCommands(current)),
    { immediate: true, flush: 'sync' },
  )
}
export function useItemCommands<T, K extends string | number>(
  items: MaybeRefOrGetter<readonly T[]>,
  key: (item: T) => K,
  create: (item: T) => readonly Command[],
): (id: K) => DropdownMenuItem[] {
  const groups = computed(() => new Map(toValue(items).map((item) => [key(item), create(item)])))
  useCommands(() => [...groups.value.values()].flat())
  return (id) => menuItems(groups.value.get(id) ?? [])
}
export function menuItems(references: readonly CommandReference[]): DropdownMenuItem[] {
  return references.flatMap((reference) => {
    const entry = registered.value.get(commandKey(reference.id))
    if (!entry || !available(entry)) return []
    const command = entry.view
    return [
      {
        label: command.title(),
        icon: command.icon ? icons[command.icon] : undefined,
        disabled: !enabled(entry),
        color: command.danger ? ('error' as const) : undefined,
        description: enabled(entry) ? undefined : reason(entry),
        onSelect: () => runCommand(command.id),
      },
    ]
  })
}
export function commandById(id: CommandId): ExecutableCommand | undefined {
  return registered.value.get(commandKey(id))?.view
}
export function commandState(id: CommandId) {
  const resolved = resolveCommand(id)
  const entry = registered.value.get(resolved.key)
  const icon = entry?.view.icon ?? resolved.metadata.icon
  return {
    title: entry?.view.title() ?? fallback(resolved.metadata),
    icon: icon ? icons[icon] : undefined,
    enabled: entry !== undefined && enabled(entry),
    busy: entry !== undefined && pending.value.has(entry.token) && !entry.metadata.repeatable,
    reason: entry ? reason(entry) : t('commands.unavailable'),
    caps: capsOf(id),
  }
}
function waiting(entry: Entry, change: 1 | -1) {
  const next = new Map(pending.value)
  const count = (next.get(entry.token) ?? 0) + change
  if (count > 0 && registered.value.get(entry.key) === entry) next.set(entry.token, count)
  else next.delete(entry.token)
  pending.value = next
}
function promiseLike(value: unknown): value is PromiseLike<unknown> {
  return (
    ((typeof value === 'object' && value !== null) || typeof value === 'function') &&
    'then' in value &&
    typeof value.then === 'function'
  )
}
/** Invalid identities reject; legitimate absence/disabled work and reported failures resolve false. */
export async function runCommand(id: CommandId): Promise<boolean> {
  const key = commandKey(id)
  const entry = registered.value.get(key)
  if (!entry) return false
  let asynchronous = false
  let entered = false
  try {
    if (
      !enabled(entry) ||
      (!entry.metadata.repeatable && executing.has(entry.token)) ||
      registered.value.get(key) !== entry
    )
      return false
    executing.add(entry.token)
    entered = true
    const result = entry.binding.run()
    if (promiseLike(result)) {
      asynchronous = true
      waiting(entry, 1)
      await result
    }
    return true
  } catch (error) {
    if (!isCancelled(error)) {
      if (errorCode(error)) reportError(error)
      else reportUnexpected(error, `command:${entry.name}`)
    }
    return false
  } finally {
    if (entered) executing.delete(entry.token)
    if (asynchronous) waiting(entry, -1)
  }
}
// Item commands have no persistent shortcuts; avoid scanning a large collection on every key press.
const shortcutCommands = computed(() =>
  [...registered.value.values()].filter(
    (entry) =>
      typeof entry.id === 'string' && (entry.metadata.shortcut !== undefined || overrides.value.has(entry.key)),
  ),
)

interface Chord {
  key: string
  ctrl: boolean
  shift: boolean
  alt: boolean
}

// The space bar types " ", which a shortcut cannot show: it is written "Space".
const keyName = (key: string): string => (key === ' ' ? 'Space' : key)

function shiftMatters(key: string): boolean {
  return key.length > 1 || /[a-z0-9]/i.test(key)
}

// core.ValidShortcut's persisted grammar; shared fixtures check both implementations.
const keyGrammar = /^(Ctrl\+)?(Shift\+)?(Alt\+)?([A-Za-z0-9]{1,12}|[^\sA-Za-z0-9+])$/u
function parseKeys(keys: string): Chord | undefined {
  if (!keyGrammar.test(keys)) return undefined
  const parts = keys.split('+')
  return {
    key: (parts.at(-1) ?? '').toLowerCase(),
    ctrl: parts.includes('Ctrl'),
    shift: shiftMatters(parts.at(-1) ?? '') && parts.includes('Shift'),
    alt: parts.includes('Alt'),
  }
}

/** The shortcut's keys as separate caps, for display: "Ctrl+K" gives ["Ctrl", "K"]. */
export function keyCaps(keys: string): string[] {
  return keys.split('+')
}

/** The caps of a registered command's shortcut in force, for a tooltip; undefined if it has none. */
export function capsOf(id: CommandId): string[] | undefined {
  const c = commandById(id)
  const keys = c ? keysOf(c) : undefined
  return keys ? keyCaps(keys) : undefined
}

function matches(e: KeyboardEvent, keys: string): boolean {
  const chord = parseKeys(keys)
  // A character's Shift is part of the character ("?"), so only named keys
  // and letters compare it.
  if (!chord) return false
  return (
    keyName(e.key).toLowerCase() === chord.key &&
    (e.ctrlKey || e.metaKey) === chord.ctrl &&
    e.altKey === chord.alt &&
    (!shiftMatters(chord.key) || e.shiftKey === chord.shift)
  )
}

function matchesKeys(e: KeyboardEvent, keys: string | undefined): boolean {
  return keys !== undefined && matches(e, keys)
}

// Keys that only modify another, which are no shortcut by themselves.
const MODIFIERS: readonly string[] = ['Control', 'Shift', 'Alt', 'Meta', 'AltGraph', 'OS']
// Keys never assigned (core.ValidShortcut): they leave a dialog, the viewer
// or a field whatever else is set; "+" joins a shortcut's parts.
const RESERVED: readonly string[] = ['escape', 'tab', '+']

/** Whether stored key text is assignable (core.ValidShortcut); defaults may still own Escape. */
export function validShortcut(value: unknown): value is string {
  if (typeof value !== 'string') return false
  if (value === '') return true
  const chord = parseKeys(value)
  return chord !== undefined && !RESERVED.includes(chord.key) && value.toLowerCase() !== 'alt+f4'
}

/**
 * The shortcut a key press spells, as it is stored and shown ("Ctrl+Shift+K"):
 * "" while only modifiers are down, null for a key that cannot be assigned.
 */
export function chordOf(e: KeyboardEvent): string | null {
  if (MODIFIERS.includes(e.key)) return ''
  const name = keyName(e.key)
  if (RESERVED.includes(name.toLowerCase()) || /\s/.test(name) || name.length > 12) return null
  // A character already says whether Shift was down ("?"); a letter, a digit
  // and a named key do not.
  const keys = [
    e.ctrlKey || e.metaKey ? 'Ctrl' : '',
    e.shiftKey && shiftMatters(name) ? 'Shift' : '',
    e.altKey ? 'Alt' : '',
    name.length === 1 ? name.toUpperCase() : name,
  ]
    .filter(Boolean)
    .join('+')
  return validShortcut(keys) ? keys : null
}

/** Whether two shortcuts are the same keys, however their letters are cased. */
export function sameKeys(a: string, b: string): boolean {
  const x = parseKeys(a)
  const y = parseKeys(b)
  if (!x || !y) return false
  return x.key === y.key && x.ctrl === y.ctrl && x.shift === y.shift && x.alt === y.alt
}

/**
 * Whether two zones hear the same key presses: the same zone, or either is
 * the whole page. The grid and the viewer never do (one is behind the other).
 */
export function zonesOverlap(a: CommandZone | undefined, b: CommandZone | undefined): boolean {
  const x = a ?? 'global'
  const y = b ?? 'global'
  return x === y || x === 'global' || y === 'global'
}

/**
 * Runs the command whose shortcut e is, among those of zone, and says
 * whether one ran (the event is then spent). A zone's owner calls this from
 * its own keydown handler; the shell calls it for "global".
 */
export function dispatchKey(e: KeyboardEvent, zone: CommandZone): boolean {
  if (e.defaultPrevented || e.isComposing) return false
  const entry = shortcutCommands.value.find(
    (entry) =>
      (entry.metadata.zone ?? 'global') === zone &&
      available(entry) &&
      (matchesKeys(e, keysOf(entry.view)) || altKeysOf(entry.view).some((keys) => matches(e, keys))),
  )
  if (!entry) return false
  e.preventDefault()
  if (enabled(entry) && (!e.repeat || entry.metadata.repeatable)) void runCommand(entry.id)
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
