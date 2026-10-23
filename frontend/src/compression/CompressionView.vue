<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UCheckbox from '@nuxt/ui/components/Checkbox.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import URadioGroup from '@nuxt/ui/components/RadioGroup.vue'
import UBadge from '@nuxt/ui/components/Badge.vue'
import { RouterLink } from 'vue-router'
import { icons } from '../design/icons'
import Icon from '../shared/Icon.vue'
import Segmented from '../shared/Segmented.vue'
import ViewBar from '../shared/ViewBar.vue'
import WorkProgress from '../shared/WorkProgress.vue'
import { notify } from '../shared/notify'
import USelect from '@nuxt/ui/components/Select.vue'
import { COMPRESSION_PAGE_SIZE, COMPRESSION_SAVINGS_MAX, CompressionMode, CompressionState, engine } from '../bridge'
import type { CompressionInput, CompressionJob, CompressionOptions, CompressionRun } from '../bridge'
import Page from '../shared/Page.vue'
import PageHeader from '../shared/PageHeader.vue'
import FeatureState from '../shared/FeatureState.vue'
import OutcomeList from '../media/OutcomeList.vue'
import { compressionRequest, compressionModes, compressionEfforts } from '../shared/compression'
import { confirm } from '../shared/confirm'
import { errorText } from '../shared/errors'
import { bytes, fileName } from '../shared/format'
import { LOCALE, t } from '../shared/i18n'
import type { MessageKey } from '../shared/i18n'
import { useLiveReload } from '../shared/liveReload'
import { usePreferences } from '../shared/preferences'

const prefs = usePreferences()
const available = ref(true)
const loading = ref(true)
const workspace = ref<HTMLElement | null>(null)
const commitTotal = ref(0)
const committedBefore = ref(0)
const error = ref('')
const readError = ref('')
const submitting = ref(false)
const inputs = shallowRef<CompressionInput[]>([])
const paths = shallowRef<Record<number, string[]>>({})
const options = ref<CompressionOptions>({ ...prefs.prefs.compression })
const directory = ref('')
const destination = ref('beside')
const inFolder = computed(() => destination.value === 'folder')
const maxThreads = ref(1)
const statusFilter = ref<CompressionState | 'all'>('all')
const pageJobs = computed(() =>
  statusFilter.value === 'all' ? jobs.value : jobs.value.filter((job) => job.state === statusFilter.value),
)
const filterItems = computed(() => [
  { value: 'all', label: t('compression.all') },
  ...[
    CompressionState.CompressionReady,
    CompressionState.CompressionSkipped,
    CompressionState.CompressionFailed,
    CompressionState.CompressionDone,
  ].map((value) => ({ value, label: stateText(value) })),
])
const selectedJobs = new Map<number, CompressionJob>()
const selectedSizes = computed(() => {
  let before = 0
  let after = 0
  for (const id of selected.value) {
    const job = selectedJobs.get(id)
    if (job) {
      before += job.size
      after += job.outputSize
    }
  }
  return { before, after, saved: Math.max(0, before - after) }
})
const badDirectory = computed(() => {
  if (!inFolder.value) return false
  const path = directory.value.replaceAll(String.fromCharCode(92), '/')
  const drive = /^[a-z]:/i.test(path) && path[2] === '/'
  const unc = path.startsWith('//') && path.slice(2).split('/').filter(Boolean).length >= 2
  const rest = path.slice(drive ? 3 : 2)
  return !(drive || unc) || /[<>:"|?*]/.test(rest) || Array.from(path).some((ch) => (ch.codePointAt(0) ?? 0) < 32)
})
const steps = [
  'compression.step.choose',
  'compression.step.prepare',
  'compression.step.review',
  'compression.step.save',
] as const
const step = computed(() =>
  !run.value || run.value.state === CompressionState.CompressionPlanned
    ? 0
    : run.value.state === CompressionState.CompressionDone || run.value.state === CompressionState.CompressionCommitting
      ? 3
      : run.value.ready
        ? 2
        : 1,
)
const whenFmt = new Intl.DateTimeFormat(LOCALE, { dateStyle: 'medium', timeStyle: 'short' })
const when = (seconds: number) => whenFmt.format(new Date(seconds * 1000))
const caption = computed(() =>
  run.value ? `${t('compression.run', { n: run.value.id })} · ${stateText(run.value.state)}` : t('compression.caption'),
)
const progressDetail = (item: CompressionRun) =>
  t('compression.progress', {
    n: item.processed,
    total: item.total,
    ready: item.ready,
    done: item.done,
    skipped: item.skipped,
    failed: item.failed,
    cancelled: item.cancelled,
  })
const runs = shallowRef<CompressionRun[]>([])
const jobs = shallowRef<CompressionJob[]>([])
const runID = ref(0)
const offset = ref(0)
const total = ref(0)
const selected = shallowRef<Set<number>>(new Set())
const run = computed(() => runs.value.find((item) => item.id === runID.value))
const active = (state: CompressionState | undefined) =>
  state === CompressionState.CompressionEncoding ||
  state === CompressionState.CompressionVerifying ||
  state === CompressionState.CompressionCommitting
const working = computed(() => active(run.value?.state))
watch([runID, working], async () => {
  await nextTick()
  const scroller = workspace.value?.parentElement
  if (scroller) scroller.scrollTop = 0
})
const anyActive = computed(() => runs.value.some((item) => active(item.state)))
const ready = computed(() => jobs.value.filter((job) => job.state === CompressionState.CompressionReady))
const canAnalyse = computed(() => !!run.value && run.value.total > run.value.done + run.value.ready + run.value.skipped)
const canCancel = computed(
  () => !!run.value && run.value.total > run.value.done + run.value.skipped + run.value.cancelled,
)
const modes = [...compressionModes, CompressionMode.CompressRestore]
  .filter((value) => value !== CompressionMode.$zero)
  .map((value) => ({
    value,
    label: t(`compression.mode.${value}`),
    description: t(`compression.description.${value}`),
    tag: t(`compression.tag.${value}`),
    disabled: value === CompressionMode.CompressRestore,
  }))
const efforts = compressionEfforts.map((value) => ({ value, label: t(`compression.effort.${value}`) }))
const reasons: Record<string, MessageKey> = {
  unsupported: 'compression.reason.unsupported',
  preservation: 'compression.reason.preservation',
  no_gain: 'compression.reason.no_gain',
  limits: 'compression.reason.limits',
  changed: 'compression.reason.changed',
  taken: 'compression.reason.taken',
  links: 'compression.reason.links',
  space: 'compression.reason.space',
  not_visible: 'compression.reason.not_visible',
  missing: 'compression.reason.missing',
  interrupted: 'compression.reason.interrupted',
  timeout: 'compression.reason.timeout',
  codec: 'compression.reason.codec',
}
const stateText = (state: CompressionState) => (state ? t(`compression.state.${state}`) : '')
let generation = 0
let refreshPending = false
let refreshAgain = false
const disposed = ref(false)
let poll: ReturnType<typeof setInterval> | undefined
const seenReady = new Set<number>()

function invalidatePlan() {
  generation++
  runID.value = 0
  commitTotal.value = 0
  offset.value = 0
  total.value = 0
  jobs.value = []
  selected.value = new Set()
  seenReady.clear()
  selectedJobs.clear()
  statusFilter.value = 'all'
}
watch([options, directory, inFolder, inputs], invalidatePlan, { deep: true, flush: 'sync' })
watch(
  compressionRequest,
  (request) => {
    if (request) {
      inputs.value = request.inputs.map((input) => ({ ...input }))
      paths.value = {}
    }
  },
  { immediate: true },
)

async function refresh() {
  if (disposed.value) return
  readError.value = ''
  if (refreshPending) {
    refreshAgain = true
    return
  }
  refreshPending = true
  const mine = generation
  const id = runID.value
  try {
    const history = await engine.compressionRuns()
    const page = id ? await engine.compressionJobs(id, offset.value) : { jobs: [], total: 0, offset: 0 }
    const outcomes = page.jobs
    if (mine !== generation) return
    runs.value = history
    jobs.value = outcomes
    total.value = page.total
    const next = new Set(
      [...selected.value].filter(
        (id) => !outcomes.some((job) => job.id === id && job.state !== CompressionState.CompressionReady),
      ),
    )
    for (const job of outcomes) {
      selectedJobs.set(job.id, job)
      if (job.state === CompressionState.CompressionReady && !seenReady.has(job.id)) {
        seenReady.add(job.id)
        next.add(job.id)
      }
    }
    selected.value = next
  } catch (e) {
    if (mine === generation) readError.value = errorText(e)
  } finally {
    refreshPending = false
    if (mine === generation) loading.value = false
    if (refreshAgain) {
      refreshAgain = false
      void refresh()
    }
  }
}

async function loadCapabilities() {
  available.value = await engine.compressionAvailable()
  const budget = await engine.settings()
  maxThreads.value = Math.max(1, Math.min(budget.cpuThreads, budget.defaultCPUThreads))
  options.value.threads = Math.min(maxThreads.value, Math.max(1, options.value.threads))
}

async function retry() {
  error.value = ''
  try {
    await loadCapabilities()
    await refresh()
  } catch (e) {
    error.value = errorText(e)
  }
}

async function perform(action: () => Promise<unknown>) {
  if (submitting.value) return
  submitting.value = true
  error.value = ''
  try {
    await action()
    await refresh()
  } catch (e) {
    error.value = errorText(e)
  } finally {
    submitting.value = false
  }
}

function chooseMode(value: unknown) {
  const mode = compressionModes.find((item) => item === value)
  if (mode) options.value.mode = mode
}

function reveal(job: CompressionJob) {
  return perform(() => engine.revealCompression(job.runId, job.id))
}

function analyse() {
  return perform(() => engine.analyseCompression(runID.value))
}

function pause() {
  return perform(() => engine.pauseCompression(runID.value))
}

function cancel() {
  return perform(() => engine.cancelCompression(runID.value))
}

async function plan() {
  if (badDirectory.value || !inputs.value.length || anyActive.value || !available.value) return
  const mine = generation
  await perform(async () => {
    if (compressionRequest.value?.selection) await engine.validateSelection(compressionRequest.value.selection)
    const result = await engine.planCompression(inputs.value, inFolder.value ? directory.value : '', options.value)
    if (mine === generation) {
      runID.value = result.id
      offset.value = 0
    }
  })
}

async function chooseDirectory() {
  await perform(async () => {
    const path = await engine.pickFolder()
    if (path) {
      directory.value = path
      destination.value = 'folder'
    }
  })
}
async function loadCopies(input: CompressionInput) {
  await perform(async () => {
    const asset = await engine.asset(input.assetId)
    paths.value = { ...paths.value, [input.assetId]: asset.paths }
  })
}
function chooseCopy(index: number, path: string) {
  inputs.value = inputs.value.map((input, i) => (i === index ? { ...input, path } : input))
}
function inspect(id: number) {
  commitTotal.value = 0
  statusFilter.value = 'all'
  selectedJobs.clear()
  generation++
  runID.value = id
  offset.value = 0
  jobs.value = []
  selected.value = new Set()
  seenReady.clear()
  void refresh()
}
function openActive() {
  const current = runs.value.find((item) => active(item.state))
  if (current) inspect(current.id)
}
function pageBy(step: number) {
  generation++
  offset.value = Math.max(0, offset.value + step * COMPRESSION_PAGE_SIZE)
  void refresh()
}
// The field holds whatever was typed; the plan takes only a whole percentage
// within the engine's bounds, which is also what the field then shows.
function setMinSavings(event: Event) {
  if (!(event.target instanceof HTMLInputElement)) return
  const n = Math.round(Number(event.target.value))
  const percent = Number.isFinite(n) ? Math.min(COMPRESSION_SAVINGS_MAX, Math.max(0, n)) : 0
  options.value.minSavings = percent
  event.target.value = String(percent)
}
function toggle(id: number, value: boolean | 'indeterminate') {
  const next = new Set(selected.value)
  if (value === true) next.add(id)
  else next.delete(id)
  selected.value = next
}
async function commit() {
  const id = runID.value
  const chosen = [...selected.value]
  if (!chosen.length || working.value) return
  await perform(async () => {
    if (
      !(await confirm(
        t('compression.confirmTitle', { n: chosen.length }),
        `${t('compression.confirm')} ${t('compression.selectionStorage', { n: chosen.length, added: bytes(selectedSizes.value.after), saved: bytes(selectedSizes.value.saved) })}`,
        t('compression.commit'),
      ))
    )
      return
    if (runID.value !== id) return
    if (anyActive.value || chosen.some((id) => selectedJobs.get(id)?.state !== CompressionState.CompressionReady))
      return
    commitTotal.value = chosen.length
    committedBefore.value = run.value?.done ?? 0
    await engine.commitCompression(id, chosen)
  })
}
function resetOptions() {
  options.value = { ...prefs.prefs.compression }
  directory.value = ''
  destination.value = 'beside'
  inputs.value = inputs.value.map((input) => ({ ...input, path: '' }))
  notify({ id: 'compression.reset', kind: 'info', title: t('compression.resetDone') })
}
function setThreads(event: Event) {
  if (!(event.target instanceof HTMLInputElement)) return
  const n = Math.round(Number(event.target.value))
  const count = Number.isFinite(n) ? Math.min(maxThreads.value, Math.max(1, n)) : 1
  options.value.threads = count
  event.target.value = String(count)
}
async function restore(job: CompressionJob) {
  const mine = generation
  await perform(async () => {
    const result = await engine.restoreCompression(job.runId, job.id, inFolder.value ? directory.value : '')
    if (mine !== generation) return
    selectedJobs.clear()
    statusFilter.value = 'all'
    commitTotal.value = 0
    runID.value = result.id
    offset.value = 0
    selected.value = new Set()
    seenReady.clear()
  })
}

useLiveReload(
  async () => {
    generation++
    jobs.value = []
    runs.value = []
    selected.value = new Set()
    await refresh()
  },
  (change) => change.assets,
  1,
)
onMounted(async () => {
  try {
    await prefs.load()
    options.value = { ...prefs.prefs.compression }
    await loadCapabilities()
  } catch (e) {
    error.value = errorText(e)
  }
  await refresh()
  if (disposed.value) return
  // Only a running worker changes anything between the user's own actions,
  // each of which reloads: an idle page asks the engine nothing.
  poll = setInterval(() => {
    if (anyActive.value) void refresh()
  }, 500)
})
onBeforeUnmount(() => {
  disposed.value = true
  generation++
  clearInterval(poll)
})
</script>

<template>
  <Page>
    <template #header
      ><PageHeader :title="t('compression.title')" :subtitle="caption"
        ><UButton to="/" variant="ghost" :icon="icons.photos">{{ t('compression.back') }}</UButton></PageHeader
      ></template
    >
    <FeatureState v-if="error || readError" kind="error" :title="error || readError"
      ><UButton variant="outline" @click="retry">{{ t('compression.retry') }}</UButton></FeatureState
    >
    <FeatureState
      v-if="!available"
      kind="error"
      :title="t('compression.unavailableTitle')"
      :text="t('compression.unavailable')"
      ><UButton to="/settings">{{ t('compression.settings') }}</UButton></FeatureState
    >
    <FeatureState v-if="loading" kind="loading" :title="t('compression.state.planned')" />
    <div v-else ref="workspace" data-compression-workspace class="compression-workspace">
      <nav class="steps" :aria-label="t('compression.steps')">
        <template v-for="(label, index) in steps" :key="label"
          ><span
            class="step"
            :class="{ current: step === index, complete: step > index }"
            :aria-current="step === index ? 'step' : undefined"
            ><span class="step-number">{{ index + 1 }}</span
            >{{ t(label) }}</span
          ><span v-if="index < steps.length - 1" class="step-line"
        /></template>
      </nav>
      <div class="workspace-columns">
        <div class="workspace-main">
          <UAlert v-if="anyActive && !runID" color="neutral" variant="subtle" :title="t('compression.busy')"
            ><template #actions
              ><UButton variant="outline" @click="openActive">{{ t('compression.openActive') }}</UButton></template
            ></UAlert
          >
          <section
            v-if="inputs.length && !runID && available"
            :aria-label="t('compression.action')"
            class="compression-plan"
          >
            <div>
              <p class="eyebrow">{{ t('compression.chooseStep') }}</p>
              <h2>{{ t('compression.heading') }}</h2>
              <p>{{ t('compression.hint') }}</p>
            </div>
            <div class="selection-scope">
              <Icon name="photos" :size="24" /><strong>{{ t('compression.scope', { n: inputs.length }) }}</strong
              ><UButton to="/" variant="ghost">{{ t('compression.change') }}</UButton>
            </div>
            <details class="source-disclosure">
              <summary>{{ t('compression.sources', { n: inputs.length }) }}</summary>
              <p>{{ t('compression.sourceHint') }}</p>
              <OutcomeList v-slot="{ item: input }" :items="inputs"
                ><div :key="input.assetId" class="compression-source">
                  <span>{{ input.path || t('compression.source') }} #{{ input.assetId }}</span>
                  <UButton
                    v-if="!paths[input.assetId]"
                    variant="ghost"
                    :disabled="submitting"
                    @click="loadCopies(input)"
                    >{{ t('compression.source') }}</UButton
                  >
                  <USelect
                    v-else
                    :model-value="input.path"
                    :items="[
                      { label: t('compression.automaticSource'), value: '' },
                      ...(paths[input.assetId] ?? []).map((path) => ({ label: path, value: path })),
                    ]"
                    :aria-label="t('compression.source')"
                    :disabled="submitting"
                    @update:model-value="
                      (path) => {
                        if (typeof path === 'string') chooseCopy(inputs.indexOf(input), path)
                      }
                    "
                  /></div
              ></OutcomeList>
            </details>
            <URadioGroup
              :model-value="modes.find((item) => item.value === options.mode)?.value"
              :items="modes"
              :legend="t('compression.mode')"
              variant="card"
              class="mode-cards"
              :disabled="submitting"
              :ui="{ fieldset: 'grid grid-cols-2 gap-3', item: 'mode-card', base: 'size-6', wrapper: 'min-w-0' }"
              @update:model-value="chooseMode"
              ><template #label="{ item }"
                >{{ item.label }}<span class="mode-tag" aria-hidden="true">{{ item.tag }}</span></template
              ></URadioGroup
            >
            <UAlert color="neutral" variant="subtle" :icon="icons.info" :title="t('compression.metadata')" />
            <p v-if="options.mode !== CompressionMode.CompressCompatible">{{ t('compression.compatibility') }}</p>
            <p v-if="options.mode === CompressionMode.CompressConvert">{{ t('compression.conversion') }}</p>
            <div class="effort">
              <div class="field">
                <span>{{ t('compression.effort') }}</span
                ><Segmented
                  v-model="options.effort"
                  :items="efforts.map((item) => ({ ...item, disabled: submitting }))"
                  :label="t('compression.effort')"
                />
              </div>
              <p>{{ t(`compression.effortHint.${options.effort || 'balanced'}`) }}</p>
            </div>
            <details class="advanced">
              <summary>{{ t('compression.advanced') }}</summary>
              <div class="compression-fields">
                <label
                  >{{ t('compression.minSavings')
                  }}<UInput
                    :model-value="options.minSavings"
                    type="number"
                    :min="0"
                    :max="COMPRESSION_SAVINGS_MAX"
                    :disabled="submitting"
                    :aria-label="t('compression.minSavings')"
                    @change="setMinSavings"
                /></label>
                <label
                  >{{ t('compression.threads')
                  }}<UInput
                    :model-value="options.threads"
                    type="number"
                    :min="1"
                    :max="maxThreads"
                    :disabled="submitting"
                    :aria-label="t('compression.threads')"
                    @change="setThreads"
                /></label>
              </div>
              <p>{{ t('compression.advancedHint', { n: maxThreads }) }}</p>
            </details>
            <label class="field"
              >{{ t('compression.destination')
              }}<USelect
                v-model="destination"
                :items="[
                  { label: t('compression.beside'), value: 'beside' },
                  { label: t('compression.folder'), value: 'folder' },
                ]"
                :disabled="submitting"
                :aria-label="t('compression.destination')"
            /></label>
            <div v-if="inFolder" class="compression-fields">
              <UInput v-model="directory" :disabled="submitting" :aria-label="t('compression.path')" /><UButton
                variant="outline"
                :disabled="submitting"
                @click="chooseDirectory"
                >{{ t('files.choose') }}</UButton
              >
            </div>
            <p v-if="badDirectory" role="alert">{{ t('compression.badFolder') }}</p>
            <div class="compression-actions">
              <UButton variant="ghost" :disabled="submitting" @click="resetOptions">{{
                t('compression.reset')
              }}</UButton
              ><RouterLink to="/settings?section=compression">{{ t('compression.defaults') }}</RouterLink>
            </div>
            <UButton
              data-compression-plan
              color="primary"
              variant="solid"
              :disabled="!available || anyActive || submitting || badDirectory"
              :title="badDirectory ? t('compression.badFolder') : anyActive ? t('compression.busy') : undefined"
              @click="plan"
              >{{ t('compression.plan') }}</UButton
            >
          </section>
          <FeatureState
            v-else-if="!runID && available"
            kind="empty"
            icon="compress"
            :title="t('compression.emptyTitle')"
            :text="t('compression.empty')"
            ><UButton to="/" color="primary" variant="solid">{{ t('compression.openLibrary') }}</UButton></FeatureState
          >

          <section
            v-if="runID"
            data-compression-results
            class="compression-results"
            :aria-label="t('compression.run', { n: runID })"
          >
            <h2>
              {{
                run?.state === CompressionState.CompressionPlanned
                  ? t('compression.plannedTitle', { n: run.total })
                  : run?.state === CompressionState.CompressionReady
                    ? t('compression.reviewTitle')
                    : run?.state === CompressionState.CompressionDone
                      ? t(run.done ? 'compression.doneTitle' : 'compression.noCopies')
                      : run
                        ? stateText(run.state)
                        : t('compression.run', { n: runID })
              }}
            </h2>
            <p v-if="run?.options.mode">{{ t(`compression.mode.${run.options.mode}`) }}</p>
            <p v-if="run?.options.mode === CompressionMode.CompressRestore">{{ t('compression.restoreHint') }}</p>
            <WorkProgress
              v-if="run && (working || run.state === CompressionState.CompressionPaused)"
              :label="stateText(run.state)"
              :detail="progressDetail(run)"
              :done="
                run.state === CompressionState.CompressionCommitting
                  ? Math.max(0, run.done - committedBefore)
                  : run.processed
              "
              :total="run.state === CompressionState.CompressionCommitting ? commitTotal : run.total"
              :indeterminate="run.state === CompressionState.CompressionCommitting && !commitTotal"
              activity
            />
            <p v-else-if="run" role="status">{{ progressDetail(run) }}</p>
            <p v-if="run?.state === CompressionState.CompressionPlanned">{{ t('compression.plannedHint') }}</p>
            <p v-if="run?.state === CompressionState.CompressionCancelled">{{ t('compression.cancelledHint') }}</p>
            <div v-if="run?.state === CompressionState.CompressionDone" class="run-stats">
              <div>
                <strong>{{ bytes(run.savedBytes) }}</strong
                ><span>{{ t('compression.reduction') }}</span>
              </div>
              <div>
                <strong>{{ bytes(run.addedBytes) }}</strong
                ><span>{{ t('compression.added') }}</span>
              </div>
            </div>
            <ViewBar
              v-if="!working && run?.state !== CompressionState.CompressionPlanned"
              :label="t('compression.filter')"
              class="candidate-filters"
              ><Segmented v-model="statusFilter" :items="filterItems" :label="t('compression.filter')" look="tabs"
            /></ViewBar>
            <p v-if="!pageJobs.length">{{ t('compression.noResults') }}</p>
            <p v-if="run">
              {{ t('compression.savings', { saved: bytes(run.savedBytes), added: bytes(run.addedBytes) }) }}
            </p>
            <OutcomeList v-slot="{ item: job }" :items="pageJobs">
              <div :key="job.id" class="compression-outcome" :data-compression-job="job.id">
                <UCheckbox
                  v-if="job.state === CompressionState.CompressionReady"
                  :model-value="selected.has(job.id)"
                  :aria-label="t('compression.saveFile', { name: job.from })"
                  :disabled="working || submitting"
                  @update:model-value="(value) => toggle(job.id, value)"
                />
                <Icon
                  v-else
                  :name="job.state === CompressionState.CompressionDone ? 'success' : job.reason ? 'warning' : 'files'"
                  :size="20"
                />
                <div class="outcome-paths">
                  <strong :title="job.from">{{ fileName(job.from) }}</strong>
                  <p
                    v-if="working || run?.state === CompressionState.CompressionPlanned"
                    class="source-path"
                    :title="job.from"
                  >
                    {{ job.from }}
                  </p>
                  <p class="destination-path" :title="job.to">→ {{ job.to }}</p>
                  <p v-if="job.copies > 1">{{ t('compression.copies', { n: job.copies }) }}</p>
                  <p v-if="job.reason || !job.verification">
                    {{ stateText(job.state)
                    }}{{ job.reason ? `: ${t(reasons[job.reason] ?? 'compression.reason.codec')}` : '' }}
                  </p>
                  <UBadge v-if="job.verification" color="success" variant="subtle">{{
                    t('compression.verified')
                  }}</UBadge>
                </div>
                <div v-if="job.outputSize" class="outcome-sizes">
                  <strong>{{ bytes(job.size) }} → {{ bytes(job.outputSize) }}</strong
                  ><span>{{
                    job.outputSize > job.size
                      ? t('compression.sizeIncrease', { added: bytes(job.outputSize - job.size) })
                      : t('compression.sizeReduction', { saved: bytes(job.size - job.outputSize) })
                  }}</span>
                </div>
                <div class="outcome-actions">
                  <UButton
                    v-if="
                      job.state === CompressionState.CompressionDone &&
                      run?.options.mode === CompressionMode.CompressReversible
                    "
                    variant="outline"
                    :disabled="anyActive || submitting"
                    @click="restore(job)"
                    >{{ t('compression.restore') }}</UButton
                  >
                  <UButton v-if="job.state === CompressionState.CompressionDone" variant="ghost" @click="reveal(job)">{{
                    t('compression.reveal')
                  }}</UButton>
                </div>
              </div>
            </OutcomeList>
            <div v-if="total > COMPRESSION_PAGE_SIZE" class="compression-actions">
              <UButton variant="outline" :disabled="!offset || submitting" @click="pageBy(-1)">{{
                t('compression.previous')
              }}</UButton>
              <span>{{
                t('compression.page', {
                  first: offset + 1,
                  last: Math.min(offset + COMPRESSION_PAGE_SIZE, total),
                  total,
                })
              }}</span>
              <UButton
                variant="outline"
                :disabled="offset + COMPRESSION_PAGE_SIZE >= total || submitting"
                @click="pageBy(1)"
                >{{ t('compression.next') }}</UButton
              >
            </div>
            <div class="compression-actions">
              <p v-if="selected.size">
                {{
                  t('compression.selectionStorage', {
                    n: selected.size,
                    added: bytes(selectedSizes.after),
                    saved: bytes(selectedSizes.saved),
                  })
                }}
              </p>
              <span v-if="selected.size">{{ t('compression.selected', { n: selected.size }) }}</span>
              <UButton
                v-if="!working && canAnalyse"
                data-compression-analyse
                :color="run?.ready ? 'neutral' : 'primary'"
                :variant="run?.ready ? 'outline' : 'solid'"
                :disabled="anyActive || submitting || !available"
                @click="analyse"
                >{{
                  run?.state === CompressionState.CompressionPlanned
                    ? t('compression.analyse')
                    : t('compression.resume')
                }}</UButton
              >
              <UButton
                v-if="selected.size || ready.length"
                data-compression-commit
                color="primary"
                variant="solid"
                :disabled="anyActive || submitting || !selected.size"
                :title="!selected.size ? t('compression.noneSelected') : undefined"
                @click="commit"
                >{{ t('compression.commit') }}</UButton
              >
              <UButton
                v-if="run?.state === CompressionState.CompressionEncoding"
                variant="outline"
                :disabled="submitting"
                @click="pause"
                >{{ t('compression.pause') }}</UButton
              >
              <UButton v-if="canCancel" variant="outline" :disabled="submitting" @click="cancel">{{
                t('compression.cancel')
              }}</UButton>
            </div>
            <UButton v-if="!working" variant="ghost" :disabled="submitting" @click="invalidatePlan">{{
              t('compression.changeOptions')
            }}</UButton>
          </section>
        </div>
        <aside class="workspace-aside">
          <section class="safety">
            <Icon name="blocked" :size="24" />
            <h2>{{ t('compression.safetyTitle') }}</h2>
            <p>{{ t('compression.safetyText') }}</p>
            <hr />
            <p>{{ t('compression.storage') }}</p>
          </section>
          <section v-if="runs.length" class="compression-history" :aria-label="t('compression.history')">
            <h2>{{ t('compression.history') }}</h2>
            <div v-for="item in runs" :key="item.id" class="history-entry">
              <UButton variant="ghost" :disabled="submitting" @click="inspect(item.id)"
                ><strong>{{ t('compression.run', { n: item.id }) }} · {{ stateText(item.state) }}</strong
                ><time :datetime="new Date(item.createdAt * 1000).toISOString()">{{ when(item.createdAt) }}</time>
                <span>{{ t('compression.historyCounts', { n: item.total, done: item.done }) }}</span
                ><span>{{
                  t('compression.savings', { saved: bytes(item.savedBytes), added: bytes(item.addedBytes) })
                }}</span></UButton
              >
            </div>
          </section>

          <p class="text-muted">{{ t('compression.localHistory') }}</p>
        </aside>
      </div>
    </div>
  </Page>
</template>

<style scoped>
.compression-workspace {
  max-width: 90rem;
  margin-inline: auto;
  display: grid;
  gap: 2rem;
  color: var(--ui-text-toned);
}
.workspace-columns {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 15.5rem;
  align-items: start;
  gap: 1.5rem;
}
.workspace-main,
.workspace-aside,
.compression-plan,
.compression-results {
  display: flex;
  flex-direction: column;
  gap: 1.25rem;
  min-width: 0;
}
h2 {
  margin: 0;
  color: var(--ui-text-highlighted);
  font: 600 1.375rem/1.3 var(--font-display);
  letter-spacing: -0.02em;
}
p {
  margin: 0;
  font-size: 0.8125rem;
  line-height: 1.6;
  overflow-wrap: anywhere;
}
.eyebrow {
  margin-bottom: 0.5rem;
  color: var(--ui-text-dimmed);
  font-size: 0.6875rem;
  text-transform: uppercase;
  letter-spacing: 0.06em;
}
.steps {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  padding-block: 1rem;
}
.step {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  color: var(--ui-text-dimmed);
  font-size: 0.75rem;
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
  border: 1px solid var(--ui-border);
  border-radius: 50%;
  font-variant-numeric: tabular-nums;
}
.current .step-number {
  border-color: var(--ui-primary);
  color: var(--ui-primary);
  box-shadow: 0 0 0 0.25rem var(--lumen-primary-soft);
}
.complete .step-number {
  background: var(--ui-primary);
  color: var(--lumen-on-primary);
}
.step-line {
  height: 1px;
  flex: 1;
  background: var(--ui-border);
}
.selection-scope,
.advanced,
.source-disclosure,
.safety {
  padding: 1rem;
  border: 1px solid var(--ui-border);
  border-radius: var(--lumen-r-lg);
  background: var(--lumen-bg-sunken);
}
.selection-scope {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  font-size: 0.8125rem;
}
.selection-scope :last-child {
  margin-left: auto;
}
summary {
  cursor: pointer;
  min-height: 1.5rem;
  font-weight: 600;
  font-size: 0.8125rem;
  color: var(--ui-text-highlighted);
}
.advanced[open] summary,
.source-disclosure[open] summary {
  margin-bottom: 1rem;
}
.mode-cards :deep(.mode-card) {
  position: relative;
  gap: 0.75rem;
  flex-direction: column;
  align-items: start;
  padding: 1rem;
  border-radius: var(--lumen-r-lg);
  background: var(--lumen-bg-raised);
  box-shadow: var(--lumen-shadow-1);
}
.mode-cards :deep(.mode-card:has([data-state='checked'])) {
  background: var(--lumen-primary-soft);
  border-color: var(--ui-primary);
}
.mode-tag {
  position: absolute;
  top: 1rem;
  right: 1rem;
  padding: 0.125rem 0.5rem;
  border-radius: var(--lumen-r-xl);
  background: var(--lumen-bg-active);
  color: var(--ui-text-toned);
  font-size: 0.6875rem;
  font-weight: 400;
}
.mode-cards :deep([data-slot='label']) {
  font-size: 0.8125rem;
  color: var(--ui-text-highlighted);
}
.mode-cards :deep([data-slot='description']) {
  margin-top: 0.5rem;
  font-size: 0.75rem;
  color: var(--ui-text-dimmed);
}
.effort {
  display: flex;
  gap: 1.25rem;
  align-items: center;
}
.effort p {
  flex: 1;
  color: var(--ui-text-dimmed);
  font-size: 0.75rem;
}
.field,
.compression-fields label {
  display: grid;
  gap: 0.5rem;
  font-size: 0.8125rem;
  font-weight: 500;
}
.compression-fields {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 1rem;
}
.compression-source,
.compression-outcome {
  padding: 1rem;
  border-bottom: 1px solid var(--ui-border);
  overflow-wrap: anywhere;
  min-width: 0;
}
.compression-outcome {
  display: grid;
  gap: 0.5rem;
  background: var(--lumen-bg-raised);
  font-size: 0.8125rem;
}
.compression-outcome :deep([role='checkbox']) {
  min-width: 1.5rem;
  min-height: 1.5rem;
}
.compression-outcome p,
.run-stats,
.history-entry {
  font-variant-numeric: tabular-nums;
}
.compression-actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.5rem;
}
.compression-actions p {
  flex-basis: 100%;
}
.compression-actions a {
  font-size: 0.75rem;
  color: var(--ui-text-toned);
}
.run-stats {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0.75rem;
}
.run-stats > div {
  display: grid;
  gap: 0.5rem;
  padding: 1rem;
  border: 1px solid var(--ui-border);
  border-radius: var(--lumen-r-lg);
  background: var(--lumen-bg-sunken);
}
.run-stats strong {
  font-size: 1.25rem;
  color: var(--ui-text-highlighted);
}
.run-stats span {
  font-size: 0.75rem;
}
.safety {
  display: grid;
  gap: 1rem;
  padding: 1.25rem;
}
.safety h2,
.compression-history h2 {
  font-size: 0.9375rem;
  letter-spacing: 0;
}
.safety p,
.workspace-aside > p {
  font-size: 0.75rem;
  color: var(--ui-text-dimmed);
}
.safety hr {
  border: 0;
  border-top: 1px solid var(--ui-border);
}
.compression-history {
  display: grid;
  gap: 0.75rem;
}
.history-entry :deep(button) {
  display: flex;
  align-items: start;
  flex-direction: column;
  gap: 0.5rem;
  width: 100%;
  padding: 0.75rem;
  border: 1px solid var(--ui-border);
  border-radius: var(--lumen-r-lg);
  background: var(--lumen-bg-raised);
  white-space: normal;
  text-align: start;
}
.history-entry span,
.history-entry time {
  color: var(--ui-text-dimmed);
  font-size: 0.75rem;
  font-weight: 400;
}
@container (max-width: 64rem) {
  .workspace-columns {
    grid-template-columns: minmax(0, 1fr);
  }
  .workspace-aside {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    align-items: start;
  }
}
@container (max-width: 44rem) {
  .workspace-aside {
    display: flex;
  }
  .run-stats {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .effort {
    flex-wrap: wrap;
  }
  .step {
    gap: 0.25rem;
  }
}
.compression-outcome {
  display: flex;
  align-items: start;
  gap: 0.75rem;
}
.outcome-paths {
  display: flex;
  flex-direction: column;
  align-items: start;
  gap: 0.375rem;
  min-width: 0;
  flex: 1;
}
.outcome-paths .source-path,
.outcome-paths .destination-path {
  max-width: 100%;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  color: var(--ui-text-dimmed);
}
.outcome-sizes {
  display: grid;
  gap: 0.375rem;
  flex: none;
  text-align: end;
  font-size: 0.75rem;
  font-variant-numeric: tabular-nums;
}
.outcome-sizes span {
  color: var(--ui-text-dimmed);
}
.outcome-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
}
.outcome-actions:empty {
  display: none;
}
@container (max-width: 44rem) {
  .compression-outcome {
    flex-wrap: wrap;
  }
  .outcome-sizes {
    margin-left: 2.25rem;
  }
  .outcome-paths {
    flex-basis: calc(100% - 3rem);
  }
}
.workspace-main > .compression-results > :deep(button:last-child) {
  align-self: start;
}
.compression-results :deep(.outcomes p) {
  margin: 0;
  line-height: 1.4;
}
.candidate-filters {
  padding: 0;
}
.candidate-filters :deep(.start) {
  overflow-x: auto;
  padding-block: 0.25rem;
}
</style>
