<script setup lang="ts">
// Full-window viewer. Shows the 1024px preview instantly, then swaps in the
// original when the WebView can display it (JPEG, PNG, WebP, AVIF).
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { engine, originalUrl, Playback, playUrl, THUMB_LARGE, thumbUrl } from '../bridge'
import type { AssetDetail } from '../bridge'
import { orFallback } from '../shared/errors'
import { dateTime, duration } from '../shared/format'
import { t } from '../shared/i18n'
import Icon from '../shared/Icon.vue'
import { isTyping } from '../shared/keys'
import { useLibrary } from './store'
import DetailsPanel from './DetailsPanel.vue'

const lib = useLibrary()
const detail = ref<AssetDetail | null>(null)
const originalReady = ref(false)
const root = ref<HTMLElement | null>(null)
const video = ref<HTMLVideoElement | null>(null)
const videoMode = ref<Playback>(Playback.PlayConvert)
const videoStart = ref(0)
const videoPosition = ref(0)
const videoFailed = ref(false)
const converting = computed(() => videoMode.value === Playback.PlayConvert)
const videoSrc = computed(() => {
  const d = detail.value
  if (!d?.video) return ''
  return converting.value ? playUrl(d.id, videoStart.value) : originalUrl(d.id)
})

watch(
  () => lib.viewer?.id,
  async (id) => {
    originalReady.value = false
    detail.value = null
    videoStart.value = lib.viewerItem?.at ?? 0
    videoPosition.value = videoStart.value
    videoFailed.value = false
    if (!id) return
    const d = await orFallback(engine.asset(id), null)
    if (lib.viewer?.id === id) {
      detail.value = d
      if (d?.video) {
        const hevc = document.createElement('video').canPlayType('video/mp4; codecs="hvc1"')
        videoMode.value = d.video.play === Playback.PlayHEVC && !hevc ? Playback.PlayConvert : d.video.play
      }
    }
  },
  { immediate: true },
)

async function go(dir: 1 | -1) {
  if (!lib.viewer) return
  const next = await lib.neighbour(lib.viewer, dir)
  if (next) lib.viewer = next
}

function close() {
  lib.viewer = null
}

function videoReady() {
  if (video.value && !converting.value && videoStart.value > 0) {
    video.value.currentTime = videoStart.value / 1000
  }
}

function videoTime() {
  if (video.value) videoPosition.value = videoStart.value + video.value.currentTime * 1000
}

function seekVideo(e: Event) {
  const target = e.target
  if (!(target instanceof HTMLInputElement)) return
  const at = Number(target.value)
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

function onKey(e: KeyboardEvent) {
  if (e.ctrlKey || e.altKey || e.metaKey) return
  // Typing in the details panel (adding a tag): only Escape still closes.
  if (isTyping(e) && e.key !== 'Escape') return
  switch (e.key) {
    case 'Escape':
      close()
      break
    case 'ArrowLeft':
      void go(-1)
      break
    case 'ArrowRight':
      void go(1)
      break
    case 'f':
    case 'F':
      void lib.toggleFavourite(lib.viewer)
      break
    case 'i':
    case 'I':
      lib.showDetails = !lib.showDetails
      break
    default:
      return
  }
  e.preventDefault()
  e.stopPropagation()
}

onMounted(() => {
  window.addEventListener('keydown', onKey, true)
  root.value?.focus()
})
onBeforeUnmount(() => window.removeEventListener('keydown', onKey, true))
</script>

<template>
  <div ref="root" class="viewer" role="dialog" aria-modal="true" tabindex="-1">
    <div class="stage">
      <header class="bar">
        <button class="icon-btn" :title="t('viewer.close') + ' (Esc)'" @click="close">
          <Icon name="close" />
        </button>
        <span class="date">{{ detail ? dateTime(detail.takenAt) : '' }}</span>
        <div class="spacer" />
        <button
          class="icon-btn"
          :aria-pressed="!!lib.viewerItem?.f"
          :title="t('details.favourite') + ' (F)'"
          @click="lib.toggleFavourite(lib.viewer)"
        >
          <Icon :name="lib.viewerItem?.f ? 'heart-filled' : 'heart'" />
        </button>
        <button
          class="icon-btn"
          :aria-pressed="lib.showDetails"
          :title="t('library.info') + ' (I)'"
          @click="lib.showDetails = !lib.showDetails"
        >
          <Icon name="info" />
        </button>
      </header>

      <div v-if="lib.viewerItem" class="media" @dblclick="onMediaDoubleClick">
        <video
          v-if="detail?.video && detail.id === lib.viewerItem.id"
          ref="video"
          :key="videoSrc"
          :src="videoSrc"
          :poster="lib.viewerItem.t ? thumbUrl(detail.id, lib.viewerItem.t, THUMB_LARGE) : undefined"
          controls
          autoplay
          @loadedmetadata="videoReady"
          @timeupdate="videoTime"
          @error="videoError"
        />
        <p v-if="videoFailed" class="video-error" role="alert">{{ t('viewer.videoFailed') }}</p>
        <template v-else>
          <img
            v-if="lib.viewerItem.t"
            v-show="!originalReady"
            :key="'p' + lib.viewerItem.id"
            class="preview"
            :src="thumbUrl(lib.viewerItem.id, lib.viewerItem.t, THUMB_LARGE)"
            alt=""
            draggable="false"
          />
          <img
            v-if="detail?.webNative && detail.id === lib.viewerItem.id"
            v-show="originalReady"
            :key="'o' + detail.id"
            class="original"
            :src="originalUrl(detail.id)"
            alt=""
            draggable="false"
            @load="originalReady = true"
          />
          <div v-if="!lib.viewerItem.t && !detail?.webNative" class="none">
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
          @change="seekVideo"
        />
        <span>{{ duration(detail.video.duration) }}</span>
      </div>

      <button class="nav prev" :title="t('viewer.prev')" @click="go(-1)">
        <Icon name="chevron-left" :size="28" />
      </button>
      <button class="nav next" :title="t('viewer.next')" @click="go(1)">
        <Icon name="chevron-right" :size="28" />
      </button>
    </div>
    <DetailsPanel v-if="lib.showDetails" :sel="lib.viewer" class="panel" />
  </div>
</template>

<style scoped>
.viewer {
  position: fixed;
  inset: 0;
  z-index: 50;
  display: flex;
  background: var(--viewer-bg);
  color: #eee;
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
  color: #ddd;
}
.bar .icon-btn:hover {
  background: rgb(255 255 255 / 0.1);
  color: #fff;
}
.date {
  font-size: 13px;
  color: #ccc;
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
  border-radius: 6px;
  background: rgb(0 0 0 / 0.68);
  color: white;
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
  border-radius: 6px;
  background: rgb(0 0 0 / 0.8);
}
.none {
  display: grid;
  justify-items: center;
  gap: 8px;
  color: #999;
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
  color: #eee;
  cursor: pointer;
  opacity: 0;
  transition: opacity 0.15s;
}
.stage:hover .nav {
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
