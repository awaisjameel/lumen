<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useVirtualizer } from '@tanstack/vue-virtual'
import { engine, Filter, thumbUrl } from '../bridge'
import type { GridItem } from '../bridge'
import { logError } from '../shared/errors'
import { dayLabel, duration } from '../shared/format'
import { listFormat, t } from '../shared/i18n'
import type { MessageKey } from '../shared/i18n'
import EmptyState from '../shared/EmptyState.vue'
import Icon from '../shared/Icon.vue'
import PersonHeader from '../people/PersonHeader.vue'
import { usePeople } from '../people/store'
import SearchBox from '../search/SearchBox.vue'
import { useThings } from '../things/store'
import { buildLayout, GAP, move, rowHeight, rowOfItem } from './layout'
import type { Row } from './layout'
import { scopeKey, useLibrary, ZOOM_MAX, ZOOM_MIN } from './store'
import type { Scope, Sel } from './store'
import TimelineScrubber from './TimelineScrubber.vue'
import DetailsPanel from './DetailsPanel.vue'
import Viewer from './Viewer.vue'
import VideoThumb from './VideoThumb.vue'

const PAD = 20 // horizontal padding of the grid

const lib = useLibrary()
const things = useThings()
const people = usePeople()
const route = useRoute()

// The route decides what the grid shows: a filter (/, /favourites, /videos),
// one Thing (/things/:id), one person (/people/:id) or search results
// (/search?q=…).
const scope = computed<Scope>(() => {
  if (route.name === 'search') return { kind: 'search', query: String(route.query.q ?? '').trim() }
  const id = Number(route.params.id) || 0
  return {
    kind: 'timeline',
    view: {
      filter: route.meta.filter ?? Filter.FilterAll,
      tag: route.name === 'thing' ? id : 0,
      person: route.name === 'person' ? id : 0,
    },
  }
})
watch(
  () => scopeKey(scope.value),
  () => void lib.setScope(scope.value).catch(logError),
  { immediate: true },
)

const titles: Record<Filter, MessageKey> = {
  [Filter.FilterAll]: 'nav.library',
  [Filter.FilterFavourites]: 'nav.favourites',
  [Filter.FilterVideos]: 'nav.videos',
}

const tagId = computed(() => (scope.value.kind === 'timeline' ? (scope.value.view.tag ?? 0) : 0))
const personId = computed(() => (scope.value.kind === 'timeline' ? (scope.value.view.person ?? 0) : 0))
watch(
  personId,
  (id) => {
    if (id && !people.byId.has(id)) void people.load().catch(logError)
  },
  { immediate: true },
)
watch(
  tagId,
  (id) => {
    if (id && !things.byId.has(id)) void things.load().catch(logError)
  },
  { immediate: true },
)

const title = computed(() => {
  const s = scope.value
  if (s.kind === 'search') return `“${s.query}”`
  if (tagId.value) return things.byId.get(tagId.value)?.name ?? ''
  if (personId.value) return people.byId.get(personId.value)?.name || t('person.unnamed')
  return t(titles[s.view.filter])
})

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

const layout = computed(() => buildLayout(lib.buckets, Math.max(width.value, 200), lib.zoom, !lib.isSearch))

const virtualizer = useVirtualizer(
  computed(() => ({
    count: layout.value.rows.length,
    getScrollElement: () => scroller.value,
    estimateSize: (i: number) => {
      const row = layout.value.rows[i]
      return row ? rowHeight(row, layout.value.cell) : layout.value.cell
    },
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
  return virtualizer.value.getVirtualItems().flatMap((v) => {
    const row = L.rows[v.index]
    if (!row) return [] // the layout shrank; the virtualizer catches up next tick
    let items: (GridItem | null)[] = []
    if (row.kind === 'cells') {
      const loaded = days.get(row.day)
      items = Array.from({ length: row.n }, (_, k) => loaded?.[row.start + k] ?? null)
    }
    return [{ key: `${row.day}:${row.kind === 'cells' ? row.start : 'h'}`, top: v.start, height: v.size, row, items }]
  })
})

// Load the days on screen, and tell the engine to prioritise them.
let focusTimer = 0
watch(
  [visible, () => lib.days],
  () => {
    const days = [...new Set(visible.value.map((v) => v.row.day))]
    const [newest, oldest] = [days[0], days.at(-1)]
    if (newest === undefined || oldest === undefined || lib.isSearch) return
    void lib.ensureDays(days)
    clearTimeout(focusTimer)
    focusTimer = window.setTimeout(() => void engine.focus(oldest, newest).catch(logError), 300)
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

const aiError = computed(() => lib.progress?.ai.error ?? '')
const empty = computed(() => lib.timelineLoaded && lib.buckets.length === 0)
// A search can take seconds (the first one loads the text model): say so.
const searching = computed(() => lib.isSearch && !lib.timelineLoaded && !lib.loadError)
const emptyText = computed((): [string, string] => {
  const s = scope.value
  if (s.kind === 'search') {
    return aiError.value ? [t('search.unavailable'), aiError.value] : [t('search.none'), t('search.noneHint')]
  }
  if (tagId.value) return [t('things.viewEmpty'), t('things.viewEmptyHint')]
  if (personId.value) return [t('person.empty'), t('person.emptyHint')]
  const keys: [MessageKey, MessageKey] =
    s.view.filter === Filter.FilterFavourites
      ? ['favourites.empty', 'favourites.emptyHint']
      : s.view.filter === Filter.FilterVideos
        ? ['videos.empty', 'videos.emptyHint']
        : lib.progress?.scanning
          ? ['library.emptyScanning', 'library.emptyHint']
          : ['library.empty', 'library.emptyHint']
  return [t(keys[0]), t(keys[1])]
})

const countText = computed(() => {
  if (!lib.total) return ''
  return lib.isSearch ? t('search.results', { n: lib.total }) : t('status.items', { n: lib.total })
})

// Why a search may be missing photos: too many matches, or photos the AI
// hasn't seen yet.
const searchNotes = computed(() => {
  const info = lib.searchInfo
  if (!lib.isSearch || !info) return []
  const notes: string[] = []
  if (info.people.length) notes.push(t('search.withPeople', { names: listFormat.format(info.people) }))
  if (!info.complete) notes.push(t('search.more', { n: lib.total }))
  if (info.pending) notes.push(t('search.pending', { n: info.pending }))
  return notes
})
</script>

<template>
  <div class="library">
    <section class="page main">
      <header class="topbar">
        <PersonHeader v-if="personId" :id="personId" />
        <div v-else class="heading">
          <h1 :title="title">{{ title }}</h1>
          <span v-if="countText" class="count">{{ countText }}</span>
        </div>
        <SearchBox />
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

      <p v-for="n in searchNotes" :key="n" class="note">{{ n }}</p>

      <div class="grid-wrap">
        <div
          ref="scroller"
          class="scroller"
          tabindex="0"
          role="grid"
          :aria-label="title"
          @scroll.passive="onScroll"
          @keydown="onKey"
        >
          <EmptyState v-if="lib.loadError" class="overlay" role="alert" icon="image-off" :title="lib.loadError" />
          <EmptyState
            v-else-if="searching"
            class="overlay"
            role="status"
            icon="search"
            :title="t('search.searching')"
          />
          <EmptyState
            v-else-if="empty"
            class="overlay"
            :icon="lib.isSearch ? 'search' : 'photos'"
            :title="emptyText[0]"
            :text="emptyText[1]"
          />

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
                  <VideoThumb v-if="it?.v && it.t" :item="it" :cell="layout.cell" />
                  <img
                    v-else-if="it && it.t"
                    :src="thumbUrl(it.id, it.t)"
                    loading="lazy"
                    decoding="async"
                    draggable="false"
                    alt=""
                  />
                  <span v-else class="ph" />
                  <span v-if="it?.v" class="badge video"><Icon name="video" :size="16" /></span>
                  <span v-if="it?.v && it.d" class="badge duration">{{ duration(it.d) }}</span>
                  <span v-if="it?.f" class="badge fav"><Icon name="heart-filled" :size="16" /></span>
                </button>
              </div>
            </div>
          </div>
        </div>

        <TimelineScrubber
          v-if="!empty && !lib.isSearch && layout.total > viewportH"
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
}
.heading {
  display: flex;
  flex-direction: column;
  justify-content: center;
  gap: 4px;
  min-width: 0;
  max-width: 30%;
}
.count {
  color: var(--text-3);
  font-size: 12px;
  white-space: nowrap;
}
.note {
  margin: 0 20px 8px;
  color: var(--text-3);
  font-size: 12px;
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
.badge.duration {
  right: 5px;
  bottom: 5px;
  padding: 2px 4px;
  border-radius: 3px;
  background: rgb(0 0 0 / 0.68);
  font-size: 11px;
}
.overlay {
  position: absolute;
  inset: 0;
  place-content: center;
}
</style>
