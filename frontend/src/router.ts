import { createRouter, createWebHistory } from 'vue-router'
import { Filter } from './bridge'
import LibraryView from './library/LibraryView.vue'
import PeopleView from './people/PeopleView.vue'
import PersonFaces from './people/PersonFaces.vue'
import SettingsView from './settings/SettingsView.vue'
import ThingsView from './things/ThingsView.vue'
import AlbumsView from './albums/AlbumsView.vue'

declare module 'vue-router' {
  interface RouteMeta {
    /** Timeline routes: which subset of the library the grid shows. */
    filter?: Filter
  }
}

// The grid (LibraryView) serves the timeline routes, one Thing (/things/:id),
// one person (/people/:id) and search results (/search?q=…); it reads which
// from the route.
export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', name: 'library', component: LibraryView, meta: { filter: Filter.FilterAll } },
    { path: '/favourites', name: 'favourites', component: LibraryView, meta: { filter: Filter.FilterFavourites } },
    { path: '/videos', name: 'videos', component: LibraryView, meta: { filter: Filter.FilterVideos } },
    { path: '/people', name: 'people', component: PeopleView },
    { path: '/people/:id(\\d+)', name: 'person', component: LibraryView, meta: { filter: Filter.FilterAll } },
    { path: '/people/:id(\\d+)/faces', name: 'personFaces', component: PersonFaces },
    { path: '/things', name: 'things', component: ThingsView },
    { path: '/things/:id(\\d+)', name: 'thing', component: LibraryView, meta: { filter: Filter.FilterAll } },
    { path: '/albums', name: 'albums', component: AlbumsView },
    { path: '/albums/:id(\\d+)', name: 'album', component: LibraryView, meta: { filter: Filter.FilterAll } },
    { path: '/search', name: 'search', component: LibraryView },
    { path: '/settings', name: 'settings', component: SettingsView },
    { path: '/:rest(.*)*', redirect: '/' },
  ],
})
