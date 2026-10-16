import { createMemoryHistory, createRouter } from 'vue-router'
import type { Face, Person } from '../bridge'

export const faceFixture: Face = {
  id: 11,
  assetId: 2,
  thumb: 0,
  assetW: 400,
  assetH: 300,
  video: false,
  frame: 0,
  x: 0.2,
  y: 0.2,
  w: 0.1,
  h: 0.1,
  person: 1,
  name: 'Ali',
  byUser: false,
}
export const personFixture = (id = 1, name = 'Ali', count = 1): Person => ({ id, name, count, cover: faceFixture })
export function peopleRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div />' } },
      ...['person', 'people', 'personFaces', 'thing', 'event', 'place', 'album', 'blocked', 'settings'].map((name) => ({
        name,
        path: `/${name}/:id?`,
        component: { template: '<div />' },
      })),
    ],
  })
}
