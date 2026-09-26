<script setup lang="ts">
// Things: what's in the photos, as cards (cover, name, count). The AI fills
// the built-in ones; people add their own ("Eiffel Tower") and the AI finds
// those across the library straight away. Built-ins can be hidden, the
// user's own deleted.
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { engine, TagKind, thumbUrl } from '../bridge'
import type { Tag } from '../bridge'
import { errorText } from '../shared/errors'
import { t } from '../shared/i18n'
import Icon from '../shared/Icon.vue'
import { useLiveReload } from '../shared/listStore'
import { useLibrary } from '../library/store'
import SearchBox from '../search/SearchBox.vue'
import { useThings } from './store'

const router = useRouter()
const lib = useLibrary()
const things = useThings()

const showHidden = ref(false)
const name = ref('')
const busy = ref(false)
const error = ref('')

const mine = computed(() => things.list.filter((x) => x.kind !== TagKind.TagBuiltin && (!x.hidden || showHidden.value)))
const builtin = computed(() =>
  things.list.filter((x) => x.kind === TagKind.TagBuiltin && x.count > 0 && (!x.hidden || showHidden.value)),
)
// Built-ins without photos never show, hidden or not.
const hiddenCount = computed(
  () => things.list.filter((x) => x.hidden && (x.kind !== TagKind.TagBuiltin || x.count > 0)).length,
)
const aiError = computed(() => lib.progress?.ai.error ?? '')
const understanding = computed(() => lib.progress?.embedding ?? 0)

onMounted(() => void things.load().catch((e: unknown) => (error.value = errorText(e))))
useLiveReload(things.load) // counts grow while the AI works

async function create() {
  const n = name.value.trim()
  if (!n || busy.value) return
  busy.value = true
  error.value = ''
  try {
    const id = await engine.createLabel(n)
    name.value = ''
    await things.load()
    void router.push({ name: 'thing', params: { id } })
  } catch (e) {
    error.value = errorText(e)
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
const remove = (tag: Tag) => act(() => engine.deleteTag(tag.id))
</script>

<template>
  <div class="things">
    <header class="topbar">
      <h1>{{ t('nav.things') }}</h1>
      <SearchBox />
      <div class="spacer" />
    </header>

    <div class="body">
      <p class="muted intro">{{ t('things.intro') }}</p>

      <form class="new" @submit.prevent="create">
        <input
          v-model="name"
          :placeholder="t('things.new')"
          :aria-label="t('things.new')"
          :disabled="busy || !!aiError"
          maxlength="60"
        />
        <button class="btn btn-primary" :disabled="busy || !name.trim() || !!aiError">
          {{ busy ? t('things.finding') : t('things.find') }}
        </button>
      </form>
      <p v-if="error || aiError" class="error" role="alert">
        {{ error || t('settings.aiError', { error: aiError }) }}
      </p>

      <template v-if="mine.length">
        <h2>{{ t('things.yours') }}</h2>
        <div class="cards">
          <div v-for="x in mine" :key="x.id" class="card">
            <RouterLink :to="{ name: 'thing', params: { id: x.id } }" class="cover">
              <img v-if="x.cover" :src="thumbUrl(x.cover, x.coverThumb)" alt="" loading="lazy" draggable="false" />
              <Icon v-else name="tag" :size="32" />
            </RouterLink>
            <div class="meta">
              <span class="name">{{ x.name }}</span>
              <span class="count">{{ x.count ? t('status.items', { n: x.count }) : t('things.noPhotos') }}</span>
            </div>
            <button class="icon-btn act" :title="t('things.delete')" @click="remove(x)">
              <Icon name="trash" :size="16" />
            </button>
          </div>
        </div>
      </template>

      <h2 v-if="mine.length && builtin.length">{{ t('nav.things') }}</h2>
      <div v-if="builtin.length" class="cards">
        <div v-for="x in builtin" :key="x.id" class="card" :class="{ hidden: x.hidden }">
          <RouterLink :to="{ name: 'thing', params: { id: x.id } }" class="cover">
            <img v-if="x.cover" :src="thumbUrl(x.cover, x.coverThumb)" alt="" loading="lazy" draggable="false" />
            <Icon v-else name="things" :size="32" />
          </RouterLink>
          <div class="meta">
            <span class="name">{{ x.name }}</span>
            <span class="count">{{ t('status.items', { n: x.count }) }}</span>
          </div>
          <button class="icon-btn act" :title="x.hidden ? t('things.unhide') : t('things.hide')" @click="hide(x)">
            <Icon :name="x.hidden ? 'eye' : 'eye-off'" :size="16" />
          </button>
        </div>
      </div>

      <div v-else-if="things.loaded && !mine.length" class="empty">
        <Icon name="things" :size="40" />
        <h2>{{ t('things.empty') }}</h2>
        <p class="muted">
          {{ understanding ? t('things.emptyWorking', { n: understanding }) : t('things.emptyHint') }}
        </p>
      </div>

      <button v-if="hiddenCount" class="btn subtle" @click="showHidden = !showHidden">
        {{ showHidden ? t('things.hideHidden') : `${t('things.showHidden')} (${t('things.hidden', { n: hiddenCount })})` }}
      </button>
    </div>
  </div>
</template>

<style scoped>
.things {
  display: flex;
  flex-direction: column;
  height: 100%;
}
.topbar {
  display: flex;
  align-items: center;
  gap: 16px;
  height: var(--topbar-h);
  padding: 0 12px 0 20px;
  flex-shrink: 0;
}
.topbar h1 {
  margin: 0;
  font: 600 20px/1 var(--font-display);
}
.spacer {
  flex: 1;
}
.body {
  flex: 1;
  overflow-y: auto;
  padding: 4px 20px 24px;
}
.intro {
  margin: 0 0 12px;
}
.new {
  display: flex;
  gap: 8px;
  max-width: 520px;
  margin-bottom: 8px;
}
.new input {
  flex: 1;
  height: 36px;
  padding: 0 12px;
  border: 1px solid var(--border);
  border-radius: var(--radius-m);
  background: var(--surface-2);
  color: var(--text);
  font: inherit;
  outline: none;
}
.new input:focus {
  border-color: var(--accent);
}
.error {
  margin: 0 0 8px;
  color: var(--danger);
}
h2 {
  margin: 20px 0 10px;
  font-size: 14px;
  font-weight: 600;
}
.cards {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
  gap: 14px;
}
.card {
  position: relative;
  min-width: 0;
}
.card.hidden {
  opacity: 0.5;
}
.cover {
  display: grid;
  place-items: center;
  aspect-ratio: 1;
  border-radius: var(--radius-m);
  overflow: hidden;
  background: var(--placeholder);
  color: var(--text-3);
}
.cover img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  transition: transform 0.2s ease;
}
.cover:hover img {
  transform: scale(1.04);
}
.meta {
  display: flex;
  flex-direction: column;
  padding: 6px 2px 0;
}
.name {
  font-weight: 600;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}
.count {
  color: var(--text-3);
  font-size: 12px;
}
.act {
  position: absolute;
  top: 6px;
  right: 6px;
  width: 30px;
  height: 30px;
  background: var(--surface);
  opacity: 0;
  transition: opacity 0.12s;
}
.card:hover .act,
.act:focus-visible {
  opacity: 1;
}
.empty {
  display: grid;
  justify-items: center;
  gap: 4px;
  margin-top: 60px;
  text-align: center;
  color: var(--text-3);
}
.empty h2 {
  margin: 8px 0 0;
  color: var(--text);
  font: 600 18px var(--font-display);
}
.empty p {
  margin: 0;
}
.subtle {
  margin-top: 20px;
}
</style>
