import { beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import { engine, ErrorCode } from '../bridge'
import type * as Bridge from '../bridge'
import { useLibrary } from '../library/store'
import Welcome from './Welcome.vue'

vi.mock('../bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof Bridge>()),
  engine: {
    suggestedFolders: vi.fn<() => Promise<string[]>>(),
    pickFolder: vi.fn<() => Promise<string>>(),
    addSources: vi.fn<(paths: string[]) => Promise<Bridge.Source[]>>(),
  },
}))

beforeEach(() => {
  setActivePinia(createPinia())
  vi.mocked(engine.suggestedFolders).mockReset().mockResolvedValue(['C:\\Users\\me\\Pictures', 'D:\\DCIM'])
  vi.mocked(engine.pickFolder).mockReset().mockResolvedValue('')
  vi.mocked(engine.addSources).mockReset().mockResolvedValue([])
})

const paths = (w: ReturnType<typeof mount>) => w.findAll('.folders .path').map((p) => p.text())

it('offers the usual photo folders, ticked, and starts with the ones left ticked', async () => {
  const lib = useLibrary()
  const sourcesChanged = vi.spyOn(lib, 'sourcesChanged').mockResolvedValue()
  const w = mount(Welcome)
  await flushPromises()
  expect(paths(w)).toEqual(['C:\\Users\\me\\Pictures', 'D:\\DCIM'])
  await w.findAll('.folders input')[1]?.setValue(false)
  await w.get('.start').trigger('click')
  await flushPromises()
  expect(engine.addSources).toHaveBeenCalledExactlyOnceWith(['C:\\Users\\me\\Pictures'])
  expect(sourcesChanged).toHaveBeenCalledOnce()
})

it('adds a picked folder once, however its letters are cased', async () => {
  vi.mocked(engine.suggestedFolders).mockResolvedValue(['D:\\Photos\\Été'])
  vi.mocked(engine.pickFolder).mockResolvedValueOnce('d:\\photos\\ÉTÉ').mockResolvedValueOnce('D:\\Photos\\Ete')
  const w = mount(Welcome)
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
  const w = mount(Welcome)
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
