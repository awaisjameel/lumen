import { defineStore } from 'pinia'
import { computed, ref, shallowRef } from 'vue'
import { engine } from '../bridge'
import type { About } from '../bridge'
import { errorText, logError } from '../shared/errors'

export const CRASH_NOTIFICATION = 'about.crash'

/**
 * Lumen's own details (Settings → About Lumen), read once, and whether its
 * last run crashed, which App says until the user dismisses it.
 */
export const useAbout = defineStore('about', () => {
  const about = shallowRef<About | null>(null)
  const loadError = ref('')
  let loading: Promise<void> | null = null

  /** Reads the details, once; a failed read is tried again next time. */
  function load(): Promise<void> {
    loading ??= engine.about().then(
      (a) => {
        about.value = a
        loadError.value = ''
      },
      (e: unknown) => {
        loading = null
        loadError.value = errorText(e)
        logError(e)
      },
    )
    return loading
  }

  const crashDismissed = ref(false)
  /** The last run crashed and the user hasn't put the notice away. */
  const crashNotice = computed(() => !!about.value?.lastRunCrashed && !crashDismissed.value)
  function dismissCrash() {
    crashDismissed.value = true
  }

  const savingReport = ref(false)
  let saving: Promise<string> | null = null
  /**
   * Asks where to save a problem report and saves it there: where it went,
   * or "" if the user cancelled. A failure rejects, for the caller to say.
   */
  function saveReport(): Promise<string> {
    if (saving) return saving
    savingReport.value = true
    saving = engine.saveProblemReport().finally(() => {
      savingReport.value = false
      saving = null
    })
    return saving
  }

  return { about, loadError, load, crashNotice, dismissCrash, savingReport, saveReport }
})
