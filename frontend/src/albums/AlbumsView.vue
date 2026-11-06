<script setup lang="ts">
import CommandButton from '../shared/CommandButton.vue'
import { itemCommand, useItemCommands, useCommands, runCommand } from '../shared/commands'
import { destination } from '../shared/navigation/contract'
import IconButton from '../shared/IconButton.vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UBadge from '@nuxt/ui/components/Badge.vue'
import UButton from '../shared/navigation/NavButton.vue'
import UCheckbox from '@nuxt/ui/components/Checkbox.vue'
import UDropdownMenu from '../shared/navigation/NavDropdownMenu.vue'
import UFormField from '@nuxt/ui/components/FormField.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import USlideover from '@nuxt/ui/components/Slideover.vue'
import { useLayoutMode } from '../shared/layout'
import USelect from '@nuxt/ui/components/Select.vue'
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import RouterLink from '../shared/navigation/NavLink.vue'
import { useDestination, useNavigation } from '../shared/navigation/router'
import { AlbumKind, BlockKind, engine, errorCode, ErrorCode, Kind, MAX_NAME_LEN, thumbUrl } from '../bridge'
import type { Album, AlbumRule, AlbumVersion } from '../bridge'
import { usePeople } from '../people/store'
import { confirm } from '../shared/confirm'
import { block } from '../shared/block'
import { refreshAll } from '../shared/listStore'
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
import FeatureState from '../shared/FeatureState.vue'
import Page from '../shared/Page.vue'
import PageHeader from '../shared/PageHeader.vue'
import { useThings } from '../things/store'
import { emptyRule, useAlbums } from './store'
import { useAlbumChanges } from './changes'

const mode = useLayoutMode()
const router = useNavigation()
const route = useDestination('albums')
const albums = useAlbums()
const albumChanges = useAlbumChanges()
const smartCount = computed(() => albums.list.filter((album) => album.kind === AlbumKind.AlbumSmart).length)
const things = useThings()
const people = usePeople()
const name = ref('')
const kind = ref(AlbumKind.AlbumManual)
const rule = ref<AlbumRule>(emptyRule())
const editing = ref<number | null>(null)
const version = ref<AlbumVersion | null>(null)
const conflict = ref(false)
const missingTag = ref(false)
const missingPerson = ref(false)
let session = 0
const newAlbumButton = ref<{ $el: HTMLElement } | null>(null)
let focusedField: { label: string; start: number | null; end: number | null } | null = null
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
  { value: AlbumKind.AlbumManual, label: t('albums.allManual') },
  { value: AlbumKind.AlbumSmart, label: t('albums.allSmart') },
])
const dateError = computed(
  () => kind.value === AlbumKind.AlbumSmart && !!rule.value.from && !!rule.value.to && rule.value.from > rule.value.to,
)
const referenceError = computed(() => missingTag.value || missingPerson.value)
// Private shared lists still clear immediately. Only this editor owns the draft;
// until fresh reads admit it, none of its fields or preview are rendered.
const suspended = computed(
  () =>
    editorOpen.value &&
    ((editing.value !== null && (!albums.loaded || !!albums.error)) ||
      (kind.value === AlbumKind.AlbumSmart &&
        ((rule.value.tag !== 0 && (!things.loaded || things.loading || !!things.error)) ||
          (rule.value.person !== 0 && (!people.loaded || people.loading || !!people.error))))),
)
const draftReadError = computed(() => suspended.value && (albums.error || things.error || people.error))
const invalid = computed(
  () =>
    suspended.value ||
    conflict.value ||
    !trimName(name.value) ||
    nameTooLong(name.value) ||
    dateError.value ||
    referenceError.value,
)
const preview = useAlbumPreview(() =>
  editorOpen.value &&
  !suspended.value &&
  !conflict.value &&
  kind.value === AlbumKind.AlbumSmart &&
  !dateError.value &&
  !referenceError.value
    ? rule.value
    : null,
)
watch(preview.referenceUnavailable, (unavailable) => {
  if (!unavailable) return
  // A preview can learn of privacy/deletion before its library event arrives.
  // Clear the existing reference caches immediately, then admit only fresh data.
  const reads: Promise<void>[] = []
  if (rule.value.tag) reads.push(things.invalidate())
  if (rule.value.person) reads.push(people.invalidate())
  void refreshAll(...reads).catch(logError)
})
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
  session++
  version.value = null
  conflict.value = false
  missingTag.value = false
  missingPerson.value = false
  focusedField = null
  error.value = ''
  attempted.value = false
  editing.value = null
  name.value = ''
  kind.value = AlbumKind.AlbumManual
  rule.value = emptyRule()
}

function closeEditor() {
  editorOpen.value = false
  reset()
}
function retireDraft() {
  editorTrigger = null
  closeEditor()
  notify({ id: 'albums.unavailable', kind: 'info', title: t('albums.draftUnavailable') })
}
// list=[] during invalidation says nothing about the target's existence. Only
// a completed successful read can remove a draft or declare a conflict.
watch([() => albums.list, () => albums.loaded, () => albums.loading], () => {
  if (editing.value === null || !version.value || !albums.loaded || albums.loading || albums.error) return
  const current = albums.byId.get(editing.value)
  if (current?.version.identity !== version.value.identity) {
    retireDraft()
    return
  }
  if (!busy.value && current.version.revision !== version.value.revision) conflict.value = true
})
watch(
  [
    () => things.list,
    () => things.loaded,
    () => things.loading,
    () => people.list,
    () => people.loaded,
    () => people.loading,
    rule,
  ],
  () => {
    if (!editorOpen.value || kind.value !== AlbumKind.AlbumSmart) return
    if (
      things.loaded &&
      !things.loading &&
      !things.error &&
      rule.value.tag &&
      !things.list.some((item) => item.id === rule.value.tag)
    ) {
      rule.value.tag = 0
      missingTag.value = true
    }
    if (
      people.loaded &&
      !people.loading &&
      !people.error &&
      rule.value.person &&
      !people.list.some((item) => item.id === rule.value.person)
    ) {
      rule.value.person = 0
      missingPerson.value = true
    }
  },
  { deep: true },
)
function rememberField(e: Event) {
  const target = e.target
  if (!(target instanceof HTMLElement)) return
  const label = target.getAttribute('aria-label')
  if (!label) return
  focusedField = {
    label,
    start: target instanceof HTMLInputElement ? target.selectionStart : null,
    end: target instanceof HTMLInputElement ? target.selectionEnd : null,
  }
}
watch(suspended, async (paused, wasPaused) => {
  if (paused || !wasPaused || !focusedField) return
  const currentSession = session
  await nextTick()
  if (!active || session !== currentSession || !editorOpen.value) return
  // Do not take focus back from a control the user deliberately chose meanwhile.
  if (document.activeElement !== document.body && !document.activeElement?.closest('[data-draft-state]')) return
  const selection = focusedField
  const field = [...document.querySelectorAll<HTMLElement>('#album-editor [aria-label]')].find(
    (element) => element.getAttribute('aria-label') === selection.label,
  )
  field?.focus()
  if (field instanceof HTMLInputElement && selection.start !== null)
    field.setSelectionRange(selection.start, selection.end)
})
async function retryDraft() {
  await refreshAll(albums.load(), things.load(), people.load()).catch(logError)
}
async function reloadSaved() {
  const current = editing.value === null ? undefined : albums.byId.get(editing.value)
  if (!current || suspended.value || busy.value) return
  const currentSession = session
  const stillCurrent = () => session === currentSession && !suspended.value
  if (!(await confirm(t('albums.reloadTitle'), t('albums.reloadText'), t('albums.reloadSaved')))) return
  if (!stillCurrent()) return
  // A server conflict can arrive before its background event. Reload the
  // authoritative definition and its choices rather than adopting stale lists.
  busy.value = true
  try {
    await refreshAll(albums.load(), things.load(), people.load())
  } catch (e) {
    logError(e)
    return
  } finally {
    busy.value = false
  }
  if (!active || !stillCurrent()) return
  const fresh = albums.byId.get(current.id)
  if (fresh && fresh.version.identity === version.value?.identity) await edit(fresh)
}
/** Opens the album's current rules; copies keep the unsaved draft local. */
async function edit(album: Album) {
  if (busy.value) return
  reset()
  version.value = { ...album.version }
  // Initial reference reads can defer the form's first render. Keep its
  // intended focus without moving focus away from a newer user action.
  focusedField = { label: t('albums.name'), start: 0, end: album.name.length }
  editing.value = album.id
  name.value = album.name
  kind.value = album.kind
  rule.value = { ...album.rule }
  editorOpen.value = true
  await nextTick()
  nameField.value?.inputRef?.focus()
  nameField.value?.inputRef?.select()
}

// Consume an edit link once. A delayed route replacement must not reopen a
// cancelled/removed session when a privacy reload finishes in the meantime.
let consumedEdit: number | undefined
watch(
  () => route.value?.query.edit,
  () => {
    consumedEdit = undefined
  },
)
// Collection heroes can open the same editor without owning a second form.
watch(
  [() => route.value?.query.edit, () => albums.loaded],
  ([id, loaded]) => {
    if (!loaded || id === undefined || editorOpen.value || consumedEdit === id) return
    consumedEdit = id
    const album = albums.byId.get(id)
    if (album) void edit(album)
  },
  { immediate: true },
)
watch(editorOpen, (open, previous) => {
  if (!open && previous) {
    reset()
    if (route.value?.query.edit !== undefined) void router.replace(destination({ name: 'albums' }))
  }
})

function submitAlbum() {
  attempted.value = true
  void runCommand('albums.save')
}

async function save() {
  attempted.value = true
  if (invalid.value || busy.value) return
  const currentSession = session
  const id = editing.value
  const savedKind = kind.value
  const savedName = name.value
  const savedRule = { ...rule.value }
  const savedVersion = version.value ? { ...version.value } : null
  if (id !== null && !savedVersion) return
  busy.value = true
  error.value = ''
  try {
    let created: number | undefined
    if (id === null) created = await engine.createAlbum(savedName, savedKind, savedRule)
    else if (savedVersion) {
      const committed = await engine.saveAlbum({ id, version: savedVersion, name: savedName, rule: savedRule })
      if (active && session === currentSession) version.value = committed
    }
    // A committed write must never be presented as a failed save or replayed
    // merely because the independent list read failed.
    await albums.load().catch(logError)
    if (!active || session !== currentSession) return
    notify({
      id: 'albums.saved',
      kind: 'success',
      title: t(
        id !== null
          ? 'albums.saved'
          : savedKind === AlbumKind.AlbumSmart
            ? 'albums.createdSmart'
            : 'albums.createdManual',
      ),
    })
    closeEditor()
    if (created !== undefined) void router.push(destination({ name: 'album', params: { id: created } }))
  } catch (e) {
    if (!active || session !== currentSession) return
    if (errorCode(e) === ErrorCode.NotFound) retireDraft()
    else {
      if (errorCode(e) === ErrorCode.AlbumChanged) conflict.value = true
      error.value = errorText(e)
      if (errorCode(e) === ErrorCode.AlbumReference) void retryDraft()
    }
  } finally {
    if (active) busy.value = false
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
    await albumChanges.remove(album.id)
    if (editing.value === album.id) closeEditor()
    await albums.load().catch(logError)
    notify({ id: 'albums.deleted', kind: 'success', title: t('albums.deleted') })
  } catch (e) {
    error.value = errorText(e)
  } finally {
    busy.value = false
  }
}

async function blockAlbum(album: Album) {
  if ((await block(BlockKind.BlockAlbum, album.id, album.name)) && editing.value === album.id) closeEditor()
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
const selectedTag = computed({
  get: () => (missingTag.value ? -1 : rule.value.tag),
  set: (value: number) => {
    if (value < 0) return
    rule.value.tag = value
    missingTag.value = false
  },
})
const selectedPerson = computed({
  get: () => (missingPerson.value ? -1 : rule.value.person),
  set: (value: number) => {
    if (value < 0) return
    rule.value.person = value
    missingPerson.value = false
  },
})
const tagChoices = computed(() => [
  ...(missingTag.value ? [{ label: t('albums.referenceUnavailable'), value: -1, disabled: true }] : []),
  { label: t('albums.anyTag'), value: 0 },
  ...things.list.map((tag) => ({ label: tag.name, value: tag.id })),
])
const personChoices = computed(() => [
  ...(missingPerson.value ? [{ label: t('albums.referenceUnavailable'), value: -1, disabled: true }] : []),
  { label: t('albums.anyPerson'), value: 0 },
  ...people.list.map((person) => ({ label: person.name || t('person.unnamed'), value: person.id })),
])
function cancelEditor() {
  if (busy.value) return
  closeEditor()
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
  e.preventDefault()
  const target = editorTrigger?.isConnected ? editorTrigger : newAlbumButton.value?.$el
  target?.focus()
}
function keepEditorFocus(e: Event) {
  if (editorOpen.value) e.preventDefault()
}
const albumItems = useItemCommands(
  () => albums.list,
  (album) => album.id,
  (album) => [
    {
      id: itemCommand('album.edit', album.id),
      title: () => t('albums.editNamed', { name: album.name }),
      enabled: () => !busy.value,
      run: () => edit(album),
    },
    {
      id: itemCommand('album.block', album.id),
      title: () => t('blocked.confirmTitle', { name: album.name }),
      enabled: () => !busy.value,
      run: () => blockAlbum(album),
    },
    {
      id: itemCommand('album.delete', album.id),
      title: () => t('albums.deleteNamed', { name: album.name }),
      enabled: () => !busy.value,
      run: () => remove(album),
    },
  ],
)
function albumMoreItems(id: number) {
  return albumItems(id).filter((item) => item.icon !== icons.pencil)
}
useCommands([
  {
    id: 'albums.new',
    enabled: () => !busy.value,
    run: newAlbum,
  },
  {
    id: 'albums.save',
    title: () => t(editing.value === null ? 'albums.create' : 'albums.save'),
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
            smart: smartCount,
          })
        "
      >
        <CommandButton
          ref="newAlbumButton"
          data-action="new-album"
          :icon="icons.plus"
          color="primary"
          variant="solid"
          :disabled="busy"
          command="albums.new"
          @click.capture="rememberTrigger"
          >{{ t('albums.new') }}</CommandButton
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
            :to="destination({ name: 'album', params: { id: album.id } })"
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
                <RouterLink
                  :to="destination({ name: 'album', params: { id: album.id } })"
                  class="album-name"
                  :title="album.name"
                  >{{ album.name }}</RouterLink
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
          ><CommandButton
            :icon="icons.plus"
            color="primary"
            variant="solid"
            command="albums.new"
            @click.capture="rememberTrigger"
            >{{ t('albums.new') }}</CommandButton
          ></FeatureState
        >
        <div v-if="albums.loaded && albums.list.length" class="album-guidance">
          <Icon name="albums" :size="20" class="guidance-icon" />
          <div>
            <strong>{{ t('albums.guidanceTitle') }}</strong>
            <p>{{ t('albums.guidance') }}</p>
            <UButton
              :to="destination({ name: 'library' })"
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
          <div v-if="suspended" data-draft-state tabindex="-1" class="flex flex-col gap-4">
            <p role="status">{{ t(draftReadError ? 'albums.draftReadFailed' : 'albums.verifyingDraft') }}</p>
            <UButton
              v-if="draftReadError"
              data-action="retry-draft"
              color="neutral"
              variant="outline"
              @click="retryDraft"
              >{{ t('albums.retry') }}</UButton
            >
          </div>
          <form
            v-else
            id="album-editor"
            class="flex flex-col gap-4"
            @focusin="rememberField"
            @input="rememberField"
            @select="rememberField"
            @keyup="rememberField"
            @submit.prevent="submitAlbum"
          >
            <UAlert v-if="conflict" role="alert" color="warning" :title="t('error.albumChanged')">
              <template #actions
                ><UButton
                  data-action="reload-album"
                  color="neutral"
                  variant="outline"
                  :disabled="busy"
                  @click="reloadSaved"
                  >{{ t('albums.reloadSaved') }}</UButton
                ></template
              >
            </UAlert>
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
                  v-model="selectedTag"
                  :disabled="busy"
                  class="w-full"
                  :items="tagChoices"
                  :aria-label="t('albums.tag')"
                />
              </UFormField>
              <UFormField :label="t('albums.person')">
                <USelect
                  v-model="selectedPerson"
                  :disabled="busy"
                  class="w-full"
                  :items="personChoices"
                  :aria-label="t('albums.person')"
                />
              </UFormField>
              <UCheckbox
                v-model="rule.favourites"
                :disabled="busy"
                :label="t('albums.favourites')"
                :aria-label="t('albums.favourites')"
                class="self-end"
              />
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
              data-album-reference-error
              role="alert"
              color="error"
              variant="subtle"
              :title="t('albums.referenceMissing')"
            >
              <template #actions>
                <UButton
                  data-action="retry-album-references"
                  color="neutral"
                  variant="outline"
                  :loading="things.loading || people.loading"
                  @click="retryDraft"
                  >{{ t('albums.retry') }}</UButton
                >
              </template>
            </UAlert>
            <section v-if="kind === AlbumKind.AlbumSmart && !conflict" class="album-preview" data-album-preview>
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
              <UAlert v-if="preview.error.value" role="alert" color="warning" :title="preview.error.value">
                <template #actions>
                  <UButton color="neutral" variant="outline" @click="preview.refresh()">{{
                    t('welcome.retry')
                  }}</UButton>
                </template>
              </UAlert>
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
          <UDropdownMenu v-if="editedAlbum && !suspended" :items="albumMoreItems(editedAlbum.id)">
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
