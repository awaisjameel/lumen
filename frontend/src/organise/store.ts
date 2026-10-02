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
import { errorText, logError } from '../shared/errors'
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
  const progress = ref<OrganiseProgress | null>(null)
  const example = shallowRef<Example | null>(null)
  const planning = ref(false)
  const error = ref('') // why the last thing the user asked for failed
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
  const loadRuns = () => readRuns().catch(logError)

  async function loadSources() {
    try {
      sources.value = (await engine.sources()).filter((s) => s.enabled)
    } catch (e) {
      logError(e)
    }
  }

  /** A real photo to show examples from: the newest one. Without one the editor says so. */
  async function loadExample() {
    try {
      const view = { filter: Filter.FilterAll }
      const day = (await engine.timeline(view))[0]?.day
      if (!day) return
      const id = (await engine.days(day, day, view))[0]?.items[0]?.id
      if (id === undefined) return
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
    } catch (e) {
      logError(e)
    }
  }

  /** A preview no longer matches the scheme or folder being edited. */
  function discardPlan() {
    plan.value = null
  }

  /** Works out the moves the scheme would make (a dry run: nothing moves). */
  async function preview() {
    planning.value = true
    error.value = ''
    try {
      plan.value = await engine.planOrganise(template.value, sourceId.value)
    } catch (e) {
      plan.value = null
      error.value = errorText(e)
    } finally {
      planning.value = false
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
    try {
      await call()
      progress.value = { runId, state: started, done: 0, total }
      plan.value = null
    } catch (e) {
      error.value = errorText(e)
    }
    await loadRuns()
  }

  /** Moves the files of the preview. */
  async function start() {
    const current = plan.value
    if (!current) return
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
    await ask('undo', () => engine.undoOrganise(run.id), OrganiseRunState.OrganiseUndoing, run.id, run.moved)
  }

  function onProgress(p: OrganiseProgress) {
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
    planning,
    error,
    lastAction,
    toMove,
    skipped,
    busy,
    loadRuns,
    loadSources,
    loadExample,
    discardPlan,
    preview,
    start,
    resume,
    undo,
  }
})
