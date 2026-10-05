import { beforeEach, expect, it, vi } from 'vitest'
import { DOMWrapper, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { BatchAction, engine, ErrorCode } from '../bridge'
import type * as Bridge from '../bridge'
import type { BatchOutcome } from '../bridge'
import { confirm } from '../shared/confirm'
import { t } from '../shared/i18n'
import { mountInApp } from '../shared/testing'
import BatchTools from './BatchTools.vue'

vi.mock('../bridge', async (original) => ({
  ...(await original<typeof Bridge>()),
  engine: {
    albums: vi.fn(),
    applyBatch: vi.fn(),
    tagAssets: vi.fn(),
    validateSelection: vi.fn(),
    restoreAssets: vi.fn(),
  },
}))
vi.mock('../shared/confirm', () => ({ confirm: vi.fn() }))

const done = (ids: number[]): BatchOutcome[] => ids.map((id) => ({ id, code: '', error: '' }))
const range = (n: number) => Array.from({ length: n }, (_, i) => i + 1)
const body = () => new DOMWrapper(document.body)

beforeEach(() => {
  setActivePinia(createPinia())
  vi.mocked(engine.albums).mockReset().mockResolvedValue([])
  vi.mocked(engine.applyBatch)
    .mockReset()
    .mockImplementation((ids) => Promise.resolve(done(ids)))
  vi.mocked(engine.tagAssets)
    .mockReset()
    .mockImplementation((ids) => Promise.resolve(done(ids)))
  vi.mocked(engine.validateSelection).mockReset().mockResolvedValue(undefined)
  vi.mocked(confirm).mockReset().mockResolvedValue(true)
})

async function apply() {
  const button = body()
    .findAll('button')
    .find((b) => b.text() === t('files.applyBatch'))
  if (!button) throw new Error('No apply button')
  await button.trigger('click')
  await flushPromises()
}

function mountBatch(ids: number[], trash = false) {
  const pinia = createPinia()
  setActivePinia(pinia)
  return mountInApp(BatchTools, { props: { open: true, ids, trash }, plugins: [pinia] })
}

it('sends a large selection to the engine a part at a time, not a call per photo', async () => {
  const wrapper = mountBatch(range(450))
  try {
    await flushPromises()
    await apply()
    const calls = vi.mocked(engine.applyBatch).mock.calls
    expect(calls.map(([ids]) => ids.length)).toEqual([200, 200, 50])
    expect(calls.every(([, action]) => action === BatchAction.BatchFavourite)).toBe(true)
    expect(calls.flatMap(([ids]) => ids)).toEqual(range(450))
    expect(body().get('[role="status"]').text()).toContain('450')
  } finally {
    wrapper.unmount()
  }
})

it('gives each photo its own outcome, a failure in the UI’s words', async () => {
  vi.mocked(engine.applyBatch).mockResolvedValueOnce([
    { id: 1, code: '', error: '' },
    { id: 2, code: ErrorCode.NotFound, error: 'not found' },
    { id: 3, code: '', error: 'Access is denied.' },
  ])
  const wrapper = mountBatch([1, 2, 3])
  try {
    await flushPromises()
    await apply()
    const rows = body()
      .findAll('.outcomes p')
      .map((p) => p.text())
    expect(rows).toEqual([
      `1: ${t('files.outcome.done')}`,
      `2: ${t('files.outcome.failed')} ${t('error.notFound')}`,
      `3: ${t('files.outcome.failed')} ${t('error.generic', { msg: 'Access is denied.' })}`,
    ])
  } finally {
    wrapper.unmount()
  }
})

it('leaves the parts not yet sent alone when the action is stopped', async () => {
  let release: (outcomes: BatchOutcome[]) => void = () => undefined
  vi.mocked(engine.applyBatch).mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        release = resolve
      }),
  )
  const wrapper = mountBatch(range(450))
  try {
    await flushPromises()
    await apply()
    const cancel = body()
      .findAll('button')
      .find((b) => b.text() === t('common.cancel'))
    if (!cancel) throw new Error('No cancel button while a batch runs')
    await cancel.trigger('click')
    release(done(range(200)))
    await flushPromises()
    expect(engine.applyBatch).toHaveBeenCalledOnce()
    const rows = body().findAll('.outcomes p')
    expect(rows).toHaveLength(101) // the count, and the first page of 450 outcomes
    expect(body().text()).toContain(t('files.batchProgress', { n: 200, total: 450, failed: 0 }))
  } finally {
    wrapper.unmount()
  }
})

it('fails every photo of a part whose call the engine refuses', async () => {
  vi.mocked(engine.applyBatch).mockRejectedValueOnce(new Error('gone', { cause: { code: ErrorCode.NotFound } }))
  const wrapper = mountBatch([1, 2])
  try {
    await flushPromises()
    await apply()
    const rows = body()
      .findAll('.outcomes p')
      .map((p) => p.text())
    expect(rows).toEqual([1, 2].map((id) => `${id}: ${t('files.outcome.failed')} ${t('error.notFound')}`))
  } finally {
    wrapper.unmount()
  }
})

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
  const wrapper = mountBatch([1, 2])
  try {
    await flushPromises()
    await apply()
    wrapper.unmount()
    answer(true)
    await flushPromises()
    expect(engine.applyBatch).not.toHaveBeenCalled()
  } finally {
    wrapper.unmount()
  }
})
