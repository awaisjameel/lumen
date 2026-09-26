<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useVirtualizer } from '@tanstack/vue-virtual'
import { engine, Filter, thumbUrl } from '../bridge'
import type { GridItem } from '../bridge'
import { logError } from '../shared/errors'
import { dayLabel } from '../shared/format'
import { t } from '../shared/i18n'
import type { MessageKey } from '../shared/i18n'
import Icon from '../shared/Icon.vue'
import { buildLayout, GAP, move, rowHeight, rowOfItem } from './layout'
import type { Row } from './layout'
import { useLibrary, ZOOM_MAX, ZOOM_MIN } from './store'
import type { Sel } from './store'
import TimelineScrubber from './TimelineScrubber.vue'
import DetailsPanel from './DetailsPanel.vue'
import Viewer from './Viewer.vue'

const PAD = 20 // horizontal padding of the grid

const lib = useLibrary()
const route = useRoute()

// The route decides the filter: /, /favourites, /videos.
const filter = computed(() => route.meta.filter ?? Filter.FilterAll)
watch(filter, (f) => void lib.setFilter(f).catch(logError), { immediate: true })

const titles: Record<Filter, MessageKey> = {
  [Filter.FilterAll]: 'nav.library',
  [Filter.FilterFavourites]: 'nav.favourites',
  [Filter.FilterVideos]: 'nav.videos',
}

// --- Layout and virtualization --------------------------------------------

const scroller = ref<HTMLElement | null>(null)
const width = ref(0)
const viewportH = ref(0)
const scrollTop = ref(0)
let ro: ResizeObserver | null = null

onMounted(() => {
  ro = new ResizeObserver(() => {
    if (!scroller.value) return
    width.value = scroller.value.clientWidth - PAD * 2
    viewportH.value = scroller.value.clientHeight
  })
  if (scroller.value) ro.observe(scroller.value)
})
onBeforeUnmount(() => ro?.disconnect())

const layout = computed(() => buildLayout(lib.buckets, Math.max(width.value, 200), lib.zoom))

const virtualizer = useVirtualizer(
  computed(() => ({
    count: layout.value.rows.length,
    getScrollElement: () => scroller.value,
    estimateSize: (i: number) => rowHeight(layout.value.rows[i], layout.value.cell),
    overscan: 4,
  })),
)
watch(layout, () => virtualizer.value.measure())

interface VisibleRow {
  key: string
  top: number
  height: number
  row: Row
  items: (GridItem | null)[] // null = not loaded yet
}

const visible = computed<VisibleRow[]>(() => {
  const L = layout.value
  const days = lib.days
  return virtualizer.value.getVirtualItems().map((v) => {
    const row = L.rows[v.index]
    let items: (GridItem | null)[] = []
    if (row.kind === 'cells') {
      const loaded = days.get(row.day)
      items = Array.from({ length: row.n }, (_, k) => loaded?.[row.start + k] ?? null)
    }
    return { key: `${row.day}:${row.kind === 'cells' ? row.start : 'h'}`, top: v.start, height: v.size, row, items }
  })
})

// Load the days on screen, and tell the engine to prioritise them.
let focusTimer = 0
watch(
  [visible, () => lib.days],
  () => {
    const days = [...new Set(visible.value.map((v) => v.row.day))]
    if (!days.length) return
    void lib.ensureDays(days)
    clearTimeout(focusTimer)
    focusTimer = window.setTimeout(() => void engine.focus(days[days.length - 1], days[0]).catch(logError), 300)
  },
  { flush: 'post' },
)

function onScroll() {
  scrollTop.value = scroller.value?.scrollTop ?? 0
}

function seek(y: number) {
  scroller.value?.scrollTo({ top: y })
}

// --- Selection, keyboard, viewer ------------------------------------------

function select(day: string, item: GridItem | null) {
  if (!item) return
  lib.selected = { day, id: item.id }
}

function open(day: string, item: GridItem | null) {
  if (!item) return
  lib.selected = { day, id: item.id }
  lib.viewer = { day, id: item.id }
}

function scrollIntoView(sel: Sel) {
  const r = rowOfItem(layout.value, sel.day, lib.indexOf(sel))
  if (r >= 0) virtualizer.value.scrollToIndex(r, { align: 'auto' })
}

const arrows: Partial<Record<string, 'left' | 'right' | 'up' | 'down'>> = {
  ArrowLeft: 'left',
  ArrowRight: 'right',
  ArrowUp: 'up',
  ArrowDown: 'down',
}

async function onKey(e: KeyboardEvent) {
  if (e.ctrlKey || e.altKey || e.metaKey) return // leave shortcuts like Ctrl+F alone
  const sel = lib.selected
  const dir = arrows[e.key]
  if (dir) {
    e.preventDefault()
    if (!sel) {
      const first = lib.buckets[0]
      if (first) {
        await lib.ensureDays([first.day])
        const s = lib.selAt(first.day, 0)
        if (s) lib.selected = s
      }
      return
    }
    const pos = move(layout.value, { day: sel.day, i: lib.indexOf(sel) }, dir)
    if (!pos) return
    await lib.ensureDays([pos.day])
    const next = lib.selAt(pos.day, pos.i)
    if (next) {
      lib.selected = next
      scrollIntoView(next)
    }
  } else if (e.key === 'Enter' && sel) {
    lib.viewer = { ...sel }
  } else if (e.key === 'Escape') {
    lib.selected = null
  } else if (e.key.toLowerCase() === 'f' && sel) {
    void lib.toggleFavourite(sel)
  } else if (e.key.toLowerCase() === 'i') {
    lib.showDetails = !lib.showDetails
  }
}

// When the viewer closes, keep the grid on the photo that was last viewed.
watch(
  () => lib.viewer,
  (now, before) => {
    if (!now && before) {
      lib.selected = before
      scrollIntoView(before)
      scroller.value?.focus()
    }
  },
)

const empty = computed(() => lib.timelineLoaded && lib.buckets.length === 0)
const emptyText = computed((): [MessageKey, MessageKey] => {
  if (filter.value === Filter.FilterFavourites) return ['favourites.empty', 'favourites.emptyHint']
  if (filter.value === Filter.FilterVideos) return ['videos.empty', 'videos.emptyHint']
  if (lib.progress?.scanning) return ['library.emptyScanning', 'library.emptyHint']
  return ['library.empty', 'library.emptyHint']
})
</script>

<template>
  <div class="library">
    <section class="main">
      <header class="topbar">
        <h1>{{ t(titles[filter]) }}</h1>
        <span v-if="lib.total" class="count">{{ t('status.items', { n: lib.total }) }}</span>
        <div class="spacer" />
        <label class="zoom" :title="t('library.zoom')">
          <Icon name="photos" :size="16" />
          <input
            type="range"
            :min="ZOOM_MIN"
            :max="ZOOM_MAX"
            step="8"
            :value="lib.zoom"
            :aria-label="t('library.zoom')"
            @input="lib.setZoom(Number(($event.target as HTMLInputElement).value))"
          />
        </label>
        <button
          class="icon-btn"
          :aria-pressed="lib.showDetails"
          :title="t('library.info') + ' (I)'"
          @click="lib.showDetails = !lib.showDetails"
        >
          <Icon name="info" />
        </button>
      </header>

      <div class="grid-wrap">
        <div
          ref="scroller"
          class="scroller"
          tabindex="0"
          role="grid"
          :aria-label="t(titles[filter])"
          @scroll.passive="onScroll"
          @keydown="onKey"
        >
          <div v-if="empty" class="empty">
            <Icon name="photos" :size="40" />
            <h2>{{ t(emptyText[0]) }}</h2>
            <p class="muted">{{ t(emptyText[1]) }}</p>
          </div>

          <div class="rows" :style="{ height: virtualizer.getTotalSize() + 'px' }">
            <div
              v-for="v in visible"
              :key="v.key"
              class="row"
              :style="{ transform: `translateY(${v.top}px)`, height: v.height + 'px', padding: `0 ${PAD}px` }"
            >
              <div v-if="v.row.kind === 'header'" class="day-header">
                <h2>{{ dayLabel(v.row.day) }}</h2>
              </div>
              <div v-else class="cells" :style="{ gap: GAP + 'px' }">
                <button
                  v-for="(it, k) in v.items"
                  :key="it?.id ?? `p${k}`"
                  class="cell"
                  :class="{ selected: it && lib.selected?.id === it.id }"
                  :style="{ width: layout.cell + 'px', height: layout.cell + 'px' }"
                  tabindex="-1"
                  @click="select(v.row.day, it)"
                  @dblclick="open(v.row.day, it)"
                >
                  <img
                    v-if="it && it.t"
                    :src="thumbUrl(it.id, it.t)"
                    loading="lazy"
                    decoding="async"
                    draggable="false"
                    alt=""
                  />
                  <span v-else class="ph" />
                  <span v-if="it?.v" class="badge video"><Icon name="video" :size="16" /></span>
                  <span v-if="it?.f" class="badge fav"><Icon name="heart-filled" :size="16" /></span>
                </button>
              </div>
            </div>
          </div>
        </div>

        <TimelineScrubber
          v-if="!empty && layout.total > viewportH"
          :buckets="lib.buckets"
          :layout="layout"
          :scroll-top="scrollTop"
          :viewport-h="viewportH"
          @seek="seek"
        />
      </div>
    </section>

    <DetailsPanel v-if="lib.showDetails" :sel="lib.selected" />
    <Viewer v-if="lib.viewer" />
  </div>
</template>

<style scoped>
.library {
  display: flex;
  height: 100%;
  min-width: 0;
}
.main {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-width: 0;
}
.topbar {
  display: flex;
  align-items: center;
  gap: 12px;
  height: var(--topbar-h);
  padding: 0 12px 0 20px;
  flex-shrink: 0;
}
.topbar h1 {
  margin: 0;
  font: 600 20px/1 var(--font-display);
}
.count {
  color: var(--text-3);
}
.spacer {
  flex: 1;
}
.zoom {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--text-3);
}
.zoom input {
  width: 110px;
  accent-color: var(--accent);
}
.grid-wrap {
  position: relative;
  flex: 1;
  min-height: 0;
}
.scroller {
  position: absolute;
  inset: 0;
  overflow-y: auto;
  overflow-x: hidden;
  outline: none;
  scrollbar-width: none; /* the timeline scrubber replaces the scrollbar */
}
.scroller::-webkit-scrollbar {
  display: none;
}
.rows {
  position: relative;
  width: 100%;
}
.row {
  position: absolute;
  top: 0;
  left: 0;
  width: 100%;
}
.day-header {
  display: flex;
  align-items: flex-end;
  height: 100%;
  padding-bottom: 10px;
}
.day-header h2 {
  margin: 0;
  font-size: 14px;
  font-weight: 600;
}
.cells {
  display: flex;
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
.cell .ph {
  background: linear-gradient(100deg, var(--placeholder) 40%, var(--surface-2) 50%, var(--placeholder) 60%);
  background-size: 300% 100%;
  animation: shimmer 1.6s ease-in-out infinite;
}
@keyframes shimmer {
  from {
    background-position: 100% 0;
  }
  to {
    background-position: 0 0;
  }
}
.cell.selected {
  outline: 3px solid var(--accent);
  outline-offset: -3px;
}
.cell.selected img {
  transform: scale(0.94);
  border-radius: var(--radius-s);
}
.cell img {
  transition: transform 0.12s ease;
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
.badge.fav {
  bottom: 6px;
  left: 6px;
}
.empty {
  position: absolute;
  inset: 0;
  display: grid;
  place-content: center;
  justify-items: center;
  gap: 4px;
  text-align: center;
  color: var(--text-3);
  padding: 24px;
}
.empty h2 {
  margin: 8px 0 0;
  color: var(--text);
  font: 600 18px var(--font-display);
}
.empty p {
  margin: 0;
  max-width: 360px;
}
</style>
