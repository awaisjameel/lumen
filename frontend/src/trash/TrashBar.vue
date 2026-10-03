<script setup lang="ts">
// The Trash view's toolbar: what is in it, and what can be done with all of it.
import UButton from '@nuxt/ui/components/Button.vue'
import { ref } from 'vue'
import { icons } from '../design/icons'
import { useLibrary } from '../library/store'
import { runCommand, useCommands } from '../shared/commands'
import { t } from '../shared/i18n'
import SelectionBar from '../shared/SelectionBar.vue'
import { deleteAllFromDisk, restoreAll } from './actions'

const lib = useLibrary()
const busy = ref<'restore' | 'recycle' | null>(null)
async function act(action: 'restore' | 'recycle') {
  if (busy.value || !lib.total) return
  busy.value = action
  try {
    if (action === 'restore') await restoreAll(lib)
    else await deleteAllFromDisk(lib)
  } finally {
    busy.value = null
  }
}
useCommands([
  {
    id: 'trash.restoreAll',
    group: 'library',
    title: () => t('trash.restoreAll'),
    icon: 'restore',
    available: () => lib.total > 0,
    enabled: () => busy.value === null,
    run: () => act('restore'),
  },
  {
    id: 'trash.deleteAll',
    group: 'library',
    title: () => t('trash.deleteAll'),
    icon: 'recycle',
    available: () => lib.total > 0,
    enabled: () => busy.value === null,
    run: () => act('recycle'),
  },
])
</script>

<template>
  <SelectionBar v-if="lib.total" :summary="t('trash.summary', { n: lib.total })" :hint="t('trash.recoveryHint')">
    <UButton
      :icon="icons.restore"
      :disabled="busy !== null"
      :loading="busy === 'restore'"
      color="primary"
      variant="soft"
      data-trash-restore-all
      @click="runCommand('trash.restoreAll')"
    >
      {{ t('trash.restoreAll') }}
    </UButton>
    <UButton
      :icon="icons.recycle"
      :disabled="busy !== null"
      :loading="busy === 'recycle'"
      color="neutral"
      variant="outline"
      data-trash-delete-all
      @click="runCommand('trash.deleteAll')"
    >
      {{ t('trash.deleteAll') }}
    </UButton>
  </SelectionBar>
</template>
