import { expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { DateSource, engine, Kind, ViewerOpen, ViewerWheel } from '../bridge'
import type * as Bridge from '../bridge'
import type { AssetDetail, RenderInfo } from '../bridge'
import ImageSurface from './ImageSurface.vue'

vi.mock('../bridge', async (original) => ({
  ...(await original<typeof Bridge>()),
  engine: { render: vi.fn(), inspect: vi.fn() },
}))
vi.mock('../shared/observeResize', () => ({ observeResize: () => () => undefined }))

function deferred<T>() {
  let resolve: (value: T) => void = () => {
    throw new Error('Promise is not initialised')
  }
  const promise = new Promise<T>((done) => {
    resolve = done
  })
  return { promise, resolve }
}
function detail(id: number): AssetDetail {
  return {
    id,
    kind: Kind.KindPhoto,
    takenAt: 0,
    dateFrom: DateSource.DateFromFile,
    width: 6000,
    height: 4000,
    camera: '',
    lat: null,
    lon: null,
    placeId: 0,
    favourite: false,
    paths: [],
    size: 1000,
    ext: '.tiff',
    video: null,
    caps: { preview: true, original: false, render: true, paged: false, directPlay: false, convertPlay: false },
  }
}
const render: RenderInfo = {
  width: 4096,
  height: 2731,
  limited: true,
  preview: false,
  page: 0,
  revision: 'new',
}

it('turns through the pages of a file that holds several', async () => {
  const paged = { ...detail(1), caps: { ...detail(1).caps, paged: true } }
  const inspection = {
    orientation: 1,
    hasProfile: false,
    profile: '',
    lens: '',
    exposure: '',
    aperture: '',
    iso: '',
    frameRate: '',
    pages: 3,
    tracks: [],
  }
  vi.mocked(engine.inspect).mockResolvedValue(inspection)
  vi.mocked(engine.render).mockImplementation((_id, _signal, page = 0) =>
    Promise.resolve({ ...render, page, revision: `page-${page}` }),
  )
  const wrapper = mount(ImageSurface, { props: { id: 1, thumb: 1, width: 6000, height: 4000, detail: paged } })
  try {
    await flushPromises()
    // The viewer's controls read these and turn the page.
    expect([wrapper.vm.page, wrapper.vm.pages]).toEqual([0, 3])
    wrapper.vm.turnPage(-1)
    expect(wrapper.vm.page).toBe(0)
    wrapper.vm.turnPage(1)
    await flushPromises()
    expect(wrapper.vm.page).toBe(1)
    expect(wrapper.get('.original').attributes('src')).toContain('/render/1/4096?v=page-1&page=1')
    wrapper.vm.turnPage(5)
    await flushPromises()
    expect(wrapper.vm.page).toBe(2)
    // Another photo starts at its own first page, with no pages to turn until it says so.
    vi.mocked(engine.inspect).mockResolvedValue({ ...inspection, pages: 1 })
    await wrapper.setProps({ id: 2, detail: { ...paged, id: 2 } })
    await flushPromises()
    expect([wrapper.vm.page, wrapper.vm.pages]).toEqual([0, 1])
    expect(wrapper.get('.original').attributes('src')).toContain('/render/2/4096?v=page-0')
  } finally {
    wrapper.unmount()
  }
})

it('says when a RAW file shows only its embedded camera preview', async () => {
  vi.mocked(engine.render).mockResolvedValue({ ...render, width: 1616, height: 1080, preview: true })
  const wrapper = mount(ImageSurface, { props: { id: 1, thumb: 1, width: 6000, height: 4000, detail: detail(1) } })
  try {
    await flushPromises()
    expect(wrapper.get('[role="status"]').text()).toContain('Camera preview from the RAW file: 1,616 × 1,080')
  } finally {
    wrapper.unmount()
  }
})

it('cancels navigation and refuses a late render from the previous asset', async () => {
  const first = deferred<RenderInfo>(),
    second = deferred<RenderInfo>()
  const signals: AbortSignal[] = []
  vi.mocked(engine.render).mockImplementation((id, signal) => {
    if (signal) signals.push(signal)
    return id === 1 ? first.promise : second.promise
  })
  const wrapper = mount(ImageSurface, { props: { id: 1, thumb: 1, width: 6000, height: 4000, detail: detail(1) } })
  try {
    await wrapper.setProps({ id: 2, detail: detail(2) })
    expect(signals[0]?.aborted).toBe(true)
    second.resolve(render)
    await flushPromises()
    first.resolve({ ...render, revision: 'old' })
    await flushPromises()
    expect(wrapper.get('.original').attributes('src')).toContain('/render/2/4096?v=new')
    expect(wrapper.text()).toContain('4,096')
  } finally {
    wrapper.unmount()
  }
  expect(signals[1]?.aborted).toBe(true)
})

it('previews a turned photo with the thumbnail the grid shows, unstretched before its details arrive', async () => {
  // The grid's size is the picture as shown: a 6000×4000 photo turned a quarter.
  const wrapper = mount(ImageSurface, { props: { id: 1, thumb: 3, width: 4000, height: 6000, detail: null } })
  try {
    const preview = () => wrapper.get<HTMLImageElement>('img.preview')
    const shape = () => [preview().element.style.width, preview().element.style.height]
    expect(preview().attributes('src')).not.toContain('canonical')
    expect(shape()).toEqual(['4000px', '6000px'])
    expect(preview().attributes('style')).not.toContain('rotate')
    await wrapper.setProps({ detail: { ...detail(1), adjustment: { rotation: 1, flip: true } } })
    expect(shape()).toEqual(['4000px', '6000px'])
    expect(preview().attributes('style')).not.toContain('rotate')
    expect(preview().attributes('style')).not.toContain('scaleX')
  } finally {
    wrapper.unmount()
  }
})

it('does not turn a photo by the details of the one shown before it', async () => {
  const turned = { ...detail(1), adjustment: { rotation: 1, flip: false }, caps: { ...detail(1).caps, original: true } }
  const wrapper = mount(ImageSurface, { props: { id: 1, thumb: 1, width: 400, height: 600, detail: turned } })
  try {
    await wrapper.setProps({ id: 2, width: 800, height: 500 })
    expect(wrapper.find('img.original').exists()).toBe(false)
    const preview = wrapper.get<HTMLImageElement>('img.preview').element
    expect([preview.style.width, preview.style.height]).toEqual(['800px', '500px'])
  } finally {
    wrapper.unmount()
  }
})

it('keeps an already loaded original visible after presentation metadata refreshes', async () => {
  const photo = { ...detail(1), width: 600, height: 400, caps: { ...detail(1).caps, original: true } }
  const wrapper = mount(ImageSurface, { props: { id: 1, thumb: 1, width: 600, height: 400, detail: photo } })
  try {
    await wrapper.get('img.original').trigger('load')
    expect(wrapper.get('img.original').isVisible()).toBe(true)
    await wrapper.setProps({ thumb: 2, detail: { ...photo, adjustment: { rotation: 1, flip: false } } })
    expect(wrapper.get('img.original').isVisible()).toBe(true)
    expect(wrapper.get('img.original').attributes('style')).toContain('rotate(90deg)')
    expect(wrapper.get('img.preview').isVisible()).toBe(false)
  } finally {
    wrapper.unmount()
  }
})

const inspected = {
  orientation: 1,
  hasProfile: false,
  profile: '',
  lens: '',
  exposure: '',
  aperture: '',
  iso: '',
  frameRate: '',
  pages: 1,
  tracks: [],
}

it('opens a photo at its actual size when that is the preference, until the user fits it', async () => {
  const photo = { ...detail(1), width: 600, height: 400, caps: { ...detail(1).caps, original: true } }
  const props = { id: 1, thumb: 1, width: 600, height: 400, detail: photo }
  const fitted = mount(ImageSurface, { props })
  const actual = mount(ImageSurface, { props: { ...props, open: ViewerOpen.ViewerOpenActual } })
  try {
    await flushPromises()
    // jsdom gives the surface no size, so a fitted picture is scaled far down.
    expect(fitted.vm.scale).toBeLessThan(100)
    expect(actual.vm.scale).toBe(100)
    actual.vm.fit()
    expect(actual.vm.scale).toBe(fitted.vm.scale)
  } finally {
    fitted.unmount()
    actual.unmount()
  }
})

it('steps between photos with the wheel when that is the preference, and zooms with Ctrl', async () => {
  const photo = { ...detail(1), width: 600, height: 400, caps: { ...detail(1).caps, original: true } }
  const props = { id: 1, thumb: 1, width: 600, height: 400, detail: photo, open: ViewerOpen.ViewerOpenActual }
  const wrapper = mount(ImageSurface, { props: { ...props, wheel: ViewerWheel.ViewerWheelNavigate } })
  const zooming = mount(ImageSurface, { props })
  try {
    await flushPromises()
    await wrapper.trigger('wheel', { deltaY: 120 })
    expect(wrapper.emitted('step')).toEqual([[1]])
    expect(wrapper.vm.scale).toBe(100)
    wrapper.element.dispatchEvent(new WheelEvent('wheel', { deltaY: -120, ctrlKey: true, cancelable: true }))
    await flushPromises()
    expect(wrapper.emitted('step')).toHaveLength(1)
    expect(wrapper.vm.scale).toBeGreaterThan(100)
    // The default wheel zooms and never steps.
    await zooming.trigger('wheel', { deltaY: -120 })
    expect(zooming.emitted('step')).toBeUndefined()
    expect(zooming.vm.scale).toBeGreaterThan(100)
  } finally {
    wrapper.unmount()
    zooming.unmount()
  }
})

it('asks for viewing copies at the preferred size', async () => {
  vi.mocked(engine.render)
    .mockReset()
    .mockResolvedValue({ ...render, width: 2048, height: 1365 })
  const wrapper = mount(ImageSurface, {
    props: { id: 1, thumb: 1, width: 6000, height: 4000, detail: detail(1), side: 2048 },
  })
  try {
    await flushPromises()
    expect(vi.mocked(engine.render).mock.calls[0]?.[3]).toBe(2048)
    expect(wrapper.get('.original').attributes('src')).toContain('/render/1/2048?v=new')
  } finally {
    wrapper.unmount()
  }
})

it('holds an animated image on its first frame until it is played', async () => {
  vi.mocked(engine.render)
    .mockReset()
    .mockResolvedValue({ ...render, width: 600, height: 400, limited: false })
  vi.mocked(engine.inspect)
    .mockReset()
    .mockResolvedValue({ ...inspected, pages: 12 })
  const gif = { ...detail(1), ext: '.gif', width: 600, height: 400, caps: { ...detail(1).caps, original: true } }
  const wrapper = mount(ImageSurface, { props: { id: 1, thumb: 1, width: 600, height: 400, detail: gif, still: true } })
  try {
    // Nothing of the original is asked for before the file says whether it animates.
    expect(wrapper.find('img.original').exists()).toBe(false)
    await flushPromises()
    expect(wrapper.vm.animated).toBe(true)
    expect(wrapper.vm.playing).toBe(false)
    expect(wrapper.get('img.original').attributes('src')).toContain('/render/1/')
    wrapper.vm.toggleAnimation()
    await flushPromises()
    expect(wrapper.get('img.original').attributes('src')).toBe('/media/original/1')
    wrapper.vm.toggleAnimation()
    await flushPromises()
    expect(wrapper.get('img.original').attributes('src')).toContain('/render/1/')
  } finally {
    wrapper.unmount()
  }
})

it('shows a still original as ever when animations are held, and an animated one when they are not', async () => {
  vi.mocked(engine.render).mockReset()
  vi.mocked(engine.inspect).mockReset().mockResolvedValue(inspected)
  const png = { ...detail(1), ext: '.png', width: 600, height: 400, caps: { ...detail(1).caps, original: true } }
  const props = { id: 1, thumb: 1, width: 600, height: 400, detail: png }
  const held = mount(ImageSurface, { props: { ...props, still: true } })
  const free = mount(ImageSurface, { props })
  try {
    await flushPromises()
    expect(held.vm.animated).toBe(false)
    expect(held.get('img.original').attributes('src')).toBe('/media/original/1')
    expect(free.get('img.original').attributes('src')).toBe('/media/original/1')
    expect(engine.inspect).toHaveBeenCalledOnce() // only the held one had to ask
    expect(engine.render).not.toHaveBeenCalled()
  } finally {
    held.unmount()
    free.unmount()
  }
})

it('retains the preview after a viewing-copy failure and retries the same page', async () => {
  vi.mocked(engine.render).mockReset().mockRejectedValueOnce(new Error('Drive disconnected')).mockResolvedValue(render)
  const wrapper = mount(ImageSurface, { props: { id: 1, thumb: 1, width: 6000, height: 4000, detail: detail(1) } })
  try {
    await flushPromises()
    expect(wrapper.get('img.preview').isVisible()).toBe(true)
    expect(wrapper.get('[role="alert"]').text()).toContain('The preview remains available.')
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(engine.render).toHaveBeenCalledTimes(2)
    await wrapper.get('img.original').trigger('load')
    expect(wrapper.get('img.original').isVisible()).toBe(true)
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  } finally {
    wrapper.unmount()
  }
})

it('exposes zoom limits without changing the pointer-centred zoom range', async () => {
  const photo = { ...detail(1), width: 600, height: 400, caps: { ...detail(1).caps, original: true } }
  const wrapper = mount(ImageSurface, { props: { id: 1, thumb: 1, width: 600, height: 400, detail: photo } })
  try {
    await flushPromises()
    wrapper.vm.zoom(100000)
    expect(wrapper.vm.scale).toBe(800)
    expect(wrapper.vm.canZoomIn).toBe(false)
    expect(wrapper.vm.canZoomOut).toBe(true)
    wrapper.vm.zoom(0.000001)
    expect(wrapper.vm.canZoomOut).toBe(false)
    expect(wrapper.vm.canZoomIn).toBe(true)
  } finally {
    wrapper.unmount()
  }
})
