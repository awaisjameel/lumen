<script setup lang="ts">
// The row under a page's top bar that holds the controls acting on the
// content below it: filters at the start, then sort, zoom and view
// options at the end. It lines up with the content's own edges, so the
// controls sit over what they change.
defineProps<{
  /** What the controls act on, for assistive technology ("Photo timeline"). */
  label: string
}>()
</script>

<template>
  <div class="view-bar" role="toolbar" :aria-label="label">
    <div class="start">
      <slot />
    </div>
    <div v-if="$slots.end" class="end">
      <slot name="end" />
    </div>
  </div>
</template>

<style scoped>
.view-bar {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  flex-shrink: 0;
  min-width: 0;
  padding: 0.25rem var(--lumen-page-x) 0.75rem;
}
.start,
.end {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  min-width: 0;
}
.start {
  flex: 1 1 auto;
}
.end {
  flex: none;
  margin-left: auto;
}
</style>
