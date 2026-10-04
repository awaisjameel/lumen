<script setup lang="ts">
// The bar that goes with a selection or a workspace's bulk actions (the
// Trash, duplicates, a block's covered media): what is selected on the
// left, what can be done with it on the right. On a narrow window the
// actions wrap below the summary rather than being cut off.
defineProps<{
  /** What the actions apply to, already worded ("3 selected"). */
  summary: string
  /** A second, quieter line: what the actions will do, or a caution. */
  hint?: string
}>()
</script>

<template>
  <div class="selection-bar lumen-surface" role="toolbar" :aria-label="summary">
    <div class="about">
      <span class="summary">{{ summary }}</span>
      <span v-if="hint" class="hint">{{ hint }}</span>
    </div>
    <div class="actions">
      <slot />
    </div>
  </div>
</template>

<style scoped>
.selection-bar {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 0.5rem var(--lumen-gap);
  margin: 0 var(--lumen-page-x) 0.5rem;
  padding: 0.5rem 0.75rem;
  border: 1px solid var(--ui-border);
  border-radius: calc(var(--ui-radius) * 2);
  background: var(--ui-bg-muted);
}
.about {
  display: flex;
  flex-direction: column;
  flex: 1 1 12rem;
  min-width: 0;
}
.summary {
  color: var(--ui-text-highlighted);
  font-weight: 600;
}
.hint {
  color: var(--ui-text-muted);
  font-size: 0.8125rem;
}
.actions {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 0.5rem;
}
</style>
