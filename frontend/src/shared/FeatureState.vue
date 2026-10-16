<script setup lang="ts">
// What a page shows in place of its content: it is loading, there is
// nothing to show, or it failed. One look for all three, so every module
// says these things the same way. Loading and failure are announced; a
// loading state names what is happening in words, so it still informs
// when motion is reduced.
import { computed } from 'vue'
import type { IconName } from '../design/icons'
import Icon from './Icon.vue'

const props = defineProps<{
  kind: 'loading' | 'empty' | 'error'
  title: string
  text?: string
  /** The icon for an empty state; loading and error have their own. */
  icon?: IconName
}>()

const shown = computed<IconName>(() =>
  props.kind === 'loading' ? 'loading' : props.kind === 'error' ? 'error' : (props.icon ?? 'photos'),
)
const role = computed(() => (props.kind === 'error' ? 'alert' : props.kind === 'loading' ? 'status' : undefined))
</script>

<template>
  <div class="feature-state" :class="kind" :role="role" :aria-busy="kind === 'loading' ? 'true' : undefined">
    <span class="art" aria-hidden="true">
      <Icon :name="shown" :size="32" :class="{ spin: kind === 'loading' }" />
    </span>
    <h2>{{ title }}</h2>
    <p v-if="text" class="lumen-selectable">{{ text }}</p>
    <div v-if="$slots.default" class="actions">
      <slot />
    </div>
  </div>
</template>

<style scoped>
.feature-state {
  display: grid;
  justify-items: center;
  align-content: center;
  gap: 0.75rem;
  max-width: 27.5rem;
  margin: 0 auto;
  padding: 4rem 1.5rem;
  text-align: center;
  animation: lumen-rise var(--lumen-d-slow) var(--lumen-ease-out) both;
}
.art {
  display: grid;
  place-items: center;
  width: 4.5rem;
  height: 4.5rem;
  margin-bottom: 0.5rem;
  border: 1px solid var(--ui-border);
  border-radius: 22px;
  background: var(--lumen-bg-sunken);
  color: var(--ui-text-dimmed);
}
.error .art {
  border-color: transparent;
  background: color-mix(in srgb, var(--ui-error) 10%, transparent);
  color: var(--ui-error);
}
h2 {
  margin: 0;
  color: var(--ui-text-highlighted);
  font: 600 1.125rem/1.3 var(--font-display);
  letter-spacing: -0.01em;
  text-wrap: balance;
}
p {
  margin: 0;
  color: var(--ui-text-toned);
  text-wrap: pretty;
}
.actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: center;
  gap: 0.5rem;
  margin-top: 0.25rem;
}
.spin {
  animation: lumen-spin 1s linear infinite;
}
@media (max-height: 600px) {
  .feature-state {
    padding-block: 1.5rem;
  }
}
</style>
