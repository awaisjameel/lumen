<script setup lang="ts">
// Duplicates: exact file copies and similar-looking photos. Each group
// pre-selects the copy to keep (the engine's suggestion, or the user's own
// pick); its Review button lists what would happen and does nothing until the
// user confirms. Similar photos go to Lumen's Trash, and the extra files of
// exact copies to the Recycle Bin.
import { computed, nextTick, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
import type { ComponentPublicInstance } from 'vue'
import { useVirtualizer } from '@tanstack/vue-virtual'
import { DuplicateKind, engine, THUMB_LARGE, thumbUrl } from '../bridge'
import type { DuplicateGroup, DuplicateItem } from '../bridge'
import SearchBox from '../search/SearchBox.vue'
import EmptyState from '../shared/EmptyState.vue'
import { errorText, notice } from '../shared/errors'
import { bytes } from '../shared/format'
import Icon from '../shared/Icon.vue'
import { t } from '../shared/i18n'
import { useLiveReload } from '../shared/liveReload'
import { showTrashUndo } from '../trash/undo'
import ReviewDialog from './ReviewDialog.vue'
import { useDuplicates, useKeepChoices } from './store'
import type { Review } from './store'

const duplicates = useDuplicates()
const choices = useKeepChoices()
const busy = ref(false)
const error = ref('')
const preview = shallowRef<DuplicateItem | null>(null)
const previewDialog = ref<HTMLDialogElement | null>(null)
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
let resizeObserver: ResizeObserver | undefined
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
    if (!current) previewDialog.value?.close()
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
  resizeObserver = new ResizeObserver(updateMargin)
  if (intro.value) resizeObserver.observe(intro.value)
  if (scroller.value) resizeObserver.observe(scroller.value)
  updateMargin()
})
onBeforeUnmount(() => resizeObserver?.disconnect())

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

function filename(path: string): string {
  return path.split(/[\\/]/).pop() || path
}

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
      if (done.kept.length) notice.value = t('trash.kept', { n: done.kept.length })
    } else {
      const ids = review.trash.map((item) => item.id)
      await engine.trashAssets(ids)
      showTrashUndo(ids)
    }
  } catch (e) {
    error.value = errorText(e)
  }
  acting.value = false
  reviewing.value = null
  await refresh() // the groups changed, or the move failed part way
}

async function openPreview(item: DuplicateItem) {
  preview.value = item
  await nextTick()
  previewDialog.value?.showModal()
}
</script>

<template>
  <div class="page">
    <header class="topbar">
      <h1>{{ t('nav.duplicates') }}</h1>
      <SearchBox />
      <div class="spacer" />
      <button class="icon-btn" :title="t('duplicates.refresh')" :disabled="busy" @click="refresh">
        <Icon name="refresh" />
      </button>
    </header>
    <div ref="scroller" class="page-body">
      <div ref="intro">
        <p class="muted">{{ t('duplicates.intro') }} {{ t('duplicates.bestHint') }}</p>
        <p v-if="error || duplicates.error" class="page-error" role="alert">{{ error || duplicates.error }}</p>
        <p v-if="busy" class="muted" role="status">{{ t('duplicates.loading') }}</p>
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
            <p class="muted">
              {{ t(row.kind === DuplicateKind.DuplicateExact ? 'duplicates.exactHint' : 'duplicates.similarHint') }}
            </p>
          </template>
          <div v-else class="group">
            <template v-if="row.kind === DuplicateKind.DuplicateExact">
              <div v-for="item in row.group.items" :key="item.id" class="item">
                <button
                  class="preview-button"
                  :aria-label="t('duplicates.preview', { name: filename(item.paths[0] ?? '') })"
                  @click="openPreview(item)"
                >
                  <img v-if="item.thumb" :src="thumbUrl(item.id, item.thumb)" alt="" loading="lazy" decoding="async" />
                  <span v-else class="ph" />
                </button>
                <div class="item-info">
                  <span class="muted">{{
                    t('duplicates.copies', {
                      n: item.paths.length,
                      size: bytes(item.size),
                    })
                  }}</span>
                  <ul>
                    <li v-for="path in item.paths" :key="path" :title="path">
                      <span>{{ path }}</span>
                      <label class="keep">
                        <input
                          type="radio"
                          :name="`keep-${item.id}`"
                          :aria-label="t('duplicates.keepNamed', { name: path })"
                          :checked="choices.keeperOf(row.group)?.path === path"
                          @change="choices.choose(row.group, item, path)"
                        />
                        {{ t('duplicates.keep') }}
                        <em v-if="item.keep === path">{{ t('duplicates.suggested') }}</em>
                      </label>
                      <button
                        class="btn btn-subtle"
                        :aria-label="t('duplicates.showFile', { name: path })"
                        @click="showPath(item, path)"
                      >
                        {{ t('common.showInFolder') }}
                      </button>
                    </li>
                  </ul>
                </div>
              </div>
            </template>
            <div v-else class="similar">
              <div v-for="item in row.group.items" :key="item.id" class="similar-item">
                <button
                  class="preview-button"
                  :aria-label="t('duplicates.preview', { name: filename(item.paths[0] ?? '') })"
                  @click="openPreview(item)"
                >
                  <img v-if="item.thumb" :src="thumbUrl(item.id, item.thumb)" alt="" loading="lazy" decoding="async" />
                  <span v-else class="ph" />
                </button>
                <strong :title="item.paths[0]">{{ filename(item.paths[0] ?? '') }}</strong>
                <span class="muted">{{
                  t('duplicates.photoSize', { w: item.width, h: item.height, size: bytes(item.size) })
                }}</span>
                <label class="keep">
                  <input
                    type="radio"
                    :name="`keep-${row.key}`"
                    :aria-label="t('duplicates.keepNamed', { name: filename(item.paths[0] ?? '') })"
                    :checked="choices.keeperOf(row.group)?.item.id === item.id"
                    @change="choices.choose(row.group, item)"
                  />
                  {{ t('duplicates.keep') }}
                  <em v-if="item.best">{{ t('duplicates.suggested') }}</em>
                </label>
                <button
                  v-if="item.paths[0]"
                  class="btn btn-subtle"
                  :aria-label="t('duplicates.showFile', { name: item.paths[0] })"
                  @click="showPath(item, item.paths[0])"
                >
                  {{ t('common.showInFolder') }}
                </button>
              </div>
            </div>
            <div class="group-actions">
              <button
                class="btn"
                :aria-label="t('duplicates.reviewNamed', { name: filename(row.group.items[0]?.paths[0] ?? '') })"
                :disabled="acting || !choices.reviewOf(row.group)"
                @click="startReview(row.group)"
              >
                {{ t('duplicates.review') }}
              </button>
            </div>
          </div>
        </div>
      </div>

      <EmptyState
        v-if="duplicates.loaded && !duplicates.list.length"
        icon="copy"
        :title="t('duplicates.empty')"
        :text="t('duplicates.emptyHint')"
      />
    </div>
    <dialog
      ref="previewDialog"
      class="dialog preview-dialog"
      :aria-label="preview ? filename(preview.paths[0] ?? '') : undefined"
      @close="preview = null"
    >
      <template v-if="preview">
        <button class="icon-btn close" :aria-label="t('common.close')" @click="previewDialog?.close()">
          <Icon name="close" />
        </button>
        <img :src="thumbUrl(preview.id, preview.thumb, THUMB_LARGE)" :alt="filename(preview.paths[0] ?? '')" />
        <p v-for="path in preview.paths" :key="path">{{ path }}</p>
      </template>
    </dialog>
    <ReviewDialog :review="reviewing" :busy="acting" @confirm="confirmReview" @cancel="cancelReview" />
  </div>
</template>

<style scoped>
h2 {
  margin: 28px 0 4px;
  font-size: 18px;
}

.group {
  border: 1px solid var(--border);
  border-radius: var(--radius-m);
  padding: 12px;
  margin: 12px 0;
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

.item {
  display: flex;
  gap: 16px;
  align-items: flex-start;
}

.item img {
  width: 120px;
  height: 120px;
  object-fit: cover;
  border-radius: var(--radius-m);
}

.preview-button {
  padding: 0;
  border: 0;
  background: transparent;
  cursor: zoom-in;
}

/* No thumbnail made yet. */
.preview-button .ph {
  display: block;
  width: 120px;
  height: 120px;
  border-radius: var(--radius-m);
  background: var(--placeholder);
}

.item-info {
  min-width: 0;
}

ul {
  padding-left: 20px;
  overflow-wrap: anywhere;
}

li + li {
  margin-top: 4px;
}

li .btn {
  display: block;
  margin: 4px 0 10px;
}

.similar {
  display: flex;
  gap: 14px;
  overflow-x: auto;
}

.keep {
  display: flex;
  align-items: center;
  gap: 6px;
  cursor: pointer;
}

.keep em {
  font-style: normal;
  font-size: 12px;
  color: var(--accent);
}

.group-actions {
  margin-top: 12px;
}

.similar-item {
  display: flex;
  flex: 0 0 160px;
  flex-direction: column;
  gap: 5px;
  min-width: 0;
}

.similar-item img {
  width: 160px;
  height: 160px;
  object-fit: cover;
  border-radius: var(--radius-m);
}

.similar-item strong {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.similar-item .btn {
  align-self: flex-start;
  margin-top: 4px;
}

.preview-dialog {
  max-width: min(90vw, 1100px);
  max-height: 90vh;
  padding: 20px;
  background: var(--surface-2);
}

.preview-dialog > img {
  display: block;
  max-width: 100%;
  max-height: 72vh;
  object-fit: contain;
  margin: auto;
}

.preview-dialog p {
  max-width: 70ch;
  overflow-wrap: anywhere;
}

.close {
  float: right;
}
</style>
