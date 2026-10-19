<script setup lang="ts">
// First run keeps the source selection until the local library starts.
import { computed, onMounted, ref } from 'vue'
import { engine } from '../bridge'
import { useLibrary } from '../library/store'
import { errorText } from '../shared/errors'
import UButton from '@nuxt/ui/components/Button.vue'
import UCheckbox from '@nuxt/ui/components/Checkbox.vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import FirstScan from './FirstScan.vue'
import { fileName } from '../shared/format'
import WorkProgress from '../shared/WorkProgress.vue'
import { icons } from '../design/icons'
import { useCommands, runCommand } from '../shared/commands'
import { t } from '../shared/i18n'
import Icon from '../shared/Icon.vue'
import BrandMark from '../shared/BrandMark.vue'

const emit = defineEmits<{ starting: []; complete: [] }>()
const lib = useLibrary()
const step = ref(0)
const scanning = ref(false)
const scanDone = ref(false)
const picking = ref(false)
const steps = ['welcome.folders', 'welcome.privacyTitle', 'welcome.start'] as const
const folders = ref<{ path: string; key: string; on: boolean }[]>([])
const loaded = ref(false)
const busy = ref(false)
const error = ref('')

async function discover() {
  loaded.value = false
  error.value = ''
  try {
    const suggested = await engine.suggestedFolders()
    // A folder picked while discovery runs keeps its selection.
    for (const path of suggested) await add(path, false)
  } catch (e) {
    error.value = errorText(e)
  } finally {
    loaded.value = true
  }
}
onMounted(() => void discover())

const chosen = computed(() => folders.value.filter((f) => f.on).map((f) => f.path))

// Use the library's path key, including its Unicode case folding and path
// cleaning. Locale collation also equates some distinct file names.
async function add(path: string, select = true) {
  const key = await engine.sourcePathKey(path)
  const existing = folders.value.find((f) => f.key === key)
  if (existing) {
    if (select) existing.on = true
  } else {
    folders.value.push({ path, key, on: true })
  }
}

async function addFolder() {
  if (picking.value || busy.value) return
  picking.value = true
  error.value = ''
  try {
    const path = await engine.pickFolder()
    if (path) await add(path)
  } catch (e) {
    error.value = errorText(e)
  } finally {
    picking.value = false
  }
}

async function start() {
  if (busy.value || !chosen.value.length || !loaded.value) return
  busy.value = true
  emit('starting')
  error.value = ''
  try {
    await engine.addSources(chosen.value)
    await lib.sourcesChanged()
    scanning.value = true
  } catch (e) {
    error.value = errorText(e)
  } finally {
    busy.value = false
  }
}
useCommands([
  {
    id: 'welcome.addFolder',
    group: 'library',
    title: () => t('welcome.addFolder'),
    icon: 'folder-add',
    enabled: () => !busy.value && !picking.value && step.value === 0 && !scanning.value,
    run: addFolder,
  },
  {
    id: 'welcome.start',
    group: 'library',
    title: () => t('welcome.start'),
    icon: 'play',
    enabled: () => !!chosen.value.length && loaded.value && !busy.value && step.value === 2 && !scanning.value,
    run: start,
  },
])
</script>

<template>
  <main class="welcome" data-welcome>
    <header class="welcome-bar">
      <span class="brand"><BrandMark />{{ t('app.name') }}</span>
      <span class="local"><Icon name="lock" :size="14" />{{ t('welcome.local') }}</span>
    </header>
    <div class="welcome-scroll">
      <div class="welcome-content">
        <div class="mark"><BrandMark class="logo" /></div>
        <div class="intro">
          <h1>{{ scanning ? (scanDone ? t('welcome.done') : t('welcome.scanning')) : t('welcome.title') }}</h1>
          <p>{{ scanning ? t('welcome.scanHint') : t('welcome.promise') }}</p>
        </div>
        <FirstScan v-if="scanning" @ready="scanDone = $event" @complete="emit('complete')" />
        <template v-else>
          <nav class="steps" :aria-label="t('welcome.setup')">
            <template v-for="(key, i) in steps" :key="key">
              <span v-if="i" class="step-line" aria-hidden="true" />
              <UButton
                color="neutral"
                variant="ghost"
                :disabled="i > step || busy"
                :aria-current="i === step ? 'step' : undefined"
                :title="i > step ? t('welcome.completeStep') : t(key)"
                @click="step = i"
              >
                <span class="step-number" :class="{ current: i === step, done: i < step }">{{ i + 1 }}</span
                >{{ t(key) }}
              </UButton>
            </template>
          </nav>
          <section class="setup-card">
            <template v-if="step === 0">
              <h2>{{ t('welcome.question') }}</h2>
              <div v-if="!loaded" class="discovery" role="status" aria-busy="true" :aria-label="t('welcome.loading')">
                <p>{{ t('welcome.loading') }}</p>
                <span class="lumen-skeleton" /><span class="lumen-skeleton" />
              </div>
              <p v-else-if="!folders.length" class="hint">{{ t('welcome.none') }}</p>
              <ul v-if="folders.length" class="folders">
                <li v-for="f in folders" :key="f.key" :data-selected="f.on">
                  <UCheckbox
                    :model-value="f.on"
                    :disabled="busy"
                    :label="f.path"
                    :title="f.path"
                    @update:model-value="f.on = $event === true"
                  >
                    <template #label
                      ><span class="folder-label"
                        ><span class="folder-icon"><Icon name="folder" :size="20" /></span
                        ><span class="folder-text"
                          ><strong>{{ fileName(f.path) || f.path }}</strong
                          ><span class="path lumen-selectable" data-folder-path>{{ f.path }}</span></span
                        ><span class="not-counted">{{ t('welcome.notCounted') }}</span></span
                      ></template
                    >
                  </UCheckbox>
                </li>
              </ul>
              <UButton
                class="add"
                data-add-folder
                :icon="icons['folder-add']"
                :disabled="busy || picking"
                :loading="picking"
                :color="folders.length ? 'neutral' : 'primary'"
                :variant="folders.length ? 'ghost' : 'solid'"
                @click="runCommand('welcome.addFolder')"
                >{{ t('welcome.addFolder') }}</UButton
              >
            </template>
            <template v-else-if="step === 1">
              <h2>{{ t('welcome.privacyTitle') }}</h2>
              <p>{{ t('welcome.privacy') }}</p>
              <div v-for="row in ['local', 'folders', 'account'] as const" :key="row" class="privacy-row">
                <Icon :name="row === 'local' ? 'theme-system' : row === 'folders' ? 'folder' : 'lock'" /><span
                  ><strong>{{ t(`welcome.${row}Title`) }}</strong
                  ><span>{{ t(`welcome.${row}Hint`) }}</span></span
                ><Icon name="check" :size="16" />
              </div>
            </template>
            <template v-else>
              <h2>{{ t('welcome.ready') }}</h2>
              <p>{{ t('welcome.review') }}</p>
              <ul class="review">
                <li v-for="path in chosen" :key="path">
                  <Icon name="folder" :size="16" /><span>{{ path }}</span
                  ><Icon name="check" :size="16" />
                </li>
              </ul>
              <p>{{ t('welcome.scanHint') }}</p>
            </template>
            <UAlert v-if="error" :description="error" color="error" variant="subtle" role="alert"
              ><template v-if="step === 0" #actions
                ><UButton color="neutral" variant="ghost" @click="discover">{{ t('welcome.retry') }}</UButton></template
              ></UAlert
            >
            <WorkProgress v-if="busy" :label="t('welcome.scanning')" :detail="t('welcome.scanHint')" indeterminate />
            <div class="foot">
              <span v-if="step === 0" class="hint" aria-live="polite">{{
                chosen.length ? t('welcome.selected', { n: chosen.length }) : t('welcome.select')
              }}</span>
              <UButton v-else color="neutral" variant="ghost" :disabled="busy" @click="step--">{{
                t('welcome.back')
              }}</UButton>
              <UButton
                v-if="step < 2"
                class="start"
                size="lg"
                data-continue
                :disabled="!chosen.length || !loaded || busy"
                :title="!chosen.length ? t('welcome.select') : undefined"
                color="primary"
                variant="solid"
                :trailing-icon="icons['arrow-right']"
                @click="step++"
                >{{ t('welcome.continue') }}</UButton
              >
              <UButton
                v-else
                class="start"
                size="lg"
                data-start
                :disabled="!chosen.length || busy"
                :loading="busy"
                color="primary"
                variant="solid"
                :trailing-icon="icons['arrow-right']"
                @click="runCommand('welcome.start')"
                >{{ busy ? t('welcome.adding') : t('welcome.start') }}</UButton
              >
            </div>
          </section>
          <p class="privacy"><Icon name="lock" :size="14" />{{ t('welcome.privacy') }}</p>
        </template>
        <p class="tagline">{{ t('welcome.tagline') }}</p>
      </div>
    </div>
  </main>
</template>
<style scoped>
.welcome {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
  background: var(--lumen-bg-base);
}
.welcome-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex: none;
  padding: 1.25rem 2rem;
  border-bottom: 1px solid var(--ui-border);
}
.brand,
.local {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}
.brand {
  font-weight: 600;
  color: var(--ui-text-highlighted);
}
.local,
.hint,
.privacy,
.tagline,
.not-counted {
  font-size: 0.75rem;
  color: var(--ui-text-dimmed);
}
.welcome-scroll {
  flex: 1;
  min-height: 0;
  overflow: auto;
  padding: 1.5rem;
  display: flex;
}
.welcome-content {
  width: min(100%, 46.5rem);
  margin: auto;
  display: flex;
  flex-direction: column;
  align-items: stretch;
  gap: 1.5rem;
  min-width: 0;
}
.mark {
  position: relative;
  align-self: center;
  isolation: isolate;
}
.mark::before {
  content: '';
  position: absolute;
  inset: -4rem;
  z-index: -1;
  background: radial-gradient(ellipse, var(--lumen-light-soft), transparent 68%);
}
.logo {
  width: 4rem;
  height: 4rem;
}
.intro {
  text-align: center;
}
h1,
h2,
p {
  margin: 0;
}
h1 {
  font: 600 2rem var(--font-display);
  color: var(--ui-text-highlighted);
  text-wrap: balance;
}
h2 {
  font-size: 1rem;
  font-weight: 600;
  color: var(--ui-text-highlighted);
}
p {
  color: var(--ui-text-toned);
}
.intro p {
  margin-top: 0.5rem;
}
.steps {
  display: flex;
  align-items: center;
  padding: 0 1rem;
}
.steps button {
  white-space: normal;
}
.step-number {
  display: grid;
  place-items: center;
  width: 1.5rem;
  height: 1.5rem;
  flex: none;
  border: 1px solid var(--ui-border-accented);
  border-radius: 50%;
}
.step-number.current {
  background: var(--lumen-primary-soft);
  border-color: var(--ui-primary);
  color: var(--lumen-primary-text);
}
.step-number.done {
  background: var(--lumen-primary-soft);
  color: var(--lumen-primary-text);
}
.step-line {
  flex: 1;
  height: 1px;
  background: var(--ui-border);
}
.setup-card {
  display: flex;
  flex-direction: column;
  gap: 1.25rem;
  padding: 2rem;
  border: 1px solid var(--ui-border);
  border-radius: var(--lumen-r-lg);
  background: var(--lumen-bg-raised);
  box-shadow: var(--lumen-shadow-1);
}
.folders,
.review {
  list-style: none;
  padding: 0;
  margin: 0;
  display: grid;
  gap: 0.75rem;
}
.folders li {
  padding: 1rem;
  border: 1px solid var(--ui-border);
  border-radius: var(--lumen-r-md);
}
.folders li[data-selected='true'] {
  background: var(--lumen-primary-soft);
  border-color: var(--lumen-primary-soft-2);
}
.folders :deep([data-slot='root']),
.folders :deep([data-slot='wrapper']),
.folders :deep([data-slot='label']) {
  width: 100%;
  min-width: 0;
}
.folders :deep([role='checkbox']) {
  width: 1.5rem;
  height: 1.5rem;
}
.folder-icon {
  display: grid;
  place-items: center;
  flex: none;
  width: 2.25rem;
  height: 2.25rem;
  border-radius: var(--lumen-r-sm);
  background: var(--lumen-bg-sunken);
  color: var(--ui-text-toned);
}
.folder-label {
  display: flex;
  align-items: center;
  gap: 1rem;
  min-width: 0;
}
.folder-text {
  display: grid;
  flex: 1;
  min-width: 0;
}
.folder-text strong {
  color: var(--ui-text-highlighted);
  font-weight: 600;
}
.path {
  font-size: 0.75rem;
  color: var(--ui-text-dimmed);
  overflow-wrap: anywhere;
}
.not-counted {
  flex: none;
}
.add {
  align-self: flex-start;
}
.foot {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 1rem;
}
.privacy {
  display: flex;
  align-items: flex-start;
  gap: 0.5rem;
  padding: 0 1rem;
}
.privacy svg {
  flex: none;
}
.tagline {
  text-align: center;
}
.privacy-row {
  display: flex;
  align-items: center;
  gap: 1rem;
}
.privacy-row > span {
  display: grid;
  flex: 1;
  gap: 0.25rem;
}
.privacy-row strong {
  font-size: 0.875rem;
  color: var(--ui-text-highlighted);
}
.privacy-row span span {
  font-size: 0.8125rem;
  color: var(--ui-text-toned);
}
.review {
  padding: 1rem;
  background: var(--lumen-bg-sunken);
  border-radius: var(--lumen-r-md);
}
.review li {
  display: flex;
  align-items: center;
  gap: 0.75rem;
}
.review li span {
  flex: 1;
  min-width: 0;
  overflow-wrap: anywhere;
}
.discovery {
  display: grid;
  gap: 0.75rem;
}
.discovery .lumen-skeleton {
  height: 4rem;
}
@media (max-height: 600px) {
  .welcome-scroll {
    padding: 1rem 1.5rem;
  }
  .welcome-content {
    gap: 1rem;
  }
  .logo {
    width: 2.5rem;
    height: 2.5rem;
  }
  h1 {
    font-size: 1.5rem;
  }
  .setup-card {
    padding: 1rem;
  }
  .folders li {
    padding: 0.75rem;
  }
}
@media (max-width: 720px) {
  .not-counted {
    display: none;
  }
  .steps {
    padding: 0;
  }
  .steps button {
    font-size: 0.75rem;
    gap: 0.375rem;
  }
}
</style>
