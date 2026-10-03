<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, toRef } from 'vue'
import AboutSection from '../about/AboutSection.vue'
import OcrSetting from './OcrSetting.vue'
import XmpSetting from './XmpSetting.vue'
import {
  AWAY_MINUTES,
  BlockKind,
  Device,
  engine,
  EVENT_GAP_DEFAULT,
  EVENT_GAP_MAX,
  EVENT_GAP_MIN,
  TAG_CONFIDENCE_MAX,
  TAG_CONFIDENCE_MIN,
  THUMB_CACHE_DEFAULT_MB,
  THUMB_CACHE_MAX_MB,
  THUMB_CACHE_MIN_MB,
} from '../bridge'
import type { Source } from '../bridge'
import { useLibrary } from '../library/store'
import { block } from '../shared/block'
import { confirm } from '../shared/confirm'
import { errorText, logError } from '../shared/errors'
import { bytes } from '../shared/format'
import { t } from '../shared/i18n'
import Icon from '../shared/Icon.vue'
import { theme } from '../shared/theme'
import type { Theme } from '../shared/theme'

const lib = useLibrary()
const error = toRef(lib, 'settingsError')

/** Run an engine call, showing any failure. It reports whether it worked. */
async function run(fn: () => Promise<unknown>): Promise<boolean> {
  error.value = ''
  try {
    await fn()
    return true
  } catch (e) {
    error.value = errorText(e)
    return false
  }
}

/** A checkbox's new state, from its change event. */
function checked(e: Event): boolean {
  return e.target instanceof HTMLInputElement && e.target.checked
}

async function addFolder() {
  error.value = ''
  let path = ''
  if (!(await run(async () => (path = await engine.pickFolder())))) return
  if (path)
    await run(async () => {
      await engine.addSources([path])
      await lib.sourcesChanged()
    })
}

async function removeFolder(s: Source) {
  if (!(await confirm(t('settings.removeTitle', { path: s.path }), t('settings.removeText'), t('settings.remove'))))
    return
  await run(async () => {
    await engine.removeSource(s.id)
    await lib.sourcesChanged()
  })
}

function blockFolder(s: Source) {
  void block(BlockKind.BlockFolder, 0, s.path, false, s.path) // the dialog refreshes what it hides
}

// A number typed in and the slider change faster than they save: each saves
// its newest value, and a failure of the newest puts back the last saved.
function setEventGap(e: Event) {
  if (!(e.target instanceof HTMLInputElement)) return
  const parsed = e.target.valueAsNumber
  const value = Math.min(
    EVENT_GAP_MAX,
    Math.max(EVENT_GAP_MIN, Number.isFinite(parsed) ? Math.round(parsed) : EVENT_GAP_DEFAULT),
  )
  e.target.value = String(value)
  return lib.saveEventGap(value)
}

// The indexing threads: the number follows the slider while it moves, and is
// saved when it is let go.
const threadsDraft = ref<number | null>(null)
const threadsShown = computed(() => threadsDraft.value ?? lib.settings.cpuThreads)
function onThreadsInput(e: Event) {
  if (e.target instanceof HTMLInputElement) threadsDraft.value = e.target.valueAsNumber
}
function setThreads(e: Event) {
  if (!(e.target instanceof HTMLInputElement)) return
  threadsDraft.value = null
  return lib.saveCPUThreads(Math.round(e.target.valueAsNumber))
}

// The cap is shown in GB and kept in MB, which is what the engine holds it in.
const MB_PER_GB = 1024
const cacheCapGB = computed(() => Number((lib.settings.thumbCacheMB / MB_PER_GB).toFixed(2)))
function setCacheCap(e: Event) {
  if (!(e.target instanceof HTMLInputElement)) return
  const parsed = e.target.valueAsNumber
  const mb = Math.min(
    THUMB_CACHE_MAX_MB,
    Math.max(THUMB_CACHE_MIN_MB, Number.isFinite(parsed) ? Math.round(parsed * MB_PER_GB) : THUMB_CACHE_DEFAULT_MB),
  )
  e.target.value = String(Number((mb / MB_PER_GB).toFixed(2)))
  return lib.saveThumbCacheMB(mb)
}

// How much the cache holds now. It grows while photos are indexed and shrinks
// when the cap is passed, so it is read again while the page is open.
const CACHE_SIZE_POLL_MS = 5000
const cacheBytes = ref<number | null>(null)
async function loadCacheSize() {
  try {
    cacheBytes.value = await engine.thumbCacheBytes()
  } catch (e) {
    logError(e)
  }
}
let cacheTimer = 0
onMounted(() => {
  void loadCacheSize()
  cacheTimer = window.setInterval(() => void loadCacheSize(), CACHE_SIZE_POLL_MS)
})
onBeforeUnmount(() => clearInterval(cacheTimer))

const themes: Theme[] = ['system', 'light', 'dark']

/** Arrow keys move the choice, and focus with it, as in any group of radio buttons. */
async function onThemeKey(e: KeyboardEvent) {
  const step = { ArrowRight: 1, ArrowDown: 1, ArrowLeft: -1, ArrowUp: -1 }[e.key]
  const group = e.currentTarget
  if (!step || !(group instanceof HTMLElement)) return
  e.preventDefault()
  const next = themes[(themes.indexOf(theme.value) + step + themes.length) % themes.length]
  if (!next) return
  theme.value = next
  await nextTick() // the newly checked button is the one that can take focus
  group.querySelector<HTMLElement>('[aria-checked="true"]')?.focus()
}

const aiStatus = computed(() => {
  const ai = lib.progress?.ai
  if (ai?.error) return t('settings.aiError', { error: ai.error })
  if (ai?.device === Device.DeviceGPU) return t('settings.aiGpu')
  if (ai?.device === Device.DeviceCPU) return t('settings.aiCpu')
  return t('settings.aiIdle')
})

// Tag confidence on a log scale: probabilities span two orders of magnitude.
// The slider runs from more tags (left) to fewer, surer ones (right).
const span = Math.log(TAG_CONFIDENCE_MAX / TAG_CONFIDENCE_MIN)
const confidencePos = computed(() =>
  Math.round((100 * Math.log(lib.settings.tagConfidence / TAG_CONFIDENCE_MIN)) / span),
)
let saveTimer = 0
let pendingConfidence: number | null = null
function saveConfidence() {
  clearTimeout(saveTimer)
  if (pendingConfidence === null) return
  const value = pendingConfidence
  pendingConfidence = null
  error.value = ''
  void lib.saveTagConfidence(value)
}
function onConfidence(e: Event) {
  if (!(e.target instanceof HTMLInputElement)) return
  lib.intendTagConfidence(TAG_CONFIDENCE_MIN * Math.exp((span * Number(e.target.value)) / 100))
  pendingConfidence = lib.settings.tagConfidence
  clearTimeout(saveTimer)
  saveTimer = window.setTimeout(saveConfidence, 250)
}
onBeforeUnmount(saveConfidence)
</script>

<template>
  <div class="page">
    <header class="topbar">
      <h1>{{ t('settings.title') }}</h1>
    </header>
    <div class="page-body">
      <div class="content">
        <p v-if="error" class="page-error" role="alert">{{ error }}</p>

        <section>
          <h2>{{ t('settings.folders') }}</h2>
          <p class="muted hint">{{ t('settings.foldersHint') }}</p>
          <ul class="list">
            <li v-for="s in lib.sources" :key="s.id">
              <Icon name="folder" :size="18" />
              <span class="path" :title="s.path">{{ s.path }}</span>
              <button class="btn" :aria-label="t('blocked.confirmTitle', { name: s.path })" @click="blockFolder(s)">
                {{ t('blocked.block') }}
              </button>
              <button class="btn" :aria-label="t('settings.removeNamed', { path: s.path })" @click="removeFolder(s)">
                {{ t('settings.remove') }}
              </button>
            </li>
          </ul>
          <div class="row">
            <button class="btn" @click="addFolder"><Icon name="plus" :size="16" />{{ t('settings.addFolder') }}</button>
            <button class="btn" @click="run(engine.rescan)">
              <Icon name="refresh" :size="16" />{{ t('settings.rescan') }}
            </button>
            <RouterLink to="/settings/organise" class="btn">{{ t('organise.link') }}</RouterLink>
          </div>
        </section>

        <section>
          <h2>{{ t('settings.indexing') }}</h2>
          <label class="toggle">
            <input type="checkbox" :checked="lib.settings.videos" @change="lib.saveVideos(checked($event))" />
            <span>
              {{ t('settings.videos') }}
              <small class="muted">{{ t('settings.videosHint') }}</small>
            </span>
          </label>
          <label class="toggle">
            <input type="checkbox" :checked="lib.progress?.paused" @change="lib.setPaused(checked($event))" />
            <span>{{ t('settings.pause') }}</span>
          </label>
          <label class="toggle">
            <input
              type="checkbox"
              :checked="lib.settings.pauseFullScreen"
              @change="lib.savePauseFullScreen(checked($event))"
            />
            <span>
              {{ t('settings.pauseFullScreen') }}
              <small class="muted">{{ t('settings.pauseFullScreenHint') }}</small>
            </span>
          </label>
          <label class="toggle">
            <input type="checkbox" :checked="lib.settings.pauseBusy" @change="lib.savePauseBusy(checked($event))" />
            <span>
              {{ t('settings.pauseBusy') }}
              <small class="muted">{{ t('settings.pauseBusyHint') }}</small>
            </span>
          </label>
          <div v-if="lib.settings.maxCPUThreads > 1" class="slider">
            <label for="cpu-threads">{{
              t('settings.threads', { n: threadsShown, max: lib.settings.maxCPUThreads })
            }}</label>
            <small class="muted">{{ t('settings.threadsHint', { n: lib.settings.defaultCPUThreads }) }}</small>
            <input
              id="cpu-threads"
              type="range"
              min="1"
              :max="lib.settings.maxCPUThreads"
              step="1"
              :value="threadsShown"
              @input="onThreadsInput"
              @change="setThreads"
            />
            <div class="ends muted">
              <span>{{ t('settings.threadsFewer') }}</span>
              <span>{{ t('settings.threadsMore') }}</span>
            </div>
          </div>
          <details class="advanced">
            <summary>{{ t('settings.advanced') }}</summary>
            <label class="toggle">
              <input type="checkbox" :checked="lib.settings.idleOnly" @change="lib.saveIdleOnly(checked($event))" />
              <span>
                {{ t('settings.idleOnly') }}
                <small class="muted">{{ t('settings.idleOnlyHint', { n: AWAY_MINUTES }) }}</small>
              </span>
            </label>
          </details>
        </section>

        <section>
          <h2>{{ t('settings.ai') }}</h2>
          <p class="muted hint">{{ t('settings.aiHint') }}</p>
          <p class="ai-status">{{ aiStatus }}</p>
          <p v-if="lib.progress?.ai.facesError" class="ai-status">
            {{ t('settings.peopleError', { error: lib.progress.ai.facesError }) }}
          </p>
          <label class="toggle">
            <input type="checkbox" :checked="lib.settings.aiOnBattery" @change="lib.saveAIOnBattery(checked($event))" />
            <span>
              {{ t('settings.aiOnBattery') }}
              <small class="muted">{{ t('settings.aiOnBatteryHint') }}</small>
            </span>
          </label>
          <OcrSetting />
          <details class="advanced">
            <summary>{{ t('settings.advanced') }}</summary>
            <label class="event-gap">
              {{ t('settings.eventGap') }}
              <small class="muted">{{ t('settings.eventGapHint') }}</small>
              <input
                type="number"
                :min="EVENT_GAP_MIN"
                :max="EVENT_GAP_MAX"
                step="1"
                class="field"
                :value="lib.settings.eventGapHours"
                @change="setEventGap"
              />
            </label>
            <div class="slider">
              <label for="confidence">{{ t('settings.confidence') }}</label>
              <small class="muted">{{ t('settings.confidenceHint') }}</small>
              <input id="confidence" type="range" min="0" max="100" :value="confidencePos" @input="onConfidence" />
              <div class="ends muted">
                <span>{{ t('settings.confidenceMore') }}</span>
                <span>{{ t('settings.confidenceFewer') }}</span>
              </div>
            </div>
          </details>
        </section>

        <section>
          <h2>{{ t('settings.xmpTitle') }}</h2>
          <XmpSetting />
        </section>

        <section>
          <h2>{{ t('settings.cache') }}</h2>
          <p class="muted hint">{{ t('settings.cacheHint') }}</p>
          <label class="cache-cap">
            {{ t('settings.cacheLimit') }}
            <input
              type="number"
              :min="THUMB_CACHE_MIN_MB / MB_PER_GB"
              :max="THUMB_CACHE_MAX_MB / MB_PER_GB"
              step="0.25"
              class="field"
              :value="cacheCapGB"
              @change="setCacheCap"
            />
          </label>
          <p v-if="cacheBytes !== null" class="cache-used muted">
            {{ t('settings.cacheUsed', { size: bytes(cacheBytes) }) }}
          </p>
        </section>

        <section>
          <h2>{{ t('settings.appearance') }}</h2>
          <div class="segmented" role="radiogroup" :aria-label="t('settings.theme')" @keydown="onThemeKey">
            <button
              v-for="th in themes"
              :key="th"
              role="radio"
              :aria-checked="theme === th"
              :tabindex="theme === th ? 0 : -1"
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
          <RouterLink to="/settings/blocked" class="btn">{{ t('blocked.title') }}</RouterLink>
        </section>

        <section>
          <h2>{{ t('about.title') }}</h2>
          <AboutSection />
        </section>
      </div>
    </div>
  </div>
</template>

<style scoped>
.event-gap {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 4px;
  margin: 16px 0;
}

.event-gap input {
  width: 80px;
}

.cache-cap {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 4px;
  margin: 0 0 8px;
}

.cache-cap input {
  width: 80px;
}

.cache-used {
  margin: 0;
}

.content {
  max-width: 720px;
  padding: 4px 0 16px;
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
  border-radius: var(--radius-m);
  background: transparent;
  cursor: pointer;
}

.segmented button.on {
  background: var(--surface);
  box-shadow: 0 1px 3px rgb(0 0 0 / 0.12);
  font-weight: 600;
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
