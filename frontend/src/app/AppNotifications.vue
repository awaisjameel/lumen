<script setup lang="ts">
// Presents the app's notifications (shared/notify.ts) as Nuxt UI toasts:
// the one place a failure, an Undo offer or a notice is shown. It renders
// nothing itself; the toaster is UApp's.
import { useToast } from '@nuxt/ui/composables'
import { onBeforeUnmount } from 'vue'
import { icons } from '../design/icons'
import type { IconName } from '../design/icons'
import { setPresenter } from '../shared/notify'
import type { Notification } from '../shared/notify'

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

function show(n: Notification) {
  // The same id again replaces the one showing, in place: the older one's
  // closing is not the newer one's, so it is not told.
  let toastID: string | number = shown.get(n.id)?.id ?? ''
  const existing = toast.toasts.value.find((t) => t.id === toastID)
  if (existing && !existing.open) toastID = ''
  const body = {
    title: n.title,
    description: n.description,
    icon: icons[n.icon ?? look[n.kind].icon],
    color: look[n.kind].color,
    // Nuxt UI keeps a toast with no duration for its default time: 0 is
    // "until dismissed".
    duration: n.duration ?? 6000,
    actions: n.actions?.map((a) => ({
      label: a.label,
      color: 'neutral' as const,
      variant: 'outline' as const,
      disabled: a.disabled,
      onClick: () => {
        if (a.disabled) return
        if (a.dismiss !== false) {
          toast.remove(toastID)
          gone(n.id, toastID)
        }
        a.run()
      },
    })),
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
    toast.remove(current.id)
    gone(id, current.id)
  },
})
onBeforeUnmount(() => setPresenter(null))
</script>

<template>
  <slot />
</template>
