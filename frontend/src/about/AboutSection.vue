<script setup lang="ts">
// Settings → About Lumen: the version and where new ones are published, the
// log and a problem report the user may choose to share, the licences, and
// the credit GeoNames' licence asks for. Lumen itself never goes online:
// the links open in the user's browser.
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import { onMounted, ref } from 'vue'
import { icons } from '../design/icons'
import MetaRow from '../shared/MetaRow.vue'
import PrefRow from '../shared/PrefRow.vue'
import FeatureState from '../shared/FeatureState.vue'
import { useCommands, runCommand } from '../shared/commands'
import type { MessageKey } from '../shared/i18n'
import { engine, Link } from '../bridge'
import { errorText } from '../shared/errors'
import { t } from '../shared/i18n'
import { useAbout } from './store'

const props = withDefaults(defineProps<{ query?: string }>(), { query: '' })
function matches(...keys: MessageKey[]) {
  const needle = props.query.trim().toLocaleLowerCase()
  return !needle || keys.some((key) => t(key).toLocaleLowerCase().includes(needle))
}
const about = useAbout()
onMounted(() => void about.load())

const error = ref('')
const savedTo = ref('')

/** Runs an engine call, saying here why it failed. */
async function run(fn: () => Promise<unknown>) {
  error.value = ''
  try {
    await fn()
  } catch (e) {
    error.value = errorText(e)
  }
}

function saveReport() {
  savedTo.value = ''
  return run(async () => (savedTo.value = await about.saveReport()))
}
useCommands([
  {
    id: 'about.logs',
    group: 'app',
    title: () => t('about.openLogs'),
    icon: 'folder',
    run: () => run(engine.openLogFolder),
  },
  {
    id: 'about.report',
    group: 'app',
    title: () => t('about.saveReport'),
    icon: 'bug',
    enabled: () => !about.savingReport,
    run: saveReport,
  },
  {
    id: 'about.licences',
    group: 'app',
    title: () => t('about.licences'),
    icon: 'document',
    run: () => run(engine.openNotices),
  },
])
</script>

<template>
  <div class="about-content">
    <FeatureState v-if="!about.about && !about.loadError" kind="loading" :title="t('about.loading')" />
    <MetaRow
      v-if="about.about && matches('about.versionLabel')"
      :label="t('about.versionLabel')"
      icon="info"
      :value="t('about.version', { version: about.about.version })"
    />
    <PrefRow
      v-if="matches('about.releases', 'about.updates')"
      :label="t('about.releases')"
      :description="t('about.updates')"
    >
      <UButton
        v-if="about.about"
        class="release-link"
        variant="link"
        :icon="icons.external"
        @click="run(() => engine.openLink(Link.LinkReleases))"
        >{{ about.about.releasesURL }}</UButton
      >
    </PrefRow>
    <PrefRow
      v-if="matches('about.openLogs', 'about.logsHint')"
      :label="t('about.openLogs')"
      :description="t('about.logsHint')"
    >
      <UButton :icon="icons.folder" loading-auto @click="runCommand('about.logs')">{{ t('about.openLogs') }}</UButton>
    </PrefRow>
    <PrefRow
      v-if="matches('about.saveReport', 'about.reportHint')"
      :label="t('about.saveReport')"
      :description="t('about.reportHint')"
    >
      <UButton
        :icon="icons.bug"
        :loading="about.savingReport"
        :disabled="about.savingReport"
        @click="runCommand('about.report')"
        >{{ about.savingReport ? t('about.saving') : t('about.saveReport') }}</UButton
      >
    </PrefRow>
    <p v-if="savedTo" class="text-muted lumen-selectable break-words" role="status">
      {{ t('about.saved', { path: savedTo }) }}
    </p>
    <PrefRow
      v-if="matches('about.licences', 'about.licencesHint')"
      :label="t('about.licences')"
      :description="t('about.licencesHint')"
    >
      <UButton :icon="icons.document" loading-auto @click="runCommand('about.licences')">{{
        t('about.licences')
      }}</UButton>
    </PrefRow>
    <UAlert
      v-if="error || about.loadError"
      :description="error || about.loadError"
      color="error"
      variant="subtle"
      role="alert"
    >
      <template #actions>
        <UButton v-if="about.loadError" loading-auto @click="about.load">{{ t('about.retry') }}</UButton>
      </template>
    </UAlert>
    <MetaRow v-if="matches('about.places', 'about.geonames')" :label="t('about.places')" icon="map">
      <UButton variant="link" @click="run(() => engine.openLink(Link.LinkGeoNames))">{{ t('about.geonames') }}</UButton>
      {{ t('about.placesLicence') }}</MetaRow
    >
  </div>
</template>
<style scoped>
.about-content {
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
}
.release-link {
  max-width: 100%;
  overflow-wrap: anywhere;
  white-space: normal;
  text-align: left;
}
</style>
