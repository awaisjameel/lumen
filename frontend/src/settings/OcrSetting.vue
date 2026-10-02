<script setup lang="ts">
// The "Find text in screenshots and documents" toggle. Off by default.
import { toRef } from 'vue'
import { engine } from '../bridge'
import { useLibrary } from '../library/store'
import { errorText } from '../shared/errors'
import { t } from '../shared/i18n'
import { latestSave } from '../shared/latestSave'

const lib = useLibrary()
const error = toRef(lib, 'settingsError')

// Clicks come faster than saves: one save at a time, the newest value last,
// and only the newest failing puts the box back.
const save = latestSave(engine.setOCR, lib.settings.ocr, {
  rollback: (value) => (lib.settings.ocr = value),
  failed: (e) => (error.value = errorText(e)),
})

function toggle(e: Event) {
  if (!(e.target instanceof HTMLInputElement)) return
  lib.settings.ocr = e.target.checked
  error.value = ''
  void save(e.target.checked)
}
</script>

<template>
  <label class="toggle">
    <input type="checkbox" :checked="lib.settings.ocr" @change="toggle" />
    <span>
      {{ t('settings.ocr') }}
      <small class="muted">{{ t('settings.ocrHint') }}</small>
    </span>
  </label>
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
</style>
