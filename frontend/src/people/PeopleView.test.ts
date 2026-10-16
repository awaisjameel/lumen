import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises } from '@vue/test-utils'
import { engine, ErrorCode } from '../bridge'
import type * as Bridge from '../bridge'
import type { Person } from '../bridge'
import { blockRequest } from '../shared/block'
import { runCommand } from '../shared/commands'
import { mountInApp, recordNotifications } from '../shared/testing'
import { usePeople } from './store'
import PeopleView from './PeopleView.vue'
import { peopleRouter, personFixture } from './testing'

vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  onLibraryChanged: vi.fn(() => () => undefined),
  engine: {
    people: vi.fn(),
    namePerson: vi.fn(),
    suggestPeople: vi.fn(),
    personByName: vi.fn(),
    mergePeople: vi.fn(),
    prepareSearch: vi.fn(),
  },
}))
let wrapper: ReturnType<typeof mountInApp> | undefined
let notices: ReturnType<typeof recordNotifications>
beforeEach(() => {
  setActivePinia(createPinia())
  notices = recordNotifications()
  vi.mocked(engine.people)
    .mockReset()
    .mockResolvedValue([personFixture(1, 'Ali', 1284), personFixture(2, '', 56)])
  vi.mocked(engine.suggestPeople).mockReset().mockResolvedValue([])
  vi.mocked(engine.namePerson).mockReset().mockResolvedValue(undefined)
})
afterEach(() => {
  wrapper?.unmount()
  notices.stop()
  blockRequest.value?.answer(false)
  blockRequest.value = null
  vi.useRealTimers()
})
async function mounted() {
  wrapper = mountInApp(PeopleView, { plugins: [peopleRouter()] })
  await flushPromises()
  return wrapper
}
it('shows live counts, separate groups and the filter in the top bar without preparing AI search', async () => {
  const w = await mounted()
  expect(w.get('[aria-live="polite"]').text()).toBe('2 people')
  expect(w.get('[aria-label="Named"]').text()).toContain('1,284 photos')
  expect(w.get('[aria-label="Who are these?"]').text()).toContain('56 photos')
  expect(w.get('[aria-label="Filter people by name"]').element.closest('header')).not.toBeNull()
  await w.get('input').trigger('focus')
  expect(engine.prepareSearch).not.toHaveBeenCalled()
})
it('retains the inline name and validation on failure and offers Undo after a successful retry', async () => {
  const w = await mounted()
  await w.get('[data-person="2"] [data-action="name"]').trigger('click')
  await flushPromises()
  const input = w.get('[aria-label="Name"]')
  await input.setValue('😀'.repeat(81))
  await input.trigger('keydown', { key: 'Enter' })
  expect(w.get('[role="alert"]').text()).toContain('80')
  expect(engine.namePerson).not.toHaveBeenCalled()
  vi.mocked(engine.namePerson).mockRejectedValueOnce(new Error('library busy'))
  await input.setValue('Sara')
  await input.trigger('keydown', { key: 'Enter' })
  await flushPromises()
  expect(w.get<HTMLInputElement>('[aria-label="Name"]').element.value).toBe('Sara')
  expect(w.get('[role="alert"]').text()).toContain('library busy')
  await input.trigger('keydown', { key: 'Enter' })
  await flushPromises()
  expect(engine.namePerson).toHaveBeenCalledWith(2, 'Sara')
  notices.find('person.2.name')?.actions?.[0]?.run()
  await flushPromises()
  expect(engine.namePerson).toHaveBeenLastCalledWith(2, '')
})
it('uses the inline merge prompt and preserves the draft if putting photos together fails', async () => {
  const w = await mounted()
  vi.mocked(engine.namePerson).mockRejectedValue(new Error('taken', { cause: { code: ErrorCode.PersonExists } }))
  vi.mocked(engine.personByName).mockResolvedValue(1)
  vi.mocked(engine.mergePeople).mockRejectedValue(new Error('library busy'))
  await w.get('[data-action="name"]').trigger('click')
  await w.get('[aria-label="Name"]').setValue('Ali')
  await w.get('[aria-label="Name"]').trigger('keydown', { key: 'Enter' })
  await flushPromises()
  expect(engine.mergePeople).not.toHaveBeenCalled()
  const same = w.findAll('button').find((button) => button.text() === 'Same person')
  if (!same) throw new Error('The inline merge choice is missing')
  await same.trigger('click')
  await flushPromises()
  expect(engine.mergePeople).toHaveBeenCalledWith(2, 1)
  expect(w.get<HTMLInputElement>('[aria-label="Name"]').element.value).toBe('Ali')
  expect(w.get('[role="alert"]').text()).toContain('library busy')
})
it('uses the protected label-only owner for hiding and provides the recovery destination', async () => {
  const w = await mounted()
  void runCommand('person.1.hide')
  expect(blockRequest.value?.targetID).toBe(1)
  expect(blockRequest.value?.photosAllowed).toBe(false)
  expect(w.get('a[href="/settings/blocked"]').text()).toBe('Show hidden people')
})
it('pages a large list of unnamed people rather than mounting every portrait', async () => {
  const many: Person[] = Array.from({ length: 10000 }, (_, i) => personFixture(i + 1, '', 3))
  vi.mocked(engine.people).mockResolvedValue(many)
  const w = await mounted()
  expect(w.findAll('[data-person]')).toHaveLength(6)
  const more = w.findAll('button').find((button) => button.text() === 'Show 24 more')
  if (!more) throw new Error('The next page control is missing')
  await more.trigger('click')
  expect(w.findAll('[data-person]')).toHaveLength(30)
})
it('recovers from a local read failure and shows the first-use next action', async () => {
  vi.mocked(engine.people).mockRejectedValueOnce(new Error('disk unavailable')).mockResolvedValue([])
  const w = await mounted()
  expect(w.get('[role="alert"]').text()).toContain('disk unavailable')
  const retry = w.findAll('button').find((button) => button.text() === 'Try again')
  if (!retry) throw new Error('The retry control is missing')
  await retry.trigger('click')
  await flushPromises()
  expect(w.text()).toContain('No people yet')
  expect(w.get('a[href="/settings?section=library"]').text()).toBe('Add a photo folder…')
  expect(usePeople().error).toBe('')
})

it('uses engine name matches and offers recovery when the filter finds nobody', async () => {
  vi.useFakeTimers()
  const w = await mounted()
  await w.get('[aria-label="Filter people by name"]').setValue('Nobody')
  await vi.advanceTimersByTimeAsync(200)
  await flushPromises()
  expect(engine.suggestPeople).toHaveBeenCalledWith('Nobody')
  expect(w.text()).toContain('No people match “Nobody”')
  const clear = w.findAll('button').find((button) => button.text() === 'Clear filter')
  if (!clear) throw new Error('The clear filter control is missing')
  await clear.trigger('click')
  expect(w.findAll('[data-person]')).toHaveLength(2)
})
