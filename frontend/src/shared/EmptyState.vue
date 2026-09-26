<script setup lang="ts">
// What a page shows when it has nothing to show: an icon, what's missing,
// and what to do about it.
import Icon from './Icon.vue'
import type { IconName } from './Icon.vue'

defineProps<{ icon: IconName; title: string; text?: string }>()
</script>

<template>
  <div class="empty-state">
    <Icon :name="icon" :size="40" />
    <h2>{{ title }}</h2>
    <p v-if="text">{{ text }}</p>
  </div>
</template>

<style scoped>
.empty-state {
  display: grid;
  justify-items: center;
  gap: 4px;
  padding: 24px;
  text-align: center;
  color: var(--text-3);
}
h2 {
  margin: 8px 0 0;
  color: var(--text);
  font: 600 18px var(--font-display);
}
p {
  margin: 0;
  max-width: 360px;
  color: var(--text-2);
}
</style>
