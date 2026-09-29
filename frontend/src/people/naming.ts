import { engine, ErrorCode, errorCode } from '../bridge'
import { confirm } from '../shared/confirm'
import { t } from '../shared/i18n'

/**
 * Names person id. If someone else already has that name, asks whether they
 * are one person and, if so, merges this one into them. Returns who to show
 * now (the other person after a merge), or null if the user said no.
 */
export async function namePerson(id: number, name: string): Promise<number | null> {
  name = name.trim()
  try {
    await engine.namePerson(id, name)
    return id
  } catch (e) {
    if (errorCode(e) !== ErrorCode.PersonExists) throw e
  }
  if (!(await confirm(t('person.mergeTitle', { name }), t('person.mergeText'), t('person.merge')))) return null
  const other = await engine.personByName(name)
  if (!other) return null // renamed meanwhile: nothing to merge with
  await engine.mergePeople(id, other)
  return other
}
