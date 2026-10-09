// The app's navigation: one list, read by the sidebar, the drawer and the
// command palette, so a destination has one name, icon and place.
import type { IconName } from '../design/icons'
import type { MessageKey } from '../shared/i18n'

export interface NavEntry {
  /** The route's name (router.ts). */
  name: string
  path: string
  label: MessageKey
  icon: IconName
  /** library: what you browse. tools: what you tidy with. app: Lumen itself. */
  section: 'library' | 'tools' | 'app'
  /** Shown only while videos are indexed. */
  videos?: boolean
}

export const navEntries: readonly NavEntry[] = [
  { name: 'library', path: '/', label: 'nav.library', icon: 'photos', section: 'library' },
  { name: 'favourites', path: '/favourites', label: 'nav.favourites', icon: 'heart', section: 'library' },
  { name: 'videos', path: '/videos', label: 'nav.videos', icon: 'video', section: 'library', videos: true },
  { name: 'people', path: '/people', label: 'nav.people', icon: 'people', section: 'library' },
  { name: 'things', path: '/things', label: 'nav.things', icon: 'things', section: 'library' },
  { name: 'places', path: '/places', label: 'nav.places', icon: 'places', section: 'library' },
  { name: 'events', path: '/events', label: 'nav.events', icon: 'events', section: 'library' },
  { name: 'albums', path: '/albums', label: 'nav.albums', icon: 'albums', section: 'library' },
  { name: 'duplicates', path: '/duplicates', label: 'nav.duplicates', icon: 'duplicates', section: 'tools' },
  { name: 'compression', path: '/compression', label: 'compression.title', icon: 'files', section: 'tools' },
  { name: 'trash', path: '/trash', label: 'nav.trash', icon: 'trash', section: 'tools' },
  { name: 'settings', path: '/settings', label: 'nav.settings', icon: 'settings', section: 'app' },
]

/** Whether path is entry's page or one under it ("/people/3" is under People). */
export function isActive(entry: NavEntry, path: string): boolean {
  return entry.path === '/' ? path === '/' : path === entry.path || path.startsWith(`${entry.path}/`)
}
