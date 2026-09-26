<script setup lang="ts">
import { ref, watch } from 'vue'
import { DateSource, engine, TagSource } from '../bridge'
import type { AssetDetail, AssetTag } from '../bridge'
import { errorText, logError } from '../shared/errors'
import { bytes, dateTime, megapixels } from '../shared/format'
import { t } from '../shared/i18n'
import type { MessageKey } from '../shared/i18n'
import Icon from '../shared/Icon.vue'
import { useLibrary } from './store'
import type { Sel } from './store'

const props = defineProps<{ sel: Sel | null }>()
const lib = useLibrary()
const detail = ref<AssetDetail | null>(null)
const tags = ref<AssetTag[]>([])
const newTag = ref('')
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

// Tags: reload when the photo changes, and as the AI works through the
// library (its tags may arrive while the panel is open).
watch(
  () => [props.sel?.id, lib.progress?.embedding] as const,
  ([id]) => {
    if (!id) {
      tags.value = []
      return
    }
    void loadTags(id)
  },
  { immediate: true },
)

async function loadTags(id: number) {
  try {
    const got = await engine.assetTags(id)
    if (props.sel?.id === id) tags.value = got
  } catch (e) {
    logError(e)
  }
}

/** Change this photo's tags, then show the result (and refresh a Thing's grid). */
async function editTags(id: number, fn: () => Promise<void>) {
  error.value = ''
  try {
    await fn()
    await loadTags(id)
    void lib.refresh().catch(logError)
  } catch (e) {
    error.value = errorText(e)
  }
}

function addTag(id: number) {
  const name = newTag.value.trim()
  if (!name) return
  newTag.value = ''
  void editTags(id, () => engine.tagAsset(id, name))
}

function removeTag(id: number, tag: AssetTag) {
  void editTags(id, () => engine.untagAsset(id, tag.id))
}

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
        <h3>{{ t('details.tags') }}</h3>
        <ul v-if="tags.length" class="tags">
          <li
            v-for="tag in tags"
            :key="tag.id"
            :class="{ ai: tag.source === TagSource.TagByAI }"
            :title="tag.source === TagSource.TagByAI ? t('details.byAI') : t('details.byYou')"
          >
            <RouterLink :to="{ name: 'thing', params: { id: tag.id } }">{{ tag.name }}</RouterLink>
            <button :title="t('details.removeTag', { name: tag.name })" @click="removeTag(detail.id, tag)">
              <Icon name="close" :size="12" />
            </button>
          </li>
        </ul>
        <p v-else class="muted small">{{ t('details.noTags') }}</p>
        <input
          v-model="newTag"
          class="add-tag"
          :placeholder="t('details.addTag')"
          :aria-label="t('details.addTag')"
          maxlength="60"
          @keydown.enter="addTag(detail.id)"
        />
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
.tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin: 2px 0 8px;
  padding: 0;
  list-style: none;
}
.tags li {
  display: flex;
  align-items: center;
  height: 26px;
  padding-left: 10px;
  border: 1px solid var(--accent);
  border-radius: 13px;
  background: var(--accent-soft);
  font-size: 12px;
}
.tags li.ai {
  border-color: var(--border);
  background: var(--surface-2);
}
.tags a {
  color: var(--text);
  text-decoration: none;
}
.tags a:hover {
  text-decoration: underline;
}
.tags button {
  display: grid;
  place-items: center;
  width: 24px;
  height: 24px;
  padding: 0;
  border: 0;
  background: none;
  color: var(--text-3);
  cursor: pointer;
}
.tags button:hover {
  color: var(--text);
}
.add-tag {
  width: 100%;
  height: 30px;
  margin-top: 4px;
  padding: 0 10px;
  border: 1px solid var(--border);
  border-radius: var(--radius-s);
  background: var(--surface-2);
  color: var(--text);
  font: inherit;
  font-size: 12px;
  outline: none;
}
.add-tag:focus {
  border-color: var(--accent);
}
</style>
