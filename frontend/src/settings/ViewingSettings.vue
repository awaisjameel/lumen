<script setup lang="ts">
import { computed } from 'vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import USwitch from '@nuxt/ui/components/Switch.vue'
import USlider from '@nuxt/ui/components/Slider.vue'
import {
  engine,
  PLAYBACK_SPEED_MAX,
  PLAYBACK_SPEED_MIN,
  SKIP_SECONDS_MAX,
  SKIP_SECONDS_MIN,
  SLIDESHOW_SECONDS_MAX,
  SLIDESHOW_SECONDS_MIN,
} from '../bridge'
import { reportError } from '../shared/errors'
import { t } from '../shared/i18n'
import { usePreferences } from '../shared/preferences'
import { viewerDefaults } from '../shared/viewing'
import PrefRow from '../shared/PrefRow.vue'

const prefs = usePreferences()
const viewing = computed(() => prefs.viewer)
const defaults = viewerDefaults()
</script>
<template>
  <div>
    <PrefRow :label="t('viewer.filmstrip')"
      ><USwitch
        :model-value="viewing.filmstrip"
        :aria-label="t('viewer.filmstrip')"
        @update:model-value="prefs.setViewer({ filmstrip: $event })"
    /></PrefRow>
    <PrefRow :label="t('viewer.autoHide')"
      ><USwitch
        :model-value="viewing.autoHide"
        :aria-label="t('viewer.autoHide')"
        @update:model-value="prefs.setViewer({ autoHide: $event })"
    /></PrefRow>
    <PrefRow :label="t('viewer.interval')"
      ><UInput
        type="number"
        :min="SLIDESHOW_SECONDS_MIN"
        :max="SLIDESHOW_SECONDS_MAX"
        :model-value="viewing.slideshowSeconds"
        :aria-label="t('viewer.interval')"
        @update:model-value="prefs.setViewer({ slideshowSeconds: Number($event) || defaults.slideshowSeconds })"
    /></PrefRow>
    <PrefRow :label="t('viewer.loop')"
      ><USwitch
        :model-value="viewing.loop"
        :aria-label="t('viewer.loop')"
        @update:model-value="prefs.setViewer({ loop: $event })"
    /></PrefRow>
    <PrefRow :label="t('viewer.autoplay')"
      ><USwitch
        :model-value="viewing.autoplay"
        :aria-label="t('viewer.autoplay')"
        @update:model-value="prefs.setViewer({ autoplay: $event })"
    /></PrefRow>
    <PrefRow :label="t('viewer.volume')"
      ><USlider
        :min="0"
        :max="100"
        :model-value="viewing.volume * 100"
        :aria-label="t('viewer.volume')"
        @update:model-value="prefs.setViewer({ volume: ($event ?? 100) / 100 })"
    /></PrefRow>
    <PrefRow :label="t('viewer.resume')" :description="t('viewer.resumeHint')"
      ><USwitch
        :model-value="viewing.resume"
        :aria-label="t('viewer.resume')"
        @update:model-value="prefs.setViewer({ resume: $event })"
    /></PrefRow>
    <PrefRow :label="t('viewer.skipInterval')"
      ><UInput
        type="number"
        :min="SKIP_SECONDS_MIN"
        :max="SKIP_SECONDS_MAX"
        :model-value="viewing.skipSeconds"
        :aria-label="t('viewer.skipInterval')"
        @update:model-value="prefs.setViewer({ skipSeconds: Number($event) || defaults.skipSeconds })"
    /></PrefRow>
    <PrefRow :label="t('viewer.speed')"
      ><UInput
        type="number"
        :min="PLAYBACK_SPEED_MIN"
        :max="PLAYBACK_SPEED_MAX"
        :step="PLAYBACK_SPEED_MIN"
        :model-value="viewing.speed"
        :aria-label="t('viewer.speed')"
        @update:model-value="prefs.setViewer({ speed: Number($event) || defaults.speed })"
    /></PrefRow>
    <UButton variant="ghost" @click="engine.clearPlaybackHistory().catch(reportError)">{{
      t('viewer.clearHistory')
    }}</UButton>
  </div>
</template>
