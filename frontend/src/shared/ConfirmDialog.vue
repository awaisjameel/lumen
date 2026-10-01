<script setup lang="ts">
// The app's one modal question (see confirm.ts). A native <dialog>, so focus
// is trapped and Escape dismisses it.
import { nextTick, ref, watch } from 'vue'
import { question } from './confirm'
import { t } from './i18n'

const dialog = ref<HTMLDialogElement | null>(null)

watch(question, async (q) => {
  await nextTick()
  if (q && !dialog.value?.open) dialog.value?.showModal()
  if (!q && dialog.value?.open) dialog.value.close()
})
</script>

<template>
  <dialog ref="dialog" class="dialog confirm" aria-labelledby="confirm-title" @cancel.prevent="question?.answer(false)">
    <template v-if="question">
      <h2 id="confirm-title">{{ question.title }}</h2>
      <p>{{ question.text }}</p>
      <div class="actions">
        <!-- Cancel is focused: Enter must not delete or merge by accident. -->
        <button class="btn" autofocus @click="question.answer(false)">{{ t('common.cancel') }}</button>
        <button class="btn btn-primary" @click="question.answer(true)">{{ question.ok }}</button>
      </div>
    </template>
  </dialog>
</template>

<style scoped>
.confirm {
  width: min(420px, calc(100vw - 32px));
  padding: 20px 22px 18px;
}
p {
  margin: 0 0 18px;
  color: var(--text-2);
}
</style>
