<script setup lang="ts">
// The one search box: type a description and press Enter to search by what's
// in the photos (names of people narrow it to them), or pick a matching
// person or Thing. Ctrl+F or / focuses it.
import IconButton from '../shared/IconButton.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import { computed, ref, useId, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import type { RouteLocationRaw } from 'vue-router'
import { engine } from '../bridge'
import type { Person, Tag } from '../bridge'
import { logError } from '../shared/errors'
import { t } from '../shared/i18n'
import Icon from '../shared/Icon.vue'
import { icons } from '../design/icons'
import { useCommands } from '../shared/commands'
import { useSuggestions } from '../shared/suggest'
import { useLibrary } from '../library/store'
import FaceThumb from '../people/FaceThumb.vue'

const route = useRoute()
const router = useRouter()
const lib = useLibrary()

const id = useId()
// UInput exposes its <input>; the box is focused and blurred through it.
const field = ref<{ inputRef: HTMLInputElement | null } | null>(null)
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

// New suggestions (or none) start from searching for the text again.
watch(options, () => (active.value = 0))

// A search needs the AI's text model, which takes seconds to load: start
// that as soon as someone is about to type one (a loaded model just stays).
function onFocus() {
  open.value = true
  engine.prepareSearch().catch(logError)
}

function close() {
  open.value = false
  field.value?.inputRef?.blur()
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
  field.value?.inputRef?.focus()
}

function onKey(e: KeyboardEvent) {
  if (e.isComposing) return // the key belongs to the input method
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
    else field.value?.inputRef?.blur()
  }
}

// Ctrl+F or "/" from anywhere: one command, so the palette and the shortcut
// reference offer it too. The registry leaves it alone while the user types
// elsewhere, or while the viewer or a dialog covers the box.
function focusBox() {
  const el = field.value?.inputRef
  el?.focus()
  el?.select()
}
useCommands([
  {
    id: 'search.focus',
    group: 'library',
    title: () => t('search.command'),
    icon: 'search',
    keys: 'Ctrl+F',
    altKeys: ['/'],
    run: focusBox,
  },
])

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
    <UInput
      ref="field"
      v-model="text"
      class="box"
      type="search"
      role="combobox"
      aria-autocomplete="list"
      :aria-controls="`${id}-list`"
      :aria-expanded="open && options.length > 0"
      :aria-activedescendant="open && options.length ? `${id}-${active}` : undefined"
      :aria-label="t('search.shortcut')"
      :placeholder="t('search.placeholder')"
      :title="t('search.shortcut')"
      :leading-icon="icons.search"
      variant="soft"
      :ui="{ base: 'rounded-full', trailing: 'pe-1' }"
      spellcheck="false"
      autocomplete="off"
      @focus="onFocus"
      @update:model-value="open = true"
      @keydown="onKey"
    >
      <template v-if="text" #trailing>
        <IconButton
          :icon="icons.close"
          :label="t('search.clear')"
          color="neutral"
          variant="ghost"
          size="xs"
          @mousedown.prevent
          @click="clear"
        />
      </template>
    </UInput>

    <ul v-if="open && options.length" :id="`${id}-list`" class="menu lumen-surface" role="listbox" @mousedown.prevent>
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
  flex: 1 1 14rem;
  max-width: 35rem;
  min-width: 10rem;
}

.box {
  width: 100%;
}

.box :deep(input::-webkit-search-cancel-button) {
  display: none;
}

.menu {
  position: absolute;
  top: calc(100% + 0.375rem);
  left: 0;
  right: 0;
  z-index: var(--lumen-z-panel);
  margin: 0;
  padding: 0.375rem;
  list-style: none;
  border: 1px solid var(--ui-border);
  border-radius: calc(var(--ui-radius) * 1.5);
  background: var(--ui-bg);
  box-shadow: var(--lumen-shadow);
}

.menu li {
  display: flex;
  align-items: center;
  gap: 0.625rem;
  min-height: var(--lumen-row-h);
  padding: 0 0.625rem;
  border-radius: var(--ui-radius);
  color: var(--ui-text-muted);
  cursor: pointer;
}

.menu li.active {
  background: var(--ui-bg-elevated);
  color: var(--ui-text-highlighted);
}

.menu li.heading {
  min-height: 1.625rem;
  font-size: 0.6875rem;
  font-weight: 600;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: var(--ui-text-dimmed);
  cursor: default;
}

.ellipsis {
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}

@media (forced-colors: active) {
  .menu li.active {
    outline: 2px solid Highlight;
    outline-offset: -2px;
  }
}
</style>
