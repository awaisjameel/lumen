import { beforeEach, expect, it, vi } from 'vitest'
import { DOMWrapper, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { reactive } from 'vue'
import { BatchAction, engine, ErrorCode } from '../bridge'
import type * as Bridge from '../bridge'
import type { BatchOutcome, Selection } from '../bridge'
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

it('opens the action chosen by the selection dock', async () => {
  const wrapper = mountInApp(BatchTools, {
    props: { open: true, ids: [1], trash: false, initialOperation: 'tag' },
    plugins: [createPinia()],
  })
  await flushPromises()
  expect(
    body()
      .find(`[aria-label="${t('details.addTag')}"]`)
      .exists(),
  ).toBe(true)
  wrapper.unmount()
})

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
    .find((b) => b.attributes('data-batch-apply') !== undefined)
  if (!button) throw new Error('No apply button')
  await button.trigger('click')
  await flushPromises()
}

function mountBatch(
  ids: number[],
  trash = false,
  props?: { open: boolean; ids: number[]; trash: boolean; selection?: Selection },
) {
  const pinia = createPinia()
  setActivePinia(pinia)
  return mountInApp(BatchTools, { props: props ?? { open: true, ids, trash }, plugins: [pinia] })
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
      .find((b) => b.text() === t('files.cancelRemaining'))
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

it('does not start a batch after its owning dialog unmounts during validation', async () => {
  let answer: () => void = () => {
    throw new Error('Missing confirmation')
  }
  vi.mocked(engine.validateSelection).mockImplementation(
    () =>
      new Promise<void>((resolve) => {
        answer = resolve
      }),
  )
  const wrapper = mountBatch([1, 2], false, {
    open: true,
    ids: [1, 2],
    trash: false,
    selection: { ids: [1, 2], revision: 1 },
  })
  try {
    await flushPromises()
    await apply()
    wrapper.unmount()
    answer()
    await flushPromises()
    expect(engine.applyBatch).not.toHaveBeenCalled()
  } finally {
    wrapper.unmount()
  }
})

it('invalidates pending validation when the dialog closes and reopens', async () => {
  let answer: () => void = () => undefined
  vi.mocked(engine.validateSelection).mockImplementationOnce(
    () =>
      new Promise<void>((resolve) => {
        answer = resolve
      }),
  )
  const props = reactive({ open: true, ids: [1, 2], trash: false, selection: { ids: [1, 2], revision: 1 } })
  const wrapper = mountBatch(props.ids, false, props)
  try {
    await flushPromises()
    await apply()
    props.open = false
    await flushPromises()
    props.open = true
    await flushPromises()
    answer()
    await flushPromises()
    expect(engine.applyBatch).not.toHaveBeenCalled()
    await apply()
    expect(engine.applyBatch).toHaveBeenCalledExactlyOnceWith([1, 2], BatchAction.BatchFavourite, 0)
  } finally {
    wrapper.unmount()
  }
})

it('does not mutate assets after its owner closes while selection validation is pending', async () => {
  let validated: () => void = () => undefined
  vi.mocked(engine.validateSelection).mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        validated = resolve
      }),
  )
  const props = reactive({ open: true, ids: [1, 2], trash: false, selection: { ids: [1, 2], revision: 1 } })
  const wrapper = mountBatch(props.ids, false, props)
  try {
    await flushPromises()
    await apply()
    expect(engine.validateSelection).toHaveBeenCalledOnce()
    props.open = false
    await flushPromises()
    validated()
    await flushPromises()
    expect(engine.applyBatch).not.toHaveBeenCalled()
  } finally {
    wrapper.unmount()
  }
})

it('runs reversible selection actions without a confirmation and retries only failed items', async () => {
  vi.mocked(engine.applyBatch).mockResolvedValueOnce([
    { id: 1, code: '', error: '' },
    { id: 2, code: ErrorCode.NotFound, error: 'gone' },
  ])
  const wrapper = mountBatch([1, 2])
  try {
    await flushPromises()
    await apply()
    expect(confirm).not.toHaveBeenCalled()
    const retry = body()
      .findAll('button')
      .find((button) => button.text() === t('files.retry'))
    if (!retry) throw new Error('Missing retry')
    await retry.trigger('click')
    await flushPromises()
    expect(engine.applyBatch).toHaveBeenLastCalledWith([2], BatchAction.BatchFavourite, 0)
    expect(body().text()).toContain(t('files.batchProgress', { n: 2, total: 2, failed: 0 }))
    expect(body().text()).toContain(`1: ${t('files.outcome.done')}`)
    expect(body().text()).toContain(`2: ${t('files.outcome.done')}`)
  } finally {
    wrapper.unmount()
  }
})
