<script setup lang="ts">
// The "Find text in screenshots and documents" toggle. Off by default. Its
// saves are queued in the library store, so they stay in order across
// remounts.
import { useLibrary } from '../library/store'
import { t } from '../shared/i18n'

const lib = useLibrary()

function toggle(e: Event) {
  if (e.target instanceof HTMLInputElement) void lib.saveOCR(e.target.checked)
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
