import { beforeEach, expect, it, vi } from 'vitest'
import { DOMWrapper, flushPromises } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { reactive } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import { mountInApp } from '../shared/testing'
import {
  engine,
  ExportFormat,
  ExportState,
  OrganiseMoveState,
  OrganiseRunState,
  SidecarState,
  OrganiseReason,
} from '../bridge'
import type * as Bridge from '../bridge'
import FileTools from './FileTools.vue'
import { confirm } from '../shared/confirm'
import type { ExportJob, Selection } from '../bridge'
import { t } from '../shared/i18n'
import { startPortalLayers } from '../shared/overlay'

vi.mock('../bridge', async (original) => ({
  ...(await original<typeof Bridge>()),
  engine: {
    planFileMoves: vi.fn(),
    startOrganise: vi.fn(),
    organiseRuns: vi.fn(),
    fileMoveOutcomes: vi.fn(),
    undoOrganise: vi.fn(),
    cancelExport: vi.fn(),
    validateSelection: vi.fn(),
    exportJobs: vi.fn(),
    planExport: vi.fn(),
    runExport: vi.fn(),
  },
}))
vi.mock('../shared/confirm', () => ({ confirm: vi.fn() }))

beforeEach(() => {
  setActivePinia(createPinia()) // the dialog reads the saved export presets
  vi.mocked(engine.validateSelection).mockReset().mockResolvedValue(undefined)
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

async function mountFiles(props: {
  open: boolean
  ids: number[]
  frame?: number
  selection?: Selection
  initialOperation?: ExportFormat | 'move' | 'rename'
}) {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: FileTools }] })
  await router.push('/')
  return mountInApp(FileTools, { props, plugins: [router] })
}

it('opens the rename operation requested by the selection dock', async () => {
  const wrapper = await mountFiles({ open: true, ids: [1], initialOperation: 'rename' })
  await flushPromises()
  expect(
    new DOMWrapper(document.body).findAll('input').some((input) => input.attributes('aria-label') === t('files.name')),
  ).toBe(true)
  wrapper.unmount()
})

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
  const props = reactive({ open: true, ids: [1], frame: 1500, selection: { ids: [1], revision: 1 } })
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
    expect(
      new DOMWrapper(document.body).findAll('button').some((button) => button.text() === t('files.exportNow')),
    ).toBe(false)
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

it('refuses an export when captured frame inputs change during validation', async () => {
  const answer = deferred<undefined>()
  vi.mocked(engine.planExport).mockResolvedValueOnce([job])
  const props = reactive({ open: true, ids: [1], frame: 1500, selection: { ids: [1], revision: 1 } })
  const wrapper = await mountFiles(props)
  try {
    await flushPromises()
    await new DOMWrapper(document.body).get('input[aria-label="Destination folder"]').setValue('D:\\Exports')
    await action('Preview paths')
    await flushPromises()
    vi.mocked(engine.validateSelection).mockReturnValueOnce(answer.promise)
    await action(t('files.exportNow'))
    props.frame = 3000
    await flushPromises()
    answer.resolve(undefined)
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
    await action(t('files.exportNow'))
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
  const props = reactive({ open: true, ids: [1], frame: 1500, selection: { ids: [1], revision: 1 } })
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
    expect(
      new DOMWrapper(document.body).findAll('button').some((button) => button.text() === t('files.exportNow')),
    ).toBe(false)
  } finally {
    wrapper.unmount()
  }
})

it('does not execute an export after its dialog closes and reopens during validation', async () => {
  const answer = deferred<undefined>()
  vi.mocked(engine.planExport).mockResolvedValueOnce([job])
  const props = reactive({ open: true, ids: [1], frame: 1500, selection: { ids: [1], revision: 1 } })
  const wrapper = await mountFiles(props)
  try {
    await flushPromises()
    await new DOMWrapper(document.body).get('input[aria-label="Destination folder"]').setValue('D:\\Exports')
    await action(t('files.preview'))
    await flushPromises()
    vi.mocked(engine.validateSelection).mockReturnValueOnce(answer.promise)
    await action(t('files.exportNow'))
    props.open = false
    await flushPromises()
    props.open = true
    await flushPromises()
    answer.resolve(undefined)
    await flushPromises()
    expect(engine.runExport).not.toHaveBeenCalled()
  } finally {
    wrapper.unmount()
  }
})

it('keeps rename discoverable but disabled with a reason for several selected items', async () => {
  const wrapper = await mountFiles({ open: true, ids: [1, 2] })
  try {
    await flushPromises()
    const body = new DOMWrapper(document.body)
    expect(body.text()).toContain(t('files.renameReason'))
    await body.get('[aria-label="File operation"]').trigger('keydown', { key: 'ArrowDown' })
    await flushPromises()
    const rename = body.findAll('[role="option"]').find((option) => option.text() === t('files.rename'))
    expect(rename?.attributes('aria-disabled')).toBe('true')
  } finally {
    wrapper.unmount()
  }
})

it('previews exact paths, exports without confirmation and re-previews only failed files for retry', async () => {
  const failed = { ...job, id: 2, assetId: 2, state: ExportState.ExportFailed, error: OrganiseReason.Taken }
  const done = { ...job, state: ExportState.ExportDone }
  vi.mocked(engine.planExport)
    .mockResolvedValueOnce([job, { ...job, id: 2, assetId: 2 }])
    .mockResolvedValueOnce([{ ...job, id: 3, batch: 2, assetId: 2 }])
  vi.mocked(engine.exportJobs).mockImplementation((batch) => Promise.resolve(batch ? [done, failed] : []))
  const wrapper = await mountFiles({ open: true, ids: [1, 2] })
  try {
    await flushPromises()
    const body = new DOMWrapper(document.body)
    await body.get('input[aria-label="Destination folder"]').setValue('D:\\Exports')
    await action(t('files.preview'))
    await flushPromises()
    expect(body.text()).toContain(t('files.step.preview'))
    expect(body.text()).toContain(job.from)
    expect(body.text()).toContain(job.to)
    await action(t('files.exportNow'))
    await flushPromises()
    expect(confirm).not.toHaveBeenCalled()
    expect(body.text()).toContain(t('files.step.done'))
    expect(body.text()).toContain(t('organise.reason.taken'))
    await action(t('files.retry'))
    await flushPromises()
    expect(engine.planExport).toHaveBeenLastCalledWith([2], 'D:\\Exports', expect.any(Object))
    expect(body.text()).toContain(t('files.step.preview'))
    expect(engine.runExport).toHaveBeenCalledTimes(1)
  } finally {
    wrapper.unmount()
  }
})

it('shows progress, cancels an export and retains completed and cancelled outcomes', async () => {
  const pending = deferred<ExportJob[]>()
  vi.mocked(engine.planExport).mockResolvedValueOnce([job])
  vi.mocked(engine.runExport).mockReturnValueOnce(pending.promise)
  vi.mocked(engine.cancelExport).mockResolvedValue(undefined)
  vi.mocked(engine.exportJobs).mockImplementation((batch) =>
    Promise.resolve(batch ? [{ ...job, state: ExportState.ExportCancelled }] : []),
  )
  const wrapper = await mountFiles({ open: true, ids: [1] })
  try {
    await flushPromises()
    const body = new DOMWrapper(document.body)
    await body.get('input[aria-label="Destination folder"]').setValue('D:\\Exports')
    await action(t('files.preview'))
    await flushPromises()
    await action(t('files.exportNow'))
    await flushPromises()
    expect(body.find('progress').exists()).toBe(true)
    expect(body.text()).toContain(t('files.step.working'))
    await action(t('files.cancel'))
    expect(engine.cancelExport).toHaveBeenCalledWith(job.batch)
    pending.resolve([])
    await flushPromises()
    expect(body.text()).toContain(t('files.state.cancelled'))
    expect(body.text()).toContain(t('files.step.done'))
  } finally {
    wrapper.unmount()
  }
})

it('previews XMP companions, moves the reviewed copies and undoes the run without another confirmation', async () => {
  const move = {
    seq: 1,
    assetId: 1,
    from: 'D:/Photos/a.jpg',
    to: 'D:/Moved/a.jpg',
    state: OrganiseMoveState.MovePlanned,
    renamed: false,
    sidecar: SidecarState.SidecarPlanned,
  }
  vi.mocked(engine.planFileMoves).mockResolvedValue({
    runId: 7,
    template: '',
    moves: [move],
    renamed: 0,
    inPlace: 0,
    sidecars: 1,
  })
  vi.mocked(engine.startOrganise).mockResolvedValue(undefined)
  vi.mocked(engine.undoOrganise).mockResolvedValue(undefined)
  vi.mocked(engine.organiseRuns)
    .mockResolvedValueOnce([
      { id: 7, template: '', createdAt: 0, state: OrganiseRunState.OrganiseDone, moved: 1, total: 1, sidecarsKept: 0 },
    ])
    .mockResolvedValueOnce([
      {
        id: 7,
        template: '',
        createdAt: 0,
        state: OrganiseRunState.OrganiseUndone,
        moved: 0,
        total: 1,
        sidecarsKept: 0,
      },
    ])
  vi.mocked(engine.fileMoveOutcomes)
    .mockResolvedValueOnce([{ ...move, state: OrganiseMoveState.MoveMoved, sidecar: SidecarState.SidecarMoved }])
    .mockResolvedValueOnce([{ ...move, state: OrganiseMoveState.MoveUndone, sidecar: SidecarState.SidecarUndone }])
  const wrapper = await mountFiles({ open: true, ids: [1] })
  try {
    await flushPromises()
    const body = new DOMWrapper(document.body)
    await body.get('[aria-label="File operation"]').trigger('keydown', { key: 'ArrowDown' })
    await flushPromises()
    const choice = body.findAll('[role="option"]').find((option) => option.text() === t('files.move'))
    if (!choice) throw new Error('Missing move operation')
    await choice.trigger('keydown', { key: 'Enter' })
    await flushPromises()
    await body.get('input[aria-label="Destination folder"]').setValue('D:/Moved')
    await action(t('files.preview'))
    await flushPromises()
    expect(body.text()).toContain('D:/Photos/a.jpg.xmp → D:/Moved/a.jpg.xmp')
    await action(t('files.moveNow'))
    await flushPromises()
    expect(engine.startOrganise).toHaveBeenCalledWith(7)
    expect(body.text()).toContain(t('files.state.moved'))
    await action(t('files.undoMove'))
    await flushPromises()
    expect(engine.undoOrganise).toHaveBeenCalledWith(7)
    expect(body.text()).toContain(t('files.state.undone'))
    expect(confirm).not.toHaveBeenCalled()
  } finally {
    wrapper.unmount()
  }
})
