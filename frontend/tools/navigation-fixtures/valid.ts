import { destination } from '../../src/shared/navigation/contract'
import type { Navigation } from '../../src/shared/navigation/router'

export function navigate(navigation: Navigation) {
  void navigation.push(destination({ name: 'person', params: { id: 1 }, query: { media: 'photos' } }))
  void navigation.replace(destination({ name: 'settings', query: { section: 'privacy' } }))
  return navigation.resolve(destination({ name: 'search', query: { q: '海 کی شام', favourites: true, place: 4 } }))
}
