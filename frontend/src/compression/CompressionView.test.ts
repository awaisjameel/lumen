import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import { CompressionMode, CompressionState, engine } from '../bridge'
import type * as Bridge from '../bridge'
import type { CompressionJob, CompressionRun } from '../bridge'
import { confirm } from '../shared/confirm'
import { compressionRequest } from '../shared/compression'
import { t } from '../shared/i18n'
import { defaultPreferences } from '../shared/preferences'
import { mountInApp } from '../shared/testing'
import CompressionView from './CompressionView.vue'

vi.mock('../shared/confirm', () => ({ confirm: vi.fn() }))
vi.mock('../bridge', async (original) => {
  const bridge = await original<typeof Bridge>()
  return {
    ...bridge,
    onLibraryChanged: vi.fn(() => () => undefined),
    engine: {
      ...bridge.engine,
      migratePreferences: vi.fn(),
      compressionAvailable: vi.fn(),
      compressionRuns: vi.fn(),
      compressionJobs: vi.fn(),
      planCompression: vi.fn(),
      commitCompression: vi.fn(),
      validateSelection: vi.fn(),
    },
  }
})

let wrapper: VueWrapper | undefined
const run = (): CompressionRun => ({
  id: 7,
  createdAt: 1,
  options: defaultPreferences().compression,
  state: CompressionState.CompressionReady,
  total: 2,
  processed: 2,
  ready: 2,
  done: 0,
  skipped: 0,
  failed: 0,
  cancelled: 0,
  savedBytes: 0,
  addedBytes: 0,
})
const job = (id: number): CompressionJob => ({
  id,
  runId: 7,
  assetId: id,
  from: `D:\\Photos\\${id}.png`,
  to: `D:\\Photos\\${id}-compressed.png`,
  copies: 1,
  size: 100,
  mtime: 1,
  digest: 'original',
  outputSize: 50,
  outputDigest: 'candidate',
  state: CompressionState.CompressionReady,
  reason: '',
  verification: 'native-samples',
  tool: 'pinned tool',
  outputAssetId: 0,
})
beforeEach(() => {
  vi.clearAllMocks()
  setActivePinia(createPinia())
  localStorage.clear()
  compressionRequest.value = {
    inputs: [
      { assetId: 1, path: '' },
      { assetId: 2, path: '' },
    ],
    selection: null,
  }
  vi.mocked(engine.migratePreferences).mockResolvedValue(defaultPreferences())
  vi.mocked(engine.compressionAvailable).mockResolvedValue(true)
  vi.mocked(engine.compressionRuns).mockResolvedValue([run()])
  vi.mocked(engine.compressionJobs).mockResolvedValue({ jobs: [job(1), job(2)], offset: 0, total: 2 })
  vi.mocked(engine.planCompression).mockResolvedValue(run())
  vi.mocked(engine.commitCompression).mockResolvedValue(undefined)
  vi.mocked(confirm).mockResolvedValue(true)
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  compressionRequest.value = null
})
async function open() {
  wrapper = mountInApp(CompressionView)
  await flushPromises()
  return wrapper
}

it('prepares no files when the tools page opens, and preserves safe defaults', async () => {
  await open()
  expect(engine.planCompression).not.toHaveBeenCalled()
  expect(defaultPreferences().compression.mode).toBe(CompressionMode.CompressCompatible)
})

it('publishes exactly the checked candidates after confirmation', async () => {
  const view = await open()
  await view.get('[data-compression-plan]').trigger('click')
  await flushPromises()
  const choices = view.findAll('[role="checkbox"]')
  expect(choices).toHaveLength(2)
  await choices[1]?.trigger('click')
  await view.get('[data-compression-commit]').trigger('click')
  await flushPromises()
  expect(confirm).toHaveBeenCalledOnce()
  expect(engine.commitCompression).toHaveBeenCalledExactlyOnceWith(7, [1])
})

it('ignores a late plan response when the selection changes', async () => {
  let finish: ((run: CompressionRun) => void) | undefined
  vi.mocked(engine.planCompression).mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        finish = resolve
      }),
  )
  const view = await open()
  await view.get('[data-compression-plan]').trigger('click')
  await flushPromises()
  compressionRequest.value = { inputs: [{ assetId: 3, path: '' }], selection: null }
  await flushPromises()
  finish?.(run())
  await flushPromises()
  expect(view.find('[data-compression-results]').exists()).toBe(false)
  expect(engine.commitCompression).not.toHaveBeenCalled()
})

it('refuses publication if the selection changes while confirmation is open', async () => {
  let finish: ((answer: boolean) => void) | undefined
  vi.mocked(confirm).mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        finish = resolve
      }),
  )
  const view = await open()
  await view.get('[data-compression-plan]').trigger('click')
  await flushPromises()
  await view.get('[data-compression-commit]').trigger('click')
  await flushPromises()
  compressionRequest.value = { inputs: [{ assetId: 3, path: '' }], selection: null }
  await flushPromises()
  finish?.(true)
  await flushPromises()
  expect(engine.commitCompression).not.toHaveBeenCalled()
})

it('asks the engine nothing while no run is working, and follows one that is', async () => {
  vi.useFakeTimers()
  try {
    await open()
    vi.mocked(engine.compressionRuns).mockClear()
    await vi.advanceTimersByTimeAsync(2000)
    expect(engine.compressionRuns).not.toHaveBeenCalled()

    vi.mocked(engine.compressionRuns).mockResolvedValue([{ ...run(), state: CompressionState.CompressionEncoding }])
    await wrapper?.get('[data-compression-plan]').trigger('click')
    await vi.advanceTimersByTimeAsync(0)
    vi.mocked(engine.compressionRuns).mockClear()
    await vi.advanceTimersByTimeAsync(1100)
    expect(engine.compressionRuns).toHaveBeenCalledTimes(2)
  } finally {
    vi.useRealTimers()
  }
})

it('plans with a whole minimum saving inside the bounds whatever is typed', async () => {
  const view = await open()
  const field = view.get<HTMLInputElement>(`input[aria-label="${t('compression.minSavings')}"]`)
  for (const [typed, percent] of [
    ['150', 90],
    ['', 0],
    ['12.6', 13],
  ] as const) {
    field.element.value = typed
    await field.trigger('change')
    expect(field.element.value).toBe(String(percent))
    await view.get('[data-compression-plan]').trigger('click')
    await flushPromises()
    expect(vi.mocked(engine.planCompression).mock.lastCall?.[2].minSavings).toBe(percent)
  }
})
