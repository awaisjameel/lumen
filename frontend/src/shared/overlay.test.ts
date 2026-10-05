import { afterEach, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { defineComponent, h, nextTick, ref } from 'vue'
import { layerOpen, portalOpen, shortcutsSuspended, startPortalLayers, useLayer } from './overlay'

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

it('orders an overlay opened inside a portal added later, and ignores the page itself', async () => {
  const page = document.createElement('div')
  page.setAttribute('data-v-app', '')
  document.body.append(page)
  const stop = startPortalLayers()
  try {
    const portal = document.createElement('div')
    document.body.append(portal)
    await Promise.resolve()
    const dialog = document.createElement('div')
    dialog.setAttribute('role', 'dialog')
    dialog.dataset.state = 'closed'
    portal.append(dialog)
    await Promise.resolve()
    expect(dialog.style.zIndex).toBe('')
    dialog.dataset.state = 'open'
    await Promise.resolve()
    expect(dialog.style.zIndex).not.toBe('')

    // The grid's tiles come and go inside the application's root: none of it
    // is an overlay, and none of it is looked at.
    const query = vi.spyOn(document, 'querySelectorAll')
    page.append(document.createElement('div'))
    page.dataset.state = 'open'
    await Promise.resolve()
    expect(query).not.toHaveBeenCalled()
    query.mockRestore()
    portal.remove()
    page.remove()
  } finally {
    stop()
  }
})

it('paints a later confirmation above a feature dialog even when its portal comes first in the DOM', async () => {
  const confirmation = document.createElement('div')
  confirmation.setAttribute('role', 'dialog')
  confirmation.dataset.state = 'closed'
  const backdrop = document.createElement('div')
  backdrop.dataset.slot = 'overlay'
  const feature = document.createElement('div')
  feature.setAttribute('role', 'dialog')
  feature.dataset.state = 'open'
  document.body.append(backdrop, confirmation, feature)
  const stop = startPortalLayers()
  try {
    confirmation.dataset.state = 'open'
    await Promise.resolve()
    expect(Number(confirmation.style.zIndex)).toBeGreaterThan(Number(feature.style.zIndex))
    expect(backdrop.style.zIndex).toBe(confirmation.style.zIndex)
    const wrapper = document.createElement('div')
    wrapper.setAttribute('data-reka-popper-content-wrapper', '')
    const menu = document.createElement('div')
    menu.setAttribute('role', 'menu')
    menu.dataset.state = 'open'
    wrapper.append(menu)
    document.body.append(wrapper)
    await Promise.resolve()
    expect(Number(wrapper.style.zIndex)).toBeGreaterThan(Number(confirmation.style.zIndex))
    expect(shortcutsSuspended()).toBe(true)
  } finally {
    stop()
  }
  expect(confirmation.style.zIndex).toBe('')
  expect(feature.style.zIndex).toBe('')
})
