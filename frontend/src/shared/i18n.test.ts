import { expect, it } from 'vitest'
import { t } from './i18n'

it('fills placeholders, formatting numbers, and picks the plural form by n', () => {
  expect(t('blocked.saved', { name: 'Lahore' })).toBe('Blocked “Lahore”.')
  expect(t('status.items', { n: 1 })).not.toBe(t('status.items', { n: 1200 }))
  expect(t('status.items', { n: 1200 })).toContain('1,200')
})

it('shows a value as given, never as a replacement pattern', () => {
  expect(t('blocked.saved', { name: "$& $` $'" })).toBe("Blocked “$& $` $'”.")
})

it('checks placeholders at compile time', () => {
  // @ts-expect-error: the message has {name}
  t('blocked.saved')
  // @ts-expect-error: a misspelt placeholder is not the message's
  t('blocked.saved', { nam: 'Lahore' })
  expect(t('nav.people')).toBe('People') // a message without placeholders takes none
})

it('formats PIN guidance from the mirrored engine bounds and cooldown', () => {
  expect(t('blocked.createPINHint', { pinMin: 4, pinMax: 9 })).toContain('4–9 digit PIN')
  expect(t('error.pinRateLimited', { pinCooldown: 45 })).toContain('45 seconds')
})
