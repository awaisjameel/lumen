<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
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
import { confirm } from '../shared/confirm'
import { errorText, reportError } from '../shared/errors'
import { t } from '../shared/i18n'
import PinFields from '../shared/PinFields.vue'

const configured = ref(true)
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
let active = true
let idleTimer = 0
let sessionRevision = 0

function relock() {
  clearTimeout(idleTimer)
  sessionRevision++
  blocks.value = []
  unlocked.value = false
  pin.value = ''
  confirmPIN.value = ''
  void nextTick(() => {
    if (isActive() && !unlocked.value) unlockForm.value?.querySelector('input')?.focus()
  })
}

function used(revision: number) {
  if (!isActive() || revision !== sessionRevision) return
  clearTimeout(idleTimer)
  idleTimer = window.setTimeout(relock, BLOCK_UNLOCK_IDLE_MINUTES * 60 * 1000)
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
  }
})
onBeforeUnmount(() => {
  active = false
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
    await engine.setBlock(item.kind, item.targetId, item.path, next)
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
  }
  return kind
}
</script>

<template>
  <div class="page">
    <header class="topbar">
      <h1>{{ t('blocked.title') }}</h1>
    </header>
    <div class="page-body">
      <RouterLink to="/settings" class="btn btn-subtle">{{ t('blocked.back') }}</RouterLink>
      <p v-if="error" class="page-error" role="alert">{{ error }}</p>
      <form v-if="!unlocked && !resetting" ref="unlockForm" class="unlock" @submit.prevent="unlock">
        <p v-if="changed" role="status">{{ t('blocked.pinChanged') }}</p>
        <p v-if="configured">{{ t('blocked.enterPINHint') }}</p>
        <p v-else>{{ t('blocked.createPINHint', { pinMin: PIN_MIN_LEN, pinMax: PIN_MAX_LEN }) }}</p>
        <PinFields v-model:pin="pin" v-model:confirm="confirmPIN" :creating="!configured" />
        <div class="actions">
          <button class="btn btn-primary" :disabled="busy">{{ t('blocked.unlock') }}</button>
          <button v-if="configured" type="button" class="btn btn-subtle" :disabled="busy" @click="openReset">
            {{ t('blocked.forgotPIN') }}
          </button>
        </div>
      </form>
      <form v-else-if="resetting" ref="resetForm" class="unlock" @submit.prevent="reset">
        <p>{{ t('blocked.resetHint') }}</p>
        <PinFields v-model:pin="pin" v-model:confirm="confirmPIN" creating />
        <div class="actions">
          <button class="btn btn-primary" :disabled="busy">{{ t('blocked.reset') }}</button>
          <button type="button" class="btn btn-subtle" :disabled="busy" @click="closeReset">
            {{ t('blocked.resetCancel') }}
          </button>
        </div>
      </form>
      <template v-else>
        <p class="muted">{{ t('blocked.privacy') }}</p>
        <ul v-if="blocks.length" class="list">
          <li v-for="item in blocks" :key="item.id">
            <div class="item-name">
              <strong>{{ item.name }}</strong
              ><small
                >{{ kindName(item.kind) }} ·
                {{ t(item.mode === BlockMode.BlockPhotos ? 'blocked.labelPhotos' : 'blocked.labelOnly') }}</small
              >
            </div>
            <button
              v-if="item.kind !== BlockKind.BlockAsset && item.kind !== BlockKind.BlockFolder"
              class="btn"
              :disabled="busy"
              @click="changeMode(item)"
            >
              {{ t('blocked.switchMode') }}
            </button>
            <button class="btn" :disabled="busy" @click="unblock(item)">{{ t('blocked.unblock') }}</button>
          </li>
        </ul>
        <p v-else>{{ t('blocked.empty') }}</p>
      </template>
    </div>
  </div>
</template>

<style scoped>
.unlock {
  display: grid;
  justify-items: start;
  gap: 10px;
  max-width: 360px;
  margin-top: 20px;
}

.unlock :deep(.field) {
  width: 100%;
}

.actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}

.list {
  list-style: none;
  padding: 0;
  margin: 20px 0;
}

.list li {
  display: flex;
  align-items: center;
  gap: 10px;
  border-bottom: 1px solid var(--border);
  padding: 12px 0;
}

.item-name {
  display: grid;
  gap: 3px;
  flex: 1;
  min-width: 0;
  overflow-wrap: anywhere;
}

.item-name small {
  color: var(--text-2);
}
</style>
