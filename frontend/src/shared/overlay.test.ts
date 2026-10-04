import { afterEach, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { defineComponent, h, nextTick, ref } from 'vue'
import { layerOpen, portalOpen, shortcutsSuspended, useLayer } from './overlay'

afterEach(() => {
  document.body.innerHTML = ''
})

/** A component that is one layer, exposing whether it owns the keyboard. */
function layer(open = ref(true)) {
  let owns: () => boolean = () => false
  const wrapper = mount(
    defineComponent({
      setup() {
        owns = useLayer(open).owns
        return () => h('div')
      },
    }),
  )
  return { wrapper, owns: () => owns(), open }
}

it('gives the keyboard to the newest open layer, and back when it closes', async () => {
  expect(layerOpen()).toBe(false)
  const viewer = layer()
  expect(viewer.owns()).toBe(true)
  expect(shortcutsSuspended()).toBe(true)

  const review = layer()
  expect(review.owns()).toBe(true)
  expect(viewer.owns()).toBe(false)

  review.wrapper.unmount()
  expect(viewer.owns()).toBe(true)
  viewer.wrapper.unmount()
  await nextTick()
  expect(layerOpen()).toBe(false)
  expect(shortcutsSuspended()).toBe(false)
})

it('registers a layer only while it is open', async () => {
  const open = ref(false)
  const l = layer(open)
  expect(layerOpen()).toBe(false)
  expect(l.owns()).toBe(false)
  open.value = true
  await nextTick()
  expect(l.owns()).toBe(true)
  open.value = false
  await nextTick()
  expect(layerOpen()).toBe(false)
  l.wrapper.unmount()
})

it('recognises the portalled overlays of Nuxt UI, which take the keyboard from a layer', () => {
  const viewer = layer()
  for (const role of ['dialog', 'alertdialog', 'menu', 'listbox']) {
    const el = document.createElement('div')
    el.setAttribute('role', role)
    el.dataset.state = 'open'
    document.body.append(el)
    expect(portalOpen(), role).toBe(true)
    expect(viewer.owns(), role).toBe(false)
    el.remove()
    expect(portalOpen(), role).toBe(false)
  }
  // A tooltip shows on hover and takes no key: the layer keeps the keyboard.
  const tip = document.createElement('div')
  tip.setAttribute('role', 'tooltip')
  tip.dataset.state = 'delayed-open'
  const popper = document.createElement('div')
  popper.setAttribute('data-reka-popper-content-wrapper', '')
  popper.append(tip)
  document.body.append(popper)
  expect(portalOpen()).toBe(false)
  expect(viewer.owns()).toBe(true)
  popper.remove()
  // The grid is a listbox too, but not an open overlay: page keys stay with it.
  const grid = document.createElement('div')
  grid.setAttribute('role', 'listbox')
  document.body.append(grid)
  expect(portalOpen()).toBe(false)
  expect(viewer.owns()).toBe(true)
  viewer.wrapper.unmount()
})
