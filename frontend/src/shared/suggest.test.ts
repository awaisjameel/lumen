import { afterEach, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import { defineComponent, ref } from 'vue'
import { useSuggestionState } from './suggest'

let wrapper: VueWrapper | undefined
afterEach(() => {
  wrapper?.unmount()
  vi.useRealTimers()
})

function start(fetch: (query: string) => Promise<string[]>) {
  const text = ref('')
  let state: ReturnType<typeof useSuggestionState<string[]>> | undefined
  wrapper = mount(
    defineComponent({
      setup() {
        state = useSuggestionState(text, fetch, [], 10)
        return () => null
      },
    }),
  )
  if (!state) throw new Error('suggestions were not mounted')
  return { text, state }
}

it('debounces text and immediately removes stale options when the query changes', async () => {
  vi.useFakeTimers()
  const fetch = vi.fn((query: string) => Promise.resolve([query]))
  const { text, state } = start(fetch)
  text.value = 'old'
  text.value = 'new'
  await vi.advanceTimersByTimeAsync(10)
  expect(fetch).toHaveBeenCalledExactlyOnceWith('new')
  expect(state.found.value).toEqual(['new'])
  text.value = 'next'
  expect(state.found.value).toEqual([])
  text.value = ' '
  await vi.advanceTimersByTimeAsync(20)
  expect(fetch).toHaveBeenCalledTimes(1)
  expect(state.loading.value).toBe(false)
})

it('drops both stale successes and stale errors including after unmount', async () => {
  vi.useFakeTimers()
  const replies: { resolve: (value: string[]) => void; reject: (error: Error) => void }[] = []
  const { text, state } = start(
    () =>
      new Promise((resolve, reject) => {
        replies.push({ resolve, reject })
      }),
  )
  text.value = 'old'
  await vi.advanceTimersByTimeAsync(10)
  text.value = 'new'
  await vi.advanceTimersByTimeAsync(10)
  replies[1]?.resolve(['new'])
  await flushPromises()
  replies[0]?.reject(new Error('old failure'))
  await flushPromises()
  expect(state.error.value).toBe('')
  expect(state.found.value).toEqual(['new'])
  text.value = 'last'
  await vi.advanceTimersByTimeAsync(10)
  wrapper?.unmount()
  replies[2]?.resolve(['after unmount'])
  await flushPromises()
  expect(state.found.value).toEqual([])
})

it('exposes a failure and retries the current query without changing the input', async () => {
  vi.useFakeTimers()
  const fetch = vi
    .fn<(query: string) => Promise<string[]>>()
    .mockRejectedValueOnce(new Error('failed'))
    .mockResolvedValue(['found'])
  const { text, state } = start(fetch)
  text.value = 'cake'
  await vi.advanceTimersByTimeAsync(10)
  expect(state.error.value).not.toBe('')
  expect(state.loading.value).toBe(false)
  state.retry()
  expect(state.error.value).toBe('')
  await vi.advanceTimersByTimeAsync(10)
  expect(fetch).toHaveBeenLastCalledWith('cake')
  expect(state.found.value).toEqual(['found'])
})
