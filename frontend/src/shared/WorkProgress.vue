<script setup lang="ts">
// Background work, shown the same way wherever it runs (indexing, an
// organise run, an XMP export): what is happening, how far it is, and the
// one or two things the user can do about it. The words carry the state, so
// it reads without the bar's motion.
import UProgress from '@nuxt/ui/components/Progress.vue'

defineProps<{
  /** What is happening, in words: "Moving files…", "Waiting for mains power". */
  label: string
  /** How far it is ("120 of 4,000"), or why it waits. */
  detail?: string
  done?: number
  total?: number
  /** Work whose length is not known yet. */
  indeterminate?: boolean
  /** It stopped on a failure: the bar turns to the error colour. */
  failed?: boolean
}>()
</script>

<template>
  <div class="work-progress" role="status">
    <div class="line">
      <span class="label">{{ label }}</span>
      <div v-if="$slots.default" class="actions">
        <slot />
      </div>
    </div>
    <UProgress
      :model-value="indeterminate ? null : (done ?? 0)"
      :max="total || 1"
      :color="failed ? 'error' : 'primary'"
      size="xs"
      :aria-label="label"
    />
    <span v-if="detail" class="detail">{{ detail }}</span>
  </div>
</template>

<style scoped>
.work-progress {
  display: flex;
  flex-direction: column;
  gap: 0.375rem;
  min-width: 0;
}
.line {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  min-width: 0;
}
.label {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  color: var(--ui-text);
  font-weight: 600;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.actions {
  display: flex;
  gap: 0.25rem;
}
.detail {
  color: var(--ui-text-dimmed);
  font-size: 0.75rem;
}
</style>
