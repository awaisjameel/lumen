<script setup lang="ts">
import NavLink from '../../src/shared/navigation/NavLink.vue'
import NavButton from '../../src/shared/navigation/NavButton.vue'
import CollectionCard from '../../src/shared/CollectionCard.vue'
import { destination } from '../../src/shared/navigation/contract'
</script>

<template>
  <!-- expect-rejection -->
  <NavLink to="/blocked">Blocked</NavLink>
  <!-- expect-rejection -->
  <NavButton :to="{ name: 'blocked' }">Blocked</NavButton>
  <!-- expect-rejection -->
  <CollectionCard :to="destination({ name: 'album' })" title="Album" icon="albums" />
  <!-- expect-rejection -->
  <NavLink :to="destination({ name: 'settings', query: { section: 'unknown' } })">Settings</NavLink>
</template>
