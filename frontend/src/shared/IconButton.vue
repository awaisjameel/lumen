<script setup lang="ts">
// A button that shows only an icon. Its label is said twice: as the button's
// accessible name, and as a tooltip on hover and keyboard focus, so no icon
// is left to be guessed. Everything else given to it reaches the button.
import UButton from './navigation/NavButton.vue'
import UTooltip from '@nuxt/ui/components/Tooltip.vue'
import { computed, getCurrentInstance, useAttrs, useTemplateRef } from 'vue'
import type { ComponentPublicInstance } from 'vue'
import type { Destination } from './navigation/contract'
import type { CommandId } from './commands'
import { commandKey, commandState, runCommand } from './commands'

const props = defineProps<
  {
    /** What the button does: its accessible name and, unless tip is given, its tooltip. */
    label?: string
    /** The icon; the default slot replaces it for one that is drawn here. */
    icon?: string
    /** The tooltip of a toggle, whose name stays while what a press does changes. */
    tip?: string
    /** The keys that do the same. */
    kbds?: string[]
    /** Where the tooltip opens, for a button at an edge of the window. */
    side?: 'top' | 'right' | 'bottom' | 'left'
  } & ({ command: CommandId; to?: never } | { command?: never; to?: Destination; label: string })
>()
const state = computed(() => (props.command ? commandState(props.command) : undefined))
defineOptions({ inheritAttrs: false })
// The tooltip, not the button, is this component's root, and Vue gives a
// parent's scoped-style attribute only to a root element: without it here a
// parent's scoped rules for a class it puts on the button would never match.
const scope = getCurrentInstance()?.vnode.scopeId
const attrs = useAttrs()
const buttonAttrs = computed(() => (scope ? { ...attrs, [scope]: '' } : attrs))
const button = useTemplateRef<ComponentPublicInstance>('button')
defineExpose({
  /** Focuses the button: a parent's ref to this component is not the element. */
  focus: () => {
    const element: unknown = button.value?.$el
    if (element instanceof HTMLElement) element.focus()
  },
})
</script>

<template>
  <UTooltip
    :text="tip ?? state?.reason ?? label ?? state?.title"
    :kbds="kbds ?? state?.caps"
    :content="side ? { side } : undefined"
  >
    <UButton
      ref="button"
      v-bind="buttonAttrs"
      :to="to"
      :icon="icon ?? ($slots.default ? undefined : state?.icon)"
      :aria-label="label ?? state?.title"
      :disabled="!!buttonAttrs.disabled || (state ? !state.enabled : undefined)"
      :loading="!!buttonAttrs.loading || state?.busy"
      :data-command="command ? commandKey(command) : undefined"
      square
      @click="command && !buttonAttrs.disabled && state?.enabled && runCommand(command)"
    >
      <template v-if="$slots.default" #default><slot /></template>
    </UButton>
  </UTooltip>
</template>
