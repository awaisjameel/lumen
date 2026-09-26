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
  <dialog ref="dialog" class="confirm" @cancel.prevent="question?.answer(false)">
    <template v-if="question">
      <h2>{{ question.title }}</h2>
      <p>{{ question.text }}</p>
      <div class="actions">
        <button class="btn" @click="question.answer(false)">{{ t('common.cancel') }}</button>
        <button class="btn btn-primary" autofocus @click="question.answer(true)">{{ question.ok }}</button>
      </div>
    </template>
  </dialog>
</template>

<style scoped>
.confirm {
  width: min(420px, calc(100vw - 32px));
  padding: 20px 22px 18px;
  border: 1px solid var(--border);
  border-radius: var(--radius-l);
  background: var(--surface);
  color: var(--text);
  box-shadow: var(--shadow);
}
.confirm::backdrop {
  background: rgb(0 0 0 / 0.4);
}
h2 {
  margin: 0 0 6px;
  font: 600 17px var(--font-display);
}
p {
  margin: 0 0 18px;
  color: var(--text-2);
}
.actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
</style>
