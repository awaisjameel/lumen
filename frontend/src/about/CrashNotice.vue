<script setup lang="ts">
// After a crash: Lumen says so once, and offers the problem report the user
// may choose to share. Nothing is sent anywhere.
import { onMounted, ref } from 'vue'
import { errorText } from '../shared/errors'
import { t } from '../shared/i18n'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import { icons } from '../design/icons'
import { useAbout } from './store'

const about = useAbout()
onMounted(() => void about.load())

const error = ref('')
const savedTo = ref('')

async function saveReport() {
  error.value = ''
  try {
    savedTo.value = await about.saveReport()
  } catch (e) {
    error.value = errorText(e)
  }
}
</script>

<template>
  <div v-if="about.crashNotice" class="crash-notice" data-crash>
    <UAlert :title="t('about.crashed')" :icon="icons.info" color="neutral" variant="outline" role="alert" close>
      <template #description
        ><p v-if="savedTo" class="lumen-selectable break-words" role="status">
          {{ t('about.saved', { path: savedTo }) }}
        </p>
        <p v-else>{{ t('about.crashedHint') }}</p>
        <p v-if="error" class="text-error">{{ error }}</p></template
      >
      <template #actions
        ><UButton
          v-if="!savedTo"
          data-crash-report
          :disabled="about.savingReport"
          :loading="about.savingReport"
          :icon="icons.bug"
          @click="saveReport"
          >{{ about.savingReport ? t('about.saving') : t('about.saveReport') }}</UButton
        ></template
      >
      <template #close
        ><UButton
          data-crash-dismiss
          :icon="icons.close"
          :aria-label="t('common.dismiss')"
          color="neutral"
          variant="ghost"
          square
          @click="about.dismissCrash" /></template
    ></UAlert>
  </div>
</template>
<style scoped>
.crash-notice {
  position: fixed;
  top: 1rem;
  left: 50%;
  z-index: calc(var(--lumen-z-viewer) + 1);
  transform: translateX(-50%);
  width: min(38rem, calc(100vw - 2rem));
}
p {
  margin: 0;
}
</style>
