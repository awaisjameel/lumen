<script setup lang="ts">
import { computed, ref, watch, nextTick } from 'vue'
import { useVirtualizer } from '@tanstack/vue-virtual'
import type { GridItem } from '../bridge'
import { t } from '../shared/i18n'
import MediaTile from '../shared/MediaTile.vue'

const props = defineProps<{ items: GridItem[]; selected: number }>()
const emit = defineEmits<{ select: [id: number] }>()
const scroller = ref<HTMLElement | null>(null)
const virtualizer = useVirtualizer(
  computed(() => ({
    count: props.items.length,
    horizontal: true,
    getScrollElement: () => scroller.value,
    estimateSize: (index: number) => (props.items[index]?.id === props.selected ? 80 : 56),
    overscan: 3,
    getItemKey: (index: number) => props.items[index]?.id ?? index,
  })),
)
watch(
  [() => props.selected, () => props.items],
  async () => {
    await nextTick()
    virtualizer.value.measure()
    const index = props.items.findIndex((item) => item.id === props.selected)
    if (index >= 0) virtualizer.value.scrollToIndex(index, { align: 'center' })
  },
  { immediate: true },
)
</script>
<template>
  <nav ref="scroller" class="filmstrip" :aria-label="t('viewer.filmstrip')">
    <div
      :style="{
        width: `${virtualizer.getTotalSize()}px`,
        height: '3.5rem',
        position: 'relative',
        marginInline: 'auto',
      }"
    >
      <template v-for="cell in virtualizer.getVirtualItems()" :key="String(cell.key)">
        <button
          v-if="items[cell.index]"
          :aria-label="t('viewer.filmstripItem', { n: cell.index + 1 })"
          :aria-current="items[cell.index]?.id === selected ? 'true' : undefined"
          :style="{ transform: `translateX(${cell.start}px)`, width: `${cell.size - 8}px` }"
          @click="emit('select', items[cell.index]?.id ?? selected)"
        >
          <MediaTile
            :id="items[cell.index]?.id ?? 0"
            :thumb="items[cell.index]?.t ?? 0"
            :video="items[cell.index]?.v"
            :duration-ms="items[cell.index]?.d"
            fit="cover"
          />
        </button>
      </template>
    </div>
  </nav>
</template>
<style scoped>
.filmstrip {
  flex: none;
  width: 100%;
  min-width: 0;
  overflow-x: auto;
  overflow-y: hidden;
}
button {
  position: absolute;
  top: 0.25rem;
  left: 0;
  height: 3rem;
  border: 0;
  padding: 0;
  cursor: pointer;
  border-radius: var(--lumen-r-sm);
  overflow: hidden;
  opacity: 0.55;
  background: var(--lumen-viewer-control);
  transition:
    opacity var(--lumen-d-fast) var(--lumen-ease-out),
    transform var(--lumen-d-base) var(--lumen-ease-out);
}
button:hover,
button:focus-visible {
  opacity: 1;
}
button[aria-current='true'] {
  opacity: 1;
  outline: 2px solid var(--lumen-viewer-text);
  outline-offset: -2px;
}
button:focus-visible {
  outline: 2px solid var(--ui-primary);
  outline-offset: -2px;
}
@media (max-height: 540px) {
  button {
    height: 2rem;
  }
  .filmstrip {
    height: 2.5rem;
  }
}
</style>
