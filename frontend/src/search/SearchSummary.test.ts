import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createTestNavigation } from '../shared/navigation/testing'
import { engine, Relation } from '../bridge'
import type * as Bridge from '../bridge'
import type { SearchInfo } from '../library/store'
import { registerCommands } from '../shared/commands'
import { mountInApp } from '../shared/testing'
import SearchSummary from './SearchSummary.vue'

vi.mock('../bridge', async (original) => ({
  ...(await original<typeof Bridge>()),
  engine: { people: vi.fn() },
}))

const router = createTestNavigation()
let wrapper: VueWrapper | undefined

const found = (info: Partial<SearchInfo>): SearchInfo => ({
  people: [],
  unassigned: [],
  description: '',
  pending: 0,
  complete: true,
  noAI: false,
  blocked: false,
  ...info,
})

async function show(info: SearchInfo, total = 6, path = '/search?q=Amina%20at%20the%20beach&media=photos') {
  await router.visit(path)
  wrapper = mountInApp(SearchSummary, { plugins: [router], props: { info, total, searching: false } })
  await flushPromises()
  return wrapper
}

beforeEach(() => {
  setActivePinia(createPinia())
  vi.mocked(engine.people).mockResolvedValue([])
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
})

it('takes a person or the description out of the search, keeping its refinements', async () => {
  const w = await show(
    found({ people: [{ text: 'Amina', people: [3], ambiguous: false }], description: 'at the beach' }),
  )
  const parts = w.findAll('[data-search-part]')
  expect(parts.map((p) => p.attributes('aria-label'))).toEqual([
    'Remove person Amina',
    'Remove description at the beach',
  ])
  await parts[0]?.trigger('click')
  await flushPromises()
  expect(Object.fromEntries(new URLSearchParams(router.fullPath.value.split('?')[1]))).toEqual({
    q: 'at the beach',
    media: 'photos',
  })
  await parts[1]?.trigger('click')
  await flushPromises()
  expect(Object.fromEntries(new URLSearchParams(router.fullPath.value.split('?')[1]))).toEqual({
    q: 'Amina',
    media: 'photos',
  })
})

it('says how many matched, and that more did when the results are cut short', async () => {
  const w = await show(found({ description: 'sea', complete: false }), 12)
  expect(w.findAll('[data-search-count] > *').map((part) => part.text())).toEqual([
    '12 matches, best first',
    'Showing the best 12. Add words to narrow it down.',
  ])
})

it('links photos still being understood to the library activity', async () => {
  const opened = vi.fn()
  const unregister = registerCommands([{ id: 'app.activity', title: () => 'Activity', run: opened }])
  const w = await show(found({ description: 'sea', pending: 1204 }))
  expect(w.get('[data-search-pending]').text()).toContain('Lumen is still understanding 1,204 photos')
  await w.get('[data-search-pending] button').trigger('click')
  await flushPromises()
  expect(opened).toHaveBeenCalledOnce()
  unregister()
})

it('says when only people were searched and when the search names something blocked', async () => {
  let w = await show(
    found({ people: [{ text: 'Amina', people: [3], ambiguous: false }], description: 'beach', noAI: true }),
  )
  expect(w.find('[data-search-no-ai]').exists()).toBe(true)
  expect(w.find('[data-search-blocked]').exists()).toBe(false)
  w.unmount()
  w = await show(found({ blocked: true }), 0)
  expect(w.find('[data-search-blocked]').exists()).toBe(true)
  expect(w.find('[data-search-count]').exists()).toBe(false)
})

const amina = (id: number, name: string, extra: Partial<Bridge.Person> = {}): Bridge.Person => ({
  id,
  name,
  count: 1,
  cover: {
    id: 0,
    assetId: 0,
    thumb: 0,
    assetW: 0,
    assetH: 0,
    video: false,
    frame: 0,
    x: 0,
    y: 0,
    w: 0,
    h: 0,
    person: id,
    name,
    byUser: false,
  },
  me: false,
  relation: Relation.RelationNone,
  relationLabel: '',
  ...extra,
})

it('says whom a relationship found, and offers each person when it found several', async () => {
  vi.mocked(engine.people).mockResolvedValue([
    amina(3, 'Amina', { relation: Relation.RelationMother }),
    amina(4, 'Sara', { relation: Relation.RelationFriend }),
    amina(5, '', { relation: Relation.RelationFriend }),
    amina(6, 'Omar', { relation: Relation.RelationFriend }),
  ])
  const w = await show(
    found({
      people: [
        { text: 'my mum', people: [3], ambiguous: false },
        { text: 'my friend', people: [4, 5, 6], ambiguous: true },
      ],
      description: 'at the beach',
    }),
    6,
    '/search?q=my%20mum%20my%20friend%20at%20the%20beach',
  )
  expect(w.findAll('[data-search-part="person"] .kind').map((k) => k.text())).toEqual(['Amina', 'Person'])
  const ambiguous = w.get('[data-search-ambiguous]')
  expect(ambiguous.text()).toContain('“my friend” could be Sara, Unnamed person and Omar')
  expect(ambiguous.text()).toContain('Name the people in People')
  // Only the named can be chosen by name; choosing one never drops the others' parts.
  expect(w.findAll('[data-search-only]').map((b) => b.text())).toEqual(['Only Sara', 'Only Omar'])
  await w.get('[data-search-only="6"]').trigger('click')
  await flushPromises()
  expect(new URLSearchParams(router.fullPath.value.split('?')[1]).get('q')).toBe('my mum Omar at the beach')
})

it('says which phrases name no one yet and leads to People', async () => {
  const w = await show(found({ unassigned: ['me', 'my mum'], description: 'me and my mum' }))
  const notice = w.get('[data-search-unassigned]')
  expect(notice.text()).toContain('No one is set as “me” and “my mum” yet')
  expect(w.find('[data-search-ambiguous]').exists()).toBe(false)
})
