<script setup lang="ts">
import IconButton from '../shared/IconButton.vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UCheckbox from '@nuxt/ui/components/Checkbox.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import UModal from '@nuxt/ui/components/Modal.vue'
import USlideover from '@nuxt/ui/components/Slideover.vue'
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
  thumbUrl,
} from '../bridge'
import type { AssetDetail, AssetTag, Face, MediaInspection } from '../bridge'
import { FileTools } from '../media/lazy'
import { errorText, logError, orFallback, reportError } from '../shared/errors'
import { block } from '../shared/block'
import { bytes, dateTime, duration, megapixels, fileName } from '../shared/format'
import { t, formatNumber } from '../shared/i18n'
import { notify } from '../shared/notify'
import DateDialog from './details/DateDialog.vue'
import PlaceDialog from './details/PlaceDialog.vue'
import AlbumPicker from './details/AlbumPicker.vue'
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
const dateOpen = ref(false)
const placeOpen = ref(false)
const tagOpen = ref(false)
const albumOpen = ref(false)
const createOpen = ref(false)
const faceOpen = ref<Face | null>(null)
const handoff = shallowRef<{ title: string; path: string; run: () => Promise<void> } | null>(null)
const handoffError = ref('')
const handoffBusy = ref(false)
const faceBusy = ref(false)
const tagBusy = ref(false)
const multiple = computed(() => !lib.viewer && lib.selectedIDs.size > 1)
const previewUrl = computed(() => (props.sel ? thumbUrl(props.sel.id, lib.itemAt(props.sel)?.t ?? 0) : undefined))
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
      tags.value = []
      faces.value = []
      albumIDs.value = []
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
    tagOpen.value = false
    createOpen.value = false
    dateOpen.value = false
    placeOpen.value = false
    albumOpen.value = false
    faceOpen.value = null
    faceBusy.value = false
    tagBusy.value = false
    handoff.value = null
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
    else
      notify({
        id: `album-${assetID}-${albumID}`,
        kind: 'success',
        title: t('details.album.saved'),
        actions: [
          {
            label: t('settings.undo'),
            run: () =>
              void engine
                .setAlbumItem(assetID, albumID, !include)
                .then(() => loadAlbums(assetID))
                .then(() => albums.load())
                .catch(reportError),
          },
        ],
      })
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
    if (revision === selectionRevision) {
      newAlbum.value = ''
      createOpen.value = false
    }
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
  if (!name || nameTooLong(newTag.value) || !id || tagBusy.value) return
  tagBusy.value = true
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
    tagOpen.value = false
  }
  if (requested === selectionRevision) tagBusy.value = false
}

async function removeTag(tag: AssetTag) {
  const id = detail.value?.id
  if (!id) return
  if (
    await edit(
      () => engine.untagAsset(id, tag.id),
      () => loadTags(id),
    )
  )
    notify({
      id: `tag-${id}-${tag.id}`,
      kind: 'success',
      title: t('details.tag.removed', { name: tag.name }),
      actions: [
        {
          label: t('settings.undo'),
          run: () =>
            void engine
              .tagAsset(id, tag.name)
              .then(() => loadTags(id))
              .then(() => lib.refresh())
              .catch(reportError),
        },
      ],
    })
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

async function nameFace(face: Face, name: string) {
  const id = detail.value?.id
  if (!id || faceBusy.value) return
  const requested = selectionRevision
  faceBusy.value = true
  if (
    (await edit(
      () => engine.nameFaces([face.id], name),
      () => loadFaces(id),
    )) &&
    requested === selectionRevision
  )
    faceOpen.value = null
  if (requested === selectionRevision) faceBusy.value = false
}

async function notThem(face: Face) {
  const id = detail.value?.id
  if (!id) return
  if (
    await edit(
      () => engine.rejectFaces(face.person, [face.id]),
      () => loadFaces(id),
    )
  )
    notify({
      id: `face-${face.id}`,
      kind: 'success',
      title: t('details.person.corrected', { name: face.name || t('person.unnamed') }),
      actions: face.name
        ? [
            {
              label: t('settings.undo'),
              run: () =>
                void engine
                  .nameFaces([face.id], face.name)
                  .then(() => loadFaces(id))
                  .then(() => lib.refresh())
                  .catch(reportError),
            },
          ]
        : [],
    })
}

async function correctionsChanged() {
  await loadDetails(props.sel?.id)
  if (detail.value) await loadExtras(props.sel?.id)
  void lib.refresh().catch(logError)
}

const dateFromText: Record<DateSource, MessageKey> = {
  [DateSource.DateFromUser]: 'details.dateFrom.user',
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
  handoff.value = {
    title: t('common.showInFolder'),
    path: path || detail.value?.paths[0] || '',
    run: () => engine.showInFolder(id, path),
  }
  void runHandoff()
}
function openCopy(id: number, path: string, choose: boolean) {
  handoff.value = {
    title: t(choose ? 'files.openWith' : 'files.open'),
    path,
    run: () => engine.openMedia(id, path, choose),
  }
  void runHandoff()
}
async function runHandoff() {
  const request = handoff.value
  if (!request || handoffBusy.value) return
  handoffBusy.value = true
  handoffError.value = ''
  try {
    await request.run()
  } catch (e) {
    if (handoff.value === request) handoffError.value = errorText(e)
  } finally {
    handoffBusy.value = false
  }
}
async function copyText(value: string) {
  try {
    await navigator.clipboard.writeText(value)
    notify({ id: 'details-copy', kind: 'success', title: t('details.copied') })
  } catch (e) {
    error.value = errorText(e)
  }
}
function copyName(path: string) {
  return copyText(fileName(path))
}
function copyMetadata() {
  const d = detail.value
  if (!d) return
  void copyText(
    [
      `${t('details.taken')}: ${dateTime(d.takenAt)}`,
      `${t('details.dateSource')}: ${t(dateFromText[d.dateFrom])}`,
      `${t('details.dimensionsLabel')}: ${d.width} × ${d.height}`,
      `${t('details.size')}: ${bytes(d.size)}`,
      `${t('details.type')}: ${d.ext}`,
      ...metadata.value.map((row) => `${row.label}: ${row.value || t('details.missing')}`),
      ...d.paths,
    ].join(String.fromCharCode(10)),
  )
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
  const row = (
    label: MessageKey,
    value: string | number | undefined,
  ): { key: string; label: string; value: string } => ({
    key: label,
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
      // A TIFF's pages are turned through in the viewer; an animation's
      // frames play there, and a still picture has neither to count.
      d.caps.paged ? row('details.pages', i?.pages) : row('details.frames', i && i.pages > 1 ? i.pages : undefined),
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
    run: () => {
      if (detail.value) openCopy(detail.value.id, detail.value.paths[0] || '', false)
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
    <FeatureState
      v-if="multiple"
      kind="empty"
      icon="select"
      :title="t('details.multiple', { n: formatNumber(lib.selectedIDs.size) })"
      :text="t('details.multipleHint')"
    />
    <FeatureState v-else-if="!detail && shownError" kind="error" :title="t('details.error')" :text="shownError"
      ><UButton @click="correctionsChanged">{{ t('common.retry') }}</UButton></FeatureState
    >
    <FeatureState
      v-else-if="!detail"
      :kind="loading ? 'loading' : 'empty'"
      icon="info"
      :title="loading ? t('details.loading') : t('details.none')"
    />
    <div v-else class="inspector-body">
      <div class="preview">
        <img v-if="previewUrl" :src="previewUrl" :alt="t('details.preview')" />
        <div v-else class="lumen-skeleton" />
      </div>
      <div class="preview-caption">
        <div>
          <h3 :title="detail.paths[0] ? fileName(detail.paths[0]) : ''">
            <bdi>{{ detail.paths[0] ? fileName(detail.paths[0]) : t('details.missing') }}</bdi>
          </h3>
          <span
            >{{ t(detail.kind === Kind.KindVideo ? 'details.video' : 'details.photo') }} ·
            {{ detail.ext.replace(/^\./, '').toUpperCase() }}</span
          >
        </div>
        <IconButton
          v-if="!lib.inTrash"
          :icon="icons.heart"
          :label="t(detail.favourite ? 'details.unfavourite' : 'details.favourite')"
          :aria-pressed="detail.favourite"
          variant="ghost"
          @click="runCommand(lib.viewer ? 'viewer.favourite' : 'inspector.favourite')"
        />
      </div>
      <section :aria-label="t('details.overview')">
        <h3>{{ t('details.overview') }}</h3>
        <div class="overview-row">
          <div>
            <b>{{ dateTime(detail.takenAt) }}</b
            ><span>{{ t('details.taken') }} · {{ t(dateFromText[detail.dateFrom]) }}</span>
          </div>
          <UButton v-if="!lib.inTrash" data-edit-date variant="ghost" size="sm" @click="dateOpen = true">{{
            t('details.editDate')
          }}</UButton>
        </div>
        <div class="overview-row">
          <div>
            <b>{{ placeName || t('details.missing') }}</b
            ><span
              >{{ t('details.location') }} ·
              {{
                detail.lat !== null && detail.lon !== null
                  ? t('details.coordinateValue', { lat: detail.lat.toFixed(5), lon: detail.lon.toFixed(5) })
                  : t('details.missing')
              }}</span
            >
          </div>
          <UButton v-if="!lib.inTrash" data-edit-place variant="ghost" size="sm" @click="placeOpen = true">{{
            t('details.editPlace')
          }}</UButton>
        </div>
        <div v-if="!lib.inTrash" class="flex flex-wrap gap-2">
          <UButton class="block-event" :icon="icons['eye-off']" variant="ghost" size="sm" @click="blockEvent">{{
            t('blocked.blockEvent')
          }}</UButton>
          <UButton v-if="detail.placeId" :icon="icons['eye-off']" variant="ghost" size="sm" @click="blockPlace">{{
            t('blocked.blockPlace')
          }}</UButton>
        </div>
      </section>

      <section :aria-label="t('details.metadata')">
        <h3>{{ t('details.metadata') }}</h3>
        <MetaRow
          :label="t('details.dimensionsLabel')"
          icon="image"
          :value="
            detail.width && detail.height
              ? t('details.dimensionValue', {
                  w: detail.width,
                  h: detail.height,
                  mp: megapixels(detail.width, detail.height),
                })
              : t('details.missing')
          "
        />
        <MetaRow :label="t('details.size')" :value="detail.size ? bytes(detail.size) : t('details.missing')" />
        <MetaRow
          :label="t('details.format')"
          :icon="detail.kind === Kind.KindVideo ? 'video' : 'image'"
          :value="
            t('common.labelValue', {
              label: t(detail.kind === Kind.KindVideo ? 'details.video' : 'details.photo'),
              value: detail.ext.replace(/^\./, '').toUpperCase() || t('details.missing'),
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

        <MetaRow
          v-for="row in metadata"
          :key="row.key"
          :copyable="!!row.value"
          :label="row.label"
          :value="row.value || t('details.missing')"
        />
        <UButton variant="ghost" size="sm" :icon="icons.copy" @click="copyMetadata">{{
          t('details.copyMetadata')
        }}</UButton>
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
              <IconButton
                :icon="icons.close"
                :label="t('details.notThisPerson', { name: f.name || t('person.unnamed') })"
                variant="ghost"
                size="xs"
                @click="notThem(f)"
              />
              <IconButton
                :icon="icons['eye-off']"
                :label="t('blocked.blockNamed', { name: f.name || t('person.unnamed') })"
                variant="ghost"
                size="xs"
                @click="blockPerson(f)"
              />
            </template>
            <UButton v-else class="who" variant="outline" @click="faceOpen = f">{{ t('details.whoIsThis') }}</UButton>
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
          <li v-for="tag in tags" :key="tag.id" :data-source="tag.source">
            <UButton
              :to="{ name: 'thing', params: { id: tag.id } }"
              variant="link"
              size="sm"
              class="tag-name"
              :title="tag.name"
              :ui="{ label: 'truncate' }"
              >{{ tag.name }}</UButton
            >
            <IconButton
              :icon="icons.close"
              :label="t('details.removeTag', { name: tag.name })"
              variant="ghost"
              size="xs"
              @click="removeTag(tag)"
            />
            <IconButton
              :icon="icons['eye-off']"
              :label="t('blocked.blockNamed', { name: tag.name })"
              variant="ghost"
              size="xs"
              @click="blockThing(tag)"
            />
            <span class="tag-source">{{ t(tag.source === TagSource.TagByAI ? 'details.byAI' : 'details.byYou') }}</span>
          </li>
        </ul>
        <p v-else class="text-sm text-muted">{{ t('details.noTags') }}</p>
        <UButton variant="outline" size="sm" @click="tagOpen = true">{{ t('details.addTag') }}</UButton>
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
        <UButton variant="outline" size="sm" @click="albumOpen = true">{{ t('details.album.add') }}</UButton>
        <UButton variant="ghost" size="sm" @click="createOpen = true">{{ t('albums.createForItem') }}</UButton>
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
            <div class="file-actions">
              <IconButton
                :icon="icons.copy"
                :label="t('details.copyPath')"
                variant="ghost"
                size="sm"
                @click="copyText(p)"
              />
              <IconButton
                v-if="!lib.inTrash"
                :icon="icons.external"
                :label="t('files.open')"
                variant="ghost"
                size="sm"
                @click="openCopy(detail.id, p, false)"
              />
              <IconButton
                v-if="!lib.inTrash"
                :icon="icons.more"
                :label="t('files.openWith')"
                variant="ghost"
                size="sm"
                @click="openCopy(detail.id, p, true)"
              />
              <IconButton
                :icon="icons.copy"
                :label="t('files.copyName')"
                variant="ghost"
                size="sm"
                @click="copyName(p)"
              />
              <IconButton
                :icon="icons.folder"
                :label="t('common.showInFolder')"
                variant="ghost"
                size="sm"
                @click="showInFolder(detail.id, p)"
              />
              <IconButton
                v-if="!lib.inTrash"
                data-block-folder
                :icon="icons['eye-off']"
                :label="t('blocked.blockFolder', { path: p })"
                variant="ghost"
                size="sm"
                @click="blockFolder(p)"
              />
            </div>
          </template>
        </MetaRow>
        <UButton
          v-if="detail.paths.length > 1"
          to="/duplicates"
          color="neutral"
          variant="soft"
          :icon="icons.duplicates"
          >{{ t('details.reviewCopies') }}</UButton
        >
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
          <UButton
            v-if="detail.kind === Kind.KindPhoto"
            :icon="icons.files"
            @click="runCommand(lib.viewer ? 'viewer.compress' : 'library.compress')"
            >{{ t('compression.action') }}</UButton
          >
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
  <DateDialog v-if="detail && dateOpen" v-model:open="dateOpen" :assets="[detail]" @changed="correctionsChanged" />
  <PlaceDialog v-if="detail && placeOpen" v-model:open="placeOpen" :assets="[detail]" @changed="correctionsChanged" />
  <AlbumPicker
    v-if="detail"
    :id="detail.id"
    v-model:open="albumOpen"
    :membership="albumIDs"
    @changed="loadAlbums(props.sel?.id)"
  />
  <UModal v-model:open="tagOpen" :title="t('details.tag.title')" :dismissible="!tagBusy" :close="!tagBusy">
    <template #body>
      <UInput
        v-model="newTag"
        class="w-full"
        data-tag-draft
        :placeholder="t('details.addTag')"
        :aria-label="t('details.addTag')"
        :aria-invalid="nameTooLong(newTag) || undefined"
        @keydown.enter="addTag" />
      <UAlert
        v-if="nameTooLong(newTag)"
        role="alert"
        color="error"
        variant="subtle"
        :title="t('error.invalidName', { max: MAX_NAME_LEN })" /><UAlert
        v-if="error"
        color="error"
        :title="error"
        role="alert"
    /></template>
    <template #footer
      ><div class="w-full flex justify-end gap-2">
        <UButton color="neutral" variant="ghost" :disabled="tagBusy" @click="tagOpen = false">{{
          t('common.cancel')
        }}</UButton
        ><UButton
          color="primary"
          variant="solid"
          :loading="tagBusy"
          :disabled="tagBusy || !trimName(newTag) || nameTooLong(newTag)"
          @click="addTag"
          >{{ t('details.tag.save') }}</UButton
        >
      </div></template
    >
  </UModal>
  <UModal
    v-model:open="createOpen"
    :title="t('details.album.createTitle')"
    :dismissible="!creatingAlbum"
    :close="!creatingAlbum"
  >
    <template #body>
      <form class="album-new" @submit.prevent="createAlbumForItem">
        <UInput
          v-model="newAlbum"
          class="min-w-0 flex-1"
          :placeholder="t('albums.createForItem')"
          :aria-label="t('albums.createForItem')"
          :aria-invalid="nameTooLong(newAlbum) || undefined"
        />
        <IconButton
          type="submit"
          :icon="icons.plus"
          :label="t('albums.createForItem')"
          :loading="creatingAlbum"
          :disabled="creatingAlbum || !trimName(newAlbum) || nameTooLong(newAlbum)"
        />
      </form>
      <UAlert
        v-if="nameTooLong(newAlbum)"
        role="alert"
        color="error"
        variant="subtle"
        :title="t('error.invalidName', { max: MAX_NAME_LEN })" /><UAlert
        v-if="error"
        color="error"
        :title="error"
        role="alert"
    /></template>
    <template #footer
      ><div class="w-full flex justify-end gap-2">
        <UButton color="neutral" variant="ghost" :disabled="creatingAlbum" @click="createOpen = false">{{
          t('common.cancel')
        }}</UButton>
      </div></template
    >
  </UModal>
  <UModal
    :open="!!faceOpen"
    :title="t('details.whoIsThis')"
    :dismissible="!faceBusy"
    :close="!faceBusy"
    @update:open="
      (value) => {
        if (!value) faceOpen = null
      }
    "
    ><template #body
      ><PersonPicker
        v-if="faceOpen"
        managed
        :placeholder="t('details.whoIsThis')"
        :busy="faceBusy"
        :error="error"
        @pick="
          (name) => {
            if (faceOpen) void nameFace(faceOpen, name)
          }
        "
        @cancel="faceOpen = null" /></template
  ></UModal>
  <UModal
    :open="!!handoff"
    :title="handoff?.title"
    :description="t('details.handoff.hint')"
    @update:open="
      (value) => {
        if (!value) handoff = null
      }
    "
  >
    <template #body
      ><p class="break-words font-mono text-sm">{{ handoff?.path }}</p>
      <UAlert v-if="handoffError" class="mt-4" color="error" :title="handoffError" role="alert" />
      <p v-else class="mt-4 text-sm text-muted" role="status">
        {{ t(handoffBusy ? 'details.handoff.working' : 'details.handoff.sent') }}
      </p></template
    >
    <template #footer
      ><div class="w-full flex justify-end gap-2">
        <UButton color="neutral" variant="ghost" @click="handoff = null">{{ t('common.close') }}</UButton
        ><UButton :loading="handoffBusy" :disabled="handoffBusy" @click="runHandoff">{{ t('common.retry') }}</UButton>
      </div></template
    >
  </UModal>
  <FileTools v-model:open="filesOpen" :ids="detail ? [detail.id] : []" @changed="loadDetails(props.sel?.id)" />

  <aside
    v-if="mode === 'wide'"
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
      <IconButton :icon="icons.close" :label="t('common.close')" variant="ghost" @click="lib.showDetails = false" />
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
    :close="true"
    :content="{ onCloseAutoFocus: returnFocus }"
    :ui="{ content: 'max-w-none w-auto', body: 'p-0 sm:p-0' }"
    @update:open="
      (open) => {
        if (!open) lib.showDetails = false
      }
    "
  >
    <template #body>
      <!-- The sheet keeps the width the user gave the inline inspector. -->
      <div :style="{ width: `min(${prefs.prefs.inspectorWidth}px, calc(100vw - 2rem))` }">
        <ReuseInspectorContent />
      </div>
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
  background: var(--lumen-bg-raised);
  color: var(--ui-text);
  user-select: text;
}

.inspector-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-shrink: 0;
  min-height: var(--lumen-topbar-h);
  border-bottom: 1px solid var(--ui-border);
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
  gap: 1.5rem;
  padding: 1rem 1.25rem 2rem;
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
.preview {
  aspect-ratio: 3 / 2;
  overflow: hidden;
  border-radius: var(--lumen-r-lg);
  background: var(--lumen-placeholder);
}
.preview img,
.preview > div {
  width: 100%;
  height: 100%;
  object-fit: cover;
}
.preview-caption {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 0.5rem;
}
.preview-caption > div {
  min-width: 0;
}
.preview-caption h3 {
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
  font-size: 0.875rem;
}
.preview-caption span,
.tag-source {
  font-size: 0.75rem;
  color: var(--ui-text-dimmed);
}
section + section {
  border-top: 1px solid var(--ui-border);
  padding-top: 1rem;
}
section > h3 {
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--ui-text-dimmed);
}
.tags li[data-source='ai'] .tag-name {
  border: 1px dashed var(--lumen-light);
  color: var(--lumen-light);
  background: var(--lumen-light-soft);
  border-radius: var(--lumen-r-md);
}
.inspector-body {
  font-variant-numeric: tabular-nums;
}
.file-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 0.25rem;
}
.path {
  flex-wrap: wrap;
}
.path :deep(.action) {
  flex-basis: 100%;
}
section :deep(.meta-row) {
  gap: 0.5rem;
  padding: 0.125rem 0;
}
section :deep(.meta-row .text) {
  display: grid;
  grid-template-columns: 6rem minmax(0, 1fr);
  align-items: baseline;
  gap: 0.5rem;
  font-size: 0.8125rem;
}
section :deep(.meta-row .value) {
  font-variant-numeric: tabular-nums;
}
.path :deep(.text) {
  display: flex;
}
.overview-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
  padding-block: 0.375rem;
}
.overview-row > div {
  min-width: 0;
}
.overview-row b {
  display: block;
  font-size: 0.8125rem;
  overflow-wrap: anywhere;
}
.overview-row span {
  display: block;
  margin-top: 0.25rem;
  font-size: 0.75rem;
  color: var(--ui-text-dimmed);
}
.overview-row > button {
  flex-shrink: 0;
}
</style>
