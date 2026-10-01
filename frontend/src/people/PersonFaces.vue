<script setup lang="ts">
// Reviewing who someone is: all their faces, least alike first, so any that
// aren't them stand out. Selected faces can be taken out ("not them"), moved
// to someone else or a new person (splitting), or used as their picture.
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
  const [p, f] = await Promise.all([engine.person(id.value), engine.personFaces(id.value)])
  if (request !== loadRequest) return
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
</script>

<template>
  <div class="page">
    <header class="topbar back">
      <RouterLink
        :to="{ name: 'person', params: { id } }"
        class="icon-btn"
        :title="t('faces.back')"
        :aria-label="t('faces.back')"
      >
        <Icon name="chevron-left" />
      </RouterLink>
      <h1>{{ t('faces.title', { name }) }}</h1>
    </header>

    <div class="bar">
      <template v-if="count">
        <span class="sel">{{ t('faces.selected', { n: count }) }}</span>
        <button class="btn" :disabled="busy" @click="reject">{{ t('faces.notThem', { name }) }}</button>
        <PersonPicker class="move" :placeholder="t('faces.moveTo')" :exclude="id" @pick="moveTo" />
        <button v-if="count === 1" class="btn" :disabled="busy" @click="useAsCover">{{ t('faces.cover') }}</button>
        <button class="btn btn-subtle" @click="selected = new Set()">{{ t('faces.clear') }}</button>
      </template>
      <template v-else>
        <span class="muted">{{ t('faces.hint', { name }) }}</span>
        <button v-if="faces.length" class="btn btn-subtle" @click="selected = new Set(faces.map((f) => f.id))">
          {{ t('faces.selectAll') }}
        </button>
      </template>
    </div>
    <p v-if="error" class="page-error bar-error" role="alert">{{ error }}</p>

    <div class="page-body">
      <div class="grid">
        <!-- A click changes one tile: v-memo leaves the others' vnodes as they were. -->
        <button
          v-for="(f, i) in faces"
          :key="f.id"
          v-memo="[f, i, selected.has(f.id)]"
          class="tile"
          :class="{ on: selected.has(f.id) }"
          :title="f.byUser ? t('faces.byYou') : undefined"
          :aria-label="f.byUser ? t('faces.faceByYou', { n: i + 1 }) : t('faces.face', { n: i + 1 })"
          :aria-pressed="selected.has(f.id)"
          @click="toggle(f)"
        >
          <FaceThumb :face="f" :size="104" />
          <span v-if="selected.has(f.id)" class="tick"><Icon name="check" :size="14" /></span>
        </button>
      </div>
      <p v-if="person && !faces.length" class="muted">{{ t('faces.none') }}</p>
    </div>
  </div>
</template>

<style scoped>
.back {
  gap: 8px;
  padding: 0 20px 0 12px;
}
.icon-btn {
  color: var(--text);
}
.bar {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  min-height: 40px;
  padding: 0 20px 8px;
}
.sel {
  font-weight: 600;
}
.move {
  width: 200px;
}
.bar-error {
  margin: 0 20px 8px;
}
.grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, 104px);
  gap: 8px;
}
.tile {
  position: relative;
  /* Someone can have thousands of faces: tiles off screen are not laid out. */
  content-visibility: auto;
  contain-intrinsic-size: 104px 104px;
  padding: 0;
  border: 0;
  border-radius: var(--radius-m);
  background: none;
  cursor: pointer;
}
.tile.on {
  outline: 3px solid var(--accent);
  outline-offset: 1px;
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
  background: var(--accent);
  color: var(--accent-text);
}
</style>
