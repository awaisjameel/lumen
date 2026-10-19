<script setup lang="ts">
import UButton from '@nuxt/ui/components/Button.vue'
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import {
  engine,
  isCancelled,
  originalUrl,
  RENDER_SIDE,
  renderUrl,
  THUMB_LARGE,
  thumbUrl,
  ViewerOpen,
  ViewerWheel,
} from '../bridge'
import type { AssetDetail, RenderInfo } from '../bridge'
import { errorText, logError } from '../shared/errors'
import { t } from '../shared/i18n'
import { observeResize } from '../shared/observeResize'
import { constrain, fitScale, zoomAt } from './viewport'
import type { ViewGeometry, Viewport } from './viewport'

const props = withDefaults(
  defineProps<{
    id: number
    thumb: number
    width: number
    height: number
    detail: AssetDetail | null
    linked?: ViewGeometry
    /** How the picture is first shown: fitted, or at its actual size (the viewing preference). */
    open?: ViewerOpen
    /** What the wheel does: zoom, or step to the next picture (Ctrl and the wheel then zoom). */
    wheel?: ViewerWheel
    /** The longest edge asked of a viewing copy. */
    side?: number
    /** Hold an animated image on its first frame until it is played. */
    still?: boolean
  }>(),
  {
    linked: undefined,
    open: ViewerOpen.ViewerOpenFit,
    wheel: ViewerWheel.ViewerWheelZoom,
    side: RENDER_SIDE,
    still: false,
  },
)
const emit = defineEmits<{ change: [value: ViewGeometry]; step: [dir: 1 | -1] }>()
const surface = ref<HTMLElement | null>(null)
const viewport = ref<Viewport>({
  width: 1,
  height: 1,
  imageWidth: Math.max(1, props.width),
  imageHeight: Math.max(1, props.height),
  scale: 1,
  x: 0,
  y: 0,
})
const fitting = ref(props.open !== ViewerOpen.ViewerOpenActual)
// Opened at its actual size, the picture stays so (through the window's
// first measure and the viewing copy's arrival) until the user zooms or pans.
let openActual = !fitting.value
// Whether the file animates is only known by reading it. With animations
// held still, the original waits for that answer; a still one then shows as
// ever, and an animated one shows its first frame until it is played.
const inspected = ref(!props.still)
const animated = ref(false)
const playing = ref(false)
const loadedSource = ref('')
const info = ref<RenderInfo | null>(null)
const originalFailed = ref(false)
const previewFailed = ref(false)
const previewLoaded = ref(false)
const failed = ref('')
// A file that holds several pages (a TIFF) is turned through with the
// viewer's controls; the grid, the details and the models know its first.
const page = ref(0)
const pages = ref(1)
// The details are asked for after the picture is shown, and until they
// arrive may still be the last picture's.
const detail = computed(() => (props.detail?.id === props.id ? props.detail : null))
const rendered = computed(() =>
  info.value ? renderUrl(props.id, info.value.revision, info.value.page, props.side) : undefined,
)
// The WebView decodes an original no larger than a render would be; a
// bigger one is shown through the bounded render instead.
const original = computed(() =>
  !originalFailed.value &&
  detail.value?.caps.original &&
  props.width * props.height <= RENDER_SIDE * RENDER_SIDE &&
  inspected.value &&
  (!animated.value || playing.value)
    ? originalUrl(props.id)
    : undefined,
)
// Only the original animates: while it plays it is shown, not the still copy.
const full = computed(() => (playing.value && original.value ? original.value : (rendered.value ?? original.value)))
const ready = computed(() => !!full.value && loadedSource.value === full.value)
const scale = computed(() => Math.round(viewport.value.scale * 100))
const canZoomIn = computed(() => viewport.value.scale < 8)
const canZoomOut = computed(() => viewport.value.scale > Math.min(fitScale(viewport.value), 0.1))
const placed = computed(
  () => `translate(-50%, -50%) translate(${viewport.value.x}px, ${viewport.value.y}px) scale(${viewport.value.scale})`,
)
// The full picture is the stored pixels, turned here as the user turned it.
const style = computed(() => ({
  width: `${detail.value?.width ?? props.width}px`,
  height: `${detail.value?.height ?? props.height}px`,
  transform: `${placed.value} scaleX(${detail.value?.adjustment?.flip ? -1 : 1}) rotate(${(detail.value?.adjustment?.rotation ?? 0) * 90}deg)`,
}))
// The preview is the thumbnail the grid shows, which is already turned: it
// fills the picture's place as it is, with or without the details.
const previewStyle = computed(() => ({
  width: `${viewport.value.imageWidth}px`,
  height: `${viewport.value.imageHeight}px`,
  transform: placed.value,
}))
let stopResize: (() => void) | undefined
let request = 0
let abort: AbortController | undefined
const pointers = new Map<number, { x: number; y: number }>()

function set(v: Viewport) {
  viewport.value = constrain(v)
  emit('change', {
    scale: viewport.value.scale,
    x: viewport.value.x / Math.max(1, viewport.value.imageWidth),
    y: viewport.value.y / Math.max(1, viewport.value.imageHeight),
  })
}
function fit() {
  openActual = false
  fitting.value = true
  set({ ...viewport.value, scale: fitScale(viewport.value), x: 0, y: 0 })
}
function actual() {
  openActual = false
  fitting.value = false
  set(zoomAt(viewport.value, pixelScale()))
}
/** Places the picture again after its size or the window's changed. */
function place() {
  if (fitting.value) set({ ...viewport.value, scale: fitScale(viewport.value), x: 0, y: 0 })
  else if (openActual) set(zoomAt({ ...viewport.value, x: 0, y: 0 }, pixelScale()))
  else set(viewport.value)
}
function pixelScale() {
  return info.value ? info.value.width / Math.max(1, detail.value?.width ?? props.width) : 1
}
async function renderCopy(current: number) {
  try {
    const result = await engine.render(props.id, abort?.signal, page.value, props.side)
    if (request === current) {
      info.value = result
      if (openActual) place() // one image pixel is now one of the copy's
    }
  } catch (e) {
    if (request === current && !isCancelled(e)) failed.value = errorText(e)
  }
}
async function countPages(current: number) {
  try {
    const inspection = await engine.inspect(props.id, abort?.signal)
    if (request === current) pages.value = Math.max(1, inspection.pages)
  } catch (e) {
    if (!isCancelled(e)) logError(e) // the first page is still shown
  }
}
async function checkAnimation(current: number) {
  try {
    const inspection = await engine.inspect(props.id, abort?.signal)
    if (request !== current) return
    animated.value = inspection.pages > 1
  } catch (e) {
    if (isCancelled(e)) return
    logError(e) // unknown: shown as a still picture is
  }
  if (request === current) inspected.value = true
}
function toggleAnimation() {
  playing.value = !playing.value
}
function turnPage(by: number) {
  const next = Math.min(pages.value - 1, Math.max(0, page.value + by))
  if (next === page.value) return
  page.value = next
  const current = ++request
  abort?.abort()
  abort = new AbortController()
  failed.value = ''
  void renderCopy(current)
}
function retryCopy() {
  abort?.abort()
  abort = new AbortController()
  failed.value = ''
  void renderCopy(++request)
}
function loadFailed() {
  loadedSource.value = ''
  if (original.value) {
    originalFailed.value = true
    void renderCopy(request)
  } else failed.value = t('viewer.renderFailed')
}
function zoom(factor: number, x?: number, y?: number) {
  openActual = false
  fitting.value = false
  set(zoomAt(viewport.value, viewport.value.scale * factor, x, y))
}
let lastStep = 0
function onWheel(e: WheelEvent) {
  e.preventDefault()
  if (props.wheel === ViewerWheel.ViewerWheelNavigate && !e.ctrlKey) {
    // One notch is one picture: a free-spinning wheel must not race through the library.
    if (!e.deltaY || e.timeStamp - lastStep < 200) return
    lastStep = e.timeStamp
    emit('step', e.deltaY > 0 ? 1 : -1)
    return
  }
  const bounds = surface.value?.getBoundingClientRect()
  if (bounds)
    zoom(Math.exp(-Math.max(-100, Math.min(100, e.deltaY)) * 0.005), e.clientX - bounds.left, e.clientY - bounds.top)
}
function pointerDown(e: PointerEvent) {
  if (e.button !== 0) return
  if (e.target instanceof Element && e.target.closest('button')) return
  surface.value?.setPointerCapture(e.pointerId)
  pointers.set(e.pointerId, { x: e.clientX, y: e.clientY })
}
function pointerMove(e: PointerEvent) {
  const previous = pointers.get(e.pointerId)
  if (!previous) return
  const other = [...pointers].find(([id]) => id !== e.pointerId)?.[1]
  pointers.set(e.pointerId, { x: e.clientX, y: e.clientY })
  if (other) {
    const oldDistance = Math.hypot(previous.x - other.x, previous.y - other.y)
    const distance = Math.hypot(e.clientX - other.x, e.clientY - other.y)
    const bounds = surface.value?.getBoundingClientRect()
    if (oldDistance > 0 && bounds)
      zoom(distance / oldDistance, (e.clientX + other.x) / 2 - bounds.left, (e.clientY + other.y) / 2 - bounds.top)
  } else {
    openActual = false
    set({
      ...viewport.value,
      x: viewport.value.x + e.clientX - previous.x,
      y: viewport.value.y + e.clientY - previous.y,
    })
  }
}
function pointerEnd(e: PointerEvent) {
  pointers.delete(e.pointerId)
}
function doubleClick(e: MouseEvent) {
  if (!fitting.value) {
    fit()
    return
  }
  const bounds = surface.value?.getBoundingClientRect()
  openActual = false
  fitting.value = false
  if (bounds) set(zoomAt(viewport.value, pixelScale(), e.clientX - bounds.left, e.clientY - bounds.top))
}

watch(
  () => props.linked,
  (value) => {
    if (!value) return
    if (
      Math.abs(value.scale - viewport.value.scale) < 0.000001 &&
      Math.abs(value.x * viewport.value.imageWidth - viewport.value.x) < 0.000001 &&
      Math.abs(value.y * viewport.value.imageHeight - viewport.value.y) < 0.000001
    )
      return
    openActual = false
    fitting.value = false
    viewport.value = constrain({
      ...viewport.value,
      scale: value.scale,
      x: value.x * viewport.value.imageWidth,
      y: value.y * viewport.value.imageHeight,
    })
  },
)
watch(
  [() => props.id, () => props.thumb, () => props.detail],
  async () => {
    const current = ++request
    abort?.abort()
    abort = new AbortController()
    info.value = null
    originalFailed.value = false
    previewFailed.value = false
    previewLoaded.value = false
    failed.value = ''
    page.value = 0
    pages.value = 1
    const rotated = (detail.value?.adjustment?.rotation ?? 0) % 2 !== 0
    const width = detail.value?.width ?? props.width
    const height = detail.value?.height ?? props.height
    viewport.value = { ...viewport.value, imageWidth: rotated ? height : width, imageHeight: rotated ? width : height }
    place()
    if (!detail.value || detail.value.video) return
    if (!inspected.value && detail.value.caps.original && !detail.value.caps.paged) await checkAnimation(current)
    else inspected.value = true
    if (request !== current || original.value) return
    if (detail.value.caps.paged) void countPages(current)
    await renderCopy(current)
  },
  { immediate: true },
)

onMounted(() => {
  stopResize = observeResize(surface.value ? [surface.value] : [], () => {
    if (!surface.value) return
    viewport.value = { ...viewport.value, width: surface.value.clientWidth, height: surface.value.clientHeight }
    place()
  })
})
onBeforeUnmount(() => {
  request++
  abort?.abort()
  stopResize?.()
  pointers.clear()
})
defineExpose({
  fit,
  actual,
  zoom,
  scale,
  canZoomIn,
  canZoomOut,
  page,
  pages,
  turnPage,
  animated,
  playing,
  toggleAnimation,
})
</script>

<template>
  <div
    ref="surface"
    class="image-surface"
    :aria-label="t('viewer.image')"
    @wheel="onWheel"
    @pointerdown="pointerDown"
    @pointermove="pointerMove"
    @pointerup="pointerEnd"
    @pointercancel="pointerEnd"
    @lostpointercapture="pointerEnd"
    @dblclick.stop="doubleClick"
  >
    <img
      v-if="thumb && !previewFailed"
      v-show="!ready"
      class="preview"
      :src="thumbUrl(id, thumb, THUMB_LARGE)"
      :style="previewStyle"
      alt=""
      draggable="false"
      @load="previewLoaded = true"
      @error="previewFailed = true"
    />
    <img
      v-if="full"
      v-show="ready"
      :key="full"
      class="original"
      :class="{ page: page > 0 }"
      :src="full"
      :style="style"
      alt=""
      draggable="false"
      @load="loadedSource = full ?? ''"
      @error="loadFailed"
    />
    <span v-if="info?.preview" class="resolution" role="status">{{
      t('viewer.embeddedPreview', { w: info.width, h: info.height })
    }}</span>
    <span v-else-if="info?.limited" class="resolution" role="status">{{
      t('viewer.resolution', { w: info.width, h: info.height })
    }}</span>
    <div v-if="failed" class="resolution failure" role="alert">
      <span>{{ thumb && !previewFailed ? t('viewer.renderFailed') : t('viewer.loadFailed') }}</span>
      <span v-if="!thumb || previewFailed">{{ t('video.failureHint') }}</span>
      <UButton variant="ghost" @click.stop="retryCopy">{{ t('common.retry') }}</UButton>
    </div>
    <div v-else-if="!ready && !previewLoaded && !previewFailed && (thumb || full)" class="loading-image" role="status">
      <div class="lumen-skeleton" />
      {{ t('details.loading') }}
    </div>
  </div>
</template>

<style scoped>
.image-surface {
  position: relative;
  width: 100%;
  height: 100%;
  overflow: hidden;
  touch-action: none;
  cursor: grab;
}
.image-surface:active {
  cursor: grabbing;
}
img {
  position: absolute;
  left: 50%;
  top: 50%;
  max-width: none;
  max-height: none;
  object-fit: fill;
  transform-origin: center;
  user-select: none;
}
/* A thumbnail made before the last turn keeps its shape until the new one arrives. */
.preview {
  object-fit: contain;
}
/* A later page may have another shape than the first, whose place it takes. */
.original.page {
  object-fit: contain;
}
.failure {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: center;
  gap: 0.5rem;
}
.loading-image {
  position: absolute;
  inset: 1rem;
  display: grid;
  grid-template-rows: minmax(0, 1fr) auto;
  gap: 0.75rem;
  text-align: center;
  color: var(--lumen-viewer-text-muted);
}
.resolution {
  position: absolute;
  top: 0.5rem;
  left: 50%;
  transform: translateX(-50%);
  max-width: calc(100% - 1rem);
  text-align: center;
  pointer-events: auto;
  padding: 0.5rem;
  background: var(--lumen-viewer-glass);
  border-radius: var(--lumen-r-md);
  color: var(--lumen-viewer-text);
  font-size: 0.8125rem;
}
</style>
