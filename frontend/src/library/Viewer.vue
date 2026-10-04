<script setup lang="ts">
// Full-window viewer. Shows the 1024px preview instantly, then swaps in the
// original when the WebView can display it (JPEG, PNG, WebP, AVIF). Its
// photos, the one beside each and what may be done with it come from a
// source: the library grid unless one is given (the Blocked screen's browser).
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UTooltip from '@nuxt/ui/components/Tooltip.vue'
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { engine, isCancelled, originalUrl, THUMB_LARGE, thumbUrl, videosAvailable } from '../bridge'
import type { AssetDetail } from '../bridge'
import { icons } from '../design/icons'
import { dispatchKey, runCommand, useCommands } from '../shared/commands'
import { errorText } from '../shared/errors'
import FeatureState from '../shared/FeatureState.vue'
import { dateTime } from '../shared/format'
import { t } from '../shared/i18n'
import Icon from '../shared/Icon.vue'
import { isTyping, ownsArrows } from '../shared/keys'
import { useLayer } from '../shared/overlay'
import { libraryViewerSource, useLibrary } from './store'
import type { ViewerSource } from './store'
import DetailsPanel from './DetailsPanel.vue'
import VideoPlayer from './VideoPlayer.vue'

const props = defineProps<{ source?: ViewerSource }>()
const lib = useLibrary()
const src = computed(() => props.source ?? libraryViewerSource(lib))
const item = computed(() => src.value.item)
const actions = computed(() => src.value.actions)
const detail = ref<AssetDetail | null>(null)
const originalReady = ref(false)
const root = ref<HTMLElement | null>(null)
const loading = ref(false)
const loadError = ref('')
const layer = useLayer()
let previousFocus: Element | null = null
let detailRequest = 0

watch(
  () => src.value.id,
  async (id, _old, onCleanup) => {
    const request = ++detailRequest
    onCleanup(() => detailRequest++)
    originalReady.value = false
    detail.value = null
    loadError.value = ''
    loading.value = !!id
    if (!id) return
    try {
      const d = await engine.asset(id)
      if (request !== detailRequest) return
      detail.value = d
    } catch (e) {
      if (request === detailRequest && !isCancelled(e)) loadError.value = errorText(e)
    } finally {
      if (request === detailRequest) loading.value = false
    }
  },
  { immediate: true },
)

function go(dir: 1 | -1) {
  void src.value.step(dir)
}

function close() {
  src.value.close()
}

function onMediaDoubleClick() {
  if (!detail.value?.video) close()
}

// Listens in the bubble phase, so a control that handles a key itself (a
// picker closing its menu on Escape) stops it before it reaches the viewer.
function onKey(e: KeyboardEvent) {
  if (e.defaultPrevented || e.ctrlKey || e.altKey || e.metaKey) return
  if (!layer.owns()) return
  // Escape belongs to the browser while the player is fullscreen.
  if (e.key === 'Escape' && document.fullscreenElement) return
  if (e.key === 'Tab') {
    trapTab(e)
    return
  }
  // Typing in the details panel (adding a tag): only Escape still closes.
  if (isTyping(e) && e.key !== 'Escape') return
  // Playback controls keep their own arrow keys.
  const slider = e.target instanceof Element && e.target.closest('[role="slider"], [role="separator"]')
  if ((ownsArrows(e) || slider) && (e.key === 'ArrowLeft' || e.key === 'ArrowRight')) return
  // These viewer shortcuts ignored Shift before the command registry did the matching.
  if (e.shiftKey && (/^[fi]$/i.test(e.key) || ['Escape', 'ArrowLeft', 'ArrowRight'].includes(e.key))) {
    const plain = new KeyboardEvent('keydown', { key: e.key.toLowerCase() })
    if (dispatchKey(plain, 'viewer')) e.preventDefault()
    return
  }
  dispatchKey(e, 'viewer')
}

useCommands([
  {
    id: 'viewer.close',
    group: 'view',
    title: () => t('common.close'),
    icon: 'close',
    keys: 'Escape',
    zone: 'viewer',
    run: close,
  },
  {
    id: 'viewer.previous',
    group: 'view',
    title: () => t('viewer.prev'),
    icon: 'chevron-left',
    keys: 'ArrowLeft',
    zone: 'viewer',
    run: () => go(-1),
  },
  {
    id: 'viewer.next',
    group: 'view',
    title: () => t('viewer.next'),
    icon: 'chevron-right',
    keys: 'ArrowRight',
    zone: 'viewer',
    run: () => go(1),
  },
  {
    id: 'viewer.favourite',
    group: 'media',
    title: () => t(item.value?.f ? 'details.unfavourite' : 'details.favourite'),
    icon: 'heart',
    keys: 'F',
    zone: 'viewer',
    available: () => !!actions.value.favourite,
    run: () => actions.value.favourite?.(),
  },
  {
    id: 'viewer.details',
    group: 'view',
    title: () => t('library.info'),
    icon: 'info',
    keys: 'I',
    zone: 'viewer',
    available: () => actions.value.details,
    run: () => (lib.showDetails = !lib.showDetails),
  },
  {
    id: 'viewer.trash',
    group: 'media',
    title: () => t('trash.move'),
    icon: 'trash',
    zone: 'viewer',
    available: () => !!actions.value.moveToTrash,
    run: () => actions.value.moveToTrash?.(),
  },
  {
    id: 'viewer.restore',
    group: 'media',
    title: () => t('trash.restore'),
    icon: 'restore',
    zone: 'viewer',
    available: () => !!actions.value.restore,
    run: () => actions.value.restore?.(),
  },
  {
    id: 'viewer.delete',
    group: 'media',
    title: () => t('trash.deleteFromDisk'),
    icon: 'trash',
    zone: 'viewer',
    available: () => !!actions.value.deleteFromDisk,
    run: () => actions.value.deleteFromDisk?.(),
  },
])

function trapTab(e: KeyboardEvent) {
  const controls = (document.fullscreenElement ?? root.value)?.querySelectorAll<HTMLElement>(
    'button:not(:disabled), a[href], input:not(:disabled), select:not(:disabled), [tabindex="0"]',
  )
  const first = controls?.[0]
  const last = controls?.[controls.length - 1]
  if (!first || !last) {
    e.preventDefault()
    root.value?.focus()
  } else if (e.shiftKey && (document.activeElement === first || document.activeElement === root.value)) {
    e.preventDefault()
    last.focus()
  } else if (!e.shiftKey && (document.activeElement === last || document.activeElement === root.value)) {
    e.preventDefault()
    first.focus()
  }
}

// A modal: the app behind it can't be tabbed to or clicked while it is open
// (the viewer is outside it, in <body>; a confirmation can still show).
function setBehind(inert: boolean) {
  document.querySelector('.shell')?.toggleAttribute('inert', inert)
}

onMounted(() => {
  previousFocus = document.activeElement
  window.addEventListener('keydown', onKey)
  setBehind(true)
  root.value?.focus()
})
onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKey)
  setBehind(false)
  if (previousFocus instanceof HTMLElement && previousFocus.isConnected) previousFocus.focus()
})
</script>

<template>
  <Teleport to="body">
    <div
      ref="root"
      class="viewer"
      role="dialog"
      aria-modal="true"
      :aria-busy="loading"
      :aria-label="detail ? dateTime(detail.takenAt) : t('viewer.title')"
      tabindex="-1"
    >
      <div class="stage">
        <header class="bar">
          <UTooltip :text="t('common.withKey', { label: t('common.close'), key: 'Esc' })">
            <UButton
              class="viewer-control"
              :icon="icons.close"
              :aria-label="t('common.close')"
              variant="ghost"
              square
              @click="runCommand('viewer.close')"
            />
          </UTooltip>
          <span class="date">{{ detail ? dateTime(detail.takenAt) : '' }}</span>
          <span v-if="loading" class="date" role="status">{{ t('details.loading') }}</span>
          <div class="grow" />
          <!-- In the Trash the two actions are spelt out: what each does to
               the file must be read without hovering. -->
          <UButton
            v-if="actions.restore"
            class="viewer-control"
            :icon="icons.restore"
            variant="ghost"
            @click="runCommand('viewer.restore')"
          >
            {{ t('trash.restore') }}
          </UButton>
          <UButton
            v-if="actions.deleteFromDisk"
            class="viewer-control"
            :icon="icons.recycle"
            variant="ghost"
            @click="runCommand('viewer.delete')"
          >
            {{ t('trash.deleteFromDisk') }}
          </UButton>
          <UTooltip
            v-if="actions.favourite"
            :text="t('common.withKey', { label: t(item?.f ? 'details.unfavourite' : 'details.favourite'), key: 'F' })"
          >
            <UButton
              class="viewer-control"
              :aria-pressed="!!item?.f"
              :aria-label="t(item?.f ? 'details.unfavourite' : 'details.favourite')"
              variant="ghost"
              square
              @click="runCommand('viewer.favourite')"
            >
              <Icon name="heart" :filled="!!item?.f" />
            </UButton>
          </UTooltip>
          <UTooltip v-if="actions.moveToTrash" :text="t('trash.move')">
            <UButton
              class="viewer-control"
              :icon="icons.trash"
              :aria-label="t('trash.move')"
              variant="ghost"
              square
              @click="runCommand('viewer.trash')"
            />
          </UTooltip>
          <UTooltip v-if="actions.details" :text="t('common.withKey', { label: t('library.info'), key: 'I' })">
            <UButton
              class="viewer-control"
              :icon="icons.info"
              :aria-pressed="lib.showDetails"
              :aria-label="t('library.info')"
              variant="ghost"
              square
              @click="runCommand('viewer.details')"
            />
          </UTooltip>
        </header>

        <div class="media" @dblclick="onMediaDoubleClick">
          <FeatureState v-if="loadError" kind="error" :title="loadError" />
          <FeatureState v-else-if="!item && !loading" kind="empty" icon="image-off" :title="t('viewer.notFound')" />
          <template v-else-if="item">
            <VideoPlayer
              v-if="detail?.video && detail.id === item.id && videosAvailable()"
              :key="detail.id"
              :detail="detail"
              :at="item.at ?? 0"
              :thumb-version="item.t"
              @timeupdate="src.touch?.()"
            />
            <UAlert
              v-if="detail?.video && !videosAvailable()"
              class="playback-message"
              role="alert"
              color="error"
              variant="subtle"
              :title="t('viewer.videoUnavailable')"
            />
            <template v-else-if="!detail?.video">
              <img
                v-if="item.t"
                v-show="!originalReady"
                :key="'p' + item.id"
                class="preview"
                :src="thumbUrl(item.id, item.t, THUMB_LARGE)"
                alt=""
                draggable="false"
              />
              <img
                v-if="detail?.caps.original && detail.id === item.id"
                v-show="originalReady"
                :key="'o' + detail.id"
                class="original"
                :src="originalUrl(detail.id)"
                alt=""
                draggable="false"
                @load="originalReady = true"
              />
              <FeatureState
                v-if="!item.t && !detail?.caps.original"
                :kind="loading ? 'loading' : 'empty'"
                icon="image-off"
                :title="loading ? t('details.loading') : t('viewer.noPreview')"
              />
            </template>
          </template>
          <FeatureState v-else kind="loading" :title="t('details.loading')" />
        </div>

        <UTooltip :text="t('viewer.prev')" :content="{ side: 'right' }">
          <UButton
            class="viewer-control nav prev"
            :icon="icons['chevron-left']"
            :aria-label="t('viewer.prev')"
            variant="ghost"
            square
            @click="runCommand('viewer.previous')"
          />
        </UTooltip>
        <UTooltip :text="t('viewer.next')" :content="{ side: 'left' }">
          <UButton
            class="viewer-control nav next"
            :icon="icons['chevron-right']"
            :aria-label="t('viewer.next')"
            variant="ghost"
            square
            @click="runCommand('viewer.next')"
          />
        </UTooltip>
      </div>
      <DetailsPanel v-if="lib.showDetails && actions.details" :sel="src.sel" />
    </div>
  </Teleport>
</template>

<style scoped>
.viewer {
  position: fixed;
  inset: 0;
  z-index: var(--lumen-z-viewer);
  display: flex;
  background: var(--lumen-viewer-bg);
  color: var(--lumen-viewer-text);
  color-scheme: dark;
  outline: none;
  /* Shared states and playback alerts keep the viewer's contrast in either theme. */
  --ui-bg: var(--lumen-viewer-bg);
  --ui-bg-muted: var(--lumen-viewer-control);
  --ui-bg-elevated: var(--lumen-viewer-control);
  --ui-text: var(--lumen-viewer-text);
  --ui-text-highlighted: var(--lumen-viewer-text);
  --ui-text-muted: var(--lumen-viewer-text-muted);
  --ui-text-dimmed: var(--lumen-viewer-text-muted);
}

.stage {
  position: relative;
  flex: 1;
  min-width: 0;
  container-type: inline-size;
}

.bar {
  position: absolute;
  inset: 0 0 auto;
  z-index: 2;
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 0.25rem;
  min-height: var(--lumen-topbar-h);
  padding: 0.375rem 0.75rem;
  background: var(--lumen-viewer-bg);
}

.viewer-control {
  flex-shrink: 0;
  color: var(--lumen-viewer-text);
}

.viewer-control:hover,
.viewer-control[aria-pressed='true'] {
  background: var(--lumen-viewer-control);
  color: var(--lumen-viewer-text);
}

.date {
  min-width: 0;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
  font-size: 0.8125rem;
  color: var(--lumen-viewer-text-muted);
}

.grow {
  flex: 1;
}

.media {
  position: absolute;
  inset: var(--lumen-topbar-h) 0 0;
  display: grid;
  /* Bounded tracks stop intrinsic image sizes expanding the stage. */
  grid-template-columns: minmax(0, 1fr);
  grid-template-rows: minmax(0, 1fr);
  place-items: center;
}

.media img,
.media video {
  grid-area: 1 / 1;
  max-width: 100%;
  max-height: 100%;
  object-fit: contain;
  width: 100%;
  height: 100%;
}

.playback-message {
  position: absolute;
  z-index: 3;
  max-width: min(28rem, calc(100% - 2rem));
}

.nav {
  position: absolute;
  top: 50%;
  transform: translateY(-50%);
  width: 3rem;
  height: 4.5rem;
  background: var(--lumen-viewer-control);
  opacity: 0;
  transition: opacity var(--lumen-motion-control) var(--lumen-ease);
}

.stage:hover .nav,
.nav:focus-visible {
  opacity: 1;
}

.prev {
  left: 0.75rem;
}

.next {
  right: 0.75rem;
}

@container (max-width: 30rem) {
  .date {
    flex: 1 1 8rem;
  }

  .grow {
    display: none;
  }
}

@media (pointer: coarse), (forced-colors: active) {
  .nav {
    opacity: 1;
  }
}

@media (forced-colors: active) {
  .viewer-control {
    border: 1px solid ButtonText;
  }
}
</style>
