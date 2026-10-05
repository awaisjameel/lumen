<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import UModal from '@nuxt/ui/components/Modal.vue'
import USelect from '@nuxt/ui/components/Select.vue'
import UCheckbox from '@nuxt/ui/components/Checkbox.vue'
import { engine, OrganiseRunState } from '../bridge'
import type { ExportJob, OrganisePlan, Selection } from '../bridge'
import { confirm } from '../shared/confirm'
import { errorText } from '../shared/errors'
import { t } from '../shared/i18n'
import { useLiveReload } from '../shared/liveReload'

const props = defineProps<{ ids: number[]; selection?: Selection | null; frame?: number }>()
const emit = defineEmits<{ changed: [] }>()
const open = defineModel<boolean>('open', { default: false })
const operation = ref(props.frame === undefined ? 'original' : 'jpeg')
const directory = ref('')
const name = ref('')
const quality = ref('92')
const side = ref('4096')
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
const options = computed(() =>
  props.frame === undefined
    ? [
        { value: 'original', label: t('files.original') },
        { value: 'jpeg', label: t('files.jpeg') },
        { value: 'png', label: t('files.png') },
        { value: 'move', label: t('files.move') },
        ...(props.ids.length === 1 ? [{ value: 'rename', label: t('files.rename') }] : []),
      ]
    : [
        { value: 'jpeg', label: t('files.jpeg') },
        { value: 'png', label: t('files.png') },
      ],
)
const completed = ref(false)
const page = ref(0)
const preview = computed(() => [
  ...(moves.value?.moves ?? []).map((m) => ({
    id: m.seq,
    from: m.from,
    to: m.to,
    state: m.state,
    error: m.error,
    sidecar: m.sidecar,
  })),
  ...exports.value.map((j) => ({ id: j.id, from: j.from, to: j.to, state: j.state, error: j.error, sidecar: '' })),
])
watch(inputKey, () => {
  plannedKey.value = ''
  if (!busy.value) {
    page.value = 0
    moves.value = null
    exports.value = []
    completed.value = false
    runID.value = 0
  }
})
watch(
  open,
  (value) => {
    if (value && props.frame !== undefined) operation.value = 'jpeg'
    void loadHistory()
  },
  { immediate: true },
)

async function chooseDirectory() {
  try {
    const path = await engine.pickFolder()
    if (path) directory.value = path
  } catch (e) {
    error.value = errorText(e)
  }
}
async function prepare() {
  if (busy.value) return
  page.value = 0
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
    if (operation.value === 'move' || operation.value === 'rename') {
      const result = await engine.planFileMoves(
        ids,
        operation.value === 'move' ? directory.value : '',
        operation.value === 'rename' ? name.value : '',
      )
      if (mine === generation && key === inputKey.value) moves.value = result
    } else {
      const result = await engine.planExport(ids, directory.value, {
        format: operation.value,
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
  if (busy.value || !key || key !== inputKey.value || props.ids.join(',') !== planIDs.value.join(',')) return
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
          clearTimeout(poll)
          exportAbort = undefined
          activeBatch.value = 0
          const jobs = await engine.exportJobs(batch)
          if (mine === generation) exports.value = jobs
        }
      }
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
onBeforeUnmount(() => {
  generation++
  historyRequest++
  exportAbort?.abort()
  clearTimeout(poll)
})

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
        <template v-if="operation === 'jpeg' || operation === 'png'">
          <label
            >{{ t('files.size')
            }}<UInput
              v-model="side"
              type="number"
              :min="256"
              :max="4096"
              :disabled="busy"
              :aria-label="t('files.size')"
          /></label>
          <label v-if="operation === 'jpeg'"
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
        <p v-if="operation === 'original'">{{ t('files.metadata') }}</p>
        <p v-if="operation === 'move' || operation === 'rename'">{{ t('files.moveHint') }}</p>
      </div>
      <div class="file-preview" role="status">
        <p v-if="preview.length > 100">
          {{ t('files.previewCount', { shown: Math.min((page + 1) * 100, preview.length), n: preview.length }) }}
        </p>
        <div v-for="row in preview.slice(page * 100, (page + 1) * 100)" :key="row.id">
          <p>{{ row.from }}</p>
          <p>→ {{ row.to }}</p>
          <p>{{ row.state }}{{ row.error ? `: ${row.error}` : '' }}</p>
          <p v-if="row.sidecar">{{ t('files.sidecar') }}: {{ row.from }}.xmp → {{ row.to }}.xmp ({{ row.sidecar }})</p>
        </div>
      </div>
      <div v-if="preview.length > 100" class="preview-pages">
        <UButton :disabled="page === 0" variant="outline" @click="page--">{{ t('files.previousPage') }}</UButton>
        <UButton :disabled="(page + 1) * 100 >= preview.length" variant="outline" @click="page++">{{
          t('files.nextPage')
        }}</UButton>
      </div>
      <details v-if="history.length">
        <summary>{{ t('files.history') }}</summary>
        <p v-for="job in history" :key="job.id">{{ job.to }}: {{ job.state }} {{ job.error }}</p>
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
.file-preview {
  max-height: 20rem;
  overflow-y: auto;
}
.file-preview > div {
  padding: 0.5rem 0;
  border-top: 1px solid var(--ui-border);
}
.file-preview p {
  overflow-wrap: anywhere;
  margin: 0.25rem 0;
  font-size: 0.8125rem;
}
</style>
