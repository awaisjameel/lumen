<script setup lang="ts">
import UBadge from '@nuxt/ui/components/Badge.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UDropdownMenu from '@nuxt/ui/components/DropdownMenu.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import { computed, onBeforeUnmount, onMounted, ref, shallowRef, useTemplateRef, watch } from 'vue'
import { Filter, MAX_NAME_LEN, thumbUrl } from '../bridge'
import { engine } from '../bridge'
import type { Event as LibraryEvent, Face, Place } from '../bridge'
import { icons } from '../design/icons'
import CollectionCard from '../shared/CollectionCard.vue'
import Icon from '../shared/Icon.vue'
import IconButton from '../shared/IconButton.vue'
import { menuItems, useCommands } from '../shared/commands'
import type { Command } from '../shared/commands'
import { useCollectionPreview } from '../shared/collectionPreview'
import { onBlocksChanged } from '../shared/block'
import { logError } from '../shared/errors'
import { LOCALE, t } from '../shared/i18n'
import { nameTooLong } from '../shared/names'
import { useRename } from '../shared/rename'
import FaceThumb from '../people/FaceThumb.vue'

const props = defineProps<{
  event: LibraryEvent
  place?: Place
  busy: boolean
  activeEditor?: number
  /** Why this event's last rename failed, shown in its editor. */
  error?: string
  rename: (event: LibraryEvent, value: string) => Promise<boolean>
}>()
const emit = defineEmits<{ pin: []; block: []; editing: [open: boolean] }>()
const editor = useRename<number>()
const { editing, draft } = editor
const saving = ref(false)
const renameFrom = ref<'title' | 'menu'>('menu')
const root = useTemplateRef<HTMLElement>('root')
const faces = shallowRef<Face[]>([])
const preview = useCollectionPreview(() => ({ filter: Filter.FilterAll, event: props.event.id }))
// Dates are wall-clock seconds, so they are formatted in UTC. An event within
// one day also says when it started and ended.
const dateFormat = new Intl.DateTimeFormat(LOCALE, { day: 'numeric', month: 'short', year: 'numeric', timeZone: 'UTC' })
const timeFormat = new Intl.DateTimeFormat(LOCALE, { hour: '2-digit', minute: '2-digit', timeZone: 'UTC' })
const dates = computed(() => {
  const start = new Date(props.event.startAt * 1000)
  const end = new Date(props.event.endAt * 1000)
  if (start.toISOString().slice(0, 10) !== end.toISOString().slice(0, 10) || start >= end)
    return dateFormat.formatRange(start, end)
  return t('events.dayTimes', { day: dateFormat.format(start), times: timeFormat.formatRange(start, end) })
})
// A kind the event has none of is left out: "9 photos", not "9 photos · 0 videos".
const counts = computed(() => {
  const videos = props.event.videos ?? 0
  const photos = props.event.count - videos
  return [photos ? t('events.photos', { n: photos }) : '', videos ? t('events.videos', { n: videos }) : '']
    .filter(Boolean)
    .join(' · ')
})
watch(editing, (id) => emit('editing', id !== null))
watch(
  () => props.activeEditor,
  (id) => {
    if (id !== undefined && id !== props.event.id && editing.value !== null) void editor.close(false)
  },
)
const images = computed(() => preview.items.value.filter((item) => item.t).slice(0, 4))
let observer: IntersectionObserver | undefined
let active = true
let requested = false
let faceRevision = 0
async function sample() {
  if (requested || !props.event.cover || !props.event.coverThumb) return
  requested = true
  const current = ++faceRevision
  await preview.load()
  try {
    const result = await engine.assetFaces(props.event.cover)
    if (active && current === faceRevision)
      faces.value = result
        .filter(
          (face, index) => face.person && face.name && result.findIndex((f) => f.person === face.person) === index,
        )
        .slice(0, 3)
  } catch (e) {
    logError(e)
  }
}
watch(
  () => `${props.event.cover}:${props.event.coverThumb}:${props.event.count}`,
  () => {
    if (!requested) return
    requested = false
    preview.clear()
    faces.value = []
    void sample()
  },
)
onMounted(() => {
  observer = new IntersectionObserver((entries) => {
    if (entries.some((entry) => entry.isIntersecting)) {
      void sample()
      observer?.disconnect()
    }
  })
  if (root.value) observer.observe(root.value)
})
onBeforeUnmount(
  onBlocksChanged(() => {
    active = false
    faceRevision++
    faces.value = []
    preview.clear()
    return Promise.resolve()
  }),
)
onBeforeUnmount(() => {
  active = false
  observer?.disconnect()
})
async function save(value: string) {
  if (saving.value || props.busy || nameTooLong(value)) return
  saving.value = true
  try {
    if (await props.rename(props.event, value)) await editor.close()
  } finally {
    saving.value = false
  }
}
const actions: Command[] = [
  {
    id: `event.${props.event.id}.rename`,
    group: 'view',
    title: () => t('events.rename', { name: props.event.name }),
    icon: 'pencil',
    enabled: () => !props.busy,
    run: () => {
      renameFrom.value = 'menu'
      return editor.open(props.event.id, props.event.name)
    },
  },
  {
    id: `event.${props.event.id}.pin`,
    group: 'view',
    title: () => t(props.event.pinned ? 'events.unpinNamed' : 'events.pinNamed', { name: props.event.name }),
    icon: 'pin',
    enabled: () => !props.busy,
    run: () => emit('pin'),
  },
  {
    id: `event.${props.event.id}.reset`,
    group: 'view',
    title: () => t('events.reset'),
    icon: 'reset',
    enabled: () => !props.busy,
    run: () => save(''),
  },
  {
    id: `event.${props.event.id}.block`,
    group: 'view',
    title: () => t('blocked.confirmTitle', { name: props.event.name }),
    icon: 'eye-off',
    enabled: () => !props.busy,
    run: () => emit('block'),
  },
]
useCommands(actions)
const items = computed(() => menuItems(actions))
function fieldRef(el: unknown) {
  editor.fieldRef(
    el && typeof el === 'object' && 'inputRef' in el && el.inputRef instanceof HTMLInputElement ? el.inputRef : null,
  )
}
function keepEditorFocus(e: Event) {
  if (editing.value !== null) e.preventDefault()
}
function titleRef(el: unknown) {
  if (renameFrom.value === 'title') editor.triggerRef(props.event.id)(el)
}
function menuRef(el: unknown) {
  if (renameFrom.value === 'menu') editor.triggerRef(props.event.id)(el)
}
function renameTitle() {
  renameFrom.value = 'title'
  void editor.open(props.event.id, props.event.name)
}
</script>

<template>
  <div ref="root">
    <CollectionCard
      class="event-card"
      :class="{ pinned: event.pinned }"
      :data-collection="event.id"
      :to="{ name: 'event', params: { id: event.id } }"
      :title="event.name"
      icon="events"
    >
      <template #cover>
        <div v-if="images.length > 1" class="mosaic">
          <img v-for="item in images" :key="item.id" :src="thumbUrl(item.id, item.t)" alt="" loading="lazy" /><img
            v-if="images[3]"
            class="alternate"
            :src="thumbUrl(images[3].id, images[3].t)"
            alt=""
          />
        </div>
        <img
          v-else-if="event.cover && event.coverThumb"
          :src="thumbUrl(event.cover, event.coverThumb)"
          alt=""
          loading="lazy"
        />
        <span v-else class="no-preview"><Icon name="events" :size="32" />{{ t('events.noPreview') }}</span>
      </template>
      <template #badge
        ><UBadge
          v-if="event.pinned"
          class="sr-only"
          data-pinned
          :icon="icons.pin"
          variant="subtle"
          :aria-label="t('events.pinned')"
          >{{ t('events.pinned') }}</UBadge
        ></template
      >
      <template #title>
        <div class="head">
          <div class="text">
            <span class="dates">{{ dates }}</span>
            <button
              v-if="editing !== event.id"
              :ref="titleRef"
              class="name rename-trigger"
              type="button"
              :title="t('events.rename', { name: event.name })"
              @click="renameTitle"
            >
              <span class="truncate">{{ event.name }}</span
              ><Icon name="pencil" :size="16" class="pencil" />
            </button>
            <form
              v-else
              class="rename"
              data-rename-event
              @submit.prevent="save(draft)"
              @keydown.esc.stop="!saving && editor.close()"
            >
              <UInput
                :ref="fieldRef"
                v-model="draft"
                class="w-full"
                :aria-label="t('events.name')"
                :aria-invalid="nameTooLong(draft) || !!error || undefined"
                :disabled="saving"
              />
              <p v-if="nameTooLong(draft)" class="text-xs text-error" role="alert">
                {{ t('error.invalidName', { max: MAX_NAME_LEN }) }}
              </p>
              <p v-else-if="error" class="text-xs text-error" role="alert">{{ error }}</p>
              <UButton type="submit" size="sm" :loading="saving" :disabled="busy || nameTooLong(draft)">{{
                t('events.save')
              }}</UButton
              ><UButton type="button" size="sm" variant="ghost" :disabled="busy" @click="editor.close()">{{
                t('common.cancel')
              }}</UButton
              ><UButton
                type="button"
                size="sm"
                data-action="reset"
                variant="ghost"
                :disabled="busy"
                @click="save('')"
                >{{ t('events.reset') }}</UButton
              >
            </form>
            <span class="meta"
              ><template v-if="place"
                ><Icon name="places" :size="14" />{{
                  t('events.placeName', { name: place.name, country: place.country })
                }}
                ·
              </template>
              {{ counts }}</span
            >
          </div>
          <IconButton
            :icon="icons.pin"
            :label="t(event.pinned ? 'events.unpinNamed' : 'events.pinNamed', { name: event.name })"
            :aria-pressed="event.pinned"
            :disabled="busy"
            :variant="event.pinned ? 'soft' : 'ghost'"
            :color="event.pinned ? 'primary' : 'neutral'"
            @click="emit('pin')"
          />
          <UDropdownMenu v-if="editing === null" :items="items" :content="{ onCloseAutoFocus: keepEditorFocus }"
            ><IconButton
              :ref="menuRef"
              :icon="icons.more"
              :label="t('events.actions', { name: event.name })"
              variant="ghost"
              color="neutral"
              :disabled="busy"
          /></UDropdownMenu>
        </div>
        <div class="event-footer">
          <RouterLink
            v-for="face in faces"
            :key="face.person"
            :to="{ name: 'person', params: { id: face.person } }"
            class="person"
            ><FaceThumb :face="face" :size="24" round />{{ face.name }}</RouterLink
          ><UButton
            :to="{ name: 'event', params: { id: event.id } }"
            variant="ghost"
            color="neutral"
            size="sm"
            :trailing-icon="icons['arrow-right']"
            >{{ t('events.open') }}</UButton
          >
        </div>
      </template>
    </CollectionCard>
  </div>
</template>

<style scoped>
.event-card :deep(.cover) {
  aspect-ratio: 8 / 3;
}
.pinned :deep(.cover) {
  aspect-ratio: 4;
}
.mosaic {
  position: absolute;
  inset: 0;
  display: grid;
  grid-template-columns: 2fr 1fr 1fr;
  grid-template-rows: 1fr 1fr;
  gap: 0.25rem;
}
.mosaic img {
  width: 100%;
  height: 100%;
  min-height: 0;
  object-fit: cover;
}
.mosaic > img:first-child {
  grid-row: span 2;
}
.mosaic > img:nth-child(4) {
  grid-column: span 2;
}
/* Fewer photos fill the cover too: three stand side by side, two halve it. */
.mosaic > img:first-child:nth-last-child(3) ~ img {
  grid-row: span 2;
}
.mosaic > img:first-child:nth-last-child(2) ~ img {
  grid-row: span 2;
  grid-column: span 2;
}
.mosaic .alternate {
  position: absolute;
  inset: 0;
  opacity: 0;
  transition: opacity var(--lumen-d-slow) var(--lumen-ease-out);
}
.event-card :deep(.cover:hover) .alternate,
.event-card :deep(.cover:focus-visible) .alternate {
  opacity: 1;
}
.event-card :deep(.cover img) {
  transform: none;
}
.head {
  display: flex;
  align-items: flex-start;
  gap: 0.25rem;
  min-width: 0;
}
.text {
  display: flex;
  flex: 1;
  flex-direction: column;
  gap: 0.25rem;
  min-width: 0;
}
.rename-trigger {
  display: inline-flex;
  align-items: center;
  gap: 0.5rem;
  max-width: 100%;
  min-height: 1.5rem;
  padding: 0;
  border: 0;
  background: transparent;
  text-align: start;
  cursor: pointer;
  color: var(--ui-text-highlighted);
  font: 600 1.25rem/1.4 var(--font-display);
}
.rename-trigger .pencil {
  flex: none;
  color: var(--ui-text-dimmed);
}
.rename-trigger:hover .pencil {
  color: var(--ui-text-toned);
}
.rename-trigger:focus-visible {
  border-radius: var(--lumen-r-xs);
  outline: 2px solid var(--ui-primary);
  outline-offset: 2px;
}
.rename {
  display: flex;
  flex-wrap: wrap;
  gap: 0.25rem;
}
.dates {
  color: var(--ui-text-dimmed);
  font-size: 0.75rem;
  font-variant-numeric: tabular-nums;
}
.meta {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 0.375rem;
  color: var(--ui-text-toned);
  font-size: 0.8125rem;
  font-variant-numeric: tabular-nums;
}
.event-footer {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 0.5rem;
  margin-top: 0.5rem;
}
.event-footer > :last-child {
  margin-left: auto;
}
.person {
  display: inline-flex;
  align-items: center;
  gap: 0.375rem;
  min-height: 2rem;
  padding: 0.25rem 0.75rem 0.25rem 0.25rem;
  border: 1px solid var(--ui-border);
  border-radius: 999px;
  background: var(--lumen-bg-raised);
  color: var(--ui-text-toned);
  font-size: 0.8125rem;
  font-weight: 600;
}
.person:hover {
  background: var(--lumen-bg-hover);
}
.person:focus-visible {
  outline: 2px solid var(--ui-primary);
  outline-offset: 2px;
}
.no-preview {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 0.5rem;
  font-size: 0.75rem;
}
</style>
