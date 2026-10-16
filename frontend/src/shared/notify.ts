// The app's one notification channel. Anything may notify (a store, an
// error handler, a view); the shell presents them as toasts (see
// app/AppNotifications.vue), which registers itself here so shared code
// needs no knowledge of how they are shown.
import type { IconName } from '../design/icons'

// Persistent notices stay until answered; the stack scrolls at short heights
// instead of discarding an older error when another notification arrives.
export const notificationToaster = {
  position: 'bottom-right',
  expand: true,
  max: Number.MAX_SAFE_INTEGER,
  ui: {
    viewport: 'bottom-24 data-[expanded=true]:h-auto max-h-[calc(100dvh-7rem)] overflow-y-auto gap-3',
    base: 'relative inset-auto shrink-0 transform-none data-[state=open]:animate-[lumen-rise_var(--lumen-d-base)_var(--lumen-ease-out)] data-[state=closed]:animate-[lumen-fade_var(--lumen-d-fast)_var(--lumen-ease-in)_reverse]',
  },
} as const

export interface NotificationAction {
  label: string
  run: () => void
  disabled?: boolean
  /** Report actions keep the notice while a local save is in progress. */
  dismiss?: boolean
}

export interface Notification {
  /** Notifying again with the same id replaces the one showing. */
  id: string
  kind: 'error' | 'success' | 'info'
  title: string
  description?: string
  icon?: IconName
  /** Defaults to six seconds; errors and live work stay until dismissed. */
  duration?: number
  actions?: NotificationAction[]
  /** Live work replaces this notice in place; completion clears this field. */
  work?: { done: number; total: number }
  /** Called once it is gone, however it went. */
  closed?: () => void
}

export interface Presenter {
  show: (n: Notification) => void
  dismiss: (id: string) => void
}

let presenter: Presenter | null = null
// Notifications raised before the shell mounted (a failure at startup).
let waiting: Notification[] = []

/** The shell registers how notifications are shown; null when it unmounts. */
export function setPresenter(p: Presenter | null): void {
  presenter = p
  if (!p) return
  for (const n of waiting) p.show(n)
  waiting = []
}

/** Shows a notification, replacing one with the same id. */
export function notify(n: Notification): void {
  if (presenter) presenter.show(n)
  else waiting = [...waiting.filter((w) => w.id !== n.id), n]
}

/** Takes a notification down, if it is still showing. */
export function dismiss(id: string): void {
  const queued = waiting.find((w) => w.id === id)
  waiting = waiting.filter((w) => w.id !== id)
  queued?.closed?.()
  presenter?.dismiss(id)
}
