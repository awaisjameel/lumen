import { vi } from 'vitest'
import { t } from './i18n'
import { undoOutcomes } from './undo'
// What tests of components built on Nuxt UI need: the app's providers around
// the component (tooltips, portals, the theme), and a way to see what was
// notified without a toaster. Only test files import this.
import UApp from '@nuxt/ui/components/App.vue'
import ui from '@nuxt/ui/vue-plugin'
import { mount, flushPromises } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import type { Component, Plugin } from 'vue'
import { setPresenter } from './notify'
import { isNavigationPlugin } from './navigation/router'
import { createTestNavigation } from './navigation/testing'
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

/** Controls when an engine read settles, including obsolete failures. */
export function deferred<T>() {
  let complete: ((value: T) => void) | undefined
  let refuse: ((error: unknown) => void) | undefined
  const promise = new Promise<T>((done, reject) => {
    complete = done
    refuse = reject
  })
  return {
    promise,
    resolve(value: T) {
      if (!complete) throw new Error('Deferred promise was not initialised')
      complete(value)
    },
    reject(error: unknown) {
      if (!refuse) throw new Error('Deferred promise was not initialised')
      refuse(error)
    },
  }
}

/**
 * Mounts component inside UApp, attached to the document (Nuxt UI's
 * overlays are portalled to the body, and focus needs a real document).
 * Find the component with wrapper.findComponent; find overlay content in
 * document.body. Unmount it when the test ends.
 */
export function mountInApp(component: Component, options: MountInAppOptions = {}): VueWrapper {
  const plugins = options.plugins ?? []
  const navigation = plugins.some(isNavigationPlugin) ? [] : [createTestNavigation()]
  const Host = defineComponent({
    name: 'TestHost',
    render: () => h(UApp, { toaster: null }, { default: () => h(component, options.props, options.slots) }),
  })
  return mount(Host, {
    attachTo: document.body,
    global: { plugins: [ui, ...navigation, ...plugins], stubs: options.stubs },
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

export async function undo(notices: RecordedNotifications) {
  const action = notices.shown
    .flatMap((notice) => notice.actions ?? [])
    .find((action) => action.label === t('settings.undo'))
  if (!action) throw new Error('Missing Undo offer')
  action.run()
  await flushPromises()
  const outcomes = notices.shown
    .flatMap((notice) => notice.actions ?? [])
    .find((action) => action.label === t('library.outcomes'))
  if (!outcomes) throw new Error('Missing inverse outcomes')
  outcomes.run()
  const job = undoOutcomes.value
  if (!job) throw new Error('Missing inverse job')
  await vi.waitFor(() => {
    if (job.busy) throw new Error('Inverse is still running')
  })
  undoOutcomes.value = null
  return job
}
