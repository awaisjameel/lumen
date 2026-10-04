<script setup lang="ts">
// Reviewing who someone is: all their faces, least alike first, so any that
// aren't them stand out. Selected faces can be taken out ("not them"), moved
// to someone else or a new person (splitting), or used as their picture.
import UButton from '@nuxt/ui/components/Button.vue'
import { useCommands } from '../shared/commands'
import FeatureState from '../shared/FeatureState.vue'
import MediaTile from '../shared/MediaTile.vue'
import Page from '../shared/Page.vue'
import PageHeader from '../shared/PageHeader.vue'
import SelectionBar from '../shared/SelectionBar.vue'
import { computed, onBeforeUnmount, ref, shallowRef, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { engine, ErrorCode, errorCode } from '../bridge'
import type { Face, Person } from '../bridge'
import { errorText, logError } from '../shared/errors'
import { t } from '../shared/i18n'
import Icon from '../shared/Icon.vue'
import { useLiveReload } from '../shared/liveReload'
import FaceThumb from './FaceThumb.vue'
import PersonPicker from './PersonPicker.vue'
import { usePeople } from './store'

const route = useRoute()
const router = useRouter()
const people = usePeople()

const id = computed(() => Number(route.params.id))
const person = ref<Person | null>(null)
// Someone can have thousands of faces: the list is replaced, never changed
// in place, so it needn't be deeply reactive; nor is the selection.
const faces = shallowRef<Face[]>([])
const selected = shallowRef(new Set<number>())
const busy = ref(false)
const error = ref('')
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
  // A newer read answers for this one, success or failure.
  const got = await Promise.all([engine.person(id.value), engine.personFaces(id.value)]).catch((e: unknown) => {
    if (request === loadRequest) throw e
  })
  if (!got || request !== loadRequest) return
  const [p, f] = got
  person.value = p
  faces.value = f
  const present = new Set(f.map((x) => x.id))
  selected.value = new Set([...selected.value].filter((x) => present.has(x)))
}
watch(
  id,
  () => {
    const requested = ++revision
    person.value = null
    faces.value = []
    error.value = ''
    selected.value = new Set()
    load().catch((e: unknown) => {
      if (requested === revision) error.value = errorText(e)
    })
  },
  { immediate: true },
)
// Faces join and leave while the AI groups people; the selection keeps.
useLiveReload(load, (c) => c.people || c.assets || c.thumbs)

function toggle(f: Face) {
  const s = new Set(selected.value)
  if (!s.delete(f.id)) s.add(f.id)
  selected.value = s
}

/** Run an edit on the selection, then reload (or leave if they're gone). */
async function act(fn: (ids: number[]) => Promise<unknown>) {
  if (!count.value || busy.value) return
  busy.value = true
  const requested = revision
  error.value = ''
  try {
    await fn([...selected.value])
    void people.load().catch(logError)
    if (requested !== revision) return
    selected.value = new Set()
    await load().catch(async (e: unknown) => {
      if (requested !== revision) return
      if (errorCode(e) !== ErrorCode.NotFound) throw e
      await router.replace({ name: 'people' }) // nothing left of them
    })
  } catch (e) {
    if (requested === revision) error.value = errorText(e)
  } finally {
    busy.value = false
  }
}

const reject = () => act((ids) => engine.rejectFaces(id.value, ids))
const moveTo = (to: string) => act((ids) => engine.nameFaces(ids, to))
const useAsCover = () => act(async ([face]) => face !== undefined && engine.setPersonCover(id.value, face))
useCommands([
  {
    id: 'faces.reject',
    group: 'media',
    title: () => t('faces.notThem', { name: name.value }),
    icon: 'user-remove',
    available: () => count.value > 0,
    enabled: () => !busy.value,
    run: reject,
  },
  {
    id: 'faces.cover',
    group: 'media',
    title: () => t('faces.cover'),
    icon: 'face',
    available: () => count.value === 1,
    enabled: () => !busy.value,
    run: useAsCover,
  },
  {
    id: 'faces.clear',
    group: 'view',
    title: () => t('faces.clear'),
    icon: 'close',
    available: () => count.value > 0,
    run: () => {
      selected.value = new Set()
    },
  },
  {
    id: 'faces.selectAll',
    group: 'view',
    title: () => t('faces.selectAll'),
    icon: 'check',
    available: () => faces.value.length > 0,
    run: () => {
      selected.value = new Set(faces.value.map((f) => f.id))
    },
  },
])
</script>

<template>
  <Page>
    <template #header>
      <PageHeader
        :title="t('faces.title', { name })"
        :back="{ to: { name: 'person', params: { id } }, label: t('faces.back') }"
      />
    </template>
    <template #toolbar>
      <SelectionBar :summary="count ? t('faces.selected', { n: count }) : t('faces.hint', { name })">
        <template v-if="count">
          <UButton data-action="reject" :disabled="busy" :loading="busy" @click="reject">{{
            t('faces.notThem', { name })
          }}</UButton>
          <PersonPicker class="w-52 max-w-full" :placeholder="t('faces.moveTo')" :exclude="id" @pick="moveTo" />
          <UButton v-if="count === 1" :disabled="busy" @click="useAsCover">{{ t('faces.cover') }}</UButton>
          <UButton variant="ghost" @click="selected = new Set()">{{ t('faces.clear') }}</UButton>
        </template>
        <UButton v-else-if="faces.length" variant="ghost" @click="selected = new Set(faces.map((f) => f.id))">{{
          t('faces.selectAll')
        }}</UButton>
      </SelectionBar>
    </template>
    <FeatureState v-if="error" kind="error" :title="error" />
    <FeatureState v-else-if="!person" kind="loading" :title="t('faces.loading')" />
    <div class="face-grid">
      <!-- Memoisation keeps a selection change from rebuilding every face. -->
      <button
        v-for="(f, i) in faces"
        :key="f.id"
        v-memo="[f, i, selected.has(f.id)]"
        class="face-tile"
        :title="f.byUser ? t('faces.byYou') : undefined"
        :aria-label="f.byUser ? t('faces.faceByYou', { n: i + 1 }) : t('faces.face', { n: i + 1 })"
        :aria-pressed="selected.has(f.id)"
        @click="toggle(f)"
      >
        <MediaTile :id="f.assetId" :thumb="f.thumb" :selected="selected.has(f.id)">
          <template #media>
            <FaceThumb :face="f" :size="104" />
          </template>
          <span v-if="selected.has(f.id)" class="tick">
            <Icon name="check" :size="14" />
          </span>
        </MediaTile>
      </button>
    </div>
    <FeatureState v-if="person && !faces.length && !error" kind="empty" icon="face" :title="t('faces.none')" />
  </Page>
</template>

<style scoped>
.face-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, 104px);
  gap: 0.5rem;
}

.face-tile {
  position: relative;
  content-visibility: auto;
  contain-intrinsic-size: 104px 104px;
  width: 104px;
  height: 104px;
  padding: 0;
  border: 0;
  border-radius: var(--ui-radius);
  background: none;
  cursor: pointer;
}

.tick {
  position: absolute;
  top: 6px;
  right: 6px;
  display: grid;
  place-items: center;
  width: 22px;
  height: 22px;
  border-radius: 50%;
  background: var(--ui-bg);
  color: var(--ui-primary);
  border: 1px solid var(--ui-primary);
}
</style>
