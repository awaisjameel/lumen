import { defineStore } from 'pinia'
import { computed, onScopeDispose, ref, shallowRef } from 'vue'
import { engine, onUpdateStatus, UpdatePhase } from '../bridge'
import type { UpdatePolicy, UpdateStatus } from '../bridge'
import { errorText } from '../shared/errors'
import { usePreferences } from '../shared/preferences'

/** Installation snapshots outlive Settings and never switch with the library. */
export const useUpdates = defineStore('updates', () => {
  const status = shallowRef<UpdateStatus | null>(null)
  const error = ref('')
  const saving = ref(false)
  const pending = ref(false)
  let stop: (() => void) | undefined
  let initial: Promise<void> | undefined
  let saves = Promise.resolve()
  let queuedSaves = 0

  const busy = computed(
    () =>
      pending.value ||
      (!!status.value &&
        [
          UpdatePhase.UpdateChecking,
          UpdatePhase.UpdateDownloading,
          UpdatePhase.UpdateVerifying,
          UpdatePhase.UpdateStaging,
          UpdatePhase.UpdateQuiescing,
          UpdatePhase.UpdateApplying,
          UpdatePhase.UpdateAwaitingHealth,
        ].includes(status.value.phase)),
  )

  function accept(next: UpdateStatus) {
    if (status.value && next.generation < status.value.generation) return
    status.value = next
  }

  function start(): Promise<void> {
    if (initial) return initial
    stop = onUpdateStatus(accept)
    initial = Promise.resolve()
      .then(() => engine.updateStatus())
      .then(accept)
      .catch((e: unknown) => {
        error.value = errorText(e)
      })
    return initial
  }
  onScopeDispose(() => stop?.())

  async function run(action: () => Promise<void>) {
    if (pending.value) return
    error.value = ''
    pending.value = true
    try {
      await action()
      accept(await engine.updateStatus())
    } catch (e) {
      error.value = errorText(e)
    } finally {
      pending.value = false
    }
  }

  function setPolicy(patch: Partial<UpdatePolicy>): Promise<void> {
    queuedSaves++
    saving.value = true
    saves = saves
      .then(async () => {
        if (!status.value) return
        error.value = ''
        try {
          await engine.setUpdatePolicy({ ...status.value.policy, ...patch })
          accept(await engine.updateStatus())
        } catch (e) {
          error.value = errorText(e)
        }
      })
      .finally(() => {
        saving.value = --queuedSaves > 0
      })
    return saves
  }

  async function restart() {
    await saves
    await run(async () => {
      await usePreferences().flush()
      await engine.restartUpdate()
    })
  }

  async function cancel() {
    try {
      await engine.cancelUpdate()
    } catch (e) {
      error.value = errorText(e)
    }
  }

  return {
    status,
    error,
    busy,
    saving,
    start,
    setPolicy,
    restart,
    check: () => run(engine.checkUpdates),
    download: () => run(engine.downloadUpdate),
    cancel,
    discard: () => run(engine.discardUpdate),
    dismiss: () => run(engine.dismissUpdate),
  }
})
