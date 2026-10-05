<script setup lang="ts">
// First run: one question, sensible folders pre-ticked, one button.
import { computed, onMounted, ref } from 'vue'
import { engine } from '../bridge'
import { useLibrary } from '../library/store'
import { errorText, orFallback } from '../shared/errors'
import UButton from '@nuxt/ui/components/Button.vue'
import UCheckbox from '@nuxt/ui/components/Checkbox.vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import FeatureState from '../shared/FeatureState.vue'
import WorkProgress from '../shared/WorkProgress.vue'
import { icons } from '../design/icons'
import { useCommands, runCommand } from '../shared/commands'
import { t } from '../shared/i18n'
import Icon from '../shared/Icon.vue'
import BrandMark from '../shared/BrandMark.vue'

const lib = useLibrary()
const folders = ref<{ path: string; key: string; on: boolean }[]>([])
const loaded = ref(false)
const busy = ref(false)
const error = ref('')

onMounted(async () => {
  try {
    const suggested = await orFallback(engine.suggestedFolders(), [])
    // A folder picked while the suggestions were being found stays.
    for (const path of suggested) await add(path)
  } catch (e) {
    error.value = errorText(e)
  } finally {
    loaded.value = true
  }
})

const chosen = computed(() => folders.value.filter((f) => f.on).map((f) => f.path))

// Use the library's path key, including its Unicode case folding and path
// cleaning. Locale collation also equates some distinct file names.
async function add(path: string) {
  const key = await engine.sourcePathKey(path)
  if (!folders.value.some((f) => f.key === key)) {
    folders.value.push({ path, key, on: true })
  }
}

async function addFolder() {
  error.value = ''
  try {
    const path = await engine.pickFolder()
    if (path) await add(path)
  } catch (e) {
    error.value = errorText(e)
  }
}

async function start() {
  busy.value = true
  error.value = ''
  try {
    await engine.addSources(chosen.value)
    await lib.sourcesChanged() // the library then shows instead of this
  } catch (e) {
    error.value = errorText(e)
  } finally {
    busy.value = false
  }
}
useCommands([
  {
    id: 'welcome.addFolder',
    group: 'library',
    title: () => t('welcome.addFolder'),
    icon: 'folder-add',
    enabled: () => !busy.value,
    run: addFolder,
  },
  {
    id: 'welcome.start',
    group: 'library',
    title: () => t('welcome.start'),
    icon: 'play',
    enabled: () => !!chosen.value.length && !busy.value,
    run: start,
  },
])
</script>

<template>
  <main class="welcome">
    <div class="welcome-content">
      <BrandMark class="logo" />
      <h1>{{ t('welcome.title') }}</h1>
      <p class="question">{{ t('welcome.question') }}</p>
      <FeatureState v-if="!loaded" kind="loading" :title="t('welcome.loading')" />
      <FeatureState v-else-if="!folders.length" kind="empty" icon="folder" :title="t('welcome.none')" />
      <ul v-if="folders.length" class="folders">
        <li v-for="f in folders" :key="f.key">
          <UCheckbox
            :model-value="f.on"
            :disabled="busy"
            :label="f.path"
            :title="f.path"
            @update:model-value="f.on = $event === true"
            ><template #label
              ><span class="folder-label"
                ><Icon name="folder" :size="18" /><span class="path lumen-selectable">{{ f.path }}</span></span
              ></template
            ></UCheckbox
          >
        </li>
      </ul>
      <UButton
        class="add"
        :icon="icons['folder-add']"
        :disabled="busy"
        loading-auto
        @click="runCommand('welcome.addFolder')"
        >{{ t('welcome.addFolder') }}</UButton
      >
      <UAlert v-if="error" :description="error" color="error" variant="subtle" role="alert" />
      <WorkProgress v-if="busy" :label="t('welcome.scanning')" :detail="t('welcome.scanHint')" indeterminate />
      <UButton
        class="start"
        color="primary"
        variant="solid"
        size="lg"
        block
        :disabled="!chosen.length || busy"
        :loading="busy"
        @click="runCommand('welcome.start')"
        >{{ t('welcome.start') }}</UButton
      >
      <p class="privacy"><Icon name="lock" :size="16" />{{ t('welcome.privacy') }}</p>
    </div>
  </main>
</template>
<style scoped>
.welcome {
  display: grid;
  place-items: center;
  height: 100%;
  padding: 1.5rem;
  overflow-y: auto;
  background: var(--ui-bg);
}
.welcome-content {
  width: min(36rem, 100%);
  display: flex;
  flex-direction: column;
  align-items: stretch;
  gap: 1rem;
  min-width: 0;
}
.logo {
  width: 3.5rem;
  height: 3.5rem;
}
h1 {
  margin: 0;
  color: var(--ui-text-highlighted);
  font-size: 1.75rem;
  font-weight: 600;
}
.question {
  margin: 0;
  font-size: 1rem;
  color: var(--ui-text-muted);
}
.folders {
  list-style: none;
  padding: 0;
  margin: 0;
  border: 1px solid var(--ui-border);
  border-radius: var(--ui-radius);
  background: var(--ui-bg-muted);
}
.folders li {
  padding: 0.75rem;
}
.folders li + li {
  border-top: 1px solid var(--ui-border);
}
.folders :deep([data-slot='wrapper']) {
  min-width: 0;
}
.folder-label {
  display: flex;
  align-items: center;
  gap: 0.625rem;
  min-width: 0;
}
.path {
  overflow-wrap: anywhere;
}
.add {
  align-self: flex-start;
}
.privacy {
  display: flex;
  gap: 0.5rem;
  margin: 0;
  color: var(--ui-text-muted);
  font-size: 0.8125rem;
}
</style>
