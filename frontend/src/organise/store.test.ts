import { beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises } from '@vue/test-utils'
import {
  DateSource,
  engine,
  ErrorCode,
  Filter,
  Kind,
  onOrganiseProgress,
  OrganiseMoveState,
  OrganiseRunState,
} from '../bridge'
import type * as Bridge from '../bridge'
import type { AssetDetail, Event, OrganiseMove, OrganisePlan, OrganiseProgress, OrganiseRun, Place } from '../bridge'
import { useOrganise } from './store'

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

let emit: (p: OrganiseProgress) => void = () => undefined

const move = (seq: number, state: OrganiseMoveState, error?: string): OrganiseMove => ({
  seq,
  assetId: seq,
  from: `D:\\Photos\\${seq}.jpg`,
  to: `D:\\Photos\\2024\\${seq}.jpg`,
  state,
  renamed: false,
  ...(error ? { error } : {}),
})

const plan = (moves: OrganiseMove[]): OrganisePlan => ({
  runId: 7,
  template: '{Year}/{Name}',
  moves,
  renamed: 0,
  inPlace: 3,
  sidecars: 0,
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

const detail = (placeId: number, camera = ''): AssetDetail => ({
  id: 11,
  camera,
  kind: Kind.KindPhoto,
  takenAt: 1_722_868_200,
  dateFrom: DateSource.DateFromFile,
  width: 100,
  height: 100,
  lat: null,
  lon: null,
  placeId,
  favourite: false,
  paths: ['D:\\Photos\\IMG_1.jpg'],
  size: 100,
  ext: '.jpg',
  caps: { preview: true, original: true, render: false, paged: false, directPlay: false, convertPlay: false },
  video: null,
})

const event = (name: string): Event => ({
  id: 11,
  name,
  startAt: 0,
  endAt: 0,
  count: 1,
  cover: 11,
  coverThumb: 1,
  placeId: 0,
  pinned: false,
})

const place = (name: string): Place => ({
  id: 2,
  name,
  region: '',
  country: '',
  lat: 0,
  lon: 0,
  count: 1,
  cover: 11,
  coverThumb: 1,
})

/** The library has one photo on one day. */
function onePhoto() {
  vi.mocked(engine.timeline).mockResolvedValue([{ day: '2024-08-05', count: 1 }])
  vi.mocked(engine.days).mockResolvedValue([{ day: '2024-08-05', items: [{ id: 11, w: 1, h: 1, t: 1 }] }])
}

beforeEach(() => {
  setActivePinia(createPinia())
  vi.clearAllMocks()
  vi.mocked(onOrganiseProgress).mockImplementation((cb) => {
    emit = cb
    return () => undefined
  })
  vi.mocked(engine.organiseRuns).mockReset().mockResolvedValue([])
  vi.mocked(engine.planOrganise).mockReset()
  vi.mocked(engine.startOrganise).mockReset().mockResolvedValue(undefined)
  vi.mocked(engine.undoOrganise).mockReset().mockResolvedValue(undefined)
})

it('previews the scheme for the chosen folder and counts what would move and what would stay', async () => {
  vi.mocked(engine.planOrganise).mockResolvedValue(
    plan([
      move(1, OrganiseMoveState.MovePlanned),
      move(2, OrganiseMoveState.MovePlanned),
      move(3, OrganiseMoveState.MoveSkipped, 'missing'),
    ]),
  )
  const organise = useOrganise()
  organise.template = '{Year}/{Name}'
  organise.sourceId = 4
  await organise.preview()
  expect(engine.planOrganise).toHaveBeenCalledWith('{Year}/{Name}', 4)
  expect(organise.toMove).toBe(2)
  expect(organise.skipped).toBe(1)
  expect(organise.error).toBe('')
  expect(organise.planning).toBe(false)
})

it('says why a scheme was refused and keeps no preview', async () => {
  const organise = useOrganise()
  vi.mocked(engine.planOrganise).mockResolvedValueOnce(plan([move(1, OrganiseMoveState.MovePlanned)]))
  await organise.preview()
  vi.mocked(engine.planOrganise).mockRejectedValueOnce(
    new Error('invalid folder scheme', { cause: { code: ErrorCode.InvalidTemplate } }),
  )
  await organise.preview()
  expect(organise.plan).toBeNull()
  expect(organise.previewError).toContain('folder scheme can’t be used')
})

it('forgets a preview when asked, as the scheme it belongs to changes', async () => {
  vi.mocked(engine.planOrganise).mockResolvedValue(plan([move(1, OrganiseMoveState.MovePlanned)]))
  const organise = useOrganise()
  await organise.preview()
  organise.discardPlan()
  expect(organise.plan).toBeNull()
  expect(organise.toMove).toBe(0)
})

it('drops a preview that answers for a scheme no longer being edited', async () => {
  let answer: (p: OrganisePlan) => void = () => undefined
  vi.mocked(engine.planOrganise).mockReturnValueOnce(new Promise((resolve) => (answer = resolve)))
  const organise = useOrganise()
  const pending = organise.preview()
  expect(organise.planning).toBe(true)
  organise.discardPlan()
  expect(organise.planning).toBe(false)
  answer(plan([move(1, OrganiseMoveState.MovePlanned)]))
  await pending
  expect(organise.plan).toBeNull()
  expect(organise.planning).toBe(false)
})

it('starts the previewed run, shows it moving, and refreshes the past runs', async () => {
  vi.mocked(engine.planOrganise).mockResolvedValue(
    plan([move(1, OrganiseMoveState.MovePlanned), move(2, OrganiseMoveState.MoveSkipped, 'taken')]),
  )
  vi.mocked(engine.organiseRuns).mockResolvedValue([run(7, OrganiseRunState.OrganiseRunning, 0, 2)])
  const organise = useOrganise()
  await organise.preview()
  await organise.start()
  expect(engine.startOrganise).toHaveBeenCalledWith(7)
  expect(organise.plan).toBeNull()
  expect(organise.progress).toEqual({ runId: 7, state: OrganiseRunState.OrganiseRunning, done: 0, total: 1 })
  expect(organise.busy).toBe(true)
  expect(organise.lastAction).toBe('move')
  expect(organise.runs).toHaveLength(1)
})

it('keeps the preview and says why when the engine refuses to start', async () => {
  vi.mocked(engine.planOrganise).mockResolvedValue(plan([move(1, OrganiseMoveState.MovePlanned)]))
  vi.mocked(engine.startOrganise).mockRejectedValue(new Error('busy', { cause: { code: ErrorCode.OrganiseBusy } }))
  const organise = useOrganise()
  await organise.preview()
  await organise.start()
  expect(organise.error).toContain('already moving files')
  expect(organise.plan).not.toBeNull()
  expect(organise.progress).toBeNull()
})

it('does nothing when there is no preview to start', async () => {
  const organise = useOrganise()
  await organise.start()
  expect(engine.startOrganise).not.toHaveBeenCalled()
})

it('follows the engine’s progress events and reloads the runs once a run ends', async () => {
  const organise = useOrganise()
  vi.mocked(engine.organiseRuns).mockResolvedValue([run(7, OrganiseRunState.OrganiseDone, 5, 5)])
  emit({ runId: 7, state: OrganiseRunState.OrganiseRunning, done: 2, total: 5 })
  expect(organise.progress?.done).toBe(2)
  expect(organise.busy).toBe(true)
  expect(engine.organiseRuns).not.toHaveBeenCalled()

  emit({ runId: 7, state: OrganiseRunState.OrganiseDone, done: 5, total: 5 })
  await flushPromises()
  expect(organise.busy).toBe(false)
  expect(engine.organiseRuns).toHaveBeenCalledTimes(1)
  expect(organise.runs[0]?.state).toBe(OrganiseRunState.OrganiseDone)
})

it('is busy while any listed run is still moving files, even before an event arrives', async () => {
  vi.mocked(engine.organiseRuns).mockResolvedValue([run(3, OrganiseRunState.OrganiseUndoing, 4, 9)])
  const organise = useOrganise()
  await organise.loadRuns()
  expect(organise.busy).toBe(true)
})

it('resumes a stopped run for what is left, and undoes a run for what it moved', async () => {
  const organise = useOrganise()
  await organise.resume(run(5, OrganiseRunState.OrganiseFailed, 2, 6))
  expect(engine.startOrganise).toHaveBeenCalledWith(5)
  expect(organise.progress).toEqual({ runId: 5, state: OrganiseRunState.OrganiseRunning, done: 0, total: 4 })

  emit({ runId: 5, state: OrganiseRunState.OrganiseDone, done: 4, total: 4 })
  await flushPromises()
  await organise.undo(run(5, OrganiseRunState.OrganiseDone, 6, 6))
  expect(engine.undoOrganise).toHaveBeenCalledWith(5)
  expect(organise.progress).toEqual({ runId: 5, state: OrganiseRunState.OrganiseUndoing, done: 0, total: 6 })
  expect(organise.lastAction).toBe('undo')
})

it('reports a refused undo in its own error', async () => {
  vi.mocked(engine.undoOrganise).mockRejectedValue(new Error('state', { cause: { code: ErrorCode.OrganiseState } }))
  const organise = useOrganise()
  await organise.undo(run(5, OrganiseRunState.OrganiseUndone, 0, 6))
  expect(organise.error).toContain('can’t be started or undone')
  expect(organise.progress).toBeNull()
})

it('takes its example from the newest photo, with its event and place by name', async () => {
  onePhoto()
  vi.mocked(engine.asset).mockResolvedValue(detail(2, 'Pixel 8'))
  vi.mocked(engine.event).mockResolvedValue(event('Beach day'))
  vi.mocked(engine.place).mockResolvedValue(place('Karachi'))
  const organise = useOrganise()
  await organise.loadExample()
  expect(engine.timeline).toHaveBeenCalledWith({ filter: Filter.FilterAll })
  expect(organise.example).toEqual({
    takenAt: 1_722_868_200,
    event: 'Beach day',
    place: 'Karachi',
    camera: 'Pixel 8',
    path: 'D:\\Photos\\IMG_1.jpg',
  })
})

it('reads a blocked place or event as none in the example', async () => {
  onePhoto()
  vi.mocked(engine.asset).mockResolvedValue(detail(2))
  vi.mocked(engine.event).mockRejectedValue(new Error('not found'))
  vi.mocked(engine.place).mockRejectedValue(new Error('not found'))
  const organise = useOrganise()
  await organise.loadExample()
  expect(organise.example).toMatchObject({ event: '', place: '' })
})

it('has no example for an empty library', async () => {
  vi.mocked(engine.timeline).mockResolvedValue([])
  const organise = useOrganise()
  await organise.loadExample()
  expect(organise.example).toBeNull()
})

it('lists only enabled folders to choose from', async () => {
  vi.mocked(engine.sources).mockResolvedValue([
    { id: 1, path: 'D:\\A', enabled: true },
    { id: 2, path: 'D:\\B', enabled: false },
  ])
  const organise = useOrganise()
  await organise.loadSources()
  expect(organise.sources.map((s) => s.id)).toEqual([1])
})

it('does not let the answer to a start undo the run’s end event that arrived before it', async () => {
  vi.mocked(engine.planOrganise).mockResolvedValue(plan([move(1, OrganiseMoveState.MovePlanned)]))
  let answer: () => void = () => undefined
  vi.mocked(engine.startOrganise).mockReturnValue(
    new Promise<void>((resolve) => {
      answer = resolve
    }),
  )
  const organise = useOrganise()
  await organise.preview()
  const starting = organise.start()
  // The run moved its one file and ended before the call was answered.
  emit({ runId: 7, state: OrganiseRunState.OrganiseDone, done: 1, total: 1 })
  answer()
  await starting
  expect(organise.progress).toEqual({ runId: 7, state: OrganiseRunState.OrganiseDone, done: 1, total: 1 })
  expect(organise.busy).toBe(false)
})

it('shows a run as started the moment it is asked for, and takes that back if the engine refuses', async () => {
  vi.mocked(engine.planOrganise).mockResolvedValue(plan([move(1, OrganiseMoveState.MovePlanned)]))
  let refuse: (e: Error) => void = () => undefined
  vi.mocked(engine.startOrganise).mockReturnValue(
    new Promise<void>((_resolve, reject) => {
      refuse = reject
    }),
  )
  const organise = useOrganise()
  await organise.preview()
  const starting = organise.start()
  expect(organise.progress).toEqual({ runId: 7, state: OrganiseRunState.OrganiseRunning, done: 0, total: 1 })
  expect(organise.busy).toBe(true)
  refuse(new Error('busy', { cause: { code: ErrorCode.OrganiseBusy } }))
  await starting
  expect(organise.progress).toBeNull()
  expect(organise.busy).toBe(false)
})

it('keeps what an event said while a refused start was being answered', async () => {
  vi.mocked(engine.planOrganise).mockResolvedValue(plan([move(1, OrganiseMoveState.MovePlanned)]))
  let refuse: (e: Error) => void = () => undefined
  vi.mocked(engine.startOrganise).mockReturnValue(
    new Promise<void>((_resolve, reject) => {
      refuse = reject
    }),
  )
  const organise = useOrganise()
  await organise.preview()
  const starting = organise.start()
  emit({ runId: 3, state: OrganiseRunState.OrganiseRunning, done: 5, total: 9 }) // another run, still going
  refuse(new Error('busy', { cause: { code: ErrorCode.OrganiseBusy } }))
  await starting
  expect(organise.progress).toEqual({ runId: 3, state: OrganiseRunState.OrganiseRunning, done: 5, total: 9 })
})

it('ignores a late event saying a run is still going once its end has arrived, until it is asked for again', async () => {
  const organise = useOrganise()
  emit({ runId: 7, state: OrganiseRunState.OrganiseDone, done: 4, total: 4 })
  emit({ runId: 7, state: OrganiseRunState.OrganiseRunning, done: 3, total: 4 })
  expect(organise.progress?.state).toBe(OrganiseRunState.OrganiseDone)
  expect(organise.busy).toBe(false)

  await organise.undo(run(7, OrganiseRunState.OrganiseDone, 4, 4))
  emit({ runId: 7, state: OrganiseRunState.OrganiseUndoing, done: 1, total: 4 })
  expect(organise.progress).toEqual({ runId: 7, state: OrganiseRunState.OrganiseUndoing, done: 1, total: 4 })
})

it('keeps read failures separate from a move error and clears them when retried', async () => {
  const organise = useOrganise()
  organise.error = 'A move could not start'
  vi.mocked(engine.organiseRuns).mockRejectedValueOnce(new Error('History unavailable'))
  await organise.loadRuns()
  expect(organise.runsError).toBe('Something went wrong: History unavailable')
  expect(organise.error).toBe('A move could not start')
  vi.mocked(engine.organiseRuns).mockResolvedValue([])
  await organise.loadRuns()
  expect(organise.runsError).toBe('')
  vi.mocked(engine.sources).mockRejectedValueOnce(new Error('Sources unavailable'))
  await organise.loadSources()
  expect(organise.sourceError).toBe('Something went wrong: Sources unavailable')
})

it('refuses a stale confirmed start while another run is already working', async () => {
  const organise = useOrganise()
  organise.plan = plan([move(1, OrganiseMoveState.MovePlanned)])
  organise.progress = { runId: 9, state: OrganiseRunState.OrganiseRunning, done: 1, total: 10 }
  await organise.start()
  expect(engine.startOrganise).not.toHaveBeenCalled()
  expect(organise.plan.runId).toBe(7)
})

it('returns from a finished result to the unchanged scheme without losing history', () => {
  const organise = useOrganise()
  organise.template = '{Year}/{Name}'
  organise.runs = [run(7, OrganiseRunState.OrganiseDone, 2, 2)]
  organise.progress = { runId: 7, state: OrganiseRunState.OrganiseDone, done: 2, total: 2 }
  organise.started = plan([move(1, OrganiseMoveState.MovePlanned)])
  organise.lastAction = 'move'
  organise.error = 'Previous failure'
  organise.dismissRun()
  expect(organise.progress).toBeNull()
  expect(organise.started).toBeNull()
  expect(organise.lastAction).toBeNull()
  expect(organise.error).toBe('')
  expect(organise.template).toBe('{Year}/{Name}')
  expect(organise.runs).toHaveLength(1)
})

it('keeps a live run when asked to dismiss, resume or undo another run', async () => {
  const organise = useOrganise()
  const active = { runId: 7, state: OrganiseRunState.OrganiseRunning, done: 1, total: 2 }
  organise.progress = active
  organise.dismissRun()
  await organise.resume(run(8, OrganiseRunState.OrganiseFailed, 1, 2))
  await organise.undo(run(9, OrganiseRunState.OrganiseDone, 2, 2))
  expect(organise.progress).toEqual(active)
  expect(engine.startOrganise).not.toHaveBeenCalled()
  expect(engine.undoOrganise).not.toHaveBeenCalled()
})
