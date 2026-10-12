<script setup lang="ts">
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import USwitch from '@nuxt/ui/components/Switch.vue'
import URadioGroup from '@nuxt/ui/components/RadioGroup.vue'
import UProgress from '@nuxt/ui/components/Progress.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import { computed, onMounted } from 'vue'
import { UpdateChannel, UpdatePhase } from '../bridge'
import PrefRow from '../shared/PrefRow.vue'
import { t } from '../shared/i18n'
import { bytes } from '../shared/format'
import { confirm } from '../shared/confirm'
import { useUpdates } from './store'

const updates = useUpdates()
onMounted(() => void updates.start())
const channels = [UpdateChannel.UpdateStable, UpdateChannel.UpdatePreview] as const
const items = computed(() => channels.map((value) => ({ value, label: t(`update.channel.${value}`) })))
function setChannel(value: unknown) {
  for (const channel of channels) if (value === channel) void updates.setPolicy({ channel })
}
async function restart() {
  if (await confirm(t('update.restart'), t('update.restartHint'), t('update.restart'))) await updates.restart()
}
function setBandwidth(event: Event) {
  if (!(event.target instanceof HTMLInputElement)) return
  const bandwidthKB = event.target.valueAsNumber
  if (Number.isInteger(bandwidthKB) && bandwidthKB >= 0 && bandwidthKB <= 102400)
    void updates.setPolicy({ bandwidthKB })
}
async function download() {
  if (await confirm(t('update.download'), t('update.downloadHint'), t('update.download'))) await updates.download()
}
const ready = computed(() => updates.status?.phase === UpdatePhase.UpdateReady)
const offered = computed(() => !!updates.status?.offer.version)
const phaseText = computed(() =>
  updates.status && updates.status.phase !== UpdatePhase.$zero
    ? t(`update.phase.${updates.status.phase}`)
    : t('update.loading'),
)
const persistedError = computed(() => {
  switch (updates.status?.error) {
    case 'update_interrupted':
      return t('update.interrupted')
    case 'update_invalid':
      return t('update.invalid')
    case 'update_publisher':
      return t('update.publisher')
    case 'update_space':
      return t('update.space')
    case 'update_busy':
      return t('update.busy')
    case 'update_failed':
      return t('update.failed')
    default:
      return ''
  }
})
</script>

<template>
  <div class="flex flex-col gap-3">
    <p class="text-muted">{{ t('update.privacy') }}</p>
    <UAlert
      v-if="updates.status && !updates.status.configured"
      :description="t('update.unavailable')"
      color="neutral"
    />
    <PrefRow :label="t('update.automatic')" :description="t('update.automaticHint')">
      <template #default="{ labelledBy, describedBy }">
        <USwitch
          :model-value="updates.status?.policy.automatic ?? false"
          :disabled="!updates.status?.configured || !updates.status.coordinator || updates.saving"
          :aria-labelledby="labelledBy"
          :aria-describedby="describedBy"
          @update:model-value="(automatic) => updates.setPolicy({ automatic })"
        />
      </template>
    </PrefRow>
    <PrefRow :label="t('update.channel')" :description="t('update.channelHint')">
      <template #default="{ labelledBy, describedBy }">
        <URadioGroup
          :model-value="channels.find((value) => value === updates.status?.policy.channel)"
          :items="items"
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
    <PrefRow :label="t('update.battery')" :description="t('update.batteryHint')">
      <template #default="{ labelledBy, describedBy }">
        <USwitch
          :model-value="updates.status?.policy.deferBattery ?? true"
          :disabled="!updates.status?.configured || !updates.status.coordinator || updates.saving"
          :aria-labelledby="labelledBy"
          :aria-describedby="describedBy"
          @update:model-value="(deferBattery) => updates.setPolicy({ deferBattery })"
        />
      </template>
    </PrefRow>
    <PrefRow :label="t('update.bandwidth')" :description="t('update.bandwidthHint')">
      <template #default="{ labelledBy, describedBy }">
        <UInput
          type="number"
          :model-value="updates.status?.policy.bandwidthKB ?? 0"
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
    <p v-if="updates.status" role="status">{{ phaseText }}</p>
    <p v-if="updates.status?.lastCheck" class="text-muted">
      {{ t('update.lastCheck', { date: new Date(updates.status.lastCheck * 1000).toLocaleString() }) }}
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
      <UProgress
        v-if="updates.busy && updates.status.offer.downloadBytes"
        :model-value="updates.status.bytes"
        :max="updates.status.offer.downloadBytes"
        :aria-label="t('update.downloadProgress')"
      />
    </template>
    <div class="flex flex-wrap gap-2">
      <UButton
        :disabled="!updates.status?.configured || !updates.status.coordinator || updates.busy || ready"
        @click="updates.check"
        >{{ t('update.check') }}</UButton
      >
      <UButton v-if="offered && !ready" :disabled="updates.busy || !updates.status?.coordinator" @click="download">{{
        t('update.download')
      }}</UButton>
      <UButton v-if="ready" :disabled="updates.busy || !updates.status?.coordinator" @click="restart">{{
        t('update.restart')
      }}</UButton>
      <UButton v-if="ready" :disabled="!updates.status?.coordinator" variant="outline" @click="updates.dismiss">{{
        t('update.later')
      }}</UButton>
      <UButton v-if="updates.busy && !ready && updates.status?.coordinator" variant="outline" @click="updates.cancel">{{
        t('common.cancel')
      }}</UButton>
      <UButton
        v-if="offered && !updates.busy"
        :disabled="!updates.status?.coordinator"
        variant="outline"
        @click="updates.discard"
        >{{ t('update.discard') }}</UButton
      >
    </div>
    <UAlert
      v-if="updates.error || persistedError"
      role="alert"
      :description="updates.error || persistedError"
      color="error"
    />
  </div>
</template>
