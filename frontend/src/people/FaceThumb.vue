<script setup lang="ts">
// One face, cut out of its photo's thumbnail with CSS (no extra files): the
// face box, widened by a margin, fills a square. The small thumbnail is used
// when it has enough pixels for the size shown, else the large one.
import { computed } from 'vue'
import { frameUrl, THUMB_LARGE, THUMB_SMALL, thumbUrl } from '../bridge'
import type { Face } from '../bridge'
import Icon from '../shared/Icon.vue'

const props = withDefaults(defineProps<{ face: Face; size: number; round?: boolean }>(), { round: false })

const MARGIN = 1.6 // the square shows this many face widths

const style = computed(() => {
  const f = props.face
  if (!f.id || !f.thumb) return null
  const w = f.assetW || 1
  const h = f.assetH || 1
  const side = Math.max(f.w * w, f.h * h) * MARGIN // in photo pixels
  const scale = props.size / side
  const cx = (f.x + f.w / 2) * w * scale
  const cy = (f.y + f.h / 2) * h * scale
  const shown = Math.max(w, h) * scale // the whole photo's long edge, as drawn
  const thumb = shown <= THUMB_SMALL * 1.25 ? THUMB_SMALL : THUMB_LARGE
  return {
    backgroundImage: `url("${f.video ? frameUrl(f.assetId, f.frame) : thumbUrl(f.assetId, f.thumb, thumb)}")`,
    backgroundSize: `${w * scale}px ${h * scale}px`,
    backgroundPosition: `${props.size / 2 - cx}px ${props.size / 2 - cy}px`,
  }
})
</script>

<template>
  <span class="face" :class="{ round }" :style="[{ width: size + 'px', height: size + 'px' }, style ?? {}]">
    <Icon v-if="!style" name="user" :size="Math.round(size / 2)" />
  </span>
</template>

<style scoped>
.face {
  display: inline-grid;
  place-items: center;
  flex-shrink: 0;
  border-radius: var(--radius-m);
  background-color: var(--placeholder);
  background-repeat: no-repeat;
  color: var(--text-3);
}
.round {
  border-radius: 50%;
}
</style>
