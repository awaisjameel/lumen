import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { enableAutoUnmount, flushPromises } from '@vue/test-utils'
import { mountInApp } from '../shared/testing'
import { engine, ErrorCode } from '../bridge'
import type * as Bridge from '../bridge'
import { useLibrary } from '../library/store'
import Welcome from './Welcome.vue'

enableAutoUnmount(afterEach)

vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  engine: {
    suggestedFolders: vi.fn<() => Promise<string[]>>(),
    sourcePathKey: vi.fn<(path: string) => Promise<string>>(),
    pickFolder: vi.fn<() => Promise<string>>(),
    addSources: vi.fn<(paths: string[]) => Promise<Bridge.Source[]>>(),
  },
}))

beforeEach(() => {
  setActivePinia(createPinia())
  vi.mocked(engine.suggestedFolders).mockReset().mockResolvedValue(['C:\\Users\\me\\Pictures', 'D:\\DCIM'])
  vi.mocked(engine.pickFolder).mockReset().mockResolvedValue('')
  // These fixtures have no multi-rune folding or alternate separators.
  vi.mocked(engine.sourcePathKey)
    .mockReset()
    .mockImplementation((path) => Promise.resolve(path.toLowerCase()))
  vi.mocked(engine.addSources).mockReset().mockResolvedValue([])
})

const paths = (w: ReturnType<typeof mountInApp>) => w.findAll('.folders .path').map((p) => p.text())

it('offers the usual photo folders, ticked, and starts with the ones left ticked', async () => {
  const lib = useLibrary()
  const sourcesChanged = vi.spyOn(lib, 'sourcesChanged').mockResolvedValue()
  const w = mountInApp(Welcome)
  await flushPromises()
  expect(paths(w)).toEqual(['C:\\Users\\me\\Pictures', 'D:\\DCIM'])
  await w.findAll('.folders [role="checkbox"]')[1]?.trigger('click')
  await w.get('.start').trigger('click')
  await flushPromises()
  expect(engine.addSources).toHaveBeenCalledExactlyOnceWith(['C:\\Users\\me\\Pictures'])
  expect(sourcesChanged).toHaveBeenCalledOnce()
})

it('adds a picked folder once, however its letters are cased', async () => {
  vi.mocked(engine.suggestedFolders).mockResolvedValue(['D:\\Photos\\Été'])
  vi.mocked(engine.pickFolder).mockResolvedValueOnce('d:\\photos\\ÉTÉ').mockResolvedValueOnce('D:\\Photos\\Ete')
  const w = mountInApp(Welcome)
  await flushPromises()
  const add = w.get('.add')
  await add.trigger('click')
  await flushPromises()
  expect(paths(w)).toEqual(['D:\\Photos\\Été'])
  await add.trigger('click') // without the accent, another folder
  await flushPromises()
  expect(paths(w)).toEqual(['D:\\Photos\\Été', 'D:\\Photos\\Ete'])
})

it('says why the folders could not be added, and waits with nothing chosen', async () => {
  vi.mocked(engine.suggestedFolders).mockResolvedValue([])
  const w = mountInApp(Welcome)
  await flushPromises()
  expect(w.text()).toContain('We couldn’t find the usual photo folders.')
  expect(w.get<HTMLButtonElement>('.start').element.disabled).toBe(true)
  vi.mocked(engine.pickFolder).mockResolvedValueOnce('E:\\Gone')
  await w.get('.add').trigger('click')
  await flushPromises()
  vi.mocked(engine.addSources).mockRejectedValueOnce(
    new Error('folder not found', { cause: { code: ErrorCode.FolderNotFound } }),
  )
  await w.get('.start').trigger('click')
  await flushPromises()
  expect(w.get('[role="alert"]').text()).toBe('That folder can’t be found.')
})

it('keeps distinct names that locale collation equates, using the library’s keys', async () => {
  vi.mocked(engine.suggestedFolders).mockResolvedValue(['D:\\Photos\\é'])
  vi.mocked(engine.sourcePathKey).mockResolvedValueOnce('d:\\photos\\É').mockResolvedValueOnce('d:\\photos\\E\u0301')
  vi.mocked(engine.pickFolder).mockResolvedValueOnce('D:\\Photos\\e\u0301')
  const w = mountInApp(Welcome)
  await flushPromises()
  await w.get('.add').trigger('click')
  await flushPromises()
  expect(paths(w)).toEqual(['D:\\Photos\\é', 'D:\\Photos\\e\u0301'])
  w.unmount()
})

it('finishes loading after a suggested folder key fails and lets a picked folder recover', async () => {
  vi.mocked(engine.sourcePathKey).mockRejectedValueOnce(new Error('folder key unavailable'))
  const wrapper = mountInApp(Welcome)
  await flushPromises()
  expect(wrapper.find('[aria-busy="true"]').exists()).toBe(false)
  expect(wrapper.get('[role="alert"]').text()).toContain('folder key unavailable')
  vi.mocked(engine.pickFolder).mockResolvedValueOnce('D:/Photos')
  await wrapper.get('.add').trigger('click')
  await flushPromises()
  expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  expect(paths(wrapper)).toEqual(['D:/Photos'])
  expect(wrapper.get<HTMLButtonElement>('.start').element.disabled).toBe(false)
})

it('shows first-scan work, prevents duplicate starts and lets the same folders be retried after a failure', async () => {
  let fail: (error: Error) => void = () => {
    throw new Error('the first scan has not started')
  }
  vi.mocked(engine.addSources).mockReturnValueOnce(
    new Promise<Bridge.Source[]>((_resolve, reject) => {
      fail = reject
    }),
  )
  const changed = vi.spyOn(useLibrary(), 'sourcesChanged').mockResolvedValue()
  const wrapper = mountInApp(Welcome)
  await flushPromises()
  await wrapper.get('.start').trigger('click')
  expect(wrapper.get<HTMLButtonElement>('.start').element.disabled).toBe(true)
  expect(wrapper.get('[role="status"]').text()).toContain('Starting your library')
  await wrapper.get('.start').trigger('click')
  expect(engine.addSources).toHaveBeenCalledTimes(1)
  fail(new Error('scan unavailable'))
  await flushPromises()
  expect(wrapper.get('[role="alert"]').text()).toContain('scan unavailable')
  expect(wrapper.get<HTMLButtonElement>('.start').element.disabled).toBe(false)
  await wrapper.get('.start').trigger('click')
  await flushPromises()
  expect(engine.addSources).toHaveBeenLastCalledWith(['C:\\Users\\me\\Pictures', 'D:\\DCIM'])
  expect(changed).toHaveBeenCalledOnce()
})
