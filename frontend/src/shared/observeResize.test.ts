import { afterEach, expect, it, vi } from 'vitest'
import { observeResize } from './observeResize'

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

it("batches resize measurements and cancels a closed view's pending frame", () => {
  vi.useFakeTimers()
  let resize = () => undefined
  const disconnect = vi.fn()
  const observe = vi.fn<(element: Element) => void>()
  vi.stubGlobal(
    'ResizeObserver',
    class implements ResizeObserver {
      constructor(callback: ResizeObserverCallback) {
        resize = () => {
          callback([], this)
          return undefined
        }
      }
      disconnect = disconnect
      observe = observe
      unobserve(): void {
        /* the view disconnects its whole observer */
      }
    },
  )
  const measure = vi.fn()
  const element = document.createElement('div')
  const stop = observeResize([element], measure)
  expect(measure).toHaveBeenCalledOnce()
  expect(observe).toHaveBeenCalledExactlyOnceWith(element)
  resize()
  resize()
  resize()
  expect(measure).toHaveBeenCalledOnce()
  vi.advanceTimersByTime(20)
  expect(measure).toHaveBeenCalledTimes(2)
  resize()
  stop()
  vi.advanceTimersByTime(20)
  expect(measure).toHaveBeenCalledTimes(2)
  expect(disconnect).toHaveBeenCalledOnce()
})
