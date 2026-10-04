<script setup lang="ts">
// The confirmation lists exactly what moves. Cancel receives focus so
// pressing Enter cannot remove files accidentally.
import UButton from '@nuxt/ui/components/Button.vue'
import UModal from '@nuxt/ui/components/Modal.vue'
import { computed, watch } from 'vue'
import { DuplicateKind } from '../bridge'
import { icons } from '../design/icons'
import { t } from '../shared/i18n'
import MediaTile from '../shared/MediaTile.vue'
import type { Review } from './store'

const props = defineProps<{ review: Review | null; busy: boolean }>()
const emit = defineEmits<{ confirm: []; cancel: [] }>()
const open = computed({
  get: () => props.review !== null,
  set: (value) => {
    if (!value && !props.busy) emit('cancel')
  },
})
const exact = computed(() => props.review?.kind === DuplicateKind.DuplicateExact)
const keeps = computed(() => (props.review ? (exact.value ? [props.review.keepPath] : props.review.keep.paths) : []))
const removes = computed(() =>
  props.review ? (exact.value ? props.review.recycle : props.review.trash.flatMap((item) => item.paths)) : [],
)
const count = computed(() => (exact.value ? props.review?.recycle.length : props.review?.trash.length) ?? 0)
let trigger: HTMLElement | null = null
watch(
  () => props.review,
  (review, previous) => {
    if (review && !previous) trigger = document.activeElement instanceof HTMLElement ? document.activeElement : null
  },
)
function returnFocus(event: Event) {
  if (!trigger?.isConnected) return
  event.preventDefault()
  trigger.focus()
}
</script>

<template>
  <UModal
    v-model:open="open"
    :title="t('duplicates.reviewTitle')"
    :description="t(exact ? 'duplicates.reviewRecycle' : 'duplicates.reviewTrash', { n: count })"
    :close="false"
    :dismissible="!busy"
    :content="{ onCloseAutoFocus: returnFocus }"
    :ui="{ content: 'max-w-4xl', footer: 'justify-end flex-wrap' }"
  >
    <template #body>
      <div v-if="review" class="comparison" data-duplicate-review>
        <section>
          <h3>{{ t('duplicates.reviewKeeps') }}</h3>
          <div class="review-tile"><MediaTile :id="review.keep.id" :thumb="review.keep.thumb" fit="contain" /></div>
          <ul class="keeps lumen-selectable">
            <li v-for="path in keeps" :key="path">{{ path }}</li>
          </ul>
        </section>
        <section>
          <h3>{{ t(exact ? 'duplicates.reviewOkRecycle' : 'duplicates.reviewOkTrash') }}</h3>
          <div class="removed-previews">
            <div v-if="exact" class="review-tile">
              <MediaTile :id="review.keep.id" :thumb="review.keep.thumb" fit="contain" />
            </div>
            <div v-for="item in review.trash" :key="item.id" class="review-tile">
              <MediaTile :id="item.id" :thumb="item.thumb" fit="contain" />
            </div>
          </div>
          <ul class="removes lumen-selectable">
            <li v-for="path in removes" :key="path">{{ path }}</li>
          </ul>
        </section>
      </div>
    </template>
    <template #footer>
      <UButton autofocus :disabled="busy" color="neutral" variant="outline" data-review-cancel @click="emit('cancel')">
        {{ t('common.cancel') }}
      </UButton>
      <UButton
        :loading="busy"
        :icon="exact ? icons.recycle : icons.trash"
        color="primary"
        variant="solid"
        data-review-confirm
        @click="emit('confirm')"
      >
        {{ t(exact ? 'duplicates.reviewOkRecycle' : 'duplicates.reviewOkTrash') }}
      </UButton>
    </template>
  </UModal>
</template>

<style scoped>
.comparison {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 1.25rem;
}
h3 {
  margin: 0 0 0.75rem;
  color: var(--ui-text-highlighted);
  font-weight: 600;
}
.review-tile {
  width: 8rem;
  height: 8rem;
  flex-shrink: 0;
}
.removed-previews {
  display: flex;
  gap: 0.5rem;
  overflow-x: auto;
}
ul {
  margin: 0.75rem 0 0;
  padding-left: 1.25rem;
  color: var(--ui-text);
  overflow-wrap: anywhere;
}
@media (max-width: 480px) {
  .comparison {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
