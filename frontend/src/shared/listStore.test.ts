import { expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { defineListStore } from './listStore'

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
