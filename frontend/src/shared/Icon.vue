<script setup lang="ts">
// One of the manifest's icons (design/icons.ts), drawn from the local
// collection. Decorative: the control it sits in carries the label.
import UIcon from '@nuxt/ui/components/Icon.vue'
import { icons } from '../design/icons'
import type { IconName } from '../design/icons'

const props = withDefaults(defineProps<{ name: IconName; size?: number; filled?: boolean }>(), { size: 20 })
</script>

<template>
  <UIcon
    :name="icons[props.name]"
    :size="props.size"
    class="lumen-icon"
    :class="{ filled: props.filled }"
    aria-hidden="true"
  />
</template>

<style scoped>
.lumen-icon {
  flex-shrink: 0;
}
/* A filled heart or star: the outline's shapes take the text colour. */
.lumen-icon.filled :deep(path) {
  fill: currentColor;
}
</style>
