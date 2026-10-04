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
  min-width: 0;
}
.cover {
  display: grid;
  place-items: center;
  overflow: hidden;
  border-radius: calc(var(--ui-radius) * 1.5);
  background: var(--lumen-placeholder);
  color: var(--ui-text-dimmed);
}
.cover img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  transition: transform var(--lumen-motion-panel) var(--lumen-ease);
}
.cover:hover img {
  transform: scale(1.04);
}
.meta {
  display: flex;
  flex-direction: column;
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
  text-decoration: underline;
}
.line {
  overflow: hidden;
  color: var(--ui-text-muted);
  font-size: 0.8125rem;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.count {
  color: var(--ui-text-dimmed);
  font-size: 0.75rem;
}

/* Tile: People, Things, Albums. */
.tile .cover {
  aspect-ratio: 1;
}
.tile.round .cover {
  border-radius: 50%;
}
.tile .meta {
  padding: 0.375rem 0.125rem 0;
}
.tile.round .meta {
  align-items: center;
  text-align: center;
}
.tile .badge {
  position: absolute;
  top: 0.375rem;
  left: 0.375rem;
}
.tile .acts {
  position: absolute;
  top: 0.375rem;
  right: 0.375rem;
  opacity: 0;
  transition: opacity var(--lumen-motion-control) var(--lumen-ease);
}
.tile:hover .acts,
.tile:focus-within .acts {
  opacity: 1;
}
/* Touch has no hover: a card's actions are always there. */
@media (pointer: coarse) {
  .tile .acts {
    opacity: 1;
  }
}

/* Row: Places, Events. */
.row {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 0.875rem;
  padding: 0.625rem;
  border: 1px solid var(--ui-border);
  border-radius: calc(var(--ui-radius) * 2);
  background: var(--ui-bg-muted);
}
.row .cover {
  flex: 0 0 5.5rem;
  width: 5.5rem;
  height: 5.5rem;
}
.row .meta {
  flex: 1;
  gap: 0.125rem;
}
.row .acts {
  display: flex;
  align-items: center;
  gap: 0.25rem;
}
</style>
