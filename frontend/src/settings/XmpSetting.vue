<script setup lang="ts">
// The XMP sidecar toggle and its "Export now" button. Off by default; while
// it is on, Lumen keeps the sidecars up to date in the background, and the
// button runs a full pass at once and says what it did. Its saves are queued
// in the library store, so they stay in order across remounts. "Delete
// Lumen's sidecar files" is always offered, on or off, so the user can take
// back everything Lumen wrote: it turns the setting off first, then moves
// Lumen's own sidecars to the Recycle Bin.
import { ref } from 'vue'
import { engine } from '../bridge'
import type { XMPDelete, XMPExport } from '../bridge'
import { useLibrary } from '../library/store'
import { confirm } from '../shared/confirm'
import { errorText } from '../shared/errors'
import { t } from '../shared/i18n'

const lib = useLibrary()
const result = ref<XMPExport | null>(null)
const exporting = ref(false)
const deleted = ref<XMPDelete | null>(null)
const deleting = ref(false)

function toggle(e: Event) {
  if (!(e.target instanceof HTMLInputElement)) return
  if (!e.target.checked) result.value = null
  deleted.value = null
  void lib.saveXMP(e.target.checked)
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
</script>

<template>
  <div>
    <label class="toggle">
      <input type="checkbox" :checked="lib.settings.xmp" :disabled="deleting" @change="toggle" />
      <span>
        {{ t('settings.xmp') }}
        <small class="muted">{{ t('settings.xmpHint') }}</small>
      </span>
    </label>
    <div v-if="lib.settings.xmp" class="export">
      <button class="btn" :disabled="exporting || deleting" @click="exportNow">
        {{ exporting ? t('settings.xmpExporting') : t('settings.xmpExport') }}
      </button>
      <p v-if="result" class="muted result" role="status">
        {{ t('settings.xmpResult', { written: result.written, unchanged: result.unchanged, skipped: result.skipped }) }}
        <template v-if="result.skipped > 0"> {{ t('settings.xmpSkippedHint') }}</template>
      </p>
    </div>
    <div class="export">
      <button class="btn" :disabled="exporting || deleting" @click="deleteAll">
        {{ deleting ? t('settings.xmpDeleting') : t('settings.xmpDelete') }}
      </button>
      <p v-if="deleted" class="muted result" role="status">
        {{ deleted.deleted > 0 ? t('settings.xmpDeleted', { n: deleted.deleted }) : t('settings.xmpDeletedNone') }}
        <template v-if="deleted.kept > 0"> {{ t('settings.xmpKept', { n: deleted.kept }) }}</template>
      </p>
      <small v-else class="muted">{{ t('settings.xmpDeleteHint') }}</small>
    </div>
  </div>
</template>

<style scoped>
.toggle {
  display: flex;
  gap: 10px;
  align-items: flex-start;
  padding: 8px 0;
  cursor: pointer;
}

.toggle input {
  accent-color: var(--accent);
  width: 16px;
  height: 16px;
  margin-top: 2px;
}

.toggle small {
  display: block;
}

.export {
  display: flex;
  gap: 12px;
  align-items: center;
  flex-wrap: wrap;
  padding: 4px 0 8px 26px;
}

.result {
  margin: 0;
}
</style>
