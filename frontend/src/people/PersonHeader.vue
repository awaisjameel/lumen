<script setup lang="ts">
import UButton from '@nuxt/ui/components/Button.vue'
import UDropdownMenu from '@nuxt/ui/components/DropdownMenu.vue'
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { RouterLink, useRouter } from 'vue-router'
import { BlockKind, engine, ErrorCode, errorCode, Filter } from '../bridge'
import type { Person } from '../bridge'
import { icons } from '../design/icons'
import { block, onBlocksChanged } from '../shared/block'
import CollectionHero from '../shared/CollectionHero.vue'
import { useCommands } from '../shared/commands'
import { errorText, logError } from '../shared/errors'
import Icon from '../shared/Icon.vue'
import IconButton from '../shared/IconButton.vue'
import { formatNumber, t } from '../shared/i18n'
import { useLiveReload } from '../shared/liveReload'
import { notify } from '../shared/notify'
import { useRename } from '../shared/rename'
import FaceThumb from './FaceThumb.vue'
import { showNameSaved } from './naming'
import PersonNameEditor from './PersonNameEditor.vue'
import { usePeople } from './store'

const props = defineProps<{ id: number; reviewing?: boolean; faceCount?: number }>()
const emit = defineEmits<{ loaded: [person: Person] }>()
const router = useRouter()
const people = usePeople()
const person = ref<Person | null>(null)
const dates = ref('')
const rename = useRename<true>()
const { editing } = rename
const error = ref('')
const loading = ref(false)
let request = 0
let alive = true
onBeforeUnmount(() => {
  alive = false
  request++
})
async function load() {
  const asked = ++request
  const id = props.id
  loading.value = true
  try {
    const got = await engine.person(id)
    if (asked !== request) return
    person.value = got
    emit('loaded', got)
    error.value = ''
    // Dates come from the same visible timeline as the person's photos.
    const days = await engine.timeline({ filter: Filter.FilterAll, person: id, tag: 0, album: 0, place: 0, event: 0 })
    if (asked !== request) return
    const years = days
      .map((d) => d.day.slice(0, 4))
      .filter((year) => /^\d{4}$/.test(year))
      .sort()
    const first = years[0],
      last = years.at(-1)
    dates.value = first && last ? (first === last ? first : `${first}–${last}`) : ''
  } catch (e) {
    if (asked !== request) return
    if (errorCode(e) === ErrorCode.NotFound) person.value = null
    error.value = errorText(e)
  } finally {
    if (asked === request) loading.value = false
  }
}
watch(
  () => props.id,
  () => {
    person.value = null
    dates.value = ''
    error.value = ''
    void rename.close(false)
    void load()
  },
  { immediate: true },
)
useLiveReload(load, (c) => c.people || c.assets || c.thumbs)
onBeforeUnmount(
  onBlocksChanged(async () => {
    person.value = null
    dates.value = ''
    await load()
  }),
)
const name = computed(() => person.value?.name || t('person.unnamed'))
const meta = computed(
  () => `${t('person.photos', { n: person.value?.count ?? 0 })}${dates.value ? ` · ${dates.value}` : ''}`,
)
const review = () => router.push({ name: 'personFaces', params: { id: props.id } })
function edit() {
  if (person.value) void rename.open(true, person.value.name)
}
async function saved(shown: number, name: string) {
  const previous = person.value
  await rename.close()
  if (!previous) return
  if (shown === previous.id)
    showNameSaved(previous.id, previous.name, name, async () => {
      await Promise.all([load(), people.load()])
    })
  else notify({ id: 'people.merged', kind: 'success', title: t('person.merged', { name }) })
  void people.load().catch(logError)
  if (!alive || props.id !== previous.id) return
  if (shown !== props.id) await router.replace({ name: 'person', params: { id: shown } })
  else await load()
}
async function blockPerson(hide = false) {
  if (!person.value) return
  const id = props.id
  if (await block(BlockKind.BlockPerson, id, name.value, true, '', !hide)) {
    // Remove cached identity before navigating, including its portrait.
    if (!alive || id !== props.id) return
    person.value = null
    dates.value = ''
    await router.push({ name: 'people' })
  }
}
const actions = computed(() => [
  { label: t('person.rename'), icon: icons.pencil, onSelect: edit },
  {
    label: t('person.changePicture'),
    icon: icons.face,
    to: { name: 'personFaces', params: { id: props.id }, query: { picture: '1' } },
  },
  { label: t('person.review'), icon: icons.face, to: { name: 'personFaces', params: { id: props.id } } },
  { label: t('person.mergeWith'), icon: icons.merge, onSelect: edit },
  { label: t('people.hide'), icon: icons['eye-off'], onSelect: () => blockPerson(true) },
  { label: t('people.block'), icon: icons.blocked, color: 'error' as const, onSelect: () => blockPerson() },
])
useCommands([
  {
    id: 'person.rename',
    group: 'view',
    title: () => t('person.rename'),
    icon: 'pencil',
    available: () => person.value !== null,
    enabled: () => !editing.value,
    run: edit,
  },
  {
    id: 'person.review',
    group: 'view',
    title: () => t('person.review'),
    icon: 'face',
    available: () => person.value !== null,
    run: review,
  },
  {
    id: 'person.block',
    group: 'view',
    title: () => t('people.block'),
    icon: 'blocked',
    available: () => person.value !== null,
    run: () => blockPerson(),
  },
])
</script>

<template>
  <div class="person-header">
    <CollectionHero v-if="person" :kind="t('person.kind')" :name="name" :meta="meta" round>
      <template #cover><FaceThumb :face="person.cover" :size="128" fill round /></template>
      <template #name>
        <PersonNameEditor v-if="editing" :key="person.id" :person="person" @cancel="rename.close()" @saved="saved" />
        <h2 v-else class="heading">
          <UButton
            :ref="rename.triggerRef(true)"
            class="name"
            data-action="rename"
            color="neutral"
            variant="ghost"
            :title="t('person.rename')"
            @click="edit"
            ><bdi class="name-text" :title="name">{{ name }}</bdi
            ><Icon name="pencil" :size="16"
          /></UButton>
        </h2>
      </template>
      <template #actions>
        <UButton
          v-if="!reviewing"
          color="primary"
          variant="solid"
          :to="{ name: 'personFaces', params: { id } }"
          :icon="icons.face"
          >{{ t('person.review') }}</UButton
        >
        <UDropdownMenu :items="actions"
          ><IconButton
            :icon="icons.more"
            :label="t('person.actions')"
            color="neutral"
            variant="ghost"
            :disabled="editing !== null"
        /></UDropdownMenu>
      </template>
    </CollectionHero>
    <nav v-if="person" class="person-tabs" :aria-label="t('person.view')">
      <RouterLink
        :to="{ name: 'person', params: { id } }"
        class="tab"
        :class="{ active: !reviewing }"
        :aria-current="!reviewing ? 'page' : undefined"
        >{{ t('person.photosTab') }}<span>{{ formatNumber(person.count) }}</span></RouterLink
      >
      <RouterLink
        :to="{ name: 'personFaces', params: { id } }"
        class="tab"
        :class="{ active: reviewing }"
        :aria-current="reviewing ? 'page' : undefined"
        >{{ t('person.facesTab') }}<span v-if="faceCount !== undefined">{{ formatNumber(faceCount) }}</span></RouterLink
      >
    </nav>
    <div v-else-if="!error" class="hero-skeleton" role="status" :aria-label="t('person.loading')">
      <span class="lumen-skeleton portrait" /><span class="lumen-skeleton name-line" /><span class="sr-only">{{
        t('person.loading')
      }}</span>
    </div>
    <div v-if="error" class="load-error" role="alert">
      <span>{{ error }}</span
      ><UButton :disabled="loading" color="neutral" variant="outline" @click="load">{{ t('common.retry') }}</UButton>
    </div>
  </div>
</template>

<style scoped>
.person-tabs {
  display: flex;
  gap: 1rem;
  margin: 1rem var(--lumen-page-x) 0;
  border-bottom: 1px solid var(--ui-border);
}
.tab {
  display: inline-flex;
  align-items: center;
  gap: 0.5rem;
  min-height: 2.5rem;
  padding: 0.5rem 0.75rem;
  color: var(--ui-text-toned);
  font-size: 0.8125rem;
  border-bottom: 2px solid transparent;
}
.tab.active {
  border-bottom-color: var(--ui-primary);
  color: var(--ui-text-highlighted);
}
.tab span {
  color: var(--ui-text-dimmed);
  font-size: 0.75rem;
  font-variant-numeric: tabular-nums;
}
.heading {
  margin: 0;
  min-width: 0;
}
.name {
  max-width: 100%;
  min-width: 0;
  padding: 0;
  color: var(--ui-text-highlighted);
  font: 600 2.25rem/1.2 var(--font-display);
  letter-spacing: -0.02em;
  text-align: left;
}
.name-text {
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}
.name :deep(svg) {
  flex: none;
  color: var(--ui-text-dimmed);
}
.hero-skeleton {
  display: flex;
  align-items: center;
  gap: 1.5rem;
  padding: 1rem var(--lumen-page-x);
}
.hero-skeleton .portrait {
  width: 8rem;
  height: 8rem;
  border-radius: 50%;
}
.name-line {
  width: min(16rem, 50%);
  height: 2rem;
}
.load-error {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 0.75rem;
  padding: 0.5rem var(--lumen-page-x);
  font-size: 0.8125rem;
  color: var(--ui-error);
}
@container (max-width: 47.5rem) {
  .name {
    font-size: 1.875rem;
  }
  .person-header :deep(.round .cover) {
    display: block;
    width: 4rem;
    height: 4rem;
  }
}
@media (max-height: 600px) {
  .person-header :deep(.round .cover) {
    display: block;
    width: 4rem;
    height: 4rem;
  }
}
</style>
