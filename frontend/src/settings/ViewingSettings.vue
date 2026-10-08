<script setup lang="ts">
// How photos are viewed: how one opens, what the wheel does, the filmstrip
// and slideshow, animations and the quality of viewing copies. Each change
// applies at once and is saved with the interface preferences.
import { computed } from 'vue'
import UInput from '@nuxt/ui/components/Input.vue'
import URadioGroup from '@nuxt/ui/components/RadioGroup.vue'
import USwitch from '@nuxt/ui/components/Switch.vue'
import {
  RENDER_SIDE_HIGH,
  RENDER_SIDE_STANDARD,
  SLIDESHOW_SECONDS_MAX,
  SLIDESHOW_SECONDS_MIN,
  ViewerOpen,
  ViewerWheel,
} from '../bridge'
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

const opens = [ViewerOpen.ViewerOpenFit, ViewerOpen.ViewerOpenActual] as const
const wheels = [ViewerWheel.ViewerWheelZoom, ViewerWheel.ViewerWheelNavigate] as const
const sides = [RENDER_SIDE_STANDARD, RENDER_SIDE_HIGH] as const
const openItems = computed(() => opens.map((value) => ({ value, label: t(`viewer.open.${value}`) })))
const wheelItems = computed(() => wheels.map((value) => ({ value, label: t(`viewer.wheel.${value}`) })))
const sideItems = computed(() => [
  { value: RENDER_SIDE_STANDARD, label: t('viewer.renderSide.standard', { n: RENDER_SIDE_STANDARD }) },
  { value: RENDER_SIDE_HIGH, label: t('viewer.renderSide.high', { n: RENDER_SIDE_HIGH }) },
])
function setOpen(value: unknown) {
  for (const open of opens) if (open === value) void prefs.setViewer({ open })
}
function setWheel(value: unknown) {
  for (const wheel of wheels) if (wheel === value) void prefs.setViewer({ wheel })
}
function setSide(value: unknown) {
  for (const renderSide of sides) if (renderSide === value) void prefs.setViewer({ renderSide })
}
</script>
<template>
  <div>
    <PrefRow
      v-if="shows('viewer.open', 'viewer.openHint')"
      :label="t('viewer.open')"
      :description="t('viewer.openHint')"
    >
      <template #default="{ labelledBy, describedBy }">
        <URadioGroup
          data-viewing="open"
          :model-value="opens.find((value) => value === viewing.open)"
          :items="openItems"
          orientation="horizontal"
          :aria-labelledby="labelledBy"
          :aria-describedby="describedBy"
          @update:model-value="setOpen"
        />
      </template>
    </PrefRow>
    <PrefRow
      v-if="shows('viewer.wheel', 'viewer.wheelHint')"
      :label="t('viewer.wheel')"
      :description="t('viewer.wheelHint')"
    >
      <template #default="{ labelledBy, describedBy }">
        <URadioGroup
          data-viewing="wheel"
          :model-value="wheels.find((value) => value === viewing.wheel)"
          :items="wheelItems"
          orientation="horizontal"
          :aria-labelledby="labelledBy"
          :aria-describedby="describedBy"
          @update:model-value="setWheel"
        />
      </template>
    </PrefRow>
    <PrefRow v-if="shows('viewer.filmstrip')" :label="t('viewer.filmstrip')"
      ><USwitch
        :model-value="viewing.filmstrip"
        :aria-label="t('viewer.filmstrip')"
        @update:model-value="prefs.setViewer({ filmstrip: $event })"
    /></PrefRow>
    <PrefRow v-if="shows('viewer.autoHide')" :label="t('viewer.autoHide')"
      ><USwitch
        :model-value="viewing.autoHide"
        :aria-label="t('viewer.autoHide')"
        @update:model-value="prefs.setViewer({ autoHide: $event })"
    /></PrefRow>
    <PrefRow v-if="shows('viewer.interval')" :label="t('viewer.interval')"
      ><UInput
        type="number"
        :min="SLIDESHOW_SECONDS_MIN"
        :max="SLIDESHOW_SECONDS_MAX"
        :model-value="viewing.slideshowSeconds"
        :aria-label="t('viewer.interval')"
        @update:model-value="prefs.setViewer({ slideshowSeconds: Number($event) || defaults.slideshowSeconds })"
    /></PrefRow>
    <PrefRow v-if="shows('viewer.slideshowLoop')" :label="t('viewer.slideshowLoop')"
      ><USwitch
        :model-value="viewing.loop"
        :aria-label="t('viewer.slideshowLoop')"
        @update:model-value="prefs.setViewer({ loop: $event })"
    /></PrefRow>
    <PrefRow
      v-if="shows('viewer.animations', 'viewer.animationsHint')"
      :label="t('viewer.animations')"
      :description="t('viewer.animationsHint')"
    >
      <template #default="{ labelledBy, describedBy }">
        <USwitch
          data-viewing="animations"
          :model-value="!viewing.stillAnimations"
          :aria-labelledby="labelledBy"
          :aria-describedby="describedBy"
          @update:model-value="prefs.setViewer({ stillAnimations: !$event })"
        />
      </template>
    </PrefRow>
    <PrefRow
      v-if="shows('viewer.renderSide', 'viewer.renderSideHint')"
      :label="t('viewer.renderSide')"
      :description="t('viewer.renderSideHint')"
    >
      <template #default="{ labelledBy, describedBy }">
        <URadioGroup
          data-viewing="renderSide"
          :model-value="sides.find((value) => value === viewing.renderSide)"
          :items="sideItems"
          orientation="horizontal"
          :aria-labelledby="labelledBy"
          :aria-describedby="describedBy"
          @update:model-value="setSide"
        />
      </template>
    </PrefRow>
  </div>
</template>
