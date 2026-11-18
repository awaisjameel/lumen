import { describe, expect, it } from 'vitest'
import { decode, destination, positiveID, routes, startDestination } from './contract'
import type { DestinationInput, DestinationName } from './contract'
import { createTestNavigation } from './testing'

// Independent expected URLs make the catch-all and an accidental route rename visible.
const examples = {
  library: { input: { name: 'library' }, href: '/' },
  favourites: { input: { name: 'favourites' }, href: '/favourites' },
  videos: { input: { name: 'videos' }, href: '/videos' },
  trash: { input: { name: 'trash' }, href: '/trash' },
  people: { input: { name: 'people' }, href: '/people' },
  person: { input: { name: 'person', params: { id: 7 } }, href: '/people/7' },
  personFaces: {
    input: { name: 'personFaces', params: { id: 7 }, query: { picture: '1' } },
    href: '/people/7/faces?picture=1',
  },
  things: { input: { name: 'things' }, href: '/things' },
  thing: { input: { name: 'thing', params: { id: 8 } }, href: '/things/8' },
  albums: { input: { name: 'albums', query: { edit: 9 } }, href: '/albums?edit=9' },
  album: { input: { name: 'album', params: { id: 9 } }, href: '/albums/9' },
  places: { input: { name: 'places', query: { place: 10 } }, href: '/places?place=10' },
  place: { input: { name: 'place', params: { id: 10 } }, href: '/places/10' },
  events: { input: { name: 'events' }, href: '/events' },
  event: { input: { name: 'event', params: { id: 11 } }, href: '/events/11' },
  duplicates: { input: { name: 'duplicates' }, href: '/duplicates' },
  compression: { input: { name: 'compression' }, href: '/compression' },
  search: {
    input: {
      name: 'search',
      query: { q: '海辺 کی شام', favourites: true, media: 'photos', from: '2026-01-01', to: '2026-02-28', place: 10 },
    },
    href: '/search?q=%E6%B5%B7%E8%BE%BA+%DA%A9%DB%8C+%D8%B4%D8%A7%D9%85&media=photos&favourites=true&from=2026-01-01&to=2026-02-28&place=10',
  },
  settings: { input: { name: 'settings', query: { section: 'privacy' } }, href: '/settings?section=privacy' },
  blocked: { input: { name: 'blocked' }, href: '/settings/blocked' },
  organise: { input: { name: 'organise' }, href: '/settings/organise' },
  showcase: { input: { name: 'showcase' }, href: '/__showcase' },
} satisfies Record<DestinationName, { input: DestinationInput; href: string }>

it('registers unique identities and paths with complete independent examples', () => {
  expect(new Set(routes.map((route) => route.name)).size).toBe(routes.length)
  expect(new Set(routes.map((route) => route.path)).size).toBe(routes.length)
  expect(Object.keys(examples).sort()).toEqual(routes.map((route) => route.name).sort())
  for (const route of routes) {
    const target = destination(examples[route.name].input)
    expect(target.name).toBe(route.name)
    expect(Object.keys(target.params)).toEqual(route.path.includes(':id') ? ['id'] : [])
    if ('group' in target.meta) {
      const group = target.meta.group
      expect(routes.some((entry) => entry.name === group)).toBe(true)
    }
  }
})

describe('exact production resolution', () => {
  for (const [name, example] of Object.entries(examples))
    it(name, async () => {
      const navigation = createTestNavigation({}, true)
      const target = destination(example.input)
      const resolved = navigation.resolve(target)
      expect(resolved.href).toBe(example.href)
      expect(resolved.matched).toEqual([name])
      await navigation.push(target)
      expect(navigation.location.value).toEqual({ kind: 'valid', target })
    })
})

it.each([
  0,
  -1,
  1.5,
  NaN,
  Infinity,
  Number.MAX_SAFE_INTEGER + 1,
  '',
  '0',
  '-2',
  '1.5',
  '1e3',
  '9007199254740992',
  ['1'],
  undefined,
])('refuses unsafe identifier %s', (value) => {
  expect(() => positiveID(value)).toThrow('Invalid application destination')
})
it('retains positive safe IDs and normalises leading zeros', () => {
  expect(positiveID(Number.MAX_SAFE_INTEGER)).toBe(Number.MAX_SAFE_INTEGER)
  expect(positiveID('0007')).toBe(7)
})
it.each([
  '/people/0',
  '/people/-1',
  '/people/1.5',
  '/people/9007199254740992',
  '/people/nope',
  '/people/0/faces',
  '/things/0',
  '/albums/0',
  '/places/0',
  '/events/0',
  '/search?place=0',
  '/albums?edit=0',
  '/places?place=-1',
  '/search?q=one&q=two',
  '/search?media=audio',
  '/search?from=2026-02-30',
  '/search?from=2026-03-01&to=2026-02-01',
  '/settings?section=unknown',
  '/settings?section=privacy&section=updates',
  '/people?edit=1',
  '/people/7?picture=1',
  '/?unexpected=1',
  '/#private',
])('keeps invalid input unavailable instead of selecting Library: %s', async (incoming) => {
  const navigation = createTestNavigation()
  await navigation.visit(incoming)
  expect(navigation.location.value).toEqual({ kind: 'invalid' })
  expect(navigation.fullPath.value).toBe(incoming)
})
it.each([
  ['video', 'viewing'],
  ['compression', 'files'],
  ['about', 'updates'],
  ['ai', 'search'],
])('normalises the incoming %s settings alias', async (alias, section) => {
  const navigation = createTestNavigation()
  await navigation.visit(`/settings?section=${alias}`)
  expect(navigation.fullPath.value).toBe(`/settings?section=${section}`)
})
it('drops empty optional values, trims text once and retains escaping', async () => {
  const navigation = createTestNavigation()
  await navigation.visit('/search?q=%20a%2Fb%26c%20&media=&from=&place=')
  expect(navigation.fullPath.value).toBe('/search?q=a/b%26c')
  expect(navigation.location.value).toMatchObject({ kind: 'valid', target: { name: 'search', query: { q: 'a/b&c' } } })
})
it('retains the deliberate unknown-location fallback and production showcase exclusion', async () => {
  const navigation = createTestNavigation()
  await navigation.visit('/unknown-location')
  expect(navigation.fullPath.value).toBe('/')
  await navigation.visit('/__showcase')
  expect(navigation.fullPath.value).toBe('/')
  expect(decode('showcase', {}, {}, false)).toEqual({ kind: 'invalid' })
})
it('checks saved startup eligibility without admitting collection details', () => {
  expect(startDestination('people')?.name).toBe('people')
  expect(startDestination('videos')?.name).toBe('videos')
  expect(startDestination('person')).toBeUndefined()
  expect(startDestination('missing')).toBeUndefined()
})
