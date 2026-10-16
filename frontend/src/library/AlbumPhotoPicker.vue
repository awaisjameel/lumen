<script setup lang="ts">
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UModal from '@nuxt/ui/components/Modal.vue'
import { computed, onBeforeUnmount, ref, shallowRef, watch } from 'vue'
import { useVirtualizer } from '@tanstack/vue-virtual'
import { BatchAction, engine, Filter, isCancelled } from '../bridge'
import type { GridItem } from '../bridge'
import MediaTile from '../shared/MediaTile.vue'
import { errorText, reportError } from '../shared/errors'
import { formatNumber, t } from '../shared/i18n'
import { notify } from '../shared/notify'

const props = defineProps<{ albumId: number; name: string }>()
const emit = defineEmits<{ changed: [] }>()
const open = defineModel<boolean>('open', { required: true })
const items = shallowRef<GridItem[]>([])
const selected = shallowRef(new Set<number>())
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const scroller = ref<HTMLElement | null>(null)
const buckets = shallowRef<string[]>([])
let controller: AbortController | undefined
const virtualizer = useVirtualizer(
  computed(() => ({
    count: Math.ceil(items.value.length / 4),
    getScrollElement: () => scroller.value,
    estimateSize: () => 120,
    overscan: 2,
  })),
)
const rows = computed(() =>
  virtualizer.value
    .getVirtualItems()
    .map((row) => ({ ...row, items: items.value.slice(row.index * 4, row.index * 4 + 4) })),
)
async function more() {
  if (loading.value || !buckets.value.length) return
  loading.value = true
  error.value = ''
  const day = buckets.value[0]
  if (!day) return
  const request = controller
  try {
    const result = await engine.days(day, day, { filter: Filter.FilterAll }, request?.signal)
    if (request?.signal.aborted) return
    items.value = [...items.value, ...result.flatMap((row) => row.items)]
    buckets.value = buckets.value.slice(1)
  } catch (e) {
    if (!isCancelled(e)) error.value = errorText(e)
  } finally {
    loading.value = false
  }
}
watch(
  open,
  async (value) => {
    controller?.abort()
    if (!value) return
    const request = new AbortController()
    controller = request
    items.value = []
    selected.value = new Set()
    error.value = ''
    loading.value = true
    try {
      const result = await engine.timeline({ filter: Filter.FilterAll }, request.signal)
      if (request.signal.aborted) return
      buckets.value = result.map((row) => row.day)
      loading.value = false
      await more()
    } catch (e) {
      if (!isCancelled(e)) error.value = errorText(e)
    } finally {
      loading.value = false
    }
  },
  { immediate: true },
)
onBeforeUnmount(() => controller?.abort())
function toggle(id: number) {
  const ids = new Set(selected.value)
  if (ids.has(id)) ids.delete(id)
  else ids.add(id)
  selected.value = ids
}
async function add() {
  if (!selected.value.size || busy.value) return
  busy.value = true
  error.value = ''
  try {
    const ids = [...selected.value]
    const albumId = props.albumId
    const before = new Set((await engine.selection({ filter: Filter.FilterAll, album: albumId }, [], false)).ids)
    const added: number[] = []
    for (let i = 0; i < ids.length; i += 200) {
      const result = await engine.applyBatch(ids.slice(i, i + 200), BatchAction.BatchAlbum, albumId)
      const succeeded = result.filter((row) => !row.error).map((row) => row.id)
      added.push(...succeeded.filter((id) => !before.has(id)))
      selected.value = new Set([...selected.value].filter((id) => !succeeded.includes(id)))
      const failures = result.filter((row) => row.error)
      if (failures.length) {
        error.value = failures.map((row) => row.error).join('\n')
        emit('changed')
        return
      }
    }
    notify({
      id: 'collection.added',
      kind: 'success',
      title: t('collection.photosAdded', { n: formatNumber(ids.length), name: props.name }),
      actions: added.length
        ? [
            {
              label: t('settings.undo'),
              run: () => {
                void (async () => {
                  for (let i = 0; i < added.length; i += 200) {
                    const result = await engine.applyBatch(
                      added.slice(i, i + 200),
                      BatchAction.BatchRemoveAlbum,
                      albumId,
                    )
                    const failed = result.find((row) => row.error)
                    if (failed) throw new Error(failed.error)
                  }
                  emit('changed')
                })().catch(reportError)
              },
            },
          ]
        : undefined,
    })
    emit('changed')
    open.value = false
  } catch (e) {
    error.value = errorText(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <UModal
    v-model:open="open"
    :title="t('collection.addPhotos')"
    :description="name"
    :dismissible="!busy"
    :close="!busy"
  >
    <template #body>
      <UAlert v-if="error" :title="error" color="error" role="alert" />
      <div ref="scroller" class="picker" :aria-busy="loading">
        <div :style="{ height: virtualizer.getTotalSize() + 'px', position: 'relative' }">
          <div
            v-for="row in rows"
            :key="String(row.key)"
            class="row"
            :style="{ transform: `translateY(${row.start}px)` }"
          >
            <button
              v-for="item in row.items"
              :key="item.id"
              type="button"
              :aria-label="t('collection.selectPhoto', { id: item.id })"
              :aria-pressed="selected.has(item.id)"
              :disabled="busy"
              @click="toggle(item.id)"
            >
              <MediaTile
                :id="item.id"
                :thumb="item.t"
                :video="item.v"
                checkable
                selecting
                :selected="selected.has(item.id)"
                @check="toggle(item.id)"
              />
            </button>
          </div>
        </div>
      </div>
      <p v-if="!loading && !items.length">{{ t('library.empty') }}</p>
      <UButton v-if="buckets.length" color="neutral" variant="outline" :loading="loading" @click="more">{{
        t('collection.loadMore')
      }}</UButton>
    </template>
    <template #footer>
      <span>{{ t('selection.count', { n: selected.size }) }}</span>
      <UButton color="neutral" variant="ghost" :disabled="busy" @click="open = false">{{ t('common.cancel') }}</UButton>
      <UButton
        :disabled="!selected.size"
        :loading="busy"
        :title="!selected.size ? t('collection.selectFirst') : undefined"
        @click="add"
        >{{ t('collection.addSelected') }}</UButton
      >
    </template>
  </UModal>
</template>

<style scoped>
.picker {
  height: 19rem;
  overflow: auto;
  margin-bottom: 0.75rem;
}
.row {
  position: absolute;
  inset: 0 0 auto;
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 0.25rem;
  height: 7.5rem;
  padding-bottom: 0.25rem;
}
.row button {
  min-width: 0;
  border: 0;
  padding: 0;
  cursor: pointer;
  border-radius: var(--lumen-r-sm);
}
</style>
