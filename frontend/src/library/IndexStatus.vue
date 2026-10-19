<script setup lang="ts">
// One activity card in the sidebar, with details in an anchored popover.
import UPopover from '@nuxt/ui/components/Popover.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import IconButton from '../shared/IconButton.vue'
import { computed, ref, shallowRef, useId, watch } from 'vue'
import { engine, FailReason, WaitReason } from '../bridge'
import type { FailureGroup, Progress } from '../bridge'
import { useCommands } from '../shared/commands'
import { fileName } from '../shared/format'
import { t, formatNumber } from '../shared/i18n'
import type { MessageKey } from '../shared/i18n'
import { icons } from '../design/icons'
import { isAI, lead, tasks } from './activity'
import type { Task } from './activity'
import { useLibrary } from './store'

const props = defineProps<{
  compact?: boolean
  progress?: Progress
  pause?: (paused: boolean) => void
  readFailures?: () => Promise<FailureGroup[]>
}>()

const lib = useLibrary()
const p = computed(() => props.progress ?? lib.progress)
const id = useId()

const list = computed(() => (p.value ? tasks(p.value) : []))
// The list behind the card: what's at work and next, and what failed.
const expanded = ref(false)
// The library's own card (not a preview of one) opens from anywhere: a
// search still waiting for photos to be understood links to it.
if (!props.progress)
  useCommands([
    {
      id: 'app.activity',
      group: 'app',
      title: () => t('status.open'),
      available: () => !!p.value,
      run: () => {
        expanded.value = true
      },
    },
  ])
const stopped = computed(
  () =>
    paused.value ||
    !!waiting.value ||
    (!shown.value?.now && (onBattery.value || !!p.value?.ai.error || !!p.value?.ai.facesError)),
)

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
  if (!task)
    return p.value?.ai.error ? t('status.aiOff') : p.value?.ai.facesError ? t('status.peopleOff') : t('status.upToDate')
  if (!task.now && onBattery.value && isAI(task.kind)) return t('status.battery')
  if (p.value?.ai.error && !task.now && isAI(task.kind)) return t('status.aiOff')
  return name(task)
})
const detail = computed(() => {
  const after = list.value.find((task) => task !== shown.value && !task.now)
  const text = count(shown.value)
  return after && text ? t('status.then', { count: text, task: name(after) }) : text
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
const failureLoading = ref(false)
const failureError = ref(false)
let failureRead = 0
let failuresAt = 0 // the count they were read at
async function loadFailures() {
  const n = p.value?.errors ?? 0
  if (!n || n === failuresAt) return
  failuresAt = n
  const read = ++failureRead
  failureLoading.value = true
  failureError.value = false
  try {
    const groups = await (props.readFailures ?? engine.failures)()
    if (read === failureRead) failures.value = groups // not overtaken by a read of a newer count
  } catch {
    if (read === failureRead) {
      failuresAt = 0
      failureError.value = true
    }
  } finally {
    if (read === failureRead) failureLoading.value = false
  }
}
watch(
  () => p.value?.errors ?? 0,
  (n) => {
    if (!n) {
      // Cleared (retried, removed): the same count later is other files.
      failureRead++
      failuresAt = 0
      failureLoading.value = false
      failureError.value = false
      failures.value = []
    } else if (expanded.value) {
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
  if (!p.value) return
  if (props.pause) props.pause(!p.value.paused)
  else void lib.setPaused(!p.value.paused)
}

watch(expanded, (open) => {
  if (open) void loadFailures()
})
</script>
<template>
  <div v-if="p" class="status" :class="{ compact, stopped }" data-index-status>
    <UPopover v-model:open="expanded" :content="{ side: 'right', align: 'end', sideOffset: 8, collisionPadding: 12 }">
      <button
        type="button"
        class="activity-trigger"
        :aria-label="t('status.details') + ': ' + label"
        :aria-describedby="id"
        data-activity-trigger
      >
        <span class="line"
          ><span
            class="lumen-light-dot"
            :class="{ idle: !list.length && !stopped, paused: stopped }"
            aria-hidden="true"
          /><span class="label" role="status">{{ label }}</span></span
        >
        <span
          v-if="list.length"
          class="bar"
          data-activity-progress
          :data-indeterminate="shown?.kind === 'scan'"
          :class="{ indeterminate: shown?.kind === 'scan' }"
          ><span :style="{ transform: `scaleX(${fraction})` }"
        /></span>
        <span v-if="detail" class="sub">{{ detail }}</span>
        <span v-else-if="!list.length && p.total" class="sub">{{ t('status.items', { n: p.total }) }}</span>
        <span v-if="p.errors" class="sub failed-count">{{ t('status.errors', { n: p.errors }) }}</span>
      </button>
      <template #content>
        <section :id="id" class="queue" :aria-label="t('status.details')" data-activity-details>
          <header class="queue-head">
            <span class="lumen-light-dot" :class="{ idle: !list.length && !stopped, paused: stopped }" />
            <h2>{{ label }}</h2>
            <IconButton
              :icon="icons.close"
              :label="t('status.close')"
              color="neutral"
              variant="ghost"
              @click="expanded = false"
            />
          </header>
          <p v-if="detail" class="note">{{ detail }}</p>
          <template v-for="g in groups" :key="g.head">
            <p class="head" data-task-group>{{ g.head }}</p>
            <ul :aria-label="g.head">
              <li v-for="task in g.tasks" :key="task.kind" :data-active="task.now" :class="{ now: task.now }">
                <span class="lumen-light-dot" :class="{ paused: !task.now }" aria-hidden="true" /><span class="what"
                  >{{ name(task)
                  }}<span v-if="task.kind === 'scan' && p.scanPath" class="path" :title="p.scanPath">{{
                    p.scanPath
                  }}</span></span
                ><span class="count">{{ count(task) }}</span>
              </li>
            </ul>
          </template>
          <p v-if="paused" class="note" data-wait-note>
            {{ p.scanning ? t('status.pauseScanNote') : t('status.pausedNote') }}
          </p>
          <p v-else-if="waiting" class="note" data-wait-note>{{ t(`status.waitNote.${waiting}`) }}</p>
          <p v-else-if="onBattery" class="note" data-wait-note>{{ t('status.batteryNote') }}</p>
          <div v-if="p.ai.error || p.ai.facesError" class="notice">
            <p v-if="p.ai.error">
              <strong>{{ t('status.aiOff') }}</strong
              ><span>{{ p.ai.error }}</span>
            </p>
            <p v-if="p.ai.facesError">
              <strong>{{ t('status.peopleOff') }}</strong
              ><span>{{ p.ai.facesError }}</span>
            </p>
            <p>{{ t('status.aiHint') }}</p>
            <UButton to="/settings?section=indexing" color="neutral" variant="outline">{{
              t('status.settings')
            }}</UButton>
          </div>
          <UButton
            :icon="p.paused ? icons.play : icons.pause"
            :disabled="!list.length"
            color="neutral"
            variant="outline"
            @click="togglePause"
            >{{ p.paused ? t('status.resume') : t('status.pause') }}</UButton
          >
          <p v-if="p.skipped" class="note">
            {{ t('status.skipped', { n: p.skipped }) }}. {{ t('status.skippedHint') }}
          </p>
          <section v-if="p.errors" class="failures">
            <h3 class="head">{{ t('status.errors', { n: p.errors }) }}</h3>
            <p v-if="failureLoading" role="status" aria-busy="true">{{ t('status.failureLoading') }}</p>
            <template v-else-if="failureError"
              ><p role="alert">{{ t('status.failureError') }}</p>
              <UButton color="neutral" variant="outline" @click="loadFailures">{{
                t('welcome.retry')
              }}</UButton></template
            >
            <ul v-else :aria-label="t('status.failed')">
              <li v-for="g in failures" :key="g.reason" class="reason" data-failure-reason>
                <span class="what"
                  >{{ t(reasonText[g.reason])
                  }}<span v-for="f in g.files" :key="f.path" class="path" data-failure-file :title="f.error">{{
                    fileName(f.path)
                  }}</span
                  ><span v-if="g.count > g.files.length" class="path">{{
                    t('status.more', { n: g.count - g.files.length })
                  }}</span></span
                ><span class="count">{{ formatNumber(g.count) }}</span>
              </li>
            </ul>
            <p class="note">{{ t('status.failedHint') }}</p>
          </section>
        </section>
      </template>
    </UPopover>
    <IconButton
      v-if="list.length || p.paused"
      class="pause-button"
      :icon="p.paused ? icons.play : icons.pause"
      :label="p.paused ? t('status.resume') : t('status.pause')"
      :disabled="!list.length"
      color="neutral"
      variant="ghost"
      size="xs"
      @click="togglePause"
    />
  </div>
</template>
<style scoped>
.status {
  position: relative;
  padding: 0.75rem;
  border: 1px solid var(--ui-border);
  border-radius: var(--lumen-r-md);
  background: var(--lumen-bg-raised);
  font-size: 0.75rem;
}
.activity-trigger {
  display: grid;
  width: 100%;
  gap: 0.5rem;
  text-align: left;
  cursor: pointer;
  border-radius: var(--lumen-r-xs);
}
.activity-trigger:focus-visible {
  outline: 2px solid var(--ui-primary);
  outline-offset: 4px;
}
.line {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  min-height: 1.5rem;
  padding-right: 1.5rem;
}
.label {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  color: var(--ui-text-highlighted);
  font-weight: 600;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.pause-button {
  position: absolute;
  right: 0.5rem;
  top: 0.75rem;
}
.bar {
  display: block;
  height: 0.25rem;
  border-radius: var(--lumen-r-xs);
  background: var(--ui-border);
  overflow: hidden;
}
.bar span {
  display: block;
  height: 100%;
  background: var(--lumen-light);
  transform-origin: left;
}
.stopped .bar span {
  background: var(--ui-text-dimmed);
}
.bar.indeterminate span {
  transform: scaleX(0.3) !important;
  animation: lumen-pulse calc(var(--lumen-d-slow) * 5) var(--lumen-ease-out) infinite;
}
.sub {
  color: var(--ui-text-dimmed);
  font-variant-numeric: tabular-nums;
  overflow-wrap: anywhere;
}
.compact {
  padding: 0.5rem;
}
.compact .activity-trigger {
  min-height: 1.5rem;
}
.compact .line {
  padding: 0;
  justify-content: center;
}
.compact .label,
.compact .bar,
.compact .sub {
  display: none;
}
.compact .pause-button {
  position: static;
  margin-top: 0.25rem;
}
.queue {
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
  padding: 1.25rem;
  width: min(25rem, calc(100vw - 6rem));
  max-height: min(40rem, calc(100dvh - 2rem));
  overflow: auto;
  font-size: 0.8125rem;
}
.queue-head {
  display: flex;
  align-items: center;
  gap: 0.75rem;
}
.queue-head h2 {
  flex: 1;
  margin: 0;
  font-size: 0.875rem;
  font-weight: 600;
  color: var(--ui-text-highlighted);
}
.head {
  margin: 0;
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
  gap: 0.75rem;
  padding: 0.5rem 0;
  color: var(--ui-text-toned);
}
.queue li.now {
  color: var(--ui-text-highlighted);
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
  flex: none;
  color: var(--ui-text-dimmed);
  font-variant-numeric: tabular-nums;
}
.note {
  margin: 0;
  color: var(--ui-text-toned);
}
.failures {
  padding-top: 0.75rem;
  border-top: 1px solid var(--ui-border);
}
.notice {
  padding: 0.75rem;
  border: 1px solid var(--ui-border);
  border-radius: var(--lumen-r-md);
  background: var(--lumen-bg-sunken);
}
.notice p {
  margin: 0 0 0.5rem;
  overflow-wrap: anywhere;
}
.notice strong,
.notice span {
  display: block;
}
@media (max-height: 600px) {
  .status:not(.compact) .sub,
  .status:not(.compact) .bar {
    display: none;
  }
}
</style>
