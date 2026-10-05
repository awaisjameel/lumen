<script setup lang="ts">
// The component showcase: the reference for reviewing screens against the
// design system. Development only: the router adds its route in a dev
// build alone, so nothing of it ships. It tries every theme, accent,
// density and text size without saving them (the user's own preferences
// come back on leaving), at the widths the layouts must survive, with long
// labels and the loading, empty and failure states.
// Its wording is a developer's sample text, not the product's, so it is
// written here and not in the dictionary (shared/i18n.ts).
import IconButton from '../../shared/IconButton.vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UBadge from '@nuxt/ui/components/Badge.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UCheckbox from '@nuxt/ui/components/Checkbox.vue'
import UDropdownMenu from '@nuxt/ui/components/DropdownMenu.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import UKbd from '@nuxt/ui/components/Kbd.vue'
import UModal from '@nuxt/ui/components/Modal.vue'
import URadioGroup from '@nuxt/ui/components/RadioGroup.vue'
import USelect from '@nuxt/ui/components/Select.vue'
import USlider from '@nuxt/ui/components/Slider.vue'
import USwitch from '@nuxt/ui/components/Switch.vue'
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { Accent, Density, TEXT_SCALE_MAX, TEXT_SCALE_MIN, Theme } from '../../bridge'
import { applyAppearance } from '../../design/appearance'
import type { Appearance } from '../../design/appearance'
import { icons } from '../../design/icons'
import type { IconName } from '../../design/icons'
import CardGrid from '../../shared/CardGrid.vue'
import CollectionCard from '../../shared/CollectionCard.vue'
import { availableCommands, keyCaps } from '../../shared/commands'
import { confirm } from '../../shared/confirm'
import FeatureState from '../../shared/FeatureState.vue'
import Icon from '../../shared/Icon.vue'
import MediaTile from '../../shared/MediaTile.vue'
import MetaRow from '../../shared/MetaRow.vue'
import { notify } from '../../shared/notify'
import Page from '../../shared/Page.vue'
import PageHeader from '../../shared/PageHeader.vue'
import { usePreferences } from '../../shared/preferences'
import PrefRow from '../../shared/PrefRow.vue'
import SelectionBar from '../../shared/SelectionBar.vue'
import WorkProgress from '../../shared/WorkProgress.vue'

const prefs = usePreferences()

// What the showcase tries on. Applied but never saved.
const look = reactive<Appearance>({
  theme: prefs.prefs.theme,
  accent: prefs.prefs.accent,
  density: prefs.prefs.density,
  textScale: prefs.prefs.textScale,
})
watch(look, (a) => applyAppearance(a))
onBeforeUnmount(() => applyAppearance(prefs.prefs))

const themes = [Theme.ThemeSystem, Theme.ThemeLight, Theme.ThemeDark]
const accents = [
  Accent.AccentBlue,
  Accent.AccentTeal,
  Accent.AccentGreen,
  Accent.AccentViolet,
  Accent.AccentRose,
  Accent.AccentAmber,
]
const densities = [Density.DensityComfortable, Density.DensityCompact]

// The content widths a screen must survive: the frame is a size container,
// so components respond to it as they do to the app's content area.
const widths = [
  { label: 'Full', value: '100%' },
  { label: '1200', value: '1200px' },
  { label: '900', value: '900px' },
  { label: '600', value: '600px' },
  { label: '480', value: '480px' },
]
const width = ref('100%')

const LONG = 'A very long name that a person, an album or a place can really have, to see where it is cut off'

const modal = ref(false)
const on = ref(true)
const checked = ref(true)
const choice = ref('one')
const picked = ref('one')
const amount = ref(40)
const text = ref('')
const options = [
  { label: 'First choice', value: 'one' },
  { label: 'Second choice with a longer label', value: 'two' },
]
const menu = [
  [
    { label: 'Rename', icon: icons.pencil },
    { label: 'Pin', icon: icons.pin },
  ],
  [{ label: 'Delete', icon: icons.trash, color: 'error' as const }],
]

const iconNames = computed(() => Object.keys(icons).filter((name): name is IconName => name in icons))

function toast(kind: 'error' | 'success' | 'info') {
  notify({
    id: `showcase.${kind}`,
    kind,
    title: kind === 'error' ? 'Something went wrong: the disk is full' : 'Moved 3 items to the Trash',
    actions: kind === 'success' ? [{ label: 'Undo', run: () => undefined }] : undefined,
  })
}
function ask() {
  void confirm('Delete 3 files?', 'They move to the Recycle Bin. Nothing is deleted for good.', 'Delete')
}
</script>

<template>
  <Page>
    <template #header>
      <PageHeader title="Component showcase" subtitle="Development only">
        <URadioGroup v-model="look.theme" :items="themes" orientation="horizontal" variant="table" size="xs" />
        <USelect v-model="look.accent" :items="accents" class="w-28" aria-label="Accent" />
        <USelect v-model="look.density" :items="densities" class="w-36" aria-label="Density" />
        <USlider
          v-model="look.textScale"
          :min="TEXT_SCALE_MIN"
          :max="TEXT_SCALE_MAX"
          :step="5"
          class="w-28"
          aria-label="Text size"
        />
      </PageHeader>
    </template>

    <div class="widths" role="group" aria-label="Content width">
      <UButton
        v-for="w in widths"
        :key="w.value"
        size="xs"
        :variant="width === w.value ? 'solid' : 'subtle'"
        :color="width === w.value ? 'primary' : 'neutral'"
        :aria-pressed="width === w.value"
        @click="width = w.value"
      >
        {{ w.label }}
      </UButton>
    </div>

    <div class="frame" :style="{ width }">
      <section>
        <h2>Page header, long title</h2>
        <PageHeader :title="LONG" subtitle="1,234 items" :back="{ to: '/', label: 'Back to Library' }">
          <UInput :leading-icon="icons.search" placeholder="Search" class="flex-1 max-w-80" />
          <UButton :icon="icons.plus" color="primary" variant="solid">New album</UButton>
          <IconButton :icon="icons.more" label="More actions" color="neutral" variant="ghost" />
        </PageHeader>
      </section>

      <section>
        <h2>Buttons</h2>
        <div class="row">
          <UButton color="primary" variant="solid">Primary</UButton>
          <UButton>Default</UButton>
          <UButton variant="outline">Outline</UButton>
          <UButton variant="ghost">Ghost</UButton>
          <UButton color="error" variant="soft" :icon="icons.trash">Destructive</UButton>
          <UButton disabled>Disabled</UButton>
          <UButton loading>Busy</UButton>
          <IconButton :icon="icons.heart" label="Favourite" :kbds="['F']" variant="ghost" />
        </div>
      </section>

      <section>
        <h2>Form controls</h2>
        <div class="row">
          <UInput v-model="text" placeholder="A text field" />
          <USelect v-model="picked" :items="options" class="w-56" />
          <USwitch v-model="on" label="A switch" />
          <UCheckbox v-model="checked" label="A checkbox" />
          <USlider v-model="amount" class="w-40" aria-label="A slider" />
        </div>
        <URadioGroup v-model="choice" :items="options" legend="A radio group" />
      </section>

      <section>
        <h2>Overlays and feedback</h2>
        <div class="row">
          <UButton @click="modal = true">Dialog</UButton>
          <UButton @click="ask">Confirmation</UButton>
          <UDropdownMenu :items="menu">
            <UButton :trailing-icon="icons['chevron-down']">Menu</UButton>
          </UDropdownMenu>
          <UButton @click="toast('success')">Toast with Undo</UButton>
          <UButton @click="toast('error')">Failure toast</UButton>
          <UButton @click="toast('info')">Notice toast</UButton>
          <UBadge color="neutral" variant="subtle">Built in</UBadge>
          <UBadge color="primary" variant="subtle">Smart album</UBadge>
        </div>
        <UAlert color="error" variant="subtle" :icon="icons.error" title="This album can’t be saved." />
        <UModal v-model:open="modal" title="A dialog" description="Focus stays inside it and returns to its button.">
          <template #body>
            <UInput placeholder="The first field takes the focus" autofocus />
          </template>
          <template #footer>
            <UButton variant="outline" @click="modal = false">Cancel</UButton>
            <UButton color="primary" variant="solid" @click="modal = false">Save</UButton>
          </template>
        </UModal>
      </section>

      <section>
        <h2>States</h2>
        <div class="states">
          <FeatureState kind="loading" title="Searching…" />
          <FeatureState kind="empty" icon="albums" title="No albums yet" text="Create one to collect photos.">
            <UButton color="primary" variant="solid" :icon="icons.plus">New album</UButton>
          </FeatureState>
          <FeatureState kind="error" title="Lumen couldn’t read this list" text="Something went wrong: disk full">
            <UButton :icon="icons.refresh">Try again</UButton>
          </FeatureState>
        </div>
      </section>

      <section>
        <h2>Collection cards</h2>
        <CardGrid>
          <CollectionCard to="/" title="Ali" count="124 items" icon="user" round />
          <CollectionCard to="/" :title="LONG" count="1 item" icon="things">
            <template #badge><UBadge size="sm" color="neutral" variant="solid">Built in</UBadge></template>
            <IconButton :icon="icons.more" label="More actions" size="xs" color="neutral" variant="solid" />
          </CollectionCard>
          <CollectionCard to="/" title="Holiday 2024" subtitle="Smart album" count="2,301 items" icon="albums" />
        </CardGrid>
        <CardGrid layout="row">
          <CollectionCard
            to="/"
            title="Lahore"
            subtitle="Punjab, Pakistan"
            count="88 items"
            icon="places"
            layout="row"
          />
          <CollectionCard to="/" :title="LONG" :subtitle="LONG" count="12 items" icon="events" layout="row">
            <IconButton :icon="icons.pin" label="Pin" variant="ghost" />
            <IconButton :icon="icons.more" label="More actions" variant="ghost" />
          </CollectionCard>
        </CardGrid>
      </section>

      <section>
        <h2>Media tiles</h2>
        <div class="tiles">
          <div class="tile"><MediaTile :id="0" :thumb="0" /></div>
          <div class="tile"><MediaTile :id="1" :thumb="0" /></div>
          <div class="tile"><MediaTile :id="2" :thumb="0" favourite /></div>
          <div class="tile"><MediaTile :id="3" :thumb="0" video :duration-ms="83_000" /></div>
          <div class="tile"><MediaTile :id="4" :thumb="0" selected favourite /></div>
        </div>
        <SelectionBar summary="3 selected" hint="Moving them to the Trash keeps the files on disk.">
          <UButton :icon="icons.restore">Restore</UButton>
          <UButton color="error" variant="soft" :icon="icons.trash">Move to Trash</UButton>
        </SelectionBar>
      </section>

      <section>
        <h2>Facts, settings and background work</h2>
        <MetaRow icon="calendar" label="Taken" value="14 August 2024, 18:32 (from the photo)" />
        <MetaRow icon="folder" label="File" :value="`D:\\Photos\\2024\\${LONG}.jpg`">
          <template #action
            ><IconButton :icon="icons['folder-open']" label="Show in folder" variant="ghost"
          /></template>
        </MetaRow>
        <MetaRow icon="places" label="Place" value="Not recorded" />
        <div class="prefs">
          <PrefRow label="Index videos" description="Videos take longer to index than photos.">
            <USwitch v-model="on" aria-label="Index videos" />
          </PrefRow>
          <PrefRow
            label="Thumbnail cache"
            description="How much disk space previews may use."
            error="Something went wrong: disk full"
          >
            <UInput model-value="2" class="w-24" aria-label="Thumbnail cache" />
          </PrefRow>
        </div>
        <div class="work">
          <WorkProgress label="Making thumbnails" detail="1,234 left" :done="766" :total="2000">
            <IconButton :icon="icons.pause" label="Pause" size="xs" variant="ghost" />
          </WorkProgress>
          <WorkProgress label="Looking for photos" detail="3,500 found" indeterminate />
          <WorkProgress label="Moving files stopped" detail="12 of 40 moved" :done="12" :total="40" failed />
        </div>
      </section>

      <section>
        <h2>Commands registered now</h2>
        <ul class="commands">
          <li v-for="c in availableCommands" :key="c.id">
            <Icon v-if="c.icon" :name="c.icon" :size="16" />
            <span>{{ c.title() }}</span>
            <span v-if="c.keys" class="caps"><UKbd v-for="cap in keyCaps(c.keys)" :key="cap" :value="cap" /></span>
          </li>
        </ul>
      </section>

      <section>
        <h2>Icons ({{ iconNames.length }})</h2>
        <div class="icons">
          <span v-for="name in iconNames" :key="name" :title="name"><Icon :name="name" /></span>
        </div>
      </section>
    </div>
  </Page>
</template>

<style scoped>
.widths {
  display: flex;
  gap: 0.375rem;
  margin-bottom: 0.75rem;
}
.frame {
  max-width: 100%;
  border: 1px dashed var(--ui-border-accented);
  border-radius: calc(var(--ui-radius) * 2);
  container-type: inline-size;
}
section {
  display: grid;
  gap: 0.75rem;
  padding: 1rem;
  border-bottom: 1px solid var(--ui-border-muted);
}
h2 {
  margin: 0;
  color: var(--ui-text-dimmed);
  font-size: 0.75rem;
  font-weight: 600;
  letter-spacing: 0.04em;
  text-transform: uppercase;
}
.row {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 0.5rem;
}
.states {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(14rem, 1fr));
  gap: 0.75rem;
}
.states > * {
  border: 1px solid var(--ui-border);
  border-radius: calc(var(--ui-radius) * 2);
}
.tiles {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}
.tile {
  width: 120px;
  height: 120px;
}
.prefs {
  container-type: inline-size;
}
.work {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(14rem, 1fr));
  gap: 1rem;
}
.commands {
  display: grid;
  gap: 0.25rem;
  margin: 0;
  padding: 0;
  list-style: none;
}
.commands li {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}
.caps {
  display: flex;
  gap: 0.25rem;
  margin-left: auto;
}
.icons {
  display: flex;
  flex-wrap: wrap;
  gap: 0.625rem;
  color: var(--ui-text-muted);
}
</style>
