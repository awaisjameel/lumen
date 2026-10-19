import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { DOMWrapper, flushPromises } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { mountInApp } from '../shared/testing'
import {
  DateSource,
  engine,
  ErrorCode,
  Kind,
  onOrganiseProgress,
  OrganiseMoveState,
  OrganiseReason,
  OrganiseRunState,
  SidecarState,
} from '../bridge'
import type * as Bridge from '../bridge'
import type * as Confirm from '../shared/confirm'
import type { AssetDetail, Event, OrganiseMove, OrganisePlan, OrganiseProgress, OrganiseRun } from '../bridge'
import { confirm } from '../shared/confirm'
import OrganiseView from './OrganiseView.vue'
import { t } from '../shared/i18n'
import { useLibrary } from '../library/store'
import { useOrganise } from './store'

vi.mock('../media/lazy', () => ({
  FileTools: {
    name: 'FileTools',
    props: ['open', 'ids', 'selection'],
    template: '<div data-organise-file-tools :data-ids="ids.join()" />',
  },
}))

vi.mock('../shared/confirm', async (importOriginal) => ({
  ...(await importOriginal<typeof Confirm>()),
  confirm: vi.fn(),
}))

vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  onOrganiseProgress: vi.fn(() => () => undefined),
  engine: {
    planOrganise: vi.fn(),
    startOrganise: vi.fn(),
    undoOrganise: vi.fn(),
    organiseRuns: vi.fn(),
    sources: vi.fn(),
    timeline: vi.fn(),
    days: vi.fn(),
    asset: vi.fn(),
    event: vi.fn(),
    place: vi.fn(),
  },
}))

let wrapper: VueWrapper | undefined
let emit: (p: OrganiseProgress) => void = () => undefined

const asset: AssetDetail = {
  id: 11,
  camera: 'Pixel 8',
  kind: Kind.KindPhoto,
  takenAt: Date.UTC(2024, 7, 5, 14, 30) / 1000,
  dateFrom: DateSource.DateFromFile,
  width: 100,
  height: 100,
  lat: null,
  lon: null,
  placeId: 0,
  favourite: false,
  paths: ['D:\\Photos\\IMG_0042.JPG'],
  size: 100,
  ext: '.jpg',
  caps: { preview: true, original: true, render: false, paged: false, directPlay: false, convertPlay: false },
  video: null,
}
const beachDay: Event = {
  id: 11,
  name: 'Beach day',
  startAt: 0,
  endAt: 0,
  count: 1,
  cover: 11,
  coverThumb: 1,
  placeId: 0,
  pinned: false,
}

const planned = (seq: number): OrganiseMove => ({
  seq,
  assetId: seq,
  from: `D:\\Photos\\IMG_${seq}.jpg`,
  to: `D:\\Photos\\2024\\08\\IMG_${seq}.jpg`,
  state: OrganiseMoveState.MovePlanned,
  renamed: false,
})
const plan = (moves: OrganiseMove[], extra: Partial<OrganisePlan> = {}): OrganisePlan => ({
  runId: 7,
  template: '{Year}/{Month}/{Name}',
  moves,
  renamed: 0,
  inPlace: 0,
  sidecars: 0,
  ...extra,
})
const run = (id: number, state: OrganiseRunState, moved: number, total: number): OrganiseRun => ({
  id,
  template: '{Year}/{Name}',
  createdAt: 1_790_000_000,
  state,
  moved,
  total,
  sidecarsKept: 0,
})

// The preview scroller is bounded; the row height includes its explanatory note.
function height(element: Element): number {
  return element.classList.contains('moves') ? 320 : 40
}

class TestResizeObserver implements ResizeObserver {
  constructor(private readonly callback: ResizeObserverCallback) {}
  observe(target: Element) {
    const size: ResizeObserverSize = { inlineSize: 900, blockSize: height(target) }
    this.callback(
      [
        {
          target,
          contentRect: new DOMRect(0, 0, 900, size.blockSize),
          borderBoxSize: [size],
          contentBoxSize: [size],
          devicePixelContentBoxSize: [size],
        },
      ],
      this,
    )
  }
  unobserve = vi.fn()
  disconnect = vi.fn()
}

beforeEach(() => {
  setActivePinia(createPinia())
  vi.clearAllMocks()
  vi.stubGlobal('ResizeObserver', TestResizeObserver)
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
    return new DOMRect(0, 0, 900, height(this))
  })
  vi.spyOn(HTMLElement.prototype, 'offsetHeight', 'get').mockImplementation(function (this: HTMLElement) {
    return height(this)
  })
  vi.mocked(onOrganiseProgress).mockImplementation((cb) => {
    emit = cb
    return () => undefined
  })
  vi.mocked(engine.organiseRuns).mockReset().mockResolvedValue([])
  vi.mocked(engine.sources)
    .mockReset()
    .mockResolvedValue([{ id: 3, path: 'D:\\Photos', enabled: true }])
  vi.mocked(engine.timeline)
    .mockReset()
    .mockResolvedValue([{ day: '2024-08-05', count: 1 }])
  vi.mocked(engine.days)
    .mockReset()
    .mockResolvedValue([{ day: '2024-08-05', items: [{ id: 11, w: 1, h: 1, t: 1 }] }])
  vi.mocked(engine.asset).mockReset().mockResolvedValue(asset)
  vi.mocked(engine.event).mockReset().mockResolvedValue(beachDay)
  vi.mocked(engine.planOrganise).mockReset()
  vi.mocked(engine.startOrganise).mockReset().mockResolvedValue(undefined)
  vi.mocked(engine.undoOrganise).mockReset().mockResolvedValue(undefined)
  vi.mocked(confirm).mockReset().mockResolvedValue(true)
})
afterEach(() => {
  wrapper?.unmount()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

async function open() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { render: () => null } },
      { path: '/settings', component: { render: () => null } },
    ],
  })
  wrapper = mountInApp(OrganiseView, { plugins: [router] })
  await flushPromises()
  return wrapper
}

async function chooseSource(view: VueWrapper, path: string) {
  // Opened from the keyboard, as someone without a pointer would.
  await view.get('#organise-source').trigger('keydown', { key: 'Enter' })
  await flushPromises()
  const body = new DOMWrapper(document.body)
  const option = body.findAll('[role="option"]').find((item) => item.text() === path)
  if (!option) throw new Error(`the source selector has no ${path}`)
  await option.trigger('keydown', { key: 'Enter' })
  await flushPromises()
  expect(view.get('#organise-source').text()).toBe(path)
}
const button = (w: VueWrapper, text: string) => w.findAll('button').find((b) => b.text().includes(text))

it('starts with the default scheme and an example drawn from a real photo', async () => {
  const w = await open()
  expect(w.get<HTMLInputElement>('#organise-scheme').element.value).toBe('{Year}/{Month}/{Event}/{Name}')
  expect(w.get('[data-organise-example]').text()).toContain(t('organise.example', { name: 'IMG_0042.JPG' }))
  expect(w.findAll('[data-organise-example] li').map((crumb) => crumb.text())).toEqual([
    'Photos',
    '2024',
    '08',
    'Beach day',
    'IMG_0042.JPG',
  ])
})

it('updates the example as the scheme is edited and warns about a token that does not exist', async () => {
  const w = await open()
  await w.get('#organise-scheme').setValue('{Camera}/{Day} {MonthName}/{Name}/{Nonsense}')
  expect(w.findAll('[data-organise-example] li').map((crumb) => crumb.text())).toEqual([
    'Photos',
    'Pixel 8',
    '05 August',
    'IMG_0042',
    '{Nonsense}.JPG',
  ])
  expect(w.text()).toContain('“{Nonsense}” is not one of the tokens above.')
})

it('says so when there is no photo to draw an example from', async () => {
  vi.mocked(engine.timeline).mockResolvedValue([])
  const w = await open()
  expect(w.get('[data-organise-example]').text()).toBe('Add some photos to see an example.')
})

it('offers a chip for every token and puts it into the scheme where the cursor is', async () => {
  const w = await open()
  const chips = w.findAll('[data-organise-token]').map((c) => c.text())
  expect(chips).toEqual([
    '{Year}',
    '{Month}',
    '{MonthName}',
    '{Day}',
    '{Event}',
    '{Place}',
    '{Camera}',
    '{Name}',
    '{Ext}',
  ])
  const input = w.get<HTMLInputElement>('#organise-scheme')
  await input.setValue('a/b')
  input.element.setSelectionRange(1, 1)
  await w.findAll('[data-organise-token]')[3]?.trigger('click')
  expect(input.element.value).toBe('a{Day}/b')
  expect(input.element.selectionStart).toBe(6)
})

it('previews the scheme for the chosen folder and shows what will move, stay and be renamed', async () => {
  vi.mocked(engine.planOrganise).mockResolvedValue(
    plan(
      [
        planned(1),
        { ...planned(2), renamed: true, to: 'D:\\Photos\\2024\\08\\IMG_2 (2).jpg' },
        {
          ...planned(3),
          state: OrganiseMoveState.MoveSkipped,
          error: OrganiseReason.Missing,
        },
        { ...planned(4), state: OrganiseMoveState.MoveSkipped, error: 'The process cannot access the file.' },
      ],
      { renamed: 1, inPlace: 5 },
    ),
  )
  const w = await open()
  await chooseSource(w, 'D:\\Photos')
  await button(w, 'Preview the moves')?.trigger('click')
  await flushPromises()
  expect(engine.planOrganise).toHaveBeenCalledWith('{Year}/{Month}/{Event}/{Name}', 3)
  const summary = w.get('[data-organise-summary]').text()
  expect(summary).toContain(t('organise.renameSummary', { n: 2 }))
  for (const [filter, count] of [
    ['move', 2],
    ['inPlace', 5],
    ['renamed', 1],
    ['skipped', 2],
  ] as const)
    expect(w.get(`[data-organise-stat="${filter}"]`).text()).toBe(`${count}${t(`organise.stat.${filter}`)}`)
  let rows = w.findAll('[data-organise-move]')
  expect(rows).toHaveLength(2)
  expect(rows[0]?.text()).toContain(planned(1).from)
  expect(rows[0]?.text()).toContain('moves to')
  expect(rows[0]?.text()).toContain(planned(1).to)
  expect(rows[1]?.text()).toContain('Renamed')
  await w.get('[data-organise-stat="skipped"]').trigger('click')
  await flushPromises()
  rows = w.findAll('[data-organise-move]')
  expect(rows).toHaveLength(2)
  expect(rows[0]?.text()).toContain('The file is not there any more.')
  expect(rows[1]?.text()).toContain('The process cannot access the file.')
  expect(button(w, 'Move 2 files…')?.attributes('disabled')).toBeUndefined()
})

it('keeps the DOM bounded for a preview of thousands of files and reaches the last one', async () => {
  vi.mocked(engine.planOrganise).mockResolvedValue(plan(Array.from({ length: 5000 }, (_, i) => planned(i + 1))))
  const w = await open()
  await button(w, 'Preview the moves')?.trigger('click')
  await flushPromises()
  expect(w.findAll('[data-organise-move]').length).toBeGreaterThan(0)
  expect(w.findAll('[data-organise-move]').length).toBeLessThan(30)
  for (let i = 0; i < 49; i++) await w.get('[data-organise-next]').trigger('click')
  await flushPromises()
  const scroller = w.get<HTMLElement>('[data-organise-moves]')
  scroller.element.scrollTop = 100 * 76 - 320
  await scroller.trigger('scroll')
  await flushPromises()
  expect(w.text()).toContain('IMG_5000.jpg')
  expect(w.text()).not.toContain('IMG_1.jpg')
  expect(w.findAll('[data-organise-move]').length).toBeLessThan(30)
})

it('says so when nothing needs to move and offers no move', async () => {
  vi.mocked(engine.planOrganise).mockResolvedValue(plan([], { inPlace: 12 }))
  const w = await open()
  await button(w, 'Preview the moves')?.trigger('click')
  await flushPromises()
  expect(w.text()).toContain('Nothing needs to move')
  expect(button(w, 'Move 0 files…')).toBeUndefined()
  await button(w, t('organise.edit'))?.trigger('click')
  expect(w.find('[data-organise-nothing]').exists()).toBe(false)
  expect(button(w, t('organise.preview'))).toBeDefined()
})

it('explains a scheme the engine refuses, in the page', async () => {
  vi.mocked(engine.planOrganise).mockRejectedValue(
    new Error('invalid folder scheme', { cause: { code: ErrorCode.InvalidTemplate } }),
  )
  const w = await open()
  await button(w, 'Preview the moves')?.trigger('click')
  await flushPromises()
  expect(w.get('[data-organise-load-error][role="alert"]').text()).toContain('folder scheme can’t be used')
  expect(w.find('[data-organise-preview]').exists()).toBe(false)
})

it('drops the preview when the scheme or the folder changes', async () => {
  vi.mocked(engine.planOrganise).mockResolvedValue(plan([planned(1)]))
  const w = await open()
  await button(w, 'Preview the moves')?.trigger('click')
  await flushPromises()
  expect(w.find('[data-organise-preview]').exists()).toBe(true)
  await w.get('#organise-scheme').setValue('{Year}/{Name}')
  expect(w.find('[data-organise-preview]').exists()).toBe(false)

  await button(w, 'Preview the moves')?.trigger('click')
  await flushPromises()
  await chooseSource(w, 'D:\\Photos')
  expect(w.find('[data-organise-preview]').exists()).toBe(false)
})

it('moves the files only after the user confirms, and shows the run start', async () => {
  vi.mocked(engine.planOrganise).mockResolvedValue(plan([planned(1), planned(2)]))
  vi.mocked(confirm).mockResolvedValueOnce(false)
  const w = await open()
  await button(w, 'Preview the moves')?.trigger('click')
  await flushPromises()

  await button(w, 'Move 2 files…')?.trigger('click')
  await flushPromises()
  expect(confirm).toHaveBeenCalledWith(
    'Move 2 files?',
    expect.stringContaining('nothing is overwritten'),
    'Move 2 files',
  )
  expect(engine.startOrganise).not.toHaveBeenCalled()
  expect(w.find('[data-organise-preview]').exists()).toBe(true)

  await button(w, 'Move 2 files…')?.trigger('click')
  await flushPromises()
  expect(engine.startOrganise).toHaveBeenCalledWith(7)
  expect(w.find('[data-organise-preview]').exists()).toBe(false)
  expect(w.get('[data-organise-progress]').text()).toBe(
    `Moving files: 0 of 20%${t('organise.scanning')}${t('organise.backToLibrary')}`,
  )
})

it('shows live progress from the engine’s events and the result when the run ends', async () => {
  const w = await open()
  emit({ runId: 7, state: OrganiseRunState.OrganiseRunning, done: 40, total: 100 })
  await flushPromises()
  const bar = w.get('[data-organise-progress] [role="progressbar"]')
  expect(bar.attributes('aria-valuenow')).toBe('40')
  expect(bar.attributes('aria-valuemax')).toBe('100')
  expect(w.get('[data-organise-progress]').text()).toBe(
    `Moving files: 40 of 10040%${t('organise.scanning')}${t('organise.backToLibrary')}`,
  )

  vi.mocked(engine.organiseRuns).mockResolvedValue([run(7, OrganiseRunState.OrganiseDone, 100, 100)])
  emit({ runId: 7, state: OrganiseRunState.OrganiseDone, done: 100, total: 100 })
  await flushPromises()
  expect(w.find('[data-organise-progress]').exists()).toBe(false)
  expect(w.get('[data-organise-finished][role="status"] h2').text()).toBe(t('organise.doneTitle'))
  expect(w.get('[data-organise-finished]').text()).toContain('100 files moved. Scanning has resumed.')
  expect(w.text()).toContain('100 of 100 files moved')
})

it('words how a run that stopped part-way, or an undo that could not finish, ended', async () => {
  const w = await open()
  emit({ runId: 7, state: OrganiseRunState.OrganiseFailed, done: 10, total: 100 })
  await flushPromises()
  expect(w.get('[data-organise-stopped]').text()).toContain(t('organise.stoppedTitle'))

  vi.mocked(engine.organiseRuns).mockResolvedValue([run(7, OrganiseRunState.OrganiseDone, 100, 100)])
  emit({ runId: 7, state: OrganiseRunState.OrganiseDone, done: 100, total: 100 })
  await flushPromises()
  await button(w, 'Undo')?.trigger('click')
  await flushPromises()
  emit({ runId: 7, state: OrganiseRunState.OrganiseDone, done: 3, total: 4 })
  await flushPromises()
  expect(w.get('[data-organise-partial]').text()).toContain(t('organise.partialText'))

  vi.mocked(engine.organiseRuns).mockResolvedValue([run(7, OrganiseRunState.OrganiseUndone, 0, 100)])
  emit({ runId: 7, state: OrganiseRunState.OrganiseUndone, done: 4, total: 4 })
  await flushPromises()
  expect(w.get('[data-organise-finished] h2').text()).toBe(t('organise.undoneTitle'))
  expect(w.get('[data-organise-finished]').text()).toContain(t('organise.undone', { n: 4 }))
})

it('lists past runs, resumes a stopped one and undoes a finished one after asking', async () => {
  vi.mocked(engine.organiseRuns).mockResolvedValue([
    run(9, OrganiseRunState.OrganiseFailed, 40, 100),
    run(8, OrganiseRunState.OrganiseDone, 20, 20),
    run(7, OrganiseRunState.OrganiseUndone, 0, 20),
  ])
  vi.mocked(confirm).mockResolvedValueOnce(false)
  const w = await open()
  const items = w.findAll('[data-organise-run]')
  expect(items).toHaveLength(3)
  expect(items[0]?.text()).toContain('Stopped part-way')
  expect(items[0]?.text()).toContain('40 of 100 files moved')
  expect(items[2]?.text()).toContain('Undone')
  expect(items[2]?.findAll('button')).toHaveLength(0)

  await button(w, 'Resume')?.trigger('click')
  await flushPromises()
  expect(engine.startOrganise).toHaveBeenCalledWith(9)

  // Everything is busy while a run moves files.
  emit({ runId: 9, state: OrganiseRunState.OrganiseRunning, done: 1, total: 60 })
  await flushPromises()
  expect(w.findAll('[data-organise-runs] button').every((b) => b.attributes('disabled') !== undefined)).toBe(true)
  vi.mocked(engine.organiseRuns).mockResolvedValue([
    run(9, OrganiseRunState.OrganiseDone, 100, 100),
    run(8, OrganiseRunState.OrganiseDone, 20, 20),
  ])
  emit({ runId: 9, state: OrganiseRunState.OrganiseDone, done: 60, total: 60 })
  await flushPromises()

  await w.findAll('[data-organise-runs] button')[1]?.trigger('click') // run 8's Undo: declined
  await flushPromises()
  expect(confirm).toHaveBeenCalledWith(
    'Put the files back?',
    expect.stringContaining('never replaces a file'),
    'Put files back',
  )
  expect(engine.undoOrganise).not.toHaveBeenCalled()

  await w.findAll('[data-organise-runs] button')[1]?.trigger('click')
  await flushPromises()
  expect(engine.undoOrganise).toHaveBeenCalledWith(8)
})

it('says how many sidecars move with their photos, and why one stays behind', async () => {
  vi.mocked(engine.planOrganise).mockResolvedValue(
    plan([planned(1), { ...planned(2), error: OrganiseReason.SidecarTaken }], { sidecars: 2 }),
  )
  const w = await open()
  await button(w, 'Preview the moves')?.trigger('click')
  await flushPromises()
  expect(w.get('[data-organise-summary]').text()).toContain('2 sidecar files (.xmp) will move with their photos.')
  const rows = w.findAll('[data-organise-move]')
  expect(rows[0]?.find('[data-organise-note]').exists()).toBe(false)
  expect(rows[1]?.get('[data-organise-note]').text()).toBe(
    'The photo moves, but its .xmp sidecar stays because something is already at its new name.',
  )
  expect(rows[1]?.text()).toContain('moves to')
})

it('says how many sidecars stayed behind in a past run', async () => {
  vi.mocked(engine.organiseRuns).mockResolvedValue([
    { ...run(8, OrganiseRunState.OrganiseDone, 20, 20), sidecarsKept: 3 },
    run(7, OrganiseRunState.OrganiseDone, 5, 5),
  ])
  const w = await open()
  const items = w.findAll('[data-organise-run]')
  expect(items[0]?.text()).toContain('3 sidecars stayed behind')
  expect(items[1]?.text()).not.toContain('stayed behind')
})

it('disables preview for invalid literal schemes and explains the rule', async () => {
  const view = await open()
  await view.get('#organise-scheme').setValue('{Year}/Trip?/{Name}')
  expect(button(view, 'Preview the moves')?.attributes('disabled')).toBeDefined()
  expect(view.get('#organise-scheme').attributes('aria-invalid')).toBe('true')
  expect(engine.planOrganise).not.toHaveBeenCalled()
})

it('replaces selected text with a token and resets the scheme', async () => {
  const view = await open()
  const input = view.get<HTMLInputElement>('#organise-scheme')
  await input.setValue('Photo/{Name}')
  input.element.setSelectionRange(0, 5)
  await view.findAll('[data-organise-token]')[0]?.trigger('click')
  expect(input.element.value).toBe('{Year}/{Name}')
  await button(view, 'Reset to default')?.trigger('click')
  expect(input.element.value).toBe('{Year}/{Month}/{Event}/{Name}')
})

it('does not move a different preview when the scheme changes during confirmation', async () => {
  vi.mocked(engine.planOrganise).mockResolvedValue(plan([planned(1)]))
  let finish: ((answer: boolean) => void) | undefined
  vi.mocked(confirm).mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        finish = resolve
      }),
  )
  const view = await open()
  await button(view, 'Preview the moves')?.trigger('click')
  await flushPromises()
  await button(view, 'Move 1 file…')?.trigger('click')
  await view.get('#organise-scheme').setValue('{Name}')
  finish?.(true)
  await flushPromises()
  expect(engine.startOrganise).not.toHaveBeenCalled()
})

it('filters skipped files while preserving their reasons and the move count', async () => {
  vi.mocked(engine.planOrganise).mockResolvedValue(
    plan([planned(1), { ...planned(2), state: OrganiseMoveState.MoveSkipped, error: OrganiseReason.Missing }]),
  )
  const view = await open()
  await button(view, 'Preview the moves')?.trigger('click')
  await flushPromises()
  await view.get('[data-organise-stat="skipped"]').trigger('click')
  await flushPromises()
  expect(view.findAll('[data-organise-move]')).toHaveLength(1)
  expect(view.get('[data-organise-move]').text()).toContain('The file is not there any more.')
  expect(button(view, 'Move 1 file…')?.attributes('disabled')).toBeUndefined()
})

it('keeps history failures visible and retries them', async () => {
  vi.mocked(engine.organiseRuns).mockRejectedValueOnce(new Error('History cannot be read'))
  const view = await open()
  expect(view.get('[role="alert"]').text()).toContain('History cannot be read')
  await button(view, 'Try again')?.trigger('click')
  await flushPromises()
  expect(engine.organiseRuns).toHaveBeenCalledTimes(2)
  expect(view.find('[role="alert"]').exists()).toBe(false)
})

it('counts only successful companions and does not subtract preview collisions twice', async () => {
  vi.mocked(engine.planOrganise).mockResolvedValue(
    plan(
      [
        { ...planned(1), sidecar: SidecarState.SidecarPlanned },
        { ...planned(2), sidecar: SidecarState.SidecarKept, error: OrganiseReason.SidecarTaken },
      ],
      { sidecars: 1 },
    ),
  )
  const view = await open()
  await button(view, t('organise.preview'))?.trigger('click')
  await flushPromises()
  await button(view, t('organise.move', { n: 2 }))?.trigger('click')
  await flushPromises()
  vi.mocked(engine.organiseRuns).mockResolvedValue([
    { ...run(7, OrganiseRunState.OrganiseDone, 2, 2), sidecarsKept: 1 },
  ])
  emit({ runId: 7, state: OrganiseRunState.OrganiseDone, done: 2, total: 2 })
  await flushPromises()
  expect(view.get('[data-organise-finished]').text()).toContain('1 companion XMP file moved')
  expect(view.get('[data-organise-finished]').text()).toContain('1 sidecar stayed behind')
  await button(view, t('organise.more'))?.trigger('click')
  await flushPromises()
  expect(view.find('[data-organise-finished]').exists()).toBe(false)
  expect(view.find('[data-organise-editor]').exists()).toBe(true)
  expect(view.findAll('[data-organise-run]')).toHaveLength(1)
})

it('offers Library as the next action when selected file tools have no scope', async () => {
  const view = await open()
  await view
    .findAll('[role="radio"]')
    .find((radio) => radio.text() === t('organise.selectedFiles'))
    ?.trigger('click')
  await flushPromises()
  expect(view.get('h1').text()).toBe(t('organise.filesTitle'))
  expect(view.get('[data-organise-files]').text()).toContain(t('organise.filesEmptyText'))
  expect(view.get('[data-organise-files] a').attributes('href')).toBe('/')
})

it('hands the Library selection to the existing file tools and preserves it when returning to the scheme', async () => {
  const library = useLibrary()
  library.selectedIDs = new Set([11, 12])
  const view = await open()
  await view
    .findAll('[role="radio"]')
    .find((radio) => radio.text() === t('organise.selectedFiles'))
    ?.trigger('click')
  await flushPromises()
  expect(view.get('[data-organise-files]').text()).toContain(t('organise.filesScope', { n: 2 }))
  await button(view, t('organise.openFiles'))?.trigger('click')
  await flushPromises()
  expect(view.get('[data-organise-file-tools]').attributes('data-ids')).toBe('11,12')
  await view
    .findAll('[role="radio"]')
    .find((radio) => radio.text() === t('organise.folderScheme'))
    ?.trigger('click')
  await flushPromises()
  expect(view.get('[data-organise-editor]').isVisible()).toBe(true)
  expect(library.selectedIDs).toEqual(new Set([11, 12]))
})

it('does not undo a larger set of files than the confirmation reviewed', async () => {
  vi.mocked(engine.organiseRuns).mockResolvedValue([run(7, OrganiseRunState.OrganiseDone, 2, 4)])
  let finish: ((answer: boolean) => void) | undefined
  vi.mocked(confirm).mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        finish = resolve
      }),
  )
  const view = await open()
  await view.get('[data-organise-run] button').trigger('click')
  await flushPromises()
  useOrganise().runs = [run(7, OrganiseRunState.OrganiseDone, 4, 4)]
  finish?.(true)
  await flushPromises()
  expect(engine.undoOrganise).not.toHaveBeenCalled()
})
