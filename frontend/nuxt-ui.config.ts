// How Nuxt UI is built into Lumen: one set of options for the app's build
// (vite.config.ts) and for the tests (vitest.config.ts), so both see the
// same components, theme and icons.
import type { NuxtUIOptions } from '@nuxt/ui/unplugin'
import { bundledIcons } from './src/design/icons.ts'

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
    colors: { primary: 'blue', neutral: 'zinc' },
    // Lumen's decisions about Nuxt UI's controls, in one place. Sizes come
    // from the density tokens (src/design/tokens.css), not from here.
    button: { defaultVariants: { color: 'neutral', variant: 'subtle' } },
    modal: { slots: { content: 'lumen-overlay' } },
    slideover: { slots: { content: 'lumen-overlay' } },
    tooltip: { slots: { content: 'lumen-tooltip' } },
  },
}
