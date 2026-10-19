<script setup lang="ts">
// A pill-shaped button: a filter that is on or off (Favourites), one of a row
// of choices (a country), a part of a search that can be taken away, or a
// search to try. On, it takes the accent's soft colour. Everything given to
// it reaches the button, so it can open a menu or a popover.
withDefaults(
  defineProps<{
    /** Whether it is on; left out for a chip that only acts. */
    pressed?: boolean
  }>(),
  { pressed: undefined },
)
</script>

<template>
  <button type="button" class="chip" :aria-pressed="pressed">
    <slot />
  </button>
</template>

<style scoped>
.chip {
  display: inline-flex;
  flex: none;
  align-items: center;
  gap: 0.375rem;
  max-width: 100%;
  min-height: var(--lumen-control-h);
  padding: 0 0.75rem;
  border: 1px solid var(--ui-border-accented);
  border-radius: 999px;
  background: var(--lumen-bg-raised);
  color: var(--ui-text-toned);
  font-size: 0.8125rem;
  font-weight: 500;
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
  cursor: pointer;
  transition:
    background-color var(--lumen-d-fast) var(--lumen-ease-out),
    border-color var(--lumen-d-fast) var(--lumen-ease-out),
    color var(--lumen-d-fast) var(--lumen-ease-out),
    transform var(--lumen-d-fast) var(--lumen-ease-out);
}
.chip:hover:not(:disabled) {
  background: var(--lumen-bg-hover);
  color: var(--ui-text-highlighted);
}
.chip:active:not(:disabled) {
  transform: scale(0.98);
}
.chip[aria-pressed='true'] {
  border-color: transparent;
  background: var(--lumen-primary-soft);
  color: var(--lumen-primary-text);
}
.chip:focus-visible {
  outline: 2px solid var(--ui-primary);
  outline-offset: 2px;
}
.chip:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}
.chip :deep(svg) {
  flex: none;
}
@media (prefers-reduced-motion: reduce) {
  .chip:active:not(:disabled) {
    transform: none;
  }
}
@media (forced-colors: active) {
  .chip[aria-pressed='true'] {
    border-color: Highlight;
  }
}
</style>
