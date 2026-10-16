<script setup lang="ts" generic="V extends string | number">
// A small set of mutually exclusive choices shown side by side (All /
// Photos / Videos, a sort order, a view mode), with a thumb that slides to
// the chosen one. A radio group to assistive technology: Tab reaches the
// chosen option, the arrow keys move the choice. The tabs look draws the
// same choice as underlined tabs, for a choice that switches what the whole
// page lists (exact or similar copies, a kind of block, a status).
import { computed, useTemplateRef } from 'vue'
import type { IconName } from '../design/icons'
import { formatNumber } from './i18n'
import Icon from './Icon.vue'

export interface SegmentedItem<T> {
  value: T
  label: string
  icon?: IconName
  disabled?: boolean
  /** Why it cannot be chosen now, shown as its tooltip while disabled. */
  reason?: string
  /** How many things the choice shows, beside its label. */
  count?: number
}

const props = withDefaults(
  defineProps<{
    items: readonly SegmentedItem<V>[]
    /** What the choice is about ("Show"). */
    label: string
    look?: 'segmented' | 'tabs'
  }>(),
  { look: 'segmented' },
)
const model = defineModel<V>({ required: true })

const index = computed(() =>
  Math.max(
    0,
    props.items.findIndex((i) => i.value === model.value),
  ),
)
const group = useTemplateRef<HTMLElement>('group')

function choose(i: number) {
  const item = props.items[i]
  if (!item || item.disabled) return
  model.value = item.value
}

function onKey(e: KeyboardEvent) {
  const step =
    e.key === 'ArrowRight' || e.key === 'ArrowDown' ? 1 : e.key === 'ArrowLeft' || e.key === 'ArrowUp' ? -1 : 0
  if (!step) return
  e.preventDefault()
  const n = props.items.length
  for (let k = 1; k <= n; k++) {
    const i = (index.value + step * k + n) % n
    if (!props.items[i]?.disabled) {
      choose(i)
      group.value?.querySelectorAll<HTMLButtonElement>('[role="radio"]')[i]?.focus()
      return
    }
  }
}
</script>

<template>
  <div
    ref="group"
    :class="look"
    role="radiogroup"
    :aria-label="label"
    :style="{ '--n': items.length, '--i': index }"
    @keydown="onKey"
  >
    <button
      v-for="(item, i) in items"
      :key="item.value"
      type="button"
      role="radio"
      :aria-checked="item.value === model"
      :tabindex="i === index ? 0 : -1"
      :disabled="item.disabled"
      :title="item.disabled ? item.reason : undefined"
      @click="choose(i)"
    >
      <Icon v-if="item.icon" :name="item.icon" :size="16" />
      {{ item.label }}
      <span v-if="item.count !== undefined" class="count">{{ formatNumber(item.count) }}</span>
    </button>
  </div>
</template>

<style scoped>
.segmented {
  position: relative;
  display: inline-grid;
  grid-auto-columns: 1fr;
  grid-auto-flow: column;
  flex: none;
  padding: 3px;
  border: 1px solid var(--ui-border);
  border-radius: var(--lumen-r-md);
  background: var(--lumen-bg-sunken);
}
.segmented::before {
  content: '';
  position: absolute;
  top: 3px;
  bottom: 3px;
  left: 3px;
  width: calc((100% - 6px) / var(--n, 2));
  border-radius: 5px;
  background: var(--lumen-bg-raised);
  box-shadow: var(--lumen-shadow-1);
  transform: translateX(calc(100% * var(--i, 0)));
  transition: transform var(--lumen-d-base) var(--lumen-ease-out);
}
:root.dark .segmented::before {
  background: var(--lumen-bg-overlay);
}
button {
  position: relative;
  z-index: 1;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 0.375rem;
  min-height: 1.5rem;
  height: calc(var(--lumen-control-h) - 0.5rem);
  padding: 0 0.75rem;
  border: 0;
  border-radius: 5px;
  background: transparent;
  color: var(--ui-text-toned);
  font-size: 0.8125rem;
  font-weight: 500;
  white-space: nowrap;
  cursor: pointer;
  transition: color var(--lumen-d-fast) var(--lumen-ease-out);
}
button:hover:not(:disabled) {
  color: var(--ui-text-highlighted);
}
button[aria-checked='true'] {
  color: var(--ui-text-highlighted);
  font-weight: 600;
}
button:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}
.count {
  color: var(--ui-text-dimmed);
  font-variant-numeric: tabular-nums;
}

.tabs {
  display: inline-flex;
  flex-wrap: wrap;
  gap: 0.25rem;
  max-width: 100%;
  border-bottom: 1px solid var(--ui-border);
}
.tabs button {
  height: 2.5rem;
  padding: 0 0.75rem;
  border-radius: 0;
  color: var(--ui-text-muted);
  font-weight: 600;
}
.tabs button[aria-checked='true']::after {
  content: '';
  position: absolute;
  right: 0.5rem;
  bottom: -1px;
  left: 0.5rem;
  height: 2px;
  border-radius: 2px;
  background: var(--ui-primary);
  animation: lumen-grow-x var(--lumen-d-base) var(--lumen-ease-out);
}
.tabs button:focus-visible {
  border-radius: var(--lumen-r-sm);
}
.tabs .count {
  min-width: 1.25rem;
  height: 1.25rem;
  padding: 0 0.4375rem;
  border-radius: 999px;
  background: var(--lumen-bg-active);
  color: var(--ui-text-toned);
  font-size: 0.6875rem;
  line-height: 1.25rem;
  text-align: center;
}
.tabs button[aria-checked='true'] .count {
  background: var(--lumen-primary-soft);
  color: var(--lumen-primary-text);
}

@media (forced-colors: active) {
  .segmented::before,
  .tabs button[aria-checked='true']::after {
    border: 2px solid Highlight;
  }
}
</style>
