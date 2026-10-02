<script setup lang="ts">
// One Thing's card: cover, name and count, with what the user can do to it.
import { thumbUrl } from '../bridge'
import type { Tag } from '../bridge'
import { t } from '../shared/i18n'
import Icon from '../shared/Icon.vue'
import type { IconName } from '../shared/Icon.vue'
import { nameTooLong } from '../shared/names'
import { useRename } from '../shared/rename'
import { MAX_NAME_LEN } from '../bridge'

const props = defineProps<{
  tag: Tag
  icon: IconName
  deletable: boolean
  /** Saves a new name ('' restores the built-in's own); resolves true when it was saved. */
  rename: (id: number, name: string) => Promise<boolean>
}>()
defineEmits<{ pin: []; block: []; remove: [] }>()

const editor = useRename<number>()
const { editing, draft } = editor

async function save(name: string) {
  if (nameTooLong(name)) return
  if (await props.rename(props.tag.id, name)) void editor.close()
}
</script>

<template>
  <div class="card">
    <RouterLink :to="{ name: 'thing', params: { id: tag.id } }" class="card-cover" :aria-label="tag.name">
      <img
        v-if="tag.cover && tag.coverThumb"
        :src="thumbUrl(tag.cover, tag.coverThumb)"
        alt=""
        loading="lazy"
        decoding="async"
        draggable="false"
      />
      <Icon v-else :name="icon" :size="32" />
    </RouterLink>
    <span v-if="tag.pinned" class="pin-badge" role="img" :aria-label="t('things.pinned')" :title="t('things.pinned')">
      <Icon name="pin" :size="14" />
    </span>
    <form v-if="editing === tag.id" class="rename" @submit.prevent="save(draft)" @keydown.esc.stop="editor.close()">
      <input
        :ref="editor.fieldRef"
        v-model="draft"
        class="field"
        :aria-label="t('things.name')"
        :aria-invalid="nameTooLong(draft) || undefined"
      />
      <p v-if="nameTooLong(draft)" class="page-error" role="alert">
        {{ t('error.invalidName', { max: MAX_NAME_LEN }) }}
      </p>
      <button class="btn" type="submit" :disabled="nameTooLong(draft)">{{ t('things.save') }}</button>
      <button class="btn btn-subtle" type="button" @click="editor.close()">{{ t('common.cancel') }}</button>
    </form>
    <div v-else class="card-meta">
      <span class="card-name">{{ tag.name }}</span>
      <span class="card-count">{{ tag.count ? t('status.items', { n: tag.count }) : t('things.noPhotos') }}</span>
      <button v-if="tag.original" class="reset" type="button" :title="tag.original" @click="rename(tag.id, '')">
        {{ t('things.resetName') }}
      </button>
    </div>
    <div class="acts">
      <button
        class="icon-btn card-act"
        :title="t('blocked.block')"
        :aria-label="t('blocked.confirmTitle', { name: tag.name })"
        @click="$emit('block')"
      >
        <Icon name="eye-off" :size="16" />
      </button>
      <button
        class="icon-btn card-act"
        :title="tag.pinned ? t('things.unpin') : t('things.pin')"
        :aria-label="tag.pinned ? t('things.unpinNamed', { name: tag.name }) : t('things.pinNamed', { name: tag.name })"
        :aria-pressed="tag.pinned"
        @click="$emit('pin')"
      >
        <Icon name="pin" :size="16" />
      </button>
      <button
        :ref="editor.triggerRef(tag.id)"
        class="icon-btn card-act"
        :title="t('things.rename')"
        :aria-label="t('things.renameNamed', { name: tag.name })"
        @click="editor.open(tag.id, tag.name)"
      >
        <Icon name="pencil" :size="16" />
      </button>
      <button
        v-if="deletable"
        class="icon-btn card-act"
        :title="t('things.delete')"
        :aria-label="t('things.deleteNamed', { name: tag.name })"
        @click="$emit('remove')"
      >
        <Icon name="trash" :size="16" />
      </button>
    </div>
  </div>
</template>

<style scoped>
.acts {
  position: absolute;
  top: 6px;
  right: 6px;
  display: flex;
  gap: 6px;
}
.acts .card-act {
  position: static;
}
.pin-badge {
  position: absolute;
  top: 6px;
  left: 6px;
  display: grid;
  place-items: center;
  width: 22px;
  height: 22px;
  border-radius: 50%;
  color: var(--text);
  background: var(--surface);
}
.rename {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  padding-top: 6px;
}
.rename input {
  min-width: 0;
  width: 100%;
}
.reset {
  align-self: flex-start;
  padding: 0;
  border: 0;
  color: var(--accent);
  background: none;
  font-size: 12px;
  cursor: pointer;
}
</style>
