<script setup lang="ts">
// A settings section's heading. A section whose settings can be put back to
// their defaults has a Reset beside it, and says below what the last reset
// did; the page asks before it resets anything.
import UButton from '@nuxt/ui/components/Button.vue'
import { icons } from '../design/icons'
import { t } from '../shared/i18n'

withDefaults(defineProps<{ title: string; resettable?: boolean; note?: string }>(), { note: '' })
const emit = defineEmits<{ reset: [] }>()
</script>
<template>
  <div class="section-head">
    <h2>{{ title }}</h2>
    <UButton
      v-if="resettable"
      size="xs"
      variant="ghost"
      :icon="icons.reset"
      :aria-label="t('settings.resetSectionNamed', { section: title })"
      data-reset-section
      @click="emit('reset')"
      >{{ t('settings.resetSection') }}</UButton
    >
  </div>
  <p v-if="note" role="status" class="text-muted">{{ note }}</p>
</template>
<style scoped>
.section-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  margin-bottom: 0.5rem;
}

h2 {
  margin: 0;
  font-size: 1rem;
  font-weight: 600;
  color: var(--ui-text-highlighted);
}
</style>
