<script setup lang="ts">
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import USlider from '@nuxt/ui/components/Slider.vue'
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { Playback, playUrl, THUMB_LARGE, thumbUrl, videoUrl } from '../bridge'
import type { AssetDetail } from '../bridge'
import { icons } from '../design/icons'
import { duration } from '../shared/format'
import { t } from '../shared/i18n'

const props = defineProps<{ detail: AssetDetail; at: number; thumbVersion?: number }>()
const emit = defineEmits<{ timeupdate: [] }>()
const player = ref<HTMLElement | null>(null)
const video = ref<HTMLVideoElement | null>(null)
// HEVC support depends on the installed WebView's decoder.
const converting = ref(
  !props.detail.caps.directPlay ||
    (props.detail.video?.play === Playback.PlayHEVC &&
      !document.createElement('video').canPlayType('video/mp4; codecs="hvc1"')),
)
const total = computed(() => props.detail.video?.duration ?? 0)
const clamp = (at: number) => Math.max(0, total.value ? Math.min(at, total.value) : at)
const start = ref(clamp(props.at))
const position = ref(start.value)
const source = computed(() => (converting.value ? playUrl(props.detail.id, start.value) : videoUrl(props.detail.id)))
const playing = ref(false)
const wantsPlay = ref(true)
const ended = ref(false)
const ready = ref(false)
const buffering = ref(true)
const failed = ref(false)
const notice = ref('')
const volume = ref(1)
const muted = ref(false)
const rate = ref(1)
const fullscreen = ref(false)
const pip = ref(false)
const canFullscreen = ref(false)
const canPip = ref(false)
let seeking = false
let playRequest = 0
const silent = computed(() => muted.value || volume.value === 0)
const rates = [0.25, 0.5, 0.75, 1, 1.25, 1.5, 1.75, 2]

function applyPreferences() {
  if (!video.value) return
  video.value.volume = volume.value
  video.value.muted = muted.value
  video.value.playbackRate = rate.value
}

// Converted seeks replace the element. Keep pause, volume and speed across
// those replacements, and ignore completion from the discarded stream.
watch(video, () => {
  playRequest++
  ready.value = false
  playing.value = false
  pip.value = false
  buffering.value = true
  applyPreferences()
})

function videoReady() {
  ready.value = true
  if (!converting.value && video.value) video.value.currentTime = start.value / 1000
  applyPreferences()
}

function videoTime() {
  emit('timeupdate')
  if (video.value && !seeking)
    position.value = clamp((converting.value ? start.value : 0) + video.value.currentTime * 1000)
}

function startSeek(at: number | undefined) {
  if (at === undefined || !Number.isFinite(at)) return
  seeking = true
  position.value = clamp(at)
}

function seekVideo(at = position.value) {
  seeking = false
  ended.value = false
  position.value = clamp(at)
  if (converting.value) {
    // A converted stream starts at this source time. Seeking to its exact
    // end would ask the converter for an empty stream.
    const nextStart = total.value ? Math.min(position.value, Math.max(0, total.value - 1)) : position.value
    if (start.value === nextStart && video.value) video.value.currentTime = 0
    else start.value = nextStart
  } else if (video.value) {
    video.value.currentTime = position.value / 1000
  }
}

async function commitSeek() {
  // Reka commits keyboard steps before emitting the new model value.
  await nextTick()
  if (seeking) seekVideo()
}

async function togglePlayback() {
  const el = video.value
  if (!el || failed.value) return
  notice.value = ''
  const request = ++playRequest
  if (playing.value || (wantsPlay.value && !el.paused)) {
    wantsPlay.value = false
    el.pause()
    return
  }
  wantsPlay.value = true
  if (ended.value || (total.value && position.value >= total.value)) {
    const oldSource = source.value
    seekVideo(0)
    if (source.value !== oldSource) return // the new element autoplays
  }
  try {
    await el.play()
  } catch {
    if (video.value === el && request === playRequest) {
      wantsPlay.value = false
      playing.value = false
      buffering.value = false
      notice.value = t('viewer.playRejected')
    }
  }
}

function playbackState() {
  if (!video.value) return
  playing.value = !video.value.paused
  if (!video.value.error) wantsPlay.value = playing.value
  if (playing.value) ended.value = false
}

function videoEnded() {
  playing.value = false
  wantsPlay.value = false
  ended.value = true
  buffering.value = false
  if (total.value) position.value = total.value
}

function videoError() {
  if (!converting.value && props.detail.caps.convertPlay) {
    start.value = position.value
    converting.value = true
  } else {
    failed.value = true
    playing.value = false
    buffering.value = false
  }
}

function setVolume(value: number | undefined) {
  if (value === undefined) return
  volume.value = value / 100
  muted.value = value === 0
  applyPreferences()
}

function toggleMute() {
  const unmute = silent.value
  if (unmute && volume.value === 0) volume.value = 1
  muted.value = !unmute
  applyPreferences()
}

function volumeChanged() {
  if (!video.value) return
  volume.value = video.value.volume
  muted.value = video.value.muted
}

function setRate(e: Event) {
  if (!(e.target instanceof HTMLSelectElement)) return
  rate.value = Number(e.target.value)
  applyPreferences()
}

function fullscreenChanged() {
  fullscreen.value = document.fullscreenElement === player.value
}

async function toggleFullscreen() {
  notice.value = ''
  try {
    if (fullscreen.value) await document.exitFullscreen()
    else await player.value?.requestFullscreen()
  } catch {
    notice.value = t('viewer.fullscreenFailed')
  }
}

async function togglePip() {
  notice.value = ''
  try {
    if (document.pictureInPictureElement) await document.exitPictureInPicture()
    else await video.value?.requestPictureInPicture()
  } catch {
    notice.value = t('viewer.pipFailed')
  }
}

function onKey(e: KeyboardEvent) {
  if (e.defaultPrevented || e.ctrlKey || e.altKey || e.metaKey || e.shiftKey) return
  // Buttons, the speed selector and both sliders retain native keyboard behavior.
  if (e.target !== video.value) return
  if (e.key === ' ' || e.key.toLowerCase() === 'k') void togglePlayback()
  else if (e.key.toLowerCase() === 'm') toggleMute()
  else if (e.key === 'ArrowLeft' && ready.value) seekVideo(position.value - 10000)
  else if (e.key === 'ArrowRight' && ready.value) seekVideo(position.value + 10000)
  else return
  e.preventDefault()
  e.stopPropagation()
}

onMounted(() => {
  canFullscreen.value = !!player.value?.requestFullscreen && document.fullscreenEnabled
  canPip.value = !!video.value?.requestPictureInPicture && document.pictureInPictureEnabled
  document.addEventListener('fullscreenchange', fullscreenChanged)
})
onBeforeUnmount(() => {
  playRequest++
  document.removeEventListener('fullscreenchange', fullscreenChanged)
  if (document.pictureInPictureElement === video.value)
    void document.exitPictureInPicture().catch(() => {
      // The browser may already have closed the picture-in-picture window.
    })
})
</script>

<template>
  <div ref="player" class="video-player" @keydown="onKey">
    <div class="picture">
      <video
        ref="video"
        :key="source"
        :src="source"
        :poster="thumbVersion ? thumbUrl(detail.id, thumbVersion, THUMB_LARGE) : undefined"
        :autoplay="wantsPlay"
        playsinline
        tabindex="0"
        :aria-label="t('viewer.videoPlayer')"
        @loadedmetadata="videoReady"
        @timeupdate="videoTime"
        @play="playbackState"
        @pause="playbackState"
        @playing="buffering = false"
        @canplay="buffering = false"
        @waiting="buffering = true"
        @ended="videoEnded"
        @error="videoError"
        @volumechange="volumeChanged"
        @enterpictureinpicture="pip = true"
        @leavepictureinpicture="pip = false"
        @click="togglePlayback"
        @dblclick.stop="canFullscreen && toggleFullscreen()"
      />
      <UAlert
        v-if="failed"
        class="playback-message"
        role="alert"
        color="error"
        variant="subtle"
        :title="t('viewer.videoFailed')"
      />
    </div>

    <div class="video-controls" role="group" :aria-label="t('viewer.videoControls')" @dblclick.stop>
      <div v-if="total" class="timeline">
        <span class="time">{{ duration(position) }}</span>
        <USlider
          class="seek-control"
          :min="0"
          :max="total"
          :step="1000"
          :model-value="position"
          :disabled="!ready || failed"
          :aria-label="t('viewer.seek')"
          :aria-valuetext="`${duration(position)} / ${duration(total)}`"
          @update:model-value="startSeek"
          @change="commitSeek"
          @pointerup="commitSeek"
          @pointercancel="commitSeek"
        />
        <span class="time">{{ duration(total) }}</span>
      </div>
      <div class="control-row">
        <UButton
          class="play-control"
          :icon="icons[playing ? 'pause' : ended ? 'reset' : 'play']"
          :aria-label="t(playing ? 'viewer.pause' : ended ? 'viewer.replay' : 'viewer.play')"
          :title="t(playing ? 'viewer.pause' : ended ? 'viewer.replay' : 'viewer.play')"
          :disabled="failed"
          variant="soft"
          square
          @click="togglePlayback"
        />
        <UButton
          class="player-control skip-control"
          :icon="icons['rewind']"
          :aria-label="t('viewer.back10')"
          :title="t('viewer.back10')"
          :disabled="!ready || failed || !total"
          variant="ghost"
          square
          @click="seekVideo(position - 10000)"
        />
        <UButton
          class="player-control skip-control"
          :icon="icons['fast-forward']"
          :aria-label="t('viewer.forward10')"
          :title="t('viewer.forward10')"
          :disabled="!ready || failed || !total"
          variant="ghost"
          square
          @click="seekVideo(position + 10000)"
        />
        <div class="volume-controls">
          <UButton
            class="player-control"
            :icon="icons[silent ? 'volume-off' : 'volume']"
            :aria-label="t(silent ? 'viewer.unmute' : 'viewer.mute')"
            :title="t(silent ? 'viewer.unmute' : 'viewer.mute')"
            :aria-pressed="silent"
            variant="ghost"
            square
            @click="toggleMute"
          />
          <USlider
            class="volume-control"
            :min="0"
            :max="100"
            :model-value="silent ? 0 : Math.round(volume * 100)"
            :aria-label="t('viewer.volume')"
            @update:model-value="setVolume"
          />
        </div>
        <span v-if="buffering && !failed" class="buffering" role="status">{{ t('viewer.buffering') }}</span>
        <div class="spacer" />
        <select
          class="speed-control"
          :value="rate"
          :aria-label="t('viewer.speed')"
          :title="t('viewer.speed')"
          @change="setRate"
        >
          <option v-for="speed in rates" :key="speed" :value="speed">{{ speed }}×</option>
        </select>
        <UButton
          v-if="canPip"
          class="player-control"
          :icon="icons.pip"
          :aria-label="t(pip ? 'viewer.exitPip' : 'viewer.pip')"
          :title="t(pip ? 'viewer.exitPip' : 'viewer.pip')"
          :aria-pressed="pip"
          :disabled="!ready || failed"
          variant="ghost"
          square
          @click="togglePip"
        />
        <UButton
          v-if="canFullscreen"
          class="player-control"
          :icon="icons[fullscreen ? 'minimize' : 'maximize']"
          :aria-label="t(fullscreen ? 'viewer.exitFullscreen' : 'viewer.fullscreen')"
          :title="t(fullscreen ? 'viewer.exitFullscreen' : 'viewer.fullscreen')"
          :aria-pressed="fullscreen"
          variant="ghost"
          square
          @click="toggleFullscreen"
        />
      </div>
      <p v-if="notice" class="notice" role="status">{{ notice }}</p>
    </div>
  </div>
</template>

<style scoped>
.video-player {
  grid-area: 1 / 1;
  display: grid;
  grid-template-rows: minmax(0, 1fr) auto;
  width: 100%;
  height: 100%;
  min-height: 0;
  background: var(--lumen-viewer-bg);
  color: var(--lumen-viewer-text);
  color-scheme: dark;
  container-type: inline-size;
}

.picture {
  position: relative;
  min-height: 0;
  display: grid;
  place-items: center;
}

video {
  width: 100%;
  height: 100%;
  min-height: 0;
  object-fit: contain;
}

video:focus-visible {
  outline: 2px solid var(--ui-primary);
  outline-offset: -2px;
}

.video-controls {
  padding: 0.625rem 1rem;
  border-top: 1px solid var(--lumen-viewer-control);
  background: var(--lumen-viewer-bg);
}

.timeline,
.control-row,
.volume-controls {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}

.timeline {
  gap: 0.875rem;
  padding: 0.375rem 0;
  margin-bottom: 0.375rem;
}

.time {
  flex-shrink: 0;
  font-size: 0.75rem;
  font-variant-numeric: tabular-nums;
  color: var(--lumen-viewer-text-muted);
}

.seek-control,
.spacer {
  flex: 1;
  min-width: 0;
}

.player-control,
.play-control {
  flex-shrink: 0;
  width: 2.5rem;
  height: 2.5rem;
  justify-content: center;
  border-radius: 50%;
}

.player-control {
  color: var(--lumen-viewer-text);
}

.player-control:hover,
.player-control[aria-pressed='true'] {
  background: var(--lumen-viewer-control);
}

.volume-control {
  width: 5rem;
}

.speed-control {
  padding: 0.375rem 0.5rem;
  min-height: 2.5rem;
  border: 1px solid var(--lumen-viewer-control);
  border-radius: var(--ui-radius);
  background: var(--lumen-viewer-bg);
  color: var(--lumen-viewer-text);
  font-size: 0.8125rem;
  font-variant-numeric: tabular-nums;
}

.speed-control:focus-visible {
  outline: 2px solid var(--ui-primary);
}

.buffering,
.notice {
  color: var(--lumen-viewer-text-muted);
  font-size: 0.75rem;
}

.playback-message {
  position: absolute;
  max-width: min(28rem, calc(100% - 2rem));
}

@container (max-width: 30rem) {
  .video-controls {
    padding-inline: 0.625rem;
  }

  .volume-control {
    width: 3.25rem;
  }

  .skip-control,
  .buffering {
    display: none;
  }
}

@container (max-width: 20rem) {
  .control-row {
    flex-wrap: wrap;
  }
}

@media (forced-colors: active) {
  .player-control,
  .play-control,
  .video-controls {
    border: 1px solid ButtonText;
  }
}
</style>
