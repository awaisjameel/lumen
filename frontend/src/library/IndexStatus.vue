<script setup lang="ts">
// The small, calm status card in the sidebar. The app is fully usable while
// indexing runs; this says what it is doing now and offers a pause. Hovering
// it, or focusing it, lists everything at work, what comes next, and why the
// files that couldn't be read failed. In the collapsed sidebar it is a
// single button (pause or resume, or a tick when there is nothing to do),
// with the same list beside it.
import IconButton from '../shared/IconButton.vue'
import { computed, onBeforeUnmount, onMounted, ref, shallowRef, useId, watch } from 'vue'
import { engine, FailReason, WaitReason } from '../bridge'
import type { FailureGroup } from '../bridge'
import { fileName } from '../shared/format'
import { t } from '../shared/i18n'
import type { MessageKey } from '../shared/i18n'
import { icons } from '../design/icons'
import Icon from '../shared/Icon.vue'
import { isAI, lead, tasks } from './activity'
import type { Task } from './activity'
import { useLibrary } from './store'

defineProps<{ compact?: boolean }>()

const lib = useLibrary()
const p = computed(() => lib.progress)
const id = useId()

const list = computed(() => (p.value ? tasks(p.value) : []))
// The list behind the card: what's at work and next, and what failed.
const hasList = computed(() => list.value.length > 0 || !!p.value?.errors)
const next = computed(() => list.value.filter((task) => !task.now))
const groups = computed(() =>
  [
    { head: t('status.now'), tasks: list.value.filter((task) => task.now) },
    { head: t('status.next'), tasks: next.value },
  ].filter((g) => g.tasks.length),
)
// The stages wait while paused, or while the PC needs them to (a game in
// front, a busy processor, the user at the PC in idle-only mode); a scan goes
// on (it is cheap).
const paused = computed(() => !!p.value?.paused && next.value.length > 0)
const waiting = computed(() => {
  const reason = p.value?.waiting ?? WaitReason.WaitNone
  return !paused.value && next.value.length > 0 ? reason : WaitReason.WaitNone
})
const onBattery = computed(() => !!p.value?.onBattery && next.value.some((task) => isAI(task.kind)))
// What the card is about: the work held back, else the lead.
const shown = computed(() => (paused.value || waiting.value ? next.value[0] : lead(list.value)))

const label = computed(() => {
  const task = shown.value
  if (paused.value) return t('status.paused')
  if (waiting.value) return t(`status.wait.${waiting.value}`)
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

// Why files failed: read when the list opens, and again once their count changes.
const failures = shallowRef<FailureGroup[]>([])
let failuresAt = 0 // the count they were read at
async function loadFailures() {
  const n = p.value?.errors ?? 0
  if (!n || n === failuresAt) return
  failuresAt = n
  try {
    const groups = await engine.failures()
    if (failuresAt === n) failures.value = groups // not overtaken by a read of a newer count
  } catch {
    failuresAt = 0 // read again next time
  }
}
const card = ref<HTMLElement | null>(null)
watch(
  () => p.value?.errors ?? 0,
  (n) => {
    if (!n) {
      // Cleared (retried, removed): the same count later is other files.
      failuresAt = 0
      failures.value = []
    } else if (card.value?.matches(':hover, :focus-within')) {
      void loadFailures() // the list is showing: keep its reasons current
    }
  },
)

const reasonText: Record<FailReason, MessageKey> = {
  [FailReason.FailDamaged]: 'status.failure.damaged',
  [FailReason.FailSlow]: 'status.failure.slow',
  [FailReason.FailLocked]: 'status.failure.locked',
  [FailReason.FailOther]: 'status.failure.other',
  [FailReason.$zero]: 'status.failure.other', // never sent; the engine names a reason for every file
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

// The pointer or focus arriving brings the list back, with the reasons read.
function open() {
  dismissed.value = false
  void loadFailures()
}
</script>

<template>
  <div v-if="p" ref="card" class="status" :class="{ compact }" @mouseenter="open" @focusin="open">
    <div class="line">
      <span class="label" role="status">{{ label }}</span>
      <IconButton
        v-if="list.length || p.paused"
        :icon="p.paused ? icons.play : icons.pause"
        :label="p.paused ? t('status.resume') : t('status.pause')"
        :aria-describedby="hasList ? id : undefined"
        color="neutral"
        variant="ghost"
        size="xs"
        @click="togglePause"
      />
      <span v-else-if="compact" class="idle" :title="label">
        <Icon name="check" :size="16" />
      </span>
    </div>
    <div v-if="list.length" class="bar" :class="{ indeterminate: shown?.kind === 'scan' }">
      <span :style="{ width: fraction * 100 + '%' }" />
    </div>
    <p v-if="count(shown)" class="sub">{{ count(shown) }}</p>
    <p v-else-if="!list.length && p.total" class="sub">{{ t('status.items', { n: p.total }) }}</p>
    <p v-if="p.ai.error" class="sub" :title="p.ai.error">{{ t('status.aiOff') }}</p>
    <p v-if="p.ai.facesError" class="sub" :title="p.ai.facesError">{{ t('status.peopleOff') }}</p>
    <p v-if="p.skipped" class="sub">{{ t('status.skipped', { n: p.skipped }) }}</p>
    <p v-if="p.errors" class="sub failed-count" tabindex="0" :aria-describedby="id">
      {{ t('status.errors', { n: p.errors }) }}
    </p>

    <div v-if="hasList" :id="id" class="queue" :class="{ dismissed }">
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
      <p v-else-if="waiting" class="note">{{ t(`status.waitNote.${waiting}`) }}</p>
      <p v-else-if="onBattery" class="note">{{ t('status.batteryNote') }}</p>
      <section v-if="p.errors" class="failures" :class="{ apart: list.length }">
        <p class="head">{{ t('status.failed') }}</p>
        <ul :aria-label="t('status.failed')">
          <li v-for="g in failures" :key="g.reason" class="reason">
            <span class="what">
              {{ t(reasonText[g.reason]) }}
              <span v-for="f in g.files" :key="f.path" class="path" :title="f.error">{{ fileName(f.path) }}</span>
              <span v-if="g.count > g.files.length" class="path">
                {{ t('status.more', { n: g.count - g.files.length }) }}
              </span>
            </span>
            <span class="count">{{ g.count }}</span>
          </li>
        </ul>
        <p class="note">{{ t('status.failedHint') }}</p>
      </section>
    </div>
  </div>
</template>

<style scoped>
.status {
  position: relative;
  padding: 12px;
  border-radius: calc(var(--ui-radius) * 1.5);
  background: var(--ui-bg-elevated);
  font-size: 0.75rem;
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
  color: var(--ui-text-muted);
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* Collapsed sidebar: only the button (or the tick) shows; what it says is
   still read out, and the list opens beside the rail. */
.status.compact {
  padding: 6px;
}

.status.compact .label,
.status.compact .sub,
.status.compact .bar {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip-path: inset(50%);
  white-space: nowrap;
}

.status.compact .line {
  justify-content: center;
}

.status.compact .queue {
  left: calc(100% + 6px);
  bottom: 0;
  min-width: 240px;
}

.idle {
  display: grid;
  place-items: center;
  width: 24px;
  height: 24px;
  color: var(--ui-text-dimmed);
}

.bar {
  height: 3px;
  margin-top: 6px;
  border-radius: 2px;
  background: var(--ui-border);
  overflow: hidden;
}

.bar span {
  display: block;
  height: 100%;
  background: var(--ui-primary);
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
  color: var(--ui-text-muted);
  /* the dimmed text is under 4.5:1 on this tinted panel */
}

/* The list behind the card: above it, shown a moment after the pointer or
   focus arrives, and kept while the pointer crosses over to it. */
.queue {
  position: absolute;
  left: 0;
  bottom: calc(100% + 6px);
  z-index: var(--lumen-z-panel);
  min-width: 100%;
  width: max-content;
  max-width: 320px;
  max-height: min(75vh, 640px);
  overflow-y: auto;
  padding: 8px 12px 10px;
  border: 1px solid var(--ui-border);
  border-radius: calc(var(--ui-radius) * 1.5);
  background: var(--ui-bg);
  box-shadow: var(--lumen-shadow);
  visibility: hidden;
  opacity: 0;
  transition:
    opacity 0.12s,
    visibility 0s 0.12s;
}

.status:is(:hover, :focus-within) .queue:not(.dismissed) {
  visibility: visible;
  opacity: 1;
  transition-delay: 0.2s;
}

/* The pointer crosses the gap to the list over this bridge. It hangs off the
   card: off the list, which scrolls, it would be cut off and scroll with it. */
.status:is(:hover, :focus-within):has(.queue:not(.dismissed))::before {
  content: '';
  position: absolute;
  inset: auto 0 100%;
  height: 8px;
}

.head {
  margin: 4px 0 2px;
  color: var(--ui-text-dimmed);
  font-size: 0.6875rem;
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
  color: var(--ui-text-muted);
}

.queue li.now {
  color: var(--ui-text-highlighted);
}

.dot {
  flex-shrink: 0;
  width: 7px;
  height: 7px;
  border: 1.5px solid var(--ui-text-dimmed);
  border-radius: 50%;
  translate: 0 -1px;
}

.now .dot {
  border-color: var(--ui-primary);
  background: var(--ui-primary);
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
  color: var(--ui-text-dimmed);
  font-size: 0.6875rem;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.count {
  flex-shrink: 0;
  color: var(--ui-text-dimmed);
  font-variant-numeric: tabular-nums;
}

.note {
  margin: 6px 0 0;
  padding-top: 6px;
  border-top: 1px solid var(--ui-border);
  color: var(--ui-text-dimmed);
}

.failures.apart {
  margin-top: 8px;
  padding-top: 4px;
  border-top: 1px solid var(--ui-border);
}

.reason {
  padding: 4px 0;
}

.reason .what {
  color: var(--ui-text-highlighted);
}

.failed-count {
  border-radius: var(--ui-radius);
}

.failed-count:focus-visible {
  outline: 2px solid var(--ui-primary);
  outline-offset: 2px;
}

@media (forced-colors: active) {
  .now .dot {
    background: Highlight;
    border-color: Highlight;
  }
}
</style>
