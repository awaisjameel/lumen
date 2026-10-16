import { engine, ErrorCode, errorCode } from '../bridge'
import { confirm } from '../shared/confirm'
import { t } from '../shared/i18n'
import { nameTooLong, trimName } from '../shared/names'
import { MAX_NAME_LEN } from '../bridge'
import { notify } from '../shared/notify'
import { reportError } from '../shared/errors'

type MergeQuestion = (name: string) => Promise<boolean>
const askMerge: MergeQuestion = (name) =>
  confirm(t('person.mergeTitle', { name }), t('person.mergeText'), t('person.merge'))

/**
 * Names person id. If someone else already has that name, asks whether they
 * are one person and, if so, merges this one into them. Returns who to show
 * now (the other person after a merge), or null if the user said no.
 */
export async function namePerson(id: number, name: string, merge: MergeQuestion = askMerge): Promise<number | null> {
  name = trimName(name)
  if (nameTooLong(name)) throw new Error(t('error.invalidName', { max: MAX_NAME_LEN }))
  try {
    await engine.namePerson(id, name)
    return id
  } catch (e) {
    if (errorCode(e) !== ErrorCode.PersonExists) throw e
  }
  if (!(await merge(name))) return null
  const other = await engine.personByName(name)
  if (!other) return null // renamed meanwhile: nothing to merge with
  await engine.mergePeople(id, other)
  return other
}

/** Restoring a name never merges identities if someone has taken it meanwhile. */
export function showNameSaved(id: number, previous: string, name: string, reload: () => Promise<unknown>): void {
  notify({
    id: `person.${id}.name`,
    kind: 'success',
    title: name ? t('person.named', { name }) : t('person.nameRemoved'),
    duration: 8000,
    actions: [{ label: t('blocked.undo'), run: () => void undo() }],
  })
  async function undo() {
    try {
      await engine.namePerson(id, previous)
      await reload()
    } catch (e) {
      reportError(e)
    }
  }
}
