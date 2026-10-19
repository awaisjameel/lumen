import { defineStore } from 'pinia'
import { computed, onScopeDispose, ref, shallowRef } from 'vue'
import {
  engine,
  Filter,
  onOrganiseProgress,
  ORGANISE_DEFAULT_TEMPLATE,
  OrganiseMoveState,
  OrganiseRunState,
} from '../bridge'
import type { OrganisePlan, OrganiseProgress, OrganiseRun, Source } from '../bridge'
import { errorText } from '../shared/errors'
import { coalescedLoad } from '../shared/listStore'
import type { Example } from './template'

/** Whether a run is moving files or putting them back. */
function working(state: OrganiseRunState): boolean {
  return state === OrganiseRunState.OrganiseRunning || state === OrganiseRunState.OrganiseUndoing
}

/**
 * Organise on disk: the scheme being edited, its preview, the run in
 * progress (the engine's organise:progress events) and the runs so far. The
 * events keep arriving while another page is open, so the state lives here,
 * not in the view.
 */
export const useOrganise = defineStore('organise', () => {
  const template = ref(ORGANISE_DEFAULT_TEMPLATE)
  const sourceId = ref(0) // 0: every folder
  const sources = shallowRef<Source[]>([])
  const plan = shallowRef<OrganisePlan | null>(null)
  const runs = shallowRef<OrganiseRun[]>([])
  const progress = shallowRef<OrganiseProgress | null>(null)
  const example = shallowRef<Example | null>(null)
  // The example photo's thumbnail, to show beside the path the scheme gives it.
  const exampleCover = shallowRef<{ id: number; thumb: number } | null>(null)
  // The preview a run was started from, to sum up what it did once it ends.
  const started = shallowRef<OrganisePlan | null>(null)
  const planning = ref(false)
  const runsError = ref('')
  const sourceError = ref('')
  const exampleError = ref('')
  const previewError = ref('') // why the last preview could not be worked out
  const error = ref('') // why the last move or undo the user asked for failed
  // What the user last asked for, to word how it ended.
  const lastAction = ref<'move' | 'undo' | null>(null)

  const toMove = computed(() => plan.value?.moves.filter((m) => m.state === OrganiseMoveState.MovePlanned).length ?? 0)
  const skipped = computed(() => plan.value?.moves.filter((m) => m.state === OrganiseMoveState.MoveSkipped).length ?? 0)
  const busy = computed(
    () => (progress.value !== null && working(progress.value.state)) || runs.value.some((r) => working(r.state)),
  )

  const readRuns = coalescedLoad(
    () => engine.organiseRuns(),
    (got) => {
      runs.value = got
    },
  )
  async function loadRuns() {
    runsError.value = ''
    try {
      await readRuns()
    } catch (e) {
      runsError.value = errorText(e)
    }
  }

  async function loadSources() {
    sourceError.value = ''
    try {
      sources.value = (await engine.sources()).filter((s) => s.enabled)
    } catch (e) {
      sourceError.value = errorText(e)
    }
  }

  /** Clears the completed task while keeping the scheme and local history. */
  function dismissRun() {
    if (busy.value) return
    progress.value = null
    started.value = null
    lastAction.value = null
    error.value = ''
    discardPlan()
  }

  /** A real photo to show examples from: the newest one. Without one the editor says so. */
  async function loadExample() {
    exampleError.value = ''
    try {
      const view = { filter: Filter.FilterAll }
      const day = (await engine.timeline(view))[0]?.day
      if (!day) return
      const item = (await engine.days(day, day, view))[0]?.items[0]
      if (!item) return
      const id = item.id
      const detail = await engine.asset(id)
      const path = detail.paths[0]
      if (path === undefined) return
      // The event and place are shown by name where the photo has them; a
      // blocked place or event is refused and reads as none, as in the plan.
      const [event, place] = await Promise.all([
        engine.event(id).then(
          (e) => e.name,
          () => '',
        ),
        detail.placeId
          ? engine.place(detail.placeId).then(
              (p) => p.name,
              () => '',
            )
          : Promise.resolve(''),
      ])
      example.value = { takenAt: detail.takenAt, event, place, camera: detail.camera, path }
      exampleCover.value = { id, thumb: item.t }
    } catch (e) {
      exampleError.value = errorText(e)
    }
  }

  /** A preview no longer matches the scheme or folder being edited; one being worked out is abandoned. */
  let planRevision = 0
  function discardPlan() {
    planRevision++
    plan.value = null
    planning.value = false
  }

  /** Works out the moves the scheme would make (a dry run: nothing moves). */
  async function preview() {
    const revision = ++planRevision
    planning.value = true
    previewError.value = ''
    try {
      const planned = await engine.planOrganise(template.value, sourceId.value)
      if (revision === planRevision) plan.value = planned
    } catch (e) {
      if (revision !== planRevision) return
      plan.value = null
      previewError.value = errorText(e)
    } finally {
      if (revision === planRevision) planning.value = false
    }
  }

  /** Runs one thing the user asked of a run, and then refreshes the list of runs. */
  async function ask(
    action: 'move' | 'undo',
    call: () => Promise<void>,
    started: OrganiseRunState,
    runId: number,
    total: number,
  ) {
    error.value = ''
    lastAction.value = action
    // The run is shown as started before the call is answered: it may move
    // every file, and say so with its last event, before the answer arrives,
    // and the answer must not take the page back to the start.
    const before = progress.value
    const seed: OrganiseProgress = { runId, state: started, done: 0, total }
    progress.value = seed
    try {
      await call()
      plan.value = null
    } catch (e) {
      error.value = errorText(e)
      // Nothing started: undo the seed, unless an event has replaced it.
      if (progress.value === seed) progress.value = before
    }
    await loadRuns()
  }

  /** Moves the files of the preview. */
  async function start() {
    const current = plan.value
    if (!current || busy.value) return
    started.value = current
    await ask(
      'move',
      () => engine.startOrganise(current.runId),
      OrganiseRunState.OrganiseRunning,
      current.runId,
      toMove.value,
    )
  }

  /** Carries on with a run that stopped part-way. */
  async function resume(run: OrganiseRun) {
    if (busy.value) return
    await ask(
      'move',
      () => engine.startOrganise(run.id),
      OrganiseRunState.OrganiseRunning,
      run.id,
      run.total - run.moved,
    )
  }

  /** Puts the files of a run back. */
  async function undo(run: OrganiseRun) {
    if (busy.value) return
    await ask('undo', () => engine.undoOrganise(run.id), OrganiseRunState.OrganiseUndoing, run.id, run.moved)
  }

  function onProgress(p: OrganiseProgress) {
    const current = progress.value
    // A run's last event stands: a late event of it that says it is still
    // going (events can arrive out of order) must not bring it back to life.
    // Asking again (resume, undo) starts afresh with a seed of its own.
    if (current?.runId === p.runId && !working(current.state) && working(p.state)) return
    progress.value = p
    if (!working(p.state)) void loadRuns()
  }
  onScopeDispose(onOrganiseProgress(onProgress))

  return {
    template,
    sourceId,
    sources,
    plan,
    runs,
    progress,
    example,
    exampleCover,
    started,
    planning,
    previewError,
    error,
    lastAction,
    runsError,
    sourceError,
    exampleError,
    toMove,
    skipped,
    busy,
    loadRuns,
    loadSources,
    loadExample,
    discardPlan,
    dismissRun,
    preview,
    start,
    resume,
    undo,
  }
})
