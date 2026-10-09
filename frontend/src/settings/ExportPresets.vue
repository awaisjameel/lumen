<script setup lang="ts">
// The saved export presets: a name for a format, size and quality, offered
// in the export dialog. A preset never holds a destination, so it can be
// carried in a customisation file; where files go is chosen at each export.
import UButton from '@nuxt/ui/components/Button.vue'
import UCheckbox from '@nuxt/ui/components/Checkbox.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import USelect from '@nuxt/ui/components/Select.vue'
import { computed, ref } from 'vue'
import {
  EXPORT_QUALITY_DEFAULT,
  EXPORT_QUALITY_MAX,
  EXPORT_QUALITY_MIN,
  ExportFormat,
  MAX_EXPORT_PRESETS,
  MAX_NAME_LEN,
  RENDER_SIDE,
  RENDER_SIDE_MIN,
} from '../bridge'
import type { ExportPreset } from '../bridge'
import { icons } from '../design/icons'
import { t } from '../shared/i18n'
import { usePreferences } from '../shared/preferences'
import IconButton from '../shared/IconButton.vue'
import PrefRow from '../shared/PrefRow.vue'

const prefs = usePreferences()
const presets = computed(() => prefs.prefs.exportPresets)
const full = computed(() => presets.value.length >= MAX_EXPORT_PRESETS)

const formats = [ExportFormat.ExportOriginal, ExportFormat.ExportJPEG, ExportFormat.ExportPNG] as const
const formatLabel = (format: ExportFormat) =>
  format === ExportFormat.ExportJPEG
    ? t('files.jpeg')
    : format === ExportFormat.ExportPNG
      ? t('files.png')
      : t('files.original')
const formatItems = computed(() => formats.map((value) => ({ value, label: formatLabel(value) })))

const name = ref('')
const format = ref<(typeof formats)[number]>(ExportFormat.ExportJPEG)
const side = ref(String(RENDER_SIDE))
const quality = ref(String(EXPORT_QUALITY_DEFAULT))
const adjust = ref(true)
const error = ref('')

function summary(p: ExportPreset): string {
  if (p.format === ExportFormat.ExportOriginal) return t('settings.presetOriginal')
  return p.format === ExportFormat.ExportJPEG
    ? t('settings.presetRenderedQuality', { format: formatLabel(p.format), side: p.side, quality: p.quality })
    : t('settings.presetRendered', { format: formatLabel(p.format), side: p.side })
}

const within = (value: string, min: number, max: number, fallback: number) => {
  const n = Math.round(Number(value))
  return Number.isFinite(n) && n > 0 ? Math.min(max, Math.max(min, n)) : fallback
}

async function add() {
  error.value = ''
  const title = name.value.trim()
  if (!title || title.length > MAX_NAME_LEN) {
    error.value = t('settings.presetNameNeeded', { n: MAX_NAME_LEN })
    return
  }
  if (presets.value.some((p) => p.name.toLocaleLowerCase() === title.toLocaleLowerCase())) {
    error.value = t('settings.presetNameTaken')
    return
  }
  const saved = await prefs.setExportPresets([
    ...presets.value,
    {
      name: title,
      format: format.value,
      side: within(side.value, RENDER_SIDE_MIN, RENDER_SIDE, RENDER_SIDE),
      quality: within(quality.value, EXPORT_QUALITY_MIN, EXPORT_QUALITY_MAX, EXPORT_QUALITY_DEFAULT),
      adjust: adjust.value,
    },
  ])
  if (!saved) return
  if (!presets.value.some((preset) => preset.name === title)) {
    error.value = t('settings.presetNameTaken')
    return
  }
  name.value = ''
}

function remove(preset: ExportPreset) {
  void prefs.setExportPresets(presets.value.filter((p) => p.name !== preset.name))
}
</script>
<template>
  <PrefRow
    :label="t('settings.exportPresets')"
    :description="t('settings.exportPresetsHint')"
    :error="error"
    :hint="full ? t('settings.presetFull', { n: MAX_EXPORT_PRESETS }) : ''"
  >
    <template #default>
      <div class="presets" data-export-presets>
        <p v-if="!presets.length" class="text-dimmed">{{ t('settings.presetNone') }}</p>
        <ul v-else>
          <li v-for="preset in presets" :key="preset.name" :data-preset="preset.name">
            <span class="name lumen-selectable">{{ preset.name }}</span>
            <span class="text-muted">{{ summary(preset) }}</span>
            <IconButton
              size="xs"
              variant="ghost"
              :icon="icons.close"
              :label="t('settings.presetRemove', { name: preset.name })"
              @click="remove(preset)"
            />
          </li>
        </ul>
        <form v-if="!full" class="add" @submit.prevent="add">
          <UInput
            v-model="name"
            data-preset-name
            :maxlength="MAX_NAME_LEN"
            :placeholder="t('settings.presetName')"
            :aria-label="t('settings.presetName')"
          />
          <USelect v-model="format" :items="formatItems" :aria-label="t('settings.presetFormat')" />
          <template v-if="format !== ExportFormat.ExportOriginal">
            <UInput
              v-model="side"
              type="number"
              class="w-24"
              :min="RENDER_SIDE_MIN"
              :max="RENDER_SIDE"
              :aria-label="t('files.size')"
              :title="t('files.size')"
            />
            <UInput
              v-if="format === ExportFormat.ExportJPEG"
              v-model="quality"
              type="number"
              class="w-20"
              :min="EXPORT_QUALITY_MIN"
              :max="EXPORT_QUALITY_MAX"
              :aria-label="t('files.quality')"
              :title="t('files.quality')"
            />
            <UCheckbox v-model="adjust" :label="t('files.adjust')" />
          </template>
          <UButton type="submit" data-preset-add :icon="icons.plus">{{ t('settings.presetAdd') }}</UButton>
        </form>
      </div>
    </template>
  </PrefRow>
</template>
<style scoped>
.presets {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
  width: 100%;
}

.presets p {
  margin: 0;
}

ul {
  margin: 0;
  padding: 0;
  list-style: none;
}

li {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  min-height: var(--lumen-row-h);
}

.name {
  font-weight: 600;
  color: var(--ui-text-highlighted);
}

li .text-muted {
  flex: 1;
  font-size: 0.8125rem;
}

.add {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.5rem;
}
</style>
