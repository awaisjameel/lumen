<script setup lang="ts">
import UButton from '@nuxt/ui/components/Button.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import { computed, nextTick, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { AlbumKind, BlockKind, engine, Filter, TagKind, thumbUrl } from '../bridge'
import type { Event, Place } from '../bridge'
import { useAlbums } from '../albums/store'
import CoverMosaic from '../shared/CoverMosaic.vue'
import { useThings } from '../things/store'
import CollectionHero from '../shared/CollectionHero.vue'
import Icon from '../shared/Icon.vue'
import { block } from '../shared/block'
import { errorText, reportError } from '../shared/errors'
import { daySpan } from '../shared/format'
import { t } from '../shared/i18n'
import { nameTooLong, trimName } from '../shared/names'
import { notify } from '../shared/notify'
import { icons } from '../design/icons'
import AlbumPhotoPicker from './AlbumPhotoPicker.vue'

const props = defineProps<{
  kind: 'thing' | 'place' | 'event' | 'album' | 'smart' | 'favourites'
  id: number
  name: string
  count: number
  days: string[]
  group?: Event | Place | null
}>()
const emit = defineEmits<{ changed: [] }>()
const router = useRouter()
const things = useThings()
const albums = useAlbums()
const thing = computed(() => things.byId.get(props.id))
const album = computed(() => albums.byId.get(props.id))
const event = computed(() => (props.group && 'startAt' in props.group ? props.group : null))
const place = computed(() => (props.group && 'country' in props.group ? props.group : null))
const cover = computed(() => (props.kind === 'thing' ? thing.value : props.group))
const renaming = ref(false)
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
const meta = computed(() => {
  const dates = props.days.filter(Boolean)
  const first = dates.at(-1),
    last = dates[0]
  return [
    first && last ? daySpan(first, last, props.kind === 'event') : '',
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
    error.value = ''
    picker.value = false
  },
)
async function rename() {
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
  const rule = album.value ? { ...album.value.rule } : null
  return async (name: string) => {
    if (kind === 'thing') await engine.renameThing(id, name)
    else if (kind === 'event') await engine.setEventName(id, name)
    else if (rule) await engine.updateAlbum(id, name, rule)
  }
}
async function saveName(reset = false) {
  const name = reset ? '' : trimName(draft.value)
  if (busy.value) return
  if (nameTooLong(name) || (!resettable.value && !name)) {
    error.value = t('collection.nameInvalid')
    return
  }
  const previous = props.name
  const writeName = nameWriter()
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
  void router.push({ name: 'albums', query: { edit: props.id } })
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
      <UButton v-if="kind === 'album' && album?.kind === AlbumKind.AlbumManual" :icon="icons.plus" @click="addPhotos">{{
        t('collection.addPhotos')
      }}</UButton>
      <UButton v-if="kind === 'smart'" @click="editRule">{{ t('collection.editRule') }}</UButton>
      <UButton v-if="kind === 'place'" :icon="icons.map" :to="{ name: 'places', query: { place: id } }">{{
        t('collection.openMap')
      }}</UButton>
      <UButton v-if="kind !== 'favourites'" color="neutral" variant="ghost" :disabled="busy" @click="blockCollection">{{
        t('blocked.block')
      }}</UButton>
    </template>
  </CollectionHero>
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
