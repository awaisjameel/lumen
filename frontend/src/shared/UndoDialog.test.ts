import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { DOMWrapper, flushPromises } from '@vue/test-utils'
import { createTestNavigation } from './navigation/testing'
import { t } from './i18n'
import { mountInApp, recordNotifications } from './testing'
import type { RecordedNotifications } from './testing'
import { offerBatchUndo, undoOutcomes } from './undo'
import type { UndoResult } from './undo'
import UndoDialog from './UndoDialog.vue'

let notifications: RecordedNotifications
beforeEach(() => {
  notifications = recordNotifications()
})
afterEach(() => {
  for (const notice of notifications.shown) notice.closed?.()
  undoOutcomes.value = null
  notifications.stop()
})
const body = () => new DOMWrapper(document.body)
const done = (ids: number[]): UndoResult[] => ids.map((id) => ({ id, state: 'done', error: '' }))
async function mountDialog() {
  const router = createTestNavigation()
  await router.visit('/')
  return mountInApp(UndoDialog, { plugins: [router] })
}

it('pages large outcomes, shows live cancellation and resumes without replaying completed rows', async () => {
  let finish: (rows: UndoResult[]) => void = () => undefined
  const apply = vi
    .fn<(ids: number[]) => Promise<UndoResult[]>>()
    .mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve
        }),
    )
    .mockImplementation((ids) => Promise.resolve(done(ids)))
  const ids = Array.from({ length: 500 }, (_, i) => i + 1)
  const job = offerBatchUndo({
    title: 'Saved',
    undoTitle: 'Undo favourites',
    ids,
    apply,
    reconcile: () => Promise.resolve(),
  })
  const pending = job.run()
  job.show()
  const wrapper = await mountDialog()
  try {
    await flushPromises()
    expect(body().findAll('[data-undo-item]')).toHaveLength(100)
    const cancel = body()
      .findAll('button')
      .find((button) => button.text() === t('files.cancelRemaining'))
    await cancel?.trigger('click')
    finish(done(ids.slice(0, 200)))
    await pending
    await flushPromises()
    expect(body().text()).toContain('200 restored')
    expect(body().text()).toContain('300 cancelled')
    const resume = body()
      .findAll('button')
      .find((button) => button.text() === t('undo.resume'))
    await resume?.trigger('click')
    await vi.waitFor(() => expect(job.busy).toBe(false))
    await flushPromises()
    expect(apply.mock.calls.map(([part]) => part.length)).toEqual([200, 200, 100])
    expect(body().findAll('[data-undo-item]')).toHaveLength(100)
    expect(body().text()).toContain('500 restored')
    const next = body()
      .findAll('button')
      .find((button) => button.text() === t('files.nextPage'))
    await next?.trigger('click')
    await next?.trigger('click')
    expect(body().findAll('[data-undo-item]')[0]?.text()).toContain('201:')
  } finally {
    wrapper.unmount()
  }
})

it('closes independently of work and keeps refresh retry separate from mutation retry', async () => {
  const apply = vi.fn((ids: number[]) => Promise.resolve(done(ids)))
  const reconcile = vi.fn().mockRejectedValueOnce(new Error('read failed')).mockResolvedValue(undefined)
  const job = offerBatchUndo({ title: 'Saved', undoTitle: 'Undo favourites', ids: [1], apply, reconcile })
  await job.run()
  job.show()
  const wrapper = await mountDialog()
  try {
    await flushPromises()
    expect(body().text()).toContain('read failed')
    const refresh = body()
      .findAll('button')
      .find((button) => button.text() === t('undo.refresh'))
    await refresh?.trigger('click')
    await flushPromises()
    expect(apply).toHaveBeenCalledOnce()
    expect(reconcile).toHaveBeenCalledTimes(2)
    const close = body()
      .findAll('button')
      .find((button) => button.text() === t('common.close'))
    await close?.trigger('click')
    await flushPromises()
    expect(undoOutcomes.value).toBeNull()
    expect(job.available).toBe(true)
  } finally {
    wrapper.unmount()
  }
})

it('starts a different job at its own first outcome page even when its length is unchanged', async () => {
  const make = (from: number) =>
    offerBatchUndo({
      title: 'Saved',
      undoTitle: 'Undo favourites',
      ids: Array.from({ length: 150 }, (_, i) => from + i),
      apply: (ids) => Promise.resolve(done(ids)),
      reconcile: () => Promise.resolve(),
    })
  const first = make(1)
  const second = make(1001)
  first.show()
  const wrapper = await mountDialog()
  try {
    await flushPromises()
    const next = body()
      .findAll('button')
      .find((button) => button.text() === t('files.nextPage'))
    await next?.trigger('click')
    expect(body().findAll('[data-undo-item]')[0]?.text()).toContain('101:')
    second.show()
    await flushPromises()
    expect(body().findAll('[data-undo-item]')[0]?.text()).toContain('1001:')
  } finally {
    wrapper.unmount()
  }
})
