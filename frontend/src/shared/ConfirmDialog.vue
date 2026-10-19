<script setup lang="ts">
// The app's one modal question (see confirm.ts): focus is trapped inside it
// and returns to where it was, and Escape or a click outside declines. Its
// actions sit on the dialog's sunken footer, the confirming one last; one
// that removes something for good is red and says so with a warning mark.
import UButton from '@nuxt/ui/components/Button.vue'
import UModal from '@nuxt/ui/components/Modal.vue'
import { computed } from 'vue'
import { question } from './confirm'
import { t } from './i18n'
import Icon from './Icon.vue'

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
    :ui="{ content: 'max-w-md', description: 'whitespace-pre-line' }"
  >
    <template #title>
      <span class="title">
        <span v-if="question?.danger" class="mark" aria-hidden="true"><Icon name="warning" :size="18" /></span>
        {{ question?.title }}
      </span>
    </template>
    <template #footer>
      <!-- Cancel is focused: Enter must not delete or merge by accident. -->
      <UButton autofocus color="neutral" variant="outline" data-confirm="cancel" @click="question?.answer(false)">
        {{ t('common.cancel') }}
      </UButton>
      <UButton
        :color="question?.danger ? 'error' : 'primary'"
        variant="solid"
        data-confirm="ok"
        @click="question?.answer(true)"
      >
        {{ question?.ok }}
      </UButton>
    </template>
  </UModal>
</template>

<style scoped>
.title {
  display: flex;
  align-items: center;
  gap: 0.75rem;
}
.mark {
  display: grid;
  place-items: center;
  flex: none;
  width: 2rem;
  height: 2rem;
  border-radius: var(--lumen-r-md);
  background: color-mix(in srgb, var(--ui-error) 12%, transparent);
  color: var(--ui-error);
}
</style>
