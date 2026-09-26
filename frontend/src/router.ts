import { createRouter, createWebHistory } from 'vue-router'
import { Filter } from './bridge'
import LibraryView from './library/LibraryView.vue'
import SettingsView from './settings/SettingsView.vue'

declare module 'vue-router' {
  interface RouteMeta {
    /** Library routes: which subset of the library the grid shows. */
    filter?: Filter
  }
}

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', component: LibraryView, meta: { filter: Filter.FilterAll } },
    { path: '/favourites', component: LibraryView, meta: { filter: Filter.FilterFavourites } },
    { path: '/videos', component: LibraryView, meta: { filter: Filter.FilterVideos } },
    { path: '/settings', component: SettingsView },
    { path: '/:rest(.*)*', redirect: '/' },
  ],
})
