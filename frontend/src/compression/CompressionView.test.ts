import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { DOMWrapper, flushPromises } from '@vue/test-utils'
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
      settings: vi.fn(),
      compressionRuns: vi.fn(),
      compressionJobs: vi.fn(),
      planCompression: vi.fn(),
      commitCompression: vi.fn(),
      analyseCompression: vi.fn(),
      pauseCompression: vi.fn(),
      cancelCompression: vi.fn(),
      restoreCompression: vi.fn(),
      revealCompression: vi.fn(),
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
  vi.mocked(engine.settings).mockResolvedValue({
    xmp: false,
    videos: true,
    aiOnBattery: false,
    tagConfidence: 50,
    eventGapHours: 4,
    thumbCacheMB: 100,
    ocr: false,
    cpuThreads: 4,
    maxCPUThreads: 8,
    defaultCPUThreads: 4,
    pauseFullScreen: false,
    pauseBusy: false,
    idleOnly: false,
    excludedFormats: [],
  })
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
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/:pathMatch(.*)*', component: { render: () => null } }],
  })
  await router.push('/compression')
  wrapper = mountInApp(CompressionView, { plugins: [router] })
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
  for (const [typed, percent] of [
    ['150', 90],
    ['', 0],
    ['12.6', 13],
  ] as const) {
    const field = view.get<HTMLInputElement>(`input[aria-label="${t('compression.minSavings')}"]`)
    field.element.value = typed
    await field.trigger('change')
    expect(field.element.value).toBe(String(percent))
    await view.get('[data-compression-plan]').trigger('click')
    await flushPromises()
    expect(vi.mocked(engine.planCompression).mock.lastCall?.[2].minSavings).toBe(percent)
    await view
      .findAll('button')
      .find((button) => button.text() === t('compression.changeOptions'))
      ?.trigger('click')
    await flushPromises()
  }
})

it('offers round radio choices and explains restoration from verified history', async () => {
  const view = await open()
  const modes = view
    .findAll('[role="radio"]')
    .filter((radio) =>
      ['compatible', 'reversible', 'convert', 'restore'].some((mode) => radio.attributes('id')?.includes(mode)),
    )
  expect(modes).toHaveLength(4)
  const compatible = modes.find((radio) => radio.attributes('aria-checked') === 'true')
  expect(compatible).toBeDefined()
  await compatible?.trigger('keydown', { key: 'ArrowDown' })
  await flushPromises()
  const restore = modes.find((radio) => radio.attributes('id')?.includes('restore'))
  expect(restore?.attributes('disabled')).toBeDefined()
  expect(view.text()).toContain(t('compression.description.restore'))
})

it('clamps CPU threads to the shared indexing budget', async () => {
  const view = await open()
  const field = view.get<HTMLInputElement>(`input[aria-label="${t('compression.threads')}"]`)
  field.element.value = '100'
  await field.trigger('change')
  expect(field.element.value).toBe('4')
  await view.get('[data-compression-plan]').trigger('click')
  await flushPromises()
  expect(vi.mocked(engine.planCompression).mock.lastCall?.[2].threads).toBe(4)
})

it('provides a Library action when opened without selected images', async () => {
  compressionRequest.value = null
  const view = await open()
  expect(view.text()).toContain(t('compression.emptyTitle'))
  expect(
    view.findAll('a').some((link) => link.text() === t('compression.openLibrary') && link.attributes('href') === '/'),
  ).toBe(true)
  expect(view.find('[data-compression-plan]').exists()).toBe(false)
})

it('explains missing local codecs and offers Settings', async () => {
  vi.mocked(engine.compressionAvailable).mockResolvedValue(false)
  const view = await open()
  expect(view.text()).toContain(t('compression.unavailable'))
  expect(view.find('[data-compression-plan]').exists()).toBe(false)
  expect(view.findAll('a').some((link) => link.text() === t('compression.settings'))).toBe(true)
})

it('shows the current run totals in history independently of the selection', async () => {
  vi.mocked(engine.compressionRuns).mockResolvedValue([{ ...run(), total: 12480, done: 12470 }])
  const view = await open()
  expect(view.text()).toContain(t('compression.historyCounts', { n: 12480, done: 12470 }))
  expect(view.text()).toContain(t('compression.scope', { n: 2 }))
})

it('starts saving progress at zero instead of reusing preparation’s completed count', async () => {
  const view = await open()
  await view.get('[data-compression-plan]').trigger('click')
  await flushPromises()
  vi.mocked(engine.commitCompression).mockImplementationOnce(() => {
    vi.mocked(engine.compressionRuns).mockResolvedValue([
      { ...run(), state: CompressionState.CompressionCommitting, ready: 0 },
    ])
    return Promise.resolve()
  })
  await view.get('[data-compression-commit]').trigger('click')
  await flushPromises()
  const bar = view.get('[role="progressbar"]')
  expect(bar.attributes('aria-valuenow')).toBe('0')
  expect(bar.attributes('aria-valuemax')).toBe('2')
})

it('returns the page scroller to the reviewed task after replacing the options', async () => {
  const view = await open()
  const scroller = view.get('[data-compression-workspace]').element.parentElement
  if (!scroller) throw new Error('the workspace needs its page scroller')
  scroller.scrollTop = 600
  await view.get('[data-compression-plan]').trigger('click')
  await flushPromises()
  expect(scroller.scrollTop).toBe(0)
})

it('retains initial capability failures and retries them without losing the selection', async () => {
  vi.mocked(engine.compressionAvailable).mockRejectedValueOnce(new Error('Cannot inspect local codecs'))
  const view = await open()
  expect(view.get('[role="alert"]').text()).toContain('Cannot inspect local codecs')
  await view
    .findAll('button')
    .find((button) => button.text() === t('compression.retry'))
    ?.trigger('click')
  await flushPromises()
  expect(view.find('[role="alert"]').exists()).toBe(false)
  expect(view.text()).toContain(t('compression.scope', { n: 2 }))
})

it('requires an absolute destination and passes a Windows folder through unchanged', async () => {
  const view = await open()
  await view.get(`[role="combobox"][aria-label="${t('compression.destination')}"]`).trigger('keydown', { key: 'Enter' })
  await flushPromises()
  const body = new DOMWrapper(document.body)
  const option = body.findAll('[role="option"]').find((option) => option.text() === t('compression.folder'))
  if (!option) throw new Error('the destination chooser needs its folder option')
  await option.trigger('keydown', { key: 'Enter' })
  await flushPromises()
  const path = view.get(`input[aria-label="${t('compression.path')}"]`)
  for (const invalid of ['', 'relative/path', 'D:relative', 'D:/bad:folder']) {
    await path.setValue(invalid)
    expect(view.get('[data-compression-plan]').attributes('disabled')).toBeDefined()
  }
  const destination = ['D:', 'Photos', 'Smaller copies'].join(String.fromCharCode(92))
  await path.setValue(destination)
  expect(view.get('[data-compression-plan]').attributes('disabled')).toBeUndefined()
  await view.get('[data-compression-plan]').trigger('click')
  await flushPromises()
  expect(vi.mocked(engine.planCompression).mock.lastCall?.[1]).toBe(destination)
})

it('explains a run-history read failure and retries it', async () => {
  vi.mocked(engine.compressionRuns).mockRejectedValueOnce(new Error('Cannot read past runs'))
  const view = await open()
  expect(view.get('[role="alert"]').text()).toContain('Cannot read past runs')
  await view
    .findAll('button')
    .find((button) => button.text() === t('compression.retry'))
    ?.trigger('click')
  await flushPromises()
  expect(view.find('[role="alert"]').exists()).toBe(false)
})

it('explains an active run conflict and opens its live outcomes', async () => {
  vi.mocked(engine.compressionRuns).mockResolvedValue([{ ...run(), state: CompressionState.CompressionEncoding }])
  const view = await open()
  expect(view.text()).toContain(t('compression.busy'))
  expect(view.get('[data-compression-plan]').attributes('disabled')).toBeDefined()
  await view
    .findAll('button')
    .find((button) => button.text() === t('compression.openActive'))
    ?.trigger('click')
  await flushPromises()
  expect(view.find('[data-compression-results]').exists()).toBe(true)
  expect(engine.planCompression).not.toHaveBeenCalled()
})

it.each([
  'unsupported',
  'preservation',
  'no_gain',
  'limits',
  'changed',
  'taken',
  'links',
  'space',
  'not_visible',
  'missing',
  'interrupted',
  'timeout',
  'codec',
] as const)('explains the %s outcome without offering to save it', async (reason) => {
  vi.mocked(engine.compressionJobs).mockResolvedValue({
    jobs: [{ ...job(1), state: CompressionState.CompressionFailed, reason, verification: '' }],
    offset: 0,
    total: 1,
  })
  const view = await open()
  await view.get('[data-compression-plan]').trigger('click')
  await flushPromises()
  expect(view.get('[data-compression-job]').text()).toContain(t(`compression.reason.${reason}`))
  expect(view.find('[role="checkbox"]').exists()).toBe(false)
  expect(view.find('[data-compression-commit]').exists()).toBe(false)
})

it('pauses, resumes and cancels the reviewed run without planning another', async () => {
  const current = { ...run(), state: CompressionState.CompressionEncoding, processed: 0, ready: 0 }
  vi.mocked(engine.compressionRuns).mockImplementation(() => Promise.resolve([{ ...current }]))
  const view = await open()
  await view
    .findAll('button')
    .find((button) => button.text() === t('compression.openActive'))
    ?.trigger('click')
  await flushPromises()
  vi.mocked(engine.pauseCompression).mockImplementationOnce(() => {
    current.state = CompressionState.CompressionPaused
    return Promise.resolve()
  })
  await view
    .findAll('button')
    .find((button) => button.text() === t('compression.pause'))
    ?.trigger('click')
  await flushPromises()
  expect(engine.pauseCompression).toHaveBeenCalledWith(7)
  await view.get('[data-compression-analyse]').trigger('click')
  await flushPromises()
  expect(engine.analyseCompression).toHaveBeenCalledWith(7)
  await view
    .findAll('button')
    .find((button) => button.text() === t('compression.cancel'))
    ?.trigger('click')
  await flushPromises()
  expect(engine.cancelCompression).toHaveBeenCalledWith(7)
  expect(engine.planCompression).not.toHaveBeenCalled()
})

it('reveals saved outputs and starts exact restoration from verified reversible history', async () => {
  const saved = {
    ...run(),
    state: CompressionState.CompressionDone,
    done: 2,
    ready: 0,
    options: { ...run().options, mode: CompressionMode.CompressReversible },
  }
  vi.mocked(engine.compressionRuns).mockResolvedValue([saved])
  vi.mocked(engine.compressionJobs).mockResolvedValue({
    jobs: [{ ...job(1), state: CompressionState.CompressionDone }],
    offset: 0,
    total: 1,
  })
  vi.mocked(engine.restoreCompression).mockResolvedValue({ ...run(), id: 8 })
  const view = await open()
  await view
    .findAll('button')
    .find((button) => button.text().includes(t('compression.run', { n: 7 })))
    ?.trigger('click')
  await flushPromises()
  await view
    .findAll('button')
    .find((button) => button.text() === t('compression.reveal'))
    ?.trigger('click')
  await flushPromises()
  expect(engine.revealCompression).toHaveBeenCalledWith(7, 1)
  await view
    .findAll('button')
    .find((button) => button.text() === t('compression.restore'))
    ?.trigger('click')
  await flushPromises()
  expect(engine.restoreCompression).toHaveBeenCalledWith(7, 1, '')
})

it('describes output growth when restoring an original JPEG', async () => {
  vi.mocked(engine.compressionRuns).mockResolvedValue([
    { ...run(), options: { ...run().options, mode: CompressionMode.CompressRestore } },
  ])
  vi.mocked(engine.compressionJobs).mockResolvedValue({ jobs: [{ ...job(1), outputSize: 200 }], offset: 0, total: 1 })
  const view = await open()
  await view.get('[data-compression-plan]').trigger('click')
  await flushPromises()
  expect(view.text()).toContain(t('compression.restoreHint'))
  expect(view.get('[data-compression-job]').text()).toContain('Output is 100 B larger')
})

it('clamps saved thread defaults after retrying a capability read', async () => {
  const preferences = defaultPreferences()
  preferences.compression.threads = 12
  vi.mocked(engine.migratePreferences).mockResolvedValue(preferences)
  vi.mocked(engine.compressionAvailable).mockRejectedValueOnce(new Error('Tools unavailable'))
  const view = await open()
  await view
    .findAll('button')
    .find((button) => button.text() === t('compression.retry'))
    ?.trigger('click')
  await flushPromises()
  expect(view.get<HTMLInputElement>(`input[aria-label="${t('compression.threads')}"]`).element.value).toBe('4')
  await view.get('[data-compression-plan]').trigger('click')
  await flushPromises()
  expect(vi.mocked(engine.planCompression).mock.lastCall?.[2].threads).toBe(4)
})
