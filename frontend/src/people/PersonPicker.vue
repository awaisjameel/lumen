<script setup lang="ts">
// A name box that suggests people already named: pick one, or type a new
// name and press Enter. Emits the name either way (the engine finds or
// creates the person).
import UInput from '@nuxt/ui/components/Input.vue'
import UPopover from '@nuxt/ui/components/Popover.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import { computed, onMounted, ref, useId, watch } from 'vue'
import { engine, MAX_NAME_LEN } from '../bridge'
import type { Person } from '../bridge'
import { useSuggestions } from '../shared/suggest'
import { t } from '../shared/i18n'
import { nameTooLong, trimName } from '../shared/names'
import { errorText, logError } from '../shared/errors'
import PersonPickerOptions from './PersonPickerOptions.vue'
import { usePeople } from './store'

const props = defineProps<{
  placeholder: string
  exclude?: number
  busy?: boolean
  error?: string
  managed?: boolean
}>()
const emit = defineEmits<{ pick: [name: string]; cancel: [] }>()
const people = usePeople()
onMounted(() => {
  if (!people.loaded) void people.load().catch(logError)
})

const id = useId()
const text = ref('')
const open = ref(false)
const active = ref(-1) // -1 = the typed text
const validation = ref('')
const checking = ref(false)

const found = useSuggestions<Person[]>(text, engine.suggestPeople, [])
const options = computed(() =>
  (trimName(text.value)
    ? found.value.map((p) => people.byId.get(p.id) ?? p)
    : people.list.filter((p) => p.name).slice(0, 24)
  ).filter((p) => p.id !== props.exclude),
)
// New suggestions (or none) start from the typed text again.
watch(options, () => (active.value = -1))
watch(text, () => {
  validation.value = ''
})
watch(
  () => props.busy,
  (busy, before) => {
    if (!busy && before && !props.error) {
      text.value = ''
      open.value = false
    }
  },
)

async function pick(name: string, person?: Person) {
  if (props.busy || checking.value) return
  name = trimName(name)
  if (!name || nameTooLong(name)) return
  checking.value = true
  try {
    if (props.exclude !== undefined && (person?.id ?? (await engine.personByName(name))) === props.exclude) {
      validation.value = t('picker.current')
      return
    }
    emit('pick', name)
    if (!props.managed) {
      text.value = ''
      open.value = false
    }
  } catch (e) {
    validation.value = errorText(e)
  } finally {
    checking.value = false
  }
}

function onKey(e: KeyboardEvent) {
  if (e.isComposing) return // the key belongs to the input method
  const n = options.value.length
  if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
    e.preventDefault()
    open.value = true
    if (n) active.value = e.key === 'ArrowDown' ? (active.value + 1) % n : (active.value <= 0 ? n : active.value) - 1
  } else if (e.key === 'Enter') {
    e.preventDefault()
    const person = options.value[active.value]
    void pick(person?.name ?? text.value, person) // -1: what was typed
  } else if (e.key === 'Escape' && (open.value || props.managed)) {
    e.stopPropagation()
    if (props.managed) emit('cancel')
    open.value = false
  }
}
// Suggestions must leave focus in the combobox so its arrow and Enter keys keep working.
function preventFocus(e: Event) {
  e.preventDefault()
}
</script>

<template>
  <div class="picker" @focusout="open = false">
    <label v-if="managed" :for="`${id}-input`" class="mb-2 block text-xs font-semibold">{{
      t('person.namePlaceholder')
    }}</label>
    <UPopover
      :open="!managed && open && (options.length > 0 || Boolean(trimName(text)))"
      :content="{ onOpenAutoFocus: preventFocus, onCloseAutoFocus: preventFocus }"
      :ui="{ content: 'min-w-52 max-w-80' }"
      @update:open="open = $event"
    >
      <template #anchor>
        <UInput
          :id="`${id}-input`"
          v-model="text"
          class="w-full"
          type="search"
          role="combobox"
          aria-autocomplete="list"
          :aria-controls="`${id}-list`"
          :aria-expanded="managed || (open && (options.length > 0 || Boolean(trimName(text))))"
          :aria-activedescendant="(managed || open) && active >= 0 ? `${id}-${active}` : undefined"
          :aria-label="placeholder"
          :placeholder="placeholder"
          :aria-invalid="nameTooLong(text) || undefined"
          :disabled="busy || checking"
          spellcheck="false"
          autocomplete="off"
          @focus="open = true"
          @input="open = true"
          @keydown="onKey"
        />
      </template>
      <template #content>
        <PersonPickerOptions
          :id="id"
          :people="options"
          :active="active"
          :disabled="busy || checking"
          @activate="active = $event"
          @pick="pick($event.name, $event)"
        />
        <p v-if="!options.length" class="px-3 py-2 text-xs text-muted">{{ t('picker.noMatch') }}</p>
        <UButton
          v-if="trimName(text) && !nameTooLong(text) && !found.some((p) => p.name === trimName(text))"
          class="m-1 max-w-full"
          color="neutral"
          variant="ghost"
          :disabled="busy || checking"
          @mousedown.prevent
          @click="pick(text)"
          ><span class="truncate">{{ t('picker.create', { name: trimName(text) }) }}</span></UButton
        >
      </template>
    </UPopover>
    <div v-if="managed">
      <PersonPickerOptions
        :id="id"
        :people="options"
        :active="active"
        :disabled="busy || checking"
        @activate="active = $event"
        @pick="pick($event.name, $event)"
      />
      <p v-if="!options.length" class="px-3 py-2 text-xs text-muted">{{ t('picker.noMatch') }}</p>
      <UButton
        v-if="trimName(text) && !nameTooLong(text) && !found.some((p) => p.name === trimName(text))"
        class="m-1 max-w-full"
        color="neutral"
        variant="ghost"
        :disabled="busy || checking"
        @mousedown.prevent
        @click="pick(text)"
        ><span class="truncate">{{ t('picker.create', { name: trimName(text) }) }}</span></UButton
      >

      <UButton color="neutral" variant="ghost" :disabled="busy || checking" @click="emit('cancel')">{{
        t('common.cancel')
      }}</UButton>
    </div>
    <p v-if="nameTooLong(text)" class="text-xs text-error" role="alert">
      {{ t('error.invalidName', { max: MAX_NAME_LEN }) }}
    </p>
    <p v-if="validation" class="text-xs text-error" role="alert">{{ validation }}</p>
    <p v-if="error" class="text-xs text-error" role="alert">{{ error }}</p>
    <p v-if="busy" class="text-xs text-muted" role="status">{{ t('picker.saving') }}</p>
  </div>
</template>

<style scoped>
.picker {
  min-width: 0;
}
</style>
