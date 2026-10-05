import { beforeEach, expect, it, vi } from 'vitest'
import { DOMWrapper, flushPromises } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { reactive } from 'vue'
import { mountInApp } from '../shared/testing'
import { engine, ExportFormat, ExportState, OrganiseReason } from '../bridge'
import type * as Bridge from '../bridge'
import FileTools from './FileTools.vue'
import { confirm } from '../shared/confirm'
import type { ExportJob } from '../bridge'
import { t } from '../shared/i18n'
import { startPortalLayers } from '../shared/overlay'

vi.mock('../bridge', async (original) => ({
  ...(await original<typeof Bridge>()),
  engine: {
    exportJobs: vi.fn(),
    planExport: vi.fn(),
    runExport: vi.fn(),
  },
}))
vi.mock('../shared/confirm', () => ({ confirm: vi.fn() }))

beforeEach(() => {
  vi.mocked(engine.exportJobs).mockReset().mockResolvedValue([])
  vi.mocked(engine.planExport).mockReset().mockResolvedValue([])
  vi.mocked(engine.runExport).mockReset().mockResolvedValue([])
  vi.mocked(confirm).mockReset().mockResolvedValue(true)
})

function deferred<T>() {
  let resolve: (value: T) => void = () => {
    throw new Error('Promise is not initialised')
  }
  const promise = new Promise<T>((done) => {
    resolve = done
  })
  return { promise, resolve }
}
const job: ExportJob = {
  id: 1,
  batch: 1,
  assetId: 1,
  from: 'D:\\Photos\\a.jpg',
  to: 'D:\\Exports\\a.jpg',
  size: 100,
  mtime: 100,
  state: ExportState.ExportPlanned,
  error: '',
  temp: '',
  digest: '',
  options: { format: ExportFormat.ExportJPEG, side: 4096, quality: 92, adjust: true, frame: 1500 },
}

async function action(label: string) {
  const button = new DOMWrapper(document.body).findAll('button').find((button) => button.text() === label)
  if (!button) throw new Error(`Missing ${label} action`)
  await button.trigger('click')
}

async function mountFiles(props: { open: boolean; ids: number[]; frame?: number }) {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: FileTools }] })
  await router.push('/')
  return mountInApp(FileTools, { props, plugins: [router] })
}

it('opens selectable file operations above the dialog and updates the fields for the chosen operation', async () => {
  const wrapper = await mountFiles({ open: true, ids: [1] })
  const stop = startPortalLayers()
  try {
    await flushPromises()
    const body = new DOMWrapper(document.body)
    const trigger = body.get('[aria-label="File operation"]')
    await trigger.trigger('keydown', { key: 'ArrowDown' })
    await flushPromises()
    const list = body.get('[role="listbox"][data-state="open"]')
    expect(list.findAll('[role="option"]').map((option) => option.text())).toEqual([
      t('files.original'),
      t('files.jpeg'),
      t('files.png'),
      t('files.move'),
      t('files.rename'),
    ])
    const positioner = list.element.closest('[data-reka-popper-content-wrapper]')
    const dialog = body.get('[role="dialog"][data-state="open"]').element
    expect(positioner).toBeInstanceOf(HTMLElement)
    if (!(positioner instanceof HTMLElement) || !(dialog instanceof HTMLElement))
      throw new Error('Missing overlay layers')
    expect(Number(positioner.style.zIndex)).toBeGreaterThan(Number(dialog.style.zIndex))
    const rename = list.findAll('[role="option"]').find((option) => option.text() === t('files.rename'))
    if (!rename) throw new Error('Missing rename option')
    await rename.trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect(trigger.text()).toContain(t('files.rename'))
    expect(body.find('input[aria-label="Destination folder"]').exists()).toBe(false)
    expect(body.find(`input[aria-label="${t('files.name')}"]`).exists()).toBe(true)
  } finally {
    wrapper.unmount()
    stop()
  }
})

it('discards a preview when its source selection changes while planning', async () => {
  const pending = deferred<ExportJob[]>()
  vi.mocked(engine.planExport).mockReturnValueOnce(pending.promise)
  const props = reactive({ open: true, ids: [1], frame: 1500 })
  const wrapper = await mountFiles(props)
  try {
    await flushPromises()
    await new DOMWrapper(document.body).get('input[aria-label="Destination folder"]').setValue('D:\\Exports')
    await action('Preview paths')
    props.ids = [2]
    await flushPromises()
    pending.resolve([job])
    await flushPromises()
    expect(new DOMWrapper(document.body).text()).not.toContain(job.to)
    expect(new DOMWrapper(document.body).findAll('button').some((button) => button.text() === t('files.apply'))).toBe(
      false,
    )
  } finally {
    wrapper.unmount()
  }
})

it('says in words why a past export failed', async () => {
  vi.mocked(engine.exportJobs).mockResolvedValue([
    { ...job, id: 1, state: ExportState.ExportFailed, error: OrganiseReason.Taken },
    { ...job, id: 2, state: ExportState.ExportFailed, error: 'Access is denied.' },
  ])
  const wrapper = await mountFiles({ open: true, ids: [1], frame: 1500 })
  try {
    await flushPromises()
    const history = new DOMWrapper(document.body).get('.file-history').text()
    expect(history).toContain(`${t('files.state.failed')}: ${t('organise.reason.taken')}`)
    expect(history).toContain(`${t('files.state.failed')}: Access is denied.`)
    expect(history).not.toContain(OrganiseReason.Taken)
  } finally {
    wrapper.unmount()
  }
})

it('refuses an export when captured frame inputs change during confirmation', async () => {
  const answer = deferred<boolean>()
  vi.mocked(confirm).mockReturnValueOnce(answer.promise)
  vi.mocked(engine.planExport).mockResolvedValueOnce([job])
  const props = reactive({ open: true, ids: [1], frame: 1500 })
  const wrapper = await mountFiles(props)
  try {
    await flushPromises()
    await new DOMWrapper(document.body).get('input[aria-label="Destination folder"]').setValue('D:\\Exports')
    await action('Preview paths')
    await flushPromises()
    await action(t('files.apply'))
    props.frame = 3000
    await flushPromises()
    answer.resolve(true)
    await flushPromises()
    expect(engine.runExport).not.toHaveBeenCalled()
    const preview = new DOMWrapper(document.body).findAll('button').find((button) => button.text() === 'Preview paths')
    expect(preview?.attributes('disabled')).toBeUndefined()
  } finally {
    wrapper.unmount()
  }
})

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

it('ignores an in-flight progress poll after the export has settled', async () => {
  const running = deferred<ExportJob[]>()
  const polling = deferred<ExportJob[]>()
  const done = { ...job, state: ExportState.ExportDone }
  let reads = 0
  vi.mocked(engine.exportJobs).mockImplementation((batch) => {
    if (!batch) return Promise.resolve([])
    reads++
    return reads === 1 ? polling.promise : Promise.resolve([done])
  })
  vi.mocked(engine.planExport).mockResolvedValueOnce([job])
  vi.mocked(engine.runExport).mockReturnValueOnce(running.promise)
  const wrapper = await mountFiles({ open: true, ids: [1], frame: 1500 })
  try {
    await flushPromises()
    await new DOMWrapper(document.body).get('input[aria-label="Destination folder"]').setValue('D:\\Exports')
    await action(t('files.preview'))
    await flushPromises()
    await action(t('files.apply'))
    await vi.waitFor(() => expect(reads).toBe(1))
    running.resolve([done])
    await flushPromises()
    polling.resolve([{ ...job, state: ExportState.ExportWriting }])
    await flushPromises()
    expect(new DOMWrapper(document.body).get('[role="status"]').text()).toContain(t('files.state.done'))
    expect(new DOMWrapper(document.body).get('[role="status"]').text()).not.toContain(t('files.state.writing'))
    expect(reads).toBe(2)
  } finally {
    wrapper.unmount()
  }
})

it('discards a preview after closing and reopening the same dialog', async () => {
  const pending = deferred<ExportJob[]>()
  vi.mocked(engine.planExport).mockReturnValueOnce(pending.promise)
  const props = reactive({ open: true, ids: [1], frame: 1500 })
  const wrapper = await mountFiles(props)
  try {
    await flushPromises()
    await new DOMWrapper(document.body).get('input[aria-label="Destination folder"]').setValue('D:\\Exports')
    await action(t('files.preview'))
    props.open = false
    await flushPromises()
    props.open = true
    await flushPromises()
    pending.resolve([job])
    await flushPromises()
    expect(new DOMWrapper(document.body).text()).not.toContain(job.to)
    expect(new DOMWrapper(document.body).findAll('button').some((button) => button.text() === t('files.apply'))).toBe(
      false,
    )
  } finally {
    wrapper.unmount()
  }
})

it('does not execute a confirmed export after its dialog closes and reopens', async () => {
  const answer = deferred<boolean>()
  vi.mocked(confirm).mockReturnValueOnce(answer.promise)
  vi.mocked(engine.planExport).mockResolvedValueOnce([job])
  const props = reactive({ open: true, ids: [1], frame: 1500 })
  const wrapper = await mountFiles(props)
  try {
    await flushPromises()
    await new DOMWrapper(document.body).get('input[aria-label="Destination folder"]').setValue('D:\\Exports')
    await action(t('files.preview'))
    await flushPromises()
    await action(t('files.apply'))
    props.open = false
    await flushPromises()
    props.open = true
    await flushPromises()
    answer.resolve(true)
    await flushPromises()
    expect(engine.runExport).not.toHaveBeenCalled()
  } finally {
    wrapper.unmount()
  }
})
