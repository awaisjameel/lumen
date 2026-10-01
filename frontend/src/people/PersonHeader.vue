<script setup lang="ts">
// The top of a person's page: their face, their name (click to change it),
// how many photos, and what can be done about them.
import { onBeforeUnmount, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { BlockKind, engine, MAX_NAME_LEN } from '../bridge'
import type { Person } from '../bridge'
import { errorText, logError } from '../shared/errors'
import { block } from '../shared/block'
import { t } from '../shared/i18n'
import Icon from '../shared/Icon.vue'
import { useLiveReload } from '../shared/liveReload'
import { useRename } from '../shared/rename'
import { nameTooLong, trimName } from '../shared/names'
import FaceThumb from './FaceThumb.vue'
import { namePerson } from './naming'
import { usePeople } from './store'

const props = defineProps<{ id: number }>()
const router = useRouter()
const people = usePeople()

const person = ref<Person | null>(null)
const rename = useRename<true>() // one editor: the name
const { editing, draft } = rename
const error = ref('')
const actionError = ref('') // a rename's; the list's own reloads don't clear it
const busy = ref(false)
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
    void rename.close(false)
    error.value = ''
    actionError.value = ''
    void load()
  },
  { immediate: true },
)
useLiveReload(load, (c) => c.people || c.assets || c.thumbs)

function edit() {
  if (!busy.value) void rename.open(true, person.value?.name ?? '')
}

/** Saves the name typed; with refocus (Enter), focus returns to the name. */
async function save(refocus: boolean) {
  if (!editing.value || nameTooLong(draft.value) || busy.value) return
  void rename.close(refocus) // leaving the field (blur) leaves focus where it went
  const name = trimName(draft.value)
  if (name === (person.value?.name ?? '')) return
  actionError.value = ''
  busy.value = true
  const requested = revision
  const id = props.id
  try {
    const shown = await namePerson(id, name)
    void people.load().catch(logError)
    if (requested !== revision) return
    if (shown !== null && shown !== props.id) await router.replace({ name: 'person', params: { id: shown } })
    else await load()
  } catch (e) {
    if (requested === revision) actionError.value = errorText(e)
  } finally {
    busy.value = false
  }
}

async function blockPerson() {
  if (!person.value) return
  const requested = revision
  if (
    (await block(BlockKind.BlockPerson, props.id, person.value.name || t('person.unnamed'))) &&
    requested === revision
  )
    await router.push({ name: 'people' }) // their page shows nothing now
}
</script>

<template>
  <div v-if="person" class="person">
    <FaceThumb :face="person.cover" :size="40" round />
    <div class="text">
      <input
        v-if="editing"
        :ref="rename.fieldRef"
        v-model="draft"
        class="name-input field"
        :aria-label="t('person.name')"
        :placeholder="t('person.namePlaceholder')"
        :aria-invalid="nameTooLong(draft) || undefined"
        @keydown.enter="save(true)"
        @keydown.esc.stop="rename.close()"
        @blur="save(false)"
      />
      <h1 v-else class="heading">
        <button
          :ref="rename.triggerRef(true)"
          class="name"
          :aria-disabled="busy || undefined"
          :title="t('person.rename')"
          @click="edit"
        >
          <span class="name-text">{{ person.name || t('person.addName') }}</span>
          <Icon name="pencil" :size="14" />
        </button>
      </h1>
      <span v-if="editing && nameTooLong(draft)" class="count error" role="alert">{{
        t('error.invalidName', { max: MAX_NAME_LEN })
      }}</span>
      <span v-else-if="actionError || error" class="count error" role="alert">{{ actionError || error }}</span>
      <span v-else class="count">{{ t('status.items', { n: person.count }) }}</span>
    </div>
    <RouterLink :to="{ name: 'personFaces', params: { id } }" class="btn" :title="t('person.review')">
      <Icon name="user" :size="16" /><span class="label">{{ t('person.review') }}</span>
    </RouterLink>
    <button
      class="icon-btn"
      :title="t('blocked.block')"
      :aria-label="t('blocked.confirmTitle', { name: person.name || t('person.unnamed') })"
      @click="blockPerson"
    >
      <Icon name="eye-off" :size="18" />
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
.heading {
  margin: 0;
  min-width: 0;
  font: inherit;
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
  border-color: var(--accent);
  font: 600 16px var(--font-display);
}
.count {
  color: var(--text-3);
  font-size: 12px;
  white-space: nowrap;
}
.count.error {
  color: var(--danger);
  white-space: normal;
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
