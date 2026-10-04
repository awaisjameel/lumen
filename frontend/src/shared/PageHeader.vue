<script setup lang="ts">
// Every page's header: where you are (an optional way back, the title, a
// count), then the page's own tools. The title never pushes the tools out:
// it shortens first, and on narrow windows the tools wrap below it.
import UButton from '@nuxt/ui/components/Button.vue'
import UTooltip from '@nuxt/ui/components/Tooltip.vue'
import type { RouteLocationRaw } from 'vue-router'
import { icons } from '../design/icons'

defineProps<{
  title: string
  /** A count or short context under the title. */
  subtitle?: string
  /** Where the page came from, shown as a back button. */
  back?: { to: RouteLocationRaw; label: string }
}>()
</script>

<template>
  <header class="page-header">
    <UTooltip v-if="back" :text="back.label">
      <UButton
        :to="back.to"
        :icon="icons['arrow-left']"
        :aria-label="back.label"
        color="neutral"
        variant="ghost"
        square
      />
    </UTooltip>
    <slot name="lead" />
    <div class="heading">
      <slot name="title">
        <h1 :title="title">{{ title }}</h1>
      </slot>
      <span v-if="subtitle" class="subtitle">{{ subtitle }}</span>
    </div>
    <div class="tools">
      <slot />
    </div>
  </header>
</template>

<style scoped>
.page-header {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 0.25rem var(--lumen-gap);
  min-height: var(--lumen-topbar-h);
  padding: 0.375rem var(--lumen-page-x);
  flex-shrink: 0;
}
.heading {
  display: flex;
  flex-direction: column;
  justify-content: center;
  min-width: 0;
  flex: 0 1 auto;
}
h1 {
  margin: 0;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
  color: var(--ui-text-highlighted);
  font: 600 1.25rem/1.2 var(--font-display);
}
.subtitle {
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
  color: var(--ui-text-dimmed);
  font-size: 0.75rem;
}
.tools {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  flex: 1 1 auto;
  gap: 0.5rem;
  min-width: 0;
}
</style>
