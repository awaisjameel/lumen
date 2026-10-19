<script setup lang="ts">
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import USelect from '@nuxt/ui/components/Select.vue'
import { computed, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
import { BlockKind, engine, MAX_NAME_LEN, TagKind } from '../bridge'
import type { Tag } from '../bridge'
import { icons } from '../design/icons'
import CardGrid from '../shared/CardGrid.vue'
import Icon from '../shared/Icon.vue'
import Page from '../shared/Page.vue'
import PageHeader from '../shared/PageHeader.vue'
import ViewBar from '../shared/ViewBar.vue'
import FeatureState from '../shared/FeatureState.vue'
import FilterField from '../shared/FilterField.vue'
import IconButton from '../shared/IconButton.vue'
import { capsOf, runCommand, useCommands } from '../shared/commands'
import { confirm } from '../shared/confirm'
import { block } from '../shared/block'
import { errorText, logError } from '../shared/errors'
import { formatNumber, t } from '../shared/i18n'
import { useLiveReload } from '../shared/liveReload'
import { useList } from '../shared/listStore'
import { compareNames, nameIncludes, nameTooLong, trimName } from '../shared/names'
import { notify } from '../shared/notify'
import WorkProgress from '../shared/WorkProgress.vue'
import { useLibrary } from '../library/store'
import ThingCard from './ThingCard.vue'
import { useThings } from './store'

const lib = useLibrary()
const things = useThings()
const albums = useList(engine.albums)
const name = ref('')
const query = ref('')
const sort = ref('count')
const busy = ref(false)
const error = ref('')
const failedRename = ref(0)
const editingId = ref(0)
const seeking = ref('')
const created = shallowRef<{ id: number; name: string } | null>(null)
const working = ref(new Set<number>())
let retry: (() => Promise<unknown>) | undefined
let active = true
onBeforeUnmount(() => (active = false))

const all = computed(() => things.list.filter((x) => x.kind !== TagKind.TagBuiltin || x.count > 0))
const hadThings = ref(false)
watch(
  all,
  (items) => {
    if (items.length) hadThings.value = true
  },
  { immediate: true },
)
const listed = computed(() =>
  all.value
    .filter((x) => [x.name, x.original].some((n) => nameIncludes(n, trimName(query.value))))
    .sort((a, b) => (sort.value === 'count' ? b.count - a.count : 0) || compareNames(a.name, b.name)),
)
const groups = computed(() =>
  [
    { key: 'pinned', title: t('things.pinnedHeading'), items: listed.value.filter((x) => x.pinned) },
    {
      key: 'mine',
      title: t('things.yours'),
      items: listed.value.filter((x) => !x.pinned && x.kind !== TagKind.TagBuiltin),
    },
    {
      key: 'builtin',
      title: t('things.fromLumen'),
      items: listed.value.filter((x) => !x.pinned && x.kind === TagKind.TagBuiltin),
    },
  ].filter((g) => g.items.length),
)
const aiError = computed(() => lib.progress?.ai.error ?? '')
const understanding = computed(() => lib.progress?.embedding ?? 0)
const found = computed(() => things.list.find((x) => x.id === created.value?.id))
const pageError = computed(() => (failedRename.value === editingId.value && editingId.value ? '' : error.value))
const findReason = computed(() =>
  aiError.value
    ? t('things.aiUnavailable')
    : busy.value
      ? t('things.finding')
      : nameTooLong(name.value)
        ? t('error.invalidName', { max: MAX_NAME_LEN })
        : !trimName(name.value)
          ? t('things.enterPrompt')
          : undefined,
)
const usedByAlbum = computed(() => new Set(albums.list.value.filter((a) => a.rule.tag).map((a) => a.rule.tag)))

onMounted(() => void things.load().catch(logError))
useLiveReload(things.load, (c) => c.tags || c.assets || c.thumbs)

async function reload() {
  error.value = ''
  try {
    await things.load()
  } catch (e) {
    error.value = errorText(e)
  }
}
async function tryAgain() {
  if (retry) await retry()
  else await reload()
}
async function create() {
  const n = trimName(name.value)
  if (!n || nameTooLong(n) || busy.value || aiError.value) return
  busy.value = true
  error.value = ''
  failedRename.value = 0
  seeking.value = n
  created.value = null
  retry = create
  try {
    const id = await engine.createLabel(n)
    if (!active) return
    created.value = { id, name: n }
    name.value = ''
    query.value = ''
    retry = reload
    await things.load()
    notify({ id: 'things.created', kind: 'success', title: t('things.created', { name: n }) })
  } catch (e) {
    if (active) error.value = errorText(e)
  } finally {
    busy.value = false
  }
}

async function change(
  id: number,
  write: () => Promise<void>,
  title: string,
  undo?: () => Promise<void>,
  inline = false,
): Promise<boolean> {
  if (working.value.has(id)) return false
  working.value = new Set([...working.value, id])
  error.value = ''
  failedRename.value = 0
  let saved = false
  retry = () => change(id, write, title, undo, inline)
  try {
    await write()
    saved = true
    retry = reload
    await things.load()
  } catch (e) {
    error.value = errorText(e)
    failedRename.value = inline && !saved ? id : 0
  } finally {
    working.value = new Set([...working.value].filter((x) => x !== id))
    if (saved)
      notify({
        id: `things.change.${id}`,
        kind: 'success',
        title,
        actions: undo
          ? [{ label: t('blocked.undo'), run: () => void change(id, undo, t('things.restored')) }]
          : undefined,
      })
  }
  return saved
}
function pin(tag: Tag) {
  return change(
    tag.id,
    () => engine.setThingPinned(tag.id, !tag.pinned),
    t(tag.pinned ? 'things.unpinnedNotice' : 'things.pinnedNotice', { name: tag.name }),
    () => engine.setThingPinned(tag.id, tag.pinned),
  )
}
async function rename(id: number, value: string): Promise<boolean> {
  const previous = things.byId.get(id)
  if (!previous) return false
  return change(
    id,
    () => engine.renameThing(id, trimName(value)),
    t(trimName(value) ? 'things.renamed' : 'things.resetNotice'),
    () => engine.renameThing(id, previous.name),
    true,
  )
}
function editingChanged(id: number, open: boolean) {
  if (open) editingId.value = id
  else {
    if (editingId.value === id) editingId.value = 0
    if (failedRename.value === id) error.value = ''
  }
}
const blockTag = (tag: Tag) => void block(BlockKind.BlockTag, tag.id, tag.name)
async function remove(tag: Tag) {
  if (
    await confirm(
      t('things.deleteTitle', { name: tag.name }),
      t('things.deleteText'),
      t('things.deleteNamed', { name: tag.name }),
      { danger: true },
    )
  ) {
    await change(tag.id, () => engine.deleteTag(tag.id), t('things.deleted', { name: tag.name }))
  }
}
useCommands([
  {
    id: 'things.create',
    group: 'library',
    title: () => t('things.find'),
    icon: 'plus',
    enabled: () => !findReason.value,
    reason: () => findReason.value,
    run: create,
  },
])
</script>

<template>
  <Page>
    <template #header>
      <PageHeader
        :title="t('nav.things')"
        :subtitle="t(all.length === 1 ? 'things.captionOne' : 'things.caption', { n: formatNumber(all.length) })"
      >
        <UButton to="/settings?section=search" variant="ghost" color="neutral" :icon="icons.sliders">{{
          t('settings.confidence')
        }}</UButton>
        <!-- The page filters its own Things; searching photos goes through the palette. -->
        <IconButton
          :icon="icons.search"
          :label="t('shell.search')"
          :kbds="capsOf('app.commandPalette')"
          variant="ghost"
          color="neutral"
          @click="runCommand('app.commandPalette')"
        />
      </PageHeader>
    </template>
    <template #toolbar>
      <ViewBar :label="t('things.search')">
        <FilterField v-model="query" :label="t('things.search')" class="min-w-0 max-w-96 flex-1" />
        <span class="shown" aria-live="polite">{{ t('things.shown', { n: formatNumber(listed.length) }) }}</span>
        <template #end
          ><USelect
            v-model="sort"
            :aria-label="t('things.sort')"
            :items="[
              { value: 'count', label: t('things.mostPhotos') },
              { value: 'az', label: t('things.az') },
            ]"
        /></template>
      </ViewBar>
    </template>
    <div class="things-content">
      <UAlert
        v-if="aiError"
        color="warning"
        variant="subtle"
        :title="t('things.aiUnavailable')"
        :description="t('things.aiHint')"
        :actions="[{ label: t('things.aiSettings'), to: '/settings?section=search' }]"
      />
      <section class="teach" :aria-label="t('things.teach')">
        <span class="ai-mark"><Icon name="smart" :size="28" /></span>
        <div class="teach-copy">
          <h2>{{ t('things.teach') }}</h2>
          <p>{{ t('things.intro') }}</p>
        </div>
        <form class="new" data-create-thing @submit.prevent="create">
          <label for="new-thing">{{ t('things.promptLabel') }}</label>
          <div class="flex min-w-0 gap-2">
            <UInput
              id="new-thing"
              v-model="name"
              size="xl"
              class="min-w-0 flex-1"
              :placeholder="t('things.new')"
              :aria-label="t('things.new')"
              aria-describedby="new-thing-limit"
              :disabled="busy || !!aiError"
              :aria-invalid="nameTooLong(name) || undefined"
            />
            <UButton
              type="submit"
              color="primary"
              variant="solid"
              size="xl"
              :loading="busy"
              :disabled="!!findReason"
              :title="findReason"
              >{{ busy ? t('things.finding') : t('things.find') }}</UButton
            >
          </div>
          <p id="new-thing-limit">{{ t('things.promptHint', { max: MAX_NAME_LEN }) }}</p>
        </form>
        <div v-if="busy" class="activity" data-thing-progress role="status">
          <span class="lumen-light-dot" /><WorkProgress
            :label="t('things.lookingFor', { name: seeking })"
            indeterminate
            class="flex-1"
          />
        </div>
        <div v-if="created" class="result" role="status" :data-created-thing="created.id">
          <span
            >{{
              found
                ? t('things.found', { n: formatNumber(found.count), name: created.name })
                : t('things.created', { name: created.name })
            }}
            {{ found && !found.count ? t('things.viewEmptyHint') : '' }}</span
          >
          <UButton :to="{ name: 'thing', params: { id: created.id } }" variant="ghost">{{
            t('things.openPhotos')
          }}</UButton>
        </div>
      </section>
      <UAlert
        v-if="nameTooLong(name)"
        role="alert"
        color="error"
        variant="subtle"
        :title="t('error.invalidName', { max: MAX_NAME_LEN })"
      />
      <UAlert
        v-if="(!things.error && pageError) || (things.loaded && things.error)"
        role="alert"
        color="warning"
        variant="subtle"
        :title="pageError || things.error"
        ><template #actions
          ><UButton variant="outline" color="neutral" size="sm" @click="tryAgain">{{
            t('common.retry')
          }}</UButton></template
        ></UAlert
      >
      <FeatureState v-if="things.error && !things.loaded" kind="error" :title="things.error"
        ><UButton @click="tryAgain">{{ t('common.retry') }}</UButton></FeatureState
      >
      <section v-else-if="!things.loaded" :aria-label="t('things.loading')" aria-busy="true">
        <p role="status">{{ t('things.loading') }}</p>
        <CardGrid><div v-for="n in 4" :key="n" class="lumen-skeleton skeleton" /></CardGrid>
      </section>
      <section v-for="group in groups" :key="group.key" :data-group="group.key">
        <div class="section-head">
          <h2>{{ group.title }}</h2>
          <span>{{
            t(group.items.length === 1 ? 'things.groupCountOne' : 'things.groupCount', {
              n: formatNumber(group.items.length),
            })
          }}</span>
        </div>
        <CardGrid>
          <ThingCard
            v-for="x in group.items"
            :key="x.id"
            :tag="x"
            :icon="x.kind === TagKind.TagBuiltin ? 'things' : 'tag'"
            :deletable="x.kind !== TagKind.TagBuiltin"
            :in-album="usedByAlbum.has(x.id)"
            :working="working.has(x.id)"
            :fresh="created?.id === x.id"
            :error="failedRename === x.id ? error : ''"
            :rename="rename"
            @pin="pin(x)"
            @block="blockTag(x)"
            @remove="remove(x)"
            @editing="editingChanged(x.id, $event)"
          />
        </CardGrid>
      </section>
      <FeatureState
        v-if="things.loaded && !listed.length && !error && !things.error"
        kind="empty"
        icon="things"
        :title="query ? t('things.noMatches') : hadThings ? t('things.cleared') : t('things.empty')"
        :text="
          query
            ? t('things.noMatchesHint')
            : hadThings
              ? t('things.clearedHint')
              : understanding
                ? t('things.emptyWorking', { n: formatNumber(understanding) })
                : t('things.emptyHint')
        "
      >
        <UButton v-if="query" variant="outline" @click="query = ''">{{ t('things.clearFilter') }}</UButton>
        <template v-else
          ><UButton to="/settings?section=library">{{ t('things.addFolder') }}</UButton
          ><UButton to="/blocked" variant="ghost">{{ t('things.manageBlocked') }}</UButton></template
        >
      </FeatureState>
      <p class="confidence">
        {{ t('things.confidenceHint') }}
        <UButton to="/settings?section=search" variant="ghost" size="sm">{{ t('settings.confidence') }}</UButton>
      </p>
    </div>
  </Page>
</template>

<style scoped>
.things-content {
  --lumen-card-min: 11.75rem;
  display: flex;
  flex-direction: column;
  gap: 1.5rem;
}
.teach {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 1.25rem;
  padding: 1.25rem;
  border: 1px solid var(--ui-border);
  border-radius: var(--lumen-r-lg);
  background: var(--lumen-bg-raised);
  box-shadow: var(--lumen-shadow-1);
}
.ai-mark {
  display: grid;
  place-items: center;
  flex: none;
  width: 3rem;
  height: 3rem;
  border-radius: var(--lumen-r-lg);
  background: var(--lumen-light-soft);
  color: var(--lumen-light-text);
}
.teach-copy {
  flex: 1 1 30%;
  min-width: 0;
}
h2 {
  margin: 0;
  color: var(--ui-text-highlighted);
  font-weight: 600;
}
.teach h2 {
  font: 600 1.125rem/1.3 var(--font-display);
}
p {
  color: var(--ui-text-toned);
  font-size: 0.8125rem;
}
.new {
  display: grid;
  flex: 1 1 40%;
  min-width: 0;
  gap: 0.375rem;
}
.new label {
  font-size: 0.8125rem;
  font-weight: 600;
}
.new p {
  color: var(--ui-text-dimmed);
  font-size: 0.75rem;
}
.activity,
.result {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.5rem;
  flex-basis: 100%;
}
.result {
  padding: 0.75rem;
  border-radius: var(--lumen-r-md);
  background: var(--lumen-light-soft);
  font-size: 0.8125rem;
}
.section-head {
  display: flex;
  align-items: baseline;
  gap: 0.75rem;
  margin-bottom: 0.75rem;
}
.section-head h2 {
  font-size: 0.9375rem;
}
.section-head span,
.shown {
  color: var(--ui-text-dimmed);
  font-size: 0.75rem;
  font-variant-numeric: tabular-nums;
}
.skeleton {
  aspect-ratio: 4 / 3;
  border-radius: var(--lumen-r-lg);
}
.confidence {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 0.25rem;
}
@container (max-width: 700px) {
  .shown {
    display: none;
  }
  .teach-copy {
    flex-basis: calc(100% - 5rem);
  }
  .new {
    flex-basis: 100%;
  }
}
</style>
