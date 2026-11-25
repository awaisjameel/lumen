// What people are to the user: which person is Me, and the relationship
// each other person has to them (core.Relation). The engine is the
// authority: a change it would have to make silently (replacing Me, losing
// a relationship in a merge) comes back as relation_conflict, and these
// helpers explain it and ask before trying again with the user's answer.
import { ref } from 'vue'
import { engine, ErrorCode, errorCode, MergeKeep, Relation, RELATIONS } from '../bridge'
import type { Person } from '../bridge'
import { choose, confirm } from '../shared/confirm'
import { t } from '../shared/i18n'
import type { MessageKey } from '../shared/i18n'
import { nameIncludes, nameStarts, sameName, trimName } from '../shared/names'
import { usePeople } from './store'

/**
 * Whether the user chose Not now on People's "Which one is you?": it stays
 * away until Lumen restarts, and This is me on any person still sets it.
 */
export const meSetupSkipped = ref(false)
export function skipMeSetup(): void {
  meSetupSkipped.value = true
}

/** A relationship Lumen offers, as opposed to none or the user's own word. */
export type BuiltInRelation = Exclude<Relation, Relation.RelationNone | Relation.RelationCustom>

const relationKeys: Record<BuiltInRelation, MessageKey> = {
  [Relation.RelationPartner]: 'relation.partner',
  [Relation.RelationMother]: 'relation.mother',
  [Relation.RelationFather]: 'relation.father',
  [Relation.RelationParent]: 'relation.parent',
  [Relation.RelationDaughter]: 'relation.daughter',
  [Relation.RelationSon]: 'relation.son',
  [Relation.RelationChild]: 'relation.child',
  [Relation.RelationSister]: 'relation.sister',
  [Relation.RelationBrother]: 'relation.brother',
  [Relation.RelationSibling]: 'relation.sibling',
  [Relation.RelationGrandmother]: 'relation.grandmother',
  [Relation.RelationGrandfather]: 'relation.grandfather',
  [Relation.RelationGrandparent]: 'relation.grandparent',
  [Relation.RelationGrandchild]: 'relation.grandchild',
  [Relation.RelationAunt]: 'relation.aunt',
  [Relation.RelationUncle]: 'relation.uncle',
  [Relation.RelationCousin]: 'relation.cousin',
  [Relation.RelationNiece]: 'relation.niece',
  [Relation.RelationNephew]: 'relation.nephew',
  [Relation.RelationRelative]: 'relation.relative',
  [Relation.RelationFriend]: 'relation.friend',
  [Relation.RelationColleague]: 'relation.colleague',
  [Relation.RelationNeighbour]: 'relation.neighbour',
}

function builtIn(r: Relation): r is BuiltInRelation {
  return r !== Relation.RelationNone && r !== Relation.RelationCustom
}

/** A built-in relationship in the UI's words. */
export function relationName(r: BuiltInRelation): string {
  return t(relationKeys[r])
}

/** What a person is to the user, in words: "Mum", their own label, or "". */
export function relationText(p: Pick<Person, 'relation' | 'relationLabel'>): string {
  if (p.relation === Relation.RelationCustom) return p.relationLabel
  return builtIn(p.relation) ? relationName(p.relation) : ''
}

/** What a person is shown as beside their name: Me, their relationship, or "". */
export function roleText(p: Pick<Person, 'me' | 'relation' | 'relationLabel'>): string {
  return p.me ? t('person.me') : relationText(p)
}

/** A person's name, or "Unnamed person". */
export function personName(p: Pick<Person, 'name'>): string {
  return p.name || t('person.unnamed')
}

/** A relationship a picker offers: built in, or the user's own word. */
export interface RelationOption {
  relation: Relation
  label: string // the custom word; "" for a built-in one
  text: string // what the option says
}

/**
 * The built-in relationships matching what the user typed (all of them for
 * nothing), those starting with it first, then the typed word as their own
 * label unless a built-in one has exactly that name.
 */
export function relationOptions(typed: string): RelationOption[] {
  // Spaced as the engine keeps a label (core.CleanRelation).
  const label = trimName(typed).replace(/\p{White_Space}+/gu, ' ')
  const all = RELATIONS.filter(builtIn).map((relation) => ({ relation, label: '', text: relationName(relation) }))
  if (!label) return all
  const matching = all.filter((o) => nameIncludes(o.text, label))
  matching.sort((a, b) => Number(nameStarts(b.text, label)) - Number(nameStarts(a.text, label)))
  if (matching.some((o) => sameName(o.text, label))) return matching
  return [...matching, { relation: Relation.RelationCustom, label, text: t('person.relationCustom', { label }) }]
}

/**
 * Marks person as the user. When someone else is Me, or the person has a
 * relationship Me can't keep, it says so and asks first. Resolves whether
 * they are Me now; failures other than the question are thrown.
 */
export async function setMe(person: Person): Promise<boolean> {
  try {
    await engine.setMe(person.id, false)
    return true
  } catch (e) {
    if (errorCode(e) !== ErrorCode.RelationConflict) throw e
  }
  // Ask from what is true now, not from what this view last read.
  const people = usePeople()
  await people.load()
  const self = people.byId.get(person.id) ?? person
  const current = people.list.find((p) => p.me && p.id !== person.id)
  const lines = [
    current ? t('person.meReplace', { name: personName(current) }) : '',
    !current && !relationText(self) ? t('person.meReplaceHidden') : '',
    relationText(self) ? t('person.meDropsRelation', { relation: relationText(self) }) : '',
  ].filter(Boolean)
  if (!(await confirm(t('person.meTitle', { name: personName(self) }), lines.join('\n'), t('person.meConfirm'))))
    return false
  await engine.setMe(person.id, true)
  return true
}

/**
 * Merges person from into person into. If the two disagree about a
 * relationship (or one is Me and the other has one) it asks which to keep.
 * Resolves whether they were merged; false if the user declined.
 */
export async function mergePeople(from: number, into: number): Promise<boolean> {
  try {
    await engine.mergePeople(from, into)
    return true
  } catch (e) {
    if (errorCode(e) !== ErrorCode.RelationConflict) throw e
  }
  const [gone, kept] = await Promise.all([engine.person(from), engine.person(into)])
  const keep = await askKeep(gone, kept)
  if (keep === null) return false
  await engine.mergePeople(from, into, keep)
  return true
}

async function askKeep(from: Person, into: Person): Promise<MergeKeep | null> {
  const me = from.me ? from : into.me ? into : null
  if (me) {
    const other = me === from ? into : from
    const relation = relationText(other)
    const yes = await confirm(t('person.mergeMeTitle'), t('person.mergeMeText', { relation }), t('person.merge'))
    return yes ? MergeKeep.MergeKeepInto : null
  }
  const choice = await choose(
    t('person.mergeRelationTitle'),
    t('person.mergeRelationText', { from: relationText(from), into: relationText(into) }),
    t('person.keepRelation', { relation: relationText(into) }),
    t('person.keepRelation', { relation: relationText(from) }),
  )
  return choice === 'ok' ? MergeKeep.MergeKeepInto : choice === 'other' ? MergeKeep.MergeKeepFrom : null
}
