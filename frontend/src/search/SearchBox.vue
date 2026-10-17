<script setup lang="ts">
// The one search box: type a description and press Enter to search by what's
// in the photos (names of people narrow it to them), or pick a matching
// person, Thing or recent search. Ctrl+F or / focuses it.
import IconButton from '../shared/IconButton.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import { computed, onBeforeUnmount, onMounted, ref, useId, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { engine } from '../bridge'
import type { Person, Tag } from '../bridge'
import { logError } from '../shared/errors'
import { formatNumber, t } from '../shared/i18n'
import type { MessageKey } from '../shared/i18n'
import Icon from '../shared/Icon.vue'
import { icons } from '../design/icons'
import { altKeysOf, keyCaps, keysOf, useCommands } from '../shared/commands'
import { useSuggestionState } from '../shared/suggest'
import { useLibrary } from '../library/store'
import FaceThumb from '../people/FaceThumb.vue'
import { useRecentSearches } from './recent'

const route = useRoute()
const router = useRouter()
const lib = useLibrary()
const recent = useRecentSearches()

const id = useId()
// UInput exposes its <input>; the box is focused and blurred through it.
const field = ref<{ inputRef: HTMLInputElement | null } | null>(null)
const text = ref('')
const open = ref(false)
const active = ref(0) // index into options

const suggest = (q: string) => Promise.all([engine.suggestPeople(q), engine.suggestTags(q)])
const suggestions = useSuggestionState<[Person[], Tag[]]>(text, suggest, [[], []])
const { found, loading, error } = suggestions
const preparing = ref(false)
const prepareFailed = ref(false)
let mounted = true
const root = ref<HTMLElement | null>(null)

/**
 * What the menu offers: searching for the text, then people, then Things,
 * then recent searches. An empty box offers its recent searches alone.
 */
type Option =
  | { kind: 'search' }
  | { kind: 'person'; person: Person }
  | { kind: 'thing'; tag: Tag }
  | { kind: 'recent'; query: string }
const recentOptions = computed(() => recent.matching(text.value).map((query): Option => ({ kind: 'recent', query })))
const options = computed<Option[]>(() => {
  if (!text.value.trim()) return recentOptions.value
  const [people, things] = found.value
  return [
    { kind: 'search' },
    ...people.map((person): Option => ({ kind: 'person', person })),
    ...things.map((tag): Option => ({ kind: 'thing', tag })),
    ...recentOptions.value,
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
function prepare() {
  if (preparing.value) return
  preparing.value = true
  prepareFailed.value = false
  void engine
    .prepareSearch()
    .catch(() => {
      if (mounted) prepareFailed.value = true
    })
    .finally(() => {
      if (mounted) preparing.value = false
    })
}
function onFocus() {
  open.value = true
  prepare()
  void recent.load()
}
function retry() {
  suggestions.retry()
  prepare()
}
function focusOut(e: FocusEvent) {
  if (!(e.relatedTarget instanceof Node) || !root.value?.contains(e.relatedTarget)) open.value = false
}
function outside(e: PointerEvent) {
  if (e.target instanceof Node && !root.value?.contains(e.target)) open.value = false
}
onMounted(() => document.addEventListener('pointerdown', outside))
onBeforeUnmount(() => {
  mounted = false
  document.removeEventListener('pointerdown', outside)
})
watch(
  active,
  () => {
    document.getElementById(`${id}-${active.value}`)?.scrollIntoView({ block: 'nearest' })
  },
  { flush: 'post' },
)

function close() {
  open.value = false
  field.value?.inputRef?.blur()
}

function search() {
  const q = text.value.trim()
  if (!q) return
  close()
  void recent.remember(q)
  if (route.name === 'search' && route.query.q === q) {
    void lib.setScope({ kind: 'search', query: q }).catch(logError) // search again
  } else {
    void router.push({ name: 'search', query: { q } })
  }
}

function choose(o: Option | undefined) {
  if (!o) return
  if (o.kind === 'person') text.value = o.person.name
  else if (o.kind === 'thing') text.value = o.tag.name
  else if (o.kind === 'recent') text.value = o.query
  search()
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
    choose(open.value ? options.value[active.value] : { kind: 'search' })
  } else if (e.key === 'Escape') {
    e.stopPropagation()
    if (open.value) open.value = false
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
const focusCommands = [
  {
    id: 'search.focus',
    group: 'library',
    title: () => t('search.command'),
    icon: 'search',
    keys: 'Ctrl+F',
    altKeys: ['/'],
    run: focusBox,
  },
] as const
useCommands(focusCommands)
const [focusCommand] = focusCommands
const focusCaps = computed(() => {
  const keys = keysOf(focusCommand)
  return keys ? keyCaps(keys) : []
})
const focusTitle = computed(() =>
  [t('search.command'), keysOf(focusCommand), ...altKeysOf(focusCommand)].filter(Boolean).join(' · '),
)

function optionKey(o: Option): string {
  return o.kind === 'person'
    ? `p${o.person.id}`
    : o.kind === 'thing'
      ? `t${o.tag.id}`
      : o.kind === 'recent'
        ? `r${o.query}`
        : 's'
}

const headings: Record<Option['kind'], MessageKey | null> = {
  search: null,
  person: 'search.peopleHeading',
  thing: 'search.thingsHeading',
  recent: 'search.recentHeading',
}
/** The first option of each kind gets a heading above it. */
function heading(o: Option, i: number): string {
  if (o.kind === options.value[i - 1]?.kind) return ''
  const key = headings[o.kind]
  return key ? t(key) : ''
}

/** Forgets the recent searches; the box keeps its focus. */
async function clearRecent() {
  await recent.clear()
  active.value = 0
}
</script>

<template>
  <div ref="root" class="search" @focusout="focusOut">
    <UInput
      ref="field"
      v-model="text"
      class="box"
      type="search"
      role="combobox"
      aria-autocomplete="list"
      aria-haspopup="listbox"
      :aria-controls="open && options.length ? `${id}-list` : undefined"
      :aria-expanded="open && options.length > 0"
      :aria-activedescendant="open && options.length ? `${id}-${active}` : undefined"
      :aria-label="t('search.command')"
      :placeholder="t('search.placeholder')"
      :title="focusTitle"
      :leading-icon="icons.search"
      variant="none"
      :ui="{ base: 'search-input', trailing: 'pe-1.5 gap-1' }"
      spellcheck="false"
      autocomplete="off"
      @focus="onFocus"
      @update:model-value="open = true"
      @keydown="onKey"
    >
      <template #trailing>
        <IconButton
          v-if="text"
          :icon="icons.close"
          :label="t('search.clear')"
          color="neutral"
          variant="ghost"
          size="xs"
          @mousedown.prevent
          @click="clear"
        />
        <template v-else
          ><kbd v-if="focusCaps.length" class="lumen-kbd" aria-hidden="true">{{ focusCaps.join(' ') }}</kbd></template
        >
      </template>
    </UInput>

    <div v-if="open && (options.length || preparing || prepareFailed)" class="menu lumen-float" @mousedown.prevent>
      <p v-if="preparing" class="status" role="status">
        <span class="lumen-light-dot" />{{ t('search.modelLoading') }}
      </p>
      <div v-else-if="prepareFailed" class="status" role="status">
        {{ t('search.modelUnavailable')
        }}<UButton color="neutral" variant="ghost" size="xs" @click="retry">{{ t('common.retry') }}</UButton>
      </div>
      <ul
        v-if="options.length"
        :id="`${id}-list`"
        role="listbox"
        :aria-label="t('search.suggestions')"
        :aria-busy="loading"
      >
        <template v-for="(o, i) in options" :key="optionKey(o)">
          <li v-if="heading(o, i)" class="heading" role="presentation">{{ heading(o, i) }}</li>
          <li
            :id="`${id}-${i}`"
            role="option"
            :aria-selected="active === i"
            :class="{ active: active === i, thing: o.kind === 'thing', recent: o.kind === 'recent' }"
            @mouseenter="active = i"
            @click="choose(o)"
          >
            <template v-if="o.kind === 'search'"
              ><Icon name="search" :size="20" /><span class="ellipsis">{{ t('search.for', { q: text.trim() }) }}</span
              ><kbd class="lumen-kbd">Enter</kbd></template
            >
            <template v-else-if="o.kind === 'person'"
              ><FaceThumb :face="o.person.cover" :size="32" round /><span class="ellipsis">{{ o.person.name }}</span
              ><span class="count">{{ formatNumber(o.person.count) }}</span></template
            >
            <template v-else-if="o.kind === 'recent'"
              ><Icon name="history" :size="16" /><span class="ellipsis">{{ o.query }}</span></template
            >
            <template v-else
              ><Icon name="tag" :size="16" /><span class="ellipsis">{{ o.tag.name }}</span></template
            >
          </li>
        </template>
      </ul>
      <div v-if="error" class="status" role="alert">
        <span>{{ t('search.suggestionError') }}</span
        ><UButton color="neutral" variant="ghost" size="xs" @click="retry">{{ t('common.retry') }}</UButton>
      </div>
      <p
        v-else-if="text.trim() && !loading && !found[0].length && !found[1].length && !recentOptions.length"
        class="status"
        role="status"
      >
        {{ t('search.noSuggestions') }}
      </p>
      <div v-if="options.length" class="foot">
        <span><kbd class="lumen-kbd">↑</kbd> <kbd class="lumen-kbd">↓</kbd> {{ t('commands.move') }}</span
        ><span><kbd class="lumen-kbd">Enter</kbd> {{ t('search.run') }}</span
        ><UButton
          v-if="recentOptions.length"
          class="forget"
          color="neutral"
          variant="ghost"
          size="xs"
          data-search-forget
          @click="clearRecent"
          >{{ t('search.recentClear') }}</UButton
        ><UButton color="neutral" variant="ghost" size="xs" @click="open = false"
          ><kbd class="lumen-kbd">Esc</kbd>{{ t('common.close') }}</UButton
        >
      </div>
    </div>
  </div>
</template>

<style scoped>
.search {
  position: relative;
  flex: 1;
  width: 100%;
  min-width: 0;
  max-width: 40rem;
}
.box {
  width: 100%;
  border: 1px solid var(--ui-border);
  border-radius: var(--lumen-r-lg);
  background: var(--lumen-bg-sunken);
  transition:
    background-color var(--lumen-d-base) var(--lumen-ease-out),
    border-color var(--lumen-d-base) var(--lumen-ease-out),
    box-shadow var(--lumen-d-base) var(--lumen-ease-out);
}
.box:hover {
  border-color: var(--ui-border-accented);
}
/* Focused, the field lifts to the raised surface with the accent's ring,
   as every place you type to find something does (FilterField). */
.box:focus-within {
  border-color: var(--ui-primary);
  background: var(--lumen-bg-raised);
  box-shadow: 0 0 0 4px var(--lumen-primary-soft);
}
.box :deep(.search-input) {
  height: 2.25rem;
  min-width: 0;
  border-radius: inherit;
  box-shadow: none;
  color: var(--ui-text-highlighted);
}
.box :deep(input::-webkit-search-cancel-button) {
  display: none;
}
.box :deep(input) {
  outline: none;
}
.menu {
  position: absolute;
  top: calc(100% + 0.5rem);
  inset-inline: 0;
  z-index: var(--lumen-z-panel);
  max-height: min(70dvh, calc(100dvh - 8rem));
  overflow: auto;
  overscroll-behavior: contain;
  padding: 0.375rem;
}
ul {
  margin: 0;
  padding: 0;
  list-style: none;
  display: flex;
  flex-wrap: wrap;
  gap: 0.25rem;
}
li[role='option'] {
  width: 100%;
  display: flex;
  align-items: center;
  gap: 0.75rem;
  min-height: 3rem;
  padding: 0.5rem 0.75rem;
  border-radius: var(--lumen-r-md);
  color: var(--ui-text-toned);
  cursor: pointer;
}
li.active {
  background: var(--lumen-primary-soft);
  color: var(--ui-text-highlighted);
}
li .lumen-kbd {
  margin-inline-start: auto;
}
.count {
  margin-inline-start: auto;
  color: var(--ui-text-dimmed);
  font-size: 0.75rem;
  font-variant-numeric: tabular-nums;
}
li.recent {
  min-height: 2.5rem;
}
li.thing {
  width: auto;
  min-height: 2rem;
  border: 1px solid var(--ui-border);
  border-radius: var(--lumen-r-sm);
  padding: 0.25rem 0.75rem;
}
.heading {
  width: 100%;
  padding: 0.75rem 0.75rem 0.25rem;
  font-size: 0.6875rem;
  font-weight: 600;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--ui-text-dimmed);
}
.ellipsis {
  min-width: 0;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}
.status {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 0.5rem;
  padding: 0.5rem 0.75rem;
  margin: 0;
  font-size: 0.75rem;
  color: var(--ui-text-dimmed);
}
.foot {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 0.5rem;
  padding: 0.5rem 0.75rem;
  border-top: 1px solid var(--ui-border);
  font-size: 0.75rem;
  color: var(--ui-text-dimmed);
}
.foot > .forget,
.foot > :last-child {
  margin-inline-start: auto;
}
.foot > .forget + :last-child {
  margin-inline-start: 0;
}
@media (forced-colors: active) {
  li.active {
    outline: 2px solid Highlight;
    outline-offset: -2px;
  }
}
</style>
