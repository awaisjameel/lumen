import { expect, it, vi } from 'vitest'
import { DOMWrapper, flushPromises } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { mountInApp } from '../shared/testing'
import { engine } from '../bridge'
import type * as Bridge from '../bridge'
import FileTools from './FileTools.vue'

vi.mock('../bridge', async (original) => ({
  ...(await original<typeof Bridge>()),
  engine: { exportJobs: vi.fn().mockResolvedValue([]), planExport: vi.fn().mockResolvedValue([]) },
}))

it('opens frame saving as an image export and plans the captured source time', async () => {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: FileTools }] })
  await router.push('/')
  const wrapper = mountInApp(FileTools, { props: { open: true, ids: [1], frame: 1500 }, plugins: [router] })
  try {
    await flushPromises()
    const body = new DOMWrapper(document.body)
    expect(body.get('[aria-label="File operation"]').text()).toContain('Export JPEG')
    await body.get('input[aria-label="Destination folder"]').setValue('D:\\Exports')
    const preview = body.findAll('button').find((button) => button.text() === 'Preview paths')
    if (!preview) throw new Error('Missing preview action')
    await preview.trigger('click')
    await flushPromises()
    expect(engine.planExport).toHaveBeenCalledWith(
      [1],
      'D:\\Exports',
      expect.objectContaining({ format: 'jpeg', frame: 1500 }),
    )
  } finally {
    wrapper.unmount()
  }
})
