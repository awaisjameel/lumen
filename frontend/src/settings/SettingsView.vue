<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, toRef } from 'vue'
import AboutSection from '../about/AboutSection.vue'
import OcrSetting from './OcrSetting.vue'
import XmpSetting from './XmpSetting.vue'
import ViewingSettings from './ViewingSettings.vue'
import {
  Accent,
  Density,
  TEXT_SCALE_MIN,
  TEXT_SCALE_MAX,
  TEXT_SCALE_DEFAULT,
  AWAY_MINUTES,
  BlockKind,
  Device,
  engine,
  EVENT_GAP_DEFAULT,
  EVENT_GAP_MAX,
  EVENT_GAP_MIN,
  TAG_CONFIDENCE_MAX,
  TAG_CONFIDENCE_MIN,
  Theme,
  THUMB_CACHE_DEFAULT_MB,
  THUMB_CACHE_MAX_MB,
  THUMB_CACHE_MIN_MB,
} from '../bridge'
import type { Settings, Source } from '../bridge'
import { useLibrary } from '../library/store'
import { block } from '../shared/block'
import { confirm } from '../shared/confirm'
import { errorText, logError } from '../shared/errors'
import { bytes } from '../shared/format'
import { t } from '../shared/i18n'
import { icons } from '../design/icons'
import Page from '../shared/Page.vue'
import PageHeader from '../shared/PageHeader.vue'
import PrefRow from '../shared/PrefRow.vue'
import FeatureState from '../shared/FeatureState.vue'
import { useCommands, runCommand } from '../shared/commands'
import type { MessageKey } from '../shared/i18n'
import UButton from '@nuxt/ui/components/Button.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import USwitch from '@nuxt/ui/components/Switch.vue'
import USlider from '@nuxt/ui/components/Slider.vue'
import URadioGroup from '@nuxt/ui/components/RadioGroup.vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import { usePreferences } from '../shared/preferences'
import { useSaveFeedback } from './saveFeedback'

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
  validation.value.eventGapHours =
    parsed !== value ? t('settings.eventGapRange', { min: EVENT_GAP_MIN, max: EVENT_GAP_MAX, n: value }) : ''
  e.target.value = String(value)
  return save('eventGapHours', () => lib.saveEventGap(value))
}

// The indexing threads: the number follows the slider while it moves, and is
// saved when it is let go.
const threadsDraft = ref<number | null>(null)
const threadsShown = computed(() => threadsDraft.value ?? lib.settings.cpuThreads)
function onThreadsInput(value: number | number[] | undefined) {
  if (typeof value === 'number') threadsDraft.value = value
}
function setThreads() {
  const value = threadsShown.value
  threadsDraft.value = null
  return save('cpuThreads', () => lib.saveCPUThreads(Math.round(value)))
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
  validation.value.thumbCacheMB =
    !Number.isFinite(parsed) || Math.round(parsed * MB_PER_GB) !== mb
      ? t('settings.cacheRange', {
          min: THUMB_CACHE_MIN_MB / MB_PER_GB,
          max: THUMB_CACHE_MAX_MB / MB_PER_GB,
          n: Number((mb / MB_PER_GB).toFixed(2)),
        })
      : ''
  e.target.value = String(Number((mb / MB_PER_GB).toFixed(2)))
  return save('thumbCacheMB', () => lib.saveThumbCacheMB(mb))
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

const prefs = usePreferences()
const themes = [Theme.ThemeSystem, Theme.ThemeLight, Theme.ThemeDark] as const
const accents = [
  Accent.AccentBlue,
  Accent.AccentTeal,
  Accent.AccentGreen,
  Accent.AccentViolet,
  Accent.AccentRose,
  Accent.AccentAmber,
] as const
const densities = [Density.DensityComfortable, Density.DensityCompact] as const
const themeItems = computed(() => themes.map((value) => ({ value, label: t(`settings.theme.${value}`) })))
const accentItems = computed(() => accents.map((value) => ({ value, label: t(`settings.accent.${value}`) })))
const densityItems = computed(() => densities.map((value) => ({ value, label: t(`settings.density.${value}`) })))
function setTheme(value: unknown) {
  for (const theme of themes) if (theme === value) void prefs.set({ theme })
}
// The previous theme group also accepted vertical arrows; retain them beside the library's horizontal keys.
async function onThemeKey(event: KeyboardEvent) {
  const step = event.key === 'ArrowDown' ? 1 : event.key === 'ArrowUp' ? -1 : 0
  const group = event.currentTarget
  if (!step || !(group instanceof HTMLElement)) return
  event.preventDefault()
  const index = themes.findIndex((value) => value === prefs.prefs.theme)
  const theme = themes[(index + step + themes.length) % themes.length]
  if (!theme) return
  await prefs.set({ theme })
  await nextTick()
  group.querySelector<HTMLElement>('[aria-checked="true"]')?.focus()
}
function setAccent(value: unknown) {
  for (const accent of accents) if (accent === value) void prefs.set({ accent })
}
function setDensity(value: unknown) {
  for (const density of densities) if (density === value) void prefs.set({ density })
}
function setTextScale(value: number | number[] | undefined) {
  if (typeof value === 'number') void prefs.set({ textScale: value })
}
const query = ref('')
const category = ref('')

function clearSearch() {
  query.value = ''
  category.value = ''
}
const categories = [
  { id: 'viewing', label: 'viewer.settings' },
  { id: 'appearance', label: 'settings.appearance' },
  { id: 'library', label: 'settings.category.library' },
  { id: 'indexing', label: 'settings.category.indexing' },
  { id: 'search', label: 'settings.category.search' },
  { id: 'files', label: 'settings.category.files' },
  { id: 'privacy', label: 'settings.privacy' },
  { id: 'about', label: 'about.title' },
] as const
function matches(...texts: string[]) {
  const needle = query.value.trim().toLocaleLowerCase()
  return !needle || texts.some((text) => text.toLocaleLowerCase().includes(needle))
}
function row(group: string, ...keys: MessageKey[]) {
  return (!category.value || category.value === group) && matches(...keys.map((key) => t(key)))
}
// The folders row is found by its actions' names too.
const folderKeys: readonly MessageKey[] = [
  'settings.folders',
  'settings.foldersHint',
  'settings.addFolder',
  'settings.rescan',
]
const showFolders = computed(() => row('library', ...folderKeys))
const validation = ref<Partial<Record<keyof Settings, string>>>({})
const { save, hint } = useSaveFeedback<keyof Settings>((key) => lib.settingErrors[key])
useCommands([
  {
    id: 'settings.addFolder',
    group: 'library',
    title: () => t('settings.addFolder'),
    icon: 'folder-add',
    run: addFolder,
  },
  {
    id: 'settings.rescan',
    group: 'library',
    title: () => t('settings.rescan'),
    icon: 'refresh',
    run: () => run(engine.rescan),
  },
])

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
  void save('tagConfidence', () => lib.saveTagConfidence(value))
}
function onConfidence(value: number | number[] | undefined) {
  if (typeof value !== 'number') return
  lib.intendTagConfidence(TAG_CONFIDENCE_MIN * Math.exp((span * value) / 100))
  pendingConfidence = lib.settings.tagConfidence
  clearTimeout(saveTimer)
  saveTimer = window.setTimeout(saveConfidence, 250)
}
onBeforeUnmount(saveConfidence)
// Filtering keeps long-running child operations mounted so their busy state and results survive.
const categoryKeys: Record<string, MessageKey[]> = {
  viewing: [
    'viewer.settings',
    'viewer.filmstrip',
    'viewer.autoHide',
    'viewer.interval',
    'viewer.loop',
    'viewer.autoplay',
    'viewer.volume',
    'viewer.skipInterval',
    'viewer.speed',
    'viewer.resume',
    'viewer.resumeHint',
    'viewer.clearHistory',
  ],
  appearance: [
    'settings.theme',
    'settings.themeHint',
    'settings.accent',
    'settings.accentHint',
    'settings.density',
    'settings.densityHint',
    'settings.textSize',
    'settings.textSizeHint',
  ],
  library: [...folderKeys, 'settings.videos', 'settings.videosHint'],
  indexing: [
    'settings.pause',
    'settings.pauseHint',
    'settings.pauseFullScreen',
    'settings.pauseFullScreenHint',
    'settings.pauseBusy',
    'settings.pauseBusyHint',
    'settings.idleOnly',
    'settings.idleOnlyHint',
    'settings.threadsHint',
    'settings.threadsLabel',
    'settings.cache',
    'settings.cacheLimit',
    'settings.cacheHint',
  ],
  search: [
    'settings.aiOnBattery',
    'settings.aiOnBatteryHint',
    'settings.ocr',
    'settings.ocrHint',
    'settings.ai',
    'settings.aiHint',
    'settings.confidence',
    'settings.confidenceHint',
  ],
  files: [
    'settings.eventGap',
    'settings.eventGapHint',
    'settings.xmp',
    'settings.xmpHint',
    'settings.xmpDelete',
    'settings.xmpDeleteHint',
    'settings.xmpExport',
    'settings.xmpExportHint',
    'settings.organise',
    'settings.organiseHint',
  ],
  privacy: ['settings.privacy', 'settings.privacyText'],
  about: [
    'about.title',
    'about.openLogs',
    'about.saveReport',
    'about.licences',
    'about.reportHint',
    'about.updates',
    'about.releases',
    'about.logsHint',
    'about.licencesHint',
    'about.geonames',
    'about.places',
    'about.versionLabel',
  ],
}
const shownCategories = computed(() =>
  categories.filter(
    (item) =>
      (!category.value || category.value === item.id) && (categoryKeys[item.id] ?? []).some((key) => matches(t(key))),
  ),
)
</script>

<template>
  <Page>
    <template #header>
      <PageHeader :title="t('settings.title')">
        <UInput
          v-model="query"
          :icon="icons.search"
          :placeholder="t('settings.search')"
          :aria-label="t('settings.search')"
          type="search"
        />
      </PageHeader>
    </template>
    <div class="settings-layout">
      <nav class="category-nav" :aria-label="t('settings.categories')">
        <UButton :aria-pressed="category === ''" :variant="category === '' ? 'soft' : 'ghost'" @click="category = ''">{{
          t('settings.all')
        }}</UButton>
        <UButton
          v-for="item in categories"
          :key="item.id"
          :aria-pressed="category === item.id"
          :variant="category === item.id ? 'soft' : 'ghost'"
          @click="category = item.id"
          >{{ t(item.label) }}</UButton
        >
      </nav>
      <div class="settings-content">
        <UAlert v-if="error" :description="error" color="error" variant="subtle" role="alert" />
        <FeatureState
          v-if="!shownCategories.length"
          kind="empty"
          icon="search"
          :title="t('settings.noResults')"
          :text="t('settings.noResultsHint')"
        >
          <UButton @click="clearSearch">{{ t('settings.clearSearch') }}</UButton>
        </FeatureState>
        <section
          v-show="shownCategories.some((item) => item.id === 'appearance')"
          data-category="appearance"
          :aria-label="t('settings.appearance')"
        >
          <h2>{{ t('settings.appearance') }}</h2>
          <PrefRow
            v-if="row('appearance', 'settings.theme', 'settings.themeHint')"
            :label="t('settings.theme')"
            :description="t('settings.themeHint')"
          >
            <template #default="{ labelledBy, describedBy }">
              <URadioGroup
                data-appearance="theme"
                :model-value="themes.find((value) => value === prefs.prefs.theme)"
                :items="themeItems"
                orientation="horizontal"
                class="appearance-choices"
                :aria-labelledby="labelledBy"
                :aria-describedby="describedBy"
                @update:model-value="setTheme"
                @keydown="onThemeKey"
              >
              </URadioGroup>
            </template>
          </PrefRow>
          <PrefRow
            v-if="row('appearance', 'settings.accent', 'settings.accentHint')"
            :label="t('settings.accent')"
            :description="t('settings.accentHint')"
          >
            <template #default="{ labelledBy, describedBy }">
              <URadioGroup
                data-appearance="accent"
                :model-value="accents.find((value) => value === prefs.prefs.accent)"
                :items="accentItems"
                orientation="horizontal"
                class="appearance-choices"
                :aria-labelledby="labelledBy"
                :aria-describedby="describedBy"
                @update:model-value="setAccent"
              >
                <template #label="{ item }">
                  <span
                    class="swatch"
                    :style="{ background: `var(--lumen-accent-${item.value})` }"
                    aria-hidden="true"
                  />{{ item.label }}</template
                >
              </URadioGroup>
            </template>
          </PrefRow>
          <PrefRow
            v-if="row('appearance', 'settings.density', 'settings.densityHint')"
            :label="t('settings.density')"
            :description="t('settings.densityHint')"
          >
            <template #default="{ labelledBy, describedBy }">
              <URadioGroup
                data-appearance="density"
                :model-value="densities.find((value) => value === prefs.prefs.density)"
                :items="densityItems"
                orientation="horizontal"
                class="appearance-choices"
                :aria-labelledby="labelledBy"
                :aria-describedby="describedBy"
                @update:model-value="setDensity"
              >
              </URadioGroup>
            </template>
          </PrefRow>
          <PrefRow
            v-if="row('appearance', 'settings.textSize', 'settings.textSizeHint')"
            :label="t('settings.textSize')"
            :description="t('settings.textSizeHint')"
          >
            <template #default="{ labelledBy, describedBy }">
              <div class="scale-controls">
                <USlider
                  data-appearance="textScale"
                  :model-value="prefs.prefs.textScale"
                  :min="TEXT_SCALE_MIN"
                  :max="TEXT_SCALE_MAX"
                  :step="5"
                  :aria-labelledby="labelledBy"
                  :aria-describedby="describedBy"
                  @update:model-value="setTextScale"
                />
                <span>{{ t('settings.textPercent', { n: prefs.prefs.textScale }) }}</span>
                <UButton
                  :icon="icons.reset"
                  :disabled="prefs.prefs.textScale === TEXT_SCALE_DEFAULT"
                  @click="prefs.set({ textScale: TEXT_SCALE_DEFAULT })"
                  >{{ t('settings.resetText') }}</UButton
                >
              </div>
            </template>
          </PrefRow>
          <p v-if="prefs.saving" role="status" class="text-muted">{{ t('settings.saving') }}</p>
        </section>
        <section
          v-show="shownCategories.some((item) => item.id === 'library')"
          data-category="library"
          :aria-label="t('settings.category.library')"
        >
          <h2>{{ t('settings.category.library') }}</h2>
          <PrefRow v-if="showFolders" :label="t('settings.folders')" :description="t('settings.foldersHint')">
            <template #default>
              <div class="flex flex-wrap gap-2">
                <UButton :icon="icons['folder-add']" loading-auto @click="runCommand('settings.addFolder')">{{
                  t('settings.addFolder')
                }}</UButton>
                <UButton :icon="icons.refresh" loading-auto @click="runCommand('settings.rescan')">{{
                  t('settings.rescan')
                }}</UButton>
              </div>
            </template>
          </PrefRow>
          <FeatureState
            v-if="!lib.sources.length && showFolders"
            kind="empty"
            icon="folder"
            :title="t('settings.noFolders')"
            :text="t('settings.noFoldersHint')"
          />
          <ul v-if="lib.sources.length && showFolders" class="source-list">
            <li v-for="s in lib.sources" :key="s.id">
              <span class="source-path lumen-selectable" :title="s.path">{{ s.path }}</span>
              <UButton
                :icon="icons.blocked"
                :aria-label="t('blocked.confirmTitle', { name: s.path })"
                @click="blockFolder(s)"
                >{{ t('blocked.block') }}</UButton
              >
              <UButton loading-auto :aria-label="t('settings.removeNamed', { path: s.path })" @click="removeFolder(s)">
                {{ t('settings.remove') }}</UButton
              >
            </li>
          </ul>
          <PrefRow
            v-if="row('library', 'settings.videos', 'settings.videosHint')"
            :label="t('settings.videos')"
            :description="t('settings.videosHint')"
            :hint="hint('videos')"
            :error="lib.settingErrors.videos"
          >
            <template #default="{ labelledBy, describedBy }">
              <USwitch
                :model-value="lib.settings.videos"
                :aria-labelledby="labelledBy"
                :aria-describedby="describedBy"
                @update:model-value="(value) => save('videos', () => lib.saveVideos(value))"
              />
            </template>
          </PrefRow>
        </section>
        <section
          v-show="shownCategories.some((item) => item.id === 'indexing')"
          data-category="indexing"
          :aria-label="t('settings.category.indexing')"
        >
          <h2>{{ t('settings.category.indexing') }}</h2>
          <PrefRow
            v-if="row('indexing', 'settings.pause', 'settings.pauseHint')"
            :label="t('settings.pause')"
            :description="t('settings.pauseHint')"
          >
            <!-- The indexer's state, not a setting: a failure is a notice, as from the sidebar. -->
            <template #default="{ labelledBy, describedBy }">
              <USwitch
                :model-value="lib.progress?.paused ?? false"
                :aria-labelledby="labelledBy"
                :aria-describedby="describedBy"
                @update:model-value="(value) => void lib.setPaused(value)"
              />
            </template>
          </PrefRow>
          <PrefRow
            v-if="row('indexing', 'settings.pauseFullScreen', 'settings.pauseFullScreenHint')"
            :label="t('settings.pauseFullScreen')"
            :description="t('settings.pauseFullScreenHint')"
            :hint="hint('pauseFullScreen')"
            :error="lib.settingErrors.pauseFullScreen"
          >
            <template #default="{ labelledBy, describedBy }">
              <USwitch
                :model-value="lib.settings.pauseFullScreen"
                :aria-labelledby="labelledBy"
                :aria-describedby="describedBy"
                @update:model-value="(value) => save('pauseFullScreen', () => lib.savePauseFullScreen(value))"
              />
            </template>
          </PrefRow>
          <PrefRow
            v-if="row('indexing', 'settings.pauseBusy', 'settings.pauseBusyHint')"
            :label="t('settings.pauseBusy')"
            :description="t('settings.pauseBusyHint')"
            :hint="hint('pauseBusy')"
            :error="lib.settingErrors.pauseBusy"
          >
            <template #default="{ labelledBy, describedBy }">
              <USwitch
                :model-value="lib.settings.pauseBusy"
                :aria-labelledby="labelledBy"
                :aria-describedby="describedBy"
                @update:model-value="(value) => save('pauseBusy', () => lib.savePauseBusy(value))"
              />
            </template>
          </PrefRow>
          <PrefRow
            v-if="row('indexing', 'settings.idleOnly', 'settings.idleOnlyHint')"
            :label="t('settings.idleOnly')"
            :hint="hint('idleOnly')"
            :error="lib.settingErrors.idleOnly"
            :description="t('settings.idleOnlyHint', { n: AWAY_MINUTES })"
          >
            <template #default="{ labelledBy, describedBy }">
              <USwitch
                :model-value="lib.settings.idleOnly"
                :aria-labelledby="labelledBy"
                :aria-describedby="describedBy"
                @update:model-value="(value) => save('idleOnly', () => lib.saveIdleOnly(value))"
              />
            </template>
          </PrefRow>
          <PrefRow
            v-if="row('indexing', 'settings.threadsLabel', 'settings.threadsHint') && lib.settings.maxCPUThreads > 1"
            :label="t('settings.threadsLabel')"
            :hint="hint('cpuThreads')"
            :error="lib.settingErrors.cpuThreads"
            :description="t('settings.threadsHint', { n: lib.settings.defaultCPUThreads })"
          >
            <template #default="{ labelledBy, describedBy }">
              <div class="slider-controls">
                <USlider
                  id="cpu-threads"
                  :model-value="threadsShown"
                  :min="1"
                  :max="lib.settings.maxCPUThreads"
                  :step="1"
                  :aria-labelledby="labelledBy"
                  :aria-describedby="describedBy"
                  @update:model-value="onThreadsInput"
                  @change="setThreads"
                />
                <div class="slider-ends">
                  <span>{{ t('settings.threadsFewer') }}</span>
                  <span>{{ t('settings.threadsMore') }}</span>
                </div>
              </div>
            </template>
          </PrefRow>
          <p v-if="lib.settings.maxCPUThreads > 1 && row('indexing', 'settings.threadsLabel', 'settings.threadsHint')">
            {{ t('settings.threads', { n: threadsShown, max: lib.settings.maxCPUThreads }) }}
          </p>
          <PrefRow
            v-if="row('indexing', 'settings.cache', 'settings.cacheLimit', 'settings.cacheHint')"
            :label="t('settings.cache')"
            :description="t('settings.cacheHint')"
            :hint="hint('thumbCacheMB', validation.thumbCacheMB) || t('settings.cacheLimit')"
            :error="lib.settingErrors.thumbCacheMB"
          >
            <template #default="{ labelledBy, describedBy }">
              <UInput
                type="number"
                class="cache-cap w-28"
                :min="THUMB_CACHE_MIN_MB / MB_PER_GB"
                :max="THUMB_CACHE_MAX_MB / MB_PER_GB"
                step="0.25"
                :model-value="cacheCapGB"
                :aria-labelledby="labelledBy"
                :aria-describedby="describedBy"
                @change="setCacheCap"
              />
            </template>
          </PrefRow>
          <p
            v-if="cacheBytes !== null && row('indexing', 'settings.cache', 'settings.cacheLimit', 'settings.cacheHint')"
            class="cache-used text-muted"
          >
            {{ t('settings.cacheUsed', { size: bytes(cacheBytes) }) }}
          </p>
        </section>
        <section
          v-show="shownCategories.some((item) => item.id === 'search')"
          data-category="search"
          :aria-label="t('settings.category.search')"
        >
          <h2>{{ t('settings.category.search') }}</h2>
          <p v-if="row('search', 'settings.ai', 'settings.aiHint')" class="text-muted">{{ t('settings.aiHint') }}</p>
          <p>{{ aiStatus }}</p>
          <UAlert
            v-if="lib.progress?.ai.facesError"
            :description="t('settings.peopleError', { error: lib.progress.ai.facesError })"
            color="error"
            variant="subtle"
          />
          <PrefRow
            v-if="row('search', 'settings.aiOnBattery', 'settings.aiOnBatteryHint')"
            :label="t('settings.aiOnBattery')"
            :description="t('settings.aiOnBatteryHint')"
            :hint="hint('aiOnBattery')"
            :error="lib.settingErrors.aiOnBattery"
          >
            <template #default="{ labelledBy, describedBy }">
              <USwitch
                :model-value="lib.settings.aiOnBattery"
                :aria-labelledby="labelledBy"
                :aria-describedby="describedBy"
                @update:model-value="(value) => save('aiOnBattery', () => lib.saveAIOnBattery(value))"
              />
            </template>
          </PrefRow>
          <OcrSetting v-show="row('search', 'settings.ocr', 'settings.ocrHint')" />
          <PrefRow
            v-if="row('search', 'settings.confidence', 'settings.confidenceHint')"
            :label="t('settings.confidence')"
            :description="t('settings.confidenceHint')"
            :hint="hint('tagConfidence')"
            :error="lib.settingErrors.tagConfidence"
          >
            <template #default="{ labelledBy, describedBy }">
              <div class="slider-controls">
                <USlider
                  id="confidence"
                  :model-value="confidencePos"
                  :min="0"
                  :max="100"
                  :aria-labelledby="labelledBy"
                  :aria-describedby="describedBy"
                  @update:model-value="onConfidence"
                />
                <div class="slider-ends">
                  <span>{{ t('settings.confidenceMore') }}</span>
                  <span>{{ t('settings.confidenceFewer') }}</span>
                </div>
              </div>
            </template>
          </PrefRow>
        </section>
        <section
          v-show="shownCategories.some((item) => item.id === 'files')"
          data-category="files"
          :aria-label="t('settings.category.files')"
        >
          <h2>{{ t('settings.category.files') }}</h2>
          <PrefRow
            v-if="row('files', 'settings.eventGap', 'settings.eventGapHint')"
            :label="t('settings.eventGap')"
            :description="t('settings.eventGapHint')"
            :hint="hint('eventGapHours', validation.eventGapHours)"
            :error="lib.settingErrors.eventGapHours"
          >
            <template #default="{ labelledBy, describedBy }">
              <UInput
                type="number"
                class="event-gap w-28"
                :min="EVENT_GAP_MIN"
                :max="EVENT_GAP_MAX"
                step="1"
                :model-value="lib.settings.eventGapHours"
                :aria-labelledby="labelledBy"
                :aria-describedby="describedBy"
                @change="setEventGap"
              />
            </template>
          </PrefRow>
          <XmpSetting
            v-show="
              row(
                'files',
                'settings.xmp',
                'settings.xmpHint',
                'settings.xmpDelete',
                'settings.xmpDeleteHint',
                'settings.xmpExport',
                'settings.xmpExportHint',
              )
            "
            :query="query"
          />
          <PrefRow
            v-if="row('files', 'settings.organise', 'settings.organiseHint')"
            :label="t('settings.organise')"
            :description="t('settings.organiseHint')"
          >
            <template #default>
              <UButton to="/settings/organise" :icon="icons.organise">{{ t('organise.link') }}</UButton>
            </template>
          </PrefRow>
        </section>
        <section
          v-show="shownCategories.some((item) => item.id === 'privacy')"
          data-category="privacy"
          :aria-label="t('settings.privacy')"
        >
          <h2>{{ t('settings.privacy') }}</h2>
          <PrefRow
            v-if="row('privacy', 'settings.privacy', 'settings.privacyText')"
            :label="t('settings.privacy')"
            :description="t('settings.privacyText')"
          >
            <template #default>
              <UButton to="/settings/blocked" :icon="icons.blocked">{{ t('blocked.title') }}</UButton>
            </template>
          </PrefRow>
        </section>
        <section
          v-show="shownCategories.some((item) => item.id === 'about')"
          data-category="about"
          :aria-label="t('about.title')"
        >
          <h2>{{ t('about.title') }}</h2>
          <AboutSection :query="query" />
        </section>
        <section
          v-show="shownCategories.some((item) => item.id === 'viewing')"
          :aria-label="t('viewer.settings')"
          data-category="viewing"
        >
          <h2>{{ t('viewer.settings') }}</h2>
          <ViewingSettings />
        </section>
      </div>
    </div>
  </Page>
</template>
<style scoped>
.settings-layout {
  display: grid;
  grid-template-columns: 12rem minmax(0, 1fr);
  gap: 1.5rem;
  padding-top: 0.75rem;
}

.category-nav {
  display: flex;
  flex-direction: column;
  align-items: stretch;
  gap: 0.25rem;
}

.category-nav :deep(button) {
  white-space: normal;
  text-align: left;
  justify-content: flex-start;
}

.settings-content {
  min-width: 0;
  max-width: 60rem;
  container-type: inline-size;
}

section {
  margin-bottom: 2rem;
}

h2 {
  margin: 0 0 0.5rem;
  font-size: 1rem;
  font-weight: 600;
  color: var(--ui-text-highlighted);
}

.source-list {
  list-style: none;
  padding: 0;
  border: 1px solid var(--ui-border);
  border-radius: var(--ui-radius);
}

.source-list li {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 0.5rem;
  padding: 0.75rem;
}

.source-list li + li {
  border-top: 1px solid var(--ui-border);
}

.source-path {
  flex: 1 1 12rem;
  min-width: 0;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}

.slider-controls {
  width: min(20rem, 100%);
  min-width: 12rem;
}

.slider-ends {
  display: flex;
  justify-content: space-between;
  gap: 0.75rem;
  margin-top: 0.5rem;
  color: var(--ui-text-muted);
  font-size: 0.75rem;
}

.scale-controls {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.75rem;
}

.scale-controls :deep([data-slot='root']) {
  min-width: 8rem;
}

.appearance-choices :deep(fieldset) {
  flex-wrap: wrap;
}

.swatch {
  display: inline-block;
  width: 0.875rem;
  height: 0.875rem;
  margin-right: 0.375rem;
  border: 1px solid var(--ui-border);
  border-radius: 50%;
}

@container (max-width: 56rem) {
  .settings-layout {
    grid-template-columns: minmax(0, 1fr);
    gap: 1rem;
  }

  .category-nav {
    flex-direction: row;
    flex-wrap: wrap;
  }
}
</style>
