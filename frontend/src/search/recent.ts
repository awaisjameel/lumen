import { defineStore } from 'pinia'
import { ref } from 'vue'
import { engine } from '../bridge'
import { logError, reportError } from '../shared/errors'
import { nameIncludes, trimName } from '../shared/names'

/** How many recent searches the box offers while the user types. */
const SHOWN_WHILE_TYPING = 4

/**
 * The searches the user ran last, newest first, kept by the engine in the
 * library. The engine leaves out any that names something blocked, so the
 * list is read again whenever the search box opens rather than kept live.
 */
export const useRecentSearches = defineStore('recentSearches', () => {
  const list = ref<string[]>([])
  let revision = 0

  async function load(): Promise<void> {
    const mine = ++revision
    try {
      const recent = await engine.recentSearches()
      if (mine === revision) list.value = recent
    } catch (e) {
      logError(e)
    }
  }

  /** Keeps query as the newest search; a failure only costs the history. */
  async function remember(query: string): Promise<void> {
    const mine = ++revision
    try {
      const recent = await engine.rememberSearch(query)
      if (mine === revision) list.value = recent
    } catch (e) {
      logError(e)
    }
  }

  async function clear(): Promise<void> {
    const before = list.value
    revision++
    list.value = []
    try {
      await engine.clearRecentSearches()
    } catch (e) {
      list.value = before
      reportError(e)
    }
  }

  /**
   * The recent searches to offer for text: all of them while the box is
   * empty, else those holding the text or one of its words, other than the
   * text itself, which the box offers already.
   */
  function matching(text: string): string[] {
    const q = trimName(text)
    if (!q) return list.value
    const words = q.split(/\s+/u).filter((w) => w.length > 1)
    return list.value
      .filter(
        (s) =>
          !(s.length === q.length && nameIncludes(s, q)) &&
          (nameIncludes(s, q) || words.some((w) => nameIncludes(s, w))),
      )
      .slice(0, SHOWN_WHILE_TYPING)
  }

  return { list, load, remember, clear, matching }
})
