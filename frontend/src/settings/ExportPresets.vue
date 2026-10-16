<script setup lang="ts">
// The saved export presets: a name for a format, size and quality, offered
// in the export dialog. A preset never holds a destination, so it can be
// carried in a customisation file; where files go is chosen at each export.
import UButton from '@nuxt/ui/components/Button.vue'
import UCheckbox from '@nuxt/ui/components/Checkbox.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import USelect from '@nuxt/ui/components/Select.vue'
import UModal from '@nuxt/ui/components/Modal.vue'
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
import { notify } from '../shared/notify'
import { nameTooLong, trimName } from '../shared/names'

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
const open = ref(false)
function openEditor() {
  error.value = ''
  open.value = true
}

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
  const title = trimName(name.value)
  if (!title || nameTooLong(title)) {
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
  open.value = false
}

async function remove(preset: ExportPreset) {
  if (!(await prefs.setExportPresets(presets.value.filter((p) => p.name !== preset.name)))) return
  notify({
    id: 'settings.presetRemoved',
    kind: 'success',
    title: t('settings.presetRemoved', { name: preset.name }),
    actions: [
      {
        label: t('settings.undo'),
        run: () => {
          void prefs.setExportPresets([...presets.value, preset])
        },
      },
    ],
  })
}
</script>
<template>
  <PrefRow
    :label="t('settings.exportPresets')"
    :modified="presets.length > 0"
    :description="t('settings.exportPresetsHint')"
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
        <UButton
          data-preset-new
          :icon="icons.plus"
          :disabled="full"
          :title="full ? t('settings.presetFull', { n: MAX_EXPORT_PRESETS }) : undefined"
          @click="openEditor"
          >{{ t('settings.presetAdd') }}</UButton
        >
      </div>
    </template>
  </PrefRow>
  <UModal v-model:open="open" :title="t('settings.presetAdd')" :description="t('settings.exportPresetsHint')">
    <template #body>
      <form id="settings-export-preset" class="add" @submit.prevent="add">
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
        <p v-if="error" role="alert" class="text-error">{{ error }}</p>
      </form>
    </template>
    <template #footer
      ><UButton variant="outline" @click="open = false">{{ t('common.cancel') }}</UButton
      ><UButton
        type="submit"
        form="settings-export-preset"
        data-preset-add
        :disabled="prefs.saving"
        :loading="prefs.saving"
        >{{ t('settings.presetAdd') }}</UButton
      ></template
    >
  </UModal>
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
  flex-wrap: wrap;
  overflow-wrap: anywhere;
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
  flex-direction: column;
  align-items: stretch;
  gap: 1rem;
}
</style>
