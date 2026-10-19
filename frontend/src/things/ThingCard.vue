<script setup lang="ts">
// Card actions share their definitions with the command palette.
import IconButton from '../shared/IconButton.vue'
import UBadge from '@nuxt/ui/components/Badge.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UDropdownMenu from '@nuxt/ui/components/DropdownMenu.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import { computed, ref, watch } from 'vue'
import { Filter, MAX_NAME_LEN, TagKind, thumbUrl } from '../bridge'
import type { Tag } from '../bridge'
import { icons } from '../design/icons'
import type { IconName } from '../design/icons'
import CollectionCard from '../shared/CollectionCard.vue'
import CoverMosaic from '../shared/CoverMosaic.vue'
import Icon from '../shared/Icon.vue'
import { menuItems, useCommands } from '../shared/commands'
import type { Command } from '../shared/commands'
import { t } from '../shared/i18n'
import { nameTooLong } from '../shared/names'
import { useRename } from '../shared/rename'

const props = defineProps<{
  tag: Tag
  icon: IconName
  deletable: boolean
  inAlbum?: boolean
  working?: boolean
  /** The Thing was just made here: it carries a New badge. */
  fresh?: boolean
  /** Why this Thing's last rename failed, shown in its editor. */
  error?: string
  /** An empty name restores the built-in name; false leaves the draft open. */
  rename: (id: number, name: string) => Promise<boolean>
}>()
const emit = defineEmits<{ pin: []; block: []; remove: []; editing: [open: boolean] }>()
const editor = useRename<number>()
const { editing, draft } = editor
const saving = ref(false)
const renameFrom = ref<'title' | 'menu'>('menu')
watch(editing, (id) => emit('editing', id !== null))

async function save(name: string) {
  if (nameTooLong(name) || saving.value || props.working) return
  saving.value = true
  try {
    if (await props.rename(props.tag.id, name)) void editor.close()
  } finally {
    saving.value = false
  }
}

const actions: Command[] = [
  {
    id: `thing.${props.tag.id}.pin`,
    group: 'view',
    title: () => t(props.tag.pinned ? 'things.unpinNamed' : 'things.pinNamed', { name: props.tag.name }),
    icon: 'pin',
    enabled: () => !props.working && !saving.value,
    run: () => emit('pin'),
  },
  {
    id: `thing.${props.tag.id}.rename`,
    group: 'view',
    title: () => t('things.renameNamed', { name: props.tag.name }),
    icon: 'pencil',
    enabled: () => !saving.value && !props.working,
    run: () => {
      renameFrom.value = 'menu'
      return editor.open(props.tag.id, props.tag.name)
    },
  },
  {
    id: `thing.${props.tag.id}.reset`,
    group: 'view',
    title: () => t('things.resetName'),
    icon: 'reset',
    available: () => !!props.tag.original,
    enabled: () => !saving.value && !props.working,
    run: () => save(''),
  },
  {
    id: `thing.${props.tag.id}.block`,
    group: 'view',
    title: () => t('blocked.confirmTitle', { name: props.tag.name }),
    icon: 'eye-off',
    enabled: () => !saving.value && !props.working,
    run: () => emit('block'),
  },
  {
    id: `thing.${props.tag.id}.delete`,
    group: 'view',
    title: () => t('things.deleteNamed', { name: props.tag.name }),
    icon: 'trash',
    available: () => props.deletable,
    enabled: () => !props.inAlbum && !props.working,
    reason: () => (props.inAlbum ? t('things.inAlbum') : undefined),
    danger: true,
    run: () => emit('remove'),
  },
]
useCommands(actions)
const items = computed(() => menuItems(actions))
function keepEditorFocus(e: Event) {
  if (editing.value !== null) e.preventDefault()
}
// Nuxt UI exposes the native input; rename keeps its focus and selection behaviour.
function fieldRef(el: unknown) {
  editor.fieldRef(
    el && typeof el === 'object' && 'inputRef' in el && el.inputRef instanceof HTMLInputElement ? el.inputRef : null,
  )
}
function titleRef(el: unknown) {
  if (renameFrom.value === 'title') editor.triggerRef(props.tag.id)(el)
}
function menuRef(el: unknown) {
  if (renameFrom.value === 'menu') editor.triggerRef(props.tag.id)(el)
}
function renameTitle() {
  renameFrom.value = 'title'
  void editor.open(props.tag.id, props.tag.name)
}
</script>

<template>
  <div>
    <CollectionCard
      class="thing-card"
      :data-collection="tag.id"
      :to="{ name: 'thing', params: { id: tag.id } }"
      :title="tag.name"
      :cover="tag.cover && tag.coverThumb ? thumbUrl(tag.cover, tag.coverThumb) : undefined"
      :icon="icon"
    >
      <template #cover>
        <CoverMosaic
          class="card-mosaic"
          :view="{ filter: Filter.FilterAll, tag: tag.id }"
          :count="tag.count"
          :cover="tag.cover"
          :cover-thumb="tag.coverThumb"
          :icon="icon"
        />
      </template>
      <template #title>
        <div class="title-row">
          <button
            v-if="editing !== tag.id"
            :ref="titleRef"
            class="name rename-trigger"
            type="button"
            :title="t('things.renameNamed', { name: tag.name })"
            @click="renameTitle"
          >
            {{ tag.name }}
          </button>
          <span v-else class="flex-1" />
          <div class="card-actions">
            <IconButton
              :icon="icons.pin"
              :label="t(tag.pinned ? 'things.unpinNamed' : 'things.pinNamed', { name: tag.name })"
              :aria-pressed="tag.pinned"
              :disabled="working || saving"
              :variant="tag.pinned ? 'soft' : 'ghost'"
              :color="tag.pinned ? 'primary' : 'neutral'"
              size="sm"
              @click="emit('pin')"
            />
            <UDropdownMenu :items="items" :content="{ onCloseAutoFocus: keepEditorFocus }">
              <IconButton
                :ref="menuRef"
                :icon="icons.more"
                :label="t('things.actions', { name: tag.name })"
                variant="ghost"
                color="neutral"
                size="sm"
              />
              <template #content-bottom>
                <p v-if="!deletable" class="menu-hint">{{ t('things.builtinRestriction') }}</p>
                <p v-if="inAlbum" class="menu-hint">{{ t('things.inAlbum') }}</p>
              </template>
            </UDropdownMenu>
          </div>
        </div>
        <form
          v-if="editing === tag.id"
          class="rename"
          data-rename-thing
          @submit.prevent="save(draft)"
          @keydown.esc.stop="!saving && editor.close()"
        >
          <UInput
            :ref="fieldRef"
            v-model="draft"
            class="w-full"
            :aria-label="t('things.name')"
            :aria-invalid="nameTooLong(draft) || !!error || undefined"
            :disabled="saving"
          />
          <p v-if="nameTooLong(draft)" class="text-xs text-error" role="alert">
            {{ t('error.invalidName', { max: MAX_NAME_LEN }) }}
          </p>
          <p v-else-if="error" class="text-xs text-error" role="alert">{{ error }}</p>
          <UButton type="submit" size="sm" :loading="saving" :disabled="saving || nameTooLong(draft)">{{
            t('things.save')
          }}</UButton>
          <UButton type="button" size="sm" variant="ghost" :disabled="saving" @click="editor.close()">{{
            t('common.cancel')
          }}</UButton>
          <UButton v-if="tag.original" type="button" size="sm" variant="ghost" :disabled="saving" @click="save('')">{{
            t('things.resetName')
          }}</UButton>
        </form>
        <div class="count-row">
          <span>{{ tag.count ? t('status.items', { n: tag.count }) : t('things.noPhotos') }}</span
          ><UBadge color="neutral" variant="soft" size="sm">{{
            t(tag.kind === TagKind.TagBuiltin ? 'things.builtin' : 'things.yours')
          }}</UBadge
          ><span v-if="fresh" class="fresh"><Icon name="smart" :size="12" />{{ t('things.fresh') }}</span
          ><span v-if="tag.pinned" data-pinned class="sr-only" :aria-label="t('things.pinned')">{{
            t('things.pinned')
          }}</span>
        </div>
        <p v-if="!tag.count" class="text-xs text-dimmed">{{ t('things.viewEmptyHint') }}</p>
        <div v-if="inAlbum" class="text-xs text-dimmed">
          {{ t('things.inAlbum') }} <UButton to="/albums" size="xs" variant="link">{{ t('things.editAlbum') }}</UButton>
        </div>
      </template>
    </CollectionCard>
  </div>
</template>

<style scoped>
.card-mosaic {
  position: absolute;
  inset: 0;
}
.title-row {
  display: flex;
  align-items: center;
  gap: 0.25rem;
  min-width: 0;
}
.rename-trigger {
  flex: 1;
  min-width: 0;
  min-height: 1.5rem;
  padding: 0 0.125rem;
  border: 0;
  border-radius: var(--lumen-r-xs);
  background: transparent;
  text-align: start;
  cursor: pointer;
  overflow: hidden;
  color: var(--ui-text-highlighted);
  font-weight: 600;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.rename-trigger:focus-visible {
  outline: 2px solid var(--ui-primary);
  outline-offset: 2px;
}
/* The card's actions show on hover or focus, and always without hover. */
.card-actions {
  display: flex;
  flex: none;
  align-items: center;
  opacity: 0;
  transition: opacity var(--lumen-d-fast) var(--lumen-ease-out);
}
.thing-card:hover .card-actions,
.thing-card:focus-within .card-actions,
.card-actions:has([aria-expanded='true']) {
  opacity: 1;
}
@media (hover: none) {
  .card-actions {
    opacity: 1;
  }
}
.rename {
  display: flex;
  flex-wrap: wrap;
  gap: 0.25rem;
  padding-top: 0.375rem;
}
.count-row {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 0.5rem;
  margin-top: 0.25rem;
  color: var(--ui-text-dimmed);
  font-size: 0.75rem;
  font-variant-numeric: tabular-nums;
}
.fresh {
  display: inline-flex;
  align-items: center;
  gap: 0.25rem;
  padding: 0.125rem 0.375rem;
  border-radius: var(--lumen-r-sm);
  background: var(--lumen-light-soft);
  color: var(--ui-text-toned);
  font-weight: 600;
}
.menu-hint {
  max-width: 18rem;
  padding: 0.5rem;
  color: var(--ui-text-dimmed);
  font-size: 0.75rem;
}
</style>
