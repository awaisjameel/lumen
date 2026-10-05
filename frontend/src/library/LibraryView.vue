<script setup lang="ts">
import IconButton from '../shared/IconButton.vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UPopover from '@nuxt/ui/components/Popover.vue'
import USlider from '@nuxt/ui/components/Slider.vue'
import UContextMenu from '@nuxt/ui/components/ContextMenu.vue'
import { computed, defineAsyncComponent, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import { useVirtualizer } from '@tanstack/vue-virtual'
import { AlbumKind, engine, Filter, TILE_SIZE_MAX, TILE_SIZE_MIN } from '../bridge'
import type { GridItem } from '../bridge'
import { icons } from '../design/icons'
import type { IconName } from '../design/icons'
import { dispatchKey, runCommand, useCommands } from '../shared/commands'
import { logError } from '../shared/errors'
import { coalescedLoad } from '../shared/listStore'
import { useLiveReload } from '../shared/liveReload'
import FeatureState from '../shared/FeatureState.vue'
import { dayLabel } from '../shared/format'
import { listFormat, t } from '../shared/i18n'
import type { MessageKey } from '../shared/i18n'
import { useLayoutMode } from '../shared/layout'
import MediaTile from '../shared/MediaTile.vue'
import Page from '../shared/Page.vue'
import PageHeader from '../shared/PageHeader.vue'
import { usePreferences } from '../shared/preferences'
import { shortcutsSuspended } from '../shared/overlay'
import { observeResize } from '../shared/observeResize'
import PersonHeader from '../people/PersonHeader.vue'
import { usePeople } from '../people/store'
import SearchBox from '../search/SearchBox.vue'
import { useThings } from '../things/store'
import { useAlbums } from '../albums/store'
import { buildLayout, GAP, move, rowHeight, rowOfItem } from './layout'
import type { Row } from './layout'
import TrashBar from '../trash/TrashBar.vue'
import { RESULTS, scopeKey, useLibrary } from './store'
import type { Scope, Sel } from './store'
import TimelineScrubber from './TimelineScrubber.vue'
import DetailsPanel from './DetailsPanel.vue'
import VideoThumb from './VideoThumb.vue'
import SelectionBar from '../shared/SelectionBar.vue'
import { BatchTools, FileTools } from '../media/lazy'

const PAD = 20 // horizontal padding of the grid

const lib = useLibrary()
// The viewer is as large as the grid itself, and a session may never open it.
const Viewer = defineAsyncComponent(() => import('./Viewer.vue'))
const filesOpen = ref(false)
const batchOpen = ref(false)
const fileIDs = computed(() => (lib.selectedIDs.size ? [...lib.selectedIDs] : lib.selected ? [lib.selected.id] : []))
const fileMenu = computed(() => [
  {
    label: t('files.title'),
    icon: icons.files,
    disabled: !fileIDs.value.length || lib.inTrash,
    onSelect: () => {
      void runCommand('library.files')
    },
  },
  {
    label: t('files.open'),
    icon: icons.external,
    disabled: fileIDs.value.length !== 1 || lib.inTrash,
    onSelect: () => {
      void runCommand('library.open')
    },
  },
])
const prefs = usePreferences()
const mode = useLayoutMode()
const things = useThings()
const people = usePeople()
const albums = useAlbums()
const route = useRoute()

// The route decides what the grid shows: a filter (/, /favourites, /videos),
// one Thing (/things/:id), one person (/people/:id), one album (/albums/:id)
// or search results (/search?q=…).
const scope = computed<Scope>(() => {
  if (route.name === 'search') return { kind: 'search', query: String(route.query.q ?? '').trim() }
  const id = Number(route.params.id) || 0
  return {
    kind: 'timeline',
    view: {
      filter: route.meta.filter ?? Filter.FilterAll,
      tag: route.name === 'thing' ? id : 0,
      person: route.name === 'person' ? id : 0,
      album: route.name === 'album' ? id : 0,
      place: route.name === 'place' ? id : 0,
      event: route.name === 'event' ? id : 0,
    },
  }
})
// The grid shows the library's scope while mounted (before the scope is set,
// so that loads).
lib.attach()
watch(
  () => scopeKey(scope.value),
  () => void lib.setScope(scope.value).catch(logError),
  { immediate: true },
)

const titles: Record<Filter, MessageKey> = {
  [Filter.FilterAll]: 'nav.library',
  [Filter.FilterFavourites]: 'nav.favourites',
  [Filter.FilterVideos]: 'nav.videos',
  [Filter.FilterTrash]: 'nav.trash',
}

const tagId = computed(() => (scope.value.kind === 'timeline' ? (scope.value.view.tag ?? 0) : 0))
const personId = computed(() => (scope.value.kind === 'timeline' ? (scope.value.view.person ?? 0) : 0))
const albumId = computed(() => (scope.value.kind === 'timeline' ? (scope.value.view.album ?? 0) : 0))
const placeId = computed(() => (scope.value.kind === 'timeline' ? (scope.value.view.place ?? 0) : 0))
const eventId = computed(() => (scope.value.kind === 'timeline' ? (scope.value.view.event ?? 0) : 0))
const groupTitle = ref('')
let active = true
const readGroupTitle = coalescedLoad(
  async () => {
    const place = placeId.value
    const event = eventId.value
    const result = !active ? null : place ? await engine.place(place) : event ? await engine.event(event) : null
    return { place, event, name: result?.name ?? '' }
  },
  (result) => {
    if (active && result.place === placeId.value && result.event === eventId.value) groupTitle.value = result.name
  },
)
async function loadGroupTitle() {
  try {
    await readGroupTitle()
  } catch (e) {
    if (active) {
      groupTitle.value = ''
      logError(e)
    }
  }
}
watch(
  [placeId, eventId],
  () => {
    groupTitle.value = ''
    void loadGroupTitle()
  },
  { immediate: true },
)
useLiveReload(loadGroupTitle, (change) => change.assets && !!(eventId.value || placeId.value))
watch(
  albumId,
  (id) => {
    if (id && !albums.byId.has(id)) void albums.load().catch(logError)
  },
  { immediate: true },
)
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
  if (s.kind === 'search') return t('search.title', { q: s.query })
  if (tagId.value) return things.byId.get(tagId.value)?.name ?? t('nav.things')
  if (personId.value) return people.byId.get(personId.value)?.name || t('person.unnamed')
  if (albumId.value) return albums.byId.get(albumId.value)?.name || t('nav.albums')
  if (placeId.value) return groupTitle.value || t('nav.places')
  if (eventId.value) return groupTitle.value || t('nav.events')
  return t(titles[s.view.filter])
})

// --- Layout and virtualization --------------------------------------------

const scroller = ref<HTMLElement | null>(null)
const width = ref(0)
const viewportH = ref(0)
const scrollTop = ref(0)
let stopResize: (() => void) | undefined

onMounted(() => {
  stopResize = observeResize(scroller.value ? [scroller.value] : [], () => {
    if (!scroller.value) return
    width.value = scroller.value.clientWidth - PAD * 2
    viewportH.value = scroller.value.clientHeight
  })
})
onBeforeUnmount(() => {
  active = false
  stopResize?.()
  clearTimeout(focusTimer)
  lib.detach()
})

const layout = computed(() => buildLayout(lib.buckets, Math.max(width.value, 200), prefs.prefs.tileSize, !lib.isSearch))

const virtualizer = useVirtualizer(
  computed(() => ({
    count: layout.value.rows.length,
    getScrollElement: () => scroller.value,
    estimateSize: (i: number) => {
      const row = layout.value.rows[i]
      return row ? rowHeight(row, layout.value.cell) : layout.value.cell
    },
    overscan: 4,
    useAnimationFrameWithResizeObserver: true,
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

/** The days of layout rows from to to (clamped). */
function daysOfRows(from: number, to: number): string[] {
  const rows = layout.value.rows
  const out = new Set<string>()
  for (let i = Math.max(0, from); i <= Math.min(rows.length - 1, to); i++) {
    const row = rows[i]
    if (row) out.add(row.day)
  }
  return [...out]
}

// Load the days on screen, and tell the engine to prioritise them. Days are
// fetched a few screens at a time: once the rows within a screen of the view
// need a day not loaded yet, everything within three screens comes in one
// request (fetching only what was on screen made a request every few rows).
let focusTimer = 0
watch(
  [visible, () => lib.days],
  () => {
    const days = [...new Set(visible.value.map((v) => v.row.day))]
    const [newest, oldest] = [days[0], days.at(-1)]
    if (newest === undefined || oldest === undefined || lib.isSearch) return
    const items = virtualizer.value.getVirtualItems()
    const first = items[0]?.index ?? 0
    const last = items.at(-1)?.index ?? first
    const screen = last - first + 1
    const near = daysOfRows(first - screen, last + screen)
    void lib.ensureDays(lib.needsDays(near) ? daysOfRows(first - 3 * screen, last + 3 * screen) : days)
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

function select(day: string, item: GridItem | null, event?: MouseEvent) {
  if (!item) return
  void lib.selectItems({ day, id: item.id }, event ? event.ctrlKey || event.metaKey : false, event?.shiftKey ?? false)
}

/** In the selection, or the one item focused while nothing is selected. */
function isSelected(item: GridItem | null): boolean {
  if (!item) return false
  return lib.selectedIDs.size ? lib.selectedIDs.has(item.id) : lib.selected?.id === item.id
}

function selectVisible() {
  lib.selectedIDs = new Set(
    visible.value
      .filter(
        (v) =>
          v.top + rowHeight(v.row, layout.value.cell) > scrollTop.value && v.top < scrollTop.value + viewportH.value,
      )
      .flatMap((row) => row.items.flatMap((item) => (item ? [item.id] : []))),
  )
  lib.selectionSnapshot = null
}
function contextSelect(day: string, item: GridItem | null) {
  if (item && !lib.selectedIDs.has(item.id)) select(day, item)
}
function clearSelection() {
  lib.clearSelection()
  lib.selected = null
}

/**
 * The grid is one listbox: keyboard focus stays on the scroller (cells are
 * not focusable, so none can take focus away and vanish with its row as the
 * grid scrolls), and aria-activedescendant names the selected cell.
 */
function cellId(id: number): string {
  return `cell-${id}`
}
const activeCell = computed(() => {
  const sel = lib.selected
  return sel && visible.value.some((v) => v.items.some((it) => it?.id === sel.id)) ? cellId(sel.id) : undefined
})

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

// What can be done with the grid and its selected photo: one definition for
// the toolbar, the command palette and the keys (the grid passes its own key
// events on, so F and I act only while it has the focus, as before).
const ZOOM_STEP = 8
function setTileSize(v: number) {
  void prefs.set({ tileSize: Math.min(TILE_SIZE_MAX, Math.max(TILE_SIZE_MIN, v)) })
}
useCommands([
  {
    id: 'library.files',
    group: 'media',
    title: () => t('files.title'),
    icon: 'files',
    zone: 'grid',
    available: () => !!fileIDs.value.length && !lib.inTrash && !lib.viewer,
    run: () => {
      filesOpen.value = true
    },
  },
  {
    id: 'library.batch',
    group: 'media',
    title: () => t('files.batch'),
    icon: 'check',
    zone: 'grid',
    available: () => !!fileIDs.value.length && !lib.viewer,
    run: () => {
      batchOpen.value = true
    },
  },
  {
    id: 'library.open',
    group: 'media',
    title: () => t('files.open'),
    icon: 'external',
    zone: 'grid',
    available: () => fileIDs.value.length === 1 && !lib.inTrash && !lib.viewer,
    run: async () => {
      const id = fileIDs.value[0]
      if (id) await engine.openMedia(id)
    },
  },
  {
    id: 'media.favourite',
    group: 'media',
    title: () => t(lib.selectedItem?.f ? 'details.unfavourite' : 'details.favourite'),
    icon: 'heart',
    keys: 'F',
    zone: 'grid',
    available: () => !!lib.selected && !lib.inTrash && !lib.viewer,
    run: () => lib.toggleFavourite(lib.selected),
  },
  {
    id: 'media.open',
    group: 'media',
    title: () => t('library.open'),
    icon: 'image',
    keys: 'Enter',
    zone: 'grid',
    available: () => !!lib.selected && !lib.viewer,
    run: () => {
      if (lib.selected) lib.viewer = { ...lib.selected }
    },
  },
  {
    id: 'view.details',
    group: 'view',
    title: () => t('library.info'),
    icon: 'inspector',
    keys: 'I',
    zone: 'grid',
    available: () => !lib.viewer,
    run: () => (lib.showDetails = !lib.showDetails),
  },
  {
    id: 'view.largerTiles',
    group: 'view',
    title: () => t('library.zoomIn'),
    icon: 'zoom-in',
    enabled: () => prefs.prefs.tileSize < TILE_SIZE_MAX,
    run: () => setTileSize(prefs.prefs.tileSize + ZOOM_STEP * 3),
  },
  {
    id: 'view.smallerTiles',
    group: 'view',
    title: () => t('library.zoomOut'),
    icon: 'zoom-out',
    enabled: () => prefs.prefs.tileSize > TILE_SIZE_MIN,
    run: () => setTileSize(prefs.prefs.tileSize - ZOOM_STEP * 3),
  },
])

async function onKey(e: KeyboardEvent) {
  if (e.defaultPrevented || e.isComposing || shortcutsSuspended()) return
  if (e.ctrlKey || e.altKey || e.metaKey) return // leave shortcuts like Ctrl+F alone
  if (dispatchKey(e, 'grid')) return
  const sel = lib.selected
  const dir = arrows[e.key]
  if (dir) {
    e.preventDefault()
    // Loading a day takes a moment: a click or Escape meanwhile wins.
    const select = async (day: string, i: number) => {
      const next = lib.selected === sel ? lib.selAt(day, i) : undefined
      if (next) {
        await lib.selectItems(next, false, e.shiftKey)
        scrollIntoView(next)
      }
    }
    if (!sel) {
      const first = lib.buckets[0]
      if (first) {
        await lib.ensureDays([first.day])
        await select(first.day, 0)
      }
      return
    }
    const pos = move(layout.value, { day: sel.day, i: lib.indexOf(sel) }, dir)
    if (!pos) return
    await lib.ensureDays([pos.day])
    await select(pos.day, pos.i)
  } else if (e.key === 'Escape') {
    clearSelection()
  }
}

// When the viewer closes, keep the grid on the photo that was last viewed.
// After the DOM update: the grid can only take focus once the viewer is gone.
watch(
  () => lib.viewer,
  (now, before) => {
    if (!now && before && lib.itemAt(before)) {
      lib.selected = before
      scrollIntoView(before)
      scroller.value?.focus()
    }
  },
  { flush: 'post' },
)

const aiError = computed(() => lib.progress?.ai.error ?? '')
const empty = computed(() => lib.timelineLoaded && lib.buckets.length === 0)
// A search can take seconds (the first one loads the text model): say so.
const searching = computed(() => lib.isSearch && !lib.timelineLoaded && !lib.loadError)
const emptyText = computed((): [string, string] => {
  const s = scope.value
  if (s.kind === 'search') {
    return lib.searchInfo?.noAI
      ? [t('search.unavailable'), aiError.value || t('error.noAI')]
      : [t('search.none'), t('search.noneHint')]
  }
  if (tagId.value) return [t('things.viewEmpty'), t('things.viewEmptyHint')]
  if (personId.value) return [t('person.empty'), t('person.emptyHint')]
  if (albumId.value)
    return [
      t('albums.viewEmpty'),
      t(
        albums.byId.get(albumId.value)?.kind === AlbumKind.AlbumSmart
          ? 'albums.smartEmptyHint'
          : 'albums.manualEmptyHint',
      ),
    ]
  if (lib.inTrash) return [t('trash.empty'), t('trash.emptyHint')]
  if (placeId.value) return [t('places.viewEmpty'), t('places.emptyHint')]
  if (eventId.value) return [t('events.viewEmpty'), t('events.emptyHint')]
  const keys: [MessageKey, MessageKey] =
    s.view.filter === Filter.FilterFavourites
      ? ['favourites.empty', 'favourites.emptyHint']
      : s.view.filter === Filter.FilterVideos
        ? ['videos.empty', 'videos.emptyHint']
        : lib.progress?.scanning
          ? ['library.emptyScanning', 'library.emptyScanningHint']
          : ['library.empty', 'library.emptyHint']
  return [t(keys[0]), t(keys[1])]
})

const countText = computed(() => {
  if (!lib.total) return ''
  return lib.isSearch ? t('search.results', { n: lib.total }) : t('status.items', { n: lib.total })
})

const emptyIcon = computed<IconName>(() => (lib.isSearch ? 'search' : lib.inTrash ? 'trash' : 'photos'))

// The slider reports each step of a drag: the preference store shows it at
// once and saves the newest.
function onZoom(v: number | undefined) {
  if (v !== undefined) setTileSize(v)
}

/** What a cell is, for screen readers: the kind, the day, a favourite. */
function cellLabel(day: string, it: GridItem): string {
  const what =
    day === RESULTS
      ? t(it.v ? 'library.video' : 'library.photo')
      : t(it.v ? 'library.videoOn' : 'library.photoOn', { day: dayLabel(day) })
  return it.f ? t('library.favouriteItem', { item: what }) : what
}

// Why a search may be missing photos: too many matches, or photos the AI
// hasn't seen yet.
const searchNotes = computed(() => {
  const info = lib.searchInfo
  if (!lib.isSearch || !info) return []
  const notes: string[] = []
  if (info.people.length) notes.push(t('search.withPeople', { names: listFormat.format(info.people) }))
  if (info.noAI && lib.total) notes.push(t('search.onlyPeople'))
  if (!info.complete) notes.push(t('search.more', { n: lib.total }))
  if (info.pending) notes.push(t('search.pending', { n: info.pending }))
  return notes
})
</script>

<template>
  <div class="library">
    <Page class="main" :scroll="false">
      <template #header>
        <PageHeader :title="title" :subtitle="personId ? undefined : countText">
          <template v-if="personId" #title>
            <PersonHeader :id="personId" />
          </template>
          <SearchBox />
          <!-- Wide windows keep the size slider in the toolbar; narrower ones
               fold it into a button so the search box keeps its room. -->
          <label v-if="mode === 'wide'" class="zoom" :title="t('library.zoom')">
            <IconButton
              :icon="icons['zoom-out']"
              :label="t('library.zoomOut')"
              color="neutral"
              variant="ghost"
              size="xs"
              :disabled="prefs.prefs.tileSize <= TILE_SIZE_MIN"
              @click="setTileSize(prefs.prefs.tileSize - ZOOM_STEP * 3)"
            />
            <USlider
              class="zoom-slider"
              :model-value="prefs.prefs.tileSize"
              :min="TILE_SIZE_MIN"
              :max="TILE_SIZE_MAX"
              :step="ZOOM_STEP"
              size="xs"
              :aria-label="t('library.zoom')"
              @update:model-value="onZoom"
            />
            <IconButton
              :icon="icons['zoom-in']"
              :label="t('library.zoomIn')"
              color="neutral"
              variant="ghost"
              size="xs"
              :disabled="prefs.prefs.tileSize >= TILE_SIZE_MAX"
              @click="setTileSize(prefs.prefs.tileSize + ZOOM_STEP * 3)"
            />
          </label>
          <UPopover v-else>
            <IconButton :icon="icons.grid" :label="t('library.zoom')" color="neutral" variant="ghost" />
            <template #content>
              <div class="zoom-pop">
                <span>{{ t('library.zoom') }}</span>
                <USlider
                  :model-value="prefs.prefs.tileSize"
                  :min="TILE_SIZE_MIN"
                  :max="TILE_SIZE_MAX"
                  :step="ZOOM_STEP"
                  :aria-label="t('library.zoom')"
                  @update:model-value="onZoom"
                />
              </div>
            </template>
          </UPopover>
          <IconButton
            :kbds="['I']"
            :icon="icons.inspector"
            :label="t('library.info')"
            :tip="t(lib.showDetails ? 'library.hideInfo' : 'library.showInfo')"
            :aria-pressed="lib.showDetails"
            color="neutral"
            :variant="lib.showDetails ? 'soft' : 'ghost'"
            @click="lib.showDetails = !lib.showDetails"
          />
        </PageHeader>
      </template>

      <template #toolbar>
        <TrashBar v-if="lib.inTrash" />
        <UAlert
          v-if="lib.daysError"
          :title="t('library.daysFailed')"
          :description="lib.daysError"
          color="error"
          variant="subtle"
          orientation="horizontal"
          role="alert"
        >
          <template #actions>
            <UButton color="neutral" variant="outline" @click="lib.retryDays()">{{ t('common.retry') }}</UButton>
          </template>
        </UAlert>
        <p v-for="n in searchNotes" :key="n" class="note">{{ n }}</p>
        <p v-if="lib.isSearch && lib.searchInfo?.blocked" class="note">
          {{ t('search.blocked') }}
          <RouterLink to="/settings/blocked" class="link">{{ t('search.openBlocked') }}</RouterLink>
        </p>
        <SelectionBar :summary="t('selection.count', { n: fileIDs.length })">
          <UButton variant="ghost" @click="selectVisible">{{ t('selection.visible') }}</UButton>
          <UButton variant="ghost" @click="lib.selectAllResults()">{{ t('selection.all') }}</UButton>
          <UButton :disabled="!fileIDs.length" variant="ghost" @click="clearSelection">{{
            t('selection.clear')
          }}</UButton>
          <UButton
            v-if="!lib.inTrash"
            :disabled="!fileIDs.length"
            :icon="icons.files"
            @click="runCommand('library.files')"
            >{{ t('files.title') }}</UButton
          >
          <UButton :disabled="!fileIDs.length" :icon="icons.check" @click="runCommand('library.batch')">{{
            t('files.batch')
          }}</UButton>
        </SelectionBar>
      </template>
      <div class="grid-wrap">
        <div
          ref="scroller"
          class="scroller"
          tabindex="0"
          role="listbox"
          aria-multiselectable="true"
          :aria-label="title"
          :aria-activedescendant="activeCell"
          :aria-busy="searching"
          @scroll.passive="onScroll"
          @keydown="onKey"
        >
          <FeatureState v-if="lib.loadError" class="overlay" kind="error" :title="lib.loadError" />
          <FeatureState v-else-if="searching" class="overlay" kind="loading" :title="t('search.searching')" />
          <FeatureState
            v-else-if="empty"
            class="overlay"
            kind="empty"
            :icon="emptyIcon"
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
              <!-- Each cell says its day, so the headings are for the eye only. -->
              <div v-if="v.row.kind === 'header'" class="day-header" aria-hidden="true">
                <h2>{{ dayLabel(v.row.day) }}</h2>
              </div>
              <div v-else class="cells" role="presentation" :style="{ gap: GAP + 'px' }">
                <div
                  v-for="(it, k) in v.items"
                  :id="it ? cellId(it.id) : undefined"
                  :key="it?.id ?? `p${k}`"
                  class="cell"
                  :class="{ selected: isSelected(it) }"
                  :style="{ width: layout.cell + 'px', height: layout.cell + 'px' }"
                  :role="it ? 'option' : undefined"
                  :aria-label="it ? cellLabel(v.row.day, it) : undefined"
                  :aria-selected="it ? isSelected(it) : undefined"
                  :aria-hidden="it ? undefined : 'true'"
                  @click="select(v.row.day, it, $event)"
                  @dblclick="open(v.row.day, it)"
                >
                  <UContextMenu :items="fileMenu" @contextmenu="contextSelect(v.row.day, it)">
                    <MediaTile
                      :id="it?.id ?? 0"
                      :thumb="it?.t ?? 0"
                      :video="it?.v"
                      :duration-ms="it?.d"
                      :favourite="it?.f"
                      :selected="isSelected(it)"
                    >
                      <template v-if="it?.v && it.t" #media>
                        <VideoThumb :key="it.id" :item="it" :cell="layout.cell" />
                      </template>
                    </MediaTile>
                  </UContextMenu>
                </div>
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
    </Page>

    <!-- The viewer has its own panel; this one would only load the same again behind it. -->
    <DetailsPanel v-if="lib.showDetails && !lib.viewer" :sel="lib.selected" />
    <Viewer v-if="lib.viewer" />
    <FileTools v-model:open="filesOpen" :ids="fileIDs" :selection="lib.selectionSnapshot" @changed="lib.refresh()" />
    <BatchTools
      v-model:open="batchOpen"
      :ids="fileIDs"
      :selection="lib.selectionSnapshot"
      :trash="lib.inTrash"
      @changed="lib.trashChanged()"
    />
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

.note {
  margin: 0 var(--lumen-page-x) 0.5rem;
  color: var(--ui-text-dimmed);
  font-size: 0.75rem;
}

.link {
  color: var(--ui-primary);
  text-decoration: underline;
}

.zoom {
  display: flex;
  align-items: center;
  gap: 0.25rem;
  flex-shrink: 0;
  color: var(--ui-text-dimmed);
}

.zoom-slider {
  width: 6.5rem;
}

.zoom-pop {
  display: grid;
  gap: 0.625rem;
  width: 14rem;
  padding: 0.75rem;
  color: var(--ui-text-muted);
  font-size: 0.8125rem;
}

.grid-wrap {
  position: absolute;
  inset: 0;
}

.scroller {
  position: absolute;
  inset: 0;
  overflow-y: auto;
  overflow-x: hidden;
  outline: none;
  scrollbar-width: none;
  /* the timeline scrubber replaces the scrollbar */
}

.scroller::-webkit-scrollbar {
  display: none;
}

/* Keyboard focus is on the grid itself: show it on the selected cell. */
.scroller:focus-visible .cell.selected {
  box-shadow: inset 0 0 0 5px var(--ui-bg);
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
  padding-bottom: 0.625rem;
}

.day-header h2 {
  margin: 0;
  color: var(--ui-text-highlighted);
  font-size: 0.875rem;
  font-weight: 600;
}

.cells {
  display: flex;
}

.cell {
  position: relative;
  flex-shrink: 0;
  cursor: pointer;
}

.overlay {
  position: absolute;
  inset: 0;
  z-index: 1;
}

@media (forced-colors: active) {
  .scroller:focus-visible .cell.selected {
    outline: 3px solid Highlight;
    outline-offset: -3px;
  }
}
</style>
