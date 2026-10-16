<script setup lang="ts">
import UButton from '@nuxt/ui/components/Button.vue'
import UDropdownMenu from '@nuxt/ui/components/DropdownMenu.vue'
import { computed, onMounted, ref } from 'vue'
import { RouterLink, useRouter } from 'vue-router'
import { BlockKind, engine } from '../bridge'
import type { Person } from '../bridge'
import { icons } from '../design/icons'
import { block } from '../shared/block'
import CardGrid from '../shared/CardGrid.vue'
import CollectionCard from '../shared/CollectionCard.vue'
import { useCommands, useItemCommands } from '../shared/commands'
import { logError } from '../shared/errors'
import FeatureState from '../shared/FeatureState.vue'
import Icon from '../shared/Icon.vue'
import IconButton from '../shared/IconButton.vue'
import { formatNumber, t } from '../shared/i18n'
import { useLiveReload } from '../shared/liveReload'
import { notify } from '../shared/notify'
import Page from '../shared/Page.vue'
import PageHeader from '../shared/PageHeader.vue'
import { useRename } from '../shared/rename'
import { useSuggestions } from '../shared/suggest'
import { useLibrary } from '../library/store'
import FilterField from '../shared/FilterField.vue'
import FaceThumb from './FaceThumb.vue'
import { showNameSaved } from './naming'
import PersonNameEditor from './PersonNameEditor.vue'
import { usePeople } from './store'

const lib = useLibrary()
const router = useRouter()
const people = usePeople()
const query = ref('')
const saving = ref(false)
const matches = useSuggestions<Person[]>(query, engine.suggestPeople, [])
const matching = computed(() => new Set(matches.value.map((p) => p.id)))
const named = computed(() => people.list.filter((p) => p.name && (!query.value.trim() || matching.value.has(p.id))))
const unnamed = computed(() => (query.value.trim() ? [] : people.list.filter((p) => !p.name)))
const namedLimit = ref(48)
const unnamedLimit = ref(6)
const facesError = computed(() => lib.progress?.ai.facesError ?? '')
const looking = computed(() => lib.progress?.faces ?? 0)
const rename = useRename<number>()
const { editing } = rename
const editingPerson = computed(() => people.list.find((p) => p.id === editing.value))

onMounted(() => void people.load().catch(logError))
useLiveReload(people.load, (c) => c.people || c.assets || c.thumbs)
const refresh = () => people.load().catch(logError)

async function blockPerson(p: Person, hide = false) {
  if (await block(BlockKind.BlockPerson, p.id, p.name || t('person.unnamed'), true, '', !hide)) await refresh()
}
async function namedPerson(p: Person, shown: number, name: string) {
  await rename.close()
  query.value = ''
  if (shown === p.id) showNameSaved(p.id, p.name, name, people.load)
  else notify({ id: 'people.merged', kind: 'success', title: t('person.merged', { name }) })
  await refresh()
}
const personItems = useItemCommands(
  () => people.list,
  (p) => p.id,
  (p) => [
    {
      id: `person.${p.id}.rename`,
      group: 'view',
      title: () => (p.name ? t('person.rename') : t('person.addName')),
      icon: 'pencil',
      enabled: () => !saving.value,
      run: () => rename.open(p.id, p.name),
    },
    {
      id: `person.${p.id}.review`,
      group: 'view',
      title: () => t('person.review'),
      icon: 'face',
      run: () => router.push({ name: 'personFaces', params: { id: p.id } }),
    },
    {
      id: `person.${p.id}.hide`,
      group: 'view',
      title: () => t('people.hide'),
      icon: 'eye-off',
      enabled: () => !saving.value,
      run: () => blockPerson(p, true),
    },
    {
      id: `person.${p.id}.block`,
      group: 'view',
      title: () => t('people.block'),
      icon: 'blocked',
      danger: true,
      enabled: () => !saving.value,
      run: () => blockPerson(p),
    },
  ],
)
useCommands([
  {
    id: 'people.refresh',
    group: 'view',
    title: () => t('people.refresh'),
    icon: 'refresh',
    enabled: () => !people.loading,
    reason: () => t('people.loading'),
    run: refresh,
  },
])
</script>

<template>
  <Page>
    <template #header>
      <PageHeader :title="t('nav.people')" :subtitle="t('people.count', { n: people.list.length })">
        <template #search
          ><fieldset class="w-full min-w-0" :disabled="saving">
            <FilterField v-model="query" :label="t('people.filter')" /></fieldset
        ></template>
        <span v-if="people.loaded && !facesError" class="discovery-status" role="status"
          ><span v-if="looking" class="lumen-light-dot" /><Icon v-else name="check" :size="16" />{{
            looking ? t('people.finding') : t('people.upToDate')
          }}</span
        >
        <IconButton
          :icon="icons.refresh"
          :label="t('people.refresh')"
          :tip="people.loading ? t('people.loading') : t('people.refresh')"
          color="neutral"
          variant="ghost"
          :disabled="people.loading"
          @click="refresh"
        />
      </PageHeader>
    </template>
    <p v-if="query.trim()" class="mb-4 text-xs text-dimmed" role="status">{{ t('people.filterHint') }}</p>
    <div v-if="looking && !facesError" class="activity" role="status">
      <Icon name="face" :size="20" /><span>{{ t('people.emptyWorking', { n: looking }) }}</span
      ><span
        v-if="lib.progress?.total"
        class="discovery-progress"
        role="progressbar"
        :aria-label="t('people.finding')"
        :aria-valuemin="0"
        :aria-valuemax="lib.progress.total"
        :aria-valuenow="Math.max(0, lib.progress.total - looking)"
        ><span :style="{ transform: `scaleX(${Math.max(0, 1 - looking / lib.progress.total)})` }"
      /></span>
    </div>
    <FeatureState
      v-if="people.error || facesError"
      kind="error"
      :title="people.error || t('people.unavailable', { error: facesError })"
      :text="facesError ? t('people.modelHint') : undefined"
    >
      <UButton v-if="facesError" to="/settings?section=search" color="neutral" variant="outline">{{
        t('nav.settings')
      }}</UButton>
      <UButton color="primary" variant="solid" :disabled="people.loading" @click="refresh">{{
        t('common.retry')
      }}</UButton>
    </FeatureState>
    <section v-if="!people.loaded && !people.error && !facesError" :aria-label="t('people.loading')" aria-busy="true">
      <h2 class="section-heading">{{ t('people.loading') }}</h2>
      <div class="people-grid skeletons" role="status">
        <div v-for="n in 8" :key="n" class="skeleton-card">
          <span class="lumen-skeleton portrait" /><span class="lumen-skeleton name-line" /><span
            class="lumen-skeleton count-line"
          />
        </div>
      </div>
    </section>
    <template v-if="people.loaded && people.list.length">
      <section v-if="named.length" :aria-label="t('people.named')" class="people-section">
        <div class="section-head">
          <h2 class="section-heading">{{ t('people.named') }}</h2>
          <span>{{ formatNumber(named.length) }}</span
          ><span class="section-note">{{ t('people.mostPhotos') }}</span>
        </div>
        <CardGrid class="people-grid">
          <CollectionCard
            v-for="p in named.slice(0, namedLimit)"
            :key="p.id"
            :data-person="p.id"
            :to="{ name: 'person', params: { id: p.id } }"
            :title="p.name"
            :count="t('person.photos', { n: p.count })"
            icon="people"
            round
          >
            <template #cover><FaceThumb :face="p.cover" :size="96" round /></template>
            <template #title
              ><UButton v-if="editing === p.id" color="neutral" variant="ghost" @click="rename.close()">{{
                t('common.cancel')
              }}</UButton
              ><RouterLink v-else :to="{ name: 'person', params: { id: p.id } }" :title="p.name" class="person-name"
                ><bdi>{{ p.name }}</bdi></RouterLink
              ></template
            >
            <UDropdownMenu :items="personItems(p.id)"
              ><IconButton
                :ref="rename.triggerRef(p.id)"
                :icon="icons.more"
                :label="t('people.actions', { name: p.name })"
                color="neutral"
                variant="subtle"
            /></UDropdownMenu>
          </CollectionCard>
        </CardGrid>
        <PersonNameEditor
          v-if="editingPerson?.name"
          :key="editingPerson.id"
          class="named-editor"
          :person="editingPerson"
          @busy="saving = $event"
          @cancel="rename.close()"
          @saved="(shown, name) => editingPerson && namedPerson(editingPerson, shown, name)"
        />
        <UButton v-if="named.length > namedLimit" color="neutral" variant="outline" @click="namedLimit += 48">{{
          t('people.more', { n: Math.min(48, named.length - namedLimit) })
        }}</UButton>
      </section>
      <section v-if="unnamed.length" :aria-label="t('people.notNamed')" class="people-section">
        <div class="section-head">
          <h2 class="section-heading">{{ t('people.notNamed') }}</h2>
          <span>{{ formatNumber(unnamed.length) }}</span
          ><span class="section-note">{{ t('people.mergeHint') }}</span>
        </div>
        <CardGrid class="people-grid unnamed-grid">
          <CollectionCard
            v-for="p in unnamed.slice(0, unnamedLimit)"
            :key="p.id"
            :data-person="p.id"
            :class="{ editing: editing === p.id }"
            :to="{ name: 'person', params: { id: p.id } }"
            :title="t('people.openPerson', { name: t('person.unnamed'), count: t('person.photos', { n: p.count }) })"
            :count="t('person.photos', { n: p.count })"
            icon="people"
            round
          >
            <template #cover><FaceThumb :face="p.cover" :size="96" round /></template>
            <template #title>
              <PersonNameEditor
                v-if="editing === p.id"
                :person="p"
                @busy="saving = $event"
                @cancel="rename.close()"
                @saved="(shown, name) => namedPerson(p, shown, name)"
              />
              <UButton
                v-else
                :ref="rename.triggerRef(p.id)"
                :icon="icons.plus"
                color="neutral"
                variant="ghost"
                data-action="name"
                :disabled="saving"
                @click="rename.open(p.id, '')"
                >{{ t('person.addName') }}</UButton
              >
            </template>
            <UDropdownMenu :items="personItems(p.id)"
              ><IconButton
                :icon="icons.more"
                :label="t('people.actions', { name: t('person.unnamed') })"
                color="neutral"
                variant="subtle"
            /></UDropdownMenu>
          </CollectionCard>
        </CardGrid>
        <UButton v-if="unnamed.length > unnamedLimit" color="neutral" variant="outline" @click="unnamedLimit += 24">{{
          t('people.more', { n: Math.min(24, unnamed.length - unnamedLimit) })
        }}</UButton>
      </section>
      <FeatureState
        v-if="query.trim() && !named.length"
        kind="empty"
        icon="search"
        :title="t('people.noResults', { query })"
        :text="t('people.noResultsHint')"
        ><UButton color="primary" variant="solid" @click="query = ''">{{
          t('people.clearFilter')
        }}</UButton></FeatureState
      >
      <p class="privacy"><Icon name="lock" :size="16" />{{ t('people.privacy') }}</p>
    </template>
    <FeatureState
      v-else-if="people.loaded && !people.error && !facesError"
      kind="empty"
      icon="people"
      :title="t('people.empty')"
      :text="t('people.emptyHint')"
    >
      <p class="w-full text-sm text-muted">{{ t('people.privacy') }}</p>
      <UButton to="/settings?section=library" color="primary" variant="solid">{{ t('people.addFolder') }}</UButton
      ><UButton color="neutral" variant="ghost" :disabled="people.loading" @click="refresh">{{
        t('people.refresh')
      }}</UButton>
    </FeatureState>
    <UButton
      class="mb-4"
      to="/settings/blocked"
      color="neutral"
      variant="ghost"
      :icon="icons['eye-off']"
      :title="t('people.hideHint')"
      >{{ t('people.manageHidden') }}</UButton
    >
  </Page>
</template>

<style scoped>
.discovery-status {
  display: inline-flex;
  align-items: center;
  gap: 0.5rem;
  font-size: 0.8125rem;
  color: var(--ui-text-dimmed);
}
.activity {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  margin: 1rem 0 2rem;
  padding: 0.75rem 1rem;
  border: 1px solid color-mix(in srgb, var(--lumen-light) 30%, var(--ui-border));
  border-radius: var(--lumen-r-md);
  background: var(--lumen-light-soft);
  color: var(--ui-text-toned);
  font-size: 0.8125rem;
}
.activity > span:first-of-type {
  flex: 1;
}
.activity > svg {
  color: var(--lumen-light);
}
.discovery-progress {
  flex: none;
  width: 8rem;
  height: 0.25rem;
  overflow: hidden;
  border-radius: var(--lumen-r-xs);
  background: var(--ui-border);
}
.discovery-progress > span {
  display: block;
  width: 100%;
  height: 100%;
  background: var(--lumen-light);
  transform-origin: left;
}
.people-section {
  margin-block: 1.5rem 3rem;
}
.section-head {
  display: flex;
  align-items: baseline;
  flex-wrap: wrap;
  gap: 0.75rem;
  margin-bottom: 1.5rem;
  color: var(--ui-text-dimmed);
  font-size: 0.75rem;
  font-variant-numeric: tabular-nums;
}
.section-heading {
  margin: 0;
  color: var(--ui-text-highlighted);
  font: 600 1.125rem/1.3 var(--font-display);
  text-wrap: balance;
}
.section-note {
  margin-left: auto;
}
.people-grid {
  grid-template-columns: repeat(auto-fit, minmax(6rem, 1fr));
  gap: 1.5rem;
  margin-bottom: 1.5rem;
}
.people-grid:has(> :only-child) {
  grid-template-columns: 6rem;
}
.unnamed-grid {
  grid-template-columns: repeat(auto-fit, minmax(8.75rem, 1fr));
}
.people-grid :deep(.cover) {
  width: 6rem;
  height: 6rem;
  margin-inline: auto;
}
.people-grid :deep(.meta) {
  min-height: 2.5rem;
}
.people-grid :deep(.acts) {
  top: 4rem;
  right: max(0rem, calc(50% - 3.5rem));
}
.people-grid .editing {
  grid-column: span 2;
}
.person-name {
  min-width: 1.5rem;
  display: block;
  overflow: hidden;
  max-width: 100%;
  min-height: 1.5rem;
  white-space: nowrap;
  text-overflow: ellipsis;
  font-size: 0.8125rem;
  font-weight: 600;
  color: var(--ui-text-highlighted);
}
.named-editor {
  margin: 1rem 0;
  padding: 1rem;
  background: var(--lumen-bg-sunken);
  border-radius: var(--lumen-r-md);
}
.privacy {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  margin-block: 3rem 1rem;
  font-size: 0.75rem;
  color: var(--ui-text-dimmed);
}
.skeleton-card {
  display: flex;
  align-items: center;
  flex-direction: column;
  gap: 0.75rem;
}
.portrait {
  width: 6rem;
  height: 6rem;
  border-radius: 50%;
}
.name-line {
  width: 4rem;
  height: 0.75rem;
}
.count-line {
  width: 3rem;
  height: 0.5rem;
}
.skeletons {
  margin-top: 1.5rem;
}
@container (max-width: 60rem) {
  .discovery-status {
    display: none;
  }
}
</style>
