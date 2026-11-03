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
export interface UndoOutcome {
  id: number
  state: UndoState
  error: string
}
export type UndoResult = UndoOutcome & { state: Exclude<UndoState, 'pending'> }

export const batchUndoOutcomes = (rows: readonly BatchOutcome[]): UndoResult[] =>
  rows.map((row) => ({
    id: row.id,
    state: row.error ? 'failed' : 'done',
    error: row.error ? failureText(row.code, row.error) : '',
  }))

export interface UndoJob {
  readonly id: string
  readonly title: string
  readonly total: number
  readonly busy: boolean
  readonly available: boolean
  readonly stopping: boolean
  readonly refreshError: string
  readonly counts: Readonly<Record<UndoState, number>>
  readonly outcomes: readonly UndoOutcome[]
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
export function offerBatchUndo(options: {
  title: string
  undoTitle: string
  ids: readonly number[]
  apply: (ids: number[], stopped: () => boolean) => Promise<UndoResult[]>
  reconcile: () => Promise<unknown>
  release?: () => void
}): UndoJob {
  const ids = [...new Set(options.ids)]
  const counts = shallowReactive<Record<UndoState, number>>({
    pending: ids.length,
    done: 0,
    failed: 0,
    cancelled: 0,
    superseded: 0,
  })
  const outcomes = shallowReactive<UndoOutcome[]>(ids.map((id) => ({ id, state: 'pending', error: '' })))
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
  function put(row: UndoOutcome) {
    const index = positions.get(row.id)
    if (index === undefined) return
    const previous = outcomes[index]
    if (!previous) return
    counts[previous.state]--
    counts[row.state]++
    outcomes[index] = row
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
        for (let from = 0; from < targets.length; from += SELECTION_PART) {
          const part = targets.slice(from, from + SELECTION_PART)
          if (stopped()) {
            for (const id of targets.slice(from)) put({ id, state: 'cancelled', error: '' })
            break
          }
          try {
            const results = new Map((await options.apply(part, stopped)).map((row) => [row.id, row]))
            for (const id of part) put(results.get(id) ?? { id, state: 'failed', error: t('error.unexpected') })
          } catch (e) {
            const error = t('undo.unconfirmed', { error: errorText(e) })
            for (const id of part) put({ id, state: 'failed', error })
          }
          publish()
          if (from + SELECTION_PART < targets.length && !stopped()) await yieldToInput()
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
  const job: UndoJob = Object.assign(state, {
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
