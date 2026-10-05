<script setup lang="ts">
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UModal from '@nuxt/ui/components/Modal.vue'
import URadioGroup from '@nuxt/ui/components/RadioGroup.vue'
import { computed, ref, watch } from 'vue'
import { BlockKind, BlockMode, engine, PIN_MAX_LEN, PIN_MIN_LEN } from '../bridge'
import { blockRequest, showBlocksChanged, showBlockUndo } from './block'
import type { BlockRequest } from './block'
import { errorText, reportError } from './errors'
import { t } from './i18n'
import PinFields from './PinFields.vue'
import { notify } from './notify'

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
  { value: BlockMode.BlockLabel, label: t('blocked.labelOnly') },
  { value: BlockMode.BlockPhotos, label: t('blocked.labelPhotos') },
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
  if (!request || busy.value) return
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
    if (blockRequest.value === request) error.value = errorText(e)
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
    :ui="{ content: 'max-w-md' }"
  >
    <template #body>
      <form v-if="blockRequest" data-block-form class="grid gap-5" @submit.prevent="save">
        <URadioGroup
          v-if="blockRequest.kind === BlockKind.BlockFace"
          v-model="faceScope"
          :legend="t('blocked.faceScope')"
          :items="faceChoices"
        />
        <URadioGroup
          v-if="blockRequest.labelOnly && blockRequest.photosAllowed"
          v-model="mode"
          :legend="t('blocked.mode')"
          :items="modeChoices"
        />
        <p v-else class="text-muted">
          {{ t(blockRequest.photosAllowed ? 'blocked.photosOnly' : 'blocked.labelOnly') }}
        </p>
        <template v-if="!configured">
          <p class="text-muted">{{ t('blocked.createPINHint', { pinMin: PIN_MIN_LEN, pinMax: PIN_MAX_LEN }) }}</p>
          <PinFields v-model:pin="pin" v-model:confirm="confirmPIN" creating />
        </template>
        <UAlert v-if="error" color="error" variant="subtle" :title="error" role="alert" />
        <div class="flex flex-wrap justify-end gap-2">
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
          <UButton type="submit" data-block="save" color="primary" variant="solid" :disabled="busy" :loading="busy">{{
            t('blocked.block')
          }}</UButton>
        </div>
      </form>
    </template>
  </UModal>
</template>
