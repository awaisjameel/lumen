<script setup lang="ts">
import UButton from '@nuxt/ui/components/Button.vue'
import { ref } from 'vue'
import { engine } from '../bridge'
import BrandMark from '../shared/BrandMark.vue'
import FeatureState from '../shared/FeatureState.vue'
import { errorText } from '../shared/errors'
import { t } from '../shared/i18n'

defineProps<{ error: string; busy: boolean }>()
const emit = defineEmits<{ retry: [] }>()
const saving = ref(false)
const saved = ref('')
const saveError = ref('')
async function save() {
  if (saving.value) return
  saving.value = true
  saveError.value = ''
  try {
    const path = await engine.saveProblemReport()
    if (path) saved.value = path
  } catch (e) {
    saveError.value = errorText(e)
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <main class="startup" data-startup-error>
    <header>
      <BrandMark /><span>{{ t('app.name') }}</span>
    </header>
    <FeatureState
      :kind="busy && !error ? 'loading' : 'error'"
      :title="busy ? t('onboarding.retrying') : t('onboarding.failed')"
      :text="t('onboarding.failedHint')"
    >
      <p v-if="error" class="detail">{{ error }}</p>
      <div class="actions">
        <UButton color="primary" variant="solid" :loading="busy" :disabled="busy" @click="emit('retry')">{{
          busy ? t('onboarding.retrying') : t('welcome.retry')
        }}</UButton
        ><UButton :disabled="saving" :loading="saving" color="neutral" variant="outline" @click="save">{{
          saving ? t('crash.saving') : saveError ? t('crash.retry') : t('crash.save')
        }}</UButton>
      </div>
      <p v-if="saveError" role="alert">{{ saveError }}</p>
      <p v-if="saved" role="status">{{ t('about.saved', { path: saved }) }}</p>
      <p class="detail">{{ t('crash.safe') }}</p>
    </FeatureState>
  </main>
</template>

<style scoped>
.startup {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: auto;
  background: var(--lumen-bg-base);
}
header {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  padding: 1.25rem 2rem;
  border-bottom: 1px solid var(--ui-border);
  font-weight: 600;
}
.startup :deep(.feature-state) {
  flex: 1;
}
.actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: center;
  gap: 0.5rem;
}
p {
  margin: 0;
  overflow-wrap: anywhere;
}
.detail {
  font-size: 0.75rem;
  color: var(--ui-text-dimmed);
}
</style>
