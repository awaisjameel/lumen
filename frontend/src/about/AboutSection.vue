<script setup lang="ts">
// Settings → About Lumen: the version and where new ones are published, the
// log and a problem report the user may choose to share, the licences, and
// the credit GeoNames' licence asks for. Lumen itself never goes online:
// the links open in the user's browser.
import { onMounted, ref } from 'vue'
import { engine, Link } from '../bridge'
import { errorText } from '../shared/errors'
import { t } from '../shared/i18n'
import Icon from '../shared/Icon.vue'
import { useAbout } from './store'

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
</script>

<template>
  <div class="about">
    <p v-if="about.about" class="version">{{ t('about.version', { version: about.about.version }) }}</p>
    <p class="muted hint">
      {{ t('about.updates') }}
      <a v-if="about.about" href="#" @click.prevent="run(() => engine.openLink(Link.LinkReleases))">{{
        about.about.releasesURL
      }}</a>
    </p>
    <div class="row">
      <button class="btn" @click="run(engine.openLogFolder)">
        <Icon name="folder" :size="16" />{{ t('about.openLogs') }}
      </button>
      <button class="btn" :disabled="about.savingReport" @click="saveReport">
        {{ about.savingReport ? t('about.saving') : t('about.saveReport') }}
      </button>
      <button class="btn" @click="run(engine.openNotices)">{{ t('about.licences') }}</button>
    </div>
    <p class="muted hint">{{ t('about.reportHint') }}</p>
    <p v-if="savedTo" class="muted" role="status">{{ t('about.saved', { path: savedTo }) }}</p>
    <p v-if="error" class="error" role="alert">{{ error }}</p>
    <p class="muted credit">
      {{ t('about.places') }}
      <a href="#" @click.prevent="run(() => engine.openLink(Link.LinkGeoNames))">GeoNames</a>
      {{ t('about.placesLicence') }}
    </p>
  </div>
</template>

<style scoped>
.version {
  margin: 0 0 4px;
}

.hint {
  margin: 0 0 12px;
}

.row {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  margin-bottom: 8px;
}

.credit {
  margin: 12px 0 0;
  font-size: 12px;
}

a {
  color: var(--accent);
}
</style>
