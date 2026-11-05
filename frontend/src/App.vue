<script setup lang="ts">
import UApp from '@nuxt/ui/components/App.vue'
import { defineAsyncComponent, onMounted, ref } from 'vue'
import NavigationView from './shared/navigation/NavigationView.vue'
import { useNavigation } from './shared/navigation/router'
import { destination, startDestination } from './shared/navigation/contract'
import CrashNotice from './about/CrashNotice.vue'
import AppNotifications from './app/AppNotifications.vue'
import AppOverlays from './app/AppOverlays.vue'
import AppShell from './app/AppShell.vue'
import UpdateNotice from './updates/UpdateNotice.vue'
import { useLibrary } from './library/store'
import { errorText, logError } from './shared/errors'
import StartupError from './onboarding/StartupError.vue'
import { engine, StartView } from './bridge'
import { notificationToaster } from './shared/notify'
import { usePreferences } from './shared/preferences'
import { useFullscreenPortal } from './shared/overlay'

// Onboarding shows once, before the first folder: not part of every start.
const Welcome = defineAsyncComponent(() => import('./onboarding/Welcome.vue'))

const lib = useLibrary()
const prefs = usePreferences()
const portal = useFullscreenPortal()
const failed = ref('')
const onboarding = ref(false)
const router = useNavigation()
// Whether the user has gone anywhere yet: the start view only ever replaces
// the screen Lumen opened on.
let navigated = false
const stopCounting = router.afterEach((hadPrevious) => {
  if (hadPrevious) navigated = true
})

/** Opens on the screen the user chose, if Lumen is still where it started. */
async function openStartView() {
  await router.ready()
  stopCounting()
  const view = prefs.prefs.startView
  const location = router.location.value
  if (navigated || view === StartView.StartLibrary || location.kind !== 'valid' || location.target.name !== 'library')
    return
  const target = startDestination(view)
  if (target) await router.replace(target)
}

const retrying = ref(false)
async function initialise() {
  if (retrying.value) return
  retrying.value = true
  failed.value = ''
  try {
    await engine.updateHealth()
  } catch (e) {
    failed.value = errorText(e)
    retrying.value = false
    return
  }
  // The interface's look is not worth holding the library back for: without
  // it the last appearance applied (or the defaults) stays.
  void prefs.load().then(openStartView).catch(logError)
  try {
    await lib.init()
  } catch (e) {
    failed.value = errorText(e)
  } finally {
    retrying.value = false
  }
}
onMounted(() => void initialise())

async function finishOnboarding() {
  await router.replace(destination({ name: 'library' }))
  onboarding.value = false
}
</script>

<template>
  <UApp :portal="portal" :toaster="notificationToaster" :tooltip="{ delayDuration: 400 }">
    <AppNotifications>
      <template v-if="lib.ready">
        <Welcome v-if="!lib.sources.length || onboarding" @starting="onboarding = true" @complete="finishOnboarding" />
        <AppShell v-else>
          <NavigationView />
        </AppShell>
      </template>
      <StartupError v-else-if="failed || retrying" :error="failed" :busy="retrying" @retry="initialise" />
      <AppOverlays />
      <UpdateNotice />
      <CrashNotice />
    </AppNotifications>
  </UApp>
</template>
