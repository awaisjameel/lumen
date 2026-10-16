import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { engine } from '../bridge'
import type * as Bridge from '../bridge'
import { mountInApp } from '../shared/testing'
import PersonNameEditor from './PersonNameEditor.vue'
import { personFixture } from './testing'

vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  engine: { namePerson: vi.fn().mockResolvedValue(undefined), suggestPeople: vi.fn().mockResolvedValue([]) },
}))
let wrapper: ReturnType<typeof mountInApp> | undefined
beforeEach(() => {
  vi.mocked(engine.suggestPeople).mockReset().mockResolvedValue([])
})
afterEach(() => {
  wrapper?.unmount()
  vi.useRealTimers()
})
it('removes an existing name through the same editor and keeps an unnamed cluster from saving an empty name', async () => {
  wrapper = mountInApp(PersonNameEditor, { props: { person: personFixture() } })
  await wrapper.get('input').setValue('')
  expect(wrapper.findAll('button').some((b) => b.text() === 'Remove name')).toBe(true)
  await wrapper.get('form').trigger('submit')
  await flushPromises()
  expect(engine.namePerson).toHaveBeenCalledWith(1, '')
  wrapper.unmount()
  wrapper = mountInApp(PersonNameEditor, { props: { person: personFixture(2, '') } })
  await wrapper.get('form').trigger('submit')
  expect(engine.namePerson).toHaveBeenCalledTimes(1)
})
it('uses arrows and Enter for suggestions, and leaves composition keys to the input method', async () => {
  vi.useFakeTimers()
  vi.mocked(engine.suggestPeople).mockResolvedValue([personFixture(2, 'Sara'), personFixture(3, 'Zoya')])
  wrapper = mountInApp(PersonNameEditor, { props: { person: personFixture(1, '') } })
  const input = wrapper.get('input')
  await input.setValue('a')
  await vi.advanceTimersByTimeAsync(200)
  await input.trigger('keydown', { key: 'ArrowUp' })
  expect(wrapper.get('[aria-selected="true"]').text()).toBe('Zoya')
  await input.trigger('keydown', { key: 'Enter', isComposing: true })
  expect(engine.namePerson).not.toHaveBeenCalled()
  await input.trigger('keydown', { key: 'Enter' })
  await flushPromises()
  expect(engine.namePerson).toHaveBeenCalledWith(1, 'Zoya')
})
