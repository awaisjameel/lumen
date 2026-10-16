<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, provide, ref, toRef, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AboutSection from '../about/AboutSection.vue'
import UpdatesSection from '../updates/UpdatesSection.vue'
import OcrSetting from './OcrSetting.vue'
import XmpSetting from './XmpSetting.vue'
import ViewingSettings from './ViewingSettings.vue'
import VideoSettings from './VideoSettings.vue'
import LibrarySettings from './LibrarySettings.vue'
import FormatSettings from './FormatSettings.vue'
import ShortcutSettings from './ShortcutSettings.vue'
import ExportPresets from './ExportPresets.vue'
import CustomisationSettings from './CustomisationSettings.vue'
import SectionHead from './SectionHead.vue'
import CompressionSettings from './CompressionSettings.vue'
import {
  Accent,
  PREF_CATEGORIES,
  Density,
  FocusRing,
  Motion,
  PrefCategory,
  ViewerBackground,
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
  TAG_CONFIDENCE_DEFAULT,
  TAG_CONFIDENCE_MIN,
  Theme,
  THUMB_CACHE_DEFAULT_MB,
  THUMB_CACHE_MAX_MB,
  THUMB_CACHE_MIN_MB,
} from '../bridge'
import type { Preferences, Settings, Source } from '../bridge'
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
import IconButton from '../shared/IconButton.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import USwitch from '@nuxt/ui/components/Switch.vue'
import USlider from '@nuxt/ui/components/Slider.vue'
import URadioGroup from '@nuxt/ui/components/RadioGroup.vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import { usePreferences } from '../shared/preferences'
import { useSaveFeedback } from './saveFeedback'
import { shortcutCatalogue } from '../shared/shortcuts'
import { useRowFilter } from './rowFilter'
import Icon from '../shared/Icon.vue'
import ViewBar from '../shared/ViewBar.vue'
import Segmented from '../shared/Segmented.vue'
import USelect from '@nuxt/ui/components/Select.vue'
import { defaultPreferences } from '../shared/preferences'
import mountainPreview from './previews/alps-dawn.jpg'
import seaPreview from './previews/sunset-sea.jpg'
import { notify } from '../shared/notify'

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
  if (
    !(await confirm(t('settings.removeTitle', { path: s.path }), t('settings.removeText'), t('settings.remove'), {
      danger: true,
    }))
  )
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
// The viewing copies are counted apart from the thumbnails: they are a
// different, disposable cache with its own bound.
const viewingBytes = ref<number | null>(null)
const viewingNote = ref('')
const viewingError = ref('')
async function loadViewingSize() {
  try {
    viewingBytes.value = await engine.viewingCacheBytes()
  } catch (e) {
    logError(e)
  }
}
async function clearViewingCache() {
  viewingNote.value = ''
  viewingError.value = ''
  try {
    const freed = await engine.clearViewingCache()
    viewingNote.value = t('settings.viewingCacheCleared', { size: bytes(freed) })
  } catch (e) {
    viewingError.value = errorText(e)
  }
  await loadViewingSize()
}
let cacheTimer = 0
onMounted(() => {
  void loadViewingSize()
  void loadCacheSize()
  cacheTimer = window.setInterval(() => void loadCacheSize(), CACHE_SIZE_POLL_MS)
})
onBeforeUnmount(() => clearInterval(cacheTimer))

const prefs = usePreferences()
const defaults = defaultPreferences()
const previewFavourite = ref(false)
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
  const step = ['ArrowDown', 'ArrowRight'].includes(event.key)
    ? 1
    : ['ArrowUp', 'ArrowLeft'].includes(event.key)
      ? -1
      : 0
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
const motions = [Motion.MotionSystem, Motion.MotionReduced, Motion.MotionFull] as const
const backgrounds = [
  ViewerBackground.ViewerBackgroundDark,
  ViewerBackground.ViewerBackgroundBlack,
  ViewerBackground.ViewerBackgroundGrey,
] as const
const focusRings = [FocusRing.FocusStandard, FocusRing.FocusStrong] as const
const motionItems = computed(() => motions.map((value) => ({ value, label: t(`settings.motion.${value}`) })))
const backgroundItems = computed(() =>
  backgrounds.map((value) => ({ value, label: t(`settings.viewerBackground.${value}`) })),
)
const focusItems = computed(() => focusRings.map((value) => ({ value, label: t(`settings.focusRing.${value}`) })))
function setMotion(value: unknown) {
  for (const motion of motions) if (motion === value) void prefs.set({ motion })
}
function setBackground(value: unknown) {
  for (const viewerBackground of backgrounds) if (viewerBackground === value) void prefs.set({ viewerBackground })
}
function setFocusRing(value: unknown) {
  for (const focusRing of focusRings) if (focusRing === value) void prefs.set({ focusRing })
}

// The screen groups some controls differently from the older engine reset
// categories. Reset through their existing save queues without changing a
// preference displayed in another category. Opt-ins remain untouched.
const resetNotes = ref<Partial<Record<PrefCategory, string>>>({})
const resetting = ref(false)
async function resetInterface(patch: Partial<Preferences>) {
  if (!(await prefs.set(patch))) throw new Error(t('settings.resetFailed'))
}
async function resetEngine(actions: { key: keyof Settings; run: () => Promise<void> }[]) {
  const failures: string[] = []
  for (const action of actions) {
    await save(action.key, action.run)
    const failure = lib.settingErrors[action.key]
    if (failure) failures.push(failure)
  }
  if (failures.length) throw new Error(t('settings.resetIncomplete', { error: failures.join(' ') }))
}
async function resetCategory(group: PrefCategory) {
  switch (group) {
    case PrefCategory.PrefAppearance:
      return resetInterface({
        theme: defaults.theme,
        accent: defaults.accent,
        density: defaults.density,
        textScale: defaults.textScale,
      })
    case PrefCategory.PrefLibrary:
      await lib.resetPreferences([group])
      return resetEngine([
        { key: 'thumbCacheMB', run: () => lib.saveThumbCacheMB(THUMB_CACHE_DEFAULT_MB) },
        ...lib.settings.excludedFormats.map((family) => ({
          key: 'excludedFormats' as const,
          run: () => lib.saveFormatExcluded(family, false),
        })),
      ])
    case PrefCategory.PrefViewing:
      await lib.resetPreferences([group, PrefCategory.PrefVideo])
      return resetInterface({ viewerBackground: defaults.viewerBackground })
    case PrefCategory.PrefIndexing:
      return resetEngine([
        { key: 'pauseFullScreen', run: () => lib.savePauseFullScreen(true) },
        { key: 'pauseBusy', run: () => lib.savePauseBusy(true) },
        { key: 'idleOnly', run: () => lib.saveIdleOnly(false) },
        { key: 'aiOnBattery', run: () => lib.saveAIOnBattery(false) },
        { key: 'cpuThreads', run: () => lib.saveCPUThreads(lib.settings.defaultCPUThreads) },
      ])
    case PrefCategory.PrefSearch:
      return resetEngine([
        { key: 'tagConfidence', run: () => lib.saveTagConfidence(TAG_CONFIDENCE_DEFAULT) },
        { key: 'eventGapHours', run: () => lib.saveEventGap(EVENT_GAP_DEFAULT) },
      ])
    case PrefCategory.PrefFiles:
      return resetInterface({ exportPresets: [], compression: { ...defaults.compression } })
    case PrefCategory.PrefAccessibility:
      return resetInterface({ focusRing: defaults.focusRing, motion: defaults.motion, shortcuts: {} })
    default:
      return lib.resetPreferences([group])
  }
}
async function resetSection(category: PrefCategory, section: string) {
  if (resetting.value) return
  resetNotes.value = { ...resetNotes.value, [category]: '' }
  if (
    !(await confirm(
      t('settings.resetSectionTitle', { section }),
      t('settings.resetSectionText'),
      t('settings.resetSection'),
    ))
  )
    return
  saveConfidence()
  resetting.value = true
  try {
    await run(async () => {
      await resetCategory(category)
      resetNotes.value = { ...resetNotes.value, [category]: t('settings.resetDone', { section }) }
    })
  } finally {
    resetting.value = false
  }
}
const query = ref('')
provide('settings-query', query)
const category = ref('appearance')
const searching = computed(() => !!query.value.trim())

function clearSearch() {
  query.value = ''
}
function selectCategory(value: string) {
  category.value = value
  query.value = ''
  void router.replace({ query: { ...route.query, section: value } })
}
const categories = [
  { id: 'appearance', label: 'settings.appearance', icon: 'appearance' },
  { id: 'library', label: 'settings.category.library', icon: 'folder' },
  { id: 'viewing', label: 'settings.category.viewing', icon: 'play' },
  { id: 'search', label: 'settings.category.search', icon: 'smart' },
  { id: 'indexing', label: 'settings.category.indexing', icon: 'cpu' },
  { id: 'files', label: 'settings.category.files', icon: 'organise' },
  { id: 'accessibility', label: 'settings.category.accessibility', icon: 'keyboard' },
  { id: 'privacy', label: 'settings.privacy', icon: 'blocked' },
  { id: 'presets', label: 'settings.category.presets', icon: 'sliders' },
  { id: 'updates', label: 'settings.category.updates', icon: 'update' },
] as const
const categoryItems = computed(() => categories.map((item) => ({ value: item.id, label: t(item.label) })))
const preferenceGroups = {
  appearance: ['theme', 'accent', 'density', 'textScale'],
  library: ['startView', 'thumbFit', 'hideFavouriteBadge', 'hideVideoBadge', 'hideDurationBadge'],
  viewing: ['viewer', 'viewerBackground'],
  files: ['exportPresets', 'compression'],
  accessibility: ['motion', 'focusRing', 'shortcuts'],
} as const
const modifiedCategories = computed(() => {
  const changed = new Map<string, boolean>(
    Object.entries(preferenceGroups).map(([id, fields]) => [
      id,
      fields.some((field) => JSON.stringify(prefs.prefs[field]) !== JSON.stringify(defaults[field])),
    ]),
  )
  changed.set(
    'library',
    !!changed.get('library') ||
      lib.settings.excludedFormats.length > 0 ||
      lib.settings.thumbCacheMB !== THUMB_CACHE_DEFAULT_MB,
  )
  changed.set(
    'search',
    lib.settings.ocr ||
      lib.settings.tagConfidence !== TAG_CONFIDENCE_DEFAULT ||
      lib.settings.eventGapHours !== EVENT_GAP_DEFAULT,
  )
  changed.set(
    'indexing',
    lib.settings.aiOnBattery ||
      !lib.settings.pauseFullScreen ||
      !lib.settings.pauseBusy ||
      lib.settings.idleOnly ||
      lib.settings.cpuThreads !== lib.settings.defaultCPUThreads,
  )
  changed.set('files', !!changed.get('files') || lib.settings.xmp)
  return changed
})
// Old deep links still reach the category that now contains their controls.
function categoryOf(id: string) {
  return id === 'video'
    ? 'viewing'
    : id === 'compression'
      ? 'files'
      : id === 'about'
        ? 'updates'
        : id === 'ai'
          ? 'search'
          : id
}
// A notice elsewhere can open Settings at one category (?section=updates).
const route = useRoute()
const router = useRouter()
watch(
  () => route.query.section,
  (section) => {
    if (typeof section === 'string' && categories.some((item) => item.id === categoryOf(section))) {
      if (category.value !== categoryOf(section)) query.value = ''
      category.value = categoryOf(section)
    }
  },
  { immediate: true },
)
const matches = useRowFilter(() => query.value)
function row(_group: string, ...keys: MessageKey[]) {
  return matches(...keys)
}
function shown(group: string) {
  return shownCategories.value.some((item) => item.id === group)
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
const { save, hint, savingAny } = useSaveFeedback<keyof Settings>((key) => lib.settingErrors[key])
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
    run: async () => {
      if (await run(engine.rescan)) notify({ id: 'settings.rescan', kind: 'info', title: t('settings.foldersChecked') })
    },
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
  updates: [
    'update.title',
    'update.automatic',
    'update.check',
    'update.channel',
    'update.restart',
    'update.privacy',
    'update.battery',
    'update.bandwidth',
  ],
  compression: [
    'settings.compressionReview',
    'settings.compressionReviewHint',
    'compression.title',
    'compression.mode',
    'compression.effort',
    'compression.minSavings',
    'compression.threads',
  ],
  viewing: [
    'settings.viewerBackground',
    'settings.viewerBackgroundHint',
    'settings.category.viewing',
    'viewer.open',
    'viewer.openHint',
    'viewer.wheel',
    'viewer.wheelHint',
    'viewer.filmstrip',
    'viewer.autoHide',
    'viewer.interval',
    'viewer.slideshowLoop',
    'viewer.animations',
    'viewer.animationsHint',
    'viewer.renderSide',
    'viewer.renderSideHint',
  ],
  video: [
    'settings.category.video',
    'viewer.autoplay',
    'viewer.volume',
    'viewer.startMuted',
    'viewer.speed',
    'viewer.skipInterval',
    'viewer.videoLoop',
    'viewer.subtitles',
    'viewer.subtitlesHint',
    'viewer.subtitleLanguage',
    'viewer.subtitleLanguageHint',
    'viewer.resume',
    'viewer.resumeHint',
    'viewer.clearHistory',
    'viewer.clearHistoryHint',
  ],
  accessibility: [
    ...shortcutCatalogue.map((entry) => entry.title),
    'settings.motion',
    'settings.motionHint',
    'settings.category.accessibility',
    'settings.focusRing',
    'settings.focusRingHint',
    'settings.accessibilityAlso',
    'settings.accessibilityAlsoHint',
    'settings.shortcuts',
    'settings.shortcutsHint',
  ],
  presets: [
    'settings.category.presets',
    'settings.resetAll',
    'settings.resetAllHint',
    'settings.presetExport',
    'settings.presetExportHint',
    'settings.presetImport',
    'settings.presetImportHint',
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
  library: [
    ...folderKeys,
    'settings.cache',
    'settings.cacheLimit',
    'settings.cacheHint',
    'settings.viewingCache',
    'settings.viewingCacheHint',
    'settings.viewingCacheClear',
    'settings.formats',
    'settings.formatsHint',

    'settings.videos',
    'settings.videosHint',
    'settings.startView',
    'settings.startViewHint',
    'settings.thumbFit',
    'settings.thumbFitHint',
    'settings.badges',
    'settings.badgesHint',
    'settings.badgeFavourite',
    'settings.badgeVideo',
    'settings.badgeDuration',
    'settings.sort',
    'settings.sortHint',
  ],
  indexing: [
    'settings.aiOnBattery',
    'settings.aiOnBatteryHint',
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
  ],
  search: [
    'settings.eventGap',
    'settings.eventGapHint',
    'settings.ocr',
    'settings.ocrHint',
    'settings.ai',
    'settings.aiHint',
    'settings.confidence',
    'settings.confidenceHint',
  ],
  files: [
    'settings.xmp',
    'settings.xmpHint',
    'settings.xmpDelete',
    'settings.xmpDeleteHint',
    'settings.xmpExport',
    'settings.xmpExportHint',
    'settings.organise',
    'settings.organiseHint',
    'settings.exportPresets',
    'settings.exportPresetsHint',
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
  Object.entries(categoryKeys)
    .filter(([id, keys]) => (searching.value || categoryOf(id) === category.value) && keys.some((key) => matches(key)))
    .map(([id]) => ({ id })),
)
const currentCategory = computed(() => categories.find((item) => item.id === category.value) ?? categories[0])
const pageSaving = computed(() => prefs.saving || savingAny.value || resetting.value)
async function resetAll() {
  if (
    !(await confirm(t('settings.resetAllTitle'), t('settings.resetAllText'), t('settings.resetAll'), { danger: true }))
  )
    return
  saveConfidence()
  await run(async () => {
    await lib.resetPreferences(PREF_CATEGORIES)
    notify({ id: 'settings.reset', kind: 'success', title: t('settings.resetAllDone') })
  })
}
</script>

<template>
  <Page :scroll="false">
    <template #header>
      <PageHeader :title="t('settings.title')" :subtitle="t('settings.subtitle')">
        <template #search>
          <UInput
            v-model="query"
            :icon="icons.search"
            :placeholder="t('settings.search')"
            :aria-label="t('settings.search')"
            type="search"
          />
        </template>
        <span role="status" class="save-status"
          ><Icon :name="pageSaving ? 'refresh' : 'check'" :size="16" />{{
            t(pageSaving ? 'settings.saving' : 'settings.saved')
          }}</span
        >
        <UButton
          variant="ghost"
          :icon="icons.reset"
          :aria-label="t('settings.resetAll')"
          data-settings-reset-all
          :disabled="resetting"
          @click="resetAll"
          ><span class="reset-label">{{ t('settings.resetAll') }}</span></UButton
        >
      </PageHeader>
    </template>
    <template #toolbar>
      <ViewBar class="mobile-categories" :label="t('settings.categories')">
        <USelect
          :model-value="categoryItems.find((item) => item.value === category)?.value"
          :items="categoryItems"
          :aria-label="t('settings.categories')"
          @update:model-value="selectCategory"
        />
        <template #end
          ><span class="text-dimmed text-xs">{{ t('settings.instant') }}</span></template
        >
      </ViewBar>
    </template>
    <div class="settings-layout" :inert="resetting">
      <nav class="category-nav" :aria-label="t('settings.categories')">
        <UButton
          v-for="item in categories"
          :key="item.id"
          :icon="icons[item.icon]"
          :aria-pressed="!searching && category === item.id"
          variant="ghost"
          :data-settings-category="item.id"
          @click="selectCategory(item.id)"
          ><span class="category-label">{{ t(item.label) }}</span
          ><span v-if="modifiedCategories.get(item.id)" class="modified-dot" :aria-label="t('settings.modified')"
        /></UButton>
        <p class="privacy-note">
          <Icon name="blocked" :size="16" /><span>{{ t('settings.local') }}</span>
        </p>
      </nav>
      <div class="settings-scroller">
        <div class="settings-content" :class="{ searching }">
          <div class="category-heading">
            <div>
              <h2>{{ searching ? t('settings.results') : t(currentCategory.label) }}</h2>
              <p>{{ searching ? t('settings.resultsHint') : t(`settings.intro.${currentCategory.id}`) }}</p>
            </div>
            <UButton v-if="searching" variant="ghost" @click="clearSearch">{{ t('settings.clearSearch') }}</UButton>
          </div>
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
          <section v-show="shown('appearance')" data-category="appearance" :aria-label="t('settings.appearance')">
            <SectionHead
              :title="t('settings.appearance')"
              :note="resetNotes.appearance"
              resettable
              @reset="resetSection(PrefCategory.PrefAppearance, t('settings.appearance'))"
            />
            <div class="settings-card">
              <PrefRow
                v-if="row('appearance', 'settings.theme', 'settings.themeHint')"
                :label="t('settings.theme')"
                :modified="prefs.prefs.theme !== defaults.theme"
                :description="t('settings.themeHint')"
              >
                <template #default="{ labelledBy, describedBy }">
                  <div
                    data-appearance="theme"
                    role="radiogroup"
                    :aria-labelledby="labelledBy"
                    :aria-describedby="describedBy"
                    class="theme-choices"
                    @keydown="onThemeKey"
                  >
                    <button
                      v-for="item in themeItems"
                      :key="item.value"
                      type="button"
                      role="radio"
                      :aria-checked="prefs.prefs.theme === item.value"
                      :tabindex="prefs.prefs.theme === item.value ? 0 : -1"
                      class="theme-card"
                      @click="setTheme(item.value)"
                    >
                      <span class="theme-preview" :data-preview="item.value" aria-hidden="true"
                        ><span class="mini-nav"><i /><i /><i /></span
                        ><span class="mini-grid"
                          ><img :src="mountainPreview" alt="" /><img :src="seaPreview" alt="" /></span
                      ></span>
                      <span class="theme-label"
                        >{{ item.label
                        }}<span class="theme-check"
                          ><Icon v-if="prefs.prefs.theme === item.value" name="check" :size="14" /></span
                      ></span>
                    </button>
                  </div>
                </template>
              </PrefRow>
              <PrefRow
                v-if="row('appearance', 'settings.accent', 'settings.accentHint')"
                :label="t('settings.accent')"
                :modified="prefs.prefs.accent !== defaults.accent"
                :description="t('settings.accentHint')"
              >
                <template #default="{ labelledBy, describedBy }">
                  <URadioGroup
                    data-appearance="accent"
                    :model-value="accents.find((value) => value === prefs.prefs.accent)"
                    :items="accentItems"
                    orientation="horizontal"
                    class="accent-choices"
                    :aria-labelledby="labelledBy"
                    :aria-describedby="describedBy"
                    @update:model-value="setAccent"
                  >
                    <template #label="{ item }"
                      ><span class="sr-only">{{ item.label }}</span
                      ><Icon v-if="prefs.prefs.accent === item.value" class="accent-check" name="check" :size="14"
                    /></template>
                  </URadioGroup>
                </template>
              </PrefRow>
            </div>
            <div class="settings-card appearance-spacing">
              <PrefRow
                v-if="row('appearance', 'settings.density', 'settings.densityHint')"
                :label="t('settings.density')"
                :modified="prefs.prefs.density !== defaults.density"
                :description="t('settings.densityHint')"
              >
                <template #default="{ labelledBy, describedBy }">
                  <Segmented
                    data-appearance="density"
                    :model-value="prefs.prefs.density"
                    :items="densityItems"
                    :label="t('settings.density')"
                    :aria-labelledby="labelledBy"
                    :aria-describedby="describedBy"
                    @update:model-value="setDensity"
                  />
                </template>
              </PrefRow>
              <PrefRow
                v-if="row('appearance', 'settings.textSize', 'settings.textSizeHint')"
                :label="t('settings.textSize')"
                :modified="prefs.prefs.textScale !== defaults.textScale"
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
                    <IconButton
                      :icon="icons.reset"
                      variant="ghost"
                      :label="t('settings.resetText')"
                      :disabled="prefs.prefs.textScale === TEXT_SCALE_DEFAULT"
                      @click="prefs.set({ textScale: TEXT_SCALE_DEFAULT })"
                    />
                  </div>
                </template>
              </PrefRow>
              <div v-if="!searching" class="live-preview" data-settings-preview>
                <h3>{{ t('settings.livePreview') }}</h3>
                <div class="preview-sample" :class="{ compact: prefs.prefs.density === Density.DensityCompact }">
                  <img :src="seaPreview" :alt="t('settings.previewAlt')" />
                  <div>
                    <strong>{{ t('settings.previewTitle') }}</strong>
                    <p>{{ t('settings.previewMeta') }}</p>
                  </div>
                  <UButton
                    :icon="icons.heart"
                    variant="soft"
                    :aria-pressed="previewFavourite"
                    @click="previewFavourite = !previewFavourite"
                    >{{ t(previewFavourite ? 'settings.previewFavourited' : 'settings.previewFavourite') }}</UButton
                  >
                </div>
              </div>
            </div>
            <div>
              <p v-if="!searching" class="appearance-note">
                <Icon name="info" :size="16" />{{ t('settings.appearanceMore') }}
              </p>
            </div>
          </section>
          <section v-show="shown('library')" data-category="library" :aria-label="t('settings.category.library')">
            <SectionHead
              :title="t('settings.category.library')"
              :note="resetNotes.library"
              resettable
              @reset="resetSection(PrefCategory.PrefLibrary, t('settings.category.library'))"
            />
            <div class="settings-card">
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
                  <UButton
                    loading-auto
                    :aria-label="t('settings.removeNamed', { path: s.path })"
                    @click="removeFolder(s)"
                  >
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
              <FormatSettings v-if="row('library', 'settings.formats', 'settings.formatsHint')" />
            </div>
            <h3 class="group-title">{{ t('settings.group.browsing') }}</h3>
            <div class="settings-card">
              <LibrarySettings :query="query" />
            </div>
            <h3 class="group-title">{{ t('settings.group.storage') }}</h3>
            <div class="settings-card">
              <PrefRow
                v-if="row('library', 'settings.cache', 'settings.cacheLimit', 'settings.cacheHint')"
                :label="t('settings.cache')"
                :modified="lib.settings.thumbCacheMB !== THUMB_CACHE_DEFAULT_MB"
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
                v-if="
                  cacheBytes !== null && row('library', 'settings.cache', 'settings.cacheLimit', 'settings.cacheHint')
                "
                class="cache-used text-muted"
              >
                {{ t('settings.cacheUsed', { size: bytes(cacheBytes) }) }}
              </p>
              <PrefRow
                v-if="
                  row('library', 'settings.viewingCache', 'settings.viewingCacheHint', 'settings.viewingCacheClear')
                "
                :label="t('settings.viewingCache')"
                :description="t('settings.viewingCacheHint')"
                :hint="
                  viewingNote ||
                  (viewingBytes === null ? '' : t('settings.viewingCacheUsed', { size: bytes(viewingBytes) }))
                "
                :error="viewingError"
              >
                <template #default>
                  <UButton
                    data-viewing-cache-clear
                    loading-auto
                    :disabled="viewingBytes === 0"
                    @click="clearViewingCache"
                    >{{ t('settings.viewingCacheClear') }}</UButton
                  >
                </template>
              </PrefRow>
            </div>
          </section>
          <section v-show="shown('viewing')" :aria-label="t('settings.category.viewing')" data-category="viewing">
            <SectionHead
              :title="t('settings.category.viewing')"
              :note="resetNotes.viewing"
              resettable
              @reset="resetSection(PrefCategory.PrefViewing, t('settings.category.viewing'))"
            />
            <div class="settings-card">
              <PrefRow
                v-if="row('viewing', 'settings.viewerBackground', 'settings.viewerBackgroundHint')"
                :label="t('settings.viewerBackground')"
                :modified="prefs.prefs.viewerBackground !== defaults.viewerBackground"
                :description="t('settings.viewerBackgroundHint')"
              >
                <template #default="{ labelledBy, describedBy }">
                  <URadioGroup
                    data-appearance="viewerBackground"
                    :model-value="backgrounds.find((value) => value === prefs.prefs.viewerBackground)"
                    :items="backgroundItems"
                    orientation="horizontal"
                    class="appearance-choices"
                    :aria-labelledby="labelledBy"
                    :aria-describedby="describedBy"
                    @update:model-value="setBackground"
                  />
                </template>
              </PrefRow>
              <ViewingSettings :query="query" />
            </div>
          </section>
          <section v-show="shown('video')" :aria-label="t('settings.category.video')" data-category="video">
            <SectionHead
              :title="t('settings.category.video')"
              :note="resetNotes.video"
              resettable
              @reset="resetSection(PrefCategory.PrefVideo, t('settings.category.video'))"
            />
            <div class="settings-card">
              <VideoSettings :query="query" />
            </div>
          </section>
          <section v-show="shown('indexing')" data-category="indexing" :aria-label="t('settings.category.indexing')">
            <SectionHead
              :title="t('settings.category.indexing')"
              :note="resetNotes.indexing"
              resettable
              @reset="resetSection(PrefCategory.PrefIndexing, t('settings.category.indexing'))"
            />
            <div class="settings-card">
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
                v-if="row('indexing', 'settings.aiOnBattery', 'settings.aiOnBatteryHint')"
                :label="t('settings.aiOnBattery')"
                :modified="lib.settings.aiOnBattery"
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
              <PrefRow
                v-if="row('indexing', 'settings.pauseFullScreen', 'settings.pauseFullScreenHint')"
                :label="t('settings.pauseFullScreen')"
                :modified="!lib.settings.pauseFullScreen"
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
                :modified="!lib.settings.pauseBusy"
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
                :modified="lib.settings.idleOnly"
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
                v-if="
                  row('indexing', 'settings.threadsLabel', 'settings.threadsHint') && lib.settings.maxCPUThreads > 1
                "
                :label="t('settings.threadsLabel')"
                :modified="lib.settings.cpuThreads !== lib.settings.defaultCPUThreads"
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
              <p
                v-if="
                  lib.settings.maxCPUThreads > 1 && row('indexing', 'settings.threadsLabel', 'settings.threadsHint')
                "
              >
                {{ t('settings.threads', { n: threadsShown, max: lib.settings.maxCPUThreads }) }}
              </p>
            </div>
          </section>
          <section v-show="shown('search')" data-category="search" :aria-label="t('settings.category.search')">
            <SectionHead
              :title="t('settings.category.search')"
              :note="resetNotes.search"
              resettable
              @reset="resetSection(PrefCategory.PrefSearch, t('settings.category.search'))"
            />
            <div class="settings-card">
              <div v-if="!searching || row('search', 'settings.ai', 'settings.aiHint')" class="ai-status">
                <span class="lumen-light-dot idle" /><span>{{ aiStatus }}</span>
              </div>
              <UAlert
                v-if="lib.progress?.ai.facesError"
                :description="t('settings.peopleError', { error: lib.progress.ai.facesError })"
                color="error"
                variant="subtle"
              />
              <PrefRow
                v-if="row('search', 'settings.eventGap', 'settings.eventGapHint')"
                :label="t('settings.eventGap')"
                :modified="lib.settings.eventGapHours !== EVENT_GAP_DEFAULT"
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
              <OcrSetting v-show="row('search', 'settings.ocr', 'settings.ocrHint')" />
              <PrefRow
                v-if="row('search', 'settings.confidence', 'settings.confidenceHint')"
                :label="t('settings.confidence')"
                :modified="lib.settings.tagConfidence !== TAG_CONFIDENCE_DEFAULT"
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
            </div>
          </section>
          <section v-show="shown('files')" data-category="files" :aria-label="t('settings.category.files')">
            <SectionHead
              :title="t('settings.category.files')"
              :note="resetNotes.files"
              resettable
              @reset="resetSection(PrefCategory.PrefFiles, t('settings.category.files'))"
            />
            <div class="settings-card">
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
              <ExportPresets v-if="row('files', 'settings.exportPresets', 'settings.exportPresetsHint')" />
            </div>
          </section>
          <section v-show="shown('compression')" :aria-label="t('compression.title')" data-category="compression">
            <SectionHead
              :title="t('compression.title')"
              :note="resetNotes.compression"
              resettable
              @reset="resetSection(PrefCategory.PrefCompression, t('compression.title'))"
            />
            <div class="settings-card">
              <CompressionSettings :query="query" />
            </div>
          </section>
          <section
            v-show="shown('accessibility')"
            :aria-label="t('settings.category.accessibility')"
            data-category="accessibility"
          >
            <SectionHead
              :title="t('settings.category.accessibility')"
              :note="resetNotes.accessibility"
              resettable
              @reset="resetSection(PrefCategory.PrefAccessibility, t('settings.category.accessibility'))"
            />
            <div class="settings-card">
              <PrefRow
                v-if="row('accessibility', 'settings.motion', 'settings.motionHint')"
                :label="t('settings.motion')"
                :modified="prefs.prefs.motion !== defaults.motion"
                :description="t('settings.motionHint')"
              >
                <template #default="{ labelledBy, describedBy }">
                  <URadioGroup
                    data-appearance="motion"
                    :model-value="motions.find((value) => value === prefs.prefs.motion)"
                    :items="motionItems"
                    orientation="horizontal"
                    class="appearance-choices"
                    :aria-labelledby="labelledBy"
                    :aria-describedby="describedBy"
                    @update:model-value="setMotion"
                  />
                </template>
              </PrefRow>
              <PrefRow
                v-if="row('accessibility', 'settings.focusRing', 'settings.focusRingHint')"
                :label="t('settings.focusRing')"
                :modified="prefs.prefs.focusRing !== defaults.focusRing"
                :description="t('settings.focusRingHint')"
              >
                <template #default="{ labelledBy, describedBy }">
                  <URadioGroup
                    data-accessibility="focusRing"
                    :model-value="focusRings.find((value) => value === prefs.prefs.focusRing)"
                    :items="focusItems"
                    orientation="horizontal"
                    class="appearance-choices"
                    :aria-labelledby="labelledBy"
                    :aria-describedby="describedBy"
                    @update:model-value="setFocusRing"
                  />
                </template>
              </PrefRow>
              <PrefRow
                v-if="row('accessibility', 'settings.accessibilityAlso', 'settings.accessibilityAlsoHint')"
                :label="t('settings.accessibilityAlso')"
                :description="t('settings.accessibilityAlsoHint')"
              >
                <template #default>
                  <UButton variant="outline" data-accessibility="appearance" @click="selectCategory('appearance')">{{
                    t('settings.appearance')
                  }}</UButton>
                </template>
              </PrefRow>
              <template
                v-if="
                  row('accessibility', 'settings.shortcuts', 'settings.shortcutsHint') ||
                  shortcutCatalogue.some((entry) => matches(entry.title))
                "
              >
                <h3 class="subhead">{{ t('settings.shortcuts') }}</h3>
                <ShortcutSettings v-if="shown('accessibility')" :query="query" />
              </template>
            </div>
          </section>
          <section v-show="shown('privacy')" data-category="privacy" :aria-label="t('settings.privacy')">
            <SectionHead :title="t('settings.privacy')" />
            <div class="settings-card">
              <PrefRow
                v-if="row('privacy', 'settings.privacy', 'settings.privacyText')"
                :label="t('settings.privacy')"
                :description="t('settings.privacyText')"
              >
                <template #default>
                  <UButton to="/settings/blocked" :icon="icons.blocked">{{ t('blocked.title') }}</UButton>
                </template>
              </PrefRow>
            </div>
          </section>
          <section v-show="shown('presets')" :aria-label="t('settings.category.presets')" data-category="presets">
            <SectionHead :title="t('settings.category.presets')" />
            <div class="settings-card">
              <CustomisationSettings :query="query" @reset="saveConfidence" />
            </div>
          </section>
          <section v-show="shown('updates')" data-category="updates" :aria-label="t('update.title')">
            <SectionHead :title="t('update.title')" />
            <div class="settings-card">
              <UpdatesSection :query="query" />
            </div>
          </section>
          <section v-show="shown('about')" data-category="about" :aria-label="t('about.title')">
            <SectionHead :title="t('about.title')" />
            <div class="settings-card">
              <AboutSection :query="query" />
            </div>
          </section>
        </div>
      </div>
    </div>
  </Page>
</template>
<style scoped>
.settings-layout {
  display: grid;
  grid-template-columns: 14rem minmax(0, 1fr);
  height: 100%;
  min-height: 0;
}
.category-nav {
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
  padding: 1.25rem 0.75rem;
  border-right: 1px solid var(--ui-border);
  overflow-y: auto;
}
.category-label {
  flex: 1;
}
.modified-dot {
  flex: none;
  width: 0.375rem;
  height: 0.375rem;
  border-radius: 50%;
  background: var(--ui-primary);
}
.category-nav :deep(button) {
  flex: none;
  min-height: 3rem;
  white-space: normal;
  text-align: left;
  justify-content: flex-start;
  font-size: 0.8125rem;
}
.category-nav :deep(button[aria-pressed='true']) {
  color: var(--lumen-primary-text);
  background: var(--lumen-bg-raised);
  box-shadow: var(--lumen-shadow-1);
}
.privacy-note {
  display: flex;
  align-items: flex-start;
  gap: 0.5rem;
  margin: auto 0 0;
  padding: 0.75rem;
  border: 1px solid var(--ui-border);
  border-radius: var(--lumen-r-lg);
  background: var(--lumen-bg-sunken);
  color: var(--ui-text-dimmed);
  font-size: 0.75rem;
}
.privacy-note :deep(svg) {
  flex: none;
}
.settings-scroller {
  min-width: 0;
  overflow: auto;
}
.settings-content {
  position: relative;
  min-width: 0;
  max-width: 80rem;
  margin-inline: auto;
  padding: 1.5rem var(--lumen-page-x) 3rem;
  container-type: inline-size;
  font-variant-numeric: tabular-nums;
}
.settings-content:not(.searching)
  > section:not([data-category='video'], [data-category='compression'], [data-category='about'])
  > :deep(.section-head) {
  position: absolute;
  top: 1.5rem;
  right: var(--lumen-page-x);
}
.settings-content:not(.searching)
  > section:not([data-category='video'], [data-category='compression'], [data-category='about'])
  > :deep(.section-head h2) {
  display: none;
}
.category-heading {
  display: flex;
  justify-content: space-between;
  gap: 1rem;
  margin-bottom: 1.25rem;
}
.category-heading > div {
  min-width: 0;
  padding-right: 4rem;
}
.category-heading h2 {
  margin: 0 0 0.5rem;
  color: var(--ui-text-highlighted);
  font: 600 1.5rem var(--font-display);
}
.category-heading p {
  margin: 0;
  color: var(--ui-text-dimmed);
  font-size: 0.8125rem;
}
.settings-content > section {
  margin-bottom: 1.5rem;
}
.appearance-spacing {
  margin-top: 1.5rem;
}
.group-title {
  margin: 1.25rem 0 0.75rem;
  font-size: 0.875rem;
  font-weight: 600;
  color: var(--ui-text-highlighted);
}
.ai-status {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  padding: 1.25rem;
  color: var(--ui-text-toned);
}
.settings-card {
  border: 1px solid var(--ui-border);
  border-radius: var(--lumen-r-lg);
  background: var(--lumen-bg-raised);
  box-shadow: var(--lumen-shadow-1);
  overflow: clip;
}
.settings-card > p,
.settings-card > :deep(.text-muted) {
  margin: 0;
  padding: 0.75rem 1.25rem;
}
.save-status {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  color: var(--ui-text-dimmed);
  font-size: 0.75rem;
  white-space: nowrap;
}
.mobile-categories {
  display: none;
}
.subhead {
  margin: 1.25rem;
  font-size: 0.875rem;
  font-weight: 600;
  color: var(--ui-text-highlighted);
}
.source-list {
  list-style: none;
  padding: 0;
  margin: 0 1.25rem;
  border: 1px solid var(--ui-border);
  border-radius: var(--lumen-r-md);
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
  overflow-wrap: anywhere;
}
.slider-controls {
  width: min(20rem, 100%);
  min-width: 10rem;
}
.slider-ends {
  display: flex;
  justify-content: space-between;
  gap: 0.75rem;
  margin-top: 0.5rem;
  color: var(--ui-text-dimmed);
  font-size: 0.75rem;
}
.scale-controls {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.75rem;
  width: 100%;
}
.scale-controls :deep([data-slot='root']) {
  flex: 1;
  min-width: 6rem;
}
.appearance-choices :deep(fieldset) {
  flex-wrap: wrap;
}
.theme-choices {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 0.75rem;
  width: 100%;
}
.theme-card {
  padding: 0.5rem;
  min-width: 0;
  text-align: left;
  border: 1px solid var(--ui-border);
  border-radius: var(--lumen-r-lg);
  background: var(--lumen-bg-raised);
  cursor: pointer;
  transition: transform var(--lumen-d-fast) var(--lumen-ease-out);
}
.theme-card:hover {
  background: var(--lumen-bg-hover);
}
.theme-card:active {
  transform: scale(0.98);
}
.theme-card[aria-checked='true'] {
  box-shadow: 0 0 0 0.25rem var(--lumen-primary-soft-2);
}
.theme-preview {
  display: flex;
  gap: 0.5rem;
  height: 6rem;
  padding: 0.5rem;
  border-radius: var(--lumen-r-sm);
  background: var(--lumen-bg-base);
}
/* Each preview uses the same palette in an explicit colour scheme. */
.theme-preview[data-preview='light'] {
  color-scheme: light;
}
.theme-preview[data-preview='dark'] {
  color-scheme: dark;
}
.theme-preview {
  background: light-dark(var(--lumen-viewer-text), var(--lumen-viewer-bg));
}
.mini-nav {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
  width: 22%;
}
.mini-nav i {
  height: 0.25rem;
  background: var(--lumen-viewer-text-muted);
}
.mini-nav i:first-child {
  width: 70%;
  height: 0.5rem;
  border-radius: var(--lumen-r-xs);
  background: var(--ui-primary);
}
.mini-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0.25rem;
  flex: 1;
  padding: 0.5rem;
  border-radius: var(--lumen-r-xs);
  background: light-dark(var(--lumen-viewer-text), var(--lumen-viewer-glass));
}
.mini-grid img {
  width: 100%;
  height: 100%;
  min-height: 0;
  object-fit: cover;
  border-radius: var(--lumen-r-xs);
}
.theme-label {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.25rem;
  padding: 0.75rem 0.25rem 0.25rem;
  font-size: 0.8125rem;
  font-weight: 600;
}
.theme-check {
  display: grid;
  place-items: center;
  width: 1.125rem;
  height: 1.125rem;
  border: 1px solid var(--ui-border-accented);
  border-radius: var(--lumen-r-xs);
}
[aria-checked='true'] .theme-check {
  background: var(--ui-primary);
  color: var(--lumen-on-primary);
  border-color: var(--ui-primary);
}
.settings-content :deep([data-pref]):has(.theme-choices) {
  grid-template-columns: minmax(0, 1fr);
  gap: 0.75rem;
}
.accent-choices :deep(fieldset) {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
}
.accent-choices :deep([data-slot='item']) {
  position: relative;
  padding: 0.25rem;
}
.accent-choices :deep([data-slot='wrapper']) {
  position: absolute;
  inset: 0;
}
.accent-choices :deep([data-slot='label']) {
  position: absolute;
  inset: 0;
  cursor: pointer;
}
.accent-choices :deep([data-slot='indicator']) {
  opacity: 0;
}
.accent-choices :deep(.accent-check) {
  position: absolute;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%);
  color: var(--lumen-on-primary);
}
.accent-choices :deep([data-slot='base']) {
  width: 1.5rem;
  height: 1.5rem;
  border: 0;
  background: var(--swatch);
  color: var(--lumen-on-primary);
}
.accent-choices :deep([data-state='checked']) {
  outline: 0.25rem solid var(--lumen-primary-soft-2);
  outline-offset: 0.125rem;
}
.accent-choices :deep([data-slot='item']:nth-child(1)) {
  --swatch: var(--lumen-accent-blue);
}
.accent-choices :deep([data-slot='item']:nth-child(2)) {
  --swatch: var(--lumen-accent-teal);
}
.accent-choices :deep([data-slot='item']:nth-child(3)) {
  --swatch: var(--lumen-accent-green);
}
.accent-choices :deep([data-slot='item']:nth-child(4)) {
  --swatch: var(--lumen-accent-violet);
}
.accent-choices :deep([data-slot='item']:nth-child(5)) {
  --swatch: var(--lumen-accent-rose);
}
.accent-choices :deep([data-slot='item']:nth-child(6)) {
  --swatch: var(--lumen-accent-amber);
}
.live-preview {
  margin: 0 1.25rem 1.25rem;
  border: 1px solid var(--ui-border);
  border-radius: var(--lumen-r-lg);
  overflow: hidden;
  background: var(--lumen-bg-sunken);
}
.live-preview h3 {
  padding: 0.75rem 1rem;
  margin: 0;
  font-size: 0.625rem;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--ui-text-dimmed);
  border-bottom: 1px solid var(--ui-border);
}
.preview-sample {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.75rem;
  padding: 1rem;
}
.preview-sample.compact {
  padding: 0.5rem 0.75rem;
}
.preview-sample img {
  width: 3rem;
  height: 3rem;
  object-fit: cover;
  border-radius: var(--lumen-r-sm);
}
.preview-sample > div {
  flex: 1;
}
.preview-sample p {
  margin: 0;
  color: var(--ui-text-dimmed);
}
.appearance-note {
  display: flex;
  gap: 0.5rem;
  margin: 0;
  padding: 0.75rem 1.25rem;
  color: var(--ui-text-dimmed);
  font-size: 0.75rem;
}
@container (max-width: 850px) {
  .settings-layout {
    grid-template-columns: minmax(0, 1fr);
  }
  .category-nav {
    display: none;
  }
  .mobile-categories {
    display: flex;
  }
  .mobile-categories :deep([data-slot='base']) {
    min-width: 12rem;
    flex: 1;
  }
  .reset-label {
    display: none;
  }
  :deep(.page-header .subtitle) {
    display: none;
  }
  .settings-content {
    padding-top: 1rem;
  }
  .settings-content:not(.searching)
    > section:not([data-category='video'], [data-category='compression'], [data-category='about'])
    > :deep(.section-head) {
    top: 1rem;
  }
}
@container (max-width: 40rem) {
  .settings-content :deep(.pref-row:has(.accent-choices)),
  .settings-content :deep(.pref-row:has(.scale-controls)) {
    grid-template-columns: minmax(0, 1fr);
    gap: 0.75rem;
  }
  .settings-content :deep(.pref-row:has(.accent-choices) .control) {
    justify-content: flex-start;
  }
}
</style>
