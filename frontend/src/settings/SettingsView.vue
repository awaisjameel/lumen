<script setup lang="ts">
import { ref } from 'vue'
import { engine } from '../bridge'
import { useLibrary } from '../library/store'
import { errorText, orFallback } from '../shared/errors'
import { t } from '../shared/i18n'
import Icon from '../shared/Icon.vue'
import { theme } from '../shared/theme'
import type { Theme } from '../shared/theme'

const lib = useLibrary()
const error = ref('')

/** Run an engine call, showing any failure. reload refreshes folders and settings after. */
async function run(fn: () => Promise<unknown>, reload = true) {
  error.value = ''
  try {
    await fn()
    if (reload) await lib.reloadSources()
  } catch (e) {
    error.value = errorText(e)
  }
}

async function addFolder() {
  const path = await orFallback(engine.pickFolder(), '')
  if (path) await run(() => engine.addSources([path]))
}

const themes: Theme[] = ['system', 'light', 'dark']
</script>

<template>
  <main class="settings">
    <header class="topbar">
      <h1>{{ t('settings.title') }}</h1>
    </header>
    <div class="content">
      <p v-if="error" class="error">{{ error }}</p>

      <section>
        <h2>{{ t('settings.folders') }}</h2>
        <p class="muted hint">{{ t('settings.foldersHint') }}</p>
        <ul class="list">
          <li v-for="s in lib.sources" :key="s.id">
            <Icon name="folder" :size="18" />
            <span class="path" :title="s.path">{{ s.path }}</span>
            <button class="btn" @click="run(() => engine.removeSource(s.id))">{{ t('settings.remove') }}</button>
          </li>
        </ul>
        <div class="row">
          <button class="btn" @click="addFolder"><Icon name="plus" :size="16" />{{ t('settings.addFolder') }}</button>
          <button class="btn" @click="run(engine.rescan, false)"><Icon name="refresh" :size="16" />{{ t('settings.rescan') }}</button>
        </div>
      </section>

      <section>
        <h2>{{ t('settings.indexing') }}</h2>
        <label class="toggle">
          <input
            type="checkbox"
            :checked="lib.settings.videos"
            @change="run(() => engine.setVideos(($event.target as HTMLInputElement).checked))"
          />
          <span>
            {{ t('settings.videos') }}
            <small class="muted">{{ t('settings.videosHint') }}</small>
          </span>
        </label>
        <label class="toggle">
          <input
            type="checkbox"
            :checked="lib.progress?.paused"
            @change="run(() => engine.setPaused(($event.target as HTMLInputElement).checked), false)"
          />
          <span>{{ t('settings.pause') }}</span>
        </label>
      </section>

      <section>
        <h2>{{ t('settings.appearance') }}</h2>
        <div class="segmented" role="radiogroup" :aria-label="t('settings.theme')">
          <button
            v-for="th in themes"
            :key="th"
            role="radio"
            :aria-checked="theme === th"
            :class="{ on: theme === th }"
            @click="theme = th"
          >
            {{ t(`settings.theme.${th}`) }}
          </button>
        </div>
      </section>

      <section>
        <h2>{{ t('settings.privacy') }}</h2>
        <p class="muted hint"><Icon name="lock" :size="16" /> {{ t('settings.privacyText') }}</p>
      </section>
    </div>
  </main>
</template>

<style scoped>
.settings {
  height: 100%;
  overflow-y: auto;
}
.topbar {
  display: flex;
  align-items: center;
  height: var(--topbar-h);
  padding: 0 20px;
}
h1 {
  margin: 0;
  font: 600 20px/1 var(--font-display);
}
.content {
  max-width: 720px;
  padding: 8px 20px 40px;
}
section {
  margin-bottom: 32px;
}
h2 {
  margin: 0 0 4px;
  font: 600 16px var(--font-display);
}
.hint {
  margin: 0 0 12px;
  display: flex;
  gap: 6px;
  align-items: flex-start;
}
.list {
  list-style: none;
  margin: 0 0 12px;
  padding: 0;
  border: 1px solid var(--border);
  border-radius: var(--radius-l);
  background: var(--surface);
}
.list:empty {
  display: none;
}
.list li {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 10px 10px 14px;
}
.list li + li {
  border-top: 1px solid var(--border);
}
.path {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.row {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}
.toggle {
  display: flex;
  gap: 10px;
  align-items: flex-start;
  padding: 8px 0;
  cursor: pointer;
}
.toggle input {
  accent-color: var(--accent);
  width: 16px;
  height: 16px;
  margin-top: 2px;
}
.toggle small {
  display: block;
}
.segmented {
  display: inline-flex;
  padding: 3px;
  border-radius: var(--radius-m);
  background: var(--surface-2);
}
.segmented button {
  height: 30px;
  padding: 0 14px;
  border: 0;
  border-radius: 6px;
  background: transparent;
  cursor: pointer;
}
.segmented button.on {
  background: var(--surface);
  box-shadow: 0 1px 3px rgb(0 0 0 / 0.12);
  font-weight: 600;
}
.error {
  color: var(--danger);
}
</style>
