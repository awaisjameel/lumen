<script setup lang="ts">
import { ref, watch } from 'vue'
import { engine, MAX_NAME_LEN } from '../bridge'
import type { Event } from '../bridge'
import { errorText } from '../shared/errors'
import { t } from '../shared/i18n'
import { useLiveReload } from '../shared/liveReload'
import { useList } from '../shared/listStore'
import { useRename } from '../shared/rename'
import { nameTooLong } from '../shared/names'
import EmptyState from '../shared/EmptyState.vue'
import GroupCard from '../shared/GroupCard.vue'
import Icon from '../shared/Icon.vue'
import SearchBox from '../search/SearchBox.vue'

const { list, loaded, error, load } = useList(engine.events)
const rename = useRename<number>()
const { editing, draft } = rename
const busy = ref(false)
const actionError = ref('') // a rename's; the list's own reloads don't clear it
useLiveReload(load, (c) => c.assets || c.thumbs)

watch(list, (events) => {
  if (editing.value !== null && !events.some((event) => event.id === editing.value)) void rename.close(false)
})

async function togglePin(event: Event) {
  if (busy.value) return
  busy.value = true
  actionError.value = ''
  try {
    await engine.setEventPinned(event.id, !event.pinned)
    await load()
  } catch (e) {
    actionError.value = errorText(e)
  } finally {
    busy.value = false
  }
}

function edit(event: Event) {
  if (!busy.value) void rename.open(event.id, event.name)
}

async function save(id: number, value: string) {
  if (busy.value || nameTooLong(value)) return
  busy.value = true
  actionError.value = ''
  try {
    await engine.setEventName(id, value)
    await load()
    void rename.close()
  } catch (e) {
    actionError.value = errorText(e)
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
      <p v-if="actionError || error" class="page-error" role="alert">{{ actionError || error }}</p>
      <div v-if="list.length" class="group-cards">
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
          <form
            v-if="editing === event.id"
            class="rename"
            @submit.prevent="save(event.id, draft)"
            @keydown.esc.stop="rename.close()"
          >
            <input
              :ref="rename.fieldRef"
              v-model="draft"
              class="field"
              :aria-label="t('events.name')"
              :aria-invalid="nameTooLong(draft) || undefined"
              required
            />
            <p v-if="nameTooLong(draft)" class="page-error" role="alert">
              {{ t('error.invalidName', { max: MAX_NAME_LEN }) }}
            </p>
            <button class="btn" type="submit" :disabled="busy || nameTooLong(draft)">{{ t('events.save') }}</button>
            <button class="btn btn-subtle" type="button" :disabled="busy" @click="rename.close()">
              {{ t('common.cancel') }}
            </button>
            <button class="btn btn-subtle" type="button" :disabled="busy" @click="save(event.id, '')">
              {{ t('events.reset') }}
            </button>
          </form>
          <button
            v-if="editing !== event.id"
            :ref="rename.triggerRef(event.id)"
            class="icon-btn"
            :disabled="busy"
            :aria-label="t('events.rename', { name: event.name })"
            @click="edit(event)"
          >
            <Icon name="pencil" :size="16" />
          </button>
          <span
            v-if="event.pinned && editing !== event.id"
            class="pin-badge"
            role="img"
            :aria-label="t('events.pinned')"
          >
            <Icon name="pin" :size="16" />
          </span>
          <button
            v-if="editing !== event.id"
            class="icon-btn"
            :disabled="busy"
            :title="event.pinned ? t('events.unpin') : t('events.pin')"
            :aria-label="
              event.pinned ? t('events.unpinNamed', { name: event.name }) : t('events.pinNamed', { name: event.name })
            "
            :aria-pressed="event.pinned"
            @click="togglePin(event)"
          >
            <Icon name="pin" :size="16" />
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
.pin-badge {
  display: grid;
  place-items: center;
  color: var(--accent);
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
