<script setup lang="ts">
import { computed } from 'vue'
import USelect from '@nuxt/ui/components/Select.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import { COMPRESSION_SAVINGS_MAX } from '../bridge'
import type { CompressionOptions } from '../bridge'
import { compressionModes, compressionEfforts } from '../shared/compression'
import PrefRow from '../shared/PrefRow.vue'
import { defaultPreferences, usePreferences } from '../shared/preferences'
import { t } from '../shared/i18n'
import { useRowFilter } from './rowFilter'

const props = withDefaults(defineProps<{ query?: string }>(), { query: '' })
const shows = useRowFilter(() => props.query)
const prefs = usePreferences()
const defaults = defaultPreferences()
const options = computed(() => prefs.prefs.compression)
const modes = compressionModes.map((value) => ({ value, label: t(`compression.mode.${value}`) }))
const efforts = compressionEfforts.map((value) => ({ value, label: t(`compression.effort.${value}`) }))
function save(patch: Partial<CompressionOptions>) {
  void prefs.setCompression(patch)
}
function number(event: Event, key: 'minSavings' | 'threads') {
  if (!(event.target instanceof HTMLInputElement)) return
  const n = Number(event.target.value)
  if (Number.isInteger(n)) save({ [key]: n })
}
</script>

<template>
  <div>
    <PrefRow
      v-if="shows('compression.mode')"
      :label="t('compression.mode')"
      :modified="options.mode !== defaults.compression.mode"
      :description="t('compression.metadata')"
    >
      <USelect
        :model-value="modes.find((item) => item.value === options.mode)?.value"
        :items="modes"
        :aria-label="t('compression.mode')"
        @update:model-value="
          (value) => {
            const mode = modes.find((item) => item.value === value)?.value
            if (mode) save({ mode })
          }
        "
      />
    </PrefRow>
    <PrefRow
      v-if="shows('compression.effort')"
      :label="t('compression.effort')"
      :modified="options.effort !== defaults.compression.effort"
    >
      <USelect
        :model-value="efforts.find((item) => item.value === options.effort)?.value"
        :items="efforts"
        :aria-label="t('compression.effort')"
        @update:model-value="
          (value) => {
            const effort = efforts.find((item) => item.value === value)?.value
            if (effort) save({ effort })
          }
        "
      />
    </PrefRow>
    <PrefRow
      v-if="shows('compression.minSavings')"
      :label="t('compression.minSavings')"
      :modified="options.minSavings !== defaults.compression.minSavings"
    >
      <UInput
        :model-value="options.minSavings"
        type="number"
        :min="0"
        :max="COMPRESSION_SAVINGS_MAX"
        :aria-label="t('compression.minSavings')"
        @change="number($event, 'minSavings')"
      />
    </PrefRow>
    <PrefRow
      v-if="shows('compression.threads')"
      :label="t('compression.threads')"
      :modified="options.threads !== defaults.compression.threads"
    >
      <UInput
        :model-value="options.threads"
        type="number"
        :min="1"
        :aria-label="t('compression.threads')"
        @change="number($event, 'threads')"
      />
    </PrefRow>
    <PrefRow
      v-if="shows('settings.compressionReview', 'settings.compressionReviewHint')"
      :label="t('settings.compressionReview')"
      :description="t('settings.compressionReviewHint')"
      ><UButton to="/compression" variant="outline">{{ t('settings.compressionOpen') }}</UButton></PrefRow
    >
  </div>
</template>
