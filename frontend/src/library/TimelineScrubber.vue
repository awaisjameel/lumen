<script setup lang="ts">
// Replaces the scrollbar: years and months placed where they sit in the
// timeline. Click or drag to jump; hover shows which month is there.
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import type { DayBucket } from '../bridge'
import { monthLabel } from '../shared/format'
import { rowAt } from './layout'
import type { Layout } from './layout'

const props = defineProps<{ buckets: DayBucket[]; layout: Layout; scrollTop: number; viewportH: number }>()
const emit = defineEmits<{ seek: [y: number] }>()

const track = ref<HTMLElement | null>(null)
const trackH = ref(0)
const hoverY = ref<number | null>(null)
const dragging = ref(false)

const scrollRange = computed(() => Math.max(1, props.layout.total - props.viewportH))

interface Mark {
  month: string // YYYY-MM, unique per mark
  label: string
  y: number // px within the track
  year: boolean
}

// One mark per month (the newest day of each month starts it); years labelled
// only where there is room, so a 20-year library stays readable.
const marks = computed<Mark[]>(() => {
  const h = trackH.value
  if (!h) return []
  const out: Mark[] = []
  let lastMonth = ''
  let lastYear = ''
  let lastYearY = -Infinity
  for (const b of props.buckets) {
    const month = b.day.slice(0, 7)
    if (month === lastMonth) continue
    lastMonth = month
    const row = props.layout.rowOfDay.get(b.day) ?? 0
    const y = Math.min(h - 8, ((props.layout.offsets[row] ?? 0) / scrollRange.value) * h)
    const year = b.day.slice(0, 4)
    const isYear = year !== lastYear && y - lastYearY > 22
    if (year !== lastYear && isYear) lastYearY = y
    lastYear = year
    out.push({ month, label: isYear ? year : '', y, year: isYear })
  }
  return out
})

const thumbY = computed(() => Math.min(1, props.scrollTop / scrollRange.value) * trackH.value)

const hoverLabel = computed(() => {
  if (hoverY.value === null) return ''
  const y = (hoverY.value / trackH.value) * scrollRange.value
  const row = props.layout.rows[rowAt(props.layout, y)]
  return row ? monthLabel(row.day) : ''
})

function yOf(e: PointerEvent): number {
  if (!track.value) return 0
  const r = track.value.getBoundingClientRect()
  trackH.value = r.height
  return Math.max(0, Math.min(r.height, e.clientY - r.top))
}

function seekTo(y: number) {
  emit('seek', (y / trackH.value) * scrollRange.value)
}

function down(e: PointerEvent) {
  dragging.value = true
  track.value?.setPointerCapture(e.pointerId)
  seekTo(yOf(e))
}
function moveP(e: PointerEvent) {
  hoverY.value = yOf(e)
  if (dragging.value) seekTo(hoverY.value)
}
function up() {
  dragging.value = false
}

// Measure once mounted and whenever the window resizes.
const ro = new ResizeObserver(() => {
  if (track.value) trackH.value = track.value.clientHeight
})
onMounted(() => {
  if (track.value) {
    ro.observe(track.value)
    trackH.value = track.value.clientHeight
  }
})
onBeforeUnmount(() => ro.disconnect())
</script>

<template>
  <!-- For the pointer only: the grid itself scrolls with the keyboard. -->
  <div
    ref="track"
    class="scrubber"
    aria-hidden="true"
    @pointerdown="down"
    @pointermove="moveP"
    @pointerup="up"
    @pointercancel="up"
    @pointerleave="hoverY = null"
  >
    <template v-for="m in marks" :key="m.month">
      <span v-if="m.year" class="year" :style="{ top: m.y + 'px' }">{{ m.label }}</span>
      <span v-else class="dot" :style="{ top: m.y + 'px' }" />
    </template>
    <span class="thumb" :style="{ top: thumbY + 'px' }" />
    <span v-if="hoverY !== null" class="hover" :style="{ top: hoverY + 'px' }">{{ hoverLabel }}</span>
  </div>
</template>

<style scoped>
.scrubber {
  position: absolute;
  top: 8px;
  right: 0;
  bottom: 8px;
  width: 56px;
  cursor: ns-resize;
  touch-action: none;
}
.year {
  position: absolute;
  right: 12px;
  transform: translateY(-2px);
  font-size: 11px;
  font-weight: 600;
  color: var(--ui-text-dimmed);
  pointer-events: none;
}
.dot {
  position: absolute;
  right: 22px;
  width: 3px;
  height: 3px;
  border-radius: 50%;
  background: var(--ui-text-dimmed);
  opacity: 0.5;
  pointer-events: none;
}
.thumb {
  position: absolute;
  right: 8px;
  width: 28px;
  height: 2px;
  background: var(--ui-primary);
  border-radius: 1px;
  pointer-events: none;
}
.hover {
  position: absolute;
  right: 48px;
  transform: translateY(-50%);
  padding: 4px 10px;
  border-radius: calc(var(--ui-radius) * 1.5);
  border: 1px solid var(--ui-border);
  background: var(--ui-bg);
  color: var(--ui-text-highlighted);
  box-shadow: var(--lumen-shadow);
  font-size: 12px;
  font-weight: 600;
  white-space: nowrap;
  pointer-events: none;
}
</style>
