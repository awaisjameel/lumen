<script setup lang="ts">
// Duplicates: exact file copies and similar-looking photos. Each group
// pre-selects the copy to keep (the engine's suggestion, or the user's own
// pick); its Review button lists what would happen and does nothing until the
// user confirms. Similar photos go to Lumen's Trash, and the extra files of
// exact copies to the Recycle Bin.
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UModal from '@nuxt/ui/components/Modal.vue'
import { computed, nextTick, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
import type { ComponentPublicInstance } from 'vue'
import { useVirtualizer } from '@tanstack/vue-virtual'
import { DuplicateKind, engine, THUMB_LARGE, thumbUrl } from '../bridge'
import type { DuplicateGroup, DuplicateItem } from '../bridge'
import SearchBox from '../search/SearchBox.vue'
import { icons } from '../design/icons'
import { useLibrary } from '../library/store'
import { runCommand, useCommands } from '../shared/commands'
import FeatureState from '../shared/FeatureState.vue'
import Page from '../shared/Page.vue'
import PageHeader from '../shared/PageHeader.vue'
import { errorText } from '../shared/errors'
import { fileName } from '../shared/format'
import { t } from '../shared/i18n'
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

interface Row {
  key: string
  kind: DuplicateGroup['kind']
  group: DuplicateGroup | null
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
  for (const [kind, groups] of [
    [DuplicateKind.DuplicateExact, exact.value],
    [DuplicateKind.DuplicateSimilar, similar.value],
  ] as const) {
    if (!groups.length) continue
    result.push({ key: `${kind}-heading`, kind, group: null })
    for (const group of groups) result.push({ key: `${kind}-${group.items[0]?.id}`, kind, group })
  }
  return result
})
const virtualizer = useVirtualizer(
  computed(() => ({
    count: rows.value.length,
    getScrollElement: () => scroller.value,
    getItemKey: (index: number) => rows.value[index]?.key ?? index,
    estimateSize: (index: number) => (rows.value[index]?.group ? 250 : 88),
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

async function refresh() {
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
  try {
    await engine.showInFolder(item.id, path)
  } catch (e) {
    error.value = errorText(e)
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
  acting.value = true
  error.value = ''
  try {
    if (review.kind === DuplicateKind.DuplicateExact) {
      const done = await engine.deleteCopies(review.keep.id, review.keepPath, review.recycle)
      if (done.kept.length) notifyKept(done.kept.length)
    } else {
      const ids = review.trash.map((item) => item.id)
      await engine.trashAssets(ids)
      showTrashUndo(ids, lib.restoreAssets)
    }
  } catch (e) {
    error.value = errorText(e)
  }
  acting.value = false
  reviewing.value = null
  await refresh() // the groups changed, or the move failed part way
}

function openPreview(item: DuplicateItem) {
  previewTrigger = document.activeElement instanceof HTMLElement ? document.activeElement : null
  preview.value = item
}

useCommands([
  {
    id: 'duplicates.refresh',
    group: 'view',
    title: () => t('duplicates.refresh'),
    icon: 'refresh',
    enabled: () => !busy.value,
    run: refresh,
  },
])
</script>

<template>
  <Page :scroll="false">
    <template #header>
      <PageHeader :title="t('nav.duplicates')">
        <SearchBox />
        <UButton
          :icon="icons.refresh"
          :aria-label="t('duplicates.refresh')"
          :loading="busy"
          color="neutral"
          variant="ghost"
          square
          @click="runCommand('duplicates.refresh')"
        />
      </PageHeader>
    </template>
    <div ref="scroller" class="duplicates-scroll" data-duplicates-scroll>
      <div ref="intro" class="guidance">
        <p class="text-muted">{{ t('duplicates.intro') }} {{ t('duplicates.bestHint') }}</p>
        <UAlert
          v-if="error || duplicates.error"
          color="error"
          variant="soft"
          :icon="icons.error"
          :title="error || duplicates.error"
          role="alert"
        />
        <FeatureState v-if="busy" kind="loading" :title="t('duplicates.loading')" />
      </div>
      <div ref="list" class="virtual-list" :style="{ height: `${virtualizer.getTotalSize()}px` }" role="list">
        <div
          v-for="{ item: virtual, row } in visible"
          :key="row.key"
          :ref="measureRow"
          :data-index="virtual.index"
          class="virtual-row"
          :style="{ transform: `translateY(${virtual.start - scrollMargin}px)` }"
          :role="row.group ? 'listitem' : undefined"
        >
          <template v-if="!row.group">
            <h2>{{ t(row.kind === DuplicateKind.DuplicateExact ? 'duplicates.exact' : 'duplicates.similar') }}</h2>
            <p class="text-muted">
              {{ t(row.kind === DuplicateKind.DuplicateExact ? 'duplicates.exactHint' : 'duplicates.similarHint') }}
            </p>
          </template>
          <DuplicateGroupCard
            v-else
            :group="row.group"
            :busy="acting"
            @review="startReview"
            @preview="openPreview"
            @show-path="showPath"
          />
        </div>
      </div>
      <FeatureState
        v-if="duplicates.loaded && !duplicates.list.length && !busy && !duplicates.error"
        kind="empty"
        icon="duplicates"
        :title="t('duplicates.empty')"
        :text="t('duplicates.emptyHint')"
      />
    </div>
    <UModal
      v-model:open="previewOpen"
      :title="preview ? fileName(preview.paths[0] ?? '') : t('duplicates.previewTitle')"
      :description="t('duplicates.previewHint')"
      :close="false"
      :content="{ onCloseAutoFocus: returnPreviewFocus }"
      :ui="{ content: 'max-w-5xl' }"
    >
      <template #close>
        <UButton
          :icon="icons.close"
          :aria-label="t('common.close')"
          color="neutral"
          variant="ghost"
          square
          @click="preview = null"
        />
      </template>
      <template #body>
        <div v-if="preview" data-duplicate-preview>
          <img
            class="preview-image"
            :src="thumbUrl(preview.id, preview.thumb, THUMB_LARGE)"
            :alt="fileName(preview.paths[0] ?? '')"
          />
          <p v-for="path in preview.paths" :key="path" class="preview-path lumen-selectable">{{ path }}</p>
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
.guidance p {
  margin: 0.5rem 0 1rem;
}
h2 {
  margin: 1.5rem 0 0.25rem;
  color: var(--ui-text-highlighted);
  font-size: 1.125rem;
  font-weight: 600;
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
</style>
