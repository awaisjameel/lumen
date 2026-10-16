<script setup lang="ts">
// A page: its top bar and view bar fixed over a body that scrolls, and the
// selection dock at its foot while something is selected. Pages with their
// own scroller (the grid) set scroll to false and fill the body themselves.
withDefaults(defineProps<{ scroll?: boolean }>(), { scroll: true })
</script>

<template>
  <section class="lumen-page">
    <slot name="header" />
    <slot name="toolbar" />
    <div class="body" :class="{ scroll }">
      <slot />
    </div>
    <slot name="dock" />
  </section>
</template>

<style scoped>
.lumen-page {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-width: 0;
}
.body {
  position: relative;
  flex: 1;
  min-height: 0;
}
.body.scroll {
  overflow-y: auto;
  padding: 0.5rem var(--lumen-page-x) 3rem;
}
</style>
