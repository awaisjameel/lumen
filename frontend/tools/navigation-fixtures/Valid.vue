<script setup lang="ts">
import NavLink from '../../src/shared/navigation/NavLink.vue'
import NavButton from '../../src/shared/navigation/NavButton.vue'
import CollectionCard from '../../src/shared/CollectionCard.vue'
import { destination } from '../../src/shared/navigation/contract'
</script>

<template>
  <NavLink :to="destination({ name: 'blocked' })">Blocked</NavLink>
  <NavButton :to="destination({ name: 'settings', query: { section: 'search' } })">Search settings</NavButton>
  <CollectionCard :to="destination({ name: 'album', params: { id: 1 } })" title="Album" icon="albums" />
</template>
