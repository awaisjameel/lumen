<script setup lang="ts">
// The grid collection cards sit in: square tiles (People, Things, Albums)
// or wide rows (Places, Events), as many per line as fit.
withDefaults(defineProps<{ layout?: 'tile' | 'row' }>(), { layout: 'tile' })
</script>

<template>
  <div class="card-grid" :class="layout">
    <slot />
  </div>
</template>

<style scoped>
.card-grid {
  display: grid;
}
.tile {
  grid-template-columns: repeat(auto-fill, minmax(var(--lumen-card-min), 1fr));
  gap: 1.125rem 0.875rem;
}
.row {
  grid-template-columns: repeat(auto-fill, minmax(min(100%, 21rem), 1fr));
  gap: 0.75rem;
}
</style>
