<script setup lang="ts">
// The head of a collection's page, over its photos: a person, a Thing, a
// place, an event, an album or Favourites. It says what kind of collection
// this is, its name (which #name replaces with an inline editor while
// renaming), what it holds and when, and offers the kind's own actions
// (#actions: the primary one first). The cover is the collection's photo,
// a mosaic, or a person's portrait (round). On a short or narrow page the
// cover gives way so the photos keep their room.
defineProps<{
  /** What kind of collection it is ("Album", "Thing"). */
  kind: string
  name: string
  /** What it holds and when, already worded ("248 photos · 2019–2026"). */
  meta?: string
  /** A person's portrait: round and larger. */
  round?: boolean
}>()
</script>

<template>
  <section class="collection-hero" :class="{ round }" :aria-label="name">
    <div v-if="$slots.cover" class="cover">
      <slot name="cover" />
    </div>
    <div class="copy">
      <span class="kind">{{ kind }}</span>
      <slot name="name">
        <h2 class="name" :title="name">
          <bdi>{{ name }}</bdi>
        </h2>
      </slot>
      <slot name="meta">
        <span v-if="meta" class="meta">{{ meta }}</span>
      </slot>
    </div>
    <div v-if="$slots.actions" class="actions">
      <slot name="actions" />
    </div>
  </section>
</template>

<style scoped>
.collection-hero {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  flex-shrink: 0;
  gap: 1rem;
  padding: 0.75rem var(--lumen-page-x);
  animation: lumen-rise var(--lumen-d-slow) var(--lumen-ease-out) both;
}
.cover {
  flex: none;
  width: 6rem;
  height: 6rem;
  overflow: hidden;
  border-radius: var(--lumen-r-lg);
  background: var(--lumen-placeholder);
  box-shadow: var(--lumen-shadow-1);
}
.round .cover {
  width: 8rem;
  height: 8rem;
  border-radius: 50%;
}
.cover :deep(img) {
  display: block;
  width: 100%;
  height: 100%;
  object-fit: cover;
}
.copy {
  display: flex;
  flex-direction: column;
  flex: 1 1 16rem;
  gap: 0.25rem;
  min-width: 0;
}
.kind {
  color: var(--ui-text-dimmed);
  font-size: 0.6875rem;
  font-weight: 600;
  letter-spacing: 0.06em;
  text-transform: uppercase;
}
.name {
  margin: 0;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
  color: var(--ui-text-highlighted);
  font: 600 1.5rem/1.2 var(--font-display);
  letter-spacing: -0.01em;
}
.round .name {
  font-size: 2.25rem;
  letter-spacing: -0.02em;
}
.meta {
  color: var(--ui-text-toned);
  font-size: 0.8125rem;
  font-variant-numeric: tabular-nums;
  text-wrap: balance;
}
.actions {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 0.5rem;
}

@container (max-width: 760px) {
  .cover {
    display: none;
  }
  .name {
    font-size: 1.25rem;
  }
  .round .name {
    font-size: 1.875rem;
  }
}
@media (max-height: 600px) {
  .collection-hero {
    padding-block: 0.5rem;
  }
  .cover {
    display: none;
  }
}
</style>
