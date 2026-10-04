import { createRouter, createWebHistory } from 'vue-router'
import { Filter } from './bridge'
import LibraryView from './library/LibraryView.vue'

// The grid is where Lumen starts, so it is part of the first script. Every
// other screen is loaded when it is first opened, from the app's own
// embedded files, which keeps the start quick as screens are added.
const PeopleView = () => import('./people/PeopleView.vue')
const PersonFaces = () => import('./people/PersonFaces.vue')
const SettingsView = () => import('./settings/SettingsView.vue')
const BlockedView = () => import('./settings/BlockedView.vue')
const ThingsView = () => import('./things/ThingsView.vue')
const AlbumsView = () => import('./albums/AlbumsView.vue')
const DuplicatesView = () => import('./duplicates/DuplicatesView.vue')
const PlacesView = () => import('./places/PlacesView.vue')
const EventsView = () => import('./events/EventsView.vue')
const OrganiseView = () => import('./organise/OrganiseView.vue')

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
    // The component showcase is a development tool. DEV is a build-time
    // constant, so a production build drops this branch and never emits the
    // showcase: the shipped app has no such route.
    ...(import.meta.env.DEV
      ? [{ path: '/__showcase', name: 'showcase', component: () => import('./app/showcase/Showcase.vue') }]
      : []),
    { path: '/:rest(.*)*', redirect: '/' },
  ],
})
