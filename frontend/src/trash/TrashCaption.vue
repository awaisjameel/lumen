<script setup lang="ts">
// Under a Trash tile: which file it is, and what deleting it from disk takes
// (its size, and how many files when it has copies). Deleting is about files,
// so the Trash names them where the library shows only the photo.
import { computed } from 'vue'
import type { GridItem } from '../bridge'
import { icons } from '../design/icons'
import { bytes } from '../shared/format'
import { t } from '../shared/i18n'
import IconButton from '../shared/IconButton.vue'

const props = defineProps<{ item: GridItem }>()
const emit = defineEmits<{ info: [] }>()

const name = computed(() => props.item.name || t(props.item.v ? 'library.video' : 'library.photo'))
const meta = computed(() =>
  [
    bytes(props.item.size ?? 0),
    t(props.item.v ? 'library.video' : 'library.photo'),
    (props.item.files ?? 0) > 1 ? t('trash.files', { n: props.item.files ?? 0 }) : '',
  ]
    .filter(Boolean)
    .join(' · '),
)
</script>

<template>
  <div class="trash-caption" data-trash-caption>
    <div class="text">
      <span class="name" :title="name">{{ name }}</span>
      <span class="meta">{{ meta }}</span>
    </div>
    <IconButton
      :icon="icons.info"
      :label="t('trash.details', { name })"
      color="neutral"
      variant="ghost"
      size="xs"
      @click.stop="emit('info')"
      @dblclick.stop
    />
  </div>
</template>

<style scoped>
.trash-caption {
  display: flex;
  align-items: flex-start;
  gap: 0.25rem;
  padding: 0.5rem 0 0;
  min-width: 0;
}
.text {
  display: grid;
  gap: 0.125rem;
  flex: 1;
  min-width: 0;
}
.name,
.meta {
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}
.name {
  color: var(--ui-text-highlighted);
  font-size: 0.8125rem;
  font-weight: 500;
}
.meta {
  color: var(--ui-text-dimmed);
  font-size: 0.75rem;
  font-variant-numeric: tabular-nums;
}
</style>
