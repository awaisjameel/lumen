<script setup lang="ts" generic="T">
// Per-file outcomes of a file or selection action, a page at a time: a
// selection can hold every result of the library.
import { computed, ref, watch } from 'vue'
import UButton from '@nuxt/ui/components/Button.vue'
import { t } from '../shared/i18n'

const props = defineProps<{ items: T[] }>()
defineSlots<{ default?: (props: { item: T }) => unknown }>()
const PAGE = 100
const page = ref(0)
const shown = computed(() => props.items.slice(page.value * PAGE, (page.value + 1) * PAGE))
watch(
  () => props.items.length,
  (n) => {
    if (page.value * PAGE >= n) page.value = 0
  },
)
</script>

<template>
  <div class="outcomes" role="status" aria-live="polite" aria-atomic="false">
    <p v-if="items.length > PAGE">
      {{ t('files.previewCount', { shown: Math.min((page + 1) * PAGE, items.length), n: items.length }) }}
    </p>
    <slot v-for="item in shown" :item="item" />
  </div>
  <nav v-if="items.length > PAGE" class="pages" :aria-label="t('files.pages')">
    <UButton :disabled="page === 0" variant="outline" @click="page--">{{ t('files.previousPage') }}</UButton>
    <UButton :disabled="(page + 1) * PAGE >= items.length" variant="outline" @click="page++">{{
      t('files.nextPage')
    }}</UButton>
  </nav>
</template>

<style scoped>
.outcomes {
  max-height: 20rem;
  overflow-y: auto;
  overflow-wrap: anywhere;
  font-size: 0.8125rem;
  font-variant-numeric: tabular-nums;
}
.outcomes :deep(p) {
  margin: 0.25rem 0;
}
.pages {
  display: flex;
  gap: 0.5rem;
  justify-content: flex-end;
  padding-top: 0.75rem;
}
</style>
