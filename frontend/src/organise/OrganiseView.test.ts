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
} from '../bridge'
import type * as Bridge from '../bridge'
import type * as Confirm from '../shared/confirm'
import type { AssetDetail, Event, OrganiseMove, OrganisePlan, OrganiseProgress, OrganiseRun } from '../bridge'
import { confirm } from '../shared/confirm'
import OrganiseView from './OrganiseView.vue'

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
  caps: { preview: true, original: true, render: false, directPlay: false, convertPlay: false },
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

// The list's scroller is 320 px high and each row 60 (the view's own height).
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
  expect(w.get('.example').text()).toBe('For example: 2024\\08\\Beach day\\IMG_0042.JPG')
})

it('updates the example as the scheme is edited and warns about a token that does not exist', async () => {
  const w = await open()
  await w.get('#organise-scheme').setValue('{Camera}/{Day} {MonthName}/{Name}/{Nonsense}')
  expect(w.get('.example').text()).toContain('Pixel 8\\05 August\\IMG_0042\\{Nonsense}.JPG')
  expect(w.text()).toContain('“{Nonsense}” is not one of the tokens above.')
})

it('says so when there is no photo to draw an example from', async () => {
  vi.mocked(engine.timeline).mockResolvedValue([])
  const w = await open()
  expect(w.get('.example').text()).toBe('Add some photos to see an example.')
})

it('offers a chip for every token and puts it into the scheme where the cursor is', async () => {
  const w = await open()
  const chips = w.findAll('.chip').map((c) => c.text())
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
  await w.findAll('.chip')[3]?.trigger('click')
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
  const summary = w.get('.summary').text()
  expect(summary).toContain('2 files will move.')
  expect(summary).toContain('5 files are already where the scheme puts them.')
  expect(summary).toContain('1 file gets a number added to its name')
  expect(summary).toContain('2 files will be left where they are.')
  const rows = w.findAll('.move')
  expect(rows).toHaveLength(4)
  expect(rows[0]?.text()).toContain('D:\\Photos\\IMG_1.jpg')
  expect(rows[0]?.text()).toContain('moves to')
  expect(rows[0]?.text()).toContain('D:\\Photos\\2024\\08\\IMG_1.jpg')
  expect(rows[1]?.text()).toContain('Renamed')
  expect(rows[2]?.text()).toContain('The file is not there any more.')
  expect(rows[3]?.text()).toContain('The process cannot access the file.')
  expect(button(w, 'Move 2 files…')?.attributes('disabled')).toBeUndefined()
})

it('keeps the DOM bounded for a preview of thousands of files and reaches the last one', async () => {
  vi.mocked(engine.planOrganise).mockResolvedValue(plan(Array.from({ length: 5000 }, (_, i) => planned(i + 1))))
  const w = await open()
  await button(w, 'Preview the moves')?.trigger('click')
  await flushPromises()
  expect(w.findAll('.move').length).toBeGreaterThan(0)
  expect(w.findAll('.move').length).toBeLessThan(30)
  const scroller = w.get<HTMLElement>('.moves')
  scroller.element.scrollTop = 5000 * 60 - 320
  await scroller.trigger('scroll')
  await flushPromises()
  expect(w.text()).toContain('IMG_5000.jpg')
  expect(w.text()).not.toContain('IMG_1.jpg')
  expect(w.findAll('.move').length).toBeLessThan(30)
})

it('says so when nothing needs to move and offers no move', async () => {
  vi.mocked(engine.planOrganise).mockResolvedValue(plan([], { inPlace: 12 }))
  const w = await open()
  await button(w, 'Preview the moves')?.trigger('click')
  await flushPromises()
  expect(w.text()).toContain('Nothing needs to move')
  expect(button(w, 'Move 0 files…')?.attributes('disabled')).toBeDefined()
})

it('explains a scheme the engine refuses, in the page', async () => {
  vi.mocked(engine.planOrganise).mockRejectedValue(
    new Error('invalid folder scheme', { cause: { code: ErrorCode.InvalidTemplate } }),
  )
  const w = await open()
  await button(w, 'Preview the moves')?.trigger('click')
  await flushPromises()
  expect(w.get('[data-organise-error][role="alert"]').text()).toContain('folder scheme can’t be used')
  expect(w.find('.preview').exists()).toBe(false)
})

it('drops the preview when the scheme or the folder changes', async () => {
  vi.mocked(engine.planOrganise).mockResolvedValue(plan([planned(1)]))
  const w = await open()
  await button(w, 'Preview the moves')?.trigger('click')
  await flushPromises()
  expect(w.find('.preview').exists()).toBe(true)
  await w.get('#organise-scheme').setValue('{Year}/{Name}')
  expect(w.find('.preview').exists()).toBe(false)

  await button(w, 'Preview the moves')?.trigger('click')
  await flushPromises()
  await chooseSource(w, 'D:\\Photos')
  expect(w.find('.preview').exists()).toBe(false)
})

it('moves the files only after the user confirms, and shows the run start', async () => {
  vi.mocked(engine.planOrganise).mockResolvedValue(plan([planned(1), planned(2)]))
  vi.mocked(confirm).mockResolvedValueOnce(false)
  const w = await open()
  await button(w, 'Preview the moves')?.trigger('click')
  await flushPromises()

  await button(w, 'Move 2 files…')?.trigger('click')
  await flushPromises()
  expect(confirm).toHaveBeenCalledWith('Move 2 files?', expect.stringContaining('nothing is overwritten'), 'Move files')
  expect(engine.startOrganise).not.toHaveBeenCalled()
  expect(w.find('.preview').exists()).toBe(true)

  await button(w, 'Move 2 files…')?.trigger('click')
  await flushPromises()
  expect(engine.startOrganise).toHaveBeenCalledWith(7)
  expect(w.find('.preview').exists()).toBe(false)
  expect(w.get('.progress').text()).toBe('Moving files: 0 of 2')
})

it('shows live progress from the engine’s events and the result when the run ends', async () => {
  const w = await open()
  emit({ runId: 7, state: OrganiseRunState.OrganiseRunning, done: 40, total: 100 })
  await flushPromises()
  const bar = w.get('.progress [role="progressbar"]')
  expect(bar.attributes('aria-valuenow')).toBe('40')
  expect(bar.attributes('aria-valuemax')).toBe('100')
  expect(w.get('.progress').text()).toBe('Moving files: 40 of 100')

  vi.mocked(engine.organiseRuns).mockResolvedValue([run(7, OrganiseRunState.OrganiseDone, 100, 100)])
  emit({ runId: 7, state: OrganiseRunState.OrganiseDone, done: 100, total: 100 })
  await flushPromises()
  expect(w.find('.progress').exists()).toBe(false)
  expect(w.get('.finished[role="status"]').text()).toBe('Finished. Your files are in their new folders.')
  expect(w.text()).toContain('100 of 100 files moved')
})

it('words how a run that stopped part-way, or an undo that could not finish, ended', async () => {
  const w = await open()
  emit({ runId: 7, state: OrganiseRunState.OrganiseFailed, done: 10, total: 100 })
  await flushPromises()
  expect(w.get('.finished').text()).toBe('The run stopped part-way. You can resume it or undo it below.')

  vi.mocked(engine.organiseRuns).mockResolvedValue([run(7, OrganiseRunState.OrganiseDone, 100, 100)])
  emit({ runId: 7, state: OrganiseRunState.OrganiseDone, done: 100, total: 100 })
  await flushPromises()
  await button(w, 'Undo')?.trigger('click')
  await flushPromises()
  emit({ runId: 7, state: OrganiseRunState.OrganiseDone, done: 3, total: 4 })
  await flushPromises()
  expect(w.get('.finished').text()).toContain('Some files could not go back')

  vi.mocked(engine.organiseRuns).mockResolvedValue([run(7, OrganiseRunState.OrganiseUndone, 0, 100)])
  emit({ runId: 7, state: OrganiseRunState.OrganiseUndone, done: 4, total: 4 })
  await flushPromises()
  expect(w.get('.finished').text()).toBe('Every file is back where it was.')
})

it('lists past runs, resumes a stopped one and undoes a finished one after asking', async () => {
  vi.mocked(engine.organiseRuns).mockResolvedValue([
    run(9, OrganiseRunState.OrganiseFailed, 40, 100),
    run(8, OrganiseRunState.OrganiseDone, 20, 20),
    run(7, OrganiseRunState.OrganiseUndone, 0, 20),
  ])
  vi.mocked(confirm).mockResolvedValueOnce(false)
  const w = await open()
  const items = w.findAll('.runs li')
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
  expect(w.findAll('.runs button').every((b) => b.attributes('disabled') !== undefined)).toBe(true)
  vi.mocked(engine.organiseRuns).mockResolvedValue([
    run(9, OrganiseRunState.OrganiseDone, 100, 100),
    run(8, OrganiseRunState.OrganiseDone, 20, 20),
  ])
  emit({ runId: 9, state: OrganiseRunState.OrganiseDone, done: 60, total: 60 })
  await flushPromises()

  await w.findAll('.runs button')[1]?.trigger('click') // run 8's Undo: declined
  await flushPromises()
  expect(confirm).toHaveBeenCalledWith(
    'Put the files back?',
    expect.stringContaining('never replaces a file'),
    'Put files back',
  )
  expect(engine.undoOrganise).not.toHaveBeenCalled()

  await w.findAll('.runs button')[1]?.trigger('click')
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
  expect(w.get('.summary').text()).toContain('2 sidecar files (.xmp) will move with their photos.')
  const rows = w.findAll('.move')
  expect(rows[0]?.find('.note').exists()).toBe(false)
  expect(rows[1]?.get('.note').text()).toBe(
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
  const items = w.findAll('.runs li')
  expect(items[0]?.text()).toContain('3 sidecars stayed behind')
  expect(items[1]?.text()).not.toContain('stayed behind')
})
