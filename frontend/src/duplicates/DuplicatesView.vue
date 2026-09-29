<script setup lang="ts">
// Duplicates: exact file copies and similar-looking photos, for review only.
// Lumen never deletes or moves files; each copy can be shown in Explorer.
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { DuplicateKind, engine, THUMB_LARGE, thumbUrl } from '../bridge'
import type { DuplicateItem } from '../bridge'
import SearchBox from '../search/SearchBox.vue'
import EmptyState from '../shared/EmptyState.vue'
import { errorText } from '../shared/errors'
import { bytes } from '../shared/format'
import Icon from '../shared/Icon.vue'
import { t } from '../shared/i18n'
import { useLiveReload } from '../shared/liveReload'
import { useDuplicates } from './store'

const duplicates = useDuplicates()
const busy = ref(false)
const error = ref('')
const preview = ref<DuplicateItem | null>(null)
const previewDialog = ref<HTMLDialogElement | null>(null)

const exact = computed(() => duplicates.groups.filter((g) => g.kind === DuplicateKind.DuplicateExact))
const similar = computed(() => duplicates.groups.filter((g) => g.kind === DuplicateKind.DuplicateSimilar))

// Groups are rendered a page at a time as the list is scrolled (a large
// library has thousands, and all of them at once took seconds to build and
// to leave). Exact copies come first, then similar photos.
const PAGE = 40
const shown = ref(PAGE)
const exactShown = computed(() => exact.value.slice(0, shown.value))
const similarShown = computed(() => similar.value.slice(0, Math.max(0, shown.value - exact.value.length)))
const more = computed(() => shown.value < duplicates.groups.length)
const sentinel = ref<HTMLElement | null>(null)
const observer =
  typeof IntersectionObserver === 'undefined'
    ? null
    : new IntersectionObserver(
        (entries) => {
          if (!entries.some((e) => e.isIntersecting)) return
          shown.value += PAGE
          // Still in view after the page was added (a tall window): observing
          // afresh reports it again.
          void nextTick(() => {
            if (!sentinel.value) return
            observer?.unobserve(sentinel.value)
            observer?.observe(sentinel.value)
          })
        },
        { rootMargin: '800px' },
      )
if (!observer) shown.value = Infinity
watch(sentinel, (el, old) => {
  if (old) observer?.unobserve(old)
  if (el) observer?.observe(el)
})
onUnmounted(() => observer?.disconnect())

async function refresh() {
  busy.value = true
  error.value = ''
  try {
    await duplicates.load()
  } catch (e) {
    error.value = errorText(e)
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
    <div class="page-body">
      <p class="muted">{{ t('duplicates.intro') }}</p>
      <p v-if="error" class="page-error" role="alert">{{ error }}</p>
      <p v-if="busy" class="muted" role="status">{{ t('duplicates.loading') }}</p>

      <section v-if="exactShown.length" aria-labelledby="exact-title">
        <h2 id="exact-title">{{ t('duplicates.exact') }}</h2>
        <p class="muted">{{ t('duplicates.exactHint') }}</p>
        <div v-for="group in exactShown" :key="`exact-${group.items[0]?.id}`" class="group">
          <div v-for="item in group.items" :key="item.id" class="item">
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
        </div>
      </section>

      <section v-if="similarShown.length" aria-labelledby="similar-title">
        <h2 id="similar-title">{{ t('duplicates.similar') }}</h2>
        <p class="muted">{{ t('duplicates.similarHint') }}</p>
        <div v-for="group in similarShown" :key="`similar-${group.items[0]?.id}`" class="group similar">
          <div v-for="item in group.items" :key="item.id" class="similar-item">
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
      </section>
      <div v-if="more" ref="sentinel" class="more" aria-hidden="true" />

      <EmptyState
        v-if="duplicates.loaded && !duplicates.groups.length"
        icon="copy"
        :title="t('duplicates.empty')"
        :text="t('duplicates.emptyHint')"
      />
    </div>
    <dialog ref="previewDialog" class="preview-dialog" @close="preview = null">
      <template v-if="preview">
        <button class="icon-btn close" :aria-label="t('common.close')" @click="previewDialog?.close()">
          <Icon name="close" />
        </button>
        <img :src="thumbUrl(preview.id, preview.thumb, THUMB_LARGE)" :alt="filename(preview.paths[0] ?? '')" />
        <p v-for="path in preview.paths" :key="path">{{ path }}</p>
      </template>
    </dialog>
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

.more {
  height: 1px;
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
  border: 1px solid var(--border);
  border-radius: var(--radius-m);
  background: var(--surface-2);
  color: var(--text);
}

.preview-dialog::backdrop {
  background: var(--backdrop);
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
