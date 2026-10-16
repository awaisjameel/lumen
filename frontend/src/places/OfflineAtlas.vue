<script setup lang="ts">
import UButton from '@nuxt/ui/components/Button.vue'
import UPopover from '@nuxt/ui/components/Popover.vue'
import { computed, onBeforeUnmount, onMounted, ref, useTemplateRef } from 'vue'
import type { Place } from '../bridge'
import { icons } from '../design/icons'
import { formatNumber, t } from '../shared/i18n'
import IconButton from '../shared/IconButton.vue'
import { atlasDots, atlasMarkers } from './atlas'

const props = defineProps<{ places: Place[]; active?: number }>()
const emit = defineEmits<{ hover: [id: number]; preview: [place: Place] }>()
const map = useTemplateRef<HTMLElement>('map')
const width = ref(600)
const markers = computed(() => atlasMarkers(props.places, width.value))
function choose(place: Place, close: () => void) {
  emit('preview', place)
  close()
}
let observer: ResizeObserver | undefined
onMounted(() => {
  observer = new ResizeObserver((entries) => {
    width.value = entries[0]?.contentRect.width ?? 600
  })
  if (map.value) observer.observe(map.value)
})
onBeforeUnmount(() => observer?.disconnect())
</script>

<template>
  <div ref="map" class="atlas" :aria-label="t('places.atlas')" role="group">
    <svg viewBox="0 0 1000 400" preserveAspectRatio="none" aria-hidden="true">
      <circle
        v-for="dot in atlasDots"
        :key="dot.key"
        :cx="dot.x * 10"
        :cy="dot.y * 4"
        :r="width > 0 ? 2000 / width : 2"
      />
    </svg>
    <template v-for="marker in markers" :key="marker.id">
      <IconButton
        v-if="marker.places.length === 1 && marker.places[0]"
        class="marker"
        :style="{ left: `${marker.x}%`, top: `${marker.y}%` }"
        :icon="icons.places"
        size="xs"
        :variant="active === marker.id ? 'solid' : 'soft'"
        :color="active === marker.id ? 'primary' : 'neutral'"
        :label="
          t('places.marker', {
            name: marker.places[0].name,
            country: marker.places[0].country,
            n: formatNumber(marker.places[0].count),
          })
        "
        :aria-pressed="active === marker.id"
        @mouseenter="emit('hover', marker.id)"
        @focus="emit('hover', marker.id)"
        @click="emit('preview', marker.places[0])"
      />
      <UPopover v-else :content="{ side: 'bottom', align: 'center' }">
        <UButton
          class="marker"
          :style="{ left: `${marker.x}%`, top: `${marker.y}%` }"
          :icon="icons.places"
          size="xs"
          :variant="marker.places.some((p) => p.id === active) ? 'solid' : 'soft'"
          :color="marker.places.some((p) => p.id === active) ? 'primary' : 'neutral'"
          :aria-pressed="marker.places.some((p) => p.id === active)"
          :aria-label="t('places.cluster', { n: formatNumber(marker.places.length) })"
          @mouseenter="!marker.places.some((p) => p.id === active) && emit('hover', marker.id)"
          @focus="!marker.places.some((p) => p.id === active) && emit('hover', marker.id)"
          >{{ formatNumber(marker.places.length) }}</UButton
        >
        <template #content="{ close }"
          ><div class="city-choices">
            <UButton
              v-for="place in marker.places"
              :key="place.id"
              variant="ghost"
              color="neutral"
              :aria-pressed="active === place.id"
              @mouseenter="emit('hover', place.id)"
              @focus="emit('hover', place.id)"
              @click="choose(place, close)"
              >{{ place.name }} · {{ place.country }}</UButton
            >
          </div></template
        >
      </UPopover>
    </template>
    <span class="caption">{{ t('places.stylised') }}</span>
  </div>
</template>

<style scoped>
.atlas {
  position: relative;
  width: 100%;
  aspect-ratio: 2.5;
  min-width: 0;
  margin-block: 1rem;
}
svg {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  fill: var(--ui-border-accented);
}
.marker {
  position: absolute;
  z-index: 1;
  transform: translate(-50%, -50%);
  min-width: 1.5rem;
  min-height: 1.5rem;
  box-shadow: var(--lumen-shadow-1);
}
.marker[aria-pressed='true'] {
  outline: 0.25rem solid var(--lumen-primary-soft-2);
}
.caption {
  position: absolute;
  left: 0;
  bottom: -1.25rem;
  color: var(--ui-text-dimmed);
  font-size: 0.6875rem;
}
.city-choices {
  display: flex;
  flex-direction: column;
  max-height: min(20rem, 60dvh);
  max-width: min(22rem, 80vw);
  overflow: auto;
  padding: 0.5rem;
}
@media (forced-colors: active) {
  svg {
    fill: CanvasText;
  }
}
</style>
