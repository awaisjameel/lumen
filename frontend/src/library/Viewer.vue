<script setup lang="ts">
// Full-window viewer. Shows the 1024px preview instantly, then swaps in the
// original when the WebView can display it (JPEG, PNG, WebP, AVIF). Its
// photos, the one beside each and what may be done with it come from a
// source: the library grid unless one is given (the Blocked screen's browser).
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { engine, originalUrl, Playback, playUrl, THUMB_LARGE, thumbUrl, videosAvailable, videoUrl } from '../bridge'
import type { AssetDetail } from '../bridge'
import { orFallback } from '../shared/errors'
import { dateTime, duration } from '../shared/format'
import { t } from '../shared/i18n'
import Icon from '../shared/Icon.vue'
import { isTyping, ownsArrows } from '../shared/keys'
import { libraryViewerSource, useLibrary } from './store'
import type { ViewerSource } from './store'
import DetailsPanel from './DetailsPanel.vue'

const props = defineProps<{ source?: ViewerSource }>()
const lib = useLibrary()
const src = computed(() => props.source ?? libraryViewerSource(lib))
const item = computed(() => src.value.item)
const actions = computed(() => src.value.actions)
const detail = ref<AssetDetail | null>(null)
const originalReady = ref(false)
const root = ref<HTMLElement | null>(null)
const video = ref<HTMLVideoElement | null>(null)
const videoMode = ref<Playback>(Playback.PlayConvert)
const videoStart = ref(0)
const videoPosition = ref(0)
const videoFailed = ref(false)
let detailRequest = 0
const converting = computed(() => videoMode.value === Playback.PlayConvert)
const videoSrc = computed(() => {
  const d = detail.value
  if (!d?.video) return ''
  return converting.value ? playUrl(d.id, videoStart.value) : videoUrl(d.id)
})

watch(
  () => src.value.id,
  async (id, _old, onCleanup) => {
    const request = ++detailRequest
    onCleanup(() => detailRequest++)
    originalReady.value = false
    detail.value = null
    videoStart.value = item.value?.at ?? 0
    videoPosition.value = videoStart.value
    videoFailed.value = false
    if (!id) return
    const d = await orFallback(engine.asset(id), null)
    if (request === detailRequest) {
      detail.value = d
      if (d?.video) {
        const hevc = document.createElement('video').canPlayType('video/mp4; codecs="hvc1"')
        videoMode.value = d.video.play === Playback.PlayHEVC && !hevc ? Playback.PlayConvert : d.video.play
      }
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

function videoReady() {
  if (video.value && !converting.value && videoStart.value > 0) {
    video.value.currentTime = videoStart.value / 1000
  }
}

// While the seek slider is dragged, playback must not move its thumb back.
let seeking = false

function videoTime() {
  src.value.touch?.()
  if (video.value && !seeking)
    videoPosition.value = (converting.value ? videoStart.value : 0) + video.value.currentTime * 1000
}

function startSeek() {
  seeking = true
}

function seekVideo(e: Event) {
  const target = e.target
  if (!(target instanceof HTMLInputElement)) return
  const at = Number(target.value)
  seeking = false
  videoPosition.value = at
  if (converting.value) {
    videoStart.value = at // a new stream starts here; no full converted copy is kept
  } else if (video.value) {
    video.value.currentTime = at / 1000
  }
}

function videoError() {
  if (!converting.value) {
    videoMode.value = Playback.PlayConvert
    videoStart.value = videoPosition.value
  } else {
    videoFailed.value = true
  }
}

function onMediaDoubleClick() {
  if (!detail.value?.video) close()
}

// Listens in the bubble phase, so a control that handles a key itself (a
// picker closing its menu on Escape) stops it before it reaches the viewer.
function onKey(e: KeyboardEvent) {
  if (e.defaultPrevented || e.ctrlKey || e.altKey || e.metaKey) return
  if (document.querySelector('dialog[open]')) return // a confirmation above the viewer owns the keys
  // Typing in the details panel (adding a tag): only Escape still closes.
  if (isTyping(e) && e.key !== 'Escape') return
  // The seek slider and the video's own controls move with the arrows.
  if (ownsArrows(e) && (e.key === 'ArrowLeft' || e.key === 'ArrowRight')) return
  switch (e.key) {
    case 'Escape':
      close()
      break
    case 'ArrowLeft':
      go(-1)
      break
    case 'ArrowRight':
      go(1)
      break
    case 'f':
    case 'F':
      if (!actions.value.favourite) return // a trashed photo can't be edited
      actions.value.favourite()
      break
    case 'i':
    case 'I':
      if (!actions.value.details) return
      lib.showDetails = !lib.showDetails
      break
    default:
      return
  }
  e.preventDefault()
}

// A modal: the app behind it can't be tabbed to or clicked while it is open
// (the viewer is outside it, in <body>; a confirmation can still show).
function setBehind(inert: boolean) {
  document.querySelector('.shell')?.toggleAttribute('inert', inert)
}

onMounted(() => {
  window.addEventListener('keydown', onKey)
  setBehind(true)
  root.value?.focus()
})
onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKey)
  setBehind(false)
})
</script>

<template>
  <Teleport to="body">
    <div
      ref="root"
      class="viewer"
      role="dialog"
      aria-modal="true"
      :aria-label="detail ? dateTime(detail.takenAt) : t('viewer.title')"
      tabindex="-1"
    >
      <div class="stage">
        <header class="bar">
          <button
            class="icon-btn"
            :title="t('common.withKey', { label: t('common.close'), key: 'Esc' })"
            :aria-label="t('common.close')"
            @click="close"
          >
            <Icon name="close" />
          </button>
          <span class="date">{{ detail ? dateTime(detail.takenAt) : '' }}</span>
          <div class="spacer" />
          <button v-if="actions.restore" class="text-btn" @click="actions.restore">{{ t('trash.restore') }}</button>
          <button v-if="actions.deleteFromDisk" class="text-btn" @click="actions.deleteFromDisk">
            {{ t('trash.deleteFromDisk') }}
          </button>
          <button
            v-if="actions.favourite"
            class="icon-btn"
            :aria-pressed="!!item?.f"
            :title="t('common.withKey', { label: t('details.favourite'), key: 'F' })"
            :aria-label="t('details.favourite')"
            @click="actions.favourite"
          >
            <Icon :name="item?.f ? 'heart-filled' : 'heart'" />
          </button>
          <button
            v-if="actions.moveToTrash"
            class="icon-btn"
            :title="t('trash.move')"
            :aria-label="t('trash.move')"
            @click="actions.moveToTrash"
          >
            <Icon name="trash" />
          </button>
          <button
            v-if="actions.details"
            class="icon-btn"
            :aria-pressed="lib.showDetails"
            :title="t('common.withKey', { label: t('library.info'), key: 'I' })"
            :aria-label="t('library.info')"
            @click="lib.showDetails = !lib.showDetails"
          >
            <Icon name="info" />
          </button>
        </header>

        <div v-if="item" class="media" @dblclick="onMediaDoubleClick">
          <video
            v-if="detail?.video && detail.id === item.id && videosAvailable()"
            ref="video"
            :key="videoSrc"
            :src="videoSrc"
            :poster="item.t ? thumbUrl(detail.id, item.t, THUMB_LARGE) : undefined"
            controls
            autoplay
            @loadedmetadata="videoReady"
            @timeupdate="videoTime"
            @error="videoError"
          />
          <p v-if="detail?.video && !videosAvailable()" class="video-error" role="alert">
            {{ t('viewer.videoUnavailable') }}
          </p>
          <p v-else-if="videoFailed" class="video-error" role="alert">{{ t('viewer.videoFailed') }}</p>
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
              v-if="detail?.webNative && detail.id === item.id"
              v-show="originalReady"
              :key="'o' + detail.id"
              class="original"
              :src="originalUrl(detail.id)"
              alt=""
              draggable="false"
              @load="originalReady = true"
            />
            <div v-if="!item.t && !detail?.webNative" class="none">
              <Icon name="image-off" :size="40" />
              <p>{{ t('viewer.noPreview') }}</p>
            </div>
          </template>
        </div>

        <div v-if="detail?.video && detail.video.duration" class="video-seek">
          <span>{{ duration(videoPosition) }}</span>
          <input
            type="range"
            min="0"
            :max="detail.video.duration"
            :value="videoPosition"
            :aria-label="t('viewer.seek')"
            @input="startSeek"
            @change="seekVideo"
          />
          <span>{{ duration(detail.video.duration) }}</span>
        </div>

        <button class="nav prev" :title="t('viewer.prev')" :aria-label="t('viewer.prev')" @click="go(-1)">
          <Icon name="chevron-left" :size="28" />
        </button>
        <button class="nav next" :title="t('viewer.next')" :aria-label="t('viewer.next')" @click="go(1)">
          <Icon name="chevron-right" :size="28" />
        </button>
      </div>
      <DetailsPanel v-if="lib.showDetails && actions.details" :sel="src.sel" class="panel" />
    </div>
  </Teleport>
</template>

<style scoped>
.viewer {
  position: fixed;
  inset: 0;
  z-index: 50;
  display: flex;
  background: var(--viewer-bg);
  color: var(--viewer-text);
  outline: none;
}
.stage {
  position: relative;
  flex: 1;
  min-width: 0;
}
.bar {
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  z-index: 2;
  display: flex;
  align-items: center;
  gap: 8px;
  height: var(--topbar-h);
  padding: 0 12px;
  background: linear-gradient(rgb(0 0 0 / 0.55), transparent);
}
.bar .icon-btn {
  color: var(--viewer-text);
}
.bar .icon-btn:hover {
  background: var(--viewer-control);
  color: var(--viewer-text);
}
.bar .text-btn {
  height: 32px;
  padding: 0 12px;
  border: 1px solid var(--viewer-control);
  border-radius: var(--radius-m);
  background: transparent;
  color: var(--viewer-text);
  cursor: pointer;
}
.bar .text-btn:hover {
  background: var(--viewer-control);
}
.date {
  font-size: 13px;
  color: var(--viewer-text-2);
}
.spacer {
  flex: 1;
}
.media {
  position: absolute;
  inset: 0;
  display: grid;
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
.video-seek {
  position: absolute;
  z-index: 2;
  left: 64px;
  right: 64px;
  bottom: 56px;
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 5px 10px;
  border-radius: var(--radius-m);
  background: rgb(0 0 0 / 0.68);
  color: var(--viewer-text);
  font-size: 12px;
}
.video-seek input {
  flex: 1;
  min-width: 0;
  accent-color: var(--accent);
}
.video-error {
  position: absolute;
  z-index: 3;
  padding: 10px 14px;
  border-radius: var(--radius-m);
  background: rgb(0 0 0 / 0.8);
}
.none {
  display: grid;
  justify-items: center;
  gap: 8px;
  color: var(--viewer-text-2);
}
.nav {
  position: absolute;
  top: 50%;
  transform: translateY(-50%);
  display: grid;
  place-items: center;
  width: 48px;
  height: 72px;
  border: 0;
  border-radius: var(--radius-m);
  background: rgb(0 0 0 / 0.25);
  color: var(--viewer-text);
  cursor: pointer;
  opacity: 0;
  transition: opacity 0.15s;
}
.stage:hover .nav,
.nav:focus-visible {
  opacity: 1;
}
.nav:hover {
  background: rgb(0 0 0 / 0.5);
}
.prev {
  left: 12px;
}
.next {
  right: 12px;
}
.panel {
  color: var(--text);
}
</style>
