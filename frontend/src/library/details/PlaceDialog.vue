<script setup lang="ts">
import { onBeforeUnmount, ref, shallowRef, watch } from 'vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import UModal from '@nuxt/ui/components/Modal.vue'
import { engine, isCancelled } from '../../bridge'
import type { AssetDetail, CityMatch, Selection } from '../../bridge'
import { errorText } from '../../shared/errors'
import { t } from '../../shared/i18n'
import { useCorrection } from './corrections'

const props = defineProps<{ assets: AssetDetail[]; selection?: Selection | null }>()
const emit = defineEmits<{ changed: [] }>()
const open = defineModel<boolean>('open', { default: false })
const query = ref('')
const lat = ref('')
const lon = ref('')
const cities = shallowRef<CityMatch[]>([])
const searching = ref(false)
const searchError = ref('')
const { busy, error, save } = useCorrection(
  () => emit('changed'),
  () => (open.value = false),
)
watch(
  open,
  (value) => {
    if (value) {
      query.value = ''
      lat.value = String(props.assets[0]?.lat ?? '')
      lon.value = String(props.assets[0]?.lon ?? '')
      error.value = ''
    }
  },
  { immediate: true },
)
watch([query, open], ([text, visible], _old, cleanup) => {
  cities.value = []
  searchError.value = ''
  searching.value = false
  if (!visible || !text.trim()) return
  const abort = new AbortController()
  searching.value = true
  const timer = setTimeout(() => {
    void engine
      .findCities(text.trim(), 20, abort.signal)
      .then((found) => {
        if (!abort.signal.aborted) cities.value = found
      })
      .catch((e: unknown) => {
        if (!abort.signal.aborted && !isCancelled(e)) searchError.value = errorText(e)
      })
      .finally(() => {
        if (!abort.signal.aborted) searching.value = false
      })
  }, 120)
  cleanup(() => {
    clearTimeout(timer)
    abort.abort()
  })
})
onBeforeUnmount(() => (cities.value = []))
function pick(city: CityMatch) {
  lat.value = String(city.lat)
  lon.value = String(city.lon)
}
async function apply(restore = false) {
  const latitude = Number(lat.value),
    longitude = Number(lon.value)
  if (
    !props.assets.length ||
    (!restore &&
      (!lat.value.trim() ||
        !lon.value.trim() ||
        !Number.isFinite(latitude) ||
        !Number.isFinite(longitude) ||
        latitude < -90 ||
        latitude > 90 ||
        longitude < -180 ||
        longitude > 180))
  ) {
    error.value = t('details.place.invalid')
    return
  }
  const previous = props.assets.map((asset) => ({
    id: asset.id,
    lat: asset.lat,
    lon: asset.lon,
    corrected: !!asset.originalLocation,
  }))
  await save(
    'correct-place',
    t(restore ? 'details.place.restored' : 'details.place.saved'),
    async (current) => {
      if (props.selection) await engine.validateSelection(props.selection)
      const ids = previous.map((asset) => asset.id)
      if (
        !current() ||
        !open.value ||
        props.assets.map((asset) => asset.id).join(',') !== previous.map((asset) => asset.id).join(',')
      )
        return false
      if (restore) await engine.restoreLocation(ids)
      else await engine.setLocation(ids, latitude, longitude)
      return true
    },
    async () => {
      for (const asset of previous) {
        if (asset.corrected && asset.lat !== null && asset.lon !== null)
          await engine.setLocation([asset.id], asset.lat, asset.lon)
        else await engine.restoreLocation([asset.id])
      }
    },
  )
}
</script>

<template>
  <UModal
    v-model:open="open"
    :title="t('details.place.title')"
    :description="t('details.place.hint')"
    :dismissible="!busy"
    :close="!busy"
  >
    <template #body>
      <UAlert v-if="error || searchError" color="error" :title="error || searchError" role="alert" class="mb-4" />
      <form id="correct-place" class="space-y-4" @submit.prevent="apply()">
        <label class="block text-sm font-semibold"
          >{{ t('details.place.search')
          }}<UInput
            v-model="query"
            class="mt-2 w-full"
            type="search"
            :disabled="busy"
            :aria-label="t('details.place.search')"
        /></label>
        <p v-if="searching" role="status" class="text-sm text-muted">{{ t('details.place.searching') }}</p>
        <p v-else-if="query.trim() && !cities.length && !searchError" class="text-sm text-muted">
          {{ t('details.place.noCities') }}
        </p>
        <ul v-if="cities.length" class="max-h-40 overflow-y-auto" :aria-label="t('details.place.matches')">
          <li v-for="city in cities" :key="city.id">
            <UButton color="neutral" variant="ghost" class="w-full justify-start" :disabled="busy" @click="pick(city)"
              ><bdi>{{ [city.name, city.region, city.country].filter(Boolean).join(' · ') }}</bdi></UButton
            >
          </li>
        </ul>
        <div class="grid grid-cols-2 gap-4">
          <label class="text-sm font-semibold"
            >{{ t('details.place.latitude')
            }}<UInput
              :model-value="lat"
              class="mt-2 w-full"
              type="number"
              :min="-90"
              :max="90"
              step="any"
              :disabled="busy"
              :aria-label="t('details.place.latitude')"
              @update:model-value="lat = String($event)"
          /></label>
          <label class="text-sm font-semibold"
            >{{ t('details.place.longitude')
            }}<UInput
              :model-value="lon"
              class="mt-2 w-full"
              type="number"
              :min="-180"
              :max="180"
              step="any"
              :disabled="busy"
              :aria-label="t('details.place.longitude')"
              @update:model-value="lon = String($event)"
          /></label>
        </div>
      </form>
    </template>
    <template #footer>
      <div class="w-full flex flex-wrap items-center justify-end gap-2">
        <UButton
          v-if="assets.some((asset) => asset.originalLocation)"
          color="neutral"
          variant="ghost"
          :disabled="busy"
          @click="apply(true)"
          >{{ t('details.restoreOriginal') }}</UButton
        >
        <UButton color="neutral" variant="ghost" :disabled="busy" @click="open = false">{{
          t('common.cancel')
        }}</UButton>
        <UButton color="primary" variant="solid" type="submit" form="correct-place" :disabled="busy" :loading="busy">{{
          t('details.place.save')
        }}</UButton>
      </div>
    </template>
  </UModal>
</template>
