<script setup lang="ts">
import UButton from '@nuxt/ui/components/Button.vue'
import UDropdownMenu from '@nuxt/ui/components/DropdownMenu.vue'
import { computed, onBeforeUnmount, onMounted, useTemplateRef, watch } from 'vue'
import type { Place } from '../bridge'
import { Filter, thumbUrl } from '../bridge'
import { icons } from '../design/icons'
import CollectionCard from '../shared/CollectionCard.vue'
import IconButton from '../shared/IconButton.vue'
import { formatNumber, t } from '../shared/i18n'
import { menuItems, useCommands } from '../shared/commands'
import type { Command } from '../shared/commands'
import { useCollectionPreview } from '../shared/collectionPreview'

const props = defineProps<{ place: Place; active: boolean; refreshCommand?: Command }>()
const emit = defineEmits<{ hover: []; preview: []; block: [] }>()
const preview = useCollectionPreview(() => ({ filter: Filter.FilterAll, place: props.place.id }))
const root = useTemplateRef<HTMLElement>('root')
const images = computed(() => preview.items.value.filter((item) => item.t).slice(0, 3))
let observer: IntersectionObserver | undefined
let visible = false
onMounted(() => {
  observer = new IntersectionObserver((entries) => {
    if (entries.some((entry) => entry.isIntersecting)) {
      visible = true
      void preview.load()
      observer?.disconnect()
    }
  })
  if (root.value) observer.observe(root.value)
})
onBeforeUnmount(() => observer?.disconnect())
watch(
  () => `${props.place.cover}:${props.place.coverThumb}:${props.place.count}`,
  () => {
    if (!visible) return
    preview.clear()
    void preview.load()
  },
)
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
  <div ref="root">
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
      <template v-if="images.length > 1" #cover
        ><div class="mosaic">
          <img v-for="item in images" :key="item.id" :src="thumbUrl(item.id, item.t)" alt="" loading="lazy" /></div
      ></template>
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
.mosaic {
  position: absolute;
  inset: 0;
  display: grid;
  grid-template-columns: 2fr 1fr;
  grid-template-rows: 1fr 1fr;
  gap: 0.25rem;
}
.mosaic img {
  width: 100%;
  height: 100%;
  min-height: 0;
  object-fit: cover;
}
.mosaic img:first-child {
  grid-row: span 2;
}
</style>
