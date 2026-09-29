<script setup lang="ts">
// The app-wide notice: a failure of something the user did that has no
// place of its own to say so (see reportError).
import { notice } from './errors'
import { t } from './i18n'
import Icon from './Icon.vue'
</script>

<template>
  <div class="notice-region">
    <p v-if="notice" class="notice" role="alert">
      <span>{{ notice }}</span>
      <button class="icon-btn" :title="t('common.dismiss')" :aria-label="t('common.dismiss')" @click="notice = ''">
        <Icon name="close" :size="16" />
      </button>
    </p>
  </div>
</template>

<style scoped>
.notice-region {
  position: fixed;
  left: 50%;
  bottom: 24px;
  z-index: 60;
  transform: translateX(-50%);
  max-width: min(560px, calc(100vw - 32px));
}
.notice {
  display: flex;
  align-items: center;
  gap: 12px;
  margin: 0;
  padding: 8px 8px 8px 16px;
  border: 1px solid var(--border);
  border-radius: var(--radius-l);
  background: var(--surface);
  color: var(--danger);
  box-shadow: var(--shadow);
}
</style>
