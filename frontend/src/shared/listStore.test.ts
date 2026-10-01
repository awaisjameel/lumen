import { expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import { showBlocksChanged } from './block'
import { defineCollectionStore, defineListStore, useList } from './listStore'

it('supersedes an initial collection read when visibility changes', async () => {
  setActivePinia(createPinia())
  let finish!: (value: string[]) => void
  const fetch = vi
    .fn<() => Promise<string[]>>()
    .mockReturnValueOnce(new Promise((resolve) => (finish = resolve)))
    .mockResolvedValueOnce(['Visible'])
  const store = defineCollectionStore('private', fetch)()
  const initial = store.load()
  expect(store.loaded).toBe(false)
  expect(store.loading).toBe(true)
  const changed = store.invalidate()
  finish(['Private'])
  await Promise.all([initial, changed])
  expect(store.list).toEqual(['Visible'])
  expect(store.loaded).toBe(true)
  expect(store.loading).toBe(false)
  expect(fetch).toHaveBeenCalledTimes(2)
})

it('drops cached names and their id index while reloading after invalidation', async () => {
  setActivePinia(createPinia())
  let fail!: (reason: Error) => void
  const fetch = vi
    .fn<() => Promise<{ id: number; name: string }[]>>()
    .mockResolvedValueOnce([{ id: 1, name: 'Private' }])
    .mockReturnValueOnce(new Promise((_resolve, reject) => (fail = reject)))
  const store = defineListStore('cached', fetch)()
  await store.load()
  const changed = store.invalidate()
  expect(store.list).toEqual([])
  expect(store.byId.size).toBe(0)
  expect(store.loaded).toBe(false)
  const rejected = expect(changed).rejects.toThrow('reload failed')
  fail(new Error('reload failed'))
  await rejected
  expect(store.list).toEqual([])
  expect(store.loading).toBe(false)
})

it('does not read an unopened collection on invalidation', async () => {
  setActivePinia(createPinia())
  const fetch = vi.fn<() => Promise<string[]>>()
  await defineCollectionStore('unopened', fetch)().invalidate()
  expect(fetch).not.toHaveBeenCalled()
})

it('reloads after an edit during a pending list request', async () => {
  setActivePinia(createPinia())
  let resolve!: (value: { id: number; name: string }[]) => void
  const fetch = vi
    .fn<() => Promise<{ id: number; name: string }[]>>()
    .mockReturnValueOnce(
      new Promise((done) => {
        resolve = done
      }),
    )
    .mockResolvedValueOnce([{ id: 1, name: 'Updated' }])
  const list = defineListStore('test', fetch)()
  const first = list.load()
  const afterEdit = list.load()
  resolve([{ id: 1, name: 'Old' }])
  await Promise.all([first, afterEdit])
  expect(list.byId.get(1)?.name).toBe('Updated')
  expect(fetch).toHaveBeenCalledTimes(2)
})

it('retains a queued refresh when the older read fails', async () => {
  setActivePinia(createPinia())
  let fail!: (reason: Error) => void
  const fetch = vi
    .fn<() => Promise<{ id: number }[]>>()
    .mockReturnValueOnce(new Promise((_resolve, reject) => (fail = reject)))
    .mockResolvedValueOnce([{ id: 2 }])
  const list = defineListStore('retry', fetch)()
  const first = list.load()
  const latest = list.load()
  fail(new Error('interrupted read'))
  await Promise.all([first, latest])
  expect(list.list).toEqual([{ id: 2 }])
})

it('coalesces mounted list reloads and ignores pending work after unmount', async () => {
  let resolve!: (value: { id: number; name: string }[]) => void
  const fetch = vi
    .fn<() => Promise<{ id: number; name: string }[]>>()
    .mockReturnValueOnce(new Promise((done) => (resolve = done)))
  const component = defineComponent({
    setup: () => useList(fetch),
    render() {
      return h('p', this.list.map((item) => item.name).join(', '))
    },
  })
  const wrapper = mount(component)
  const state = wrapper.vm
  const latest = state.load()
  expect(fetch).toHaveBeenCalledOnce()
  wrapper.unmount()
  resolve([{ id: 1, name: 'Old' }])
  await latest
  await flushPromises()
  expect(fetch).toHaveBeenCalledOnce()
  expect(state.list).toEqual([])
  expect(state.loaded).toBe(false)
})

it('refreshes the loaded lists of its own app when blocks change, and no other', async () => {
  const other = createPinia()
  setActivePinia(other)
  const otherFetch = vi.fn<() => Promise<string[]>>().mockResolvedValue(['other app'])
  await defineCollectionStore('blocks-other', otherFetch)().load()

  setActivePinia(createPinia())
  const fetch = vi.fn<() => Promise<{ id: number }[]>>().mockResolvedValue([{ id: 1 }])
  const unopened = vi.fn<() => Promise<string[]>>().mockResolvedValue([])
  const store = defineListStore('blocks-loaded', fetch)()
  defineCollectionStore('blocks-unopened', unopened)()
  await store.load()
  fetch.mockResolvedValue([])
  await showBlocksChanged()
  expect(store.list).toEqual([])
  expect(fetch).toHaveBeenCalledTimes(2)
  expect(unopened).not.toHaveBeenCalled()
  expect(otherFetch).toHaveBeenCalledOnce()
})
