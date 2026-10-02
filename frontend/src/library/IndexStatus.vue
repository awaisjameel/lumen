<script setup lang="ts">
// The small, calm status card in the sidebar. The app is fully usable while
// indexing runs; this says what it is doing now and offers a pause. Hovering
// it, or focusing its button, lists everything at work and what comes next.
import { computed, onBeforeUnmount, onMounted, ref, useId } from 'vue'
import { fileName } from '../shared/format'
import { t } from '../shared/i18n'
import Icon from '../shared/Icon.vue'
import { isAI, lead, tasks } from './activity'
import type { Task } from './activity'
import { useLibrary } from './store'

const lib = useLibrary()
const p = computed(() => lib.progress)
const id = useId()

const list = computed(() => (p.value ? tasks(p.value) : []))
const next = computed(() => list.value.filter((task) => !task.now))
const groups = computed(() =>
  [
    { head: t('status.now'), tasks: list.value.filter((task) => task.now) },
    { head: t('status.next'), tasks: next.value },
  ].filter((g) => g.tasks.length),
)
// The stages wait while paused; a scan goes on (it is cheap).
const paused = computed(() => !!p.value?.paused && next.value.length > 0)
const onBattery = computed(() => !!p.value?.onBattery && next.value.some((task) => isAI(task.kind)))
// What the card is about: the work waiting while paused, else the lead.
const shown = computed(() => (paused.value ? next.value[0] : lead(list.value)))

const label = computed(() => {
  const task = shown.value
  if (paused.value) return t('status.paused')
  if (!task) return t('status.upToDate')
  if (!task.now && onBattery.value && isAI(task.kind)) return t('status.battery')
  return name(task)
})

const fraction = computed(() => {
  const total = p.value?.total ?? 0
  const task = shown.value
  if (!total || !task || task.kind === 'scan') return 0
  return Math.min(1, Math.max(0, (total - task.count) / total))
})

function name(task: Task): string {
  const folder = p.value?.scanPath
  if (task.kind === 'scan' && folder) return t('status.scanIn', { folder: fileName(folder) })
  return t(`status.${task.kind}`)
}

function count(task: Task | undefined): string {
  if (!task?.count) return ''
  return task.kind === 'scan' ? t('status.found', { n: task.count }) : t('status.left', { n: task.count })
}

function togglePause() {
  if (p.value) void lib.setPaused(!p.value.paused)
}

// Escape puts the list away until the pointer or focus comes back.
const dismissed = ref(false)
function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') dismissed.value = true
}
onMounted(() => window.addEventListener('keydown', onKey))
onBeforeUnmount(() => window.removeEventListener('keydown', onKey))
</script>

<template>
  <div v-if="p" class="status" @mouseenter="dismissed = false" @focusin="dismissed = false">
    <div class="line">
      <span class="label" role="status">{{ label }}</span>
      <button
        v-if="list.length || p.paused"
        class="icon-btn small"
        :title="p.paused ? t('status.resume') : t('status.pause')"
        :aria-label="p.paused ? t('status.resume') : t('status.pause')"
        :aria-describedby="list.length ? id : undefined"
        @click="togglePause"
      >
        <Icon :name="p.paused ? 'play' : 'pause'" :size="14" />
      </button>
    </div>
    <div v-if="list.length" class="bar" :class="{ indeterminate: shown?.kind === 'scan' }">
      <span :style="{ width: fraction * 100 + '%' }" />
    </div>
    <p v-if="count(shown)" class="sub">{{ count(shown) }}</p>
    <p v-else-if="!list.length && p.total" class="sub">{{ t('status.items', { n: p.total }) }}</p>
    <p v-if="p.ai.error" class="sub" :title="p.ai.error">{{ t('status.aiOff') }}</p>
    <p v-if="p.ai.facesError" class="sub" :title="p.ai.facesError">{{ t('status.peopleOff') }}</p>
    <p v-if="p.skipped" class="sub">{{ t('status.skipped', { n: p.skipped }) }}</p>
    <p v-if="p.errors" class="sub">{{ t('status.errors', { n: p.errors }) }}</p>

    <div v-if="list.length" :id="id" class="queue" :class="{ dismissed }">
      <template v-for="g in groups" :key="g.head">
        <p class="head">{{ g.head }}</p>
        <ul :aria-label="g.head">
          <li v-for="task in g.tasks" :key="task.kind" :class="{ now: task.now }">
            <span class="dot" aria-hidden="true" />
            <span class="what">
              {{ name(task) }}
              <span v-if="task.kind === 'scan' && p.scanPath" class="path" :title="p.scanPath">{{ p.scanPath }}</span>
            </span>
            <span class="count">{{ count(task) }}</span>
          </li>
        </ul>
      </template>
      <p v-if="paused" class="note">{{ t('status.pausedNote') }}</p>
      <p v-else-if="onBattery" class="note">{{ t('status.batteryNote') }}</p>
    </div>
  </div>
</template>

<style scoped>
.status {
  position: relative;
  padding: 12px;
  border-radius: var(--radius-m);
  background: var(--surface-hover);
  font-size: 12px;
}
.line {
  display: flex;
  align-items: center;
  gap: 4px;
  min-height: 24px;
}
.label {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  color: var(--text-2);
  text-overflow: ellipsis;
  white-space: nowrap;
}
.icon-btn.small {
  width: 24px;
  height: 24px;
}
.bar {
  height: 3px;
  margin-top: 6px;
  border-radius: 2px;
  background: var(--border);
  overflow: hidden;
}
.bar span {
  display: block;
  height: 100%;
  background: var(--accent);
  transition: width 0.6s ease;
}
.bar.indeterminate span {
  width: 30% !important;
  animation: slide 1.4s ease-in-out infinite;
}
@keyframes slide {
  from {
    transform: translateX(-100%);
  }
  to {
    transform: translateX(340%);
  }
}
.sub {
  margin: 4px 0 0;
  color: var(--text-2); /* --text-3 is under 4.5:1 on this tinted panel */
}

/* The list behind the card: above it, shown a moment after the pointer or
   focus arrives, and kept while the pointer crosses over to it. */
.queue {
  position: absolute;
  left: 0;
  bottom: calc(100% + 6px);
  z-index: 20;
  min-width: 100%;
  width: max-content;
  max-width: 320px;
  padding: 8px 12px 10px;
  border: 1px solid var(--border);
  border-radius: var(--radius-m);
  background: var(--surface);
  box-shadow: var(--shadow);
  visibility: hidden;
  opacity: 0;
  transition:
    opacity 0.12s,
    visibility 0s 0.12s;
}
.queue::after {
  content: '';
  position: absolute;
  inset: 100% 0 auto;
  height: 8px;
}
.status:is(:hover, :focus-within) .queue:not(.dismissed) {
  visibility: visible;
  opacity: 1;
  transition-delay: 0.2s;
}
.head {
  margin: 4px 0 2px;
  color: var(--text-3);
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.04em;
}
.queue ul {
  margin: 0;
  padding: 0;
  list-style: none;
}
.queue li {
  display: flex;
  align-items: baseline;
  gap: 8px;
  padding: 3px 0;
  color: var(--text-2);
}
.queue li.now {
  color: var(--text);
}
.dot {
  flex-shrink: 0;
  width: 7px;
  height: 7px;
  border: 1.5px solid var(--text-3);
  border-radius: 50%;
  translate: 0 -1px;
}
.now .dot {
  border-color: var(--accent);
  background: var(--accent);
  animation: pulse 1.6s ease-in-out infinite;
}
@keyframes pulse {
  50% {
    opacity: 0.35;
  }
}
.what {
  flex: 1;
  min-width: 0;
}
.path {
  display: block;
  overflow: hidden;
  color: var(--text-3);
  font-size: 11px;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.count {
  flex-shrink: 0;
  color: var(--text-3);
  font-variant-numeric: tabular-nums;
}
.note {
  margin: 6px 0 0;
  padding-top: 6px;
  border-top: 1px solid var(--border);
  color: var(--text-3);
}
</style>
