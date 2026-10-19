<script setup lang="ts">
import UButton from '@nuxt/ui/components/Button.vue'
import { computed, onBeforeUnmount, watch } from 'vue'
import { useLibrary } from '../library/store'
import { lead, tasks } from '../library/activity'
import { icons } from '../design/icons'
import { t } from '../shared/i18n'
import Icon from '../shared/Icon.vue'
import MediaTile from '../shared/MediaTile.vue'

const emit = defineEmits<{ complete: []; ready: [value: boolean] }>()
const lib = useLibrary()
lib.attach()
onBeforeUnmount(() => lib.detach())
const list = computed(() => (lib.progress ? tasks(lib.progress) : []))
const current = computed(() => lead(list.value))
const done = computed(() => !!lib.progress && !list.value.length)
const stageKinds = ['scan', 'metadata', 'thumbnail', 'embedding', 'faces'] as const
const stages = computed(() => {
  const currentKind = current.value?.kind
  const currentIndex =
    currentKind === 'videos' || currentKind === 'ocr'
      ? stageKinds.length
      : stageKinds.findIndex((kind) => kind === currentKind)
  return stageKinds.map((kind, i) => ({
    kind,
    number: i + 1,
    current: currentKind === kind,
    done: done.value || (i < currentIndex && !list.value.some((task) => task.kind === kind)),
  }))
})
watch(done, (value) => emit('ready', value), { immediate: true })
const previews = computed(() => Array.from(lib.days.values()).flat().slice(0, 12))
watch(
  () => lib.buckets.slice(0, 2).map((b) => b.day),
  (days) => void lib.ensureDays(days),
  { immediate: true },
)
const fraction = computed(() => {
  const p = lib.progress
  if (!p || p.scanning || !p.total) return undefined
  const remaining = Math.max(p.metadata, p.thumbnail, p.embedding, p.faces)
  return Math.round(Math.max(0, Math.min(1, 1 - remaining / p.total)) * 100)
})
</script>

<template>
  <section class="first-scan" data-first-scan>
    <div class="scan-head" role="status">
      <span class="lumen-light-dot" :class="{ idle: done, paused: lib.progress?.paused }" />
      <h2>{{ done ? t('status.upToDate') : t(`status.${current?.kind ?? 'scan'}`) }}</h2>
      <span>{{ t('status.found', { n: lib.progress?.total ?? 0 }) }}</span>
    </div>
    <div class="previews" :aria-label="t('welcome.scanHint')">
      <div v-for="i in 12" :key="i" class="preview">
        <MediaTile v-if="previews[i - 1]" :id="previews[i - 1]?.id ?? 0" :thumb="previews[i - 1]?.t ?? 0" /><span
          v-else
          class="lumen-skeleton"
        />
      </div>
    </div>
    <ol class="stages">
      <li v-for="stage in stages" :key="stage.kind" :data-current="stage.current">
        <Icon v-if="stage.done" name="check" :size="16" /><span
          v-else-if="stage.current"
          class="lumen-light-dot"
        /><span v-else class="number">{{ stage.number }}</span> <span>{{ t(`status.${stage.kind}`) }}</span
        ><small>{{ stage.done ? t('status.done') : stage.current ? t('status.now') : t('status.next') }}</small>
      </li>
    </ol>
    <div
      class="progress"
      role="progressbar"
      :aria-label="t('welcome.progress')"
      :aria-valuenow="fraction"
      :aria-valuemin="0"
      :aria-valuemax="100"
      :class="{ indeterminate: fraction === undefined }"
    >
      <span :style="{ transform: `scaleX(${(fraction ?? 30) / 100})` }" />
    </div>
    <p v-if="lib.loadError || lib.daysError" role="alert">{{ lib.loadError || lib.daysError }}</p>
    <div class="scan-foot">
      <p>{{ t('welcome.explore') }}</p>
      <UButton
        data-browse
        color="primary"
        variant="solid"
        :trailing-icon="icons['arrow-right']"
        @click="emit('complete')"
        >{{ t('welcome.browse') }}</UButton
      >
    </div>
    <p v-if="!previews.length && !done" class="waiting">{{ t('welcome.waiting') }}</p>
  </section>
</template>

<style scoped>
.first-scan {
  display: grid;
  gap: 1.25rem;
  padding: 1.5rem;
  border: 1px solid var(--ui-border);
  border-radius: var(--lumen-r-lg);
  background: var(--lumen-bg-raised);
  box-shadow: var(--lumen-shadow-1);
}
.scan-head,
.scan-foot {
  display: flex;
  align-items: center;
  gap: 0.75rem;
}
.scan-head h2 {
  flex: 1;
  font-size: 0.875rem;
  font-weight: 600;
  color: var(--ui-text-highlighted);
}
.scan-head {
  font-variant-numeric: tabular-nums;
}
h2,
p {
  margin: 0;
}
.previews {
  display: grid;
  grid-template-columns: repeat(6, minmax(0, 1fr));
  gap: 0.25rem;
}
.preview {
  aspect-ratio: 1;
  min-width: 0;
}
.lumen-skeleton {
  display: block;
  width: 100%;
  height: 100%;
}
.stages {
  list-style: none;
  padding: 0;
  margin: 0;
}
.stages li {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  padding: 0.5rem 0.75rem;
  min-height: 2rem;
  font-size: 0.8125rem;
  border-radius: var(--lumen-r-md);
}
.stages [data-current='true'] {
  background: var(--lumen-light-soft);
}
.stages li > span:nth-last-child(2) {
  flex: 1;
}
.number {
  width: 1rem;
  text-align: center;
  color: var(--ui-text-dimmed);
}
small,
.waiting,
.scan-foot p {
  font-size: 0.75rem;
  color: var(--ui-text-dimmed);
}
.scan-foot p {
  flex: 1;
}
.progress {
  height: 0.25rem;
  overflow: hidden;
  border-radius: var(--lumen-r-xs);
  background: var(--ui-border);
}
.progress span {
  display: block;
  height: 100%;
  background: var(--lumen-light);
  transform-origin: left;
}
.indeterminate span {
  animation: lumen-pulse calc(var(--lumen-d-slow) * 5) var(--lumen-ease-out) infinite;
}
</style>
