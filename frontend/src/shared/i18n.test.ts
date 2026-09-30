import { expect, it } from 'vitest'
import { t } from './i18n'

it('fills placeholders, formatting numbers, and picks the plural form by n', () => {
  expect(t('blocked.saved', { name: 'Lahore' })).toBe('Blocked “Lahore”.')
  expect(t('status.items', { n: 1 })).not.toBe(t('status.items', { n: 1200 }))
  expect(t('status.items', { n: 1200 })).toContain('1,200')
})

it('checks placeholders at compile time', () => {
  // @ts-expect-error: the message has {name}
  t('blocked.saved')
  // @ts-expect-error: a misspelt placeholder is not the message's
  t('blocked.saved', { nam: 'Lahore' })
  expect(t('nav.people')).toBe('People') // a message without placeholders takes none
})
