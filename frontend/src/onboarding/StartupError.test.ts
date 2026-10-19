import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises } from '@vue/test-utils'
import { engine } from '../bridge'
import type * as Bridge from '../bridge'
import { mountInApp } from '../shared/testing'
import StartupError from './StartupError.vue'

vi.mock('../bridge', async (original) => ({
  ...(await original<typeof Bridge>()),
  engine: { saveProblemReport: vi.fn<typeof Bridge.engine.saveProblemReport>() },
}))
enableAutoUnmount(afterEach)
beforeEach(() => vi.mocked(engine.saveProblemReport).mockReset())
it('offers a retry and keeps the report offer through cancellation, failure and a successful save', async () => {
  const w = mountInApp(StartupError, { props: { error: 'Database could not be read', busy: false } })
  const button = (label: string) => w.findAll('button').find((b) => b.text() === label)
  await button('Try again')?.trigger('click')
  expect(w.findComponent(StartupError).emitted('retry')).toHaveLength(1)
  vi.mocked(engine.saveProblemReport).mockResolvedValueOnce('')
  await button('Save a problem report…')?.trigger('click')
  await flushPromises()
  expect(w.text()).not.toContain('Saved to')
  vi.mocked(engine.saveProblemReport).mockRejectedValueOnce(new Error('Disk full'))
  await button('Save a problem report…')?.trigger('click')
  await flushPromises()
  expect(w.text()).toContain('Disk full')
  vi.mocked(engine.saveProblemReport).mockResolvedValueOnce('D:/report.zip')
  await button('Try saving again…')?.trigger('click')
  await flushPromises()
  expect(w.text()).toContain('Saved to D:/report.zip')
  expect(w.text()).toContain('Your photos are still in their folders.')
})
