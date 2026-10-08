<script setup lang="ts">
// One photo or video in a grid, wherever grids appear (the library, a
// block's covered media, duplicate groups). It always shows exactly its own
// item: the image is keyed by the item, so a reused cell never flashes the
// previous photo, and the placeholder holds the cell's shape until the
// thumbnail arrives. Tiles do not animate while the grid scrolls.
import { thumbUrl } from '../bridge'
import type { ThumbSize } from '../bridge'
import { duration } from './format'
import Icon from './Icon.vue'

defineProps<{
  /** The asset's id; 0 while the cell's item is not loaded. */
  id: number
  /** The thumbnail's version; 0 while there is none. */
  thumb: number
  size?: ThumbSize
  video?: boolean
  /** The video's length in ms, shown when known. */
  durationMs?: number
  favourite?: boolean
  selected?: boolean
  /**
   * contain shows the whole picture; cover fills the tile. Left out, the
   * tile follows the thumbnail preference (cover unless the user chose).
   */
  fit?: 'cover' | 'contain'
}>()
</script>

<template>
  <div class="media-tile" :class="{ selected, contain: fit === 'contain', cover: fit === 'cover' }">
    <slot name="media">
      <img
        v-if="id && thumb"
        :key="`${id}:${thumb}`"
        :src="thumbUrl(id, thumb, size)"
        loading="lazy"
        decoding="async"
        draggable="false"
        alt=""
      />
      <span v-else class="placeholder" :class="{ waiting: !id }" />
    </slot>
    <span v-if="video" class="badge kind"><Icon name="video" :size="16" /></span>
    <span v-if="video && durationMs" class="badge duration">{{ duration(durationMs) }}</span>
    <span v-if="favourite" class="badge fav"><Icon name="heart" :size="16" filled /></span>
    <slot />
  </div>
</template>

<style scoped>
.media-tile {
  position: relative;
  width: 100%;
  height: 100%;
  overflow: hidden;
  border-radius: var(--ui-radius);
  background: var(--lumen-placeholder);
}
img,
.placeholder {
  display: block;
  width: 100%;
  height: 100%;
  object-fit: cover;
}
.contain img {
  object-fit: contain;
}
/* The thumbnail and badge preferences, set on the root by appearance.ts. */
:root[data-thumb-fit='contain'] .media-tile:not(.cover) img {
  object-fit: contain;
}
:root[data-hide-favourite] .badge.fav,
:root[data-hide-video] .badge.kind,
:root[data-hide-duration] .badge.duration {
  display: none;
}
/* Only a cell still waiting for its item shimmers; an item without a
   thumbnail yet is a still placeholder. */
.placeholder.waiting {
  background: linear-gradient(
    100deg,
    var(--lumen-placeholder) 40%,
    var(--ui-bg-elevated) 50%,
    var(--lumen-placeholder) 60%
  );
  background-size: 300% 100%;
  animation: lumen-shimmer 1.6s ease-in-out infinite;
}
@keyframes lumen-shimmer {
  from {
    background-position: 100% 0;
  }
  to {
    background-position: 0 0;
  }
}
.selected {
  outline: 3px solid var(--ui-primary);
  outline-offset: -3px;
}
.selected img {
  transform: scale(0.94);
  border-radius: var(--ui-radius);
}
.badge {
  position: absolute;
  display: grid;
  place-items: center;
  color: #fff;
  filter: drop-shadow(0 1px 2px rgb(0 0 0 / 0.6));
  pointer-events: none;
}
.badge.kind {
  top: 6px;
  right: 6px;
}
.badge.fav {
  bottom: 6px;
  left: 6px;
}
.badge.duration {
  right: 5px;
  bottom: 5px;
  padding: 2px 4px;
  border-radius: var(--ui-radius);
  background: rgb(0 0 0 / 0.68);
  font-size: 0.6875rem;
  filter: none;
}
@media (forced-colors: active) {
  .selected {
    outline-color: Highlight;
  }
}
</style>
