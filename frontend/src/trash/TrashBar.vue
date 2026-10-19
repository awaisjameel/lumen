<script setup lang="ts">
import UButton from '@nuxt/ui/components/Button.vue'
import { onBeforeUnmount, watch } from 'vue'
import { engine } from '../bridge'
import { icons } from '../design/icons'
import { useLibrary } from '../library/store'
import { runCommand, useCommands } from '../shared/commands'
import { t } from '../shared/i18n'
import { logError } from '../shared/errors'
import { useLiveReload } from '../shared/liveReload'
import { useTrashActions } from './actions'

const emit = defineEmits<{ files: [count: number] }>()
const lib = useLibrary()
const actions = useTrashActions(lib)
let revision = 0
async function refreshCount() {
  const mine = ++revision
  if (!lib.total) {
    emit('files', 0)
    return
  }
  try {
    const snapshot = await engine.trashSelection([])
    if (mine !== revision) return
    emit('files', snapshot.files)
  } catch (e) {
    if (mine === revision) logError(e)
  }
}
watch(
  () => lib.total,
  () => void refreshCount(),
  { immediate: true },
)
useLiveReload(refreshCount, (change) => change.assets)
onBeforeUnmount(() => revision++)
useCommands([
  {
    id: 'trash.restoreAll',
    group: 'library',
    title: () => t('trash.restoreAll'),
    icon: 'restore',
    available: () => lib.total > 0,
    enabled: () => !actions.reason.value,
    reason: () => actions.reason.value,
    run: () => actions.act('restore', true),
  },
  {
    id: 'trash.deleteAll',
    group: 'library',
    title: () => t('trash.deleteAll'),
    icon: 'recycle',
    danger: true,
    available: () => lib.total > 0,
    enabled: () => !actions.reason.value,
    reason: () => actions.reason.value,
    run: () => actions.act('recycle', true),
  },
])
</script>

<template>
  <!-- The Trash's own actions, in the page header. The page shows the count
  of files on disk this reports in its subtitle. -->
  <div v-if="lib.total" class="trash-actions" :aria-busy="actions.work.busy !== null">
    <span v-if="actions.work.busy" role="status" class="sr-only">
      {{ t(actions.work.busy === 'restore' ? 'trash.restoring' : 'trash.recycling') }}
    </span>
    <UButton
      :icon="icons.restore"
      :disabled="!!actions.reason.value"
      :title="actions.reason.value"
      :loading="actions.work.busy === 'restore'"
      color="neutral"
      variant="outline"
      data-trash-restore-all
      @click="runCommand('trash.restoreAll')"
      >{{ t('trash.restoreAll') }}</UButton
    >
    <UButton
      :disabled="!!actions.reason.value"
      :title="actions.reason.value"
      :loading="actions.work.busy === 'recycle'"
      color="error"
      variant="ghost"
      data-trash-delete-all
      @click="runCommand('trash.deleteAll')"
      >{{ t('trash.deleteAll') }}</UButton
    >
  </div>
</template>

<style scoped>
.trash-actions {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}
</style>
