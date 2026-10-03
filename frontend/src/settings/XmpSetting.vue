<script setup lang="ts">
// The XMP sidecar toggle and its "Export now" button. Off by default; while
// it is on, Lumen keeps the sidecars up to date in the background, and the
// button runs a full pass at once and says what it did. Its saves are queued
// in the library store, so they stay in order across remounts.
import { ref } from 'vue'
import { engine } from '../bridge'
import type { XMPExport } from '../bridge'
import { useLibrary } from '../library/store'
import { errorText } from '../shared/errors'
import { t } from '../shared/i18n'

const lib = useLibrary()
const result = ref<XMPExport | null>(null)
const exporting = ref(false)

function toggle(e: Event) {
  if (!(e.target instanceof HTMLInputElement)) return
  if (!e.target.checked) result.value = null
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
</script>

<template>
  <div>
    <label class="toggle">
      <input type="checkbox" :checked="lib.settings.xmp" @change="toggle" />
      <span>
        {{ t('settings.xmp') }}
        <small class="muted">{{ t('settings.xmpHint') }}</small>
      </span>
    </label>
    <div v-if="lib.settings.xmp" class="export">
      <button class="btn" :disabled="exporting" @click="exportNow">
        {{ exporting ? t('settings.xmpExporting') : t('settings.xmpExport') }}
      </button>
      <p v-if="result" class="muted result" role="status">
        {{ t('settings.xmpResult', { written: result.written, unchanged: result.unchanged, skipped: result.skipped }) }}
        <template v-if="result.skipped > 0"> {{ t('settings.xmpSkippedHint') }}</template>
      </p>
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
