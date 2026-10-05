import { expect, it } from 'vitest'
import { OrganiseReason } from '../bridge'
import { reasonText } from './fileReasons'
import { t } from './i18n'

it('words every reason the engine names, and leaves Windows’s own message as it is', () => {
  for (const reason of Object.values(OrganiseReason)) {
    expect(reasonText(reason), reason).toBe(t(`organise.reason.${reason}`))
    expect(reasonText(reason), reason).not.toBe(reason)
  }
  expect(reasonText('Access is denied.')).toBe('Access is denied.')
  expect(reasonText('toString')).toBe('toString') // not a reason, though every object has it
  expect(reasonText()).toBe('')
})
