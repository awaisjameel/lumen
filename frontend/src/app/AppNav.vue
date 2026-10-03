<script setup lang="ts">
// The app's navigation, as the sidebar and the narrow window's drawer both
// show it: the brand, where you can go, what indexing is doing, Settings.
// Collapsed, it is a rail of icons whose names appear as tooltips.
import UButton from '@nuxt/ui/components/Button.vue'
import UNavigationMenu from '@nuxt/ui/components/NavigationMenu.vue'
import type { NavigationMenuItem } from '@nuxt/ui/components/NavigationMenu.vue'
import UTooltip from '@nuxt/ui/components/Tooltip.vue'
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { icons } from '../design/icons'
import IndexStatus from '../library/IndexStatus.vue'
import { useLibrary } from '../library/store'
import { t } from '../shared/i18n'
import { isActive, navEntries } from './nav'
import type { NavEntry } from './nav'

const props = defineProps<{
  collapsed?: boolean
  /** Whether the user may collapse or expand it (the wide layout). */
  collapsible?: boolean
}>()
const emit = defineEmits<{ toggle: [] }>()

const lib = useLibrary()
const route = useRoute()

function item(e: NavEntry): NavigationMenuItem {
  return {
    label: t(e.label),
    icon: icons[e.icon],
    to: e.path,
    active: isActive(e, route.path),
    badge: e.name === 'trash' && lib.trashCount ? lib.trashCount : undefined,
    tooltip: props.collapsed ? { text: t(e.label) } : undefined,
  }
}

function section(name: NavEntry['section']): NavigationMenuItem[] {
  return navEntries.filter((e) => e.section === name && (!e.videos || lib.settings.videos)).map(item)
}

const main = computed(() => [section('library'), section('tools')])
const app = computed(() => [section('app')])
const toggleLabel = computed(() => t(props.collapsed ? 'shell.expandSidebar' : 'shell.collapseSidebar'))
</script>

<template>
  <nav class="app-nav" :class="{ collapsed }" :aria-label="t('shell.navigation')">
    <div class="brand">
      <img src="/lumen.png" alt="" />
      <span v-if="!collapsed" class="name">{{ t('app.name') }}</span>
      <UTooltip v-if="collapsible && !collapsed" :text="toggleLabel">
        <UButton
          class="toggle"
          :icon="icons['sidebar-collapse']"
          :aria-label="toggleLabel"
          color="neutral"
          variant="ghost"
          size="sm"
          square
          @click="emit('toggle')"
        />
      </UTooltip>
    </div>
    <!-- Only the destinations scroll: the status card's list opens outside
         the sidebar, which a scrolling ancestor would cut off. -->
    <div class="places">
      <UNavigationMenu :items="main" orientation="vertical" :collapsed="collapsed" color="primary" tooltip />
    </div>
    <IndexStatus :compact="collapsed" />
    <UNavigationMenu :items="app" orientation="vertical" :collapsed="collapsed" color="primary" tooltip />
    <UTooltip v-if="collapsible && collapsed" :text="toggleLabel" :content="{ side: 'right' }">
      <UButton
        class="expand"
        :icon="icons['sidebar-expand']"
        :aria-label="toggleLabel"
        color="neutral"
        variant="ghost"
        square
        @click="emit('toggle')"
      />
    </UTooltip>
  </nav>
</template>

<style scoped>
.app-nav {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
  height: 100%;
  min-height: 0;
  padding: 0.75rem;
}
.places {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
}
.app-nav.collapsed {
  align-items: center;
  padding: 0.75rem 0.5rem;
}
.brand {
  display: flex;
  align-items: center;
  gap: 0.625rem;
  min-height: 2.25rem;
  padding: 0 0.5rem;
  margin-bottom: 0.25rem;
}
.collapsed .brand {
  padding: 0;
}
.brand img {
  width: 1.5rem;
  height: 1.5rem;
}
.brand .name {
  flex: 1;
  color: var(--ui-text-highlighted);
  font: 600 1rem var(--font-display);
}
</style>
