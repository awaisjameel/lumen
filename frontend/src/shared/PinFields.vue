<script setup lang="ts">
// The Blocked screen's PIN: one field to enter it, or two to create one (the
// second confirms it). The engine checks the PIN; the pattern only helps.
import { PIN_PATTERN } from '../bridge'
import { t } from './i18n'

defineProps<{ creating: boolean }>()
const pin = defineModel<string>('pin', { required: true })
const confirmPin = defineModel<string>('confirm', { default: '' })
</script>

<template>
  <input
    v-model="pin"
    class="field"
    type="password"
    inputmode="numeric"
    :autocomplete="creating ? 'new-password' : 'current-password'"
    :pattern="PIN_PATTERN"
    required
    :aria-label="t(creating ? 'blocked.newPIN' : 'blocked.enterPIN')"
    :placeholder="t(creating ? 'blocked.newPIN' : 'blocked.enterPIN')"
  />
  <input
    v-if="creating"
    v-model="confirmPin"
    class="field"
    type="password"
    inputmode="numeric"
    autocomplete="new-password"
    :pattern="PIN_PATTERN"
    required
    :aria-label="t('blocked.confirmPIN')"
    :placeholder="t('blocked.confirmPIN')"
  />
</template>
