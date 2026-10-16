<script setup lang="ts">
// Everything that can cover a page, mounted once: the confirmation, the
// block dialog, the command palette and the shortcut reference. It also
// owns the page-wide shortcut listener and the commands that belong to the
// app as a whole (going somewhere, the palette, the reference).
import { defineAsyncComponent, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useLibrary } from '../library/store'
import BlockDialog from '../shared/BlockDialog.vue'
import { onGlobalKey, useCommands } from '../shared/commands'
import type { Command } from '../shared/commands'
import ConfirmDialog from '../shared/ConfirmDialog.vue'
import { t } from '../shared/i18n'
import { startPortalLayers } from '../shared/overlay'
import { navEntries } from './nav'

const router = useRouter()
const lib = useLibrary()

// The palette and the shortcut reference are loaded when first asked for:
// most sessions never open them, and the start should not wait for them.
const CommandPalette = defineAsyncComponent(() => import('./CommandPalette.vue'))
const ShortcutHelp = defineAsyncComponent(() => import('./ShortcutHelp.vue'))
const palette = ref(false)
const shortcuts = ref(false)
const paletteUsed = ref(false)
const shortcutsUsed = ref(false)

// Navigation only makes sense once there is a library to navigate.
const inApp = () => lib.ready && lib.sources.length > 0

useCommands([
  ...navEntries.map((e): Command => ({
    id: `go.${e.name}`,
    group: 'navigate',
    title: () => t('commands.goTo', { place: t(e.label) }),
    icon: e.icon,
    available: inApp,
    run: () => router.push(e.path),
  })),
  // Videos have no page of their own: this opens Library on its Videos
  // filter, while videos are indexed.
  {
    id: 'go.videos',
    group: 'navigate',
    title: () => t('commands.showVideos'),
    icon: 'video',
    available: () => inApp() && lib.settings.videos,
    run: () => router.push('/videos'),
  },
  {
    id: 'app.commandPalette',
    group: 'app',
    title: () => t('commands.title'),
    icon: 'command',
    keys: 'Ctrl+K',
    available: inApp,
    run: () => (paletteUsed.value = palette.value = true),
  },
  {
    id: 'app.shortcuts',
    group: 'app',
    title: () => t('shortcuts.title'),
    icon: 'keyboard',
    keys: '?',
    run: () => (shortcutsUsed.value = shortcuts.value = true),
  },
])

let stopPortalLayers: (() => void) | undefined
onMounted(() => {
  stopPortalLayers = startPortalLayers()
  window.addEventListener('keydown', onGlobalKey)
})
onBeforeUnmount(() => {
  stopPortalLayers?.()
  window.removeEventListener('keydown', onGlobalKey)
})
</script>

<template>
  <ConfirmDialog />
  <BlockDialog />
  <CommandPalette v-if="paletteUsed" v-model:open="palette" />
  <ShortcutHelp v-if="shortcutsUsed" v-model:open="shortcuts" />
</template>
