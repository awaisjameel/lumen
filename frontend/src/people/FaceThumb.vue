<script setup lang="ts">
// One face, cut out of its photo's thumbnail (no extra files): the face box,
// widened by a margin, fills a square. The small thumbnail is used when it
// has enough pixels for the size shown, else the large one. An <img>, not a
// CSS background, so faces off screen load only when scrolled to.
import { computed, ref, watch } from 'vue'
import { t } from '../shared/i18n'
import { frameUrl, THUMB_LARGE, THUMB_SMALL, thumbUrl } from '../bridge'
import type { Face } from '../bridge'
import Icon from '../shared/Icon.vue'

const props = withDefaults(defineProps<{ face: Face; size: number; round?: boolean; fill?: boolean }>(), {
  round: false,
})

const failed = ref(false)
watch(
  () => props.face,
  () => {
    failed.value = false
  },
)

const MARGIN = 1.6 // the square shows this many face widths

const image = computed(() => {
  const f = props.face
  if (!f.id || !f.thumb || f.w <= 0 || f.h <= 0) return null
  const w = f.assetW || 1
  const h = f.assetH || 1
  const side = Math.max(f.w * w, f.h * h) * MARGIN // in photo pixels
  const scale = props.size / side
  const cx = (f.x + f.w / 2) * w * scale
  const cy = (f.y + f.h / 2) * h * scale
  const shown = Math.max(w, h) * scale // the whole photo's long edge, as drawn
  const thumb = shown <= THUMB_SMALL * 1.25 ? THUMB_SMALL : THUMB_LARGE
  return {
    src: f.video ? frameUrl(f.assetId, f.frame) : `${thumbUrl(f.assetId, f.thumb, thumb)}&canonical=1`,
    style: {
      width: `${((w * scale) / props.size) * 100}%`,
      height: `${((h * scale) / props.size) * 100}%`,
      left: `${((props.size / 2 - cx) / props.size) * 100}%`,
      top: `${((props.size / 2 - cy) / props.size) * 100}%`,
    },
  }
})
</script>

<template>
  <span class="face" :class="{ round, fill }" :style="fill ? undefined : { width: size + 'px', height: size + 'px' }">
    <img
      v-if="image && !failed"
      :src="image.src"
      :style="image.style"
      alt=""
      loading="lazy"
      decoding="async"
      draggable="false"
      @error="failed = true"
    />
    <Icon v-else name="user" :size="Math.round(size / 2)" :title="t('person.previewUnavailable')" />
  </span>
</template>

<style scoped>
.face {
  position: relative;
  display: inline-grid;
  place-items: center;
  flex-shrink: 0;
  overflow: hidden;
  border-radius: var(--ui-radius);
  background-color: var(--lumen-placeholder);
  color: var(--ui-text-dimmed);
}
.face img {
  position: absolute;
  max-width: none;
}
.round {
  border-radius: 50%;
}
.fill {
  width: 100%;
  height: 100%;
}
</style>
