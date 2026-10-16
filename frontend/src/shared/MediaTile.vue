<script setup lang="ts">
// One photo or video in a grid, wherever grids appear (the library, a
// block's covered media, duplicate groups). It always shows exactly its own
// item: the image is keyed by the item, so a reused cell never flashes the
// previous photo, and the placeholder holds the cell's shape until the
// thumbnail arrives. Tiles do not animate while the grid scrolls.
//
// Selectable tiles carry a round check at the top left: it appears on
// hover, on focus within the tile and while the grid is selecting, and a
// click on it toggles the tile without opening it. The grid itself says
// what is selected (aria-selected on its options), so the check is for the
// pointer and is hidden from assistive technology. Quick actions (#actions)
// sit at the top right and appear the same way.
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
  /** Shows the selection check. */
  checkable?: boolean
  /** The grid has a selection: every check shows, so the next is one click. */
  selecting?: boolean
  /**
   * contain shows the whole picture; cover fills the tile. Left out, the
   * tile follows the thumbnail preference (cover unless the user chose).
   */
  fit?: 'cover' | 'contain'
}>()
const emit = defineEmits<{ check: [event: MouseEvent] }>()
</script>

<template>
  <div class="media-tile" :class="{ selected, selecting, contain: fit === 'contain', cover: fit === 'cover' }">
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
    <span class="shade" aria-hidden="true" />
    <span v-if="checkable && id" class="check" aria-hidden="true" data-tile-check @click.stop="emit('check', $event)">
      <Icon name="check" :size="14" />
    </span>
    <span v-if="video && !(durationMs && id)" class="badge kind"><Icon name="video" :size="14" /></span>
    <span v-if="video && durationMs" class="badge duration"
      ><Icon name="play" :size="10" filled />{{ duration(durationMs) }}</span
    >
    <span v-if="favourite" class="badge fav"><Icon name="heart" :size="16" filled /></span>
    <div v-if="$slots.actions && id" class="actions">
      <slot name="actions" />
    </div>
    <slot />
  </div>
</template>

<style scoped>
.media-tile {
  position: relative;
  width: 100%;
  height: 100%;
  overflow: hidden;
  isolation: isolate;
  border-radius: var(--lumen-r-sm);
  background: var(--lumen-placeholder);
  transition: background-color var(--lumen-d-fast) var(--lumen-ease-out);
}
img,
.placeholder {
  display: block;
  width: 100%;
  height: 100%;
  object-fit: cover;
}
img {
  transition:
    transform var(--lumen-d-slow) var(--lumen-ease-out),
    border-radius var(--lumen-d-base) var(--lumen-ease-out);
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
    90deg,
    var(--lumen-placeholder) 0%,
    color-mix(in srgb, var(--lumen-placeholder) 55%, var(--lumen-bg-raised)) 50%,
    var(--lumen-placeholder) 100%
  );
  background-size: 200% 100%;
  animation: lumen-shimmer 1.6s linear infinite;
}

/* A soft shade at the top and bottom, under the check and the badges, on
   hover, focus and while selecting. */
.shade {
  position: absolute;
  inset: 0;
  z-index: 1;
  background: linear-gradient(180deg, rgb(0 0 0 / 0.38) 0%, transparent 28%, transparent 70%, rgb(0 0 0 / 0.38) 100%);
  opacity: 0;
  pointer-events: none;
  transition: opacity var(--lumen-d-base) var(--lumen-ease-out);
}
.media-tile:hover .shade,
.media-tile:focus-within .shade,
:focus-visible > .media-tile .shade,
.selecting .shade {
  opacity: 1;
}
.media-tile:hover img {
  transform: scale(1.03);
}

/* Selected: the photo draws in from the tile's edge over the accent's
   tint, and the check fills. */
.selected {
  background: var(--lumen-primary-soft-2);
}
.selected img,
.selected:hover img {
  transform: scale(0.86);
  border-radius: var(--lumen-r-sm);
}
.selected .shade {
  opacity: 0;
}

.check {
  position: absolute;
  top: 0.5rem;
  left: 0.5rem;
  z-index: 2;
  display: grid;
  place-items: center;
  width: 24px;
  height: 24px;
  border: 2px solid rgb(255 255 255 / 0.9);
  border-radius: 50%;
  background: rgb(0 0 0 / 0.15);
  color: transparent;
  cursor: pointer;
  opacity: 0;
  transform: scale(0.8);
  transition:
    opacity var(--lumen-d-fast) var(--lumen-ease-out),
    transform var(--lumen-d-base) var(--lumen-ease-spring),
    background-color var(--lumen-d-fast) var(--lumen-ease-out);
}
.check :deep(path) {
  stroke-width: 3.2;
}
.media-tile:hover .check,
.media-tile:focus-within .check,
:focus-visible > .media-tile .check,
.selecting .check {
  opacity: 1;
  transform: none;
}
.check:hover {
  background: rgb(0 0 0 / 0.35);
}
.selected .check {
  opacity: 1;
  transform: none;
  border-color: var(--ui-primary);
  background: var(--ui-primary);
  color: var(--lumen-on-primary);
}

.actions {
  position: absolute;
  top: 0.375rem;
  right: 0.375rem;
  z-index: 2;
  display: flex;
  gap: 0.25rem;
  opacity: 0;
  transform: translateY(-4px);
  transition:
    opacity var(--lumen-d-fast) var(--lumen-ease-out),
    transform var(--lumen-d-base) var(--lumen-ease-out);
}
.media-tile:hover .actions,
.media-tile:focus-within .actions,
:focus-visible > .media-tile .actions {
  opacity: 1;
  transform: none;
}
/* A quick action is a small glass button over the photo. */
.actions :deep(button) {
  display: grid;
  place-items: center;
  width: 28px;
  height: 28px;
  border: 0;
  border-radius: var(--lumen-r-sm);
  background: rgb(10 12 18 / 0.45);
  color: #fff;
  backdrop-filter: blur(8px);
  cursor: pointer;
  transition: background-color var(--lumen-d-fast) var(--lumen-ease-out);
}
.actions :deep(button:hover) {
  background: rgb(10 12 18 / 0.7);
}
/* Touch has no hover: the check and actions are always there. */
@media (pointer: coarse) {
  .check,
  .actions {
    opacity: 1;
    transform: none;
  }
}

.badge {
  position: absolute;
  z-index: 2;
  display: inline-flex;
  align-items: center;
  gap: 0.25rem;
  color: #fff;
  filter: drop-shadow(0 1px 2px rgb(0 0 0 / 0.5));
  pointer-events: none;
}
.badge.kind {
  top: 0.5rem;
  right: 0.5rem;
}
.badge.fav {
  bottom: 0.5rem;
  left: 0.5rem;
}
.badge.duration {
  right: 0.5rem;
  bottom: 0.5rem;
  height: 1.25rem;
  padding: 0 0.375rem;
  border-radius: var(--lumen-r-xs);
  background: rgb(0 0 0 / 0.5);
  backdrop-filter: blur(6px);
  font-size: 0.6875rem;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
  filter: none;
}
@media (forced-colors: active) {
  .selected {
    outline: 3px solid Highlight;
    outline-offset: -3px;
  }
}
</style>
