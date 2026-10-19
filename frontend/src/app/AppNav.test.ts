import { expect, it } from 'vitest'
import { createPinia } from 'pinia'
import { flushPromises } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { useLibrary } from '../library/store'
import { mountInApp } from '../shared/testing'
import AppNav from './AppNav.vue'

it('shows current engine counts, including zero, for the sidebar destinations', async () => {
  const pinia = createPinia()
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/:path(.*)*', component: { template: '<div />' } }],
  })
  await router.push('/')
  const wrapper = mountInApp(AppNav, { plugins: [pinia, router], stubs: { IndexStatus: true } })
  const lib = useLibrary(pinia)
  lib.counts = { photos: 12480, videos: 1104, favourites: 248, people: 38, trash: 0 }
  lib.countsLoaded = true
  await flushPromises()
  expect(wrapper.get('a[href="/"]').text()).toContain('13,584')
  expect(wrapper.get('a[href="/favourites"]').text()).toContain('248')
  expect(wrapper.get('a[href="/people"]').text()).toContain('38')
  expect(wrapper.get('a[href="/trash"]').text()).toContain('0')
  lib.counts = { ...lib.counts, trash: 6 }
  await flushPromises()
  expect(wrapper.get('a[href="/trash"]').text()).toContain('6')
  wrapper.unmount()
})
