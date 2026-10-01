<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { RouterLink, RouterView, useRoute } from 'vue-router'
import IndexStatus from './library/IndexStatus.vue'
import { useLibrary } from './library/store'
import Welcome from './onboarding/Welcome.vue'
import ConfirmDialog from './shared/ConfirmDialog.vue'
import BlockDialog from './shared/BlockDialog.vue'
import { errorText } from './shared/errors'
import Icon from './shared/Icon.vue'
import { t } from './shared/i18n'
import Notice from './shared/Notice.vue'

const lib = useLibrary()
const route = useRoute()
const failed = ref('')
onMounted(() => lib.init().catch((e: unknown) => (failed.value = errorText(e))))
</script>

<template>
  <template v-if="lib.ready">
    <Welcome v-if="!lib.sources.length" />

    <div v-else class="shell">
      <nav class="sidebar" :aria-label="t('app.name')">
        <div class="brand">
          <img src="/lumen.png" alt="" />
          <span>{{ t('app.name') }}</span>
        </div>
        <RouterLink to="/" class="nav-item"><Icon name="photos" />{{ t('nav.library') }}</RouterLink>
        <RouterLink to="/people" class="nav-item" :class="{ active: route.path.startsWith('/people') }">
          <Icon name="people" />{{ t('nav.people') }}
        </RouterLink>
        <RouterLink to="/things" class="nav-item" :class="{ active: route.path.startsWith('/things') }">
          <Icon name="things" />{{ t('nav.things') }}
        </RouterLink>
        <RouterLink to="/places" class="nav-item" :class="{ active: route.path.startsWith('/places') }">
          <Icon name="pin" />{{ t('nav.places') }}
        </RouterLink>
        <RouterLink to="/events" class="nav-item" :class="{ active: route.path.startsWith('/events') }">
          <Icon name="calendar" />{{ t('nav.events') }}
        </RouterLink>
        <RouterLink to="/albums" class="nav-item" :class="{ active: route.path.startsWith('/albums') }">
          <Icon name="folder" />{{ t('nav.albums') }}
        </RouterLink>
        <RouterLink to="/duplicates" class="nav-item"><Icon name="copy" />{{ t('nav.duplicates') }}</RouterLink>
        <RouterLink to="/favourites" class="nav-item"><Icon name="heart" />{{ t('nav.favourites') }}</RouterLink>
        <RouterLink v-if="lib.settings.videos" to="/videos" class="nav-item">
          <Icon name="video" />{{ t('nav.videos') }}
        </RouterLink>
        <div class="grow" />
        <IndexStatus />
        <RouterLink to="/settings" class="nav-item" :class="{ active: route.path.startsWith('/settings') }">
          <Icon name="settings" />{{ t('nav.settings') }}
        </RouterLink>
      </nav>
      <main class="content">
        <RouterView />
      </main>
    </div>
  </template>
  <p v-else-if="failed" class="failed" role="alert">{{ failed }}</p>
  <ConfirmDialog />
  <BlockDialog />
  <Notice />
</template>

<style scoped>
.shell {
  display: flex;
  height: 100%;
}
.sidebar {
  width: var(--sidebar-w);
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 12px;
  border-right: 1px solid var(--border);
}
.brand {
  display: flex;
  align-items: center;
  gap: 10px;
  height: 36px;
  padding: 0 8px;
  margin-bottom: 12px;
  font: 600 16px var(--font-display);
}
.brand img {
  width: 24px;
  height: 24px;
}
.nav-item {
  display: flex;
  align-items: center;
  gap: 12px;
  height: 36px;
  padding: 0 10px;
  border-radius: var(--radius-m);
  color: var(--text-2);
  text-decoration: none;
}
.nav-item:hover {
  background: var(--surface-hover);
  color: var(--text);
}
.nav-item.router-link-exact-active,
.nav-item.active {
  background: var(--accent-soft);
  color: var(--accent-on-soft);
  font-weight: 600;
}
.grow {
  flex: 1;
}
.sidebar :deep(.status) {
  margin-bottom: 8px;
}
.content {
  flex: 1;
  min-width: 0;
}
.failed {
  display: grid;
  place-items: center;
  height: 100%;
  margin: 0;
  padding: 24px;
  color: var(--danger);
}
@media (max-width: 900px) {
  .sidebar {
    width: 64px;
    align-items: center;
  }
  .brand span,
  .sidebar :deep(.status),
  .nav-item {
    font-size: 0;
  }
  .sidebar :deep(.status) {
    display: none;
  }
  .nav-item {
    justify-content: center;
    width: 40px;
    padding: 0;
  }
}
</style>
