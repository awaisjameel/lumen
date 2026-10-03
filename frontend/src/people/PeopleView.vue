<script setup lang="ts">
// Naming someone once confirms their identity; grouping remains the engine's.
import UButton from '@nuxt/ui/components/Button.vue'
import UDropdownMenu from '@nuxt/ui/components/DropdownMenu.vue'
import type { DropdownMenuItem } from '@nuxt/ui/components/DropdownMenu.vue'
import { computed, onMounted, watch } from 'vue'
import { BlockKind } from '../bridge'
import type { Person } from '../bridge'
import { icons } from '../design/icons'
import { block } from '../shared/block'
import CardGrid from '../shared/CardGrid.vue'
import CollectionCard from '../shared/CollectionCard.vue'
import { registerCommands, useCommands } from '../shared/commands'
import type { Command } from '../shared/commands'
import { logError } from '../shared/errors'
import FeatureState from '../shared/FeatureState.vue'
import { t } from '../shared/i18n'
import { useLiveReload } from '../shared/liveReload'
import Page from '../shared/Page.vue'
import PageHeader from '../shared/PageHeader.vue'
import { useLibrary } from '../library/store'
import SearchBox from '../search/SearchBox.vue'
import FaceThumb from './FaceThumb.vue'
import { usePeople } from './store'

const lib = useLibrary()
const people = usePeople()
const named = computed(() => people.list.filter((p) => p.name))
const unnamed = computed(() => people.list.filter((p) => !p.name))
const facesError = computed(() => lib.progress?.ai.facesError ?? '')
const looking = computed(() => lib.progress?.faces ?? 0)

onMounted(() => void people.load().catch(logError))
useLiveReload(people.load, (c) => c.people || c.assets || c.thumbs)

function blockPerson(p: Person) {
  void block(BlockKind.BlockPerson, p.id, p.name || t('person.unnamed'))
}

const cardCommands = computed<Command[]>(() =>
  people.list.map((p) => ({
    id: `person.${p.id}.block`,
    group: 'view',
    title: () => t('blocked.confirmTitle', { name: p.name || t('person.unnamed') }),
    icon: 'eye-off',
    run: () => blockPerson(p),
  })),
)
watch(cardCommands, (entries, _previous, cleanup) => cleanup(registerCommands(entries)), { immediate: true })
function personItems(p: Person): DropdownMenuItem[] {
  return cardCommands.value
    .filter((c) => c.id === `person.${p.id}.block`)
    .map((c) => ({
      label: c.title(),
      icon: c.icon ? icons[c.icon] : undefined,
      onSelect: () => {
        void c.run()
      },
    }))
}
useCommands([
  {
    id: 'people.refresh',
    group: 'view',
    title: () => t('people.refresh'),
    icon: 'refresh',
    enabled: () => !people.loading,
    run: () => people.load().catch(logError),
  },
])
</script>

<template>
  <Page>
    <template #header
      ><PageHeader :title="t('nav.people')"><SearchBox /></PageHeader
    ></template>
    <p class="mb-4 text-muted">{{ t('people.intro') }}</p>
    <FeatureState
      v-if="people.error || facesError"
      kind="error"
      :title="people.error || t('people.unavailable', { error: facesError })"
    />
    <FeatureState v-else-if="!people.loaded" kind="loading" :title="t('people.loading')" />
    <template v-for="group in [named, unnamed]" :key="group === named ? 'named' : 'unnamed'">
      <template v-if="group.length">
        <h2 class="mb-3 mt-5 text-sm font-semibold">
          {{ group === named ? t('people.named') : t('people.notNamed') }}
        </h2>
        <CardGrid>
          <CollectionCard
            v-for="p in group"
            :key="p.id"
            :to="{ name: 'person', params: { id: p.id } }"
            :title="p.name || t('person.addName')"
            :count="t('status.items', { n: p.count })"
            icon="people"
            round
          >
            <template #cover><FaceThumb :face="p.cover" :size="128" round /></template>
            <UDropdownMenu :items="personItems(p)">
              <UButton
                :icon="icons.more"
                :aria-label="t('people.actions', { name: p.name || t('person.unnamed') })"
                color="neutral"
                variant="subtle"
                square
              />
            </UDropdownMenu>
          </CollectionCard>
        </CardGrid>
      </template>
    </template>
    <FeatureState
      v-if="people.loaded && !people.list.length && !people.error && !facesError"
      kind="empty"
      icon="people"
      :title="t('people.empty')"
      :text="looking ? t('people.emptyWorking', { n: looking }) : t('people.emptyHint')"
    />
  </Page>
</template>
