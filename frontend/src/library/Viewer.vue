<script setup lang="ts">
// Full-window viewer. Shows the 1024px preview instantly, then swaps in the
// original when the WebView can display it (JPEG, PNG, WebP, AVIF). Its
// photos, the one beside each and what may be done with it come from a
// source: the library grid unless one is given (the Blocked screen's browser).
import IconButton from '../shared/IconButton.vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UDropdownMenu from '@nuxt/ui/components/DropdownMenu.vue'
import { computed, nextTick, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
import { engine, isCancelled, videosAvailable, ViewerWheel } from '../bridge'
import type { AssetDetail, GridItem } from '../bridge'
import { icons } from '../design/icons'
import { capsOf, commandById, dispatchKey, menuItems, runCommand, useCommands } from '../shared/commands'
import { compressionCommand } from '../shared/compression'
import { errorText, orFallback, reportError } from '../shared/errors'
import FeatureState from '../shared/FeatureState.vue'
import { fileName, takenDay } from '../shared/format'
import { formatNumber, t } from '../shared/i18n'
import Icon from '../shared/Icon.vue'
import { isTyping, ownsArrows } from '../shared/keys'
import { useLayer } from '../shared/overlay'
import { libraryViewerSource, useLibrary } from './store'
import type { ViewerSource } from './store'
import DetailsPanel from './DetailsPanel.vue'
import VideoPlayer from './VideoPlayer.vue'
import ImageSurface from '../media/ImageSurface.vue'
import type { ImageControls, ViewGeometry } from '../media/viewport'
import Filmstrip from '../media/Filmstrip.vue'
import { usePreferences } from '../shared/preferences'
import { useLiveReload } from '../shared/liveReload'
import { useLayoutMode } from '../shared/layout'
import { notify } from '../shared/notify'
import { FileTools } from '../media/lazy'

const props = defineProps<{ source?: ViewerSource }>()
const lib = useLibrary()
const prefs = usePreferences()
const viewing = computed(() => prefs.viewer)
const neighbours = shallowRef<GridItem[]>([])
const comparing = ref(false)
const comparison = ref<AssetDetail | null>(null)
const comparisonItem = ref<GridItem | null>(null)
const linked = ref(true)
const geometry = ref<ViewGeometry>()
const sliding = ref(false)
const slideProgress = ref(0)
const slideCycle = ref(0)
const hidden = ref(false)
const chromeHovered = ref(false)
let hideTimer: ReturnType<typeof setTimeout> | undefined
let slideTimer: ReturnType<typeof setTimeout> | undefined
let slideTick: ReturnType<typeof setInterval> | undefined
let neighbourhoodRequest = 0
let comparisonRequest = 0
const src = computed(() => props.source ?? libraryViewerSource(lib))
const item = computed(() => src.value.item)
const actions = computed(() => src.value.actions)
// Only an ordinary library photo can be a favourite; the same ones keep
// display adjustments and playback history. The Trash and Blocked do not.
const ordinary = computed(() => !!actions.value.favourite)
const detail = ref<AssetDetail | null>(null)
const image = ref<ImageControls | null>(null)
const player = ref<{ handleKey: (event: KeyboardEvent) => boolean } | null>(null)
const root = ref<HTMLElement | null>(null)
const loading = ref(false)
const loadError = ref('')
const adjusting = ref(false)
const layer = useLayer()
const mode = useLayoutMode()
const detailsButton = ref<{ focus: () => void } | null>(null)
let previousFocus: Element | null = null
let detailRequest = 0
const retry = ref(0)
const filesOpen = ref(false)
const menuOpen = ref(false)
const zoomOpen = ref(false)
const isFullscreen = ref(false)
const title = computed(() => fileName(detail.value?.paths[0] ?? '') || t('viewer.title'))
// The heading says when and where the photo was taken; a place whose name is
// blocked is not found, so it names none.
const placeName = ref('')
let placeAsked = 0
watch(
  () => detail.value?.placeId ?? 0,
  async (id) => {
    const asked = ++placeAsked
    placeName.value = ''
    const place = id ? await orFallback(engine.place(id), null) : null
    if (asked === placeAsked) placeName.value = place?.name ?? ''
  },
)
const heading = computed(() =>
  detail.value ? [takenDay(detail.value.takenAt), placeName.value].filter(Boolean).join(' · ') : t('viewer.title'),
)
const neighbourIndex = computed(() => neighbours.value.findIndex((entry) => entry.id === src.value.id))
const canPrevious = computed(() => !src.value.window || neighbourIndex.value !== 0)
const canNext = computed(
  () => !src.value.window || neighbourIndex.value < 0 || neighbourIndex.value < neighbours.value.length - 1,
)
const canCompare = computed(() => neighbours.value.some((entry) => entry.id !== src.value.id && !entry.v))
const adjusted = computed(() => !!detail.value?.adjustment?.rotation || !!detail.value?.adjustment?.flip)
const positionText = computed(() => {
  const selection = src.value.sel
  if (!props.source && selection && lib.total) {
    const bucket = lib.buckets.findIndex((entry) => entry.day === selection.day)
    const index = lib.days.get(selection.day)?.findIndex((entry) => entry.id === selection.id) ?? -1
    if (bucket >= 0 && index >= 0)
      return t('viewer.position', {
        n: lib.buckets.slice(0, bucket).reduce((sum, entry) => sum + entry.count, 0) + index + 1,
        total: lib.total,
      })
  }
  return neighbourIndex.value < 0
    ? ''
    : t('viewer.nearbyPosition', { n: neighbourIndex.value + 1, total: neighbours.value.length })
})
watch(
  () => lib.showDetails,
  async (open) => {
    await nextTick()
    if (open && mode.value !== 'wide') root.value?.querySelector<HTMLElement>('.inspector button')?.focus()
    else if (!open) detailsButton.value?.focus()
  },
)
const moreItems = computed(() =>
  menuItems(
    [
      'viewer.openFile',
      'viewer.chooseApp',
      'viewer.folder',
      'viewer.files',
      'viewer.compress',
      'viewer.filmstrip',
      'viewer.autoHide',
      'viewer.trash',
    ].flatMap((id) => {
      const command = commandById(id)
      return command ? [command] : []
    }),
  ),
)
const zoomItems = computed(() =>
  menuItems(
    ['viewer.fit', 'viewer.actual', 'viewer.doubleZoom'].flatMap((id) => {
      const command = commandById(id)
      return command ? [command] : []
    }),
  ),
)
function fullscreenChanged() {
  isFullscreen.value = document.fullscreenElement === root.value
}
function openDefault() {
  if (src.value.id) void engine.openMedia(src.value.id).catch(reportError)
}
function setWheel(e: Event) {
  if (!(e.target instanceof HTMLSelectElement)) return
  const value = e.target.value
  const wheel = Object.values(ViewerWheel).find((choice) => choice.localeCompare(value) === 0)
  if (wheel !== undefined) void prefs.setViewer({ wheel })
}
function setIntervalSeconds(e: Event) {
  if (e.target instanceof HTMLSelectElement) void prefs.setViewer({ slideshowSeconds: Number(e.target.value) })
}

watch(
  [() => src.value.id, retry],
  async ([id], _old, onCleanup) => {
    geometry.value = undefined
    const request = ++detailRequest
    onCleanup(() => detailRequest++)
    detail.value = null
    loadError.value = ''
    loading.value = !!id
    if (!id) return
    try {
      const d = await engine.asset(id)
      if (request !== detailRequest) return
      detail.value = d
    } catch (e) {
      if (request === detailRequest && !isCancelled(e)) loadError.value = errorText(e)
    } finally {
      if (request === detailRequest) loading.value = false
    }
  },
  { immediate: true },
)

function go(dir: 1 | -1) {
  void src.value.step(dir).catch(reportError)
}

function close() {
  src.value.close()
}

async function fullscreen() {
  try {
    if (document.fullscreenElement) await document.exitFullscreen()
    else await root.value?.requestFullscreen()
  } catch (e) {
    reportError(e)
  }
}

function activity(event?: Event) {
  if (event?.type.startsWith('pointer'))
    chromeHovered.value =
      event.target instanceof Element &&
      !!event.target.closest('.bar, .bottom-chrome, .video-controls, [data-viewer-settings]')
  hidden.value = false
  clearTimeout(hideTimer)
  if (viewing.value.autoHide)
    hideTimer = setTimeout(() => {
      const focused = document.activeElement
      const usingChrome =
        focused instanceof Element &&
        !!focused.closest('.bar, .bottom-chrome, .video-controls, .inspector, [data-viewer-settings]')
      if (
        layer.owns() &&
        !usingChrome &&
        !chromeHovered.value &&
        !menuOpen.value &&
        !zoomOpen.value &&
        !lib.showDetails &&
        !filesOpen.value
      )
        hidden.value = true
    }, 3000)
}

watch([() => src.value.id, () => viewing.value.filmstrip, comparing], loadNeighbours, { immediate: true })

async function loadNeighbours() {
  const request = ++neighbourhoodRequest
  neighbours.value = []
  try {
    const items = await src.value.window?.()
    if (request === neighbourhoodRequest) neighbours.value = items ?? []
  } catch (e) {
    if (request === neighbourhoodRequest && !isCancelled(e)) reportError(e)
  }
}

watch([comparing, () => src.value.id], async ([value]) => {
  const request = ++comparisonRequest
  comparison.value = null
  comparisonItem.value = null
  if (!value) return
  const id = src.value.id
  try {
    const items = await src.value.window?.()
    if (request !== comparisonRequest) return
    const other = items?.find((entry) => entry.id !== id && !entry.v)
    if (!other) {
      comparing.value = false
      return
    }
    const data = await engine.asset(other.id)
    if (request === comparisonRequest && comparing.value && src.value.id === id) {
      comparison.value = data
      comparisonItem.value = other
    }
  } catch (e) {
    if (request === comparisonRequest) {
      comparing.value = false
      if (!isCancelled(e)) reportError(e)
    }
  }
})

useLiveReload(
  async () => {
    await loadNeighbours()
    const currentID = src.value.id
    const request = ++detailRequest
    if (currentID) {
      try {
        const data = await engine.asset(currentID)
        if (request === detailRequest && src.value.id === currentID) {
          detail.value = data
          loadError.value = ''
          loading.value = false
        }
      } catch (e) {
        if (request === detailRequest && !isCancelled(e)) {
          detail.value = null
          loadError.value = errorText(e)
          loading.value = false
        }
      }
    }
    const current = comparison.value
    if (!current) return
    const comparisonRevision = comparisonRequest
    try {
      const data = await engine.asset(current.id)
      if (comparisonRevision === comparisonRequest && comparison.value?.id === current.id) comparison.value = data
    } catch (e) {
      if (comparisonRevision === comparisonRequest && !isCancelled(e)) {
        comparing.value = false
        comparison.value = null
        comparisonItem.value = null
      }
    }
  },
  (c) => c.assets || c.thumbs,
  1,
)
watch(
  () => viewing.value.autoHide,
  () => activity(),
)

watch([sliding, () => src.value.id, () => viewing.value.slideshowSeconds, slideCycle], () => {
  clearTimeout(slideTimer)
  clearInterval(slideTick)
  slideProgress.value = 0
  if (!sliding.value) return
  const started = Date.now()
  slideTick = setInterval(() => {
    slideProgress.value = Math.min(1, (Date.now() - started) / (viewing.value.slideshowSeconds * 1000))
  }, 200)
  slideTimer = setTimeout(() => {
    if (!layer.owns() || document.hidden) {
      sliding.value = false
      return
    }
    const id = src.value.id
    void src.value
      .step(1)
      .then(async () => {
        if (src.value.id === id) {
          if (viewing.value.loop && src.value.first) {
            await src.value.first()
            if (src.value.id === id) slideCycle.value++
          } else {
            sliding.value = false
            notify({ id: 'viewer.slideshowFinished', kind: 'info', title: t('viewer.slideshowFinished') })
          }
        }
      })
      .catch((e: unknown) => {
        sliding.value = false
        reportError(e)
      })
  }, viewing.value.slideshowSeconds * 1000)
})

async function adjust(rotation: number, flip: boolean) {
  const current = detail.value
  if (!current || !ordinary.value || adjusting.value) return
  adjusting.value = true
  try {
    const adjustment = await engine.setAdjustment(current.id, rotation, flip)
    if (detail.value?.id === current.id) detail.value = { ...detail.value, adjustment }
  } catch (e) {
    reportError(e)
  } finally {
    adjusting.value = false
  }
}

// Listens in the bubble phase, so a control that handles a key itself (a
// picker closing its menu on Escape) stops it before it reaches the viewer.
function onKey(e: KeyboardEvent) {
  activity()
  if (e.defaultPrevented || !layer.owns()) return
  // A chord with Ctrl or Alt is only ever a shortcut the user gave a viewer
  // command: nothing else here answers to one.
  if (e.ctrlKey || e.altKey || e.metaKey) {
    if (!isTyping(e)) dispatchKey(e, 'viewer')
    return
  }
  // Escape belongs to the browser while the player is fullscreen.
  if (e.key === 'Escape' && document.fullscreenElement) return
  if (e.key === 'Tab') {
    trapTab(e)
    return
  }
  if (e.key === 'Escape' && lib.showDetails) {
    e.preventDefault()
    lib.showDetails = false
    return
  }
  // Typing in the details panel (adding a tag): only Escape still closes.
  if (isTyping(e) && e.key !== 'Escape') return
  if (e.target === root.value && [' ', 'k', 'm'].includes(e.key.toLowerCase())) {
    if (player.value?.handleKey(e)) return
  }
  if (e.key === '+') {
    e.preventDefault()
    void runCommand('viewer.zoomIn')
    return
  }
  // Playback controls keep their own arrow keys.
  const slider = e.target instanceof Element && e.target.closest('[role="slider"], [role="separator"]')
  if ((ownsArrows(e) || slider) && (e.key === 'ArrowLeft' || e.key === 'ArrowRight')) return
  if (dispatchKey(e, 'viewer')) return
  // These viewer shortcuts ignored Shift before the command registry did the
  // matching: they still do, unless the user gave the shifted key to another.
  if (e.shiftKey && (/^[fi]$/i.test(e.key) || ['Escape', 'ArrowLeft', 'ArrowRight'].includes(e.key))) {
    const plain = new KeyboardEvent('keydown', { key: e.key.toLowerCase() })
    if (dispatchKey(plain, 'viewer')) e.preventDefault()
  }
}

useCommands([
  {
    id: 'viewer.openFile',
    group: 'media',
    title: () => t('files.open'),
    icon: 'external',
    zone: 'viewer',
    available: () => !!detail.value?.paths.length,
    run: openDefault,
  },
  {
    id: 'viewer.chooseApp',
    group: 'media',
    title: () => t('files.openWith'),
    icon: 'external',
    zone: 'viewer',
    available: () => !!detail.value?.paths.length,
    run: () => detail.value && engine.openMedia(detail.value.id, '', true),
  },
  {
    id: 'viewer.folder',
    group: 'media',
    title: () => t('common.showInFolder'),
    icon: 'folder',
    zone: 'viewer',
    available: () => !!detail.value?.paths.length,
    run: () => detail.value && engine.showInFolder(detail.value.id),
  },
  {
    id: 'viewer.files',
    group: 'media',
    title: () => t('files.title'),
    icon: 'files',
    zone: 'viewer',
    available: () => ordinary.value && !!detail.value,
    run: () => (filesOpen.value = true),
  },
  {
    id: 'viewer.autoHide',
    group: 'view',
    title: () => t('viewer.autoHide'),
    icon: 'eye-off',
    zone: 'viewer',
    run: () => prefs.setViewer({ autoHide: !viewing.value.autoHide }),
  },
  {
    id: 'viewer.doubleZoom',
    group: 'view',
    title: () => t('viewer.doubleZoom'),
    icon: 'zoom-in',
    zone: 'viewer',
    available: () => !!image.value,
    run: () => image.value?.zoom(200 / Math.max(1, image.value.scale)),
  },
  compressionCommand(
    'viewer.compress',
    () => !!image.value && ordinary.value,
    () => (detail.value ? [{ assetId: detail.value.id, path: '' }] : []),
    'viewer',
  ),
  {
    id: 'viewer.fullscreen',
    group: 'view',
    title: () => t('viewer.fullscreen'),
    icon: 'maximize',
    zone: 'viewer',
    run: fullscreen,
  },
  {
    id: 'viewer.filmstrip',
    group: 'view',
    title: () => t('viewer.filmstrip'),
    icon: 'film',
    zone: 'viewer',
    available: () => !!src.value.window,
    run: () => prefs.setViewer({ filmstrip: !viewing.value.filmstrip }),
  },
  {
    id: 'viewer.slideshow',
    group: 'view',
    title: () => t('viewer.slideshow'),
    icon: 'slideshow',
    enabled: () => sliding.value || canNext.value || viewing.value.loop,
    zone: 'viewer',
    run: () => (sliding.value = !sliding.value),
  },
  {
    id: 'viewer.compare',
    group: 'view',
    title: () => t('viewer.compare'),
    icon: 'layers',
    zone: 'viewer',
    available: () => !!image.value && !!src.value.window,
    enabled: () => comparing.value || canCompare.value,
    run: () => (comparing.value = !comparing.value),
  },
  {
    id: 'viewer.rotate',
    group: 'media',
    title: () => t('viewer.rotate'),
    icon: 'rotate',
    zone: 'viewer',
    available: () => !!image.value && ordinary.value && !adjusting.value,
    run: () => adjust((detail.value?.adjustment?.rotation ?? 0) + 1, detail.value?.adjustment?.flip ?? false),
  },
  {
    id: 'viewer.flip',
    group: 'media',
    title: () => t('viewer.flip'),
    icon: 'flip',
    zone: 'viewer',
    available: () => !!image.value && ordinary.value && !adjusting.value,
    run: () => adjust(detail.value?.adjustment?.rotation ?? 0, !detail.value?.adjustment?.flip),
  },
  {
    id: 'viewer.resetAdjustment',
    group: 'media',
    title: () => t('viewer.resetAdjustment'),
    enabled: () => adjusted.value,
    icon: 'undo',
    zone: 'viewer',
    available: () => !!image.value && ordinary.value && !adjusting.value,
    run: () => adjust(0, false),
  },
  {
    id: 'viewer.zoomIn',
    group: 'view',
    title: () => t('viewer.zoomIn'),
    enabled: () => !!image.value?.canZoomIn,
    icon: 'zoom-in',
    keys: '=',
    zone: 'viewer',
    available: () => !!image.value,
    run: () => image.value?.zoom(1.25),
  },
  {
    id: 'viewer.zoomOut',
    group: 'view',
    title: () => t('viewer.zoomOut'),
    enabled: () => !!image.value?.canZoomOut,
    icon: 'zoom-out',
    keys: '-',
    zone: 'viewer',
    available: () => !!image.value,
    run: () => image.value?.zoom(0.8),
  },
  {
    id: 'viewer.fit',
    group: 'view',
    title: () => t('viewer.fit'),
    icon: 'maximize',
    keys: '0',
    zone: 'viewer',
    available: () => !!image.value,
    run: () => image.value?.fit(),
  },
  {
    id: 'viewer.actual',
    group: 'view',
    title: () => t('viewer.actual'),
    icon: 'image',
    keys: '1',
    zone: 'viewer',
    available: () => !!image.value,
    run: () => image.value?.actual(),
  },
  {
    id: 'viewer.close',
    group: 'view',
    title: () => t('common.close'),
    icon: 'close',
    keys: 'Escape',
    zone: 'viewer',
    run: close,
  },
  {
    id: 'viewer.previous',
    group: 'view',
    title: () => t('viewer.prev'),
    enabled: () => canPrevious.value,
    icon: 'chevron-left',
    keys: 'ArrowLeft',
    zone: 'viewer',
    run: () => go(-1),
  },
  {
    id: 'viewer.next',
    group: 'view',
    title: () => t('viewer.next'),
    enabled: () => canNext.value,
    icon: 'chevron-right',
    keys: 'ArrowRight',
    zone: 'viewer',
    run: () => go(1),
  },
  {
    id: 'viewer.favourite',
    group: 'media',
    title: () => t(item.value?.f ? 'details.unfavourite' : 'details.favourite'),
    icon: 'heart',
    keys: 'F',
    zone: 'viewer',
    available: () => !!actions.value.favourite,
    run: () => actions.value.favourite?.(),
  },
  {
    id: 'viewer.details',
    group: 'view',
    title: () => t('library.info'),
    icon: 'info',
    keys: 'I',
    zone: 'viewer',
    available: () => actions.value.details,
    run: () => (lib.showDetails = !lib.showDetails),
  },
  {
    id: 'viewer.trash',
    group: 'media',
    title: () => t('trash.move'),
    icon: 'trash',
    zone: 'viewer',
    available: () => !!actions.value.moveToTrash,
    run: () => actions.value.moveToTrash?.(),
  },
  {
    id: 'viewer.restore',
    group: 'media',
    title: () => t('trash.restore'),
    icon: 'restore',
    zone: 'viewer',
    available: () => !!actions.value.restore,
    run: () => actions.value.restore?.(),
  },
  {
    id: 'viewer.delete',
    group: 'media',
    title: () => t('trash.deleteFromDisk'),
    icon: 'trash',
    zone: 'viewer',
    available: () => !!actions.value.deleteFromDisk,
    run: () => actions.value.deleteFromDisk?.(),
  },
])

function trapTab(e: KeyboardEvent) {
  const scope =
    mode.value !== 'wide' && lib.showDetails
      ? root.value?.querySelector('.inspector')
      : (document.fullscreenElement ?? root.value)
  const controls = scope?.querySelectorAll<HTMLElement>(
    'button:not(:disabled), a[href], input:not(:disabled), select:not(:disabled), [tabindex="0"]',
  )
  const first = controls?.[0]
  const last = controls?.[controls.length - 1]
  if (!first || !last) {
    e.preventDefault()
    root.value?.focus()
  } else if (e.shiftKey && (document.activeElement === first || document.activeElement === root.value)) {
    e.preventDefault()
    last.focus()
  } else if (!e.shiftKey && (document.activeElement === last || document.activeElement === root.value)) {
    e.preventDefault()
    first.focus()
  }
}

// A modal: the app behind it can't be tabbed to or clicked while it is open
// (the viewer is outside it, in <body>; a confirmation can still show).
function setBehind(inert: boolean) {
  document.querySelector('.shell')?.toggleAttribute('inert', inert)
}

onMounted(() => {
  previousFocus = document.activeElement
  window.addEventListener('keydown', onKey)
  document.addEventListener('fullscreenchange', fullscreenChanged)
  setBehind(true)
  root.value?.focus()
  activity()
})
onBeforeUnmount(() => {
  detailRequest++
  comparisonRequest++
  neighbourhoodRequest++
  clearTimeout(slideTimer)
  clearInterval(slideTick)
  clearTimeout(hideTimer)
  window.removeEventListener('keydown', onKey)
  document.removeEventListener('fullscreenchange', fullscreenChanged)
  setBehind(false)
  if (previousFocus instanceof HTMLElement && previousFocus.isConnected) previousFocus.focus()
})
</script>

<template>
  <Teleport to="body">
    <div
      ref="root"
      class="viewer"
      :class="{ 'controls-hidden': hidden }"
      role="dialog"
      aria-modal="true"
      :aria-busy="loading"
      :aria-label="heading"
      tabindex="-1"
      @pointermove="activity"
      @pointerdown="activity"
      @pointerleave="activity"
      @focusin="activity"
    >
      <div class="stage">
        <header class="bar">
          <IconButton
            :kbds="['Esc']"
            class="viewer-control"
            :icon="icons['arrow-left']"
            :label="t('common.close')"
            :tip="t('viewer.return')"
            variant="ghost"
            @click="runCommand('viewer.close')"
          />
          <img class="brand" src="/lumen.png" alt="" />
          <div class="identity">
            <h1>{{ heading }}</h1>
            <span class="date" :title="title">{{ title }}</span>
          </div>
          <IconButton
            v-if="actions.favourite"
            class="viewer-control favourite"
            :label="t(item?.f ? 'details.unfavourite' : 'details.favourite')"
            :kbds="capsOf('viewer.favourite')"
            :aria-pressed="!!item?.f"
            variant="ghost"
            @click="runCommand('viewer.favourite')"
            ><Icon name="heart" :filled="!!item?.f"
          /></IconButton>
          <IconButton
            v-if="actions.details"
            ref="detailsButton"
            class="viewer-control"
            :icon="icons.inspector"
            :label="t('library.info')"
            :tip="t(lib.showDetails ? 'library.hideInfo' : 'library.showInfo')"
            :kbds="capsOf('viewer.details')"
            :aria-pressed="lib.showDetails"
            variant="ghost"
            @click="runCommand('viewer.details')"
          />
          <!-- Blocked media (a covered photo) keeps its bar to Close and Move to
               Trash: nothing invites lingering over it. -->
          <IconButton
            v-if="image && src.window && ordinary"
            class="viewer-control"
            :icon="icons.compare"
            :label="t('viewer.compare')"
            :tip="t(comparing ? 'viewer.stopComparing' : canCompare ? 'viewer.compare' : 'viewer.compareUnavailable')"
            :disabled="!comparing && !canCompare"
            :aria-pressed="comparing"
            variant="ghost"
            @click="runCommand('viewer.compare')"
          />
          <IconButton
            v-if="(image || sliding) && ordinary"
            class="viewer-control"
            :icon="icons[sliding ? 'pause' : 'slideshow']"
            :label="t('viewer.slideshow')"
            :tip="t(sliding ? 'viewer.stopSlideshow' : 'viewer.startSlideshow')"
            :aria-pressed="sliding"
            :disabled="!sliding && !canNext && !viewing.loop"
            variant="ghost"
            @click="runCommand('viewer.slideshow')"
          />
          <!-- Blocked media has one thing to do: its bar offers it directly. -->
          <UButton
            v-if="!ordinary && actions.moveToTrash"
            class="viewer-control"
            :icon="icons.trash"
            color="error"
            variant="ghost"
            @click="runCommand('viewer.trash')"
            >{{ t('trash.move') }}</UButton
          >
          <UDropdownMenu v-else v-model:open="menuOpen" :items="moreItems" :portal="false" :content="{ align: 'end' }">
            <IconButton class="viewer-control" :icon="icons.more" :label="t('viewer.more')" variant="ghost" />
            <template #content-bottom>
              <div class="viewing-settings" data-viewer-settings>
                <label
                  >{{ t('viewer.wheel')
                  }}<select :value="viewing.wheel" @change="setWheel">
                    <option :value="ViewerWheel.ViewerWheelZoom">{{ t('viewer.wheel.zoom') }}</option>
                    <option :value="ViewerWheel.ViewerWheelNavigate">{{ t('viewer.wheel.navigate') }}</option>
                  </select></label
                >
                <label
                  >{{ t('viewer.interval')
                  }}<select :value="viewing.slideshowSeconds" @change="setIntervalSeconds">
                    <option v-for="seconds in [1, 3, 5, 10, 30]" :key="seconds" :value="seconds">
                      {{ formatNumber(seconds) }}
                    </option>
                  </select></label
                >
                <UButton
                  variant="ghost"
                  :aria-pressed="viewing.loop"
                  @click="prefs.setViewer({ loop: !viewing.loop })"
                  >{{ t('viewer.slideshowLoop') }}</UButton
                >
              </div>
            </template>
          </UDropdownMenu>
        </header>
        <div v-if="!ordinary" class="private-notice" role="status">
          <span>{{ t(actions.restore ? 'viewer.trashContext' : 'viewer.blockedContext') }}</span>
          <UButton v-if="actions.restore" :icon="icons.restore" variant="ghost" @click="runCommand('viewer.restore')">{{
            t('trash.restore')
          }}</UButton>
          <UButton
            v-if="actions.deleteFromDisk"
            :icon="icons.recycle"
            variant="ghost"
            @click="runCommand('viewer.delete')"
            >{{ t('trash.deleteFromDisk') }}</UButton
          >
        </div>

        <div
          class="media"
          :class="{
            comparing: comparison && comparisonItem,
            'with-filmstrip': viewing.filmstrip && neighbours.length,
            'photo-media': !detail?.video,
          }"
        >
          <FeatureState v-if="loadError" kind="error" :title="t('viewer.loadFailed')" :text="loadError"
            ><UButton @click="retry++">{{ t('common.retry') }}</UButton
            ><UButton variant="ghost" @click="close">{{ t('viewer.return') }}</UButton></FeatureState
          >
          <FeatureState v-else-if="!item && !loading" kind="empty" icon="image-off" :title="t('viewer.notFound')" />
          <template v-else-if="item">
            <VideoPlayer
              v-if="detail?.video && detail.id === item.id && videosAvailable()"
              ref="player"
              :key="detail.id"
              :detail="detail"
              :at="item.at ?? 0"
              :thumb-version="item.t"
              :private="!ordinary"
              :chrome-hidden="hidden"
              @timeupdate="src.touch?.()"
              ><template #filmstrip
                ><Filmstrip
                  v-if="viewing.filmstrip && neighbours.length"
                  :items="neighbours"
                  :selected="item.id"
                  @select="src.jump?.($event)" /></template
            ></VideoPlayer>
            <UAlert
              v-if="detail?.video && !videosAvailable()"
              class="playback-message"
              role="alert"
              color="error"
              variant="subtle"
              :title="t('viewer.videoUnavailable')"
            />
            <template v-else-if="!detail?.video">
              <ImageSurface
                :id="item.id"
                ref="image"
                :key="item.id"
                :thumb="item.t"
                :width="item.w"
                :height="item.h"
                :detail="detail"
                :linked="comparing && linked ? geometry : undefined"
                :open="viewing.open"
                :wheel="viewing.wheel"
                :side="viewing.renderSide"
                :still="viewing.stillAnimations"
                @change="geometry = $event"
                @step="go"
              />
              <FeatureState
                v-if="!item.t && !detail?.caps.original"
                :kind="loading ? 'loading' : 'empty'"
                icon="image-off"
                :title="loading ? t('details.loading') : t('viewer.noPreview')"
                :text="loading ? undefined : t('viewer.noPreviewHint')"
                ><UButton v-if="!loading && detail?.paths.length" @click="openDefault">{{
                  t('files.open')
                }}</UButton></FeatureState
              >
            </template>
          </template>
          <FeatureState v-else kind="loading" :title="t('details.loading')" />
          <ImageSurface
            v-if="comparison && comparisonItem"
            :id="comparisonItem.id"
            :key="comparisonItem.id"
            :thumb="comparisonItem.t"
            :width="comparisonItem.w"
            :height="comparisonItem.h"
            :detail="comparison"
            :linked="linked ? geometry : undefined"
            :side="viewing.renderSide"
            :still="viewing.stillAnimations"
            @change="geometry = $event"
          />
        </div>

        <div v-if="comparing" class="comparison-controls glass">
          <UButton :aria-pressed="linked" variant="ghost" @click="linked = !linked">{{ t('viewer.linked') }}</UButton
          ><IconButton
            :icon="icons.close"
            :label="t('viewer.stopComparing')"
            variant="ghost"
            @click="comparing = false"
          />
        </div>
        <div v-if="!detail?.video" class="bottom-chrome">
          <div v-if="sliding" class="slideshow-status" role="status">
            <span>{{ t('viewer.slideshowStatus', { n: viewing.slideshowSeconds }) }}</span
            ><UButton variant="ghost" @click="sliding = false">{{ t('viewer.stopSlideshow') }}</UButton>
            <div
              class="slide-progress"
              role="progressbar"
              :aria-label="t('viewer.slideshow')"
              :aria-valuenow="Math.round(slideProgress * 100)"
              :aria-valuemin="0"
              :aria-valuemax="100"
            >
              <span :style="{ transform: `scaleX(${slideProgress})` }" />
            </div>
          </div>
          <Filmstrip
            v-if="viewing.filmstrip && neighbours.length && item"
            :items="neighbours"
            :selected="item.id"
            @select="src.jump?.($event)"
          />
          <div v-if="image" class="image-controls">
            <div v-if="image.pages > 1 || image.animated" class="photo-secondary">
              <template v-if="image.pages > 1">
                <IconButton
                  :icon="icons['chevron-left']"
                  :label="t('viewer.previousPage')"
                  variant="ghost"
                  :disabled="image.page === 0"
                  @click="image.turnPage(-1)"
                />
                <span role="status" data-pages>{{
                  t('viewer.page', { page: image.page + 1, pages: image.pages })
                }}</span>
                <IconButton
                  :icon="icons['chevron-right']"
                  :label="t('viewer.nextPage')"
                  variant="ghost"
                  :disabled="image.page === image.pages - 1"
                  @click="image.turnPage(1)"
                />
              </template>
              <UButton
                v-if="image.animated"
                :icon="icons[image.playing ? 'pause' : 'play']"
                :aria-pressed="image.playing"
                variant="ghost"
                data-animation
                @click="image.toggleAnimation()"
                >{{ t(image.playing ? 'viewer.pauseAnimation' : 'viewer.playAnimation') }}</UButton
              >
            </div>
            <div class="photo-tools">
              <div class="tool-group">
                <div v-if="ordinary" class="glass tool-group">
                  <IconButton
                    :icon="icons.rotate"
                    :label="t('viewer.rotate')"
                    :disabled="adjusting"
                    variant="ghost"
                    @click="runCommand('viewer.rotate')"
                  />
                  <IconButton
                    :icon="icons.flip"
                    :label="t('viewer.flip')"
                    :disabled="adjusting"
                    variant="ghost"
                    @click="runCommand('viewer.flip')"
                  />
                  <IconButton
                    :icon="icons.reset"
                    :label="t('viewer.resetAdjustment')"
                    :tip="t(adjusted ? 'viewer.resetAdjustment' : 'viewer.resetUnavailable')"
                    :disabled="adjusting || !adjusted"
                    variant="ghost"
                    @click="runCommand('viewer.resetAdjustment')"
                  />
                </div>
                <IconButton
                  v-if="src.window"
                  :icon="icons.film"
                  :label="t('viewer.filmstrip')"
                  :tip="t(viewing.filmstrip ? 'viewer.hideFilmstrip' : 'viewer.showFilmstrip')"
                  :aria-pressed="viewing.filmstrip"
                  variant="ghost"
                  @click="runCommand('viewer.filmstrip')"
                />
                <IconButton
                  v-if="actions.moveToTrash"
                  :icon="icons.trash"
                  :label="t('trash.move')"
                  variant="ghost"
                  @click="runCommand('viewer.trash')"
                />
              </div>
              <span v-if="positionText" class="position">{{ positionText }}</span>
              <div class="tool-group zoom-tools">
                <div class="glass tool-group">
                  <IconButton
                    :icon="icons.minus"
                    :label="t('viewer.zoomOut')"
                    :kbds="capsOf('viewer.zoomOut')"
                    :disabled="!image.canZoomOut"
                    :tip="t(image.canZoomOut ? 'viewer.zoomOut' : 'viewer.minimumZoom')"
                    variant="ghost"
                    @click="runCommand('viewer.zoomOut')"
                  />
                  <UDropdownMenu
                    v-model:open="zoomOpen"
                    :items="zoomItems"
                    :portal="false"
                    :content="{ side: 'top', align: 'end' }"
                    ><UButton
                      class="zoom-level"
                      :aria-label="t('viewer.zoomLevel')"
                      :trailing-icon="icons['chevron-down']"
                      variant="ghost"
                      ><span role="status" data-zoom>{{ image?.scale }}%</span></UButton
                    ></UDropdownMenu
                  >
                  <IconButton
                    :icon="icons.plus"
                    :label="t('viewer.zoomIn')"
                    :kbds="capsOf('viewer.zoomIn')"
                    :disabled="!image.canZoomIn"
                    :tip="t(image.canZoomIn ? 'viewer.zoomIn' : 'viewer.maximumZoom')"
                    variant="ghost"
                    @click="runCommand('viewer.zoomIn')"
                  />
                </div>
                <IconButton
                  :icon="icons[isFullscreen ? 'minimize' : 'maximize']"
                  :label="t(isFullscreen ? 'viewer.exitFullscreen' : 'viewer.fullscreen')"
                  :aria-pressed="isFullscreen"
                  variant="ghost"
                  @click="runCommand('viewer.fullscreen')"
                />
              </div>
            </div>
          </div>
        </div>

        <IconButton
          side="right"
          class="viewer-control nav prev"
          :icon="icons['chevron-left']"
          :label="t('viewer.prev')"
          :kbds="capsOf('viewer.previous')"
          :disabled="!canPrevious"
          :tip="t(canPrevious ? 'viewer.prev' : 'viewer.firstItem')"
          variant="ghost"
          @click="runCommand('viewer.previous')"
        />
        <IconButton
          side="left"
          class="viewer-control nav next"
          :icon="icons['chevron-right']"
          :label="t('viewer.next')"
          :kbds="capsOf('viewer.next')"
          :disabled="!canNext"
          :tip="t(canNext ? 'viewer.next' : 'viewer.lastItem')"
          variant="ghost"
          @click="runCommand('viewer.next')"
        />
      </div>
      <DetailsPanel v-if="lib.showDetails && actions.details" :sel="src.sel" />
      <FileTools v-model:open="filesOpen" :ids="detail ? [detail.id] : []" />
    </div>
  </Teleport>
</template>

<style scoped>
.viewer {
  position: fixed;
  inset: 0;
  z-index: var(--lumen-z-viewer);
  display: flex;
  background: var(--lumen-viewer-bg);
  color: var(--lumen-viewer-text);
  color-scheme: dark;
  outline: none;
  --ui-bg: var(--lumen-viewer-bg);
  --ui-bg-muted: var(--lumen-viewer-control);
  --ui-bg-elevated: var(--lumen-viewer-bg);
  --ui-bg-accented: var(--lumen-viewer-control);
  --ui-text: var(--lumen-viewer-text);
  --ui-text-highlighted: var(--lumen-viewer-text);
  --ui-text-toned: var(--lumen-viewer-text-muted);
  --ui-text-muted: var(--lumen-viewer-text-muted);
  --ui-text-dimmed: var(--lumen-viewer-text-muted);
  --ui-border: var(--lumen-viewer-control);
  --lumen-bg-sunken: var(--lumen-viewer-glass);
  --lumen-bg-overlay: var(--lumen-viewer-bg);
  --lumen-bg-raised: var(--lumen-viewer-glass);
}
.stage {
  position: relative;
  flex: 1;
  min-width: 0;
  overflow: hidden;
  container-type: inline-size;
}
.bar {
  position: absolute;
  inset: 0 0 auto;
  z-index: 4;
  display: flex;
  align-items: center;
  gap: 0.25rem;
  height: 4rem;
  padding: 0.75rem 1rem;
  background: linear-gradient(var(--lumen-viewer-bg), transparent);
}
.brand {
  width: 1.5rem;
  height: 1.5rem;
  margin-inline: 0.5rem;
}
.identity {
  flex: 1;
  min-width: 0;
  display: grid;
  gap: 0.25rem;
  margin-inline: 0.5rem;
}
.identity h1,
.date {
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
  margin: 0;
}
.identity h1 {
  font-size: 0.8125rem;
  font-weight: 600;
}
.date,
.position {
  font-size: 0.75rem;
  color: var(--lumen-viewer-text-muted);
  font-variant-numeric: tabular-nums;
}
.viewer-control,
.tool-group :deep(button),
.photo-secondary :deep(button) {
  flex-shrink: 0;
  color: var(--lumen-viewer-text);
}
.viewer-control:hover,
.viewer-control[aria-pressed='true'],
.tool-group :deep(button:hover) {
  background: var(--lumen-viewer-control);
}
.favourite[aria-pressed='true'] {
  color: var(--lumen-heart);
  animation: lumen-pop var(--lumen-d-base) var(--lumen-ease-spring);
}
.media {
  position: absolute;
  inset: 5rem 4rem 0;
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  grid-template-rows: minmax(0, 1fr);
  place-items: center;
}
.media.photo-media {
  bottom: 5rem;
}
.media.photo-media.with-filmstrip {
  bottom: 9.5rem;
}
.media.comparing {
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 1rem;
}
.media:has(.video-player) {
  inset: 4rem 0 0;
}
.bottom-chrome {
  position: absolute;
  inset: auto 0 0;
  z-index: 3;
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
  padding: 0.75rem 1rem;
  background: linear-gradient(transparent, var(--lumen-viewer-bg));
}
.image-controls {
  display: grid;
  gap: 0.5rem;
}
.photo-tools {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
}
.tool-group,
.photo-secondary,
.slideshow-status,
.comparison-controls {
  display: flex;
  align-items: center;
  gap: 0.25rem;
}
.photo-secondary,
.slideshow-status {
  justify-content: center;
  font-size: 0.8125rem;
}
.slide-progress {
  width: 8rem;
  height: 0.25rem;
  overflow: hidden;
  background: var(--lumen-viewer-control);
  border-radius: var(--lumen-r-xs);
}
.slide-progress span {
  display: block;
  width: 100%;
  height: 100%;
  background: var(--ui-primary);
  transform-origin: left;
  transition: transform var(--lumen-d-base) linear;
}
.glass {
  padding: 0.25rem;
  border: 1px solid var(--lumen-viewer-control);
  border-radius: var(--lumen-r-lg);
  background: var(--lumen-viewer-glass);
  backdrop-filter: blur(16px);
}
.zoom-level {
  min-width: 4rem;
  color: var(--lumen-viewer-text);
  font-variant-numeric: tabular-nums;
}
.comparison-controls {
  position: absolute;
  top: 4rem;
  left: 50%;
  transform: translateX(-50%);
  z-index: 3;
}
.private-notice {
  position: absolute;
  inset: 4rem 1rem auto;
  z-index: 3;
  display: flex;
  align-items: center;
  gap: 0.5rem;
  padding: 0.5rem 0.75rem;
  font-size: 0.8125rem;
  background: var(--lumen-viewer-glass);
  border-radius: var(--lumen-r-md);
}
.private-notice span {
  flex: 1;
}
.stage:has(.private-notice) .media {
  top: 8rem;
}
.viewing-settings {
  display: grid;
  gap: 0.75rem;
  padding: 0.75rem;
  font-size: 0.8125rem;
}
.viewing-settings label {
  display: grid;
  gap: 0.25rem;
}
.viewing-settings select {
  min-height: 2rem;
  padding: 0.25rem 0.5rem;
  background: var(--lumen-viewer-bg);
  color: var(--lumen-viewer-text);
  border: 1px solid var(--lumen-viewer-control);
  border-radius: var(--lumen-r-sm);
}
.bar,
.bottom-chrome {
  transition:
    opacity var(--lumen-d-base) var(--lumen-ease-out),
    transform var(--lumen-d-base) var(--lumen-ease-out);
}
.controls-hidden .bar {
  opacity: 0;
  transform: translateY(-0.5rem);
  pointer-events: none;
}
.controls-hidden .bottom-chrome {
  opacity: 0;
  transform: translateY(0.5rem);
  pointer-events: none;
}
.controls-hidden .nav {
  opacity: 0;
  pointer-events: none;
}
.controls-hidden :is(.bar, .bottom-chrome):focus-within,
.controls-hidden .nav:focus-visible {
  opacity: 1;
  transform: none;
  pointer-events: auto;
}
.nav {
  position: absolute;
  top: 50%;
  z-index: 2;
  transform: translateY(-50%);
  width: 2.75rem;
  height: 2.75rem;
  border-radius: 50%;
  background: var(--lumen-viewer-glass);
  backdrop-filter: blur(10px);
  transition: opacity var(--lumen-d-fast) var(--lumen-ease-out);
}
.prev {
  left: 1rem;
}
.next {
  right: 1rem;
}
.playback-message {
  position: absolute;
  z-index: 3;
  max-width: min(28rem, calc(100% - 2rem));
}
.viewer :deep(.feature-state) {
  padding: 1rem;
}
@media (max-width: 1023px) {
  .viewer :deep(> .inspector) {
    position: absolute;
    inset: 0 0 0 auto;
    z-index: 6;
    height: 100%;
    box-shadow: var(--lumen-shadow-3);
  }
}
@container (max-width: 47.5rem) {
  .brand {
    display: none;
  }
  .media {
    inset-inline: 3rem;
  }
  .photo-tools {
    gap: 0.25rem;
  }
}
@container (max-width: 30rem) {
  .position {
    display: none;
  }
  .tool-group {
    gap: 0;
  }
  .bar {
    padding-inline: 0.5rem;
  }
  .photo-tools :deep(button) {
    width: 2rem;
    min-width: 2rem;
  }
  .zoom-level {
    min-width: 4rem !important;
  }
}
@media (max-height: 540px) {
  .media.photo-media.with-filmstrip {
    bottom: 8.5rem;
  }
  .bottom-chrome {
    gap: 0.5rem;
  }
}
@media (forced-colors: active) {
  .viewer :deep(button),
  .glass {
    outline: thin solid ButtonText;
  }
}
</style>
