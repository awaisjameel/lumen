<script setup lang="ts">
// Organise files on disk: move photos into folders by a scheme the user
// writes, after a dry-run preview, with live progress and a full undo. The
// engine does all the moving (internal/fileops); this page only asks.
import { useVirtualizer } from '@tanstack/vue-virtual'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UBadge from '@nuxt/ui/components/Badge.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import USelect from '@nuxt/ui/components/Select.vue'
import UFormField from '@nuxt/ui/components/FormField.vue'
import { storeToRefs } from 'pinia'
import { computed, nextTick, onMounted, onScopeDispose, ref, watch } from 'vue'
import { ORGANISE_TOKENS, OrganiseMoveState, OrganiseReason, OrganiseRunState } from '../bridge'
import type { OrganiseMove, OrganiseRun } from '../bridge'
import { confirm } from '../shared/confirm'
import { icons } from '../design/icons'
import { registerCommands, runCommand, useCommands } from '../shared/commands'
import type { Command } from '../shared/commands'
import FeatureState from '../shared/FeatureState.vue'
import Page from '../shared/Page.vue'
import PageHeader from '../shared/PageHeader.vue'
import PrefRow from '../shared/PrefRow.vue'
import WorkProgress from '../shared/WorkProgress.vue'
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

const schemeInput = ref<{ inputRef: HTMLInputElement | null } | null>(null)
const loadingRuns = ref(true)

onMounted(() => {
  void organise.loadRuns().finally(() => {
    loadingRuns.value = false
  })
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
  const input = schemeInput.value?.inputRef
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
    case OrganiseReason.NotAFolder:
      return t('organise.reason.not_a_folder')
    case OrganiseReason.SidecarTaken:
      return t('organise.reason.sidecar_taken')
    default:
      return error
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

const sourceChoices = computed(() => [
  { value: 0, label: t('organise.allSources') },
  ...sources.value.map((source) => ({ value: source.id, label: source.path })),
])
useCommands([
  {
    id: 'organise.preview',
    group: 'library',
    title: () => t('organise.preview'),
    icon: 'eye',
    enabled: () => !planning.value && !busy.value && !!template.value.trim(),
    run: () => organise.preview(),
  },
  {
    id: 'organise.move',
    group: 'library',
    title: () => t('organise.move', { n: toMove.value }),
    icon: 'organise',
    available: () => plan.value !== null,
    enabled: () => toMove.value > 0 && !busy.value,
    run: startRun,
  },
])

// Run actions follow the live history, so the palette and its buttons use
// the same run and availability checks even after a progress event.
let removeRunCommands: () => void = () => undefined
watch(
  runs,
  (history) => {
    removeRunCommands()
    const commands: Command[] = history.flatMap((run) => [
      {
        id: `organise.resume.${run.id}`,
        group: 'library',
        title: () => `${t('organise.resume')} · ${when(run.createdAt)}`,
        icon: 'play',
        available: () => run.state === OrganiseRunState.OrganiseFailed,
        enabled: () => !busy.value,
        run: () => organise.resume(run),
      },
      {
        id: `organise.undo.${run.id}`,
        group: 'library',
        title: () => `${t('organise.undo')} · ${when(run.createdAt)}`,
        icon: 'undo',
        available: () => canUndo(run),
        enabled: () => !busy.value,
        run: () => undoRun(run),
      },
    ])
    removeRunCommands = registerCommands(commands)
  },
  { immediate: true },
)
onScopeDispose(() => removeRunCommands())
</script>

<template>
  <Page>
    <template #header
      ><PageHeader :title="t('organise.title')" :back="{ to: '/settings', label: t('organise.back') }"
    /></template>
    <p class="text-muted mb-4">{{ t('organise.intro') }}</p>
    <UAlert v-if="error" data-organise-error color="error" variant="subtle" :title="error" role="alert" class="mb-4" />
    <section class="scheme grid gap-4">
      <UFormField :label="t('organise.scheme')" :help="t('organise.schemeHint')" class="w-full max-w-xl">
        <UInput
          id="organise-scheme"
          ref="schemeInput"
          v-model="template"
          type="text"
          spellcheck="false"
          class="w-full"
          :ui="{ base: 'font-mono' }"
        />
      </UFormField>
      <div class="flex flex-wrap gap-1.5" role="group" :aria-label="t('organise.tokens')">
        <UButton
          v-for="token in ORGANISE_TOKENS"
          :key="token"
          type="button"
          class="chip font-mono"
          size="sm"
          variant="outline"
          :title="t(`organise.token.${token}`)"
          @click="insertToken(token)"
          >{{ braced(token) }}</UButton
        >
      </div>
      <UAlert
        v-for="token in unknown"
        :key="token"
        color="error"
        variant="subtle"
        :title="t('organise.unknownToken', { token: braced(token) })"
        role="alert"
      />
      <p class="example text-muted font-mono break-words" aria-live="polite">
        <template v-if="exampleText">{{ t('organise.example', { path: exampleText }) }}</template>
        <template v-else-if="!example">{{ t('organise.exampleNone') }}</template>
      </p>
      <PrefRow :label="t('organise.source')">
        <template #default="{ labelledBy }"
          ><USelect
            id="organise-source"
            v-model="sourceId"
            :items="sourceChoices"
            :aria-labelledby="labelledBy"
            class="source-select"
        /></template>
      </PrefRow>
      <div>
        <UButton
          color="primary"
          variant="solid"
          :icon="icons.eye"
          :disabled="planning || busy || !template.trim()"
          :loading="planning"
          @click="runCommand('organise.preview')"
          >{{ t('organise.preview') }}</UButton
        >
      </div>
    </section>
    <FeatureState v-if="planning" kind="loading" :title="t('organise.previewing')" />
    <section v-if="plan" class="preview mt-4">
      <ul class="summary text-muted mb-3 pl-5 list-disc">
        <li v-if="toMove">{{ t('organise.willMove', { n: toMove }) }}</li>
        <li v-if="plan.inPlace">{{ t('organise.inPlace', { n: plan.inPlace }) }}</li>
        <li v-if="plan.renamed">{{ t('organise.renamed', { n: plan.renamed }) }}</li>
        <li v-if="plan.sidecars">{{ t('organise.sidecars', { n: plan.sidecars }) }}</li>
        <li v-if="skipped">{{ t('organise.skipped', { n: skipped }) }}</li>
      </ul>
      <FeatureState v-if="!plan.moves.length" kind="empty" icon="organise" :title="t('organise.nothing')" />
      <div v-else ref="scroller" class="moves" role="list" tabindex="0" :aria-label="t('organise.moves')">
        <div class="moves-inner" :style="{ height: `${virtualizer.getTotalSize()}px` }">
          <div
            v-for="{ item, move } in visibleMoves"
            :key="move.seq"
            class="move"
            :class="{ left: move.state === OrganiseMoveState.MoveSkipped }"
            role="listitem"
            :aria-posinset="item.index + 1"
            :aria-setsize="plan.moves.length"
            :style="{ height: `${rowHeight}px`, transform: `translateY(${item.start}px)` }"
          >
            <span class="path" :title="move.from">{{ move.from }}</span>
            <span v-if="move.state === OrganiseMoveState.MoveSkipped" class="path reason" :title="reasonText(move)">{{
              reasonText(move)
            }}</span>
            <span v-else class="path" :title="move.to"
              ><span class="sr-only">{{ t('organise.movesTo') }}</span
              ><span aria-hidden="true">→</span> {{ move.to }}
              <UBadge v-if="move.renamed" color="neutral" variant="outline" size="sm">{{
                t('organise.renamedTag')
              }}</UBadge></span
            >
            <span
              v-if="move.state !== OrganiseMoveState.MoveSkipped && move.error"
              class="path note"
              :title="reasonText(move)"
              >{{ reasonText(move) }}</span
            >
          </div>
        </div>
      </div>
      <UButton
        class="mt-3"
        color="primary"
        variant="solid"
        :icon="icons.organise"
        :disabled="!toMove || busy"
        @click="runCommand('organise.move')"
        >{{ t('organise.move', { n: toMove }) }}</UButton
      >
    </section>
    <WorkProgress
      v-if="moving && progress"
      class="progress my-4"
      :label="progressText"
      :done="progress.done"
      :total="progress.total"
    />
    <UAlert
      v-else-if="finished"
      class="finished my-4"
      :color="progress?.state === OrganiseRunState.OrganiseFailed ? 'error' : 'neutral'"
      variant="subtle"
      :title="finished"
      role="status"
    />
    <section class="runs mt-6">
      <h2 class="text-highlighted font-semibold text-lg mb-3">{{ t('organise.runs') }}</h2>
      <FeatureState v-if="loadingRuns" kind="loading" :title="t('organise.loadingRuns')" />
      <FeatureState v-else-if="!runs.length" kind="empty" icon="history" :title="t('organise.noRuns')" />
      <ul v-else class="run-list">
        <li v-for="run in runs" :key="run.id" class="run-entry">
          <WorkProgress
            class="min-w-0 flex-1"
            :label="when(run.createdAt)"
            :detail="`${t(stateText(run.state))} · ${t('organise.runMoved', { moved: run.moved, total: run.total })}`"
            :done="run.moved"
            :total="run.total"
            :failed="run.state === OrganiseRunState.OrganiseFailed"
          >
            <UButton
              v-if="run.state === OrganiseRunState.OrganiseFailed"
              :icon="icons.play"
              :disabled="busy"
              @click="runCommand(`organise.resume.${run.id}`)"
              >{{ t('organise.resume') }}</UButton
            >
            <UButton
              v-if="canUndo(run)"
              :icon="icons.undo"
              :disabled="busy"
              @click="runCommand(`organise.undo.${run.id}`)"
              >{{ t('organise.undo') }}</UButton
            >
          </WorkProgress>
          <p v-if="run.sidecarsKept" class="text-muted text-xs">{{ t('organise.runKept', { n: run.sidecarsKept }) }}</p>
          <p class="text-muted text-xs font-mono break-words">{{ run.template }}</p>
        </li>
      </ul>
    </section>
  </Page>
</template>

<style scoped>
.source-select {
  width: min(28rem, 100%);
}
.example {
  min-height: 1.4em;
  margin: 0;
  overflow-wrap: anywhere;
}
.moves {
  height: 320px;
  overflow-y: auto;
  border: 1px solid var(--ui-border);
  border-radius: calc(var(--ui-radius) * 2);
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
  padding: 0 0.75rem;
  border-bottom: 1px solid var(--ui-border);
  font-size: 0.8125rem;
}
.path {
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}
.move .path:first-child,
.note {
  color: var(--ui-text-muted);
}
.move.left .reason {
  color: var(--ui-error);
}
.note {
  font-size: 0.75rem;
}
.run-list {
  list-style: none;
  margin: 0;
  padding: 0;
}
.run-entry {
  padding: 1rem 0;
  border-bottom: 1px solid var(--ui-border);
}
@container (max-width: 40rem) {
  .source-select {
    width: 100%;
  }
}
</style>
