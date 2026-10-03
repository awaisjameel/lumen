<script setup lang="ts">
// The frame around every page. Wide windows: a sidebar the user can resize
// or collapse. Medium: the sidebar is a rail of icons. Narrow: the page
// takes the whole window and navigation opens as a drawer.
import UButton from '@nuxt/ui/components/Button.vue'
import USlideover from '@nuxt/ui/components/Slideover.vue'
import { computed, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { SIDEBAR_WIDTH_MAX, SIDEBAR_WIDTH_MIN } from '../bridge'
import { icons } from '../design/icons'
import { useCommands } from '../shared/commands'
import { t } from '../shared/i18n'
import { useLayoutMode } from '../shared/layout'
import { usePreferences } from '../shared/preferences'
import ResizeHandle from '../shared/ResizeHandle.vue'
import AppNav from './AppNav.vue'

const mode = useLayoutMode()
const prefs = usePreferences()
const route = useRoute()

const drawer = ref(false)
// Going somewhere puts the drawer away; so does the window growing.
watch([() => route.fullPath, mode], () => (drawer.value = false))

const collapsed = computed(() => mode.value === 'medium' || prefs.prefs.sidebarCollapsed)

function toggleSidebar() {
  if (mode.value === 'narrow') drawer.value = !drawer.value
  else if (mode.value === 'wide') void prefs.set({ sidebarCollapsed: !prefs.prefs.sidebarCollapsed })
}

useCommands([
  {
    id: 'app.toggleSidebar',
    group: 'view',
    title: () => t('shell.toggleSidebar'),
    icon: 'sidebar-collapse',
    keys: 'Ctrl+B',
    available: () => mode.value !== 'medium',
    run: toggleSidebar,
  },
])
</script>

<template>
  <div class="shell" :class="mode">
    <aside
      v-if="mode !== 'narrow'"
      class="sidebar"
      :class="{ collapsed }"
      :style="collapsed ? undefined : { width: `${prefs.prefs.sidebarWidth}px` }"
    >
      <AppNav :collapsed="collapsed" :collapsible="mode === 'wide'" @toggle="toggleSidebar" />
      <ResizeHandle
        v-if="mode === 'wide' && !collapsed"
        edge="right"
        :width="prefs.prefs.sidebarWidth"
        :min="SIDEBAR_WIDTH_MIN"
        :max="SIDEBAR_WIDTH_MAX"
        :label="t('shell.resizeSidebar')"
        @resize="(w) => void prefs.set({ sidebarWidth: w })"
      />
    </aside>

    <div class="main">
      <header v-if="mode === 'narrow'" class="narrow-bar">
        <UButton
          :icon="icons.menu"
          :aria-label="t('shell.openNavigation')"
          :aria-expanded="drawer"
          color="neutral"
          variant="ghost"
          square
          @click="drawer = true"
        />
        <img src="/lumen.png" alt="" />
        <span>{{ t('app.name') }}</span>
      </header>
      <main class="content">
        <slot />
      </main>
    </div>

    <USlideover
      v-if="mode === 'narrow'"
      v-model:open="drawer"
      side="left"
      :title="t('shell.navigation')"
      :description="t('shell.navigationHint')"
      :ui="{ content: 'max-w-64', header: 'sr-only', body: 'p-0 sm:p-0' }"
    >
      <template #body>
        <AppNav />
      </template>
    </USlideover>
  </div>
</template>

<style scoped>
.shell {
  display: flex;
  height: 100%;
  min-width: 0;
}
.sidebar {
  position: relative;
  flex-shrink: 0;
  border-right: 1px solid var(--ui-border);
  background: var(--ui-bg-muted);
}
.sidebar.collapsed {
  width: 3.75rem;
}
.main {
  display: flex;
  flex: 1;
  flex-direction: column;
  min-width: 0;
}
.narrow-bar {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  flex-shrink: 0;
  height: 2.75rem;
  padding: 0 0.5rem;
  border-bottom: 1px solid var(--ui-border);
  color: var(--ui-text-highlighted);
  font: 600 0.9375rem var(--font-display);
}
.narrow-bar img {
  width: 1.25rem;
  height: 1.25rem;
}
.content {
  flex: 1;
  min-width: 0;
  min-height: 0;
  container-type: inline-size;
}
</style>
