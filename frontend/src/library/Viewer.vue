<script setup lang="ts">
// Full-window viewer. Shows the 1024px preview instantly, then swaps in the
// original when the WebView can display it (JPEG, PNG, WebP, AVIF). Its
// photos, the one beside each and what may be done with it come from a
// source: the library grid unless one is given (the Blocked screen's browser).
import IconButton from '../shared/IconButton.vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import { computed, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
import { engine, isCancelled, videosAvailable } from '../bridge'
import type { AssetDetail, GridItem } from '../bridge'
import { icons } from '../design/icons'
import { dispatchKey, runCommand, useCommands } from '../shared/commands'
import { errorText, reportError } from '../shared/errors'
import FeatureState from '../shared/FeatureState.vue'
import { dateTime } from '../shared/format'
import { t } from '../shared/i18n'
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
const hidden = ref(false)
let hideTimer: ReturnType<typeof setTimeout> | undefined
let slideTimer: ReturnType<typeof setTimeout> | undefined
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
const root = ref<HTMLElement | null>(null)
const loading = ref(false)
const loadError = ref('')
const adjusting = ref(false)
const layer = useLayer()
let previousFocus: Element | null = null
let detailRequest = 0

watch(
  () => src.value.id,
  async (id, _old, onCleanup) => {
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

function activity() {
  hidden.value = false
  clearTimeout(hideTimer)
  if (viewing.value.autoHide)
    hideTimer = setTimeout(() => {
      if (layer.owns()) hidden.value = true
    }, 3000)
}

watch([() => src.value.id, () => viewing.value.filmstrip, comparing], loadNeighbours, { immediate: true })

async function loadNeighbours() {
  const request = ++neighbourhoodRequest
  neighbours.value = []
  if (!viewing.value.filmstrip && !comparing.value) return
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
watch(() => viewing.value.autoHide, activity)

watch([sliding, () => src.value.id, () => viewing.value.slideshowSeconds], () => {
  clearTimeout(slideTimer)
  if (!sliding.value) return
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
          if (viewing.value.loop && src.value.first) await src.value.first()
          else sliding.value = false
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
  if (e.defaultPrevented || e.ctrlKey || e.altKey || e.metaKey) return
  if (!layer.owns()) return
  // Escape belongs to the browser while the player is fullscreen.
  if (e.key === 'Escape' && document.fullscreenElement) return
  if (e.key === 'Tab') {
    trapTab(e)
    return
  }
  // Typing in the details panel (adding a tag): only Escape still closes.
  if (isTyping(e) && e.key !== 'Escape') return
  if (e.key === '+') {
    e.preventDefault()
    void runCommand('viewer.zoomIn')
    return
  }
  // Playback controls keep their own arrow keys.
  const slider = e.target instanceof Element && e.target.closest('[role="slider"], [role="separator"]')
  if ((ownsArrows(e) || slider) && (e.key === 'ArrowLeft' || e.key === 'ArrowRight')) return
  // These viewer shortcuts ignored Shift before the command registry did the matching.
  if (e.shiftKey && (/^[fi]$/i.test(e.key) || ['Escape', 'ArrowLeft', 'ArrowRight'].includes(e.key))) {
    const plain = new KeyboardEvent('keydown', { key: e.key.toLowerCase() })
    if (dispatchKey(plain, 'viewer')) e.preventDefault()
    return
  }
  dispatchKey(e, 'viewer')
}

useCommands([
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
    icon: 'play',
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
    icon: 'undo',
    zone: 'viewer',
    available: () => !!image.value && ordinary.value && !adjusting.value,
    run: () => adjust(0, false),
  },
  {
    id: 'viewer.zoomIn',
    group: 'view',
    title: () => t('viewer.zoomIn'),
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
    icon: 'chevron-left',
    keys: 'ArrowLeft',
    zone: 'viewer',
    run: () => go(-1),
  },
  {
    id: 'viewer.next',
    group: 'view',
    title: () => t('viewer.next'),
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
  const controls = (document.fullscreenElement ?? root.value)?.querySelectorAll<HTMLElement>(
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
  setBehind(true)
  root.value?.focus()
  activity()
})
onBeforeUnmount(() => {
  detailRequest++
  comparisonRequest++
  neighbourhoodRequest++
  clearTimeout(slideTimer)
  clearTimeout(hideTimer)
  window.removeEventListener('keydown', onKey)
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
      :aria-label="detail ? dateTime(detail.takenAt) : t('viewer.title')"
      tabindex="-1"
      @pointermove="activity"
      @focusin="activity"
    >
      <div class="stage">
        <header class="bar">
          <IconButton
            :kbds="['Esc']"
            class="viewer-control"
            :icon="icons.close"
            :label="t('common.close')"
            variant="ghost"
            @click="runCommand('viewer.close')"
          />
          <span class="date">{{ detail ? dateTime(detail.takenAt) : '' }}</span>
          <span v-if="loading" class="date" role="status">{{ t('details.loading') }}</span>
          <IconButton
            v-if="sliding && !image"
            :icon="icons.pause"
            :label="t('viewer.slideshow')"
            :tip="t('viewer.stopSlideshow')"
            :aria-pressed="sliding"
            variant="ghost"
            @click="runCommand('viewer.slideshow')"
          />
          <div class="grow" />
          <!-- In the Trash the two actions are spelt out: what each does to
               the file must be read without hovering. -->
          <UButton
            v-if="actions.restore"
            class="viewer-control"
            :icon="icons.restore"
            variant="ghost"
            @click="runCommand('viewer.restore')"
          >
            {{ t('trash.restore') }}
          </UButton>
          <UButton
            v-if="actions.deleteFromDisk"
            class="viewer-control"
            :icon="icons.recycle"
            variant="ghost"
            @click="runCommand('viewer.delete')"
          >
            {{ t('trash.deleteFromDisk') }}
          </UButton>
          <IconButton
            v-if="actions.favourite"
            class="viewer-control"
            :label="t(item?.f ? 'details.unfavourite' : 'details.favourite')"
            :kbds="['F']"
            :aria-pressed="!!item?.f"
            variant="ghost"
            @click="runCommand('viewer.favourite')"
          >
            <Icon name="heart" :filled="!!item?.f" />
          </IconButton>
          <IconButton
            v-if="actions.moveToTrash"
            class="viewer-control"
            :icon="icons.trash"
            :label="t('trash.move')"
            variant="ghost"
            @click="runCommand('viewer.trash')"
          />
          <IconButton
            v-if="actions.details"
            class="viewer-control"
            :icon="icons.info"
            :label="t('library.info')"
            :tip="t(lib.showDetails ? 'library.hideInfo' : 'library.showInfo')"
            :kbds="['I']"
            :aria-pressed="lib.showDetails"
            variant="ghost"
            @click="runCommand('viewer.details')"
          />
        </header>

        <div
          class="media"
          :class="{ comparing: comparison && comparisonItem, 'with-filmstrip': viewing.filmstrip && neighbours.length }"
        >
          <FeatureState v-if="loadError" kind="error" :title="loadError" />
          <FeatureState v-else-if="!item && !loading" kind="empty" icon="image-off" :title="t('viewer.notFound')" />
          <template v-else-if="item">
            <VideoPlayer
              v-if="detail?.video && detail.id === item.id && videosAvailable()"
              :key="detail.id"
              :detail="detail"
              :at="item.at ?? 0"
              :thumb-version="item.t"
              :private="!ordinary"
              @timeupdate="src.touch?.()"
            />
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
                @change="geometry = $event"
              />
              <FeatureState
                v-if="!item.t && !detail?.caps.original"
                :kind="loading ? 'loading' : 'empty'"
                icon="image-off"
                :title="loading ? t('details.loading') : t('viewer.noPreview')"
              />
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
            @change="geometry = $event"
          />
        </div>

        <div
          v-if="image && !detail?.video"
          class="image-controls"
          :class="{ raised: viewing.filmstrip && neighbours.length }"
        >
          <IconButton
            :icon="icons['zoom-out']"
            :label="t('viewer.zoomOut')"
            variant="ghost"
            @click="runCommand('viewer.zoomOut')"
          />
          <span role="status">{{ image.scale }}%</span>
          <IconButton
            :icon="icons['zoom-in']"
            :label="t('viewer.zoomIn')"
            variant="ghost"
            @click="runCommand('viewer.zoomIn')"
          />
          <UButton variant="ghost" @click="runCommand('viewer.fit')">{{ t('viewer.fit') }}</UButton>
          <UButton variant="ghost" @click="runCommand('viewer.actual')">{{ t('viewer.actual') }}</UButton>
          <IconButton
            :icon="icons.maximize"
            :label="t('viewer.fullscreen')"
            variant="ghost"
            @click="runCommand('viewer.fullscreen')"
          />
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
            :icon="icons[sliding ? 'pause' : 'play']"
            :label="t('viewer.slideshow')"
            :tip="t(sliding ? 'viewer.stopSlideshow' : 'viewer.startSlideshow')"
            :aria-pressed="sliding"
            variant="ghost"
            @click="runCommand('viewer.slideshow')"
          />
          <IconButton
            v-if="src.window"
            :icon="icons.layers"
            :label="t('viewer.compare')"
            :tip="t(comparing ? 'viewer.stopComparing' : 'viewer.compare')"
            :aria-pressed="comparing"
            variant="ghost"
            @click="runCommand('viewer.compare')"
          />
          <UButton v-if="comparing" :aria-pressed="linked" variant="ghost" @click="linked = !linked">{{
            t('viewer.linked')
          }}</UButton>
          <IconButton
            v-if="ordinary"
            :icon="icons.rotate"
            :label="t('viewer.rotate')"
            :disabled="adjusting"
            variant="ghost"
            @click="runCommand('viewer.rotate')"
          />
          <IconButton
            v-if="ordinary"
            :icon="icons.flip"
            :label="t('viewer.flip')"
            :disabled="adjusting"
            variant="ghost"
            @click="runCommand('viewer.flip')"
          />
          <IconButton
            v-if="ordinary"
            :icon="icons.undo"
            :label="t('viewer.resetAdjustment')"
            :disabled="adjusting"
            variant="ghost"
            @click="runCommand('viewer.resetAdjustment')"
          />
        </div>
        <Filmstrip
          v-if="viewing.filmstrip && neighbours.length && item"
          :items="neighbours"
          :selected="item.id"
          @select="src.jump?.($event)"
        />

        <IconButton
          side="right"
          class="viewer-control nav prev"
          :icon="icons['chevron-left']"
          :label="t('viewer.prev')"
          variant="ghost"
          @click="runCommand('viewer.previous')"
        />
        <IconButton
          side="left"
          class="viewer-control nav next"
          :icon="icons['chevron-right']"
          :label="t('viewer.next')"
          variant="ghost"
          @click="runCommand('viewer.next')"
        />
      </div>
      <DetailsPanel v-if="lib.showDetails && actions.details" :sel="src.sel" />
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
  --lumen-filmstrip-h: 84px;
  /* Shared states and playback alerts keep the viewer's contrast in either theme. */
  --ui-bg: var(--lumen-viewer-bg);
  --ui-bg-muted: var(--lumen-viewer-control);
  --ui-bg-elevated: var(--lumen-viewer-control);
  --ui-text: var(--lumen-viewer-text);
  --ui-text-highlighted: var(--lumen-viewer-text);
  --ui-text-muted: var(--lumen-viewer-text-muted);
  --ui-text-dimmed: var(--lumen-viewer-text-muted);
}

.stage {
  position: relative;
  flex: 1;
  min-width: 0;
  container-type: inline-size;
}

.bar {
  position: absolute;
  inset: 0 0 auto;
  z-index: 2;
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 0.25rem;
  min-height: var(--lumen-topbar-h);
  padding: 0.375rem 0.75rem;
  background: var(--lumen-viewer-bg);
}

.viewer-control {
  flex-shrink: 0;
  color: var(--lumen-viewer-text);
}

.viewer-control:hover,
.viewer-control[aria-pressed='true'] {
  background: var(--lumen-viewer-control);
  color: var(--lumen-viewer-text);
}

.date {
  min-width: 0;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
  font-size: 0.8125rem;
  color: var(--lumen-viewer-text-muted);
}

.grow {
  flex: 1;
}

.media {
  position: absolute;
  inset: var(--lumen-topbar-h) 0 0;
  display: grid;
  /* Bounded tracks stop intrinsic image sizes expanding the stage. */
  grid-template-columns: minmax(0, 1fr);
  grid-template-rows: minmax(0, 1fr);
  place-items: center;
}

.image-controls {
  position: absolute;
  bottom: 0.75rem;
  left: 50%;
  transform: translateX(-50%);
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  justify-content: center;
  gap: 0.25rem;
  width: max-content;
  max-width: calc(100% - 1rem);
  background: var(--lumen-viewer-bg);
  border-radius: var(--ui-radius);
}
.image-controls.raised {
  bottom: calc(var(--lumen-filmstrip-h) + 0.375rem);
}
.media.with-filmstrip {
  bottom: var(--lumen-filmstrip-h);
}
.media.comparing {
  grid-template-columns: repeat(2, minmax(0, 1fr));
}
.controls-hidden .bar,
.controls-hidden .image-controls,
.controls-hidden .nav {
  opacity: 0;
  pointer-events: none;
}
.controls-hidden .bar:focus-within,
.controls-hidden .image-controls:focus-within,
.controls-hidden .nav:focus-visible {
  opacity: 1;
  pointer-events: auto;
}

.media img,
.media video {
  grid-area: 1 / 1;
  max-width: 100%;
  max-height: 100%;
  object-fit: contain;
  width: 100%;
  height: 100%;
}

.playback-message {
  position: absolute;
  z-index: 3;
  max-width: min(28rem, calc(100% - 2rem));
}

.nav {
  position: absolute;
  top: 50%;
  transform: translateY(-50%);
  width: 3rem;
  height: 4.5rem;
  background: var(--lumen-viewer-control);
  opacity: 0;
  transition: opacity var(--lumen-motion-control) var(--lumen-ease);
}

.stage:hover .nav,
.nav:focus-visible {
  opacity: 1;
}

.prev {
  left: 0.75rem;
}

.next {
  right: 0.75rem;
}

@container (max-width: 30rem) {
  .date {
    flex: 1 1 8rem;
  }

  .grow {
    display: none;
  }
}

@media (pointer: coarse), (forced-colors: active) {
  .nav {
    opacity: 1;
  }
}

@media (forced-colors: active) {
  .viewer-control {
    border: 1px solid ButtonText;
  }
}
</style>
