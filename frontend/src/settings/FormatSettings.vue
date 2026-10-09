<script setup lang="ts">
// Which photo formats indexing takes in. Turning one off stops new files of
// it being added or read again; what the library already holds of it stays,
// and nothing on disk is touched. Turning it on again has the folders
// checked, so its files are found. Videos have their own switch.
import { computed } from 'vue'
import USwitch from '@nuxt/ui/components/Switch.vue'
import { EXCLUDABLE_FAMILIES } from '../bridge'
import { useLibrary } from '../library/store'
import { t } from '../shared/i18n'
import PrefRow from '../shared/PrefRow.vue'
import { useSaveFeedback } from './saveFeedback'

const lib = useLibrary()
const { save, hint } = useSaveFeedback(() => lib.settingErrors.excludedFormats)

const formats = computed(() => EXCLUDABLE_FAMILIES.map((family) => ({ family, label: t(`settings.format.${family}`) })))
const off = computed(() => lib.settings.excludedFormats.length)
</script>
<template>
  <PrefRow
    :label="t('settings.formats')"
    :description="t('settings.formatsHint')"
    :hint="hint('excludedFormats', off ? t('settings.formatsOff', { n: off }) : '')"
    :error="lib.settingErrors.excludedFormats"
  >
    <template #default>
      <div class="formats">
        <USwitch
          v-for="format in formats"
          :key="format.family"
          :data-format="format.family"
          :label="format.label"
          :model-value="!lib.settings.excludedFormats.includes(format.family)"
          @update:model-value="
            (on: boolean) => save('excludedFormats', () => lib.saveFormatExcluded(format.family, !on))
          "
        />
      </div>
    </template>
  </PrefRow>
</template>
<style scoped>
.formats {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, max-content));
  gap: 0.5rem 1.5rem;
}
</style>
