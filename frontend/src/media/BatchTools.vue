<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import UModal from '@nuxt/ui/components/Modal.vue'
import USelect from '@nuxt/ui/components/Select.vue'
import { AlbumKind, BatchAction, engine } from '../bridge'
import type { Selection } from '../bridge'
import { useAlbums } from '../albums/store'
import { confirm } from '../shared/confirm'
import { errorText, failureText, logError } from '../shared/errors'
import { t } from '../shared/i18n'
import { showTrashUndo } from '../trash/undo'
import { blockAssets } from '../shared/block'
import OutcomeList from './OutcomeList.vue'

type Operation = BatchAction | 'tag' | 'block' | 'delete'
interface Result {
  id: number
  error: string
  state: 'done' | 'failed' | 'cancelled'
}

const props = defineProps<{ ids: number[]; selection?: Selection | null; trash: boolean }>()
const emit = defineEmits<{ changed: [] }>()
const open = defineModel<boolean>('open', { default: false })
// How many assets one call to the engine takes: few enough that Cancel and the
// progress line keep up, enough that a whole library is not a call per photo.
const PART = 200
const albums = useAlbums()
const operation = ref<Operation>(BatchAction.BatchFavourite)
const tag = ref('')
const album = ref(0)
const busy = ref(false)
const stopped = ref(false)
let generation = 0
const inputKey = computed(() =>
  JSON.stringify({
    ids: props.ids,
    selection: props.selection,
    trash: props.trash,
    operation: operation.value,
    tag: tag.value,
    album: album.value,
  }),
)
const error = ref('')
const results = ref<Result[]>([])
const batchTotal = ref(0)
const needsAlbum = computed(
  () => operation.value === BatchAction.BatchAlbum || operation.value === BatchAction.BatchRemoveAlbum,
)
onBeforeUnmount(() => {
  generation++
  stopped.value = true
})
const options = computed((): { value: Operation; label: string }[] =>
  props.trash
    ? [
        { value: BatchAction.BatchRestore, label: t('trash.restore') },
        { value: 'delete', label: t('trash.deleteFromDisk') },
      ]
    : [
        { value: BatchAction.BatchFavourite, label: t('details.favourite') },
        { value: BatchAction.BatchUnfavourite, label: t('details.unfavourite') },
        { value: 'tag', label: t('details.addTag') },
        { value: BatchAction.BatchAlbum, label: t('files.addAlbum') },
        { value: BatchAction.BatchRemoveAlbum, label: t('files.removeAlbum') },
        { value: BatchAction.BatchTrash, label: t('trash.move') },
        { value: 'block', label: t('blocked.block') },
      ],
)
const albumOptions = computed(() =>
  albums.list.filter((a) => a.kind === AlbumKind.AlbumManual).map((a) => ({ value: a.id, label: a.name })),
)
watch(
  open,
  (value) => {
    if (!value) {
      generation++
      stopped.value = true
    }
    if (value) {
      operation.value = props.trash ? BatchAction.BatchRestore : BatchAction.BatchFavourite
      results.value = []
      void albums.load().catch(logError)
    }
  },
  { immediate: true, flush: 'sync' },
)
const cancelled = (ids: number[]): Result[] => ids.map((id) => ({ id, error: '', state: 'cancelled' }))
async function apply() {
  const ids = [...props.ids]
  const selectedOperation = operation.value
  const name = tag.value.trim(),
    albumID = album.value
  if (
    busy.value ||
    !open.value ||
    !ids.length ||
    (selectedOperation === 'tag' && !name) ||
    (needsAlbum.value && !albumID)
  )
    return
  const mine = ++generation
  const isStopped = () => stopped.value || mine !== generation
  const key = inputKey.value
  const selection = props.selection
  busy.value = true
  batchTotal.value = ids.length
  stopped.value = false
  error.value = ''
  results.value = []
  try {
    if (!(await confirm(t('files.batch'), t('files.batchConfirm', { n: ids.length }), t('files.applyBatch')))) return
    if (mine !== generation || key !== inputKey.value) return
    if (selection) await engine.validateSelection(selection)
    if (mine !== generation || key !== inputKey.value || isStopped()) return
    if (selectedOperation === 'block') {
      await blockAssets(
        ids,
        (id, error) => {
          if (mine === generation) results.value.push({ id, error, state: error ? 'failed' : 'done' })
        },
        isStopped,
      )
      if (mine !== generation) return
      const processed = new Set(results.value.map((r) => r.id))
      results.value.push(...cancelled(ids.filter((id) => !processed.has(id))))
      return
    }
    if (selectedOperation === 'delete') {
      const selection = await engine.trashSelection(ids)
      if (mine !== generation || isStopped()) return
      if (
        !(await confirm(
          t('trash.deleteFromDisk'),
          t('files.recycleConfirm', { n: selection.assets.length }),
          t('trash.deleteFromDisk'),
        ))
      )
        return
      if (mine !== generation || isStopped()) return
      for (const id of selection.assets) {
        if (isStopped()) {
          results.value.push(...cancelled([id]))
          continue
        }
        try {
          const item = await engine.trashSelection([id])
          if (mine !== generation || isStopped()) {
            if (mine === generation) results.value.push(...cancelled([id]))
            continue
          }
          const outcome = await engine.deleteFromDisk(item)
          if (mine !== generation) return
          results.value.push({
            id,
            error: outcome.kept.length ? t('files.kept', { n: outcome.kept.length }) : '',
            state: outcome.kept.length ? 'failed' : 'done',
          })
        } catch (e) {
          if (mine !== generation) return
          results.value.push({ id, error: errorText(e), state: 'failed' })
        }
      }
    } else {
      for (let from = 0; from < ids.length; from += PART) {
        const part = ids.slice(from, from + PART)
        if (isStopped()) {
          results.value.push(...cancelled(part))
          continue
        }
        try {
          const outcomes =
            selectedOperation === 'tag'
              ? await engine.tagAssets(part, name)
              : await engine.applyBatch(part, selectedOperation, albumID)
          if (mine !== generation) return
          results.value.push(
            ...outcomes.map((o): Result => ({
              id: o.id,
              error: o.error ? failureText(o.code, o.error) : '',
              state: o.error ? 'failed' : 'done',
            })),
          )
        } catch (e) {
          if (mine !== generation) return
          // The call itself failed (the tag's name, say): none of its assets changed.
          const error = errorText(e)
          results.value.push(...part.map((id): Result => ({ id, error, state: 'failed' })))
        }
      }
      if (selectedOperation === BatchAction.BatchTrash) {
        const done = results.value.filter((r) => r.state === 'done').map((r) => r.id)
        if (done.length)
          showTrashUndo(done, async (ids) => {
            await engine.restoreAssets(ids)
            emit('changed')
          })
      }
    }
  } catch (e) {
    if (mine === generation) error.value = errorText(e)
  } finally {
    busy.value = false
    if (mine === generation) emit('changed')
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
      <USelect v-if="needsAlbum" v-model="album" :items="albumOptions" :disabled="busy" :aria-label="t('nav.albums')" />
      <p role="status">
        {{
          t('files.batchProgress', {
            n: results.filter((r) => r.state !== 'cancelled').length,
            total: busy || results.length ? batchTotal : ids.length,
            failed: results.filter((r) => r.error).length,
          })
        }}
      </p>
      <OutcomeList v-slot="{ item: result }" :items="results">
        <p :key="result.id">{{ result.id }}: {{ t(`files.outcome.${result.state}`) }} {{ result.error }}</p>
      </OutcomeList>
    </template>
    <template #footer>
      <UButton :disabled="busy" @click="apply">{{ t('files.applyBatch') }}</UButton>
      <UButton v-if="busy" variant="outline" @click="stopped = true">{{ t('common.cancel') }}</UButton>
    </template>
  </UModal>
</template>
