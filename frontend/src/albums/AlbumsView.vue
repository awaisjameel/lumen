<script setup lang="ts">
import UAlert from '@nuxt/ui/components/Alert.vue'
import UBadge from '@nuxt/ui/components/Badge.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UCheckbox from '@nuxt/ui/components/Checkbox.vue'
import UDropdownMenu from '@nuxt/ui/components/DropdownMenu.vue'
import UFormField from '@nuxt/ui/components/FormField.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import UModal from '@nuxt/ui/components/Modal.vue'
import USelect from '@nuxt/ui/components/Select.vue'
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { AlbumKind, BlockKind, engine, Kind, MAX_NAME_LEN, thumbUrl } from '../bridge'
import type { Album, AlbumRule } from '../bridge'
import { usePeople } from '../people/store'
import { confirm } from '../shared/confirm'
import { block } from '../shared/block'
import { errorText, logError } from '../shared/errors'
import { t } from '../shared/i18n'
import { useLiveReload } from '../shared/liveReload'
import { nameTooLong, trimName } from '../shared/names'
import { icons } from '../design/icons'
import CardGrid from '../shared/CardGrid.vue'
import CollectionCard from '../shared/CollectionCard.vue'
import { useItemCommands, useCommands } from '../shared/commands'
import FeatureState from '../shared/FeatureState.vue'
import Page from '../shared/Page.vue'
import PageHeader from '../shared/PageHeader.vue'
import SearchBox from '../search/SearchBox.vue'
import { useThings } from '../things/store'
import { emptyRule, useAlbums } from './store'

const router = useRouter()
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
let active = true
onBeforeUnmount(() => (active = false))

onMounted(() => {
  void albums.load().catch(logError)
  void things.load().catch(logError)
  void people.load().catch(logError)
})
useLiveReload(albums.load) // any change can change a smart album

function reset() {
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
  editing.value = album.id
  name.value = album.name
  kind.value = album.kind
  rule.value = { ...album.rule }
  editorOpen.value = true
  await nextTick()
  nameField.value?.inputRef?.focus()
  nameField.value?.inputRef?.select()
}

async function save() {
  if (!trimName(name.value) || nameTooLong(name.value) || busy.value) return
  busy.value = true
  error.value = ''
  try {
    const id = editing.value
    if (id === null) {
      const created = await engine.createAlbum(name.value, kind.value, rule.value)
      // It exists now: a list that fails to refresh does not undo that.
      await albums.load().catch(logError)
      if (!active) return
      reset()
      editorOpen.value = false
      void router.push({ name: 'album', params: { id: created } })
    } else {
      await engine.updateAlbum(id, name.value, rule.value)
      await albums.load().catch(logError)
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
  if (!(await confirm(t('albums.deleteTitle', { name: album.name }), t('albums.deleteText'), t('albums.delete'))))
    return
  error.value = ''
  try {
    await engine.deleteAlbum(album.id)
    if (editing.value === album.id) reset()
    await albums.load()
  } catch (e) {
    error.value = errorText(e)
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
  editorOpen.value = false
  reset()
}
function newAlbum() {
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
      run: () => blockAlbum(album),
    },
    {
      id: `album.${album.id}.delete`,
      group: 'library',
      title: () => t('albums.deleteNamed', { name: album.name }),
      icon: 'trash',
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
    enabled: () => !busy.value && !!trimName(name.value) && !nameTooLong(name.value),
    run: save,
  },
])
</script>

<template>
  <Page>
    <template #header>
      <PageHeader :title="t('nav.albums')">
        <SearchBox />
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
    <p class="mb-4 text-muted">{{ t('albums.intro') }}</p>
    <FeatureState v-if="(error && !editorOpen) || albums.error" kind="error" :title="error || albums.error" />
    <FeatureState v-else-if="!albums.loaded" kind="loading" :title="t('albums.loading')" />
    <CardGrid v-if="albums.list.length">
      <CollectionCard
        v-for="album in albums.list"
        :key="album.id"
        :to="{ name: 'album', params: { id: album.id } }"
        :title="album.name"
        :count="
          t('albums.summary', {
            items: t('status.items', { n: album.count }),
            kind: t(album.kind === AlbumKind.AlbumSmart ? 'albums.smart' : 'albums.manual'),
          })
        "
        :cover="album.cover && album.coverThumb ? thumbUrl(album.cover, album.coverThumb) : undefined"
        :icon="album.kind === AlbumKind.AlbumSmart ? 'smart' : 'albums'"
      >
        <template #badge>
          <UBadge
            :icon="album.kind === AlbumKind.AlbumSmart ? icons.smart : icons.albums"
            :color="album.kind === AlbumKind.AlbumSmart ? 'primary' : 'neutral'"
            variant="solid"
            size="xs"
            >{{ t(album.kind === AlbumKind.AlbumSmart ? 'albums.smart' : 'albums.manual') }}</UBadge
          >
        </template>
        <UDropdownMenu :items="albumItems(album.id)" :content="{ onCloseAutoFocus: keepEditorFocus }">
          <UButton
            :icon="icons.more"
            :aria-label="t('albums.actions', { name: album.name })"
            square
            @pointerdown="rememberTrigger"
            @keydown="rememberTrigger"
          />
        </UDropdownMenu>
      </CollectionCard>
    </CardGrid>
    <FeatureState
      v-else-if="albums.loaded && !albums.error"
      kind="empty"
      icon="albums"
      :title="t('albums.empty')"
      :text="t('albums.emptyHint')"
    />
    <UModal
      v-model:open="editorOpen"
      :title="editing === null ? t('albums.new') : t('albums.edit')"
      :description="t('albums.intro')"
      :content="{ onOpenAutoFocus: focusName, onCloseAutoFocus: returnFocus }"
      :close="!busy"
      :dismissible="!busy"
      :ui="{ content: 'max-w-2xl' }"
    >
      <template #body>
        <form id="album-editor" class="flex flex-col gap-4" @submit.prevent="save">
          <UFormField :label="t('albums.name')" required>
            <UInput
              ref="nameField"
              v-model="name"
              class="w-full"
              :aria-label="t('albums.name')"
              :placeholder="t('albums.name')"
              :aria-invalid="nameTooLong(name) || undefined"
              required
            />
          </UFormField>
          <UFormField v-if="editing === null" :label="t('albums.type')">
            <USelect v-model="kind" class="w-full" :items="albumKinds" :aria-label="t('albums.type')" />
          </UFormField>
          <div v-if="kind === AlbumKind.AlbumSmart" class="rule-grid">
            <UFormField :label="t('albums.kind')">
              <USelect v-model="mediaKind" class="w-full" :items="mediaKinds" :aria-label="t('albums.kind')" />
            </UFormField>
            <UFormField :label="t('albums.from')">
              <UInput v-model="rule.from" class="w-full" type="date" :aria-label="t('albums.from')" />
            </UFormField>
            <UFormField :label="t('albums.to')">
              <UInput v-model="rule.to" class="w-full" type="date" :aria-label="t('albums.to')" />
            </UFormField>
            <UFormField :label="t('albums.tag')">
              <USelect v-model="rule.tag" class="w-full" :items="tagChoices" :aria-label="t('albums.tag')" />
            </UFormField>
            <UFormField :label="t('albums.person')">
              <USelect v-model="rule.person" class="w-full" :items="personChoices" :aria-label="t('albums.person')" />
            </UFormField>
            <UCheckbox v-model="rule.favourites" :label="t('albums.favourites')" class="self-end" />
          </div>
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
          <UButton
            :icon="icons.more"
            :aria-label="t('albums.actions', { name: editedAlbum.name })"
            :disabled="busy"
            square
          />
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
          :disabled="busy || !trimName(name) || nameTooLong(name)"
          >{{ editing === null ? t('albums.create') : t('albums.save') }}</UButton
        >
      </template>
    </UModal>
  </Page>
</template>

<style scoped>
.rule-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 11rem), 1fr));
  gap: 0.75rem;
}
</style>
