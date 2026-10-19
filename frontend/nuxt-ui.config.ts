// How Nuxt UI is built into Lumen: one set of options for the app's build
// (vite.config.ts) and for the tests (vitest.config.ts), so both see the
// same components, theme and icons.
import type { NuxtUIOptions } from '@nuxt/ui/unplugin'
import { bundledIcons } from './src/design/icons.ts'

// Every floating surface (dialogs, sheets, menus, popovers, lists) is one
// raised overlay: its background, shadow and arrival come from tokens.css.
const overlay = { slots: { content: 'lumen-overlay' } }

export const uiOptions: NuxtUIOptions = {
  // Lumen's own preference store owns the theme (src/design/appearance.ts):
  // Nuxt UI's colour-mode integration would be a second owner writing to
  // the WebView's storage.
  colorMode: false,
  // Components and composables are imported by name, as the rest of the
  // code is: nothing appears in a template that its script did not import.
  autoImport: false,
  components: false,
  dts: false,
  // Icons come only from the local collection, and only those in the
  // manifest (plus Nuxt UI's own). A name that does not resolve fails the
  // build rather than being fetched when it is first shown.
  icon: { clientBundle: { icons: bundledIcons } },
  ui: {
    colors: { primary: 'blue', neutral: 'slate' },
    // Lumen's decisions about Nuxt UI's controls, in one place. Sizes come
    // from the density tokens (src/design/tokens.css), not from here.
    // Buttons are semibold and give a little under the pointer.
    button: {
      slots: {
        base: 'font-semibold active:not-disabled:scale-[0.97] transition-[color,background-color,box-shadow,transform]',
      },
      defaultVariants: { color: 'neutral', variant: 'subtle' },
    },
    modal: { slots: { content: 'lumen-overlay rounded-(--lumen-r-xl)', footer: 'lumen-dialog-foot' } },
    slideover: overlay,
    drawer: overlay,
    popover: overlay,
    dropdownMenu: overlay,
    contextMenu: overlay,
    select: overlay,
    selectMenu: overlay,
    inputMenu: overlay,
    // A notice is the dark pill of either theme; its reason goes on its own line.
    toast: { slots: { root: 'lumen-toast', description: 'whitespace-pre-line' } },
    tooltip: { slots: { content: 'lumen-tooltip' } },
  },
}
