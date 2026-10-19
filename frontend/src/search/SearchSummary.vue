<script setup lang="ts">
// Above a search's results: what was searched for, as chips that each take
// their part out of the search (a person, the description), then why the
// results may be fewer than asked for (photos not yet understood, the AI
// unavailable, a blocked name), and how many matched.
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { icons } from '../design/icons'
import type { SearchInfo } from '../library/store'
import { usePeople } from '../people/store'
import FaceThumb from '../people/FaceThumb.vue'
import Chip from '../shared/Chip.vue'
import { runCommand } from '../shared/commands'
import { t } from '../shared/i18n'
import Icon from '../shared/Icon.vue'

const props = defineProps<{
  /** What the search found beyond its items; none while it first runs. */
  info: SearchInfo | null
  /** How many results are shown. */
  total: number
  /** Whether the search is still at work; Cancel keeps what is shown. */
  searching: boolean
}>()
const emit = defineEmits<{ cancel: [] }>()

const route = useRoute()
const router = useRouter()
const people = usePeople()
void people.load()
const info = computed(
  (): SearchInfo =>
    props.info ?? { people: [], description: '', pending: 0, complete: true, noAI: false, blocked: false },
)
const named = computed(() => info.value.people.map((p) => ({ ...p, cover: people.byId.get(p.id)?.cover })))
const shown = computed(
  () =>
    named.value.length > 0 ||
    !!info.value.description ||
    props.searching ||
    info.value.pending > 0 ||
    info.value.blocked ||
    props.total > 0,
)

/** Searches again without one part: a person (by id), or the description. */
function without(person?: number) {
  const q = [
    ...info.value.people.filter((p) => p.id !== person).map((p) => p.name),
    person === undefined ? '' : info.value.description,
  ]
    .filter(Boolean)
    .join(' ')
  void router.push({ name: 'search', query: { ...route.query, q: q || undefined } })
}
</script>

<template>
  <div v-if="shown" class="search-summary" data-search-summary>
    <div
      v-if="named.length || info.description"
      class="parts"
      role="group"
      :aria-label="t('search.parts')"
      data-search-parts
    >
      <Chip
        v-for="p in named"
        :key="p.id"
        class="part"
        :aria-label="t('search.removePerson', { name: p.name })"
        data-search-part="person"
        @click="without(p.id)"
      >
        <FaceThumb v-if="p.cover && p.cover.id" :face="p.cover" :size="20" round />
        <Icon v-else name="people" :size="14" />
        <span class="name">{{ p.name }}</span
        ><span class="kind">{{ t('search.person') }}</span>
        <Icon name="close" :size="14" />
      </Chip>
      <Chip
        v-if="info.description"
        class="part"
        :aria-label="t('search.removeDescription', { description: info.description })"
        data-search-part="description"
        @click="without()"
      >
        <Icon name="search" :size="14" />
        <span class="name">{{ info.description }}</span
        ><span class="kind">{{ t('search.description') }}</span>
        <Icon name="close" :size="14" />
      </Chip>
    </div>

    <div v-if="searching" class="searching" role="status" data-search-searching>
      <span class="lumen-light-dot" aria-hidden="true" />
      <span>{{ t('search.searching') }}</span>
      <span class="hint">{{ t('library.modelLoading') }}</span>
      <UButton color="neutral" variant="ghost" @click="emit('cancel')">{{ t('search.cancel') }}</UButton>
    </div>

    <div v-if="info.pending" class="pending" data-search-pending>
      <span class="lumen-light-dot" aria-hidden="true" />
      <span class="text">{{ t('search.pending', { n: info.pending }) }}</span>
      <UButton
        color="neutral"
        variant="ghost"
        :trailing-icon="icons['arrow-right']"
        @click="void runCommand('app.activity')"
        >{{ t('search.viewActivity') }}</UButton
      >
    </div>
    <UAlert
      v-if="info.noAI && total"
      :icon="icons.warning"
      :title="t('search.onlyPeopleTitle')"
      :description="t('search.onlyPeople')"
      color="warning"
      variant="subtle"
      orientation="horizontal"
      data-search-no-ai
    >
      <template #actions>
        <UButton color="neutral" variant="outline" to="/settings?section=search">{{ t('search.reviewAI') }}</UButton>
      </template>
    </UAlert>
    <UAlert
      v-if="info.blocked"
      :icon="icons.lock"
      :title="t('search.blocked')"
      color="neutral"
      variant="subtle"
      orientation="horizontal"
      data-search-blocked
    >
      <template #actions>
        <UButton color="neutral" variant="outline" to="/settings/blocked" :trailing-icon="icons['arrow-right']">{{
          t('search.openBlocked')
        }}</UButton>
      </template>
    </UAlert>

    <p v-if="total && !searching" class="count" aria-live="polite" data-search-count>
      <strong>{{ t('search.results', { n: total }) }}</strong>
      <span v-if="!info.complete">{{ t('search.more', { n: total }) }}</span>
    </p>
  </div>
</template>

<style scoped>
.search-summary {
  display: grid;
  gap: 0.75rem;
  padding: 0 var(--lumen-page-x) 0.5rem;
}
.parts {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
}
.part {
  height: auto;
  padding-left: 0.375rem;
  white-space: normal;
  overflow-wrap: anywhere;
}
.part .name {
  color: var(--ui-text-highlighted);
}
.kind,
.hint {
  color: var(--ui-text-dimmed);
  font-size: 0.75rem;
}
.searching,
.pending {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  font-size: 0.8125rem;
}
.searching > :last-child {
  margin-left: auto;
}
.pending {
  padding: 0.5rem 0.5rem 0.5rem 1rem;
  border-radius: var(--lumen-r-lg);
  background: var(--lumen-light-soft);
  color: var(--ui-text-highlighted);
}
.pending .text {
  flex: 1;
}
.count {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: 0.5rem;
  margin: 0;
  color: var(--ui-text-dimmed);
  font-size: 0.75rem;
  font-variant-numeric: tabular-nums;
}
.count strong {
  color: var(--ui-text-highlighted);
  font-size: 0.8125rem;
  font-weight: 600;
}
@media (forced-colors: active) {
  .pending {
    border: 1px solid CanvasText;
  }
}
</style>
