<script setup lang="ts">
// Which photo formats indexing takes in. Turning one off stops new files of
// it being added or read again; what the library already holds of it stays,
// and nothing on disk is touched. Turning it on again has the folders
// checked, so its files are found. Videos have their own switch.
import { computed } from 'vue'
import UButton from '@nuxt/ui/components/Button.vue'
import { icons } from '../design/icons'
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
    :modified="off > 0"
    :description="t('settings.formatsHint')"
    :hint="hint('excludedFormats', off ? t('settings.formatsOff', { n: off }) : '')"
    :error="lib.settingErrors.excludedFormats"
  >
    <template #default>
      <div class="formats">
        <UButton
          v-for="format in formats"
          :key="format.family"
          :data-format="format.family"
          role="switch"
          variant="outline"
          :aria-checked="!lib.settings.excludedFormats.includes(format.family)"
          :icon="!lib.settings.excludedFormats.includes(format.family) ? icons.check : undefined"
          @click="
            save('excludedFormats', () =>
              lib.saveFormatExcluded(format.family, !lib.settings.excludedFormats.includes(format.family)),
            )
          "
          >{{ format.label }}</UButton
        >
      </div>
    </template>
  </PrefRow>
</template>
<style scoped>
.formats {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 0.5rem;
}
.formats :deep(button) {
  border-radius: var(--lumen-r-xl);
}
.formats :deep([aria-checked='true']) {
  color: var(--lumen-primary-text);
  background: var(--lumen-primary-soft);
}
</style>
