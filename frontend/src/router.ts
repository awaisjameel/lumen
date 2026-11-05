import { createWebHistory } from 'vue-router'
import LibraryView from './library/LibraryView.vue'
import { reportUnexpected } from './shared/errors'
import { createNavigation } from './shared/navigation/router'
import type { NavigationView } from './shared/navigation/router'
import type { ViewName } from './shared/navigation/contract'

// The first grid is eager. Every other production view stays in an embedded,
// lazy chunk; adding a view identity requires an explicit binding here.
const views: Record<Exclude<ViewName, 'showcase'>, NavigationView> = {
  library: LibraryView,
  people: () => import('./people/PeopleView.vue'),
  personFaces: () => import('./people/PersonFaces.vue'),
  things: () => import('./things/ThingsView.vue'),
  albums: () => import('./albums/AlbumsView.vue'),
  places: () => import('./places/PlacesView.vue'),
  events: () => import('./events/EventsView.vue'),
  duplicates: () => import('./duplicates/DuplicatesView.vue'),
  compression: () => import('./compression/CompressionView.vue'),
  settings: () => import('./settings/SettingsView.vue'),
  blocked: () => import('./settings/BlockedView.vue'),
  organise: () => import('./organise/OrganiseView.vue'),
}
export const router = createNavigation({
  history: createWebHistory(),
  development: import.meta.env.DEV,
  view(name) {
    if (name !== 'showcase') return views[name]
    // A production build must not emit the showcase chunk.
    if (import.meta.env.DEV) return () => import('./app/showcase/Showcase.vue')
    throw new Error('Development navigation is unavailable')
  },
  failure: (error) => reportUnexpected(error, 'navigation'),
})
