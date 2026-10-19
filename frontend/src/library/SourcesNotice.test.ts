import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import { engine, SourceAvailability } from '../bridge'
import type { Source } from '../bridge'
import { mountInApp } from '../shared/testing'
import SourcesNotice from './SourcesNotice.vue'
import { useLibrary } from './store'

let mounted: VueWrapper | undefined

const source = (id: number, path: string, availability: SourceAvailability, enabled = true): Source => ({
  id,
  path,
  enabled,
  availability,
})

function show(sources: Source[]) {
  useLibrary().sources = sources
  mounted = mountInApp(SourcesNotice)
  return mounted
}

beforeEach(() => setActivePinia(createPinia()))
afterEach(() => {
  mounted?.unmount()
  vi.restoreAllMocks()
})

it('says nothing while every folder can be reached, or is still being checked', async () => {
  const w = show([
    source(1, 'C:\\Photos', SourceAvailability.SourceAvailable),
    source(2, 'E:\\Camera', SourceAvailability.SourceChecking),
    source(3, 'F:\\Old', SourceAvailability.SourceUnavailable, false),
  ])
  await flushPromises()
  expect(w.find('[data-sources-notice]').exists()).toBe(false)
})

it('names the one folder that cannot be reached and keeps its photos', async () => {
  const w = show([
    source(1, 'C:\\Photos', SourceAvailability.SourceAvailable),
    source(2, 'E:\\Camera roll', SourceAvailability.SourceUnavailable),
  ])
  await flushPromises()
  const notice = w.get('[data-sources-notice]')
  expect(notice.text()).toContain('“Camera roll” isn’t available')
  expect(notice.text()).toContain('stay in your library')
})

it('counts several missing folders, and looks again when asked', async () => {
  const recheck = vi.spyOn(engine, 'recheckSources').mockResolvedValue(undefined)
  const w = show([
    source(1, 'E:\\Camera', SourceAvailability.SourceUnavailable),
    source(2, '\\\\nas\\photos', SourceAvailability.SourceUnavailable),
  ])
  await flushPromises()
  expect(w.get('[data-sources-notice]').text()).toContain('2 of your folders aren’t available')
  const again = w.findAll('button').find((b) => b.text() === 'Try again')
  if (!again) throw new Error('Try again is missing')
  await again.trigger('click')
  await flushPromises()
  expect(recheck).toHaveBeenCalledOnce()
})
