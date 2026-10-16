<script setup lang="ts">
// Replaces the scrollbar: years and months placed where they sit in the
// timeline. Click or drag to jump; hover shows which month is there.
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import type { DayBucket } from '../bridge'
import { t } from '../shared/i18n'
import { monthLabel } from '../shared/format'
import { observeResize } from '../shared/observeResize'
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

const currentYear = computed(() => props.layout.rows[rowAt(props.layout, props.scrollTop)]?.day.slice(0, 4))

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
  if (trackH.value > 0) emit('seek', (y / trackH.value) * scrollRange.value)
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
let stopResize: (() => void) | undefined
onMounted(() => {
  stopResize = observeResize(track.value ? [track.value] : [], () => {
    if (track.value) trackH.value = track.value.clientHeight
  })
})
onBeforeUnmount(() => stopResize?.())
</script>

<template>
  <!-- Year buttons give keyboard users the same jumps as the pointer track. -->
  <div
    ref="track"
    class="scrubber"
    role="navigation"
    :aria-label="t('library.timeline')"
    @pointerdown="down"
    @pointermove="moveP"
    @pointerup="up"
    @pointercancel="up"
    @pointerleave="hoverY = null"
  >
    <template v-for="m in marks" :key="m.month">
      <button
        v-if="m.year"
        type="button"
        class="year"
        :class="{ 'is-current': m.label === currentYear }"
        :aria-label="t('library.jump', { year: m.label })"
        :aria-current="m.label === currentYear ? 'date' : undefined"
        :style="{ top: m.y + 'px' }"
        @pointerdown.stop
        @click.stop="seekTo(m.y)"
      >
        {{ m.label }}
      </button>
      <span v-else aria-hidden="true" class="dot" :style="{ top: m.y + 'px' }" />
    </template>
    <span v-if="hoverY !== null" class="hover" :style="{ top: hoverY + 'px' }">{{ hoverLabel }}</span>
  </div>
</template>

<style scoped>
.scrubber {
  position: absolute;
  top: 1rem;
  right: 0;
  bottom: 1rem;
  width: 56px;
  cursor: ns-resize;
  touch-action: none;
}
.year {
  position: absolute;
  right: 0.5rem;
  min-height: 1.5rem;
  min-width: 1.5rem;
  padding: 0 0.25rem;
  border: 0;
  border-radius: var(--lumen-r-sm);
  background: transparent;
  cursor: pointer;
  font-size: 0.6875rem;
  font-weight: 600;
  color: var(--ui-text-dimmed);
}
.year:hover {
  background: var(--lumen-bg-hover);
  color: var(--ui-text-highlighted);
}
.year.is-current {
  color: var(--ui-primary);
}
.year.is-current::before {
  content: '';
  position: absolute;
  right: calc(100% + 0.25rem);
  top: 50%;
  width: 0.75rem;
  height: 2px;
  background: var(--ui-primary);
  border-radius: var(--lumen-r-xs);
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
.hover {
  position: absolute;
  right: 48px;
  transform: translateY(-50%);
  padding: 4px 10px;
  border-radius: calc(var(--ui-radius) * 1.5);
  border: 1px solid var(--ui-border);
  background: var(--ui-bg);
  color: var(--ui-text-highlighted);
  box-shadow: var(--lumen-shadow-3);
  font-size: 0.75rem;
  font-weight: 600;
  white-space: nowrap;
  pointer-events: none;
}
</style>
