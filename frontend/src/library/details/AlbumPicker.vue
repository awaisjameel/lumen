<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UCheckbox from '@nuxt/ui/components/Checkbox.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import UModal from '@nuxt/ui/components/Modal.vue'
import { AlbumKind, engine } from '../../bridge'
import { emptyRule, useAlbums } from '../../albums/store'
import { errorText, reportError } from '../../shared/errors'
import { nameTooLong, trimName } from '../../shared/names'
import { notify } from '../../shared/notify'
import { t } from '../../shared/i18n'

const props = defineProps<{ id: number; membership: number[] }>()
const emit = defineEmits<{ changed: [] }>()
const open = defineModel<boolean>('open', { default: false })
const albums = useAlbums()
const query = ref('')
const chosen = ref<number[]>([])
const busy = ref(false)
const error = ref('')
const manual = computed(() => albums.list.filter((album) => album.kind === AlbumKind.AlbumManual))
const search = new Intl.Collator('en-GB', { usage: 'search', sensitivity: 'accent' })
const matches = computed(() => {
  const needle = Array.from(trimName(query.value))
  return manual.value.filter((album) => {
    const letters = Array.from(album.name)
    return (
      !needle.length ||
      letters.some(
        (_letter, index) => search.compare(letters.slice(index, index + needle.length).join(''), needle.join('')) === 0,
      )
    )
  })
})
let generation = 0
onBeforeUnmount(() => generation++)
watch(
  [open, () => props.id],
  ([value]) => {
    generation++
    busy.value = false
    if (value) {
      chosen.value = [...props.membership]
      query.value = ''
      error.value = ''
      const mine = generation
      void albums.load().catch((e: unknown) => {
        if (mine === generation) error.value = errorText(e)
      })
    }
  },
  { immediate: true },
)
function toggle(id: number, checked: boolean | 'indeterminate') {
  if (checked === 'indeterminate') return
  chosen.value = checked ? [...chosen.value, id] : chosen.value.filter((value) => value !== id)
}
async function apply(create = false) {
  if (busy.value) return
  const name = trimName(query.value)
  if (create && (!name || nameTooLong(name))) return
  const id = props.id
  const mine = generation
  const current = () => mine === generation && open.value
  const previous = [...props.membership]
  const changes: { album: number; include: boolean }[] = []
  busy.value = true
  error.value = ''
  try {
    if (create) {
      const album = await engine.createAlbum(name, AlbumKind.AlbumManual, emptyRule(), [id])
      changes.push({ album, include: true })
      chosen.value = [...chosen.value, album]
    }
    for (const album of manual.value) {
      if (!current()) return
      const include = chosen.value.includes(album.id)
      if (include === previous.includes(album.id)) continue
      await engine.setAlbumItem(id, album.id, include)
      changes.push({ album: album.id, include })
    }
    emit('changed')
    if (current()) open.value = false
  } catch (e) {
    if (current()) error.value = errorText(e)
    // Retain partial successes so retry never reverses changes that were already saved.
    emit('changed')
  } finally {
    if (current()) busy.value = false
    if (changes.length)
      notify({
        id: `albums-${id}`,
        kind: 'success',
        title: t('details.album.saved'),
        actions: [
          {
            label: t('settings.undo'),
            run: () => {
              void Promise.all(changes.map((change) => engine.setAlbumItem(id, change.album, !change.include)))
                .then(() => emit('changed'))
                .catch(reportError)
            },
          },
        ],
      })
    void albums.load().catch(reportError)
  }
}
</script>

<template>
  <UModal
    v-model:open="open"
    :title="t('details.album.add')"
    :description="t('details.album.empty')"
    :dismissible="!busy"
    :close="!busy"
  >
    <template #body>
      <UAlert v-if="error" color="error" :title="error" role="alert" class="mb-4" />
      <UInput
        v-model="query"
        class="w-full mb-4"
        type="search"
        :disabled="busy"
        :aria-label="t('details.album.search')"
        :placeholder="t('details.album.search')"
      />
      <div class="max-h-64 overflow-y-auto space-y-3">
        <UCheckbox
          v-for="album in matches"
          :key="album.id"
          :label="album.name"
          :model-value="chosen.includes(album.id)"
          :disabled="busy"
          @update:model-value="toggle(album.id, $event)"
        />
        <p v-if="!matches.length" class="text-sm text-muted">{{ t('details.album.empty') }}</p>
      </div>
      <UButton
        v-if="trimName(query) && !matches.length"
        class="mt-4 max-w-full"
        color="neutral"
        variant="outline"
        :disabled="busy || nameTooLong(query)"
        @click="apply(true)"
        >{{ t('details.album.create', { name: trimName(query) }) }}</UButton
      >
    </template>
    <template #footer
      ><div class="w-full flex justify-end gap-2">
        <UButton color="neutral" variant="ghost" :disabled="busy" @click="open = false">{{
          t('common.cancel')
        }}</UButton>
        <UButton color="primary" variant="solid" :loading="busy" :disabled="busy" @click="apply()">{{
          t('details.album.apply')
        }}</UButton>
      </div></template
    >
  </UModal>
</template>
