import { createRouter, createWebHistory } from 'vue-router'
import { Filter } from './bridge'
import LibraryView from './library/LibraryView.vue'
import SettingsView from './settings/SettingsView.vue'
import ThingsView from './things/ThingsView.vue'

declare module 'vue-router' {
  interface RouteMeta {
    /** Timeline routes: which subset of the library the grid shows. */
    filter?: Filter
  }
}

// The grid (LibraryView) serves the timeline routes, one Thing (/things/:id)
// and search results (/search?q=…); it reads which from the route.
export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', name: 'library', component: LibraryView, meta: { filter: Filter.FilterAll } },
    { path: '/favourites', name: 'favourites', component: LibraryView, meta: { filter: Filter.FilterFavourites } },
    { path: '/videos', name: 'videos', component: LibraryView, meta: { filter: Filter.FilterVideos } },
    { path: '/things', name: 'things', component: ThingsView },
    { path: '/things/:id(\\d+)', name: 'thing', component: LibraryView, meta: { filter: Filter.FilterAll } },
    { path: '/search', name: 'search', component: LibraryView },
    { path: '/settings', name: 'settings', component: SettingsView },
    { path: '/:rest(.*)*', redirect: '/' },
  ],
})
