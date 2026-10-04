<script setup lang="ts">
// The app's one modal question (see confirm.ts): focus is trapped inside it
// and returns to where it was, and Escape or a click outside declines.
import UButton from '@nuxt/ui/components/Button.vue'
import UModal from '@nuxt/ui/components/Modal.vue'
import { computed } from 'vue'
import { question } from './confirm'
import { t } from './i18n'

const open = computed({
  get: () => question.value !== null,
  // Dismissed (Escape, the backdrop): that is "no".
  set: (isOpen) => {
    if (!isOpen) question.value?.answer(false)
  },
})
</script>

<template>
  <UModal
    v-model:open="open"
    :title="question?.title ?? ''"
    :description="question?.text ?? ''"
    :close="false"
    :ui="{ content: 'max-w-md', footer: 'justify-end' }"
  >
    <template #footer>
      <!-- Cancel is focused: Enter must not delete or merge by accident. -->
      <UButton autofocus color="neutral" variant="outline" data-confirm="cancel" @click="question?.answer(false)">
        {{ t('common.cancel') }}
      </UButton>
      <UButton color="primary" variant="solid" data-confirm="ok" @click="question?.answer(true)">
        {{ question?.ok }}
      </UButton>
    </template>
  </UModal>
</template>
