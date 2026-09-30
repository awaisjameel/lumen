<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { BlockKind, BlockMode, engine } from '../bridge'
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
let active = true

function isActive(): boolean {
  return active
}

onMounted(async () => {
  try {
    const got = await engine.blockPINConfigured()
    if (isActive()) configured.value = got
  } catch (e) {
    if (isActive()) error.value = errorText(e)
  }
})
onBeforeUnmount(() => {
  active = false
  void engine.lockBlocks().catch(reportError)
})

async function unlock() {
  if (busy.value) return
  busy.value = true
  error.value = ''
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
    if (!isActive()) return
    const got = await engine.blocks()
    if (!isActive()) return
    blocks.value = got
    unlocked.value = true
    pin.value = ''
    confirmPIN.value = ''
  } catch (e) {
    if (isActive()) error.value = errorText(e)
  } finally {
    busy.value = false
  }
}

async function reload() {
  await showBlocksChanged()
  if (!isActive()) return
  const got = await engine.blocks()
  if (isActive()) blocks.value = got
}

async function changeMode(item: Block) {
  if (busy.value) return
  busy.value = true
  error.value = ''
  try {
    const next = item.mode === BlockMode.BlockPhotos ? BlockMode.BlockLabel : BlockMode.BlockPhotos
    await engine.setBlock(item.kind, item.targetId, item.path, next)
    await reload()
  } catch (e) {
    error.value = errorText(e)
  } finally {
    busy.value = false
  }
}

async function unblock(item: Block) {
  if (busy.value) return
  busy.value = true
  error.value = ''
  try {
    const confirmed = await confirm(
      t('blocked.unblockTitle', { name: item.name }),
      t('blocked.unblockText'),
      t('blocked.unblock'),
    )
    if (!confirmed || !isActive()) return
    await engine.removeBlock(item.id)
    await reload()
  } catch (e) {
    error.value = errorText(e)
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
      <form v-if="!unlocked" class="unlock" @submit.prevent="unlock">
        <p>{{ t(configured ? 'blocked.enterPINHint' : 'blocked.createPINHint') }}</p>
        <PinFields v-model:pin="pin" v-model:confirm="confirmPIN" :creating="!configured" />
        <button class="btn btn-primary" :disabled="busy">{{ t('blocked.unlock') }}</button>
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
