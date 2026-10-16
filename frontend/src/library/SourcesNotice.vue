<script setup lang="ts">
// Says when a library folder cannot be reached (an unplugged drive, a
// disconnected share, a renamed folder): which one, that its photos stay in
// the library until it is back, and a way to look again now. The engine
// checks folders in the background and says when that changes (the
// library store reloads the folders), so this never waits on a dead drive.
import UAlert from '@nuxt/ui/components/Alert.vue'
import { computed, ref } from 'vue'
import { engine, SourceAvailability } from '../bridge'
import { icons } from '../design/icons'
import { reportError } from '../shared/errors'
import { fileName } from '../shared/format'
import { t } from '../shared/i18n'
import { useLibrary } from './store'

const lib = useLibrary()
const missing = computed(() =>
  lib.sources.filter((s) => s.enabled && s.availability === SourceAvailability.SourceUnavailable),
)
const checking = ref(false)

const title = computed(() => {
  const [first] = missing.value
  if (!first) return ''
  return missing.value.length === 1
    ? t('sources.unavailable', { folder: fileName(first.path) || first.path })
    : t('sources.unavailableMany', { n: missing.value.length })
})

async function recheck() {
  checking.value = true
  try {
    await engine.recheckSources()
  } catch (e) {
    reportError(e)
  } finally {
    checking.value = false
  }
}
</script>

<template>
  <UAlert
    v-if="missing.length"
    class="sources-notice"
    data-sources-notice
    color="warning"
    variant="subtle"
    :icon="icons.warning"
    :title="title"
    :description="t('sources.unavailableHint')"
    role="status"
    :actions="[
      {
        label: t('sources.recheck'),
        color: 'neutral',
        variant: 'outline',
        loading: checking,
        onClick: recheck,
      },
    ]"
  />
</template>

<style scoped>
.sources-notice {
  flex-shrink: 0;
  width: auto;
  margin: 0 var(--lumen-page-x) 0.75rem;
  border-radius: var(--lumen-r-lg);
}
.sources-notice :deep([data-slot='title']) {
  overflow-wrap: anywhere;
}
</style>
