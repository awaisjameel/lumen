import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { engine, MAX_NAME_LEN, TagKind } from '../bridge'
import type * as Bridge from '../bridge'
import { showBlocksChanged } from '../shared/block'
import { mountInApp } from '../shared/testing'
import ThingCard from './ThingCard.vue'

let wrapper: ReturnType<typeof mountInApp> | undefined
vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  engine: { timeline: vi.fn(), days: vi.fn() },
}))
afterEach(() => {
  wrapper?.unmount()
  vi.unstubAllGlobals()
})
it('keeps an invalid draft editable, restores a blank name and uses a named cover link', async () => {
  const rename = vi.fn().mockResolvedValue(true)
  const tag = {
    id: 1,
    name: 'Dogs',
    original: '',
    kind: TagKind.TagBuiltin,
    count: 123456,
    cover: 0,
    coverThumb: 0,
    pinned: false,
  }
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div />' } },
      { name: 'thing', path: '/things/:id', component: { template: '<div />' } },
    ],
  })
  wrapper = mountInApp(ThingCard, { props: { tag, icon: 'things', deletable: false, rename }, plugins: [router] })
  expect(wrapper.get('a[aria-label="Dogs"]').attributes('href')).toBe('/things/1')
  expect(wrapper.text()).toContain('123,456')
  await wrapper.get('button[title="Rename “Dogs”"]').trigger('click')
  await flushPromises()
  await wrapper.get('input[aria-label="Thing name"]').setValue('😀'.repeat(MAX_NAME_LEN + 1))
  await wrapper.get('form').trigger('submit')
  expect(rename).not.toHaveBeenCalled()
  expect(wrapper.get('[role="alert"]').text()).toContain('80')
  await wrapper.get('input[aria-label="Thing name"]').setValue('')
  await wrapper.get('form').trigger('submit')
  await flushPromises()
  expect(rename).toHaveBeenCalledWith(1, '')
})

it('loads a visible Thing mosaic on demand and clears it immediately when blocks change', async () => {
  vi.stubGlobal(
    'IntersectionObserver',
    class {
      private readonly callback: (entries: { isIntersecting: boolean }[]) => void
      constructor(callback: (entries: { isIntersecting: boolean }[]) => void) {
        this.callback = callback
      }
      observe() {
        this.callback([{ isIntersecting: true }])
      }
      disconnect = vi.fn()
    },
  )
  vi.mocked(engine.timeline).mockResolvedValue([{ day: '2026-10-01', count: 3 }])
  vi.mocked(engine.days).mockResolvedValue([
    { day: '2026-10-01', items: [1, 2, 3].map((id) => ({ id, w: 100, h: 100, t: 1 })) },
  ])
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div />' } },
      { name: 'thing', path: '/things/:id', component: { template: '<div />' } },
    ],
  })
  wrapper = mountInApp(ThingCard, {
    plugins: [router, createPinia()],
    props: {
      tag: {
        id: 1,
        name: 'Dogs',
        kind: TagKind.TagBuiltin,
        count: 3,
        cover: 0,
        coverThumb: 0,
        pinned: false,
        original: '',
      },
      icon: 'things',
      deletable: false,
      rename: vi.fn(),
    },
  })
  await flushPromises()
  expect(engine.timeline).toHaveBeenCalledWith({ filter: '', tag: 1 }, expect.any(AbortSignal))
  expect(wrapper.findAll('a[aria-label="Dogs"] img')).toHaveLength(3)
  await showBlocksChanged()
  await flushPromises()
  expect(wrapper.findAll('a[aria-label="Dogs"] img')).toHaveLength(0)
})
