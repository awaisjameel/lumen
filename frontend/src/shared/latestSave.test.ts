import { expect, it, vi } from 'vitest'
import { latestSave } from './latestSave'

/** A save that waits until settled by hand, in call order. */
function deferredSaves() {
  const calls: { value: number; resolve: () => void; reject: (e: Error) => void }[] = []
  const save = vi.fn(
    (value: number) =>
      new Promise<void>((resolve, reject) => {
        calls.push({ value, resolve, reject })
      }),
  )
  return { save, calls }
}

it('saves one value at a time, then only the newest asked for meanwhile', async () => {
  const { save, calls } = deferredSaves()
  const saved = vi.fn()
  const set = latestSave(save, 0, { saved, rollback: vi.fn(), failed: vi.fn() })
  const done = set(1)
  void set(2)
  void set(3)
  expect(save).toHaveBeenCalledTimes(1)
  calls[0]?.resolve()
  await vi.waitFor(() => expect(save).toHaveBeenCalledTimes(2))
  expect(calls[1]?.value).toBe(3) // 2 was superseded before it could start
  calls[1]?.resolve()
  await done
  expect(saved).toHaveBeenCalledOnce()
  expect(saved).toHaveBeenCalledWith(3)
})

it('rolls back to the last saved value only when the newest save fails', async () => {
  const { save, calls } = deferredSaves()
  const rollback = vi.fn()
  const failed = vi.fn()
  const set = latestSave(save, 10, { rollback, failed })
  const done = set(20)
  void set(30)
  calls[0]?.reject(new Error('superseded')) // not the newest: nothing to show
  await vi.waitFor(() => expect(save).toHaveBeenCalledTimes(2))
  expect(rollback).not.toHaveBeenCalled()
  calls[1]?.reject(new Error('disk full'))
  await done
  expect(rollback).toHaveBeenCalledWith(10)
  expect(failed).toHaveBeenCalledOnce()
})

it('reports a failure after saving without rolling the saved value back', async () => {
  const rollback = vi.fn()
  const failed = vi.fn()
  const set = latestSave(() => Promise.resolve(), 1, {
    saved: () => Promise.reject(new Error('refresh failed')),
    rollback,
    failed,
  })
  await set(2)
  expect(failed).toHaveBeenCalledOnce()
  expect(rollback).not.toHaveBeenCalled()
})
