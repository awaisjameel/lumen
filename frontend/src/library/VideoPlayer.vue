<script setup lang="ts">
import IconButton from '../shared/IconButton.vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import USlider from '@nuxt/ui/components/Slider.vue'
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import {
  engine,
  isCancelled,
  Playback,
  PLAYBACK_SPEED_MAX,
  PLAYBACK_SPEED_MIN,
  playUrl,
  subtitleUrl,
  SPRITE_COLS,
  SPRITE_ROWS,
  THUMB_LARGE,
  thumbUrl,
  TrackKind,
  videoUrl,
} from '../bridge'
import type { AssetDetail, MediaInspection, MediaTrack } from '../bridge'
import { icons } from '../design/icons'
import { duration } from '../shared/format'
import { t } from '../shared/i18n'
import { usePreferences } from '../shared/preferences'
import { logError } from '../shared/errors'
import { subtitleCues } from '../media/subtitles'
import { FileTools } from '../media/lazy'
import { videoSpriteStyle } from '../shared/videoSprite'

const props = defineProps<{ detail: AssetDetail; at: number; thumbVersion?: number; private?: boolean }>()
const emit = defineEmits<{ timeupdate: [] }>()
const player = ref<HTMLElement | null>(null)
const video = ref<HTMLVideoElement | null>(null)
const prefs = usePreferences()
const viewing = computed(() => prefs.viewer)
const inspection = ref<MediaInspection | null>(null)
const tracksOf = (kind: TrackKind) => inspection.value?.tracks.filter((track) => track.kind === kind) ?? []
const audioTracks = computed(() => tracksOf(TrackKind.TrackAudio))
const subtitleTracks = computed(() => tracksOf(TrackKind.TrackSubtitle))
const trackLabel = (track: MediaTrack) => `${track.title || track.language || track.codec} (${track.index})`
const audio = ref(-1)
const subtitle = ref(-1)
const looping = ref(viewing.value.loop)
const seekPreview = ref(false)
const frameOpen = ref(false)
const framePosition = ref(0)
function saveFrame() {
  framePosition.value = Math.round(position.value)
  frameOpen.value = true
}
let resumeAllowed = true
let subtitleAbort: AbortController | undefined
let subtitleTrack: TextTrack | undefined
let subtitleElement: HTMLVideoElement | null = null
let disposed = false
const inspectionAbort = new AbortController()
let lastPositionSave = 0
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
const seekStyle = computed(() => {
  const ratio = props.detail.width > 0 && props.detail.height > 0 ? props.detail.width / props.detail.height : 16 / 9
  const width = Math.min(160, 100 * ratio)
  const frame = Math.floor((position.value / Math.max(1, total.value)) * SPRITE_COLS * SPRITE_ROWS)
  return videoSpriteStyle(props.detail.id, props.thumbVersion ?? 0, frame, width, width / ratio)
})
const source = computed(() =>
  converting.value ? playUrl(props.detail.id, start.value, audio.value) : videoUrl(props.detail.id),
)
const playing = ref(false)
const wantsPlay = ref(viewing.value.autoplay)
const ended = ref(false)
const ready = ref(false)
const buffering = ref(true)
const failed = ref(false)
const notice = ref('')
const volume = ref(viewing.value.volume)
const muted = ref(viewing.value.muted)
const rate = ref(viewing.value.speed)
const fullscreen = ref(false)
const pip = ref(false)
const canFullscreen = ref(false)
const canPip = ref(false)
let seeking = false
let playRequest = 0
const silent = computed(() => muted.value || volume.value === 0)
// Every quarter of the allowed range, and the saved speed if it is between two.
const rates = computed(() => {
  const steps = Array.from(
    { length: Math.round(PLAYBACK_SPEED_MAX / PLAYBACK_SPEED_MIN) },
    (_, i) => (i + 1) * PLAYBACK_SPEED_MIN,
  )
  return [...new Set([...steps, rate.value])].sort((a, b) => a - b)
})

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

function videoReady(event: Event) {
  if (event.target !== video.value) return
  ready.value = true
  if (!converting.value && video.value) video.value.currentTime = start.value / 1000
  applyPreferences()
}

function videoTime(event: Event) {
  if (event.target !== video.value) return
  emit('timeupdate')
  if (video.value && !seeking)
    position.value = clamp((converting.value ? start.value : 0) + video.value.currentTime * 1000)
  if (!props.private && viewing.value.resume && Date.now() - lastPositionSave > 10000) {
    lastPositionSave = Date.now()
    void engine.setPlaybackPosition(props.detail.id, position.value).catch(logError)
  }
}

function startSeek(at: number | undefined) {
  resumeAllowed = false
  if (at === undefined || !Number.isFinite(at)) return
  seeking = true
  seekPreview.value = true
  position.value = clamp(at)
}

function seekVideo(at = position.value) {
  resumeAllowed = false
  seeking = false
  seekPreview.value = false
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
  resumeAllowed = false
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

function playbackState(event: Event) {
  if (!video.value || event.target !== video.value) return
  playing.value = !video.value.paused
  if (!video.value.error) wantsPlay.value = playing.value
  if (playing.value) ended.value = false
}

function bufferingState(event: Event) {
  if (event.target === video.value) buffering.value = event.type === 'waiting'
}

function pictureState(event: Event) {
  if (event.target === video.value) pip.value = event.type === 'enterpictureinpicture'
}

function videoEnded(event: Event) {
  if (event.target !== video.value) return
  playing.value = false
  wantsPlay.value = false
  ended.value = true
  buffering.value = false
  if (total.value) position.value = total.value
  if (looping.value) {
    wantsPlay.value = true
    const previousSource = source.value
    seekVideo(0)
    if (source.value === previousSource) void video.value?.play().catch(logError)
  }
}

function videoError(event: Event) {
  if (event.target !== video.value) return
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

function volumeChanged(event: Event) {
  if (!video.value || event.target !== video.value) return
  volume.value = video.value.volume
  muted.value = video.value.muted
}

function setRate(e: Event) {
  if (!(e.target instanceof HTMLSelectElement)) return
  rate.value = Number(e.target.value)
  applyPreferences()
}

function selectAudio(e: Event) {
  if (!(e.target instanceof HTMLSelectElement)) return
  resumeAllowed = false
  audio.value = Number(e.target.value)
  start.value = position.value
  converting.value = true
}

function selectSubtitle(e: Event) {
  if (e.target instanceof HTMLSelectElement) subtitle.value = Number(e.target.value)
}

watch([subtitle, video, source], async () => {
  subtitleAbort?.abort()
  subtitleAbort = new AbortController()
  const el = video.value
  if (subtitleTrack) {
    // Disabled tracks hide their cue lists. Make the list readable without
    // showing captions, then release old cues before reusing the track.
    subtitleTrack.mode = 'hidden'
    while (subtitleTrack.cues?.length) {
      const cue = subtitleTrack.cues[0]
      if (cue) subtitleTrack.removeCue(cue)
    }
    subtitleTrack.mode = 'disabled'
  }
  if (el !== subtitleElement) subtitleTrack = undefined
  subtitleElement = el
  if (subtitle.value < 0 || !el?.addTextTrack) return
  const mine = subtitleAbort
  try {
    const result = await fetch(subtitleUrl(props.detail.id, subtitle.value, converting.value ? start.value : 0), {
      signal: mine.signal,
    })
    if (!result.ok) throw new Error(t('viewer.subtitlesFailed'))
    const data = await result.text()
    if (mine.signal.aborted || el !== video.value) return
    const track = subtitleTrack ?? el.addTextTrack('subtitles')
    for (const cue of subtitleCues(data)) track.addCue(new VTTCue(cue.start, cue.end, cue.text))
    track.mode = 'showing'
    subtitleTrack = track
  } catch (e) {
    if (!mine.signal.aborted && !isCancelled(e)) notice.value = t('viewer.subtitlesFailed')
  }
})

watch(video, (_current, old) => {
  if (old) {
    old.pause()
    old.removeAttribute('src')
    old.load()
  }
})

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
  else if (e.key === 'ArrowLeft' && ready.value) seekVideo(position.value - viewing.value.skipSeconds * 1000)
  else if (e.key === 'ArrowRight' && ready.value) seekVideo(position.value + viewing.value.skipSeconds * 1000)
  else return
  e.preventDefault()
  e.stopPropagation()
}

onMounted(() => {
  void engine
    .inspect(props.detail.id, inspectionAbort.signal)
    .then((info) => {
      if (!disposed) inspection.value = info
    })
    .catch((e: unknown) => {
      if (!isCancelled(e)) logError(e)
    })
  if (!props.private && viewing.value.resume && !props.at) {
    void engine
      .playbackPosition(props.detail.id)
      .then((at) => {
        if (!disposed && viewing.value.resume && resumeAllowed && position.value === 0) {
          start.value = clamp(at)
          position.value = start.value
          if (ready.value && !converting.value && video.value) video.value.currentTime = start.value / 1000
        }
      })
      .catch(logError)
  }
  canFullscreen.value = !!player.value?.requestFullscreen && document.fullscreenEnabled
  canPip.value = !!video.value?.requestPictureInPicture && document.pictureInPictureEnabled
  document.addEventListener('fullscreenchange', fullscreenChanged)
})
onBeforeUnmount(() => {
  disposed = true
  inspectionAbort.abort()
  subtitleAbort?.abort()
  if (!props.private && viewing.value.resume)
    void engine.setPlaybackPosition(props.detail.id, position.value).catch(logError)
  if (video.value) {
    video.value.pause()
    video.value.removeAttribute('src')
    video.value.load()
  }
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
        @playing="bufferingState"
        @canplay="bufferingState"
        @waiting="bufferingState"
        @ended="videoEnded"
        @error="videoError"
        @volumechange="volumeChanged"
        @enterpictureinpicture="pictureState"
        @leavepictureinpicture="pictureState"
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
        <span v-if="seekPreview && thumbVersion" class="seek-preview" :style="seekStyle" aria-hidden="true" />
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
        <IconButton
          class="play-control"
          :icon="icons[playing ? 'pause' : ended ? 'reset' : 'play']"
          :label="t(playing ? 'viewer.pause' : ended ? 'viewer.replay' : 'viewer.play')"
          :disabled="failed"
          variant="soft"
          @click="togglePlayback"
        />
        <IconButton
          class="player-control skip-control"
          :icon="icons['rewind']"
          :label="t('viewer.skipBack', { n: viewing.skipSeconds })"
          :disabled="!ready || failed || !total"
          variant="ghost"
          @click="seekVideo(position - viewing.skipSeconds * 1000)"
        />
        <IconButton
          class="player-control skip-control"
          :icon="icons['fast-forward']"
          :label="t('viewer.skipForward', { n: viewing.skipSeconds })"
          :disabled="!ready || failed || !total"
          variant="ghost"
          @click="seekVideo(position + viewing.skipSeconds * 1000)"
        />
        <div class="volume-controls">
          <IconButton
            class="player-control"
            :icon="icons[silent ? 'volume-off' : 'volume']"
            :label="t(silent ? 'viewer.unmute' : 'viewer.mute')"
            :aria-pressed="silent"
            variant="ghost"
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
        <IconButton
          v-if="!props.private"
          class="player-control"
          :icon="icons.image"
          :label="t('viewer.saveFrame')"
          :disabled="!ready || failed"
          variant="ghost"
          @click="saveFrame"
        />
        <UButton :aria-label="t('viewer.loop')" :aria-pressed="looping" variant="ghost" @click="looping = !looping">{{
          t('viewer.loop')
        }}</UButton>
        <select
          v-if="audioTracks.length"
          class="player-select"
          :value="audio"
          :aria-label="t('viewer.audioTrack')"
          :title="t('viewer.audioTrack')"
          @change="selectAudio"
        >
          <option :value="-1">{{ t('viewer.defaultTrack') }}</option>
          <option v-for="track in audioTracks" :key="track.index" :value="track.index">
            {{ trackLabel(track) }}
          </option>
        </select>
        <select
          v-if="subtitleTracks.length"
          class="player-select"
          :value="subtitle"
          :aria-label="t('viewer.subtitleTrack')"
          :title="t('viewer.subtitleTrack')"
          @change="selectSubtitle"
        >
          <option :value="-1">{{ t('viewer.subtitlesOff') }}</option>
          <option v-for="track in subtitleTracks" :key="track.index" :value="track.index" :disabled="!track.supported">
            {{ trackLabel(track) }}{{ track.supported ? '' : t('viewer.bitmapSubtitles') }}
          </option>
        </select>
        <select
          class="player-select"
          :value="rate"
          :aria-label="t('viewer.speed')"
          :title="t('viewer.speed')"
          @change="setRate"
        >
          <option v-for="speed in rates" :key="speed" :value="speed">{{ speed }}×</option>
        </select>
        <IconButton
          v-if="canPip"
          class="player-control"
          :icon="icons.pip"
          :label="t(pip ? 'viewer.exitPip' : 'viewer.pip')"
          :aria-pressed="pip"
          :disabled="!ready || failed"
          variant="ghost"
          @click="togglePip"
        />
        <IconButton
          v-if="canFullscreen"
          class="player-control"
          :icon="icons[fullscreen ? 'minimize' : 'maximize']"
          :label="t(fullscreen ? 'viewer.exitFullscreen' : 'viewer.fullscreen')"
          :aria-pressed="fullscreen"
          variant="ghost"
          @click="toggleFullscreen"
        />
      </div>
      <p v-if="notice" class="notice" role="status">{{ notice }}</p>
    </div>
    <FileTools v-model:open="frameOpen" :ids="[detail.id]" :frame="framePosition" />
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
.timeline {
  position: relative;
}
.seek-preview {
  position: absolute;
  bottom: 100%;
  left: 50%;
  transform: translateX(-50%);
  background-repeat: no-repeat;
  pointer-events: none;
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

.player-select {
  max-width: 10rem;
  padding: 0.375rem 0.5rem;
  min-height: 2.5rem;
  border: 1px solid var(--lumen-viewer-control);
  border-radius: var(--ui-radius);
  background: var(--lumen-viewer-bg);
  color: var(--lumen-viewer-text);
  font-size: 0.8125rem;
  font-variant-numeric: tabular-nums;
}

.player-select:focus-visible {
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
