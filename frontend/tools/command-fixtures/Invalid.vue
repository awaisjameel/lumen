<script setup lang="ts">
import CommandButton from '../../src/shared/CommandButton.vue'
import IconButton from '../../src/shared/IconButton.vue'
import { runCommand } from '../../src/shared/commands'
import { destination } from '../../src/shared/navigation/contract'
</script>
<template>
  <!-- expect-rejection -->
  <CommandButton command="media.favourtie" />
  <!-- expect-rejection -->
  <IconButton command="unknown.action" />
  <!-- expect-rejection -->
  <CommandButton command="person.12.rename" />
  <!-- expect-rejection -->
  <CommandButton :command="{ name: 'person.rename', id: '12' }" />
  <!-- expect-rejection -->
  <CommandButton command="media.open" href="/people" />
  <!-- expect-rejection -->
  <IconButton command="media.open" :to="destination({ name: 'people' })" />
  <!-- expect-rejection -->
  <CommandButton @click="runCommand('unknown.action')" command="media.open" />
</template>
