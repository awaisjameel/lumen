<script setup lang="ts">
// People: everyone the AI found in the photos, as round faces with names.
// Naming someone once is how they are confirmed; people can be hidden.
import { computed, onMounted, ref, toRef } from 'vue'
import { engine } from '../bridge'
import type { Person } from '../bridge'
import { errorText, logError } from '../shared/errors'
import { useHidden } from '../shared/hidden'
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
const error = ref('')

const { showHidden, hiddenCount, shown, toggleLabel } = useHidden(toRef(people, 'list'))
const named = computed(() => shown.value.filter((p) => p.name))
const unnamed = computed(() => shown.value.filter((p) => !p.name))
const facesError = computed(() => lib.progress?.ai.facesError ?? '')
const looking = computed(() => lib.progress?.faces ?? 0)

onMounted(() => void people.load().catch(logError))
useLiveReload(people.load, (c) => c.people || c.assets) // new people appear while faces are found

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
      <p v-if="error || people.error || facesError" class="page-error" role="alert">
        {{ error || people.error || t('people.unavailable', { error: facesError }) }}
      </p>

      <template v-for="group in [named, unnamed]" :key="group === named ? 'named' : 'unnamed'">
        <template v-if="group.length">
          <h2>{{ group === named ? t('people.named') : t('people.notNamed') }}</h2>
          <div class="cards">
            <div v-for="p in group" :key="p.id" class="card person" :class="{ hidden: p.hidden }">
              <RouterLink
                :to="{ name: 'person', params: { id: p.id } }"
                class="face"
                :aria-label="p.name || t('person.unnamed')"
              >
                <FaceThumb :face="p.cover" :size="128" round />
              </RouterLink>
              <span class="card-name" :class="{ muted: !p.name }">{{ p.name || t('person.addName') }}</span>
              <span class="card-count">{{ t('status.items', { n: p.count }) }}</span>
              <button
                class="icon-btn card-act"
                :title="p.hidden ? t('people.unhide') : t('people.hide')"
                :aria-label="p.hidden ? t('people.unhide') : t('people.hide')"
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

      <button v-if="hiddenCount" class="btn btn-subtle show-hidden" @click="showHidden = !showHidden">
        {{ toggleLabel }}
      </button>
    </div>
  </div>
</template>

<style scoped>
.intro {
  margin: 0 0 8px;
}
h2 {
  margin: 20px 0 12px;
  font-size: 14px;
  font-weight: 600;
}
.person {
  display: flex;
  flex-direction: column;
  align-items: center;
  text-align: center;
}
.face {
  display: block;
  border-radius: 50%;
  transition: transform 0.15s ease;
}
.face:hover {
  transform: scale(1.04);
}
.person .card-name {
  max-width: 100%;
  margin-top: 8px;
}
.card-name.muted {
  color: var(--text-2);
  font-weight: 400;
}
.person .card-act {
  right: 8px;
}
.empty {
  margin-top: 36px;
}
.show-hidden {
  margin-top: 24px;
}
</style>
