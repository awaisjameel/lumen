<script setup lang="ts">
// Saves remain in the library store so rapid choices survive remounts.
import USwitch from '@nuxt/ui/components/Switch.vue'
import { useLibrary } from '../library/store'
import { t } from '../shared/i18n'
import PrefRow from '../shared/PrefRow.vue'
import { useSaveFeedback } from './saveFeedback'

const lib = useLibrary()
const { save, hint } = useSaveFeedback(() => lib.settingErrors.ocr)
</script>
<template>
  <PrefRow
    :label="t('settings.ocr')"
    :description="t('settings.ocrHint')"
    :error="lib.settingErrors.ocr"
    :hint="hint('ocr')"
  >
    <template #default="{ labelledBy, describedBy }">
      <USwitch
        :model-value="lib.settings.ocr"
        :aria-labelledby="labelledBy"
        :aria-describedby="describedBy"
        @update:model-value="(value) => save('ocr', () => lib.saveOCR(value))"
      />
    </template>
  </PrefRow>
</template>
