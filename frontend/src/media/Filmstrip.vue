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
    estimateSize: () => 84,
    overscan: 3,
    getItemKey: (index: number) => props.items[index]?.id ?? index,
  })),
)
watch(
  [() => props.selected, () => props.items],
  async () => {
    await nextTick()
    const index = props.items.findIndex((item) => item.id === props.selected)
    if (index >= 0) virtualizer.value.scrollToIndex(index, { align: 'center' })
  },
  { immediate: true },
)
</script>
<template>
  <nav ref="scroller" class="filmstrip" :aria-label="t('viewer.filmstrip')">
    <div :style="{ width: `${virtualizer.getTotalSize()}px`, height: '72px', position: 'relative' }">
      <template v-for="cell in virtualizer.getVirtualItems()" :key="String(cell.key)">
        <button
          v-if="items[cell.index]"
          :aria-label="t('viewer.filmstripItem', { n: cell.index + 1 })"
          :aria-current="items[cell.index]?.id === selected ? 'true' : undefined"
          :style="{ transform: `translateX(${cell.start}px)` }"
          @click="emit('select', items[cell.index]?.id ?? selected)"
        >
          <MediaTile
            :id="items[cell.index]?.id ?? 0"
            :thumb="items[cell.index]?.t ?? 0"
            :video="items[cell.index]?.v"
            :duration-ms="items[cell.index]?.d"
            :selected="items[cell.index]?.id === selected"
            fit="contain"
          />
        </button>
      </template>
    </div>
  </nav>
</template>
<style scoped>
.filmstrip {
  position: absolute;
  bottom: 0;
  inset-inline: 0;
  height: 84px;
  overflow-x: auto;
  background: var(--lumen-viewer-bg);
}
button {
  position: absolute;
  top: 4px;
  left: 0;
  width: 80px;
  height: 68px;
  border: 0;
  padding: 0;
  cursor: pointer;
}
button:focus-visible {
  outline: 2px solid var(--ui-primary);
}
</style>
