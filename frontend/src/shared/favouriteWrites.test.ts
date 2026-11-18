import { expect, it } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useFavouriteWrites } from './favouriteWrites'

it('permanently supersedes only overlapping targets, including after the newer ticket is released', () => {
  setActivePinia(createPinia())
  const owner = useFavouriteWrites()
  const first = owner.begin([1, 2])
  const second = owner.begin([1])
  expect(first.current(1)).toBe(false)
  expect(first.current(2)).toBe(true)
  second.release()
  expect(first.current(1)).toBe(false)
  first.release()
  expect(first.current(2)).toBe(false)
})

it('does not let an older release unregister the newer owner', () => {
  setActivePinia(createPinia())
  const owner = useFavouriteWrites()
  const first = owner.begin([1])
  const second = owner.begin([1])
  first.release()
  const third = owner.begin([1])
  expect(second.current(1)).toBe(false)
  expect(third.current(1)).toBe(true)
  second.release()
  third.release()
})

it('puts a later write after the in-flight inverse and invalidates ownership before waiting', async () => {
  setActivePinia(createPinia())
  const owner = useFavouriteWrites()
  const lease = owner.begin([1])
  const order: string[] = []
  let release: () => void = () => undefined
  const inverse = owner.take(
    () =>
      new Promise<void>((resolve) => {
        order.push('inverse')
        release = resolve
      }),
  )
  const edit = owner.write([1], () => {
    order.push('new edit')
    return Promise.resolve()
  })
  expect(lease.current(1)).toBe(false)
  expect(order).toEqual(['inverse'])
  release()
  await Promise.all([inverse, edit])
  expect(order).toEqual(['inverse', 'new edit'])
  lease.release()
})

it('shares turns within an app while keeping different libraries independent', async () => {
  setActivePinia(createPinia())
  const first = useFavouriteWrites()
  expect(useFavouriteWrites()).toBe(first)
  let release: () => void = () => undefined
  const pending = first.write(
    [1],
    () =>
      new Promise<void>((resolve) => {
        release = resolve
      }),
  )
  setActivePinia(createPinia())
  const second = useFavouriteWrites()
  let written = false
  await second.write([1], () => {
    written = true
    return Promise.resolve()
  })
  expect(written).toBe(true)
  release()
  await pending
})
