<script setup lang="ts">
// The shortcut editor: every action whose keys the user may choose, by where
// its shortcut works. A change is refused, with the reason, when the keys
// already belong to an action that would hear them too, so no action is left
// unreachable. Escape and Tab are never assigned: they always leave a dialog,
// a field or the viewer.
import UButton from '@nuxt/ui/components/Button.vue'
import UKbd from '@nuxt/ui/components/Kbd.vue'
import { computed, ref } from 'vue'
import { chordOf, keyCaps, keysOf, sameKeys } from '../shared/commands'
import { t } from '../shared/i18n'
import { usePreferences } from '../shared/preferences'
import { assignmentConflict, canReassign, shortcutCatalogue, shortcutZones, zoneOf } from '../shared/shortcuts'
import type { ShortcutEntry } from '../shared/shortcuts'
import { useRowFilter } from './rowFilter'

const props = withDefaults(defineProps<{ query?: string }>(), { query: '' })
const prefs = usePreferences()
const matches = useRowFilter(() => props.query)

const zones = computed(() =>
  shortcutZones.map((zone) => ({
    zone,
    label: t(`settings.shortcutZone.${zone}`),
    entries: shortcutCatalogue
      .filter(
        (entry) =>
          zoneOf(entry) === zone && (matches('settings.shortcuts', 'settings.shortcutsHint') || matches(entry.title)),
      )
      .map((entry) => {
        const keys = keysOf(entry)
        return {
          entry,
          title: t(entry.title),
          caps: keys ? keyCaps(keys) : [],
          changed: entry.id in prefs.prefs.shortcuts,
        }
      }),
  })),
)
const anyChanged = computed(() => Object.keys(prefs.prefs.shortcuts).length > 0)

/** The action whose next key press becomes its shortcut. */
const recording = ref('')
/** What the last change did, or why it was refused, and for which action. */
const note = ref<{ id: string; text: string; failed: boolean } | null>(null)

function say(entry: ShortcutEntry, text: string, failed = false) {
  note.value = { id: entry.id, text, failed }
}

function record(entry: ShortcutEntry) {
  note.value = null
  recording.value = recording.value === entry.id ? '' : entry.id
}

async function assign(entry: ShortcutEntry, keys: string | undefined) {
  // The action's own keys are no reassignment: it simply has its own again.
  const own = keys !== undefined && entry.keys !== undefined && sameKeys(keys, entry.keys)
  const next = own ? undefined : keys
  const conflict = assignmentConflict(entry, next)
  if (conflict) {
    say(entry, t('settings.shortcutTaken', { keys: conflict.keys, action: t(conflict.taken.title) }), true)
    return
  }
  if (!(await prefs.setShortcut(entry.id, next))) return
  const now = keysOf(entry)
  say(
    entry,
    now
      ? t('settings.shortcutSet', { action: t(entry.title), keys: now })
      : t('settings.shortcutCleared', { action: t(entry.title) }),
  )
}

function onKey(e: KeyboardEvent, entry: ShortcutEntry) {
  if (recording.value !== entry.id) return
  // While recording, the press is the answer: it must not also run a shortcut.
  e.stopPropagation()
  if (e.key === 'Escape') {
    e.preventDefault()
    recording.value = ''
    return
  }
  if (e.key === 'Tab') {
    recording.value = '' // moving on by keyboard ends the recording
    return
  }
  e.preventDefault()
  const keys = chordOf(e)
  if (keys === '') return // only modifiers so far
  recording.value = ''
  if (keys === null) {
    say(entry, t('settings.shortcutReserved'), true)
    return
  }
  if (!canReassign(prefs.prefs.shortcuts, entry.id)) {
    say(entry, t('settings.shortcutFull'), true)
    return
  }
  void assign(entry, keys)
}

function resetAll() {
  note.value = null
  recording.value = ''
  void prefs.set({ shortcuts: {} })
}
</script>
<template>
  <div class="shortcuts" data-shortcuts>
    <div class="intro">
      <p class="text-muted">{{ t('settings.shortcutsHint') }}</p>
      <UButton data-shortcut-reset-all variant="outline" :disabled="!anyChanged" @click="resetAll">{{
        t('settings.shortcutResetAll')
      }}</UButton>
    </div>
    <section v-for="z in zones.filter((zone) => zone.entries.length)" :key="z.zone" :aria-label="z.label">
      <h3>{{ z.label }}</h3>
      <ul>
        <li v-for="row in z.entries" :key="row.entry.id" :data-shortcut="row.entry.id">
          <span class="title"
            >{{ row.title }}<span v-if="row.changed" class="modified-dot" :aria-label="t('settings.modified')"
          /></span>
          <span class="keys" data-shortcut-keys>
            <UKbd v-for="cap in row.caps" :key="cap" :value="cap" />
            <span v-if="!row.caps.length" class="text-dimmed">{{ t('settings.shortcutNone') }}</span>
          </span>
          <span class="actions">
            <span v-if="row.entry.locked" class="text-dimmed">{{ t('settings.shortcutLocked') }}</span>
            <template v-else>
              <UButton
                size="xs"
                :variant="recording === row.entry.id ? 'solid' : 'outline'"
                :aria-pressed="recording === row.entry.id"
                :aria-label="t('settings.shortcutChangeNamed', { action: row.title })"
                data-shortcut-change
                @click="record(row.entry)"
                @keydown="onKey($event, row.entry)"
                @blur="recording === row.entry.id && (recording = '')"
                >{{ t(recording === row.entry.id ? 'settings.shortcutRecording' : 'settings.shortcutChange') }}</UButton
              >
              <UButton
                v-if="row.caps.length"
                size="xs"
                variant="ghost"
                :aria-label="t('settings.shortcutRemoveNamed', { action: row.title })"
                data-shortcut-remove
                @click="assign(row.entry, '')"
                >{{ t('settings.shortcutRemove') }}</UButton
              >
              <UButton
                v-if="row.changed"
                size="xs"
                variant="ghost"
                :aria-label="t('settings.shortcutResetNamed', { action: row.title })"
                data-shortcut-reset
                @click="assign(row.entry, undefined)"
                >{{ t('settings.shortcutReset') }}</UButton
              >
            </template>
          </span>
          <p
            v-if="note?.id === row.entry.id"
            class="note"
            :class="{ failed: note.failed }"
            :role="note.failed ? 'alert' : 'status'"
          >
            {{ note.text }}
          </p>
        </li>
      </ul>
    </section>
  </div>
</template>
<style scoped>
.shortcuts {
  padding: 1.25rem;
}
.modified-dot {
  display: inline-block;
  width: 0.375rem;
  height: 0.375rem;
  margin-left: 0.5rem;
  border-radius: 50%;
  background: var(--ui-primary);
}
.intro {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  padding: 0.5rem 0;
}

.intro p {
  flex: 1 1 16rem;
  margin: 0;
}

h3 {
  margin: 0.75rem 0 0.25rem;
  color: var(--ui-text-dimmed);
  font-size: 0.75rem;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.04em;
}

ul {
  margin: 0;
  padding: 0;
  list-style: none;
}

li {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto auto;
  align-items: center;
  gap: 0.25rem 1rem;
  min-height: var(--lumen-row-h);
  padding: 0.25rem 0;
  border-bottom: 1px solid var(--ui-border-muted);
}

.keys,
.actions {
  display: flex;
  align-items: center;
  gap: 0.25rem;
}

.actions {
  justify-content: flex-end;
  min-width: 11rem;
}

.note {
  grid-column: 1 / -1;
  margin: 0;
  color: var(--ui-text-dimmed);
  font-size: 0.8125rem;
}

.note.failed {
  color: var(--ui-error);
}

@container (max-width: 32rem) {
  li {
    grid-template-columns: minmax(0, 1fr) auto;
  }

  .actions {
    grid-column: 1 / -1;
    justify-content: flex-start;
  }
}
</style>
