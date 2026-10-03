<script setup lang="ts">
// After a crash: Lumen says so once, and offers the problem report the user
// may choose to share. Nothing is sent anywhere.
import { onMounted, ref } from 'vue'
import { errorText } from '../shared/errors'
import { t } from '../shared/i18n'
import Icon from '../shared/Icon.vue'
import { useAbout } from './store'

const about = useAbout()
onMounted(() => void about.load())

const error = ref('')
const savedTo = ref('')

async function saveReport() {
  error.value = ''
  try {
    savedTo.value = await about.saveReport()
  } catch (e) {
    error.value = errorText(e)
  }
}
</script>

<template>
  <div v-if="about.crashNotice" class="crash" role="alert">
    <Icon name="info" :size="18" />
    <div class="text">
      <p>{{ t('about.crashed') }}</p>
      <p v-if="savedTo" class="muted" role="status">{{ t('about.saved', { path: savedTo }) }}</p>
      <p v-else class="muted">{{ t('about.crashedHint') }}</p>
      <p v-if="error" class="error">{{ error }}</p>
    </div>
    <button v-if="!savedTo" class="btn" :disabled="about.savingReport" @click="saveReport">
      {{ about.savingReport ? t('about.saving') : t('about.saveReport') }}
    </button>
    <button class="icon-btn" :title="t('common.dismiss')" :aria-label="t('common.dismiss')" @click="about.dismissCrash">
      <Icon name="close" :size="16" />
    </button>
  </div>
</template>

<style scoped>
.crash {
  position: fixed;
  top: 16px;
  left: 50%;
  z-index: 60;
  transform: translateX(-50%);
  display: flex;
  align-items: flex-start;
  gap: 12px;
  width: max-content;
  max-width: min(600px, calc(100vw - 32px));
  padding: 12px 12px 12px 16px;
  border: 1px solid var(--border);
  border-radius: var(--radius-l);
  background: var(--surface);
  box-shadow: 0 8px 24px rgb(0 0 0 / 0.18);
}

.crash > svg {
  flex-shrink: 0;
  margin-top: 2px;
  color: var(--accent);
}

.text p {
  margin: 0;
}

.text p + p {
  margin-top: 2px;
  font-size: 12px;
}
</style>
