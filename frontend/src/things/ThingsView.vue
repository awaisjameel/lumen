<script setup lang="ts">
// Things: what's in the photos, as cards (cover, name, count). The AI fills
// the built-in ones; people add their own ("Eiffel Tower") and the AI finds
// those across the library straight away. Built-ins can be hidden, the
// user's own deleted.
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { engine, MAX_NAME_LEN, TagKind, thumbUrl } from '../bridge'
import type { Tag } from '../bridge'
import { confirm } from '../shared/confirm'
import { errorText, logError } from '../shared/errors'
import { useHidden } from '../shared/hidden'
import { t } from '../shared/i18n'
import EmptyState from '../shared/EmptyState.vue'
import Icon from '../shared/Icon.vue'
import { useLiveReload } from '../shared/liveReload'
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

// Built-ins without photos never show, hidden or not.
const listed = computed(() => things.list.filter((x) => x.kind !== TagKind.TagBuiltin || x.count > 0))
const { showHidden, hiddenCount, shown, toggleLabel } = useHidden(listed)
const mine = computed(() => shown.value.filter((x) => x.kind !== TagKind.TagBuiltin))
const builtin = computed(() => shown.value.filter((x) => x.kind === TagKind.TagBuiltin))
const aiError = computed(() => lib.progress?.ai.error ?? '')
const understanding = computed(() => lib.progress?.embedding ?? 0)

onMounted(() => void things.load().catch(logError))
useLiveReload(things.load, (c) => c.tags || c.assets) // counts grow while the AI works

async function create() {
  const n = name.value.trim()
  if (!n || busy.value) return
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

const hide = (tag: Tag) => act(() => engine.setTagHidden(tag.id, !tag.hidden))

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
          :maxlength="MAX_NAME_LEN"
        />
        <button class="btn btn-primary" :disabled="busy || !name.trim() || !!aiError">
          {{ busy ? t('things.finding') : t('things.find') }}
        </button>
      </form>
      <p v-if="error || things.error || aiError" class="page-error" role="alert">
        {{ error || things.error || t('settings.aiError', { error: aiError }) }}
      </p>

      <template v-if="mine.length">
        <h2>{{ t('things.yours') }}</h2>
        <div class="cards">
          <div v-for="x in mine" :key="x.id" class="card" :class="{ hidden: x.hidden }">
            <RouterLink :to="{ name: 'thing', params: { id: x.id } }" class="card-cover" :aria-label="x.name">
              <img
                v-if="x.cover"
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
        <div v-for="x in builtin" :key="x.id" class="card" :class="{ hidden: x.hidden }">
          <RouterLink :to="{ name: 'thing', params: { id: x.id } }" class="card-cover" :aria-label="x.name">
            <img
              v-if="x.cover"
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
            :title="x.hidden ? t('things.unhide') : t('things.hide')"
            :aria-label="x.hidden ? t('things.unhide') : t('things.hide')"
            @click="hide(x)"
          >
            <Icon :name="x.hidden ? 'eye' : 'eye-off'" :size="16" />
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

      <button v-if="hiddenCount" class="btn btn-subtle show-hidden" @click="showHidden = !showHidden">
        {{ toggleLabel }}
      </button>
    </div>
  </div>
</template>

<style scoped>
.intro {
  margin: 0 0 12px;
}
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
.show-hidden {
  margin-top: 20px;
}
</style>
