<script setup lang="ts">
// The small, calm progress line in the sidebar. The app is fully usable
// while this runs; it only says what is happening and offers a pause.
import { computed } from 'vue'
import { engine } from '../bridge'
import { logError } from '../shared/errors'
import { t } from '../shared/i18n'
import Icon from '../shared/Icon.vue'
import { useLibrary } from './store'

const lib = useLibrary()
const p = computed(() => lib.progress)

// Thumbnails are the long pole: every asset needs one, metadata first.
const left = computed(() => (p.value ? Math.max(p.value.metadata, p.value.thumbnail) : 0))
const busy = computed(() => !!p.value && (p.value.scanning || left.value > 0))
const fraction = computed(() => {
  const v = p.value
  if (!v || !v.total) return 0
  return Math.min(1, Math.max(0, (v.total - left.value) / v.total))
})

const label = computed(() => {
  const v = p.value
  if (!v) return ''
  if (v.paused && left.value) return t('status.paused', { left: left.value })
  if (v.scanning) return t('status.scanning', { n: v.discovered })
  if (left.value) return t('status.preparing', { left: left.value })
  return t('status.upToDate')
})

function togglePause() {
  if (p.value) engine.setPaused(!p.value.paused).catch(logError)
}
</script>

<template>
  <div v-if="p" class="status" role="status" aria-live="polite">
    <div class="line">
      <span class="label">{{ label }}</span>
      <button
        v-if="busy || p.paused"
        class="icon-btn small"
        :title="p.paused ? t('status.resume') : t('status.pause')"
        @click="togglePause"
      >
        <Icon :name="p.paused ? 'play' : 'pause'" :size="14" />
      </button>
    </div>
    <div v-if="busy" class="bar" :class="{ indeterminate: p.scanning && !p.total }">
      <span :style="{ width: fraction * 100 + '%' }" />
    </div>
    <p v-if="!busy && p.total" class="sub">{{ t('status.items', { n: p.total }) }}</p>
    <p v-if="p.skipped" class="sub">{{ t('status.skipped', { n: p.skipped }) }}</p>
    <p v-if="p.errors" class="sub">{{ t('status.errors', { n: p.errors }) }}</p>
  </div>
</template>

<style scoped>
.status {
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
  color: var(--text-2);
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
  color: var(--text-3);
}
</style>
