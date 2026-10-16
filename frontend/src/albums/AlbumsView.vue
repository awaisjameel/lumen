<script setup lang="ts">
import IconButton from '../shared/IconButton.vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UBadge from '@nuxt/ui/components/Badge.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UCheckbox from '@nuxt/ui/components/Checkbox.vue'
import UDropdownMenu from '@nuxt/ui/components/DropdownMenu.vue'
import UFormField from '@nuxt/ui/components/FormField.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import USlideover from '@nuxt/ui/components/Slideover.vue'
import { useLayoutMode } from '../shared/layout'
import USelect from '@nuxt/ui/components/Select.vue'
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { AlbumKind, BlockKind, engine, Kind, MAX_NAME_LEN, thumbUrl } from '../bridge'
import type { Album, AlbumRule } from '../bridge'
import { usePeople } from '../people/store'
import { confirm } from '../shared/confirm'
import { block } from '../shared/block'
import { errorText, logError } from '../shared/errors'
import { t } from '../shared/i18n'
import { notify } from '../shared/notify'
import Segmented from '../shared/Segmented.vue'
import Icon from '../shared/Icon.vue'
import MediaTile from '../shared/MediaTile.vue'
import { useAlbumPreview } from './preview'
import { useLiveReload } from '../shared/liveReload'
import { nameTooLong, trimName } from '../shared/names'
import { icons } from '../design/icons'
import CardGrid from '../shared/CardGrid.vue'
import CollectionCard from '../shared/CollectionCard.vue'
import { useItemCommands, useCommands } from '../shared/commands'
import FeatureState from '../shared/FeatureState.vue'
import Page from '../shared/Page.vue'
import PageHeader from '../shared/PageHeader.vue'
import { useThings } from '../things/store'
import { emptyRule, useAlbums } from './store'

const mode = useLayoutMode()
const router = useRouter()
const route = useRoute()
const albums = useAlbums()
const things = useThings()
const people = usePeople()
const name = ref('')
const kind = ref(AlbumKind.AlbumManual)
const rule = ref<AlbumRule>(emptyRule())
const editing = ref<number | null>(null)
const nameField = ref<{ inputRef: HTMLInputElement | null } | null>(null)
const editorOpen = ref(false)
let editorTrigger: HTMLElement | null = null
const busy = ref(false)
const error = ref('')
const attempted = ref(false)
const filter = ref<AlbumKind | 'all'>('all')
const visibleAlbums = computed(() =>
  albums.list.filter((album) => filter.value === 'all' || album.kind === filter.value),
)
const filters = computed(() => [
  { value: 'all', label: t('albums.all') },
  { value: AlbumKind.AlbumManual, label: t('albums.manual') },
  { value: AlbumKind.AlbumSmart, label: t('albums.smart') },
])
const dateError = computed(
  () => kind.value === AlbumKind.AlbumSmart && !!rule.value.from && !!rule.value.to && rule.value.from > rule.value.to,
)
const referenceError = computed(
  () =>
    kind.value === AlbumKind.AlbumSmart &&
    ((things.loaded && rule.value.tag !== 0 && !things.list.some((item) => item.id === rule.value.tag)) ||
      (people.loaded && rule.value.person !== 0 && !people.list.some((item) => item.id === rule.value.person))),
)
const invalid = computed(
  () => !trimName(name.value) || nameTooLong(name.value) || dateError.value || referenceError.value,
)
const preview = useAlbumPreview(() =>
  editorOpen.value && kind.value === AlbumKind.AlbumSmart && !dateError.value && !referenceError.value
    ? rule.value
    : null,
)
function ruleSummary(value: AlbumRule) {
  return [
    t(
      value.kind === Kind.KindPhoto
        ? 'albums.photos'
        : value.kind === Kind.KindVideo
          ? 'albums.videos'
          : 'albums.anyMedia',
    ),
    value.favourites ? t('albums.favourites') : '',
    things.list.find((item) => item.id === value.tag)?.name,
    people.list.find((item) => item.id === value.person)?.name,
    value.from ? t('albums.after', { date: value.from }) : '',
    value.to ? t('albums.before', { date: value.to }) : '',
  ]
    .filter(Boolean)
    .join(' · ')
}
let active = true
onBeforeUnmount(() => (active = false))

onMounted(() => {
  void albums.load().catch(logError)
  void things.load().catch(logError)
  void people.load().catch(logError)
})
useLiveReload(async () => {
  await albums.load()
  await preview.refresh()
})

function reset() {
  error.value = ''
  attempted.value = false
  editing.value = null
  name.value = ''
  kind.value = AlbumKind.AlbumManual
  rule.value = emptyRule()
}

watch(
  () => albums.list,
  (list) => {
    if (editing.value !== null && !list.some((album) => album.id === editing.value)) reset()
  },
)

/** Opens the album's current rules; copies keep the unsaved draft local. */
async function edit(album: Album) {
  if (busy.value) return
  error.value = ''
  attempted.value = false
  editing.value = album.id
  name.value = album.name
  kind.value = album.kind
  rule.value = { ...album.rule }
  editorOpen.value = true
  await nextTick()
  nameField.value?.inputRef?.focus()
  nameField.value?.inputRef?.select()
}

// Collection heroes can open the same editor without owning a second form.
watch(
  [() => route.query.edit, () => albums.loaded],
  ([id, loaded]) => {
    if (!loaded || typeof id !== 'string' || !/^\d+$/.test(id) || editorOpen.value) return
    const album = albums.byId.get(Number(id))
    if (album) void edit(album)
  },
  { immediate: true },
)
watch(editorOpen, (open, previous) => {
  if (!open && previous && route.query.edit !== undefined)
    void router.replace({ query: { ...route.query, edit: undefined } })
})

async function save() {
  attempted.value = true
  if (invalid.value || busy.value) return
  busy.value = true
  error.value = ''
  try {
    const id = editing.value
    if (id === null) {
      const created = await engine.createAlbum(name.value, kind.value, rule.value)
      // It exists now: a list that fails to refresh does not undo that.
      await albums.load().catch(logError)
      if (!active) return
      notify({
        id: 'albums.saved',
        kind: 'success',
        title: t(kind.value === AlbumKind.AlbumSmart ? 'albums.createdSmart' : 'albums.createdManual'),
      })
      reset()
      editorOpen.value = false
      void router.push({ name: 'album', params: { id: created } })
    } else {
      await engine.updateAlbum(id, name.value, rule.value)
      await albums.load().catch(logError)
      notify({ id: 'albums.saved', kind: 'success', title: t('albums.saved') })
      reset()
      editorOpen.value = false
    }
  } catch (e) {
    error.value = errorText(e)
  } finally {
    busy.value = false
  }
}

async function remove(album: Album) {
  if (busy.value) return
  if (
    !(await confirm(
      t('albums.deleteTitle', { name: album.name }),
      t(album.kind === AlbumKind.AlbumSmart ? 'albums.deleteSmartText' : 'albums.deleteText'),
      t('albums.delete'),
      { danger: true },
    ))
  )
    return
  error.value = ''
  busy.value = true
  try {
    await engine.deleteAlbum(album.id)
    if (editing.value === album.id) reset()
    await albums.load().catch(logError)
    notify({ id: 'albums.deleted', kind: 'success', title: t('albums.deleted') })
  } catch (e) {
    error.value = errorText(e)
  } finally {
    busy.value = false
  }
}

async function blockAlbum(album: Album) {
  if ((await block(BlockKind.BlockAlbum, album.id, album.name)) && editing.value === album.id) reset()
}
const editedAlbum = computed(() => (editing.value === null ? undefined : albums.byId.get(editing.value)))
const albumKinds = computed(() => [
  { label: t('albums.manual'), value: AlbumKind.AlbumManual },
  { label: t('albums.smart'), value: AlbumKind.AlbumSmart },
])
// Reka Select reserves the empty string; only the control uses numeric values.
const mediaKind = computed({
  get: () => (rule.value.kind === Kind.KindPhoto ? 1 : rule.value.kind === Kind.KindVideo ? 2 : 0),
  set: (value: number) => {
    rule.value.kind = value === 1 ? Kind.KindPhoto : value === 2 ? Kind.KindVideo : Kind.$zero
  },
})
const mediaKinds = computed(() => [
  { label: t('albums.anyMedia'), value: 0 },
  { label: t('albums.photos'), value: 1 },
  { label: t('albums.videos'), value: 2 },
])
const tagChoices = computed(() => [
  { label: t('albums.anyTag'), value: 0 },
  ...things.list.map((tag) => ({ label: tag.name, value: tag.id })),
])
const personChoices = computed(() => [
  { label: t('albums.anyPerson'), value: 0 },
  ...people.list.map((person) => ({ label: person.name || t('person.unnamed'), value: person.id })),
])
function cancelEditor() {
  if (busy.value) return
  editorOpen.value = false
  reset()
}
function newAlbum() {
  if (busy.value) return
  reset()
  editorOpen.value = true
}
function focusName(e: Event) {
  e.preventDefault()
  void nextTick(() => {
    nameField.value?.inputRef?.focus()
    nameField.value?.inputRef?.select()
  })
}
function rememberTrigger(e: Event) {
  if (e.currentTarget instanceof HTMLElement) editorTrigger = e.currentTarget
}
function returnFocus(e: Event) {
  if (!editorTrigger?.isConnected) return
  e.preventDefault()
  editorTrigger.focus()
}
function keepEditorFocus(e: Event) {
  if (editorOpen.value) e.preventDefault()
}
const albumItems = useItemCommands(
  () => albums.list,
  (album) => album.id,
  (album) => [
    {
      id: `album.${album.id}.edit`,
      group: 'library',
      title: () => t('albums.editNamed', { name: album.name }),
      icon: 'pencil',
      enabled: () => !busy.value,
      run: () => edit(album),
    },
    {
      id: `album.${album.id}.block`,
      group: 'library',
      title: () => t('blocked.confirmTitle', { name: album.name }),
      icon: 'eye-off',
      enabled: () => !busy.value,
      run: () => blockAlbum(album),
    },
    {
      id: `album.${album.id}.delete`,
      group: 'library',
      title: () => t('albums.deleteNamed', { name: album.name }),
      icon: 'trash',
      danger: true,
      enabled: () => !busy.value,
      run: () => remove(album),
    },
  ],
)
useCommands([
  {
    id: 'albums.new',
    group: 'library',
    title: () => t('albums.new'),
    icon: 'plus',
    enabled: () => !busy.value,
    run: newAlbum,
  },
  {
    id: 'albums.save',
    group: 'library',
    title: () => t(editing.value === null ? 'albums.create' : 'albums.save'),
    icon: 'save',
    available: () => editorOpen.value,
    enabled: () => !busy.value && !invalid.value,
    run: save,
  },
])
</script>

<template>
  <Page :scroll="false">
    <template #header>
      <PageHeader
        :title="t('nav.albums')"
        :subtitle="
          t('albums.caption', {
            n: albums.list.length,
            smart: albums.list.filter((album) => album.kind === AlbumKind.AlbumSmart).length,
          })
        "
      >
        <UButton
          data-action="new-album"
          :icon="icons.plus"
          color="primary"
          variant="solid"
          :disabled="busy"
          @click.capture="rememberTrigger"
          @click="newAlbum"
          >{{ t('albums.new') }}</UButton
        >
      </PageHeader>
    </template>
    <div class="album-workspace">
      <div class="album-list">
        <p class="intro">{{ t('albums.intro') }}</p>
        <div v-if="albums.loaded && albums.list.length" class="section-head">
          <h2 class="section-heading">{{ t('albums.collections') }}</h2>
          <Segmented v-model="filter" :items="filters" :label="t('albums.type')" />
        </div>
        <FeatureState v-if="(error && !editorOpen) || albums.error" kind="error" :title="error || albums.error">
          <UButton color="neutral" variant="outline" @click="albums.load().catch(logError)">{{
            t('albums.retry')
          }}</UButton>
        </FeatureState>
        <template v-else-if="!albums.loaded">
          <p role="status" class="text-sm text-dimmed">{{ t('albums.loading') }}</p>
          <CardGrid class="album-grid" aria-hidden="true"
            ><div v-for="n in 6" :key="n">
              <div class="lumen-skeleton aspect-[4/3] rounded-lg" />
              <div class="lumen-skeleton mt-3 h-4 w-2/3" /></div
          ></CardGrid>
        </template>
        <CardGrid v-if="visibleAlbums.length" class="album-grid">
          <CollectionCard
            v-for="album in visibleAlbums"
            :key="album.id"
            :to="{ name: 'album', params: { id: album.id } }"
            :title="album.name"
            :subtitle="album.kind === AlbumKind.AlbumSmart ? ruleSummary(album.rule) : undefined"
            :count="
              t('albums.summary', {
                items: t('status.items', { n: album.count }),
                kind: t(album.kind === AlbumKind.AlbumSmart ? 'albums.smart' : 'albums.manual'),
              })
            "
            :cover="album.cover && album.coverThumb ? thumbUrl(album.cover, album.coverThumb) : undefined"
            :icon="album.kind === AlbumKind.AlbumSmart ? 'smart' : 'albums'"
          >
            <template #title>
              <div class="album-title">
                <RouterLink :to="{ name: 'album', params: { id: album.id } }" class="album-name" :title="album.name">{{
                  album.name
                }}</RouterLink
                ><UBadge
                  v-if="album.kind === AlbumKind.AlbumSmart"
                  :icon="icons.smart"
                  color="neutral"
                  variant="soft"
                  size="xs"
                  >{{ t('albums.smartBadge') }}</UBadge
                >
              </div>
            </template>
            <UDropdownMenu :items="albumItems(album.id)" :content="{ onCloseAutoFocus: keepEditorFocus }">
              <IconButton
                :icon="icons.more"
                :label="t('albums.actions', { name: album.name })"
                @pointerdown="rememberTrigger"
                @keydown="rememberTrigger"
              />
            </UDropdownMenu>
          </CollectionCard>
        </CardGrid>
        <FeatureState
          v-else-if="albums.loaded && !albums.error && !error"
          kind="empty"
          icon="albums"
          :title="t(filter === 'all' ? 'albums.empty' : 'albums.filterEmpty')"
          :text="t('albums.emptyHint')"
          ><UButton
            :icon="icons.plus"
            color="primary"
            variant="solid"
            @click.capture="rememberTrigger"
            @click="newAlbum"
            >{{ t('albums.new') }}</UButton
          ></FeatureState
        >
        <div v-if="albums.loaded && albums.list.length" class="album-guidance">
          <Icon name="albums" :size="20" class="guidance-icon" />
          <div>
            <strong>{{ t('albums.guidanceTitle') }}</strong>
            <p>{{ t('albums.guidance') }}</p>
            <UButton
              to="/"
              variant="link"
              color="neutral"
              size="sm"
              class="px-0"
              :trailing-icon="icons['arrow-right']"
              >{{ t('albums.choosePhotos') }}</UButton
            >
          </div>
        </div>
      </div>
      <USlideover
        v-model:open="editorOpen"
        :portal="mode !== 'wide'"
        :modal="mode !== 'wide'"
        :overlay="mode !== 'wide'"
        :title="editing === null ? t('albums.new') : t('albums.edit')"
        :description="t('albums.intro')"
        :content="{ onOpenAutoFocus: focusName, onCloseAutoFocus: returnFocus }"
        :close="!busy"
        :dismissible="!busy"
        :ui="{
          content:
            mode === 'wide'
              ? 'static w-[22rem] max-w-[22rem] h-full shrink-0 shadow-none border-s border-default'
              : 'max-w-sm',
          body: 'overflow-y-auto min-h-0',
          footer: 'shrink-0',
        }"
      >
        <template #body>
          <form id="album-editor" class="flex flex-col gap-4" @submit.prevent="save">
            <UFormField :label="t('albums.name')" :hint="t('albums.nameHint')" required>
              <UInput
                ref="nameField"
                v-model="name"
                class="w-full"
                :aria-label="t('albums.name')"
                :placeholder="t('albums.name')"
                :aria-invalid="nameTooLong(name) || (attempted && !trimName(name)) || undefined"
                :disabled="busy"
                required
                @blur="attempted = true"
              />
            </UFormField>
            <UFormField :label="t('albums.type')" :hint="editing !== null ? t('albums.typeLocked') : undefined">
              <USelect
                v-model="kind"
                class="w-full"
                :items="albumKinds"
                :disabled="busy || editing !== null"
                :aria-label="t('albums.type')"
              />
            </UFormField>
            <p v-if="kind === AlbumKind.AlbumManual" class="text-sm text-toned">{{ t('albums.manualHint') }}</p>
            <div v-if="kind === AlbumKind.AlbumSmart" class="rule-grid">
              <UFormField :label="t('albums.kind')">
                <USelect
                  v-model="mediaKind"
                  :disabled="busy"
                  class="w-full"
                  :items="mediaKinds"
                  :aria-label="t('albums.kind')"
                />
              </UFormField>
              <UFormField :label="t('albums.from')">
                <UInput
                  v-model="rule.from"
                  :disabled="busy"
                  class="w-full"
                  type="date"
                  :aria-label="t('albums.from')"
                />
              </UFormField>
              <UFormField :label="t('albums.to')">
                <UInput v-model="rule.to" :disabled="busy" class="w-full" type="date" :aria-label="t('albums.to')" />
              </UFormField>
              <UFormField :label="t('albums.tag')">
                <USelect
                  v-model="rule.tag"
                  :disabled="busy"
                  class="w-full"
                  :items="tagChoices"
                  :aria-label="t('albums.tag')"
                />
              </UFormField>
              <UFormField :label="t('albums.person')">
                <USelect
                  v-model="rule.person"
                  :disabled="busy"
                  class="w-full"
                  :items="personChoices"
                  :aria-label="t('albums.person')"
                />
              </UFormField>
              <UCheckbox v-model="rule.favourites" :disabled="busy" :label="t('albums.favourites')" class="self-end" />
            </div>
            <UAlert
              v-if="attempted && !trimName(name)"
              role="alert"
              color="error"
              variant="subtle"
              :title="t('albums.nameRequired')"
            />
            <UAlert v-if="dateError" role="alert" color="error" variant="subtle" :title="t('error.albumDateOrder')" />
            <UAlert
              v-if="referenceError"
              role="alert"
              color="error"
              variant="subtle"
              :title="t('albums.referenceMissing')"
            />
            <section v-if="kind === AlbumKind.AlbumSmart" class="album-preview" data-album-preview>
              <strong>{{ ruleSummary(rule) }}</strong>
              <div role="status" aria-live="polite" class="flex items-center gap-2">
                <UBadge color="success" variant="soft">{{ t('albums.live') }}</UBadge>
                <span class="tabular-nums">{{
                  preview.loading.value
                    ? t('albums.checking')
                    : preview.count.value === null
                      ? t('albums.previewUnavailable')
                      : t('albums.matches', { n: preview.count.value })
                }}</span>
              </div>
              <div v-if="preview.items.value.length" class="preview-grid">
                <div v-for="item in preview.items.value" :key="item.id">
                  <MediaTile :id="item.id" :thumb="item.t" />
                </div>
              </div>
              <p v-if="preview.count.value === 0">{{ t('albums.noMatches') }}</p>
              <UAlert v-if="preview.error.value" role="alert" color="warning" :title="preview.error.value" />
              <p>{{ t('albums.localRules') }}</p>
            </section>
            <UAlert
              v-if="nameTooLong(name)"
              role="alert"
              color="error"
              variant="subtle"
              :title="t('error.invalidName', { max: MAX_NAME_LEN })"
            />
            <UAlert v-if="error" role="alert" color="error" variant="subtle" :title="error" />
          </form>
        </template>
        <template #footer>
          <UDropdownMenu
            v-if="editedAlbum"
            :items="albumItems(editedAlbum.id).filter((item) => item.icon !== icons.pencil)"
          >
            <IconButton :icon="icons.more" :label="t('albums.actions', { name: editedAlbum.name })" :disabled="busy" />
          </UDropdownMenu>
          <div class="flex-1" />
          <UButton data-action="cancel-album" :disabled="busy" variant="ghost" @click="cancelEditor">{{
            t('common.cancel')
          }}</UButton>
          <UButton
            form="album-editor"
            type="submit"
            color="primary"
            variant="solid"
            :loading="busy"
            :disabled="busy || invalid"
            >{{ editing === null ? t('albums.create') : t('albums.save') }}</UButton
          >
        </template>
      </USlideover>
    </div>
  </Page>
</template>

<style scoped>
.rule-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 11rem), 1fr));
  gap: 0.75rem;
}
.album-preview {
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
  padding: 1rem;
  border-radius: var(--lumen-r-lg);
  background: var(--lumen-bg-sunken);
  color: var(--ui-text-toned);
  font-size: 0.8125rem;
}
.album-preview p {
  margin: 0;
}
.intro {
  max-width: 65ch;
  margin: 0 0 1.5rem;
  color: var(--ui-text-toned);
}
.section-head {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 0.75rem;
  margin-bottom: 1rem;
}
.section-heading {
  margin: 0 auto 0 0;
  color: var(--ui-text-highlighted);
  font: 600 1.125rem/1.3 var(--font-display);
}
/* A note under the albums, framed like a card rather than filled like a panel. */
.album-guidance {
  display: flex;
  align-items: flex-start;
  gap: 1rem;
  margin-top: 2rem;
  padding: 1rem 1.25rem;
  border: 1px solid var(--ui-border);
  border-radius: var(--lumen-r-lg);
  background: var(--lumen-bg-raised);
  color: var(--ui-text-toned);
  font-size: 0.8125rem;
}
.album-guidance strong {
  color: var(--ui-text-highlighted);
}
.album-guidance p {
  margin: 0.125rem 0 0.25rem;
}
.guidance-icon {
  flex: none;
  margin-top: 0.125rem;
  color: var(--ui-text-muted);
}
.preview-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 0.25rem;
}
.preview-grid > div {
  aspect-ratio: 1;
  overflow: hidden;
  border-radius: var(--lumen-r-sm);
}
.album-title {
  display: flex;
  align-items: baseline;
  gap: 0.5rem;
  min-width: 0;
}
.album-name {
  overflow: hidden;
  min-width: 0;
  color: var(--ui-text-highlighted);
  font-weight: 600;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.album-name:hover {
  color: var(--lumen-primary-text);
}
:deep(.collection-card .line:not(.count)) {
  order: 3;
}
.album-grid {
  grid-template-columns: repeat(auto-fill, minmax(min(100%, max(12rem, calc((100% - 3rem) / 4))), 1fr));
}
.album-workspace {
  display: flex;
  height: 100%;
  min-width: 0;
  min-height: 0;
}
.album-list {
  flex: 1;
  min-width: 0;
  overflow-y: auto;
  padding: 0.5rem var(--lumen-page-x) 3rem;
}
</style>
