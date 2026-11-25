import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { engine, ErrorCode, MAX_RELATION_LABEL_LEN, Relation } from '../bridge'
import type * as Bridge from '../bridge'
import { mountInApp } from '../shared/testing'
import RelationshipEditor from './RelationshipEditor.vue'
import { peopleRouter, personFixture } from './testing'

vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  engine: { setRelation: vi.fn() },
}))

let wrapper: ReturnType<typeof mountInApp> | undefined
beforeEach(() => {
  setActivePinia(createPinia())
  vi.mocked(engine.setRelation)
    .mockReset()
    .mockImplementation((id, relation, label) =>
      Promise.resolve({
        ...personFixture(id, 'Jane'),
        relation,
        relationLabel: label,
      }),
    )
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
})

function mounted(person = personFixture(3, 'Jane')) {
  wrapper = mountInApp(RelationshipEditor, { props: { person }, plugins: [peopleRouter()] })
  return wrapper
}
/** What the editor itself emitted (mountInApp wraps it in the app). */
const emitted = (name: 'saved' | 'cancel') => wrapper?.findComponent(RelationshipEditor).emitted(name)

it('filters the relationships as the user types and saves the one picked', async () => {
  const w = mounted()
  const input = w.get('input')
  expect(input.attributes('role')).toBe('combobox')
  await input.setValue('sis')
  expect(w.findAll('[role="option"]').map((o) => o.text())).toEqual(['Sister', 'Use “sis”'])
  await w.get('[data-relation="sister"]').trigger('click')
  await flushPromises()
  expect(engine.setRelation).toHaveBeenCalledExactlyOnceWith(3, Relation.RelationSister, '')
  expect(emitted('saved')?.[0]?.[0]).toMatchObject({ id: 3, relation: Relation.RelationSister })
})

it('moves through the options with the arrow keys and saves the highlighted one with Enter', async () => {
  const w = mounted()
  const input = w.get('input')
  await input.setValue('grand')
  await input.trigger('keydown', { key: 'ArrowDown' })
  await input.trigger('keydown', { key: 'ArrowDown' })
  expect(input.attributes('aria-activedescendant')).toMatch(/-1$/)
  expect(w.findAll('[role="option"]')[1]?.attributes('aria-selected')).toBe('true')
  await input.trigger('keydown', { key: 'Enter' })
  await flushPromises()
  expect(engine.setRelation).toHaveBeenCalledWith(3, Relation.RelationGrandfather, '')
})

it('saves a typed word as the user’s own label, and a built-in name as that relationship', async () => {
  const w = mounted()
  const input = w.get('input')
  await input.setValue('  Football coach ')
  await w.get('form').trigger('submit')
  await flushPromises()
  expect(engine.setRelation).toHaveBeenLastCalledWith(3, Relation.RelationCustom, 'Football coach')
  await input.setValue('dad')
  await w.get('form').trigger('submit')
  await flushPromises()
  expect(engine.setRelation).toHaveBeenLastCalledWith(3, Relation.RelationFather, '')
})

it('refuses a label that is too long before asking the engine, and shows the engine’s refusal', async () => {
  const w = mounted()
  const input = w.get('input')
  await input.setValue('x'.repeat(MAX_RELATION_LABEL_LEN + 1))
  expect(w.get('[role="alert"]').text()).toContain(`1 to ${MAX_RELATION_LABEL_LEN} characters`)
  await w.get('form').trigger('submit')
  expect(engine.setRelation).not.toHaveBeenCalled()
  vi.mocked(engine.setRelation).mockRejectedValueOnce(
    new Error('invalid', { cause: { code: ErrorCode.InvalidRelation } }),
  )
  await input.setValue('family')
  await w.get('form').trigger('submit')
  await flushPromises()
  expect(w.get('[role="alert"]').text()).toContain('other than “me” or “family”')
  expect(emitted('saved')).toBeUndefined()
})

it('removes a relationship, keeps an unchanged one without a call, and cancels with Escape', async () => {
  const w = mounted({ ...personFixture(3, 'Jane'), relation: Relation.RelationMother })
  expect(w.get('input').element.value).toBe('Mum')
  await w.get('form').trigger('submit')
  expect(engine.setRelation).not.toHaveBeenCalled()
  expect(emitted('cancel')).toHaveLength(1)
  await w.get('[data-action="remove-relation"]').trigger('click')
  await flushPromises()
  expect(engine.setRelation).toHaveBeenCalledWith(3, Relation.RelationNone, '')
  await w.get('input').trigger('keydown', { key: 'Escape' })
  expect(emitted('cancel')).toHaveLength(2)
})
