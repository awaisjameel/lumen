<script setup lang="ts">
import IconButton from '../shared/IconButton.vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UCheckbox from '@nuxt/ui/components/Checkbox.vue'
import UDropdownMenu from '@nuxt/ui/components/DropdownMenu.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import UModal from '@nuxt/ui/components/Modal.vue'
import USelect from '@nuxt/ui/components/Select.vue'
import UPopover from '@nuxt/ui/components/Popover.vue'
import USlider from '@nuxt/ui/components/Slider.vue'
import UContextMenu from '@nuxt/ui/components/ContextMenu.vue'
import { computed, defineAsyncComponent, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { useVirtualizer } from '@tanstack/vue-virtual'
import {
  AlbumKind,
  BatchAction,
  engine,
  ExportFormat,
  Filter,
  INSPECTOR_WIDTH_DEFAULT,
  INSPECTOR_WIDTH_MAX,
  INSPECTOR_WIDTH_MIN,
  SIDEBAR_WIDTH_DEFAULT,
  SIDEBAR_WIDTH_MAX,
  SIDEBAR_WIDTH_MIN,
  TILE_SIZE_MAX,
  TILE_SIZE_MIN,
} from '../bridge'
import type { Event, GridItem, Place } from '../bridge'
import { icons } from '../design/icons'
import type { IconName } from '../design/icons'
import { capsOf, dispatchKey, runCommand, useCommands } from '../shared/commands'
import type { Command } from '../shared/commands'
import { logError } from '../shared/errors'
import { coalescedLoad } from '../shared/listStore'
import { useLiveReload } from '../shared/liveReload'
import FeatureState from '../shared/FeatureState.vue'
import { dayLabel } from '../shared/format'
import { formatNumber, listFormat, t } from '../shared/i18n'
import type { MessageKey } from '../shared/i18n'
import { useLayoutMode } from '../shared/layout'
import Icon from '../shared/Icon.vue'
import MediaTile from '../shared/MediaTile.vue'
import Page from '../shared/Page.vue'
import PageHeader from '../shared/PageHeader.vue'
import { usePreferences } from '../shared/preferences'
import { shortcutsSuspended } from '../shared/overlay'
import { observeResize } from '../shared/observeResize'
import PersonHeader from '../people/PersonHeader.vue'
import { usePeople } from '../people/store'
import SearchBox from '../search/SearchBox.vue'
import SourcesNotice from './SourcesNotice.vue'
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
import LibraryThumb from './LibraryThumb.vue'
import SelectionDock from '../shared/SelectionDock.vue'
import Segmented from '../shared/Segmented.vue'
import ViewBar from '../shared/ViewBar.vue'
import CollectionHead from './CollectionHead.vue'
import { notify } from '../shared/notify'
import OutcomeList from '../media/OutcomeList.vue'
import { useTrashActions } from '../trash/actions'
import { useSelectionActions } from './selectionActions'
import { BatchTools, FileTools } from '../media/lazy'
import { compressionCommand } from '../shared/compression'

const PAD = 24 // horizontal padding of the grid

const lib = useLibrary()
// The viewer is as large as the grid itself, and a session may never open it.
const FirstScan = defineAsyncComponent(() => import('../onboarding/FirstScan.vue'))
const scanDismissed = ref(false)
const Viewer = defineAsyncComponent(() => import('./Viewer.vue'))
const filesOpen = ref(false)
const batchOpen = ref(false)
const fileOperation = ref<ExportFormat | 'move' | 'rename'>(ExportFormat.ExportOriginal)
const batchOperation = ref<BatchAction | 'tag' | 'block' | 'delete'>(BatchAction.BatchFavourite)
const albumOpen = ref(false)
const targetAlbum = ref(0)
const layoutOpen = ref(false)
const work = useSelectionActions(lib)
const trash = useTrashActions(lib)
const collectionHead = ref<{ addPhotos: () => void; editRule: () => void } | null>(null)
const fileIDs = computed(() => (lib.selectedIDs.size ? [...lib.selectedIDs] : lib.selected ? [lib.selected.id] : []))
const fileMenu = computed(() => [
  {
    label: t('compression.action'),
    icon: icons.files,
    disabled: !fileIDs.value.length || lib.inTrash,
    onSelect: () => {
      void runCommand('library.compress')
    },
  },
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
const router = useRouter()
const media = computed<Filter>(() =>
  route.query.media === 'photos'
    ? Filter.FilterPhotos
    : route.query.media === 'videos' || route.name === 'videos'
      ? Filter.FilterVideos
      : route.meta.filter === Filter.FilterPhotos
        ? Filter.FilterPhotos
        : Filter.FilterAll,
)
function showMedia(value: Filter) {
  void router.replace({
    name: route.name === 'videos' ? 'library' : (route.name ?? 'library'),
    params: route.params,
    query: { ...route.query, media: value === Filter.FilterAll ? undefined : value },
  })
}
const mediaItems = computed(() => [
  { value: Filter.FilterAll, label: t('library.all') },
  { value: Filter.FilterPhotos, label: t('library.photos') },
  ...(lib.settings.videos ? [{ value: Filter.FilterVideos, label: t('library.videos') }] : []),
])
const places = shallowRef<Place[]>([])
function refine(key: string, value: string | number | boolean | undefined) {
  void router.replace({ query: { ...route.query, [key]: value ? String(value) : undefined } })
}
async function loadPlaces() {
  try {
    places.value = await engine.places()
  } catch (e) {
    logError(e)
  }
}
const placeChoices = computed(() => [
  { value: 0, label: t('search.resultsAnyPlace') },
  ...places.value.map((p) => ({ value: p.id, label: p.name })),
])
const manualAlbums = computed(() =>
  albums.list.filter((a) => a.kind === AlbumKind.AlbumManual).map((a) => ({ value: a.id, label: a.name })),
)
function addToAlbum() {
  void work.apply(BatchAction.BatchAlbum, targetAlbum.value)
  albumOpen.value = false
}
function openFiles(operation: ExportFormat | 'move' | 'rename') {
  fileOperation.value = operation
  filesOpen.value = true
}
function openBatch(operation: BatchAction | 'tag' | 'block' | 'delete') {
  batchOperation.value = operation
  batchOpen.value = true
}

// The route decides what the grid shows: a filter (/, /favourites, /videos),
// one Thing (/things/:id), one person (/people/:id), one album (/albums/:id)
// or search results (/search?q=…).
const scope = computed<Scope>(() => {
  if (route.name === 'search')
    return {
      kind: 'search',
      query: String(route.query.q ?? '').trim(),
      media: media.value,
      favourites: route.query.favourites === 'true',
      from: String(route.query.from ?? ''),
      to: String(route.query.to ?? ''),
      place: Number(route.query.place) || 0,
    }
  const id = Number(route.params.id) || 0
  return {
    kind: 'timeline',
    media: media.value === Filter.FilterAll ? undefined : media.value,
    view: {
      filter:
        route.name === 'favourites'
          ? Filter.FilterFavourites
          : route.name === 'trash'
            ? Filter.FilterTrash
            : media.value,
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
  [Filter.FilterPhotos]: 'albums.photos',
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
const group = shallowRef<Event | Place | null>(null)
let active = true
const readGroupTitle = coalescedLoad(
  async () => {
    const place = placeId.value
    const event = eventId.value
    const result = !active ? null : place ? await engine.place(place) : event ? await engine.event(event) : null
    return { place, event, name: result?.name ?? '', group: result }
  },
  (result) => {
    if (active && result.place === placeId.value && result.event === eventId.value) {
      groupTitle.value = result.name
      group.value = result.group
    }
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
    group.value = null
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
  if (s.kind === 'search') return s.query ? t('search.title', { q: s.query }) : t('search.command')
  if (tagId.value) return things.byId.get(tagId.value)?.name ?? t('nav.things')
  if (personId.value) return people.byId.get(personId.value)?.name || t('person.unnamed')
  if (albumId.value) return albums.byId.get(albumId.value)?.name || t('nav.albums')
  if (placeId.value) return groupTitle.value || t('nav.places')
  if (eventId.value) return groupTitle.value || t('nav.events')
  return t(titles[s.view.filter])
})

const collectionKind = computed(() =>
  tagId.value
    ? 'thing'
    : placeId.value
      ? 'place'
      : eventId.value
        ? 'event'
        : albumId.value
          ? albums.byId.get(albumId.value)?.kind === AlbumKind.AlbumSmart
            ? 'smart'
            : 'album'
          : route.name === 'favourites'
            ? 'favourites'
            : null,
)
const collectionID = computed(() => tagId.value || placeId.value || eventId.value || albumId.value)
const back = computed(() => {
  const name = personId.value
    ? 'people'
    : tagId.value
      ? 'things'
      : placeId.value
        ? 'places'
        : eventId.value
          ? 'events'
          : albumId.value
            ? 'albums'
            : route.name === 'favourites'
              ? 'library'
              : null
  return name ? { to: { name }, label: t(`nav.${name}`) } : undefined
})
function reloadCollection() {
  void loadGroupTitle()
  void lib.refresh().catch(logError)
}
function clearFilters() {
  if (lib.isSearch) void router.replace({ name: 'search', query: { q: route.query.q } })
  else showMedia(Filter.FilterAll)
}

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
  lib.selectVisible(
    visible.value
      .filter(
        (v) =>
          v.top + rowHeight(v.row, layout.value.cell) > scrollTop.value && v.top < scrollTop.value + viewportH.value,
      )
      .flatMap((row) => row.items.flatMap((item) => (item ? [item.id] : []))),
  )
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
const gridCommands: Command[] = [
  compressionCommand(
    'library.compress',
    () => !!fileIDs.value.length && !lib.inTrash && !lib.viewer,
    () => fileIDs.value.map((assetId) => ({ assetId, path: '' })),
    'grid',
    () => lib.selectionSnapshot,
  ),
  {
    id: 'library.files',
    group: 'media',
    title: () => t('files.title'),
    icon: 'files',
    zone: 'grid',
    available: () => !!fileIDs.value.length && !lib.inTrash && !lib.viewer,
    run: () => {
      openFiles(ExportFormat.ExportOriginal)
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
      openBatch(BatchAction.BatchFavourite)
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
    reason: () => t('library.largest'),
    run: () => setTileSize(prefs.prefs.tileSize + ZOOM_STEP * 3),
  },
  {
    id: 'view.smallerTiles',
    group: 'view',
    title: () => t('library.zoomOut'),
    icon: 'zoom-out',
    enabled: () => prefs.prefs.tileSize > TILE_SIZE_MIN,
    reason: () => t('library.smallest'),
    run: () => setTileSize(prefs.prefs.tileSize - ZOOM_STEP * 3),
  },
]
useCommands(gridCommands)

const selectionDefinitions: Command[] = [
  {
    id: 'library.favouriteSelection',
    group: 'media',
    title: () => t('details.favourite'),
    icon: 'heart',
    run: () => work.apply(BatchAction.BatchFavourite),
  },
  {
    id: 'library.albumSelection',
    group: 'media',
    title: () => t('library.album'),
    icon: 'albums',
    run: () => {
      targetAlbum.value = 0
      albumOpen.value = true
      void albums.load().catch(logError)
    },
  },
  {
    id: 'library.exportSelection',
    group: 'media',
    title: () => t('library.export'),
    icon: 'download',
    run: () => openFiles(ExportFormat.ExportOriginal),
  },
  {
    id: 'library.trashSelection',
    group: 'media',
    title: () => t('trash.move'),
    icon: 'trash',
    danger: true,
    run: () => work.apply(BatchAction.BatchTrash),
  },
  {
    id: 'library.unfavouriteSelection',
    group: 'media',
    title: () => t('details.unfavourite'),
    icon: 'heart',
    run: () => work.apply(BatchAction.BatchUnfavourite),
  },
  {
    id: 'library.tagSelection',
    group: 'media',
    title: () => t('details.addTag'),
    icon: 'tag',
    run: () => openBatch('tag'),
  },
  {
    id: 'library.removeAlbumSelection',
    group: 'media',
    title: () => t('files.removeAlbum'),
    icon: 'albums',
    available: () => !!albumId.value && albums.byId.get(albumId.value)?.kind === AlbumKind.AlbumManual,
    run: () => work.apply(BatchAction.BatchRemoveAlbum, albumId.value),
  },
  {
    id: 'library.moveSelection',
    group: 'media',
    title: () => t('files.move'),
    icon: 'move',
    run: () => openFiles('move'),
  },
  {
    id: 'library.renameSelection',
    group: 'media',
    title: () => t('files.rename'),
    icon: 'pencil',
    enabled: () => fileIDs.value.length === 1,
    reason: () => t('library.singleFile'),
    run: () => openFiles('rename'),
  },
  {
    id: 'library.revealSelection',
    group: 'media',
    title: () => t('library.showExplorer'),
    icon: 'folder',
    enabled: () => fileIDs.value.length === 1,
    reason: () => t('library.singleFile'),
    run: () => {
      const id = fileIDs.value[0]
      if (id) return engine.showInFolder(id)
    },
  },
  {
    id: 'library.copyPath',
    group: 'media',
    title: () => t('files.copyPath'),
    icon: 'copy',
    enabled: () => fileIDs.value.length === 1,
    reason: () => t('library.singleFile'),
    run: async () => {
      const id = fileIDs.value[0]
      if (!id) return
      const detail = await engine.asset(id)
      const path = detail.paths[0]
      if (!path) return
      await navigator.clipboard.writeText(path)
      notify({ id: 'library.copied', kind: 'success', title: t('files.copied', { name: path }) })
    },
  },
  {
    id: 'library.rotateSelection',
    group: 'media',
    title: () => t('viewer.rotate'),
    icon: 'rotate',
    enabled: () => fileIDs.value.length === 1 && !lib.selectedItem?.v,
    reason: () => t('library.singleFile'),
    run: async () => {
      const id = fileIDs.value[0]
      if (!id) return
      const detail = await engine.asset(id)
      await engine.setAdjustment(id, (detail.adjustment?.rotation ?? 0) + 1, detail.adjustment?.flip ?? false)
      await lib.refresh()
    },
  },
  {
    id: 'library.blockSelection',
    group: 'media',
    title: () => t('blocked.block'),
    icon: 'blocked',
    run: () => openBatch('block'),
  },
]
const selectionCommands = selectionDefinitions.map((command) => ({
  ...command,
  enabled: () => !work.busy.value && (command.enabled?.() ?? true),
  available: () => !lib.inTrash && fileIDs.value.length > 0 && (command.available?.() ?? true),
}))
useCommands(selectionCommands)
useCommands(trash.commands)
const dockActions = computed(() =>
  lib.inTrash
    ? trash.commands
    : [
        ...selectionCommands.slice(0, 3),
        ...gridCommands.filter((c) => c.id === 'library.compress'),
        ...selectionCommands.slice(3, 4),
      ],
)
const dockMore = computed(() => [
  ...selectionCommands.slice(4),
  ...gridCommands.filter((c) => ['library.files', 'library.batch', 'library.open'].includes(c.id)),
])
const selectionOptions = computed(() => [
  { label: t('selection.visible'), onSelect: selectVisible },
  { label: t('selection.all'), onSelect: () => lib.selectAllResults() },
  {
    label: t('library.layout'),
    onSelect: () => {
      layoutOpen.value = true
    },
  },
  { label: t('library.sidebar'), kbds: capsOf('app.toggleSidebar'), onSelect: () => runCommand('app.toggleSidebar') },
  { label: t('selection.clear'), disabled: !lib.selectedIDs.size, onSelect: clearSelection },
])
function dayState(day: string): boolean | 'indeterminate' {
  const items = lib.days.get(day) ?? []
  const selected = items.filter((item) => lib.selectedIDs.has(item.id)).length
  return selected === 0 ? false : selected === (layout.value.countOfDay.get(day) ?? 0) ? true : 'indeterminate'
}
function check(day: string, item: GridItem | null, event: MouseEvent) {
  if (!item) return
  void lib.selectItems({ day, id: item.id }, true, event.shiftKey)
  scroller.value?.focus()
}

async function onKey(e: KeyboardEvent) {
  if (e.defaultPrevented || e.isComposing || shortcutsSuspended()) return
  if (dispatchKey(e, 'grid')) return
  if (e.ctrlKey || e.altKey || e.metaKey) return // leave shortcuts like Ctrl+F alone
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
      // A selection of several is the user's and stays; the one photo the
      // grid's keys act on moves with the viewer, and is shown as selected.
      if (lib.selectedIDs.size > 1) lib.selected = before
      else void lib.selectItems(before, false, false)
      scrollIntoView(before)
      scroller.value?.focus()
    }
  },
  { flush: 'post' },
)

const aiError = computed(() => lib.progress?.ai.error ?? '')
const empty = computed(() => lib.timelineLoaded && lib.buckets.length === 0)
// A search can take seconds (the first one loads the text model): say so.
const searching = computed(() => lib.isSearch && lib.searchLoading)
const emptyText = computed((): [string, string] => {
  const s = scope.value
  if (s.kind === 'search') {
    if (!s.query) return [t('search.resultsFirstUse'), t('search.resultsFirstUseHint')]
    return lib.searchInfo?.noAI
      ? [t('search.unavailable'), aiError.value || t('error.noAI')]
      : [t('search.none'), t('search.noneHint')]
  }
  if (media.value !== Filter.FilterAll && route.name !== 'videos')
    return [t('library.noResults'), t('library.noResultsHint')]
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

// How many files the Trash holds on disk (an item can have sidecars), as
// TrashBar reports it; unknown until it has asked.
const trashFiles = ref<number | null>(null)
const countText = computed(() => {
  if (lib.isSearch) return t('search.results', { n: lib.total })
  if (lib.inTrash && trashFiles.value !== null && lib.total)
    return t('trash.count', {
      items: t('status.items', { n: lib.total }),
      files: t('trash.files', { n: trashFiles.value }),
    })
  if (collectionKind.value || personId.value || lib.inTrash) return t('status.items', { n: lib.total })
  if (media.value === Filter.FilterPhotos) return t('library.photoCount', { n: formatNumber(lib.total) })
  if (media.value === Filter.FilterVideos) return t('library.videoCount', { n: formatNumber(lib.total) })
  return lib.countsLoaded
    ? t('library.caption', { photos: formatNumber(lib.counts.photos), videos: formatNumber(lib.counts.videos) })
    : t('status.items', { n: lib.total })
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
        <PageHeader :title="title" :subtitle="countText" :back="back">
          <template v-if="!lib.inTrash" #search><SearchBox /></template>
          <TrashBar v-if="lib.inTrash" @files="trashFiles = $event" />
          <IconButton
            :kbds="capsOf('view.details')"
            :icon="icons.inspector"
            :label="t('library.info')"
            :tip="t(lib.showDetails ? 'library.hideInfo' : 'library.showInfo')"
            :aria-pressed="lib.showDetails"
            color="neutral"
            :variant="lib.showDetails ? 'soft' : 'ghost'"
            @click="lib.showDetails = !lib.showDetails"
          />
        </PageHeader>
        <PersonHeader v-if="personId" :id="personId" />
        <CollectionHead
          v-else-if="collectionKind"
          :id="collectionID"
          ref="collectionHead"
          :kind="collectionKind"
          :name="title"
          :count="lib.total"
          :days="lib.buckets.map((b) => b.day)"
          :group="group"
          @changed="reloadCollection"
        />
      </template>

      <template #toolbar>
        <UAlert v-if="lib.searchLoading && lib.total" :title="t('search.searching')" color="neutral" variant="subtle"
          ><template #actions
            ><UButton color="neutral" variant="outline" @click="lib.cancelSearch()">{{
              t('common.cancel')
            }}</UButton></template
          ></UAlert
        >
        <UAlert
          v-if="lib.loadError && lib.total"
          :title="t('library.loadFailed')"
          :description="lib.loadError"
          color="error"
          ><template #actions
            ><UButton color="neutral" variant="outline" @click="lib.setScope(scope).catch(logError)">{{
              t('common.retry')
            }}</UButton></template
          ></UAlert
        >
        <p v-if="lib.isSearch && lib.timelineLoaded && lib.total" class="results-order">
          {{ t('search.resultsFirst') }}
        </p>
        <div v-if="lib.inTrash && lib.total" class="trash-intro" data-trash-notice>
          <p>{{ t('trash.summary', { n: lib.total }) }}</p>
          <p class="note"><Icon name="info" :size="14" />{{ t('trash.recoveryHint') }}</p>
        </div>
        <UAlert
          v-if="lib.daysError"
          :title="t('library.daysFailed')"
          :description="`${t('library.disconnected')} ${lib.daysError}`"
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
        <ViewBar :label="t('library.view')">
          <Segmented
            v-if="!lib.inTrash"
            :model-value="media"
            :items="mediaItems"
            :label="t('library.show')"
            @update:model-value="showMedia"
          />
          <template #end>
            <UPopover
              v-if="lib.isSearch"
              @update:open="
                (value) => {
                  if (value) void loadPlaces()
                }
              "
            >
              <IconButton :icon="icons.filter" :label="t('search.resultsRefine')" color="neutral" variant="ghost" />
              <template #content>
                <div class="refine">
                  <UCheckbox
                    :model-value="route.query.favourites === 'true'"
                    :label="t('search.resultsFavourites')"
                    @update:model-value="refine('favourites', $event === true || undefined)"
                  />
                  <label
                    >{{ t('search.resultsFrom')
                    }}<UInput
                      type="date"
                      :model-value="String(route.query.from ?? '')"
                      @update:model-value="refine('from', $event)"
                  /></label>
                  <label
                    >{{ t('search.resultsTo')
                    }}<UInput
                      type="date"
                      :model-value="String(route.query.to ?? '')"
                      @update:model-value="refine('to', $event)"
                  /></label>
                  <USelect
                    :model-value="Number(route.query.place) || 0"
                    :items="placeChoices"
                    :aria-label="t('search.resultsPlace')"
                    @update:model-value="refine('place', $event)"
                  />
                  <UButton color="neutral" variant="ghost" @click="clearFilters">{{
                    t('library.clearFilters')
                  }}</UButton>
                </div>
              </template>
            </UPopover>
            <div class="zoom" role="group" :aria-label="t('library.zoom')">
              <IconButton
                :icon="icons['zoom-out']"
                :label="t('library.zoomOut')"
                :tip="t(prefs.prefs.tileSize <= TILE_SIZE_MIN ? 'library.smallest' : 'library.zoomOut')"
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
                :tip="t(prefs.prefs.tileSize >= TILE_SIZE_MAX ? 'library.largest' : 'library.zoomIn')"
                color="neutral"
                variant="ghost"
                size="xs"
                :disabled="prefs.prefs.tileSize >= TILE_SIZE_MAX"
                @click="setTileSize(prefs.prefs.tileSize + ZOOM_STEP * 3)"
              />
            </div>
            <UDropdownMenu :items="selectionOptions" :content="{ align: 'end' }">
              <IconButton :icon="icons.select" :label="t('library.options')" color="neutral" variant="ghost" />
            </UDropdownMenu>
          </template>
        </ViewBar>
        <SourcesNotice />
        <UAlert
          v-if="work.total.value"
          class="batch-banner"
          :title="work.caption.value"
          :color="work.failed.value.length ? 'warning' : 'neutral'"
          variant="subtle"
          role="status"
        >
          <template #actions>
            <UButton v-if="work.busy.value" color="neutral" variant="outline" @click="work.cancel">{{
              t('library.cancelRemaining')
            }}</UButton>
            <UButton color="neutral" variant="ghost" @click="work.open.value = true">{{
              t('library.outcomes')
            }}</UButton>
          </template>
        </UAlert>
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
          <FeatureState
            v-if="lib.loadError"
            class="overlay"
            kind="error"
            :title="t('library.loadFailed')"
            :text="lib.loadError"
          >
            <UButton @click="lib.setScope(scope).catch(logError)">{{ t('common.retry') }}</UButton>
          </FeatureState>
          <div
            v-else-if="!lib.timelineLoaded"
            class="loading-grid"
            :aria-label="t(lib.isSearch ? 'search.searching' : 'library.loading')"
            role="status"
          >
            <p>{{ t(lib.isSearch ? 'search.searching' : 'library.loading') }}</p>
            <UButton v-if="lib.isSearch" color="neutral" variant="outline" @click="lib.cancelSearch()">{{
              t('common.cancel')
            }}</UButton>
            <p v-if="lib.isSearch">{{ t('library.modelLoading') }}</p>
            <div class="skeletons"><span v-for="i in 12" :key="i" class="lumen-skeleton" /></div>
          </div>
          <div v-else-if="empty && lib.progress?.scanning && lib.sources.length && !scanDismissed" class="scan-state">
            <FirstScan @complete="scanDismissed = true" />
          </div>
          <FeatureState
            v-else-if="empty"
            class="overlay"
            kind="empty"
            :icon="emptyIcon"
            :title="emptyText[0]"
            :text="emptyText[1]"
          >
            <UButton v-if="media !== Filter.FilterAll" @click="clearFilters">{{ t('library.clearFilters') }}</UButton>
            <UButton v-else-if="collectionKind === 'album'" @click="collectionHead?.addPhotos()">{{
              t('collection.addPhotos')
            }}</UButton>
            <UButton v-else-if="collectionKind === 'smart'" @click="collectionHead?.editRule()">{{
              t('collection.editRule')
            }}</UButton>
            <UButton v-else-if="route.name === 'favourites' || lib.isSearch" to="/">{{ t('nav.library') }}</UButton>
            <UButton v-else-if="!lib.inTrash && !collectionKind && !personId" to="/settings">{{
              t('settings.addFolder')
            }}</UButton>
          </FeatureState>

          <div class="rows" :style="{ height: virtualizer.getTotalSize() + 'px' }">
            <div
              v-for="v in visible"
              :key="v.key"
              class="row"
              :style="{ transform: `translateY(${v.top}px)`, height: v.height + 'px', padding: `0 ${PAD}px` }"
            >
              <!-- Each cell says its day, so the headings are for the eye only. -->
              <div v-if="v.row.kind === 'header'" class="day-header" :class="{ selecting: lib.selectedIDs.size > 0 }">
                <UCheckbox
                  class="day-check"
                  :ui="{ base: 'size-6' }"
                  :model-value="dayState(v.row.day)"
                  :aria-label="t('library.selectDay', { day: dayLabel(v.row.day) })"
                  @update:model-value="lib.selectDay(v.row.day)"
                />
                <h2>{{ dayLabel(v.row.day) }}</h2>
                <span v-if="group && 'country' in group" class="day-place">{{ group.name }}</span>
                <span class="day-count">{{ t('library.dayCount', { n: v.row.count }) }}</span>
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
                      :checkable="!!it"
                      :selecting="lib.selectedIDs.size > 0"
                      @check="check(v.row.day, it, $event)"
                    >
                      <template v-if="it && !lib.inTrash" #actions>
                        <button
                          type="button"
                          :aria-label="t(it.f ? 'details.unfavourite' : 'details.favourite')"
                          :aria-pressed="!!it.f"
                          :title="t(it.f ? 'details.unfavourite' : 'details.favourite')"
                          @click.stop="lib.toggleFavourite({ day: v.row.day, id: it.id })"
                          @dblclick.stop
                        >
                          <Icon name="heart" :size="16" :filled="it.f" />
                        </button>
                      </template>
                      <template v-if="it && it.t" #media>
                        <LibraryThumb v-if="!it.v" :id="it.id" :thumb="it.t" />
                        <VideoThumb v-else :key="it.id" :item="it" :cell="layout.cell" />
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
      <template #dock>
        <SelectionDock
          :count="lib.selectedIDs.size"
          :summary="
            t(
              lib.selectionSnapshot && lib.selectedIDs.size === lib.total ? 'selection.allResults' : 'selection.count',
              { n: lib.selectedIDs.size },
            )
          "
          :actions="dockActions"
          :more="lib.inTrash ? undefined : dockMore"
          @clear="clearSelection"
        />
      </template>
    </Page>

    <!-- The viewer has its own panel; this one would only load the same again behind it. -->
    <DetailsPanel v-if="lib.showDetails && !lib.viewer" :sel="lib.selected" />
    <Viewer v-if="lib.viewer" />
    <FileTools
      :key="fileOperation"
      v-model:open="filesOpen"
      :initial-operation="fileOperation"
      :ids="fileIDs"
      :selection="lib.selectionSnapshot"
      @changed="lib.refresh()"
    />
    <BatchTools
      :key="batchOperation"
      v-model:open="batchOpen"
      :initial-operation="batchOperation"
      :ids="fileIDs"
      :selection="lib.selectionSnapshot"
      :trash="lib.inTrash"
      @changed="lib.trashChanged()"
    />
    <UModal
      v-model:open="albumOpen"
      :title="t('library.album')"
      :description="t('selection.count', { n: lib.selectedIDs.size })"
    >
      <template #body>
        <USelect
          v-if="manualAlbums.length"
          v-model="targetAlbum"
          :items="manualAlbums"
          :aria-label="t('collection.chooseAlbum')"
        />
        <p v-else>{{ t('collection.noAlbums') }}</p>
        <UButton v-if="!manualAlbums.length" to="/albums">{{ t('nav.albums') }}</UButton>
      </template>
      <template #footer>
        <UButton color="neutral" variant="ghost" @click="albumOpen = false">{{ t('common.cancel') }}</UButton>
        <UButton :disabled="!targetAlbum || work.busy.value" @click="addToAlbum">{{ t('library.album') }}</UButton>
      </template>
    </UModal>
    <UModal v-model:open="work.open.value" :title="t('library.batchTitle')" :description="work.caption.value">
      <template #body
        ><OutcomeList v-slot="{ item }" :items="work.outcomes.value"
          ><p>{{ item.id }}: {{ work.text(item) }}</p></OutcomeList
        ></template
      >
      <template #footer
        ><UButton :disabled="work.busy.value || !work.failed.value.length" @click="work.retry">{{
          t('library.retryFailed')
        }}</UButton></template
      >
    </UModal>
    <UModal v-model:open="layoutOpen" :title="t('library.layout')">
      <template #body>
        <div class="refine">
          <label
            >{{ t('shell.navigation')
            }}<USlider
              :model-value="prefs.prefs.sidebarWidth"
              :min="SIDEBAR_WIDTH_MIN"
              :max="SIDEBAR_WIDTH_MAX"
              :disabled="mode !== 'wide'"
              :aria-label="t('shell.navigation')"
              @update:model-value="(v) => v !== undefined && prefs.set({ sidebarWidth: v })"
          /></label>
          <label
            >{{ t('library.info')
            }}<USlider
              :model-value="prefs.prefs.inspectorWidth"
              :min="INSPECTOR_WIDTH_MIN"
              :max="INSPECTOR_WIDTH_MAX"
              :aria-label="t('library.info')"
              @update:model-value="(v) => v !== undefined && prefs.set({ inspectorWidth: v })"
          /></label>
        </div>
      </template>
      <template #footer>
        <UButton
          color="neutral"
          variant="outline"
          @click="
            prefs.set({
              sidebarWidth: SIDEBAR_WIDTH_DEFAULT,
              inspectorWidth: INSPECTOR_WIDTH_DEFAULT,
              sidebarCollapsed: false,
            })
          "
          >{{ t('library.resetLayout') }}</UButton
        >
        <UButton @click="layoutOpen = false">{{ t('common.close') }}</UButton>
      </template>
    </UModal>
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

/* What the Trash keeps and what deleting does, said once above its items. */
.trash-intro {
  margin: 0.25rem var(--lumen-page-x) 0.5rem;
}
.trash-intro > p:first-child {
  max-width: 65ch;
  margin: 0 0 0.5rem;
  color: var(--ui-text-toned);
}
.trash-intro .note {
  display: flex;
  align-items: center;
  gap: 0.375rem;
  margin: 0;
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
  align-items: center;
  gap: 0.5rem;
  height: 100%;
  padding-top: 0.5rem;
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
.results-order {
  padding: 0 var(--lumen-page-x);
  margin: 0 0 0.5rem;
  color: var(--ui-text-dimmed);
  font-size: 0.75rem;
}
.scan-state {
  padding: 1rem var(--lumen-page-x);
}
.day-place {
  margin-left: auto;
  color: var(--ui-text-dimmed);
  font-size: 0.75rem;
}
.day-count {
  margin-left: auto;
  color: var(--ui-text-dimmed);
  font-size: 0.75rem;
  font-variant-numeric: tabular-nums;
}
.day-check {
  opacity: 0;
}
.day-header:hover .day-check,
.day-header:focus-within .day-check,
.day-header.selecting .day-check {
  opacity: 1;
}
.refine {
  display: grid;
  gap: 1rem;
  padding: 1rem;
  min-width: 15rem;
}
.refine label {
  display: grid;
  gap: 0.5rem;
  font-size: 0.8125rem;
}
.batch-banner {
  margin: 0 var(--lumen-page-x) 0.5rem;
}
.loading-grid {
  padding: 1rem var(--lumen-page-x);
  color: var(--ui-text-toned);
}
.skeletons {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(8rem, 1fr));
  gap: 0.25rem;
  margin-top: 1rem;
}
.skeletons span {
  aspect-ratio: 1;
}
/* The timeline scrubber replaces the scrollbar wherever it shows. */
.grid-wrap:has(.scrubber) .scroller {
  right: 3.5rem;
  scrollbar-width: none;
}
@container (max-width: 760px) {
  .grid-wrap:has(.scrubber) .scroller {
    right: 0;
    scrollbar-width: auto;
  }
  .scrubber {
    display: none;
  }
  .zoom-slider {
    width: 4rem;
  }
}
@container (max-width: 432px) {
  .zoom-slider {
    display: none;
  }
}
@media (pointer: coarse) {
  .day-check {
    opacity: 1;
  }
}
</style>
