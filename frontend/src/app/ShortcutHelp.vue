<script setup lang="ts">
import IconButton from '../shared/IconButton.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import UModal from '@nuxt/ui/components/Modal.vue'
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { altKeysOf, keyCaps, keysOf, runCommand } from '../shared/commands'
import { shortcutCatalogue, shortcutZones, zoneOf } from '../shared/shortcuts'
import { t } from '../shared/i18n'
import { icons } from '../design/icons'
import { whenPortalsClosed } from '../shared/overlay'
import { reportError } from '../shared/errors'

const open = defineModel<boolean>('open', { required: true })
const router = useRouter()
const query = ref('')
watch(open, (value) => {
  if (value) query.value = ''
})

interface Row {
  id: string
  title: string
  caps: string[]
  reason?: string
}
const groups = computed(() => {
  const fixed: Row[] = [
    { id: 'move', title: t('shortcuts.gridMove'), caps: ['↑', '↓', '←', '→'], reason: t('shortcuts.fixedNavigation') },
    {
      id: 'extend',
      title: t('shortcuts.gridExtend'),
      caps: ['Shift', '↑', '↓', '←', '→'],
      reason: t('shortcuts.fixedNavigation'),
    },
    {
      id: 'toggle',
      title: t('shortcuts.gridToggle'),
      caps: ['Ctrl', t('shortcuts.click')],
      reason: t('shortcuts.fixedPointer'),
    },
    { id: 'clear', title: t('shortcuts.gridClear'), caps: ['Escape'], reason: t('shortcuts.escape') },
  ]
  const catalogue = shortcutZones.map((zone) => ({
    id: zone,
    label: t(`shortcuts.zone.${zone}`),
    items: [
      ...shortcutCatalogue
        .filter((entry) => zoneOf(entry) === zone)
        .flatMap((entry): Row[] => {
          const keys = keysOf(entry)
          return [
            {
              id: entry.id,
              title: t(entry.title),
              caps: keys ? keyCaps(keys) : [],
              reason: entry.locked ? t('shortcuts.escape') : undefined,
            },
            ...altKeysOf(entry).map((alt): Row => ({
              id: `${entry.id}-${alt}`,
              title: t('shortcuts.alternative', { action: t(entry.title) }),
              caps: keyCaps(alt),
              reason: t('shortcuts.alias', { action: t(entry.title) }),
            })),
          ]
        }),
      ...(zone === 'grid' ? fixed : []),
    ],
  }))
  // These keys are handled by VideoPlayer while the player itself has focus.
  const video = {
    id: 'video',
    label: t('shortcuts.zone.video'),
    items: [
      { id: 'play', title: t('shortcuts.videoPlay'), caps: ['Space', '/', 'K'], reason: t('shortcuts.fixedPlayer') },
      { id: 'mute', title: t('shortcuts.videoMute'), caps: ['M'], reason: t('shortcuts.fixedPlayer') },
      { id: 'seek', title: t('shortcuts.videoSeek'), caps: ['←', '/', '→'], reason: t('shortcuts.skipInterval') },
    ],
  }
  const needle = query.value.trim().toLocaleLowerCase()
  return [...catalogue, video]
    .map((group) => ({
      ...group,
      items: group.items.filter((row) =>
        `${row.title} ${row.caps.join(' ')} ${group.label} ${row.reason ?? ''}`.toLocaleLowerCase().includes(needle),
      ),
    }))
    .filter((group) => group.items.length)
})
async function back() {
  open.value = false
  await whenPortalsClosed()
  void runCommand('app.commandPalette').catch(reportError)
}
function customise() {
  open.value = false
  void router.push('/settings?section=accessibility')
}
</script>

<template>
  <UModal
    v-model:open="open"
    :title="t('shortcuts.title')"
    :description="t('shortcuts.hint')"
    :ui="{
      content: 'max-w-[40rem] max-h-[calc(100dvh-3rem)] overflow-hidden',
      body: 'p-0 sm:p-0 overflow-hidden flex flex-col min-h-0',
      footer: 'p-0 sm:p-0',
    }"
  >
    <template #body>
      <div class="search">
        <UInput
          v-model="query"
          type="search"
          autofocus
          :icon="icons.search"
          :aria-label="t('shortcuts.search')"
          :placeholder="t('shortcuts.placeholder')"
          class="w-full"
          size="xl"
          :ui="{ base: 'bg-(--lumen-bg-sunken) rounded-(--lumen-r-lg)' }"
          ><template v-if="query" #trailing
            ><IconButton
              :icon="icons.close"
              :label="t('shortcuts.clear')"
              color="neutral"
              variant="ghost"
              size="xs"
              @click="query = ''" /></template
        ></UInput>
      </div>
      <div class="reference">
        <section v-for="group in groups" :key="group.id" class="group">
          <h3>{{ group.label }}</h3>
          <dl>
            <div v-for="row in group.items" :key="row.id" class="row" :data-shortcut="row.id">
              <dt>{{ row.title }}</dt>
              <dd>
                <span class="caps"
                  ><kbd v-for="(cap, index) in row.caps" :key="index" class="lumen-kbd">{{ cap }}</kbd
                  ><span v-if="!row.caps.length">{{ t('shortcuts.unassigned') }}</span></span
                ><span v-if="row.reason" class="reason">{{ row.reason }}</span
                ><UButton
                  v-else
                  color="neutral"
                  variant="ghost"
                  size="xs"
                  :aria-label="t('shortcuts.changeNamed', { action: row.title })"
                  @click="customise"
                  >{{ t('shortcuts.change') }}</UButton
                >
              </dd>
            </div>
          </dl>
        </section>
        <div v-if="!groups.length" class="empty" role="status">
          <h3>{{ t('shortcuts.none') }}</h3>
          <p>{{ t('shortcuts.noneHint') }}</p>
          <UButton color="neutral" variant="outline" @click="query = ''">{{ t('shortcuts.showAll') }}</UButton>
        </div>
      </div>
    </template>
    <template #footer
      ><div class="foot">
        <UButton color="neutral" variant="ghost" size="sm" :icon="icons.command" @click="back">{{
          t('shortcuts.back')
        }}</UButton
        ><UButton color="primary" variant="solid" size="sm" @click="customise">{{ t('shortcuts.customise') }}</UButton
        ><UButton color="neutral" variant="ghost" size="sm" @click="open = false"
          ><kbd class="lumen-kbd">Esc</kbd>{{ t('common.close') }}</UButton
        >
      </div></template
    >
  </UModal>
</template>

<style scoped>
.search {
  flex-shrink: 0;
  padding: 0 1.25rem 1rem;
  border-bottom: 1px solid var(--ui-border);
}
.search :deep(input::-webkit-search-cancel-button) {
  display: none;
}
.search :deep(input) {
  outline: none;
}
.reference {
  min-height: 0;
  max-height: 28rem;
  overflow: auto;
  overscroll-behavior: contain;
  padding: 1rem 1.25rem;
}
.group + .group {
  margin-top: 1.5rem;
}
h3 {
  margin: 0 0 0.5rem;
  color: var(--ui-text-dimmed);
  font-size: 0.6875rem;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.08em;
}
dl {
  margin: 0;
}
.row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  min-height: 2.5rem;
  padding: 0.5rem 0;
}
dt {
  min-width: 0;
  color: var(--ui-text-highlighted);
  font-size: 0.875rem;
  overflow-wrap: anywhere;
}
dd {
  margin: 0;
  display: flex;
  align-items: center;
  gap: 0.75rem;
  max-width: 65%;
  color: var(--ui-text-dimmed);
  font-size: 0.75rem;
}
.caps {
  display: flex;
  gap: 0.25rem;
  flex-wrap: wrap;
  justify-content: flex-end;
}
.reason {
  max-width: 11rem;
}
.foot {
  width: 100%;
  display: flex;
  justify-content: space-between;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.5rem;
  padding: 0.75rem 1.25rem;
  background: var(--lumen-bg-sunken);
}
.empty {
  padding: 1.5rem 0;
  text-align: center;
  color: var(--ui-text-dimmed);
}
.empty h3 {
  font-size: 1.25rem;
  text-transform: none;
  color: var(--ui-text-highlighted);
  letter-spacing: normal;
}
.empty p {
  margin: 0.5rem 0 1rem;
}
</style>
