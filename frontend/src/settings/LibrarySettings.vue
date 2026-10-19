<script setup lang="ts">
// How the library is browsed: the screen Lumen opens on, how thumbnails fill
// their squares and which badges they carry. The thumbnail size and the
// panels' widths are set where they are used (the library toolbar, the
// panels' edges) and are saved with these.
import { computed } from 'vue'
import URadioGroup from '@nuxt/ui/components/RadioGroup.vue'
import USelect from '@nuxt/ui/components/Select.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import USwitch from '@nuxt/ui/components/Switch.vue'
import { START_VIEWS, StartView, ThumbFit } from '../bridge'
import { t } from '../shared/i18n'
import { defaultPreferences, usePreferences } from '../shared/preferences'
import PrefRow from '../shared/PrefRow.vue'
import { useRowFilter } from './rowFilter'

const props = withDefaults(defineProps<{ query?: string }>(), { query: '' })
const shows = useRowFilter(() => props.query)
const prefs = usePreferences()
const defaults = defaultPreferences()

const startItems = computed(() =>
  START_VIEWS.filter((value) => value !== StartView.StartVideos).map((value) => ({ value, label: t(`nav.${value}`) })),
)
function setStart(value: unknown) {
  for (const startView of START_VIEWS) if (startView === value) void prefs.set({ startView })
}

const fits = [ThumbFit.ThumbCover, ThumbFit.ThumbContain] as const
const fitItems = computed(() => fits.map((value) => ({ value, label: t(`settings.thumbFit.${value}`) })))
function setFit(value: unknown) {
  for (const thumbFit of fits) if (thumbFit === value) void prefs.set({ thumbFit })
}
</script>
<template>
  <div>
    <PrefRow
      v-if="shows('settings.startView', 'settings.startViewHint')"
      :label="t('settings.startView')"
      :modified="prefs.prefs.startView !== defaults.startView"
      :description="t('settings.startViewHint')"
    >
      <template #default="{ labelledBy, describedBy }">
        <USelect
          data-library="startView"
          class="w-44"
          :model-value="startItems.find((item) => item.value === prefs.prefs.startView)?.value"
          :items="startItems"
          :aria-labelledby="labelledBy"
          :aria-describedby="describedBy"
          @update:model-value="setStart"
        />
      </template>
    </PrefRow>
    <PrefRow
      v-if="shows('settings.thumbFit', 'settings.thumbFitHint')"
      :label="t('settings.thumbFit')"
      :modified="prefs.prefs.thumbFit !== defaults.thumbFit"
      :description="t('settings.thumbFitHint')"
    >
      <template #default="{ labelledBy, describedBy }">
        <URadioGroup
          data-library="thumbFit"
          :model-value="fits.find((value) => value === prefs.prefs.thumbFit)"
          :items="fitItems"
          orientation="horizontal"
          :aria-labelledby="labelledBy"
          :aria-describedby="describedBy"
          @update:model-value="setFit"
        />
      </template>
    </PrefRow>
    <PrefRow
      v-if="
        shows(
          'settings.badges',
          'settings.badgesHint',
          'settings.badgeFavourite',
          'settings.badgeVideo',
          'settings.badgeDuration',
        )
      "
      :label="t('settings.badges')"
      :modified="prefs.prefs.hideFavouriteBadge || prefs.prefs.hideVideoBadge || prefs.prefs.hideDurationBadge"
      :description="t('settings.badgesHint')"
    >
      <template #default>
        <div class="badges">
          <USwitch
            data-library="badge-favourite"
            :label="t('settings.badgeFavourite')"
            :model-value="!prefs.prefs.hideFavouriteBadge"
            @update:model-value="prefs.set({ hideFavouriteBadge: !$event })"
          />
          <USwitch
            data-library="badge-video"
            :label="t('settings.badgeVideo')"
            :model-value="!prefs.prefs.hideVideoBadge"
            @update:model-value="prefs.set({ hideVideoBadge: !$event })"
          />
          <USwitch
            data-library="badge-duration"
            :label="t('settings.badgeDuration')"
            :model-value="!prefs.prefs.hideDurationBadge"
            @update:model-value="prefs.set({ hideDurationBadge: !$event })"
          />
        </div>
      </template>
    </PrefRow>
    <PrefRow
      v-if="shows('settings.sort', 'settings.sortHint')"
      :label="t('settings.sort')"
      :description="t('settings.sortHint')"
      ><UButton to="/" variant="outline">{{ t('settings.openLibrary') }}</UButton></PrefRow
    >
  </div>
</template>
<style scoped>
.badges {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}
</style>
