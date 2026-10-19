import { afterEach, expect, it } from 'vitest'
import { defineComponent, h } from 'vue'
import { flushPromises } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import IconButton from './IconButton.vue'
import { icons } from '../design/icons'
import { mountInApp } from './testing'

let wrapper: VueWrapper | undefined

afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
})

async function tooltipOf(props: Record<string, unknown>): Promise<string> {
  wrapper = mountInApp(IconButton, { props: { icon: icons.close, ...props } })
  const button = wrapper.get('button')
  expect(button.text()).toBe('')
  button.element.focus()
  await button.trigger('focus')
  await flushPromises()
  return document.body.querySelector('[role="tooltip"]')?.textContent.trim() ?? ''
}

it('says its label as the name of the button and as its tooltip', async () => {
  expect(await tooltipOf({ label: 'Remove “Beach”' })).toBe('Remove “Beach”')
  expect(wrapper?.get('button').attributes('aria-label')).toBe('Remove “Beach”')
})

it('keeps the name of a toggle while the tooltip says what a press does', async () => {
  expect(await tooltipOf({ label: 'Filmstrip', tip: 'Hide filmstrip', 'aria-pressed': true })).toBe('Hide filmstrip')
  const button = wrapper?.get('button')
  expect(button?.attributes('aria-label')).toBe('Filmstrip')
  expect(button?.attributes('aria-pressed')).toBe('true')
})

it('hands what it is given to the button', async () => {
  let clicks = 0
  wrapper = mountInApp(IconButton, {
    props: { icon: icons.close, label: 'Close', class: 'mine', disabled: false, onClick: () => clicks++ },
  })
  const button = wrapper.get('button')
  await button.trigger('click')
  expect(clicks).toBe(1)
  expect(button.classes()).toContain('mine')
})

// The tooltip is the root, so without care a parent's scoped rules for a
// class it puts on the button (an atlas marker's position) never match.
it('gives the button the scoped styles of the component that draws it', () => {
  const Parent = defineComponent({
    __scopeId: 'data-v-parent',
    render: () => h(IconButton, { icon: icons.close, label: 'Close', class: 'marker' }),
  })
  wrapper = mountInApp(Parent)
  expect(wrapper.get('button.marker').attributes()).toHaveProperty('data-v-parent')
})

// A button that shows only an icon says what it does only through
// IconButton; one written as a bare UButton has no tooltip.
it('is the only way a component draws a button with no words', () => {
  const sources = import.meta.glob<string>('../**/*.vue', { query: '?raw', import: 'default', eager: true })
  const button = /<UButton\b((?:"[^"]*"|[^>"])*?)(?:\/>|>([\s\S]*?)<\/UButton\s*>)/g
  const bare: string[] = []
  for (const [file, source] of Object.entries(sources)) {
    if (file.endsWith('/IconButton.vue')) continue
    for (const [, attrs = '', body = ''] of source.matchAll(button)) {
      if (/(^|\s):?icon=/.test(attrs) && !/(^|\s):?label=/.test(attrs) && !body.trim()) {
        bare.push(`${file}: <UButton${attrs.replace(/\s+/g, ' ')}>`)
      }
    }
  }
  expect(Object.keys(sources).length).toBeGreaterThan(20)
  expect(bare).toEqual([])
})
