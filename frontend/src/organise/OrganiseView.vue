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
import { storeToRefs } from 'pinia'
import { computed, nextTick, onMounted, onScopeDispose, ref, watch } from 'vue'
import {
  ORGANISE_DEFAULT_TEMPLATE,
  ORGANISE_TOKENS,
  OrganiseMoveState,
  OrganiseRunState,
  SidecarState,
  thumbUrl,
} from '../bridge'
import type { OrganiseMove, OrganiseRun } from '../bridge'
import { icons } from '../design/icons'
import { useLibrary } from '../library/store'
import { FileTools } from '../media/lazy'
import Chip from '../shared/Chip.vue'
import ViewBar from '../shared/ViewBar.vue'
import Segmented from '../shared/Segmented.vue'
import { registerCommands, runCommand, useCommands } from '../shared/commands'
import type { Command } from '../shared/commands'
import { confirm } from '../shared/confirm'
import FeatureState from '../shared/FeatureState.vue'
import { reasonText } from '../shared/fileReasons'
import { fileName } from '../shared/format'
import { formatNumber, LOCALE, t } from '../shared/i18n'
import type { MessageKey } from '../shared/i18n'
import Icon from '../shared/Icon.vue'
import { notify } from '../shared/notify'
import Page from '../shared/Page.vue'
import PageHeader from '../shared/PageHeader.vue'
import WorkProgress from '../shared/WorkProgress.vue'
import FolderTree from './FolderTree.vue'
import { useOrganise } from './store'
import { renderExample, unknownTokens, validTemplate } from './template'

const organise = useOrganise()
const library = useLibrary()
const tool = ref<'scheme' | 'files'>('scheme')
const filesOpen = ref(false)
const fileIDs = computed(() =>
  library.inTrash
    ? []
    : library.selectedIDs.size
      ? [...library.selectedIDs]
      : library.selected
        ? [library.selected.id]
        : [],
)
async function filesChanged() {
  await Promise.all([library.refresh(), organise.loadRuns()])
}
const {
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
} = storeToRefs(organise)

const schemeInput = ref<{ inputRef: HTMLInputElement | null } | null>(null)
const workspace = ref<HTMLElement | null>(null)
const loadingRuns = ref(true)
const loadingSources = ref(true)

onMounted(() => {
  void organise.loadRuns().finally(() => {
    loadingRuns.value = false
  })
  void organise.loadSources().finally(() => {
    loadingSources.value = false
  })
  void organise.loadExample()
})

// --- The scheme ---------------------------------------------------------

const valid = computed(() => validTemplate(template.value))
const unknown = computed(() => unknownTokens(template.value))
/** A token as it is written in a scheme. */
const braced = (token: string) => `{${token}}`
const schemeError = computed(() =>
  valid.value
    ? ''
    : unknown.value[0] !== undefined
      ? t('organise.unknownToken', { token: braced(unknown.value[0]) })
      : t('organise.invalid'),
)

// A preview is of one scheme and one folder choice: editing either ends it,
// and the page says the exact paths need a fresh preview.
const stale = ref(false)
watch([template, sourceId], () => {
  if (plan.value || planning.value) stale.value = true
  organise.discardPlan()
})

/** Puts a token into the scheme where the cursor is, replacing any selected text. */
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

function resetScheme() {
  template.value = ORGANISE_DEFAULT_TEMPLATE
  notify({ id: 'organise.reset', kind: 'info', title: t('organise.resetDone') })
}

const sourceChoices = computed(() => [
  { value: 0, label: t('organise.allSources') },
  ...sources.value.map((source) => ({ value: source.id, label: source.path })),
])

// Paths are shown from the library folder they are in, as a user knows them
// ("Photos › 2026 › …"); the engine keeps every file inside its own folder.
const slashed = (path: string) => path.replaceAll('\\', '/')
function sourceOf(path: string) {
  const file = slashed(path)
  let best: { path: string; name: string } | undefined
  for (const source of sources.value) {
    const root = slashed(source.path).replace(/\/+$/, '')
    if (file.startsWith(`${root}/`) && (!best || root.length > best.path.length)) {
      best = { path: root, name: fileName(source.path) }
    }
  }
  return best
}
function inSource(path: string): string {
  const source = sourceOf(path)
  return source ? `${source.name}/${slashed(path).slice(source.path.length + 1)}` : path
}

const exampleText = computed(() => (example.value ? renderExample(template.value, example.value) : ''))
const exampleRoot = computed(() => {
  const chosen = sources.value.find((source) => source.id === sourceId.value)
  if (chosen) return fileName(chosen.path)
  return example.value ? (sourceOf(example.value.path)?.name ?? '') : ''
})
const crumbs = computed(() =>
  exampleText.value ? [exampleRoot.value, ...exampleText.value.split('\\')].filter(Boolean) : [],
)
const exampleThumb = computed(() =>
  exampleCover.value ? thumbUrl(exampleCover.value.id, exampleCover.value.thumb) : undefined,
)

// --- The preview --------------------------------------------------------

type Filter = 'move' | 'inPlace' | 'renamed' | 'skipped'
const filter = ref<Filter>('move')
const page = ref(0)
const pageSize = 100
const filteredMoves = computed<OrganiseMove[]>(() => {
  const moves = plan.value?.moves ?? []
  switch (filter.value) {
    case 'move':
      return moves.filter((move) => move.state === OrganiseMoveState.MovePlanned)
    case 'renamed':
      return moves.filter((move) => move.state === OrganiseMoveState.MovePlanned && move.renamed)
    case 'skipped':
      return moves.filter((move) => move.state === OrganiseMoveState.MoveSkipped)
    default:
      return []
  }
})
const pages = computed(() => Math.max(1, Math.ceil(filteredMoves.value.length / pageSize)))
const pagedMoves = computed(() => filteredMoves.value.slice(page.value * pageSize, (page.value + 1) * pageSize))
watch(plan, () => {
  filter.value = 'move'
  if (plan.value) stale.value = false
})
watch([plan, filter], () => {
  page.value = 0
})
watch(page, () => virtualizer.value.scrollToOffset(0))

const kept = computed(
  () =>
    plan.value?.moves.filter((move) => move.error === 'sidecar_taken' || move.sidecar === SidecarState.SidecarKept)
      .length ?? 0,
)
const stats = computed(() => {
  const counts: Record<Filter, number> = {
    move: toMove.value,
    inPlace: plan.value?.inPlace ?? 0,
    renamed: plan.value?.renamed ?? 0,
    skipped: skipped.value,
  }
  return (['move', 'inPlace', 'renamed', 'skipped'] as const).map((value) => ({
    value,
    n: counts[value],
    label: t(`organise.stat.${value}`),
    title: counts[value] ? t(`organise.statHint.${value}`) : t('organise.statEmpty'),
  }))
})
const treePaths = computed(() =>
  plan.value
    ? plan.value.moves.filter((move) => move.state === OrganiseMoveState.MovePlanned).map((move) => inSource(move.to))
    : crumbs.value.length
      ? [crumbs.value.join('/')]
      : [],
)
const chosenFolder = computed(() => sources.value.find((source) => source.id === sourceId.value)?.path)

function status(move: OrganiseMove): { label: string; color: 'neutral' | 'warning' } {
  if (move.state === OrganiseMoveState.MoveSkipped) return { label: t('organise.status.skipped'), color: 'warning' }
  return { label: t(move.renamed ? 'organise.status.renamed' : 'organise.status.move'), color: 'neutral' }
}
function note(move: OrganiseMove): string {
  if (move.state === OrganiseMoveState.MoveSkipped) return ''
  if (move.error) return reasonText(move.error)
  if (move.renamed) return t('organise.note.renamed')
  return move.sidecar === SidecarState.SidecarPlanned ? t('organise.note.sidecar') : ''
}

// The preview can hold every file of a library, so only the rows in view exist.
const rowHeight = 76
const scroller = ref<HTMLElement | null>(null)
const virtualizer = useVirtualizer(
  computed(() => ({
    count: pagedMoves.value.length,
    getScrollElement: () => scroller.value,
    getItemKey: (index: number) => pagedMoves.value[index]?.seq ?? index,
    estimateSize: () => rowHeight,
    overscan: 6,
  })),
)
const visibleMoves = computed(() =>
  virtualizer.value.getVirtualItems().flatMap((item) => {
    const move = pagedMoves.value[item.index]
    return move ? [{ item, move }] : []
  }),
)

// --- Running, and how it ended -----------------------------------------

const moving = computed(() => progress.value !== null && busy.value)
watch(moving, async () => {
  await nextTick()
  const scroller = workspace.value?.parentElement
  if (scroller) scroller.scrollTop = 0
})
const progressText = computed(() => {
  const p = progress.value
  if (!p) return ''
  const counts = { done: p.done, total: p.total }
  return t(p.state === OrganiseRunState.OrganiseUndoing ? 'organise.puttingBack' : 'organise.moving', counts)
})
const percent = computed(() => {
  const p = progress.value
  return p?.total ? Math.min(100, Math.round((p.done / p.total) * 100)) : 0
})
const percentText = computed(() => new Intl.NumberFormat(LOCALE, { style: 'percent' }).format(percent.value / 100))

type Ending = 'done' | 'undone' | 'partial' | 'stopped'
const ending = computed<Ending | null>(() => {
  const p = progress.value
  if (!p || moving.value) return null
  switch (p.state) {
    case OrganiseRunState.OrganiseDone:
      return lastAction.value === 'undo' ? 'partial' : 'done'
    case OrganiseRunState.OrganiseUndone:
      return 'undone'
    case OrganiseRunState.OrganiseFailed:
      return 'stopped'
    default:
      return null
  }
})
/** The run the last progress was about, as the list of runs now has it. */
const endedRun = computed(() => runs.value.find((run) => run.id === progress.value?.runId))
const doneText = computed(() => {
  const run = endedRun.value
  if (!run) return ''
  const plan = started.value?.runId === run.id ? started.value : null
  const parts = [t('organise.done.moved', { n: run.moved })]
  // History counts all retained companions, including collisions already known in the preview.
  // Exact moved-sidecar totals can be inferred only when every planned photo moved.
  if (run.moved === plan?.moves.filter((move) => move.state === OrganiseMoveState.MovePlanned).length) {
    const alreadyKept = plan.moves.filter((move) => move.sidecar === SidecarState.SidecarKept).length
    const movedSidecars = plan.sidecars - Math.max(0, run.sidecarsKept - alreadyKept)
    if (movedSidecars > 0) parts.push(t('organise.done.sidecars', { n: movedSidecars }))
  }
  if (run.sidecarsKept) parts.push(t('organise.done.kept', { n: run.sidecarsKept }))
  if (run.total > run.moved) parts.push(t('organise.done.left', { n: run.total - run.moved }))
  return t('organise.done.end', { summary: parts.join(' · ') })
})

const step = computed(() => (progress.value ? 2 : plan.value || planning.value ? 1 : 0))
const steps = ['organise.step.build', 'organise.step.preview', 'organise.step.finish'] as const
const caption = computed(() =>
  moving.value ? t('organise.captionWorking') : ending.value ? t('organise.captionFinished') : t('organise.caption'),
)
const editing = computed(() => !moving.value && !ending.value && !loadingSources.value && sources.value.length > 0)
const noSources = computed(() => !loadingSources.value && !sourceError.value && !sources.value.length)
const loadError = computed(() => previewError.value || sourceError.value || exampleError.value || runsError.value)

async function retryLoads() {
  const again: Promise<void>[] = []
  if (sourceError.value) again.push(organise.loadSources())
  if (exampleError.value) again.push(organise.loadExample())
  if (runsError.value) again.push(organise.loadRuns())
  if (previewError.value) again.push(organise.preview())
  await Promise.all(again)
}

// A run's time is a real moment (unlike a photo's date), shown in this PC's zone.
const whenFmt = new Intl.DateTimeFormat(LOCALE, { dateStyle: 'medium', timeStyle: 'short' })
const when = (seconds: number) => whenFmt.format(new Date(seconds * 1000))

const canUndo = (run: OrganiseRun) =>
  run.moved > 0 &&
  (run.state === OrganiseRunState.OrganiseDone ||
    run.state === OrganiseRunState.OrganiseFailed ||
    run.state === OrganiseRunState.OrganiseUndoing)

async function startRun() {
  const reviewed = plan.value
  if (!reviewed || busy.value) return
  const n = toMove.value
  const lines = [
    [
      t('organise.confirmCount', { n }),
      ...(reviewed.sidecars ? [t('organise.confirmSidecars', { n: reviewed.sidecars })] : []),
    ].join(' · '),
    [
      ...(reviewed.renamed ? [t('organise.confirmRenamed', { n: reviewed.renamed })] : []),
      ...(skipped.value ? [t('organise.confirmLeft', { n: skipped.value })] : []),
    ].join(' '),
  ].filter(Boolean)
  const text = [t('organise.confirmText'), lines.join('\n')].join('\n\n')
  if (!(await confirm(t('organise.confirmTitle', { n }), text, t('organise.confirmOk', { n })))) return
  // The preview the user agreed to is the one that moves, or nothing does.
  if (plan.value !== reviewed) {
    notify({ id: 'organise.previewChanged', kind: 'info', title: t('organise.previewChanged') })
    return
  }
  await organise.start()
}

async function undoRun(run: OrganiseRun) {
  const text = [
    t('organise.undoText'),
    `${t('organise.undoDetail', { n: run.moved, when: when(run.createdAt) })}\n${t('organise.undoOnly')}`,
  ].join('\n\n')
  if (!(await confirm(t('organise.undoTitle'), text, t('organise.undoOk')))) return
  // The run may have changed while the question was open (another window, a
  // run that ended): undo only what is still there to undo.
  const current = runs.value.find((item) => item.id === run.id)
  if (!current || !canUndo(current) || busy.value) return
  if (current.moved !== run.moved || current.state !== run.state) {
    notify({ id: 'organise.runChanged', kind: 'info', title: t('organise.runChanged') })
    return
  }
  await organise.undo(current)
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
function stateColor(state: OrganiseRunState): 'success' | 'warning' | 'neutral' {
  if (state === OrganiseRunState.OrganiseDone) return 'success'
  return state === OrganiseRunState.OrganiseFailed ? 'warning' : 'neutral'
}

function organiseMore() {
  organise.dismissRun()
  filter.value = 'move'
}

useCommands([
  {
    id: 'organise.preview',
    group: 'library',
    title: () => t('organise.preview'),
    icon: 'eye',
    enabled: () => !planning.value && !busy.value && valid.value,
    reason: () => (!valid.value ? t('organise.fixFirst') : busy.value ? t('organise.busyReason') : undefined),
    run: () => organise.preview(),
  },
  {
    id: 'organise.move',
    group: 'library',
    title: () => t('organise.move', { n: toMove.value }),
    icon: 'organise',
    available: () => plan.value !== null,
    enabled: () => toMove.value > 0 && !busy.value,
    reason: () => (busy.value ? t('organise.busyReason') : !toMove.value ? t('organise.noneToMove') : undefined),
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
        reason: () => (busy.value ? t('organise.busyReason') : undefined),
        run: () => organise.resume(run),
      },
      {
        id: `organise.undo.${run.id}`,
        group: 'library',
        title: () => `${t('organise.undo')} · ${when(run.createdAt)}`,
        icon: 'undo',
        available: () => canUndo(run),
        enabled: () => !busy.value,
        reason: () => (busy.value ? t('organise.busyReason') : undefined),
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
      ><PageHeader
        :title="t(tool === 'files' ? 'organise.filesTitle' : 'organise.title')"
        :subtitle="caption"
        :back="{ to: '/settings', label: t('organise.back') }"
    /></template>
    <template #toolbar
      ><ViewBar :label="t('organise.tools')"
        ><Segmented
          v-model="tool"
          :label="t('organise.tools')"
          :items="[
            {
              value: 'scheme',
              label: t('organise.folderScheme'),
              disabled: busy,
              reason: busy ? t('organise.busyReason') : undefined,
            },
            {
              value: 'files',
              label: t('organise.selectedFiles'),
              disabled: busy,
              reason: busy ? t('organise.busyReason') : undefined,
            },
          ]" /></ViewBar
    ></template>
    <section v-if="tool === 'files'" data-organise-files class="organise-workspace">
      <FeatureState
        v-if="!fileIDs.length"
        kind="empty"
        icon="files"
        :title="t('organise.filesEmptyTitle')"
        :text="t('organise.filesEmptyText')"
        ><UButton to="/" color="primary" variant="solid">{{ t('organise.selectFiles') }}</UButton></FeatureState
      >
      <section v-else class="card">
        <h2>{{ t('organise.filesHeading') }}</h2>
        <p>{{ t('organise.filesScope', { n: fileIDs.length }) }}</p>
        <p>{{ t('organise.filesHint') }}</p>
        <div class="actions">
          <UButton color="primary" variant="solid" :disabled="busy" @click="filesOpen = true">{{
            t('organise.openFiles')
          }}</UButton
          ><UButton to="/" variant="ghost">{{ t('organise.changeSelection') }}</UButton>
        </div>
      </section>
    </section>
    <div v-show="tool === 'scheme'" ref="workspace" data-organise-workspace class="organise-workspace">
      <nav class="steps" :aria-label="t('organise.steps')">
        <template v-for="(label, index) in steps" :key="label"
          ><span
            class="step"
            :class="{ current: step === index, complete: step > index }"
            :aria-current="step === index ? 'step' : undefined"
            ><span class="step-number">{{ index + 1 }}</span
            >{{ t(label) }}</span
          ><span v-if="index < steps.length - 1" class="step-line" :class="{ complete: step > index }"
        /></template>
      </nav>
      <p class="safety"><Icon name="blocked" :size="16" />{{ t('organise.intro') }}</p>

      <UAlert
        v-if="error"
        data-organise-error
        color="error"
        variant="subtle"
        :icon="icons.error"
        :title="error"
        role="alert"
      />
      <UAlert
        v-if="loadError && !moving"
        data-organise-load-error
        color="warning"
        variant="subtle"
        :icon="icons.warning"
        :title="t('organise.loadErrorTitle')"
        :description="t('organise.loadErrorKept', { reason: loadError })"
        role="alert"
        :actions="[{ label: t('organise.retry'), color: 'primary', variant: 'solid', onClick: retryLoads }]"
      />
      <UAlert
        v-else-if="busy && !moving"
        data-organise-busy
        color="neutral"
        variant="subtle"
        :icon="icons.info"
        :title="t('organise.busyTitle')"
        :description="t('organise.busy')"
        role="status"
      />

      <FeatureState
        v-if="noSources && !moving"
        kind="empty"
        icon="organise"
        :title="t('organise.emptyTitle')"
        :text="t('organise.emptyText')"
        ><UButton to="/settings" color="primary" variant="solid">{{ t('organise.addFolder') }}</UButton></FeatureState
      >

      <section v-if="editing" data-organise-editor class="scheme">
        <div class="section-head">
          <h2>{{ t('organise.heading') }}</h2>
          <UButton variant="ghost" size="sm" :icon="icons.reset" :disabled="busy" @click="resetScheme">{{
            t('organise.reset')
          }}</UButton>
        </div>
        <div class="scheme-columns">
          <div class="scheme-editor">
            <div class="field">
              <label for="organise-scheme">{{ t('organise.scheme') }}</label>
              <UInput
                id="organise-scheme"
                ref="schemeInput"
                v-model="template"
                :disabled="busy"
                :color="valid ? 'primary' : 'error'"
                :highlight="!valid"
                :aria-invalid="!valid"
                aria-describedby="organise-scheme-help organise-scheme-check"
                type="text"
                spellcheck="false"
                class="w-full"
              />
              <p id="organise-scheme-help" class="hint">{{ t('organise.schemeHint') }}</p>
            </div>
            <div class="chips" role="group" :aria-label="t('organise.tokens')">
              <Chip
                v-for="token in ORGANISE_TOKENS"
                :key="token"
                :disabled="busy"
                data-organise-token
                :title="t(`organise.token.${token}`)"
                @click="insertToken(token)"
                >{{ braced(token) }}</Chip
              >
            </div>
            <p v-if="schemeError" id="organise-scheme-check" class="scheme-error" aria-live="polite">
              <Icon name="error" :size="16" />{{ schemeError }}
            </p>
            <p v-else id="organise-scheme-check" class="hint">{{ t('organise.tokenHint') }}</p>
          </div>
          <div class="field scheme-source">
            <label for="organise-source">{{ t('organise.source') }}</label
            ><USelect
              id="organise-source"
              v-model="sourceId"
              :items="sourceChoices"
              :disabled="busy"
              :aria-label="t('organise.source')"
              class="w-full"
            />
            <p class="hint">{{ t('organise.sourceHint') }}</p>
          </div>
        </div>
        <div data-organise-example class="example">
          <img v-if="exampleThumb" :src="exampleThumb" alt="" class="example-thumb" />
          <div class="example-text">
            <template v-if="example && crumbs.length">
              <span class="eyebrow">{{ t('organise.example', { name: fileName(example.path) }) }}</span>
              <ol class="crumbs" aria-live="polite">
                <li v-for="(crumb, index) in crumbs" :key="index">
                  {{ crumb
                  }}<Icon v-if="index < crumbs.length - 1" name="chevron-right" :size="16" class="crumb-mark" />
                </li>
              </ol>
            </template>
            <span v-else class="hint">{{ t('organise.exampleNone') }}</span>
          </div>
        </div>
        <FolderTree
          v-if="!plan && !planning && valid && treePaths.length"
          :paths="treePaths"
          :label="t('organise.exampleTree')"
          >{{ t('organise.exampleTreeHint') }}</FolderTree
        >
        <div v-if="!plan && !planning" class="draft">
          <span class="hint">{{
            !valid ? t('organise.fixFirst') : stale ? t('organise.stale') : t('organise.previewHint')
          }}</span>
          <UButton
            color="primary"
            variant="solid"
            :icon="icons.eye"
            :disabled="busy || !valid"
            :title="!valid ? t('organise.fixFirst') : busy ? t('organise.busyReason') : t('organise.previewHint')"
            @click="runCommand('organise.preview')"
            >{{ t('organise.preview') }}</UButton
          >
        </div>
      </section>

      <section v-if="planning && editing" class="preview-loading" aria-busy="true" role="status">
        <h2>{{ t('organise.previewing') }}</h2>
        <div class="stats"><span v-for="n in 4" :key="n" class="lumen-skeleton stat-skeleton" /></div>
        <span class="lumen-skeleton rows-skeleton" />
      </section>

      <FeatureState
        v-if="plan && editing && !plan.moves.length"
        data-organise-nothing
        kind="empty"
        icon="success"
        :title="t('organise.nothingTitle')"
        :text="t('organise.nothing')"
        ><UButton color="primary" variant="solid" @click="organise.discardPlan">{{
          t('organise.edit')
        }}</UButton></FeatureState
      >
      <section v-else-if="plan && editing" data-organise-preview class="preview">
        <div class="section-head">
          <h2>{{ t('organise.previewHeading') }}</h2>
          <UBadge color="neutral" variant="subtle">{{ t('organise.previewed') }}</UBadge>
        </div>
        <div class="stats" role="group" :aria-label="t('organise.previewHeading')">
          <button
            v-for="stat in stats"
            :key="stat.value"
            type="button"
            :data-organise-stat="stat.value"
            :aria-pressed="filter === stat.value"
            :disabled="!stat.n"
            :title="stat.title"
            @click="filter = stat.value"
          >
            <strong>{{ formatNumber(stat.n) }}</strong
            ><span>{{ stat.label }}</span>
          </button>
        </div>
        <div class="preview-columns">
          <FolderTree v-if="treePaths.length" :paths="treePaths" :label="t('organise.tree')">{{
            chosenFolder ? t('organise.treeOne', { folder: chosenFolder }) : t('organise.treeAll')
          }}</FolderTree>
          <div class="moves-card">
            <div class="moves-head">
              <h3>{{ t(`organise.list.${filter}`) }}</h3>
              <span v-if="filteredMoves.length" class="range">{{
                t('organise.range', {
                  first: page * pageSize + 1,
                  last: Math.min((page + 1) * pageSize, filteredMoves.length),
                  total: filteredMoves.length,
                })
              }}</span>
            </div>
            <p v-if="filter === 'inPlace'" class="moves-note">{{ t('organise.inPlaceHint') }}</p>
            <div
              v-else
              ref="scroller"
              data-organise-moves
              class="moves"
              role="list"
              tabindex="0"
              :aria-label="t(`organise.list.${filter}`)"
            >
              <div class="moves-inner" :style="{ height: `${virtualizer.getTotalSize()}px` }">
                <div
                  v-for="{ item, move } in visibleMoves"
                  :key="move.seq"
                  data-organise-move
                  class="move"
                  :class="{ left: move.state === OrganiseMoveState.MoveSkipped }"
                  role="listitem"
                  :aria-posinset="page * pageSize + item.index + 1"
                  :aria-setsize="filteredMoves.length"
                  :style="{ height: `${rowHeight}px`, transform: `translateY(${item.start}px)` }"
                >
                  <div class="move-text">
                    <span class="path from" :title="move.from">{{ move.from }}</span>
                    <span
                      v-if="move.state === OrganiseMoveState.MoveSkipped"
                      class="path reason"
                      :title="reasonText(move.error)"
                      >{{ reasonText(move.error) }}</span
                    >
                    <span v-else class="path to" :title="move.to"
                      ><span class="sr-only">{{ t('organise.movesTo') }}</span
                      ><span aria-hidden="true">→ </span>{{ move.to }}</span
                    >
                    <span v-if="note(move)" data-organise-note class="note" :title="note(move)">{{ note(move) }}</span>
                  </div>
                  <UBadge :color="status(move).color" variant="subtle" size="sm" class="move-status">{{
                    status(move).label
                  }}</UBadge>
                </div>
              </div>
            </div>
            <div v-if="filter !== 'inPlace'" class="pagination">
              <UButton
                data-organise-previous
                variant="ghost"
                size="sm"
                :disabled="page === 0"
                :title="page === 0 ? t('organise.firstPage') : undefined"
                @click="page--"
                >{{ t('organise.previousFiles') }}</UButton
              ><span>{{ t('organise.pageOf', { page: page + 1, pages }) }}</span
              ><UButton
                data-organise-next
                variant="ghost"
                size="sm"
                :disabled="page + 1 >= pages"
                :title="page + 1 >= pages ? t('organise.lastPage') : undefined"
                @click="page++"
                >{{ t('organise.nextFiles') }}</UButton
              >
            </div>
          </div>
        </div>
        <div class="preview-foot">
          <div data-organise-summary class="summary">
            <p>
              <Icon name="files" :size="16" />{{
                plan.sidecars ? t('organise.sidecars', { n: plan.sidecars }) : t('organise.noSidecars')
              }}
              <template v-if="kept">{{ t('organise.sidecarsKept', { n: kept }) }}</template>
            </p>
            <p class="hint">
              {{ plan.renamed ? t('organise.renameSummary', { n: toMove }) : t('organise.noRenames') }}
            </p>
          </div>
          <UButton
            color="primary"
            variant="solid"
            size="lg"
            :trailing-icon="icons['arrow-right']"
            :disabled="!toMove || busy"
            :title="busy ? t('organise.busyReason') : !toMove ? t('organise.noneToMove') : undefined"
            @click="runCommand('organise.move')"
            >{{ t('organise.move', { n: toMove }) }}</UButton
          >
        </div>
      </section>

      <section v-if="moving && progress" data-organise-progress class="card working">
        <WorkProgress
          :label="progressText"
          :done="progress.done"
          :total="progress.total"
          :detail="t('organise.scanning')"
          activity
          ><span class="lumen-light-dot" aria-hidden="true" /><span class="percent">{{
            percentText
          }}</span></WorkProgress
        >
        <div class="actions">
          <UButton to="/" color="neutral" variant="outline">{{ t('organise.backToLibrary') }}</UButton>
        </div>
      </section>

      <section
        v-if="ending === 'done' || ending === 'undone'"
        data-organise-finished
        class="card finished"
        role="status"
      >
        <Icon name="success" :size="32" class="finished-mark" />
        <div>
          <p class="eyebrow">{{ t('organise.finishedStep') }}</p>
          <h2>{{ t(ending === 'undone' ? 'organise.undoneTitle' : 'organise.doneTitle') }}</h2>
          <p>{{ ending === 'undone' ? t('organise.undone', { n: progress?.done ?? 0 }) : doneText }}</p>
        </div>
        <div class="actions">
          <UButton
            v-if="ending === 'done' && endedRun && canUndo(endedRun)"
            color="primary"
            variant="solid"
            :icon="icons.undo"
            :disabled="busy"
            @click="runCommand(`organise.undo.${endedRun.id}`)"
            >{{ t('organise.undoThis') }}</UButton
          >
          <UButton
            :color="ending === 'done' ? 'neutral' : 'primary'"
            :variant="ending === 'done' ? 'outline' : 'solid'"
            @click="organiseMore"
            >{{ t('organise.more') }}</UButton
          >
          <UButton to="/" variant="ghost">{{ t('organise.backToLibrary') }}</UButton>
        </div>
      </section>
      <UAlert
        v-if="ending === 'partial'"
        data-organise-partial
        color="warning"
        variant="subtle"
        :icon="icons.warning"
        role="status"
        :title="
          t('organise.partialTitle', {
            back: t('organise.partialBack', { n: progress?.done ?? 0 }),
            left: t('organise.partialLeft', { n: endedRun?.moved ?? 0 }),
          })
        "
        :description="t('organise.partialText')"
        :actions="
          endedRun && canUndo(endedRun)
            ? [
                {
                  label: t('organise.undoRemaining'),
                  color: 'primary',
                  variant: 'solid',
                  disabled: busy,
                  onClick: () => runCommand(`organise.undo.${endedRun?.id}`),
                },
                { label: t('organise.more'), color: 'neutral', variant: 'outline', onClick: organiseMore },
              ]
            : [{ label: t('organise.more'), color: 'neutral', variant: 'outline', onClick: organiseMore }]
        "
      />
      <section v-if="ending === 'stopped'" data-organise-stopped class="stopped">
        <UAlert
          color="warning"
          variant="subtle"
          :icon="icons.warning"
          role="alert"
          :title="t('organise.stoppedTitle')"
          :description="t('organise.stoppedText', { moved: endedRun?.moved ?? 0, total: endedRun?.total ?? 0 })"
        />
        <div v-if="endedRun" class="actions">
          <UButton
            color="primary"
            variant="solid"
            :icon="icons.play"
            :disabled="busy"
            @click="runCommand(`organise.resume.${endedRun.id}`)"
            >{{ t('organise.resume') }}</UButton
          >
          <UButton
            color="neutral"
            variant="outline"
            :icon="icons.undo"
            :disabled="busy || !canUndo(endedRun)"
            :title="!endedRun.moved ? t('organise.noneMoved') : undefined"
            @click="runCommand(`organise.undo.${endedRun.id}`)"
            >{{ t('organise.undoMoved') }}</UButton
          >
          <UButton variant="ghost" @click="organiseMore">{{ t('organise.more') }}</UButton>
        </div>
      </section>

      <section data-organise-runs class="runs">
        <div class="section-head">
          <h2>{{ t('organise.runs') }}</h2>
          <span class="hint">{{ t('organise.runsSaved') }}</span>
        </div>
        <FeatureState v-if="loadingRuns" kind="loading" :title="t('organise.loadingRuns')" />
        <p v-else-if="!runs.length && !runsError" class="no-runs">{{ t('organise.noRuns') }}</p>
        <ul v-else-if="runs.length" class="run-list">
          <li v-for="run in runs" :key="run.id" data-organise-run class="run-entry">
            <Icon name="history" :size="20" class="run-mark" />
            <div class="run-text">
              <strong>{{ when(run.createdAt) }}</strong>
              <span class="run-count"
                >{{
                  run.state === OrganiseRunState.OrganiseUndone
                    ? t('organise.runUndone')
                    : t('organise.runMoved', { moved: run.moved, total: run.total })
                }}<template v-if="run.sidecarsKept">
                  · {{ t('organise.runKept', { n: run.sidecarsKept }) }}</template
                ></span
              >
              <span class="run-scheme" :class="{ mono: run.template }" :title="run.template || undefined">{{
                run.template || t('organise.chosenFiles')
              }}</span>
            </div>
            <UBadge :color="stateColor(run.state)" variant="subtle">{{ t(stateText(run.state)) }}</UBadge>
            <div class="run-actions">
              <UButton
                v-if="run.state === OrganiseRunState.OrganiseFailed"
                color="neutral"
                variant="outline"
                size="sm"
                :icon="icons.play"
                :disabled="busy"
                :title="busy ? t('organise.busyReason') : undefined"
                @click="runCommand(`organise.resume.${run.id}`)"
                >{{ t('organise.resume') }}</UButton
              >
              <UButton
                v-if="canUndo(run)"
                variant="ghost"
                size="sm"
                :icon="icons.undo"
                :disabled="busy"
                :title="busy ? t('organise.busyReason') : undefined"
                @click="runCommand(`organise.undo.${run.id}`)"
                >{{ t('organise.undoRun') }}</UButton
              >
            </div>
          </li>
        </ul>
        <p class="hint">{{ t('organise.occupied') }} {{ t('organise.historyPrivacy') }}</p>
      </section>
    </div>
    <FileTools
      v-if="filesOpen"
      v-model:open="filesOpen"
      :ids="fileIDs"
      :selection="library.selectionSnapshot"
      @changed="filesChanged"
    />
  </Page>
</template>

<style scoped>
.organise-workspace {
  max-width: 90rem;
  margin-inline: auto;
  display: flex;
  flex-direction: column;
  gap: 1.5rem;
  color: var(--ui-text-toned);
}
h2 {
  margin: 0;
  font: 600 1.125rem/1.4 var(--font-display);
  color: var(--ui-text-highlighted);
  letter-spacing: -0.015em;
  text-wrap: balance;
}
p {
  margin: 0;
  text-wrap: pretty;
}
.hint {
  margin: 0;
  font-size: 0.75rem;
  line-height: 1.6;
  color: var(--ui-text-dimmed);
}
.eyebrow {
  margin: 0;
  color: var(--ui-text-dimmed);
  font-size: 0.6875rem;
  font-weight: 500;
  text-transform: uppercase;
  letter-spacing: 0.06em;
}
.section-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
}
.steps {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  padding-block: 0.75rem;
}
.step {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  color: var(--ui-text-dimmed);
  font-size: 0.8125rem;
  white-space: nowrap;
}
.step.current {
  color: var(--ui-text-highlighted);
  font-weight: 600;
}
.step-number {
  display: grid;
  place-items: center;
  width: 1.5rem;
  height: 1.5rem;
  border: 1px solid var(--ui-border-accented);
  border-radius: 50%;
  font-size: 0.6875rem;
  font-variant-numeric: tabular-nums;
}
.current .step-number {
  color: var(--ui-primary);
  border-color: var(--ui-primary);
  box-shadow: 0 0 0 0.25rem var(--lumen-primary-soft);
}
.complete .step-number {
  color: var(--lumen-on-primary);
  border-color: var(--ui-primary);
  background: var(--ui-primary);
}
.step-line {
  flex: 1;
  min-width: 0.5rem;
  height: 1px;
  background: var(--ui-border);
}
.step-line.complete {
  background: var(--ui-primary);
}
.safety {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  padding: 0.75rem 1rem;
  border: 1px solid var(--ui-border);
  border-radius: var(--lumen-r-lg);
  background: var(--lumen-bg-sunken);
  font-size: 0.8125rem;
}
.safety :deep(svg) {
  flex: none;
  color: var(--ui-text-dimmed);
}
.scheme,
.preview,
.preview-loading,
.runs,
.stopped {
  display: flex;
  flex-direction: column;
  gap: 1rem;
  min-width: 0;
}
.scheme-columns {
  display: grid;
  grid-template-columns: minmax(0, 2.4fr) minmax(0, 1fr);
  gap: 1.25rem;
  align-items: start;
}
.scheme-editor,
.field {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
  min-width: 0;
}
.scheme-editor {
  gap: 0.75rem;
}
.field label {
  font-size: 0.8125rem;
  font-weight: 600;
  color: var(--ui-text-highlighted);
}
.chips {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
}
.scheme-error {
  display: flex;
  align-items: flex-start;
  gap: 0.375rem;
  color: var(--ui-error);
  font-size: 0.75rem;
  line-height: 1.6;
}
.scheme-error :deep(svg) {
  flex: none;
  margin-top: 0.125rem;
}
.example {
  display: flex;
  align-items: center;
  gap: 1rem;
  padding: 0.75rem 1rem;
  border: 1px solid var(--ui-border);
  border-radius: var(--lumen-r-lg);
  background: var(--lumen-bg-sunken);
}
.example-thumb {
  flex: none;
  width: 3rem;
  height: 3rem;
  object-fit: cover;
  border-radius: var(--lumen-r-sm);
  background: var(--lumen-placeholder);
}
.example-text {
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
  min-width: 0;
}
.crumbs {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.25rem;
  margin: 0;
  padding: 0;
  list-style: none;
  font-size: 0.8125rem;
  font-weight: 500;
  color: var(--ui-text-highlighted);
}
.crumbs li {
  display: inline-flex;
  align-items: center;
  gap: 0.25rem;
  overflow-wrap: anywhere;
}
.crumb-mark {
  color: var(--ui-text-dimmed);
}
.draft {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 0.75rem;
}
.stats {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 0.75rem;
}
.stats button {
  display: grid;
  gap: 0.25rem;
  padding: 0.75rem 1rem;
  text-align: start;
  border: 1px solid var(--ui-border);
  border-radius: var(--lumen-r-lg);
  background: var(--lumen-bg-sunken);
  color: inherit;
  cursor: pointer;
  transition:
    transform var(--lumen-d-fast) var(--lumen-ease-out),
    opacity var(--lumen-d-fast) var(--lumen-ease-out);
}
.stats button:hover:not(:disabled) {
  background: var(--lumen-bg-hover);
}
.stats button:active:not(:disabled) {
  transform: scale(0.99);
}
.stats button:disabled {
  cursor: default;
  opacity: 0.6;
}
.stats button[aria-pressed='true'] {
  border-color: var(--ui-primary);
  box-shadow: inset 0 0 0 1px var(--ui-primary);
}
.stats strong {
  font: 600 1.375rem/1.3 var(--font-display);
  font-variant-numeric: tabular-nums;
  color: var(--ui-text-highlighted);
}
.stats span {
  font-size: 0.75rem;
  color: var(--ui-text-dimmed);
}
.stat-skeleton {
  height: 4rem;
}
.rows-skeleton {
  height: 12rem;
}
.preview-columns {
  display: grid;
  grid-template-columns: 15.5rem minmax(0, 1fr);
  gap: 1.25rem;
  align-items: start;
}
.moves-card {
  min-width: 0;
  overflow: hidden;
  border: 1px solid var(--ui-border);
  border-radius: var(--lumen-r-lg);
  background: var(--lumen-bg-raised);
}
.moves-head {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  min-height: 2.5rem;
  padding: 0.5rem 1rem;
  background: var(--lumen-bg-sunken);
  border-bottom: 1px solid var(--ui-border);
}
.moves-head h3 {
  margin: 0;
  font-size: 0.8125rem;
  font-weight: 600;
  color: var(--ui-text-highlighted);
}
.range {
  margin-left: auto;
  font-size: 0.75rem;
  color: var(--ui-text-dimmed);
  font-variant-numeric: tabular-nums;
}
.moves-note {
  padding: 1rem;
  font-size: 0.8125rem;
}
.moves {
  height: 20rem;
  overflow-y: auto;
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
  align-items: flex-start;
  gap: 0.75rem;
  padding: 0.625rem 1rem;
  border-bottom: 1px solid var(--ui-border);
  font-size: 0.8125rem;
}
.move-text {
  display: flex;
  flex-direction: column;
  gap: 0.125rem;
  flex: 1;
  min-width: 0;
}
.path,
.note {
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}
.from {
  font-family: var(--font-mono);
  font-size: 0.75rem;
  color: var(--ui-text-dimmed);
}
.to {
  color: var(--ui-text-highlighted);
}
.move.left .reason {
  color: var(--ui-text-toned);
}
.note {
  font-size: 0.75rem;
  color: var(--ui-text-dimmed);
}
.move-status {
  flex: none;
}
.pagination {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  padding: 0.5rem 0.75rem;
  border-top: 1px solid var(--ui-border);
}
.pagination span {
  margin-inline: auto;
  font-size: 0.75rem;
  color: var(--ui-text-dimmed);
  font-variant-numeric: tabular-nums;
}
.preview-foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 1rem;
}
.summary {
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
  min-width: 0;
}
.summary p:first-child {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.375rem;
  font-size: 0.8125rem;
}
.card {
  display: flex;
  flex-direction: column;
  gap: 1rem;
  padding: 1.25rem;
  border: 1px solid var(--ui-border);
  border-radius: var(--lumen-r-lg);
  background: var(--lumen-bg-raised);
  box-shadow: var(--lumen-shadow-1);
}
.working .percent {
  padding: 0.125rem 0.5rem;
  border-radius: 999px;
  background: var(--lumen-light-soft);
  color: var(--lumen-light-text);
  font-size: 0.75rem;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}
.working .lumen-light-dot {
  order: -1;
}
.actions,
.run-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
}
.finished h2 {
  margin-block: 0.5rem;
  font-size: 1.375rem;
}
.finished p {
  font-size: 0.875rem;
}
.finished-mark {
  color: var(--ui-success);
}
.no-runs {
  padding: 1.25rem;
  border: 1px solid var(--ui-border);
  border-radius: var(--lumen-r-lg);
  background: var(--lumen-bg-sunken);
  font-size: 0.8125rem;
}
.run-list {
  display: flex;
  flex-direction: column;
  margin: 0;
  padding: 0;
  list-style: none;
  border: 1px solid var(--ui-border);
  border-radius: var(--lumen-r-lg);
  background: var(--lumen-bg-raised);
  overflow: hidden;
}
.run-entry {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.75rem;
  padding: 0.75rem 1rem;
}
.run-entry + .run-entry {
  border-top: 1px solid var(--ui-border);
}
.run-mark {
  flex: none;
  color: var(--ui-text-dimmed);
}
.run-text {
  display: flex;
  flex-direction: column;
  gap: 0.125rem;
  flex: 1;
  min-width: 12rem;
  font-size: 0.8125rem;
}
.run-text strong {
  color: var(--ui-text-highlighted);
  font-weight: 600;
}
.run-count {
  font-size: 0.75rem;
  color: var(--ui-text-toned);
  font-variant-numeric: tabular-nums;
}
.run-scheme {
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
  font-size: 0.75rem;
  color: var(--ui-text-dimmed);
}
.run-scheme.mono {
  font-family: var(--font-mono);
}
@container (max-width: 64rem) {
  .scheme-columns,
  .preview-columns {
    grid-template-columns: minmax(0, 1fr);
  }
}
@container (max-width: 44rem) {
  .stats {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .step {
    gap: 0.25rem;
    font-size: 0.75rem;
  }
  .section-head {
    flex-wrap: wrap;
  }
  .preview-foot > :deep(button) {
    width: 100%;
    justify-content: center;
  }
}
</style>
