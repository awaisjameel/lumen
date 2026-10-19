<script setup lang="ts">
// The dots and number pad edit the same real input, keeping keyboard,
// autofill and assistive technology on a single control for each PIN.
import UButton from '@nuxt/ui/components/Button.vue'
import IconButton from './IconButton.vue'
import { computed, ref, useId } from 'vue'
import { PIN_MAX_LEN, PIN_MIN_LEN, PIN_PATTERN } from '../bridge'
import { icons } from '../design/icons'
import { t } from './i18n'

const props = defineProps<{
  creating: boolean
  step?: 'new' | 'confirm'
  keypad?: boolean
  disabled?: boolean
  error?: string
}>()
const emit = defineEmits<{ edit: [] }>()
const pin = defineModel<string>('pin', { required: true })
const confirmPin = defineModel<string>('confirm', { default: '' })
const uid = useId()
const inputs = ref<HTMLInputElement[]>([])
const fields = computed(() => (props.creating ? (props.step ? [props.step] : ['new', 'confirm']) : ['enter']))
const value = (field: string) => (field === 'confirm' ? confirmPin.value : pin.value)
const label = (field: string) =>
  t(field === 'confirm' ? 'blocked.confirmPIN' : field === 'new' ? 'blocked.newPIN' : 'blocked.enterPIN')
function update(field: string, raw: string) {
  if (props.disabled) return
  const clean = raw.replace(/[^0-9]/g, '').slice(0, PIN_MAX_LEN)
  if (field === 'confirm') confirmPin.value = clean
  else pin.value = clean
  emit('edit')
}
function type(field: string, event: Event) {
  if (!(event.target instanceof HTMLInputElement)) return
  update(field, event.target.value)
  event.target.value = value(field)
}
function pad(digit: string) {
  const field = fields.value[0]
  if (!field) return
  const current = value(field)
  update(field, digit === 'clear' ? '' : digit === 'delete' ? current.slice(0, -1) : current + digit)
  inputs.value[0]?.focus()
}
</script>

<template>
  <div class="pin-fields" :class="{ 'with-pad': keypad }">
    <div v-for="field in fields" :key="field" class="field">
      <label :for="`${uid}-${field}`">{{ label(field) }}</label>
      <div class="pin-wrap" :class="{ invalid: error, disabled }" data-pin-wrap>
        <div class="dots" aria-hidden="true">
          <span
            v-for="dot in Math.max(PIN_MIN_LEN, value(field).length)"
            :key="dot"
            :class="{ filled: dot <= value(field).length }"
          />
        </div>
        <input
          :id="`${uid}-${field}`"
          ref="inputs"
          :value="value(field)"
          type="password"
          inputmode="numeric"
          :autocomplete="creating ? 'new-password' : 'current-password'"
          :pattern="PIN_PATTERN"
          :minlength="PIN_MIN_LEN"
          :maxlength="PIN_MAX_LEN"
          :aria-label="label(field)"
          :aria-invalid="Boolean(error)"
          :aria-describedby="error ? `${uid}-error` : undefined"
          :disabled="disabled"
          required
          @input="type(field, $event)"
        />
      </div>
    </div>
    <p v-if="error" :id="`${uid}-error`" data-pin-error class="pin-error" role="alert">{{ error }}</p>
    <div v-if="keypad" class="pinpad" role="group" :aria-label="t('pin.numberPad')">
      <UButton
        v-for="digit in 9"
        :key="digit"
        type="button"
        color="neutral"
        variant="outline"
        :disabled="disabled"
        :aria-label="String(digit)"
        @click="pad(String(digit))"
        >{{ digit }}</UButton
      >
      <IconButton
        type="button"
        color="neutral"
        variant="outline"
        :icon="icons.close"
        :disabled="disabled"
        :label="t('pin.clear')"
        @click="pad('clear')"
      />
      <UButton type="button" color="neutral" variant="outline" :disabled="disabled" aria-label="0" @click="pad('0')"
        >0</UButton
      >
      <IconButton
        type="button"
        color="neutral"
        variant="outline"
        :icon="icons['chevron-left']"
        :disabled="disabled"
        :label="t('pin.delete')"
        @click="pad('delete')"
      />
    </div>
  </div>
</template>

<style scoped>
.pin-fields {
  display: grid;
  gap: 1.25rem;
  min-width: 0;
}
.field {
  display: grid;
  justify-items: center;
  gap: 0.75rem;
  min-width: 0;
}
label {
  color: var(--ui-text-highlighted);
  font-size: 0.8125rem;
  font-weight: 600;
}
.pin-wrap {
  position: relative;
  display: flex;
  justify-content: center;
  max-width: 100%;
  padding: 0.75rem 1.25rem;
  border-radius: var(--lumen-r-lg);
}
.pin-wrap:focus-within {
  outline: 2px solid var(--ui-primary);
  box-shadow: 0 0 0 6px var(--lumen-primary-soft);
}
input {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  opacity: 0;
  cursor: text;
}
.dots {
  display: flex;
  justify-content: center;
  gap: 0.875rem;
}
.dots span {
  width: 0.875rem;
  height: 0.875rem;
  flex: none;
  border: 2px solid var(--ui-text-dimmed);
  border-radius: 50%;
  transition: transform var(--lumen-d-base) var(--lumen-ease-spring);
}
.dots .filled {
  background: var(--ui-text-highlighted);
  border-color: var(--ui-text-highlighted);
  transform: scale(1.1);
}
.invalid .dots {
  animation: lumen-shake var(--lumen-d-slow) var(--lumen-ease-out);
}
.invalid .dots span {
  border-color: var(--ui-error);
}
.disabled {
  opacity: 0.5;
}
.pin-error {
  margin: 0;
  color: var(--ui-error);
  font-size: 0.8125rem;
  text-align: center;
  overflow-wrap: anywhere;
}
.pinpad {
  display: grid;
  grid-template-columns: repeat(3, 4.5rem);
  gap: 0.75rem;
  justify-content: center;
}
.pinpad :deep(button) {
  height: 3.5rem;
  justify-content: center;
  border-radius: var(--lumen-r-xl);
  background: var(--lumen-bg-raised);
  font-size: 1.25rem;
  font-variant-numeric: tabular-nums;
}
@media (max-height: 600px) {
  .with-pad {
    grid-template-columns: minmax(0, 1fr) auto;
    align-items: center;
    gap: 0.75rem 1.5rem;
  }
  .with-pad .field,
  .with-pad .pin-error {
    grid-column: 1;
  }
  .pinpad {
    grid-column: 2;
    grid-row: 1 / span 2;
    grid-template-columns: repeat(3, 3rem);
    gap: 0.5rem;
  }
  .pinpad :deep(button) {
    height: 2.5rem;
  }
}
@container (max-width: 450px) {
  .dots {
    gap: 0.5rem;
  }
}
</style>
