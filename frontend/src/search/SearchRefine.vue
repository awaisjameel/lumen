<script setup lang="ts">
// The search's refinements, as chips in the view bar: which kind of item
// (All, Photos, Videos), Favourites on its own, and menus for when and where.
// They live in the route's query beside the words, so a refined search is
// a place the back button returns to; the library store reads them from the
// scope and narrows the results without searching again.
import UButton from '@nuxt/ui/components/Button.vue'
import UDropdownMenu from '@nuxt/ui/components/DropdownMenu.vue'
import type { DropdownMenuItem } from '@nuxt/ui/components/DropdownMenu.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import UPopover from '@nuxt/ui/components/Popover.vue'
import { computed, onMounted, shallowRef } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import type { LocationQueryRaw } from 'vue-router'
import { engine, Filter } from '../bridge'
import type { Place } from '../bridge'
import Chip from '../shared/Chip.vue'
import { logError } from '../shared/errors'
import { dateLabel, daySpan } from '../shared/format'
import { t } from '../shared/i18n'
import Icon from '../shared/Icon.vue'

const props = defineProps<{
  /** Which kind of item the results show. */
  media: Filter
  /** Whether the library shows videos at all, so Videos is offered. */
  videos: boolean
}>()

const route = useRoute()
const router = useRouter()

const favourites = computed(() => route.query.favourites === 'true')
const from = computed(() => String(route.query.from ?? ''))
const to = computed(() => String(route.query.to ?? ''))
const place = computed(() => Number(route.query.place) || 0)
const refined = computed(
  () => props.media !== Filter.FilterAll || favourites.value || !!from.value || !!to.value || !!place.value,
)

type Refinement = 'media' | 'favourites' | 'from' | 'to' | 'place'
/** Sets refinements in one navigation, so none overwrites another. */
function refine(changes: Partial<Record<Refinement, string | number | boolean>>) {
  const query: LocationQueryRaw = { ...route.query }
  for (const [key, value] of Object.entries(changes)) query[key] = value ? String(value) : undefined
  void router.replace({ query })
}
function clear() {
  void router.replace({ query: { q: route.query.q } })
}

const kinds = computed(() => [
  { value: Filter.FilterAll, label: t('library.all') },
  { value: Filter.FilterPhotos, label: t('library.photos') },
  ...(props.videos ? [{ value: Filter.FilterVideos, label: t('library.videos') }] : []),
])

const when = computed(() => {
  if (from.value && to.value) return daySpan(from.value, to.value, true)
  if (from.value) return t('search.since', { day: dateLabel(from.value) })
  if (to.value) return t('search.until', { day: dateLabel(to.value) })
  return t('search.anyTime')
})

// The places are read when the menu first opens, or at once when the route
// already names one, so the chip can say which.
const places = shallowRef<Place[]>([])
async function loadPlaces() {
  try {
    places.value = await engine.places()
  } catch (e) {
    logError(e)
  }
}
onMounted(() => {
  if (place.value) void loadPlaces()
})
const where = computed(
  () =>
    (place.value && places.value.find((p) => p.id === place.value)?.name) ||
    t(place.value ? 'search.resultsPlace' : 'search.resultsAnyPlace'),
)
const placeItems = computed<DropdownMenuItem[]>(() => [
  {
    label: t('search.resultsAnyPlace'),
    type: 'checkbox',
    checked: !place.value,
    onSelect: () => refine({ place: 0 }),
  },
  ...places.value.map((p): DropdownMenuItem => ({
    label: p.name,
    type: 'checkbox',
    checked: p.id === place.value,
    onSelect: () => refine({ place: p.id }),
  })),
])
</script>

<template>
  <div class="search-refine" data-search-refine>
    <div class="kinds" role="group" :aria-label="t('library.show')">
      <Chip
        v-for="kind in kinds"
        :key="kind.value"
        :pressed="media === kind.value"
        :data-search-kind="kind.value || 'all'"
        @click="refine({ media: kind.value })"
        >{{ kind.label }}</Chip
      >
      <Chip :pressed="favourites" data-search-favourites @click="refine({ favourites: !favourites })">{{
        t('nav.favourites')
      }}</Chip>
    </div>
    <span class="divider" aria-hidden="true" />
    <UPopover :content="{ align: 'start' }">
      <Chip :pressed="!!(from || to)" :aria-label="t('search.when', { when })" data-search-when
        ><Icon name="calendar" :size="14" />{{ when }}<Icon name="chevron-down" :size="14"
      /></Chip>
      <template #content>
        <div class="when">
          <label
            >{{ t('search.resultsFrom')
            }}<UInput
              type="date"
              :model-value="from"
              :max="to || undefined"
              @update:model-value="refine({ from: $event })"
          /></label>
          <label
            >{{ t('search.resultsTo')
            }}<UInput
              type="date"
              :model-value="to"
              :min="from || undefined"
              @update:model-value="refine({ to: $event })"
          /></label>
          <UButton v-if="from || to" color="neutral" variant="ghost" @click="refine({ from: '', to: '' })">{{
            t('search.anyTime')
          }}</UButton>
        </div>
      </template>
    </UPopover>
    <UDropdownMenu
      :items="placeItems"
      :content="{ align: 'start' }"
      @update:open="
        (open: boolean) => {
          if (open) void loadPlaces()
        }
      "
    >
      <Chip :pressed="!!place" :aria-label="t('search.where', { where })" data-search-where
        ><Icon name="places" :size="14" />{{ where }}<Icon name="chevron-down" :size="14"
      /></Chip>
    </UDropdownMenu>
    <UButton v-if="refined" color="neutral" variant="ghost" data-search-clear-filters @click="clear">{{
      t('library.clearFilters')
    }}</UButton>
  </div>
</template>

<style scoped>
.search-refine,
.kinds {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  min-width: 0;
}
.search-refine {
  flex-wrap: wrap;
}
.divider {
  width: 1px;
  height: 1.25rem;
  margin: 0 0.25rem;
  background: var(--ui-border);
}
.when {
  display: grid;
  gap: 0.75rem;
  padding: 0.75rem;
}
.when label {
  display: grid;
  gap: 0.25rem;
  color: var(--ui-text-muted);
  font-size: 0.75rem;
}
</style>
