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
    <Icon :name="shown" :size="40" :class="{ spin: kind === 'loading' }" />
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
  gap: 0.25rem;
  padding: 1.5rem;
  text-align: center;
  color: var(--ui-text-dimmed);
}
.feature-state.error {
  color: var(--ui-error);
}
h2 {
  margin: 0.5rem 0 0;
  color: var(--ui-text-highlighted);
  font: 600 1.125rem var(--font-display);
}
p {
  margin: 0;
  max-width: 24rem;
  color: var(--ui-text-muted);
}
.actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: center;
  gap: 0.5rem;
  margin-top: 0.75rem;
}
.spin {
  animation: lumen-spin 1s linear infinite;
}
@keyframes lumen-spin {
  to {
    transform: rotate(360deg);
  }
}
</style>
