<script setup lang="ts">
// The one search box: type a description and press Enter to search by what's
// in the photos, or pick a matching Thing. Ctrl+F or / focuses it.
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { engine } from '../bridge'
import type { Tag } from '../bridge'
import { logError } from '../shared/errors'
import { t } from '../shared/i18n'
import Icon from '../shared/Icon.vue'
import { useLibrary } from '../library/store'

const route = useRoute()
const router = useRouter()
const lib = useLibrary()

const input = ref<HTMLInputElement | null>(null)
const text = ref('')
const open = ref(false)
const suggestions = ref<Tag[]>([])
const active = ref(0) // 0 = "search for …", then one per suggestion

// Show the current query while on the results page.
watch(
  () => (route.name === 'search' ? String(route.query.q ?? '') : null),
  (q) => {
    if (q !== null) text.value = q
  },
  { immediate: true },
)

let timer = 0
let asked = 0
watch(text, (v) => {
  active.value = 0
  clearTimeout(timer)
  if (!v.trim()) {
    suggestions.value = []
    return
  }
  timer = window.setTimeout(async () => {
    const n = ++asked
    try {
      const s = await engine.suggestTags(v)
      if (n === asked) suggestions.value = s
    } catch (e) {
      logError(e)
    }
  }, 120)
})

const options = computed(() => (text.value.trim() ? 1 + suggestions.value.length : 0))

function search() {
  const q = text.value.trim()
  if (!q) return
  open.value = false
  input.value?.blur()
  if (route.name === 'search' && route.query.q === q) {
    void lib.setScope({ kind: 'search', query: q }).catch(logError) // search again
  } else {
    void router.push({ name: 'search', query: { q } })
  }
}

function pick(tag: Tag) {
  open.value = false
  input.value?.blur()
  text.value = ''
  void router.push({ name: 'thing', params: { id: tag.id } })
}

function choose() {
  if (active.value === 0) search()
  else pick(suggestions.value[active.value - 1])
}

function clear() {
  text.value = ''
  suggestions.value = []
  if (route.name === 'search') void router.push('/')
  input.value?.focus()
}

function onKey(e: KeyboardEvent) {
  if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
    e.preventDefault()
    open.value = true
    if (options.value) active.value = (active.value + (e.key === 'ArrowDown' ? 1 : options.value - 1)) % options.value
  } else if (e.key === 'Enter') {
    e.preventDefault()
    choose()
  } else if (e.key === 'Escape') {
    if (open.value && options.value) open.value = false
    else input.value?.blur()
  }
}

// Ctrl+F or "/" from anywhere (except while typing elsewhere).
function onGlobalKey(e: KeyboardEvent) {
  const typing = e.target instanceof HTMLInputElement || e.target instanceof HTMLTextAreaElement
  if ((e.ctrlKey && e.key.toLowerCase() === 'f') || (e.key === '/' && !typing)) {
    e.preventDefault()
    input.value?.focus()
    input.value?.select()
  }
}
onMounted(() => window.addEventListener('keydown', onGlobalKey))
onBeforeUnmount(() => {
  window.removeEventListener('keydown', onGlobalKey)
  clearTimeout(timer)
})
</script>

<template>
  <div class="search" @focusout="open = false">
    <Icon name="search" :size="18" class="lead" />
    <input
      ref="input"
      v-model="text"
      type="search"
      role="combobox"
      aria-autocomplete="list"
      aria-controls="search-options"
      :aria-expanded="open && options > 0"
      :aria-activedescendant="open && options ? `search-opt-${active}` : undefined"
      :aria-label="t('search.shortcut')"
      :placeholder="t('search.placeholder')"
      :title="t('search.shortcut')"
      spellcheck="false"
      autocomplete="off"
      @focus="open = true"
      @input="open = true"
      @keydown="onKey"
    />
    <button v-if="text" class="icon-btn clear" :title="t('search.clear')" @mousedown.prevent @click="clear">
      <Icon name="close" :size="16" />
    </button>

    <ul v-if="open && options" id="search-options" class="menu" role="listbox" @mousedown.prevent>
      <li
        id="search-opt-0"
        role="option"
        :aria-selected="active === 0"
        :class="{ active: active === 0 }"
        @mouseenter="active = 0"
        @click="search"
      >
        <Icon name="search" :size="16" />
        <span class="ellipsis">{{ t('search.for', { q: text.trim() }) }}</span>
      </li>
      <li v-if="suggestions.length" class="heading" role="presentation">{{ t('search.thingsHeading') }}</li>
      <li
        v-for="(s, i) in suggestions"
        :id="`search-opt-${i + 1}`"
        :key="s.id"
        role="option"
        :aria-selected="active === i + 1"
        :class="{ active: active === i + 1 }"
        @mouseenter="active = i + 1"
        @click="pick(s)"
      >
        <Icon name="tag" :size="16" />
        <span class="ellipsis">{{ s.name }}</span>
      </li>
    </ul>
  </div>
</template>

<style scoped>
.search {
  position: relative;
  display: flex;
  align-items: center;
  flex: 1;
  max-width: 560px;
  min-width: 180px;
}
.lead {
  position: absolute;
  left: 12px;
  color: var(--text-3);
  pointer-events: none;
}
input {
  width: 100%;
  height: 36px;
  padding: 0 36px 0 38px;
  border: 1px solid var(--border);
  border-radius: 18px;
  background: var(--surface-2);
  color: var(--text);
  font: inherit;
  outline: none;
}
input::-webkit-search-cancel-button {
  display: none;
}
input:focus {
  border-color: var(--accent);
  background: var(--surface);
}
.clear {
  position: absolute;
  right: 4px;
  width: 28px;
  height: 28px;
}
.menu {
  position: absolute;
  top: 42px;
  left: 0;
  right: 0;
  z-index: 20;
  margin: 0;
  padding: 6px;
  list-style: none;
  border: 1px solid var(--border);
  border-radius: var(--radius-m);
  background: var(--surface);
  box-shadow: var(--shadow);
}
.menu li {
  display: flex;
  align-items: center;
  gap: 10px;
  height: 34px;
  padding: 0 10px;
  border-radius: var(--radius-s);
  color: var(--text-2);
  cursor: pointer;
}
.menu li.active {
  background: var(--surface-hover);
  color: var(--text);
}
.menu li.heading {
  height: 26px;
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: var(--text-3);
  cursor: default;
}
.ellipsis {
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}
</style>
