<script setup lang="ts">
// Duplicates: exact file copies and similar-looking photos. Each group
// pre-selects the copy to keep (the engine's suggestion, or the user's own
// pick); its Review button lists what would happen and does nothing until the
// user confirms. Similar photos go to Lumen's Trash, and the extra files of
// exact copies to the Recycle Bin.
import IconButton from '../shared/IconButton.vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UBadge from '@nuxt/ui/components/Badge.vue'
import UModal from '@nuxt/ui/components/Modal.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import ViewBar from '../shared/ViewBar.vue'
import Segmented from '../shared/Segmented.vue'
import { computed, nextTick, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
import type { ComponentPublicInstance } from 'vue'
import { useVirtualizer } from '@tanstack/vue-virtual'
import { DuplicateKind, engine, THUMB_LARGE, thumbUrl } from '../bridge'
import type { DuplicateGroup, DuplicateItem } from '../bridge'
import { icons } from '../design/icons'
import { useLibrary } from '../library/store'
import { runCommand, useCommands } from '../shared/commands'
import FeatureState from '../shared/FeatureState.vue'
import Page from '../shared/Page.vue'
import PageHeader from '../shared/PageHeader.vue'
import { errorText } from '../shared/errors'
import { bytes, dateTime, fileName } from '../shared/format'
import { t } from '../shared/i18n'
import { notify } from '../shared/notify'
import { useLiveReload } from '../shared/liveReload'
import { observeResize } from '../shared/observeResize'
import { notifyKept } from '../trash/actions'
import { showTrashUndo } from '../trash/undo'
import DuplicateGroupCard from './DuplicateGroup.vue'
import ReviewDialog from './ReviewDialog.vue'
import { useDuplicates, useKeepChoices } from './store'
import type { Review } from './store'

const lib = useLibrary()
const duplicates = useDuplicates()
const choices = useKeepChoices()
const busy = ref(false)
const error = ref('')
const preview = shallowRef<DuplicateItem | null>(null)
const previewPath = ref('')
const previewError = ref('')
const previewOpen = computed({
  get: () => preview.value !== null,
  set: (open) => {
    if (!open) preview.value = null
  },
})
let previewTrigger: HTMLElement | null = null
function returnPreviewFocus(event: Event) {
  if (!previewTrigger?.isConnected) return
  event.preventDefault()
  previewTrigger.focus()
}
const reviewing = shallowRef<Review | null>(null)
const acting = ref(false) // a confirmed move is under way

const exact = computed(() => duplicates.list.filter((g) => g.kind === DuplicateKind.DuplicateExact))
const similar = computed(() => duplicates.list.filter((g) => g.kind === DuplicateKind.DuplicateSimilar))
const selectedKind = ref<DuplicateKind | null>(null)
const tab = computed({
  get: () => selectedKind.value ?? (exact.value.length ? DuplicateKind.DuplicateExact : DuplicateKind.DuplicateSimilar),
  set: (value: DuplicateKind) => {
    selectedKind.value = value
    queue.value = false
    queueIDs.value = []
    position.value = 0
    completed.value = false
  },
})
const tabs = computed(() => [
  {
    value: DuplicateKind.DuplicateExact,
    label: t('duplicates.exact'),
    count: exact.value.length,
    icon: 'duplicates' as const,
    disabled: acting.value,
    reason: t('duplicates.wait'),
  },
  {
    value: DuplicateKind.DuplicateSimilar,
    label: t('duplicates.similar'),
    count: similar.value.length,
    icon: 'compare' as const,
    disabled: acting.value,
    reason: t('duplicates.wait'),
  },
])
const groups = computed(() => (tab.value === DuplicateKind.DuplicateExact ? exact.value : similar.value))
// Nothing left to review: the page says so and offers to check again itself.
const settled = computed(
  () =>
    duplicates.loaded && (completed.value || !groups.value.length) && !busy.value && !duplicates.error && !error.value,
)
const space = computed(() =>
  exact.value.reduce(
    (n, group) => n + group.items.reduce((total, item) => total + item.size * Math.max(0, item.paths.length - 1), 0),
    0,
  ),
)
// What the shown kind holds: the space exact copies take, or the photos to compare.
const summary = computed(() =>
  tab.value === DuplicateKind.DuplicateExact
    ? t('duplicates.exactSummary', { n: exact.value.length, size: bytes(space.value) })
    : t('duplicates.similarSummary', {
        n: similar.value.length,
        photos: similar.value.reduce((n, group) => n + group.items.length, 0),
      }),
)
const queue = ref(false)
const queueIDs = ref<string[]>([])
const position = ref(0)
const reviewed = ref(0)
const skipped = ref(0)
const completed = ref(false)
const keptPaths = shallowRef<string[]>([])
const groupKey = (group: DuplicateGroup) => `${group.kind}-${group.items[0]?.id}`
const queueGroup = computed(() => groups.value.find((group) => groupKey(group) === queueIDs.value[position.value]))
function beginQueue() {
  if (acting.value || busy.value) return
  if (!queueIDs.value.length || completed.value) {
    queueIDs.value = groups.value.map(groupKey)
    position.value = 0
    reviewed.value = 0
    skipped.value = 0
    completed.value = false
  }
  queue.value = queueIDs.value.length > position.value
  if (scroller.value) scroller.value.scrollTop = 0
}
function advanceQueue(skip = false) {
  if (acting.value) return
  if (skip) skipped.value++
  position.value++
  if (position.value >= queueIDs.value.length) {
    queue.value = false
    completed.value = true
  }
}

interface Row {
  key: string
  group: DuplicateGroup
}
const scroller = ref<HTMLElement | null>(null)
const intro = ref<HTMLElement | null>(null)
const list = ref<HTMLElement | null>(null)
const scrollMargin = ref(0)
let stopResize: (() => void) | undefined
function updateMargin() {
  if (!list.value || !scroller.value) return
  scrollMargin.value =
    list.value.getBoundingClientRect().top - scroller.value.getBoundingClientRect().top + scroller.value.scrollTop
}
const rows = computed<Row[]>(() => {
  const result: Row[] = []
  const shown = completed.value ? [] : queue.value ? (queueGroup.value ? [queueGroup.value] : []) : groups.value
  for (const group of shown) result.push({ key: groupKey(group), group })
  return result
})
const virtualizer = useVirtualizer(
  computed(() => ({
    count: rows.value.length,
    getScrollElement: () => scroller.value,
    getItemKey: (index: number) => rows.value[index]?.key ?? index,
    estimateSize: () => 250,
    overscan: 3,
    useAnimationFrameWithResizeObserver: true,
    scrollMargin: scrollMargin.value,
  })),
)
const visible = computed(() =>
  virtualizer.value.getVirtualItems().flatMap((item) => {
    const row = rows.value[item.index]
    return row ? [{ item, row }] : []
  }),
)
const hasMore = computed(
  () =>
    !queue.value && !completed.value && (visible.value.at(-1)?.item.index ?? rows.value.length) < rows.value.length - 1,
)
function showMore() {
  const next = visible.value.at(-1)?.item.index
  if (next !== undefined) virtualizer.value.scrollToIndex(next, { align: 'start' })
}
watch(rows, (current, previous) => {
  if (current.length !== previous.length || current.some((row, i) => row.key !== previous[i]?.key))
    virtualizer.value.measure()
})
watch(
  () => duplicates.list,
  (groups) => {
    const id = preview.value?.id
    if (id === undefined) return
    let current: DuplicateItem | undefined
    for (const group of groups) {
      current = group.items.find((item) => item.id === id)
      if (current) break
    }
    preview.value = current ?? null
    if (current && !current.paths.includes(previewPath.value)) previewPath.value = current.paths[0] ?? ''
  },
)
function measureRow(element: Element | ComponentPublicInstance | null) {
  virtualizer.value.measureElement(element instanceof Element ? element : null)
}
watch(
  [intro, busy, error, () => duplicates.error],
  async () => {
    await nextTick()
    updateMargin()
  },
  { flush: 'post' },
)
onMounted(() => {
  const elements = [intro.value, scroller.value].filter((element) => element !== null)
  stopResize = observeResize(elements, updateMargin)
})
onBeforeUnmount(() => stopResize?.())

function checkAgain() {
  error.value = ''
  keptPaths.value = []
  completed.value = false
  queue.value = false
  queueIDs.value = []
  void refresh()
}

async function refresh() {
  if (acting.value || busy.value) return
  busy.value = true
  try {
    await duplicates.load()
  } catch {
    // duplicates.error says why
  } finally {
    busy.value = false
  }
}

onMounted(() => void refresh())
useLiveReload(duplicates.load, (c) => c.assets || c.thumbs) // groups change as thumbnails are made

async function showPath(item: DuplicateItem, path: string) {
  error.value = ''
  previewError.value = ''
  try {
    await engine.showInFolder(item.id, path)
  } catch (e) {
    error.value = errorText(e)
    if (preview.value) previewError.value = error.value
  }
}

/** Opens the confirmation for a group; nothing is changed until it is confirmed. */
function startReview(group: DuplicateGroup) {
  reviewing.value = choices.reviewOf(group)
}

// A review lists paths of photos the reload may have hidden (blocked, missing)
// or changed: it must not outlive the group it describes.
watch(
  () => duplicates.list,
  () => {
    if (reviewing.value && !choices.holds(reviewing.value) && !acting.value) reviewing.value = null
  },
)

function cancelReview() {
  if (!acting.value) reviewing.value = null
}

async function confirmReview() {
  const review = reviewing.value
  if (!review || acting.value) return
  if (!choices.holds(review)) {
    reviewing.value = null
    return
  }
  acting.value = true
  error.value = ''
  keptPaths.value = []
  let succeeded = false
  try {
    if (review.kind === DuplicateKind.DuplicateExact) {
      const done = await engine.deleteCopies(review.keep.id, review.keepPath, review.recycle)
      if (done.kept.length) notifyKept(done.kept.length, done.kept)
      if (done.recycled)
        notify({
          id: 'duplicates.recycled',
          kind: 'success',
          title: t('trash.recycled', { n: done.recycled }),
          description: t('duplicates.recycleRecovery'),
        })
      keptPaths.value = done.kept
      succeeded = done.kept.length === 0
    } else {
      const ids = review.trash.map((item) => item.id)
      await engine.trashAssets(ids)
      showTrashUndo(ids, lib.restoreAssets)
      succeeded = true
    }
  } catch (e) {
    error.value = errorText(e)
  }
  acting.value = false
  reviewing.value = null
  if (succeeded) {
    reviewed.value++
    if (queue.value) advanceQueue()
  }
  await refresh() // the groups changed, or the move failed part way
  if (succeeded && !groups.value.length && !duplicates.error) completed.value = true
}

function openPreview(item: DuplicateItem, path = item.paths[0] ?? '') {
  previewTrigger = document.activeElement instanceof HTMLElement ? document.activeElement : null
  preview.value = item
  previewPath.value = path
  previewError.value = ''
}

useCommands([
  {
    id: 'duplicates.refresh',
    group: 'view',
    title: () => t('duplicates.refresh'),
    icon: 'refresh',
    enabled: () => !busy.value && !acting.value,
    run: checkAgain,
  },
])
</script>

<template>
  <Page :scroll="false">
    <template #header>
      <PageHeader :title="t('nav.duplicates')" :subtitle="groups.length ? summary : undefined">
        <UButton
          v-if="!settled"
          :icon="icons.refresh"
          :loading="busy"
          :disabled="acting"
          color="neutral"
          variant="ghost"
          @click="runCommand('duplicates.refresh')"
          >{{ t('duplicates.refresh') }}</UButton
        >
        <UButton
          v-if="groups.length && !queue"
          :disabled="acting || busy"
          :trailing-icon="icons['arrow-right']"
          color="primary"
          variant="solid"
          data-review-all
          @click="beginQueue"
          >{{ t(queueIDs.length && !completed ? 'duplicates.resume' : 'duplicates.reviewAll') }}</UButton
        >
      </PageHeader>
    </template>
    <template #toolbar>
      <ViewBar :label="t('nav.duplicates')">
        <Segmented v-model="tab" :items="tabs" :label="t('duplicates.type')" look="tabs" />
      </ViewBar>
    </template>
    <div ref="scroller" class="duplicates-scroll" data-duplicates-scroll>
      <div ref="intro" class="guidance">
        <template v-if="groups.length && !completed">
          <div class="section-head">
            <div>
              <h2>{{ summary }}</h2>
              <p>
                {{ t(tab === DuplicateKind.DuplicateExact ? 'duplicates.exactHint' : 'duplicates.similarHint') }}
              </p>
            </div>
            <UBadge :icon="icons.lock" color="neutral" variant="soft">{{ t('duplicates.onDevice') }}</UBadge>
          </div>
          <UAlert
            :icon="tab === DuplicateKind.DuplicateExact ? icons.info : icons.smart"
            :description="t(tab === DuplicateKind.DuplicateExact ? 'duplicates.exactGuidance' : 'duplicates.bestHint')"
            :color="tab === DuplicateKind.DuplicateExact ? 'neutral' : 'primary'"
            :variant="tab === DuplicateKind.DuplicateExact ? 'outline' : 'soft'"
            data-duplicates-guidance
          />
        </template>
        <UAlert
          v-if="keptPaths.length"
          color="warning"
          :title="t('trash.kept', { n: keptPaths.length })"
          :description="t('duplicates.recoverKept')"
          role="alert"
        >
          <template #description
            ><p v-for="path in keptPaths" :key="path" class="preview-path">{{ path }}</p>
            <p>{{ t('duplicates.recoverKept') }}</p></template
          >
        </UAlert>
        <UAlert
          v-if="error || duplicates.error"
          color="error"
          variant="soft"
          :icon="icons.error"
          :title="error || duplicates.error"
          role="alert"
        />
        <div v-if="busy" class="checking" role="status">
          <span class="lumen-light-dot" />
          <div>
            <strong>{{ t('duplicates.loading') }}</strong>
            <p>{{ t('duplicates.checkingHint') }}</p>
          </div>
        </div>
        <template v-if="busy && !duplicates.loaded"
          ><div v-for="n in 2" :key="n" class="loading-group" aria-hidden="true">
            <div class="lumen-skeleton h-5 w-1/3" />
            <div class="loading-copies">
              <div v-for="copy in 3" :key="copy" class="lumen-skeleton aspect-video" />
            </div></div
        ></template>
        <p v-if="acting" role="status" class="text-sm text-toned">
          {{ t(reviewing?.kind === DuplicateKind.DuplicateExact ? 'duplicates.recycling' : 'duplicates.trashing') }}
        </p>
        <div v-if="queue" class="queue-status">
          <strong>{{ t('duplicates.queuePosition', { n: position + 1, total: queueIDs.length }) }}</strong>
          <span>{{ t('duplicates.queueStats', { reviewed, skipped }) }}</span>
          <UButton variant="ghost" color="neutral" :disabled="acting" data-queue-later @click="queue = false">{{
            t('duplicates.later')
          }}</UButton>
          <progress :value="position" :max="queueIDs.length" :aria-label="t('duplicates.progress')" />
          <UButton
            v-if="!queueGroup"
            variant="outline"
            color="neutral"
            :disabled="acting"
            @click="advanceQueue(true)"
            >{{ t('duplicates.skipUnavailable') }}</UButton
          >
        </div>
      </div>
      <div ref="list" class="virtual-list" :style="{ height: `${virtualizer.getTotalSize()}px` }" role="list">
        <div
          v-for="{ item: virtual, row } in visible"
          :key="row.key"
          :ref="measureRow"
          :data-index="virtual.index"
          class="virtual-row"
          :style="{ transform: `translateY(${virtual.start - scrollMargin}px)` }"
          role="listitem"
        >
          <DuplicateGroupCard
            :group="row.group"
            :busy="acting"
            @review="startReview"
            @preview="openPreview"
            @show-path="showPath"
          />
          <div v-if="queue" class="queue-actions">
            <UButton color="neutral" variant="ghost" :disabled="acting" data-queue-skip @click="advanceQueue(true)">{{
              t('duplicates.skip')
            }}</UButton>
          </div>
        </div>
      </div>
      <div v-if="hasMore" class="more-groups">
        <UButton
          color="neutral"
          variant="outline"
          :trailing-icon="icons['chevron-down']"
          data-duplicates-more
          @click="showMore"
          >{{ t('duplicates.showMore') }}</UButton
        >
      </div>
      <FeatureState
        v-if="settled"
        kind="empty"
        icon="duplicates"
        :title="t(completed ? 'duplicates.done' : 'duplicates.empty')"
        :text="completed ? t('duplicates.queueStats', { reviewed, skipped }) : t('duplicates.emptyHint')"
        ><UButton v-if="completed" to="/" color="primary">{{ t('duplicates.library') }}</UButton
        ><UButton variant="outline" color="neutral" @click="checkAgain">{{
          t('duplicates.refresh')
        }}</UButton></FeatureState
      >
      <UButton v-if="duplicates.error || error" variant="outline" color="neutral" @click="checkAgain">{{
        t('duplicates.refresh')
      }}</UButton>
    </div>
    <UModal
      v-model:open="previewOpen"
      :title="preview ? fileName(previewPath) : t('duplicates.previewTitle')"
      :description="t('duplicates.previewHint')"
      :close="false"
      :content="{ onCloseAutoFocus: returnPreviewFocus }"
      :ui="{ content: 'max-w-5xl' }"
    >
      <template #close>
        <IconButton
          :icon="icons.close"
          :label="t('common.close')"
          color="neutral"
          variant="ghost"
          @click="preview = null"
        />
      </template>
      <template #body>
        <div v-if="preview" data-duplicate-preview>
          <UAlert
            v-if="previewError"
            color="error"
            :title="previewError"
            :description="t('duplicates.folderRecovery')"
            role="alert"
          />
          <img
            class="preview-image"
            :src="thumbUrl(preview.id, preview.thumb, THUMB_LARGE)"
            :alt="fileName(previewPath)"
          />
          <p class="copy-spec">
            {{ t('duplicates.photoSize', { w: preview.width, h: preview.height, size: bytes(preview.size) })
            }}<template v-if="preview.takenAt"> · {{ dateTime(preview.takenAt) }}</template>
          </p>
          <p v-for="path in preview.paths" :key="path" class="preview-path lumen-selectable">{{ path }}</p>
          <UButton
            v-for="path in preview.paths"
            :key="path"
            :icon="icons['folder-open']"
            color="neutral"
            variant="ghost"
            @click="showPath(preview, path)"
            >{{ t('common.showInFolder') }}</UButton
          >
        </div>
      </template>
    </UModal>
    <ReviewDialog :review="reviewing" :busy="acting" @confirm="confirmReview" @cancel="cancelReview" />
  </Page>
</template>

<style scoped>
.duplicates-scroll {
  height: 100%;
  overflow-y: auto;
  padding: 0.25rem var(--lumen-page-x) 1.5rem;
}
.guidance {
  display: flex;
  flex-direction: column;
  gap: 1.25rem;
  padding-block: 1rem 0.5rem;
}
.section-head {
  display: flex;
  align-items: flex-start;
  flex-wrap: wrap;
  gap: 1rem;
}
.section-head > div {
  flex: 1;
  min-width: 0;
}
h2 {
  margin: 0;
  color: var(--ui-text-highlighted);
  font: 600 1.125rem/1.3 var(--font-display);
  font-variant-numeric: tabular-nums;
}
.section-head p {
  max-width: 70ch;
  margin: 0.5rem 0 0;
  color: var(--ui-text-toned);
  font-size: 0.8125rem;
}
.virtual-list {
  position: relative;
}
.virtual-row {
  position: absolute;
  top: 0;
  left: 0;
  width: 100%;
  display: flow-root;
}
.preview-image {
  display: block;
  max-width: 100%;
  max-height: 65vh;
  object-fit: contain;
  margin: auto;
}
.preview-path {
  overflow-wrap: anywhere;
  margin: 0.75rem 0 0;
  color: var(--ui-text-muted);
}
.queue-status {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 0.75rem;
  padding: 1rem;
  background: var(--lumen-bg-sunken);
  border-radius: var(--lumen-r-lg);
  font-size: 0.8125rem;
}
.queue-status progress {
  flex-basis: 100%;
  width: 100%;
  height: 0.25rem;
  accent-color: var(--ui-primary);
}
.queue-actions {
  display: flex;
  justify-content: flex-end;
}
.checking {
  display: flex;
  align-items: center;
  gap: 1rem;
  padding: 1rem;
  border: 1px solid var(--lumen-light);
  border-radius: var(--lumen-r-lg);
  font-size: 0.8125rem;
}
.checking p {
  margin: 0.25rem 0 0;
  color: var(--ui-text-dimmed);
}
.loading-group {
  display: grid;
  gap: 1rem;
  margin-top: 1rem;
  padding: 1.25rem;
  border: 1px solid var(--ui-border);
  border-radius: var(--lumen-r-lg);
}
.loading-copies {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 1rem;
}
.more-groups {
  position: sticky;
  bottom: 1rem;
  display: flex;
  justify-content: center;
  pointer-events: none;
}
.more-groups > * {
  pointer-events: auto;
  box-shadow: var(--lumen-shadow-1);
}
</style>
