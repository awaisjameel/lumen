import { createRouter, createWebHistory } from 'vue-router'
import { Filter } from './bridge'
import LibraryView from './library/LibraryView.vue'
import PeopleView from './people/PeopleView.vue'
import PersonFaces from './people/PersonFaces.vue'
import SettingsView from './settings/SettingsView.vue'
import BlockedView from './settings/BlockedView.vue'
import ThingsView from './things/ThingsView.vue'
import AlbumsView from './albums/AlbumsView.vue'
import DuplicatesView from './duplicates/DuplicatesView.vue'
import PlacesView from './places/PlacesView.vue'
import EventsView from './events/EventsView.vue'
import OrganiseView from './organise/OrganiseView.vue'

declare module 'vue-router' {
  interface RouteMeta {
    /** Timeline routes: which subset of the library the grid shows. */
    filter?: Filter
  }
}

// The grid (LibraryView) serves the timeline routes, one Thing (/things/:id),
// one person (/people/:id), one album (/albums/:id) and search results
// (/search?q=…); it reads which from the route.
export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', name: 'library', component: LibraryView, meta: { filter: Filter.FilterAll } },
    { path: '/favourites', name: 'favourites', component: LibraryView, meta: { filter: Filter.FilterFavourites } },
    { path: '/videos', name: 'videos', component: LibraryView, meta: { filter: Filter.FilterVideos } },
    { path: '/trash', name: 'trash', component: LibraryView, meta: { filter: Filter.FilterTrash } },
    { path: '/people', name: 'people', component: PeopleView },
    { path: '/people/:id(\\d+)', name: 'person', component: LibraryView, meta: { filter: Filter.FilterAll } },
    { path: '/people/:id(\\d+)/faces', name: 'personFaces', component: PersonFaces },
    { path: '/things', name: 'things', component: ThingsView },
    { path: '/things/:id(\\d+)', name: 'thing', component: LibraryView, meta: { filter: Filter.FilterAll } },
    { path: '/albums', name: 'albums', component: AlbumsView },
    { path: '/albums/:id(\\d+)', name: 'album', component: LibraryView, meta: { filter: Filter.FilterAll } },
    { path: '/places', name: 'places', component: PlacesView },
    { path: '/places/:id(\\d+)', name: 'place', component: LibraryView, meta: { filter: Filter.FilterAll } },
    { path: '/events', name: 'events', component: EventsView },
    { path: '/events/:id(\\d+)', name: 'event', component: LibraryView, meta: { filter: Filter.FilterAll } },
    { path: '/duplicates', name: 'duplicates', component: DuplicatesView },
    { path: '/search', name: 'search', component: LibraryView },
    { path: '/settings', name: 'settings', component: SettingsView },
    { path: '/settings/blocked', name: 'blocked', component: BlockedView },
    { path: '/settings/organise', name: 'organise', component: OrganiseView },
    { path: '/:rest(.*)*', redirect: '/' },
  ],
})
