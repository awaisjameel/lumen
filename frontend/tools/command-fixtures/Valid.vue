<script setup lang="ts">
import CommandButton from '../../src/shared/CommandButton.vue'
import IconButton from '../../src/shared/IconButton.vue'
import { itemCommand } from '../../src/shared/commandContract'
import { destination } from '../../src/shared/navigation/contract'
</script>
<template>
  <CommandButton command="media.open" color="neutral" />
  <CommandButton :command="itemCommand('person.rename', 12)">Rename</CommandButton>
  <IconButton command="media.open" />
  <IconButton label="People" :to="destination({ name: 'people' })" />
</template>
