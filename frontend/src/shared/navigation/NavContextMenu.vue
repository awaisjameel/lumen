<script setup lang="ts">
import UContextMenu from '@nuxt/ui/components/ContextMenu.vue'
import type { ContextMenuProps } from '@nuxt/ui/components/ContextMenu.vue'
import { computed } from 'vue'
import { useForwardProps } from 'reka-ui'
import { resolvedMenu } from './menu'
import type { NavigationMenuItems } from './menu'
import { useNavigation } from './router'

const props = defineProps<
  Omit<ContextMenuProps, 'items' | 'labelKey' | 'descriptionKey'> & { items?: NavigationMenuItems }
>()
const navigation = useNavigation()
const forwarded = useForwardProps(props)
const items = computed(() => resolvedMenu(props.items, (target) => navigation.resolve(target).href))
</script>

<template>
  <UContextMenu v-bind="forwarded" :items="items">
    <template v-for="(_, name) in $slots" #[name]="slot"><slot :name="name" v-bind="slot" /></template>
  </UContextMenu>
</template>
