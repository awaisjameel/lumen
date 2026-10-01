<script setup lang="ts">
import { BlockKind, engine } from '../bridge'
import { block } from '../shared/block'
import { logError } from '../shared/errors'
import Icon from '../shared/Icon.vue'
import { t } from '../shared/i18n'
import { useLiveReload } from '../shared/liveReload'
import { useList } from '../shared/listStore'
import EmptyState from '../shared/EmptyState.vue'
import GroupCard from '../shared/GroupCard.vue'
import SearchBox from '../search/SearchBox.vue'

const { list, loaded, error, load, invalidate } = useList(engine.places)
useLiveReload(load, (c) => c.assets || c.thumbs)

async function blockPlace(id: number, name: string) {
  if (await block(BlockKind.BlockPlace, id, name)) await invalidate().catch(logError)
}
</script>

<template>
  <div class="page">
    <header class="topbar">
      <h1>{{ t('nav.places') }}</h1>
      <SearchBox />
      <div class="spacer" />
    </header>
    <div class="page-body">
      <p class="muted intro">{{ t('places.intro') }}</p>
      <p v-if="error" class="page-error" role="alert">{{ error }}</p>
      <div v-if="list.length" class="group-cards">
        <GroupCard
          v-for="place in list"
          :id="place.id"
          :key="place.id"
          route="place"
          :title="place.name"
          :subtitle="[place.region, place.country].filter(Boolean).join(', ')"
          :count="place.count"
          :cover="place.cover"
          :cover-thumb="place.coverThumb"
          icon="pin"
        >
          <button
            class="icon-btn"
            :title="t('blocked.block')"
            :aria-label="t('blocked.confirmTitle', { name: place.name })"
            @click="blockPlace(place.id, place.name)"
          >
            <Icon name="eye-off" :size="16" />
          </button>
        </GroupCard>
      </div>
      <EmptyState v-else-if="loaded && !error" icon="pin" :title="t('places.empty')" :text="t('places.emptyHint')" />
    </div>
  </div>
</template>
