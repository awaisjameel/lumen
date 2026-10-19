<script setup lang="ts">
// How videos play: what each starts with (sound, speed, looping, subtitles)
// and whether Lumen remembers where one was left. The player's own controls
// still change a video while it plays; these are what the next one opens with.
import { computed, ref } from 'vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import USwitch from '@nuxt/ui/components/Switch.vue'
import USlider from '@nuxt/ui/components/Slider.vue'
import { engine, PLAYBACK_SPEED_MAX, PLAYBACK_SPEED_MIN, SKIP_SECONDS_MAX, SKIP_SECONDS_MIN } from '../bridge'
import { errorText } from '../shared/errors'
import { t } from '../shared/i18n'
import { usePreferences } from '../shared/preferences'
import { viewerDefaults } from '../shared/viewing'
import PrefRow from '../shared/PrefRow.vue'
import { useRowFilter } from './rowFilter'

const props = withDefaults(defineProps<{ query?: string }>(), { query: '' })
const shows = useRowFilter(() => props.query)
const prefs = usePreferences()
const viewing = computed(() => prefs.viewer)
const defaults = viewerDefaults()

function setLanguage(e: Event) {
  if (e.target instanceof HTMLInputElement) void prefs.setViewer({ subtitleLanguage: e.target.value.trim() })
}

const historyNote = ref('')
const historyError = ref('')
async function clearHistory() {
  historyNote.value = ''
  historyError.value = ''
  try {
    await engine.clearPlaybackHistory()
    historyNote.value = t('viewer.historyCleared')
  } catch (e) {
    historyError.value = errorText(e)
  }
}
</script>
<template>
  <div>
    <PrefRow
      v-if="shows('viewer.autoplay')"
      :label="t('viewer.autoplay')"
      :modified="viewing.autoplay !== defaults.autoplay"
      ><USwitch
        :model-value="viewing.autoplay"
        :aria-label="t('viewer.autoplay')"
        @update:model-value="prefs.setViewer({ autoplay: $event })"
    /></PrefRow>
    <PrefRow v-if="shows('viewer.volume')" :label="t('viewer.volume')" :modified="viewing.volume !== defaults.volume"
      ><USlider
        :min="0"
        :max="100"
        :model-value="viewing.volume * 100"
        :aria-label="t('viewer.volume')"
        @update:model-value="prefs.setViewer({ volume: ($event ?? 100) / 100 })"
    /></PrefRow>
    <PrefRow
      v-if="shows('viewer.startMuted')"
      :label="t('viewer.startMuted')"
      :modified="viewing.muted !== defaults.muted"
      ><USwitch
        data-video="muted"
        :model-value="viewing.muted"
        :aria-label="t('viewer.startMuted')"
        @update:model-value="prefs.setViewer({ muted: $event })"
    /></PrefRow>
    <PrefRow v-if="shows('viewer.speed')" :label="t('viewer.speed')" :modified="viewing.speed !== defaults.speed"
      ><UInput
        type="number"
        :min="PLAYBACK_SPEED_MIN"
        :max="PLAYBACK_SPEED_MAX"
        :step="PLAYBACK_SPEED_MIN"
        :model-value="viewing.speed"
        :aria-label="t('viewer.speed')"
        @update:model-value="prefs.setViewer({ speed: Number($event) || defaults.speed })"
    /></PrefRow>
    <PrefRow
      v-if="shows('viewer.skipInterval')"
      :label="t('viewer.skipInterval')"
      :modified="viewing.skipSeconds !== defaults.skipSeconds"
      ><UInput
        type="number"
        :min="SKIP_SECONDS_MIN"
        :max="SKIP_SECONDS_MAX"
        :model-value="viewing.skipSeconds"
        :aria-label="t('viewer.skipInterval')"
        @update:model-value="prefs.setViewer({ skipSeconds: Number($event) || defaults.skipSeconds })"
    /></PrefRow>
    <PrefRow
      v-if="shows('viewer.videoLoop')"
      :label="t('viewer.videoLoop')"
      :modified="viewing.videoLoop !== defaults.videoLoop"
      ><USwitch
        data-video="loop"
        :model-value="viewing.videoLoop"
        :aria-label="t('viewer.videoLoop')"
        @update:model-value="prefs.setViewer({ videoLoop: $event })"
    /></PrefRow>
    <PrefRow
      v-if="shows('viewer.subtitles', 'viewer.subtitlesHint')"
      :label="t('viewer.subtitles')"
      :modified="viewing.subtitles !== defaults.subtitles"
      :description="t('viewer.subtitlesHint')"
    >
      <template #default="{ labelledBy, describedBy }">
        <USwitch
          data-video="subtitles"
          :model-value="viewing.subtitles"
          :aria-labelledby="labelledBy"
          :aria-describedby="describedBy"
          @update:model-value="prefs.setViewer({ subtitles: $event })"
        />
      </template>
    </PrefRow>
    <PrefRow
      v-if="shows('viewer.subtitleLanguage', 'viewer.subtitleLanguageHint')"
      :label="t('viewer.subtitleLanguage')"
      :modified="viewing.subtitleLanguage !== defaults.subtitleLanguage"
      :description="t('viewer.subtitleLanguageHint')"
    >
      <template #default="{ labelledBy, describedBy }">
        <UInput
          data-video="language"
          class="w-28"
          maxlength="16"
          :disabled="!viewing.subtitles"
          :model-value="viewing.subtitleLanguage"
          :aria-labelledby="labelledBy"
          :aria-describedby="describedBy"
          @change="setLanguage"
        />
      </template>
    </PrefRow>
    <PrefRow
      v-if="shows('viewer.resume', 'viewer.resumeHint')"
      :label="t('viewer.resume')"
      :modified="viewing.resume !== defaults.resume"
      :description="t('viewer.resumeHint')"
      ><USwitch
        :model-value="viewing.resume"
        :aria-label="t('viewer.resume')"
        @update:model-value="prefs.setViewer({ resume: $event })"
    /></PrefRow>
    <PrefRow
      v-if="shows('viewer.clearHistory', 'viewer.clearHistoryHint')"
      :label="t('viewer.clearHistory')"
      :description="t('viewer.clearHistoryHint')"
      :hint="historyNote"
      :error="historyError"
    >
      <template #default>
        <UButton data-video="clear-history" loading-auto @click="clearHistory">{{ t('viewer.clearHistory') }}</UButton>
      </template>
    </PrefRow>
  </div>
</template>
