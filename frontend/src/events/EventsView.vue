<script setup lang="ts">
import { ref } from 'vue'
import { engine, MAX_NAME_LEN } from '../bridge'
import type { Event } from '../bridge'
import { errorText } from '../shared/errors'
import { t } from '../shared/i18n'
import { useLiveReload } from '../shared/liveReload'
import { useList } from '../shared/listStore'
import EmptyState from '../shared/EmptyState.vue'
import GroupCard from '../shared/GroupCard.vue'
import Icon from '../shared/Icon.vue'
import SearchBox from '../search/SearchBox.vue'

const { list, loaded, error, load } = useList(engine.events)
const editing = ref(0)
const name = ref('')
const busy = ref(false)
useLiveReload(load, (c) => c.assets || c.thumbs)

function edit(event: Event) {
  if (busy.value) return
  editing.value = event.id
  name.value = event.name
}

async function save(id: number, value: string) {
  if (busy.value) return
  busy.value = true
  error.value = ''
  try {
    await engine.setEventName(id, value)
    editing.value = 0
    await load()
  } catch (e) {
    error.value = errorText(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="page">
    <header class="topbar">
      <h1>{{ t('nav.events') }}</h1>
      <SearchBox />
      <div class="spacer" />
    </header>
    <div class="page-body">
      <p class="muted intro">{{ t('events.intro') }}</p>
      <p v-if="error" class="page-error" role="alert">{{ error }}</p>
      <div v-if="list.length" class="groups">
        <GroupCard
          v-for="event in list"
          :id="event.id"
          :key="event.id"
          route="event"
          :title="event.name"
          :count="event.count"
          :cover="event.cover"
          :cover-thumb="event.coverThumb"
          icon="calendar"
        >
          <form v-if="editing === event.id" class="rename" @submit.prevent="save(event.id, name)">
            <input v-model="name" class="field" :aria-label="t('events.name')" :maxlength="MAX_NAME_LEN" required />
            <button class="btn" type="submit" :disabled="busy">{{ t('events.save') }}</button>
            <button class="btn btn-subtle" type="button" :disabled="busy" @click="editing = 0">
              {{ t('common.cancel') }}
            </button>
            <button class="btn btn-subtle" type="button" :disabled="busy" @click="save(event.id, '')">
              {{ t('events.reset') }}
            </button>
          </form>
          <button
            v-else
            class="icon-btn"
            :disabled="busy"
            :aria-label="t('events.rename', { name: event.name })"
            @click="edit(event)"
          >
            <Icon name="pencil" :size="16" />
          </button>
        </GroupCard>
      </div>
      <EmptyState
        v-else-if="loaded && !error"
        icon="calendar"
        :title="t('events.empty')"
        :text="t('events.emptyHint')"
      />
    </div>
  </div>
</template>

<style scoped>
.intro {
  margin: 0 0 16px;
}

.groups {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(min(100%, 360px), 1fr));
  gap: 12px;
}

.rename {
  display: flex;
  flex: 0 0 100%;
  flex-wrap: wrap;
  gap: 4px;
}

.rename input {
  min-width: 0;
  width: 100%;
}
</style>
