<script setup lang="ts">
// A button that shows only an icon. Its label is said twice: as the button's
// accessible name, and as a tooltip on hover and keyboard focus, so no icon
// is left to be guessed. Everything else given to it reaches the button.
import UButton from '@nuxt/ui/components/Button.vue'
import UTooltip from '@nuxt/ui/components/Tooltip.vue'
import { useTemplateRef } from 'vue'
import type { ComponentPublicInstance } from 'vue'

defineProps<{
  /** What the button does: its accessible name and, unless tip is given, its tooltip. */
  label: string
  /** The icon; the default slot replaces it for one that is drawn here. */
  icon?: string
  /** The tooltip of a toggle, whose name stays while what a press does changes. */
  tip?: string
  /** The keys that do the same. */
  kbds?: string[]
  /** Where the tooltip opens, for a button at an edge of the window. */
  side?: 'top' | 'right' | 'bottom' | 'left'
}>()
defineOptions({ inheritAttrs: false })
const button = useTemplateRef<ComponentPublicInstance>('button')
defineExpose({
  /** Focuses the button: a parent's ref to this component is not the element. */
  focus: () => (button.value?.$el as HTMLElement | null | undefined)?.focus(),
})
</script>

<template>
  <UTooltip :text="tip ?? label" :kbds="kbds" :content="side ? { side } : undefined">
    <UButton ref="button" v-bind="$attrs" :icon="icon" :aria-label="label" square>
      <template v-if="$slots.default" #default><slot /></template>
    </UButton>
  </UTooltip>
</template>
