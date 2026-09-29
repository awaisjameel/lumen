<script setup lang="ts">
// The top of a person's page: their face, their name (click to change it),
// how many photos, and what can be done about them.
import { nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { engine, MAX_NAME_LEN } from '../bridge'
import type { Person } from '../bridge'
import { errorText, logError } from '../shared/errors'
import { t } from '../shared/i18n'
import Icon from '../shared/Icon.vue'
import { useLiveReload } from '../shared/liveReload'
import FaceThumb from './FaceThumb.vue'
import { namePerson } from './naming'
import { usePeople } from './store'

const props = defineProps<{ id: number }>()
const router = useRouter()
const people = usePeople()

const person = ref<Person | null>(null)
const editing = ref(false)
const draft = ref('')
const input = ref<HTMLInputElement | null>(null)
const error = ref('')
let revision = 0
let loadRequest = 0
onBeforeUnmount(() => {
  revision++
  loadRequest++
})

async function load() {
  const request = ++loadRequest
  try {
    const p = await engine.person(props.id)
    if (request === loadRequest) {
      person.value = p
      error.value = ''
    }
  } catch (e) {
    if (request === loadRequest) error.value = errorText(e)
  }
}
watch(
  () => props.id,
  () => {
    revision++
    person.value = null
    editing.value = false
    error.value = ''
    void load()
  },
  { immediate: true },
)
useLiveReload(load, (c) => c.people || c.assets || c.thumbs)

async function edit() {
  draft.value = person.value?.name ?? ''
  editing.value = true
  await nextTick()
  input.value?.select()
}

async function save() {
  if (!editing.value) return
  editing.value = false
  const name = draft.value.trim()
  if (name === (person.value?.name ?? '')) return
  error.value = ''
  const requested = revision
  const id = props.id
  try {
    const shown = await namePerson(id, name)
    void people.load().catch(logError)
    if (requested !== revision) return
    if (shown !== null && shown !== props.id) await router.replace({ name: 'person', params: { id: shown } })
    else await load()
  } catch (e) {
    if (requested === revision) error.value = errorText(e)
  }
}

async function toggleHidden() {
  if (!person.value) return
  const requested = revision
  try {
    await engine.setPersonHidden(props.id, !person.value.hidden)
    if (requested === revision) await load()
    void people.load().catch(logError)
  } catch (e) {
    if (requested === revision) error.value = errorText(e)
  }
}
</script>

<template>
  <div v-if="person" class="person">
    <FaceThumb :face="person.cover" :size="40" round />
    <div class="text">
      <input
        v-if="editing"
        ref="input"
        v-model="draft"
        class="name-input"
        :aria-label="t('person.name')"
        :placeholder="t('person.namePlaceholder')"
        :maxlength="MAX_NAME_LEN"
        @keydown.enter="save"
        @keydown.esc.stop="editing = false"
        @blur="save"
      />
      <button v-else class="name" :title="t('person.rename')" @click="edit">
        <span class="name-text">{{ person.name || t('person.addName') }}</span>
        <Icon name="pencil" :size="14" />
      </button>
      <span class="count">{{ error || t('status.items', { n: person.count }) }}</span>
    </div>
    <RouterLink :to="{ name: 'personFaces', params: { id } }" class="btn" :title="t('person.review')">
      <Icon name="user" :size="16" /><span class="label">{{ t('person.review') }}</span>
    </RouterLink>
    <button
      class="icon-btn"
      :title="person.hidden ? t('people.unhide') : t('people.hide')"
      :aria-label="person.hidden ? t('people.unhide') : t('people.hide')"
      @click="toggleHidden"
    >
      <Icon :name="person.hidden ? 'eye' : 'eye-off'" :size="18" />
    </button>
  </div>
  <p v-else-if="error" class="error" role="alert">{{ error }}</p>
</template>

<style scoped>
.person {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-shrink: 1;
  min-width: 0;
}
.text {
  display: flex;
  flex-direction: column;
  min-width: 0;
}
.name {
  display: flex;
  align-items: center;
  gap: 6px;
  max-width: 280px;
  min-width: 0;
  padding: 0;
  border: 0;
  background: none;
  font: 600 20px/1.1 var(--font-display);
  text-align: left;
  cursor: text;
}
.name-text {
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}
.name :deep(svg) {
  flex-shrink: 0;
  color: var(--text-3);
  opacity: 0;
}
.name:hover :deep(svg),
.name:focus-visible :deep(svg) {
  opacity: 1;
}
.name-input {
  width: 240px;
  height: 28px;
  padding: 0 8px;
  border: 1px solid var(--accent);
  border-radius: var(--radius-s);
  background: var(--surface);
  font: 600 16px var(--font-display);
  outline: none;
}
.count {
  color: var(--text-3);
  font-size: 12px;
  white-space: nowrap;
}
.btn {
  flex-shrink: 0;
  text-decoration: none;
  color: var(--text);
}
@media (max-width: 1200px) {
  .btn .label {
    display: none;
  }
}
</style>
