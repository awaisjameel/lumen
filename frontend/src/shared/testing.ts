// What tests of components built on Nuxt UI need: the app's providers around
// the component (tooltips, portals, the theme), and a way to see what was
// notified without a toaster. Only test files import this.
import UApp from '@nuxt/ui/components/App.vue'
import ui from '@nuxt/ui/vue-plugin'
import { mount } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import type { Component, Plugin } from 'vue'
import { setPresenter } from './notify'
import type { Notification } from './notify'

export interface MountInAppOptions {
  /** The component's props. To change them later, pass refs' values through a wrapper of your own. */
  props?: Record<string, unknown>
  /** Slots for the component, as render functions. */
  slots?: Record<string, () => unknown>
  /** Plugins besides Nuxt UI's: a Pinia, a router. */
  plugins?: Plugin[]
  /** Components to stub, by name, as @vue/test-utils takes them. */
  stubs?: Record<string, Component | boolean>
}

/**
 * Mounts component inside UApp, attached to the document (Nuxt UI's
 * overlays are portalled to the body, and focus needs a real document).
 * Find the component with wrapper.findComponent; find overlay content in
 * document.body. Unmount it when the test ends.
 */
export function mountInApp(component: Component, options: MountInAppOptions = {}): VueWrapper {
  const Host = defineComponent({
    name: 'TestHost',
    render: () => h(UApp, { toaster: null }, { default: () => h(component, options.props, options.slots) }),
  })
  return mount(Host, {
    attachTo: document.body,
    global: { plugins: [ui, ...(options.plugins ?? [])], stubs: options.stubs },
  })
}

export interface RecordedNotifications {
  /** Every notification shown and not dismissed since, oldest first. */
  readonly shown: Notification[]
  /** The newest notification with this id, if it is showing. */
  find: (id: string) => Notification | undefined
  /** Stops recording and forgets what was shown. Call it when the test ends. */
  stop: () => void
}

/** Records what is notified (shared/notify.ts) in place of the app's toasts. */
export function recordNotifications(): RecordedNotifications {
  const shown: Notification[] = []
  const remove = (id: string) => {
    const i = shown.findIndex((n) => n.id === id)
    if (i >= 0) shown.splice(i, 1)
  }
  setPresenter({
    show: (n) => {
      remove(n.id)
      shown.push(n)
    },
    dismiss: remove,
  })
  return {
    shown,
    find: (id) => shown.find((n) => n.id === id),
    stop: () => {
      setPresenter(null)
      shown.length = 0
    },
  }
}
