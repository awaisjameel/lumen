<script setup lang="ts">
// Organise files on disk: move photos into folders by a scheme the user
// writes, after a dry-run preview, with live progress and a full undo. The
// engine does all the moving (internal/fileops); this page only asks.
import { useVirtualizer } from '@tanstack/vue-virtual'
import { storeToRefs } from 'pinia'
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { ORGANISE_TOKENS, OrganiseMoveState, OrganiseReason, OrganiseRunState } from '../bridge'
import type { OrganiseMove, OrganiseRun } from '../bridge'
import { confirm } from '../shared/confirm'
import { LOCALE, t } from '../shared/i18n'
import type { MessageKey } from '../shared/i18n'
import { renderExample, unknownTokens } from './template'
import { useOrganise } from './store'

const organise = useOrganise()
const {
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
} = storeToRefs(organise)

const schemeInput = ref<HTMLInputElement | null>(null)

onMounted(() => {
  void organise.loadRuns()
  void organise.loadSources()
  void organise.loadExample()
})
// A preview is of one scheme and one folder choice: editing either ends it.
watch([template, sourceId], () => organise.discardPlan())

/** A token as it is written in a scheme. */
const braced = (token: string) => `{${token}}`
const unknown = computed(() => unknownTokens(template.value))
const exampleText = computed(() => (example.value ? renderExample(template.value, example.value) : ''))

/** Puts a token into the scheme where the cursor is. */
async function insertToken(token: string) {
  const input = schemeInput.value
  const text = braced(token)
  const start = input?.selectionStart ?? template.value.length
  const end = input?.selectionEnd ?? start
  template.value = template.value.slice(0, start) + text + template.value.slice(end)
  await nextTick()
  input?.focus()
  input?.setSelectionRange(start + text.length, start + text.length)
}

const reasons: readonly string[] = Object.values(OrganiseReason)

/** Why a move is left out, in words; Windows's own message when it is not one of ours. */
function reasonText(move: OrganiseMove): string {
  const error = move.error ?? ''
  if (!reasons.includes(error)) return error
  switch (error) {
    case OrganiseReason.Missing:
      return t('organise.reason.missing')
    case OrganiseReason.NotVisible:
      return t('organise.reason.not_visible')
    case OrganiseReason.BlockedFolder:
      return t('organise.reason.blocked_folder')
    case OrganiseReason.Changed:
      return t('organise.reason.changed')
    case OrganiseReason.Taken:
      return t('organise.reason.taken')
    case OrganiseReason.TooLong:
      return t('organise.reason.too_long')
    default:
      return t('organise.reason.not_a_folder')
  }
}

// The preview can hold every file of a library, so only the rows in view exist.
const rowHeight = 60
const scroller = ref<HTMLElement | null>(null)
const virtualizer = useVirtualizer(
  computed(() => ({
    count: plan.value?.moves.length ?? 0,
    getScrollElement: () => scroller.value,
    getItemKey: (index: number) => plan.value?.moves[index]?.seq ?? index,
    estimateSize: () => rowHeight,
    overscan: 6,
  })),
)
const visibleMoves = computed(() =>
  virtualizer.value.getVirtualItems().flatMap((item) => {
    const move = plan.value?.moves[item.index]
    return move ? [{ item, move }] : []
  }),
)

const moving = computed(() => progress.value !== null && busy.value)
const progressText = computed(() => {
  const p = progress.value
  if (!p) return ''
  const counts = { done: p.done, total: p.total }
  return t(p.state === OrganiseRunState.OrganiseUndoing ? 'organise.puttingBack' : 'organise.moving', counts)
})
const finished = computed(() => {
  const p = progress.value
  if (!p || moving.value) return ''
  switch (p.state) {
    case OrganiseRunState.OrganiseDone:
      return t(lastAction.value === 'undo' ? 'organise.finished.undoneSome' : 'organise.finished.done')
    case OrganiseRunState.OrganiseUndone:
      return t('organise.finished.undone')
    case OrganiseRunState.OrganiseFailed:
      return t('organise.finished.failed')
    default:
      return ''
  }
})

async function startRun() {
  const n = toMove.value
  if (!(await confirm(t('organise.confirmTitle', { n }), t('organise.confirmText'), t('organise.confirmOk')))) return
  await organise.start()
}

async function undoRun(run: OrganiseRun) {
  if (!(await confirm(t('organise.undoTitle'), t('organise.undoText'), t('organise.undoOk')))) return
  await organise.undo(run)
}

function stateText(state: OrganiseRunState): MessageKey {
  switch (state) {
    case OrganiseRunState.OrganiseRunning:
      return 'organise.runState.running'
    case OrganiseRunState.OrganiseDone:
      return 'organise.runState.done'
    case OrganiseRunState.OrganiseUndoing:
      return 'organise.runState.undoing'
    case OrganiseRunState.OrganiseUndone:
      return 'organise.runState.undone'
    case OrganiseRunState.OrganiseFailed:
      return 'organise.runState.failed'
    default:
      return 'organise.runState.planned'
  }
}

// A run's time is a real moment (unlike a photo's date), shown in this PC's zone.
const whenFmt = new Intl.DateTimeFormat(LOCALE, { dateStyle: 'medium', timeStyle: 'short' })
const when = (seconds: number) => whenFmt.format(new Date(seconds * 1000))

const canUndo = (run: OrganiseRun) =>
  run.moved > 0 &&
  (run.state === OrganiseRunState.OrganiseDone ||
    run.state === OrganiseRunState.OrganiseFailed ||
    run.state === OrganiseRunState.OrganiseUndoing)
</script>

<template>
  <div class="page">
    <header class="topbar">
      <h1>{{ t('organise.title') }}</h1>
    </header>
    <div class="page-body">
      <RouterLink to="/settings" class="btn btn-subtle">{{ t('organise.back') }}</RouterLink>
      <p class="muted">{{ t('organise.intro') }}</p>
      <p v-if="error" class="page-error" role="alert">{{ error }}</p>

      <section class="scheme">
        <label for="organise-scheme">{{ t('organise.scheme') }}</label>
        <input id="organise-scheme" ref="schemeInput" v-model="template" type="text" spellcheck="false" />
        <p class="muted hint">{{ t('organise.schemeHint') }}</p>
        <div class="chips" role="group" :aria-label="t('organise.tokens')">
          <button
            v-for="token in ORGANISE_TOKENS"
            :key="token"
            type="button"
            class="chip"
            :title="t(`organise.token.${token}`)"
            @click="insertToken(token)"
          >
            {{ braced(token) }}
          </button>
        </div>
        <p v-for="token in unknown" :key="token" class="page-error" role="alert">
          {{ t('organise.unknownToken', { token: braced(token) }) }}
        </p>
        <p class="example" aria-live="polite">
          <template v-if="exampleText">{{ t('organise.example', { path: exampleText }) }}</template>
          <template v-else-if="!example">{{ t('organise.exampleNone') }}</template>
        </p>

        <label for="organise-source">{{ t('organise.source') }}</label>
        <select id="organise-source" v-model.number="sourceId">
          <option :value="0">{{ t('organise.allSources') }}</option>
          <option v-for="source in sources" :key="source.id" :value="source.id">{{ source.path }}</option>
        </select>
        <div class="actions">
          <button class="btn btn-primary" :disabled="planning || busy || !template.trim()" @click="organise.preview()">
            {{ t('organise.preview') }}
          </button>
        </div>
      </section>

      <p v-if="planning" class="muted" role="status">{{ t('organise.previewing') }}</p>

      <section v-if="plan" class="preview">
        <ul class="summary">
          <li v-if="toMove">{{ t('organise.willMove', { n: toMove }) }}</li>
          <li v-if="plan.inPlace">{{ t('organise.inPlace', { n: plan.inPlace }) }}</li>
          <li v-if="plan.renamed">{{ t('organise.renamed', { n: plan.renamed }) }}</li>
          <li v-if="skipped">{{ t('organise.skipped', { n: skipped }) }}</li>
        </ul>
        <p v-if="!plan.moves.length" class="muted">{{ t('organise.nothing') }}</p>
        <div
          v-else
          ref="scroller"
          class="moves"
          role="list"
          tabindex="0"
          :aria-label="t('organise.moves')"
          :aria-rowcount="plan.moves.length"
        >
          <div class="moves-inner" :style="{ height: `${virtualizer.getTotalSize()}px` }">
            <div
              v-for="{ item, move } in visibleMoves"
              :key="move.seq"
              class="move"
              :class="{ left: move.state === OrganiseMoveState.MoveSkipped }"
              role="listitem"
              :style="{ height: `${rowHeight}px`, transform: `translateY(${item.start}px)` }"
            >
              <span class="path" :title="move.from">{{ move.from }}</span>
              <span v-if="move.state === OrganiseMoveState.MoveSkipped" class="path reason">{{
                reasonText(move)
              }}</span>
              <span v-else class="path" :title="move.to">
                <span class="sr-only">{{ t('organise.movesTo') }}</span
                ><span aria-hidden="true">→</span> {{ move.to }}
                <span v-if="move.renamed" class="tag">{{ t('organise.renamedTag') }}</span>
              </span>
            </div>
          </div>
        </div>
        <div class="actions">
          <button class="btn btn-primary" :disabled="!toMove || busy" @click="startRun">
            {{ t('organise.move', { n: toMove }) }}
          </button>
        </div>
      </section>

      <section v-if="moving && progress" class="progress">
        <progress :value="progress.done" :max="Math.max(progress.total, 1)" :aria-label="progressText"></progress>
        <p>{{ progressText }}</p>
      </section>
      <p v-else-if="finished" class="finished" role="status">{{ finished }}</p>

      <section class="runs">
        <h2>{{ t('organise.runs') }}</h2>
        <p v-if="!runs.length" class="muted">{{ t('organise.noRuns') }}</p>
        <ul v-else class="list">
          <li v-for="run in runs" :key="run.id">
            <div class="item-name">
              <strong>{{ when(run.createdAt) }}</strong>
              <small
                >{{ t(stateText(run.state)) }} · {{ t('organise.runMoved', { moved: run.moved, total: run.total }) }} ·
                {{ run.template }}</small
              >
            </div>
            <button
              v-if="run.state === OrganiseRunState.OrganiseFailed"
              class="btn"
              :disabled="busy"
              @click="organise.resume(run)"
            >
              {{ t('organise.resume') }}
            </button>
            <button v-if="canUndo(run)" class="btn" :disabled="busy" @click="undoRun(run)">
              {{ t('organise.undo') }}
            </button>
          </li>
        </ul>
      </section>
    </div>
  </div>
</template>

<style scoped>
.scheme {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 6px;
  margin: 12px 0 16px;
}

.scheme input[type='text'] {
  width: min(560px, 100%);
  font-family: ui-monospace, 'Cascadia Mono', Consolas, monospace;
}

.hint {
  margin: 0;
}

.chips {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.chip {
  padding: 3px 10px;
  border: 1px solid var(--border);
  border-radius: 999px;
  background: var(--surface);
  color: var(--text);
  font:
    12px/1.4 ui-monospace,
    'Cascadia Mono',
    Consolas,
    monospace;
  cursor: pointer;
}

.chip:hover {
  background: var(--surface-hover);
}

.example {
  margin: 0;
  min-height: 1.4em;
  color: var(--text-2);
  font-family: ui-monospace, 'Cascadia Mono', Consolas, monospace;
  overflow-wrap: anywhere;
}

.preview {
  margin: 8px 0 16px;
}

.summary {
  margin: 0 0 8px;
  padding-left: 18px;
}

.moves {
  height: 320px;
  overflow-y: auto;
  border: 1px solid var(--border);
  border-radius: 8px;
}

.moves-inner {
  position: relative;
  width: 100%;
}

.move {
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  display: flex;
  flex-direction: column;
  justify-content: center;
  gap: 2px;
  padding: 0 12px;
  border-bottom: 1px solid var(--border);
  font-size: 13px;
}

.path {
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}

.move .path:first-child {
  color: var(--text-2);
}

.move.left .reason {
  color: var(--danger);
}

.tag {
  margin-left: 6px;
  padding: 0 6px;
  border: 1px solid var(--border);
  border-radius: 999px;
  font-size: 11px;
}

.progress {
  margin: 8px 0 16px;
}

.progress progress {
  width: min(560px, 100%);
}

.finished {
  margin: 8px 0 16px;
}

.runs .list {
  margin: 0;
  padding: 0;
  list-style: none;
}

.runs li {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 0;
  border-bottom: 1px solid var(--border);
}

.item-name {
  display: flex;
  flex: 1;
  flex-direction: column;
  min-width: 0;
}

.item-name small {
  overflow: hidden;
  color: var(--text-2);
  text-overflow: ellipsis;
  white-space: nowrap;
}

.actions {
  display: flex;
  gap: 8px;
  margin-top: 8px;
}
</style>
