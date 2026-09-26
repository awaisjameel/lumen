<script setup lang="ts">
import { ref, watch } from 'vue'
import { DateSource, engine } from '../bridge'
import type { AssetDetail } from '../bridge'
import { errorText } from '../shared/errors'
import { bytes, dateTime, megapixels } from '../shared/format'
import { t } from '../shared/i18n'
import type { MessageKey } from '../shared/i18n'
import Icon from '../shared/Icon.vue'
import { useLibrary } from './store'
import type { Sel } from './store'

const props = defineProps<{ sel: Sel | null }>()
const lib = useLibrary()
const detail = ref<AssetDetail | null>(null)
const error = ref('')

watch(
  () => [props.sel?.id, lib.itemAt(props.sel)?.f] as const,
  async ([id]) => {
    error.value = ''
    if (!id) {
      detail.value = null
      return
    }
    try {
      const d = await engine.asset(id)
      if (props.sel?.id === id) detail.value = d
    } catch (e) {
      error.value = errorText(e)
    }
  },
  { immediate: true },
)

const dateFromText: Record<DateSource, MessageKey> = {
  [DateSource.DateFromEXIF]: 'details.dateFrom.exif',
  [DateSource.DateFromFilename]: 'details.dateFrom.filename',
  [DateSource.DateFromFile]: 'details.dateFrom.file',
  [DateSource.$zero]: 'details.dateFrom.file', // never stored; the scanner always sets one
}

function showInFolder(id: number) {
  error.value = ''
  engine.showInFolder(id).catch((e: unknown) => (error.value = errorText(e)))
}
</script>

<template>
  <aside class="details" :aria-label="t('details.title')">
    <header>
      <h2>{{ t('details.title') }}</h2>
      <button class="icon-btn" :title="t('viewer.close')" @click="lib.showDetails = false">
        <Icon name="close" :size="18" />
      </button>
    </header>

    <p v-if="!detail" class="muted pad">{{ error || t('details.none') }}</p>

    <div v-else class="pad body">
      <section>
        <h3>{{ t('details.taken') }}</h3>
        <p>{{ dateTime(detail.takenAt) }}</p>
        <p class="muted small">{{ t(dateFromText[detail.dateFrom]) }}</p>
      </section>

      <section v-if="detail.width">
        <h3>{{ t('details.size') }}</h3>
        <p>
          {{ detail.width }} × {{ detail.height }} · {{ megapixels(detail.width, detail.height) }} ·
          {{ bytes(detail.size) }}
        </p>
      </section>

      <section v-if="detail.camera">
        <h3>{{ t('details.camera') }}</h3>
        <p>{{ detail.camera }}</p>
      </section>

      <section v-if="detail.lat !== null && detail.lon !== null">
        <h3>{{ t('details.location') }}</h3>
        <p class="mono">{{ detail.lat.toFixed(5) }}, {{ detail.lon.toFixed(5) }}</p>
      </section>

      <section>
        <h3>{{ t('details.files') }}</h3>
        <p v-if="(detail.paths?.length ?? 0) > 1" class="muted small">
          {{ t('details.copies', { n: detail.paths!.length }) }}
        </p>
        <p v-for="p in detail.paths" :key="p" class="path" :title="p">{{ p }}</p>
      </section>

      <div class="actions">
        <button class="btn" @click="lib.toggleFavourite(props.sel)">
          <Icon :name="detail.favourite ? 'heart-filled' : 'heart'" :size="16" />
          {{ detail.favourite ? t('details.unfavourite') : t('details.favourite') }}
        </button>
        <button class="btn" @click="showInFolder(detail.id)">
          <Icon name="folder" :size="16" />
          {{ t('details.showInFolder') }}
        </button>
      </div>
      <p v-if="error" class="error small">{{ error }}</p>
    </div>
  </aside>
</template>

<style scoped>
.details {
  width: var(--details-w);
  flex-shrink: 0;
  border-left: 1px solid var(--border);
  background: var(--surface);
  overflow-y: auto;
  user-select: text;
}
header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  height: var(--topbar-h);
  padding: 0 8px 0 20px;
}
h2 {
  margin: 0;
  font: 600 16px var(--font-display);
}
.pad {
  padding: 0 20px 20px;
}
section {
  margin-bottom: 18px;
}
h3 {
  margin: 0 0 4px;
  font-size: 12px;
  font-weight: 600;
  color: var(--text-3);
  text-transform: uppercase;
  letter-spacing: 0.04em;
}
p {
  margin: 0;
}
.small {
  font-size: 12px;
}
.mono {
  font-variant-numeric: tabular-nums;
}
.path {
  font-size: 12px;
  color: var(--text-2);
  word-break: break-all;
  margin-bottom: 4px;
}
.error {
  margin-top: 12px;
  color: var(--danger);
}
.actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
</style>
