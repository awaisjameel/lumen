<script setup lang="ts">
// People: everyone the AI found in the photos, as round faces with names.
// Naming someone once is how they are confirmed; people can be hidden.
import { computed, onMounted, ref } from 'vue'
import { engine } from '../bridge'
import type { Person } from '../bridge'
import { errorText } from '../shared/errors'
import { t } from '../shared/i18n'
import EmptyState from '../shared/EmptyState.vue'
import Icon from '../shared/Icon.vue'
import { useLiveReload } from '../shared/liveReload'
import { useLibrary } from '../library/store'
import SearchBox from '../search/SearchBox.vue'
import FaceThumb from './FaceThumb.vue'
import { usePeople } from './store'

const lib = useLibrary()
const people = usePeople()
const showHidden = ref(false)
const error = ref('')

const shown = computed(() => people.list.filter((p) => !p.hidden || showHidden.value))
const named = computed(() => shown.value.filter((p) => p.name))
const unnamed = computed(() => shown.value.filter((p) => !p.name))
const hiddenCount = computed(() => people.list.filter((p) => p.hidden).length)
const hiddenToggle = computed(() =>
  showHidden.value
    ? t('people.hideHidden')
    : `${t('people.showHidden')} (${t('people.hidden', { n: hiddenCount.value })})`,
)
const facesError = computed(() => lib.progress?.ai.facesError ?? '')
const looking = computed(() => lib.progress?.faces ?? 0)

onMounted(() => void people.load().catch((e: unknown) => (error.value = errorText(e))))
useLiveReload(people.load) // new people appear while faces are found

async function toggleHidden(p: Person) {
  error.value = ''
  try {
    await engine.setPersonHidden(p.id, !p.hidden)
    await people.load()
  } catch (e) {
    error.value = errorText(e)
  }
}
</script>

<template>
  <div class="page">
    <header class="topbar">
      <h1>{{ t('nav.people') }}</h1>
      <SearchBox />
      <div class="spacer" />
    </header>

    <div class="page-body">
      <p class="muted intro">{{ t('people.intro') }}</p>
      <p v-if="error || facesError" class="error" role="alert">
        {{ error || t('people.unavailable', { error: facesError }) }}
      </p>

      <template v-for="group in [named, unnamed]" :key="group === named ? 'named' : 'unnamed'">
        <template v-if="group.length">
          <h2>{{ group === named ? t('people.named') : t('people.notNamed') }}</h2>
          <div class="cards">
            <div v-for="p in group" :key="p.id" class="card" :class="{ hidden: p.hidden }">
              <RouterLink :to="{ name: 'person', params: { id: p.id } }" class="cover">
                <FaceThumb :face="p.cover" :size="128" round />
              </RouterLink>
              <span class="name" :class="{ muted: !p.name }">{{ p.name || t('person.addName') }}</span>
              <span class="count">{{ t('status.items', { n: p.count }) }}</span>
              <button
                class="icon-btn act"
                :title="p.hidden ? t('people.unhide') : t('people.hide')"
                @click="toggleHidden(p)"
              >
                <Icon :name="p.hidden ? 'eye' : 'eye-off'" :size="16" />
              </button>
            </div>
          </div>
        </template>
      </template>

      <EmptyState
        v-if="people.loaded && !shown.length"
        class="empty"
        icon="people"
        :title="t('people.empty')"
        :text="looking ? t('people.emptyWorking', { n: looking }) : t('people.emptyHint')"
      />

      <button v-if="hiddenCount" class="btn subtle" @click="showHidden = !showHidden">
        {{ hiddenToggle }}
      </button>
    </div>
  </div>
</template>

<style scoped>
.intro {
  margin: 0 0 8px;
}
.error {
  margin: 0 0 8px;
}
h2 {
  margin: 20px 0 12px;
  font-size: 14px;
  font-weight: 600;
}
.cards {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(144px, 1fr));
  gap: 18px 14px;
}
.card {
  position: relative;
  display: flex;
  flex-direction: column;
  align-items: center;
  min-width: 0;
  text-align: center;
}
.card.hidden {
  opacity: 0.5;
}
.cover {
  display: block;
  border-radius: 50%;
  transition: transform 0.15s ease;
}
.cover:hover {
  transform: scale(1.04);
}
.name {
  max-width: 100%;
  margin-top: 8px;
  font-weight: 600;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}
.name.muted {
  font-weight: 400;
}
.count {
  color: var(--text-3);
  font-size: 12px;
}
.act {
  position: absolute;
  top: 0;
  right: 8px;
  width: 30px;
  height: 30px;
  background: var(--surface);
  opacity: 0;
  transition: opacity 0.12s;
}
.card:hover .act,
.act:focus-visible {
  opacity: 1;
}
.empty {
  margin-top: 36px;
}
.subtle {
  margin-top: 24px;
}
</style>
