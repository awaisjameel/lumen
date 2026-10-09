import { expect, it } from 'vitest'
import { turns } from './turns'

/** A promise settled by hand. */
function deferred() {
  let resolve: () => void = () => undefined
  let reject: (e: Error) => void = () => undefined
  const promise = new Promise<void>((yes, no) => {
    resolve = yes
    reject = no
  })
  return { promise, resolve, reject }
}

it('starts the first turn at once and each later one when those before it have ended', async () => {
  const queue = turns()
  const first = deferred()
  const order: string[] = []
  const one = queue.take(async () => {
    order.push('one starts')
    await first.promise
    order.push('one ends')
  })
  const two = queue.take(() => {
    order.push('two runs')
    return Promise.resolve('two')
  })
  expect(order).toEqual(['one starts']) // no tick was needed
  first.resolve()
  await one
  await expect(two).resolves.toBe('two')
  expect(order).toEqual(['one starts', 'one ends', 'two runs'])
})

it('is pending until the last turn ends, whether or not a turn failed', async () => {
  const queue = turns()
  expect(queue.pending).toBeNull()
  const first = deferred()
  const failing = queue.take(() => first.promise)
  let ran = false
  const next = queue.take(() => {
    ran = true
    return Promise.resolve()
  })
  const waited = (async () => {
    while (queue.pending) await queue.pending
    return ran
  })()
  first.reject(new Error('refused'))
  await expect(failing).rejects.toThrow('refused') // its caller hears why
  await next
  await expect(waited).resolves.toBe(true) // those waiting saw every turn end
  expect(queue.pending).toBeNull()
})
