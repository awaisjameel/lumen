<script setup lang="ts">
import UApp from '@nuxt/ui/components/App.vue'
import { defineAsyncComponent, onMounted, ref } from 'vue'
import { RouterView } from 'vue-router'
import CrashNotice from './about/CrashNotice.vue'
import AppNotifications from './app/AppNotifications.vue'
import AppOverlays from './app/AppOverlays.vue'
import AppShell from './app/AppShell.vue'
import { useLibrary } from './library/store'
import { errorText, logError } from './shared/errors'
import FeatureState from './shared/FeatureState.vue'
import { t } from './shared/i18n'
import { usePreferences } from './shared/preferences'
import { useFullscreenPortal } from './shared/overlay'

// Onboarding shows once, before the first folder: not part of every start.
const Welcome = defineAsyncComponent(() => import('./onboarding/Welcome.vue'))

const lib = useLibrary()
const prefs = usePreferences()
const portal = useFullscreenPortal()
const failed = ref('')
onMounted(() => {
  // The interface's look is not worth holding the library back for: without
  // it the last appearance applied (or the defaults) stays.
  void prefs.load().catch(logError)
  lib.init().catch((e: unknown) => (failed.value = errorText(e)))
})
</script>

<template>
  <UApp :portal="portal" :toaster="{ position: 'bottom-center', expand: false }" :tooltip="{ delayDuration: 400 }">
    <AppNotifications>
      <template v-if="lib.ready">
        <Welcome v-if="!lib.sources.length" />
        <AppShell v-else>
          <RouterView />
        </AppShell>
      </template>
      <FeatureState v-else-if="failed" class="failed" kind="error" :title="t('app.failed')" :text="failed" />
      <AppOverlays />
      <CrashNotice />
    </AppNotifications>
  </UApp>
</template>

<style scoped>
.failed {
  height: 100%;
}
</style>
