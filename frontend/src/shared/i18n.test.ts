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

// A message no screen shows is a drift waiting to happen: text that reads
// right but is never seen, and is not kept in step with what is.
it('shows every message somewhere, by its name or as part of a name built from a value', () => {
  const sources = import.meta.glob<string>(['../**/*.{ts,vue}', '!../**/*.test.ts', '!./i18n.ts'], {
    query: '?raw',
    import: 'default',
    eager: true,
  })
  const code = Object.values(sources).join('\n')
  const own = import.meta.glob<string>('./i18n.ts', { query: '?raw', import: 'default', eager: true })
  const keys = [...(Object.values(own)[0] ?? '').matchAll(/^ {2}'([\w.-]+)':/gm)].map((m) => m[1] ?? '')
  // Names built from a value: t(`compression.mode.${value}`) shows every compression.mode.* message.
  const built = [...code.matchAll(/`([\w.]+)\.\$\{/g)].map((m) => `${m[1] ?? ''}.`)
  const unshown = keys.filter(
    (key) => !code.includes(`'${key}'`) && !code.includes(`"${key}"`) && !built.some((p) => key.startsWith(p)),
  )
  expect(keys.length).toBeGreaterThan(1000)
  expect(unshown).toEqual([])
})
