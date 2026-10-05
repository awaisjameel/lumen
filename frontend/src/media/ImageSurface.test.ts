import { expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { DateSource, engine, Kind } from '../bridge'
import type * as Bridge from '../bridge'
import type { AssetDetail, RenderInfo } from '../bridge'
import ImageSurface from './ImageSurface.vue'

vi.mock('../bridge', async (original) => ({
  ...(await original<typeof Bridge>()),
  engine: { render: vi.fn() },
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
    caps: { preview: true, original: false, render: true, directPlay: false, convertPlay: false },
  }
}
const render: RenderInfo = {
  width: 4096,
  height: 2731,
  limited: true,
  revision: 'new',
}

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
