<script setup lang="ts">
// Card actions share their definitions with the command palette.
import UBadge from '@nuxt/ui/components/Badge.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UDropdownMenu from '@nuxt/ui/components/DropdownMenu.vue'
import type { DropdownMenuItem } from '@nuxt/ui/components/DropdownMenu.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import { computed, ref } from 'vue'
import { MAX_NAME_LEN, TagKind, thumbUrl } from '../bridge'
import type { Tag } from '../bridge'
import { icons } from '../design/icons'
import type { IconName } from '../design/icons'
import CollectionCard from '../shared/CollectionCard.vue'
import { isEnabled, useCommands } from '../shared/commands'
import type { Command } from '../shared/commands'
import { t } from '../shared/i18n'
import { nameTooLong } from '../shared/names'
import { useRename } from '../shared/rename'

const props = defineProps<{
  tag: Tag
  icon: IconName
  deletable: boolean
  /** An empty name restores the built-in name; false leaves the draft open. */
  rename: (id: number, name: string) => Promise<boolean>
}>()
const emit = defineEmits<{ pin: []; block: []; remove: [] }>()
const editor = useRename<number>()
const { editing, draft } = editor
const saving = ref(false)

async function save(name: string) {
  if (nameTooLong(name) || saving.value) return
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
    run: () => emit('pin'),
  },
  {
    id: `thing.${props.tag.id}.rename`,
    group: 'view',
    title: () => t('things.renameNamed', { name: props.tag.name }),
    icon: 'pencil',
    enabled: () => !saving.value,
    run: () => editor.open(props.tag.id, props.tag.name),
  },
  {
    id: `thing.${props.tag.id}.reset`,
    group: 'view',
    title: () => t('things.resetName'),
    icon: 'reset',
    available: () => !!props.tag.original,
    enabled: () => !saving.value,
    run: () => save(''),
  },
  {
    id: `thing.${props.tag.id}.block`,
    group: 'view',
    title: () => t('blocked.confirmTitle', { name: props.tag.name }),
    icon: 'eye-off',
    run: () => emit('block'),
  },
  {
    id: `thing.${props.tag.id}.delete`,
    group: 'view',
    title: () => t('things.deleteNamed', { name: props.tag.name }),
    icon: 'trash',
    available: () => props.deletable,
    run: () => emit('remove'),
  },
]
useCommands(actions)
const items = computed<DropdownMenuItem[]>(() =>
  actions
    .filter((c) => c.available?.() ?? true)
    .map((c) => ({
      label: c.title(),
      icon: c.icon ? icons[c.icon] : undefined,
      disabled: !isEnabled(c),
      onSelect: () => {
        void c.run()
      },
    })),
)
function keepEditorFocus(e: Event) {
  if (editing.value !== null) e.preventDefault()
}
// Nuxt UI exposes the native input; rename keeps its focus and selection behaviour.
function fieldRef(el: unknown) {
  editor.fieldRef(
    el && typeof el === 'object' && 'inputRef' in el && el.inputRef instanceof HTMLInputElement ? el.inputRef : null,
  )
}
function triggerRef(el: unknown, key: number) {
  editor.triggerRef(key)(el && typeof el === 'object' && '$el' in el && el.$el instanceof HTMLElement ? el.$el : null)
}
</script>

<template>
  <CollectionCard
    :to="{ name: 'thing', params: { id: tag.id } }"
    :title="tag.name"
    :count="tag.count ? t('status.items', { n: tag.count }) : t('things.noPhotos')"
    :cover="tag.cover && tag.coverThumb ? thumbUrl(tag.cover, tag.coverThumb) : undefined"
    :icon="icon"
  >
    <template #badge>
      <div class="flex flex-wrap gap-1">
        <UBadge color="neutral" variant="subtle" size="xs">{{
          t(tag.kind === TagKind.TagBuiltin ? 'things.builtin' : 'things.custom')
        }}</UBadge>
        <UBadge
          v-if="tag.pinned"
          data-pinned
          :icon="icons.pin"
          color="primary"
          variant="subtle"
          size="xs"
          :aria-label="t('things.pinned')"
          >{{ t('things.pinned') }}</UBadge
        >
      </div>
    </template>
    <template v-if="editing === tag.id" #title>
      <form class="rename" @submit.prevent="save(draft)" @keydown.esc.stop="editor.close()">
        <UInput
          :ref="fieldRef"
          v-model="draft"
          class="w-full"
          :aria-label="t('things.name')"
          :aria-invalid="nameTooLong(draft) || undefined"
        />
        <p v-if="nameTooLong(draft)" class="text-xs text-error" role="alert">
          {{ t('error.invalidName', { max: MAX_NAME_LEN }) }}
        </p>
        <UButton type="submit" :loading="saving" :disabled="saving || nameTooLong(draft)">{{
          t('things.save')
        }}</UButton>
        <UButton type="button" variant="ghost" :disabled="saving" @click="editor.close()">{{
          t('common.cancel')
        }}</UButton>
      </form>
    </template>
    <UDropdownMenu :items="items" :content="{ onCloseAutoFocus: keepEditorFocus }">
      <UButton
        :ref="(el) => triggerRef(el, tag.id)"
        :icon="icons.more"
        :aria-label="t('things.actions', { name: tag.name })"
        square
      />
    </UDropdownMenu>
  </CollectionCard>
</template>

<style scoped>
.rename {
  display: flex;
  flex-wrap: wrap;
  gap: 0.25rem;
  padding-top: 0.375rem;
}
</style>
