<script setup lang="ts">
// The Blocked screen's PIN: one field to enter it, or two to create one (the
// second confirms it). The engine checks the PIN; the pattern only helps.
import UFormField from '@nuxt/ui/components/FormField.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import { PIN_PATTERN } from '../bridge'
import { t } from './i18n'

defineProps<{ creating: boolean }>()
const pin = defineModel<string>('pin', { required: true })
const confirmPin = defineModel<string>('confirm', { default: '' })
</script>

<template>
  <UFormField :label="t(creating ? 'blocked.newPIN' : 'blocked.enterPIN')" class="w-full" required>
    <UInput
      v-model="pin"
      class="w-full"
      type="password"
      inputmode="numeric"
      :autocomplete="creating ? 'new-password' : 'current-password'"
      :pattern="PIN_PATTERN"
      required
      :aria-label="t(creating ? 'blocked.newPIN' : 'blocked.enterPIN')"
      :placeholder="t(creating ? 'blocked.newPIN' : 'blocked.enterPIN')"
    />
  </UFormField>
  <UFormField v-if="creating" :label="t('blocked.confirmPIN')" class="w-full" required>
    <UInput
      v-model="confirmPin"
      class="w-full"
      type="password"
      inputmode="numeric"
      autocomplete="new-password"
      :pattern="PIN_PATTERN"
      required
      :aria-label="t('blocked.confirmPIN')"
      :placeholder="t('blocked.confirmPIN')"
    />
  </UFormField>
</template>
