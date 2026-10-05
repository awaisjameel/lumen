<script setup lang="ts">
// The top of a person's page: their face, their name (click to change it),
// how many photos, and what can be done about them.
import IconButton from '../shared/IconButton.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UDropdownMenu from '@nuxt/ui/components/DropdownMenu.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { BlockKind, engine, MAX_NAME_LEN } from '../bridge'
import type { Person } from '../bridge'
import { errorText, logError } from '../shared/errors'
import { block } from '../shared/block'
import { icons } from '../design/icons'
import { useCommands } from '../shared/commands'
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
const actions = computed(() => [
  { label: t('person.review'), icon: icons.face, to: { name: 'personFaces', params: { id: props.id } } },
  {
    label: t('blocked.confirmTitle', { name: person.value?.name || t('person.unnamed') }),
    icon: icons['eye-off'],
    onSelect: blockPerson,
  },
])
useCommands([
  {
    id: 'person.rename',
    group: 'view',
    title: () => t('person.rename'),
    icon: 'pencil',
    available: () => person.value !== null,
    enabled: () => !busy.value,
    run: edit,
  },
  {
    id: 'person.review',
    group: 'view',
    title: () => t('person.review'),
    icon: 'face',
    available: () => person.value !== null,
    run: () => router.push({ name: 'personFaces', params: { id: props.id } }),
  },
  {
    id: 'person.block',
    group: 'view',
    title: () => t('blocked.block'),
    icon: 'eye-off',
    available: () => person.value !== null,
    run: blockPerson,
  },
])
// Nuxt UI exposes the native input; rename keeps its focus and selection behaviour.
function fieldRef(el: unknown) {
  rename.fieldRef(
    el && typeof el === 'object' && 'inputRef' in el && el.inputRef instanceof HTMLInputElement ? el.inputRef : null,
  )
}
function triggerRef(el: unknown, key: true) {
  rename.triggerRef(key)(el)
}
</script>

<template>
  <div v-if="person" class="person-header">
    <FaceThumb :face="person.cover" :size="40" round />
    <div class="identity">
      <UInput
        v-if="editing"
        :ref="fieldRef"
        v-model="draft"
        class="name-input"
        :aria-label="t('person.name')"
        :placeholder="t('person.namePlaceholder')"
        :aria-invalid="nameTooLong(draft) || undefined"
        @keydown.enter="save(true)"
        @keydown.esc.stop="rename.close()"
        @blur="save(false)"
      />
      <h1 v-else class="heading">
        <UButton
          :ref="(el) => triggerRef(el, true)"
          class="name"
          color="neutral"
          variant="ghost"
          :aria-disabled="busy || undefined"
          :title="t('person.rename')"
          @click="edit"
        >
          <span class="name-text">{{ person.name || t('person.addName') }}</span
          ><Icon name="pencil" :size="14" />
        </UButton>
      </h1>
      <span v-if="editing && nameTooLong(draft)" class="validation" role="alert">{{
        t('error.invalidName', { max: MAX_NAME_LEN })
      }}</span>
      <span v-else-if="actionError || error" class="validation" role="alert">{{ actionError || error }}</span>
      <span v-else class="text-xs text-dimmed">{{ t('status.items', { n: person.count }) }}</span>
    </div>
    <UButton
      class="review"
      :to="{ name: 'personFaces', params: { id } }"
      :icon="icons.face"
      :title="t('person.review')"
      >{{ t('person.review') }}</UButton
    >
    <UDropdownMenu :items="actions">
      <IconButton :icon="icons.more" :label="t('person.actions')" color="neutral" variant="ghost" />
    </UDropdownMenu>
  </div>
  <p v-else-if="error" class="text-error" role="alert">{{ error }}</p>
  <span v-else class="inline-flex min-w-0 items-center gap-2 text-sm text-muted" role="status">
    <Icon name="loading" :size="16" />{{ t('person.loading') }}
  </span>
</template>

<style scoped>
.person-header {
  display: inline-flex;
  align-items: center;
  gap: 0.5rem;
  flex: 1 1 auto;
  min-width: 0;
  max-width: 100%;
}
.identity {
  display: flex;
  flex-direction: column;
  min-width: 0;
  flex: 1 1 auto;
}
.heading {
  margin: 0;
  min-width: 0;
  font: inherit;
}
.name {
  max-width: 100%;
  min-width: 0;
  padding: 0;
  font-size: 1.25rem;
  line-height: 1.2;
  font-weight: 600;
  text-align: left;
}
.name-text {
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}
.name-input {
  width: min(100%, 15rem);
}
.validation {
  color: var(--ui-error);
  font-size: 0.75rem;
  overflow-wrap: anywhere;
}
.review {
  flex-shrink: 0;
}
@container (max-width: 56.25rem) {
  .review {
    display: none;
  }
}
</style>
