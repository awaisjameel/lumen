<script setup lang="ts">
import UDropdownMenu from '@nuxt/ui/components/DropdownMenu.vue'
import type { DropdownMenuProps } from '@nuxt/ui/components/DropdownMenu.vue'
import { computed } from 'vue'
import { useForwardProps } from 'reka-ui'
import { resolvedMenu } from './menu'
import type { NavigationMenuItems } from './menu'
import { useNavigation } from './router'

const props = defineProps<
  Omit<DropdownMenuProps, 'items' | 'labelKey' | 'descriptionKey'> & { items?: NavigationMenuItems }
>()
const navigation = useNavigation()
const forwarded = useForwardProps(props)
const items = computed(() => resolvedMenu(props.items, (target) => navigation.resolve(target).href))
</script>

<template>
  <UDropdownMenu v-bind="forwarded" :items="items">
    <template v-for="(_, name) in $slots" #[name]="slot"><slot :name="name" v-bind="slot" /></template>
  </UDropdownMenu>
</template>
