<script setup lang="ts">
import IconButton from '../shared/IconButton.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UCommandPalette from '@nuxt/ui/components/CommandPalette.vue'
import type { CommandPaletteGroup, CommandPaletteItem } from '@nuxt/ui/components/CommandPalette.vue'
import UModal from '@nuxt/ui/components/Modal.vue'
import { computed, ref, useId, watch } from 'vue'
import {
  availableCommands,
  chordOf,
  commandGroups,
  isEnabled,
  keyCaps,
  keysOf,
  runCommand,
  sameKeys,
} from '../shared/commands'
import type { Command } from '../shared/commands'
import { reportError } from '../shared/errors'
import { t } from '../shared/i18n'
import { icons } from '../design/icons'
import { whenPortalsClosed } from '../shared/overlay'
import Icon from '../shared/Icon.vue'

const open = defineModel<boolean>('open', { required: true })
const query = ref('')
const inputId = useId()
const activeId = ref('')
const activeCommand = computed(() => availableCommands.value.find((command) => command.id === activeId.value))
const helpCommand = computed(() => availableCommands.value.find((command) => command.id === 'app.shortcuts'))
const helpCaps = computed(() => {
  const keys = helpCommand.value ? keysOf(helpCommand.value) : undefined
  return keys ? keyCaps(keys) : []
})
function onKey(event: KeyboardEvent) {
  if (event.isComposing || event.defaultPrevented || !helpCommand.value) return
  const chord = chordOf(event)
  const keys = keysOf(helpCommand.value)
  if (chord && keys && sameKeys(chord, keys)) {
    event.preventDefault()
    event.stopPropagation()
    void execute(helpCommand.value.id)
  }
}
function highlight(payload: { value: unknown } | undefined) {
  const value = payload?.value
  activeId.value = value && typeof value === 'object' && 'id' in value && typeof value.id === 'string' ? value.id : ''
}
// Recents live only for this session; commands are resolved against current availability.
const recent = ref<string[]>([])
watch(open, (value) => {
  if (value) query.value = ''
})

interface Part {
  text: string
  match: boolean
}

/** Match a subsequence, retaining the original text so highlighting never renders HTML. */
function parts(label: string): Part[] | undefined {
  const needle = Array.from(query.value.toLocaleLowerCase().replace(/\s/gu, ''))
  let next = 0
  const result: Part[] = []
  for (const character of label) {
    const match = next < needle.length && character.toLocaleLowerCase() === needle[next]
    if (match) next++
    result.push({ text: character, match })
  }
  return next === needle.length ? result : undefined
}

async function execute(id: string) {
  open.value = false
  // Let the modal return focus before an action focuses a field or opens another layer.
  await whenPortalsClosed()
  try {
    if (await runCommand(id)) recent.value = [id, ...recent.value.filter((value) => value !== id)].slice(0, 3)
  } catch (error) {
    reportError(error)
  }
}
function item(command: Command): CommandPaletteItem[] {
  const matched = parts(command.title())
  if (!matched) return []
  const keys = keysOf(command)
  return [
    {
      id: command.id,
      label: command.title(),
      icon: command.icon ? icons[command.icon] : undefined,
      kbds: keys ? keyCaps(keys) : undefined,
      description: isEnabled(command) ? undefined : command.reason?.(),
      disabled: !isEnabled(command),
      onSelect: () => {
        void execute(command.id)
      },
    },
  ]
}
const groups = computed<CommandPaletteGroup[]>(() => {
  const catalogue = availableCommands.value.filter((command) => command.id !== 'app.commandPalette')
  const normal = commandGroups.map((group) => ({
    id: group,
    label: t(`commands.group.${group}`),
    ignoreFilter: true,
    items: catalogue.filter((command) => command.group === group).flatMap(item),
  }))
  const recents = query.value.trim()
    ? []
    : [
        {
          id: 'recent',
          label: t('commands.recent'),
          ignoreFilter: true,
          items: recent.value.flatMap((id) => {
            const command = catalogue.find((c) => c.id === id)
            return command ? item(command) : []
          }),
        },
      ]
  return [...recents, ...normal].filter((group) => group.items.length)
})
</script>

<template>
  <UModal
    v-model:open="open"
    :title="t('commands.title')"
    :description="t('commands.hint')"
    :ui="{
      content: 'max-w-[40rem] max-h-[calc(100dvh-3rem)] overflow-hidden divide-y-0',
      header: 'px-5 py-4 shrink-0',
      wrapper: 'relative ps-12',
      title: 'text-sm',
      description: 'text-xs',
      body: 'p-0 sm:p-0 overflow-hidden flex flex-col min-h-0',
    }"
  >
    <template #title
      ><span class="title-icon"><Icon name="command" :size="20" /></span>{{ t('commands.title') }}</template
    >
    <template #body>
      <label :for="inputId" class="sr-only">{{ t('commands.title') }}</label>
      <div class="palette">
        <UCommandPalette
          v-model:search-term="query"
          :groups="groups"
          :placeholder="t('commands.placeholder')"
          :input="{ id: inputId }"
          :fuse="{ resultLimit: Number.MAX_SAFE_INTEGER }"
          preserve-group-order
          :ui="{
            root: 'w-full min-h-0 flex-1 divide-y-0',
            input: 'mx-5 mb-4 rounded-(--lumen-r-lg) border border-default bg-(--lumen-bg-sunken) shrink-0',
            content: 'min-h-0 overflow-y-auto overscroll-contain border-t border-default px-3 py-2',
            item: 'min-h-10 py-2 gap-3 rounded-(--lumen-r-md)',
            itemLabel: 'whitespace-normal break-words',
            itemDescription: 'whitespace-normal',
            itemWrapper: 'min-w-0',
            label: 'uppercase tracking-widest text-dimmed text-[11px]',
            footer: 'p-0 border-t border-default',
          }"
          @highlight="highlight"
          @keydown.capture="onKey"
        >
          <template #close
            ><IconButton
              v-if="query"
              color="neutral"
              variant="ghost"
              :icon="icons.close"
              :label="t('commands.clear')"
              @click="query = ''"
            /><UButton color="neutral" variant="ghost" :aria-label="t('common.close')" @click="open = false"
              ><kbd class="lumen-kbd">Esc</kbd></UButton
            ></template
          >
          <template #item-label="{ item: row }"
            ><template v-for="(part, index) in parts(row.label ?? '')" :key="index"
              ><mark v-if="part.match">{{ part.text }}</mark
              ><template v-else>{{ part.text }}</template></template
            ></template
          >
          <template #item-trailing="{ item: row }"
            ><span class="caps"
              ><kbd v-for="(cap, index) in row.kbds" :key="index" class="lumen-kbd">{{ cap }}</kbd></span
            ></template
          >
          <template #empty
            ><div class="empty">
              <h3>{{ t('commands.none') }}</h3>
              <p>{{ t('commands.noneHint') }}</p>
              <UButton color="neutral" variant="outline" @click="query = ''">{{ t('commands.showAll') }}</UButton>
            </div></template
          >
          <template #footer
            ><div class="foot">
              <span><kbd class="lumen-kbd">↑</kbd> <kbd class="lumen-kbd">↓</kbd> {{ t('commands.move') }}</span
              ><UButton
                color="primary"
                variant="solid"
                size="sm"
                :disabled="!activeCommand || !isEnabled(activeCommand)"
                :title="activeCommand?.reason?.()"
                @click="activeCommand && execute(activeCommand.id)"
                ><kbd class="lumen-kbd">Enter</kbd>{{ t('commands.run') }}</UButton
              ><UButton color="neutral" variant="ghost" size="sm" @click="open = false"
                ><kbd class="lumen-kbd">Esc</kbd>{{ t('common.close') }}</UButton
              ><UButton color="neutral" variant="ghost" size="sm" @click="execute('app.shortcuts')"
                ><kbd v-for="cap in helpCaps" :key="cap" class="lumen-kbd">{{ cap }}</kbd
                >{{ t('commands.allShortcuts') }}</UButton
              >
            </div></template
          >
        </UCommandPalette>
      </div>
    </template>
  </UModal>
</template>

<style scoped>
.palette {
  width: 100%;
  min-height: 0;
  display: flex;
  flex: 1;
  flex-direction: column;
}
.title-icon {
  position: absolute;
  inset-inline-start: 0;
  top: 0;
  width: 2.5rem;
  height: 2.5rem;
  display: grid;
  place-items: center;
  border-radius: var(--lumen-r-lg);
  background: var(--lumen-bg-sunken);
  color: var(--ui-text-toned);
}
.palette :deep([data-slot='input']) {
  flex-shrink: 0;
}
.palette :deep([data-slot='content']) {
  max-height: 28rem;
}
.palette :deep([data-slot='input']:focus-within) {
  outline: 2px solid var(--ui-primary);
  outline-offset: 2px;
}
.palette :deep(input) {
  outline: none;
  box-shadow: none;
}
.palette :deep([data-slot='item'][data-highlighted]) {
  background: var(--lumen-primary-soft);
  color: var(--ui-text-highlighted);
}
.palette :deep([data-slot='item'][data-disabled]) {
  opacity: 1;
  color: var(--ui-text-dimmed);
  background: var(--lumen-bg-sunken);
}
mark {
  color: var(--ui-text-highlighted);
  background: var(--lumen-primary-soft-2);
  border-radius: var(--lumen-r-xs);
  font-weight: 600;
}
.caps {
  display: flex;
  gap: 0.25rem;
  flex-shrink: 0;
}
.foot {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.75rem;
  padding: 0.75rem 1.25rem;
  color: var(--ui-text-dimmed);
  background: var(--lumen-bg-sunken);
  font-size: 0.75rem;
}
.foot > :last-child {
  margin-inline-start: auto;
}
.empty {
  padding: 2.5rem 1.25rem;
  text-align: center;
  color: var(--ui-text-dimmed);
}
.empty h3 {
  color: var(--ui-text-highlighted);
  font-size: 1.25rem;
  font-weight: 600;
}
.empty p {
  margin: 0.5rem 0 1rem;
}
</style>
