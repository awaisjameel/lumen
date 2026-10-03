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

// What each notification wants told once it is gone.
const closed = new Map<string, () => void>()

function gone(id: string) {
  const tell = closed.get(id)
  closed.delete(id)
  tell?.()
}

function show(n: Notification) {
  // The same id again replaces the one showing, in place: the older one's
  // closing is not the newer one's, so it is not told.
  closed.delete(n.id)
  if (n.closed) closed.set(n.id, n.closed)
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
      onClick: () => {
        toast.remove(n.id)
        a.run()
      },
    })),
    'onUpdate:open': (open: boolean) => {
      if (!open) gone(n.id)
    },
  }
  // Removing and adding again would let the removal's delayed clean-up take
  // the new toast with it (they share the id): update the one showing.
  if (toast.toasts.value.some((shown) => shown.id === n.id)) toast.update(n.id, body)
  else toast.add({ id: n.id, ...body })
}

setPresenter({
  show,
  dismiss: (id) => {
    toast.remove(id)
    gone(id)
  },
})
onBeforeUnmount(() => setPresenter(null))
</script>

<template>
  <slot />
</template>
