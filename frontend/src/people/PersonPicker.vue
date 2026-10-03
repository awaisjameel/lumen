<script setup lang="ts">
// A name box that suggests people already named: pick one, or type a new
// name and press Enter. Emits the name either way (the engine finds or
// creates the person).
import UInput from '@nuxt/ui/components/Input.vue'
import UPopover from '@nuxt/ui/components/Popover.vue'
import { computed, ref, useId, watch } from 'vue'
import { engine, MAX_NAME_LEN } from '../bridge'
import type { Person } from '../bridge'
import { useSuggestions } from '../shared/suggest'
import { t } from '../shared/i18n'
import { nameTooLong, trimName } from '../shared/names'
import FaceThumb from './FaceThumb.vue'

const props = defineProps<{ placeholder: string; exclude?: number }>()
const emit = defineEmits<{ pick: [name: string] }>()

const id = useId()
const text = ref('')
const open = ref(false)
const active = ref(-1) // -1 = the typed text

const found = useSuggestions<Person[]>(text, engine.suggestPeople, [])
const options = computed(() => found.value.filter((p) => p.id !== props.exclude))
watch(text, () => (active.value = -1))

function pick(name: string) {
  name = trimName(name)
  if (!name || nameTooLong(name)) return
  emit('pick', name)
  text.value = ''
  open.value = false
}

function onKey(e: KeyboardEvent) {
  const n = options.value.length
  if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
    e.preventDefault()
    open.value = true
    if (n) active.value = e.key === 'ArrowDown' ? (active.value + 1) % n : (active.value - 1 + n) % n
  } else if (e.key === 'Enter') {
    e.preventDefault()
    pick(options.value[active.value]?.name ?? text.value) // -1: what was typed
  } else if (e.key === 'Escape' && open.value) {
    e.stopPropagation()
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
    <UPopover
      :open="open && options.length > 0"
      :content="{ onOpenAutoFocus: preventFocus, onCloseAutoFocus: preventFocus }"
      :ui="{ content: 'min-w-52 max-w-80' }"
      @update:open="open = $event"
    >
      <template #anchor>
        <UInput
          v-model="text"
          class="w-full"
          role="combobox"
          aria-autocomplete="list"
          :aria-controls="`${id}-list`"
          :aria-expanded="open && options.length > 0"
          :aria-activedescendant="open && active >= 0 ? `${id}-${active}` : undefined"
          :aria-label="placeholder"
          :placeholder="placeholder"
          :aria-invalid="nameTooLong(text) || undefined"
          spellcheck="false"
          autocomplete="off"
          @focus="open = true"
          @input="open = true"
          @keydown="onKey"
        />
      </template>
      <template #content>
        <ul :id="`${id}-list`" class="suggestions" role="listbox" @mousedown.prevent>
          <li
            v-for="(p, i) in options"
            :id="`${id}-${i}`"
            :key="p.id"
            role="option"
            :aria-selected="active === i"
            :class="{ active: active === i }"
            @mouseenter="active = i"
            @click="pick(p.name)"
          >
            <FaceThumb :face="p.cover" :size="24" round /><span class="truncate">{{ p.name }}</span>
          </li>
        </ul>
      </template>
    </UPopover>
    <p v-if="nameTooLong(text)" class="text-xs text-error" role="alert">
      {{ t('error.invalidName', { max: MAX_NAME_LEN }) }}
    </p>
  </div>
</template>

<style scoped>
.picker {
  min-width: 0;
}
.suggestions {
  margin: 0;
  padding: 0.25rem;
  list-style: none;
}
.suggestions li {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  min-height: 2rem;
  padding: 0.25rem 0.5rem;
  border-radius: var(--ui-radius);
  cursor: pointer;
}
.suggestions li.active {
  background: var(--ui-bg-elevated);
  outline: 1px solid var(--ui-primary);
}
</style>
