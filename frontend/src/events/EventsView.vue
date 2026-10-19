<script setup lang="ts">
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { BlockKind, engine } from '../bridge'
import type { Event as LibraryEvent } from '../bridge'
import { icons } from '../design/icons'
import { block, onBlocksChanged } from '../shared/block'
import { useCommands } from '../shared/commands'
import { errorText } from '../shared/errors'
import { formatNumber, LOCALE, t } from '../shared/i18n'
import { useList } from '../shared/listStore'
import { useLiveReload } from '../shared/liveReload'
import { trimName } from '../shared/names'
import { notify } from '../shared/notify'
import FeatureState from '../shared/FeatureState.vue'
import Icon from '../shared/Icon.vue'
import IconButton from '../shared/IconButton.vue'
import Page from '../shared/Page.vue'
import PageHeader from '../shared/PageHeader.vue'
import ViewBar from '../shared/ViewBar.vue'
import WorkProgress from '../shared/WorkProgress.vue'
import EventCard from './EventCard.vue'
import TimeGap from './TimeGap.vue'

const { list, loaded, loading, error, load, invalidate } = useList(engine.events)
const places = useList(engine.places)
const hadEvents = ref(false)
watch(list, (items) => {
  if (items.length) hadEvents.value = true
})
const busy = ref(false)
const actionError = ref('')
/** The event whose change failed: a failed rename shows in that event's editor. */
const failed = ref(0)
const editingId = ref(0)
const gapOpen = ref(false)
const regrouping = ref(false)
let retry: (() => Promise<unknown>) | undefined
useLiveReload(load, (c) => c.assets || c.thumbs)
onBeforeUnmount(onBlocksChanged(invalidate))

// Months are named on their own; the year stands at the end of the heading.
// Dates are wall-clock seconds, so they are read in UTC.
const monthName = new Intl.DateTimeFormat(LOCALE, { month: 'long', timeZone: 'UTC' })
const chronological = computed(() => [...list.value].sort((a, b) => b.startAt - a.startAt || b.id - a.id))
const groups = computed(() => {
  const out: { key: string; title: string; year: string; pinned: boolean; items: LibraryEvent[] }[] = []
  const pinned = list.value.filter((event) => event.pinned)
  if (pinned.length) out.push({ key: 'pinned', title: t('events.pinned'), year: '', pinned: true, items: pinned })
  for (const event of chronological.value.filter((event) => !event.pinned)) {
    const start = new Date(event.startAt * 1000)
    const key = start.toISOString().slice(0, 7)
    const group = out.find((g) => g.key === key)
    if (group) group.items.push(event)
    else out.push({ key, title: monthName.format(start), year: key.slice(0, 4), pinned: false, items: [event] })
  }
  return out
})
const pageError = computed(() => (failed.value && failed.value === editingId.value ? '' : actionError.value))
const placeById = computed(() => new Map(places.list.value.map((place) => [place.id, place])))

async function refresh() {
  actionError.value = ''
  try {
    await load()
    notify({ id: 'events.refresh', kind: 'success', title: t('events.upToDate') })
  } catch (e) {
    actionError.value = errorText(e)
    failed.value = 0
    retry = refresh
  }
}
async function change(
  event: LibraryEvent,
  write: () => Promise<void>,
  title: string,
  undo: () => Promise<void>,
  inline = false,
) {
  if (busy.value) return false
  busy.value = true
  actionError.value = ''
  failed.value = 0
  let saved = false
  retry = () => change(event, write, title, undo, inline)
  try {
    await write()
    saved = true
    retry = load
    await load()
  } catch (e) {
    actionError.value = errorText(e)
    failed.value = inline && !saved ? event.id : 0
  } finally {
    busy.value = false
    if (saved)
      notify({
        id: `events.change.${event.id}`,
        kind: 'success',
        title,
        actions: [{ label: t('blocked.undo'), run: () => void change(event, undo, t('events.restored'), write) }],
      })
  }
  return saved
}
function togglePin(event: LibraryEvent) {
  return change(
    event,
    () => engine.setEventPinned(event.id, !event.pinned),
    t(event.pinned ? 'events.unpinnedNotice' : 'events.pinnedNotice', { name: event.name }),
    () => engine.setEventPinned(event.id, event.pinned),
  )
}
function save(event: LibraryEvent, value: string) {
  return change(
    event,
    () => engine.setEventName(event.id, trimName(value)),
    t(trimName(value) ? 'events.renamed' : 'events.resetNotice'),
    () => engine.setEventName(event.id, event.nameOverride ?? ''),
    true,
  )
}
/** Closing an editor dismisses its failed rename, rather than moving it up the page. */
function editingChanged(event: LibraryEvent, open: boolean) {
  if (open) editingId.value = event.id
  else {
    if (editingId.value === event.id) editingId.value = 0
    if (failed.value === event.id) actionError.value = ''
  }
}
const blockEvent = (event: LibraryEvent) => void block(BlockKind.BlockEvent, event.id, event.name)
async function tryAgain() {
  actionError.value = ''
  try {
    if (retry) await retry()
    else await load()
  } catch (e) {
    actionError.value = errorText(e)
  }
}
useCommands([
  {
    id: 'events.refresh',
    group: 'view',
    title: () => t('events.refresh'),
    icon: 'refresh',
    enabled: () => !busy.value && !loading.value,
    run: refresh,
  },
])
</script>

<template>
  <Page>
    <template #header
      ><PageHeader
        :title="t('nav.events')"
        :subtitle="t(list.length === 1 ? 'events.captionOne' : 'events.caption', { n: formatNumber(list.length) })"
        ><IconButton
          :icon="icons.refresh"
          :label="t('events.refresh')"
          variant="ghost"
          color="neutral"
          :disabled="loading || busy"
          :loading="loading && loaded"
          @click="refresh"
        /><UButton
          v-if="chronological[0]"
          :color="gapOpen ? 'neutral' : 'primary'"
          :variant="gapOpen ? 'outline' : 'solid'"
          :to="{ name: 'event', params: { id: chronological[0].id } }"
          :trailing-icon="icons['arrow-right']"
          >{{ t('events.latest') }}</UButton
        ></PageHeader
      ></template
    >
    <template #toolbar
      ><ViewBar :label="t('events.grouping')"
        ><TimeGap v-model:open="gapOpen" v-model:working="regrouping" :count="list.length" :reload="load" /><span
          class="gap-hint"
          >{{ t('events.quietHint') }}</span
        ></ViewBar
      ></template
    >
    <div class="events-content">
      <p v-if="loading && loaded" role="status">{{ t('events.refreshing') }}</p>
      <div v-if="regrouping" class="regroup-banner">
        <span class="lumen-light-dot" /><WorkProgress :label="t('events.working')" indeterminate class="flex-1" />
      </div>
      <UAlert
        v-if="(!error && pageError) || (loaded && error)"
        role="alert"
        orientation="horizontal"
        color="warning"
        variant="subtle"
        :icon="icons.error"
        :title="pageError || error"
        ><template #actions
          ><UButton color="neutral" variant="outline" size="sm" :disabled="busy" @click="tryAgain">{{
            t('common.retry')
          }}</UButton
          ><UButton color="neutral" variant="ghost" size="sm" @click="actionError = ''">{{
            t('common.dismiss')
          }}</UButton></template
        ></UAlert
      >
      <FeatureState v-if="error && !loaded" kind="error" :title="t('events.loadError')" :text="error"
        ><UButton @click="refresh">{{ t('events.refresh') }}</UButton></FeatureState
      >
      <section v-else-if="!loaded" aria-busy="true" :aria-label="t('events.loading')">
        <p role="status">{{ t('events.loading') }}</p>
        <div class="lumen-skeleton pinned-skeleton" />
        <div class="event-grid"><div v-for="n in 2" :key="n" class="lumen-skeleton event-skeleton" /></div>
      </section>
      <section v-for="group in groups" :key="group.key" :data-group="group.key">
        <div class="section-head" :class="{ pinned: group.pinned }">
          <h2><Icon v-if="group.pinned" name="pin" :size="16" />{{ group.title }}</h2>
          <span>{{
            t(group.items.length === 1 ? 'events.groupCountOne' : 'events.groupCount', {
              n: formatNumber(group.items.length),
            })
          }}</span>
          <time v-if="group.year" :datetime="group.year" class="year">{{ group.year }}</time>
        </div>
        <div :class="group.pinned ? 'pinned-grid' : 'event-grid'">
          <EventCard
            v-for="event in group.items"
            :key="event.id"
            :event="event"
            :place="placeById.get(event.placeId)"
            :busy="busy"
            :active-editor="editingId"
            :error="failed === event.id ? actionError : ''"
            :rename="save"
            @pin="togglePin(event)"
            @block="blockEvent(event)"
            @editing="editingChanged(event, $event)"
          />
        </div>
      </section>
      <FeatureState
        v-if="loaded && !list.length && !error"
        kind="empty"
        icon="events"
        :title="t(hadEvents ? 'events.cleared' : 'events.empty')"
        :text="t(hadEvents ? 'events.clearedHint' : 'events.emptyHint')"
        ><UButton to="/settings?section=library" color="primary" variant="solid">{{ t('events.addFolder') }}</UButton
        ><UButton to="/blocked" variant="ghost">{{ t('events.manageBlocked') }}</UButton></FeatureState
      >
      <p v-if="list.length" class="regrouping">
        <Icon name="info" :size="14" />{{ t('events.followHint')
        }}<UButton variant="ghost" size="sm" :disabled="regrouping" @click="gapOpen = true">{{
          t('events.adjustGap')
        }}</UButton>
      </p>
    </div>
  </Page>
</template>

<style scoped>
.events-content {
  display: flex;
  flex-direction: column;
  gap: 1.5rem;
}
.section-head {
  display: flex;
  align-items: baseline;
  gap: 0.75rem;
  margin-bottom: 1rem;
}
h2 {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  color: var(--ui-text-highlighted);
  font: 600 1.25rem/1.3 var(--font-display);
}
.pinned h2 {
  font-size: 1rem;
}
.section-head span,
.gap-hint,
.regrouping,
p {
  color: var(--ui-text-dimmed);
  font-size: 0.75rem;
}
.section-head span {
  font-variant-numeric: tabular-nums;
}
.year {
  margin-left: auto;
  color: var(--ui-text-dimmed);
  font-size: 0.75rem;
  font-variant-numeric: tabular-nums;
}
.regrouping {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 0.5rem;
}
.regroup-banner {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  padding: 0.75rem 1rem;
  border-radius: var(--lumen-r-lg);
  background: var(--lumen-light-soft);
}
.event-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(min(100%, 24rem), 1fr));
  gap: 1.5rem;
}
.pinned-grid {
  display: grid;
  gap: 1.5rem;
}
.pinned-skeleton {
  aspect-ratio: 4;
  margin-block: 1rem;
  border-radius: var(--lumen-r-lg);
}
.event-skeleton {
  aspect-ratio: 8 / 3;
  border-radius: var(--lumen-r-lg);
}
@container (max-width: 700px) {
  .gap-hint {
    display: none;
  }
}
</style>
