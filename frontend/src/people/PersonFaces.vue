<script setup lang="ts">
// Reviewing who someone is: all their faces, least alike first, so any that
// aren't them stand out. Selected faces can be taken out ("not them"), moved
// to someone else or a new person (splitting), or used as their picture.
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { engine } from '../bridge'
import type { Face, Person } from '../bridge'
import { errorText, logError } from '../shared/errors'
import { t } from '../shared/i18n'
import Icon from '../shared/Icon.vue'
import FaceThumb from './FaceThumb.vue'
import PersonPicker from './PersonPicker.vue'
import { usePeople } from './store'

const route = useRoute()
const router = useRouter()
const people = usePeople()

const id = computed(() => Number(route.params.id))
const person = ref<Person | null>(null)
const faces = ref<Face[]>([])
const selected = ref(new Set<number>())
const busy = ref(false)
const error = ref('')

const name = computed(() => person.value?.name || t('person.unnamed'))
const count = computed(() => selected.value.size)

async function load() {
  const [p, f] = await Promise.all([engine.person(id.value), engine.personFaces(id.value)])
  person.value = p
  faces.value = f
  selected.value = new Set([...selected.value].filter((x) => f.some((y) => y.id === x)))
}
watch(id, () => {
  selected.value = new Set()
  load().catch((e: unknown) => (error.value = errorText(e)))
}, { immediate: true })

function toggle(f: Face) {
  const s = new Set(selected.value)
  if (!s.delete(f.id)) s.add(f.id)
  selected.value = s
}

/** Run an edit on the selection, then reload (or leave if they're gone). */
async function act(fn: (ids: number[]) => Promise<unknown>) {
  if (!count.value || busy.value) return
  busy.value = true
  error.value = ''
  try {
    await fn([...selected.value])
    selected.value = new Set()
    void people.load().catch(logError)
    await load().catch(async () => router.replace({ name: 'people' })) // nothing left of them
  } catch (e) {
    error.value = errorText(e)
  } finally {
    busy.value = false
  }
}

const reject = () => act((ids) => engine.rejectFaces(id.value, ids))
const moveTo = (to: string) => act((ids) => engine.nameFaces(ids, to))
const useAsCover = () => act(async ([face]) => engine.setPersonCover(id.value, face))
</script>

<template>
  <div class="faces-view">
    <header class="topbar">
      <RouterLink :to="{ name: 'person', params: { id } }" class="icon-btn" :title="t('faces.back')">
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
        <button class="btn subtle" @click="selected = new Set()">{{ t('faces.clear') }}</button>
      </template>
      <template v-else>
        <span class="muted">{{ t('faces.hint', { name }) }}</span>
        <button v-if="faces.length" class="btn subtle" @click="selected = new Set(faces.map((f) => f.id))">
          {{ t('faces.selectAll') }}
        </button>
      </template>
    </div>
    <p v-if="error" class="error" role="alert">{{ error }}</p>

    <div class="body">
      <div class="grid">
        <button
          v-for="f in faces"
          :key="f.id"
          class="tile"
          :class="{ on: selected.has(f.id) }"
          :title="f.byUser ? t('faces.byYou') : undefined"
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
.faces-view {
  display: flex;
  flex-direction: column;
  height: 100%;
}
.topbar {
  display: flex;
  align-items: center;
  gap: 8px;
  height: var(--topbar-h);
  padding: 0 20px 0 12px;
  flex-shrink: 0;
}
.topbar h1 {
  margin: 0;
  font: 600 20px/1 var(--font-display);
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
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
.error {
  margin: 0 20px 8px;
  color: var(--danger);
}
.body {
  flex: 1;
  overflow-y: auto;
  padding: 0 20px 24px;
}
.grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, 104px);
  gap: 8px;
}
.tile {
  position: relative;
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
