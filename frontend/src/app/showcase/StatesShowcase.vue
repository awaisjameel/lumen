<script setup lang="ts">
// Interactive reference data never changes the user's library or files.
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import USelect from '@nuxt/ui/components/Select.vue'
import UModal from '@nuxt/ui/components/Modal.vue'
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { Device, FailReason, WaitReason } from '../../bridge'
import type { FailureGroup, Progress } from '../../bridge'
import { icons } from '../../design/icons'
import IndexStatus from '../../library/IndexStatus.vue'
import FeatureState from '../../shared/FeatureState.vue'
import WorkProgress from '../../shared/WorkProgress.vue'
import { confirm } from '../../shared/confirm'
import { nameLength, trimName } from '../../shared/names'
import { notify } from '../../shared/notify'

const mode = ref('working')
const paused = ref(false)
const modes = [
  { value: 'working', label: 'Working' },
  { value: 'scanning', label: 'Looking in a folder' },
  { value: 'full_screen', label: 'Full-screen app' },
  { value: 'busy', label: 'PC is busy' },
  { value: 'idle', label: 'Away from the PC' },
  { value: 'battery', label: 'On battery' },
  { value: 'off', label: 'AI unavailable' },
  { value: 'done', label: 'Up to date' },
  { value: 'skipped', label: 'Unsupported files' },
  { value: 'large', label: 'Large library' },
  { value: 'failure-loading', label: 'Loading failure details' },
  { value: 'failure-error', label: 'Failure details unavailable' },
]
const progress = computed<Progress>(() => ({
  scanning: mode.value === 'scanning',
  scanPath: String.raw`D:\Photos\Family memories\October 2026\京都 · یادیں`,
  scanSeen: 1240,
  total: mode.value === 'large' ? 124863 : 12792,
  metadata: 0,
  thumbnail: 0,
  embedding: mode.value === 'done' ? 0 : mode.value === 'large' ? 98204 : 1204,
  faces: mode.value === 'done' ? 0 : 842,
  videos: mode.value === 'done' ? 0 : 68,
  ocr: mode.value === 'done' ? 0 : 216,
  working: {
    metadata: false,
    thumbnail: false,
    embedding: mode.value === 'working' && !paused.value,
    faces: false,
    videos: false,
    ocr: false,
  },
  errors: 28,
  skipped: mode.value === 'skipped' ? 46 : 0,
  paused: paused.value,
  onBattery: mode.value === 'battery',
  waiting:
    mode.value === 'full_screen'
      ? WaitReason.WaitFullScreen
      : mode.value === 'busy'
        ? WaitReason.WaitBusy
        : mode.value === 'idle'
          ? WaitReason.WaitIdle
          : WaitReason.WaitNone,
  ai: {
    device: Device.DeviceCPU,
    error: mode.value === 'off' ? 'The bundled model could not be loaded.' : '',
    facesError: mode.value === 'off' ? 'The face model could not be loaded.' : '',
  },
}))
const reasons: FailureGroup[] = [
  {
    reason: FailReason.FailDamaged,
    count: 12,
    files: [{ path: String.raw`D:\Photos\2026\10\IMG_2481.JPG`, error: 'The file is damaged.' }],
  },
  {
    reason: FailReason.FailSlow,
    count: 8,
    files: [{ path: String.raw`D:\Photos\2026\09\VID_0760.MOV`, error: 'Took too long to read.' }],
  },
  {
    reason: FailReason.FailLocked,
    count: 6,
    files: [{ path: String.raw`D:\Photos\Family\DSC_0740.ARW`, error: 'Another app is using the file.' }],
  },
  {
    reason: FailReason.FailOther,
    count: 2,
    files: [{ path: String.raw`D:\Photos\Archive\IMG_0912.HEIC`, error: 'Could not read the file.' }],
  },
]
const failureRetried = ref(false)
watch(mode, () => {
  failureRetried.value = false
  paused.value = false
})
async function readFailures(): Promise<FailureGroup[]> {
  if (mode.value === 'failure-error' && !failureRetried.value) {
    failureRetried.value = true
    throw new Error('Failure details unavailable')
  }
  if (mode.value === 'failure-loading') await new Promise((resolve) => setTimeout(resolve, 800))
  return reasons
}
const empty = ref('scanning')
const empties = [
  {
    value: 'scanning',
    label: 'Scanning',
    title: 'Looking for photos…',
    hint: 'Your photos and videos appear here as Lumen finds them.',
    icon: 'aperture' as const,
    action: 'View activity',
  },
  {
    value: 'library',
    label: 'No photos',
    title: 'No photos here yet',
    hint: 'Add a folder in Settings, or check that your folders contain photos.',
    icon: 'folder' as const,
    action: 'Add a folder…',
  },
  {
    value: 'favourites',
    label: 'Favourites',
    title: 'No favourites yet',
    hint: 'Press F or click the heart on any photo to add it here.',
    icon: 'heart' as const,
    action: 'Browse your photos',
  },
  {
    value: 'videos-off',
    label: 'Videos off',
    title: 'No videos',
    hint: 'Turn on “Include videos” in Settings to add them to your library.',
    icon: 'video' as const,
    action: 'Review Include videos',
  },
  {
    value: 'videos-empty',
    label: 'No videos found',
    title: 'No videos found yet',
    hint: 'Include videos is on. Add a folder containing videos in Settings.',
    icon: 'video' as const,
    action: 'Add a folder…',
  },
  {
    value: 'results',
    label: 'No results',
    title: 'No matching photos',
    hint: 'Try a different filter or clear the filters to see your library.',
    icon: 'search' as const,
    action: 'Clear filters',
  },
  {
    value: 'cleared',
    label: 'Cleared',
    title: 'Your library is clear',
    hint: 'Your files are still in their folders. Add a folder whenever you’re ready.',
    icon: 'folder' as const,
    action: 'Add a folder…',
  },
]
const state = computed(() => empties.find((e) => e.value === empty.value) ?? empties[0])
const loaded = ref(false)
const connected = ref(false)
const album = ref('Summer 2026')
const validated = ref(false)
const nameError = computed(() =>
  !album.value.trim()
    ? 'Enter an album name.'
    : nameLength(album.value) > 80
      ? 'A name must be 1 to 80 characters.'
      : trimName(album.value) === 'Summer 2026'
        ? 'An album already has that name.'
        : '',
)
function rename() {
  validated.value = true
  if (!nameError.value)
    notify({ id: 'showcase.rename', kind: 'success', title: `Album renamed to “${album.value.trim()}”.` })
}
const update = ref('available')
function showUpdate() {
  notify({
    id: 'showcase.update',
    kind: update.value === 'complete' ? 'success' : 'info',
    icon: 'update',
    title:
      update.value === 'complete'
        ? 'Lumen 0.13.0 is running with this library.'
        : update.value === 'ready'
          ? 'Update ready — restart Lumen to finish.'
          : 'Lumen 0.13.0 is available.',
    actions: [
      { label: 'Application updates', run: () => undefined },
      { label: 'Later', run: () => undefined },
    ],
  })
}
let timer: ReturnType<typeof setInterval> | undefined
const moved = ref(320)
function showMoving() {
  if (timer) clearInterval(timer)
  moved.value = 320
  const draw = () =>
    notify({
      id: 'showcase.moving',
      kind: 'info',
      duration: 0,
      title: `Moving files: ${moved.value.toLocaleString('en-GB')} of 1,284`,
      work: { done: moved.value, total: 1284 },
      actions: [{ label: 'View run', dismiss: false, run: () => undefined }],
    })
  draw()
  timer = setInterval(() => {
    moved.value = Math.min(1284, moved.value + 107)
    if (moved.value === 1284) {
      clearInterval(timer)
      notify({ id: 'showcase.moving', kind: 'success', title: 'Moved 1,284 files. Your folder organisation is ready.' })
    } else draw()
  }, 750)
}
function failMoving() {
  clearInterval(timer)
  notify({
    id: 'showcase.moving',
    kind: 'error',
    title: `Moved ${moved.value.toLocaleString('en-GB')} of 1,284 files. The drive disconnected. Reconnect it and review the run before retrying.`,
    work: { done: moved.value, total: 1284 },
    actions: [{ label: 'View run', run: () => undefined }],
  })
}
onBeforeUnmount(() => clearInterval(timer))
const errorKind = ref('recycle')
function showError() {
  notify({
    id: 'showcase.failure',
    kind: 'error',
    title:
      errorKind.value === 'unexpected'
        ? 'Something went wrong. Lumen saved the details in its log. Try again, or save a problem report from Settings.'
        : errorKind.value === 'not-found'
          ? 'That isn’t there any more: it may have been deleted, or its file moved away. Reconnect the drive or check the folder.'
          : 'Lumen couldn’t use the Recycle Bin, so it left the remaining files where they are.',
  })
}
const report = ref('idle')
const reportResult = ref('saved')
const reportLabel = computed(() =>
  report.value === 'saving'
    ? 'Saving report…'
    : report.value === 'failed'
      ? 'Try saving again…'
      : 'Save a problem report…',
)
let reportTimer: ReturnType<typeof setTimeout> | undefined
let crashOpen = false
function openCrash() {
  report.value = 'idle'
  showCrash()
}
function showCrash() {
  crashOpen = true
  notify({
    id: 'showcase.crash',
    kind: report.value === 'failed' ? 'error' : 'info',
    icon: 'bug',
    duration: 0,
    title: 'Lumen closed unexpectedly last time.',
    description:
      report.value === 'saved'
        ? 'Saved to D:/Reports/Lumen-problem-report.zip'
        : report.value === 'failed'
          ? 'Couldn’t save the report. Choose another folder and try again.'
          : 'A crash report was saved on this PC. You can save a problem report to share it.',
    actions:
      report.value === 'saved'
        ? []
        : [{ label: reportLabel.value, disabled: report.value === 'saving', dismiss: false, run: saveReport }],
    closed: () => {
      crashOpen = false
    },
  })
}
function saveReport() {
  if (report.value === 'saving') return
  report.value = 'saving'
  if (crashOpen) showCrash()
  reportTimer = setTimeout(() => {
    report.value = reportResult.value === 'cancelled' ? 'idle' : reportResult.value
    if (crashOpen) showCrash()
  }, 800)
}
onBeforeUnmount(() => clearTimeout(reportTimer))
const startup = ref(false)
function removeFolder() {
  void confirm(
    'Remove “Family archive” from Lumen?',
    'The folder is removed from this library. Original files stay in their folders.',
    'Remove folder',
    { danger: true },
  )
}
function undoToast() {
  notify({
    id: 'showcase.undo',
    kind: 'success',
    title: 'Moved 12 items to the Trash.',
    actions: [
      {
        label: 'Undo',
        run: () => notify({ id: 'showcase.undo', kind: 'success', title: 'Restored 12 items to your library.' }),
      },
    ],
  })
}
</script>
<template>
  <section class="state-reference" data-states-showcase>
    <h2>App states</h2>
    <div class="columns">
      <article>
        <h3>Background activity</h3>
        <USelect v-model="mode" :items="modes" aria-label="Activity state" />
        <div class="activity-examples">
          <IndexStatus
            :key="`wide-${mode}`"
            :progress="progress"
            :read-failures="readFailures"
            :pause="(value) => (paused = value)"
          /><IndexStatus
            :key="`compact-${mode}`"
            compact
            :progress="progress"
            :read-failures="readFailures"
            :pause="(value) => (paused = value)"
          />
        </div>
        <p>Open the card for now, next, wait conditions and the AI explanation.</p>
      </article>
      <article>
        <h3>Notifications</h3>
        <USelect
          v-model="errorKind"
          :items="[
            { value: 'recycle', label: 'Recycle Bin unavailable' },
            { value: 'unexpected', label: 'Unexpected error' },
            { value: 'not-found', label: 'Missing file' },
          ]"
          aria-label="Error kind"
        />
        <div class="row">
          <UButton @click="undoToast">Toast with Undo</UButton><UButton @click="showError">Persistent error</UButton
          ><UButton @click="showMoving">Moving files</UButton><UButton @click="failMoving">Disconnect drive</UButton>
        </div>
        <USelect v-model="update" :items="['available', 'ready', 'complete']" aria-label="Update state" /><UButton
          :icon="icons.update"
          @click="showUpdate"
          >Show update notice</UButton
        >
        <USelect
          v-model="reportResult"
          :items="[
            { value: 'saved', label: 'Save report' },
            { value: 'failed', label: 'Save error' },
            { value: 'cancelled', label: 'Save cancellation' },
          ]"
          aria-label="Report save result"
        />
        <UButton :icon="icons.bug" @click="openCrash">Show crash notice</UButton>
      </article>
      <article>
        <h3>Empty examples</h3>
        <USelect v-model="empty" :items="empties" aria-label="Empty state" /><FeatureState
          v-if="state"
          kind="empty"
          :icon="state.icon"
          :title="state.title"
          :text="state.hint"
          ><WorkProgress v-if="empty === 'scanning'" label="Looking for photos" indeterminate /><UButton
            @click="empty = 'library'"
            >{{ state.action }}</UButton
          ></FeatureState
        >
      </article>
      <article>
        <h3>Loading content</h3>
        <UButton :aria-pressed="loaded" @click="loaded = !loaded">{{
          loaded ? 'Replay loading' : 'Reveal loaded content'
        }}</UButton>
        <div class="loading-grid" :aria-busy="!loaded" aria-label="Photo grid">
          <div v-for="i in 6" :key="i" :class="{ 'lumen-skeleton': !loaded }">
            <span v-if="loaded">{{ i }}</span>
          </div>
        </div>
        <div v-for="name in ['Summer 2026', 'Skardu', 'Family']" :key="name" class="loading-row" :aria-busy="!loaded">
          <span class="cover" :class="{ 'lumen-skeleton': !loaded }" /><span v-if="loaded">{{ name }} · 248 photos</span
          ><span v-else class="lumen-skeleton text-skeleton" />
        </div>
        <dl :aria-busy="!loaded">
          <dt>Details</dt>
          <dd v-if="loaded">14 August 2026 · Karachi</dd>
          <dd v-else class="lumen-skeleton text-skeleton" />
        </dl>
      </article>
      <article>
        <h3>Inline validation and confirmation</h3>
        <form @submit.prevent="rename">
          <label for="showcase-album">Album name</label
          ><UInput
            id="showcase-album"
            v-model="album"
            :aria-invalid="validated && !!nameError"
            aria-describedby="showcase-album-hint"
          />
          <p id="showcase-album-hint" :role="validated && nameError ? 'alert' : undefined">
            {{ validated && nameError ? nameError : 'Choose a name for this album. 1 to 80 characters.' }}
          </p>
          <UButton type="submit">Rename album</UButton>
        </form>
        <UButton @click="removeFolder">Open confirmation…</UButton>
      </article>
      <article>
        <h3>Missing drive and startup recovery</h3>
        <UAlert
          :color="connected ? 'success' : 'warning'"
          variant="subtle"
          :title="connected ? 'D:\Photos is available again' : 'D:\Photos can’t be found.'"
          :description="
            connected
              ? 'Lumen has resumed looking through this folder.'
              : 'Reconnect the drive, then try again. Photos from other folders are still available.'
          "
          ><template #actions
            ><UButton @click="connected = !connected">{{
              connected ? 'Preview missing drive' : 'Try again'
            }}</UButton></template
          ></UAlert
        ><UButton @click="startup = true">Open full-window state</UButton>
        <p>Everything stays on this computer. Nothing is uploaded.</p>
      </article>
    </div>
    <UModal
      v-model:open="startup"
      fullscreen
      title="Lumen couldn’t open your library"
      description="The local library database could not be read. Your photos are still in their folders."
      ><template #body
        ><FeatureState
          kind="error"
          title="Lumen couldn’t open your library"
          text="Your photos are still in their folders. Try again, or save a problem report to help find the cause."
          ><UButton color="primary" variant="solid" @click="startup = false">Try again</UButton
          ><UButton
            @click="notify({ id: 'showcase.report', kind: 'success', title: 'Problem report saved on this PC.' })"
            >Save a problem report…</UButton
          ></FeatureState
        ></template
      ></UModal
    >
  </section>
</template>

<style scoped>
.state-reference {
  display: grid;
  gap: 1rem;
  padding: 1rem;
  border-bottom: 1px solid var(--ui-border);
}
h2 {
  font-size: 0.75rem;
  font-weight: 600;
  text-transform: uppercase;
  color: var(--ui-text-dimmed);
}
h3 {
  font: 600 1rem var(--font-display);
  color: var(--ui-text-highlighted);
}
h2,
h3,
p {
  margin: 0;
}
p {
  font-size: 0.8125rem;
  color: var(--ui-text-toned);
}
.columns {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 20rem), 1fr));
  gap: 1.5rem;
}
article {
  display: flex;
  flex-direction: column;
  gap: 1rem;
  min-width: 0;
}
article > button {
  align-self: flex-start;
}
.activity-examples {
  display: flex;
  align-items: flex-end;
  gap: 1rem;
}
.activity-examples > :first-child {
  flex: 1;
  min-width: 0;
}
.activity-examples > :last-child {
  flex: none;
  width: 3rem;
}
.row {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
}
.loading-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 0.25rem;
}
.loading-grid > div {
  display: grid;
  place-items: center;
  aspect-ratio: 1;
  background: var(--lumen-primary-soft);
  border-radius: var(--lumen-r-sm);
}
.loading-row {
  display: flex;
  align-items: center;
  gap: 0.75rem;
}
.cover {
  display: block;
  width: 4rem;
  height: 3rem;
  border-radius: var(--lumen-r-sm);
  background: var(--lumen-placeholder);
}
.text-skeleton {
  display: block;
  width: 65%;
  height: 1rem;
}
form {
  display: grid;
  gap: 0.75rem;
}
form button {
  justify-self: start;
}
dl {
  margin: 0;
  display: grid;
  gap: 0.5rem;
}
dd {
  margin: 0;
}
</style>
