<script setup lang="ts">
// The Blocked screen's browser: the photos one block covers, in a virtualised
// grid. A photo opens in the Viewer, which can move it to the Trash. Photos
// that other blocks hide too say which, so unblocking this one is no surprise.
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UCheckbox from '@nuxt/ui/components/Checkbox.vue'
import { useVirtualizer } from '@tanstack/vue-virtual'
import { BlockMode } from '../bridge'
import type { CoveredItem } from '../bridge'
import { listFormat, t } from '../shared/i18n'
import { icons } from '../design/icons'
import { runCommand, useCommands } from '../shared/commands'
import { observeResize } from '../shared/observeResize'
import FeatureState from '../shared/FeatureState.vue'
import MediaTile from '../shared/MediaTile.vue'
import SelectionBar from '../shared/SelectionBar.vue'
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
onBeforeUnmount(() => stopResize?.())

const labelOnly = computed(() => props.covered.block?.mode === BlockMode.BlockLabel)
const layout = computed(() =>
  buildLayout(
    props.covered.items.length ? [{ day: 'covered', count: props.covered.items.length }] : [],
    Math.max(width.value, 200),
    TARGET_CELL,
    false,
  ),
)
const virtualizer = useVirtualizer(
  computed(() => ({
    count: layout.value.rows.length,
    getScrollElement: () => scroller.value,
    estimateSize: (i: number) => {
      const row = layout.value.rows[i]
      return (row ? rowHeight(row, layout.value.cell) : layout.value.cell) + CAPTION_H
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
    <SelectionBar
      v-if="covered.selected.size"
      :summary="t('blocked.selected', { n: covered.selected.size })"
      :hint="t('covered.trashText')"
    >
      <UButton
        :disabled="covered.trashing"
        :loading="covered.trashing"
        :icon="icons.trash"
        @click="runCommand('blocked.covered.trash')"
        >{{ t('trash.move') }}</UButton
      >
      <UButton @click="runCommand('blocked.covered.clear')">{{ t('blocked.clearSelection') }}</UButton>
    </SelectionBar>
    <FeatureState v-if="covered.loading" kind="loading" :title="t('covered.loading')" />
    <FeatureState v-else-if="!covered.items.length" kind="empty" icon="photos" :title="t('covered.empty')" />
    <div ref="scroller" class="scroller" @scroll.passive="covered.touch">
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
                :aria-label="t(item.v ? 'covered.openVideo' : 'covered.openPhoto')"
                @click="covered.view(item.id)"
              >
                <MediaTile
                  :id="item.id"
                  :thumb="item.t"
                  :video="item.v"
                  :duration-ms="item.d"
                  :selected="covered.selected.has(item.id)"
                >
                  <template v-if="item.v && item.t" #media><VideoThumb :item="item" :cell="layout.cell" /></template>
                </MediaTile>
              </button>
              <UCheckbox
                class="tile-select"
                :model-value="covered.selected.has(item.id)"
                :aria-label="t('blocked.selectMedia')"
                :disabled="covered.trashing"
                @update:model-value="covered.select(item.id, $event === true)"
              />
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
  height: calc(100vh - 280px);
  min-height: 320px;
  overflow-y: auto;
  margin-top: 0.75rem;
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
  border-radius: var(--ui-radius);
  background: var(--lumen-placeholder);
  cursor: pointer;
}
.tile-select {
  position: absolute;
  top: 0.375rem;
  left: 0.375rem;
  padding: 0.125rem;
  border-radius: var(--ui-radius);
  background: var(--ui-bg);
}
.also {
  margin: 0;
  max-height: calc(2 * 1.3em);
  overflow: hidden;
  color: var(--ui-text-muted);
  font-size: 12px;
  line-height: 1.3;
  overflow-wrap: anywhere;
}
</style>
