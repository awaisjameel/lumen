<script setup lang="ts">
// The command palette (Ctrl+K): every command that applies now, by group,
// with its shortcut. It lists what the registry holds, so a command added
// to a toolbar is here too, under the same name and rule.
import UCommandPalette from '@nuxt/ui/components/CommandPalette.vue'
import type { CommandPaletteGroup, CommandPaletteItem } from '@nuxt/ui/components/CommandPalette.vue'
import UModal from '@nuxt/ui/components/Modal.vue'
import { computed } from 'vue'
import { icons } from '../design/icons'
import { availableCommands, isEnabled, keyCaps } from '../shared/commands'
import type { Command, CommandGroup } from '../shared/commands'
import { t } from '../shared/i18n'
import type { MessageKey } from '../shared/i18n'

const open = defineModel<boolean>('open', { required: true })

const groupOrder: readonly CommandGroup[] = ['media', 'view', 'library', 'navigate', 'app']
const groupLabel: Record<CommandGroup, MessageKey> = {
  media: 'commands.group.media',
  view: 'commands.group.view',
  library: 'commands.group.library',
  navigate: 'commands.group.navigate',
  app: 'commands.group.app',
}

function item(c: Command): CommandPaletteItem {
  return {
    id: c.id,
    label: c.title(),
    icon: c.icon ? icons[c.icon] : undefined,
    kbds: c.keys ? keyCaps(c.keys) : undefined,
    disabled: !isEnabled(c),
    onSelect: () => {
      open.value = false
      void c.run()
    },
  }
}

const groups = computed<CommandPaletteGroup[]>(() =>
  groupOrder
    .map((g) => ({
      id: g,
      label: t(groupLabel[g]),
      // The palette's own command would only close it again.
      items: availableCommands.value.filter((c) => c.group === g && c.id !== 'app.commandPalette').map(item),
    }))
    .filter((g) => g.items.length),
)
</script>

<template>
  <UModal
    v-model:open="open"
    :title="t('commands.title')"
    :description="t('commands.hint')"
    :ui="{ content: 'max-w-xl', header: 'sr-only', body: 'p-0 sm:p-0' }"
  >
    <template #body>
      <UCommandPalette
        :groups="groups"
        :placeholder="t('commands.placeholder')"
        class="h-96 max-h-[70vh]"
        close
        @update:open="open = $event"
      />
    </template>
  </UModal>
</template>
