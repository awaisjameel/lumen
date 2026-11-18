/** Pure navigation vocabulary, shared by registration, input boundaries and tests. */
export class InvalidNavigation extends Error {
  constructor() {
    super('Invalid application destination')
  }
}

type Values = Readonly<Record<string, unknown>>
function isValues(value: unknown): value is Values {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}
function fields(value: unknown): Values {
  if (!isValues(value)) throw new InvalidNavigation()
  return value
}
function only(value: Values, keys: readonly string[]) {
  if (Object.keys(value).some((key) => !keys.includes(key))) throw new InvalidNavigation()
}
function optionalText(value: unknown): string | undefined {
  if (value === undefined || value === null || value === '') return
  if (typeof value !== 'string') throw new InvalidNavigation()
  return value
}
export function positiveID(value: unknown): number {
  const number =
    typeof value === 'number' ? value : typeof value === 'string' && /^\d+$/.test(value) ? Number(value) : 0
  if (!Number.isSafeInteger(number) || number <= 0) throw new InvalidNavigation()
  return number
}
function optionalID(value: unknown) {
  return value === undefined || value === null || value === '' ? undefined : positiveID(value)
}
function choice<const T extends string>(value: unknown, values: readonly T[]): T | undefined {
  const text = optionalText(value)
  if (text === undefined) return
  const found = values.find((item) => item === text)
  if (found === undefined) throw new InvalidNavigation()
  return found
}
function day(value: unknown) {
  const text = optionalText(value)
  if (text === undefined) return
  if (!/^\d{4}-\d{2}-\d{2}$/.test(text)) throw new InvalidNavigation()
  const date = new Date(`${text}T00:00:00Z`)
  if (!Number.isFinite(date.getTime()) || date.toISOString().slice(0, 10) !== text) throw new InvalidNavigation()
  return text
}
export const settingsSections = [
  'appearance',
  'library',
  'viewing',
  'search',
  'indexing',
  'files',
  'accessibility',
  'privacy',
  'presets',
  'updates',
] as const
export type SettingsSection = (typeof settingsSections)[number]
/** Incoming compatibility only; callers construct canonical sections. */
export function settingsSection(value: unknown): SettingsSection | undefined {
  const text = optionalText(value)
  const canonical =
    text === 'video'
      ? 'viewing'
      : text === 'compression'
        ? 'files'
        : text === 'about'
          ? 'updates'
          : text === 'ai'
            ? 'search'
            : text
  return choice(canonical, settingsSections)
}
const noParams = (value: Values) => {
  only(value, [])
  return {}
}
const idParams = (value: Values) => {
  only(value, ['id'])
  return { id: positiveID(value.id) }
}
const noQuery = () => ({})
const mediaQuery = (value: Values) => ({ media: choice(value.media, ['photos', 'videos']) })
const searchQuery = (value: Values) => {
  const favourites = value.favourites === true ? true : choice(value.favourites, ['true']) === 'true' ? true : undefined
  const result = {
    q: optionalText(value.q)?.trim() || undefined,
    ...mediaQuery(value),
    favourites: favourites ? (true as const) : undefined,
    from: day(value.from),
    to: day(value.to),
    place: optionalID(value.place),
  }
  if (result.from && result.to && result.from > result.to) throw new InvalidNavigation()
  return result
}
const settingsQuery = (value: Values) => ({ section: settingsSection(value.section) })
const albumQuery = (value: Values) => ({ edit: optionalID(value.edit) })
const placesQuery = (value: Values) => ({ place: optionalID(value.place) })
const facesQuery = (value: Values) => ({ picture: choice(value.picture, ['1']) })

interface Metadata {
  view: string
  group?: string
  timeline?: 'all' | 'favourites' | 'videos' | 'trash' | 'search'
  development?: true
  start?: true
}
function route<const N extends string, P extends object, Q extends object, const M extends Metadata>(
  name: N,
  path: string,
  params: (value: Values) => P,
  query: (value: Values) => Q,
  meta: M,
) {
  return {
    name,
    path,
    meta,
    read(rawParams: unknown, rawQuery: unknown) {
      const parsedParams = params(fields(rawParams))
      const sourceQuery = fields(rawQuery)
      const parsedQuery = query(sourceQuery)
      only(sourceQuery, Object.keys(parsedQuery))
      return { name, params: parsedParams, query: parsedQuery, meta }
    },
  }
}

/** Every registered destination is declared here exactly once. Views bind at the root. */
export const routes = [
  route('library', '/', noParams, mediaQuery, { view: 'library', group: 'library', timeline: 'all', start: true }),
  route('favourites', '/favourites', noParams, mediaQuery, {
    view: 'library',
    group: 'favourites',
    timeline: 'favourites',
    start: true,
  }),
  route('videos', '/videos', noParams, mediaQuery, { view: 'library', timeline: 'videos', start: true }),
  route('trash', '/trash', noParams, mediaQuery, { view: 'library', group: 'trash', timeline: 'trash' }),
  route('people', '/people', noParams, noQuery, { view: 'people', group: 'people', start: true }),
  route('person', '/people/:id', idParams, mediaQuery, { view: 'library', group: 'people', timeline: 'all' }),
  route('personFaces', '/people/:id/faces', idParams, facesQuery, { view: 'personFaces', group: 'people' }),
  route('things', '/things', noParams, noQuery, { view: 'things', group: 'things', start: true }),
  route('thing', '/things/:id', idParams, mediaQuery, { view: 'library', group: 'things', timeline: 'all' }),
  route('albums', '/albums', noParams, albumQuery, { view: 'albums', group: 'albums', start: true }),
  route('album', '/albums/:id', idParams, mediaQuery, { view: 'library', group: 'albums', timeline: 'all' }),
  route('places', '/places', noParams, placesQuery, { view: 'places', group: 'places', start: true }),
  route('place', '/places/:id', idParams, mediaQuery, { view: 'library', group: 'places', timeline: 'all' }),
  route('events', '/events', noParams, noQuery, { view: 'events', group: 'events', start: true }),
  route('event', '/events/:id', idParams, mediaQuery, { view: 'library', group: 'events', timeline: 'all' }),
  route('duplicates', '/duplicates', noParams, noQuery, { view: 'duplicates', group: 'duplicates', start: true }),
  route('compression', '/compression', noParams, noQuery, { view: 'compression', group: 'compression' }),
  route('search', '/search', noParams, searchQuery, { view: 'library', timeline: 'search' }),
  route('settings', '/settings', noParams, settingsQuery, { view: 'settings', group: 'settings' }),
  route('blocked', '/settings/blocked', noParams, noQuery, { view: 'blocked', group: 'settings' }),
  route('organise', '/settings/organise', noParams, noQuery, { view: 'organise', group: 'settings' }),
  route('showcase', '/__showcase', noParams, noQuery, { view: 'showcase', development: true }),
] as const

export type Definition = (typeof routes)[number]
export type Destination = ReturnType<Definition['read']>
export type DestinationName = Destination['name']
export type ViewName = Definition['meta']['view']
export type StartDestinationName = Extract<Destination, { meta: { start: true } }>['name']
type Input<T> = T extends { name: infer N; params: infer P; query: infer Q }
  ? { name: N } & (keyof P extends never ? { params?: never } : { params: P }) &
      (keyof Q extends never ? { query?: never } : { query?: Partial<Q> })
  : never
export type DestinationInput = Input<Destination>
export type SearchQuery = Partial<Extract<Destination, { name: 'search' }>['query']>
type ExtraKeys<Actual, Expected> = Record<Exclude<keyof Actual, keyof Expected>, never>
type ExactInput<T extends DestinationInput> = T &
  ExtraKeys<T, { name: unknown; params: unknown; query: unknown }> &
  (T extends { params: infer P }
    ? { params: P & ExtraKeys<P, NonNullable<Extract<DestinationInput, { name: T['name'] }>['params']>> }
    : unknown) &
  (T extends { query: infer Q }
    ? { query: Q & ExtraKeys<Q, NonNullable<Extract<DestinationInput, { name: T['name'] }>['query']>> }
    : unknown)

/** Internal targets have no raw-string or partially named escape hatch. */
export function destination<const T extends DestinationInput>(
  input: ExactInput<T>,
): Extract<Destination, { name: T['name'] }>
export function destination(input: DestinationInput): Destination {
  const definition = routes.find((entry) => entry.name === input.name)
  if (!definition) throw new InvalidNavigation()
  return definition.read(input.params ?? {}, input.query ?? {})
}

export type Location = { kind: 'valid'; target: Destination } | { kind: 'invalid' } | { kind: 'pending' }
export function decode(name: unknown, params: unknown, query: unknown, development: boolean): Location {
  const definition = routes.find((entry) => entry.name === name)
  if (!definition || ('development' in definition.meta && !development)) return { kind: 'invalid' }
  try {
    return { kind: 'valid', target: definition.read(params, query) }
  } catch (error) {
    if (!(error instanceof InvalidNavigation)) throw error
    return { kind: 'invalid' }
  }
}

/** Static persisted choices must also be eligible for startup. */
export function startDestination(value: unknown): Destination | undefined {
  const definition = routes.find((entry) => entry.name === value && 'start' in entry.meta)
  return definition?.read({}, {})
}
