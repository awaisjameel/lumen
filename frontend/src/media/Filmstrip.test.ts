import { afterEach, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import Filmstrip from './Filmstrip.vue'
import { mountInApp } from '../shared/testing'

let wrapper: VueWrapper | undefined
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  vi.restoreAllMocks()
})

it('keeps the current frame wider while virtualising a long strip', async () => {
  vi.spyOn(HTMLElement.prototype, 'offsetWidth', 'get').mockReturnValue(600)
  vi.spyOn(HTMLElement.prototype, 'offsetHeight', 'get').mockReturnValue(56)
  const scrollTo = Object.getOwnPropertyDescriptor(HTMLElement.prototype, 'scrollTo')
  Object.defineProperty(HTMLElement.prototype, 'scrollTo', { configurable: true, value: () => undefined })
  try {
    wrapper = mountInApp(Filmstrip, {
      props: {
        items: Array.from({ length: 1000 }, (_, i) => ({ id: i + 1, w: 600, h: 400, t: 1 })),
        selected: 1,
      },
    })
    await flushPromises()
    expect(wrapper.findAll('button').length).toBeLessThan(25)
    const current = wrapper.get('[aria-current="true"]')
    expect(current.attributes('style')).toContain('width: 72px')
    const next = wrapper.get('button[aria-label="View neighbouring item 2"]')
    expect(next.attributes('style')).toContain('width: 48px')
    await next.trigger('click')
    expect(wrapper.findComponent(Filmstrip).emitted('select')).toEqual([[2]])
  } finally {
    if (scrollTo) Object.defineProperty(HTMLElement.prototype, 'scrollTo', scrollTo)
    else Reflect.deleteProperty(HTMLElement.prototype, 'scrollTo')
  }
})
