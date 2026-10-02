<script setup lang="ts">
// The confirmation that lists exactly what a group's Review will do. Nothing
// is changed before the user presses the confirming button. A native
// <dialog>, so focus is trapped and Escape dismisses it.
import { computed, nextTick, ref, watch } from 'vue'
import { DuplicateKind } from '../bridge'
import { t } from '../shared/i18n'
import type { Review } from './store'

const props = defineProps<{ review: Review | null; busy: boolean }>()
const emit = defineEmits<{ confirm: []; cancel: [] }>()

const dialog = ref<HTMLDialogElement | null>(null)

const exact = computed(() => props.review?.kind === DuplicateKind.DuplicateExact)
// What is kept and what moves, as files: a photo is listed by every file it has.
const keeps = computed(() => (props.review ? (exact.value ? [props.review.keepPath] : props.review.keep.paths) : []))
const removes = computed(() =>
  props.review ? (exact.value ? props.review.recycle : props.review.trash.flatMap((item) => item.paths)) : [],
)
const count = computed(() => (exact.value ? props.review?.recycle.length : props.review?.trash.length) ?? 0)

watch(
  () => props.review,
  async (review) => {
    await nextTick()
    if (review && !dialog.value?.open) dialog.value?.showModal()
    if (!review && dialog.value?.open) dialog.value.close()
  },
)
</script>

<template>
  <dialog ref="dialog" class="dialog review" aria-labelledby="duplicates-review-title" @cancel.prevent="emit('cancel')">
    <template v-if="review">
      <h2 id="duplicates-review-title">{{ t('duplicates.reviewTitle') }}</h2>
      <h3>{{ t('duplicates.reviewKeeps') }}</h3>
      <ul class="keeps">
        <li v-for="path in keeps" :key="path">{{ path }}</li>
      </ul>
      <p>{{ t(exact ? 'duplicates.reviewRecycle' : 'duplicates.reviewTrash', { n: count }) }}</p>
      <ul class="removes">
        <li v-for="path in removes" :key="path">{{ path }}</li>
      </ul>
      <div class="actions">
        <!-- Cancel is focused: Enter must not move files by accident. -->
        <button class="btn" autofocus :disabled="busy" @click="emit('cancel')">{{ t('common.cancel') }}</button>
        <button class="btn btn-primary" :disabled="busy" @click="emit('confirm')">
          {{ t(exact ? 'duplicates.reviewOkRecycle' : 'duplicates.reviewOkTrash') }}
        </button>
      </div>
    </template>
  </dialog>
</template>

<style scoped>
.review {
  width: min(560px, calc(100vw - 32px));
  max-height: min(640px, calc(100vh - 32px));
  padding: 20px 22px 18px;
}

h3 {
  margin: 12px 0 4px;
  font-size: 14px;
  color: var(--text-2);
}

p {
  margin: 12px 0 4px;
  color: var(--text-2);
}

ul {
  max-height: 200px;
  margin: 0 0 12px;
  padding-left: 20px;
  overflow: auto;
  overflow-wrap: anywhere;
}
</style>
