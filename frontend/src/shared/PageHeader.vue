<script setup lang="ts">
// Every page's top bar: where you are, the search field where the page has
// one, and at most one or two actions for the whole page. Controls that
// act on the content below (filters, sort, zoom) go in the ViewBar under
// it, never here, so the bar never wraps into a jumble. A page inside
// another (a person, an album, the Blocked list) leads with a breadcrumb:
// the parent's name, which goes back to it, then the page's own name.
import UButton from '@nuxt/ui/components/Button.vue'
import type { RouteLocationRaw } from 'vue-router'
import { icons } from '../design/icons'
import { t } from './i18n'

defineProps<{
  title: string
  /** A live count or short context under the title. */
  subtitle?: string
  /** The page this one sits in: its name is shown and goes back to it. */
  back?: { to: RouteLocationRaw; label: string }
}>()
</script>

<template>
  <header class="page-header" :class="{ nested: back }">
    <template v-if="back">
      <UButton
        class="crumb"
        :to="back.to"
        :icon="icons['arrow-left']"
        :aria-label="t('shell.backTo', { place: back.label })"
        color="neutral"
        variant="ghost"
      >
        <span class="crumb-text">{{ back.label }}</span>
      </UButton>
      <span class="slash" aria-hidden="true">/</span>
    </template>
    <slot name="lead" />
    <div class="heading">
      <slot name="title">
        <h1 :title="title">{{ title }}</h1>
      </slot>
      <span v-if="subtitle" class="subtitle" aria-live="polite">{{ subtitle }}</span>
    </div>
    <div v-if="$slots.search" class="search">
      <slot name="search" />
    </div>
    <div v-if="$slots.default" class="tools">
      <slot />
    </div>
  </header>
</template>

<style scoped>
.page-header {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  min-height: var(--lumen-topbar-h);
  padding: 0.75rem var(--lumen-page-x);
  flex-shrink: 0;
}
.heading {
  display: flex;
  flex-direction: column;
  justify-content: center;
  flex: 0 1 auto;
  min-width: 0;
  max-width: 40%;
}
.page-header:not(:has(.search)) .heading {
  max-width: none;
  flex: 1 1 auto;
}
h1 {
  margin: 0;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
  color: var(--ui-text-highlighted);
  font: 600 1.5rem/1.2 var(--font-display);
  letter-spacing: -0.015em;
}
/* Inside a parent, the page's own name is the smaller second crumb: the
   page's hero says it in full. */
.nested h1 {
  font: 600 0.8125rem/1.4 var(--font-sans);
  letter-spacing: 0;
}
.subtitle {
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
  color: var(--ui-text-dimmed);
  font-size: 0.75rem;
  font-variant-numeric: tabular-nums;
}
.crumb {
  flex: none;
  max-width: 12rem;
  padding-inline-start: 0.25rem;
}
.crumb-text {
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}
.slash {
  flex: none;
  color: var(--ui-text-dimmed);
}
/* The search field takes the room between the title and the actions, up
   to a comfortable width, centred in it. */
.search {
  display: flex;
  justify-content: center;
  flex: 1 1 auto;
  min-width: 0;
}
.search > :deep(*) {
  flex: 1 1 auto;
  min-width: 0;
  max-width: 40rem;
}
.tools {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  flex: none;
  gap: 0.5rem;
  margin-left: auto;
}
.page-header:has(.search) .tools {
  margin-left: 0;
}

@container (max-width: 760px) {
  .heading {
    max-width: 42%;
  }
  .crumb {
    max-width: 8rem;
  }
}
</style>
