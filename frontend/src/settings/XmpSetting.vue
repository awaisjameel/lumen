<script setup lang="ts">
// The XMP sidecar toggle and its "Export now" button. Off by default; while
// it is on, Lumen keeps the sidecars up to date in the background, and the
// button runs a full pass at once and says what it did. Its saves are queued
// in the library store, so they stay in order across remounts. "Delete
// Lumen's sidecar files" is always offered, on or off, so the user can take
// back everything Lumen wrote: it turns the setting off first, then moves
// Lumen's own sidecars to the Recycle Bin.
import UButton from '@nuxt/ui/components/Button.vue'
import USwitch from '@nuxt/ui/components/Switch.vue'
import { ref } from 'vue'
import PrefRow from '../shared/PrefRow.vue'
import { icons } from '../design/icons'
import { useCommands, runCommand } from '../shared/commands'
import { engine } from '../bridge'
import type { XMPDelete, XMPExport } from '../bridge'
import { useLibrary } from '../library/store'
import { confirm } from '../shared/confirm'
import { errorText } from '../shared/errors'
import { t } from '../shared/i18n'
import type { MessageKey } from '../shared/i18n'

const props = withDefaults(defineProps<{ query?: string }>(), { query: '' })
function matches(...keys: MessageKey[]) {
  const needle = props.query.trim().toLocaleLowerCase()
  return !needle || keys.some((key) => t(key).toLocaleLowerCase().includes(needle))
}

const lib = useLibrary()
const result = ref<XMPExport | null>(null)
const exporting = ref(false)
const deleted = ref<XMPDelete | null>(null)
const deleting = ref(false)

const pending = ref(0)
const changed = ref(false)
async function toggle(value: boolean) {
  if (!value) result.value = null
  deleted.value = null
  changed.value = true
  pending.value++
  try {
    await lib.saveXMP(value)
  } finally {
    pending.value--
  }
}

async function exportNow() {
  exporting.value = true
  lib.settingsError = ''
  result.value = null
  try {
    result.value = await engine.exportXMPNow()
  } catch (e) {
    lib.settingsError = errorText(e)
  } finally {
    exporting.value = false
  }
}

async function deleteAll() {
  if (!(await confirm(t('settings.xmpDeleteTitle'), t('settings.xmpDeleteText'), t('settings.xmpDeleteOk')))) return
  deleting.value = true
  result.value = null
  deleted.value = null
  try {
    // Through the store's queue, so the box and any save still on its way agree.
    await lib.saveXMP(false)
    if (lib.settings.xmp) return // the save failed, and said why
    deleted.value = await engine.deleteXMP()
  } catch (e) {
    lib.settingsError = errorText(e)
  } finally {
    deleting.value = false
  }
}
useCommands([
  {
    id: 'settings.xmpExport',
    group: 'library',
    title: () => t('settings.xmpExport'),
    icon: 'sidecar',
    available: () => lib.settings.xmp,
    enabled: () => !exporting.value && !deleting.value,
    run: exportNow,
  },
  {
    id: 'settings.xmpDelete',
    group: 'library',
    title: () => t('settings.xmpDelete'),
    icon: 'recycle',
    enabled: () => !exporting.value && !deleting.value,
    run: deleteAll,
  },
])
</script>

<template>
  <div>
    <PrefRow
      v-if="matches('settings.xmp', 'settings.xmpHint')"
      :label="t('settings.xmp')"
      :description="t('settings.xmpHint')"
      :error="changed ? lib.settingsError : ''"
      :hint="pending ? t('settings.saving') : changed ? t('settings.saved') : ''"
    >
      <template #default="{ labelledBy, describedBy }"
        ><USwitch
          :model-value="lib.settings.xmp"
          :disabled="deleting"
          :aria-labelledby="labelledBy"
          :aria-describedby="describedBy"
          @update:model-value="toggle"
      /></template>
    </PrefRow>
    <PrefRow
      v-if="lib.settings.xmp && matches('settings.xmpExport', 'settings.xmpExportHint')"
      :label="t('settings.xmpExport')"
      :description="t('settings.xmpExportHint')"
    >
      <UButton
        data-xmp="export"
        :icon="icons.sidecar"
        :loading="exporting"
        :disabled="exporting || deleting"
        @click="runCommand('settings.xmpExport')"
        >{{ exporting ? t('settings.xmpExporting') : t('settings.xmpExport') }}</UButton
      >
    </PrefRow>
    <p v-if="result && lib.settings.xmp" class="text-muted" role="status">
      {{ t('settings.xmpResult', { written: result.written, unchanged: result.unchanged, skipped: result.skipped })
      }}<template v-if="result.skipped > 0"> {{ t('settings.xmpSkippedHint') }}</template>
    </p>
    <PrefRow
      v-if="matches('settings.xmpDelete', 'settings.xmpDeleteHint')"
      :label="t('settings.xmpDelete')"
      :description="t('settings.xmpDeleteHint')"
    >
      <UButton
        data-xmp="delete"
        :icon="icons.recycle"
        :loading="deleting"
        :disabled="exporting || deleting"
        @click="runCommand('settings.xmpDelete')"
        >{{ deleting ? t('settings.xmpDeleting') : t('settings.xmpDelete') }}</UButton
      >
    </PrefRow>
    <p v-if="deleted" class="text-muted" role="status">
      {{ deleted.deleted > 0 ? t('settings.xmpDeleted', { n: deleted.deleted }) : t('settings.xmpDeletedNone')
      }}<template v-if="deleted.kept > 0"> {{ t('settings.xmpKept', { n: deleted.kept }) }}</template>
    </p>
  </div>
</template>
