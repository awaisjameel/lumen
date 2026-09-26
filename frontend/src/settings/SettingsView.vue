<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import { engine, TAG_CONFIDENCE_MAX, TAG_CONFIDENCE_MIN } from '../bridge'
import { useLibrary } from '../library/store'
import { errorText, logError, orFallback } from '../shared/errors'
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

const aiStatus = computed(() => {
  const ai = lib.progress?.ai
  if (ai?.error) return t('settings.aiError', { error: ai.error })
  if (ai?.device === 'gpu') return t('settings.aiGpu')
  if (ai?.device === 'cpu') return t('settings.aiCpu')
  return t('settings.aiIdle')
})

// Tag confidence on a log scale: probabilities span two orders of magnitude.
// The slider runs from more tags (left) to fewer, surer ones (right).
const span = Math.log(TAG_CONFIDENCE_MAX / TAG_CONFIDENCE_MIN)
const confidencePos = computed(() =>
  Math.round((100 * Math.log(lib.settings.tagConfidence / TAG_CONFIDENCE_MIN)) / span),
)
let saveTimer = 0
function onConfidence(pos: number) {
  lib.settings.tagConfidence = TAG_CONFIDENCE_MIN * Math.exp((span * pos) / 100)
  clearTimeout(saveTimer)
  saveTimer = window.setTimeout(() => {
    engine
      .setTagConfidence(lib.settings.tagConfidence)
      .then(() => lib.refresh())
      .catch((e: unknown) => (error.value = errorText(e)))
  }, 250)
}
onBeforeUnmount(() => clearTimeout(saveTimer))

function setAIOnBattery(on: boolean) {
  lib.settings.aiOnBattery = on
  engine.setAIOnBattery(on).catch((e: unknown) => {
    lib.settings.aiOnBattery = !on
    error.value = errorText(e)
    logError(e)
  })
}
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
        <h2>{{ t('settings.ai') }}</h2>
        <p class="muted hint">{{ t('settings.aiHint') }}</p>
        <p class="ai-status">{{ aiStatus }}</p>
        <p v-if="lib.progress?.ai.facesError" class="ai-status">
          {{ t('settings.peopleError', { error: lib.progress.ai.facesError }) }}
        </p>
        <label class="toggle">
          <input
            type="checkbox"
            :checked="lib.settings.aiOnBattery"
            @change="setAIOnBattery(($event.target as HTMLInputElement).checked)"
          />
          <span>
            {{ t('settings.aiOnBattery') }}
            <small class="muted">{{ t('settings.aiOnBatteryHint') }}</small>
          </span>
        </label>
        <details class="advanced">
          <summary>{{ t('settings.advanced') }}</summary>
          <div class="slider">
            <label for="confidence">{{ t('settings.confidence') }}</label>
            <small class="muted">{{ t('settings.confidenceHint') }}</small>
            <input
              id="confidence"
              type="range"
              min="0"
              max="100"
              :value="confidencePos"
              @input="onConfidence(Number(($event.target as HTMLInputElement).value))"
            />
            <div class="ends muted">
              <span>{{ t('settings.confidenceMore') }}</span>
              <span>{{ t('settings.confidenceFewer') }}</span>
            </div>
          </div>
        </details>
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
.ai-status {
  margin: 0 0 4px;
}
.advanced {
  margin-top: 8px;
}
.advanced summary {
  padding: 6px 0;
  color: var(--text-2);
  cursor: pointer;
}
.slider {
  display: flex;
  flex-direction: column;
  gap: 2px;
  max-width: 420px;
  padding: 8px 0 0;
}
.slider input {
  margin: 8px 0 2px;
  accent-color: var(--accent);
}
.ends {
  display: flex;
  justify-content: space-between;
  font-size: 12px;
}
</style>
