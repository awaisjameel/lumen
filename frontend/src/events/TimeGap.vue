<script setup lang="ts">
// The time-gap control in the Events view bar: a draft gap previews how many
// events it would make (the engine counts without saving), and Apply saves it
// with Undo. The parent can open it and shows the regrouping progress.
import UButton from '@nuxt/ui/components/Button.vue'
import UPopover from '@nuxt/ui/components/Popover.vue'
import USlider from '@nuxt/ui/components/Slider.vue'
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { engine, EVENT_GAP_DEFAULT, EVENT_GAP_MAX, EVENT_GAP_MIN, isCancelled } from '../bridge'
import { icons } from '../design/icons'
import Icon from '../shared/Icon.vue'
import IconButton from '../shared/IconButton.vue'
import { errorText } from '../shared/errors'
import { formatNumber, t } from '../shared/i18n'
import { notify } from '../shared/notify'
import { useLibrary } from '../library/store'

const props = defineProps<{ count: number; reload: () => Promise<void> }>()
const open = defineModel<boolean>('open', { default: false })
const working = defineModel<boolean>('working', { default: false })
/** How long the slider rests before its draft is counted, so dragging reads once. */
const PREVIEW_DELAY_MS = 200
const lib = useLibrary()
const draft = ref(EVENT_GAP_DEFAULT)
const error = ref('')
const refreshFailed = ref(false)
const previewCount = ref<number | null>(null)
const previewError = ref('')
const saved = computed(() => lib.settings.eventGapHours)
/** The draft's event count: the list's own when the draft is the saved gap. */
const shown = computed(() => (draft.value === saved.value ? props.count : previewCount.value))
const previewText = computed(() => {
  const n = shown.value
  if (n === null) return t('events.previewCounting')
  return n === props.count
    ? t('events.previewSame')
    : n < props.count
      ? t('events.previewFewer')
      : t('events.previewMore')
})
let request: AbortController | undefined
let timer: number | undefined

function stopPreview() {
  window.clearTimeout(timer)
  request?.abort()
  request = undefined
}
function preview(hours: number) {
  stopPreview()
  previewError.value = ''
  previewCount.value = null
  if (hours === saved.value) return
  timer = window.setTimeout(async () => {
    const current = new AbortController()
    request = current
    try {
      const n = await engine.eventCount(hours, current.signal)
      if (request === current) previewCount.value = n
    } catch (e) {
      if (request === current && !isCancelled(e)) previewError.value = errorText(e)
    }
  }, PREVIEW_DELAY_MS)
}
watch(open, (value) => {
  if (value) {
    draft.value = saved.value
    error.value = ''
  } else stopPreview()
})
watch(draft, (hours) => {
  if (open.value) preview(hours)
})
onBeforeUnmount(stopPreview)

async function apply(value: number, undo = true) {
  if (working.value || !Number.isInteger(value) || value < EVENT_GAP_MIN || value > EVENT_GAP_MAX) return
  const previous = saved.value
  working.value = true
  error.value = ''
  refreshFailed.value = false
  let changed = false
  try {
    await lib.saveEventGap(value)
    if (lib.settingErrors.eventGapHours && saved.value !== value) {
      error.value = lib.settingErrors.eventGapHours
      return
    }
    open.value = false
    changed = true
    if (lib.settingErrors.eventGapHours) error.value = lib.settingErrors.eventGapHours
    await props.reload()
  } catch (e) {
    error.value = errorText(e)
    refreshFailed.value = changed
  } finally {
    working.value = false
    // Publish Undo once it can run, including after a failed list refresh.
    if (changed)
      notify({
        id: 'events.gap',
        kind: 'success',
        title: t('events.gapSaved', { n: formatNumber(value) }),
        actions: undo ? [{ label: t('blocked.undo'), run: () => void apply(previous, false) }] : undefined,
      })
  }
}
async function retryReload() {
  error.value = ''
  working.value = true
  try {
    await props.reload()
    refreshFailed.value = false
  } catch (e) {
    error.value = errorText(e)
  } finally {
    working.value = false
  }
}
</script>

<template>
  <UPopover v-model:open="open" :content="{ side: 'bottom', align: 'start' }" :dismissible="!working">
    <UButton
      :icon="icons.clock"
      :trailing-icon="icons['chevron-down']"
      variant="ghost"
      color="neutral"
      :disabled="working"
      :title="t('events.gapAdjust')"
      data-action="event-gap"
      >{{ t('events.gapButton', { n: formatNumber(saved) }) }}</UButton
    >
    <template #content>
      <section class="gap-panel" :aria-label="t('events.gapTitle')" :aria-busy="working">
        <header>
          <h2>{{ t('events.gapTitle') }}</h2>
          <IconButton
            :icon="icons.close"
            :label="t('events.gapClose')"
            variant="ghost"
            color="neutral"
            :disabled="working"
            @click="open = false"
          />
        </header>
        <p>{{ t('events.gapHint') }}</p>
        <div class="slider-row">
          <label id="event-gap-label"
            >{{ t('events.quietGap') }}
            <span>{{ t('events.gapRange', { min: EVENT_GAP_MIN, max: EVENT_GAP_MAX }) }}</span></label
          ><USlider
            v-model="draft"
            :min="EVENT_GAP_MIN"
            :max="EVENT_GAP_MAX"
            :step="1"
            :disabled="working"
            aria-labelledby="event-gap-label"
            class="flex-1"
          /><output>{{ t('events.hours', { n: formatNumber(draft) }) }}</output>
        </div>
        <div class="preview" data-gap-preview aria-live="polite" :aria-busy="shown === null && !previewError">
          <Icon name="events" :size="20" />
          <template v-if="previewError">
            <span role="alert">{{ previewError }}</span>
            <UButton size="sm" variant="ghost" @click="preview(draft)">{{ t('common.retry') }}</UButton>
          </template>
          <template v-else>
            <strong v-if="shown !== null">{{
              t(shown === 1 ? 'events.groupCountOne' : 'events.groupCount', { n: formatNumber(shown) })
            }}</strong>
            <span>{{ previewText }}</span>
          </template>
        </div>
        <p>{{ t('events.regrouping') }}</p>
        <p v-if="working" role="status" class="activity"><span class="lumen-light-dot" />{{ t('events.working') }}</p>
        <p v-if="error" role="alert" class="failure">{{ error }}</p>
        <footer>
          <UButton variant="ghost" color="neutral" :disabled="working" @click="draft = EVENT_GAP_DEFAULT">{{
            t('events.gapReset', { n: EVENT_GAP_DEFAULT })
          }}</UButton
          ><UButton
            color="primary"
            variant="solid"
            :loading="working"
            :disabled="working || draft === saved"
            :title="draft === saved ? t('events.gapUnchanged') : undefined"
            @click="apply(draft)"
            >{{ t('events.gapApply') }}</UButton
          >
        </footer>
      </section>
    </template>
  </UPopover>
  <p v-if="error && !open" role="alert" class="failure">
    {{ error }}
    <UButton variant="ghost" size="sm" :disabled="working" @click="refreshFailed ? retryReload() : (open = true)">{{
      t('common.retry')
    }}</UButton>
  </p>
</template>

<style scoped>
.gap-panel {
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
  width: min(40rem, calc(100vw - 3rem));
  max-height: min(36rem, calc(100dvh - 8rem));
  overflow-y: auto;
  padding: 1rem;
}
header,
footer,
.slider-row,
.activity,
.preview {
  display: flex;
  align-items: center;
  gap: 0.75rem;
}
header h2 {
  flex: 1;
  font-weight: 600;
  color: var(--ui-text-highlighted);
}
footer {
  justify-content: flex-end;
  flex-wrap: wrap;
}
p,
label,
.preview span {
  color: var(--ui-text-toned);
  font-size: 0.8125rem;
}
label {
  font-weight: 600;
  white-space: nowrap;
}
label span {
  color: var(--ui-text-dimmed);
  font-weight: 400;
}
output {
  color: var(--ui-text-highlighted);
  font-weight: 600;
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}
.preview {
  flex-wrap: wrap;
  min-height: 2.75rem;
  padding: 0.5rem 0.75rem;
  border-radius: var(--lumen-r-md);
  background: var(--lumen-bg-sunken);
  color: var(--ui-text-toned);
}
.preview strong {
  color: var(--ui-text-highlighted);
  font-size: 0.9375rem;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}
.failure,
.preview [role='alert'] {
  color: var(--ui-error);
}
</style>
