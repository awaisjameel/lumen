<script setup lang="ts">
import UButton from '@nuxt/ui/components/Button.vue'
import UPopover from '@nuxt/ui/components/Popover.vue'
import { computed, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { BlockKind, engine, ErrorCode, errorCode } from '../bridge'
import type { Face, Person } from '../bridge'
import { icons } from '../design/icons'
import { block, onBlocksChanged } from '../shared/block'
import { useCommands } from '../shared/commands'
import type { Command } from '../shared/commands'
import { errorText, logError, reportError } from '../shared/errors'
import FeatureState from '../shared/FeatureState.vue'
import Icon from '../shared/Icon.vue'
import IconButton from '../shared/IconButton.vue'
import { t } from '../shared/i18n'
import { useLiveReload } from '../shared/liveReload'
import MediaTile from '../shared/MediaTile.vue'
import { notify } from '../shared/notify'
import { shortcutsSuspended } from '../shared/overlay'
import Page from '../shared/Page.vue'
import PageHeader from '../shared/PageHeader.vue'
import SelectionDock from '../shared/SelectionDock.vue'
import ViewBar from '../shared/ViewBar.vue'
import FaceThumb from './FaceThumb.vue'
import PersonHeader from './PersonHeader.vue'
import PersonPicker from './PersonPicker.vue'
import { usePeople } from './store'

const route = useRoute()
const router = useRouter()
const people = usePeople()
const id = computed(() => Number(route.params.id))
const person = ref<Person | null>(null)
const faces = shallowRef<Face[]>([])
const selected = shallowRef(new Set<number>())
const busy = ref(false)
const loading = ref(false)
const error = ref('')
const picker = ref(false)
const limit = ref(24)
const pictureHint = ref(route.query.picture === '1')
let revision = 0
let loadRequest = 0
onBeforeUnmount(() => {
  revision++
  loadRequest++
})
const name = computed(() => person.value?.name || t('person.unnamed'))
const count = computed(() => selected.value.size)

async function load() {
  const request = ++loadRequest
  loading.value = true
  try {
    const [p, f] = await Promise.all([engine.person(id.value), engine.personFaces(id.value)])
    if (request !== loadRequest) return
    person.value = p
    faces.value = f
    const present = new Set(f.map((face) => face.id))
    selected.value = new Set([...selected.value].filter((face) => present.has(face)))
  } catch (e) {
    if (request !== loadRequest) return
    // A privacy change must remove a cached face grid and its identity.
    if (errorCode(e) === ErrorCode.NotFound) {
      person.value = null
      faces.value = []
      selected.value = new Set()
    }
    throw e
  } finally {
    if (request === loadRequest) loading.value = false
  }
}
async function refresh() {
  const requested = revision
  error.value = ''
  try {
    await load()
  } catch (e) {
    if (requested === revision) error.value = errorText(e)
  }
}
watch(
  id,
  () => {
    revision++
    person.value = null
    faces.value = []
    error.value = ''
    selected.value = new Set()
    picker.value = false
    limit.value = 24
    void refresh()
  },
  { immediate: true },
)
useLiveReload(load, (c) => c.people || c.assets || c.thumbs)
onBeforeUnmount(
  onBlocksChanged(async () => {
    person.value = null
    faces.value = []
    selected.value = new Set()
    await refresh()
  }),
)

function toggle(f: Face) {
  if (busy.value) return
  const s = new Set(selected.value)
  if (!s.delete(f.id)) s.add(f.id)
  selected.value = s
}
function clear() {
  if (!busy.value) selected.value = new Set()
}
function selectAll() {
  if (!busy.value)
    selected.value = count.value === faces.value.length ? new Set() : new Set(faces.value.map((f) => f.id))
}
function escape(e: KeyboardEvent) {
  if (e.key === 'Escape' && !shortcutsSuspended() && !picker.value) clear()
}
onMounted(() => window.addEventListener('keydown', escape))
onBeforeUnmount(() => window.removeEventListener('keydown', escape))

/** Corrections are atomic in the engine; a failed edit keeps the selection. */
async function act(fn: (ids: number[], before: Person) => Promise<void>) {
  if (!count.value || busy.value || !person.value) return
  busy.value = true
  const requested = revision
  const ids = [...selected.value]
  const before = person.value
  error.value = ''
  try {
    await fn(ids, before)
    void people.load().catch(logError)
    if (requested !== revision) return
    selected.value = new Set()
    picker.value = false
    await load().catch(async (e: unknown) => {
      if (requested !== revision) return
      if (errorCode(e) !== ErrorCode.NotFound) throw e
      await router.replace({ name: 'people' })
    })
  } catch (e) {
    if (requested === revision) error.value = errorText(e)
  } finally {
    busy.value = false
  }
}
const reject = () =>
  act(async (ids, before) => {
    await engine.rejectFaces(before.id, ids)
    notify({ id: 'faces.corrected', kind: 'success', title: t('faces.rejected', { n: ids.length }) })
  })
const moveTo = (to: string) =>
  act(async (ids) => {
    await engine.nameFaces(ids, to)
    notify({ id: 'faces.corrected', kind: 'success', title: t('faces.moved', { n: ids.length, name: to }) })
  })
const useAsCover = () =>
  count.value === 1 &&
  act(async ([face], before) => {
    if (face === undefined) return
    await engine.setPersonCover(before.id, face)
    notify({
      id: 'faces.picture',
      kind: 'success',
      title: t('faces.pictureSaved'),
      duration: 8000,
      actions: before.cover.id
        ? [
            {
              label: t('blocked.undo'),
              run: () =>
                void engine
                  .setPersonCover(before.id, before.cover.id)
                  .then(() => people.load())
                  .then(refresh)
                  .catch(reportError),
            },
          ]
        : undefined,
    })
  })
async function blockFace(face: Face | undefined) {
  if (!face || busy.value) return
  const requested = revision
  if (await block(BlockKind.BlockFace, face.id, t('faces.face', { n: faces.value.indexOf(face) + 1 }), false)) {
    if (requested !== revision) return
    person.value = null
    faces.value = []
    selected.value = new Set()
    await refresh()
  }
}
const rejectCommand: Command = {
  id: 'faces.reject',
  group: 'media',
  title: () => t('faces.notThem', { name: name.value }),
  icon: 'user-remove',
  available: () => count.value > 0,
  enabled: () => !busy.value,
  reason: () => t('faces.saving'),
  run: reject,
}
const coverCommand: Command = {
  id: 'faces.cover',
  group: 'media',
  title: () => t('faces.cover'),
  icon: 'face',
  available: () => faces.value.length > 0,
  enabled: () => count.value === 1 && !busy.value,
  reason: () => (busy.value ? t('faces.saving') : t('faces.coverReason')),
  run: useAsCover,
}
const blockCommand: Command = {
  id: 'faces.block',
  group: 'media',
  title: () => t('faces.block'),
  icon: 'blocked',
  available: () => count.value > 0,
  enabled: () => count.value === 1 && !busy.value,
  reason: () => (busy.value ? t('faces.saving') : t('faces.blockReason')),
  run: () => blockFace(faces.value.find((f) => selected.value.has(f.id))),
}
useCommands([
  rejectCommand,
  coverCommand,
  blockCommand,
  {
    id: 'faces.clear',
    group: 'view',
    title: () => t('faces.clear'),
    icon: 'close',
    available: () => count.value > 0,
    enabled: () => !busy.value,
    run: clear,
  },
  {
    id: 'faces.selectAll',
    group: 'view',
    title: () => t('faces.selectAll'),
    icon: 'check',
    available: () => faces.value.length > 0,
    enabled: () => !busy.value,
    run: selectAll,
  },
])
</script>

<template>
  <Page>
    <template #header>
      <PageHeader
        :title="t('faces.title', { name })"
        :subtitle="person ? t('faces.count', { n: faces.length }) : t('faces.loading')"
        :back="{ to: { name: 'person', params: { id } }, label: name }"
      >
        <IconButton
          :icon="icons.refresh"
          :label="t('person.refresh')"
          color="neutral"
          variant="ghost"
          :disabled="loading || busy"
          @click="refresh"
        />
      </PageHeader>
    </template>
    <PersonHeader v-if="person" :id="id" :face-count="faces.length" reviewing class="face-hero" />
    <ViewBar class="faces-viewbar" :label="t('faces.title', { name })"
      ><p class="text-sm text-muted">{{ t('faces.hint', { name }) }}</p>
      <template #end
        ><UButton
          v-if="faces.length"
          color="neutral"
          variant="ghost"
          :disabled="busy"
          :aria-pressed="count === faces.length"
          @click="selectAll"
          >{{ count === faces.length ? t('faces.clear') : t('faces.selectAll') }}</UButton
        ></template
      ></ViewBar
    >
    <FeatureState v-if="error" kind="error" :title="error"
      ><UButton color="primary" variant="solid" :disabled="busy || loading" @click="refresh">{{
        t('common.retry')
      }}</UButton></FeatureState
    >
    <section
      v-if="loading && !person"
      role="status"
      :aria-label="t('faces.loading')"
      aria-busy="true"
      class="face-grid"
    >
      <span v-for="n in 24" :key="n" class="lumen-skeleton face-skeleton" />
    </section>
    <div v-if="pictureHint" class="picture-hint">
      <Icon name="face" :size="20" /><span>{{ t('faces.pictureHint') }}</span
      ><IconButton
        :icon="icons.close"
        :label="t('common.close')"
        color="neutral"
        variant="ghost"
        @click="pictureHint = false"
      />
    </div>
    <div class="face-grid">
      <div v-for="(f, i) in faces.slice(0, limit)" :key="f.id" class="face-cell" :data-face="f.id">
        <MediaTile
          :id="f.assetId"
          :thumb="f.thumb"
          :selected="selected.has(f.id)"
          :selecting="count > 0"
          checkable
          @check="toggle(f)"
        >
          <template #media
            ><button
              class="face-tile"
              :disabled="busy"
              :title="f.byUser ? t('faces.byYou') : undefined"
              :aria-label="f.byUser ? t('faces.faceByYou', { n: i + 1 }) : t('faces.face', { n: i + 1 })"
              :aria-pressed="selected.has(f.id)"
              @click="toggle(f)"
            >
              <FaceThumb :face="f" :size="160" fill /></button
          ></template>
          <span v-if="f.byUser" class="placed">{{ t('faces.byYou') }}</span>
          <template #actions
            ><IconButton
              :icon="icons.blocked"
              :label="t('faces.blockFace', { n: i + 1 })"
              :tip="t('faces.block')"
              :disabled="busy"
              @click="blockFace(f)"
          /></template>
        </MediaTile>
      </div>
    </div>
    <UButton v-if="faces.length > limit" class="mt-6" color="neutral" variant="outline" @click="limit += 48">{{
      t('people.more', { n: Math.min(48, faces.length - limit) })
    }}</UButton>
    <FeatureState
      v-if="person && !faces.length && !error"
      kind="empty"
      icon="face"
      :title="t('faces.none')"
      :text="t('faces.noneHint')"
      ><UButton color="primary" variant="solid" to="/people">{{ t('person.back') }}</UButton></FeatureState
    >
    <p v-if="person" class="kept"><Icon name="info" :size="16" />{{ t('faces.kept') }}</p>
    <template #dock>
      <SelectionDock class="faces-dock" :count="count" :summary="t('faces.selected', { n: count })" @clear="clear">
        <UButton
          color="primary"
          variant="solid"
          data-action="reject"
          :icon="icons['user-remove']"
          :disabled="busy"
          :loading="busy"
          @click="reject"
          >{{ t('faces.notThem', { name }) }}</UButton
        >
        <UPopover
          v-model:open="picker"
          :content="{ side: 'top', align: 'end' }"
          :ui="{ content: 'p-3 w-80 max-w-[calc(100vw-2rem)] max-h-[calc(100vh-8rem)] overflow-y-auto' }"
        >
          <UButton color="neutral" variant="ghost" :icon="icons.people" :disabled="busy" :aria-label="t('faces.moveTo')"
            ><span class="dock-label">{{ t('faces.moveTo') }}</span></UButton
          >
          <template #content
            ><PersonPicker
              :placeholder="t('picker.find')"
              :exclude="id"
              :busy="busy"
              :error="error"
              managed
              @pick="moveTo"
              @cancel="picker = false"
          /></template>
        </UPopover>
        <UButton
          color="neutral"
          variant="ghost"
          :icon="icons.face"
          :disabled="count !== 1 || busy"
          :aria-label="t('faces.cover')"
          :title="count === 1 ? t('faces.cover') : t('faces.coverReason')"
          @click="useAsCover"
          ><span class="dock-label">{{ t('faces.cover') }}</span></UButton
        >
        <IconButton
          :icon="icons.blocked"
          :label="t('faces.block')"
          :tip="count === 1 ? t('faces.block') : t('faces.blockReason')"
          color="neutral"
          variant="ghost"
          :disabled="count !== 1 || busy"
          @click="blockFace(faces.find((f) => selected.has(f.id)))"
        />
      </SelectionDock>
    </template>
  </Page>
</template>

<style scoped>
.faces-dock {
  flex-wrap: wrap;
  justify-content: center;
  overflow: visible;
}
.faces-dock :deep(button) {
  max-width: 100%;
}
.faces-viewbar {
  margin-inline: calc(-1 * var(--lumen-page-x));
  margin-block: 0.5rem;
}
.face-hero {
  margin-inline: calc(-1 * var(--lumen-page-x));
  margin-bottom: 1.5rem;
}
.face-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(7rem, 1fr));
  gap: 0.75rem;
}
.face-cell,
.face-skeleton {
  position: relative;
  aspect-ratio: 1;
}
.face-tile {
  display: block;
  width: 100%;
  height: 100%;
  padding: 0;
  border: 0;
  background: none;
  cursor: pointer;
  transition: transform var(--lumen-d-base) var(--lumen-ease-out);
}
.face-tile:active {
  transform: scale(0.96);
}
.face-cell :deep(.shade) {
  pointer-events: none;
}
.face-cell :deep(.selected) .face-tile {
  transform: scale(0.86);
  border-radius: var(--lumen-r-sm);
  overflow: hidden;
}
.placed {
  position: absolute;
  left: 0.25rem;
  bottom: 0.25rem;
  max-width: calc(100% - 0.5rem);
  padding: 0.25rem;
  border-radius: var(--lumen-r-xs);
  background: var(--lumen-bg-overlay);
  color: var(--ui-text-toned);
  font-size: 0.75rem;
  pointer-events: none;
}
.kept {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  margin-top: 1.5rem;
  color: var(--ui-text-dimmed);
  font-size: 0.75rem;
}
.picture-hint {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  margin-block: 1rem;
  padding: 0.75rem;
  border: 1px solid var(--ui-border);
  border-radius: var(--lumen-r-md);
  background: var(--lumen-primary-soft);
  font-size: 0.8125rem;
}
.picture-hint > span {
  flex: 1;
}
@container (max-width: 56.25rem) {
  .dock-label {
    display: none;
  }
}
</style>
