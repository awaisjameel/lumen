<script setup lang="ts">
// One setting: its name and what it does on the left, its control on the
// right, and under it whatever the control needs to say (a failed save, a
// value out of range). The control is labelled by the name and described by
// the description, so a screen reader hears both. On a narrow window the
// control drops below the text.
import { computed, inject, useId } from 'vue'
import type { Ref } from 'vue'
import { t } from './i18n'

const props = defineProps<{
  label: string
  description?: string
  /** Why the last change was not saved, or the value was refused. */
  error?: string
  /** A quiet note under the control: the range, the current state. */
  hint?: string
  modified?: boolean
}>()

const id = useId()
// Settings supplies its search once so nested controls retain their own state.
const query = inject<Readonly<Ref<string>> | undefined>('settings-query', undefined)
function parts(text: string) {
  const needle = query?.value.trim() ?? ''
  const at = needle ? text.toLocaleLowerCase().indexOf(needle.toLocaleLowerCase()) : -1
  return at < 0
    ? { before: text, hit: '', after: '' }
    : { before: text.slice(0, at), hit: text.slice(at, at + needle.length), after: text.slice(at + needle.length) }
}
const title = computed(() => parts(props.label))
const detail = computed(() => parts(props.description ?? ''))
</script>

<template>
  <div class="pref-row" :data-pref="label">
    <div class="text">
      <span :id="`${id}-label`" class="label"
        ><span
          >{{ title.before }}<mark v-if="title.hit">{{ title.hit }}</mark
          >{{ title.after }}</span
        ><span
          v-if="modified"
          class="modified-dot"
          :title="t('settings.modified')"
          :aria-label="t('settings.modified')"
      /></span>
      <span v-if="description" :id="`${id}-desc`" class="description"
        >{{ detail.before }}<mark v-if="detail.hit">{{ detail.hit }}</mark
        >{{ detail.after }}</span
      >
    </div>
    <div class="control">
      <slot
        :labelled-by="`${id}-label`"
        :described-by="
          [description ? `${id}-desc` : '', error || hint ? `${id}-message` : ''].filter(Boolean).join(' ') || undefined
        "
      />
    </div>
    <p v-if="error" :id="`${id}-message`" class="message failed" role="alert">{{ error }}</p>
    <p v-else-if="hint" :id="`${id}-message`" class="message">{{ hint }}</p>
  </div>
</template>

<style scoped>
.pref-row {
  display: grid;
  grid-template-columns: minmax(0, 1.1fr) minmax(0, 1fr);
  align-items: center;
  gap: 0.25rem 1.5rem;
  padding: 1rem 1.25rem;
  border-bottom: 1px solid var(--ui-border-muted);
}
.pref-row:last-child {
  border-bottom: 0;
}
.text {
  display: flex;
  flex-direction: column;
  gap: 0.125rem;
  min-width: 0;
}
.label {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  overflow-wrap: anywhere;
  color: var(--ui-text-highlighted);
  font-weight: 600;
}
.description {
  color: var(--ui-text-dimmed);
  font-size: 0.8125rem;
}
.modified-dot {
  flex: none;
  width: 0.375rem;
  height: 0.375rem;
  border-radius: 50%;
  background: var(--ui-primary);
}
mark {
  background: var(--lumen-primary-soft-2);
  color: var(--ui-text-highlighted);
}
.control {
  display: flex;
  min-width: 0;
  align-items: center;
  justify-content: flex-end;
  flex-wrap: wrap;
  gap: 0.5rem;
}
.message {
  grid-column: 1 / -1;
  margin: 0;
  color: var(--ui-text-dimmed);
  font-size: 0.8125rem;
}
.message.failed {
  color: var(--ui-error);
}
@container (max-width: 32rem) {
  .pref-row {
    grid-template-columns: minmax(0, 1fr);
  }
  .control {
    justify-content: flex-start;
  }
}
</style>
