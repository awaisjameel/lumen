import { expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import { defineListStore, useList } from './listStore'

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
