<script setup lang="ts">
// After a crash, the shared notification channel offers a local problem
// report until dismissed. Cancellation and failure keep the offer available.
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { errorText } from '../shared/errors'
import { t } from '../shared/i18n'
import { dismiss, notify } from '../shared/notify'
import { CRASH_NOTIFICATION, useAbout } from './store'

const about = useAbout()
const error = ref('')
const savedTo = ref('')
onMounted(() => void about.load())

async function saveReport() {
  if (!about.crashNotice || about.savingReport) return
  error.value = ''
  try {
    savedTo.value = await about.saveReport()
  } catch (e) {
    error.value = errorText(e)
  }
}

watch(
  [() => about.crashNotice, () => about.savingReport, error, savedTo],
  ([showing]) => {
    if (!showing) {
      dismiss(CRASH_NOTIFICATION)
      return
    }
    notify({
      id: CRASH_NOTIFICATION,
      kind: error.value ? 'error' : 'info',
      title: t('about.crashed'),
      icon: 'bug',
      description: savedTo.value
        ? t('about.saved', { path: savedTo.value })
        : [t('about.crashedHint'), error.value].filter(Boolean).join('\n'),
      duration: 0,
      actions: savedTo.value
        ? []
        : [
            {
              label: about.savingReport ? t('crash.saving') : error.value ? t('crash.retry') : t('crash.save'),
              disabled: about.savingReport,
              dismiss: false,
              run: () => void saveReport(),
            },
          ],
      closed: about.dismissCrash,
    })
  },
  { immediate: true },
)
onBeforeUnmount(() => dismiss(CRASH_NOTIFICATION))
</script>

<template>
  <slot />
</template>
