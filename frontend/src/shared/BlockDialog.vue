<script setup lang="ts">
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UModal from '@nuxt/ui/components/Modal.vue'
import URadioGroup from '@nuxt/ui/components/RadioGroup.vue'
import { computed, ref, useId, watch } from 'vue'
import { BlockKind, BlockMode, engine, errorCode, ErrorCode, PIN_MAX_LEN, PIN_MIN_LEN, PIN_PATTERN } from '../bridge'
import { blockRequest, showBlocksChanged, showBlockUndo } from './block'
import type { BlockRequest } from './block'
import { errorText, reportError } from './errors'
import { t } from './i18n'
import PinFields from './PinFields.vue'
import Icon from './Icon.vue'
import { notify } from './notify'

const formID = useId()
const pinStep = ref(false)
const configurationFailed = ref(false)
const ready = ref(false)
let opener: HTMLElement | null = null
const mode = ref<BlockMode>(BlockMode.BlockPhotos)
const lookalikes = ref(false) // a face: block faces like it too
const configured = ref(true)
const pin = ref('')
const confirmPIN = ref('')
const error = ref('')
const busy = ref(false)
const batchCancelled = ref(false)
const isBatchCancelled = () => batchCancelled.value

watch(blockRequest, async (request, previous) => {
  ready.value = false
  pin.value = ''
  confirmPIN.value = ''
  pinStep.value = false
  configurationFailed.value = false
  if (!request) {
    return
  }
  if (!previous) opener = document.activeElement instanceof HTMLElement ? document.activeElement : null
  mode.value = request.photosAllowed ? BlockMode.BlockPhotos : BlockMode.BlockLabel
  lookalikes.value = false
  configured.value = true
  pin.value = ''
  confirmPIN.value = ''
  error.value = ''
  try {
    const got = await engine.blockPINConfigured()
    if (blockRequest.value !== request) return
    configured.value = got
  } catch (e) {
    if (blockRequest.value !== request) return
    configurationFailed.value = true
    error.value = errorText(e)
  }
  if (blockRequest.value !== request) return
  ready.value = true
})

const open = computed({
  get: () => ready.value && blockRequest.value !== null,
  set: (value) => {
    if (!value) cancel()
  },
})
const modeChoices = computed(() => [
  { value: BlockMode.BlockLabel, label: t('block.hideName'), description: t('block.labelHint') },
  { value: BlockMode.BlockPhotos, label: t('block.hidePhotos'), description: t('block.photosHint') },
])
// Radio values are strings; the request still receives its original boolean.
const faceScope = computed({
  get: () => (lookalikes.value ? 'similar' : 'one'),
  set: (value: string) => {
    lookalikes.value = value === 'similar'
  },
})
const faceChoices = computed(() => [
  { value: 'one', label: t('blocked.faceOnly') },
  { value: 'similar', label: t('blocked.faceLookalikes'), description: t('blocked.faceLookalikesHint') },
])

async function retryConfiguration() {
  const request = blockRequest.value
  if (!request || busy.value) return
  busy.value = true
  try {
    const got = await engine.blockPINConfigured()
    if (blockRequest.value !== request) return
    configured.value = got
    configurationFailed.value = false
    error.value = ''
  } catch (e) {
    if (blockRequest.value === request) error.value = errorText(e)
  } finally {
    busy.value = false
  }
}

function backScope() {
  pinStep.value = false
  error.value = ''
}

function returnFocus(event: Event) {
  event.preventDefault()
  if (blockRequest.value === null && opener?.isConnected) opener.focus({ preventScroll: true })
}

function close(request: BlockRequest, saved: boolean) {
  request.answer(saved)
  if (blockRequest.value === request) blockRequest.value = null
}

function cancel() {
  if (busy.value && blockRequest.value?.ids) batchCancelled.value = true
  else if (!busy.value && blockRequest.value) close(blockRequest.value, false)
}

async function save() {
  const request = blockRequest.value
  if (!request || busy.value || configurationFailed.value) return
  if (!configured.value && !pinStep.value) {
    pinStep.value = true
    return
  }
  if (
    !configured.value &&
    (!new RegExp(`^${PIN_PATTERN}$`).test(pin.value) || !new RegExp(`^${PIN_PATTERN}$`).test(confirmPIN.value))
  ) {
    error.value = t('pin.required', { pinMin: PIN_MIN_LEN, pinMax: PIN_MAX_LEN })
    return
  }
  error.value = ''
  if (!configured.value && pin.value !== confirmPIN.value) {
    error.value = t('blocked.pinMismatch')
    return
  }
  busy.value = true
  batchCancelled.value = false
  const requestedMode = mode.value
  const requestedLookalikes = lookalikes.value
  try {
    if (!configured.value) {
      await engine.createBlockPIN(pin.value)
      if (blockRequest.value !== request) return
      configured.value = true
    }
    if (request.ids) {
      let saved = 0
      for (const id of request.ids) {
        if (blockRequest.value !== request || isBatchCancelled() || request.stopped?.()) break
        try {
          await engine.setBlock(BlockKind.BlockAsset, id, '', BlockMode.BlockPhotos)
          saved++
          request.outcome?.(id, '')
        } catch (e) {
          request.outcome?.(id, errorText(e))
        }
      }
      if (saved)
        notify({
          id: 'block.batch',
          kind: 'success',
          title: t('files.blockedCount', { n: saved }),
          duration: 8000,
        })
      close(request, saved > 0)
      void showBlocksChanged().catch(reportError)
      return
    }
    const change =
      request.kind === BlockKind.BlockFace
        ? await engine.blockFace(request.targetID, requestedMode, requestedLookalikes)
        : await engine.setBlock(request.kind, request.targetID, request.path, requestedMode)
    if (change.created) showBlockUndo(change.id, request.name)
    close(request, true)
    void showBlocksChanged().catch(reportError)
  } catch (e) {
    if (blockRequest.value === request) {
      error.value = errorText(e)
      if (errorCode(e) === ErrorCode.PINExists) {
        configured.value = true
        pinStep.value = false
        pin.value = ''
        confirmPIN.value = ''
      }
    }
    if (request.ids) void showBlocksChanged().catch(reportError)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <UModal
    v-model:open="open"
    :title="blockRequest ? t('blocked.confirmTitle', { name: blockRequest.name }) : ''"
    :description="t('blocked.confirmText')"
    :close="false"
    :dismissible="!busy"
    :content="{ onCloseAutoFocus: returnFocus }"
    :ui="{ content: 'max-w-md', title: 'flex items-center gap-3 text-lg' }"
  >
    <template #title
      ><Icon name="blocked" :size="24" /><span>{{
        blockRequest ? t('blocked.confirmTitle', { name: blockRequest.name }) : ''
      }}</span></template
    >
    <template #body>
      <form v-if="blockRequest" :id="formID" data-block-form class="grid gap-5" @submit.prevent="save">
        <URadioGroup
          v-if="!pinStep && blockRequest.kind === BlockKind.BlockFace"
          v-model="faceScope"
          :legend="t('blocked.faceScope')"
          :items="faceChoices"
          variant="card"
          :disabled="busy"
        />
        <URadioGroup
          v-if="!pinStep && blockRequest.labelOnly && blockRequest.photosAllowed"
          v-model="mode"
          :legend="t('blocked.mode')"
          :items="modeChoices"
          variant="card"
          :disabled="busy"
        />
        <p v-else-if="!pinStep" class="text-muted">
          {{ t(blockRequest.photosAllowed ? 'blocked.photosOnly' : 'blocked.labelOnly') }}
        </p>
        <p v-if="!pinStep" class="block-hint">{{ t('block.manageHint') }}</p>
        <template v-if="pinStep">
          <h3 class="font-semibold text-highlighted">{{ t('block.createTitle') }}</h3>
          <p class="text-muted">{{ t('blocked.createPINHint', { pinMin: PIN_MIN_LEN, pinMax: PIN_MAX_LEN }) }}</p>
          <PinFields
            v-model:pin="pin"
            v-model:confirm="confirmPIN"
            creating
            :disabled="busy"
            :error="error"
            @edit="error = ''"
          />
        </template>
        <UAlert v-if="error && !pinStep" color="error" variant="subtle" :title="error" role="alert"
          ><template v-if="configurationFailed" #actions
            ><UButton :disabled="busy" @click="retryConfiguration">{{ t('blocked.retry') }}</UButton></template
          ></UAlert
        >
      </form>
    </template>
    <template #footer>
      <div v-if="blockRequest" class="lumen-dialog-foot flex w-full items-center border-0" :aria-busy="busy">
        <UButton
          v-if="pinStep && !configured"
          type="button"
          color="neutral"
          variant="ghost"
          :disabled="busy"
          @click="backScope()"
          >{{ t('pin.back') }}</UButton
        >
        <UButton
          type="button"
          data-block="cancel"
          autofocus
          color="neutral"
          variant="outline"
          :disabled="busy && !blockRequest.ids"
          @click="cancel"
          >{{ t('common.cancel') }}</UButton
        >
        <UButton
          type="submit"
          :form="formID"
          data-block="save"
          color="primary"
          variant="solid"
          :disabled="busy || configurationFailed"
          :loading="busy"
          >{{
            busy
              ? t('block.saving')
              : !configured
                ? pinStep
                  ? t('block.createSave')
                  : t('pin.continue')
                : t('blocked.block')
          }}</UButton
        >
      </div>
    </template>
  </UModal>
</template>

<style scoped>
.block-hint {
  margin: 0;
  padding: 0.75rem;
  border-radius: var(--lumen-r-sm);
  background: var(--lumen-bg-sunken);
  color: var(--ui-text-toned);
  font-size: 0.8125rem;
}
</style>
