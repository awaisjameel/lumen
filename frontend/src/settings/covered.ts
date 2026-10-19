import { computed, markRaw, reactive, ref, shallowRef } from 'vue'
import { engine, errorCode, ErrorCode } from '../bridge'
import type { Block, CoveredItem } from '../bridge'
import { confirm } from '../shared/confirm'
import { ERROR_NOTIFICATION, errorText, reportError } from '../shared/errors'
import { t } from '../shared/i18n'
import { dismiss } from '../shared/notify'
import { showBlocksChanged } from '../shared/block'
import { showTrashUndo, TRASH_UNDO_NOTIFICATION } from '../trash/undo'
import type { ViewerActions, ViewerSource } from '../library/store'

/** What the browser needs of the Blocked screen around it. */
export interface CoveredHooks {
  /** The user did something with the engine's unlock: it counts for the screen's idle timer. */
  used(): void
  /** A load failed (or the engine said the screen is locked): the screen says why and, if locked, relocks. */
  failed(e: unknown): void
}

/** Scrolling and playback tell the screen it is in use at most this often (ms). */
const USE_THROTTLE_MS = 5000

/**
 * The Blocked screen's browser: the photos of the block it was opened on, and
 * the one open in the Viewer. Its state is private, so it lives in the screen
 * and not in a store: `close` empties it, and the screen calls that on
 * relocking and on leaving.
 */
export function useCoveredPhotos(hooks: CoveredHooks) {
  const block = shallowRef<Block | null>(null)
  const items = shallowRef<CoveredItem[]>([]) // up to tens of thousands
  const loading = ref(false)
  const error = ref('')
  const selected = shallowRef<ReadonlySet<number>>(new Set())
  const trashing = ref(false)
  const viewing = ref<number | null>(null)
  let request = 0
  let retryAction: (() => Promise<void>) | null = null

  const position = computed(() => new Map(items.value.map((it, i) => [it.id, i])))

  /** Opens the browser on a block and loads its photos. */
  async function open(b: Block) {
    close()
    const mine = request
    block.value = b
    loading.value = true
    try {
      const got = await engine.blockedItems(b.id)
      if (mine !== request) return
      items.value = got
      hooks.used()
    } catch (e) {
      if (mine !== request) return
      if (errorCode(e) === ErrorCode.BlocksLocked) {
        close()
        hooks.failed(e)
      } else error.value = errorText(e)
    } finally {
      if (mine === request) loading.value = false
    }
  }

  /** Closes the browser and the Viewer, and forgets the photos. */
  function close() {
    request++
    dismiss(TRASH_UNDO_NOTIFICATION)
    dismiss(ERROR_NOTIFICATION)
    error.value = ''
    retryAction = null
    block.value = null
    items.value = []
    viewing.value = null
    loading.value = false
    selected.value = new Set()
    trashing.value = false
  }

  function select(id: number, checked: boolean) {
    if (!position.value.has(id)) return
    const next = new Set(selected.value)
    if (checked) next.add(id)
    else next.delete(id)
    selected.value = next
    hooks.used()
  }

  function clearSelection() {
    selected.value = new Set()
  }

  /** Says the photos are being looked at (scrolling, playing a video), without telling the screen at every event. */
  let lastUse = Number.NEGATIVE_INFINITY
  function touch() {
    const now = Date.now()
    if (now - lastUse < USE_THROTTLE_MS) return
    lastUse = now
    hooks.used()
  }

  function view(id: number) {
    if (!position.value.has(id) || trashing.value) return
    viewing.value = id
    hooks.used()
  }

  function step(dir: 1 | -1) {
    const at = viewing.value === null ? undefined : position.value.get(viewing.value)
    const next = at === undefined ? undefined : items.value[at + dir]
    if (next) view(next.id)
    return Promise.resolve()
  }

  /**
   * Moves the photo in the Viewer to the Trash, and the Viewer on to the next
   * one (else the previous), once the user has been told that the Trash is
   * open to anyone: a photo kept behind the PIN leaves its protection there.
   * The Viewer covers the screen, so a failure is reported as a toast;
   * a locked screen relocks instead.
   */
  async function moveToTrash(ids?: number[]) {
    const id = viewing.value
    const targets = ids ?? (id === null ? [] : [id])
    if (trashing.value || !targets.length || targets.some((target) => !position.value.has(target))) return
    const mine = request
    error.value = ''
    retryAction = null
    trashing.value = true
    try {
      if (!(await confirm(t('covered.trashTitle'), t('covered.trashText'), t('trash.move')))) return
      if (mine !== request) return // the screen locked, or was left, while the question was open
      const at = id === null ? undefined : position.value.get(id)
      const removed = new Set(targets)
      const next =
        at === undefined
          ? undefined
          : (items.value.slice(at + 1).find((item) => !removed.has(item.id)) ??
            items.value
              .slice(0, at)
              .reverse()
              .find((item) => !removed.has(item.id)))
      try {
        await engine.trashCovered(targets)
      } catch (e) {
        if (mine !== request) return
        if (errorCode(e) === ErrorCode.BlocksLocked) hooks.failed(e)
        else {
          error.value = errorText(e)
          retryAction = () => moveToTrash(targets)
          reportError(e)
        }
        return
      }
      if (mine !== request) return
      items.value = items.value.filter((it) => !removed.has(it.id))
      selected.value = new Set([...selected.value].filter((target) => !removed.has(target)))
      if (viewing.value === id && id !== null && removed.has(id)) viewing.value = next?.id ?? null
      hooks.used()
      const restoreTrashed = async () => {
        if (mine !== request || !block.value || trashing.value) return
        trashing.value = true
        error.value = ''
        retryAction = null
        try {
          await engine.restoreAssets(targets)
          if (mine !== request) return
          const got = await engine.blockedItems(block.value.id)
          if (mine !== request) return
          items.value = got
          hooks.used()
          await showBlocksChanged()
        } catch (e) {
          if (mine !== request) return
          if (errorCode(e) === ErrorCode.BlocksLocked) hooks.failed(e)
          else {
            error.value = errorText(e)
            retryAction = restoreTrashed
            reportError(e)
          }
        } finally {
          if (mine === request) trashing.value = false
        }
      }
      showTrashUndo(targets, restoreTrashed)
      void showBlocksChanged().catch(reportError)
    } finally {
      if (mine === request) trashing.value = false
    }
  }

  const trashSelection = () => moveToTrash([...selected.value])

  const actions: ViewerActions = { moveToTrash: () => void moveToTrash(), details: false }

  /** The Viewer's source: the browser's photos, in the order listed. */
  const source: ViewerSource = {
    first() {
      const first = items.value[0]
      if (first) view(first.id)
      return Promise.resolve()
    },
    window() {
      const at = viewing.value === null ? undefined : position.value.get(viewing.value)
      return Promise.resolve(at === undefined ? [] : items.value.slice(Math.max(0, at - 12), at + 13))
    },
    jump: view,
    get id() {
      return viewing.value
    },
    get item() {
      const at = viewing.value === null ? undefined : position.value.get(viewing.value)
      return at === undefined ? null : (items.value[at] ?? null)
    },
    sel: null,
    touch,
    step,
    close() {
      viewing.value = null
    },
    actions,
  }

  // Reactive, so a component reading it need not unwrap each ref.
  return reactive({
    block,
    items,
    loading,
    error,
    viewing,
    selected,
    trashing,
    source: markRaw(source),
    open,
    retry: () => {
      if (retryAction) return retryAction()
      const current = block.value
      return current ? open(current) : Promise.resolve()
    },
    close,
    view,
    touch,
    select,
    clearSelection,
    trashSelection,
  })
}

export type CoveredPhotos = ReturnType<typeof useCoveredPhotos>
