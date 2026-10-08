<script setup lang="ts">
// Reset everything, and carry the interface preferences between libraries
// as a file. A file is read for review first: the dialog lists what would
// change, and only Apply changes it. Neither a reset nor a file touches the
// library's folders, photos, tags, albums, people, PIN or blocks, and a file
// holds interface preferences only, so it cannot turn on XMP sidecars or
// start a run that moves files.
import UButton from '@nuxt/ui/components/Button.vue'
import UModal from '@nuxt/ui/components/Modal.vue'
import { computed, ref, shallowRef } from 'vue'
import { engine, PREF_CATEGORIES, PrefCategory } from '../bridge'
import type { CustomisationPreview } from '../bridge'
import { useLibrary } from '../library/store'
import { confirm } from '../shared/confirm'
import { errorText } from '../shared/errors'
import { t } from '../shared/i18n'
import type { MessageKey } from '../shared/i18n'
import { usePreferences } from '../shared/preferences'
import PrefRow from '../shared/PrefRow.vue'
import { useRowFilter } from './rowFilter'

const props = withDefaults(defineProps<{ query?: string }>(), { query: '' })
const emit = defineEmits<{ reset: [] }>()
const shows = useRowFilter(() => props.query)
const prefs = usePreferences()
const lib = useLibrary()

// Every category, as the engine names them (the generated enum also carries Go's zero value).
const byName = new Map<string, PrefCategory>(Object.values(PrefCategory).map((c) => [c, c]))
const categories = PREF_CATEGORIES.flatMap((name) => byName.get(name) ?? [])

const resetNote = ref('')
const resetError = ref('')
async function resetAll() {
  resetNote.value = ''
  resetError.value = ''
  if (!(await confirm(t('settings.resetAllTitle'), t('settings.resetAllText'), t('settings.resetAll')))) return
  emit('reset')
  try {
    await lib.resetPreferences(categories)
    resetNote.value = t('settings.resetAllDone')
  } catch (e) {
    resetError.value = errorText(e)
  }
}

const exportNote = ref('')
const exportError = ref('')
async function exportFile() {
  exportNote.value = ''
  exportError.value = ''
  try {
    const name = await engine.exportCustomisation()
    if (name) exportNote.value = t('settings.presetExported', { name })
  } catch (e) {
    exportError.value = errorText(e)
  }
}

// What each preference a file may change is called, by its name in the file.
const labels: Record<string, MessageKey> = {
  theme: 'settings.pref.theme',
  accent: 'settings.pref.accent',
  density: 'settings.pref.density',
  textScale: 'settings.pref.textScale',
  tileSize: 'settings.pref.tileSize',
  sidebarCollapsed: 'settings.pref.sidebar',
  sidebarWidth: 'settings.pref.sidebar',
  inspectorWidth: 'settings.pref.inspectorWidth',
  motion: 'settings.motion',
  viewerBackground: 'settings.viewerBackground',
  startView: 'settings.startView',
  thumbFit: 'settings.thumbFit',
  focusRing: 'settings.focusRing',
  hideFavouriteBadge: 'settings.badges',
  hideVideoBadge: 'settings.badges',
  hideDurationBadge: 'settings.badges',
  shortcuts: 'settings.shortcuts',
  exportPresets: 'settings.exportPresets',
  'viewer.filmstrip': 'viewer.filmstrip',
  'viewer.autoHide': 'viewer.autoHide',
  'viewer.slideshowSeconds': 'viewer.interval',
  'viewer.loop': 'viewer.slideshowLoop',
  'viewer.autoplay': 'viewer.autoplay',
  'viewer.volume': 'viewer.volume',
  'viewer.muted': 'viewer.startMuted',
  'viewer.speed': 'viewer.speed',
  'viewer.skipSeconds': 'viewer.skipInterval',
  'viewer.resume': 'viewer.resume',
  'viewer.open': 'viewer.open',
  'viewer.wheel': 'viewer.wheel',
  'viewer.stillAnimations': 'viewer.animations',
  'viewer.renderSide': 'viewer.renderSide',
  'viewer.videoLoop': 'viewer.videoLoop',
  'viewer.subtitles': 'viewer.subtitles',
  'viewer.subtitleLanguage': 'viewer.subtitleLanguage',
}

const preview = shallowRef<CustomisationPreview | null>(null)
const open = computed({
  get: () => preview.value !== null,
  set: (value) => {
    if (!value) preview.value = null
  },
})
// Several names in the file can be one setting on screen (the three badges).
const changes = computed(() => [
  ...new Set((preview.value?.changed ?? []).map((name) => (labels[name] ? t(labels[name]) : name))),
])
const importNote = ref('')
const importError = ref('')
async function chooseFile() {
  importNote.value = ''
  importError.value = ''
  try {
    const read = await engine.readCustomisation()
    if (read.name) preview.value = read
  } catch (e) {
    importError.value = errorText(e)
  }
}
async function apply() {
  const read = preview.value
  if (!read) return
  if (!(await prefs.set(read.preferences))) return
  preview.value = null
  importNote.value = t('settings.presetApplied')
}
</script>
<template>
  <div data-customisation>
    <PrefRow
      v-if="shows('settings.resetAll', 'settings.resetAllHint')"
      :label="t('settings.resetAll')"
      :description="t('settings.resetAllHint')"
      :hint="resetNote"
      :error="resetError"
    >
      <template #default>
        <UButton data-reset-all color="error" variant="outline" loading-auto @click="resetAll">{{
          t('settings.resetAll')
        }}</UButton>
      </template>
    </PrefRow>
    <PrefRow
      v-if="shows('settings.presetExport', 'settings.presetExportHint')"
      :label="t('settings.presetExport')"
      :description="t('settings.presetExportHint')"
      :hint="exportNote"
      :error="exportError"
    >
      <template #default>
        <UButton data-preset-export loading-auto @click="exportFile">{{ t('settings.presetExportButton') }}</UButton>
      </template>
    </PrefRow>
    <PrefRow
      v-if="shows('settings.presetImport', 'settings.presetImportHint')"
      :label="t('settings.presetImport')"
      :description="t('settings.presetImportHint')"
      :hint="importNote"
      :error="importError"
    >
      <template #default>
        <UButton data-preset-import loading-auto @click="chooseFile">{{ t('settings.presetImportButton') }}</UButton>
      </template>
    </PrefRow>
    <UModal
      v-model:open="open"
      :title="t('settings.presetPreviewTitle', { name: preview?.name ?? '' })"
      :description="t(changes.length ? 'settings.presetPreviewHint' : 'settings.presetPreviewNone')"
    >
      <template v-if="changes.length" #body>
        <ul class="changes" data-preset-changes>
          <li v-for="change in changes" :key="change">{{ change }}</li>
        </ul>
      </template>
      <template #footer>
        <UButton variant="outline" @click="preview = null">{{ t('common.cancel') }}</UButton>
        <UButton v-if="changes.length" data-preset-apply @click="apply">{{ t('settings.presetApply') }}</UButton>
      </template>
    </UModal>
  </div>
</template>
<style scoped>
.changes {
  margin: 0;
  padding-left: 1.25rem;
  list-style: disc;
}
</style>
