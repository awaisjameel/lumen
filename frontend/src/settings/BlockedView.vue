<script setup lang="ts">
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import { useVirtualizer } from '@tanstack/vue-virtual'
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import {
  BLOCK_UNLOCK_IDLE_MINUTES,
  BlockKind,
  BlockMode,
  engine,
  errorCode,
  ErrorCode,
  PIN_COOLDOWN_SECONDS,
  PIN_PATTERN,
  PIN_MAX_LEN,
  PIN_MIN_LEN,
  thumbUrl,
} from '../bridge'
import type { ComponentPublicInstance } from 'vue'
import type { Block } from '../bridge'
import { showBlocksChanged } from '../shared/block'
import { icons } from '../design/icons'
import { registerCommands, runCommand, useCommands } from '../shared/commands'
import type { Command } from '../shared/commands'
import { errorText, reportError } from '../shared/errors'
import { t } from '../shared/i18n'
import { dismiss, notify } from '../shared/notify'
import { question } from '../shared/confirm'
import Segmented from '../shared/Segmented.vue'
import ViewBar from '../shared/ViewBar.vue'
import SelectionDock from '../shared/SelectionDock.vue'
import PinFields from '../shared/PinFields.vue'
import Page from '../shared/Page.vue'
import PageHeader from '../shared/PageHeader.vue'
import FeatureState from '../shared/FeatureState.vue'
import Icon from '../shared/Icon.vue'
import FaceThumb from '../people/FaceThumb.vue'
import CoveredBrowser from './CoveredBrowser.vue'
import { useCoveredPhotos } from './covered'

const pinStep = ref<'new' | 'confirm'>('new')
const query = ref('')
const filterKind = ref<BlockKind | ''>('')
const cooldown = ref(0)
const configurationError = ref(false)
const listError = ref(false)
let cooldownTimer = 0
const PRIVATE_NOTICE = 'blocked.change'
const validPIN = (value: string) => new RegExp(`^${PIN_PATTERN}$`).test(value)
const creating = computed(() => resetting.value || !configured.value)
const pinError = computed(() => (cooldown.value ? t('pin.cooldown', { n: cooldown.value }) : error.value))
const cannotSubmit = computed(
  () =>
    busy.value ||
    cooldown.value > 0 ||
    !validPIN(creating.value && pinStep.value === 'confirm' ? confirmPIN.value : pin.value),
)
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
  dismiss(PRIVATE_NOTICE)
  question.value?.answer(false)
  query.value = ''
  error.value = ''
  filterKind.value = ''
  listError.value = false
  sessionRevision++
  covered.close()
  blocks.value = []
  unlocked.value = false
  pin.value = ''
  confirmPIN.value = ''
  pinStep.value = 'new'
  resetting.value = false
  changed.value = false
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
  if (unlocked.value) expire()
  pinStep.value = 'new'
  resetting.value = true
  changed.value = false
  error.value = ''
  pin.value = ''
  confirmPIN.value = ''
  void nextTick(() => resetForm.value?.querySelector('input')?.focus())
}

function closeReset() {
  pinStep.value = 'new'
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
  if (errorCode(e) === ErrorCode.PINExists) {
    configured.value = true
    pinStep.value = 'new'
    pin.value = ''
    confirmPIN.value = ''
  }
  if (errorCode(e) === ErrorCode.PINRateLimited) {
    cooldown.value = PIN_COOLDOWN_SECONDS
    clearInterval(cooldownTimer)
    cooldownTimer = window.setInterval(() => {
      cooldown.value--
      if (cooldown.value <= 0) {
        clearInterval(cooldownTimer)
        error.value = ''
      }
    }, 1000)
  }
}

function setPinForm(element: Element | ComponentPublicInstance | null) {
  if (!(element instanceof HTMLFormElement)) return
  if (resetting.value) resetForm.value = element
  else unlockForm.value = element
}

function isActive(): boolean {
  return active
}

async function checkConfiguration() {
  loading.value = true
  configurationError.value = false
  error.value = ''
  try {
    const got = await engine.blockPINConfigured()
    if (isActive()) configured.value = got
  } catch (e) {
    configurationError.value = true
    failed(e)
  } finally {
    if (isActive()) {
      loading.value = false
      void nextTick(() => unlockForm.value?.querySelector('input')?.focus())
    }
  }
}
onMounted(checkConfiguration)
onBeforeUnmount(() => {
  active = false
  dismiss(PRIVATE_NOTICE)
  question.value?.answer(false)
  clearInterval(cooldownTimer)
  covered.close()
  sessionRevision++
  clearTimeout(idleTimer)
  void engine.lockBlocks().catch(reportError)
})

function backPIN() {
  if (pinStep.value === 'confirm') {
    pinStep.value = 'new'
    confirmPIN.value = ''
    error.value = ''
  } else if (resetting.value) closeReset()
  void nextTick(() => (resetting.value ? resetForm.value : unlockForm.value)?.querySelector('input')?.focus())
}
function advancePIN(): boolean {
  if (!creating.value || pinStep.value === 'confirm') return false
  pinStep.value = 'confirm'
  error.value = ''
  void nextTick(() => (resetting.value ? resetForm.value : unlockForm.value)?.querySelector('input')?.focus())
  return true
}
async function unlock() {
  if (cannotSubmit.value || advancePIN()) return
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
    unlocked.value = true
    listError.value = false
    const got = await engine.blocks()
    if (!isActive() || revision !== sessionRevision) return
    used(revision)
    blocks.value = got
    unlocked.value = true
    pin.value = ''
    confirmPIN.value = ''
  } catch (e) {
    if (revision === sessionRevision) {
      listError.value = unlocked.value
      failed(e)
    }
  } finally {
    busy.value = false
  }
}

// reset replaces a forgotten PIN: the engine has the user sign in to Windows
// first, and the list stays locked, so the new PIN is entered to open it.
async function reset() {
  if (cannotSubmit.value || advancePIN()) return
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
    pinStep.value = 'new'
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
    listError.value = false
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
    if (isActive() && revision === sessionRevision)
      notify({
        id: PRIVATE_NOTICE,
        kind: 'success',
        title: t('blocked.modeChanged', { name: item.name }),
        duration: 8000,
        actions: [
          {
            label: t('blocked.undo'),
            run: () => {
              void undoMode(item, revision)
            },
          },
        ],
      })
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
    // The engine keeps what the rule covered (a face's look-alikes, an
    // event's members) for a short while, so every kind can be put back.
    const receipt = await engine.unblock(item.id)
    if (isActive() && revision === sessionRevision)
      notify({
        id: PRIVATE_NOTICE,
        kind: 'success',
        title: t('blocked.unblocked', { name: item.name }),
        duration: 8000,
        actions: [
          {
            label: t('blocked.undo'),
            run: () => {
              void restoreBlock(receipt, revision)
            },
          },
        ],
      })
    used(revision)
    await reload(revision)
  } catch (e) {
    if (revision === sessionRevision) failed(e)
  } finally {
    busy.value = false
  }
}

async function restoreBlock(receipt: number, revision: number) {
  if (!isActive() || revision !== sessionRevision || busy.value) return
  dismiss(PRIVATE_NOTICE)
  busy.value = true
  error.value = ''
  try {
    // Re-hiding needs no PIN: Undo works even after the list relocked.
    await engine.undoUnblock(receipt)
    if (unlocked.value) await reload(revision)
  } catch (e) {
    if (revision === sessionRevision) failed(e)
  } finally {
    busy.value = false
  }
}

async function undoMode(item: Block, revision: number) {
  if (!isActive() || !unlocked.value || revision !== sessionRevision || busy.value) return
  dismiss(PRIVATE_NOTICE)
  busy.value = true
  try {
    await engine.setBlockMode(item.id, item.mode)
    await reload(revision)
  } catch (e) {
    if (revision === sessionRevision) failed(e)
  } finally {
    busy.value = false
  }
}
async function retryList() {
  const revision = sessionRevision
  busy.value = true
  error.value = ''
  try {
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
      return t('blocked.assets')
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

const kinds = [
  BlockKind.BlockPerson,
  BlockKind.BlockTag,
  BlockKind.BlockPlace,
  BlockKind.BlockEvent,
  BlockKind.BlockAlbum,
  BlockKind.BlockFolder,
  BlockKind.BlockAsset,
  BlockKind.BlockFace,
]
const tabs = computed(() => [
  { value: '' as const, label: t('blocked.all'), count: blocks.value.length },
  ...kinds.map((kind) => ({
    value: kind,
    label: kindName(kind),
    count: blocks.value.filter((item) => item.kind === kind).length,
  })),
])
const groups = computed(() => {
  const needle = query.value.trim().toLocaleLowerCase()
  return kinds
    .filter((kind) => !filterKind.value || filterKind.value === kind)
    .map((kind) => ({
      kind,
      items: blocks.value.filter(
        (item) => item.kind === kind && (!needle || item.name.toLocaleLowerCase().includes(needle)),
      ),
    }))
    .filter((group) => group.items.length)
})
function pickKind(kind: BlockKind | '') {
  filterKind.value = kind
  used(sessionRevision)
}
function clearFilters() {
  query.value = ''
  filterKind.value = ''
}
const managerScroller = ref<HTMLElement | null>(null)
const rows = computed(() =>
  groups.value.flatMap((group) => [
    { key: group.kind, kind: group.kind, item: null },
    ...group.items.map((item) => ({ key: String(item.id), kind: group.kind, item })),
  ]),
)
const virtual = computed(() => rows.value.length > 100)
const virtualizer = useVirtualizer(
  computed(() => ({
    count: virtual.value ? rows.value.length : 0,
    getScrollElement: () => managerScroller.value,
    estimateSize: (index: number) => (rows.value[index]?.item ? 112 : 40),
    overscan: 5,
  })),
)
function measureRow(element: Element | ComponentPublicInstance | null) {
  if (virtual.value && element instanceof HTMLElement) virtualizer.value.measureElement(element)
}
const shownRows = computed(() =>
  virtual.value
    ? virtualizer.value.getVirtualItems().flatMap((row) => {
        const entry = rows.value[row.index]
        return entry ? [{ ...entry, top: row.start, index: row.index }] : []
      })
    : rows.value.map((entry, index) => ({ ...entry, top: 0, index })),
)

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
  <Page :scroll="!unlocked">
    <template #header>
      <PageHeader
        :title="t('blocked.title')"
        :subtitle="unlocked ? t('blocked.count', { n: blocks.length }) : t('blocked.subtitleLocked')"
        :back="{ to: '/settings', label: t('blocked.back') }"
      >
        <template v-if="unlocked && !covered.block" #search>
          <UInput
            v-model="query"
            type="search"
            :icon="icons.search"
            :aria-label="t('blocked.search')"
            :placeholder="t('blocked.search')"
            @update:model-value="used(sessionRevision)"
          />
        </template>
        <template v-if="unlocked">
          <span class="session">{{ t('blocked.session', { n: BLOCK_UNLOCK_IDLE_MINUTES }) }}</span>
          <UButton :icon="icons.lock" color="neutral" variant="outline" @click="runCommand('blocked.lock')">{{
            t('blocked.lock')
          }}</UButton>
        </template>
        <span v-else-if="configured" class="scope"><Icon name="lock" :size="14" />{{ t('blocked.protected') }}</span>
      </PageHeader>
    </template>
    <template v-if="unlocked && !covered.block" #toolbar>
      <ViewBar :label="t('blocked.filter')">
        <Segmented
          :model-value="filterKind"
          :items="tabs"
          :label="t('blocked.filter')"
          look="tabs"
          @update:model-value="pickKind"
        />
      </ViewBar>
    </template>
    <FeatureState v-if="loading" kind="loading" :title="t('blocked.loading')" />
    <FeatureState v-else-if="configurationError" kind="error" :title="error">
      <UButton @click="checkConfiguration">{{ t('blocked.retry') }}</UButton>
    </FeatureState>
    <div v-else-if="!unlocked" class="lock-wrap">
      <form :ref="setPinForm" class="pin-form" @submit.prevent="resetting ? reset() : unlock()">
        <header class="lock-heading">
          <span class="lock-art"><Icon :name="resetting ? 'key' : 'blocked'" :size="24" /></span>
          <div>
            <h2>{{ t(resetting ? 'blocked.resetTitle' : configured ? 'blocked.locked' : 'blocked.createPIN') }}</h2>
            <p>
              {{
                creating ? t('pin.required', { pinMin: PIN_MIN_LEN, pinMax: PIN_MAX_LEN }) : t('blocked.enterPINHint')
              }}
            </p>
          </div>
        </header>
        <p v-if="resetting" class="hint">{{ t('blocked.resetHint') }}</p>
        <p v-if="changed" role="status">{{ t('blocked.pinChanged') }}</p>
        <div v-if="creating" class="steps" :aria-label="t('pin.steps')">
          <span :aria-current="pinStep === 'new' ? 'step' : undefined">1 · {{ t('blocked.newPIN') }}</span
          ><span aria-hidden="true">—</span
          ><span :aria-current="pinStep === 'confirm' ? 'step' : undefined">2 · {{ t('blocked.confirmPIN') }}</span>
        </div>
        <PinFields
          v-model:pin="pin"
          v-model:confirm="confirmPIN"
          :creating="creating"
          :step="creating ? pinStep : undefined"
          keypad
          :disabled="busy || cooldown > 0"
          :error="pinError"
          @edit="error = ''"
        />
        <span v-if="error" data-blocked-error class="sr-only">{{ error }}</span>
        <p v-if="busy && resetting" role="status">{{ t('pin.waitingWindows') }}</p>
        <UButton
          type="submit"
          class="submit"
          color="primary"
          variant="solid"
          :disabled="cannotSubmit"
          :loading="busy"
          >{{
            busy
              ? t('pin.checking')
              : creating && pinStep === 'new'
                ? t('pin.continue')
                : resetting
                  ? t('blocked.reset')
                  : creating
                    ? t('pin.createUnlock')
                    : t('blocked.unlock')
          }}</UButton
        >
        <UButton
          v-if="creating && (resetting || pinStep === 'confirm')"
          type="button"
          data-blocked="cancel-reset"
          color="neutral"
          variant="ghost"
          :disabled="busy"
          @click="backPIN"
          >{{ pinStep === 'confirm' ? t('pin.back') : t('blocked.resetCancel') }}</UButton
        >
        <UButton
          v-else-if="configured"
          type="button"
          class="forgot"
          data-blocked="forgot"
          color="neutral"
          variant="ghost"
          :disabled="busy"
          @click="openReset"
          >{{ t('blocked.forgotPIN') }}</UButton
        >
      </form>
      <p class="privacy"><Icon name="info" :size="16" />{{ t('blocked.privacy') }}</p>
    </div>
    <CoveredBrowser v-else-if="covered.block" :covered="covered" />
    <div v-else class="manager">
      <UAlert v-if="error" data-blocked-error color="error" variant="subtle" :title="error" role="alert"
        ><template #actions
          ><UButton :disabled="busy" @click="retryList">{{ t('blocked.retry') }}</UButton></template
        ></UAlert
      >
      <header>
        <h2>{{ t('blocked.managerTitle') }}</h2>
        <p>{{ t('blocked.managerHint') }}</p>
      </header>
      <FeatureState v-if="busy && !blocks.length" kind="loading" :title="t('blocked.loading')" />
      <div
        v-else-if="rows.length"
        ref="managerScroller"
        data-block-scroller
        class="manager-scroller"
        @scroll.passive="used(sessionRevision)"
      >
        <ul
          ref="blockList"
          class="list"
          :class="{ virtual }"
          :style="virtual ? { height: virtualizer.getTotalSize() + 'px' } : undefined"
        >
          <li
            v-for="row in shownRows"
            :key="row.key"
            :ref="measureRow"
            :class="row.item ? 'block-entry' : 'kind-heading'"
            :data-block-row="row.item?.id"
            :data-index="row.index"
            :style="
              virtual
                ? {
                    position: 'absolute',
                    transform: `translateY(${row.top}px)`,
                    width: '100%',
                  }
                : undefined
            "
          >
            <template v-if="row.item">
              <!-- What the rule hides, shown by its face or newest photo: the
              list is read only while the screen is unlocked, as the media is. -->
              <span class="block-art" :class="{ round: row.item.face.id > 0 }" data-block-cover>
                <FaceThumb v-if="row.item.face.id" :face="row.item.face" :size="48" round fill />
                <img
                  v-else-if="row.item.cover && row.item.coverThumb"
                  :src="thumbUrl(row.item.cover, row.item.coverThumb)"
                  alt=""
                  loading="lazy"
                  decoding="async"
                  draggable="false"
                />
                <Icon v-else name="blocked" :size="20" />
              </span>
              <div class="item-name">
                <strong :title="row.item.name">{{ row.item.name }}</strong
                ><small>{{ kindName(row.kind) }} · {{ t('covered.count', { n: row.item.count }) }}</small>
              </div>
              <span class="scope">{{
                t(row.item.mode === BlockMode.BlockPhotos ? 'blocked.labelPhotos' : 'blocked.labelOnly')
              }}</span>
              <div class="block-actions">
                <UButton
                  data-blocked="show"
                  :data-block-id="row.item.id"
                  :icon="icons.eye"
                  color="neutral"
                  variant="ghost"
                  :disabled="busy"
                  :aria-label="t('blocked.actionFor', { action: t('covered.show'), name: row.item.name })"
                  @click="runCommand(`blocked.show.${row.item.id}`)"
                  >{{ t('covered.show') }}</UButton
                >
                <UButton
                  v-if="row.kind !== BlockKind.BlockAsset && row.kind !== BlockKind.BlockFolder"
                  data-blocked="mode"
                  color="neutral"
                  variant="ghost"
                  :disabled="busy"
                  :aria-label="t('blocked.actionFor', { action: t('blocked.switchMode'), name: row.item.name })"
                  @click="runCommand(`blocked.mode.${row.item.id}`)"
                  >{{ t('blocked.switchMode') }}</UButton
                >
                <UButton
                  data-blocked="unblock"
                  color="neutral"
                  variant="ghost"
                  :disabled="busy"
                  :aria-label="t('blocked.actionFor', { action: t('blocked.unblock'), name: row.item.name })"
                  @click="runCommand(`blocked.unblock.${row.item.id}`)"
                  >{{ t('blocked.unblock') }}</UButton
                >
              </div>
            </template>
            <h3 v-else>{{ kindName(row.kind) }}</h3>
          </li>
        </ul>
      </div>
      <FeatureState
        v-else-if="!listError"
        kind="empty"
        icon="blocked"
        :title="t(blocks.length ? 'blocked.noMatches' : 'blocked.empty')"
        :text="t(blocks.length ? 'blocked.noMatchesHint' : 'blocked.emptyHint')"
      >
        <UButton v-if="blocks.length" color="neutral" variant="outline" @click="clearFilters()">{{
          t('blocked.showAll')
        }}</UButton>
        <UButton v-else to="/settings" color="neutral" variant="outline">{{ t('blocked.back') }}</UButton>
      </FeatureState>
      <UAlert color="neutral" variant="subtle" :icon="icons.info" :title="t('blocked.privacy')" />
      <footer>
        <p>{{ t('blocked.modeHint') }}</p>
        <UButton :icon="icons.key" color="neutral" variant="ghost" :disabled="busy" @click="openReset">{{
          t('blocked.changePIN')
        }}</UButton>
      </footer>
    </div>
    <template #dock>
      <SelectionDock
        :count="covered.selected.size"
        :summary="t('blocked.selected', { n: covered.selected.size })"
        @clear="runCommand('blocked.covered.clear')"
      >
        <UButton
          :disabled="covered.trashing"
          :loading="covered.trashing"
          :icon="icons.trash"
          color="error"
          variant="ghost"
          @click="runCommand('blocked.covered.trash')"
          >{{ t('trash.move') }}</UButton
        >
      </SelectionDock>
    </template>
  </Page>
</template>

<style scoped>
h2,
h3,
p {
  margin: 0;
}
h2 {
  color: var(--ui-text-highlighted);
  font: 600 1.125rem/1.3 var(--font-display);
  text-wrap: balance;
}
p {
  color: var(--ui-text-dimmed);
  font-size: 0.8125rem;
  text-wrap: pretty;
}
.lock-wrap {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  min-height: 100%;
  padding: 2rem 1.5rem;
  gap: 1.25rem;
}
.pin-form {
  display: grid;
  gap: 1.25rem;
  width: min(100%, 28rem);
  padding: 2rem;
  border: 1px solid var(--ui-border);
  border-radius: var(--lumen-r-lg);
  background: var(--lumen-bg-raised);
  box-shadow: var(--lumen-shadow-1);
}
.lock-heading {
  display: flex;
  gap: 1rem;
  align-items: center;
}
.lock-heading p {
  margin-top: 0.25rem;
}
.lock-art,
.block-art {
  display: grid;
  place-items: center;
  flex: none;
  width: 3rem;
  height: 3rem;
  overflow: hidden;
  border-radius: var(--lumen-r-md);
  color: var(--ui-text-toned);
  background: var(--lumen-bg-sunken);
}
.block-art.round {
  border-radius: 50%;
}
.block-art img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}
.privacy {
  display: flex;
  gap: 0.5rem;
  max-width: 28rem;
  text-align: center;
}
.privacy :deep(svg) {
  flex: none;
}
.hint {
  padding: 0.75rem;
  border-radius: var(--lumen-r-sm);
  background: var(--lumen-bg-sunken);
  color: var(--ui-text-toned);
}
.steps {
  display: flex;
  gap: 0.75rem;
  justify-content: center;
  color: var(--ui-text-dimmed);
  font-size: 0.75rem;
}
.steps [aria-current] {
  color: var(--lumen-primary-text);
  font-weight: 600;
}
.forgot {
  justify-content: center;
}
.submit {
  justify-content: center;
  min-height: 2.75rem;
}
.session {
  color: var(--ui-text-dimmed);
  font-size: 0.75rem;
}
.scope {
  display: inline-flex;
  align-items: center;
  gap: 0.375rem;
  padding: 0.25rem 0.5rem;
  border: 1px solid var(--ui-border);
  border-radius: var(--lumen-r-sm);
  background: var(--lumen-bg-sunken);
  color: var(--ui-text-toned);
  font-size: 0.6875rem;
  white-space: nowrap;
}
.manager {
  display: flex;
  flex-direction: column;
  gap: 1.25rem;
  height: 100%;
  min-height: 0;
  overflow: auto;
  padding: 1.5rem var(--lumen-page-x);
}
.manager > :not(.manager-scroller) {
  flex-shrink: 0;
}
.manager header p {
  margin-top: 0.5rem;
}
.manager-scroller {
  overflow: auto;
  flex: 1 1 auto;
  min-height: 8rem;
}
.list {
  position: relative;
  list-style: none;
  padding: 0;
  margin: 0;
}
.block-entry {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 0.75rem;
  padding: 1rem;
  border: 1px solid var(--ui-border);
  border-radius: var(--lumen-r-md);
  background: var(--lumen-bg-raised);
}
.kind-heading {
  display: flex;
  align-items: center;
  min-height: 2.5rem;
  padding: 0.5rem 0;
}
.kind-heading h3 {
  font-size: 0.8125rem;
  font-weight: 600;
  color: var(--ui-text-toned);
}
.item-name {
  display: grid;
  gap: 0.25rem;
  flex: 1 1 20%;
  min-width: 0;
}
.item-name strong {
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
  color: var(--ui-text-highlighted);
  font-size: 0.8125rem;
}
.item-name small {
  color: var(--ui-text-dimmed);
  font-size: 0.75rem;
}
.block-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 0.25rem;
}
.manager footer {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  flex-wrap: wrap;
}
.manager footer p {
  flex: 1 1 20rem;
}
@container (max-width: 900px) {
  .session {
    display: none;
  }
  .block-actions {
    width: 100%;
  }
}
@media (max-height: 600px) {
  .lock-wrap {
    justify-content: flex-start;
    padding: 1rem;
  }
  .pin-form {
    padding: 1.25rem;
    width: min(100%, 44rem);
    gap: 0.75rem 1.5rem;
    grid-template-columns: minmax(0, 1fr) auto;
  }
  .pin-form > * {
    grid-column: 1;
  }
  .pin-form :deep(.pin-fields.with-pad) {
    display: contents;
  }
  .pin-form :deep(.pinpad) {
    grid-column: 2;
    grid-row: 1 / span 6;
    align-self: center;
    grid-template-columns: repeat(3, 4rem);
  }
}
</style>
