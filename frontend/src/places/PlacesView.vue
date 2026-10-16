<script setup lang="ts">
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import { computed, nextTick, onBeforeUnmount, ref, useTemplateRef, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { BlockKind, engine, Filter, thumbUrl } from '../bridge'
import type { Place, View } from '../bridge'
import { icons } from '../design/icons'
import { block, onBlocksChanged } from '../shared/block'
import { useCollectionPreview } from '../shared/collectionPreview'
import type { PreviewItem } from '../shared/collectionPreview'
import { logError, reportError } from '../shared/errors'
import { formatNumber, t } from '../shared/i18n'
import { useLiveReload } from '../shared/liveReload'
import { useList } from '../shared/listStore'
import { compareNames } from '../shared/names'
import { notify } from '../shared/notify'
import { useCommands } from '../shared/commands'
import type { Command } from '../shared/commands'
import CardGrid from '../shared/CardGrid.vue'
import FeatureState from '../shared/FeatureState.vue'
import Icon from '../shared/Icon.vue'
import IconButton from '../shared/IconButton.vue'
import Page from '../shared/Page.vue'
import PageHeader from '../shared/PageHeader.vue'
import ViewBar from '../shared/ViewBar.vue'
import { useLibrary } from '../library/store'
import OfflineAtlas from './OfflineAtlas.vue'
import PlaceCard from './PlaceCard.vue'

const router = useRouter()
const route = useRoute()
const { list, loaded, loading, error, load, invalidate } = useList(engine.places)
const country = ref('')
const hadPlaces = ref(false)
watch(list, (items) => {
  if (items.length) hadPlaces.value = true
})
const hovered = ref(0)
const selected = ref(0)
const panel = useTemplateRef<HTMLElement>('panel')
let returnFocus: HTMLElement | null = null
const countryGroups = computed(() => {
  const byCountry = new Map<string, Place[]>()
  for (const place of list.value) {
    const group = byCountry.get(place.country)
    if (group) group.push(place)
    else byCountry.set(place.country, [place])
  }
  return [...byCountry]
    .map(([name, items]) => ({ name, items, count: items.reduce((total, p) => total + p.count, 0) }))
    .sort((a, b) => b.count - a.count || compareNames(a.name, b.name))
})
const countries = computed(() => countryGroups.value.map((group) => group.name))
const filtered = computed(() => list.value.filter((p) => !country.value || p.country === country.value))
const activePlace = computed(() => filtered.value.find((p) => p.id === hovered.value) ?? filtered.value[0])
const selectedPlace = computed(() => filtered.value.find((p) => p.id === selected.value))
const groups = computed(() => countryGroups.value.filter((group) => !country.value || group.name === country.value))
const view = computed<View>(() => ({ filter: Filter.FilterAll, place: selected.value }))
const preview = useCollectionPreview(() => view.value)
useLiveReload(load, (c) => c.assets || c.thumbs)
onBeforeUnmount(onBlocksChanged(invalidate))
watch(country, () => {
  selected.value = 0
})
watch(selectedPlace, (place) => {
  if (!place) preview.clear()
})

async function refresh() {
  try {
    await load()
    preview.clear()
    if (selectedPlace.value) await preview.load()
    notify({ id: 'places.refresh', kind: 'success', title: t('places.upToDate') })
  } catch (e) {
    logError(e)
  }
}
async function openPreview(place: Place) {
  returnFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
  selected.value = place.id
  hovered.value = place.id
  await nextTick()
  panel.value?.focus()
  panel.value?.scrollIntoView({ block: 'nearest' })
  await preview.load()
}
// A place's page sends "Open on map" here with ?place=: once the places are
// read, that place is shown on the atlas with its preview open, and the
// query is dropped so going back and forth does not open it again.
watch(
  [() => route.query.place, loaded],
  async ([query, ready]) => {
    const id = Number(query)
    if (!ready || !Number.isInteger(id) || id <= 0) return
    const place = list.value.find((p) => p.id === id)
    void router.replace({ query: { ...route.query, place: undefined } })
    if (!place) return
    country.value = ''
    await openPreview(place)
  },
  { immediate: true },
)
async function closePreview() {
  selected.value = 0
  await nextTick()
  if (returnFocus?.isConnected) returnFocus.focus({ preventScroll: true })
}
async function openPhoto(item: PreviewItem) {
  const place = selectedPlace.value
  if (!place) return
  const scope: View = { filter: Filter.FilterAll, place: place.id }
  const day = item.day
  await router.push({ name: 'place', params: { id: place.id } })
  const lib = useLibrary()
  await lib.setScope({ kind: 'timeline', view: scope })
  await lib.ensureDays([day])
  if (lib.itemAt({ day, id: item.id })) lib.viewer = { day, id: item.id }
}
async function blockPlace(place: Place) {
  if (await block(BlockKind.BlockPlace, place.id, place.name)) await invalidate().catch(logError)
}
const refreshCommand: Command = {
  id: 'places.refresh',
  group: 'view',
  title: () => t('places.refresh'),
  icon: 'refresh',
  enabled: () => !loading.value,
  run: refresh,
}
useCommands([refreshCommand])
</script>

<template>
  <Page>
    <template #header
      ><PageHeader
        :title="t('nav.places')"
        :subtitle="
          t('places.caption', {
            cities: t(list.length === 1 ? 'places.cityCountOne' : 'places.cityCount', { n: formatNumber(list.length) }),
            countries: t(countries.length === 1 ? 'places.countryCountOne' : 'places.countryCount', {
              n: formatNumber(countries.length),
            }),
          })
        "
        ><span class="offline-label"><Icon name="globe" :size="16" />{{ t('places.offline') }}</span
        ><IconButton
          :icon="icons.refresh"
          :label="t('places.refresh')"
          variant="ghost"
          color="neutral"
          :loading="loading && loaded"
          :disabled="loading"
          @click="refresh" /></PageHeader
    ></template>
    <template v-if="list.length" #toolbar
      ><ViewBar :label="t('places.country')"
        ><div class="country-chips">
          <span>{{ t('places.country') }}</span
          ><UButton
            :variant="!country ? 'soft' : 'ghost'"
            :color="!country ? 'primary' : 'neutral'"
            :aria-pressed="!country"
            @click="country = ''"
            >{{ t('places.allCountries') }} <span class="tnum">{{ formatNumber(list.length) }}</span></UButton
          ><UButton
            v-for="name in countries"
            :key="name"
            :variant="country === name ? 'soft' : 'ghost'"
            :aria-pressed="country === name"
            :color="country === name ? 'primary' : 'neutral'"
            @click="country = name"
            >{{ name }}
            <span class="tnum">{{ formatNumber(list.filter((p) => p.country === name).length) }}</span></UButton
          >
        </div></ViewBar
      ></template
    >
    <div class="places-content">
      <p v-if="list.length" class="intro">{{ t('places.intro') }}</p>
      <p v-if="loading && loaded" role="status">{{ t('places.refreshing') }}</p>
      <UAlert v-if="error && loaded" role="alert" color="warning" variant="subtle" :title="error">
        <template #actions
          ><UButton variant="outline" color="neutral" size="sm" @click="refresh">{{
            t('common.retry')
          }}</UButton></template
        >
      </UAlert>
      <FeatureState v-if="error && !loaded" kind="error" :title="error"
        ><UButton @click="refresh">{{ t('common.retry') }}</UButton></FeatureState
      >
      <section v-else-if="!loaded" aria-busy="true" :aria-label="t('places.loading')">
        <p role="status">{{ t('places.loading') }}</p>
        <div class="lumen-skeleton atlas-skeleton" />
        <CardGrid><div v-for="n in 4" :key="n" class="lumen-skeleton card-skeleton" /></CardGrid>
      </section>
      <section v-if="activePlace" class="atlas-panel" :aria-label="t('places.atlas')">
        <OfflineAtlas :places="filtered" :active="activePlace.id" @hover="hovered = $event" @preview="openPreview" />
        <div class="atlas-cover">
          <button
            type="button"
            class="cover"
            :aria-label="t('places.previewNamed', { name: activePlace.name })"
            @click="openPreview(activePlace)"
          >
            <img
              v-if="activePlace.cover && activePlace.coverThumb"
              :src="thumbUrl(activePlace.cover, activePlace.coverThumb)"
              alt=""
            /><Icon v-else name="places" :size="32" />
          </button>
          <div class="flex min-w-0 items-baseline gap-2">
            <strong class="truncate">{{ activePlace.name }}</strong
            ><span>{{ activePlace.country }}</span>
          </div>
          <span>{{ t('status.items', { n: formatNumber(activePlace.count) }) }}</span>
          <UButton
            :color="selectedPlace ? 'neutral' : 'primary'"
            :variant="selectedPlace ? 'ghost' : 'solid'"
            :trailing-icon="icons['arrow-right']"
            @click="openPreview(activePlace)"
            >{{ t('places.preview') }}</UButton
          >
        </div>
      </section>
      <section
        v-if="selectedPlace"
        id="place-preview"
        ref="panel"
        class="preview-panel"
        tabindex="-1"
        :aria-label="t('places.previewTitle')"
        @keydown.esc.stop="closePreview"
      >
        <header>
          <div>
            <h2>{{ selectedPlace.name }}</h2>
            <p>
              {{ t('places.nearCity', { location: selectedPlace.country }) }} ·
              {{ t('status.items', { n: formatNumber(selectedPlace.count) }) }}
            </p>
          </div>
          <UButton :to="{ name: 'place', params: { id: selectedPlace.id } }" :trailing-icon="icons['arrow-right']">{{
            t('places.openAll')
          }}</UButton
          ><IconButton :icon="icons.close" :label="t('places.closePreview')" variant="ghost" @click="closePreview" />
        </header>
        <p v-if="preview.loading.value" role="status">{{ t('places.loadingPhotos') }}</p>
        <div v-else-if="preview.error.value" role="alert">
          {{ preview.error.value }} <UButton @click="preview.load">{{ t('common.retry') }}</UButton>
        </div>
        <p v-else-if="!preview.items.value.length">{{ t('places.noPreviews') }}</p>
        <div v-else class="preview-photos">
          <button
            v-for="item in preview.items.value"
            :key="item.id"
            type="button"
            :aria-label="t('places.openPhoto', { n: formatNumber(item.id) })"
            @click="openPhoto(item).catch(reportError)"
          >
            <img v-if="item.t" :src="thumbUrl(item.id, item.t)" alt="" /><Icon v-else name="photos" :size="24" />
          </button>
        </div>
      </section>
      <section v-for="group in groups" :key="group.name">
        <div class="section-head">
          <h2>{{ group.name }}</h2>
          <span>{{
            t('places.groupCaption', {
              cities: t(group.items.length === 1 ? 'places.cityCountOne' : 'places.cityCount', {
                n: formatNumber(group.items.length),
              }),
              n: formatNumber(group.items.reduce((total, p) => total + p.count, 0)),
            })
          }}</span>
        </div>
        <CardGrid
          ><PlaceCard
            v-for="place in group.items"
            :key="place.id"
            :place="place"
            :refresh-command="refreshCommand"
            :active="place.id === activePlace?.id"
            @hover="hovered = place.id"
            @preview="openPreview(place)"
            @block="blockPlace(place)"
        /></CardGrid>
      </section>
      <FeatureState
        v-if="loaded && !list.length && !error"
        kind="empty"
        icon="globe"
        :title="t(hadPlaces ? 'places.cleared' : 'places.empty')"
        :text="t(hadPlaces ? 'places.clearedHint' : 'places.emptyHint')"
        ><UButton to="/settings?section=library" color="primary" variant="solid">{{ t('places.addFolder') }}</UButton
        ><UButton to="/blocked" variant="ghost">{{ t('places.manageBlocked') }}</UButton></FeatureState
      >
      <FeatureState
        v-else-if="loaded && list.length && !filtered.length && !error"
        kind="empty"
        icon="places"
        :title="t('places.noCountry')"
        ><UButton @click="country = ''">{{ t('places.showAll') }}</UButton></FeatureState
      >
      <p class="privacy"><Icon name="lock" :size="14" />{{ t('places.privacy') }}</p>
    </div>
  </Page>
</template>

<style scoped>
.places-content {
  --lumen-card-min: 11.75rem;
  display: flex;
  flex-direction: column;
  gap: 1.5rem;
}
.intro,
p,
.offline-label,
.privacy {
  color: var(--ui-text-toned);
  font-size: 0.8125rem;
}
.offline-label,
.privacy {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}
.country-chips {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  overflow-x: auto;
  padding-block: 0.25rem;
}
.country-chips > span {
  color: var(--ui-text-dimmed);
  font-size: 0.75rem;
}
.country-chips > * {
  flex-shrink: 0;
}
.tnum,
.section-head span {
  font-variant-numeric: tabular-nums;
}
.atlas-panel {
  display: grid;
  grid-template-columns: minmax(0, 4fr) minmax(12rem, 1fr);
  align-items: center;
  gap: 1.5rem;
  padding: 1.25rem;
  border: 1px solid var(--ui-border);
  border-radius: var(--lumen-r-lg);
  background: var(--lumen-bg-sunken);
}
.atlas-cover {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 0.5rem;
  min-width: 0;
}
.atlas-cover span {
  color: var(--ui-text-dimmed);
  font-size: 0.75rem;
}
.cover {
  display: grid;
  place-items: center;
  width: 100%;
  aspect-ratio: 2;
  overflow: hidden;
  border: 0;
  border-radius: var(--lumen-r-lg);
  background: var(--lumen-placeholder);
  color: var(--ui-text-dimmed);
  cursor: pointer;
}
img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}
.preview-panel {
  padding: 1.25rem;
  border: 1px solid var(--ui-border);
  border-radius: var(--lumen-r-lg);
  background: var(--lumen-bg-raised);
  box-shadow: var(--lumen-shadow-1);
}
.preview-panel header {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 0.75rem;
  margin-bottom: 1rem;
}
.preview-panel header > div {
  flex: 1;
  min-width: 0;
}
.preview-photos {
  display: grid;
  grid-template-columns: repeat(5, minmax(0, 1fr));
  gap: 0.5rem;
}
.preview-photos button {
  display: grid;
  place-items: center;
  min-width: 1.5rem;
  min-height: 1.5rem;
  aspect-ratio: 1.5;
  overflow: hidden;
  border: 0;
  border-radius: var(--lumen-r-md);
  background: var(--lumen-placeholder);
  cursor: pointer;
}
h2 {
  color: var(--ui-text-highlighted);
  font-weight: 600;
  overflow-wrap: anywhere;
}
.section-head {
  display: flex;
  align-items: baseline;
  gap: 0.75rem;
  margin-bottom: 0.75rem;
}
.section-head span {
  color: var(--ui-text-dimmed);
  font-size: 0.75rem;
}
.atlas-skeleton {
  height: 16rem;
  margin-block: 1rem;
  border-radius: var(--lumen-r-lg);
}
.card-skeleton {
  aspect-ratio: 4 / 3;
  border-radius: var(--lumen-r-lg);
}
@container (max-width: 540px) {
  .atlas-panel {
    grid-template-columns: minmax(0, 1fr);
  }
  .atlas-cover {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
    column-gap: 1rem;
  }
  .atlas-cover .cover {
    grid-row: span 3;
  }
  .offline-label {
    display: none;
  }
}
</style>
