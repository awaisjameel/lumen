<script setup lang="ts">
import UBadge from '@nuxt/ui/components/Badge.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import URadioGroup from '@nuxt/ui/components/RadioGroup.vue'
import { computed } from 'vue'
import { DuplicateKind } from '../bridge'
import type { DuplicateGroup, DuplicateItem } from '../bridge'
import { icons } from '../design/icons'
import { runCommand, useCommands } from '../shared/commands'
import { bytes, fileName } from '../shared/format'
import { t } from '../shared/i18n'
import MediaTile from '../shared/MediaTile.vue'
import SelectionBar from '../shared/SelectionBar.vue'
import { useKeepChoices } from './store'

const props = defineProps<{ group: DuplicateGroup; busy: boolean }>()
const emit = defineEmits<{
  review: [group: DuplicateGroup]
  preview: [item: DuplicateItem]
  showPath: [item: DuplicateItem, path: string]
}>()
const choices = useKeepChoices()
const exact = computed(() => props.group.kind === DuplicateKind.DuplicateExact)
const keeper = computed(() => choices.keeperOf(props.group))
const review = computed(() => choices.reviewOf(props.group))
const first = computed(() => props.group.items[0])
const paths = computed(() =>
  (first.value?.paths ?? []).map((path) => ({ value: path, label: t('duplicates.keepNamed', { name: path }) })),
)
const photos = computed(() =>
  props.group.items.map((photo) => ({
    value: photo.id,
    label: t('duplicates.keepNamed', { name: fileName(photo.paths[0] ?? '') }),
    photo,
  })),
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

function choosePath(path: unknown) {
  if (first.value && typeof path === 'string') choices.choose(props.group, first.value, path)
}
function choosePhoto(id: unknown) {
  const photo = props.group.items.find((item) => item.id === id)
  if (photo) choices.choose(props.group, photo)
}
</script>

<template>
  <article class="duplicate-group lumen-surface" data-duplicate-group>
    <div v-if="exact && first" class="exact-copy">
      <button
        class="preview-button"
        :aria-label="t('duplicates.preview', { name: fileName(first.paths[0] ?? '') })"
        @click="emit('preview', first)"
      >
        <MediaTile :id="first.id" :thumb="first.thumb" />
      </button>
      <div class="copy-files">
        <p class="text-muted">{{ t('duplicates.copies', { n: first.paths.length, size: bytes(first.size) }) }}</p>
        <URadioGroup
          :items="paths"
          :model-value="keeper?.path"
          :legend="t('duplicates.keep')"
          :ui="{ label: 'break-all', wrapper: 'min-w-0' }"
          @update:model-value="choosePath"
        >
          <template #label="{ item }">
            <span class="lumen-selectable">{{ item.label }}</span>
            <UBadge v-if="first.keep === item.value" variant="outline" size="sm" data-suggested>
              {{ t('duplicates.suggested') }}
            </UBadge>
          </template>
          <template #description="{ item }">
            <UButton
              :icon="icons['folder-open']"
              :aria-label="t('duplicates.showFile', { name: item.value })"
              size="sm"
              color="neutral"
              variant="ghost"
              data-show-file
              @click="emit('showPath', first, item.value)"
              >{{ t('common.showInFolder') }}</UButton
            >
          </template>
        </URadioGroup>
      </div>
    </div>
    <URadioGroup
      v-else
      :items="photos"
      :model-value="keeper?.item.id"
      :legend="t('duplicates.keep')"
      orientation="horizontal"
      :ui="{
        fieldset: 'flex overflow-x-auto pb-2 gap-4',
        item: 'flex-none w-48 min-w-0',
        wrapper: 'min-w-0 flex-1',
        label: 'block truncate',
      }"
      @update:model-value="choosePhoto"
    >
      <template #label="{ item }">
        <span :title="item.photo.paths[0]">{{ item.label }}</span>
      </template>
      <template #description="{ item }">
        <span class="similar-item">
          <button
            class="preview-button"
            :aria-label="t('duplicates.preview', { name: fileName(item.photo.paths[0] ?? '') })"
            @click="emit('preview', item.photo)"
          >
            <MediaTile :id="item.photo.id" :thumb="item.photo.thumb" />
          </button>
          <span>{{
            t('duplicates.photoSize', { w: item.photo.width, h: item.photo.height, size: bytes(item.photo.size) })
          }}</span>
          <UBadge v-if="item.photo.best" variant="outline" size="sm" data-suggested>{{
            t('duplicates.suggested')
          }}</UBadge>
          <UButton
            v-if="item.photo.paths[0]"
            :icon="icons['folder-open']"
            :aria-label="t('duplicates.showFile', { name: item.photo.paths[0] })"
            color="neutral"
            variant="ghost"
            size="sm"
            data-show-file
            @click="emit('showPath', item.photo, item.photo.paths[0])"
            >{{ t('common.showInFolder') }}</UButton
          >
        </span>
      </template>
    </URadioGroup>
    <SelectionBar :summary="summary" :hint="hint" class="group-summary">
      <UButton
        :icon="icons.duplicates"
        :aria-label="t('duplicates.reviewNamed', { name: fileName(group.items[0]?.paths[0] ?? '') })"
        :disabled="busy || !review"
        color="primary"
        variant="soft"
        data-review
        @click="runCommand(commandId)"
        >{{ t('duplicates.review') }}</UButton
      >
    </SelectionBar>
  </article>
</template>

<style scoped>
.duplicate-group {
  padding: 0.875rem;
  margin: 0.75rem 0;
  border: 1px solid var(--ui-border);
  border-radius: calc(var(--ui-radius) * 2);
  background: var(--ui-bg);
}
.exact-copy {
  display: flex;
  align-items: flex-start;
  flex-wrap: wrap;
  gap: 1rem;
}
.copy-files {
  flex: 1 1 16rem;
  min-width: 0;
}
.copy-files p {
  margin: 0 0 0.5rem;
}
.preview-button {
  display: block;
  width: 10rem;
  height: 10rem;
  padding: 0;
  border: 0;
  border-radius: var(--ui-radius);
  background: var(--lumen-placeholder);
  cursor: zoom-in;
}
.similar-item {
  display: flex;
  align-items: flex-start;
  flex-direction: column;
  gap: 0.375rem;
  margin-top: 0.375rem;
}
.group-summary {
  margin: 0.875rem 0 0;
}
.group-summary :deep(.summary) {
  overflow-wrap: anywhere;
}
</style>
