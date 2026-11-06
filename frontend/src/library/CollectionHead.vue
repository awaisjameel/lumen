<script setup lang="ts">
import { destination } from '../shared/navigation/contract'
import UButton from '../shared/navigation/NavButton.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import { computed, nextTick, onBeforeUnmount, ref, shallowRef, watch } from 'vue'
import RouterLink from '../shared/navigation/NavLink.vue'
import { useNavigation } from '../shared/navigation/router'
import { AlbumKind, BlockKind, engine, Filter, isCancelled, TagKind, thumbUrl } from '../bridge'
import type { Event, NamedPlace, Place, View } from '../bridge'
import { useAlbums } from '../albums/store'
import CoverMosaic from '../shared/CoverMosaic.vue'
import { useThings } from '../things/store'
import CollectionHero from '../shared/CollectionHero.vue'
import Icon from '../shared/Icon.vue'
import { block, onBlocksChanged } from '../shared/block'
import { errorText, logError, reportError } from '../shared/errors'
import { spanOfDays } from '../shared/format'
import { t } from '../shared/i18n'
import { nameTooLong, trimName } from '../shared/names'
import { notify } from '../shared/notify'
import { icons } from '../design/icons'
import AlbumPhotoPicker from './AlbumPhotoPicker.vue'
import { coalescedLoad } from '../shared/listStore'
import { useLibrary } from './store'

const props = defineProps<{
  kind: 'thing' | 'place' | 'event' | 'album' | 'smart' | 'favourites'
  id: number
  name: string
  count: number
  revision: number
  days: string[]
  group?: Event | Place | null
}>()
const emit = defineEmits<{ changed: [] }>()
const router = useNavigation()
const things = useThings()
const albums = useAlbums()
const library = useLibrary()
const thing = computed(() => things.byId.get(props.id))
const album = computed(() => albums.byId.get(props.id))
const event = computed(() => (props.group && 'startAt' in props.group ? props.group : null))
const place = computed(() => (props.group && 'country' in props.group ? props.group : null))
const cover = computed(() => (props.kind === 'thing' ? thing.value : props.group))
const renaming = ref(false)
let writeDraftName: ((name: string) => Promise<void>) | undefined
const draft = ref('')
const error = ref('')
const busy = ref(false)
const picker = ref(false)
const field = ref<{ inputRef: HTMLInputElement | null } | null>(null)
const canRename = computed(() => !['favourites', 'place'].includes(props.kind))
const resettable = computed(
  () => props.kind === 'event' || (props.kind === 'thing' && thing.value?.kind === TagKind.TagBuiltin),
)
const pinned = computed(() => (props.kind === 'thing' ? thing.value?.pinned : event.value?.pinned))

// Where most of an album's, a Thing's or an event's photos were taken; a
// place is itself one, and Favourites says what it is instead.
const view = computed((): View | null => {
  if (props.kind === 'thing') return { filter: Filter.FilterAll, tag: props.id }
  if (props.kind === 'album' || props.kind === 'smart') return { filter: Filter.FilterAll, album: props.id }
  if (props.kind === 'event') return { filter: Filter.FilterAll, event: props.id }
  return null
})
const places = shallowRef<NamedPlace[]>([])
const placesError = ref('')
const placesPending = ref(0)
let placesAsked = 0
let placesActive = true
let placesAbort: AbortController | undefined
let placesTimer: ReturnType<typeof setTimeout> | undefined
// A privacy refresh also emits an ordinary library change. Let same-view
// reads settle under their coalesced owner instead of aborting them again.
// Identity, privacy and unmount still end the underlying read's context.
function clearPlaces(cancel = false) {
  placesAsked++
  if (cancel) placesAbort?.abort()
  places.value = []
  placesError.value = ''
}
const readPlaces = coalescedLoad(
  async () => {
    const asked = placesAsked
    const scope = view.value
    const request = new AbortController()
    placesAbort = request
    try {
      const top = placesActive && scope ? await engine.topPlaces(scope, request.signal) : []
      return { asked, top }
    } catch (e) {
      if (!placesActive || asked !== placesAsked || isCancelled(e)) return { asked, top: [] }
      placesError.value = errorText(e)
      throw e
    }
  },
  ({ asked, top }) => {
    if (placesActive && asked === placesAsked) places.value = top
  },
)
async function loadPlaces(cancel = false) {
  clearTimeout(placesTimer)
  placesTimer = undefined
  clearPlaces(cancel)
  placesPending.value++
  try {
    await readPlaces()
  } finally {
    placesPending.value--
  }
}
// Album kind is presentation: clearing its list during privacy refresh can
// switch smart/manual temporarily without changing the wire view's identity.
watch(
  () => JSON.stringify(view.value),
  () => void loadPlaces(true).catch(logError),
  { immediate: true },
)
// The library owns change admission. Reuse its revision rather than adding
// another event subscription or guessing changes from count/day equality.
watch(
  [() => props.revision, () => props.count],
  () => {
    clearPlaces()
    if (!view.value || placesTimer !== undefined) return
    placesTimer = setTimeout(() => void loadPlaces().catch(logError), 3000)
  },
  { flush: 'sync' },
)
onBeforeUnmount(onBlocksChanged(() => loadPlaces(true)))
// Explicit corrections and Undo reconcile immediately; background bursts
// share the trailing read above. Both paths use the same coalesced owner.
onBeforeUnmount(library.onReconcile(loadPlaces))
onBeforeUnmount(() => {
  placesActive = false
  clearPlaces(true)
  clearTimeout(placesTimer)
})

const meta = computed(() => {
  return [
    spanOfDays(props.days, props.kind === 'event'),
    t('collection.items', { n: props.count }),
    place.value
      ? [place.value.region, place.value.country].filter(Boolean).join(', ')
      : props.kind === 'favourites'
        ? t('collection.favouritesHint')
        : '',
  ]
    .filter(Boolean)
    .join(' · ')
})
const eyebrow = computed(() =>
  props.kind === 'thing'
    ? t(thing.value?.kind === TagKind.TagBuiltin ? 'collection.suggested' : 'collection.added')
    : t(`collection.kind.${props.kind}`),
)
watch(
  () => [props.kind, props.id],
  () => {
    renaming.value = false
    writeDraftName = undefined
    error.value = ''
    picker.value = false
  },
)
async function rename() {
  writeDraftName = nameWriter()
  draft.value = props.name
  error.value = ''
  renaming.value = true
  await nextTick()
  field.value?.inputRef?.focus()
  field.value?.inputRef?.select()
}
async function refresh() {
  if (props.kind === 'thing') await things.load()
  if (props.kind === 'album' || props.kind === 'smart') await albums.load()
  emit('changed')
}
function nameWriter() {
  const id = props.id
  const kind = props.kind
  let version = album.value?.version
  return async (name: string) => {
    if (kind === 'thing') await engine.renameThing(id, name)
    else if (kind === 'event') await engine.setEventName(id, name)
    else {
      if (!version) throw new Error(t('error.notFound'))
      version = await engine.saveAlbum({ id, version, name, rule: null })
    }
  }
}
async function saveName(reset = false) {
  const name = reset ? '' : trimName(draft.value)
  if (busy.value) return
  if (nameTooLong(name) || (!resettable.value && !name)) {
    error.value = t('collection.nameInvalid')
    return
  }
  // An automatic title has no override; storing its displayed name on Undo
  // would stop it following later date or location corrections.
  const previous = props.kind === 'event' ? (event.value?.nameOverride ?? '') : props.name
  const writeName = writeDraftName ?? nameWriter()
  busy.value = true
  error.value = ''
  try {
    await writeName(name)
    renaming.value = false
    await refresh()
    notify({
      id: 'collection.name',
      kind: 'success',
      title: t(reset ? 'collection.nameReset' : 'collection.renamed'),
      actions: [
        {
          label: t('settings.undo'),
          run: () => {
            void writeName(previous).then(refresh).catch(reportError)
          },
        },
      ],
    })
  } catch (e) {
    error.value = errorText(e)
  } finally {
    busy.value = false
  }
}
async function togglePin() {
  if (busy.value) return
  const previous = !!pinned.value
  const id = props.id
  const kind = props.kind
  const name = props.name
  const write = (value: boolean) =>
    kind === 'thing' ? engine.setThingPinned(id, value) : engine.setEventPinned(id, value)
  busy.value = true
  error.value = ''
  try {
    await write(!previous)
    await refresh()
    notify({
      id: 'collection.pin',
      kind: 'success',
      title: t(previous ? 'collection.unpinned' : 'collection.pinned', { name }),
      actions: [
        {
          label: t('settings.undo'),
          run: () => {
            void write(previous).then(refresh).catch(reportError)
          },
        },
      ],
    })
  } catch (e) {
    error.value = errorText(e)
  } finally {
    busy.value = false
  }
}
function editRule() {
  void router.push(destination({ name: 'albums', query: { edit: props.id } }))
}
function addPhotos() {
  picker.value = true
}
async function blockCollection() {
  const kind =
    props.kind === 'thing'
      ? BlockKind.BlockTag
      : props.kind === 'event'
        ? BlockKind.BlockEvent
        : props.kind === 'place'
          ? BlockKind.BlockPlace
          : BlockKind.BlockAlbum
  if (await block(kind, props.id, props.name)) emit('changed')
}
defineExpose({ addPhotos, editRule })
</script>

<template>
  <CollectionHero :kind="eyebrow" :name="name" :meta="meta">
    <template #meta>
      {{ meta }}
      <template v-for="p in places" :key="p.id">
        <span aria-hidden="true"> · </span>
        <RouterLink
          :to="destination({ name: 'place', params: { id: p.id } })"
          class="underline decoration-dotted underline-offset-2 hover:text-highlighted"
          data-collection-place
          ><bdi>{{ p.name }}</bdi></RouterLink
        >
      </template>
    </template>
    <template #cover>
      <CoverMosaic
        v-if="album && (kind === 'album' || kind === 'smart')"
        :view="{ filter: Filter.FilterAll, album: album.id }"
        :count="album.count"
        :cover="album.cover"
        :cover-thumb="album.coverThumb"
        :icon="album.kind === AlbumKind.AlbumSmart ? 'smart' : 'albums'"
      />
      <CoverMosaic
        v-else-if="kind === 'favourites'"
        :view="{ filter: Filter.FilterFavourites }"
        :count="count"
        icon="heart"
      />
      <img v-else-if="cover?.cover && cover.coverThumb" :src="thumbUrl(cover.cover, cover.coverThumb)" alt="" />
      <span v-else class="cover-icon"><Icon :name="kind === 'place' ? 'map' : 'photos'" :size="32" /></span>
    </template>
    <template v-if="renaming" #name>
      <form class="rename" @submit.prevent="saveName()" @keydown.esc.stop="renaming = false">
        <UInput ref="field" v-model="draft" :aria-label="t('collection.name')" :disabled="busy" />
        <UButton type="submit" :loading="busy">{{ t('collection.saveName') }}</UButton>
        <UButton color="neutral" variant="ghost" :disabled="busy" @click="renaming = false">{{
          t('common.cancel')
        }}</UButton>
      </form>
    </template>
    <template #actions>
      <UButton
        v-if="kind === 'thing' || kind === 'event'"
        :icon="icons.pin"
        color="neutral"
        variant="outline"
        :aria-pressed="!!pinned"
        :disabled="busy"
        @click="togglePin"
        >{{ t(pinned ? 'collection.unpin' : 'collection.pin') }}</UButton
      >
      <UButton v-if="canRename && !renaming" color="neutral" variant="ghost" :disabled="busy" @click="rename">{{
        t('collection.rename')
      }}</UButton>
      <UButton v-if="resettable" color="neutral" variant="ghost" :disabled="busy" @click="saveName(true)">{{
        t('collection.resetName')
      }}</UButton>
      <UButton
        v-if="kind === 'album' && album?.kind === AlbumKind.AlbumManual"
        :icon="icons.plus"
        color="primary"
        variant="solid"
        @click="addPhotos"
        >{{ t('collection.addPhotos') }}</UButton
      >
      <UButton v-if="kind === 'smart'" color="primary" variant="solid" @click="editRule">{{
        t('collection.editRule')
      }}</UButton>
      <UButton
        v-if="kind === 'place'"
        :icon="icons.map"
        color="primary"
        variant="solid"
        :to="destination({ name: 'places', query: { place: id } })"
        >{{ t('collection.openMap') }}</UButton
      >
      <UButton v-if="kind !== 'favourites'" color="neutral" variant="ghost" :disabled="busy" @click="blockCollection">{{
        t('collection.block')
      }}</UButton>
    </template>
  </CollectionHero>
  <p v-if="placesError" role="alert" class="error" data-collection-places-error>
    {{ placesError }}
    <UButton color="neutral" variant="outline" :loading="placesPending > 0" @click="loadPlaces().catch(logError)">{{
      t('common.retry')
    }}</UButton>
  </p>
  <p v-if="error" role="alert" class="error">{{ error }}</p>
  <AlbumPhotoPicker v-if="picker" v-model:open="picker" :album-id="id" :name="name" @changed="refresh" />
</template>

<style scoped>
.cover-icon {
  display: grid;
  place-items: center;
  height: 100%;
  color: var(--ui-text-dimmed);
}
.rename {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.5rem;
}
.error {
  margin: 0.25rem var(--lumen-page-x);
  color: var(--ui-error);
  font-size: 0.8125rem;
}
</style>
