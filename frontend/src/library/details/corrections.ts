import { onBeforeUnmount, ref } from 'vue'
import { errorText, reportError } from '../../shared/errors'
import { notify } from '../../shared/notify'
import { t } from '../../shared/i18n'

/** Camera clock values stay in UTC even when the operating system uses another zone. */
export function dateInput(seconds: number): string {
  return new Date(seconds * 1000).toISOString().slice(0, 19)
}

/** Reject calendar rollovers before converting a user's wall-clock date to seconds. */
export function parseDateInput(value: string): number | undefined {
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(:\d{2})?$/.test(value)) return
  const full = value.length === 16 ? `${value}:00` : value
  const date = new Date(`${full}Z`)
  if (!Number.isFinite(date.getTime()) || date.getUTCFullYear() < 1800 || date.getUTCFullYear() > 2199) return
  if (date.toISOString().slice(0, 19) !== full) return
  return date.getTime() / 1000
}

/** Keep write errors in the dialog; Undo survives its closure and reports through the toast owner. */
export function useCorrection(changed: () => void, close: () => void) {
  const busy = ref(false)
  const error = ref('')
  let alive = true
  onBeforeUnmount(() => (alive = false))
  async function save(
    id: string,
    title: string,
    /** Makes the change; resolves false when it did not (the dialog moved on). */
    apply: (current: () => boolean) => Promise<boolean>,
    undo: () => Promise<void>,
  ) {
    if (busy.value) return
    busy.value = true
    error.value = ''
    try {
      if (!(await apply(() => alive))) return
      changed()
      notify({
        id,
        title,
        kind: 'success',
        actions: [{ label: t('settings.undo'), run: () => void undo().then(changed).catch(reportError) }],
      })
      if (alive) close()
    } catch (e) {
      if (alive) error.value = errorText(e)
    } finally {
      if (alive) busy.value = false
    }
  }
  return { busy, error, save }
}
