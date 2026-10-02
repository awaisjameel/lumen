<script setup lang="ts">
import { nextTick, ref, watch } from 'vue'
import { BlockKind, BlockMode, engine, PIN_MAX_LEN, PIN_MIN_LEN } from '../bridge'
import { blockRequest, showBlocksChanged, showBlockUndo } from './block'
import type { BlockRequest } from './block'
import { errorText, reportError } from './errors'
import { t } from './i18n'
import PinFields from './PinFields.vue'

const dialog = ref<HTMLDialogElement | null>(null)
const mode = ref<BlockMode>(BlockMode.BlockPhotos)
const lookalikes = ref(false) // a face: block faces like it too
const configured = ref(true)
const pin = ref('')
const confirmPIN = ref('')
const error = ref('')
const busy = ref(false)

watch(blockRequest, async (request) => {
  if (!request) {
    dialog.value?.close()
    return
  }
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
  await nextTick()
  if (!dialog.value?.open) dialog.value?.showModal()
})

function close(request: BlockRequest, saved: boolean) {
  request.answer(saved)
  if (blockRequest.value === request) blockRequest.value = null
}

function cancel() {
  if (!busy.value && blockRequest.value) close(blockRequest.value, false)
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
  const requestedMode = mode.value
  const requestedLookalikes = lookalikes.value
  try {
    if (!configured.value) {
      await engine.createBlockPIN(pin.value)
      if (blockRequest.value !== request) return
      configured.value = true
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
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <dialog ref="dialog" class="dialog block-dialog" aria-labelledby="block-title" @cancel.prevent="cancel">
    <form v-if="blockRequest" @submit.prevent="save">
      <h2 id="block-title">{{ t('blocked.confirmTitle', { name: blockRequest.name }) }}</h2>
      <p>{{ t('blocked.confirmText') }}</p>
      <fieldset v-if="blockRequest.kind === BlockKind.BlockFace">
        <legend>{{ t('blocked.faceScope') }}</legend>
        <label><input v-model="lookalikes" type="radio" :value="false" />{{ t('blocked.faceOnly') }}</label>
        <label><input v-model="lookalikes" type="radio" :value="true" />{{ t('blocked.faceLookalikes') }}</label>
        <p class="hint">{{ t('blocked.faceLookalikesHint') }}</p>
      </fieldset>
      <fieldset v-if="blockRequest.labelOnly && blockRequest.photosAllowed">
        <legend>{{ t('blocked.mode') }}</legend>
        <label><input v-model="mode" type="radio" :value="BlockMode.BlockLabel" />{{ t('blocked.labelOnly') }}</label>
        <label
          ><input v-model="mode" type="radio" :value="BlockMode.BlockPhotos" />{{ t('blocked.labelPhotos') }}</label
        >
      </fieldset>
      <p v-else>{{ t(blockRequest.photosAllowed ? 'blocked.photosOnly' : 'blocked.labelOnly') }}</p>
      <template v-if="!configured">
        <p>{{ t('blocked.createPINHint', { pinMin: PIN_MIN_LEN, pinMax: PIN_MAX_LEN }) }}</p>
        <PinFields v-model:pin="pin" v-model:confirm="confirmPIN" creating />
      </template>
      <p v-if="error" class="page-error" role="alert">{{ error }}</p>
      <div class="actions">
        <button type="button" class="btn" :disabled="busy" @click="cancel">{{ t('common.cancel') }}</button>
        <button class="btn btn-primary" :disabled="busy">{{ t('blocked.block') }}</button>
      </div>
    </form>
  </dialog>
</template>

<style scoped>
.block-dialog {
  width: min(440px, calc(100vw - 32px));
  padding: 20px;
}

p {
  color: var(--text-2);
}

fieldset {
  display: grid;
  gap: 8px;
  margin: 16px 0;
  border: 0;
  padding: 0;
}

.hint {
  margin: 0;
  font-size: 12px;
}

legend {
  margin-bottom: 8px;
  font-weight: 600;
}

label {
  display: flex;
  align-items: center;
  gap: 8px;
}

:deep(.field) {
  display: block;
  width: 100%;
  margin: 8px 0;
}

.actions {
  margin-top: 20px;
}
</style>
