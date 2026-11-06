<script setup lang="ts">
import UButton from '@nuxt/ui/components/Button.vue'
import type { ButtonProps, ButtonSlots } from '@nuxt/ui/components/Button.vue'
import { computed } from 'vue'
import { useForwardProps } from 'reka-ui'
import { commandKey, commandState, runCommand } from './commands'
import type { CommandId } from './commands'

const props = defineProps<
  Omit<ButtonProps, 'to' | 'href' | 'external' | 'target' | 'rel'> & {
    command: CommandId
    to?: never
    href?: never
    external?: never
    target?: never
    rel?: never
  }
>()
const slots = defineSlots<ButtonSlots>()
const forwarded = useForwardProps(props)
const button = computed(() => {
  const { command, ...rest } = forwarded.value
  const state = commandState(command)
  return {
    ...rest,
    label: props.label ?? state.title,
    icon: props.icon ?? (slots.default ? undefined : state.icon),
    disabled: props.disabled || !state.enabled,
    loading: props.loading || state.busy,
    title: state.reason,
    'data-command': commandKey(command),
  }
})
</script>

<template>
  <UButton v-bind="button" @click="!button.disabled && runCommand(command)">
    <template v-if="$slots.leading" #leading="slot"><slot name="leading" v-bind="slot" /></template>
    <template v-if="$slots.default" #default="slot"><slot v-bind="slot" /></template>
    <template v-if="$slots.trailing" #trailing="slot"><slot name="trailing" v-bind="slot" /></template>
  </UButton>
</template>
