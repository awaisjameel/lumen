<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { AlbumKind, engine, Kind, thumbUrl } from '../bridge'
import type { Album, AlbumRule } from '../bridge'
import { usePeople } from '../people/store'
import { confirm } from '../shared/confirm'
import { errorText, logError } from '../shared/errors'
import { t } from '../shared/i18n'
import { useLiveReload } from '../shared/liveReload'
import EmptyState from '../shared/EmptyState.vue'
import Icon from '../shared/Icon.vue'
import SearchBox from '../search/SearchBox.vue'
import { useThings } from '../things/store'
import { useAlbums } from './store'

const router = useRouter()
const albums = useAlbums()
const things = useThings()
const people = usePeople()
const name = ref('')
const kind = ref(AlbumKind.AlbumManual)
const rule = ref<AlbumRule>(emptyRule())
const editing = ref<number | null>(null)
const busy = ref(false)
const error = ref('')
const active = ref(true)
onBeforeUnmount(() => (active.value = false))

function emptyRule(): AlbumRule {
  return { kind: Kind.$zero, favourites: false, from: '', to: '', tag: 0, person: 0 }
}

onMounted(() => {
  void albums.load().catch((e: unknown) => (error.value = errorText(e)))
  void things.load().catch(logError)
  void people.load().catch(logError)
})
useLiveReload(albums.load)

function reset() {
  editing.value = null
  name.value = ''
  kind.value = AlbumKind.AlbumManual
  rule.value = emptyRule()
}

function edit(album: Album) {
  if (busy.value) return
  editing.value = album.id
  name.value = album.name
  kind.value = album.kind
  rule.value = { ...album.rule }
}

async function save() {
  if (!name.value.trim() || busy.value) return
  busy.value = true
  error.value = ''
  try {
    const id = editing.value
    if (id === null) {
      const created = await engine.createAlbum(name.value, kind.value, rule.value)
      await albums.load()
      if (!active.value) return
      reset()
      void router.push({ name: 'album', params: { id: created } })
    } else {
      await engine.updateAlbum(id, name.value, rule.value)
      await albums.load()
      reset()
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
</script>

<template>
  <div class="page">
    <header class="topbar">
      <h1>{{ t('nav.albums') }}</h1>
      <SearchBox />
      <div class="spacer" />
    </header>
    <div class="page-body">
      <p class="muted intro">{{ t('albums.intro') }}</p>
      <form class="editor" @submit.prevent="save">
        <div class="editor-head">
          <h2>{{ editing === null ? t('albums.new') : t('albums.edit') }}</h2>
          <button v-if="editing !== null" type="button" class="btn subtle" :disabled="busy" @click="reset">
            {{ t('common.cancel') }}
          </button>
        </div>
        <div class="form-row">
          <input
            v-model="name"
            :aria-label="t('albums.name')"
            :placeholder="t('albums.name')"
            maxlength="80"
            required
          />
          <select v-if="editing === null" v-model="kind" :aria-label="t('albums.type')">
            <option :value="AlbumKind.AlbumManual">{{ t('albums.manual') }}</option>
            <option :value="AlbumKind.AlbumSmart">{{ t('albums.smart') }}</option>
          </select>
        </div>
        <div v-if="kind === AlbumKind.AlbumSmart" class="rules">
          <label
            >{{ t('albums.kind') }}
            <select v-model="rule.kind">
              <option :value="Kind.$zero">{{ t('albums.anyMedia') }}</option>
              <option :value="Kind.KindPhoto">{{ t('albums.photos') }}</option>
              <option :value="Kind.KindVideo">{{ t('albums.videos') }}</option>
            </select>
          </label>
          <label>{{ t('albums.from') }} <input v-model="rule.from" type="date" /></label>
          <label>{{ t('albums.to') }} <input v-model="rule.to" type="date" /></label>
          <label
            >{{ t('albums.tag') }}
            <select v-model.number="rule.tag">
              <option :value="0">{{ t('albums.anyTag') }}</option>
              <option v-for="tag in things.list" :key="tag.id" :value="tag.id">{{ tag.name }}</option>
            </select>
          </label>
          <label
            >{{ t('albums.person') }}
            <select v-model.number="rule.person">
              <option :value="0">{{ t('albums.anyPerson') }}</option>
              <option v-for="person in people.list" :key="person.id" :value="person.id">
                {{ person.name || t('person.unnamed') }}
              </option>
            </select>
          </label>
          <label class="check"><input v-model="rule.favourites" type="checkbox" />{{ t('albums.favourites') }}</label>
        </div>
        <button class="btn btn-primary" :disabled="busy || !name.trim()">
          {{ editing === null ? t('albums.create') : t('albums.save') }}
        </button>
      </form>
      <p v-if="error" class="error" role="alert">{{ error }}</p>

      <div v-if="albums.list.length" class="cards">
        <div v-for="album in albums.list" :key="album.id" class="card">
          <RouterLink :to="{ name: 'album', params: { id: album.id } }" class="cover" :aria-label="album.name">
            <img v-if="album.cover" :src="thumbUrl(album.cover, album.coverThumb)" alt="" loading="lazy" />
            <Icon v-else name="folder" :size="36" />
          </RouterLink>
          <div class="meta">
            <RouterLink :to="{ name: 'album', params: { id: album.id } }">{{ album.name }}</RouterLink>
            <span class="muted"
              >{{ t('status.items', { n: album.count }) }} ·
              {{ t(album.kind === AlbumKind.AlbumSmart ? 'albums.smart' : 'albums.manual') }}</span
            >
          </div>
          <button class="icon-btn" :title="t('albums.edit')" @click="edit(album)">
            <Icon name="pencil" :size="16" />
          </button>
          <button class="icon-btn" :title="t('albums.delete')" @click="remove(album)">
            <Icon name="trash" :size="16" />
          </button>
        </div>
      </div>
      <EmptyState v-else-if="albums.loaded" icon="folder" :title="t('albums.empty')" :text="t('albums.emptyHint')" />
    </div>
  </div>
</template>

<style scoped>
.intro {
  margin: 0 0 16px;
}
.editor {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 12px;
  padding: 16px;
  border: 1px solid var(--border);
  border-radius: var(--radius-l);
  margin-bottom: 20px;
}
.editor-head,
.form-row {
  display: flex;
  align-items: center;
  gap: 12px;
  width: 100%;
}
.editor-head {
  justify-content: space-between;
}
h2 {
  margin: 0;
  font-size: 16px;
}
.form-row input {
  flex: 1;
  min-width: 0;
}
input:not([type='checkbox']),
select {
  min-height: 32px;
  padding: 4px 8px;
  border: 1px solid var(--border);
  border-radius: var(--radius-s);
  background: var(--surface);
}
.rules {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(165px, 1fr));
  gap: 12px;
  width: 100%;
}
.rules label {
  display: flex;
  flex-direction: column;
  gap: 4px;
  color: var(--text-2);
  font-size: 12px;
}
.rules label.check {
  flex-direction: row;
  align-items: center;
  align-self: end;
  min-height: 32px;
  font-size: 14px;
}
.cards {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(170px, 1fr));
  gap: 18px;
}
.card {
  min-width: 0;
}
.cover {
  display: grid;
  place-items: center;
  aspect-ratio: 1;
  border-radius: var(--radius-m);
  overflow: hidden;
  background: var(--surface-2);
  color: var(--text-3);
}
.cover img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}
.meta {
  display: flex;
  flex-direction: column;
  margin: 6px 0;
}
.meta a {
  color: var(--text);
  font-weight: 600;
  text-decoration: none;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.meta .muted {
  font-size: 12px;
}
</style>
