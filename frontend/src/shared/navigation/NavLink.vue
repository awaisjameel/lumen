<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink } from 'vue-router'
import type { RouterLinkProps } from 'vue-router'
import type { Destination } from './contract'
import { useNavigation } from './router'

const props = defineProps<Omit<RouterLinkProps, 'to' | 'custom'> & { to: Destination }>()
const navigation = useNavigation()
const href = computed(() => navigation.resolve(props.to).href)
</script>

<template>
  <RouterLink v-bind="props" :to="href"><slot /></RouterLink>
</template>
