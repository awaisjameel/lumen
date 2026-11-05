// The app's navigation: one list, read by the sidebar, the drawer and the
// command palette, so a destination has one name, icon and place. Videos
// have no destination of their own: Library's filter shows them, and
// /videos (router.ts) opens Library on it.
import { destination } from '../shared/navigation/contract'
import type { Destination } from '../shared/navigation/contract'
import type { IconName } from '../design/icons'
import type { MessageKey } from '../shared/i18n'

export interface NavEntry {
  to: Destination
  label: MessageKey
  icon: IconName
  /**
   * library: the whole timeline. collections: what Lumen groups it into.
   * tools: what you tidy with. app: Lumen itself.
   */
  section: NavSection
}

export type NavSection = 'library' | 'collections' | 'tools' | 'app'

/** The sidebar's groups in order, each with its heading (the first has none). */
export const navSections: readonly { section: NavSection; label?: MessageKey }[] = [
  { section: 'library' },
  { section: 'collections', label: 'nav.group.collections' },
  { section: 'tools', label: 'nav.group.tools' },
]

export const navEntries: readonly NavEntry[] = [
  { to: destination({ name: 'library' }), label: 'nav.library', icon: 'photos', section: 'library' },
  { to: destination({ name: 'favourites' }), label: 'nav.favourites', icon: 'heart', section: 'library' },
  { to: destination({ name: 'people' }), label: 'nav.people', icon: 'people', section: 'collections' },
  { to: destination({ name: 'things' }), label: 'nav.things', icon: 'things', section: 'collections' },
  { to: destination({ name: 'places' }), label: 'nav.places', icon: 'places', section: 'collections' },
  { to: destination({ name: 'events' }), label: 'nav.events', icon: 'events', section: 'collections' },
  { to: destination({ name: 'albums' }), label: 'nav.albums', icon: 'albums', section: 'collections' },
  { to: destination({ name: 'duplicates' }), label: 'nav.duplicates', icon: 'duplicates', section: 'tools' },
  { to: destination({ name: 'compression' }), label: 'compression.title', icon: 'compress', section: 'tools' },
  { to: destination({ name: 'trash' }), label: 'nav.trash', icon: 'trash', section: 'tools' },
  { to: destination({ name: 'settings' }), label: 'nav.settings', icon: 'settings', section: 'app' },
]

/** A detail inherits its declared parent section, independently of its path. */
export function isActive(entry: NavEntry, target: Destination | undefined): boolean {
  return !!target && 'group' in target.meta && target.meta.group === entry.to.name
}
