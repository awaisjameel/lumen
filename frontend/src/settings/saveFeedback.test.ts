import { expect, it } from 'vitest'
import { useSaveFeedback } from './saveFeedback'
import { t } from '../shared/i18n'

it('keeps saving visible until every pending row completes', async () => {
  const feedback = useSaveFeedback(() => '')
  let finish: (() => void) | undefined
  const first = feedback.save(
    'theme',
    () =>
      new Promise<void>((resolve) => {
        finish = resolve
      }),
  )
  await feedback.save('density', () => Promise.resolve())
  expect(feedback.savingAny.value).toBe(true)
  expect(feedback.hint('theme')).toBe(t('settings.saving'))
  finish?.()
  await first
  expect(feedback.savingAny.value).toBe(false)
  expect(feedback.hint('theme')).toBe(t('settings.saved'))
})

it('does not report a refused save as saved', async () => {
  const feedback = useSaveFeedback(() => 'Library is busy.')
  await feedback.save('theme', () => Promise.resolve())
  expect(feedback.hint('theme')).toBe('')
})
