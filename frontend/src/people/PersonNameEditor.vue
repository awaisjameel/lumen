<script setup lang="ts">
import UButton from '@nuxt/ui/components/Button.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import { computed, nextTick, onBeforeUnmount, onMounted, ref, shallowRef, useId, watch } from 'vue'
import { engine, MAX_NAME_LEN } from '../bridge'
import type { Person } from '../bridge'
import { icons } from '../design/icons'
import { errorText } from '../shared/errors'
import { t } from '../shared/i18n'
import { nameTooLong, trimName } from '../shared/names'
import { whenPortalsClosed } from '../shared/overlay'
import { useSuggestions } from '../shared/suggest'
import FaceThumb from './FaceThumb.vue'
import { namePerson } from './naming'

const props = defineProps<{ person: Person }>()
const emit = defineEmits<{ saved: [id: number, name: string]; cancel: []; busy: [value: boolean] }>()
const id = useId()
const draft = ref(props.person.name)
const field = ref<{ inputRef: HTMLInputElement | null } | null>(null)
const busy = ref(false)
watch(busy, (value) => emit('busy', value), { flush: 'sync' })
const error = ref('')
const active = ref(-1)
const suggestionsOpen = ref(true)
const found = useSuggestions<Person[]>(draft, engine.suggestPeople, [])
const options = computed(() => found.value.filter((p) => p.id !== props.person.id))
const invalid = computed(() => nameTooLong(draft.value) || (!trimName(draft.value) && !props.person.name))
const merge = shallowRef<{ name: string; answer: (same: boolean) => void } | null>(null)
let alive = true
onBeforeUnmount(() => {
  alive = false
  merge.value?.answer(false)
  emit('busy', false)
})
onMounted(async () => {
  await nextTick()
  await whenPortalsClosed()
  if (!alive) return
  field.value?.inputRef?.focus()
  field.value?.inputRef?.select()
})

async function save() {
  if (busy.value || invalid.value) return
  const name = trimName(draft.value)
  if (name === props.person.name) {
    emit('cancel')
    return
  }
  busy.value = true
  error.value = ''
  try {
    const shown = await namePerson(
      props.person.id,
      name,
      (name) =>
        new Promise((answer) => {
          merge.value = { name, answer }
        }),
    )
    if (alive && shown !== null) emit('saved', shown, name)
  } catch (e) {
    if (alive) error.value = errorText(e)
  } finally {
    busy.value = false
    merge.value = null
  }
}
function answer(same: boolean) {
  merge.value?.answer(same)
  merge.value = null
  if (!same) void nextTick(() => field.value?.inputRef?.focus())
}
function keys(e: KeyboardEvent) {
  if (e.isComposing || busy.value) return
  if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
    e.preventDefault()
    suggestionsOpen.value = true
    if (options.value.length)
      active.value =
        e.key === 'ArrowDown'
          ? (active.value + 1) % options.value.length
          : (active.value <= 0 ? options.value.length : active.value) - 1
  } else if (e.key === 'Enter') {
    e.preventDefault()
    const chosen = suggestionsOpen.value ? options.value[active.value] : undefined
    if (chosen) draft.value = chosen.name
    suggestionsOpen.value = false
    void save()
  } else if (e.key === 'Escape') {
    e.stopPropagation()
    if (suggestionsOpen.value && options.value.length) suggestionsOpen.value = false
    else emit('cancel')
  }
}
function changed() {
  active.value = -1
  suggestionsOpen.value = true
}
function choose(p: Person) {
  draft.value = p.name
  suggestionsOpen.value = false
  void save()
}
</script>

<template>
  <form class="name-editor" data-person-editor @submit.prevent="save">
    <label :for="id" class="text-xs font-semibold">{{
      person.name ? t('person.rename') : t('person.namePlaceholder')
    }}</label>
    <UInput
      :id="id"
      ref="field"
      v-model="draft"
      class="w-full"
      role="combobox"
      :aria-label="t('person.name')"
      :placeholder="t('person.typeName')"
      :aria-invalid="nameTooLong(draft) || undefined"
      :aria-expanded="!busy && suggestionsOpen && options.length > 0"
      :aria-controls="`${id}-suggestions`"
      :aria-activedescendant="active >= 0 && suggestionsOpen ? `${id}-${active}` : undefined"
      aria-autocomplete="list"
      autocomplete="off"
      :disabled="busy"
      @update:model-value="changed"
      @keydown="keys"
    />
    <p v-if="nameTooLong(draft)" role="alert" class="text-xs text-error">
      {{ t('error.invalidName', { max: MAX_NAME_LEN }) }}
    </p>
    <p v-else class="text-xs text-dimmed">{{ t('person.nameHint') }}</p>
    <p v-if="error" role="alert" class="text-sm text-error">{{ error }}</p>
    <div class="editor-actions">
      <UButton
        color="primary"
        type="submit"
        :disabled="invalid || busy"
        :loading="busy"
        :variant="merge ? 'outline' : 'solid'"
        >{{ error ? t('common.retry') : trimName(draft) ? t('person.saveName') : t('person.removeName') }}</UButton
      >
      <UButton
        color="neutral"
        variant="ghost"
        :disabled="busy && !merge"
        @click="merge ? answer(false) : emit('cancel')"
        >{{ t('common.cancel') }}</UButton
      >
    </div>
    <div
      v-if="merge"
      class="merge-prompt"
      role="group"
      :aria-label="t('person.mergeTitle', { name: merge.name })"
      @keydown.esc.stop.prevent="answer(false)"
    >
      <b>{{ t('person.mergeTitle', { name: merge.name }) }}</b>
      <p>{{ t('person.mergeText') }}</p>
      <UButton color="primary" variant="solid" :icon="icons.merge" @click="answer(true)">{{
        t('person.merge')
      }}</UButton>
      <UButton color="neutral" variant="ghost" @click="answer(false)">{{ t('person.differentName') }}</UButton>
    </div>
    <ul
      v-else-if="!busy && suggestionsOpen && options.length"
      :id="`${id}-suggestions`"
      role="listbox"
      :aria-label="t('person.alreadyNamed')"
      class="suggestions"
    >
      <li v-for="(p, i) in options" :id="`${id}-${i}`" :key="p.id" role="option" :aria-selected="active === i">
        <UButton
          color="neutral"
          :variant="active === i ? 'soft' : 'ghost'"
          class="w-full justify-start"
          @mousedown.prevent
          @click="choose(p)"
        >
          <FaceThumb :face="p.cover" :size="24" round /><span class="truncate" :title="p.name">{{ p.name }}</span>
        </UButton>
      </li>
    </ul>
  </form>
</template>

<style scoped>
.name-editor {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
  min-width: 0;
  width: 100%;
  max-width: 28rem;
}
.editor-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 0.25rem;
}
.suggestions,
.merge-prompt {
  padding: 0.5rem;
  border: 1px solid var(--ui-border);
  border-radius: var(--lumen-r-md);
  background: var(--lumen-bg-sunken);
}
.suggestions {
  list-style: none;
  margin: 0;
}
.merge-prompt {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
  overflow-wrap: anywhere;
  font-size: 0.8125rem;
}
</style>
