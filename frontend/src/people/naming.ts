import { engine } from '../bridge'
import { confirm } from '../shared/confirm'
import { t } from '../shared/i18n'

/**
 * Names person id. If someone else already has that name, asks whether they
 * are one person and, if so, merges this one into them. Returns who to show
 * now (the other person after a merge), or null if the user said no.
 */
export async function namePerson(id: number, name: string): Promise<number | null> {
  name = name.trim()
  const other = name ? await engine.personByName(name) : 0
  if (other && other !== id) {
    if (!(await confirm(t('person.mergeTitle', { name }), t('person.mergeText'), t('person.merge')))) return null
    await engine.mergePeople(id, other)
    return other
  }
  await engine.namePerson(id, name)
  return id
}
