import { expect, it, vi } from 'vitest'
import { createPinia } from 'pinia'
import { DOMWrapper, flushPromises } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { engine } from '../bridge'
import type * as Bridge from '../bridge'
import { confirm } from '../shared/confirm'
import { mountInApp } from '../shared/testing'
import { t } from '../shared/i18n'
import BatchTools from './BatchTools.vue'

vi.mock('../bridge', async (original) => ({
  ...(await original<typeof Bridge>()),
  engine: { albums: vi.fn().mockResolvedValue([]), setFavourite: vi.fn() },
}))
vi.mock('../shared/confirm', () => ({ confirm: vi.fn() }))

it('does not start a confirmed batch after its owning dialog unmounts', async () => {
  let answer: (value: boolean) => void = () => {
    throw new Error('Missing confirmation')
  }
  vi.mocked(confirm).mockImplementation(
    () =>
      new Promise<boolean>((resolve) => {
        answer = resolve
      }),
  )
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: BatchTools }] })
  await router.push('/')
  const wrapper = mountInApp(BatchTools, {
    props: { open: true, ids: [1, 2], trash: false },
    plugins: [createPinia(), router],
  })
  try {
    await flushPromises()
    const apply = new DOMWrapper(document.body)
      .findAll('button')
      .find((button) => button.text() === t('files.applyBatch'))
    if (!apply) throw new Error('Missing batch action')
    await apply.trigger('click')
    wrapper.unmount()
    answer(true)
    await flushPromises()
    expect(engine.setFavourite).not.toHaveBeenCalled()
  } finally {
    wrapper.unmount()
  }
})
