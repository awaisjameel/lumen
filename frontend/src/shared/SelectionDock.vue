<script setup lang="ts">
// The dock that comes up while something is selected, and only then: how
// many are selected (with the way to clear it), the main things to do with
// them, and the rest in a menu. It sits at the foot of the page, in the
// flow under the content, so it never covers what it acts on. Its actions
// are commands (shared/commands.ts), so the dock, menus, the palette and
// shortcuts share one name, icon and rule. On a narrow page the labels
// give way to icons, whose names stay as tooltips.
import UButton from '@nuxt/ui/components/Button.vue'
import UDropdownMenu from '@nuxt/ui/components/DropdownMenu.vue'
import UTooltip from '@nuxt/ui/components/Tooltip.vue'
import { computed } from 'vue'
import { icons } from '../design/icons'
import { capsOf, isEnabled, menuItems, runCommand } from './commands'
import type { Command } from './commands'
import { t } from './i18n'
import IconButton from './IconButton.vue'

const props = defineProps<{
  /** How many are selected; the dock shows while this is above 0. */
  count: number
  /** What is selected, already worded ("3 selected"). */
  summary: string
  /** The main actions, as buttons. */
  actions?: readonly Command[]
  /** The rest, under More. */
  more?: readonly Command[]
}>()
const emit = defineEmits<{ clear: [] }>()

const moreItems = computed(() => menuItems(props.more ?? []))
const tip = (c: Command) => (isEnabled(c) ? c.title() : (c.reason?.() ?? c.title()))
</script>

<template>
  <Transition name="dock">
    <div v-if="count > 0" class="selection-dock lumen-float" role="toolbar" :aria-label="summary">
      <span class="count">
        <IconButton
          :icon="icons.close"
          :label="t('selection.clear')"
          :kbds="['Esc']"
          color="neutral"
          variant="ghost"
          size="sm"
          @click="emit('clear')"
        />
        <span aria-live="polite">{{ summary }}</span>
      </span>
      <span class="divider" aria-hidden="true" />
      <UTooltip v-for="c in actions" :key="c.id" :text="tip(c)" :kbds="capsOf(c.id)">
        <UButton
          :icon="c.icon ? icons[c.icon] : undefined"
          :color="c.danger ? 'error' : 'neutral'"
          variant="ghost"
          :disabled="!isEnabled(c)"
          :aria-label="c.title()"
          :data-command="c.id"
          @click="runCommand(c.id)"
        >
          <span class="dock-label">{{ c.title() }}</span>
        </UButton>
      </UTooltip>
      <slot />
      <UDropdownMenu v-if="moreItems.length" :items="moreItems" :content="{ side: 'top', align: 'end' }">
        <IconButton :icon="icons.more" :label="t('selection.more')" color="neutral" variant="ghost" />
      </UDropdownMenu>
    </div>
  </Transition>
</template>

<style scoped>
.selection-dock {
  display: flex;
  align-items: center;
  flex: none;
  gap: 0.25rem;
  width: fit-content;
  max-width: calc(100% - 2rem);
  margin: 0.75rem auto;
  padding: 0.375rem;
  border-color: var(--ui-border-accented);
  border-radius: var(--lumen-r-xl);
  overflow-x: auto;
}
.count {
  display: inline-flex;
  align-items: center;
  gap: 0.5rem;
  padding: 0 0.5rem 0 0;
  color: var(--ui-text-highlighted);
  font-size: 0.8125rem;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}
.divider {
  align-self: stretch;
  width: 1px;
  margin: 0.25rem 0.25rem;
  background: var(--ui-border);
}
.selection-dock :deep(button) {
  flex: none;
}
/* Narrow pages: icons only; each name stays as its tooltip and label. */
@container (max-width: 900px) {
  .dock-label {
    display: none;
  }
}

/* It rises from the page's foot with a little spring, and leaves faster. */
.dock-enter-active {
  transition:
    opacity var(--lumen-d-slow) var(--lumen-ease-out),
    transform var(--lumen-d-slow) var(--lumen-ease-spring);
}
.dock-leave-active {
  transition:
    opacity var(--lumen-d-fast) var(--lumen-ease-in),
    transform var(--lumen-d-fast) var(--lumen-ease-in);
}
.dock-enter-from,
.dock-leave-to {
  opacity: 0;
  transform: translateY(1rem) scale(0.96);
}
</style>
