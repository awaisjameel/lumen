<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import UModal from '@nuxt/ui/components/Modal.vue'
import USelect from '@nuxt/ui/components/Select.vue'
import { AlbumKind, engine } from '../bridge'
import type { Selection } from '../bridge'
import { useAlbums } from '../albums/store'
import { confirm } from '../shared/confirm'
import { errorText, logError } from '../shared/errors'
import { t } from '../shared/i18n'
import { showTrashUndo } from '../trash/undo'
import { blockAssets } from '../shared/block'

const props = defineProps<{ ids: number[]; selection?: Selection | null; trash: boolean }>()
const emit = defineEmits<{ changed: [] }>()
const open = defineModel<boolean>('open', { default: false })
const albums = useAlbums()
const operation = ref('favourite')
const tag = ref('')
const album = ref(0)
const busy = ref(false)
const stopped = ref(false)
const isStopped = () => stopped.value
const error = ref('')
const results = ref<{ id: number; error: string; state: 'done' | 'failed' | 'cancelled' }[]>([])
const batchTotal = ref(0)
const page = ref(0)
onBeforeUnmount(() => {
  stopped.value = true
})
const options = computed(() =>
  props.trash
    ? [
        { value: 'restore', label: t('trash.restore') },
        { value: 'delete', label: t('trash.deleteFromDisk') },
      ]
    : [
        { value: 'favourite', label: t('details.favourite') },
        { value: 'unfavourite', label: t('details.unfavourite') },
        { value: 'tag', label: t('details.addTag') },
        { value: 'album', label: t('files.addAlbum') },
        { value: 'removeAlbum', label: t('files.removeAlbum') },
        { value: 'trash', label: t('trash.move') },
        { value: 'block', label: t('blocked.block') },
      ],
)
const albumOptions = computed(() =>
  albums.list.filter((a) => a.kind === AlbumKind.AlbumManual).map((a) => ({ value: a.id, label: a.name })),
)
watch(open, (value) => {
  if (value) {
    operation.value = props.trash ? 'restore' : 'favourite'
    results.value = []
    void albums.load().catch(logError)
  }
})
async function apply() {
  const ids = [...props.ids]
  const selectedOperation = operation.value
  const name = tag.value.trim(),
    albumID = album.value
  if (
    busy.value ||
    !ids.length ||
    (selectedOperation === 'tag' && !name) ||
    ((selectedOperation.includes('Album') || selectedOperation === 'album') && !albumID)
  )
    return
  if (!(await confirm(t('files.batch'), t('files.batchConfirm', { n: ids.length }), t('files.applyBatch')))) return
  if (ids.join(',') !== props.ids.join(',')) return
  busy.value = true
  batchTotal.value = ids.length
  stopped.value = false
  error.value = ''
  results.value = []
  page.value = 0
  try {
    if (props.selection) await engine.validateSelection(props.selection)
    if (selectedOperation === 'block') {
      await blockAssets(ids, (id, error) => results.value.push({ id, error, state: error ? 'failed' : 'done' }))
      for (const id of ids)
        if (!results.value.some((r) => r.id === id)) results.value.push({ id, error: '', state: 'cancelled' })
      return
    }
    if (selectedOperation === 'delete') {
      const selection = await engine.trashSelection(ids)
      if (
        !(await confirm(
          t('trash.deleteFromDisk'),
          t('files.recycleConfirm', { n: selection.assets.length }),
          t('trash.deleteFromDisk'),
        ))
      )
        return
      for (const id of selection.assets) {
        if (isStopped()) {
          results.value.push({ id, error: '', state: 'cancelled' })
          continue
        }
        try {
          const item = await engine.trashSelection([id])
          const outcome = await engine.deleteFromDisk(item)
          results.value.push({
            id,
            error: outcome.kept.length ? t('files.kept', { n: outcome.kept.length }) : '',
            state: outcome.kept.length ? 'failed' : 'done',
          })
        } catch (e) {
          results.value.push({ id, error: errorText(e), state: 'failed' })
        }
      }
    } else {
      for (const id of ids) {
        if (isStopped()) {
          results.value.push({ id, error: '', state: 'cancelled' })
          continue
        }
        try {
          switch (selectedOperation) {
            case 'favourite':
              await engine.setFavourite(id, true)
              break
            case 'unfavourite':
              await engine.setFavourite(id, false)
              break
            case 'tag':
              await engine.tagAsset(id, name)
              break
            case 'album':
              await engine.setAlbumItem(id, albumID, true)
              break
            case 'removeAlbum':
              await engine.setAlbumItem(id, albumID, false)
              break
            case 'trash':
              await engine.trashAssets([id])
              break
            case 'restore':
              await engine.restoreAssets([id])
              break
          }
          results.value.push({ id, error: '', state: 'done' })
        } catch (e) {
          results.value.push({ id, error: errorText(e), state: 'failed' })
        }
      }
      if (selectedOperation === 'trash') {
        const done = results.value.filter((r) => r.state === 'done').map((r) => r.id)
        if (done.length)
          showTrashUndo(done, async (ids) => {
            await engine.restoreAssets(ids)
            emit('changed')
          })
      }
    }
  } catch (e) {
    error.value = errorText(e)
  } finally {
    busy.value = false
    emit('changed')
  }
}
</script>
<template>
  <UModal
    v-model:open="open"
    :title="t('files.batch')"
    :description="t('selection.count', { n: busy || results.length ? batchTotal : ids.length })"
    :dismissible="!busy"
    :close="!busy"
  >
    <template #body>
      <UAlert v-if="error" color="error" :title="error" />
      <USelect v-model="operation" :items="options" :disabled="busy" :aria-label="t('files.operation')" />
      <UInput v-if="operation === 'tag'" v-model="tag" :disabled="busy" :aria-label="t('details.addTag')" />
      <USelect
        v-if="operation === 'album' || operation === 'removeAlbum'"
        v-model="album"
        :items="albumOptions"
        :disabled="busy"
        :aria-label="t('nav.albums')"
      />
      <p role="status">
        {{
          t('files.batchProgress', {
            n: results.filter((r) => r.state !== 'cancelled').length,
            total: busy || results.length ? batchTotal : ids.length,
            failed: results.filter((r) => r.error).length,
          })
        }}
      </p>
      <p v-for="result in results.slice(page * 100, (page + 1) * 100)" :key="result.id">
        {{ result.id }}: {{ t(`files.outcome.${result.state}`) }} {{ result.error }}
      </p>
      <div v-if="results.length > 100">
        <UButton :disabled="page === 0" variant="outline" @click="page--">{{ t('files.previousPage') }}</UButton>
        <UButton :disabled="(page + 1) * 100 >= results.length" variant="outline" @click="page++">{{
          t('files.nextPage')
        }}</UButton>
      </div>
    </template>
    <template #footer>
      <UButton :disabled="busy" @click="apply">{{ t('files.applyBatch') }}</UButton>
      <UButton v-if="busy" variant="outline" @click="stopped = true">{{ t('common.cancel') }}</UButton>
    </template>
  </UModal>
</template>
