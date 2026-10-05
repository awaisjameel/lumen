<script setup lang="ts">
import UAlert from '@nuxt/ui/components/Alert.vue'
import UBadge from '@nuxt/ui/components/Badge.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UCheckbox from '@nuxt/ui/components/Checkbox.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import USlideover from '@nuxt/ui/components/Slideover.vue'
import UTooltip from '@nuxt/ui/components/Tooltip.vue'
import { createReusableTemplate } from '@vueuse/core'
import { computed, onBeforeUnmount, ref, shallowRef, watch } from 'vue'
import {
  AlbumKind,
  BlockKind,
  DateSource,
  engine,
  INSPECTOR_WIDTH_MAX,
  INSPECTOR_WIDTH_MIN,
  isCancelled,
  Kind,
  MAX_NAME_LEN,
  TagSource,
  TrackKind,
} from '../bridge'
import type { AssetDetail, AssetTag, Face, MediaInspection } from '../bridge'
import { FileTools } from '../media/lazy'
import { errorText, logError, orFallback } from '../shared/errors'
import { block } from '../shared/block'
import { bytes, dateTime, duration, megapixels } from '../shared/format'
import { t } from '../shared/i18n'
import type { MessageKey } from '../shared/i18n'
import { icons } from '../design/icons'
import { runCommand, useCommands } from '../shared/commands'
import FeatureState from '../shared/FeatureState.vue'
import { useLayoutMode } from '../shared/layout'
import MetaRow from '../shared/MetaRow.vue'
import { usePreferences } from '../shared/preferences'
import ResizeHandle from '../shared/ResizeHandle.vue'
import { useLiveReload } from '../shared/liveReload'
import { nameTooLong, trimName } from '../shared/names'
import FaceThumb from '../people/FaceThumb.vue'
import PersonPicker from '../people/PersonPicker.vue'
import { deleteFromDisk } from '../trash/actions'
import { useLibrary } from './store'
import { emptyRule, useAlbums } from '../albums/store'
import type { Sel } from './store'

const props = defineProps<{ sel: Sel | null }>()
const lib = useLibrary()
const mode = useLayoutMode()
const prefs = usePreferences()
const [InspectorContent, ReuseInspectorContent] = createReusableTemplate()
const trigger = document.activeElement
const loading = ref(false)
const albums = useAlbums()
const detail = ref<AssetDetail | null>(null)
const inspection = ref<MediaInspection | null>(null)
const filesOpen = ref(false)
const placeName = ref('') // the city near the photo, once known
const tags = ref<AssetTag[]>([])
const faces = ref<Face[]>([])
const newTag = ref('')
const error = ref('')
const detailError = ref('')
const shownError = computed(() => error.value || detailError.value)
const albumIDs = ref<number[]>([])
// One save per album box at a time, so a failure restores exactly what it changed.
const savingAlbums = shallowRef(new Set<number>())
const newAlbum = ref('')
const creatingAlbum = ref(false)
const manualAlbums = computed(() => albums.list.filter((a) => a.kind === AlbumKind.AlbumManual))
let selectionRevision = 0
let detailRequest = 0
let tagsRequest = 0
let facesRequest = 0
let albumsRequest = 0
let inspectionAbort: AbortController | undefined
onBeforeUnmount(() => {
  inspectionAbort?.abort()
  selectionRevision++
  detailRequest++
  tagsRequest++
  facesRequest++
  albumsRequest++
})

// The photo's details: again when another is selected, or its favourite
// flag changes (two sources, so a grid update that changes neither doesn't
// ask again, nor clear what an edit said went wrong).
watch(
  [() => props.sel?.id, () => lib.itemAt(props.sel)?.f],
  ([id], old) => {
    if (id !== old[0]) {
      inspection.value = null
      error.value = ''
      detail.value = null
      placeName.value = ''
    }
    void loadDetails(id)
  },
  { immediate: true },
)

async function loadDetails(id: number | undefined) {
  inspectionAbort?.abort()
  inspectionAbort = new AbortController()
  const signal = inspectionAbort.signal
  const request = ++detailRequest
  detailError.value = ''
  loading.value = !!id
  if (!id) return
  try {
    const d = await engine.asset(id)
    if (request !== detailRequest) return
    detail.value = d
    if (!lib.inTrash) {
      void engine
        .inspect(id, signal)
        .then((value) => {
          if (request === detailRequest) inspection.value = value
        })
        .catch((e: unknown) => {
          if (!isCancelled(e)) logError(e)
        })
    }
    const place = d.placeId ? await orFallback(engine.place(d.placeId), null) : null
    if (request === detailRequest) placeName.value = place?.name ?? ''
  } catch (e) {
    if (request === detailRequest && !isCancelled(e)) {
      detail.value = null
      placeName.value = ''
      detailError.value = errorText(e)
    }
  } finally {
    if (request === detailRequest) loading.value = false
  }
}
useLiveReload(
  () => loadDetails(props.sel?.id),
  (c) => c.assets,
)

// Tags and faces: load with the photo, and again as the library changes
// while the panel is open (the AI adds tags, finds faces, groups people).
// A trashed photo can't be edited, so its tags, people and albums are not shown.
const loadExtras = (id: number | undefined) =>
  lib.inTrash ? Promise.resolve([]) : Promise.all([loadTags(id), loadFaces(id), loadAlbums(id)])
watch(
  () => props.sel?.id,
  (id) => {
    selectionRevision++
    tags.value = []
    faces.value = []
    albumIDs.value = []
    savingAlbums.value = new Set()
    newTag.value = ''
    newAlbum.value = ''
    creatingAlbum.value = false // one still on its way no longer concerns this photo
    void loadExtras(id)
  },
  { immediate: true },
)
useLiveReload(
  () => loadExtras(props.sel?.id),
  (c) => c.tags || c.people || c.assets || c.thumbs,
)
void albums.load().catch(logError)

async function loadAlbums(id: number | undefined) {
  if (id !== props.sel?.id) return
  const request = ++albumsRequest
  if (!id) {
    albumIDs.value = []
    return
  }
  try {
    const got = await engine.assetAlbums(id)
    if (request === albumsRequest) albumIDs.value = got
  } catch (e) {
    logError(e)
  }
}

function showInAlbum(albumID: number, include: boolean) {
  const others = albumIDs.value.filter((id) => id !== albumID)
  albumIDs.value = include ? [...others, albumID] : others
}

function toggleAlbum(albumID: number, checked: boolean | 'indeterminate') {
  const assetID = detail.value?.id
  if (!assetID || checked === 'indeterminate' || savingAlbums.value.has(albumID)) return
  const include = checked
  const revision = selectionRevision
  showInAlbum(albumID, include)
  savingAlbums.value = new Set(savingAlbums.value).add(albumID)
  void edit(
    () => engine.setAlbumItem(assetID, albumID, include),
    async () => {
      await Promise.all([loadAlbums(assetID), albums.load()])
    },
  ).then((ok) => {
    // Another photo's boxes are its own: this one's outcome is not theirs.
    if (revision !== selectionRevision) return
    if (!ok) showInAlbum(albumID, !include)
    const saving = new Set(savingAlbums.value)
    saving.delete(albumID)
    savingAlbums.value = saving
  })
}

async function createAlbumForItem() {
  const name = trimName(newAlbum.value)
  const assetID = detail.value?.id
  if (!name || nameTooLong(newAlbum.value) || !assetID || creatingAlbum.value) return
  const revision = selectionRevision
  creatingAlbum.value = true
  error.value = ''
  try {
    await engine.createAlbum(name, AlbumKind.AlbumManual, emptyRule(), [assetID]) // with the photo in it, at once
    if (revision === selectionRevision) newAlbum.value = ''
    await Promise.all([loadAlbums(assetID), albums.load()])
  } catch (e) {
    if (revision === selectionRevision) error.value = errorText(e)
  } finally {
    if (revision === selectionRevision) creatingAlbum.value = false
  }
}

async function loadTags(id: number | undefined) {
  if (id !== props.sel?.id) return
  const request = ++tagsRequest
  if (!id) {
    tags.value = []
    return
  }
  try {
    const got = await engine.assetTags(id)
    if (request === tagsRequest) tags.value = got
  } catch (e) {
    logError(e)
  }
}

async function loadFaces(id: number | undefined) {
  if (id !== props.sel?.id) return
  const request = ++facesRequest
  if (!id) {
    faces.value = []
    return
  }
  try {
    const got = await engine.assetFaces(id)
    if (request === facesRequest) faces.value = got
  } catch (e) {
    logError(e)
  }
}

/**
 * Change something about this photo, then show the result with reload (and
 * refresh the grid, which may be a Thing's or a person's). It reports whether
 * the change was made.
 */
async function edit(fn: () => Promise<unknown>, reload: () => Promise<void>): Promise<boolean> {
  const requested = selectionRevision
  error.value = ''
  try {
    await fn()
  } catch (e) {
    if (selectionRevision === requested) error.value = errorText(e)
    return false
  }
  await reload().catch(logError)
  void lib.refresh().catch(logError)
  return true
}

async function addTag() {
  const name = trimName(newTag.value)
  const id = detail.value?.id
  if (!name || nameTooLong(newTag.value) || !id) return
  const requested = selectionRevision
  // What was typed stays until the tag is on, so a failure loses nothing.
  if (
    (await edit(
      () => engine.tagAsset(id, name),
      () => loadTags(id),
    )) &&
    requested === selectionRevision
  ) {
    newTag.value = ''
  }
}

function removeTag(tag: AssetTag) {
  const id = detail.value?.id
  if (id)
    void edit(
      () => engine.untagAsset(id, tag.id),
      () => loadTags(id),
    )
}

async function blockAsset() {
  const item = detail.value
  if (!item) return
  if (await block(BlockKind.BlockAsset, item.id, item.paths[0] || String(item.id), false)) {
    lib.showDetails = false
    lib.viewer = null
    lib.selected = null
  }
}

async function blockThing(tag: AssetTag) {
  if (await block(BlockKind.BlockTag, tag.id, tag.name)) await loadExtras(props.sel?.id)
}

async function blockPerson(face: Face) {
  if (face.person && (await block(BlockKind.BlockPerson, face.person, face.name || t('person.unnamed'))))
    await loadExtras(props.sel?.id)
}

async function blockFace(face: Face) {
  if (await block(BlockKind.BlockFace, face.id, face.name || t('blocked.thisFace'))) await loadExtras(props.sel?.id)
}

async function blockPlace() {
  const id = detail.value?.placeId
  if (id && (await block(BlockKind.BlockPlace, id, placeName.value || t('details.location'))))
    await loadExtras(props.sel?.id)
}

async function blockEvent() {
  const item = detail.value
  if (!item) return
  try {
    const event = await engine.event(item.id) // the event holding this photo; its name titles the dialog
    if (await block(BlockKind.BlockEvent, item.id, event.name)) await loadExtras(props.sel?.id)
  } catch (e) {
    error.value = errorText(e)
  }
}

async function blockFolder(path: string) {
  const cut = Math.max(path.lastIndexOf('\\'), path.lastIndexOf('/'))
  if (cut <= 0) return
  const folder = path.slice(0, cut === 2 && path[1] === ':' ? cut + 1 : cut)
  if (await block(BlockKind.BlockFolder, 0, folder, false, folder)) await loadExtras(props.sel?.id)
}

function nameFace(face: Face, name: string) {
  const id = detail.value?.id
  if (id)
    void edit(
      () => engine.nameFaces([face.id], name),
      () => loadFaces(id),
    )
}

function notThem(face: Face) {
  const id = detail.value?.id
  if (id)
    void edit(
      () => engine.rejectFaces(face.person, [face.id]),
      () => loadFaces(id),
    )
}

const dateFromText: Record<DateSource, MessageKey> = {
  [DateSource.DateFromEXIF]: 'details.dateFrom.exif',
  [DateSource.DateFromFilename]: 'details.dateFrom.filename',
  [DateSource.DateFromFile]: 'details.dateFrom.file',
  [DateSource.$zero]: 'details.dateFrom.file', // never stored; the scanner always sets one
}

function moveToTrash() {
  if (props.sel) void lib.moveToTrash(props.sel)
}
function restore() {
  if (props.sel) void lib.restoreFromTrash(props.sel)
}
function deleteThisFromDisk() {
  if (props.sel) void deleteFromDisk(lib, props.sel)
}

function showInFolder(id: number, path = '') {
  error.value = ''
  engine.showInFolder(id, path).catch((e: unknown) => {
    if (props.sel?.id === id) error.value = errorText(e)
  })
}
async function openCopy(id: number, path: string, choose: boolean) {
  try {
    await engine.openMedia(id, path, choose)
  } catch (e) {
    if (props.sel?.id === id) error.value = errorText(e)
  }
}
async function copyName(path: string) {
  try {
    await navigator.clipboard.writeText(path.split(/[\\/]/).at(-1) ?? path)
  } catch (e) {
    error.value = errorText(e)
  }
}

function returnFocus(e: Event) {
  if (trigger instanceof HTMLElement && trigger.isConnected) {
    e.preventDefault()
    trigger.focus()
  }
}

/**
 * What the file says about itself. A row it has no value for says so; a
 * video is not asked for what only a photo records.
 */
const metadata = computed(() => {
  const d = detail.value
  if (!d) return []
  const i = inspection.value
  const row = (label: MessageKey, value: string | number | undefined) => ({
    key: label as string,
    label: t(label),
    value: value ? String(value) : '',
  })
  const rows = [row('details.camera', d.camera)]
  if (d.video) rows.push(row('details.frameRate', i?.frameRate))
  else
    rows.push(
      row('details.lens', i?.lens),
      row('details.exposure', i?.exposure),
      row('details.aperture', i?.aperture),
      row('details.iso', i?.iso),
      row('details.profile', i?.hasProfile ? i.profile || t('details.profileEmbedded') : undefined),
      row('details.orientation', i?.orientation),
      row('details.pages', i?.pages),
    )
  for (const track of i?.tracks ?? [])
    rows.push({
      key: `track.${track.index}`,
      label: t(track.kind === TrackKind.TrackAudio ? 'viewer.audioTrack' : 'viewer.subtitleTrack'),
      value: `${track.index}: ${[track.codec, track.language, track.title].filter(Boolean).join(' · ')}`,
    })
  return rows
})

const hasDetail = () => detail.value !== null
const editable = () => hasDetail() && !lib.inTrash
useCommands([
  {
    id: 'inspector.files',
    group: 'media',
    title: () => t('files.title'),
    icon: 'files',
    available: editable,
    run: () => {
      filesOpen.value = true
    },
  },
  {
    id: 'inspector.open',
    group: 'media',
    title: () => t('files.open'),
    icon: 'external',
    available: editable,
    run: async () => {
      if (detail.value) await engine.openMedia(detail.value.id)
    },
  },
  {
    id: 'inspector.close',
    group: 'view',
    title: () => t('common.close'),
    icon: 'close',
    available: () => lib.showDetails,
    run: () => (lib.showDetails = false),
  },
  {
    id: 'inspector.favourite',
    group: 'media',
    title: () => t(detail.value?.favourite ? 'details.unfavourite' : 'details.favourite'),
    icon: 'heart',
    available: () => editable() && !lib.viewer,
    run: () => lib.toggleFavourite(props.sel),
  },
  {
    id: 'inspector.trash',
    group: 'media',
    title: () => t('trash.move'),
    icon: 'trash',
    available: () => editable() && !lib.viewer,
    run: moveToTrash,
  },
  {
    id: 'inspector.restore',
    group: 'media',
    title: () => t('trash.restore'),
    icon: 'restore',
    available: () => hasDetail() && lib.inTrash && !lib.viewer,
    run: restore,
  },
  {
    id: 'inspector.delete',
    group: 'media',
    title: () => t('trash.deleteFromDisk'),
    icon: 'trash',
    available: () => hasDetail() && lib.inTrash && !lib.viewer,
    run: deleteThisFromDisk,
  },
  {
    id: 'inspector.block',
    group: 'media',
    title: () => t('blocked.block'),
    icon: 'eye-off',
    available: editable,
    run: blockAsset,
  },
  {
    id: 'inspector.folder',
    group: 'media',
    title: () => t('common.showInFolder'),
    icon: 'folder',
    available: hasDetail,
    run: () => {
      if (detail.value) showInFolder(detail.value.id)
    },
  },
])
</script>

<template>
  <InspectorContent>
    <FeatureState v-if="!detail && shownError" kind="error" :title="shownError" />
    <FeatureState
      v-else-if="!detail"
      :kind="loading ? 'loading' : 'empty'"
      icon="info"
      :title="loading ? t('details.loading') : t('details.none')"
    />
    <div v-else class="inspector-body">
      <section :aria-label="t('details.overview')">
        <h3>{{ t('details.overview') }}</h3>
        <MetaRow
          :label="t('details.taken')"
          icon="calendar"
          :value="detail.takenAt ? dateTime(detail.takenAt) : t('details.missing')"
        />
        <MetaRow :label="t('details.dateSource')" :value="t(dateFromText[detail.dateFrom])" />
        <MetaRow
          :label="t('details.dimensionsLabel')"
          icon="image"
          :value="
            detail.width && detail.height
              ? t('details.dimensions', {
                  w: detail.width,
                  h: detail.height,
                  mp: megapixels(detail.width, detail.height),
                  size: bytes(detail.size),
                })
              : t('details.missing')
          "
        />
        <MetaRow :label="t('details.size')" :value="detail.size ? bytes(detail.size) : t('details.missing')" />
        <MetaRow
          :label="t('details.type')"
          :icon="detail.kind === Kind.KindVideo ? 'video' : 'image'"
          :value="
            t('common.labelValue', {
              label: t(detail.kind === Kind.KindVideo ? 'details.video' : 'details.photo'),
              value: detail.ext || t('details.missing'),
            })
          "
        />
        <template v-if="detail.video">
          <MetaRow
            :label="t('details.duration')"
            icon="clock"
            :value="detail.video.duration ? duration(detail.video.duration) : t('details.missing')"
          />
          <MetaRow :label="t('details.videoCodec')" :value="detail.video.codec || t('details.missing')" />
          <MetaRow :label="t('details.audioCodec')" :value="detail.video.audio || t('details.missing')" />
        </template>
        <UButton
          v-if="!lib.inTrash"
          class="block-event"
          :icon="icons['eye-off']"
          variant="ghost"
          size="sm"
          @click="blockEvent"
        >
          {{ t('blocked.blockEvent') }}</UButton
        >
      </section>

      <section :aria-label="t('details.metadata')">
        <h3>{{ t('details.metadata') }}</h3>
        <MetaRow
          v-for="row in metadata"
          :key="row.key"
          :copyable="!!row.value"
          :label="row.label"
          :value="row.value || t('details.missing')"
        />
      </section>

      <h3 v-if="!lib.inTrash">{{ t('details.organisation') }}</h3>

      <section :aria-label="t('details.location')">
        <h3>{{ t('details.location') }}</h3>
        <MetaRow :label="t('details.location')" icon="places" :value="placeName || t('details.missing')" />
        <MetaRow
          :label="t('details.coordinates')"
          :value="
            detail.lat !== null && detail.lon !== null
              ? t('details.coordinateValue', { lat: detail.lat.toFixed(5), lon: detail.lon.toFixed(5) })
              : t('details.missing')
          "
        />
        <UButton
          v-if="detail.placeId && !lib.inTrash"
          :icon="icons['eye-off']"
          variant="ghost"
          size="sm"
          @click="blockPlace"
        >
          {{ t('blocked.blockPlace') }}</UButton
        >
      </section>

      <section v-if="!lib.inTrash" :aria-label="t('details.people')">
        <h3>{{ t('details.people') }}</h3>
        <ul v-if="faces.length" class="faces">
          <li v-for="f in faces" :key="f.id">
            <FaceThumb :face="f" :size="36" round />
            <template v-if="f.person">
              <UButton
                :to="{ name: 'person', params: { id: f.person } }"
                class="who"
                variant="link"
                :title="f.name || t('person.unnamed')"
                :ui="{ label: 'truncate' }"
                >{{ f.name || t('person.unnamed') }}
              </UButton>
              <UTooltip :text="t('details.notThisPerson', { name: f.name || t('person.unnamed') })">
                <UButton
                  :icon="icons.close"
                  :aria-label="t('details.notThisPerson', { name: f.name || t('person.unnamed') })"
                  variant="ghost"
                  size="xs"
                  square
                  @click="notThem(f)"
                />
              </UTooltip>
              <UTooltip :text="t('blocked.confirmTitle', { name: f.name || t('person.unnamed') })">
                <UButton
                  :icon="icons['eye-off']"
                  :aria-label="t('blocked.confirmTitle', { name: f.name || t('person.unnamed') })"
                  variant="ghost"
                  size="xs"
                  square
                  @click="blockPerson(f)"
                />
              </UTooltip>
            </template>
            <PersonPicker v-else class="who" :placeholder="t('details.whoIsThis')" @pick="(n) => nameFace(f, n)" />
            <UButton class="block-face" :icon="icons['eye-off']" variant="ghost" size="sm" @click="blockFace(f)">{{
              t('blocked.blockFace')
            }}</UButton>
          </li>
        </ul>
        <p v-else class="text-sm text-muted">{{ t('details.noPeople') }}</p>
      </section>

      <section v-if="!lib.inTrash" :aria-label="t('details.tags')">
        <h3>{{ t('details.tags') }}</h3>
        <ul v-if="tags.length" class="tags">
          <li v-for="tag in tags" :key="tag.id">
            <UButton
              :to="{ name: 'thing', params: { id: tag.id } }"
              variant="link"
              size="sm"
              class="tag-name"
              :title="tag.name"
              :ui="{ label: 'truncate' }"
              >{{ tag.name }}</UButton
            >
            <UTooltip :text="t('details.removeTag', { name: tag.name })">
              <UButton
                :icon="icons.close"
                :aria-label="t('details.removeTag', { name: tag.name })"
                variant="ghost"
                size="xs"
                square
                @click="removeTag(tag)"
              />
            </UTooltip>
            <UTooltip :text="t('blocked.confirmTitle', { name: tag.name })">
              <UButton
                :icon="icons['eye-off']"
                :aria-label="t('blocked.confirmTitle', { name: tag.name })"
                variant="ghost"
                size="xs"
                square
                @click="blockThing(tag)"
              />
            </UTooltip>
            <UBadge color="neutral" variant="subtle" size="sm">{{
              t(tag.source === TagSource.TagByAI ? 'details.byAI' : 'details.byYou')
            }}</UBadge>
          </li>
        </ul>
        <p v-else class="text-sm text-muted">{{ t('details.noTags') }}</p>
        <UInput
          v-model="newTag"
          class="w-full"
          data-tag-draft
          :placeholder="t('details.addTag')"
          :aria-label="t('details.addTag')"
          :aria-invalid="nameTooLong(newTag) || undefined"
          @keydown.enter="addTag"
        />
        <UAlert
          v-if="nameTooLong(newTag)"
          role="alert"
          color="error"
          variant="subtle"
          :title="t('error.invalidName', { max: MAX_NAME_LEN })"
        />
      </section>

      <section v-if="!lib.inTrash" :aria-label="t('nav.albums')">
        <h3>{{ t('nav.albums') }}</h3>
        <UCheckbox
          v-for="album in manualAlbums"
          :key="album.id"
          class="album-option"
          :label="album.name"
          :model-value="albumIDs.includes(album.id)"
          :disabled="savingAlbums.has(album.id)"
          :ui="{ label: 'break-words', wrapper: 'min-w-0' }"
          @update:model-value="toggleAlbum(album.id, $event)"
        />
        <p v-if="!manualAlbums.length" class="text-sm text-muted">{{ t('details.noAlbums') }}</p>
        <form class="album-new" @submit.prevent="createAlbumForItem">
          <UInput
            v-model="newAlbum"
            class="min-w-0 flex-1"
            :placeholder="t('albums.createForItem')"
            :aria-label="t('albums.createForItem')"
            :aria-invalid="nameTooLong(newAlbum) || undefined"
          />
          <UTooltip :text="t('albums.createForItem')">
            <UButton
              type="submit"
              :icon="icons.plus"
              :aria-label="t('albums.createForItem')"
              :loading="creatingAlbum"
              :disabled="creatingAlbum || !trimName(newAlbum) || nameTooLong(newAlbum)"
              square
            />
          </UTooltip>
        </form>
        <UAlert
          v-if="nameTooLong(newAlbum)"
          role="alert"
          color="error"
          variant="subtle"
          :title="t('error.invalidName', { max: MAX_NAME_LEN })"
        />
      </section>

      <section :aria-label="t('details.files')">
        <h3>{{ t('details.files') }}</h3>
        <p v-if="detail.paths.length > 1" class="text-sm text-muted">
          {{ t('details.copies', { n: detail.paths.length }) }}
        </p>
        <MetaRow
          v-for="p in detail.paths"
          :key="p"
          copyable
          :label="t('details.path')"
          icon="files"
          class="path"
          :value="p"
        >
          <template #action>
            <div class="flex flex-col items-end gap-1">
              <UTooltip v-if="!lib.inTrash" :text="t('files.open')">
                <UButton
                  :icon="icons.external"
                  :aria-label="t('files.open')"
                  variant="ghost"
                  size="sm"
                  square
                  @click="openCopy(detail.id, p, false)"
                />
              </UTooltip>
              <UTooltip v-if="!lib.inTrash" :text="t('files.openWith')">
                <UButton
                  :icon="icons.more"
                  :aria-label="t('files.openWith')"
                  variant="ghost"
                  size="sm"
                  square
                  @click="openCopy(detail.id, p, true)"
                />
              </UTooltip>
              <UTooltip :text="t('files.copyName')">
                <UButton
                  :icon="icons.copy"
                  :aria-label="t('files.copyName')"
                  variant="ghost"
                  size="sm"
                  square
                  @click="copyName(p)"
                />
              </UTooltip>
              <UTooltip :text="t('common.showInFolder')">
                <UButton
                  :icon="icons.folder"
                  :aria-label="t('common.showInFolder')"
                  variant="ghost"
                  size="sm"
                  square
                  @click="showInFolder(detail.id, p)"
                />
              </UTooltip>
              <UTooltip v-if="!lib.inTrash" :text="t('blocked.blockFolder', { path: p })">
                <UButton
                  data-block-folder
                  :icon="icons['eye-off']"
                  :aria-label="t('blocked.blockFolder', { path: p })"
                  variant="ghost"
                  size="sm"
                  square
                  @click="blockFolder(p)"
                />
              </UTooltip>
            </div>
          </template>
        </MetaRow>
        <p v-if="!detail.paths.length" class="text-sm text-muted">{{ t('details.missing') }}</p>
      </section>

      <div class="actions">
        <template v-if="lib.inTrash">
          <UButton :icon="icons.restore" @click="runCommand(lib.viewer ? 'viewer.restore' : 'inspector.restore')">{{
            t('trash.restore')
          }}</UButton>
          <UButton :icon="icons.trash" @click="runCommand(lib.viewer ? 'viewer.delete' : 'inspector.delete')">{{
            t('trash.deleteFromDisk')
          }}</UButton>
        </template>
        <template v-else>
          <UButton :icon="icons.files" @click="runCommand('inspector.files')">{{ t('files.title') }}</UButton>
          <UButton :icon="icons.external" @click="runCommand('inspector.open')">{{ t('files.open') }}</UButton>
          <UButton :icon="icons['eye-off']" @click="runCommand('inspector.block')">{{ t('blocked.block') }}</UButton>
          <UButton
            :icon="icons.heart"
            :aria-pressed="detail.favourite"
            @click="runCommand(lib.viewer ? 'viewer.favourite' : 'inspector.favourite')"
            >{{ detail.favourite ? t('details.unfavourite') : t('details.favourite') }}</UButton
          >
          <UButton :icon="icons.trash" @click="runCommand(lib.viewer ? 'viewer.trash' : 'inspector.trash')">{{
            t('trash.move')
          }}</UButton>
        </template>
        <UButton :icon="icons.folder" @click="runCommand('inspector.folder')">{{ t('common.showInFolder') }}</UButton>
      </div>
      <UAlert v-if="shownError" role="alert" color="error" variant="subtle" :title="shownError" />
    </div>
  </InspectorContent>
  <FileTools v-model:open="filesOpen" :ids="detail ? [detail.id] : []" @changed="loadDetails(props.sel?.id)" />

  <aside
    v-if="mode !== 'narrow'"
    class="inspector lumen-surface"
    :aria-label="t('details.title')"
    :style="{ width: `${prefs.prefs.inspectorWidth}px` }"
  >
    <ResizeHandle
      edge="left"
      :width="prefs.prefs.inspectorWidth"
      :min="INSPECTOR_WIDTH_MIN"
      :max="INSPECTOR_WIDTH_MAX"
      :label="t('shell.resizeInspector')"
      @resize="(inspectorWidth) => void prefs.set({ inspectorWidth })"
    />
    <header class="inspector-header">
      <h2>{{ t('details.title') }}</h2>
      <UTooltip :text="t('common.close')">
        <UButton
          :icon="icons.close"
          :aria-label="t('common.close')"
          variant="ghost"
          square
          @click="lib.showDetails = false"
        />
      </UTooltip>
    </header>
    <div class="inspector-scroll">
      <ReuseInspectorContent />
    </div>
  </aside>
  <USlideover
    v-else
    :open="true"
    side="right"
    :title="t('details.title')"
    :close="false"
    :content="{ onCloseAutoFocus: returnFocus }"
    :ui="{ content: 'w-full max-w-md', body: 'p-0 sm:p-0' }"
    @update:open="
      (open) => {
        if (!open) lib.showDetails = false
      }
    "
  >
    <template #close>
      <UButton :icon="icons.close" :aria-label="t('common.close')" variant="ghost" square />
    </template>
    <template #body>
      <ReuseInspectorContent />
    </template>
  </USlideover>
</template>

<style scoped>
.inspector {
  position: relative;
  display: flex;
  flex-direction: column;
  flex-shrink: 0;
  min-height: 0;
  max-width: 100%;
  border-left: 1px solid var(--ui-border);
  background: var(--ui-bg);
  color: var(--ui-text);
  user-select: text;
}

.inspector-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-shrink: 0;
  min-height: var(--lumen-topbar-h);
  padding: 0.375rem 0.75rem 0.375rem 1.25rem;
}

h2 {
  margin: 0;
  font-size: 1rem;
  font-weight: 600;
  color: var(--ui-text-highlighted);
}

.inspector-scroll {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
}

.inspector-body {
  display: flex;
  flex-direction: column;
  gap: 1.125rem;
  padding: 0.5rem 1.25rem 1.25rem;
}

section {
  display: flex;
  flex-direction: column;
  align-items: stretch;
  gap: 0.375rem;
  min-width: 0;
}

h3 {
  margin: 0;
  color: var(--ui-text-highlighted);
  font-size: 0.8125rem;
  font-weight: 600;
}

p {
  margin: 0;
}

.album-new {
  display: flex;
  align-items: center;
  gap: 0.375rem;
}

.actions {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
}

.tags,
.faces {
  margin: 0;
  padding: 0;
  list-style: none;
}

.tags {
  display: flex;
  flex-direction: column;
  gap: 0.375rem;
}

.tags li {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.25rem;
  min-width: 0;
}

.tag-name {
  flex: 1;
  min-width: 0;
}

.tags li > :last-child {
  flex-basis: 100%;
  width: fit-content;
}

.faces {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

.faces li {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.375rem;
}

.who {
  flex: 1;
  min-width: 0;
}

.block-face {
  flex: 0 0 calc(100% - 2.625rem);
  margin-left: 2.625rem;
  justify-content: flex-start;
}
</style>
