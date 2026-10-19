<script setup lang="ts">
import UButton from '@nuxt/ui/components/Button.vue'
import UDropdownMenu from '@nuxt/ui/components/DropdownMenu.vue'
import { computed } from 'vue'
import type { Place } from '../bridge'
import { Filter, thumbUrl } from '../bridge'
import { icons } from '../design/icons'
import CollectionCard from '../shared/CollectionCard.vue'
import CoverMosaic from '../shared/CoverMosaic.vue'
import IconButton from '../shared/IconButton.vue'
import { formatNumber, t } from '../shared/i18n'
import { menuItems, useCommands } from '../shared/commands'
import type { Command } from '../shared/commands'

const props = defineProps<{ place: Place; active: boolean; refreshCommand?: Command }>()
const emit = defineEmits<{ hover: []; preview: []; block: [] }>()
const actions: Command[] = [
  {
    id: `place.${props.place.id}.preview`,
    group: 'view',
    title: () => t('places.preview'),
    icon: 'photos',
    run: () => emit('preview'),
  },
  {
    id: `place.${props.place.id}.block`,
    group: 'view',
    title: () => t('blocked.confirmTitle', { name: props.place.name }),
    icon: 'eye-off',
    run: () => emit('block'),
  },
]
useCommands(actions)
const items = computed(() =>
  menuItems(
    props.refreshCommand
      ? [actions[0], props.refreshCommand, ...actions.slice(1)].filter((action): action is Command => !!action)
      : actions,
  ),
)
</script>

<template>
  <div>
    <CollectionCard
      :data-collection="place.id"
      :data-active="active || undefined"
      :to="{ name: 'place', params: { id: place.id } }"
      :title="place.name"
      :subtitle="
        t('places.nearCity', { location: [place.region, place.country].filter(Boolean).join(', ') || place.name })
      "
      :count="t('status.items', { n: formatNumber(place.count) })"
      :cover="place.cover && place.coverThumb ? thumbUrl(place.cover, place.coverThumb) : undefined"
      icon="places"
      @mouseenter="emit('hover')"
      @focusin="emit('hover')"
    >
      <template #cover
        ><CoverMosaic
          class="card-mosaic"
          :view="{ filter: Filter.FilterAll, place: place.id }"
          :count="place.count"
          :cover="place.cover"
          :cover-thumb="place.coverThumb"
          icon="places"
      /></template>
      <UButton
        variant="soft"
        size="xs"
        :aria-label="t('places.previewNamed', { name: place.name })"
        @click="emit('preview')"
        >{{ t('places.preview') }}</UButton
      >
      <UDropdownMenu :items="items"
        ><IconButton :icon="icons.more" :label="t('places.actions', { name: place.name })"
      /></UDropdownMenu>
    </CollectionCard>
  </div>
</template>

<style scoped>
[data-active] :deep(.cover) {
  outline: 0.25rem solid var(--lumen-primary-soft-2);
}
.card-mosaic {
  position: absolute;
  inset: 0;
}
</style>
