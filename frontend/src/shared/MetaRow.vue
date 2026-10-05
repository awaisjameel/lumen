<script setup lang="ts">
// One fact about a photo, a run or the app: an icon, what it is, and its
// value, which can be selected and copied. A missing value is said, never
// left blank or invented.
import IconButton from './IconButton.vue'
import type { IconName } from '../design/icons'
import Icon from './Icon.vue'
import { ref, watch } from 'vue'
import { icons } from '../design/icons'
import { t } from './i18n'
import { reportError } from './errors'

const props = defineProps<{
  label: string
  icon?: IconName
  /** The value; the default slot replaces it for anything richer. */
  value?: string
  copyable?: boolean
}>()
const copied = ref(false)
watch([() => props.value, () => props.label], () => {
  copied.value = false
})
async function copy() {
  if (!props.value) return
  const value = props.value,
    label = props.label
  try {
    await navigator.clipboard.writeText(value)
    if (props.value === value && props.label === label) copied.value = true
  } catch (e) {
    reportError(e)
  }
}
</script>

<template>
  <div class="meta-row">
    <Icon v-if="icon" :name="icon" :size="18" class="lead" />
    <div class="text">
      <span class="label">{{ label }}</span>
      <span class="value lumen-selectable">
        <slot>{{ value }}</slot>
      </span>
    </div>
    <div v-if="$slots.action" class="action">
      <slot name="action" />
    </div>
    <IconButton
      v-if="copyable && value"
      :icon="icons.copy"
      :label="t(copied ? 'files.copied' : 'files.copyField', { name: label })"
      variant="ghost"
      size="xs"
      @click="copy"
    />
  </div>
</template>

<style scoped>
.meta-row {
  display: flex;
  align-items: flex-start;
  gap: 0.75rem;
  min-width: 0;
  padding: 0.375rem 0;
}
.lead {
  margin-top: 0.125rem;
  color: var(--ui-text-dimmed);
}
.text {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-width: 0;
}
.label {
  color: var(--ui-text-dimmed);
  font-size: 0.75rem;
}
.value {
  color: var(--ui-text);
  overflow-wrap: anywhere;
}
.action {
  flex-shrink: 0;
}
</style>
