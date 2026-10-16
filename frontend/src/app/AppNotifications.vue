<script setup lang="ts">
// Presents the app's notifications (shared/notify.ts) as Nuxt UI toasts:
// the one place a failure, an Undo offer or a notice is shown. It renders
// nothing itself; the toaster is UApp's.
import UButton from '@nuxt/ui/components/Button.vue'
import { useToast } from '@nuxt/ui/composables'
import { h, nextTick, onBeforeUnmount } from 'vue'
import { t } from '../shared/i18n'
import { icons } from '../design/icons'
import type { IconName } from '../design/icons'
import { setPresenter } from '../shared/notify'
import type { Notification, NotificationAction } from '../shared/notify'

const toast = useToast()

const look: Record<Notification['kind'], { color: 'error' | 'success' | 'neutral'; icon: IconName }> = {
  error: { color: 'error', icon: 'error' },
  success: { color: 'success', icon: 'success' },
  info: { color: 'neutral', icon: 'info' },
}

// A notification's logical id and the current toast carrying it. A toast
// already closing keeps its own id, so its delayed removal cannot take a
// newer notification with it.
const shown = new Map<string, { id: string | number; closed?: () => void }>()

function gone(id: string, toastID: string | number) {
  const current = shown.get(id)
  if (current?.id !== toastID) return
  shown.delete(id)
  current.closed?.()
}

// Nuxt UI ignores removing a toast updated in the same tick.
const take = (toastID: string | number) => void nextTick(() => toast.remove(toastID))

function show(n: Notification) {
  // The same id again replaces the one showing, in place: the older one's
  // closing is not the newer one's, so it is not told.
  let toastID: string | number = shown.get(n.id)?.id ?? ''
  const existing = toast.toasts.value.find((t) => t.id === toastID)
  if (existing && !existing.open) toastID = ''
  const button = (a: NotificationAction) => ({
    label: a.label,
    color: 'neutral' as const,
    variant: 'ghost' as const,
    class: 'text-(--lumen-toast-text) hover:bg-elevated focus-visible:ring-inverted',
    disabled: a.disabled,
    onClick: () => {
      if (a.disabled) return
      if (a.dismiss !== false) {
        take(toastID)
        gone(n.id, toastID)
      }
      a.run()
    },
  })
  // Nuxt UI wraps its actions in Reka's ToastAction, which always closes the
  // toast: an action that keeps its notice is drawn below the description.
  const keeping = n.actions?.filter((a) => a.dismiss === false) ?? []
  const body = {
    title: n.title,
    description:
      keeping.length || n.work
        ? () =>
            h('div', [
              n.description,
              n.work
                ? h(
                    'div',
                    {
                      role: 'progressbar',
                      'aria-label': n.title,
                      'aria-valuemin': 0,
                      'aria-valuemax': n.work.total,
                      'aria-valuenow': n.work.done,
                      class: 'mt-3 h-1 overflow-hidden rounded-full bg-accented',
                    },
                    h('span', {
                      class: 'block size-full origin-left bg-primary',
                      style: {
                        transform: `scaleX(${n.work.total > 0 ? Math.min(1, Math.max(0, n.work.done / n.work.total)) : 0})`,
                      },
                    }),
                  )
                : null,
              keeping.length
                ? h(
                    'div',
                    { class: 'mt-2.5 flex flex-wrap gap-1.5' },
                    keeping.map((a) => h(UButton, { size: 'xs', ...button(a) })),
                  )
                : null,
            ])
        : n.description,
    icon: icons[n.icon ?? look[n.kind].icon],
    color: look[n.kind].color,
    // Nuxt UI keeps a toast with no duration for its default time: 0 is
    // "until dismissed".
    progress: { color: 'primary' as const, ui: { base: 'h-0.5', indicator: 'transition-none' } },
    duration: n.duration ?? (n.kind === 'error' || n.work ? 0 : 6000),
    type: n.kind === 'error' ? ('foreground' as const) : ('background' as const),
    close: { 'aria-label': t('notify.dismiss'), class: 'text-(--lumen-toast-text)' },
    actions: n.actions?.filter((a) => a.dismiss !== false).map(button),
    'onUpdate:open': (open: boolean) => {
      if (!open) gone(n.id, toastID)
    },
  }
  // Removing and adding again would let the removal's delayed clean-up take
  // the new toast with it (they share the id): update the one showing.
  if (existing?.open) toast.update(toastID, body)
  else toastID = toast.add({ ...(toastID ? { id: toastID } : {}), ...body }).id
  shown.set(n.id, { id: toastID, closed: n.closed })
}

setPresenter({
  show,
  dismiss: (id) => {
    const current = shown.get(id)
    if (!current) return
    take(current.id)
    gone(id, current.id)
  },
})
onBeforeUnmount(() => setPresenter(null))
</script>

<template>
  <slot />
</template>
