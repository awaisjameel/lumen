<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { thumbUrl } from '../bridge'
import { t } from '../shared/i18n'
import Icon from '../shared/Icon.vue'

const props = defineProps<{ id: number; thumb: number }>()
const failed = ref(false)
const attempt = ref(0)
watch(
  () => [props.id, props.thumb],
  () => {
    failed.value = false
    attempt.value = 0
  },
)
const src = computed(() => {
  const url = thumbUrl(props.id, props.thumb)
  return url && attempt.value ? `${url}&retry=${attempt.value}` : url
})
function retry() {
  attempt.value++
  failed.value = false
}
</script>

<template>
  <div v-if="failed" class="unavailable" role="status">
    <Icon name="image-off" :size="24" />
    <span>{{ t('library.thumbUnavailable') }}</span>
    <button type="button" @click.stop="retry" @dblclick.stop>{{ t('common.retry') }}</button>
  </div>
  <img
    v-else
    :key="`${id}:${thumb}:${attempt}`"
    :src="src"
    loading="lazy"
    decoding="async"
    draggable="false"
    alt=""
    @error="failed = true"
  />
</template>

<style scoped>
img {
  display: block;
  width: 100%;
  height: 100%;
  object-fit: cover;
}
:root[data-thumb-fit='contain'] img {
  object-fit: contain;
}
.unavailable {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 0.5rem;
  height: 100%;
  padding: 0.5rem;
  color: var(--ui-text-toned);
  font-size: 0.75rem;
  text-align: center;
}
.unavailable button {
  min-height: 1.5rem;
  padding: 0.25rem 0.5rem;
  border: 1px solid var(--ui-border-accented);
  border-radius: var(--lumen-r-sm);
  background: var(--lumen-bg-raised);
  cursor: pointer;
}
:global(.media-tile.selected) img {
  transform: scale(0.86);
  border-radius: var(--lumen-r-sm);
}
img {
  transition: transform var(--lumen-d-base) var(--lumen-ease-out);
}
</style>
