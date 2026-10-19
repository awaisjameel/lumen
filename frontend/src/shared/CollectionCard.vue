<script setup lang="ts">
// The card every collection uses: a person, a Thing, an album (a square
// cover over its name), a place or an event (the cover beside its text).
// The cover and the name both open the collection; the card's own actions
// go in the default slot (shown on hover or focus for tiles), and badges
// over the cover in #badge.
import { computed } from 'vue'
import { RouterLink } from 'vue-router'
import type { RouteLocationRaw } from 'vue-router'
import type { IconName } from '../design/icons'
import Icon from './Icon.vue'

const props = defineProps<{
  to: RouteLocationRaw
  title: string
  /** A second line: where, when, what kind. */
  subtitle?: string
  /** The count line, already worded ("12 items"). */
  count?: string
  /** The cover image's address; the icon shows without one. */
  cover?: string
  icon: IconName
  /** tile: a square cover over the text. row: the cover beside it. */
  layout?: 'tile' | 'row'
  /** A round cover, for people. */
  round?: boolean
}>()

const shape = computed(() => props.layout ?? 'tile')
</script>

<template>
  <article class="collection-card lumen-surface" :class="[shape, { round }]">
    <RouterLink :to="to" class="cover" :aria-label="title">
      <slot name="cover">
        <img v-if="cover" :src="cover" alt="" loading="lazy" decoding="async" draggable="false" />
        <Icon v-else :name="icon" :size="shape === 'row' ? 32 : 40" />
      </slot>
    </RouterLink>
    <div v-if="$slots.badge" class="badge">
      <slot name="badge" />
    </div>
    <div class="meta">
      <slot name="title">
        <RouterLink :to="to" class="name" :title="title">{{ title }}</RouterLink>
      </slot>
      <span v-if="subtitle" class="line" :title="subtitle">{{ subtitle }}</span>
      <span v-if="count" class="line count">{{ count }}</span>
    </div>
    <div v-if="$slots.default" class="acts">
      <slot />
    </div>
  </article>
</template>

<style scoped>
.collection-card {
  position: relative;
  display: flex;
  flex-direction: column;
  gap: 0.625rem;
  min-width: 0;
}
.cover {
  position: relative;
  display: grid;
  place-items: center;
  overflow: hidden;
  border-radius: var(--lumen-r-lg);
  background: var(--lumen-placeholder);
  box-shadow: var(--lumen-shadow-1);
  color: var(--ui-text-dimmed);
  transition:
    transform var(--lumen-d-base) var(--lumen-ease-out),
    box-shadow var(--lumen-d-base) var(--lumen-ease-out);
}
.cover img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  transition: transform var(--lumen-d-slow) var(--lumen-ease-out);
}
/* The card lifts toward the pointer, and its photo leans in. */
.collection-card:hover .cover {
  transform: translateY(-2px);
  box-shadow: var(--lumen-shadow-2);
}
.collection-card:hover .cover img {
  transform: scale(1.04);
}
.meta {
  display: flex;
  flex-direction: column;
  gap: 0.125rem;
  min-width: 0;
}
.name {
  overflow: hidden;
  color: var(--ui-text-highlighted);
  font-weight: 600;
  text-decoration: none;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.name:hover {
  color: var(--lumen-primary-text);
}
.line {
  overflow: hidden;
  color: var(--ui-text-toned);
  font-size: 0.8125rem;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.count {
  color: var(--ui-text-dimmed);
  font-size: 0.75rem;
  font-variant-numeric: tabular-nums;
}

/* Tile: Things, Albums, Places, Events (a 4:3 cover); People (round). */
.tile .cover {
  aspect-ratio: 4 / 3;
}
.tile.round .cover {
  aspect-ratio: 1;
  border-radius: 50%;
}
.tile.round:hover .cover {
  transform: scale(1.03);
  box-shadow:
    var(--lumen-shadow-2),
    0 0 0 3px var(--lumen-primary-soft-2);
}
.tile .meta {
  padding: 0 0.125rem;
}
.tile.round .meta {
  align-items: center;
  text-align: center;
}
.tile .badge {
  position: absolute;
  top: 0.5rem;
  left: 0.5rem;
}
.tile .acts {
  position: absolute;
  top: 0.5rem;
  right: 0.5rem;
  display: flex;
  gap: 0.25rem;
  opacity: 0;
  transform: translateY(-4px);
  transition:
    opacity var(--lumen-d-fast) var(--lumen-ease-out),
    transform var(--lumen-d-base) var(--lumen-ease-out);
}
.tile:hover .acts,
.tile:focus-within .acts {
  opacity: 1;
  transform: none;
}
/* Touch has no hover: a card's actions are always there. */
@media (pointer: coarse) {
  .tile .acts {
    opacity: 1;
    transform: none;
  }
}

/* Row: a cover beside its text, for lists. */
.row {
  flex-direction: row;
  align-items: center;
  flex-wrap: wrap;
  gap: 0.875rem;
  padding: 0.625rem;
  border: 1px solid var(--ui-border);
  border-radius: var(--lumen-r-lg);
  background: var(--lumen-bg-raised);
  box-shadow: var(--lumen-shadow-1);
}
.row .cover {
  flex: 0 0 5.5rem;
  width: 5.5rem;
  height: 5.5rem;
  border-radius: var(--lumen-r-md);
  box-shadow: none;
}
.row:hover .cover {
  transform: none;
  box-shadow: none;
}
.row .meta {
  flex: 1;
}
.row .acts {
  display: flex;
  align-items: center;
  gap: 0.25rem;
}
</style>
