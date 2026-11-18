import { shallowReactive, shallowRef } from 'vue'
import type { BatchOutcome } from '../bridge'
import { errorText, failureText } from './errors'
import { formatNumber, t } from './i18n'
import { notify } from './notify'
import type { NotificationAction } from './notify'

/** One bridge read/write unit, shared by selection capture and its inverse. */
export const SELECTION_PART = 200

/** Local/superseded units must yield to input as well as bridge calls do. */
function yieldToInput(): Promise<void> {
  return new Promise((resolve) => {
    const channel = new MessageChannel()
    channel.port1.onmessage = () => {
      channel.port1.close()
      channel.port2.close()
      resolve()
    }
    channel.port2.postMessage(null)
  })
}

export type UndoState = 'pending' | 'done' | 'failed' | 'cancelled' | 'superseded'
export type UndoID = number | string
export interface UndoOutcome<ID extends UndoID = number> {
  readonly id: ID
  readonly label?: string
  readonly state: UndoState
  readonly error: string
}
export type UndoResult<ID extends UndoID = number> = UndoOutcome<ID> & { state: Exclude<UndoState, 'pending'> }

export const batchUndoOutcomes = (rows: readonly BatchOutcome[]): UndoResult[] =>
  rows.map((row) => ({
    id: row.id,
    state: row.error ? 'failed' : 'done',
    error: row.error ? failureText(row.code, row.error) : '',
  }))

/** Only targets actually submitted to an unconfirmed write are mutation failures. */
export function failedUndoOutcomes<ID extends UndoID>(ids: readonly ID[], error: unknown): UndoResult<ID>[] {
  const message = t('undo.unconfirmed', { error: errorText(error) })
  return ids.map((id) => ({ id, state: 'failed', error: message }))
}

export async function attemptBatchUndo(ids: number[], write: () => Promise<BatchOutcome[]>): Promise<UndoResult[]> {
  if (!ids.length) return []
  try {
    return batchUndoOutcomes(await write())
  } catch (e) {
    return failedUndoOutcomes(ids, e)
  }
}

export interface UndoJob<ID extends UndoID = UndoID> {
  readonly id: string
  readonly title: string
  readonly total: number
  readonly busy: boolean
  readonly available: boolean
  readonly stopping: boolean
  readonly refreshError: string
  readonly counts: Readonly<Record<UndoState, number>>
  readonly outcomes: readonly UndoOutcome<ID>[]
  run: () => Promise<void>
  retry: () => Promise<void>
  resume: () => Promise<void>
  refresh: () => Promise<void>
  cancel: () => void
  show: () => void
}

/** The shell owns presentation; the captured inverse survives view disposal. */
export const undoOutcomes = shallowRef<UndoJob | null>(null)

export function undoCaption(job: UndoJob): string {
  return t('undo.summary', {
    done: formatNumber(job.counts.done),
    failed: formatNumber(job.counts.failed),
    cancelled: formatNumber(job.counts.cancelled),
    superseded: formatNumber(job.counts.superseded),
    pending: formatNumber(job.counts.pending),
  })
}

/**
 * A bounded, ephemeral inverse. Mutation outcomes survive retry and refresh
 * failure independently; only failed or explicitly resumed rows are replayed.
 * Dismissing/expiring the notice discards recovery and stops future units.
 */
interface UndoOptions<ID extends UndoID> {
  title: string
  undoTitle: string
  ids: readonly ID[]
  label?: (id: ID) => string
  unitSize?: 1 | typeof SELECTION_PART
  apply: (ids: ID[], stopped: () => boolean) => Promise<UndoResult<ID>[]>
  reconcile: () => Promise<unknown>
  release?: () => void
}

export function offerBatchUndo(options: UndoOptions<number>): UndoJob<number>
export function offerBatchUndo(options: UndoOptions<string>): UndoJob<string>
export function offerBatchUndo<ID extends UndoID>(options: UndoOptions<ID>): UndoJob<ID> {
  const ids = [...new Set(options.ids)]
  const unitSize = options.unitSize ?? SELECTION_PART
  const counts = shallowReactive<Record<UndoState, number>>({
    pending: ids.length,
    done: 0,
    failed: 0,
    cancelled: 0,
    superseded: 0,
  })
  const outcomes = shallowReactive<UndoOutcome<ID>[]>(
    ids.map((id) => ({ id, state: 'pending', error: '', ...(options.label ? { label: options.label(id) } : {}) })),
  )
  const positions = new Map(ids.map((id, index) => [id, index]))
  const noticeID = `undo.${crypto.randomUUID()}`
  const state = shallowReactive({
    id: noticeID,
    title: options.undoTitle,
    total: ids.length,
    counts,
    outcomes,
    busy: false,
    available: true,
    stopping: false,
    refreshError: '',
  })
  let running: Promise<void> | null = null
  let released = false
  function release() {
    if (released) return
    released = true
    options.release?.()
  }
  function discard() {
    state.available = false
    state.stopping = true
    release()
  }
  function put(row: UndoOutcome<ID>) {
    const index = positions.get(row.id)
    if (index === undefined) return
    const previous = outcomes[index]
    if (!previous) return
    counts[previous.state]--
    counts[row.state]++
    outcomes[index] = previous.label === undefined ? row : { ...row, label: previous.label }
  }
  function publish() {
    if (!state.available) return
    const actions: NotificationAction[] = [{ label: t('library.outcomes'), dismiss: false, run: () => job.show() }]
    if (state.busy) {
      actions.push({
        label: t('files.cancelRemaining'),
        dismiss: false,
        disabled: state.stopping,
        run: () => job.cancel(),
      })
    } else {
      if (counts.failed) actions.push({ label: t('undo.retry'), dismiss: false, run: () => void job.retry() })
      if (counts.cancelled) actions.push({ label: t('undo.resume'), dismiss: false, run: () => void job.resume() })
      if (state.refreshError) actions.push({ label: t('undo.refresh'), dismiss: false, run: () => void job.refresh() })
    }
    notify({
      id: noticeID,
      title: state.title,
      description: [undoCaption(job), state.refreshError && t('undo.refreshFailed', { error: state.refreshError })]
        .filter(Boolean)
        .join(' '),
      kind: counts.failed || state.refreshError ? 'error' : state.busy || counts.cancelled ? 'info' : 'success',
      duration: state.busy || counts.failed || counts.cancelled || state.refreshError ? 0 : 6000,
      work: state.busy ? { done: ids.length - counts.pending, total: ids.length } : undefined,
      actions,
      closed: discard,
    })
  }
  function operate(wanted: UndoState | null): Promise<void> {
    if (!state.available) return Promise.resolve()
    if (running) return running
    const targets = wanted ? outcomes.filter((row) => row.state === wanted).map((row) => row.id) : []
    if (wanted && !targets.length) return Promise.resolve()
    state.busy = true
    state.stopping = false
    if (wanted !== 'pending') for (const id of targets) put({ id, state: 'pending', error: '' })
    const stopped = () => state.stopping || !state.available
    publish()
    running = (async () => {
      try {
        for (let from = 0; from < targets.length; from += unitSize) {
          const part = targets.slice(from, from + unitSize)
          if (stopped()) {
            for (const id of targets.slice(from)) put({ id, state: 'cancelled', error: '' })
            break
          }
          try {
            const results = new Map((await options.apply(part, stopped)).map((row) => [row.id, row]))
            for (const id of part) put(results.get(id) ?? { id, state: 'failed', error: t('error.unexpected') })
          } catch (e) {
            for (const row of failedUndoOutcomes(part, e)) put(row)
          }
          publish()
          if (from + unitSize < targets.length && !stopped()) await yieldToInput()
        }
      } finally {
        // Reconciliation has its own outcome: retrying it must never repeat a
        // committed inverse, even when an earlier unit failed or was cancelled.
        try {
          await options.reconcile()
          state.refreshError = ''
        } catch (e) {
          state.refreshError = errorText(e)
        }
        state.busy = false
        running = null
        if (!counts.pending && !counts.failed && !counts.cancelled) release()
        publish()
      }
    })()
    return running
  }
  const job: UndoJob<ID> = Object.assign(state, {
    run: () => operate('pending'),
    retry: () => operate('failed'),
    resume: () => operate('cancelled'),
    refresh: () => operate(null),
    cancel: () => {
      state.stopping = true
      publish()
    },
    show: () => {
      undoOutcomes.value = job
    },
  })
  notify({
    id: noticeID,
    kind: 'success',
    title: options.title,
    duration: 8000,
    actions: [{ label: t('settings.undo'), dismiss: false, run: () => void job.run() }],
    closed: discard,
  })
  return job
}
