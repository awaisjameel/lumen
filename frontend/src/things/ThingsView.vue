<script setup lang="ts">
// Things: what's in the photos, as cards (cover, name, count). The AI fills
// the built-in ones; people add their own ("Eiffel Tower") and the AI finds
// those across the library straight away. User labels can be deleted.
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { BlockKind, engine, MAX_NAME_LEN, TagKind, thumbUrl } from '../bridge'
import type { Tag } from '../bridge'
import { confirm } from '../shared/confirm'
import { block } from '../shared/block'
import { errorText, logError } from '../shared/errors'
import { t } from '../shared/i18n'
import EmptyState from '../shared/EmptyState.vue'
import Icon from '../shared/Icon.vue'
import { useLiveReload } from '../shared/liveReload'
import { nameTooLong, trimName } from '../shared/names'
import { useLibrary } from '../library/store'
import SearchBox from '../search/SearchBox.vue'
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
const mine = computed(() => listed.value.filter((x) => x.kind !== TagKind.TagBuiltin))
const builtin = computed(() => listed.value.filter((x) => x.kind === TagKind.TagBuiltin))
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

      <template v-if="mine.length">
        <h2>{{ t('things.yours') }}</h2>
        <div class="cards">
          <div v-for="x in mine" :key="x.id" class="card">
            <RouterLink :to="{ name: 'thing', params: { id: x.id } }" class="card-cover" :aria-label="x.name">
              <img
                v-if="x.cover && x.coverThumb"
                :src="thumbUrl(x.cover, x.coverThumb)"
                alt=""
                loading="lazy"
                decoding="async"
                draggable="false"
              />
              <Icon v-else name="tag" :size="32" />
            </RouterLink>
            <div class="card-meta">
              <span class="card-name">{{ x.name }}</span>
              <span class="card-count">{{ x.count ? t('status.items', { n: x.count }) : t('things.noPhotos') }}</span>
            </div>
            <button
              class="icon-btn card-act block-act"
              :title="t('blocked.block')"
              :aria-label="t('blocked.confirmTitle', { name: x.name })"
              @click="blockTag(x)"
            >
              <Icon name="eye-off" :size="16" />
            </button>
            <button
              class="icon-btn card-act"
              :title="t('things.delete')"
              :aria-label="t('things.deleteNamed', { name: x.name })"
              @click="remove(x)"
            >
              <Icon name="trash" :size="16" />
            </button>
          </div>
        </div>
      </template>

      <h2 v-if="mine.length && builtin.length">{{ t('nav.things') }}</h2>
      <div v-if="builtin.length" class="cards">
        <div v-for="x in builtin" :key="x.id" class="card">
          <RouterLink :to="{ name: 'thing', params: { id: x.id } }" class="card-cover" :aria-label="x.name">
            <img
              v-if="x.cover && x.coverThumb"
              :src="thumbUrl(x.cover, x.coverThumb)"
              alt=""
              loading="lazy"
              decoding="async"
              draggable="false"
            />
            <Icon v-else name="things" :size="32" />
          </RouterLink>
          <div class="card-meta">
            <span class="card-name">{{ x.name }}</span>
            <span class="card-count">{{ t('status.items', { n: x.count }) }}</span>
          </div>
          <button
            class="icon-btn card-act"
            :title="t('blocked.block')"
            :aria-label="t('blocked.confirmTitle', { name: x.name })"
            @click="blockTag(x)"
          >
            <Icon name="eye-off" :size="16" />
          </button>
        </div>
      </div>

      <EmptyState
        v-else-if="things.loaded && !mine.length"
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
.block-act {
  right: 42px;
}
</style>
