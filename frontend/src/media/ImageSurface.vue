<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { engine, isCancelled, originalUrl, RENDER_SIDE, renderUrl, THUMB_LARGE, thumbUrl } from '../bridge'
import type { AssetDetail, RenderInfo } from '../bridge'
import { errorText } from '../shared/errors'
import { t } from '../shared/i18n'
import { observeResize } from '../shared/observeResize'
import { constrain, fitScale, zoomAt } from './viewport'
import type { ViewGeometry, Viewport } from './viewport'

const props = defineProps<{
  id: number
  thumb: number
  width: number
  height: number
  detail: AssetDetail | null
  linked?: ViewGeometry
}>()
const emit = defineEmits<{ change: [value: ViewGeometry] }>()
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
const fitting = ref(true)
const loadedSource = ref('')
const info = ref<RenderInfo | null>(null)
const originalFailed = ref(false)
const failed = ref('')
// The details are asked for after the picture is shown, and until they
// arrive may still be the last picture's.
const detail = computed(() => (props.detail?.id === props.id ? props.detail : null))
const rendered = computed(() => (info.value ? renderUrl(props.id, info.value.revision) : undefined))
// The WebView decodes an original no larger than a render would be; a
// bigger one is shown through the bounded render instead.
const original = computed(() =>
  !originalFailed.value && detail.value?.caps.original && props.width * props.height <= RENDER_SIDE * RENDER_SIDE
    ? originalUrl(props.id)
    : undefined,
)
const full = computed(() => rendered.value ?? original.value)
const ready = computed(() => !!full.value && loadedSource.value === full.value)
const scale = computed(() => Math.round(viewport.value.scale * 100))
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
  fitting.value = true
  set({ ...viewport.value, scale: fitScale(viewport.value), x: 0, y: 0 })
}
function actual() {
  fitting.value = false
  set(zoomAt(viewport.value, pixelScale()))
}
function pixelScale() {
  return info.value ? info.value.width / Math.max(1, detail.value?.width ?? props.width) : 1
}
async function renderCopy(current: number) {
  try {
    const result = await engine.render(props.id, abort?.signal)
    if (request === current) info.value = result
  } catch (e) {
    if (request === current && !isCancelled(e)) failed.value = errorText(e)
  }
}
function loadFailed() {
  loadedSource.value = ''
  if (original.value) {
    originalFailed.value = true
    void renderCopy(request)
  } else failed.value = t('viewer.renderFailed')
}
function zoom(factor: number, x?: number, y?: number) {
  fitting.value = false
  set(zoomAt(viewport.value, viewport.value.scale * factor, x, y))
}
function wheel(e: WheelEvent) {
  e.preventDefault()
  const bounds = surface.value?.getBoundingClientRect()
  if (bounds)
    zoom(Math.exp(-Math.max(-100, Math.min(100, e.deltaY)) * 0.005), e.clientX - bounds.left, e.clientY - bounds.top)
}
function pointerDown(e: PointerEvent) {
  if (e.button !== 0) return
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
    failed.value = ''
    const rotated = (detail.value?.adjustment?.rotation ?? 0) % 2 !== 0
    const width = detail.value?.width ?? props.width
    const height = detail.value?.height ?? props.height
    viewport.value = { ...viewport.value, imageWidth: rotated ? height : width, imageHeight: rotated ? width : height }
    if (fitting.value) fit()
    else set(viewport.value)
    if (!detail.value || detail.value.video || original.value) return
    await renderCopy(current)
  },
  { immediate: true },
)

onMounted(() => {
  stopResize = observeResize(surface.value ? [surface.value] : [], () => {
    if (!surface.value) return
    viewport.value = { ...viewport.value, width: surface.value.clientWidth, height: surface.value.clientHeight }
    if (fitting.value) fit()
    else set(viewport.value)
  })
})
onBeforeUnmount(() => {
  request++
  abort?.abort()
  stopResize?.()
  pointers.clear()
})
defineExpose({ fit, actual, zoom, scale })
</script>

<template>
  <div
    ref="surface"
    class="image-surface"
    :aria-label="t('viewer.image')"
    @wheel="wheel"
    @pointerdown="pointerDown"
    @pointermove="pointerMove"
    @pointerup="pointerEnd"
    @pointercancel="pointerEnd"
    @lostpointercapture="pointerEnd"
    @dblclick.stop="doubleClick"
  >
    <img
      v-if="thumb"
      v-show="!ready"
      class="preview"
      :src="thumbUrl(id, thumb, THUMB_LARGE)"
      :style="previewStyle"
      alt=""
      draggable="false"
    />
    <img
      v-if="full"
      v-show="ready"
      :key="full"
      class="original"
      :src="full"
      :style="style"
      alt=""
      draggable="false"
      @load="loadedSource = full ?? ''"
      @error="loadFailed"
    />
    <span v-if="info?.limited" class="resolution" role="status">{{
      t('viewer.resolution', { w: info.width, h: info.height })
    }}</span>
    <span v-if="failed" class="resolution" role="status">{{ failed }}</span>
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
.resolution {
  position: absolute;
  top: 0.5rem;
  left: 50%;
  transform: translateX(-50%);
  max-width: calc(100% - 1rem);
  text-align: center;
  pointer-events: none;
  padding: 0.5rem;
  background: var(--lumen-viewer-bg);
  color: var(--lumen-viewer-text);
  font-size: 0.8125rem;
}
</style>
