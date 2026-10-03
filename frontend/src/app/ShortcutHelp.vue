<script setup lang="ts">
// The keyboard shortcut reference: every registered command that has a
// shortcut, by group. It reads the registry, so it cannot drift from what
// the keys actually do.
import UKbd from '@nuxt/ui/components/Kbd.vue'
import UModal from '@nuxt/ui/components/Modal.vue'
import { computed } from 'vue'
import { commands, keyCaps } from '../shared/commands'
import type { CommandGroup } from '../shared/commands'
import { t } from '../shared/i18n'
import type { MessageKey } from '../shared/i18n'

const open = defineModel<boolean>('open', { required: true })

const groupLabel: Record<CommandGroup, MessageKey> = {
  media: 'commands.group.media',
  view: 'commands.group.view',
  library: 'commands.group.library',
  navigate: 'commands.group.navigate',
  app: 'commands.group.app',
}
const order: readonly CommandGroup[] = ['media', 'view', 'library', 'navigate', 'app']

const groups = computed(() =>
  order
    .map((g) => ({
      id: g,
      label: t(groupLabel[g]),
      items: commands.value.flatMap((c) =>
        c.group === g && c.keys ? [{ id: c.id, title: c.title(), caps: keyCaps(c.keys) }] : [],
      ),
    }))
    .filter((g) => g.items.length),
)
</script>

<template>
  <UModal
    v-model:open="open"
    :title="t('shortcuts.title')"
    :description="t('shortcuts.hint')"
    :ui="{ content: 'max-w-lg' }"
  >
    <template #body>
      <section v-for="g in groups" :key="g.id" class="group">
        <h3>{{ g.label }}</h3>
        <dl>
          <div v-for="c in g.items" :key="c.id" class="row">
            <dt>{{ c.title }}</dt>
            <dd>
              <UKbd v-for="cap in c.caps" :key="cap" :value="cap" />
            </dd>
          </div>
        </dl>
      </section>
    </template>
  </UModal>
</template>

<style scoped>
.group + .group {
  margin-top: 1rem;
}
h3 {
  margin: 0 0 0.375rem;
  color: var(--ui-text-dimmed);
  font-size: 0.75rem;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.04em;
}
dl {
  margin: 0;
}
.row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  min-height: var(--lumen-row-h);
}
dt {
  color: var(--ui-text);
}
dd {
  display: flex;
  gap: 0.25rem;
  margin: 0;
}
</style>
