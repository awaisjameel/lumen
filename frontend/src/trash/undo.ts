import { reportError } from '../shared/errors'
import { t } from '../shared/i18n'
import { dismiss, notify } from '../shared/notify'

/** The id of the notification that offers to undo "Move to Trash". */
export const TRASH_UNDO_NOTIFICATION = 'trash.undo'

/**
 * Says assets were moved to the Trash, and offers for a few seconds to put
 * them back with restore (the library's own, so every view follows).
 */
export function showTrashUndo(ids: number[], restore: (ids: number[]) => Promise<unknown>): void {
  notify({
    id: TRASH_UNDO_NOTIFICATION,
    kind: 'success',
    title: t('trash.moved', { n: ids.length }),
    duration: 8000,
    actions: [
      {
        label: t('trash.undo'),
        run: () => {
          dismiss(TRASH_UNDO_NOTIFICATION)
          void restore(ids).catch(reportError)
        },
      },
    ],
  })
}
