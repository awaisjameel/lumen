<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { AlbumKind, BlockKind, DateSource, engine, MAX_NAME_LEN, TagSource } from '../bridge'
import type { AssetDetail, AssetTag, Face } from '../bridge'
import { errorText, logError, orFallback } from '../shared/errors'
import { block } from '../shared/block'
import { bytes, dateTime, duration, megapixels } from '../shared/format'
import { t } from '../shared/i18n'
import type { MessageKey } from '../shared/i18n'
import Icon from '../shared/Icon.vue'
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
const albums = useAlbums()
const detail = ref<AssetDetail | null>(null)
const placeName = ref('') // the city near the photo, once known
const tags = ref<AssetTag[]>([])
const faces = ref<Face[]>([])
const newTag = ref('')
const error = ref('')
const detailError = ref('')
const shownError = computed(() => error.value || detailError.value)
const albumIDs = ref<number[]>([])
const newAlbum = ref('')
const creatingAlbum = ref(false)
const manualAlbums = computed(() => albums.list.filter((a) => a.kind === AlbumKind.AlbumManual))
let selectionRevision = 0
let detailRequest = 0
let tagsRequest = 0
let facesRequest = 0
let albumsRequest = 0
onBeforeUnmount(() => {
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
      error.value = ''
      detail.value = null
      placeName.value = ''
    }
    void loadDetails(id)
  },
  { immediate: true },
)

async function loadDetails(id: number | undefined) {
  const request = ++detailRequest
  detailError.value = ''
  if (!id) return
  try {
    const d = await engine.asset(id)
    if (request !== detailRequest) return
    detail.value = d
    const place = d.placeId ? await orFallback(engine.place(d.placeId), null) : null
    if (request === detailRequest) placeName.value = place?.name ?? ''
  } catch (e) {
    if (request === detailRequest) detailError.value = errorText(e)
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

function toggleAlbum(albumID: number, e: Event) {
  const box = e.target
  const assetID = detail.value?.id
  if (!(box instanceof HTMLInputElement) || !assetID) return
  const include = box.checked
  void edit(
    () => engine.setAlbumItem(assetID, albumID, include),
    async () => {
      await Promise.all([loadAlbums(assetID), albums.load()])
    },
  ).then((ok) => {
    if (!ok) box.checked = !include // the album didn't change: neither does the box
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

async function blockPlace() {
  const id = detail.value?.placeId
  if (id && (await block(BlockKind.BlockPlace, id, placeName.value || t('details.location'))))
    await loadExtras(props.sel?.id)
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
  if (props.sel && detail.value) void deleteFromDisk(lib, props.sel, Math.max(detail.value.paths.length, 1))
}

function showInFolder(id: number) {
  error.value = ''
  engine.showInFolder(id).catch((e: unknown) => {
    if (props.sel?.id === id) error.value = errorText(e)
  })
}
</script>

<template>
  <aside class="details" :aria-label="t('details.title')">
    <header>
      <h2>{{ t('details.title') }}</h2>
      <button
        class="icon-btn"
        :title="t('common.close')"
        :aria-label="t('common.close')"
        @click="lib.showDetails = false"
      >
        <Icon name="close" :size="18" />
      </button>
    </header>

    <p v-if="!detail && shownError" class="error pad" role="alert">{{ shownError }}</p>
    <p v-else-if="!detail" class="muted pad">{{ props.sel ? t('details.loading') : t('details.none') }}</p>

    <div v-else class="pad body">
      <section>
        <h3>{{ t('details.taken') }}</h3>
        <p>{{ dateTime(detail.takenAt) }}</p>
        <p class="muted small">{{ t(dateFromText[detail.dateFrom]) }}</p>
      </section>

      <section v-if="detail.width">
        <h3>{{ t('details.size') }}</h3>
        <p>
          {{
            t('details.dimensions', {
              w: detail.width,
              h: detail.height,
              mp: megapixels(detail.width, detail.height),
              size: bytes(detail.size),
            })
          }}
        </p>
      </section>

      <section v-if="detail.video">
        <h3>{{ t('details.duration') }}</h3>
        <p>{{ duration(detail.video.duration) }}</p>
        <p v-if="detail.video.codec" class="muted small">
          {{ t('common.labelValue', { label: t('details.videoCodec'), value: detail.video.codec }) }}
        </p>
        <p v-if="detail.video.audio" class="muted small">
          {{ t('common.labelValue', { label: t('details.audioCodec'), value: detail.video.audio }) }}
        </p>
      </section>

      <section v-if="detail.camera">
        <h3>{{ t('details.camera') }}</h3>
        <p>{{ detail.camera }}</p>
      </section>

      <section v-if="detail.lat !== null && detail.lon !== null">
        <h3>{{ t('details.location') }}</h3>
        <p v-if="placeName">{{ placeName }}</p>
        <p class="mono">{{ detail.lat.toFixed(5) }}, {{ detail.lon.toFixed(5) }}</p>
        <button v-if="detail.placeId && !lib.inTrash" class="btn btn-subtle" @click="blockPlace">
          {{ t('blocked.blockPlace') }}
        </button>
      </section>

      <section v-if="!lib.inTrash">
        <h3>{{ t('details.tags') }}</h3>
        <ul v-if="tags.length" class="tags">
          <li
            v-for="tag in tags"
            :key="tag.id"
            :class="{ ai: tag.source === TagSource.TagByAI }"
            :title="tag.source === TagSource.TagByAI ? t('details.byAI') : t('details.byYou')"
          >
            <RouterLink :to="{ name: 'thing', params: { id: tag.id } }">{{ tag.name }}</RouterLink>
            <button
              :title="t('details.removeTag', { name: tag.name })"
              :aria-label="t('details.removeTag', { name: tag.name })"
              @click="removeTag(tag)"
            >
              <Icon name="close" :size="12" />
            </button>
            <button
              :title="t('blocked.confirmTitle', { name: tag.name })"
              :aria-label="t('blocked.confirmTitle', { name: tag.name })"
              @click="blockThing(tag)"
            >
              <Icon name="eye-off" :size="12" />
            </button>
          </li>
        </ul>
        <p v-else class="muted small">{{ t('details.noTags') }}</p>
        <input
          v-model="newTag"
          class="field add-tag"
          :placeholder="t('details.addTag')"
          :aria-label="t('details.addTag')"
          :aria-invalid="nameTooLong(newTag) || undefined"
          @keydown.enter="addTag"
        />
        <p v-if="nameTooLong(newTag)" class="error small" role="alert">
          {{ t('error.invalidName', { max: MAX_NAME_LEN }) }}
        </p>
      </section>

      <section v-if="!lib.inTrash">
        <h3>{{ t('nav.albums') }}</h3>
        <label v-for="album in manualAlbums" :key="album.id" class="album-option">
          <input type="checkbox" :checked="albumIDs.includes(album.id)" @change="toggleAlbum(album.id, $event)" />
          {{ album.name }}
        </label>
        <form class="album-new" @submit.prevent="createAlbumForItem">
          <input
            v-model="newAlbum"
            class="field"
            :placeholder="t('albums.createForItem')"
            :aria-label="t('albums.createForItem')"
            :aria-invalid="nameTooLong(newAlbum) || undefined"
          />
          <button
            class="icon-btn"
            :title="t('albums.createForItem')"
            :aria-label="t('albums.createForItem')"
            :disabled="creatingAlbum || !trimName(newAlbum) || nameTooLong(newAlbum)"
          >
            <Icon name="plus" :size="16" />
          </button>
        </form>
        <p v-if="nameTooLong(newAlbum)" class="error small" role="alert">
          {{ t('error.invalidName', { max: MAX_NAME_LEN }) }}
        </p>
      </section>

      <section v-if="faces.length && !lib.inTrash">
        <h3>{{ t('details.people') }}</h3>
        <ul class="faces">
          <li v-for="f in faces" :key="f.id">
            <FaceThumb :face="f" :size="36" round />
            <template v-if="f.person">
              <RouterLink :to="{ name: 'person', params: { id: f.person } }" class="who">
                {{ f.name || t('person.unnamed') }}
              </RouterLink>
              <button
                class="not"
                :title="t('details.notThisPerson', { name: f.name || t('person.unnamed') })"
                :aria-label="t('details.notThisPerson', { name: f.name || t('person.unnamed') })"
                @click="notThem(f)"
              >
                <Icon name="close" :size="12" />
              </button>
              <button
                class="not"
                :title="t('blocked.confirmTitle', { name: f.name || t('person.unnamed') })"
                :aria-label="t('blocked.confirmTitle', { name: f.name || t('person.unnamed') })"
                @click="blockPerson(f)"
              >
                <Icon name="eye-off" :size="12" />
              </button>
            </template>
            <PersonPicker v-else class="who" :placeholder="t('details.whoIsThis')" @pick="(n) => nameFace(f, n)" />
          </li>
        </ul>
      </section>

      <section>
        <h3>{{ t('details.files') }}</h3>
        <p v-if="detail.paths.length > 1" class="muted small">
          {{ t('details.copies', { n: detail.paths.length }) }}
        </p>
        <div v-for="p in detail.paths" :key="p" class="path" :title="p">
          {{ p }}
          <button
            v-if="!lib.inTrash"
            class="btn btn-subtle"
            :aria-label="t('blocked.blockFolder', { path: p })"
            @click="blockFolder(p)"
          >
            {{ t('blocked.blockFolderShort') }}
          </button>
        </div>
      </section>

      <div class="actions">
        <template v-if="lib.inTrash">
          <button class="btn" @click="restore">{{ t('trash.restore') }}</button>
          <button class="btn" @click="deleteThisFromDisk">
            <Icon name="trash" :size="16" />{{ t('trash.deleteFromDisk') }}
          </button>
        </template>
        <template v-else>
          <button class="btn" @click="blockAsset"><Icon name="eye-off" :size="16" />{{ t('blocked.block') }}</button>
          <button class="btn" @click="lib.toggleFavourite(props.sel)">
            <Icon :name="detail.favourite ? 'heart-filled' : 'heart'" :size="16" />
            {{ detail.favourite ? t('details.unfavourite') : t('details.favourite') }}
          </button>
          <button class="btn" @click="moveToTrash"><Icon name="trash" :size="16" />{{ t('trash.move') }}</button>
        </template>
        <button class="btn" @click="showInFolder(detail.id)">
          <Icon name="folder" :size="16" />
          {{ t('common.showInFolder') }}
        </button>
      </div>
      <p v-if="shownError" class="error small" role="alert">{{ shownError }}</p>
    </div>
  </aside>
</template>

<style scoped>
.album-option {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 6px 0;
}

.album-new {
  display: flex;
  align-items: center;
  margin-top: 8px;
}

.album-new .field {
  flex: 1;
}

.details {
  width: var(--details-w);
  flex-shrink: 0;
  border-left: 1px solid var(--border);
  background: var(--surface);
  overflow-y: auto;
  user-select: text;
}

header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  height: var(--topbar-h);
  padding: 0 8px 0 20px;
}

h2 {
  margin: 0;
  font: 600 16px var(--font-display);
}

.pad {
  padding: 0 20px 20px;
}

section {
  margin-bottom: 18px;
}

h3 {
  margin: 0 0 4px;
  font-size: 12px;
  font-weight: 600;
  color: var(--text-3);
  text-transform: uppercase;
  letter-spacing: 0.04em;
}

p {
  margin: 0;
}

.small {
  font-size: 12px;
}

.mono {
  font-variant-numeric: tabular-nums;
}

.path {
  font-size: 12px;
  color: var(--text-2);
  word-break: break-all;
  margin-bottom: 4px;
}

.error {
  margin-top: 12px;
  color: var(--danger);
}

.actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin: 2px 0 8px;
  padding: 0;
  list-style: none;
}

.tags li {
  display: flex;
  align-items: center;
  height: 26px;
  padding-left: 10px;
  border: 1px solid var(--accent);
  border-radius: 13px;
  background: var(--accent-soft);
  font-size: 12px;
}

.tags li.ai {
  border-color: var(--border);
  background: var(--surface-2);
}

.tags a {
  color: var(--text);
  text-decoration: none;
}

.tags a:hover {
  text-decoration: underline;
}

.tags button {
  display: grid;
  place-items: center;
  width: 24px;
  height: 24px;
  padding: 0;
  border: 0;
  background: none;
  color: var(--text-3);
  cursor: pointer;
}

.tags button:hover {
  color: var(--text);
}

.add-tag {
  width: 100%;
  margin-top: 4px;
}

.faces {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin: 2px 0 0;
  padding: 0;
  list-style: none;
}

.faces li {
  display: flex;
  align-items: center;
  gap: 10px;
}

.who {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
  color: var(--text);
  text-decoration: none;
}

a.who:hover {
  text-decoration: underline;
}

.not {
  display: grid;
  place-items: center;
  width: 24px;
  height: 24px;
  padding: 0;
  border: 0;
  border-radius: 50%;
  background: none;
  color: var(--text-3);
  cursor: pointer;
}

.not:hover {
  background: var(--surface-hover);
  color: var(--text);
}
</style>
