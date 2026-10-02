<script setup lang="ts">
// The Trash view's toolbar: what is in it, and what can be done with all of it.
import { useLibrary } from '../library/store'
import { t } from '../shared/i18n'
import { deleteAllFromDisk, restoreAll } from './actions'

const lib = useLibrary()
</script>

<template>
  <div v-if="lib.total" class="trash-bar">
    <p>{{ t('trash.summary', { n: lib.total }) }}</p>
    <button class="btn" @click="restoreAll(lib)">{{ t('trash.restoreAll') }}</button>
    <button class="btn" @click="deleteAllFromDisk(lib, lib.total)">{{ t('trash.deleteAll') }}</button>
  </div>
</template>

<style scoped>
.trash-bar {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px 12px;
  padding: 0 20px 8px;
}
p {
  flex: 1;
  min-width: 240px;
  margin: 0;
  color: var(--text-2);
}
</style>
