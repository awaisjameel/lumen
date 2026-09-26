<script setup lang="ts">
// First run: one question, sensible folders pre-ticked, one button.
import { computed, onMounted, ref } from 'vue'
import { engine } from '../bridge'
import { errorText, orFallback } from '../shared/errors'
import { t } from '../shared/i18n'
import Icon from '../shared/Icon.vue'

const emit = defineEmits<{ started: [] }>()

const folders = ref<{ path: string; on: boolean }[]>([])
const busy = ref(false)
const error = ref('')

onMounted(async () => {
  const suggested = await orFallback(engine.suggestedFolders(), [])
  folders.value = suggested.map((path) => ({ path, on: true }))
})

const chosen = computed(() => folders.value.filter((f) => f.on).map((f) => f.path))

async function addFolder() {
  const path = await orFallback(engine.pickFolder(), '')
  if (!path) return
  if (!folders.value.some((f) => f.path.toLowerCase() === path.toLowerCase())) {
    folders.value.push({ path, on: true })
  }
}

async function start() {
  busy.value = true
  error.value = ''
  try {
    await engine.addSources(chosen.value)
    emit('started')
  } catch (e) {
    error.value = errorText(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <main class="welcome">
    <div class="card">
      <img src="/lumen.png" alt="" class="logo" />
      <h1>{{ t('welcome.title') }}</h1>
      <p class="question">{{ t('welcome.question') }}</p>

      <p v-if="!folders.length" class="muted">{{ t('welcome.none') }}</p>
      <ul v-else class="folders">
        <li v-for="f in folders" :key="f.path">
          <label>
            <input v-model="f.on" type="checkbox" />
            <Icon name="folder" :size="18" />
            <span class="path" :title="f.path">{{ f.path }}</span>
          </label>
        </li>
      </ul>

      <button class="btn add" @click="addFolder">
        <Icon name="plus" :size="16" />
        {{ t('welcome.addFolder') }}
      </button>

      <p v-if="error" class="error">{{ error }}</p>

      <button class="btn btn-primary btn-large start" :disabled="!chosen.length || busy" @click="start">
        {{ t('welcome.start') }}
      </button>

      <p class="privacy">
        <Icon name="lock" :size="16" />
        {{ t('welcome.privacy') }}
      </p>
    </div>
  </main>
</template>

<style scoped>
.welcome {
  display: grid;
  place-items: center;
  height: 100%;
  padding: 24px;
  overflow-y: auto;
}
.card {
  width: min(520px, 100%);
  display: flex;
  flex-direction: column;
  align-items: flex-start;
}
.logo {
  width: 56px;
  height: 56px;
  margin-bottom: 20px;
}
h1 {
  margin: 0;
  font: 600 28px/1.2 var(--font-display);
}
.question {
  margin: 8px 0 20px;
  font-size: 16px;
  color: var(--text-2);
}
.folders {
  list-style: none;
  padding: 0;
  margin: 0 0 12px;
  width: 100%;
  border: 1px solid var(--border);
  border-radius: var(--radius-l);
  background: var(--surface);
  overflow: hidden;
}
.folders li + li {
  border-top: 1px solid var(--border);
}
.folders label {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 12px 14px;
  cursor: pointer;
}
.folders label:hover {
  background: var(--surface-hover);
}
.folders input {
  accent-color: var(--accent);
  width: 16px;
  height: 16px;
}
.path {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.add {
  margin-bottom: 28px;
}
.start {
  align-self: stretch;
  justify-content: center;
}
.privacy {
  display: flex;
  gap: 8px;
  margin: 16px 0 0;
  font-size: 12px;
  color: var(--text-3);
}
.privacy svg {
  flex-shrink: 0;
  margin-top: 1px;
}
.error {
  color: var(--danger);
}
</style>
