<script setup lang="ts">
// The one search box: type a description and press Enter to search by what's
// in the photos (names of people narrow it to them), or pick a matching
// person or Thing. Ctrl+F or / focuses it.
import { computed, onBeforeUnmount, onMounted, ref, useId, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import type { RouteLocationRaw } from 'vue-router'
import { engine } from '../bridge'
import type { Person, Tag } from '../bridge'
import { logError } from '../shared/errors'
import { t } from '../shared/i18n'
import Icon from '../shared/Icon.vue'
import { isTyping, modalOpen } from '../shared/keys'
import { useSuggestions } from '../shared/suggest'
import { useLibrary } from '../library/store'
import FaceThumb from '../people/FaceThumb.vue'

const route = useRoute()
const router = useRouter()
const lib = useLibrary()

const id = useId()
const input = ref<HTMLInputElement | null>(null)
const text = ref('')
const open = ref(false)
const active = ref(0) // index into options

const suggest = (q: string) => Promise.all([engine.suggestPeople(q), engine.suggestTags(q)])
const found = useSuggestions<[Person[], Tag[]]>(text, suggest, [[], []])

/** What the menu offers: searching for the text, then people, then Things. */
type Option = { kind: 'search' } | { kind: 'person'; person: Person } | { kind: 'thing'; tag: Tag }
const options = computed<Option[]>(() => {
  if (!text.value.trim()) return []
  const [people, things] = found.value
  return [
    { kind: 'search' },
    ...people.map((person): Option => ({ kind: 'person', person })),
    ...things.map((tag): Option => ({ kind: 'thing', tag })),
  ]
})

// Show the current query while on the results page, and forget it on leaving.
watch(
  () => (route.name === 'search' ? String(route.query.q ?? '') : null),
  (q, before) => {
    if (q !== null) text.value = q
    else if (before != null) text.value = ''
  },
  { immediate: true },
)

watch(text, () => (active.value = 0))

function close() {
  open.value = false
  input.value?.blur()
}

function search() {
  const q = text.value.trim()
  if (!q) return
  close()
  if (route.name === 'search' && route.query.q === q) {
    void lib.setScope({ kind: 'search', query: q }).catch(logError) // search again
  } else {
    void router.push({ name: 'search', query: { q } })
  }
}

function go(to: RouteLocationRaw) {
  close()
  text.value = ''
  void router.push(to)
}

function choose(o: Option | undefined) {
  if (o?.kind === 'search') search()
  else if (o?.kind === 'person') go({ name: 'person', params: { id: o.person.id } })
  else if (o?.kind === 'thing') go({ name: 'thing', params: { id: o.tag.id } })
}

function clear() {
  text.value = ''
  if (route.name === 'search') void router.push('/')
  input.value?.focus()
}

function onKey(e: KeyboardEvent) {
  const n = options.value.length
  if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
    e.preventDefault()
    open.value = true
    if (n) active.value = (active.value + (e.key === 'ArrowDown' ? 1 : n - 1)) % n
  } else if (e.key === 'Enter') {
    e.preventDefault()
    choose(options.value[active.value])
  } else if (e.key === 'Escape') {
    if (open.value && n) open.value = false
    else input.value?.blur()
  }
}

// Ctrl+F or "/" from anywhere (except while typing elsewhere, or when the
// viewer or a question covers the box).
function onGlobalKey(e: KeyboardEvent) {
  if (modalOpen()) return
  if ((e.ctrlKey && e.key.toLowerCase() === 'f') || (e.key === '/' && !isTyping(e))) {
    e.preventDefault()
    input.value?.focus()
    input.value?.select()
  }
}
onMounted(() => window.addEventListener('keydown', onGlobalKey))
onBeforeUnmount(() => window.removeEventListener('keydown', onGlobalKey))

function optionKey(o: Option): string {
  return o.kind === 'person' ? `p${o.person.id}` : o.kind === 'thing' ? `t${o.tag.id}` : 's'
}

/** The first option of each kind gets a heading above it. */
function heading(o: Option, i: number): string {
  if (o.kind === options.value[i - 1]?.kind) return ''
  return o.kind === 'person' ? t('search.peopleHeading') : o.kind === 'thing' ? t('search.thingsHeading') : ''
}
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
      :aria-controls="`${id}-list`"
      :aria-expanded="open && options.length > 0"
      :aria-activedescendant="open && options.length ? `${id}-${active}` : undefined"
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

    <ul v-if="open && options.length" :id="`${id}-list`" class="menu" role="listbox" @mousedown.prevent>
      <template v-for="(o, i) in options" :key="optionKey(o)">
        <li v-if="heading(o, i)" class="heading" role="presentation">{{ heading(o, i) }}</li>
        <li
          :id="`${id}-${i}`"
          role="option"
          :aria-selected="active === i"
          :class="{ active: active === i }"
          @mouseenter="active = i"
          @click="choose(o)"
        >
          <template v-if="o.kind === 'search'">
            <Icon name="search" :size="16" />
            <span class="ellipsis">{{ t('search.for', { q: text.trim() }) }}</span>
          </template>
          <template v-else-if="o.kind === 'person'">
            <FaceThumb :face="o.person.cover" :size="22" round />
            <span class="ellipsis">{{ o.person.name }}</span>
          </template>
          <template v-else>
            <Icon name="tag" :size="16" />
            <span class="ellipsis">{{ o.tag.name }}</span>
          </template>
        </li>
      </template>
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
