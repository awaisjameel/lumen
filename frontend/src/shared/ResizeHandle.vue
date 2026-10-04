<script setup lang="ts">
// The edge a panel is resized by (the sidebar, the inspector): dragged with
// the pointer, or moved with the arrow keys when focused. It reports the
// width the panel should have; the owner stores it.
import { ref } from 'vue'

const props = defineProps<{
  /** The panel's width now, in CSS px. */
  width: number
  min: number
  max: number
  /** Which edge of the panel the handle is on. */
  edge: 'left' | 'right'
  label: string
}>()
const emit = defineEmits<{ resize: [width: number] }>()

const STEP = 16
const dragging = ref(false)
let startX = 0
let startWidth = 0

function clamp(w: number): number {
  return Math.round(Math.min(props.max, Math.max(props.min, w)))
}

// A panel on the left grows as its right edge moves right; one on the right
// grows as its left edge moves left.
function grow(dx: number): number {
  return props.edge === 'right' ? dx : -dx
}

function onDown(e: PointerEvent) {
  if (!(e.currentTarget instanceof HTMLElement)) return
  e.currentTarget.setPointerCapture(e.pointerId)
  dragging.value = true
  startX = e.clientX
  startWidth = props.width
}

function onMove(e: PointerEvent) {
  if (!dragging.value) return
  const w = clamp(startWidth + grow(e.clientX - startX))
  if (w !== props.width) emit('resize', w)
}

function onUp() {
  dragging.value = false
}

function onKey(e: KeyboardEvent) {
  const dx = e.key === 'ArrowRight' ? STEP : e.key === 'ArrowLeft' ? -STEP : 0
  if (!dx) return
  e.preventDefault()
  const w = clamp(props.width + grow(dx))
  if (w !== props.width) emit('resize', w)
}
</script>

<template>
  <div
    class="resize-handle"
    :class="[edge, { dragging }]"
    role="separator"
    aria-orientation="vertical"
    tabindex="0"
    :aria-label="label"
    :aria-valuenow="width"
    :aria-valuemin="min"
    :aria-valuemax="max"
    @pointerdown="onDown"
    @pointermove="onMove"
    @pointerup="onUp"
    @pointercancel="onUp"
    @keydown="onKey"
  />
</template>

<style scoped>
.resize-handle {
  position: absolute;
  top: 0;
  bottom: 0;
  z-index: var(--lumen-z-sticky);
  width: 7px;
  cursor: col-resize;
  touch-action: none;
}
.resize-handle.right {
  right: -4px;
}
.resize-handle.left {
  left: -4px;
}
.resize-handle::after {
  content: '';
  position: absolute;
  top: 0;
  bottom: 0;
  left: 3px;
  width: 1px;
  background: transparent;
  transition: background var(--lumen-motion-control) var(--lumen-ease);
}
.resize-handle:hover::after,
.resize-handle:focus-visible::after,
.resize-handle.dragging::after {
  width: 2px;
  left: 2px;
  background: var(--ui-primary);
}
.resize-handle:focus-visible {
  outline: none;
}
</style>
