<script setup lang="ts">
import UButton from '@nuxt/ui/components/Button.vue'
import type { Person } from '../bridge'
import { t } from '../shared/i18n'
import FaceThumb from './FaceThumb.vue'

defineProps<{ id: string; people: Person[]; active: number; disabled?: boolean }>()
const emit = defineEmits<{ pick: [person: Person]; activate: [index: number] }>()
</script>

<template>
  <ul :id="`${id}-list`" class="suggestions" role="listbox" @mousedown.prevent>
    <li v-for="(p, i) in people" :key="p.id" role="presentation">
      <UButton
        :id="`${id}-${i}`"
        role="option"
        :aria-selected="active === i"
        :disabled="disabled"
        color="neutral"
        :variant="active === i ? 'soft' : 'ghost'"
        class="option"
        @mouseenter="emit('activate', i)"
        @click="emit('pick', p)"
      >
        <FaceThumb :face="p.cover" :size="32" round /><span class="name" :title="p.name" data-person-name>{{
          p.name
        }}</span
        ><span v-if="p.count" class="count">{{ t('person.photos', { n: p.count }) }}</span>
      </UButton>
    </li>
  </ul>
</template>

<style scoped>
.suggestions {
  margin: 0;
  padding: 0.25rem;
  list-style: none;
  max-height: min(14rem, calc(100vh - 14rem));
  overflow-y: auto;
}
.option {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  width: 100%;
  min-height: 3rem;
  padding: 0.25rem 0.5rem;
}
.name {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.count {
  margin-left: auto;
  white-space: nowrap;
  color: var(--ui-text-dimmed);
  font-size: 0.75rem;
  font-variant-numeric: tabular-nums;
}
</style>
