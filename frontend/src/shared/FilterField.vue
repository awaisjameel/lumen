<script setup lang="ts">
// A page's own filter: it narrows what the page lists as you type (people
// by name, settings by what they say), and goes nowhere else. It looks like
// the search field, so typing to find things looks the same everywhere,
// but it offers no suggestions. It sits in PageHeader's #search slot.
import UInput from '@nuxt/ui/components/Input.vue'
import { icons } from '../design/icons'
import { t } from './i18n'
import IconButton from './IconButton.vue'

defineProps<{
  /** What it filters, as its label and placeholder ("Filter people by name"). */
  label: string
}>()
const text = defineModel<string>({ required: true })
</script>

<template>
  <UInput
    v-model="text"
    class="filter-field"
    type="search"
    :aria-label="label"
    :placeholder="label"
    :leading-icon="icons.search"
    variant="none"
    :ui="{ base: 'filter-input', trailing: 'pe-1.5' }"
    spellcheck="false"
    autocomplete="off"
    @keydown.esc="text ? ((text = ''), $event.stopPropagation()) : undefined"
  >
    <template v-if="text" #trailing>
      <IconButton
        :icon="icons.close"
        :label="t('common.clearFilter')"
        color="neutral"
        variant="ghost"
        size="xs"
        @click="text = ''"
      />
    </template>
  </UInput>
</template>

<style scoped>
.filter-field {
  width: 100%;
  min-width: 0;
  max-width: 24rem;
  border: 1px solid var(--ui-border);
  border-radius: var(--lumen-r-lg);
  background: var(--lumen-bg-sunken);
  transition:
    background-color var(--lumen-d-base) var(--lumen-ease-out),
    border-color var(--lumen-d-base) var(--lumen-ease-out),
    box-shadow var(--lumen-d-base) var(--lumen-ease-out);
}
.filter-field:hover {
  border-color: var(--ui-border-accented);
}
.filter-field:focus-within {
  border-color: var(--ui-primary);
  background: var(--lumen-bg-raised);
  box-shadow: 0 0 0 4px var(--lumen-primary-soft);
}
.filter-field :deep(.filter-input) {
  height: 2.25rem;
  min-width: 0;
  border-radius: inherit;
  box-shadow: none;
  outline: none;
  color: var(--ui-text-highlighted);
}
.filter-field :deep(input::-webkit-search-cancel-button) {
  display: none;
}
</style>
