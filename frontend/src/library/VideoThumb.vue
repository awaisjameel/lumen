<script setup lang="ts">
import { computed, ref } from 'vue'
import { SPRITE_COLS, SPRITE_ROWS } from '../bridge'
import type { GridItem } from '../bridge'
import LibraryThumb from './LibraryThumb.vue'
import { videoSpriteStyle } from '../shared/videoSprite'

const props = defineProps<{ item: GridItem; cell: number }>()
const frame = ref(-1)
const count = SPRITE_COLS * SPRITE_ROWS

function move(e: PointerEvent) {
  if (e.pointerType === 'touch' || !props.item.t) return
  const rect = e.currentTarget instanceof Element ? e.currentTarget.getBoundingClientRect() : null
  if (!rect?.width) return
  frame.value = Math.max(0, Math.min(count - 1, Math.floor(((e.clientX - rect.left) / rect.width) * count)))
}

const sprite = computed(() => {
  const ratio = props.item.w > 0 && props.item.h > 0 ? props.item.w / props.item.h : 16 / 9
  const w = Math.ceil(props.cell * Math.max(1, ratio))
  const h = Math.ceil(props.cell * Math.max(1, 1 / ratio))
  return {
    ...videoSpriteStyle(props.item.id, props.item.t, frame.value, w, h),
    left: `${(props.cell - w) / 2}px`,
    top: `${(props.cell - h) / 2}px`,
  }
})
</script>

<template>
  <span class="video-thumb" @pointermove="move" @pointerleave="frame = -1">
    <LibraryThumb v-show="frame < 0" :id="item.id" :thumb="item.t" />
    <span v-if="frame >= 0" class="sprite-frame" :style="sprite" />
  </span>
</template>

<style scoped>
.video-thumb {
  display: block;
  position: absolute;
  inset: 0;
  overflow: hidden;
}
.video-thumb img {
  display: block;
  width: 100%;
  height: 100%;
  object-fit: cover;
}
.sprite-frame {
  position: absolute;
  display: block;
  background-repeat: no-repeat;
}
</style>
