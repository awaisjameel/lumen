import { computed, shallowReactive } from 'vue'
import { t } from '../shared/i18n'

/**
 * What a setting's row says about its saves: "Saving…" while any is on its
 * way, then "Saved" unless the newest failed (failed(key) says why, and the
 * row shows that instead). A note (a value brought into range) shows between.
 */
export function useSaveFeedback<K extends string>(failed: (key: K) => string | undefined) {
  const running = shallowReactive(new Map<K, number>())
  const saved = shallowReactive(new Set<K>())

  async function save(key: K, fn: () => Promise<unknown>): Promise<void> {
    saved.delete(key)
    running.set(key, (running.get(key) ?? 0) + 1)
    try {
      await fn()
    } finally {
      const left = (running.get(key) ?? 1) - 1
      if (left) running.set(key, left)
      else running.delete(key)
      if (!left && !failed(key)) saved.add(key)
    }
  }

  const saving = (key: K): boolean => running.has(key)

  function hint(key: K, note = ''): string {
    if (saving(key)) return t('settings.saving')
    return note || (saved.has(key) ? t('settings.saved') : '')
  }

  const savingAny = computed(() => running.size > 0)
  return { save, saving, hint, savingAny }
}
