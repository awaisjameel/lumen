<script setup lang="ts">
// One fact about a photo, a run or the app: an icon, what it is, and its
// value, which can be selected and copied. A missing value is said, never
// left blank or invented.
import type { IconName } from '../design/icons'
import Icon from './Icon.vue'

defineProps<{
  label: string
  icon?: IconName
  /** The value; the default slot replaces it for anything richer. */
  value?: string
}>()
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
