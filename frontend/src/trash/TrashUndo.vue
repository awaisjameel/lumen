<script setup lang="ts">
// The Notice's offer to undo "Move to Trash": restoring what was just moved.
import { useLibrary } from '../library/store'
import { reportError } from '../shared/errors'
import { t } from '../shared/i18n'
import Icon from '../shared/Icon.vue'
import { clearTrashUndo, trashUndo } from './undo'

const lib = useLibrary()

async function undo() {
  const ids = trashUndo.value
  if (!ids) return
  clearTrashUndo()
  try {
    await lib.restoreAssets(ids)
  } catch (e) {
    reportError(e)
  }
}
</script>

<template>
  <p v-if="trashUndo" class="notice success" role="status">
    <span>{{ t('trash.moved', { n: trashUndo.length }) }}</span>
    <button class="btn btn-subtle" @click="undo">{{ t('trash.undo') }}</button>
    <button class="icon-btn" :title="t('common.dismiss')" :aria-label="t('common.dismiss')" @click="clearTrashUndo">
      <Icon name="close" :size="16" />
    </button>
  </p>
</template>

<style scoped>
.notice {
  position: fixed;
  left: 50%;
  bottom: 88px; /* above the Notice region's own line */
  z-index: 60;
  transform: translateX(-50%);
  max-width: min(560px, calc(100vw - 32px));
  display: flex;
  align-items: center;
  gap: 12px;
  margin: 0;
  padding: 8px 8px 8px 16px;
  border: 1px solid var(--border);
  border-radius: var(--radius-l);
  background: var(--surface);
  color: var(--text);
  box-shadow: var(--shadow);
}
</style>
