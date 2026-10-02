<script setup lang="ts">
// The Blocked screen's browser: the photos one block covers, in a virtualised
// grid. A photo opens in the Viewer, which can move it to the Trash. Photos
// that other blocks hide too say which, so unblocking this one is no surprise.
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useVirtualizer } from '@tanstack/vue-virtual'
import { BlockMode, thumbUrl } from '../bridge'
import type { CoveredItem } from '../bridge'
import { duration } from '../shared/format'
import { listFormat, t } from '../shared/i18n'
import Icon from '../shared/Icon.vue'
import { buildLayout, GAP, rowHeight } from '../library/layout'
import VideoThumb from '../library/VideoThumb.vue'
import Viewer from '../library/Viewer.vue'
import type { CoveredPhotos } from './covered'

const props = defineProps<{ covered: CoveredPhotos }>()

const TARGET_CELL = 160
const CAPTION_H = 36 // two lines of "Also hidden by", under every cell so rows keep one height

const scroller = ref<HTMLElement | null>(null)
const width = ref(0)
let observer: ResizeObserver | undefined
onMounted(() => {
  observer = new ResizeObserver(() => {
    if (scroller.value) width.value = scroller.value.clientWidth
  })
  if (scroller.value) observer.observe(scroller.value)
})
onBeforeUnmount(() => observer?.disconnect())

const labelOnly = computed(() => props.covered.block?.mode === BlockMode.BlockLabel)
const layout = computed(() =>
  buildLayout(
    props.covered.items.length ? [{ day: 'covered', count: props.covered.items.length }] : [],
    Math.max(width.value, 200),
    TARGET_CELL,
    false,
  ),
)
const virtualizer = useVirtualizer(
  computed(() => ({
    count: layout.value.rows.length,
    getScrollElement: () => scroller.value,
    estimateSize: (i: number) => {
      const row = layout.value.rows[i]
      return (row ? rowHeight(row, layout.value.cell) : layout.value.cell) + CAPTION_H
    },
    overscan: 4,
  })),
)
watch(layout, () => virtualizer.value.measure())

interface VisibleRow {
  key: number
  top: number
  height: number
  items: CoveredItem[]
}
const visible = computed<VisibleRow[]>(() =>
  virtualizer.value.getVirtualItems().flatMap((v) => {
    const row = layout.value.rows[v.index]
    if (row?.kind !== 'cells') return [] // the layout shrank; the virtualizer catches up next tick
    return [
      { key: row.start, top: v.start, height: v.size, items: props.covered.items.slice(row.start, row.start + row.n) },
    ]
  }),
)

const names = (item: CoveredItem) => listFormat.format(item.hiddenBy.map((b) => b.name))
</script>

<template>
  <section class="covered" :aria-label="covered.block ? t('covered.title', { name: covered.block.name }) : undefined">
    <header class="head">
      <button class="btn btn-subtle" @click="covered.close()">{{ t('covered.back') }}</button>
      <h2 v-if="covered.block">{{ t('covered.title', { name: covered.block.name }) }}</h2>
      <small v-if="!covered.loading" class="muted">{{ t('covered.count', { n: covered.items.length }) }}</small>
    </header>
    <p v-if="labelOnly" class="muted">{{ t('covered.stillShown') }}</p>
    <p v-if="covered.loading" class="muted" role="status">{{ t('covered.loading') }}</p>
    <p v-else-if="!covered.items.length" role="status">{{ t('covered.empty') }}</p>
    <div ref="scroller" class="scroller">
      <div class="rows" :style="{ height: virtualizer.getTotalSize() + 'px' }">
        <div
          v-for="row in visible"
          :key="row.key"
          class="row"
          :style="{ transform: `translateY(${row.top}px)`, height: row.height + 'px', gap: GAP + 'px' }"
        >
          <div v-for="item in row.items" :key="item.id" class="entry" :style="{ width: layout.cell + 'px' }">
            <button
              class="cell"
              :style="{ width: layout.cell + 'px', height: layout.cell + 'px' }"
              :aria-label="t(item.v ? 'covered.openVideo' : 'covered.openPhoto')"
              @click="covered.view(item.id)"
            >
              <VideoThumb v-if="item.v && item.t" :item="item" :cell="layout.cell" />
              <img
                v-else-if="item.t"
                :src="thumbUrl(item.id, item.t)"
                loading="lazy"
                decoding="async"
                draggable="false"
                alt=""
              />
              <span v-else class="ph" />
              <span v-if="item.v" class="badge video"><Icon name="video" :size="16" /></span>
              <span v-if="item.v && item.d" class="badge duration">{{ duration(item.d) }}</span>
            </button>
            <p v-if="item.hiddenBy.length" class="also" :title="names(item)">
              {{ t('covered.hiddenBy', { names: names(item) }) }}
            </p>
          </div>
        </div>
      </div>
    </div>
    <Viewer v-if="covered.viewing !== null" :source="covered.source" />
  </section>
</template>

<style scoped>
.head {
  display: flex;
  align-items: center;
  gap: 12px;
  margin: 16px 0 8px;
}

.head h2 {
  margin: 0;
  font-size: 18px;
  overflow-wrap: anywhere;
}

.scroller {
  height: calc(100vh - 280px);
  min-height: 320px;
  overflow-y: auto;
  margin-top: 12px;
}

.rows {
  position: relative;
  width: 100%;
}

.row {
  position: absolute;
  top: 0;
  left: 0;
  display: flex;
  width: 100%;
}

.entry {
  display: grid;
  align-content: start;
  gap: 4px;
}

.cell {
  position: relative;
  flex-shrink: 0;
  padding: 0;
  border: 0;
  border-radius: var(--radius-s);
  overflow: hidden;
  background: var(--placeholder);
  cursor: pointer;
}

.cell img,
.cell .ph {
  display: block;
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.also {
  margin: 0;
  max-height: calc(2 * 1.3em);
  overflow: hidden;
  color: var(--text-2);
  font-size: 12px;
  line-height: 1.3;
  overflow-wrap: anywhere;
}

.badge {
  position: absolute;
  display: grid;
  place-items: center;
  color: #fff;
  filter: drop-shadow(0 1px 2px rgb(0 0 0 / 0.6));
}

.badge.video {
  top: 6px;
  right: 6px;
}

.badge.duration {
  right: 5px;
  bottom: 5px;
  padding: 2px 4px;
  border-radius: var(--radius-s);
  background: rgb(0 0 0 / 0.68);
  font-size: 11px;
}
</style>
