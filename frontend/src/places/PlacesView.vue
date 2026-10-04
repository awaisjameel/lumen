<script setup lang="ts">
import UButton from '@nuxt/ui/components/Button.vue'
import UDropdownMenu from '@nuxt/ui/components/DropdownMenu.vue'
import { BlockKind, engine, thumbUrl } from '../bridge'
import { block } from '../shared/block'
import { logError } from '../shared/errors'
import { t } from '../shared/i18n'
import { useLiveReload } from '../shared/liveReload'
import { useList } from '../shared/listStore'
import { icons } from '../design/icons'
import CardGrid from '../shared/CardGrid.vue'
import CollectionCard from '../shared/CollectionCard.vue'
import FeatureState from '../shared/FeatureState.vue'
import Page from '../shared/Page.vue'
import PageHeader from '../shared/PageHeader.vue'
import { useItemCommands, useCommands } from '../shared/commands'
import SearchBox from '../search/SearchBox.vue'

const { list, loaded, error, load, invalidate } = useList(engine.places)
useLiveReload(load, (c) => c.assets || c.thumbs)

async function blockPlace(id: number, name: string) {
  if (await block(BlockKind.BlockPlace, id, name)) await invalidate().catch(logError)
}
const placeItems = useItemCommands(
  list,
  (place) => place.id,
  (place) => [
    {
      id: `place.${place.id}.block`,
      group: 'view',
      title: () => t('blocked.confirmTitle', { name: place.name }),
      icon: 'eye-off',
      run: () => blockPlace(place.id, place.name),
    },
  ],
)
useCommands([
  {
    id: 'places.refresh',
    group: 'view',
    title: () => t('places.refresh'),
    icon: 'refresh',
    run: () => load().catch(logError),
  },
])
</script>

<template>
  <Page>
    <template #header
      ><PageHeader :title="t('nav.places')"><SearchBox /></PageHeader
    ></template>
    <p class="mb-4 text-muted">{{ t('places.intro') }}</p>
    <FeatureState v-if="error" kind="error" :title="error" />
    <FeatureState v-else-if="!loaded" kind="loading" :title="t('places.loading')" />
    <CardGrid v-if="list.length" layout="row">
      <CollectionCard
        v-for="place in list"
        :key="place.id"
        :to="{ name: 'place', params: { id: place.id } }"
        :title="place.name"
        :subtitle="
          t('places.nearCity', { location: [place.region, place.country].filter(Boolean).join(', ') || place.name })
        "
        :count="t('status.items', { n: place.count })"
        :cover="place.cover && place.coverThumb ? thumbUrl(place.cover, place.coverThumb) : undefined"
        icon="places"
        layout="row"
      >
        <UDropdownMenu :items="placeItems(place.id)">
          <UButton :icon="icons.more" :aria-label="t('places.actions', { name: place.name })" square />
        </UDropdownMenu>
      </CollectionCard>
    </CardGrid>
    <FeatureState
      v-else-if="loaded && !error"
      kind="empty"
      icon="places"
      :title="t('places.empty')"
      :text="t('places.emptyHint')"
    />
  </Page>
</template>
