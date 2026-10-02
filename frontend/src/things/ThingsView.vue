<script setup lang="ts">
// Things: what's in the photos, as cards (cover, name, count). The AI fills
// the built-in ones; people add their own ("Eiffel Tower") and the AI finds
// those across the library straight away. User labels can be deleted.
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { BlockKind, engine, MAX_NAME_LEN, TagKind } from '../bridge'
import type { Tag } from '../bridge'
import { confirm } from '../shared/confirm'
import { block } from '../shared/block'
import { errorText, logError } from '../shared/errors'
import { t } from '../shared/i18n'
import EmptyState from '../shared/EmptyState.vue'
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
const busy = ref(false)
const error = ref('')
let active = true
onBeforeUnmount(() => (active = false))

// Built-ins without photos and the internal sink kind never show.
const listed = computed(() => things.list.filter((x) => x.kind !== TagKind.TagBuiltin || x.count > 0))
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
</script>

<template>
  <div class="page">
    <header class="topbar">
      <h1>{{ t('nav.things') }}</h1>
      <SearchBox />
      <div class="spacer" />
    </header>

    <div class="page-body">
      <p class="muted intro">{{ t('things.intro') }}</p>

      <form class="new" @submit.prevent="create">
        <input
          v-model="name"
          class="field"
          :placeholder="t('things.new')"
          :aria-label="t('things.new')"
          :disabled="busy || !!aiError"
          :aria-invalid="nameTooLong(name) || undefined"
        />
        <button class="btn btn-primary" :disabled="busy || !trimName(name) || nameTooLong(name) || !!aiError">
          {{ busy ? t('things.finding') : t('things.find') }}
        </button>
      </form>
      <p v-if="nameTooLong(name)" class="page-error" role="alert">
        {{ t('error.invalidName', { max: MAX_NAME_LEN }) }}
      </p>
      <p v-if="error || things.error || aiError" class="page-error" role="alert">
        {{ error || things.error || t('settings.aiError', { error: aiError }) }}
      </p>

      <template v-if="pinned.length">
        <h2>{{ t('things.pinnedHeading') }}</h2>
        <div class="cards">
          <ThingCard
            v-for="x in pinned"
            :key="x.id"
            :tag="x"
            :icon="x.kind === TagKind.TagBuiltin ? 'things' : 'tag'"
            :deletable="x.kind !== TagKind.TagBuiltin"
            :rename="rename"
            @pin="pin(x)"
            @block="blockTag(x)"
            @remove="remove(x)"
          />
        </div>
      </template>

      <template v-if="mine.length">
        <h2>{{ t('things.yours') }}</h2>
        <div class="cards">
          <ThingCard
            v-for="x in mine"
            :key="x.id"
            :tag="x"
            icon="tag"
            deletable
            :rename="rename"
            @pin="pin(x)"
            @block="blockTag(x)"
            @remove="remove(x)"
          />
        </div>
      </template>

      <h2 v-if="mine.length && builtin.length">{{ t('nav.things') }}</h2>
      <div v-if="builtin.length" class="cards">
        <ThingCard
          v-for="x in builtin"
          :key="x.id"
          :tag="x"
          icon="things"
          :deletable="false"
          :rename="rename"
          @pin="pin(x)"
          @block="blockTag(x)"
        />
      </div>

      <EmptyState
        v-else-if="things.loaded && !mine.length && !pinned.length"
        class="empty"
        icon="things"
        :title="t('things.empty')"
        :text="understanding ? t('things.emptyWorking', { n: understanding }) : t('things.emptyHint')"
      />
    </div>
  </div>
</template>

<style scoped>
.new {
  display: flex;
  gap: 8px;
  max-width: 520px;
  margin-bottom: 8px;
}
.new .field {
  flex: 1;
}
h2 {
  margin: 20px 0 10px;
  font-size: 14px;
  font-weight: 600;
}
.empty {
  margin-top: 36px;
}
</style>
