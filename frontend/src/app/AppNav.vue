<script setup lang="ts">
// The app's navigation, as the sidebar and the narrow window's drawer both
// show it: the brand, the way into search and commands, where you can go
// (the library, its collections, the tools), what Lumen is doing, and
// Settings. As a rail it is a column of icons whose names appear as
// tooltips and are still read out.
import IconButton from '../shared/IconButton.vue'
import UTooltip from '@nuxt/ui/components/Tooltip.vue'
import { computed } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import { icons } from '../design/icons'
import IndexStatus from '../library/IndexStatus.vue'
import { useLibrary } from '../library/store'
import { availableCommands, capsOf, runCommand } from '../shared/commands'
import { formatNumber, t } from '../shared/i18n'
import BrandMark from '../shared/BrandMark.vue'
import Icon from '../shared/Icon.vue'
import { isActive, navEntries, navSections } from './nav'
import type { NavEntry } from './nav'

const props = defineProps<{
  /** Shown as a rail of icons. */
  collapsed?: boolean
  /** Whether the user may collapse or expand it (the wide layout). */
  collapsible?: boolean
}>()
const emit = defineEmits<{ toggle: [] }>()

const lib = useLibrary()
const route = useRoute()

const PALETTE = 'app.commandPalette'
const canSearch = computed(() => availableCommands.value.some((c) => c.id === PALETTE))
const searchKeys = computed(() => capsOf(PALETTE) ?? [])

const groups = computed(() =>
  navSections
    .map((s) => ({ ...s, entries: navEntries.filter((e) => e.section === s.section) }))
    .filter((g) => g.entries.length),
)
const settings = navEntries.find((e) => e.section === 'app')

/** The count beside a destination, where Lumen keeps one. */
function badge(e: NavEntry): string | undefined {
  if (!lib.countsLoaded) return undefined
  const n =
    e.name === 'library'
      ? lib.counts.photos + lib.counts.videos
      : e.name === 'favourites'
        ? lib.counts.favourites
        : e.name === 'people'
          ? lib.counts.people
          : e.name === 'trash'
            ? lib.counts.trash
            : undefined
  if (n !== undefined) return formatNumber(n)
  return undefined
}

const toggleLabel = computed(() => t(props.collapsed ? 'shell.expandSidebar' : 'shell.collapseSidebar'))
</script>

<template>
  <nav class="app-nav" :class="{ rail: collapsed }" :aria-label="t('shell.navigation')">
    <div class="brand">
      <BrandMark :decorative="!collapsed" />
      <span v-if="!collapsed" class="name">{{ t('app.name') }}</span>
      <IconButton
        v-if="collapsible && !collapsed"
        class="toggle"
        :icon="icons['sidebar-collapse']"
        :label="toggleLabel"
        :kbds="capsOf('app.toggleSidebar')"
        color="neutral"
        variant="ghost"
        size="sm"
        @click="emit('toggle')"
      />
    </div>

    <UTooltip
      v-if="canSearch"
      :text="t('shell.search')"
      :kbds="searchKeys"
      :content="{ side: 'right' }"
      :disabled="!collapsed"
    >
      <button type="button" class="search" :aria-label="t('shell.search')" @click="runCommand(PALETTE)">
        <Icon name="search" :size="16" />
        <span class="search-text">{{ t('shell.search') }}</span>
        <kbd v-if="searchKeys.length" class="keys lumen-kbd" aria-hidden="true">{{ searchKeys.join(' ') }}</kbd>
      </button>
    </UTooltip>

    <!-- Only the destinations scroll: the status card's list opens outside
         the sidebar, which a scrolling ancestor would cut off. -->
    <div class="places">
      <div
        v-for="g in groups"
        :key="g.section"
        class="group"
        role="group"
        :aria-label="g.label ? t(g.label) : undefined"
      >
        <p v-if="g.label && !collapsed" class="heading" aria-hidden="true">{{ t(g.label) }}</p>
        <UTooltip
          v-for="e in g.entries"
          :key="e.name"
          :text="t(e.label)"
          :content="{ side: 'right' }"
          :disabled="!collapsed"
        >
          <RouterLink
            :to="e.path"
            class="item"
            :class="{ active: isActive(e, route.path) }"
            :aria-current="isActive(e, route.path) ? 'page' : undefined"
            :aria-label="collapsed ? t(e.label) : undefined"
          >
            <Icon :name="e.icon" :size="18" />
            <span class="label">{{ t(e.label) }}</span>
            <span v-if="badge(e)" class="count">{{ badge(e) }}</span>
          </RouterLink>
        </UTooltip>
      </div>
    </div>

    <div class="foot">
      <IndexStatus :compact="collapsed" />
      <UTooltip v-if="settings" :text="t(settings.label)" :content="{ side: 'right' }" :disabled="!collapsed">
        <RouterLink
          :to="settings.path"
          class="item"
          :class="{ active: isActive(settings, route.path) }"
          :aria-current="isActive(settings, route.path) ? 'page' : undefined"
          :aria-label="collapsed ? t(settings.label) : undefined"
        >
          <Icon :name="settings.icon" :size="18" />
          <span class="label">{{ t(settings.label) }}</span>
        </RouterLink>
      </UTooltip>
      <IconButton
        v-if="collapsible && collapsed"
        side="right"
        class="expand"
        :icon="icons['sidebar-expand']"
        :label="toggleLabel"
        :kbds="capsOf('app.toggleSidebar')"
        color="neutral"
        variant="ghost"
        @click="emit('toggle')"
      />
    </div>
  </nav>
</template>

<style scoped>
.app-nav {
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
  height: 100%;
  min-height: 0;
  padding: 0.75rem;
}
.rail {
  align-items: center;
  padding: 0.75rem 0.5rem;
}

.brand {
  display: flex;
  align-items: center;
  gap: 0.625rem;
  min-height: 2.5rem;
  padding: 0 0.5rem;
}
.rail .brand {
  justify-content: center;
  padding: 0;
}
.brand .name {
  flex: 1;
  color: var(--ui-text-highlighted);
  font: 600 1rem var(--font-display);
  letter-spacing: -0.01em;
}

/* The way into search and commands: it looks like a field, and opens the
   palette. */
.search {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  width: 100%;
  height: 2.125rem;
  margin: 0.25rem 0 0.5rem;
  padding: 0 0.5rem 0 0.625rem;
  border: 1px solid var(--ui-border);
  border-radius: var(--lumen-r-md);
  background: var(--lumen-bg-raised);
  box-shadow: var(--lumen-shadow-1);
  color: var(--ui-text-dimmed);
  font-size: 0.8125rem;
  text-align: left;
  cursor: pointer;
  transition:
    border-color var(--lumen-d-fast) var(--lumen-ease-out),
    color var(--lumen-d-fast) var(--lumen-ease-out);
}
.search:hover {
  border-color: var(--ui-border-accented);
  color: var(--ui-text-toned);
}
.search-text {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}
.keys {
  flex: none;
}
.rail .search {
  justify-content: center;
  width: 2.5rem;
  height: 2.5rem;
  padding: 0;
}
.rail .search-text,
.rail .keys {
  display: none;
}

.places {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  /* Room for the current item's marker, which sits outside it. */
  margin: 0 -0.75rem;
  padding: 0 0.75rem;
}
.rail .places {
  align-items: center;
  margin: 0 -0.5rem;
  padding: 0 0.5rem;
}
.group {
  display: flex;
  flex-direction: column;
  gap: 1px;
  padding: 0.25rem 0;
}
.rail .group + .group {
  margin-top: 0.25rem;
  padding-top: 0.5rem;
  border-top: 1px solid var(--ui-border);
}
.heading {
  margin: 0;
  padding: 0.75rem 0.625rem 0.25rem;
  color: var(--ui-text-dimmed);
  font-size: 0.6875rem;
  font-weight: 600;
  letter-spacing: 0.06em;
  text-transform: uppercase;
}

.item {
  position: relative;
  display: flex;
  align-items: center;
  gap: 0.625rem;
  width: 100%;
  height: 2.125rem;
  padding: 0 0.625rem;
  border-radius: var(--lumen-r-md);
  color: var(--ui-text-toned);
  font-size: 0.875rem;
  text-decoration: none;
  transition:
    background-color var(--lumen-d-fast) var(--lumen-ease-out),
    color var(--lumen-d-fast) var(--lumen-ease-out);
}
.item:hover {
  background: var(--lumen-bg-hover);
  color: var(--ui-text-highlighted);
}
.item:active {
  background: var(--lumen-bg-active);
}
.item.active {
  background: var(--lumen-bg-raised);
  box-shadow: var(--lumen-shadow-1);
  color: var(--ui-text-highlighted);
  font-weight: 600;
}
:root.dark .item.active {
  background: var(--lumen-bg-active);
  box-shadow: none;
}
/* The current place is marked twice: the raised fill, and the accent bar
   that grows in beside it. */
.item.active::before {
  content: '';
  position: absolute;
  top: 9px;
  bottom: 9px;
  left: -6px;
  width: 3px;
  border-radius: 3px;
  background: var(--ui-primary);
  animation: lumen-grow-y var(--lumen-d-base) var(--lumen-ease-out);
}
.item.active :deep(.icon) {
  color: var(--ui-primary);
}
.label {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}
.count {
  color: var(--ui-text-dimmed);
  font-size: 0.75rem;
  font-weight: 400;
  font-variant-numeric: tabular-nums;
}
.rail .item {
  justify-content: center;
  width: 2.5rem;
  height: 2.5rem;
  padding: 0;
}
.rail .item.active::before {
  left: -8px;
}
.rail .label,
.rail .count {
  display: none;
}

.foot {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
  margin-top: 0.5rem;
}
.rail .foot {
  align-items: center;
}

/* A short window keeps every destination: the brand gives way first. */
@media (max-height: 600px) {
  .app-nav {
    gap: 0;
    padding-block: 0.25rem;
  }
  .brand {
    display: none;
  }
  .item,
  .search {
    height: 2rem;
    flex-shrink: 0;
  }
  .heading {
    padding-block: 0.25rem;
  }
  .group {
    padding-block: 0;
  }
}
</style>
