import { afterEach, expect, it } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import { confirm, question } from './confirm'
import ConfirmDialog from './ConfirmDialog.vue'
import { mountInApp } from './testing'

let wrapper: VueWrapper | undefined

afterEach(() => {
  question.value?.answer(false)
  wrapper?.unmount()
  wrapper = undefined
})

function button(which: 'cancel' | 'ok'): HTMLButtonElement {
  const el = document.body.querySelector<HTMLButtonElement>(`[data-confirm="${which}"]`)
  if (!el) throw new Error(`the confirmation has no ${which} button`)
  return el
}

it('asks the question in a modal dialog and resolves true when confirmed', async () => {
  wrapper = mountInApp(ConfirmDialog)
  const answer = confirm('Delete 3 files?', 'They go to the Recycle Bin.', 'Delete')
  await flushPromises()
  const dialog = document.body.querySelector('[role="dialog"]')
  expect(dialog?.textContent).toContain('Delete 3 files?')
  expect(dialog?.textContent).toContain('They go to the Recycle Bin.')
  expect(button('ok').textContent.trim()).toBe('Delete')
  button('ok').click()
  expect(await answer).toBe(true)
  await flushPromises()
  expect(question.value).toBeNull()
})

it('resolves false when cancelled, and when a newer question replaces it', async () => {
  wrapper = mountInApp(ConfirmDialog)
  const first = confirm('First?', 'One.', 'Yes')
  const second = confirm('Second?', 'Two.', 'Yes')
  expect(await first).toBe(false)
  await flushPromises()
  expect(document.body.querySelector('[role="dialog"]')?.textContent).toContain('Second?')
  button('cancel').click()
  expect(await second).toBe(false)
})
