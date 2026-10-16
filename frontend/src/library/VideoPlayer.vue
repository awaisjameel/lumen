<script setup lang="ts">
import IconButton from '../shared/IconButton.vue'
import UPopover from '@nuxt/ui/components/Popover.vue'
import FeatureState from '../shared/FeatureState.vue'
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
import { logError, reportError } from '../shared/errors'
import { preferredSubtitle, subtitleCues } from '../media/subtitles'
import { FileTools } from '../media/lazy'
import { viewerDefaults } from '../shared/viewing'
import { videoSpriteStyle } from '../shared/videoSprite'

const props = defineProps<{
  detail: AssetDetail
  at: number
  thumbVersion?: number
  private?: boolean
  chromeHidden?: boolean
}>()
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
const looping = ref(viewing.value.videoLoop)
const seekPreview = ref(false)
const hoverTime = ref<number>()
const settingsOpen = ref(false)
const subtitleRetry = ref(0)
const subtitleFailed = ref(false)
const retry = ref(0)
const resumeAt = ref<number>()
const buffered = ref<{ start: number; end: number }[]>([])
const previewTime = computed(() => hoverTime.value ?? position.value)
function seekHover(e: PointerEvent) {
  if (!(e.currentTarget instanceof HTMLElement) || !total.value || !ready.value || failed.value) return
  const bounds = e.currentTarget.getBoundingClientRect()
  if (bounds.width) hoverTime.value = clamp(((e.clientX - bounds.left) / bounds.width) * total.value)
}
function updateBuffered(event: Event) {
  const el = video.value
  if (!el || event.target !== el || !total.value) return
  const ranges = []
  for (let i = 0; i < el.buffered.length; i++) {
    const offset = converting.value ? start.value : 0
    ranges.push({
      start: clamp(offset + el.buffered.start(i) * 1000) / total.value,
      end: clamp(offset + el.buffered.end(i) * 1000) / total.value,
    })
  }
  buffered.value = ranges
}
function retryPlayback() {
  failed.value = false
  notice.value = ''
  wantsPlay.value = true
  retry.value++
}
function resumePlayback() {
  seekVideo(resumeAt.value)
  resumeAt.value = undefined
}
function resetSettings() {
  const defaults = viewerDefaults()
  rate.value = defaults.speed
  audio.value = -1
  subtitle.value = -1
  void prefs.setViewer({ skipSeconds: defaults.skipSeconds })
  applyPreferences()
}
function setSkip(e: Event) {
  if (e.target instanceof HTMLSelectElement) void prefs.setViewer({ skipSeconds: Number(e.target.value) })
}
const frameOpen = ref(false)
const framePosition = ref(0)
function saveFrame() {
  wantsPlay.value = false
  video.value?.pause()
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
  const frame = Math.floor((previewTime.value / Math.max(1, total.value)) * SPRITE_COLS * SPRITE_ROWS)
  return {
    ...videoSpriteStyle(props.detail.id, props.thumbVersion ?? 0, frame, width, width / ratio),
    left: `clamp(${width / 2}px, ${(previewTime.value / Math.max(1, total.value)) * 100}%, calc(100% - ${width / 2}px))`,
  }
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
  buffered.value = []
  hoverTime.value = undefined
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
  hoverTime.value = undefined
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

watch([subtitle, video, source, subtitleRetry], async () => {
  subtitleFailed.value = false
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
    if (notice.value === t('viewer.subtitlesFailed')) notice.value = ''
  } catch (e) {
    if (!mine.signal.aborted && !isCancelled(e)) {
      subtitleFailed.value = true
      notice.value = t('viewer.subtitlesFailed')
    }
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

function onKey(e: KeyboardEvent, fromViewer = false) {
  if (e.defaultPrevented || e.ctrlKey || e.altKey || e.metaKey || e.shiftKey) return
  // Buttons, the speed selector and both sliders retain native keyboard behavior.
  if (!fromViewer && e.target !== video.value) return
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
      if (disposed) return
      inspection.value = info
      // Subtitles from the start (the playback preference), unless the user
      // has already chosen a track for this video.
      if (viewing.value.subtitles && subtitle.value < 0)
        subtitle.value = preferredSubtitle(subtitleTracks.value, viewing.value.subtitleLanguage)
    })
    .catch((e: unknown) => {
      if (!isCancelled(e)) logError(e)
    })
  if (!props.private && viewing.value.resume && !props.at) {
    void engine
      .playbackPosition(props.detail.id)
      .then((at) => {
        if (!disposed && viewing.value.resume && resumeAllowed && position.value === 0) {
          resumeAt.value = at > 0 ? clamp(at) : undefined
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
defineExpose({
  handleKey: (event: KeyboardEvent) => {
    onKey(event, true)
    return event.defaultPrevented
  },
})
</script>

<template>
  <div ref="player" class="video-player" :class="{ 'controls-hidden': chromeHidden && !settingsOpen }" @keydown="onKey">
    <div class="picture">
      <video
        ref="video"
        :key="`${source}:${retry}`"
        :src="source"
        :poster="thumbVersion ? thumbUrl(detail.id, thumbVersion, THUMB_LARGE) : undefined"
        :autoplay="wantsPlay"
        playsinline
        tabindex="0"
        :aria-label="t('viewer.videoPlayer')"
        @loadedmetadata="videoReady"
        @progress="updateBuffered"
        @loadeddata="updateBuffered"
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
      <FeatureState
        v-if="failed"
        class="playback-message"
        kind="error"
        :title="t('viewer.videoFailed')"
        :text="t('video.failureHint')"
      >
        <UButton @click="retryPlayback">{{ t('common.retry') }}</UButton>
        <UButton variant="ghost" @click="engine.openMedia(detail.id).catch(reportError)">{{ t('files.open') }}</UButton>
      </FeatureState>
      <div v-else-if="!playing && !wantsPlay && !ended && !notice" class="central-play">
        <IconButton
          :icon="icons.play"
          :label="t('video.playVideo')"
          :kbds="['Space', 'K']"
          color="primary"
          variant="solid"
          @click="togglePlayback"
        />
        <span>{{ t('video.autoplayOff') }}</span>
      </div>
      <div v-else-if="buffering && wantsPlay" class="buffer-card" role="status">
        {{ t(converting && audio >= 0 ? 'video.converting' : 'viewer.buffering') }}
      </div>
      <div v-else-if="ended" class="ended-message" role="status">{{ t('video.finished') }}</div>
    </div>

    <div class="video-controls" role="group" :aria-label="t('viewer.videoControls')" @dblclick.stop>
      <slot name="filmstrip" />
      <div v-if="resumeAt !== undefined" class="resume-chip">
        <UButton variant="ghost" @click="resumePlayback">{{
          t('video.resumePosition', { time: duration(resumeAt) })
        }}</UButton
        ><IconButton
          :icon="icons.close"
          :label="t('video.dismissResume')"
          variant="ghost"
          @click="resumeAt = undefined"
        />
      </div>
      <div
        v-if="total"
        class="timeline"
        data-video-timeline
        @pointermove="seekHover"
        @pointerleave="hoverTime = undefined"
      >
        <span
          v-if="(seekPreview || hoverTime !== undefined) && thumbVersion"
          class="seek-preview"
          :style="seekStyle"
          aria-hidden="true"
          ><span>{{ duration(previewTime) }}</span></span
        >
        <div class="buffered-track" aria-hidden="true">
          <span
            v-for="range in buffered"
            :key="range.start"
            data-buffered-range
            :style="{ left: `${range.start * 100}%`, transform: `scaleX(${range.end - range.start})` }"
          />
        </div>
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
      </div>
      <div class="control-row">
        <IconButton
          :kbds="['Space', 'K']"
          class="play-control"
          :icon="icons[playing ? 'pause' : ended ? 'reset' : 'play']"
          :label="t(playing ? 'viewer.pause' : ended ? 'viewer.replay' : 'viewer.play')"
          :disabled="failed"
          color="primary"
          variant="solid"
          @click="togglePlayback"
        />
        <span class="time"
          >{{ duration(position) }} <span>/ {{ duration(total) }}</span></span
        >
        <IconButton
          :kbds="['←']"
          class="player-control skip-control"
          :icon="icons['skip-back']"
          :label="t('viewer.skipBack', { n: viewing.skipSeconds })"
          :disabled="!ready || failed || !total"
          variant="ghost"
          @click="seekVideo(position - viewing.skipSeconds * 1000)"
        />
        <IconButton
          :kbds="['→']"
          class="player-control skip-control"
          :icon="icons['skip-forward']"
          :label="t('viewer.skipForward', { n: viewing.skipSeconds })"
          :disabled="!ready || failed || !total"
          variant="ghost"
          @click="seekVideo(position + viewing.skipSeconds * 1000)"
        />
        <div class="spacer" />
        <div class="volume-controls">
          <IconButton
            :kbds="['M']"
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
        <IconButton
          v-if="!props.private"
          class="player-control"
          :icon="icons.camera"
          :label="t('viewer.saveFrame')"
          :disabled="!ready || failed"
          variant="ghost"
          @click="saveFrame"
        />
        <IconButton
          :label="t('viewer.loop')"
          :aria-pressed="looping"
          class="player-control"
          :icon="icons.reset"
          variant="ghost"
          @click="looping = !looping"
        />
        <IconButton
          class="player-control"
          :icon="icons.subtitles"
          :label="t('viewer.subtitleTrack')"
          :aria-pressed="subtitle >= 0"
          :aria-expanded="settingsOpen"
          variant="ghost"
          @click="settingsOpen = !settingsOpen"
        />
        <UPopover v-model:open="settingsOpen" :portal="false" :content="{ side: 'top', align: 'end' }">
          <IconButton class="player-control" :icon="icons.settings" :label="t('video.settings')" variant="ghost" />
          <template #content
            ><section class="playback-settings" data-viewer-settings>
              <header>
                <h2>{{ t('video.settings') }}</h2>
                <IconButton
                  :icon="icons.close"
                  :label="t('video.closeSettings')"
                  variant="ghost"
                  @click="settingsOpen = false"
                />
              </header>
              <label :for="`video-audio-${detail.id}`">{{ t('viewer.audioTrack') }}</label>
              <select
                :id="`video-audio-${detail.id}`"
                :disabled="!audioTracks.length"
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
              <label :for="`video-subtitle-${detail.id}`">{{ t('viewer.subtitleTrack') }}</label>
              <select
                :id="`video-subtitle-${detail.id}`"
                :disabled="!subtitleTracks.length"
                class="player-select"
                :value="subtitle"
                :aria-label="t('viewer.subtitleTrack')"
                :title="t('viewer.subtitleTrack')"
                @change="selectSubtitle"
              >
                <option :value="-1">{{ t('viewer.subtitlesOff') }}</option>
                <option
                  v-for="track in subtitleTracks"
                  :key="track.index"
                  :value="track.index"
                  :disabled="!track.supported"
                >
                  {{ trackLabel(track) }}{{ track.supported ? '' : t('viewer.bitmapSubtitles') }}
                </option>
              </select>
              <label :for="`video-rate-${detail.id}`">{{ t('viewer.speed') }}</label>
              <select
                :id="`video-rate-${detail.id}`"
                class="player-select"
                :value="rate"
                :aria-label="t('viewer.speed')"
                :title="t('viewer.speed')"
                @change="setRate"
              >
                <option v-for="speed in rates" :key="speed" :value="speed">{{ speed }}×</option>
              </select>

              <p>{{ t(subtitleTracks.length ? 'video.subtitleHint' : 'video.noSubtitles') }}</p>
              <p>{{ t(audioTracks.length ? 'video.audioHint' : 'video.noAudio') }}</p>
              <label
                >{{ t('viewer.skipInterval')
                }}<select class="player-select" :value="viewing.skipSeconds" @change="setSkip">
                  <option v-for="seconds in [5, 10, 30]" :key="seconds" :value="seconds">{{ seconds }}</option>
                </select></label
              >
              <UButton variant="ghost" @click="resetSettings">{{ t('video.resetSettings') }}</UButton>
            </section></template
          >
        </UPopover>
        <IconButton
          class="player-control"
          :icon="icons.pip"
          :label="t(pip ? 'viewer.exitPip' : 'viewer.pip')"
          :aria-pressed="pip"
          :disabled="!canPip || !ready || failed"
          :tip="canPip ? undefined : t('video.pipUnavailable')"
          variant="ghost"
          @click="togglePip"
        />
        <IconButton
          class="player-control"
          :icon="icons[fullscreen ? 'minimize' : 'maximize']"
          :label="t(fullscreen ? 'viewer.exitFullscreen' : 'viewer.fullscreen')"
          :disabled="!canFullscreen"
          :tip="canFullscreen ? undefined : t('video.fullscreenUnavailable')"
          :aria-pressed="fullscreen"
          variant="ghost"
          @click="toggleFullscreen"
        />
      </div>
      <p v-if="notice" class="notice" role="status">
        {{ notice
        }}<UButton v-if="subtitleFailed" variant="ghost" @click="subtitleRetry++">{{ t('common.retry') }}</UButton>
      </p>
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
.picture {
  position: relative;
  min-height: 0;
  display: grid;
  place-items: center;
  padding: 1rem 2.5rem;
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
  position: relative;
  z-index: 3;
  padding: 0.5rem 1rem 0.75rem;
  background: linear-gradient(transparent, var(--lumen-viewer-bg));
  transition:
    opacity var(--lumen-d-base) var(--lumen-ease-out),
    transform var(--lumen-d-base) var(--lumen-ease-out);
}
.controls-hidden .video-controls {
  opacity: 0;
  transform: translateY(0.5rem);
  pointer-events: none;
}
.controls-hidden .video-controls:focus-within {
  opacity: 1;
  transform: none;
  pointer-events: auto;
}
.timeline,
.control-row,
.volume-controls,
.settings-triggers,
.resume-chip {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}
.timeline {
  position: relative;
  height: 2rem;
  margin-bottom: 0.25rem;
}
.time {
  flex-shrink: 0;
  font-size: 0.8125rem;
  font-variant-numeric: tabular-nums;
  color: var(--lumen-viewer-text);
}
.time span {
  color: var(--lumen-viewer-text-muted);
}
.seek-control,
.spacer {
  flex: 1;
  min-width: 0;
}
.seek-control {
  z-index: 1;
}
.seek-control :deep([data-slot='track']) {
  background: var(--lumen-viewer-control);
}
.buffered-track {
  position: absolute;
  inset: calc(50% - 0.125rem) 0 auto;
  height: 0.25rem;
  overflow: hidden;
  border-radius: var(--lumen-r-sm);
  pointer-events: none;
}
.buffered-track span {
  position: absolute;
  inset-block: 0;
  width: 100%;
  background: var(--lumen-viewer-text-muted);
  opacity: 0.5;
  transform-origin: left;
}
.seek-preview {
  position: absolute;
  bottom: 100%;
  transform: translateX(-50%);
  background-repeat: no-repeat;
  background-color: var(--lumen-viewer-glass);
  border-radius: var(--lumen-r-md);
  box-shadow: var(--lumen-shadow-2);
  pointer-events: none;
  z-index: 4;
}
.seek-preview span {
  position: absolute;
  bottom: 0.25rem;
  left: 50%;
  transform: translateX(-50%);
  padding: 0.25rem 0.5rem;
  background: var(--lumen-viewer-glass);
  border-radius: var(--lumen-r-sm);
  font-size: 0.75rem;
  font-variant-numeric: tabular-nums;
}
.player-control,
.play-control {
  flex-shrink: 0;
  width: 2.5rem;
  height: 2.5rem;
  justify-content: center;
  border-radius: var(--lumen-r-md);
  color: var(--lumen-viewer-text);
}
.play-control {
  background: var(--ui-primary);
  color: var(--lumen-on-primary);
}
.player-control:hover,
.player-control[aria-pressed='true'] {
  background: var(--lumen-viewer-control);
}
.volume-control {
  width: 4rem;
}
.player-select {
  width: 100%;
  min-width: 0;
  padding: 0.5rem;
  min-height: 2.5rem;
  border: 1px solid var(--lumen-viewer-control);
  border-radius: var(--lumen-r-sm);
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
.notice {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}
.playback-message {
  position: absolute;
  max-width: min(28rem, calc(100% - 2rem));
  background: var(--lumen-viewer-bg);
  border-radius: var(--lumen-r-lg);
}
.central-play {
  position: absolute;
  display: grid;
  justify-items: center;
  gap: 0.75rem;
  font-size: 0.8125rem;
}
.central-play :deep(button) {
  width: 4rem;
  height: 4rem;
  border-radius: 50%;
}
.buffer-card,
.ended-message {
  position: absolute;
  padding: 1rem;
  background: var(--lumen-viewer-glass);
  border-radius: var(--lumen-r-lg);
}
.ended-message {
  bottom: 1rem;
}
.playback-settings {
  display: grid;
  gap: 0.75rem;
  width: 22rem;
  max-width: calc(100vw - 2rem);
  max-height: calc(100vh - 10rem);
  overflow: auto;
  padding: 1rem;
  background: var(--lumen-viewer-bg);
  color: var(--lumen-viewer-text);
  border: 1px solid var(--lumen-viewer-control);
  border-radius: var(--lumen-r-lg);
}
.playback-settings header {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.playback-settings h2 {
  font-size: 1rem;
  font-weight: 600;
}
.playback-settings p {
  font-size: 0.75rem;
  color: var(--lumen-viewer-text-muted);
  margin: 0;
}
.playback-settings label {
  display: grid;
  gap: 0.5rem;
  font-size: 0.8125rem;
}
.resume-chip {
  font-size: 0.8125rem;
}
@container (max-width: 45rem) {
  .control-row {
    gap: 0.25rem;
  }
  .player-control {
    width: 2rem;
    height: 2rem;
  }
  .volume-control {
    width: 3rem;
  }
  .buffering {
    display: none;
  }
}
@container (max-width: 30rem) {
  .control-row {
    flex-wrap: wrap;
  }
  .picture {
    padding-inline: 1rem;
  }
  .video-controls {
    padding-inline: 0.5rem;
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
