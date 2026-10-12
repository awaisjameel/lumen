<script setup lang="ts">
// Tells the user, wherever they are, that an update is available, ready, or
// has just been installed: once per release, until they answer.
import { onMounted, watch } from 'vue'
import { useRouter } from 'vue-router'
import { UpdatePhase } from '../bridge'
import { notify } from '../shared/notify'
import { t } from '../shared/i18n'
import { useUpdates } from './store'

const updates = useUpdates()
const router = useRouter()
onMounted(() => void updates.start())
let announced = ''
watch(
  () => updates.status,
  (status) => {
    // Another window coordinates this installation, and tells the user itself.
    if (!status?.coordinator) return
    if (status.phase === UpdatePhase.UpdateComplete) {
      if (status.dismissed === status.installedVersion) return
      notify({
        id: 'update',
        kind: 'success',
        title: t('update.phase.complete'),
        description: t('update.completed', { version: status.installedVersion }),
      })
      void updates.dismiss()
      return
    }
    if (status.phase !== UpdatePhase.UpdateAvailable && status.phase !== UpdatePhase.UpdateReady) return
    if (!status.offer.version || status.dismissed === status.offer.version) return
    const key = `${status.offer.version}:${status.phase}`
    if (announced === key) return
    announced = key
    notify({
      id: 'update',
      kind: 'info',
      title: status.phase === UpdatePhase.UpdateReady ? t('update.phase.ready') : t('update.phase.available'),
      description: t('update.notice', { version: status.offer.version }),
      actions: [
        { label: t('update.title'), run: () => void router.push({ name: 'settings', query: { section: 'updates' } }) },
        { label: t('update.later'), run: () => void updates.dismiss() },
      ],
    })
  },
)
</script>

<template><span class="sr-only" /></template>
