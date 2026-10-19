// The app's navigation: one list, read by the sidebar, the drawer and the
// command palette, so a destination has one name, icon and place. Videos
// have no destination of their own: Library's filter shows them, and
// /videos (router.ts) opens Library on it.
import type { IconName } from '../design/icons'
import type { MessageKey } from '../shared/i18n'

export interface NavEntry {
  /** The route's name (router.ts). */
  name: string
  path: string
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
  { name: 'library', path: '/', label: 'nav.library', icon: 'photos', section: 'library' },
  { name: 'favourites', path: '/favourites', label: 'nav.favourites', icon: 'heart', section: 'library' },
  { name: 'people', path: '/people', label: 'nav.people', icon: 'people', section: 'collections' },
  { name: 'things', path: '/things', label: 'nav.things', icon: 'things', section: 'collections' },
  { name: 'places', path: '/places', label: 'nav.places', icon: 'places', section: 'collections' },
  { name: 'events', path: '/events', label: 'nav.events', icon: 'events', section: 'collections' },
  { name: 'albums', path: '/albums', label: 'nav.albums', icon: 'albums', section: 'collections' },
  { name: 'duplicates', path: '/duplicates', label: 'nav.duplicates', icon: 'duplicates', section: 'tools' },
  { name: 'compression', path: '/compression', label: 'compression.title', icon: 'compress', section: 'tools' },
  { name: 'trash', path: '/trash', label: 'nav.trash', icon: 'trash', section: 'tools' },
  { name: 'settings', path: '/settings', label: 'nav.settings', icon: 'settings', section: 'app' },
]

/** Whether path is entry's page or one under it ("/people/3" is under People). */
export function isActive(entry: NavEntry, path: string): boolean {
  return entry.path === '/' ? path === '/' : path === entry.path || path.startsWith(`${entry.path}/`)
}
