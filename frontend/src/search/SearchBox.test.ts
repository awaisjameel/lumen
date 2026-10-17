import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import type { VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { engine, TagKind } from '../bridge'
import type * as Bridge from '../bridge'
import { mountInApp } from '../shared/testing'
import { setShortcutOverrides } from '../shared/commands'
import SearchBox from './SearchBox.vue'

const scope = vi.hoisted(() => vi.fn().mockResolvedValue(undefined))
vi.mock('../library/store', () => ({ useLibrary: () => ({ setScope: scope }) }))
vi.mock('../bridge', async (original) => ({
  ...(await original<typeof Bridge>()),
  engine: {
    suggestPeople: vi.fn(),
    suggestTags: vi.fn(),
    prepareSearch: vi.fn(),
    recentSearches: vi.fn(),
    rememberSearch: vi.fn(),
    clearRecentSearches: vi.fn(),
  },
}))
let wrapper: VueWrapper | undefined
const router = createRouter({
  history: createMemoryHistory(),
  routes: [
    { path: '/', name: 'library', component: { template: '<div />' } },
    { path: '/search', name: 'search', component: { template: '<div />' } },
  ],
})
async function start(path = '/') {
  await router.push(path)
  wrapper = mountInApp(SearchBox, { plugins: [router] })
  const input = wrapper.get<HTMLInputElement>('input[role="combobox"]')
  await input.trigger('focus')
  return input
}
const settle = async () => {
  await new Promise((resolve) => setTimeout(resolve, 150))
  await flushPromises()
}
beforeEach(() => {
  setActivePinia(createPinia())
  vi.mocked(engine.recentSearches).mockResolvedValue([])
  vi.mocked(engine.rememberSearch).mockResolvedValue([])
  vi.mocked(engine.clearRecentSearches).mockResolvedValue(undefined)
  vi.mocked(engine.suggestPeople).mockResolvedValue([])
  vi.mocked(engine.suggestTags).mockResolvedValue([])
  vi.mocked(engine.prepareSearch).mockResolvedValue(undefined)
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  setShortcutOverrides({})
})

it('shows the actual focus shortcut while empty and rejects blank or composing Enter', async () => {
  setShortcutOverrides({ 'search.focus': 'Ctrl+G' })
  const input = await start()
  expect(wrapper?.findAll('kbd').map((cap) => cap.text())).toEqual(['Ctrl G'])
  await input.setValue('  ')
  await input.trigger('keydown', { key: 'Enter' })
  expect(router.currentRoute.value.name).toBe('library')
  await input.setValue('海辺 کی شام')
  await input.trigger('keydown', { key: 'Enter', isComposing: true })
  expect(router.currentRoute.value.name).toBe('library')
  await input.trigger('keydown', { key: 'Enter' })
  await flushPromises()
  expect(router.currentRoute.value.query.q).toBe('海辺 کی شام')
})

it('offers a description first and searches the chosen Thing instead of opening its collection', async () => {
  vi.mocked(engine.suggestTags).mockResolvedValue([
    { id: 1, name: 'Beach', count: 8, cover: 0, coverThumb: 0, kind: TagKind.TagBuiltin, pinned: false, original: '' },
  ])
  const input = await start()
  await input.setValue('bea')
  await settle()
  expect(wrapper?.findAll('[role="option"]').map((row) => row.text())).toEqual(['Search for “bea”Enter', 'Beach'])
  await input.trigger('keydown', { key: 'ArrowDown' })
  const active = input.attributes('aria-activedescendant')
  expect(document.getElementById(active ?? '')?.textContent).toContain('Beach')
  await input.trigger('keydown', { key: 'Enter' })
  await flushPromises()
  expect(router.currentRoute.value).toMatchObject({ name: 'search', query: { q: 'Beach' } })
  expect(input.attributes('aria-expanded')).toBe('false')
})

it('keeps the first search available when suggestions fail and retries the saved text', async () => {
  vi.mocked(engine.suggestTags).mockRejectedValueOnce(new Error('private engine detail'))
  const input = await start()
  await input.setValue('cake')
  await settle()
  expect(wrapper?.get('[role="alert"]').text()).toContain('Your words are saved')
  expect(wrapper?.text()).not.toContain('private engine detail')
  await wrapper
    ?.findAll('button')
    .find((button) => button.text() === 'Try again')
    ?.trigger('click')
  await settle()
  expect(engine.suggestTags).toHaveBeenCalledTimes(2)
  expect(wrapper?.find('[role="alert"]').exists()).toBe(false)
  expect(wrapper?.text()).toContain('No matching people or Things')
  await input.trigger('keydown', { key: 'Enter' })
  await flushPromises()
  expect(router.currentRoute.value.query.q).toBe('cake')
})

it('shows preparation feedback and retries preparation after failure', async () => {
  let reject: (error: Error) => void = () => undefined
  vi.mocked(engine.prepareSearch).mockReturnValueOnce(
    new Promise((_, fail) => {
      reject = fail
    }),
  )
  await start()
  expect(wrapper?.text()).toContain('Preparing on-device search')
  reject(new Error('not ready'))
  await flushPromises()
  expect(wrapper?.text()).toContain('could not be prepared')
  await wrapper
    ?.findAll('button')
    .find((button) => button.text() === 'Try again')
    ?.trigger('click')
  await flushPromises()
  expect(engine.prepareSearch).toHaveBeenCalledTimes(2)
  expect(wrapper?.text()).not.toContain('could not be prepared')
})

it('dismisses suggestions with Escape and an outside pointer while retaining the words', async () => {
  const input = await start()
  await input.setValue('party')
  await input.trigger('keydown', { key: 'Escape' })
  expect(input.attributes('aria-expanded')).toBe('false')
  expect(input.element.value).toBe('party')
  await input.trigger('focus')
  document.body.dispatchEvent(new Event('pointerdown', { bubbles: true }))
  await flushPromises()
  expect(input.attributes('aria-expanded')).toBe('false')
})

it('repeats the same search and clears results while restoring input focus', async () => {
  const input = await start('/search?q=cake')
  await input.trigger('keydown', { key: 'Enter' })
  expect(scope).toHaveBeenCalledWith({ kind: 'search', query: 'cake' })
  await wrapper?.get('[aria-label="Clear search"]').trigger('click')
  await flushPromises()
  expect(input.element.value).toBe('')
  expect(document.activeElement).toBe(input.element)
  expect(router.currentRoute.value.name).toBe('library')
})

it('searches a picked person and ignores a hidden active suggestion after Escape', async () => {
  vi.mocked(engine.suggestPeople).mockResolvedValue([
    {
      id: 4,
      name: 'Amina Khan',
      count: 32,
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
        person: 4,
        name: 'Amina Khan',
        byUser: false,
      },
    },
  ])
  const input = await start()
  await input.setValue('amina')
  await settle()
  const person = wrapper?.findAll('[role="option"]').find((option) => option.text().includes('Amina Khan'))
  expect(person?.text()).toContain('32')
  await person?.trigger('click')
  await flushPromises()
  expect(router.currentRoute.value.query.q).toBe('Amina Khan')
  await input.trigger('focus')
  await input.setValue('amina at the beach')
  await settle()
  await input.trigger('keydown', { key: 'ArrowDown' })
  await input.trigger('keydown', { key: 'Escape' })
  await input.trigger('keydown', { key: 'Enter' })
  await flushPromises()
  expect(router.currentRoute.value.query.q).toBe('amina at the beach')
})

it('offers the recent searches in an empty box and runs one when it is chosen', async () => {
  vi.mocked(engine.recentSearches).mockResolvedValue(['beach sunset', 'birthday cake'])
  vi.mocked(engine.rememberSearch).mockResolvedValue(['birthday cake', 'beach sunset'])
  const input = await start()
  await flushPromises()
  const options = () => wrapper?.findAll('li[role="option"]').map((o) => o.text()) ?? []
  expect(wrapper?.text()).toContain('Recent searches')
  expect(options()).toEqual(['beach sunset', 'birthday cake'])
  await input.trigger('keydown', { key: 'ArrowDown' })
  await input.trigger('keydown', { key: 'Enter' })
  await flushPromises()
  expect(router.currentRoute.value.query.q).toBe('birthday cake')
  expect(engine.rememberSearch).toHaveBeenCalledWith('birthday cake')
})

it('offers the recent searches that hold what is typed after the suggestions', async () => {
  vi.mocked(engine.recentSearches).mockResolvedValue(['beach sunset', 'birthday cake', 'beach'])
  const input = await start()
  await flushPromises()
  await input.setValue('beach')
  await settle()
  const options = wrapper?.findAll('li[role="option"]').map((o) => o.text()) ?? []
  // Searching for the text comes first; the recent search that is the text
  // itself is not offered twice.
  expect(options[0]).toContain('Search for “beach”')
  expect(options.slice(1)).toEqual(['beach sunset'])
})

it('forgets the recent searches when asked', async () => {
  vi.mocked(engine.recentSearches).mockResolvedValue(['beach sunset'])
  await start()
  await flushPromises()
  await wrapper?.get('[data-search-forget]').trigger('click')
  await flushPromises()
  expect(engine.clearRecentSearches).toHaveBeenCalledOnce()
  expect(wrapper?.text()).not.toContain('Recent searches')
})
