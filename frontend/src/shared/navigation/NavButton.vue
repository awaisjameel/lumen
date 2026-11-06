<script setup lang="ts">
import UButton from '@nuxt/ui/components/Button.vue'
import type { ButtonProps, ButtonSlots } from '@nuxt/ui/components/Button.vue'
import { computed } from 'vue'
import { useForwardProps } from 'reka-ui'
import type { Destination } from './contract'
import { useNavigation } from './router'

const props = defineProps<Omit<ButtonProps, 'to' | 'href' | 'external' | 'target' | 'rel'> & { to?: Destination }>()
defineSlots<ButtonSlots>()
const navigation = useNavigation()
const forwarded = useForwardProps(props)
const href = computed(() => (props.to ? navigation.resolve(props.to).href : undefined))
</script>

<template>
  <UButton v-bind="forwarded" :to="href">
    <template v-if="$slots.leading" #leading="slot"><slot name="leading" v-bind="slot" /></template>
    <template v-if="$slots.default" #default="slot"><slot v-bind="slot" /></template>
    <template v-if="$slots.trailing" #trailing="slot"><slot name="trailing" v-bind="slot" /></template>
  </UButton>
</template>
