import { afterEach, expect, it } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import { defineComponent, h, ref } from 'vue'
import type { Ref } from 'vue'
import FolderTree from './FolderTree.vue'
import { mountInApp } from '../shared/testing'

function openTree(paths: Ref<string[]>) {
  const host = defineComponent({ setup: () => () => h(FolderTree, { label: 'Resulting folders', paths: paths.value }) })
  return mountInApp(host)
}

let wrapper: VueWrapper | undefined
afterEach(() => wrapper?.unmount())
it('expands real destination folders, leaves file names out and follows changed paths', async () => {
  const paths = ref([
    'D:/Photos/2026/one.jpg',
    ['D:', 'Photos', '2026', 'two.jpg'].join(String.fromCharCode(92)),
    'D:/Photos/2025/雪.jpg',
  ])
  wrapper = openTree(paths)
  expect(wrapper.get('[role="tree"]').attributes('aria-label')).toBe('Resulting folders')
  const folder = wrapper.findAll('button').find((button) => button.text() === 'Photos')
  expect(folder).toBeDefined()
  expect(folder?.attributes('aria-expanded')).toBe('true')
  await folder?.trigger('click')
  await flushPromises()
  expect(wrapper.text()).not.toContain('2026')
  await folder?.trigger('click')
  await flushPromises()
  expect(wrapper.text()).toContain('2026')
  expect(wrapper.text()).toContain('2025')
  expect(wrapper.text()).not.toContain('one.jpg')
  expect(wrapper.findAll('button').filter((button) => button.text() === '2026')).toHaveLength(1)
  paths.value = ['E:/Archive/three.jpg']
  await flushPromises()
  expect(wrapper.text()).toContain('E:')
  expect(wrapper.text()).not.toContain('Photos')
})

it('keeps a branch collapsed when another destination is added', async () => {
  const paths = ref(['Photos/2026/a.jpg'])
  wrapper = openTree(paths)
  const folder = wrapper.findAll('button').find((button) => button.text() === 'Photos')
  expect(folder?.attributes('aria-expanded')).toBe('true')
  await folder?.trigger('click')
  paths.value = ['Photos/2026/a.jpg', 'Photos/2025/b.jpg']
  await flushPromises()
  expect(folder?.attributes('aria-expanded')).toBe('false')
  expect(wrapper.text()).not.toContain('2026')
  await folder?.trigger('click')
  await flushPromises()
  expect(wrapper.text()).toContain('2025')
  expect(wrapper.text()).toContain('2026')
})
