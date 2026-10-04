<script setup lang="ts">
import UBadge from '@nuxt/ui/components/Badge.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UDropdownMenu from '@nuxt/ui/components/DropdownMenu.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import { icons } from '../design/icons'
import CardGrid from '../shared/CardGrid.vue'
import CollectionCard from '../shared/CollectionCard.vue'
import { useItemCommands, useCommands } from '../shared/commands'
import FeatureState from '../shared/FeatureState.vue'
import Page from '../shared/Page.vue'
import PageHeader from '../shared/PageHeader.vue'
import { onBeforeUnmount, ref, watch } from 'vue'
import { BlockKind, engine, MAX_NAME_LEN, thumbUrl } from '../bridge'
import type { Event as LibraryEvent } from '../bridge'
import { block, onBlocksChanged } from '../shared/block'
import { errorText, logError } from '../shared/errors'
import { t } from '../shared/i18n'
import { useLiveReload } from '../shared/liveReload'
import { useList } from '../shared/listStore'
import { useRename } from '../shared/rename'
import { nameTooLong } from '../shared/names'
import SearchBox from '../search/SearchBox.vue'

const { list, loaded, error, load, invalidate } = useList(engine.events)
const rename = useRename<number>()
const { editing, draft } = rename
const busy = ref(false)
const actionError = ref('') // a rename's; the list's own reloads don't clear it
useLiveReload(load, (c) => c.assets || c.thumbs)
// A block, its Undo or its removal on the Blocked screen changes which events
// there are, whoever made it.
onBeforeUnmount(onBlocksChanged(invalidate))

watch(list, (events) => {
  if (editing.value !== null && !events.some((event) => event.id === editing.value)) void rename.close(false)
})

async function togglePin(event: LibraryEvent) {
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

function blockEvent(event: LibraryEvent) {
  void block(BlockKind.BlockEvent, event.id, event.name)
}

function edit(event: LibraryEvent) {
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
const eventItems = useItemCommands(
  list,
  (event) => event.id,
  (event) => [
    {
      id: `event.${event.id}.rename`,
      group: 'view',
      title: () => t('events.rename', { name: event.name }),
      icon: 'pencil',
      enabled: () => !busy.value,
      run: () => edit(event),
    },
    {
      id: `event.${event.id}.pin`,
      group: 'view',
      title: () => t(event.pinned ? 'events.unpinNamed' : 'events.pinNamed', { name: event.name }),
      icon: 'pin',
      enabled: () => !busy.value,
      run: () => togglePin(event),
    },
    {
      id: `event.${event.id}.block`,
      group: 'view',
      title: () => t('blocked.confirmTitle', { name: event.name }),
      icon: 'eye-off',
      enabled: () => !busy.value,
      run: () => blockEvent(event),
    },
  ],
)
function keepEditorFocus(e: Event) {
  if (editing.value !== null) e.preventDefault()
}
useCommands([
  {
    id: 'events.refresh',
    group: 'view',
    title: () => t('events.refresh'),
    icon: 'refresh',
    enabled: () => !busy.value,
    run: () => load().catch(logError),
  },
  {
    id: 'events.save',
    group: 'view',
    title: () => t('events.save'),
    icon: 'save',
    available: () => editing.value !== null,
    enabled: () => !busy.value && !nameTooLong(draft.value),
    run: () => {
      if (editing.value !== null) return save(editing.value, draft.value)
    },
  },
  {
    id: 'events.reset',
    group: 'view',
    title: () => t('events.reset'),
    icon: 'reset',
    available: () => editing.value !== null,
    enabled: () => !busy.value,
    run: () => {
      if (editing.value !== null) return save(editing.value, '')
    },
  },
])
// Nuxt UI exposes the native input; rename keeps its focus and selection behaviour.
function fieldRef(el: unknown) {
  rename.fieldRef(
    el && typeof el === 'object' && 'inputRef' in el && el.inputRef instanceof HTMLInputElement ? el.inputRef : null,
  )
}
function triggerRef(el: unknown, key: number) {
  rename.triggerRef(key)(el && typeof el === 'object' && '$el' in el && el.$el instanceof HTMLElement ? el.$el : null)
}
</script>

<template>
  <Page>
    <template #header
      ><PageHeader :title="t('nav.events')"><SearchBox /></PageHeader
    ></template>
    <p class="mb-2 text-muted">{{ t('events.intro') }}</p>
    <p class="mb-4 text-sm text-dimmed">{{ t('events.regrouping') }}</p>
    <FeatureState v-if="actionError || error" kind="error" :title="actionError || error" />
    <FeatureState v-else-if="!loaded" kind="loading" :title="t('events.loading')" />
    <CardGrid v-if="list.length" layout="row">
      <CollectionCard
        v-for="event in list"
        :key="event.id"
        :to="{ name: 'event', params: { id: event.id } }"
        :title="event.name"
        :count="t('status.items', { n: event.count })"
        :cover="event.cover && event.coverThumb ? thumbUrl(event.cover, event.coverThumb) : undefined"
        icon="events"
        layout="row"
      >
        <template #badge
          ><UBadge
            v-if="event.pinned"
            data-pinned
            :icon="icons.pin"
            variant="subtle"
            :aria-label="t('events.pinned')"
            >{{ t('events.pinned') }}</UBadge
          ></template
        >
        <form
          v-if="editing === event.id"
          class="rename"
          @submit.prevent="save(event.id, draft)"
          @keydown.esc.stop="rename.close()"
        >
          <UInput
            :ref="fieldRef"
            v-model="draft"
            class="w-full"
            :aria-label="t('events.name')"
            :aria-invalid="nameTooLong(draft) || undefined"
            required
          />
          <p v-if="nameTooLong(draft)" class="text-xs text-error" role="alert">
            {{ t('error.invalidName', { max: MAX_NAME_LEN }) }}
          </p>
          <UButton type="submit" :loading="busy" :disabled="busy || nameTooLong(draft)">{{ t('events.save') }}</UButton>
          <UButton type="button" variant="ghost" :disabled="busy" @click="rename.close()">{{
            t('common.cancel')
          }}</UButton>
          <UButton type="button" data-action="reset" variant="ghost" :disabled="busy" @click="save(event.id, '')">{{
            t('events.reset')
          }}</UButton>
        </form>
        <UDropdownMenu v-else :items="eventItems(event.id)" :content="{ onCloseAutoFocus: keepEditorFocus }">
          <UButton
            :ref="(el) => triggerRef(el, event.id)"
            :icon="icons.more"
            :aria-label="t('events.actions', { name: event.name })"
            :disabled="busy"
            square
          />
        </UDropdownMenu>
      </CollectionCard>
    </CardGrid>
    <FeatureState
      v-else-if="loaded && !error"
      kind="empty"
      icon="events"
      :title="t('events.empty')"
      :text="t('events.emptyHint')"
    />
  </Page>
</template>

<style scoped>
.rename {
  display: flex;
  flex: 0 0 100%;
  flex-wrap: wrap;
  gap: 0.25rem;
  min-width: 0;
}
</style>
