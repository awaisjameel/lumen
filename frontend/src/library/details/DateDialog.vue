<script setup lang="ts">
import { ref, watch } from 'vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import UModal from '@nuxt/ui/components/Modal.vue'
import { engine } from '../../bridge'
import type { AssetDetail, Selection } from '../../bridge'
import { t } from '../../shared/i18n'
import { dateInput, parseDateInput, useCorrection } from './corrections'

const props = defineProps<{ assets: AssetDetail[]; selection?: Selection | null }>()
const emit = defineEmits<{ changed: [] }>()
const open = defineModel<boolean>('open', { default: false })
const draft = ref('')
const { busy, error, save } = useCorrection(
  () => emit('changed'),
  () => (open.value = false),
)
watch(
  open,
  (value) => {
    if (value) {
      draft.value = props.assets[0] ? dateInput(props.assets[0].takenAt) : ''
      error.value = ''
    }
  },
  { immediate: true },
)
async function apply(restore = false) {
  const value = parseDateInput(draft.value)
  if ((!restore && value === undefined) || !props.assets.length) {
    error.value = t('details.date.invalid')
    return
  }
  const previous = props.assets.map((asset) => ({
    id: asset.id,
    takenAt: asset.takenAt,
    corrected: !!asset.originalDate,
  }))
  const ids = previous.map((asset) => asset.id)
  await save(
    'correct-date',
    t(restore ? 'details.date.restored' : 'details.date.saved'),
    async (current) => {
      if (props.selection) await engine.validateSelection(props.selection)
      if (
        !current() ||
        !open.value ||
        props.assets.map((asset) => asset.id).join(',') !== previous.map((asset) => asset.id).join(',')
      )
        return false
      if (restore) await engine.restoreTakenAt(ids)
      else if (value !== undefined) await engine.setTakenAt(ids, value)
      return true
    },
    async () => {
      for (const asset of previous) {
        if (asset.corrected) await engine.setTakenAt([asset.id], asset.takenAt)
        else await engine.restoreTakenAt([asset.id])
      }
    },
  )
}
</script>

<template>
  <UModal
    v-model:open="open"
    :title="t('details.date.title')"
    :description="t('details.date.hint')"
    :dismissible="!busy"
    :close="!busy"
  >
    <template #body>
      <UAlert v-if="error" color="error" :title="error" role="alert" class="mb-4" />
      <form id="correct-date" @submit.prevent="apply()">
        <label for="correct-date-input" class="mb-2 block text-sm font-semibold">{{ t('details.date.field') }}</label>
        <UInput
          id="correct-date-input"
          v-model="draft"
          class="w-full"
          type="datetime-local"
          step="1"
          min="1800-01-01T00:00"
          max="2199-12-31T23:59:59"
          :disabled="busy"
          :aria-invalid="!!error"
          :aria-label="t('details.date.field')"
        />
      </form>
    </template>
    <template #footer>
      <div class="w-full flex flex-wrap items-center justify-end gap-2">
        <UButton
          v-if="assets.some((asset) => asset.originalDate)"
          color="neutral"
          variant="ghost"
          :disabled="busy"
          @click="apply(true)"
          >{{ t('details.restoreOriginal') }}</UButton
        >
        <UButton color="neutral" variant="ghost" :disabled="busy" @click="open = false">{{
          t('common.cancel')
        }}</UButton>
        <UButton color="primary" variant="solid" type="submit" form="correct-date" :loading="busy" :disabled="busy">{{
          t('details.date.save')
        }}</UButton>
      </div>
    </template>
  </UModal>
</template>
