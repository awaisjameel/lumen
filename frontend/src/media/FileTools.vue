<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import UModal from '@nuxt/ui/components/Modal.vue'
import USelect from '@nuxt/ui/components/Select.vue'
import UCheckbox from '@nuxt/ui/components/Checkbox.vue'
import { engine, ExportFormat, OrganiseRunState, RENDER_SIDE, RENDER_SIDE_MIN } from '../bridge'
import type { ExportJob, ExportState, OrganiseMoveState, OrganisePlan, Selection, SidecarState } from '../bridge'
import { confirm } from '../shared/confirm'
import { errorText } from '../shared/errors'
import { reasonText } from '../shared/fileReasons'
import { t } from '../shared/i18n'
import { useLiveReload } from '../shared/liveReload'
import OutcomeList from './OutcomeList.vue'

type Operation = ExportFormat | 'move' | 'rename'
interface Choice {
  value: Operation
  label: string
}
const props = defineProps<{ ids: number[]; selection?: Selection | null; frame?: number }>()
const emit = defineEmits<{ changed: [] }>()
const open = defineModel<boolean>('open', { default: false })
/** A journalled state in words; the bindings' empty zero value is none. */
const stateText = (state: ExportState | OrganiseMoveState | SidecarState) => (state ? t(`files.state.${state}`) : '')
// A frame of a video can only be rendered; anything else starts as a plain copy.
const startOperation = (): Operation =>
  props.frame === undefined ? ExportFormat.ExportOriginal : ExportFormat.ExportJPEG
const operation = ref<Operation>(startOperation())
const directory = ref('')
const name = ref('')
const quality = ref('92')
const side = ref(String(RENDER_SIDE))
const adjust = ref(true)
const busy = ref(false)
const error = ref('')
const moves = ref<OrganisePlan | null>(null)
const exports = ref<ExportJob[]>([])
const planIDs = ref<number[]>([])
const plannedKey = ref('')
const inputKey = computed(() =>
  JSON.stringify({
    operation: operation.value,
    directory: directory.value,
    name: name.value,
    quality: quality.value,
    side: side.value,
    adjust: adjust.value,
    frame: props.frame,
    ids: props.ids,
    selection: props.selection,
  }),
)
const runID = ref(0)
const moveState = ref<OrganiseRunState>(OrganiseRunState.OrganisePlanned)
let poll: ReturnType<typeof setTimeout> | undefined
let generation = 0
let exportAbort: AbortController | undefined
const history = ref<ExportJob[]>([])
let historyRequest = 0
async function loadHistory() {
  const mine = ++historyRequest
  history.value = []
  if (!open.value) return
  try {
    const jobs = await engine.exportJobs()
    if (mine === historyRequest) history.value = jobs
  } catch (e) {
    if (mine === historyRequest) error.value = errorText(e)
  }
}
useLiveReload(loadHistory, (change) => change.assets, 1)
const activeBatch = ref(0)
async function cancelExport() {
  try {
    await engine.cancelExport(activeBatch.value)
  } catch (e) {
    error.value = errorText(e)
  }
}
const options = computed((): Choice[] => {
  const rendered: Choice[] = [
    { value: ExportFormat.ExportJPEG, label: t('files.jpeg') },
    { value: ExportFormat.ExportPNG, label: t('files.png') },
  ]
  if (props.frame !== undefined) return rendered
  return [
    { value: ExportFormat.ExportOriginal, label: t('files.original') },
    ...rendered,
    { value: 'move', label: t('files.move') },
    ...(props.ids.length === 1 ? [{ value: 'rename' as const, label: t('files.rename') }] : []),
  ]
})
const moving = computed(() => operation.value === 'move' || operation.value === 'rename')
const completed = ref(false)
const preview = computed(() => [
  ...(moves.value?.moves ?? []).map((m) => ({
    id: m.seq,
    from: m.from,
    to: m.to,
    state: m.state,
    error: m.error,
    sidecar: m.sidecar,
  })),
  ...exports.value.map((j) => ({
    id: j.id,
    from: j.from,
    to: j.to,
    state: j.state,
    error: j.error,
    sidecar: undefined,
  })),
])
watch(inputKey, () => {
  plannedKey.value = ''
  if (!busy.value) {
    moves.value = null
    exports.value = []
    completed.value = false
    runID.value = 0
  }
})
watch(
  open,
  (value) => {
    if (!value) invalidate()
    if (value && props.frame !== undefined) operation.value = startOperation()
    void loadHistory()
  },
  { immediate: true, flush: 'sync' },
)

async function chooseDirectory() {
  const mine = generation
  try {
    const path = await engine.pickFolder()
    if (path && mine === generation && open.value && !busy.value) directory.value = path
  } catch (e) {
    if (mine === generation) error.value = errorText(e)
  }
}
async function prepare() {
  if (busy.value || !open.value) return
  const chosen = operation.value
  busy.value = true
  completed.value = false
  runID.value = 0
  error.value = ''
  moves.value = null
  exports.value = []
  const mine = ++generation
  const key = inputKey.value
  const ids = [...props.ids]
  try {
    if (props.selection) await engine.validateSelection(props.selection)
    if (mine !== generation || key !== inputKey.value) return
    if (chosen === 'move' || chosen === 'rename') {
      const result = await engine.planFileMoves(
        ids,
        chosen === 'move' ? directory.value : '',
        chosen === 'rename' ? name.value : '',
      )
      if (mine === generation && key === inputKey.value) moves.value = result
    } else {
      const result = await engine.planExport(ids, directory.value, {
        format: chosen,
        side: Number(side.value),
        quality: Number(quality.value),
        adjust: adjust.value,
        frame: props.frame ?? -1,
      })
      if (mine === generation && key === inputKey.value) exports.value = result
    }
    if (mine === generation && key === inputKey.value) {
      planIDs.value = ids
      plannedKey.value = key
    }
  } catch (e) {
    if (mine === generation) error.value = errorText(e)
  } finally {
    if (mine === generation) busy.value = false
  }
}

async function updateRun(id: number, mine = generation) {
  try {
    const run = (await engine.organiseRuns()).find((r) => r.id === id)
    if (!run) return
    if (runID.value !== id || mine !== generation) return
    const outcomes = await engine.fileMoveOutcomes(id)
    if (runID.value !== id || mine !== generation) return
    if (moves.value) moves.value.moves = outcomes
    moveState.value = run.state
    if (run.state === OrganiseRunState.OrganiseRunning || run.state === OrganiseRunState.OrganiseUndoing)
      poll = setTimeout(() => {
        void updateRun(id, mine)
      }, 250)
    else {
      busy.value = false
      completed.value = true
      emit('changed')
    }
  } catch (e) {
    if (mine !== generation) return
    error.value = errorText(e)
    busy.value = false
  }
}
async function commit() {
  const key = plannedKey.value
  if (busy.value || !open.value || !key || key !== inputKey.value || props.ids.join(',') !== planIDs.value.join(','))
    return
  busy.value = true
  error.value = ''
  const mine = ++generation
  try {
    if (!(await confirm(t('files.title'), t('files.confirm'), t('files.apply')))) return
    if (mine !== generation || key !== inputKey.value) return
    if (props.selection) await engine.validateSelection(props.selection)
    if (mine !== generation || key !== inputKey.value) return
    if (moves.value) {
      runID.value = moves.value.runId
      await engine.startOrganise(runID.value)
      await updateRun(runID.value)
    } else {
      const batch = exports.value[0]?.batch
      if (batch) {
        exportAbort = new AbortController()
        activeBatch.value = batch
        pollExport(batch, mine)
        try {
          await engine.runExport(batch, exportAbort.signal)
        } finally {
          if (mine === generation) {
            clearTimeout(poll)
            exportAbort = undefined
            activeBatch.value = 0
            const jobs = await engine.exportJobs(batch)
            if (mine === generation) exports.value = jobs
          }
        }
      }
      if (mine !== generation) return
      busy.value = false
      completed.value = true
      emit('changed')
    }
  } catch (e) {
    if (mine !== generation) return
    error.value = errorText(e)
    busy.value = false
  } finally {
    if (mine === generation && !runID.value) busy.value = false
  }
}
async function undo() {
  if (busy.value) return
  const mine = generation
  const id = runID.value
  busy.value = true
  try {
    if (!(await confirm(t('files.title'), t('organise.undoText'), t('organise.undo')))) {
      if (mine === generation) busy.value = false
      return
    }
    if (mine !== generation || runID.value !== id) return
    await engine.undoOrganise(id)
    await updateRun(id, mine)
  } catch (e) {
    if (mine !== generation) return
    busy.value = false
    error.value = errorText(e)
  }
}
function invalidate() {
  generation++
  historyRequest++
  exportAbort?.abort()
  exportAbort = undefined
  clearTimeout(poll)
  activeBatch.value = 0
  busy.value = false
  plannedKey.value = ''
  runID.value = 0
  moves.value = null
  exports.value = []
  completed.value = false
}
onBeforeUnmount(invalidate)

function pollExport(batch: number, mine: number) {
  poll = setTimeout(() => {
    if (!busy.value || activeBatch.value !== batch || mine !== generation) return
    void engine
      .exportJobs(batch)
      .then((jobs) => {
        if (activeBatch.value !== batch || mine !== generation) return
        exports.value = jobs
        pollExport(batch, mine)
      })
      .catch((e: unknown) => {
        if (activeBatch.value === batch && mine === generation) error.value = errorText(e)
      })
  }, 250)
}
</script>
<template>
  <UModal
    v-model:open="open"
    :title="t('files.title')"
    :description="t('files.hint')"
    :dismissible="!busy"
    :close="!busy"
  >
    <template #body>
      <UAlert v-if="error" color="error" :title="error" />
      <div class="file-fields">
        <USelect v-model="operation" :items="options" :disabled="busy" :aria-label="t('files.operation')" />
        <template v-if="operation === 'rename'"
          ><UInput v-model="name" :disabled="busy" :aria-label="t('files.name')" :placeholder="t('files.name')"
        /></template>
        <template v-else
          ><UInput
            v-model="directory"
            :disabled="busy"
            :aria-label="t('files.destination')"
            :placeholder="t('files.destination')"
          /><UButton :disabled="busy" variant="outline" @click="chooseDirectory">{{
            t('files.choose')
          }}</UButton></template
        >
        <template v-if="!moving && operation !== ExportFormat.ExportOriginal">
          <label
            >{{ t('files.size')
            }}<UInput
              v-model="side"
              type="number"
              :min="RENDER_SIDE_MIN"
              :max="RENDER_SIDE"
              :disabled="busy"
              :aria-label="t('files.size')"
          /></label>
          <label v-if="operation === ExportFormat.ExportJPEG"
            >{{ t('files.quality')
            }}<UInput
              v-model="quality"
              type="number"
              :min="1"
              :max="100"
              :disabled="busy"
              :aria-label="t('files.quality')"
          /></label>
          <UCheckbox v-model="adjust" :disabled="busy" :label="t('files.adjust')" />
          <p>{{ t('files.stripped') }}</p>
        </template>
        <p v-if="operation === ExportFormat.ExportOriginal">{{ t('files.metadata') }}</p>
        <p v-if="moving">{{ t('files.moveHint') }}</p>
      </div>
      <OutcomeList v-slot="{ item: row }" :items="preview">
        <div :key="row.id" class="file-outcome">
          <p>{{ row.from }}</p>
          <p>→ {{ row.to }}</p>
          <p>{{ stateText(row.state) }}{{ row.error ? `: ${reasonText(row.error)}` : '' }}</p>
          <p v-if="row.sidecar">
            {{ t('files.sidecar') }}: {{ row.from }}.xmp → {{ row.to }}.xmp ({{ stateText(row.sidecar) }})
          </p>
        </div>
      </OutcomeList>
      <details v-if="history.length" class="file-history">
        <summary>{{ t('files.history') }}</summary>
        <p v-for="job in history" :key="job.id">
          {{ job.to }}: {{ stateText(job.state) }}{{ job.error ? `: ${reasonText(job.error)}` : '' }}
        </p>
      </details>
    </template>
    <template #footer>
      <UButton :disabled="busy" variant="outline" @click="prepare">{{ t('files.preview') }}</UButton>
      <UButton
        v-if="preview.length && plannedKey === inputKey && !runID && !completed"
        :disabled="busy"
        @click="commit"
        >{{ t('files.apply') }}</UButton
      >
      <UButton
        v-if="runID && moveState === OrganiseRunState.OrganiseDone"
        :disabled="busy"
        variant="outline"
        @click="undo"
        >{{ t('organise.undo') }}</UButton
      >
      <span v-if="busy" role="status">{{ t('files.working') }}</span>
      <UButton v-if="busy && activeBatch" variant="outline" @click="cancelExport">{{ t('files.cancel') }}</UButton>
    </template>
  </UModal>
</template>
<style scoped>
.file-fields {
  display: grid;
  gap: 0.75rem;
}
.file-outcome {
  padding: 0.5rem 0;
  border-top: 1px solid var(--ui-border);
}
.file-history p {
  overflow-wrap: anywhere;
  margin: 0.25rem 0;
  font-size: 0.8125rem;
}
</style>
