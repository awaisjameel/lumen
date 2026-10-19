<script setup lang="ts">
// A collection's cover drawn from its first photos: up to three of them in a
// mosaic, read only once the cover scrolls into view. With one photo, or
// before they arrive, it shows the collection's own cover, else its icon.
import { onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
import { engine, isCancelled, thumbUrl } from '../bridge'
import type { GridItem, View } from '../bridge'
import type { IconName } from '../design/icons'
import Icon from './Icon.vue'
import { onBlocksChanged } from './block'
import { logError } from './errors'

const props = defineProps<{
  /** Which photos the collection holds. */
  view: View
  count: number
  cover?: number
  coverThumb?: number
  icon: IconName
}>()
/** How many photos the mosaic shows, as the design draws it. */
const MOSAIC = 3

const root = ref<HTMLElement | null>(null)
const items = shallowRef<GridItem[]>([])
let observer: IntersectionObserver | undefined
let controller: AbortController | undefined
let visible = false
let revision = 0

async function load() {
  const mine = ++revision
  controller?.abort()
  items.value = []
  if (!visible || props.count < 2) return
  const request = new AbortController()
  controller = request
  const view = props.view
  try {
    const days = await engine.timeline(view, request.signal)
    if (mine !== revision) return
    const covers: GridItem[] = []
    for (const day of days) {
      const result = await engine.days(day.day, day.day, view, request.signal)
      if (mine !== revision) return
      covers.push(...result.flatMap((row) => row.items).slice(0, MOSAIC - covers.length))
      if (covers.length === MOSAIC) break
    }
    if (mine === revision) items.value = covers
  } catch (e) {
    if (mine === revision && !isCancelled(e)) logError(e)
  }
}

onMounted(() => {
  if (!root.value || typeof IntersectionObserver === 'undefined') return
  observer = new IntersectionObserver((entries) => {
    if (!entries.some((entry) => entry.isIntersecting)) return
    visible = true
    observer?.disconnect()
    void load()
  })
  observer.observe(root.value)
})
watch(
  () => [JSON.stringify(props.view), props.count, props.cover, props.coverThumb],
  () => void load(),
)
// A block can hide any photo shown here: they go at once, before the
// collection is read again and its new count or cover loads them afresh.
const stopBlocks = onBlocksChanged(() => {
  revision++
  controller?.abort()
  items.value = []
  return Promise.resolve()
})
onBeforeUnmount(() => {
  revision++
  controller?.abort()
  observer?.disconnect()
  stopBlocks()
})
</script>

<template>
  <div ref="root" class="cover-mosaic" :class="{ mosaic: items.length > 1 }" data-cover-mosaic>
    <template v-if="items.length > 1">
      <img
        v-for="item in items"
        :key="item.id"
        :src="thumbUrl(item.id, item.t)"
        alt=""
        loading="lazy"
        decoding="async"
        draggable="false"
      />
    </template>
    <img
      v-else-if="cover && coverThumb"
      :src="thumbUrl(cover, coverThumb)"
      alt=""
      loading="lazy"
      decoding="async"
      draggable="false"
    />
    <Icon v-else :name="icon" :size="40" />
  </div>
</template>

<style scoped>
.cover-mosaic {
  display: grid;
  place-items: center;
  width: 100%;
  height: 100%;
  overflow: hidden;
  color: var(--ui-text-dimmed);
}
.cover-mosaic img {
  width: 100%;
  height: 100%;
  min-height: 0;
  object-fit: cover;
}
/* The newest large, the next two stacked beside it; two share the height. */
.mosaic {
  grid-template-columns: minmax(0, 2fr) minmax(0, 1fr);
  grid-template-rows: repeat(2, minmax(0, 1fr));
  gap: 0.125rem;
}
.mosaic img:first-child,
.mosaic img:first-child:nth-last-child(2) ~ img {
  grid-row: span 2;
}
</style>
