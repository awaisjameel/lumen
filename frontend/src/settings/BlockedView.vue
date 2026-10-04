<script setup lang="ts">
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import USwitch from '@nuxt/ui/components/Switch.vue'
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import {
  BLOCK_UNLOCK_IDLE_MINUTES,
  BlockKind,
  BlockMode,
  engine,
  errorCode,
  ErrorCode,
  PIN_MAX_LEN,
  PIN_MIN_LEN,
} from '../bridge'
import type { Block } from '../bridge'
import { showBlocksChanged } from '../shared/block'
import { icons } from '../design/icons'
import { registerCommands, runCommand, useCommands } from '../shared/commands'
import type { Command } from '../shared/commands'
import { confirm } from '../shared/confirm'
import { errorText, reportError } from '../shared/errors'
import { t } from '../shared/i18n'
import PinFields from '../shared/PinFields.vue'
import Page from '../shared/Page.vue'
import PageHeader from '../shared/PageHeader.vue'
import FeatureState from '../shared/FeatureState.vue'
import Icon from '../shared/Icon.vue'
import CoveredBrowser from './CoveredBrowser.vue'
import { useCoveredPhotos } from './covered'

const configured = ref(true)
const loading = ref(true)
const unlocked = ref(false)
const pin = ref('')
const confirmPIN = ref('')
const blocks = ref<Block[]>([])
const error = ref('')
const busy = ref(false)
const resetting = ref(false) // the forgotten-PIN form is open
const changed = ref(false) // the PIN was just reset: say so above the unlock form
const unlockForm = ref<HTMLFormElement | null>(null)
const resetForm = ref<HTMLFormElement | null>(null)
const blockList = ref<HTMLElement | null>(null)
let active = true
let idleTimer = 0
let sessionRevision = 0

// The browser's photos are as private as the list: it closes, with the
// Viewer, whenever the screen relocks or is left.
const covered = useCoveredPhotos({
  used: () => used(sessionRevision),
  failed: (e) => failed(e),
})

function relock() {
  clearTimeout(idleTimer)
  sessionRevision++
  covered.close()
  blocks.value = []
  unlocked.value = false
  pin.value = ''
  confirmPIN.value = ''
  void nextTick(() => {
    if (isActive() && !unlocked.value) unlockForm.value?.querySelector('input')?.focus()
  })
}

// Left unused for too long: the screen locks, and so does the engine, which
// otherwise goes on serving the covered photos until its own timeout.
function expire() {
  void engine.lockBlocks().catch(reportError)
  relock()
}

function used(revision: number) {
  if (!isActive() || revision !== sessionRevision) return
  clearTimeout(idleTimer)
  idleTimer = window.setTimeout(expire, BLOCK_UNLOCK_IDLE_MINUTES * 60 * 1000)
}

function openReset() {
  resetting.value = true
  changed.value = false
  error.value = ''
  pin.value = ''
  confirmPIN.value = ''
  void nextTick(() => resetForm.value?.querySelector('input')?.focus())
}

function closeReset() {
  resetting.value = false
  error.value = ''
  pin.value = ''
  confirmPIN.value = ''
  void nextTick(() => unlockForm.value?.querySelector('input')?.focus())
}

function failed(e: unknown) {
  if (!isActive()) return
  if (errorCode(e) === ErrorCode.BlocksLocked) relock()
  error.value = errorText(e)
}

function isActive(): boolean {
  return active
}

onMounted(async () => {
  try {
    const got = await engine.blockPINConfigured()
    if (isActive()) configured.value = got
  } catch (e) {
    failed(e)
  } finally {
    if (isActive()) loading.value = false
  }
})
onBeforeUnmount(() => {
  active = false
  covered.close()
  sessionRevision++
  clearTimeout(idleTimer)
  void engine.lockBlocks().catch(reportError)
})

async function unlock() {
  if (busy.value) return
  busy.value = true
  error.value = ''
  changed.value = false
  const revision = ++sessionRevision
  try {
    if (!configured.value) {
      if (pin.value !== confirmPIN.value) {
        error.value = t('blocked.pinMismatch')
        return
      }
      await engine.createBlockPIN(pin.value)
      if (!isActive()) return
      configured.value = true
    }
    await engine.unlockBlocks(pin.value)
    if (!isActive() || revision !== sessionRevision) return
    used(revision)
    const got = await engine.blocks()
    if (!isActive() || revision !== sessionRevision) return
    used(revision)
    blocks.value = got
    unlocked.value = true
    pin.value = ''
    confirmPIN.value = ''
  } catch (e) {
    if (revision === sessionRevision) failed(e)
  } finally {
    busy.value = false
  }
}

// reset replaces a forgotten PIN: the engine has the user sign in to Windows
// first, and the list stays locked, so the new PIN is entered to open it.
async function reset() {
  if (busy.value) return
  error.value = ''
  if (pin.value !== confirmPIN.value) {
    error.value = t('blocked.pinMismatch')
    return
  }
  busy.value = true
  try {
    await engine.resetBlockPIN(pin.value)
    if (!isActive()) return
    resetting.value = false
    changed.value = true
    pin.value = ''
    confirmPIN.value = ''
    void nextTick(() => unlockForm.value?.querySelector('input')?.focus())
  } catch (e) {
    failed(e)
  } finally {
    busy.value = false
  }
}

async function reload(revision: number) {
  await showBlocksChanged()
  if (!isActive() || revision !== sessionRevision) return
  const got = await engine.blocks()
  if (isActive() && revision === sessionRevision) {
    blocks.value = got
    used(revision)
  }
}

async function changeMode(item: Block) {
  if (busy.value) return
  busy.value = true
  error.value = ''
  const revision = sessionRevision
  try {
    const next = item.mode === BlockMode.BlockPhotos ? BlockMode.BlockLabel : BlockMode.BlockPhotos
    await engine.setBlockMode(item.id, next)
    used(revision)
    await reload(revision)
  } catch (e) {
    if (revision === sessionRevision) failed(e)
  } finally {
    busy.value = false
  }
}

async function unblock(item: Block) {
  if (busy.value) return
  busy.value = true
  error.value = ''
  const revision = sessionRevision
  try {
    const confirmed = await confirm(
      t('blocked.unblockTitle', { name: item.name }),
      t('blocked.unblockText'),
      t('blocked.unblock'),
    )
    if (!confirmed || !isActive() || revision !== sessionRevision) return
    await engine.removeBlock(item.id)
    used(revision)
    await reload(revision)
  } catch (e) {
    if (revision === sessionRevision) failed(e)
  } finally {
    busy.value = false
  }
}

function kindName(kind: BlockKind): string {
  switch (kind) {
    case BlockKind.BlockAsset:
      return t('blocked.asset')
    case BlockKind.BlockPerson:
      return t('nav.people')
    case BlockKind.BlockTag:
      return t('nav.things')
    case BlockKind.BlockPlace:
      return t('nav.places')
    case BlockKind.BlockFolder:
      return t('settings.folders')
    case BlockKind.BlockAlbum:
      return t('nav.albums')
    case BlockKind.BlockEvent:
      return t('nav.events')
    case BlockKind.BlockFace:
      return t('blocked.kindFace')
  }
  return kind
}

useCommands([
  {
    id: 'blocked.lock',
    group: 'view',
    title: () => t('blocked.lock'),
    icon: 'lock',
    available: () => unlocked.value,
    run: expire,
  },
])

let removeBlockCommands: () => void = () => undefined
watch(
  blocks,
  (items) => {
    removeBlockCommands()
    const commands: Command[] = items.flatMap((item) => [
      {
        id: `blocked.mode.${item.id}`,
        group: 'library',
        title: () => t('blocked.actionFor', { action: t('blocked.switchMode'), name: item.name }),
        icon: 'refresh',
        available: () =>
          unlocked.value && !covered.block && item.kind !== BlockKind.BlockAsset && item.kind !== BlockKind.BlockFolder,
        enabled: () => !busy.value,
        run: () => changeMode(item),
      },
      {
        id: `blocked.show.${item.id}`,
        group: 'view',
        title: () => t('blocked.actionFor', { action: t('covered.show'), name: item.name }),
        icon: 'eye',
        available: () => unlocked.value && !covered.block,
        enabled: () => !busy.value,
        run: () => covered.open(item),
      },
      {
        id: `blocked.unblock.${item.id}`,
        group: 'library',
        title: () => t('blocked.actionFor', { action: t('blocked.unblock'), name: item.name }),
        icon: 'unlock',
        available: () => unlocked.value && !covered.block,
        enabled: () => !busy.value,
        run: () => unblock(item),
      },
    ])
    removeBlockCommands = registerCommands(commands)
  },
  { immediate: true },
)
onBeforeUnmount(() => removeBlockCommands())

// The browser replaces its trigger's list; once that list is back, restore
// focus to the same block rather than leaving it on the document body.
watch(
  () => covered.block?.id,
  (id, previous) => {
    if (id !== undefined || previous === undefined) return
    void nextTick(() => {
      if (unlocked.value) blockList.value?.querySelector<HTMLElement>(`[data-block-id="${previous}"]`)?.focus()
    })
  },
)
</script>

<template>
  <Page>
    <template #header>
      <PageHeader :title="t('blocked.title')" :back="{ to: '/settings', label: t('blocked.back') }">
        <UButton v-if="unlocked" :icon="icons.lock" @click="runCommand('blocked.lock')">{{
          t('blocked.lock')
        }}</UButton>
      </PageHeader>
    </template>
    <UAlert v-if="error" data-blocked-error color="error" variant="subtle" :title="error" role="alert" class="mb-4" />
    <FeatureState v-if="loading" kind="loading" :title="t('blocked.loading')" />
    <form v-else-if="!unlocked && !resetting" ref="unlockForm" class="pin-form lumen-surface" @submit.prevent="unlock">
      <Icon name="lock" :size="32" />
      <h2>{{ t(configured ? 'blocked.locked' : 'blocked.createPIN') }}</h2>
      <p v-if="changed" role="status">{{ t('blocked.pinChanged') }}</p>
      <p class="text-muted">
        {{
          configured
            ? t('blocked.enterPINHint')
            : t('blocked.createPINHint', { pinMin: PIN_MIN_LEN, pinMax: PIN_MAX_LEN })
        }}
      </p>
      <PinFields v-model:pin="pin" v-model:confirm="confirmPIN" :creating="!configured" />
      <div class="flex flex-wrap gap-2">
        <UButton type="submit" color="primary" variant="solid" :disabled="busy" :loading="busy">{{
          t('blocked.unlock')
        }}</UButton>
        <UButton
          v-if="configured"
          type="button"
          data-blocked="forgot"
          variant="ghost"
          :disabled="busy"
          @click="openReset"
          >{{ t('blocked.forgotPIN') }}</UButton
        >
      </div>
    </form>
    <form v-else-if="resetting" ref="resetForm" class="pin-form lumen-surface" @submit.prevent="reset">
      <Icon name="key" :size="32" />
      <h2>{{ t('blocked.forgotPIN') }}</h2>
      <p class="text-muted">{{ t('blocked.resetHint') }}</p>
      <PinFields v-model:pin="pin" v-model:confirm="confirmPIN" creating />
      <div class="flex flex-wrap gap-2">
        <UButton type="submit" color="primary" variant="solid" :disabled="busy" :loading="busy">{{
          t('blocked.reset')
        }}</UButton>
        <UButton type="button" data-blocked="cancel-reset" variant="ghost" :disabled="busy" @click="closeReset">{{
          t('blocked.resetCancel')
        }}</UButton>
      </div>
    </form>
    <CoveredBrowser v-else-if="covered.block" :covered="covered" />
    <template v-else>
      <UAlert color="neutral" variant="subtle" :icon="icons.info" :title="t('blocked.privacy')" />
      <ul v-if="blocks.length" ref="blockList" class="list">
        <li v-for="item in blocks" :key="item.id" class="block-entry">
          <div class="item-name">
            <strong>{{ item.name }}</strong>
            <small
              >{{ kindName(item.kind) }} ·
              {{ t(item.mode === BlockMode.BlockPhotos ? 'blocked.labelPhotos' : 'blocked.labelOnly') }}</small
            >
          </div>
          <div class="block-actions">
            <USwitch
              v-if="item.kind !== BlockKind.BlockAsset && item.kind !== BlockKind.BlockFolder"
              data-blocked="mode"
              :model-value="item.mode === BlockMode.BlockPhotos"
              :label="t('blocked.labelPhotos')"
              :aria-label="t('blocked.actionFor', { action: t('blocked.switchMode'), name: item.name })"
              :disabled="busy"
              @update:model-value="runCommand(`blocked.mode.${item.id}`)"
            />
            <UButton
              data-blocked="show"
              :data-block-id="item.id"
              :icon="icons.eye"
              :disabled="busy"
              @click="runCommand(`blocked.show.${item.id}`)"
              >{{ t('covered.show') }}</UButton
            >
            <UButton
              data-blocked="unblock"
              :icon="icons.unlock"
              :disabled="busy"
              @click="runCommand(`blocked.unblock.${item.id}`)"
              >{{ t('blocked.unblock') }}</UButton
            >
          </div>
        </li>
      </ul>
      <FeatureState v-else kind="empty" icon="blocked" :title="t('blocked.empty')" />
    </template>
  </Page>
</template>

<style scoped>
.pin-form {
  display: grid;
  gap: 1rem;
  max-width: 28rem;
  margin: 1rem auto;
  padding: 1.5rem;
  border: 1px solid var(--ui-border);
  border-radius: calc(var(--ui-radius) * 2);
  background: var(--ui-bg-muted);
}
h2,
p {
  margin: 0;
}
h2 {
  color: var(--ui-text-highlighted);
  font-size: 1.125rem;
  font-weight: 600;
}
.list {
  list-style: none;
  padding: 0;
  margin: 1rem 0;
}
.block-entry {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 0.75rem;
  border-bottom: 1px solid var(--ui-border);
  padding: 0.75rem 0;
}
.item-name {
  display: grid;
  gap: 0.25rem;
  flex: 1 1 12rem;
  min-width: 0;
  overflow-wrap: anywhere;
}
.item-name small {
  color: var(--ui-text-muted);
}
.block-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
}
@container (max-width: 40rem) {
  .block-actions {
    width: 100%;
  }
}
</style>
