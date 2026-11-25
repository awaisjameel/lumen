<script setup lang="ts">
// Chooses what a person is to the user: a searchable list of the built-in
// relationships, or the user's own word, with Remove when they have one.
// Picking an option saves it; Enter saves the highlighted option or the
// typed word. The engine keeps a label that is a built-in word as that
// relationship, and the saved person comes back as it was kept.
import UButton from '@nuxt/ui/components/Button.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import { computed, nextTick, onBeforeUnmount, onMounted, ref, useId, watch } from 'vue'
import { engine, MAX_RELATION_LABEL_LEN, Relation } from '../bridge'
import type { Person } from '../bridge'
import { errorText } from '../shared/errors'
import { t } from '../shared/i18n'
import { sameName, trimName } from '../shared/names'
import { whenPortalsClosed } from '../shared/overlay'
import { personName, relationOptions, relationText } from './relationships'
import type { RelationOption } from './relationships'

const props = defineProps<{ person: Person }>()
const emit = defineEmits<{ saved: [person: Person]; cancel: []; busy: [value: boolean] }>()
const id = useId()
const draft = ref(relationText(props.person))
const field = ref<{ inputRef: HTMLInputElement | null } | null>(null)
const busy = ref(false)
watch(busy, (value) => emit('busy', value), { flush: 'sync' })
const error = ref('')
const active = ref(-1)
const options = computed(() => relationOptions(draft.value))
const tooLong = computed(() => Array.from(trimName(draft.value)).length > MAX_RELATION_LABEL_LEN)
const had = computed(() => props.person.relation !== Relation.RelationNone)
let alive = true
onBeforeUnmount(() => {
  alive = false
  emit('busy', false)
})
onMounted(async () => {
  await nextTick()
  await whenPortalsClosed()
  if (!alive) return
  field.value?.inputRef?.focus()
  field.value?.inputRef?.select()
})
watch(draft, () => {
  active.value = -1
  error.value = ''
})

async function save(option: RelationOption | undefined) {
  if (busy.value || !option || (option.relation === Relation.RelationCustom && tooLong.value)) return
  if (option.relation === props.person.relation && option.label === props.person.relationLabel) {
    emit('cancel')
    return
  }
  busy.value = true
  error.value = ''
  try {
    const saved = await engine.setRelation(props.person.id, option.relation, option.label)
    if (alive) emit('saved', saved)
  } catch (e) {
    if (alive) error.value = errorText(e)
  } finally {
    busy.value = false
  }
}
/** What Enter or Save means now: the highlighted option, else the one the text names. */
function chosen(): RelationOption | undefined {
  const typed = trimName(draft.value)
  const highlighted = options.value[active.value]
  if (highlighted || !typed) return highlighted
  return (
    options.value.find((o) => o.relation !== Relation.RelationCustom && sameName(o.text, typed)) ??
    options.value.find((o) => o.relation === Relation.RelationCustom)
  )
}
function remove() {
  void save({ relation: Relation.RelationNone, label: '', text: '' })
}
function keys(e: KeyboardEvent) {
  if (e.isComposing || busy.value) return
  const n = options.value.length
  if ((e.key === 'ArrowDown' || e.key === 'ArrowUp') && n) {
    e.preventDefault()
    active.value = e.key === 'ArrowDown' ? (active.value + 1) % n : (active.value <= 0 ? n : active.value) - 1
  } else if (e.key === 'Enter') {
    e.preventDefault()
    void save(chosen())
  } else if (e.key === 'Escape') {
    e.stopPropagation()
    emit('cancel')
  }
}
watch(active, () => document.getElementById(`${id}-${active.value}`)?.scrollIntoView({ block: 'nearest' }), {
  flush: 'post',
})
</script>

<template>
  <form class="relation-editor" data-relation-editor @submit.prevent="save(chosen())">
    <label :for="id" class="text-xs font-semibold">{{ t('person.relationTo', { name: personName(person) }) }}</label>
    <UInput
      :id="id"
      ref="field"
      v-model="draft"
      class="w-full"
      role="combobox"
      :aria-label="t('person.relation')"
      :placeholder="t('person.relationPlaceholder')"
      :aria-invalid="tooLong || undefined"
      aria-expanded="true"
      :aria-controls="`${id}-options`"
      :aria-activedescendant="active >= 0 ? `${id}-${active}` : undefined"
      aria-autocomplete="list"
      autocomplete="off"
      :disabled="busy"
      @keydown="keys"
    />
    <p v-if="tooLong" role="alert" class="text-xs text-error">
      {{ t('error.invalidRelation', { relationMax: MAX_RELATION_LABEL_LEN }) }}
    </p>
    <p v-else class="text-xs text-dimmed">{{ t('person.relationHint', { max: MAX_RELATION_LABEL_LEN }) }}</p>
    <p v-if="error" role="alert" class="text-sm text-error">{{ error }}</p>
    <ul :id="`${id}-options`" role="listbox" :aria-label="t('person.relationOptions')" class="options">
      <li
        v-for="(o, i) in options"
        :id="`${id}-${i}`"
        :key="`${o.relation}:${o.label}`"
        role="option"
        :aria-selected="active === i"
      >
        <UButton
          color="neutral"
          :variant="active === i ? 'soft' : 'ghost'"
          class="w-full justify-start"
          :disabled="busy"
          tabindex="-1"
          :data-relation="o.relation"
          @mousedown.prevent
          @click="save(o)"
          ><span class="truncate" :title="o.text">{{ o.text }}</span></UButton
        >
      </li>
      <li v-if="!options.length" class="empty" aria-disabled="true">{{ t('person.relationNone') }}</li>
    </ul>
    <div class="editor-actions">
      <UButton color="primary" type="submit" :disabled="!chosen() || busy" :loading="busy">{{
        error ? t('common.retry') : t('person.saveRelation')
      }}</UButton>
      <UButton
        v-if="had"
        color="neutral"
        variant="outline"
        :disabled="busy"
        data-action="remove-relation"
        @click="remove"
        >{{ t('person.removeRelation') }}</UButton
      >
      <UButton color="neutral" variant="ghost" :disabled="busy" @click="emit('cancel')">{{
        t('common.cancel')
      }}</UButton>
    </div>
  </form>
</template>

<style scoped>
.relation-editor {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
  min-width: 0;
  width: 100%;
  max-width: 28rem;
}
.options {
  max-height: 14rem;
  overflow-y: auto;
  margin: 0;
  padding: 0.5rem;
  list-style: none;
  border: 1px solid var(--ui-border);
  border-radius: var(--lumen-r-md);
  background: var(--lumen-bg-sunken);
}
.empty {
  padding: 0.375rem 0.625rem;
  color: var(--ui-text-dimmed);
  font-size: 0.8125rem;
}
.editor-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 0.25rem;
}
@media (forced-colors: active) {
  .options {
    border-color: CanvasText;
  }
}
</style>
