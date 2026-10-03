<script setup lang="ts">
// Things: what's in the photos, as cards (cover, name, count). The AI fills
// the built-in ones; people add their own ("Eiffel Tower") and the AI finds
// those across the library straight away. User labels can be deleted.
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import { icons } from '../design/icons'
import CardGrid from '../shared/CardGrid.vue'
import { useCommands } from '../shared/commands'
import Page from '../shared/Page.vue'
import PageHeader from '../shared/PageHeader.vue'
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { BlockKind, engine, MAX_NAME_LEN, TagKind } from '../bridge'
import type { Tag } from '../bridge'
import { confirm } from '../shared/confirm'
import { block } from '../shared/block'
import { errorText, logError } from '../shared/errors'
import { t } from '../shared/i18n'
import FeatureState from '../shared/FeatureState.vue'
import { useLiveReload } from '../shared/liveReload'
import { nameTooLong, trimName } from '../shared/names'
import { useLibrary } from '../library/store'
import SearchBox from '../search/SearchBox.vue'
import ThingCard from './ThingCard.vue'
import { useThings } from './store'

const router = useRouter()
const lib = useLibrary()
const things = useThings()

const name = ref('')
const query = ref('')
const busy = ref(false)
const error = ref('')
let active = true
onBeforeUnmount(() => (active = false))

// Built-ins without photos and the internal sink kind never show.
const listed = computed(() =>
  things.list.filter(
    (x) =>
      (x.kind !== TagKind.TagBuiltin || x.count > 0) &&
      [x.name, x.original].some((n) => n.toLocaleLowerCase().includes(query.value.trim().toLocaleLowerCase())),
  ),
)
// Pinned Things come first whoever made them (the engine sends them newest pin first).
const pinned = computed(() => listed.value.filter((x) => x.pinned))
const mine = computed(() => listed.value.filter((x) => !x.pinned && x.kind !== TagKind.TagBuiltin))
const builtin = computed(() => listed.value.filter((x) => !x.pinned && x.kind === TagKind.TagBuiltin))
const aiError = computed(() => lib.progress?.ai.error ?? '')
const understanding = computed(() => lib.progress?.embedding ?? 0)

onMounted(() => void things.load().catch(logError))
useLiveReload(things.load, (c) => c.tags || c.assets || c.thumbs) // counts grow while the AI works

async function create() {
  const n = trimName(name.value)
  if (!n || nameTooLong(name.value) || busy.value) return
  busy.value = true
  error.value = ''
  try {
    const id = await engine.createLabel(n) // the first one loads the text model: seconds
    await things.load()
    if (!active) return // the user went elsewhere meanwhile: stay there
    name.value = ''
    void router.push({ name: 'thing', params: { id } })
  } catch (e) {
    if (active) error.value = errorText(e)
  } finally {
    busy.value = false
  }
}

/** Run a change to the Things, then reload them; failures show inline. */
async function act(fn: () => Promise<void>) {
  error.value = ''
  try {
    await fn()
    await things.load()
  } catch (e) {
    error.value = errorText(e)
  }
}

const pin = (tag: Tag) => void act(() => engine.setThingPinned(tag.id, !tag.pinned))

/** Saves a Thing's new name; false (with the reason shown) if it was refused. */
async function rename(id: number, name: string): Promise<boolean> {
  error.value = ''
  try {
    await engine.renameThing(id, name)
    await things.load()
    return true
  } catch (e) {
    error.value = errorText(e)
    return false
  }
}

const blockTag = (tag: Tag) => void block(BlockKind.BlockTag, tag.id, tag.name)

async function remove(tag: Tag) {
  if (await confirm(t('things.deleteTitle', { name: tag.name }), t('things.deleteText'), t('things.delete'))) {
    await act(() => engine.deleteTag(tag.id))
  }
}
useCommands([
  {
    id: 'things.create',
    group: 'library',
    title: () => t('things.find'),
    icon: 'plus',
    enabled: () => !busy.value && !aiError.value && !!trimName(name.value) && !nameTooLong(name.value),
    run: create,
  },
])
</script>

<template>
  <Page>
    <template #header
      ><PageHeader :title="t('nav.things')"><SearchBox /></PageHeader
    ></template>
    <p class="mb-4 text-muted">{{ t('things.intro') }}</p>
    <div class="mb-4 flex flex-wrap gap-2">
      <UInput
        v-model="query"
        :icon="icons.search"
        :placeholder="t('things.search')"
        :aria-label="t('things.search')"
        class="min-w-48 flex-1"
      />
      <form class="new flex min-w-0 flex-1 gap-2" @submit.prevent="create">
        <UInput
          v-model="name"
          class="min-w-0 flex-1"
          :placeholder="t('things.new')"
          :aria-label="t('things.new')"
          :disabled="busy || !!aiError"
          :aria-invalid="nameTooLong(name) || undefined"
        />
        <UButton
          type="submit"
          color="primary"
          variant="solid"
          :loading="busy"
          :disabled="busy || !trimName(name) || nameTooLong(name) || !!aiError"
          >{{ busy ? t('things.finding') : t('things.find') }}</UButton
        >
      </form>
    </div>
    <UAlert
      v-if="nameTooLong(name)"
      class="mb-3"
      role="alert"
      color="error"
      variant="subtle"
      :title="t('error.invalidName', { max: MAX_NAME_LEN })"
    />
    <FeatureState
      v-if="error || things.error || aiError"
      kind="error"
      :title="error || things.error || t('settings.aiError', { error: aiError })"
    />
    <FeatureState v-else-if="!things.loaded" kind="loading" :title="t('things.loading')" />
    <template v-if="pinned.length">
      <h2 class="mb-3 mt-5 text-sm font-semibold">{{ t('things.pinnedHeading') }}</h2>
      <CardGrid
        ><ThingCard
          v-for="x in pinned"
          :key="x.id"
          :tag="x"
          :icon="x.kind === TagKind.TagBuiltin ? 'things' : 'tag'"
          :deletable="x.kind !== TagKind.TagBuiltin"
          :rename="rename"
          @pin="pin(x)"
          @block="blockTag(x)"
          @remove="remove(x)"
      /></CardGrid>
    </template>
    <template v-if="mine.length">
      <h2 class="mb-3 mt-5 text-sm font-semibold">{{ t('things.yours') }}</h2>
      <CardGrid
        ><ThingCard
          v-for="x in mine"
          :key="x.id"
          :tag="x"
          icon="tag"
          deletable
          :rename="rename"
          @pin="pin(x)"
          @block="blockTag(x)"
          @remove="remove(x)"
      /></CardGrid>
    </template>
    <h2 v-if="mine.length && builtin.length" class="mb-3 mt-5 text-sm font-semibold">{{ t('nav.things') }}</h2>
    <CardGrid v-if="builtin.length"
      ><ThingCard
        v-for="x in builtin"
        :key="x.id"
        :tag="x"
        icon="things"
        :deletable="false"
        :rename="rename"
        @pin="pin(x)"
        @block="blockTag(x)"
    /></CardGrid>
    <FeatureState
      v-if="things.loaded && !listed.length && !error && !things.error && !aiError"
      kind="empty"
      icon="things"
      :title="query ? t('things.noMatches') : t('things.empty')"
      :text="
        query
          ? t('things.noMatchesHint')
          : understanding
            ? t('things.emptyWorking', { n: understanding })
            : t('things.emptyHint')
      "
    />
  </Page>
</template>
