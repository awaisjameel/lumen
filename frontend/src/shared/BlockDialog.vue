<script setup lang="ts">
import { nextTick, ref, watch } from 'vue'
import { BlockMode, engine } from '../bridge'
import { useLibrary } from '../library/store'
import { blockRequest, showBlockUndo } from './block'
import type { BlockRequest } from './block'
import { errorText, reportError } from './errors'
import { t } from './i18n'

const lib = useLibrary()
const dialog = ref<HTMLDialogElement | null>(null)
const mode = ref<BlockMode>(BlockMode.BlockPhotos)
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
  try {
    if (!configured.value) {
      await engine.createBlockPIN(pin.value)
      if (blockRequest.value !== request) return
      configured.value = true
    }
    const change = await engine.setBlock(request.kind, request.targetID, request.path, requestedMode)
    if (change.created) showBlockUndo(change.id, request.name)
    close(request, true)
    void lib.refreshVisibility().catch(reportError)
  } catch (e) {
    if (blockRequest.value === request) error.value = errorText(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <dialog ref="dialog" class="block-dialog" @cancel.prevent="cancel">
    <form v-if="blockRequest" @submit.prevent="save">
      <h2>{{ t('blocked.confirmTitle', { name: blockRequest.name }) }}</h2>
      <p>{{ t('blocked.confirmText') }}</p>
      <fieldset v-if="blockRequest.labelOnly && blockRequest.photosAllowed">
        <legend>{{ t('blocked.mode') }}</legend>
        <label><input v-model="mode" type="radio" :value="BlockMode.BlockLabel" />{{ t('blocked.labelOnly') }}</label>
        <label
          ><input v-model="mode" type="radio" :value="BlockMode.BlockPhotos" />{{ t('blocked.labelPhotos') }}</label
        >
      </fieldset>
      <p v-else>{{ t(blockRequest.photosAllowed ? 'blocked.photosOnly' : 'blocked.labelOnly') }}</p>
      <template v-if="!configured">
        <p>{{ t('blocked.createPINHint') }}</p>
        <input
          v-model="pin"
          class="field"
          type="password"
          inputmode="numeric"
          autocomplete="new-password"
          pattern="[0-9]{6,12}"
          required
          :aria-label="t('blocked.newPIN')"
          :placeholder="t('blocked.newPIN')"
        />
        <input
          v-model="confirmPIN"
          class="field"
          type="password"
          inputmode="numeric"
          autocomplete="new-password"
          pattern="[0-9]{6,12}"
          required
          :aria-label="t('blocked.confirmPIN')"
          :placeholder="t('blocked.confirmPIN')"
        />
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
  border: 1px solid var(--border);
  border-radius: var(--radius-l);
  background: var(--surface);
  color: var(--text);
  box-shadow: var(--shadow);
}

.block-dialog::backdrop {
  background: var(--backdrop);
}

h2 {
  margin: 0 0 8px;
  font: 600 17px var(--font-display);
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

legend {
  margin-bottom: 8px;
  font-weight: 600;
}

label {
  display: flex;
  align-items: center;
  gap: 8px;
}

.field {
  display: block;
  width: 100%;
  margin: 8px 0;
}

.actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 20px;
}
</style>
