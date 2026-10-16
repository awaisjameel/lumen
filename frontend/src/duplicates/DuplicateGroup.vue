<script setup lang="ts">
import IconButton from '../shared/IconButton.vue'
import UBadge from '@nuxt/ui/components/Badge.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import URadioGroup from '@nuxt/ui/components/RadioGroup.vue'
import { computed } from 'vue'
import { DuplicateKind } from '../bridge'
import type { DuplicateGroup, DuplicateItem } from '../bridge'
import { icons } from '../design/icons'
import { runCommand, useCommands } from '../shared/commands'
import { bytes, dateTime, fileName } from '../shared/format'
import { t } from '../shared/i18n'
import MediaTile from '../shared/MediaTile.vue'
import { useKeepChoices } from './store'

const props = defineProps<{ group: DuplicateGroup; busy: boolean }>()
const emit = defineEmits<{
  review: [group: DuplicateGroup]
  preview: [item: DuplicateItem, path: string]
  showPath: [item: DuplicateItem, path: string]
}>()
const choices = useKeepChoices()
const exact = computed(() => props.group.kind === DuplicateKind.DuplicateExact)
const keeper = computed(() => choices.keeperOf(props.group))
const review = computed(() => choices.reviewOf(props.group))
const first = computed(() => props.group.items[0])
const copies = computed(() =>
  exact.value
    ? (first.value?.paths ?? []).flatMap((path) =>
        first.value
          ? [{ value: path, label: fileName(path), photo: first.value, path, suggested: path === first.value.keep }]
          : [],
      )
    : props.group.items.map((photo) => ({
        value: String(photo.id),
        label: fileName(photo.paths[0] ?? ''),
        photo,
        path: photo.paths[0] ?? '',
        suggested: photo.best,
      })),
)
const selected = computed(() => (exact.value ? keeper.value?.path : String(keeper.value?.item.id)))
const removalCount = computed(() =>
  exact.value ? (review.value?.recycle.length ?? 0) : (review.value?.trash.length ?? 0),
)
const description = computed(() =>
  exact.value && first.value
    ? t('duplicates.groupExact', { n: first.value.paths.length, size: bytes(first.value.size * removalCount.value) })
    : t('duplicates.groupSimilar', { n: props.group.items.length }),
)
const summary = computed(() =>
  t('duplicates.selection', {
    name: keeper.value ? (exact.value ? keeper.value.path : fileName(keeper.value.item.paths[0] ?? '')) : '',
  }),
)
const hint = computed(() =>
  t(exact.value ? 'duplicates.recycleSummary' : 'duplicates.trashSummary', {
    n: exact.value ? (review.value?.recycle.length ?? 0) : (review.value?.trash.length ?? 0),
  }),
)
const commandId = `duplicates.review.${props.group.kind}.${props.group.items[0]?.id ?? 0}`
useCommands([
  {
    id: commandId,
    group: 'library',
    title: () => t('duplicates.reviewNamed', { name: fileName(props.group.items[0]?.paths[0] ?? '') }),
    icon: 'duplicates',
    enabled: () => !props.busy && review.value !== null,
    run: () => emit('review', props.group),
  },
])

function chooseCopy(value: unknown) {
  const copy = copies.value.find((item) => item.value === value)
  if (copy && !props.busy) choices.choose(props.group, copy.photo, exact.value ? copy.path : '')
}
</script>

<template>
  <article class="duplicate-group" data-duplicate-group>
    <header class="group-heading">
      <h3>{{ fileName(first?.paths[0] ?? '') }}</h3>
      <span class="group-description">{{ description }}</span>
      <span v-if="first?.takenAt" class="group-date">{{ dateTime(first.takenAt) }}</span>
    </header>
    <URadioGroup
      :items="copies"
      :model-value="selected"
      :legend="t('duplicates.keep')"
      :disabled="busy"
      orientation="horizontal"
      indicator="hidden"
      :ui="{
        fieldset: 'copy-grid',
        item: 'copy-choice',
        wrapper: 'min-w-0 flex-1',
        label: 'sr-only',
        legend: 'sr-only',
      }"
      @update:model-value="chooseCopy"
    >
      <template #label="{ item }"
        ><span :title="item.path">{{ item.label }}</span></template
      >
      <template #description="{ item }">
        <div class="copy-card">
          <button
            class="keep-button"
            :aria-label="t('duplicates.keepNamed', { name: item.path })"
            :aria-pressed="item.value === selected"
            :disabled="busy"
            @click="chooseCopy(item.value)"
          >
            <MediaTile :id="item.photo.id" :thumb="item.photo.thumb" />
            <span v-if="item.value === selected" class="kept-badge">{{ t('duplicates.keeping') }}</span>
          </button>
          <strong class="copy-name">{{ item.label }}</strong>
          <span class="copy-spec">{{
            t('duplicates.photoSize', { w: item.photo.width, h: item.photo.height, size: bytes(item.photo.size) })
          }}</span>
          <span v-if="item.photo.takenAt" class="copy-spec">{{ dateTime(item.photo.takenAt) }}</span>
          <UBadge v-if="item.suggested" variant="outline" color="neutral" size="sm" data-suggested>{{
            t('duplicates.suggested')
          }}</UBadge>
          <span v-for="path in exact ? [item.path] : item.photo.paths" :key="path" class="copy-path lumen-selectable">{{
            path
          }}</span>
          <div class="copy-actions">
            <UButton
              class="preview-button"
              :icon="icons.aperture"
              :aria-label="t('duplicates.preview', { name: item.label })"
              color="neutral"
              variant="ghost"
              size="sm"
              data-copy-preview
              @click="emit('preview', item.photo, item.path)"
              >{{ t('duplicates.previewAction') }}</UButton
            >
            <UButton
              :disabled="busy"
              :variant="item.value === selected ? 'soft' : 'ghost'"
              :icon="item.value === selected ? icons.check : undefined"
              size="sm"
              :aria-label="t('duplicates.keepNamed', { name: item.path })"
              @click="chooseCopy(item.value)"
              >{{ t(item.value === selected ? 'duplicates.keeping' : 'duplicates.keepThis') }}</UButton
            >
            <IconButton
              :icon="icons['folder-open']"
              :label="t('duplicates.showFile', { name: item.path })"
              size="sm"
              color="neutral"
              variant="ghost"
              data-show-file
              @click="emit('showPath', item.photo, item.path)"
            />
          </div>
        </div>
      </template>
    </URadioGroup>
    <div class="group-summary" role="toolbar" :aria-label="t('duplicates.reviewTitle')">
      <div class="outcome">
        <span>{{ summary }}</span
        ><span>{{ hint }}</span>
      </div>
      <UButton
        :icon="icons['chevron-right']"
        :aria-label="t('duplicates.reviewNamed', { name: fileName(first?.paths[0] ?? '') })"
        :disabled="busy || !review"
        color="primary"
        variant="solid"
        data-review
        @click="runCommand(commandId)"
        >{{ t('duplicates.review') }}</UButton
      >
    </div>
  </article>
</template>

<style scoped>
.duplicate-group {
  padding: 1.25rem;
  margin: 0.75rem 0;
  border: 1px solid var(--ui-border);
  border-radius: var(--lumen-r-lg);
  background: var(--lumen-bg-raised);
}
.group-heading {
  display: flex;
  align-items: baseline;
  gap: 0.75rem;
  flex-wrap: wrap;
  margin-bottom: 1rem;
}
h3 {
  margin: 0;
  font-size: 1rem;
  font-weight: 600;
  color: var(--ui-text-highlighted);
  overflow-wrap: anywhere;
}
.group-description,
.group-date {
  color: var(--ui-text-dimmed);
  font-size: 0.75rem;
  font-variant-numeric: tabular-nums;
}
.group-date {
  margin-left: auto;
}
:deep(.copy-grid) {
  display: grid;
  grid-auto-flow: column;
  grid-auto-columns: minmax(8rem, 1fr);
  gap: 1rem;
  overflow-x: auto;
  padding: 0.25rem;
}
:deep(.copy-choice) {
  min-width: 0;
}
.copy-card {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 0.5rem;
  margin-top: 0.5rem;
}
.keep-button {
  position: relative;
  display: block;
  width: 100%;
  aspect-ratio: 16 / 9;
  padding: 0;
  border: 0;
  border-radius: var(--lumen-r-md);
  overflow: hidden;
  background: var(--lumen-placeholder);
  cursor: pointer;
}
.keep-button[aria-pressed='true'] {
  outline: 2px solid var(--ui-primary);
  outline-offset: 2px;
}
.kept-badge {
  position: absolute;
  left: 0.5rem;
  top: 0.5rem;
  padding: 0.25rem 0.5rem;
  border-radius: var(--lumen-r-sm);
  background: var(--ui-primary);
  color: var(--lumen-on-primary);
  font-size: 0.75rem;
  font-weight: 600;
}
.copy-spec {
  color: var(--ui-text-toned);
  font-size: 0.75rem;
  font-variant-numeric: tabular-nums;
}
.copy-path {
  color: var(--ui-text-dimmed);
  font-family: var(--font-mono);
  font-size: 0.6875rem;
  overflow-wrap: anywhere;
}
.copy-actions,
.group-summary {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  flex-wrap: wrap;
}
.group-summary {
  justify-content: space-between;
  margin-top: 1rem;
  padding-top: 1rem;
  border-top: 1px solid var(--ui-border);
}
.outcome {
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
  min-width: 0;
  color: var(--ui-text-toned);
  font-size: 0.75rem;
  overflow-wrap: anywhere;
}
.copy-name {
  color: var(--ui-text-highlighted);
  font-size: 0.8125rem;
  overflow-wrap: anywhere;
}
:deep(.copy-choice:focus-within) .keep-button {
  outline: 2px solid var(--ui-primary);
  outline-offset: 2px;
}
</style>
