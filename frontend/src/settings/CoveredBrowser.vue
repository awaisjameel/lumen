<script setup lang="ts">
// The Blocked screen's browser: the photos one block covers, in a virtualised
// grid. A photo opens in the Viewer, which can move it to the Trash. Photos
// that other blocks hide too say which, so unblocking this one is no surprise.
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import { useVirtualizer } from '@tanstack/vue-virtual'
import { BlockMode } from '../bridge'
import type { CoveredItem } from '../bridge'
import { listFormat, t } from '../shared/i18n'
import { icons } from '../design/icons'
import { runCommand, useCommands } from '../shared/commands'
import { shortcutsSuspended } from '../shared/overlay'
import { observeResize } from '../shared/observeResize'
import FeatureState from '../shared/FeatureState.vue'
import MediaTile from '../shared/MediaTile.vue'
import { buildLayout, GAP, rowHeight } from '../library/layout'
import VideoThumb from '../library/VideoThumb.vue'
import Viewer from '../library/Viewer.vue'
import type { CoveredPhotos } from './covered'

const props = defineProps<{ covered: CoveredPhotos }>()

const TARGET_CELL = 160
const CAPTION_H = 36 // two lines of "Also hidden by", under every cell so rows keep one height

const scroller = ref<HTMLElement | null>(null)
const width = ref(0)
let stopResize: (() => void) | undefined
onMounted(() => {
  stopResize = observeResize(scroller.value ? [scroller.value] : [], () => {
    if (scroller.value) width.value = scroller.value.clientWidth
  })
})
function onKey(event: KeyboardEvent) {
  if (event.key !== 'Escape' || event.defaultPrevented || shortcutsSuspended()) return
  if (props.covered.selected.size) props.covered.clearSelection()
  else props.covered.close()
  event.preventDefault()
}
onMounted(() => window.addEventListener('keydown', onKey))
onBeforeUnmount(() => {
  stopResize?.()
  window.removeEventListener('keydown', onKey)
})

const labelOnly = computed(() => props.covered.block?.mode === BlockMode.BlockLabel)
const layout = computed(() =>
  buildLayout(
    props.covered.items.length ? [{ day: 'covered', count: props.covered.items.length }] : [],
    Math.max(width.value, 200),
    TARGET_CELL,
    { headers: false, caption: CAPTION_H },
  ),
)
const virtualizer = useVirtualizer(
  computed(() => ({
    count: layout.value.rows.length,
    getScrollElement: () => scroller.value,
    estimateSize: (i: number) => {
      const row = layout.value.rows[i]
      return row ? rowHeight(row, layout.value) : layout.value.cell + CAPTION_H
    },
    overscan: 4,
    useAnimationFrameWithResizeObserver: true,
  })),
)
watch(layout, () => virtualizer.value.measure())

interface VisibleRow {
  key: number
  top: number
  height: number
  items: CoveredItem[]
}
const visible = computed<VisibleRow[]>(() =>
  virtualizer.value.getVirtualItems().flatMap((v) => {
    const row = layout.value.rows[v.index]
    if (row?.kind !== 'cells') return [] // the layout shrank; the virtualizer catches up next tick
    return [
      { key: row.start, top: v.start, height: v.size, items: props.covered.items.slice(row.start, row.start + row.n) },
    ]
  }),
)

const names = (item: CoveredItem) => listFormat.format(item.hiddenBy.map((b) => b.name))

useCommands([
  {
    id: 'blocked.covered.back',
    group: 'view',
    title: () => t('covered.back'),
    icon: 'arrow-left',
    run: () => props.covered.close(),
  },
  {
    id: 'blocked.covered.trash',
    group: 'media',
    title: () => t('trash.move'),
    icon: 'trash',
    available: () => props.covered.selected.size > 0,
    enabled: () => !props.covered.trashing,
    run: () => props.covered.trashSelection(),
  },
  {
    id: 'blocked.covered.clear',
    group: 'view',
    title: () => t('blocked.clearSelection'),
    available: () => props.covered.selected.size > 0,
    run: () => props.covered.clearSelection(),
  },
])
</script>

<template>
  <section class="covered" :aria-label="covered.block ? t('covered.title', { name: covered.block.name }) : undefined">
    <header class="head">
      <UButton :icon="icons['arrow-left']" data-covered="back" @click="runCommand('blocked.covered.back')">{{
        t('covered.back')
      }}</UButton>
      <h2 v-if="covered.block">{{ t('covered.title', { name: covered.block.name }) }}</h2>
      <small v-if="!covered.loading" class="text-muted">{{ t('covered.count', { n: covered.items.length }) }}</small>
    </header>
    <UAlert v-if="labelOnly" color="neutral" variant="subtle" :title="t('covered.stillShown')" />
    <UAlert color="warning" variant="subtle" :icon="icons.warning" :title="t('covered.trashText')" class="mt-4" />
    <UAlert v-if="covered.error" data-covered-error color="error" :title="covered.error" role="alert" class="mt-4">
      <template #actions
        ><UButton :disabled="covered.trashing" @click="covered.retry">{{ t('blocked.retry') }}</UButton></template
      >
    </UAlert>
    <FeatureState v-if="covered.loading" kind="loading" :title="t('covered.loading')" />
    <FeatureState
      v-else-if="!covered.items.length && !covered.error"
      kind="empty"
      icon="photos"
      :title="t('covered.empty')"
      :text="t('covered.emptyHint')"
    />
    <div ref="scroller" class="scroller" role="group" :aria-label="t('covered.grid')" @scroll.passive="covered.touch">
      <div class="rows" :style="{ height: virtualizer.getTotalSize() + 'px' }">
        <div
          v-for="row in visible"
          :key="row.key"
          class="row"
          :style="{ transform: `translateY(${row.top}px)`, height: row.height + 'px', gap: GAP + 'px' }"
        >
          <div v-for="item in row.items" :key="item.id" class="entry" :style="{ width: layout.cell + 'px' }">
            <div class="tile-wrap" :style="{ width: layout.cell + 'px', height: layout.cell + 'px' }">
              <button
                class="cell"
                data-covered-preview
                :aria-label="t(item.v ? 'covered.openVideo' : 'covered.openPhoto')"
                @click="covered.view(item.id)"
              >
                <MediaTile
                  :id="item.id"
                  :thumb="item.t"
                  :video="item.v"
                  :duration-ms="item.d"
                  :selected="covered.selected.has(item.id)"
                  checkable
                  selecting
                  @check="!covered.trashing && covered.select(item.id, !covered.selected.has(item.id))"
                >
                  <template v-if="item.v && item.t" #media><VideoThumb :item="item" :cell="layout.cell" /></template>
                </MediaTile>
              </button>
              <button
                type="button"
                class="sr-only accessible-check"
                data-covered-select
                :aria-label="t('blocked.selectMedia')"
                :aria-pressed="covered.selected.has(item.id)"
                :disabled="covered.trashing"
                @click="covered.select(item.id, !covered.selected.has(item.id))"
              >
                {{ t('blocked.selectMedia') }}
              </button>
            </div>
            <p v-if="item.hiddenBy.length" class="also" :title="names(item)">
              {{ t('covered.hiddenBy', { names: names(item) }) }}
            </p>
          </div>
        </div>
      </div>
    </div>
    <Viewer v-if="covered.viewing !== null" :source="covered.source" />
  </section>
</template>

<style scoped>
.covered {
  display: flex;
  flex-direction: column;
  height: 100%;
  padding: 1.5rem var(--lumen-page-x);
  overflow: auto;
}
.covered > :not(.scroller) {
  flex-shrink: 0;
}
.head {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.75rem;
  margin: 1rem 0 0.5rem;
}
.head h2 {
  margin: 0;
  min-width: 0;
  flex: 1 1 15rem;
  font-size: 1.125rem;
  font-weight: 600;
  overflow-wrap: anywhere;
}
.scroller {
  flex: 1;
  min-height: 8rem;
  overflow-y: auto;
  margin-top: 1.25rem;
}
.rows {
  position: relative;
  width: 100%;
}
.row {
  position: absolute;
  top: 0;
  left: 0;
  display: flex;
  width: 100%;
}
.entry {
  display: grid;
  align-content: start;
  gap: 4px;
}
.tile-wrap {
  position: relative;
}
.cell {
  display: block;
  width: 100%;
  height: 100%;
  padding: 0;
  border: 0;
  border-radius: var(--lumen-r-sm);
  background: var(--lumen-placeholder);
  cursor: pointer;
}
.accessible-check:focus-visible {
  clip: auto;
  clip-path: none;
  width: 1.5rem;
  height: 1.5rem;
  top: 0.5rem;
  left: 0.5rem;
  z-index: 3;
  white-space: normal;
  overflow: hidden;
  border-radius: 50%;
  background: var(--ui-primary);
  color: var(--lumen-on-primary);
  outline: 2px solid var(--ui-primary);
  outline-offset: 2px;
}
.also {
  margin: 0;
  max-height: calc(2 * 1.3em);
  overflow: hidden;
  color: var(--ui-text-muted);
  font-size: 0.75rem;
  line-height: 1.3;
  overflow-wrap: anywhere;
}
</style>
