<script setup lang="ts">
// Saves remain in the library store so rapid choices survive remounts.
import USwitch from '@nuxt/ui/components/Switch.vue'
import { ref } from 'vue'
import { useLibrary } from '../library/store'
import { t } from '../shared/i18n'
import PrefRow from '../shared/PrefRow.vue'

const lib = useLibrary()
const pending = ref(0)
const changed = ref(false)
async function toggle(value: boolean) {
  changed.value = true
  pending.value++
  try {
    await lib.saveOCR(value)
  } finally {
    pending.value--
  }
}
</script>
<template>
  <PrefRow
    :label="t('settings.ocr')"
    :description="t('settings.ocrHint')"
    :error="changed ? lib.settingsError : ''"
    :hint="pending ? t('settings.saving') : changed ? t('settings.saved') : ''"
  >
    <template #default="{ labelledBy, describedBy }">
      <USwitch
        :model-value="lib.settings.ocr"
        :aria-labelledby="labelledBy"
        :aria-describedby="describedBy"
        @update:model-value="toggle"
      />
    </template>
  </PrefRow>
</template>
