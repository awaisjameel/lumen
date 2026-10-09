<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UCheckbox from '@nuxt/ui/components/Checkbox.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import USelect from '@nuxt/ui/components/Select.vue'
import { CompressionMode, CompressionState, engine } from '../bridge'
import type { CompressionInput, CompressionJob, CompressionOptions, CompressionRun } from '../bridge'
import Page from '../shared/Page.vue'
import PageHeader from '../shared/PageHeader.vue'
import FeatureState from '../shared/FeatureState.vue'
import OutcomeList from '../media/OutcomeList.vue'
import { compressionRequest, compressionModes, compressionEfforts } from '../shared/compression'
import { confirm } from '../shared/confirm'
import { errorText } from '../shared/errors'
import { bytes } from '../shared/format'
import { t } from '../shared/i18n'
import type { MessageKey } from '../shared/i18n'
import { useLiveReload } from '../shared/liveReload'
import { usePreferences } from '../shared/preferences'

const prefs = usePreferences()
const available = ref(true)
const loading = ref(true)
const error = ref('')
const submitting = ref(false)
const inputs = shallowRef<CompressionInput[]>([])
const paths = shallowRef<Record<number, string[]>>({})
const options = ref<CompressionOptions>({ ...prefs.prefs.compression })
const directory = ref('')
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
const anyActive = computed(() => runs.value.some((item) => active(item.state)))
const ready = computed(() => jobs.value.filter((job) => job.state === CompressionState.CompressionReady))
const canAnalyse = computed(() => !!run.value && run.value.total > run.value.done + run.value.ready + run.value.skipped)
const canCancel = computed(
  () => !!run.value && run.value.total > run.value.done + run.value.skipped + run.value.cancelled,
)
const modes = compressionModes.map((value) => ({ value, label: t(`compression.mode.${value}`) }))
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
let poll: ReturnType<typeof setInterval> | undefined
const seenReady = new Set<number>()

function invalidatePlan() {
  generation++
  runID.value = 0
  offset.value = 0
  total.value = 0
  jobs.value = []
  selected.value = new Set()
  seenReady.clear()
}
watch([options, directory, inputs], invalidatePlan, { deep: true, flush: 'sync' })
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
  if (refreshPending) return
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
    for (const job of outcomes)
      if (job.state === CompressionState.CompressionReady && !seenReady.has(job.id)) {
        seenReady.add(job.id)
        next.add(job.id)
      }
    selected.value = next
  } catch (e) {
    if (mine === generation) error.value = errorText(e)
  } finally {
    refreshPending = false
    if (mine === generation) loading.value = false
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

async function plan() {
  const mine = generation
  await perform(async () => {
    if (compressionRequest.value?.selection) await engine.validateSelection(compressionRequest.value.selection)
    const result = await engine.planCompression(inputs.value, directory.value, options.value)
    if (mine === generation) {
      runID.value = result.id
      offset.value = 0
    }
  })
}

async function chooseDirectory() {
  await perform(async () => {
    const path = await engine.pickFolder()
    if (path) directory.value = path
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
  generation++
  runID.value = id
  offset.value = 0
  jobs.value = []
  selected.value = new Set()
  seenReady.clear()
  void refresh()
}
function pageBy(step: number) {
  generation++
  offset.value = Math.max(0, offset.value + step * 100)
  void refresh()
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
    if (!(await confirm(t('compression.title'), t('compression.confirm'), t('compression.commit')))) return
    if (runID.value !== id) return
    await engine.commitCompression(id, chosen)
  })
}
async function restore(job: CompressionJob) {
  const mine = generation
  await perform(async () => {
    const result = await engine.restoreCompression(job.runId, job.id, directory.value)
    if (mine !== generation) return
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
    available.value = await engine.compressionAvailable()
  } catch (e) {
    error.value = errorText(e)
  }
  await refresh()
  poll = setInterval(() => {
    void refresh()
  }, 500)
})
onBeforeUnmount(() => {
  generation++
  clearInterval(poll)
})
</script>

<template>
  <Page>
    <PageHeader :title="t('compression.title')" :subtitle="t('compression.hint')" />
    <UAlert v-if="error" color="error" :title="error" />
    <UAlert v-if="!available" color="warning" :title="t('compression.unavailable')" />
    <FeatureState v-if="loading" kind="loading" :title="t('compression.state.planned')" />
    <div v-else class="compression-workspace">
      <section v-if="inputs.length" :aria-label="t('compression.action')" class="compression-plan">
        <div class="compression-fields">
          <label
            >{{ t('compression.mode')
            }}<USelect
              :model-value="modes.find((item) => item.value === options.mode)?.value"
              :items="modes"
              :disabled="working || submitting"
              :aria-label="t('compression.mode')"
              @update:model-value="
                (value) => {
                  const mode = modes.find((item) => item.value === value)?.value
                  if (mode) options.mode = mode
                }
              "
          /></label>
          <label
            >{{ t('compression.effort')
            }}<USelect
              :model-value="efforts.find((item) => item.value === options.effort)?.value"
              :items="efforts"
              :disabled="working || submitting"
              :aria-label="t('compression.effort')"
              @update:model-value="
                (value) => {
                  const effort = efforts.find((item) => item.value === value)?.value
                  if (effort) options.effort = effort
                }
              "
          /></label>
          <label
            >{{ t('compression.minSavings')
            }}<UInput
              v-model.number="options.minSavings"
              type="number"
              :min="0"
              :max="90"
              :disabled="working || submitting"
              :aria-label="t('compression.minSavings')"
          /></label>
          <label
            >{{ t('compression.destination')
            }}<UInput
              v-model="directory"
              :placeholder="t('compression.beside')"
              :disabled="working || submitting"
              :aria-label="t('compression.destination')"
          /></label>
          <UButton variant="outline" :disabled="working || submitting" @click="chooseDirectory">{{
            t('files.choose')
          }}</UButton>
        </div>
        <p>{{ t('compression.metadata') }}</p>
        <p v-if="options.mode !== CompressionMode.CompressCompatible">{{ t('compression.compatibility') }}</p>
        <p v-if="options.mode === CompressionMode.CompressConvert">{{ t('compression.conversion') }}</p>
        <OutcomeList v-slot="{ item: input }" :items="inputs">
          <div :key="`${input.assetId}:${input.path}`" class="compression-source">
            <span>{{ input.path || t('compression.source') }} #{{ input.assetId }}</span>
            <UButton
              v-if="!paths[input.assetId]"
              variant="ghost"
              :disabled="submitting || working"
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
              :disabled="submitting || working"
              @update:model-value="
                (path) => {
                  if (typeof path === 'string') chooseCopy(inputs.indexOf(input), path)
                }
              "
            />
          </div>
        </OutcomeList>
        <UButton data-compression-plan :disabled="!available || working || submitting" @click="plan">{{
          t('compression.plan')
        }}</UButton>
      </section>
      <p v-else>{{ t('compression.empty') }}</p>

      <section
        v-if="runID"
        data-compression-results
        class="compression-results"
        :aria-label="t('compression.run', { n: runID })"
      >
        <h2>{{ t('compression.run', { n: runID }) }} · {{ run ? stateText(run.state) : '' }}</h2>
        <p v-if="run?.options.mode">{{ t(`compression.mode.${run.options.mode}`) }}</p>
        <p v-if="run" role="status">
          {{
            t('compression.progress', {
              n: run.processed,
              total: run.total,
              ready: run.ready,
              done: run.done,
              skipped: run.skipped,
              failed: run.failed,
              cancelled: run.cancelled,
            })
          }}
        </p>
        <p v-if="run">{{ t('compression.savings', { saved: bytes(run.savedBytes), added: bytes(run.addedBytes) }) }}</p>
        <OutcomeList v-slot="{ item: job }" :items="jobs">
          <div :key="job.id" class="compression-outcome" :data-compression-job="job.id">
            <UCheckbox
              v-if="job.state === CompressionState.CompressionReady"
              :model-value="selected.has(job.id)"
              :label="job.from"
              :disabled="working || submitting"
              @update:model-value="(value) => toggle(job.id, value)"
            />
            <p v-else>{{ job.from }}</p>
            <p>→ {{ job.to }}</p>
            <p v-if="job.copies > 1">{{ t('compression.copies', { n: job.copies }) }}</p>
            <p>
              {{ stateText(job.state)
              }}{{ job.reason ? `: ${t(reasons[job.reason] ?? 'compression.reason.codec')}` : '' }}
            </p>
            <p v-if="job.outputSize">
              {{
                t('compression.readySavings', {
                  before: bytes(job.size),
                  after: bytes(job.outputSize),
                  saved: bytes(Math.max(job.size - job.outputSize, 0)),
                })
              }}
            </p>
            <p v-if="job.verification">{{ t('compression.verified') }}</p>
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
            <UButton
              v-if="job.state === CompressionState.CompressionDone"
              variant="ghost"
              @click="perform(() => engine.revealCompression(job.runId, job.id))"
              >{{ t('compression.reveal') }}</UButton
            >
          </div>
        </OutcomeList>
        <div v-if="total > 100" class="compression-actions">
          <UButton variant="outline" :disabled="!offset || submitting" @click="pageBy(-1)">{{
            t('compression.previous')
          }}</UButton>
          <span>{{ t('compression.page', { first: offset + 1, last: Math.min(offset + 100, total), total }) }}</span>
          <UButton variant="outline" :disabled="offset + 100 >= total || submitting" @click="pageBy(1)">{{
            t('compression.next')
          }}</UButton>
        </div>
        <div class="compression-actions">
          <span v-if="selected.size">{{ t('compression.selected', { n: selected.size }) }}</span>
          <UButton
            v-if="!working && canAnalyse"
            data-compression-analyse
            :disabled="anyActive || submitting || !available"
            @click="perform(() => engine.analyseCompression(runID))"
            >{{
              run?.state === CompressionState.CompressionPlanned ? t('compression.analyse') : t('compression.resume')
            }}</UButton
          >
          <UButton
            v-if="selected.size || ready.length"
            data-compression-commit
            :disabled="anyActive || submitting || !selected.size"
            @click="commit"
            >{{ t('compression.commit') }}</UButton
          >
          <UButton
            v-if="run?.state === CompressionState.CompressionEncoding"
            variant="outline"
            :disabled="submitting"
            @click="perform(() => engine.pauseCompression(runID))"
            >{{ t('compression.pause') }}</UButton
          >
          <UButton
            v-if="canCancel"
            variant="outline"
            :disabled="submitting"
            @click="perform(() => engine.cancelCompression(runID))"
            >{{ t('compression.cancel') }}</UButton
          >
        </div>
      </section>

      <section v-if="runs.length" class="compression-history" :aria-label="t('compression.history')">
        <h2>{{ t('compression.history') }}</h2>
        <p v-for="item in runs" :key="item.id">
          <UButton variant="ghost" :disabled="submitting" @click="inspect(item.id)"
            >{{ t('compression.run', { n: item.id }) }} · {{ stateText(item.state) }}</UButton
          >
        </p>
      </section>
      <p class="text-muted">{{ t('compression.storage') }}</p>
      <p class="text-muted">{{ t('compression.localHistory') }}</p>
    </div>
  </Page>
</template>

<style scoped>
.compression-workspace {
  display: grid;
  gap: 1.5rem;
}
.compression-fields {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(14rem, 1fr));
  gap: 1rem;
  align-items: end;
}
.compression-fields label {
  display: grid;
  gap: 0.5rem;
}
.compression-plan,
.compression-results,
.compression-history {
  padding: calc(var(--lumen-gap) * 2);
  border: 1px solid var(--ui-border);
  border-radius: var(--ui-radius);
}
.compression-source,
.compression-outcome {
  padding: 0.75rem 0;
  border-bottom: 1px solid var(--ui-border);
  overflow-wrap: anywhere;
}
.compression-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
  margin-top: 1rem;
}
</style>
