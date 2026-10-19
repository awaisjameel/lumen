<script setup lang="ts">
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import USwitch from '@nuxt/ui/components/Switch.vue'
import URadioGroup from '@nuxt/ui/components/RadioGroup.vue'
import UProgress from '@nuxt/ui/components/Progress.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import { computed, onMounted, ref } from 'vue'
import { UpdateChannel, UpdateFailure, UpdatePhase } from '../bridge'
import PrefRow from '../shared/PrefRow.vue'
import { LOCALE, t } from '../shared/i18n'
import type { MessageKey } from '../shared/i18n'
import { bytes } from '../shared/format'
import { confirm } from '../shared/confirm'
import { useUpdates } from './store'

const props = withDefaults(defineProps<{ query?: string }>(), { query: '' })
const updates = useUpdates()
function matches(...keys: MessageKey[]) {
  const needle = props.query.trim().toLocaleLowerCase()
  return !needle || keys.some((key) => t(key).toLocaleLowerCase().includes(needle))
}
onMounted(() => void updates.start())
const channels = [UpdateChannel.UpdateStable, UpdateChannel.UpdatePreview] as const
const items = computed(() => channels.map((value) => ({ value, label: t(`update.channel.${value}`) })))
function setChannel(value: unknown) {
  for (const channel of channels) if (value === channel) void updates.setPolicy({ channel })
}
async function restart() {
  if (await confirm(t('update.restart'), t('update.restartHint'), t('update.restart'))) await updates.restart()
}
const bandwidthError = ref('')
function setBandwidth(event: Event) {
  if (!(event.target instanceof HTMLInputElement)) return
  const bandwidthKB = event.target.valueAsNumber
  bandwidthError.value = ''
  if (Number.isInteger(bandwidthKB) && bandwidthKB >= 0 && bandwidthKB <= 102400)
    void updates.setPolicy({ bandwidthKB })
  else {
    bandwidthError.value = t('update.bandwidthRange')
    event.target.value = String(updates.status?.policy.bandwidthKB ?? 0)
  }
}
async function download() {
  const offer = updates.status?.offer
  if (!offer) return
  if (
    await confirm(
      t('update.download'),
      `${t('update.offer', { version: offer.version, download: bytes(offer.downloadBytes), expanded: bytes(offer.expandedBytes) })} ${t('update.downloadHint')}`,
      t('update.download'),
    )
  )
    await updates.download()
}
const ready = computed(() => updates.status?.phase === UpdatePhase.UpdateReady)
const offered = computed(() => !!updates.status?.offer.version)
const phaseText = computed(() =>
  updates.status && updates.status.phase !== UpdatePhase.$zero
    ? t(`update.phase.${updates.status.phase}`)
    : t('update.loading'),
)
// What the last operation left behind, in the engine's own terms (core.UpdateFailure).
const failureText: Record<UpdateFailure, MessageKey | null> = {
  [UpdateFailure.$zero]: null,
  [UpdateFailure.UpdateFailureFailed]: 'update.failed',
  [UpdateFailure.UpdateFailureInterrupted]: 'update.interrupted',
  [UpdateFailure.UpdateFailureInvalid]: 'update.invalid',
  [UpdateFailure.UpdateFailurePublisher]: 'update.publisher',
  [UpdateFailure.UpdateFailureSpace]: 'update.space',
  [UpdateFailure.UpdateFailureObserver]: null, // not a failure: said once, neutrally, below
}
const persistedError = computed(() => {
  const key = updates.status ? failureText[updates.status.error] : null
  return key ? t(key) : ''
})
const observing = computed(() => !!updates.status?.managed && !updates.status.coordinator)
const reason = computed(() =>
  !updates.status?.configured
    ? t('update.unavailable')
    : observing.value
      ? t('update.observer')
      : ready.value
        ? t('update.readyReason')
        : updates.busy
          ? t('update.checkReason')
          : '',
)
const showStatus = computed(() =>
  matches('update.title', 'update.check', 'update.restart', 'update.download', 'update.privacy'),
)
// A real moment, in the PC's own time zone (photo dates are the camera's clock instead).
const whenFmt = new Intl.DateTimeFormat(LOCALE, { dateStyle: 'medium', timeStyle: 'short' })
</script>

<template>
  <div class="updates-content">
    <div v-show="showStatus" class="update-status" data-update-status>
      <p class="text-muted">{{ t('update.privacy') }}</p>
      <UAlert
        v-if="updates.status && !updates.status.configured"
        :description="t('update.unavailable')"
        color="neutral"
      />
      <UAlert v-else-if="observing" :description="t('update.observer')" color="neutral" />
      <p v-if="updates.status" role="status" aria-live="polite">{{ phaseText }}</p>
      <p
        v-if="updates.status?.phase === UpdatePhase.UpdateIdle && !updates.status.policy.automatic"
        class="text-dimmed"
      >
        {{ t('update.off') }}
      </p>
      <p v-if="updates.status?.lastCheck" class="text-dimmed text-xs">
        {{ t('update.lastCheck', { date: whenFmt.format(updates.status.lastCheck * 1000) }) }}
      </p>
      <template v-if="offered && updates.status">
        <p>
          {{
            t('update.offer', {
              version: updates.status.offer.version,
              download: bytes(updates.status.offer.downloadBytes),
              expanded: bytes(updates.status.offer.expandedBytes),
            })
          }}
        </p>
        <p class="whitespace-pre-wrap break-words">{{ updates.status.offer.notes }}</p>
        <p class="text-dimmed text-xs">{{ t('update.storage') }}</p>
        <UProgress
          v-if="updates.status.phase === UpdatePhase.UpdateDownloading && updates.status.offer.downloadBytes"
          :model-value="updates.status.bytes"
          :max="updates.status.offer.downloadBytes"
          :aria-label="t('update.downloadProgress')"
        />
        <p v-if="updates.status.phase === UpdatePhase.UpdateDownloading" class="text-dimmed text-xs">
          {{
            t('update.downloaded', {
              done: bytes(updates.status.bytes),
              total: bytes(updates.status.offer.downloadBytes),
            })
          }}
        </p>
      </template>
      <div class="flex flex-wrap gap-2">
        <UButton
          variant="outline"
          :disabled="!updates.status?.configured || !updates.status.coordinator || updates.busy || ready"
          :title="reason"
          @click="updates.check"
          >{{ t('update.check') }}</UButton
        >
        <UButton
          v-if="offered && !ready"
          :disabled="updates.busy || !updates.status?.coordinator"
          :title="reason"
          @click="download"
          >{{ t('update.download') }}</UButton
        >
        <UButton
          v-if="ready"
          :disabled="updates.busy || !updates.status?.coordinator"
          :title="reason"
          @click="restart"
          >{{ t('update.restart') }}</UButton
        >
        <UButton
          v-if="ready"
          :disabled="!updates.status?.coordinator"
          :title="reason"
          variant="outline"
          @click="updates.dismiss"
          >{{ t('update.later') }}</UButton
        >
        <UButton v-if="updates.cancellable && updates.status?.coordinator" variant="outline" @click="updates.cancel">{{
          t(updates.status.phase === UpdatePhase.UpdateChecking ? 'update.cancelCheck' : 'update.cancelDownload')
        }}</UButton>
        <UButton
          v-if="offered && !updates.busy"
          :disabled="!updates.status?.coordinator"
          :title="reason"
          variant="ghost"
          @click="updates.discard"
          >{{ t('update.discard') }}</UButton
        >
      </div>
    </div>
    <PrefRow
      v-if="matches('update.automatic', 'update.automaticHint')"
      :label="t('update.automatic')"
      :description="t('update.automaticHint')"
    >
      <template #default="{ labelledBy, describedBy }">
        <USwitch
          :model-value="updates.status?.policy.automatic ?? false"
          :title="reason"
          :disabled="!updates.status?.configured || !updates.status.coordinator || updates.saving"
          :aria-labelledby="labelledBy"
          :aria-describedby="describedBy"
          @update:model-value="(automatic) => updates.setPolicy({ automatic })"
        />
      </template>
    </PrefRow>
    <PrefRow
      v-if="matches('update.channel', 'update.channelHint')"
      :label="t('update.channel')"
      :description="t('update.channelHint')"
    >
      <template #default="{ labelledBy, describedBy }">
        <URadioGroup
          :model-value="channels.find((value) => value === updates.status?.policy.channel)"
          :items="items"
          :title="reason"
          orientation="horizontal"
          :disabled="
            !updates.status?.configured || !updates.status.coordinator || updates.busy || ready || updates.saving
          "
          :aria-labelledby="labelledBy"
          :aria-describedby="describedBy"
          @update:model-value="setChannel"
        />
      </template>
    </PrefRow>
    <PrefRow
      v-if="matches('update.battery', 'update.batteryHint')"
      :label="t('update.battery')"
      :description="t('update.batteryHint')"
    >
      <template #default="{ labelledBy, describedBy }">
        <USwitch
          :model-value="updates.status?.policy.deferBattery ?? true"
          :title="reason"
          :disabled="!updates.status?.configured || !updates.status.coordinator || updates.saving"
          :aria-labelledby="labelledBy"
          :aria-describedby="describedBy"
          @update:model-value="(deferBattery) => updates.setPolicy({ deferBattery })"
        />
      </template>
    </PrefRow>
    <PrefRow
      v-if="matches('update.bandwidth', 'update.bandwidthHint')"
      :label="t('update.bandwidth')"
      :description="t('update.bandwidthHint')"
      :error="bandwidthError"
    >
      <template #default="{ labelledBy, describedBy }">
        <UInput
          type="number"
          :model-value="updates.status?.policy.bandwidthKB ?? 0"
          :title="reason"
          :min="0"
          :max="102400"
          :step="1"
          :disabled="!updates.status?.configured || !updates.status.coordinator || updates.saving || updates.busy"
          :aria-labelledby="labelledBy"
          :aria-describedby="describedBy"
          @change="setBandwidth"
        />
      </template>
    </PrefRow>
    <UAlert
      v-if="updates.error || persistedError"
      role="alert"
      :description="updates.error || persistedError"
      color="error"
      ><template #actions
        ><UButton v-if="!updates.status" loading-auto @click="updates.start">{{ t('common.retry') }}</UButton></template
      ></UAlert
    >
  </div>
</template>

<style scoped>
.updates-content {
  font-variant-numeric: tabular-nums;
}
.update-status {
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
  padding: 1.25rem;
  border-bottom: 1px solid var(--ui-border);
}
.update-status p {
  margin: 0;
}
</style>
